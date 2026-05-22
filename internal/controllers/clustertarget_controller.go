package controllers

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	platformv1alpha1 "github.com/sindef/servicer/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/tools/clientcmd"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// ClusterTargetReconciler reconciles ClusterTarget resources.
type ClusterTargetReconciler struct {
	client.Client
	Scheme          *runtime.Scheme
	ArgoCDNamespace string
	ArgoCDProject   string
}

func (r *ClusterTargetReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var target platformv1alpha1.ClusterTarget
	if err := r.Get(ctx, req.NamespacedName, &target); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	originalStatus := target.Status
	target.Status.ObservedGeneration = target.Generation
	target.Status.OperatorInventory = operatorInventoryFromCapabilities(target.Spec.Capabilities)
	target.Status.KubernetesVersion = clusterCapabilityValue(target.Spec.Capabilities, "kubernetesVersion", "kubernetes-version")
	requeueAfter := 30 * time.Second

	setStatusCondition(&target.Status.Conditions, target.Generation, "Accepted", metav1.ConditionTrue, "ValidationSucceeded", "Cluster target accepted for reconciliation.")

	// Resolve the connection secret.
	var connectionSecret corev1.Secret
	err := r.Get(ctx, client.ObjectKey{Name: target.Spec.ConnectionRef.Name, Namespace: target.Spec.ConnectionRef.Namespace}, &connectionSecret)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
		target.Status.Phase = "PendingCredentials"
		target.Status.Reachable = false
		target.Status.LastValidatedAt = nil
		setStatusCondition(&target.Status.Conditions, target.Generation, "Ready", metav1.ConditionFalse, "CredentialsPending", fmt.Sprintf("Waiting for connection Secret %s/%s.", target.Spec.ConnectionRef.Namespace, target.Spec.ConnectionRef.Name))
		setStatusCondition(&target.Status.Conditions, target.Generation, "Failed", metav1.ConditionFalse, "CredentialsPending", "Cluster target has not failed; credentials are still pending.")
		if !equality.Semantic.DeepEqual(originalStatus, target.Status) {
			if err := r.Status().Update(ctx, &target); err != nil {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{RequeueAfter: requeueAfter}, nil
	}
	if !hasClusterConnectionData(&connectionSecret) {
		target.Status.Phase = "PendingCredentials"
		target.Status.Reachable = false
		target.Status.LastValidatedAt = nil
		setStatusCondition(&target.Status.Conditions, target.Generation, "Ready", metav1.ConditionFalse, "CredentialsPending", fmt.Sprintf("Connection Secret %s/%s does not contain kubeconfig data yet.", connectionSecret.Namespace, connectionSecret.Name))
		setStatusCondition(&target.Status.Conditions, target.Generation, "Failed", metav1.ConditionFalse, "CredentialsPending", "Cluster target has not failed; credentials are incomplete.")
		if !equality.Semantic.DeepEqual(originalStatus, target.Status) {
			if err := r.Status().Update(ctx, &target); err != nil {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{RequeueAfter: requeueAfter}, nil
	}

	// Build a client for the target cluster.
	kubeconfigBytes := clusterConnectionData(&connectionSecret)
	targetClient, serverURL, err := buildTargetClient(kubeconfigBytes, r.Scheme)
	if err != nil {
		target.Status.Phase = "Failed"
		target.Status.Reachable = false
		setStatusCondition(&target.Status.Conditions, target.Generation, "Ready", metav1.ConditionFalse, "ClientError", fmt.Sprintf("Cannot build client for target cluster: %v", err))
		setStatusCondition(&target.Status.Conditions, target.Generation, "Failed", metav1.ConditionTrue, "ClientError", err.Error())
		if !equality.Semantic.DeepEqual(originalStatus, target.Status) {
			if err := r.Status().Update(ctx, &target); err != nil {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{RequeueAfter: requeueAfter}, nil
	}

	target.Status.Phase = "Ready"
	target.Status.Reachable = true
	now := metav1.Now()
	target.Status.LastValidatedAt = &now
	setStatusCondition(&target.Status.Conditions, target.Generation, "Ready", metav1.ConditionTrue, "ClusterValidated", "Cluster target credentials resolved for reconciliation.")
	setStatusCondition(&target.Status.Conditions, target.Generation, "Failed", metav1.ConditionFalse, "ClusterValidated", "Cluster target has not failed.")

	effectivePackages, err := effectiveRequiredPackagesForClusterTarget(ctx, r.Client, &target)
	if err != nil {
		return ctrl.Result{}, err
	}
	if packageNamesContain(effectivePackages, "yugabyte") {
		if err := ensureClusterTopologyLabels(ctx, targetClient, &target); err != nil {
			return ctrl.Result{}, err
		}
	}

	// Probe and reconcile required operator packages.
	if len(effectivePackages) > 0 {
		// Register the cluster with Argo CD so Applications can target it by name.
		if err := r.ensureArgoCDClusterSecret(ctx, &target, kubeconfigBytes, serverURL); err != nil {
			// Non-fatal: Argo CD may not be installed; log and continue.
			ctrl.LoggerFrom(ctx).Info("ArgoCD cluster registration skipped", "target", target.Name, "reason", err.Error())
		}

		packageStatuses, err := r.reconcilePackages(ctx, &target, kubeconfigBytes, targetClient, effectivePackages)
		if err != nil {
			return ctrl.Result{}, err
		}
		target.Status.Packages = packageStatuses
	} else {
		target.Status.Packages = nil
	}

	if !equality.Semantic.DeepEqual(originalStatus, target.Status) {
		if err := r.Status().Update(ctx, &target); err != nil {
			return ctrl.Result{}, err
		}
	}
	return ctrl.Result{RequeueAfter: requeueAfter}, nil
}

// reconcilePackages probes each required OperatorPackage on the target cluster,
// installs any that are missing (directly via ManifestURL, or via ArgoCD), and
// returns a status entry for each.
func (r *ClusterTargetReconciler) reconcilePackages(ctx context.Context, target *platformv1alpha1.ClusterTarget, kubeconfigBytes []byte, targetClient client.Client, packageNames []string) ([]platformv1alpha1.PackageStatus, error) {
	log := ctrl.LoggerFrom(ctx)
	statuses := make([]platformv1alpha1.PackageStatus, 0, len(packageNames))

	for _, pkgName := range packageNames {
		var pkg platformv1alpha1.OperatorPackage
		if err := r.Get(ctx, types.NamespacedName{Name: pkgName}, &pkg); err != nil {
			if apierrors.IsNotFound(err) {
				statuses = append(statuses, platformv1alpha1.PackageStatus{
					Name:    pkgName,
					Phase:   platformv1alpha1.PackagePhaseError,
					Message: fmt.Sprintf("OperatorPackage %q not found in catalog.", pkgName),
				})
				continue
			}
			return nil, err
		}

		now := metav1.Now()
		installed, err := r.probePackage(ctx, targetClient, &pkg)
		if err != nil {
			statuses = append(statuses, platformv1alpha1.PackageStatus{
				Name:          pkgName,
				Phase:         platformv1alpha1.PackagePhaseError,
				Message:       fmt.Sprintf("Probe error: %v", err),
				LastProbeTime: &now,
			})
			continue
		}

		if installed {
			statuses = append(statuses, platformv1alpha1.PackageStatus{
				Name:          pkgName,
				Phase:         platformv1alpha1.PackagePhaseInstalled,
				Message:       "All CRD probes passed.",
				LastProbeTime: &now,
			})
			continue
		}

		// Not installed — try direct install first.
		if pkg.Spec.Source.ManifestURL != "" || pkg.Spec.Source.ChartArchiveURL != "" {
			log.Info("Installing operator package directly", "package", pkgName, "manifestURL", pkg.Spec.Source.ManifestURL, "chartArchiveURL", pkg.Spec.Source.ChartArchiveURL)
			if installErr := r.installPackageDirect(ctx, kubeconfigBytes, targetClient, &pkg); installErr != nil {
				log.Error(installErr, "Direct install failed", "package", pkgName)
				statuses = append(statuses, platformv1alpha1.PackageStatus{
					Name:          pkgName,
					Phase:         platformv1alpha1.PackagePhaseError,
					Message:       fmt.Sprintf("Direct install failed: %v", installErr),
					LastProbeTime: &now,
				})
				continue
			}
			// Re-probe immediately — CRDs may already be established.
			installed, _ = r.probePackage(ctx, targetClient, &pkg)
			phase := platformv1alpha1.PackagePhaseDeploying
			msg := "Direct install applied; waiting for operator to become ready."
			if installed {
				phase = platformv1alpha1.PackagePhaseInstalled
				msg = "All CRD probes passed."
			}
			statuses = append(statuses, platformv1alpha1.PackageStatus{
				Name:          pkgName,
				Phase:         phase,
				Message:       msg,
				LastProbeTime: &now,
			})
			continue
		}

		// Fall back to ArgoCD Application.
		appName, appErr := r.ensurePackageArgoApp(ctx, target, &pkg)
		if appErr != nil {
			statuses = append(statuses, platformv1alpha1.PackageStatus{
				Name:          pkgName,
				Phase:         platformv1alpha1.PackagePhaseMissing,
				Message:       fmt.Sprintf("Not installed; ArgoCD Application could not be created: %v", appErr),
				LastProbeTime: &now,
			})
			continue
		}

		phase := platformv1alpha1.PackagePhaseMissing
		msg := "Not installed; no manifestURL configured and ArgoCD is not available."
		if appName != "" {
			phase = platformv1alpha1.PackagePhaseDeploying
			msg = fmt.Sprintf("ArgoCD Application %q created; waiting for CRD probes to pass.", appName)
		}
		statuses = append(statuses, platformv1alpha1.PackageStatus{
			Name:          pkgName,
			Phase:         phase,
			Message:       msg,
			ArgoAppName:   appName,
			LastProbeTime: &now,
		})
	}
	return statuses, nil
}

// installPackageDirect fetches the manifest from pkg.Spec.Source.ManifestURL and
// applies every document to the target cluster using server-side apply.
func (r *ClusterTargetReconciler) installPackageDirect(ctx context.Context, kubeconfigBytes []byte, targetClient client.Client, pkg *platformv1alpha1.OperatorPackage) error {
	targetNamespace := firstNonEmptyTrimmed(pkg.Spec.TargetNamespace, "operators")
	if targetNamespace != "" {
		namespace := &corev1.Namespace{
			TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Namespace"},
			ObjectMeta: metav1.ObjectMeta{
				Name: targetNamespace,
			},
		}
		if err := targetClient.Patch(ctx, namespace, client.Apply, client.FieldOwner("servicer-operator-installer"), client.ForceOwnership); err != nil {
			return fmt.Errorf("ensuring target namespace %q: %w", targetNamespace, err)
		}
	}
	if pkg.Spec.Source.ManifestURL != "" {
		body, err := fetchURLBytes(pkg.Spec.Source.ManifestURL)
		if err != nil {
			return err
		}
		if err := applyManifestBytes(ctx, targetClient, body, targetNamespace); err != nil {
			return err
		}
	}
	if pkg.Spec.Source.ChartArchiveURL != "" {
		if err := installPackageHelmChart(ctx, kubeconfigBytes, targetClient, pkg); err != nil {
			return err
		}
	}
	return nil
}

func fetchURLBytes(url string) ([]byte, error) {
	resp, err := http.Get(url) //nolint:noctx
	if err != nil {
		return nil, fmt.Errorf("fetching %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetching %s: HTTP %d", url, resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", url, err)
	}
	return body, nil
}

func applyManifestBytes(ctx context.Context, targetClient client.Client, body []byte, defaultNamespace string) error {
	reader := utilyaml.NewYAMLReader(bufio.NewReader(bytes.NewReader(body)))
	var applyErr error
	for {
		docBytes, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("parsing manifest: %w", err)
		}
		docBytes = bytes.TrimSpace(docBytes)
		if len(docBytes) == 0 {
			continue
		}
		jsonBytes, err := utilyaml.ToJSON(docBytes)
		if err != nil || bytes.Equal(jsonBytes, []byte("null")) {
			continue
		}
		obj := &unstructured.Unstructured{}
		if err := json.Unmarshal(jsonBytes, obj); err != nil {
			continue
		}
		if obj.GetKind() == "" || obj.GetAPIVersion() == "" {
			continue
		}
		normalizeManifestObject(obj)
		if shouldDefaultObjectNamespace(obj) && defaultNamespace != "" {
			obj.SetNamespace(defaultNamespace)
		}
		obj.SetManagedFields(nil)
		if err := targetClient.Patch(ctx, obj, client.Apply,
			client.FieldOwner("servicer-operator-installer"),
			client.ForceOwnership,
		); err != nil {
			applyErr = fmt.Errorf("applying %s %s/%s: %w", obj.GetKind(), obj.GetNamespace(), obj.GetName(), err)
		}
	}
	return applyErr
}

func installPackageHelmChart(ctx context.Context, kubeconfigBytes []byte, targetClient client.Client, pkg *platformv1alpha1.OperatorPackage) error {
	_ = kubeconfigBytes
	archiveBytes, err := fetchURLBytes(pkg.Spec.Source.ChartArchiveURL)
	if err != nil {
		return err
	}

	workdir, err := os.MkdirTemp("", "servicer-operator-chart-*")
	if err != nil {
		return fmt.Errorf("creating helm workdir: %w", err)
	}
	defer os.RemoveAll(workdir)

	if err := extractTarGz(workdir, archiveBytes); err != nil {
		return err
	}

	chartPath, err := resolveExtractedChartPath(workdir, pkg.Spec.Source.ChartPath)
	if err != nil {
		return err
	}

	releaseName := pkg.Name
	targetNamespace := firstNonEmptyTrimmed(pkg.Spec.TargetNamespace, "operators")
	if targetNamespace != "" {
		namespace := &corev1.Namespace{
			TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Namespace"},
			ObjectMeta: metav1.ObjectMeta{
				Name: targetNamespace,
			},
		}
		if err := targetClient.Patch(ctx, namespace, client.Apply, client.FieldOwner("servicer-operator-installer"), client.ForceOwnership); err != nil {
			return fmt.Errorf("ensuring target namespace %q: %w", targetNamespace, err)
		}
	}
	args := []string{
		"template", releaseName, chartPath,
		"--namespace", targetNamespace,
		"--include-crds",
		"--no-hooks",
	}
	keys := make([]string, 0, len(pkg.Spec.Source.HelmValues))
	for key := range pkg.Spec.Source.HelmValues {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		args = append(args, "--set", fmt.Sprintf("%s=%s", key, pkg.Spec.Source.HelmValues[key]))
	}

	cmd := exec.Command("/helm", args...) //nolint:gosec
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("helm template failed: %w: %s", err, strings.TrimSpace(string(output)))
	}
	if err := applyManifestBytes(ctx, targetClient, output, targetNamespace); err != nil {
		return fmt.Errorf("applying rendered helm chart: %w", err)
	}
	return nil
}

func shouldDefaultObjectNamespace(obj *unstructured.Unstructured) bool {
	if obj == nil {
		return false
	}
	if obj.GetKind() == "Namespace" {
		return false
	}
	switch obj.GroupVersionKind().Group {
	case "apiextensions.k8s.io", "admissionregistration.k8s.io", "apiregistration.k8s.io", "rbac.authorization.k8s.io", "storage.k8s.io", "scheduling.k8s.io":
		switch obj.GetKind() {
		case "Role", "RoleBinding":
			return true
		default:
			return false
		}
	}
	return true
}

func normalizeManifestObject(obj *unstructured.Unstructured) {
	if obj == nil {
		return
	}
	if obj.GetKind() == "PodDisruptionBudget" && obj.GetAPIVersion() == "policy/v1beta1" {
		obj.SetAPIVersion("policy/v1")
	}
}

func extractTarGz(destDir string, data []byte) error {
	gzipReader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("opening chart archive: %w", err)
	}
	defer gzipReader.Close()

	tarReader := tar.NewReader(gzipReader)
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("reading chart archive: %w", err)
		}

		targetPath := filepath.Join(destDir, header.Name)
		cleanTarget := filepath.Clean(targetPath)
		if !strings.HasPrefix(cleanTarget, filepath.Clean(destDir)+string(os.PathSeparator)) && cleanTarget != filepath.Clean(destDir) {
			return fmt.Errorf("chart archive contains invalid path %q", header.Name)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(cleanTarget, 0o755); err != nil {
				return fmt.Errorf("creating chart directory: %w", err)
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(cleanTarget), 0o755); err != nil {
				return fmt.Errorf("creating chart parent directory: %w", err)
			}
			file, err := os.OpenFile(cleanTarget, os.O_CREATE|os.O_RDWR|os.O_TRUNC, os.FileMode(header.Mode))
			if err != nil {
				return fmt.Errorf("creating chart file: %w", err)
			}
			if _, err := io.Copy(file, tarReader); err != nil {
				file.Close()
				return fmt.Errorf("writing chart file: %w", err)
			}
			if err := file.Close(); err != nil {
				return fmt.Errorf("closing chart file: %w", err)
			}
		}
	}
}

func resolveExtractedChartPath(workdir, chartPath string) (string, error) {
	entries, err := os.ReadDir(workdir)
	if err != nil {
		return "", fmt.Errorf("reading chart workdir: %w", err)
	}
	if len(entries) == 0 {
		return "", fmt.Errorf("chart archive did not extract any files")
	}
	rootPath := filepath.Join(workdir, entries[0].Name())
	fullPath := filepath.Join(rootPath, filepath.FromSlash(chartPath))
	info, err := os.Stat(fullPath)
	if err != nil {
		return "", fmt.Errorf("resolving chart path %q: %w", chartPath, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("chart path %q is not a directory", chartPath)
	}
	return fullPath, nil
}

// probePackage returns true if all CRD probes in the package pass on the target cluster.
func (r *ClusterTargetReconciler) probePackage(ctx context.Context, targetClient client.Client, pkg *platformv1alpha1.OperatorPackage) (bool, error) {
	if pkg.Spec.TargetNamespace != "" {
		var namespace corev1.Namespace
		if err := targetClient.Get(ctx, types.NamespacedName{Name: pkg.Spec.TargetNamespace}, &namespace); err != nil {
			if apierrors.IsNotFound(err) {
				return false, nil
			}
			return false, fmt.Errorf("probing namespace %q: %w", pkg.Spec.TargetNamespace, err)
		}
	}
	for _, probe := range pkg.Spec.Probes {
		crd := &unstructured.Unstructured{}
		crd.SetGroupVersionKind(schema.GroupVersionKind{Group: "apiextensions.k8s.io", Version: "v1", Kind: "CustomResourceDefinition"})
		if err := targetClient.Get(ctx, types.NamespacedName{Name: probe.CRD}, crd); err != nil {
			if apierrors.IsNotFound(err) {
				return false, nil
			}
			return false, fmt.Errorf("probing CRD %q: %w", probe.CRD, err)
		}
	}
	if pkg.Spec.TargetNamespace != "" {
		ready, err := operatorWorkloadsReady(ctx, targetClient, pkg.Spec.TargetNamespace)
		if err != nil {
			return false, err
		}
		if !ready {
			return false, nil
		}
	}
	return true, nil
}

func operatorWorkloadsReady(ctx context.Context, targetClient client.Client, namespace string) (bool, error) {
	var deployments appsv1.DeploymentList
	if err := targetClient.List(ctx, &deployments, client.InNamespace(namespace)); err != nil {
		return false, fmt.Errorf("listing deployments in namespace %q: %w", namespace, err)
	}
	var statefulSets appsv1.StatefulSetList
	if err := targetClient.List(ctx, &statefulSets, client.InNamespace(namespace)); err != nil {
		return false, fmt.Errorf("listing statefulsets in namespace %q: %w", namespace, err)
	}
	var daemonSets appsv1.DaemonSetList
	if err := targetClient.List(ctx, &daemonSets, client.InNamespace(namespace)); err != nil {
		return false, fmt.Errorf("listing daemonsets in namespace %q: %w", namespace, err)
	}

	workloadCount := len(deployments.Items) + len(statefulSets.Items) + len(daemonSets.Items)
	if workloadCount == 0 {
		return false, nil
	}
	for _, deployment := range deployments.Items {
		if deployment.Status.AvailableReplicas < replicasOrDefault(deployment.Spec.Replicas) {
			return false, nil
		}
	}
	for _, statefulSet := range statefulSets.Items {
		if statefulSet.Status.ReadyReplicas < replicasOrDefault(statefulSet.Spec.Replicas) {
			return false, nil
		}
	}
	for _, daemonSet := range daemonSets.Items {
		if daemonSet.Status.NumberAvailable < daemonSet.Status.DesiredNumberScheduled {
			return false, nil
		}
	}
	return true, nil
}

func replicasOrDefault(replicas *int32) int32 {
	if replicas == nil {
		return 1
	}
	return *replicas
}

// ensurePackageArgoApp creates (or updates) an Argo CD Application on the management cluster
// that will deploy the operator package to the target cluster.
// Returns the Application name (empty string if Argo CD is not installed).
func (r *ClusterTargetReconciler) ensurePackageArgoApp(ctx context.Context, target *platformv1alpha1.ClusterTarget, pkg *platformv1alpha1.OperatorPackage) (string, error) {
	// Check if Argo CD is installed.
	argoCDNS := r.argoCDNamespace()
	crd := &unstructured.Unstructured{}
	crd.SetGroupVersionKind(schema.GroupVersionKind{Group: "apiextensions.k8s.io", Version: "v1", Kind: "CustomResourceDefinition"})
	if err := r.Get(ctx, types.NamespacedName{Name: "applications.argoproj.io"}, crd); err != nil {
		if apierrors.IsNotFound(err) {
			return "", nil // Argo CD not installed — skip silently.
		}
		return "", err
	}

	appName := fmt.Sprintf("operator-%s-%s", target.Name, pkg.Name)
	revision := pkg.Spec.Source.TargetRevision
	if revision == "" {
		revision = "HEAD"
	}

	desired := &unstructured.Unstructured{}
	desired.SetGroupVersionKind(schema.GroupVersionKind{Group: "argoproj.io", Version: "v1alpha1", Kind: "Application"})
	desired.SetName(appName)
	desired.SetNamespace(argoCDNS)
	desired.SetLabels(map[string]string{
		"servicer.io/managed-by":     "servicer",
		"servicer.io/operator-pkg":   pkg.Name,
		"servicer.io/cluster-target": target.Name,
	})

	targetNS := pkg.Spec.TargetNamespace
	if targetNS == "" {
		targetNS = "operators"
	}

	desired.Object["spec"] = map[string]any{
		"project": r.argoCDProject(),
		"source": map[string]any{
			"repoURL":        pkg.Spec.Source.RepoURL,
			"targetRevision": revision,
			"path":           pkg.Spec.Source.Path,
			"directory": map[string]any{
				"recurse": true,
			},
		},
		"destination": map[string]any{
			"name":      target.Name,
			"namespace": targetNS,
		},
		"syncPolicy": map[string]any{
			"automated": map[string]any{
				"prune":    true,
				"selfHeal": true,
			},
			"syncOptions": []string{
				"CreateNamespace=true",
				"ServerSideApply=true",
			},
		},
	}

	var existing unstructured.Unstructured
	existing.SetGroupVersionKind(desired.GroupVersionKind())
	err := r.Get(ctx, types.NamespacedName{Name: appName, Namespace: argoCDNS}, &existing)
	if apierrors.IsNotFound(err) {
		if err := r.Create(ctx, desired); err != nil {
			return "", err
		}
		return appName, nil
	}
	if err != nil {
		return "", err
	}
	existing.Object["spec"] = desired.Object["spec"]
	existing.SetLabels(mergeStringMaps(existing.GetLabels(), desired.GetLabels()))
	if err := r.Update(ctx, &existing); err != nil {
		return "", err
	}
	return appName, nil
}

// ensureArgoCDClusterSecret creates or updates the Argo CD cluster registration Secret so that
// Argo CD Applications can target this cluster by name.
func (r *ClusterTargetReconciler) ensureArgoCDClusterSecret(ctx context.Context, target *platformv1alpha1.ClusterTarget, kubeconfigBytes []byte, serverURL string) error {
	argoCDNS := r.argoCDNamespace()

	// Only proceed if Argo CD is installed.
	var ns corev1.Namespace
	if err := r.Get(ctx, types.NamespacedName{Name: argoCDNS}, &ns); err != nil {
		return fmt.Errorf("ArgoCD namespace %q not found", argoCDNS)
	}

	restCfg, err := clientcmd.RESTConfigFromKubeConfig(kubeconfigBytes)
	if err != nil {
		return fmt.Errorf("parsing kubeconfig: %w", err)
	}

	// Build the Argo CD cluster config JSON.
	type tlsConfig struct {
		CAData   []byte `json:"caData,omitempty"`
		CertData []byte `json:"certData,omitempty"`
		KeyData  []byte `json:"keyData,omitempty"`
		Insecure bool   `json:"insecure,omitempty"`
	}
	type argoClusterConfig struct {
		BearerToken     string    `json:"bearerToken,omitempty"`
		TLSClientConfig tlsConfig `json:"tlsClientConfig"`
	}
	clusterCfg := argoClusterConfig{
		BearerToken: restCfg.BearerToken,
		TLSClientConfig: tlsConfig{
			CAData:   restCfg.TLSClientConfig.CAData,
			CertData: restCfg.TLSClientConfig.CertData,
			KeyData:  restCfg.TLSClientConfig.KeyData,
			Insecure: restCfg.TLSClientConfig.Insecure,
		},
	}
	cfgJSON, err := json.Marshal(clusterCfg)
	if err != nil {
		return fmt.Errorf("marshalling cluster config: %w", err)
	}

	secretName := "cluster-" + strings.ReplaceAll(strings.ReplaceAll(target.Name, ".", "-"), "/", "-")
	desired := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: argoCDNS,
			Labels: map[string]string{
				"argocd.argoproj.io/secret-type": "cluster",
				"servicer.io/managed-by":         "servicer",
				"servicer.io/cluster-target":     target.Name,
			},
		},
		StringData: map[string]string{
			"name":   target.Name,
			"server": serverURL,
			"config": string(cfgJSON),
		},
	}

	var existing corev1.Secret
	err = r.Get(ctx, types.NamespacedName{Name: secretName, Namespace: argoCDNS}, &existing)
	if apierrors.IsNotFound(err) {
		return r.Create(ctx, desired)
	}
	if err != nil {
		return err
	}
	existing.StringData = desired.StringData
	existing.Labels = mergeStringMaps(existing.Labels, desired.Labels)
	return r.Update(ctx, &existing)
}

func (r *ClusterTargetReconciler) argoCDNamespace() string {
	if r.ArgoCDNamespace != "" {
		return r.ArgoCDNamespace
	}
	return "argocd"
}

func (r *ClusterTargetReconciler) argoCDProject() string {
	if r.ArgoCDProject != "" {
		return r.ArgoCDProject
	}
	return "default"
}

func (r *ClusterTargetReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&platformv1alpha1.ClusterTarget{}).
		Watches(
			&corev1.Secret{},
			handler.EnqueueRequestsFromMapFunc(r.clusterTargetsForConnectionSecret),
		).
		Complete(r)
}

func (r *ClusterTargetReconciler) clusterTargetsForConnectionSecret(ctx context.Context, obj client.Object) []reconcile.Request {
	secret, ok := obj.(*corev1.Secret)
	if !ok {
		return nil
	}

	var targets platformv1alpha1.ClusterTargetList
	if err := r.List(ctx, &targets); err != nil {
		return nil
	}

	requests := make([]reconcile.Request, 0, len(targets.Items))
	for i := range targets.Items {
		target := &targets.Items[i]
		if target.Spec.ConnectionRef.Name != secret.Name || target.Spec.ConnectionRef.Namespace != secret.Namespace {
			continue
		}
		requests = append(requests, reconcile.Request{NamespacedName: types.NamespacedName{Name: target.Name}})
	}
	return requests
}

// buildTargetClient constructs a controller-runtime client and returns the server URL
// from the provided kubeconfig bytes.
func buildTargetClient(kubeconfigBytes []byte, scheme *runtime.Scheme) (client.Client, string, error) {
	restCfg, err := clientcmd.RESTConfigFromKubeConfig(kubeconfigBytes)
	if err != nil {
		return nil, "", fmt.Errorf("parsing kubeconfig: %w", err)
	}
	c, err := client.New(restCfg, client.Options{Scheme: scheme})
	if err != nil {
		return nil, "", fmt.Errorf("building client: %w", err)
	}
	return c, restCfg.Host, nil
}

func ensureClusterTopologyLabels(ctx context.Context, targetClient client.Client, target *platformv1alpha1.ClusterTarget) error {
	var nodes corev1.NodeList
	if err := targetClient.List(ctx, &nodes); err != nil {
		return fmt.Errorf("listing target cluster nodes for topology labels: %w", err)
	}

	region := firstNonEmptyTrimmed(
		target.Spec.Capabilities["topology.kubernetes.io/region"],
		target.Spec.Capabilities["failure-domain.beta.kubernetes.io/region"],
		target.Spec.Region,
		target.Name,
	)
	zone := firstNonEmptyTrimmed(
		target.Spec.Capabilities["topology.kubernetes.io/zone"],
		target.Spec.Capabilities["failure-domain.beta.kubernetes.io/zone"],
		target.Spec.Capabilities["zone"],
	)
	if zone == "" {
		zone = region + "-a"
	}

	for i := range nodes.Items {
		node := &nodes.Items[i]
		updated := false
		labels := node.GetLabels()
		if labels == nil {
			labels = map[string]string{}
		}

		if labels["topology.kubernetes.io/region"] == "" {
			labels["topology.kubernetes.io/region"] = region
			updated = true
		}
		if labels["failure-domain.beta.kubernetes.io/region"] == "" {
			labels["failure-domain.beta.kubernetes.io/region"] = labels["topology.kubernetes.io/region"]
			updated = true
		}
		if labels["topology.kubernetes.io/zone"] == "" {
			labels["topology.kubernetes.io/zone"] = zone
			updated = true
		}
		if labels["failure-domain.beta.kubernetes.io/zone"] == "" {
			labels["failure-domain.beta.kubernetes.io/zone"] = labels["topology.kubernetes.io/zone"]
			updated = true
		}
		if !updated {
			continue
		}

		node.SetLabels(labels)
		if err := targetClient.Update(ctx, node); err != nil {
			return fmt.Errorf("updating topology labels on node %q: %w", node.Name, err)
		}
	}

	return nil
}

func operatorInventoryFromCapabilities(capabilities map[string]string) []string {
	inventory := make([]string, 0)
	for key, value := range capabilities {
		if !strings.HasPrefix(key, "operator.") {
			continue
		}
		if value == "" || strings.EqualFold(value, "false") || strings.EqualFold(value, "disabled") {
			continue
		}
		inventory = append(inventory, strings.TrimPrefix(key, "operator."))
	}
	sort.Strings(inventory)
	return inventory
}

func clusterCapabilityValue(capabilities map[string]string, keys ...string) string {
	for _, key := range keys {
		if value := capabilities[key]; value != "" {
			return value
		}
	}
	return ""
}

func packageNamesContain(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}

func hasClusterConnectionData(secret *corev1.Secret) bool {
	return len(clusterConnectionData(secret)) > 0
}

func clusterConnectionData(secret *corev1.Secret) []byte {
	if secret == nil {
		return nil
	}
	if len(secret.Data["kubeconfig"]) > 0 {
		return secret.Data["kubeconfig"]
	}
	if len(secret.Data["value"]) > 0 {
		return secret.Data["value"]
	}
	if secret.StringData != nil {
		if secret.StringData["kubeconfig"] != "" {
			return []byte(secret.StringData["kubeconfig"])
		}
		if secret.StringData["value"] != "" {
			return []byte(secret.StringData["value"])
		}
	}
	return nil
}
