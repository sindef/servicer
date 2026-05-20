package controllers

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	platformv1alpha1 "github.com/sindef/servicer/api/v1alpha1"
	"github.com/sindef/servicer/internal/adapters"
	"github.com/sindef/servicer/internal/materializer"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ServiceInstanceReconciler reconciles ServiceInstance resources.
type ServiceInstanceReconciler struct {
	client.Client
	Scheme       *runtime.Scheme
	Adapters     *adapters.Registry
	Materializer *materializer.Materializer
	Recorder     record.EventRecorder
}

const instanceFinalizer = "servicer.io/instance-cleanup"

func (r *ServiceInstanceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var instance platformv1alpha1.ServiceInstance
	if err := r.Get(ctx, req.NamespacedName, &instance); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	originalStatus := instance.Status
	instance.Status.ObservedGeneration = instance.Generation

	// Handle deletion before any other logic.
	if !instance.DeletionTimestamp.IsZero() {
		return r.handleDeletion(ctx, &instance)
	}

	// Ensure finalizer is present so we can clean up on delete.
	if !serviceInstanceContains(instance.Finalizers, instanceFinalizer) {
		instance.Finalizers = append(instance.Finalizers, instanceFinalizer)
		if err := r.Update(ctx, &instance); err != nil {
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

	clusterName := resolvedClusterName(&project)
	instance.Status.Placement.ClusterName = clusterName
	instance.Status.Placement.Namespace = resolvedNamespace(&project, &instance)

	setStatusCondition(&instance.Status.Conditions, instance.Generation, "Accepted", metav1.ConditionTrue, "SpecAccepted", "Service instance accepted for reconciliation.")
	if clusterName == "" {
		instance.Status.Phase = "PendingPlacement"
		setStatusCondition(&instance.Status.Conditions, instance.Generation, "Placed", metav1.ConditionFalse, "PlacementPending", "Service instance is waiting for cluster placement.")
		if !equality.Semantic.DeepEqual(originalStatus, instance.Status) {
			if err := r.Status().Update(ctx, &instance); err != nil {
				return ctrl.Result{}, err
			}
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

	adapter, ok := r.Adapters.Get(adapters.ServiceClass(instance.Spec.ServiceClassRef.Name))
	if !ok {
		instance.Status.Phase = "Failed"
		setStatusCondition(&instance.Status.Conditions, instance.Generation, "Accepted", metav1.ConditionFalse, "UnsupportedServiceClass", fmt.Sprintf("No adapter is registered for service class %q.", instance.Spec.ServiceClassRef.Name))
		setStatusCondition(&instance.Status.Conditions, instance.Generation, "Materialized", metav1.ConditionFalse, "UnsupportedServiceClass", "Service instance cannot be materialized because its service class is unsupported.")
		setStatusCondition(&instance.Status.Conditions, instance.Generation, "Synced", metav1.ConditionFalse, "UnsupportedServiceClass", "Service instance cannot become sync-ready because its service class is unsupported.")
		setStatusCondition(&instance.Status.Conditions, instance.Generation, "Ready", metav1.ConditionFalse, "UnsupportedServiceClass", "Service instance cannot reconcile runtime health because its service class is unsupported.")
		setStatusCondition(&instance.Status.Conditions, instance.Generation, "Failed", metav1.ConditionTrue, "UnsupportedServiceClass", "Service instance cannot be reconciled because its service class is unsupported.")
		if !equality.Semantic.DeepEqual(originalStatus, instance.Status) {
			if err := r.Status().Update(ctx, &instance); err != nil {
				return ctrl.Result{}, err
			}
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
		if !equality.Semantic.DeepEqual(originalStatus, instance.Status) {
			if updateErr := r.Status().Update(ctx, &instance); updateErr != nil {
				return ctrl.Result{}, updateErr
			}
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
		if !equality.Semantic.DeepEqual(originalStatus, instance.Status) {
			if err := r.Status().Update(ctx, &instance); err != nil {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	renderResult, err := adapter.Render(ctx, adapters.RenderRequest{Context: serviceContext})
	if err != nil {
		instance.Status.Phase = "Failed"
		setStatusCondition(&instance.Status.Conditions, instance.Generation, "Materialized", metav1.ConditionFalse, "RenderFailed", err.Error())
		setStatusCondition(&instance.Status.Conditions, instance.Generation, "Failed", metav1.ConditionTrue, "RenderFailed", err.Error())
		if !equality.Semantic.DeepEqual(originalStatus, instance.Status) {
			if err := r.Status().Update(ctx, &instance); err != nil {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
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
		if !equality.Semantic.DeepEqual(originalStatus, instance.Status) {
			if err := r.Status().Update(ctx, &instance); err != nil {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	instance.Status.Phase = "Materialized"
	instance.Status.Runtime.Driver = renderResult.RuntimeDriver
	instance.Status.Runtime.ObjectRef = renderResult.PrimaryResource
	instance.Status.Endpoints = endpointStatus(renderResult.Endpoints)
	instance.Status.CredentialRefs = append([]platformv1alpha1.NamespacedObjectReference(nil), renderResult.CredentialRefs...)
	instance.Status.Artifact = materializedArtifactStatus(materializeResult)
	instance.Status.Sync = platformv1alpha1.DeliverySyncStatus{
		Phase:           string(adapters.SyncPhasePending),
		ApplicationName: argoApplicationName(&project, &instance),
		Message:         "Delivery artifacts are materialized and ready for Argo CD reconciliation.",
	}
	instance.Status.Health.Summary = fmt.Sprintf("Materialized %d artifact(s) for %s.", len(renderResult.Artifacts), renderResult.RuntimeDriver)
	setStatusCondition(&instance.Status.Conditions, instance.Generation, "Placed", metav1.ConditionTrue, "PlacementResolved", "Service instance placement resolved.")
	setStatusCondition(&instance.Status.Conditions, instance.Generation, "Materialized", metav1.ConditionTrue, "ArtifactsMaterialized", "Delivery artifacts materialized successfully.")
	setStatusCondition(&instance.Status.Conditions, instance.Generation, "Synced", metav1.ConditionFalse, "AwaitingArgoCD", "Delivery artifacts are ready for Argo CD reconciliation.")
	setStatusCondition(&instance.Status.Conditions, instance.Generation, "Ready", metav1.ConditionFalse, "AwaitingRuntime", "Service instance runtime health has not yet been observed.")
	setStatusCondition(&instance.Status.Conditions, instance.Generation, "Failed", metav1.ConditionFalse, "ReconciliationProgressing", "Service instance has materialized successfully.")

	if err := r.ensureCredentialSecrets(ctx, &instance, renderResult); err != nil {
		if apierrors.IsNotFound(err) {
			setStatusCondition(&instance.Status.Conditions, instance.Generation, "Ready", metav1.ConditionFalse, "CredentialProjectionPending", "Credential projection is waiting for the delivered runtime namespace.")
			if !equality.Semantic.DeepEqual(originalStatus, instance.Status) {
				if updateErr := r.Status().Update(ctx, &instance); updateErr != nil {
					return ctrl.Result{}, updateErr
				}
			}
			return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
		}
		instance.Status.Phase = "Failed"
		setStatusCondition(&instance.Status.Conditions, instance.Generation, "Ready", metav1.ConditionFalse, "CredentialProjectionFailed", err.Error())
		setStatusCondition(&instance.Status.Conditions, instance.Generation, "Failed", metav1.ConditionTrue, "CredentialProjectionFailed", "Service instance credential projection failed.")
		if !equality.Semantic.DeepEqual(originalStatus, instance.Status) {
			if updateErr := r.Status().Update(ctx, &instance); updateErr != nil {
				return ctrl.Result{}, updateErr
			}
		}
		return ctrl.Result{}, nil
	}

	serviceContext.Instance = &instance
	runtimeObservation, observedResources, err := r.observeRuntime(ctx, renderResult.PrimaryResource, renderResult.CredentialRefs)
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

	if r.Recorder != nil && originalStatus.Phase != instance.Status.Phase {
		eventType := corev1.EventTypeNormal
		if instance.Status.Phase == "Failed" || instance.Status.Phase == "Blocked" {
			eventType = corev1.EventTypeWarning
		}
		r.Recorder.Event(&instance, eventType, instance.Status.Phase, instance.Status.Health.Summary)
	}

	if !equality.Semantic.DeepEqual(originalStatus, instance.Status) {
		if err := r.Status().Update(ctx, &instance); err != nil {
			return ctrl.Result{}, err
		}
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

func (r *ServiceInstanceReconciler) handleDependencyError(ctx context.Context, instance *platformv1alpha1.ServiceInstance, originalStatus platformv1alpha1.ServiceInstanceStatus, reason, message string, err error) (ctrl.Result, error) {
	if err != nil && !apierrors.IsNotFound(err) {
		return ctrl.Result{}, err
	}

	instance.Status.Phase = "Failed"
	setStatusCondition(&instance.Status.Conditions, instance.Generation, "Accepted", metav1.ConditionFalse, reason, message)
	setStatusCondition(&instance.Status.Conditions, instance.Generation, "Failed", metav1.ConditionTrue, reason, message)
	if !equality.Semantic.DeepEqual(originalStatus, instance.Status) {
		if updateErr := r.Status().Update(ctx, instance); updateErr != nil {
			return ctrl.Result{}, updateErr
		}
	}
	return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
}

func (r *ServiceInstanceReconciler) handleDependencyPending(ctx context.Context, instance *platformv1alpha1.ServiceInstance, originalStatus platformv1alpha1.ServiceInstanceStatus, reason, message string) (ctrl.Result, error) {
	instance.Status.Phase = "PendingDependencies"
	setStatusCondition(&instance.Status.Conditions, instance.Generation, "Accepted", metav1.ConditionFalse, reason, message)
	setStatusCondition(&instance.Status.Conditions, instance.Generation, "Ready", metav1.ConditionFalse, reason, message)
	setStatusCondition(&instance.Status.Conditions, instance.Generation, "Failed", metav1.ConditionFalse, reason, message)
	if !equality.Semantic.DeepEqual(originalStatus, instance.Status) {
		if updateErr := r.Status().Update(ctx, instance); updateErr != nil {
			return ctrl.Result{}, updateErr
		}
	}
	return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
}

func (r *ServiceInstanceReconciler) handleDeletion(ctx context.Context, instance *platformv1alpha1.ServiceInstance) (ctrl.Result, error) {
	m := r.Materializer
	if m == nil {
		m = materializer.New("")
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
		if err := r.Get(ctx, types.NamespacedName{Name: ns}, &namespace); err == nil {
			if err := r.Delete(ctx, &namespace); err != nil && !apierrors.IsNotFound(err) {
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
			if err := r.Delete(ctx, obj); err != nil && !apierrors.IsNotFound(err) {
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
	if !equality.Semantic.DeepEqual(originalStatus, instance.Status) {
		if updateErr := r.Status().Update(ctx, instance); updateErr != nil {
			return ctrl.Result{}, updateErr
		}
	}
	return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
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
		Count:     int32(len(artifacts)),
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

func (r *ServiceInstanceReconciler) ensureCredentialSecrets(ctx context.Context, instance *platformv1alpha1.ServiceInstance, renderResult adapters.RenderResult) error {
	if renderResult.RuntimeDriver != "servicer-valkey" && renderResult.RuntimeDriver != "servicer-nats" && renderResult.RuntimeDriver != "yb-operator" && renderResult.RuntimeDriver != "servicer-mysql" {
		return nil
	}
	if instance.Spec.SecretPolicy.DeliveryMode == platformv1alpha1.SecretDeliveryModeManual {
		return nil
	}
	if renderResult.RuntimeDriver == "servicer-nats" {
		namespace := instance.Status.Placement.Namespace
		if len(renderResult.CredentialRefs) > 0 && renderResult.CredentialRefs[0].Namespace != "" {
			namespace = renderResult.CredentialRefs[0].Namespace
		}
		return r.ensureNATSCredentialSecrets(ctx, instance, namespace)
	}
	for _, ref := range renderResult.CredentialRefs {
		var secret corev1.Secret
		err := r.Get(ctx, types.NamespacedName{Name: ref.Name, Namespace: ref.Namespace}, &secret)
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
				if updateErr := r.Update(ctx, &secret); updateErr != nil {
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
		if err := r.Create(ctx, &secret); err != nil {
			return err
		}
	}
	return nil
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

func (r *ServiceInstanceReconciler) observeRuntime(ctx context.Context, ref *platformv1alpha1.TypedObjectReference, credentialRefs []platformv1alpha1.NamespacedObjectReference) (adapters.RuntimeObservation, []platformv1alpha1.TypedObjectReference, error) {
	observation := adapters.RuntimeObservation{}
	observedResources := make([]platformv1alpha1.TypedObjectReference, 0, 1)

	if ref != nil && ref.APIVersion == "v1" && ref.Kind == "Namespace" && ref.Name != "" {
		var namespace corev1.Namespace
		err := r.Get(ctx, types.NamespacedName{Name: ref.Name}, &namespace)
		if err != nil && !apierrors.IsNotFound(err) {
			return observation, nil, err
		}
		if err == nil {
			observedResources = append(observedResources, *ref)
		}
	}

	if ref != nil && ref.APIVersion == "apps/v1" && ref.Kind == "StatefulSet" && ref.Namespace != "" {
		var statefulSet appsv1.StatefulSet
		err := r.Get(ctx, types.NamespacedName{Name: ref.Name, Namespace: ref.Namespace}, &statefulSet)
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
			if err := r.List(ctx, &pods, client.InNamespace(ref.Namespace), client.MatchingLabels{
				"servicer.io/service-instance": ref.Name,
			}); err != nil {
				return observation, nil, err
			}
			observation.TotalPods = int32(len(pods.Items))
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
		err := r.Get(ctx, types.NamespacedName{Name: ref.Name, Namespace: ref.Namespace}, u)
		if err != nil && !apierrors.IsNotFound(err) {
			return observation, nil, err
		}
		if err == nil {
			observedResources = append(observedResources, *ref)
			readyInstances, _, _ := unstructured.NestedInt64(u.Object, "status", "readyInstances")
			instances, _, _ := unstructured.NestedInt64(u.Object, "status", "instances")
			observation.Workload = &adapters.WorkloadObservation{
				DesiredReplicas: int32(instances),
				ReadyReplicas:   int32(readyInstances),
				UpdatedReplicas: int32(readyInstances),
				Observed:        true,
			}
		}
	}

	if ref != nil && ref.APIVersion == "operator.yugabyte.io/v1alpha1" && ref.Kind == "YBUniverse" && ref.Namespace != "" {
		crd := &unstructured.Unstructured{}
		crd.SetGroupVersionKind(schema.GroupVersionKind{Group: "apiextensions.k8s.io", Version: "v1", Kind: "CustomResourceDefinition"})
		if err := r.Get(ctx, types.NamespacedName{Name: "ybuniverses.operator.yugabyte.io"}, crd); err != nil {
			if apierrors.IsNotFound(err) {
				observation.Blocked = true
				observation.Message = "YugabyteDB operator CRD ybuniverses.operator.yugabyte.io is not installed in the target cluster."
				return observation, observedResources, nil
			}
			return observation, nil, err
		}

		u := &unstructured.Unstructured{}
		u.SetGroupVersionKind(schema.GroupVersionKind{Group: "operator.yugabyte.io", Version: "v1alpha1", Kind: "YBUniverse"})
		err := r.Get(ctx, types.NamespacedName{Name: ref.Name, Namespace: ref.Namespace}, u)
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
					DesiredReplicas: int32(tservers),
					ReadyReplicas:   int32(readyTServers),
					UpdatedReplicas: int32(readyTServers),
					Observed:        true,
				}
			}
		}
	}

	for _, ref := range credentialRefs {
		var secret corev1.Secret
		err := r.Get(ctx, types.NamespacedName{Name: ref.Name, Namespace: ref.Namespace}, &secret)
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
