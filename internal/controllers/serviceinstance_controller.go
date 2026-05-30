package controllers

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"path/filepath"
	"strings"
	"sync"
	"time"

	platformv1alpha1 "github.com/sindef/servicer/api/v1alpha1"
	"github.com/sindef/servicer/internal/adapters"
	"github.com/sindef/servicer/internal/deliveryrepo"
	"github.com/sindef/servicer/internal/materializer"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ServiceInstanceReconciler reconciles ServiceInstance resources.
type ServiceInstanceReconciler struct {
	client.Client
	Scheme           *runtime.Scheme
	Adapters         *adapters.Registry
	Materializer     *materializer.Materializer
	Publisher        *deliveryrepo.Publisher
	Recorder         record.EventRecorder
	ArgoCDNamespace  string
	ArgoCDProject    string
	DeliveryRepoURL  string
	DeliveryRepoRef  string
	DeliveryRepoPath string
	ProductionMode   bool
	targetClients    sync.Map // keyed by ClusterTarget name → targetClientCacheEntry
}

type targetClientCacheEntry struct {
	key    string
	client client.Client
}

// getTargetClient lazily builds and caches a client for the given ClusterTarget's remote cluster.
func (r *ServiceInstanceReconciler) getTargetClient(ctx context.Context, target *platformv1alpha1.ClusterTarget) (client.Client, error) {
	if target == nil {
		return nil, nil
	}
	if strings.TrimSpace(target.Spec.ConnectionRef.Name) == "" || strings.TrimSpace(target.Spec.ConnectionRef.Namespace) == "" {
		r.targetClients.Delete(target.Name)
		return nil, nil
	}
	var secret corev1.Secret
	if err := r.Get(ctx, types.NamespacedName{Name: target.Spec.ConnectionRef.Name, Namespace: target.Spec.ConnectionRef.Namespace}, &secret); err != nil {
		return nil, fmt.Errorf("reading connection secret for ClusterTarget %q: %w", target.Name, err)
	}
	cacheKey := targetClientCacheKey(target, &secret)
	if cached, ok := r.targetClients.Load(target.Name); ok {
		if entry, castOK := cached.(targetClientCacheEntry); castOK && entry.key == cacheKey && entry.client != nil {
			return entry.client, nil
		}
	}
	kubeconfigBytes := clusterConnectionData(&secret)
	if len(kubeconfigBytes) == 0 {
		r.targetClients.Delete(target.Name)
		return nil, nil
	}
	restCfg, err := clientcmd.RESTConfigFromKubeConfig(kubeconfigBytes)
	if err != nil {
		return nil, fmt.Errorf("parsing kubeconfig for ClusterTarget %q: %w", target.Name, err)
	}
	c, err := client.New(restCfg, client.Options{Scheme: r.Scheme})
	if err != nil {
		return nil, fmt.Errorf("building client for ClusterTarget %q: %w", target.Name, err)
	}
	r.targetClients.Store(target.Name, targetClientCacheEntry{key: cacheKey, client: c})
	return c, nil
}

func targetClientCacheKey(target *platformv1alpha1.ClusterTarget, secret *corev1.Secret) string {
	if target == nil || secret == nil {
		return ""
	}
	return strings.Join([]string{
		target.Name,
		fmt.Sprintf("%d", target.Generation),
		target.ResourceVersion,
		secret.Namespace + "/" + secret.Name,
		secret.ResourceVersion,
	}, "|")
}

const (
	instanceFinalizer             = "servicer.io/instance-cleanup"
	kubeVirtServiceClassDriver    = "kubevirt"
	kubeVirtVirtualMachineCRDName = "virtualmachines.kubevirt.io"
	kubeVirtDataVolumeCRDName     = "datavolumes.cdi.kubevirt.io"
)

func (r *ServiceInstanceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, reconcileErr error) {
	start := time.Now()
	previousPhase := ""
	failureStage := ""
	defer func() {
		observeServiceInstanceReconcileDuration(start)
		if reconcileErr != nil {
			observeServiceInstanceFailure(failureStage)
		}
	}()

	var instance platformv1alpha1.ServiceInstance
	if err := r.Get(ctx, req.NamespacedName, &instance); err != nil {
		reconcileErr = client.IgnoreNotFound(err)
		if reconcileErr != nil {
			failureStage = "load_instance"
		}
		return ctrl.Result{}, reconcileErr
	}

	originalStatus := instance.Status
	previousPhase = originalStatus.Phase
	defer func() {
		if previousPhase != instance.Status.Phase {
			observeServiceInstancePhase(instance.Status.Phase)
		}
	}()
	instance.Status.ObservedGeneration = instance.Generation

	// Handle deletion before any other logic.
	if !instance.DeletionTimestamp.IsZero() {
		return r.handleDeletion(ctx, &instance)
	}

	// Ensure finalizer is present so we can clean up on delete.
	if !serviceInstanceContains(instance.Finalizers, instanceFinalizer) {
		instance.Finalizers = append(instance.Finalizers, instanceFinalizer)
		if err := r.Update(ctx, &instance); err != nil {
			failureStage = "ensure_finalizer"
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	var project platformv1alpha1.Project
	if err := r.Get(ctx, client.ObjectKey{Name: instance.Spec.ProjectRef.Name}, &project); err != nil {
		return r.handleDependencyError(ctx, &instance, originalStatus, "ProjectUnavailable", fmt.Sprintf("Referenced Project %q is not available.", instance.Spec.ProjectRef.Name), err)
	}

	var tenant platformv1alpha1.Tenant
	if err := r.Get(ctx, client.ObjectKey{Name: project.Spec.TenantRef.Name}, &tenant); err != nil {
		return r.handleDependencyError(ctx, &instance, originalStatus, "TenantUnavailable", fmt.Sprintf("Referenced Tenant %q is not available.", project.Spec.TenantRef.Name), err)
	}

	var class platformv1alpha1.ServiceClass
	if err := r.Get(ctx, client.ObjectKey{Name: instance.Spec.ServiceClassRef.Name}, &class); err != nil {
		return r.handleDependencyError(ctx, &instance, originalStatus, "ServiceClassUnavailable", fmt.Sprintf("Referenced ServiceClass %q is not available.", instance.Spec.ServiceClassRef.Name), err)
	}

	var plan platformv1alpha1.ServicePlan
	if err := r.Get(ctx, client.ObjectKey{Name: instance.Spec.ServicePlanRef.Name}, &plan); err != nil {
		return r.handleDependencyError(ctx, &instance, originalStatus, "ServicePlanUnavailable", fmt.Sprintf("Referenced ServicePlan %q is not available.", instance.Spec.ServicePlanRef.Name), err)
	}

	if !serviceInstanceContains(tenant.Spec.AllowedServiceClasses, instance.Spec.ServiceClassRef.Name) {
		return r.handleValidationFailure(ctx, &instance, originalStatus, "ServiceClassNotAllowed", fmt.Sprintf("Service class %q is not allowed by tenant %q.", instance.Spec.ServiceClassRef.Name, tenant.Name))
	}
	if !isStatusCurrent(project.Generation, project.Status.ObservedGeneration) || !isStatusConditionTrue(project.Status.Conditions, "Ready") {
		return r.handleDependencyPending(ctx, &instance, originalStatus, "ProjectPending", fmt.Sprintf("Referenced Project %q is not ready yet.", project.Name))
	}
	if plan.Spec.ServiceClassRef.Name != class.Name {
		return r.handleValidationFailure(ctx, &instance, originalStatus, "ServicePlanMismatch", fmt.Sprintf("Service plan %q does not belong to service class %q.", plan.Name, class.Name))
	}
	if !isStatusCurrent(class.Generation, class.Status.ObservedGeneration) {
		return r.handleDependencyPending(ctx, &instance, originalStatus, "ServiceClassPending", fmt.Sprintf("Referenced ServiceClass %q has not completed reconciliation yet.", class.Name))
	}
	if !isStatusConditionTrue(class.Status.Conditions, "Accepted") {
		return r.handleDependencyPending(ctx, &instance, originalStatus, "ServiceClassPendingAcceptance", fmt.Sprintf("Referenced ServiceClass %q has not been accepted yet; waiting for controller reconciliation.", class.Name))
	}
	if !class.Status.Published {
		return r.handleValidationFailure(ctx, &instance, originalStatus, "ServiceClassUnpublished", fmt.Sprintf("Referenced ServiceClass %q is not published for provisioning.", class.Name))
	}
	if !isStatusCurrent(plan.Generation, plan.Status.ObservedGeneration) {
		return r.handleDependencyPending(ctx, &instance, originalStatus, "ServicePlanPending", fmt.Sprintf("Referenced ServicePlan %q has not completed reconciliation yet.", plan.Name))
	}
	if !isStatusConditionTrue(plan.Status.Conditions, "Accepted") {
		return r.handleValidationFailure(ctx, &instance, originalStatus, "ServicePlanInvalid", fmt.Sprintf("Referenced ServicePlan %q is invalid and cannot be used for provisioning.", plan.Name))
	}
	if !plan.Status.Published {
		return r.handleValidationFailure(ctx, &instance, originalStatus, "ServicePlanUnpublished", fmt.Sprintf("Referenced ServicePlan %q is not published for provisioning.", plan.Name))
	}
	if policyErrs := evaluateInstancePolicies(ctx, r.Client, &instance, &tenant, &project, &plan); len(policyErrs) > 0 {
		return r.handleValidationFailure(ctx, &instance, originalStatus, "PolicyViolation", policyErrs.ToAggregate().Error())
	}
	if result, handled, err := r.enforceProjectQuota(ctx, &instance, &project, &tenant, originalStatus); handled || err != nil {
		return result, err
	}

	clusterName := resolvedClusterName(&project)
	instance.Status.Placement.ClusterName = clusterName
	instance.Status.Placement.Namespace = resolvedNamespace(&project, &instance)

	setStatusCondition(&instance.Status.Conditions, instance.Generation, "Accepted", metav1.ConditionTrue, "SpecAccepted", "Service instance accepted for reconciliation.")
	if clusterName == "" {
		instance.Status.Phase = "PendingPlacement"
		setStatusCondition(&instance.Status.Conditions, instance.Generation, "Placed", metav1.ConditionFalse, "PlacementPending", "Service instance is waiting for cluster placement.")
		if err := r.persistStatusIfChanged(ctx, &instance, originalStatus); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	var clusterTarget *platformv1alpha1.ClusterTarget
	if project.Spec.TargetSelector.ClusterRef != nil {
		var target platformv1alpha1.ClusterTarget
		if err := r.Get(ctx, client.ObjectKey{Name: project.Spec.TargetSelector.ClusterRef.Name}, &target); err != nil {
			return r.handleDependencyError(ctx, &instance, originalStatus, "ClusterTargetUnavailable", fmt.Sprintf("Referenced ClusterTarget %q is not available.", project.Spec.TargetSelector.ClusterRef.Name), err)
		}
		clusterTarget = &target
	}
	requiredPackages := requiredPackagesForServiceInstance(&class, &instance)
	if len(requiredPackages) > 0 {
		if clusterTarget == nil {
			resolvedTarget, err := resolveClusterTargetForProject(ctx, r.Client, &project, clusterName)
			if err != nil {
				return r.handleDependencyError(ctx, &instance, originalStatus, "ClusterTargetUnavailable", fmt.Sprintf("Resolved ClusterTarget %q is not available.", clusterName), err)
			}
			clusterTarget = resolvedTarget
		}
		if ready, message := packagesReady(clusterTarget, requiredPackages); !ready {
			instance.Status.Phase = "PendingDependencies"
			setStatusCondition(&instance.Status.Conditions, instance.Generation, "Materialized", metav1.ConditionFalse, "OperatorPackagePending", message)
			setStatusCondition(&instance.Status.Conditions, instance.Generation, "Synced", metav1.ConditionFalse, "OperatorPackagePending", "Service instance is waiting for required operator packages to become ready.")
			setStatusCondition(&instance.Status.Conditions, instance.Generation, "Ready", metav1.ConditionFalse, "OperatorPackagePending", message)
			setStatusCondition(&instance.Status.Conditions, instance.Generation, "Failed", metav1.ConditionFalse, "OperatorPackagePending", "Service instance is blocked on an operator dependency, not failed.")
			if err := r.persistStatusIfChanged(ctx, &instance, originalStatus); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
		}
	}

	var targetClient client.Client
	if class.Spec.Driver == kubeVirtServiceClassDriver {
		if clusterTarget == nil {
			instance.Status.Phase = "PendingDependencies"
			message := "KubeVirt provisioning requires placement on a reachable remote ClusterTarget."
			setStatusCondition(&instance.Status.Conditions, instance.Generation, "Materialized", metav1.ConditionFalse, "RuntimeDependencyMissing", message)
			setStatusCondition(&instance.Status.Conditions, instance.Generation, "Synced", metav1.ConditionFalse, "RuntimeDependencyMissing", "Service instance is waiting for KubeVirt runtime dependencies.")
			setStatusCondition(&instance.Status.Conditions, instance.Generation, "Ready", metav1.ConditionFalse, "RuntimeDependencyMissing", message)
			setStatusCondition(&instance.Status.Conditions, instance.Generation, "Failed", metav1.ConditionFalse, "RuntimeDependencyMissing", "Service instance is blocked on a runtime dependency, not failed.")
			if err := r.persistStatusIfChanged(ctx, &instance, originalStatus); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
		}

		var targetClientErr error
		targetClient, targetClientErr = r.getTargetClient(ctx, clusterTarget)
		if targetClientErr != nil {
			return ctrl.Result{}, targetClientErr
		}
		if targetClient == nil {
			instance.Status.Phase = "PendingDependencies"
			message := fmt.Sprintf("ClusterTarget %q does not have usable connection credentials for KubeVirt runtime validation.", clusterTarget.Name)
			setStatusCondition(&instance.Status.Conditions, instance.Generation, "Materialized", metav1.ConditionFalse, "RuntimeDependencyMissing", message)
			setStatusCondition(&instance.Status.Conditions, instance.Generation, "Synced", metav1.ConditionFalse, "RuntimeDependencyMissing", "Service instance is waiting for KubeVirt runtime dependencies.")
			setStatusCondition(&instance.Status.Conditions, instance.Generation, "Ready", metav1.ConditionFalse, "RuntimeDependencyMissing", message)
			setStatusCondition(&instance.Status.Conditions, instance.Generation, "Failed", metav1.ConditionFalse, "RuntimeDependencyMissing", "Service instance is blocked on a runtime dependency, not failed.")
			if err := r.persistStatusIfChanged(ctx, &instance, originalStatus); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
		}
		if ready, message, dependenciesErr := kubeVirtDependenciesReady(ctx, targetClient); dependenciesErr != nil {
			return ctrl.Result{}, dependenciesErr
		} else if !ready {
			instance.Status.Phase = "PendingDependencies"
			setStatusCondition(&instance.Status.Conditions, instance.Generation, "Materialized", metav1.ConditionFalse, "RuntimeDependencyMissing", message)
			setStatusCondition(&instance.Status.Conditions, instance.Generation, "Synced", metav1.ConditionFalse, "RuntimeDependencyMissing", "Service instance is waiting for KubeVirt runtime dependencies.")
			setStatusCondition(&instance.Status.Conditions, instance.Generation, "Ready", metav1.ConditionFalse, "RuntimeDependencyMissing", message)
			setStatusCondition(&instance.Status.Conditions, instance.Generation, "Failed", metav1.ConditionFalse, "RuntimeDependencyMissing", "Service instance is blocked on a runtime dependency, not failed.")
			if err := r.persistStatusIfChanged(ctx, &instance, originalStatus); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
		}
	}

	adapter, ok := r.Adapters.Get(adapters.ServiceClass(instance.Spec.ServiceClassRef.Name))
	if !ok {
		instance.Status.Phase = "Failed"
		setStatusCondition(&instance.Status.Conditions, instance.Generation, "Accepted", metav1.ConditionFalse, "UnsupportedServiceClass", fmt.Sprintf("No adapter is registered for service class %q.", instance.Spec.ServiceClassRef.Name))
		setStatusCondition(&instance.Status.Conditions, instance.Generation, "Materialized", metav1.ConditionFalse, "UnsupportedServiceClass", "Service instance cannot be materialized because its service class is unsupported.")
		setStatusCondition(&instance.Status.Conditions, instance.Generation, "Synced", metav1.ConditionFalse, "UnsupportedServiceClass", "Service instance cannot become sync-ready because its service class is unsupported.")
		setStatusCondition(&instance.Status.Conditions, instance.Generation, "Ready", metav1.ConditionFalse, "UnsupportedServiceClass", "Service instance cannot reconcile runtime health because its service class is unsupported.")
		setStatusCondition(&instance.Status.Conditions, instance.Generation, "Failed", metav1.ConditionTrue, "UnsupportedServiceClass", "Service instance cannot be reconciled because its service class is unsupported.")
		if err := r.persistStatusIfChanged(ctx, &instance, originalStatus); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	serviceContext := adapters.ServiceContext{
		Tenant:        &tenant,
		Project:       &project,
		ClusterTarget: clusterTarget,
		Class:         &class,
		Plan:          &plan,
		Instance:      &instance,
	}

	validationResult, err := adapter.Validate(ctx, adapters.ValidationRequest{Context: serviceContext})
	if err != nil {
		instance.Status.Phase = "Failed"
		setStatusCondition(&instance.Status.Conditions, instance.Generation, "Accepted", metav1.ConditionFalse, "ValidationError", err.Error())
		setStatusCondition(&instance.Status.Conditions, instance.Generation, "Materialized", metav1.ConditionFalse, "ValidationError", "Service instance validation failed before materialization.")
		setStatusCondition(&instance.Status.Conditions, instance.Generation, "Synced", metav1.ConditionFalse, "ValidationError", "Service instance validation failed before sync readiness.")
		setStatusCondition(&instance.Status.Conditions, instance.Generation, "Ready", metav1.ConditionFalse, "ValidationError", "Service instance validation failed before runtime reconciliation.")
		setStatusCondition(&instance.Status.Conditions, instance.Generation, "Failed", metav1.ConditionTrue, "ValidationError", err.Error())
		if updateErr := r.persistStatusIfChanged(ctx, &instance, originalStatus); updateErr != nil {
			return ctrl.Result{}, updateErr
		}
		return ctrl.Result{}, nil
	}
	if !validationResult.Valid {
		instance.Status.Phase = "Failed"
		setStatusCondition(&instance.Status.Conditions, instance.Generation, "Accepted", metav1.ConditionFalse, "ValidationFailed", summarizeValidationIssues(validationResult.Issues))
		setStatusCondition(&instance.Status.Conditions, instance.Generation, "Materialized", metav1.ConditionFalse, "ValidationFailed", "Service instance validation failed before materialization.")
		setStatusCondition(&instance.Status.Conditions, instance.Generation, "Synced", metav1.ConditionFalse, "ValidationFailed", "Service instance validation failed before sync readiness.")
		setStatusCondition(&instance.Status.Conditions, instance.Generation, "Ready", metav1.ConditionFalse, "ValidationFailed", "Service instance validation failed before runtime reconciliation.")
		setStatusCondition(&instance.Status.Conditions, instance.Generation, "Failed", metav1.ConditionTrue, "ValidationFailed", "Service instance validation failed.")
		if err := r.persistStatusIfChanged(ctx, &instance, originalStatus); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	renderResult, err := adapter.Render(ctx, adapters.RenderRequest{Context: serviceContext})
	if err != nil {
		instance.Status.Phase = "Failed"
		setStatusCondition(&instance.Status.Conditions, instance.Generation, "Materialized", metav1.ConditionFalse, "RenderFailed", err.Error())
		setStatusCondition(&instance.Status.Conditions, instance.Generation, "Failed", metav1.ConditionTrue, "RenderFailed", err.Error())
		if err := r.persistStatusIfChanged(ctx, &instance, originalStatus); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}
	if r.productionDeliveryRequired(renderResult) && !r.deliveryPublishingConfigured() {
		markDeliveryRepoRequired(&instance, "DeliveryRepoRequired", "Production mode requires delivery repository publishing with auto-commit and auto-push before service instances can be provisioned.")
		if updateErr := r.persistStatusIfChanged(ctx, &instance, originalStatus); updateErr != nil {
			return ctrl.Result{}, updateErr
		}
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}
	if targetClient == nil {
		targetClient, err = r.getTargetClient(ctx, clusterTarget)
		if err != nil {
			return ctrl.Result{}, err
		}
	}
	if err := r.ensureCredentialSecrets(ctx, &instance, renderResult, targetClient); err != nil {
		if apierrors.IsNotFound(err) {
			markCredentialProjectionPending(&instance)
			if updateErr := r.persistStatusIfChanged(ctx, &instance, originalStatus); updateErr != nil {
				return ctrl.Result{}, updateErr
			}
			return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
		}
		instance.Status.Phase = "Failed"
		setStatusCondition(&instance.Status.Conditions, instance.Generation, "Ready", metav1.ConditionFalse, "CredentialProjectionFailed", err.Error())
		setStatusCondition(&instance.Status.Conditions, instance.Generation, "Failed", metav1.ConditionTrue, "CredentialProjectionFailed", "Service instance credential projection failed.")
		if updateErr := r.persistStatusIfChanged(ctx, &instance, originalStatus); updateErr != nil {
			return ctrl.Result{}, updateErr
		}
		return ctrl.Result{}, nil
	}

	statusCredentialRefs := append([]platformv1alpha1.NamespacedObjectReference(nil), renderResult.CredentialRefs...)
	if instance.Spec.SecretPolicy.DeliveryMode == platformv1alpha1.SecretDeliveryModeExternalSecret {
		projectedRefs := projectedCredentialRefs(&instance, renderResult.CredentialRefs)
		sourceSecretKeys, projectionErr := r.credentialSecretKeys(ctx, targetClient, renderResult.CredentialRefs)
		if projectionErr == nil {
			externalSecretArtifacts, projectionErr := renderExternalSecretArtifacts(&instance, renderResult.PackagePath, renderResult.CredentialRefs, projectedRefs, sourceSecretKeys)
			if projectionErr == nil {
				renderResult.Artifacts = append(renderResult.Artifacts, externalSecretArtifacts...)
				statusCredentialRefs = projectedRefs
			}
		}
		if projectionErr != nil {
			instance.Status.Phase = "Failed"
			setStatusCondition(&instance.Status.Conditions, instance.Generation, "Materialized", metav1.ConditionFalse, "ExternalSecretRenderFailed", projectionErr.Error())
			setStatusCondition(&instance.Status.Conditions, instance.Generation, "Failed", metav1.ConditionTrue, "ExternalSecretRenderFailed", "Credential projection artifacts could not be rendered.")
			if updateErr := r.persistStatusIfChanged(ctx, &instance, originalStatus); updateErr != nil {
				return ctrl.Result{}, updateErr
			}
			return ctrl.Result{}, nil
		}
	}

	m := r.Materializer
	if m == nil {
		m = materializer.New("")
	}
	materializeResult, err := m.Materialize(ctx, materializer.Request{
		PackagePath:  renderResult.PackagePath,
		PackagePaths: renderResult.PackagePaths,
		Artifacts:    renderResult.Artifacts,
	})
	if err != nil {
		instance.Status.Phase = "Failed"
		setStatusCondition(&instance.Status.Conditions, instance.Generation, "Materialized", metav1.ConditionFalse, "MaterializeFailed", err.Error())
		setStatusCondition(&instance.Status.Conditions, instance.Generation, "Synced", metav1.ConditionFalse, "MaterializeFailed", "Delivery artifacts are not ready for Argo CD reconciliation.")
		setStatusCondition(&instance.Status.Conditions, instance.Generation, "Ready", metav1.ConditionFalse, "MaterializeFailed", "Service instance runtime cannot be reconciled until delivery artifacts are materialized.")
		setStatusCondition(&instance.Status.Conditions, instance.Generation, "Failed", metav1.ConditionTrue, "MaterializeFailed", "Delivery artifacts could not be materialized.")
		if err := r.persistStatusIfChanged(ctx, &instance, originalStatus); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}
	publishedPath := materializeResult.Path
	publishedCommit := ""
	publishedRemote := ""
	publishedBranch := ""
	publishedPushed := false
	if r.Publisher != nil && r.Publisher.Enabled() {
		observeDeliveryPublish("attempted")
		publishResult, publishErr := r.Publisher.Publish(ctx, deliveryrepo.Request{
			PackagePath:  renderResult.PackagePath,
			PackagePaths: renderResult.PackagePaths,
			Artifacts:    renderResult.Artifacts,
			Revision:     materializeResult.Revision,
			Message:      fmt.Sprintf("servicer: publish %s/%s", project.Name, instance.Name),
		})
		if publishErr != nil {
			observeDeliveryPublish("failed")
			instance.Status.Phase = "Failed"
			setStatusCondition(&instance.Status.Conditions, instance.Generation, "Materialized", metav1.ConditionFalse, "PublishFailed", publishErr.Error())
			setStatusCondition(&instance.Status.Conditions, instance.Generation, "Synced", metav1.ConditionFalse, "PublishFailed", "Delivery artifacts could not be published to the configured Git worktree.")
			setStatusCondition(&instance.Status.Conditions, instance.Generation, "Failed", metav1.ConditionTrue, "PublishFailed", "Delivery artifacts could not be published.")
			if updateErr := r.persistStatusIfChanged(ctx, &instance, originalStatus); updateErr != nil {
				return ctrl.Result{}, updateErr
			}
			return ctrl.Result{}, nil
		}
		observeDeliveryPublish("succeeded")
		if publishResult.PublishedPath != "" {
			publishedPath = publishResult.PublishedPath
		}
		publishedCommit = publishResult.Commit
		publishedRemote = publishResult.Remote
		publishedBranch = publishResult.Branch
		publishedPushed = publishResult.Pushed
	}
	if r.productionDeliveryRequired(renderResult) && !publishedPushed {
		markDeliveryRepoRequired(&instance, "DeliveryPushRequired", "Production mode requires delivery artifacts to be pushed to the configured Git repository before service instances can be provisioned.")
		if updateErr := r.persistStatusIfChanged(ctx, &instance, originalStatus); updateErr != nil {
			return ctrl.Result{}, updateErr
		}
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	instance.Status.Phase = "Materialized"
	instance.Status.Runtime.Driver = renderResult.RuntimeDriver
	instance.Status.Runtime.ObjectRef = renderResult.PrimaryResource
	instance.Status.Endpoints = endpointStatus(renderResult.Endpoints)
	instance.Status.CredentialRefs = statusCredentialRefs
	instance.Status.Artifact = materializedArtifactStatus(materializeResult)
	instance.Status.Sync = platformv1alpha1.DeliverySyncStatus{
		Phase:           string(adapters.SyncPhasePending),
		ApplicationName: argoApplicationName(&project, &instance),
		Message:         "Delivery artifacts are materialized and ready for Argo CD reconciliation.",
	}
	if publishedCommit != "" {
		instance.Status.Sync.Message = fmt.Sprintf("%s Published commit %s.", instance.Status.Sync.Message, shortCommit(publishedCommit))
	}
	if publishedPushed {
		instance.Status.Sync.Message = fmt.Sprintf("%s Pushed to %s/%s.", instance.Status.Sync.Message, publishedRemote, publishedBranch)
	}
	instance.Status.Health.Summary = fmt.Sprintf("Materialized %d artifact(s) for %s.", len(renderResult.Artifacts), renderResult.RuntimeDriver)
	setStatusCondition(&instance.Status.Conditions, instance.Generation, "Placed", metav1.ConditionTrue, "PlacementResolved", "Service instance placement resolved.")
	setStatusCondition(&instance.Status.Conditions, instance.Generation, "Materialized", metav1.ConditionTrue, "ArtifactsMaterialized", "Delivery artifacts materialized successfully.")
	setStatusCondition(&instance.Status.Conditions, instance.Generation, "Synced", metav1.ConditionFalse, "AwaitingArgoCD", "Delivery artifacts are ready for Argo CD reconciliation.")
	setStatusCondition(&instance.Status.Conditions, instance.Generation, "Ready", metav1.ConditionFalse, "AwaitingRuntime", "Service instance runtime health has not yet been observed.")
	setStatusCondition(&instance.Status.Conditions, instance.Generation, "Failed", metav1.ConditionFalse, "ReconciliationProgressing", "Service instance has materialized successfully.")

	if err := r.ensureArgoApplication(ctx, &project, &instance, publishedPath, renderResult.PackagePaths); err != nil {
		instance.Status.Phase = "Failed"
		setStatusCondition(&instance.Status.Conditions, instance.Generation, "Synced", metav1.ConditionFalse, "ArgoApplicationFailed", err.Error())
		setStatusCondition(&instance.Status.Conditions, instance.Generation, "Failed", metav1.ConditionTrue, "ArgoApplicationFailed", "Argo CD application could not be reconciled.")
		if updateErr := r.persistStatusIfChanged(ctx, &instance, originalStatus); updateErr != nil {
			return ctrl.Result{}, updateErr
		}
		return ctrl.Result{}, nil
	}

	serviceContext.Instance = &instance
	runtimeObservation, observedResources, err := r.observeRuntime(ctx, renderResult.PrimaryResource, statusCredentialRefs, targetClient)
	if err != nil {
		return ctrl.Result{}, err
	}
	normalized, err := adapter.Observe(ctx, adapters.ObserveRequest{
		Context:           serviceContext,
		ObservedResources: observedResources,
		ArtifactRevision:  materializeResult.Revision,
		ApplicationName:   instance.Status.Sync.ApplicationName,
		Runtime:           runtimeObservation,
	})
	if err != nil {
		return ctrl.Result{}, err
	}
	applyObservedStatus(&instance, normalized)
	r.applyArgoObservedStatus(ctx, &instance)

	if r.Recorder != nil && originalStatus.Phase != instance.Status.Phase {
		eventType := corev1.EventTypeNormal
		if instance.Status.Phase == "Failed" || instance.Status.Phase == "Blocked" || instance.Status.Phase == "Degraded" {
			eventType = corev1.EventTypeWarning
		}
		r.Recorder.Event(&instance, eventType, instance.Status.Phase, instance.Status.Health.Summary)
	}

	if err := r.persistStatusIfChanged(ctx, &instance, originalStatus); err != nil {
		return ctrl.Result{}, err
	}

	if instance.Status.Phase != "Ready" {
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}

	return ctrl.Result{}, nil
}

func (r *ServiceInstanceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&platformv1alpha1.ServiceInstance{}).
		Complete(r)
}

func (r *ServiceInstanceReconciler) persistStatusIfChanged(ctx context.Context, desired *platformv1alpha1.ServiceInstance, original platformv1alpha1.ServiceInstanceStatus) error {
	if desired == nil || equality.Semantic.DeepEqual(original, desired.Status) {
		return nil
	}
	key := types.NamespacedName{Name: desired.Name, Namespace: desired.Namespace}
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		var current platformv1alpha1.ServiceInstance
		if err := r.Get(ctx, key, &current); err != nil {
			return err
		}
		base := current.DeepCopy()
		current.Status = desired.Status
		if err := r.Status().Patch(ctx, &current, client.MergeFrom(base)); err != nil {
			if apierrors.IsConflict(err) {
				return err
			}
			return err
		}
		return nil
	})
}

func (r *ServiceInstanceReconciler) handleDependencyError(ctx context.Context, instance *platformv1alpha1.ServiceInstance, originalStatus platformv1alpha1.ServiceInstanceStatus, reason, message string, err error) (ctrl.Result, error) {
	if err != nil && !apierrors.IsNotFound(err) {
		return ctrl.Result{}, err
	}

	instance.Status.Phase = "Failed"
	setStatusCondition(&instance.Status.Conditions, instance.Generation, "Accepted", metav1.ConditionFalse, reason, message)
	setStatusCondition(&instance.Status.Conditions, instance.Generation, "Failed", metav1.ConditionTrue, reason, message)
	if updateErr := r.persistStatusIfChanged(ctx, instance, originalStatus); updateErr != nil {
		return ctrl.Result{}, updateErr
	}
	return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
}

func (r *ServiceInstanceReconciler) handleDependencyPending(ctx context.Context, instance *platformv1alpha1.ServiceInstance, originalStatus platformv1alpha1.ServiceInstanceStatus, reason, message string) (ctrl.Result, error) {
	instance.Status.Phase = "PendingDependencies"
	setStatusCondition(&instance.Status.Conditions, instance.Generation, "Accepted", metav1.ConditionFalse, reason, message)
	setStatusCondition(&instance.Status.Conditions, instance.Generation, "Ready", metav1.ConditionFalse, reason, message)
	setStatusCondition(&instance.Status.Conditions, instance.Generation, "Failed", metav1.ConditionFalse, reason, message)
	if updateErr := r.persistStatusIfChanged(ctx, instance, originalStatus); updateErr != nil {
		return ctrl.Result{}, updateErr
	}
	return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
}

func (r *ServiceInstanceReconciler) handleDeletion(ctx context.Context, instance *platformv1alpha1.ServiceInstance) (ctrl.Result, error) {
	m := r.Materializer
	if m == nil {
		m = materializer.New("")
	}

	runtimeClient := r.Client
	if instance.Spec.ProjectRef.Name != "" {
		var project platformv1alpha1.Project
		if err := r.Get(ctx, types.NamespacedName{Name: instance.Spec.ProjectRef.Name}, &project); err == nil {
			clusterName := resolvedClusterName(&project)
			if clusterName != "" {
				clusterTarget, err := resolveClusterTargetForProject(ctx, r.Client, &project, clusterName)
				if err == nil {
					targetClient, targetErr := r.getTargetClient(ctx, clusterTarget)
					if targetErr != nil {
						return ctrl.Result{}, targetErr
					}
					if targetClient != nil {
						runtimeClient = targetClient
					}
				}
			}
		} else if !apierrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
	}

	// Remove materialized delivery artifacts so ArgoCD stops syncing this instance.
	if instance.Status.Artifact.Path != "" {
		if err := m.Purge(instance.Status.Artifact.Path); err != nil {
			return ctrl.Result{}, fmt.Errorf("purge artifacts for %q: %w", instance.Name, err)
		}
	}

	// Delete the namespace directly for immediate cleanup.
	if ns := instance.Status.Placement.Namespace; ns != "" {
		var namespace corev1.Namespace
		if err := runtimeClient.Get(ctx, types.NamespacedName{Name: ns}, &namespace); err == nil {
			if err := runtimeClient.Delete(ctx, &namespace); err != nil && !apierrors.IsNotFound(err) {
				return ctrl.Result{}, fmt.Errorf("delete namespace %q: %w", ns, err)
			}
		} else if !apierrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
	}

	// Delete the primary runtime resource (e.g. YBUniverse, CNPG Cluster) directly.
	// This ensures cleanup even when ArgoCD is not present (e.g. dev/demo environments).
	if ref := instance.Status.Runtime.ObjectRef; ref != nil {
		gv, err := schema.ParseGroupVersion(ref.APIVersion)
		if err == nil {
			obj := &unstructured.Unstructured{}
			obj.SetGroupVersionKind(schema.GroupVersionKind{Group: gv.Group, Version: gv.Version, Kind: ref.Kind})
			obj.SetName(ref.Name)
			obj.SetNamespace(ref.Namespace)
			if err := runtimeClient.Delete(ctx, obj); err != nil && !apierrors.IsNotFound(err) && !apimeta.IsNoMatchError(err) {
				return ctrl.Result{}, fmt.Errorf("delete primary resource %s/%s %q: %w", ref.APIVersion, ref.Kind, ref.Name, err)
			}
		}
	}

	// Remove finalizer to allow Kubernetes to complete deletion.
	instance.Finalizers = removeFromSlice(instance.Finalizers, instanceFinalizer)
	if err := r.Update(ctx, instance); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func (r *ServiceInstanceReconciler) handleValidationFailure(ctx context.Context, instance *platformv1alpha1.ServiceInstance, originalStatus platformv1alpha1.ServiceInstanceStatus, reason, message string) (ctrl.Result, error) {
	instance.Status.Phase = "Failed"
	setStatusCondition(&instance.Status.Conditions, instance.Generation, "Accepted", metav1.ConditionFalse, reason, message)
	setStatusCondition(&instance.Status.Conditions, instance.Generation, "Ready", metav1.ConditionFalse, reason, message)
	setStatusCondition(&instance.Status.Conditions, instance.Generation, "Materialized", metav1.ConditionFalse, reason, "Service instance validation failed before materialization.")
	setStatusCondition(&instance.Status.Conditions, instance.Generation, "Synced", metav1.ConditionFalse, reason, "Service instance validation failed before sync readiness.")
	setStatusCondition(&instance.Status.Conditions, instance.Generation, "Failed", metav1.ConditionTrue, reason, message)
	if updateErr := r.persistStatusIfChanged(ctx, instance, originalStatus); updateErr != nil {
		return ctrl.Result{}, updateErr
	}
	return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
}

func kubeVirtDependenciesReady(ctx context.Context, c client.Client) (bool, string, error) {
	if c == nil {
		return false, "KubeVirt runtime validation requires a remote cluster client.", nil
	}
	for _, requiredCRD := range []string{kubeVirtVirtualMachineCRDName, kubeVirtDataVolumeCRDName} {
		crd := &unstructured.Unstructured{}
		crd.SetGroupVersionKind(schema.GroupVersionKind{Group: "apiextensions.k8s.io", Version: "v1", Kind: "CustomResourceDefinition"})
		if err := c.Get(ctx, types.NamespacedName{Name: requiredCRD}, crd); err != nil {
			if apierrors.IsNotFound(err) || apimeta.IsNoMatchError(err) {
				return false, fmt.Sprintf("KubeVirt dependency missing on target cluster: CRD %q is not installed.", requiredCRD), nil
			}
			return false, "", err
		}
	}
	return true, "", nil
}

func resolvedClusterName(project *platformv1alpha1.Project) string {
	if project.Status.Placement.ClusterName != "" {
		return project.Status.Placement.ClusterName
	}
	if project.Spec.TargetSelector.ClusterRef != nil {
		return project.Spec.TargetSelector.ClusterRef.Name
	}
	return ""
}

func resolvedNamespace(project *platformv1alpha1.Project, instance *platformv1alpha1.ServiceInstance) string {
	tenant := project.Spec.TenantRef.Name
	if tenant == "" {
		tenant = "tenant"
	}
	return fmt.Sprintf("%s-%s-%s", tenant, project.Name, instance.Name)
}

func summarizeValidationIssues(issues []adapters.ValidationIssue) string {
	parts := make([]string, 0, len(issues))
	for _, issue := range issues {
		if issue.Path == "" {
			parts = append(parts, issue.Message)
			continue
		}
		parts = append(parts, fmt.Sprintf("%s: %s", issue.Path, issue.Message))
	}
	return strings.Join(parts, "; ")
}

func (r *ServiceInstanceReconciler) enforceProjectQuota(ctx context.Context, instance *platformv1alpha1.ServiceInstance, project *platformv1alpha1.Project, tenant *platformv1alpha1.Tenant, originalStatus platformv1alpha1.ServiceInstanceStatus) (ctrl.Result, bool, error) {
	if project == nil {
		return ctrl.Result{}, false, nil
	}
	quota := project.Status.EffectiveQuota
	if quota.MaxServices == nil && quota.MaxNamespaces == nil {
		quota = effectiveProjectQuota(project, tenant)
	}
	maxServices := quota.MaxServices
	maxNamespaces := quota.MaxNamespaces
	if maxServices == nil && maxNamespaces == nil {
		return ctrl.Result{}, false, nil
	}

	var instances platformv1alpha1.ServiceInstanceList
	if err := r.List(ctx, &instances); err != nil {
		return ctrl.Result{}, false, err
	}

	serviceCount := int32(0)
	namespaces := map[string]struct{}{}
	for _, item := range instances.Items {
		if item.DeletionTimestamp != nil || item.Spec.ProjectRef.Name != project.Name {
			continue
		}
		serviceCount++
		namespace := item.Status.Placement.Namespace
		if namespace == "" {
			namespace = resolvedNamespace(project, &item)
		}
		if namespace != "" {
			namespaces[namespace] = struct{}{}
		}
	}

	if maxServices != nil && serviceCount > *maxServices {
		result, err := r.handleValidationFailure(ctx, instance, originalStatus, "ProjectServiceQuotaExceeded", fmt.Sprintf("Project %q allows at most %d service instances; observed %d.", project.Name, *maxServices, serviceCount))
		return result, true, err
	}
	if maxNamespaces != nil && int64(len(namespaces)) > int64(*maxNamespaces) {
		result, err := r.handleValidationFailure(ctx, instance, originalStatus, "ProjectNamespaceQuotaExceeded", fmt.Sprintf("Project %q allows at most %d namespaces; observed %d.", project.Name, *maxNamespaces, len(namespaces)))
		return result, true, err
	}
	return ctrl.Result{}, false, nil
}

func materializedArtifactStatus(result materializer.Result) platformv1alpha1.ArtifactStatus {
	artifacts := make([]platformv1alpha1.MaterializedArtifactStatus, 0, len(result.Artifacts))
	for _, artifact := range result.Artifacts {
		artifacts = append(artifacts, platformv1alpha1.MaterializedArtifactStatus{
			Path:   artifact.Path,
			Digest: artifact.Digest,
		})
	}
	return platformv1alpha1.ArtifactStatus{
		Revision:  result.Revision,
		Path:      result.Path,
		Count:     safeInt32FromLen(len(artifacts)),
		Artifacts: artifacts,
	}
}

func endpointStatus(endpoints []adapters.Endpoint) map[string]string {
	if len(endpoints) == 0 {
		return nil
	}
	status := make(map[string]string, len(endpoints))
	for _, endpoint := range endpoints {
		status[endpoint.Name] = endpoint.Address
	}
	return status
}

func markCredentialProjectionPending(instance *platformv1alpha1.ServiceInstance) {
	if instance == nil {
		return
	}
	instance.Status.Phase = "Provisioning"
	setStatusCondition(&instance.Status.Conditions, instance.Generation, "Ready", metav1.ConditionFalse, "CredentialProjectionPending", "Credential projection is waiting for the delivered runtime namespace.")
}

func (r *ServiceInstanceReconciler) productionDeliveryRequired(renderResult adapters.RenderResult) bool {
	return r.ProductionMode && len(renderResult.Artifacts) > 0
}

func (r *ServiceInstanceReconciler) deliveryPublishingConfigured() bool {
	return strings.TrimSpace(r.DeliveryRepoURL) != "" &&
		r.Publisher != nil &&
		r.Publisher.Enabled() &&
		r.Publisher.AutoCommit &&
		r.Publisher.AutoPush
}

func markDeliveryRepoRequired(instance *platformv1alpha1.ServiceInstance, reason, message string) {
	if instance == nil {
		return
	}
	instance.Status.Phase = "Degraded"
	instance.Status.Sync.Phase = string(adapters.SyncPhasePending)
	instance.Status.Sync.Message = message
	instance.Status.Health.Summary = message
	setStatusCondition(&instance.Status.Conditions, instance.Generation, "Materialized", metav1.ConditionFalse, reason, "Delivery artifacts were not materialized because production GitOps publishing is not ready.")
	setStatusCondition(&instance.Status.Conditions, instance.Generation, "Synced", metav1.ConditionFalse, reason, message)
	setStatusCondition(&instance.Status.Conditions, instance.Generation, "Ready", metav1.ConditionFalse, reason, message)
	setStatusCondition(&instance.Status.Conditions, instance.Generation, "Degraded", metav1.ConditionTrue, reason, message)
	setStatusCondition(&instance.Status.Conditions, instance.Generation, "Failed", metav1.ConditionFalse, reason, "Service instance is blocked by delivery configuration, not failed.")
}

func (r *ServiceInstanceReconciler) ensureCredentialSecrets(ctx context.Context, instance *platformv1alpha1.ServiceInstance, renderResult adapters.RenderResult, targetClient client.Client) error {
	if renderResult.RuntimeDriver != "servicer-valkey" && renderResult.RuntimeDriver != "servicer-nats" && renderResult.RuntimeDriver != "yb-operator" && renderResult.RuntimeDriver != "servicer-mysql" && renderResult.RuntimeDriver != "cnpg" {
		return nil
	}
	if instance.Spec.SecretPolicy.DeliveryMode == platformv1alpha1.SecretDeliveryModeManual {
		return nil
	}
	secretClient := r.Client
	if targetClient != nil {
		secretClient = targetClient
	}
	if renderResult.RuntimeDriver == "servicer-nats" {
		namespace := instance.Status.Placement.Namespace
		if len(renderResult.CredentialRefs) > 0 && renderResult.CredentialRefs[0].Namespace != "" {
			namespace = renderResult.CredentialRefs[0].Namespace
		}
		if err := ensureNamespaceExists(ctx, secretClient, namespace); err != nil {
			return err
		}
		return r.ensureNATSCredentialSecrets(ctx, secretClient, instance, namespace)
	}
	for _, ref := range renderResult.CredentialRefs {
		if err := ensureNamespaceExists(ctx, secretClient, ref.Namespace); err != nil {
			return err
		}
		var secret corev1.Secret
		err := secretClient.Get(ctx, types.NamespacedName{Name: ref.Name, Namespace: ref.Namespace}, &secret)
		if err == nil {
			if secret.Data == nil || len(secret.Data["password"]) == 0 || len(secret.Data["username"]) == 0 {
				if secret.Data == nil {
					secret.Data = map[string][]byte{}
				}
				password := string(secret.Data["password"])
				if password == "" {
					var genErr error
					password, genErr = randomPassword()
					if genErr != nil {
						return genErr
					}
				}
				for key, value := range managedCredentialData(renderResult.RuntimeDriver, instance, password) {
					secret.Data[key] = value
				}
				if secret.Labels == nil {
					secret.Labels = map[string]string{}
				}
				secret.Labels["servicer.io/managed-by"] = "servicer"
				secret.Labels["servicer.io/service-instance"] = instance.Name
				if updateErr := secretClient.Update(ctx, &secret); updateErr != nil {
					return updateErr
				}
			}
			continue
		}
		if !apierrors.IsNotFound(err) {
			return err
		}
		password, genErr := randomPassword()
		if genErr != nil {
			return genErr
		}
		secret = corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      ref.Name,
				Namespace: ref.Namespace,
				Labels: map[string]string{
					"servicer.io/managed-by":       "servicer",
					"servicer.io/service-instance": instance.Name,
				},
			},
			Type: corev1.SecretTypeOpaque,
			Data: managedCredentialData(renderResult.RuntimeDriver, instance, password),
		}
		if err := secretClient.Create(ctx, &secret); err != nil {
			return err
		}
	}
	return nil
}

func ensureNamespaceExists(ctx context.Context, c client.Client, namespace string) error {
	namespace = strings.TrimSpace(namespace)
	if namespace == "" {
		return nil
	}
	var existing corev1.Namespace
	if err := c.Get(ctx, types.NamespacedName{Name: namespace}, &existing); err == nil {
		return nil
	} else if !apierrors.IsNotFound(err) {
		return err
	}
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: namespace,
		},
	}
	if err := c.Create(ctx, ns); err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}
	return nil
}

func (r *ServiceInstanceReconciler) credentialSecretKeys(ctx context.Context, targetClient client.Client, refs []platformv1alpha1.NamespacedObjectReference) (map[string][]string, error) {
	secretClient := targetClient
	if secretClient == nil {
		secretClient = r.Client
	}
	keysBySecret := make(map[string][]string, len(refs))
	for _, ref := range refs {
		if ref.Name == "" || ref.Namespace == "" {
			continue
		}
		var secret corev1.Secret
		if err := secretClient.Get(ctx, types.NamespacedName{Name: ref.Name, Namespace: ref.Namespace}, &secret); err != nil {
			return nil, err
		}
		keys := make([]string, 0, len(secret.Data))
		for key := range secret.Data {
			keys = append(keys, key)
		}
		keysBySecret[ref.Namespace+"/"+ref.Name] = keys
	}
	return keysBySecret, nil
}

func managedCredentialData(runtimeDriver string, instance *platformv1alpha1.ServiceInstance, password string) map[string][]byte {
	data := map[string][]byte{
		"password": []byte(password),
	}
	switch runtimeDriver {
	case "servicer-nats":
		data["username"] = []byte("nats")
	case "servicer-valkey":
		data["username"] = []byte("default")
	case "cnpg":
		name := instanceDatabaseName(instance)
		data["username"] = []byte(name)
		data["database"] = []byte(name)
	case "yb-operator":
		data["username"] = []byte("yugabyte")
		data["database"] = []byte(instanceDatabaseName(instance))
	case "servicer-mysql":
		name := instanceDatabaseName(instance)
		data["username"] = []byte(name)
		data["database"] = []byte(name)
	}
	return data
}

func instanceDatabaseName(instance *platformv1alpha1.ServiceInstance) string {
	if instance == nil {
		return adapters.ResolveDatabaseName("", "")
	}
	type relationalParameters struct {
		DatabaseName string `json:"databaseName,omitempty"`
	}
	params := relationalParameters{}
	if instance.Spec.Parameters != nil {
		_ = json.Unmarshal(instance.Spec.Parameters.Raw, &params)
	}
	return adapters.ResolveDatabaseName(instance.Name, params.DatabaseName)
}

func (r *ServiceInstanceReconciler) observeRuntime(ctx context.Context, ref *platformv1alpha1.TypedObjectReference, credentialRefs []platformv1alpha1.NamespacedObjectReference, targetClient client.Client) (adapters.RuntimeObservation, []platformv1alpha1.TypedObjectReference, error) {
	observation := adapters.RuntimeObservation{}
	observedResources := make([]platformv1alpha1.TypedObjectReference, 0, 1)

	// observeClient returns targetClient when available, falling back to the app-cluster client.
	observeClient := func() client.Client {
		if targetClient != nil {
			return targetClient
		}
		return r.Client
	}

	if ref != nil && ref.APIVersion == "v1" && ref.Kind == "Namespace" && ref.Name != "" {
		var namespace corev1.Namespace
		err := observeClient().Get(ctx, types.NamespacedName{Name: ref.Name}, &namespace)
		if err != nil && !apierrors.IsNotFound(err) {
			return observation, nil, err
		}
		if err == nil {
			observedResources = append(observedResources, *ref)
		}
	}

	if ref != nil && ref.APIVersion == "apps/v1" && ref.Kind == "StatefulSet" && ref.Namespace != "" {
		var statefulSet appsv1.StatefulSet
		err := observeClient().Get(ctx, types.NamespacedName{Name: ref.Name, Namespace: ref.Namespace}, &statefulSet)
		if err != nil && !apierrors.IsNotFound(err) {
			return observation, nil, err
		}
		if err == nil {
			desired := statefulSet.Status.Replicas
			if statefulSet.Spec.Replicas != nil {
				desired = *statefulSet.Spec.Replicas
			}
			observation.Workload = &adapters.WorkloadObservation{
				DesiredReplicas: desired,
				ReadyReplicas:   statefulSet.Status.ReadyReplicas,
				UpdatedReplicas: statefulSet.Status.UpdatedReplicas,
				Observed:        true,
			}
			observedResources = append(observedResources, *ref)

			var pods corev1.PodList
			if err := observeClient().List(ctx, &pods, client.InNamespace(ref.Namespace), client.MatchingLabels{
				"servicer.io/service-instance": ref.Name,
			}); err != nil {
				return observation, nil, err
			}
			observation.TotalPods = safeInt32FromLen(len(pods.Items))
			for _, pod := range pods.Items {
				if isPodReady(&pod) {
					observation.ReadyPods++
				}
			}
		}
	}

	if ref != nil && ref.APIVersion == "postgresql.cnpg.io/v1" && ref.Kind == "Cluster" && ref.Namespace != "" {
		u := &unstructured.Unstructured{}
		u.SetGroupVersionKind(schema.GroupVersionKind{Group: "postgresql.cnpg.io", Version: "v1", Kind: "Cluster"})
		err := observeClient().Get(ctx, types.NamespacedName{Name: ref.Name, Namespace: ref.Namespace}, u)
		if err != nil {
			if apimeta.IsNoMatchError(err) {
				observation.Blocked = true
				observation.Message = "CloudNative PG operator (postgresql.cnpg.io/v1) is not installed in the target cluster."
				return observation, observedResources, nil
			}
			if !apierrors.IsNotFound(err) {
				return observation, nil, err
			}
		}
		if err == nil {
			observedResources = append(observedResources, *ref)
			readyInstances, _, _ := unstructured.NestedInt64(u.Object, "status", "readyInstances")
			instances, _, _ := unstructured.NestedInt64(u.Object, "status", "instances")
			observation.Workload = &adapters.WorkloadObservation{
				DesiredReplicas: safeInt32FromInt64(instances),
				ReadyReplicas:   safeInt32FromInt64(readyInstances),
				UpdatedReplicas: safeInt32FromInt64(readyInstances),
				Observed:        true,
			}
		}
	}

	if ref != nil && ref.APIVersion == "operator.yugabyte.io/v1alpha1" && ref.Kind == "YBUniverse" && ref.Namespace != "" {
		crd := &unstructured.Unstructured{}
		crd.SetGroupVersionKind(schema.GroupVersionKind{Group: "apiextensions.k8s.io", Version: "v1", Kind: "CustomResourceDefinition"})
		if err := observeClient().Get(ctx, types.NamespacedName{Name: "ybuniverses.operator.yugabyte.io"}, crd); err != nil {
			if apierrors.IsNotFound(err) || apimeta.IsNoMatchError(err) {
				observation.Blocked = true
				observation.Message = "YugabyteDB operator CRD ybuniverses.operator.yugabyte.io is not installed in the target cluster."
				return observation, observedResources, nil
			}
			return observation, nil, err
		}

		u := &unstructured.Unstructured{}
		u.SetGroupVersionKind(schema.GroupVersionKind{Group: "operator.yugabyte.io", Version: "v1alpha1", Kind: "YBUniverse"})
		err := observeClient().Get(ctx, types.NamespacedName{Name: ref.Name, Namespace: ref.Namespace}, u)
		if err != nil {
			if !apierrors.IsNotFound(err) {
				return observation, nil, err
			}
		}
		if err == nil {
			observedResources = append(observedResources, *ref)
			tservers, _, _ := unstructured.NestedInt64(u.Object, "spec", "numNodes")
			state, _, _ := unstructured.NestedString(u.Object, "status", "universeState")
			if state == "" {
				state, _, _ = unstructured.NestedString(u.Object, "status", "state")
			}
			if state == "" {
				state, _, _ = unstructured.NestedString(u.Object, "status", "phase")
			}
			// Only mark as observed when the YBA operator has started reconciling
			// (i.e. status.universeState/state/phase is set). An empty status means YBA has not yet
			// begun provisioning, so leave Observed=false to avoid the misleading
			// "Waiting for TServer readiness: 0/N ready" message.
			if state != "" {
				if strings.Contains(strings.ToLower(state), "error") {
					observation.Blocked = true
					observation.Message = fmt.Sprintf("YugabyteDB operator reported universe state %q.", state)
				}
				readyTServers := int64(0)
				if state == "Ready" || state == "Succeeded" || state == "ReadyToUse" {
					readyTServers = tservers
				}
				observation.Workload = &adapters.WorkloadObservation{
					DesiredReplicas: safeInt32FromInt64(tservers),
					ReadyReplicas:   safeInt32FromInt64(readyTServers),
					UpdatedReplicas: safeInt32FromInt64(readyTServers),
					Observed:        true,
				}
			}
		}
	}

	if ref != nil && ref.APIVersion == "kubevirt.io/v1" && ref.Kind == "VirtualMachine" && ref.Namespace != "" {
		if ready, message, err := kubeVirtDependenciesReady(ctx, observeClient()); err != nil {
			return observation, nil, err
		} else if !ready {
			observation.Blocked = true
			observation.Message = message
			return observation, observedResources, nil
		}

		u := &unstructured.Unstructured{}
		u.SetGroupVersionKind(schema.GroupVersionKind{Group: "kubevirt.io", Version: "v1", Kind: "VirtualMachine"})
		err := observeClient().Get(ctx, types.NamespacedName{Name: ref.Name, Namespace: ref.Namespace}, u)
		if err != nil {
			if !apierrors.IsNotFound(err) {
				return observation, nil, err
			}
		}
		if err == nil {
			observedResources = append(observedResources, *ref)
		}
	}

	for _, ref := range credentialRefs {
		var secret corev1.Secret
		err := observeClient().Get(ctx, types.NamespacedName{Name: ref.Name, Namespace: ref.Namespace}, &secret)
		if err != nil && !apierrors.IsNotFound(err) {
			return observation, nil, err
		}
		if err == nil && len(secret.Data["password"]) > 0 {
			observation.CredentialSecretPresent = true
			break
		}
	}

	return observation, observedResources, nil
}

func safeInt32FromInt64(value int64) int32 {
	if value > math.MaxInt32 {
		return math.MaxInt32
	}
	if value < math.MinInt32 {
		return math.MinInt32
	}
	return int32(value) // #nosec G115 -- Value is bounds-checked above.
}

func safeInt32FromLen(length int) int32 {
	if length > math.MaxInt32 {
		return math.MaxInt32
	}
	return int32(length) // #nosec G115 -- Value is bounds-checked above.
}

func applyObservedStatus(instance *platformv1alpha1.ServiceInstance, status adapters.NormalizedStatus) {
	if status.Phase != "" {
		instance.Status.Phase = status.Phase
	}
	if status.Summary != "" {
		instance.Status.Health.Summary = status.Summary
	}
	instance.Status.Endpoints = endpointStatus(status.Endpoints)
	if len(status.CredentialRefs) > 0 {
		instance.Status.CredentialRefs = append([]platformv1alpha1.NamespacedObjectReference(nil), status.CredentialRefs...)
	}
	if status.CacheTopology != nil {
		instance.Status.CacheTopology = *status.CacheTopology
	}
	if status.Sync.Phase != "" {
		instance.Status.Sync.Phase = string(status.Sync.Phase)
	}
	if status.Sync.Message != "" {
		instance.Status.Sync.Message = status.Sync.Message
	}
	switch status.Phase {
	case "Ready":
		instance.Status.Sync.Phase = string(adapters.SyncPhaseSynced)
		instance.Status.Sync.Message = "Runtime resources have been observed after materialization."
		setStatusCondition(&instance.Status.Conditions, instance.Generation, "Synced", metav1.ConditionTrue, "RuntimeObserved", "Runtime resources have been observed after materialization.")
		setStatusCondition(&instance.Status.Conditions, instance.Generation, "Ready", metav1.ConditionTrue, "RuntimeReady", status.Summary)
	case "Blocked":
		setStatusCondition(&instance.Status.Conditions, instance.Generation, "Synced", metav1.ConditionFalse, "RuntimeDependencyMissing", status.Summary)
		setStatusCondition(&instance.Status.Conditions, instance.Generation, "Ready", metav1.ConditionFalse, "RuntimeDependencyMissing", status.Summary)
	case "Provisioning":
		setStatusCondition(&instance.Status.Conditions, instance.Generation, "Ready", metav1.ConditionFalse, "RuntimePending", status.Summary)
	default:
		setStatusCondition(&instance.Status.Conditions, instance.Generation, "Ready", metav1.ConditionFalse, "AwaitingRuntime", status.Summary)
	}
}

func (r *ServiceInstanceReconciler) ensureArgoApplication(ctx context.Context, project *platformv1alpha1.Project, instance *platformv1alpha1.ServiceInstance, packagePath string, packagePaths []string) error {
	if instance == nil || project == nil {
		return nil
	}
	if strings.TrimSpace(r.DeliveryRepoURL) == "" {
		if r.ProductionMode {
			return fmt.Errorf("delivery repository URL is required in production mode")
		}
		instance.Status.Sync.Message = "Delivery artifacts are materialized, but Argo CD application creation is disabled until delivery repo settings are configured."
		return nil
	}

	var crd unstructured.Unstructured
	crd.SetGroupVersionKind(schema.GroupVersionKind{Group: "apiextensions.k8s.io", Version: "v1", Kind: "CustomResourceDefinition"})
	if err := r.Get(ctx, types.NamespacedName{Name: "applications.argoproj.io"}, &crd); err != nil {
		if apierrors.IsNotFound(err) {
			if r.ProductionMode {
				return fmt.Errorf("Argo CD Application CRD is required in production mode")
			}
			instance.Status.Sync.Message = "Delivery artifacts are materialized, but Argo CD is not installed in the management cluster."
			return nil
		}
		return err
	}

	namespace := firstNonEmptyTrimmed(r.ArgoCDNamespace, "argocd")
	appName := argoApplicationName(project, instance)
	if len(packagePaths) > 1 {
		var appSetCRD unstructured.Unstructured
		appSetCRD.SetGroupVersionKind(schema.GroupVersionKind{Group: "apiextensions.k8s.io", Version: "v1", Kind: "CustomResourceDefinition"})
		if err := r.Get(ctx, types.NamespacedName{Name: "applicationsets.argoproj.io"}, &appSetCRD); err == nil {
			return r.ensureArgoApplicationSet(ctx, namespace, appName, project, instance, packagePaths)
		} else if !apierrors.IsNotFound(err) {
			return err
		}
	}
	sourcePath := deliverySourcePath(r.DeliveryRepoPath, packagePath)

	desired := &unstructured.Unstructured{}
	desired.SetGroupVersionKind(schema.GroupVersionKind{Group: "argoproj.io", Version: "v1alpha1", Kind: "Application"})
	desired.SetName(appName)
	desired.SetNamespace(namespace)
	desired.SetLabels(map[string]string{
		"servicer.io/managed-by":       "servicer",
		"servicer.io/service-instance": instance.Name,
		"servicer.io/project":          project.Name,
	})
	desired.Object["spec"] = map[string]any{
		"project": firstNonEmptyTrimmed(r.ArgoCDProject, "default"),
		"source": map[string]any{
			"repoURL":        r.DeliveryRepoURL,
			"targetRevision": firstNonEmptyTrimmed(r.DeliveryRepoRef, "HEAD"),
			"path":           sourcePath,
			"directory": map[string]any{
				"recurse": true,
			},
		},
		"destination": map[string]any{
			"name":      instance.Status.Placement.ClusterName,
			"namespace": instance.Status.Placement.Namespace,
		},
		"syncPolicy": map[string]any{
			"automated": map[string]any{
				"prune":    true,
				"selfHeal": true,
			},
			"syncOptions": []any{
				"CreateNamespace=true",
			},
		},
	}

	var existing unstructured.Unstructured
	existing.SetGroupVersionKind(desired.GroupVersionKind())
	err := r.Get(ctx, types.NamespacedName{Name: appName, Namespace: namespace}, &existing)
	if apierrors.IsNotFound(err) {
		if err := r.deleteArgoResourceIfExists(ctx, namespace, "ApplicationSet", appName); err != nil {
			return err
		}
		return r.Create(ctx, desired)
	}
	if err != nil {
		return err
	}

	existing.Object["spec"] = desired.Object["spec"]
	existing.SetLabels(mergeStringMaps(existing.GetLabels(), desired.GetLabels()))
	return r.Update(ctx, &existing)
}

func (r *ServiceInstanceReconciler) ensureArgoApplicationSet(ctx context.Context, namespace, appName string, project *platformv1alpha1.Project, instance *platformv1alpha1.ServiceInstance, packagePaths []string) error {
	elements := applicationSetElements(packagePaths)
	if len(elements) == 0 {
		return nil
	}
	desired := &unstructured.Unstructured{}
	desired.SetGroupVersionKind(schema.GroupVersionKind{Group: "argoproj.io", Version: "v1alpha1", Kind: "ApplicationSet"})
	desired.SetName(appName)
	desired.SetNamespace(namespace)
	desired.SetLabels(map[string]string{
		"servicer.io/managed-by":       "servicer",
		"servicer.io/service-instance": instance.Name,
		"servicer.io/project":          project.Name,
	})
	desired.Object["spec"] = map[string]any{
		"generators": []any{
			map[string]any{
				"list": map[string]any{
					"elements": elements,
				},
			},
		},
		"template": map[string]any{
			"metadata": map[string]any{
				"name": "{{cluster}}-" + appName,
				"labels": map[string]any{
					"servicer.io/managed-by":       "servicer",
					"servicer.io/service-instance": instance.Name,
					"servicer.io/project":          project.Name,
					"servicer.io/application-set":  appName,
				},
			},
			"spec": map[string]any{
				"project": firstNonEmptyTrimmed(r.ArgoCDProject, "default"),
				"source": map[string]any{
					"repoURL":        r.DeliveryRepoURL,
					"targetRevision": firstNonEmptyTrimmed(r.DeliveryRepoRef, "HEAD"),
					"path":           "{{path}}",
					"directory": map[string]any{
						"recurse": true,
					},
				},
				"destination": map[string]any{
					"name":      "{{cluster}}",
					"namespace": instance.Status.Placement.Namespace,
				},
				"syncPolicy": map[string]any{
					"automated": map[string]any{
						"prune":    true,
						"selfHeal": true,
					},
					"syncOptions": []any{
						"CreateNamespace=true",
					},
				},
			},
		},
	}

	var existing unstructured.Unstructured
	existing.SetGroupVersionKind(desired.GroupVersionKind())
	err := r.Get(ctx, types.NamespacedName{Name: appName, Namespace: namespace}, &existing)
	if apierrors.IsNotFound(err) {
		if err := r.deleteArgoResourceIfExists(ctx, namespace, "Application", appName); err != nil {
			return err
		}
		return r.Create(ctx, desired)
	}
	if err != nil {
		return err
	}

	existing.Object["spec"] = desired.Object["spec"]
	existing.SetLabels(mergeStringMaps(existing.GetLabels(), desired.GetLabels()))
	if err := r.Update(ctx, &existing); err != nil {
		return err
	}
	return r.deleteArgoResourceIfExists(ctx, namespace, "Application", appName)
}

func (r *ServiceInstanceReconciler) deleteArgoResourceIfExists(ctx context.Context, namespace, kind, name string) error {
	resource := &unstructured.Unstructured{}
	resource.SetGroupVersionKind(schema.GroupVersionKind{Group: "argoproj.io", Version: "v1alpha1", Kind: kind})
	if err := r.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, resource); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	if err := r.Delete(ctx, resource); err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	return nil
}

func (r *ServiceInstanceReconciler) applyArgoObservedStatus(ctx context.Context, instance *platformv1alpha1.ServiceInstance) {
	if instance == nil || strings.TrimSpace(instance.Status.Sync.ApplicationName) == "" {
		return
	}
	namespace := firstNonEmptyTrimmed(r.ArgoCDNamespace, "argocd")
	app := &unstructured.Unstructured{}
	app.SetGroupVersionKind(schema.GroupVersionKind{Group: "argoproj.io", Version: "v1alpha1", Kind: "Application"})
	if err := r.Get(ctx, types.NamespacedName{Name: instance.Status.Sync.ApplicationName, Namespace: namespace}, app); err != nil {
		if apierrors.IsNotFound(err) {
			r.applyArgoApplicationSetObservedStatus(ctx, instance, namespace)
		}
		return
	}

	syncStatus, _, _ := unstructured.NestedString(app.Object, "status", "sync", "status")
	healthStatus, _, _ := unstructured.NestedString(app.Object, "status", "health", "status")
	healthMessage, _, _ := unstructured.NestedString(app.Object, "status", "health", "message")

	switch strings.ToLower(syncStatus) {
	case "synced":
		instance.Status.Sync.Phase = string(adapters.SyncPhaseSynced)
		setStatusCondition(&instance.Status.Conditions, instance.Generation, "Synced", metav1.ConditionTrue, "ArgoApplicationSynced", "Argo CD application reports synced status.")
	case "outofsync":
		instance.Status.Sync.Phase = string(adapters.SyncPhaseOutOfSync)
		setStatusCondition(&instance.Status.Conditions, instance.Generation, "Synced", metav1.ConditionFalse, "ArgoApplicationOutOfSync", "Argo CD application reports out-of-sync status.")
	case "unknown":
		instance.Status.Sync.Phase = string(adapters.SyncPhaseUnknown)
	default:
		if syncStatus != "" {
			instance.Status.Sync.Phase = string(adapters.SyncPhasePending)
			setStatusCondition(&instance.Status.Conditions, instance.Generation, "Synced", metav1.ConditionFalse, "ArgoApplicationProgressing", fmt.Sprintf("Argo CD application reports %q sync status.", syncStatus))
		}
	}

	messageParts := make([]string, 0, 2)
	if syncStatus != "" {
		messageParts = append(messageParts, fmt.Sprintf("sync=%s", syncStatus))
	}
	if healthStatus != "" {
		messageParts = append(messageParts, fmt.Sprintf("health=%s", healthStatus))
	}
	if len(messageParts) > 0 {
		instance.Status.Sync.Message = strings.Join(messageParts, ", ")
	}
	if healthMessage != "" && instance.Status.Phase != "Ready" {
		instance.Status.Health.Summary = healthMessage
	}
}

func (r *ServiceInstanceReconciler) applyArgoApplicationSetObservedStatus(ctx context.Context, instance *platformv1alpha1.ServiceInstance, namespace string) {
	appSet := &unstructured.Unstructured{}
	appSet.SetGroupVersionKind(schema.GroupVersionKind{Group: "argoproj.io", Version: "v1alpha1", Kind: "ApplicationSet"})
	if err := r.Get(ctx, types.NamespacedName{Name: instance.Status.Sync.ApplicationName, Namespace: namespace}, appSet); err != nil {
		return
	}

	appList := &unstructured.UnstructuredList{}
	appList.SetGroupVersionKind(schema.GroupVersionKind{Group: "argoproj.io", Version: "v1alpha1", Kind: "ApplicationList"})
	if err := r.List(ctx, appList, client.InNamespace(namespace), client.MatchingLabels{
		"servicer.io/application-set": instance.Status.Sync.ApplicationName,
	}); err == nil && len(appList.Items) > 0 {
		syncedCount := 0
		outOfSyncCount := 0
		healthParts := make([]string, 0, len(appList.Items))
		for _, item := range appList.Items {
			syncStatus, _, _ := unstructured.NestedString(item.Object, "status", "sync", "status")
			healthStatus, _, _ := unstructured.NestedString(item.Object, "status", "health", "status")
			switch strings.ToLower(syncStatus) {
			case "synced":
				syncedCount++
			case "outofsync":
				outOfSyncCount++
			}
			if healthStatus != "" {
				healthParts = append(healthParts, fmt.Sprintf("%s=%s", item.GetName(), healthStatus))
			}
		}
		switch {
		case outOfSyncCount > 0:
			instance.Status.Sync.Phase = string(adapters.SyncPhaseOutOfSync)
			setStatusCondition(&instance.Status.Conditions, instance.Generation, "Synced", metav1.ConditionFalse, "ArgoApplicationSetOutOfSync", "One or more Argo CD applications generated from the ApplicationSet are out of sync.")
		case syncedCount == len(appList.Items):
			instance.Status.Sync.Phase = string(adapters.SyncPhaseSynced)
			setStatusCondition(&instance.Status.Conditions, instance.Generation, "Synced", metav1.ConditionTrue, "ArgoApplicationSetSynced", "All Argo CD applications generated from the ApplicationSet report synced status.")
		default:
			instance.Status.Sync.Phase = string(adapters.SyncPhasePending)
			setStatusCondition(&instance.Status.Conditions, instance.Generation, "Synced", metav1.ConditionFalse, "ArgoApplicationSetProgressing", "Argo CD ApplicationSet is still creating or syncing generated applications.")
		}
		instance.Status.Sync.Message = fmt.Sprintf("%d application(s): synced=%d, outOfSync=%d", len(appList.Items), syncedCount, outOfSyncCount)
		if len(healthParts) > 0 {
			instance.Status.Health.Summary = strings.Join(healthParts, ", ")
		}
		return
	}

	if conditions, found, _ := unstructured.NestedSlice(appSet.Object, "status", "conditions"); found && len(conditions) > 0 {
		if condition, ok := conditions[0].(map[string]any); ok {
			reason, _, _ := unstructured.NestedString(condition, "reason")
			message, _, _ := unstructured.NestedString(condition, "message")
			instance.Status.Sync.Phase = string(adapters.SyncPhasePending)
			if message != "" {
				instance.Status.Sync.Message = message
			} else if reason != "" {
				instance.Status.Sync.Message = reason
			}
			setStatusCondition(&instance.Status.Conditions, instance.Generation, "Synced", metav1.ConditionFalse, "ArgoApplicationSetProgressing", "Argo CD ApplicationSet is progressing.")
		}
	}
}

func isPodReady(pod *corev1.Pod) bool {
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

func randomPassword() (string, error) {
	bytes := make([]byte, 24)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}

func argoApplicationName(project *platformv1alpha1.Project, instance *platformv1alpha1.ServiceInstance) string {
	return fmt.Sprintf("%s-%s", project.Name, instance.Name)
}

func deliverySourcePath(deliveryRepoPath, packagePath string) string {
	return strings.TrimPrefix(filepath.ToSlash(filepath.Join(firstNonEmptyTrimmed(deliveryRepoPath, materializer.DefaultRoot), packagePath)), "/")
}

func applicationSetElements(packagePaths []string) []any {
	elements := make([]any, 0, len(packagePaths))
	seen := map[string]struct{}{}
	for _, packagePath := range packagePaths {
		cluster := clusterNameFromPackagePath(packagePath)
		sourcePath := deliverySourcePath("", packagePath)
		if cluster == "" || sourcePath == "" {
			continue
		}
		key := cluster + "|" + sourcePath
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		elements = append(elements, map[string]any{
			"cluster": cluster,
			"path":    sourcePath,
		})
	}
	return elements
}

func clusterNameFromPackagePath(packagePath string) string {
	parts := strings.Split(filepath.ToSlash(strings.Trim(packagePath, "/")), "/")
	if len(parts) < 2 || parts[0] != "clusters" {
		return ""
	}
	return parts[1]
}

func firstNonEmptyTrimmed(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func mergeStringMaps(base, overlay map[string]string) map[string]string {
	if len(base) == 0 && len(overlay) == 0 {
		return nil
	}
	merged := map[string]string{}
	for key, value := range base {
		merged[key] = value
	}
	for key, value := range overlay {
		merged[key] = value
	}
	return merged
}

func shortCommit(commit string) string {
	commit = strings.TrimSpace(commit)
	if len(commit) > 12 {
		return commit[:12]
	}
	return commit
}

func serviceInstanceContains(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}

func removeFromSlice(items []string, target string) []string {
	result := items[:0:0]
	for _, item := range items {
		if item != target {
			result = append(result, item)
		}
	}
	return result
}
