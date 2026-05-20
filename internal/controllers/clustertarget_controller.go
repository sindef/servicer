package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	platformv1alpha1 "github.com/sindef/servicer/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/clientcmd"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
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
	targetClient, serverURL, err := buildTargetClient(connectionSecret.Data["kubeconfig"], r.Scheme)
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

	// Probe and reconcile required operator packages.
	if len(target.Spec.RequiredPackages) > 0 {
		// Register the cluster with Argo CD so Applications can target it by name.
		if err := r.ensureArgoCDClusterSecret(ctx, &target, connectionSecret.Data["kubeconfig"], serverURL); err != nil {
			// Non-fatal: Argo CD may not be installed; log and continue.
			ctrl.LoggerFrom(ctx).Info("ArgoCD cluster registration skipped", "target", target.Name, "reason", err.Error())
		}

		packageStatuses, err := r.reconcilePackages(ctx, &target, targetClient)
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

// reconcilePackages probes each required OperatorPackage on the target cluster and creates
// Argo CD Applications for packages that are not yet installed.
func (r *ClusterTargetReconciler) reconcilePackages(ctx context.Context, target *platformv1alpha1.ClusterTarget, targetClient client.Client) ([]platformv1alpha1.PackageStatus, error) {
	statuses := make([]platformv1alpha1.PackageStatus, 0, len(target.Spec.RequiredPackages))

	for _, pkgName := range target.Spec.RequiredPackages {
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

		// Not installed — ensure an Argo CD Application exists for this package.
		appName, appErr := r.ensurePackageArgoApp(ctx, target, &pkg)
		if appErr != nil {
			statuses = append(statuses, platformv1alpha1.PackageStatus{
				Name:          pkgName,
				Phase:         platformv1alpha1.PackagePhaseMissing,
				Message:       fmt.Sprintf("Not installed; Argo CD Application could not be created: %v", appErr),
				LastProbeTime: &now,
			})
			continue
		}

		phase := platformv1alpha1.PackagePhaseMissing
		msg := "Not installed; Argo CD is not available to deliver this package."
		if appName != "" {
			phase = platformv1alpha1.PackagePhaseDeploying
			msg = fmt.Sprintf("Argo CD Application %q created; waiting for CRD probes to pass.", appName)
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

// probePackage returns true if all CRD probes in the package pass on the target cluster.
func (r *ClusterTargetReconciler) probePackage(ctx context.Context, targetClient client.Client, pkg *platformv1alpha1.OperatorPackage) (bool, error) {
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
	return true, nil
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
		Complete(r)
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

func hasClusterConnectionData(secret *corev1.Secret) bool {
	if secret == nil {
		return false
	}
	if len(secret.Data["kubeconfig"]) > 0 || len(secret.Data["value"]) > 0 {
		return true
	}
	if secret.StringData != nil {
		return secret.StringData["kubeconfig"] != "" || secret.StringData["value"] != ""
	}
	return false
}
