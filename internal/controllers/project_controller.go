package controllers

import (
	"context"
	"fmt"
	"time"

	platformv1alpha1 "github.com/sindef/servicer/api/v1alpha1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ProjectReconciler reconciles Project resources.
type ProjectReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

func (r *ProjectReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var project platformv1alpha1.Project
	if err := r.Get(ctx, req.NamespacedName, &project); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	originalStatus := project.Status
	project.Status.ObservedGeneration = project.Generation
	project.Status.EffectiveQuota = project.Spec.Quotas
	if err := r.populateUsageSummary(ctx, &project); err != nil {
		return ctrl.Result{}, err
	}

	var tenant platformv1alpha1.Tenant
	if err := r.Get(ctx, client.ObjectKey{Name: project.Spec.TenantRef.Name}, &tenant); err != nil {
		return r.handleDependencyError(ctx, &project, originalStatus, "TenantUnavailable", fmt.Sprintf("Referenced Tenant %q is not available.", project.Spec.TenantRef.Name), err)
	}

	if tenant.Spec.Lifecycle.Phase != platformv1alpha1.TenantLifecyclePhaseActive {
		project.Status.Phase = "Failed"
		setStatusCondition(&project.Status.Conditions, project.Generation, "Accepted", metav1.ConditionFalse, "TenantInactive", fmt.Sprintf("Tenant %q is not active.", tenant.Name))
		setStatusCondition(&project.Status.Conditions, project.Generation, "Placed", metav1.ConditionFalse, "TenantInactive", "Project placement is blocked until the tenant is active.")
		setStatusCondition(&project.Status.Conditions, project.Generation, "Ready", metav1.ConditionFalse, "TenantInactive", "Project is not ready because its tenant is inactive.")
		setStatusCondition(&project.Status.Conditions, project.Generation, "Failed", metav1.ConditionTrue, "TenantInactive", "Project cannot proceed while the tenant is inactive.")
		if !equality.Semantic.DeepEqual(originalStatus, project.Status) {
			if err := r.Status().Update(ctx, &project); err != nil {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	clusterName := ""
	if project.Spec.TargetSelector.ClusterRef != nil {
		clusterName = project.Spec.TargetSelector.ClusterRef.Name
		var clusterTarget platformv1alpha1.ClusterTarget
		if err := r.Get(ctx, client.ObjectKey{Name: clusterName}, &clusterTarget); err != nil {
			return r.handleDependencyError(ctx, &project, originalStatus, "ClusterTargetUnavailable", fmt.Sprintf("Referenced ClusterTarget %q is not available.", clusterName), err)
		}
		if !isStatusCurrent(clusterTarget.Generation, clusterTarget.Status.ObservedGeneration) || !isStatusConditionTrue(clusterTarget.Status.Conditions, "Ready") || !clusterTarget.Status.Reachable {
			project.Status.Placement.ClusterName = clusterName
			project.Status.Phase = "PendingPlacement"
			setStatusCondition(&project.Status.Conditions, project.Generation, "Accepted", metav1.ConditionTrue, "SpecAccepted", "Project accepted for reconciliation.")
			setStatusCondition(&project.Status.Conditions, project.Generation, "Placed", metav1.ConditionFalse, "ClusterTargetNotReady", fmt.Sprintf("Cluster target %q is not ready for placement yet.", clusterName))
			setStatusCondition(&project.Status.Conditions, project.Generation, "Ready", metav1.ConditionFalse, "ClusterTargetNotReady", "Project is not ready until the selected cluster target is ready.")
			setStatusCondition(&project.Status.Conditions, project.Generation, "Failed", metav1.ConditionFalse, "ClusterTargetNotReady", "Project has not failed; placement is waiting on the cluster target.")
			if !equality.Semantic.DeepEqual(originalStatus, project.Status) {
				if err := r.Status().Update(ctx, &project); err != nil {
					return ctrl.Result{}, err
				}
			}
			return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
		}
	}
	project.Status.Placement.ClusterName = clusterName

	setStatusCondition(&project.Status.Conditions, project.Generation, "Accepted", metav1.ConditionTrue, "SpecAccepted", "Project accepted for reconciliation.")
	if clusterName == "" {
		project.Status.Phase = "PendingPlacement"
		setStatusCondition(&project.Status.Conditions, project.Generation, "Placed", metav1.ConditionFalse, "PlacementPending", "Project is waiting for a resolved cluster target.")
		setStatusCondition(&project.Status.Conditions, project.Generation, "Ready", metav1.ConditionFalse, "PlacementPending", "Project is not ready until placement is resolved.")
		setStatusCondition(&project.Status.Conditions, project.Generation, "Failed", metav1.ConditionFalse, "PlacementPending", "Project has not failed.")
	} else {
		project.Status.Phase = "Ready"
		setStatusCondition(&project.Status.Conditions, project.Generation, "Placed", metav1.ConditionTrue, "PlacementResolved", "Project placement resolved.")
		setStatusCondition(&project.Status.Conditions, project.Generation, "Ready", metav1.ConditionTrue, "ProjectReady", "Project is ready for product placement.")
		setStatusCondition(&project.Status.Conditions, project.Generation, "Failed", metav1.ConditionFalse, "ProjectReady", "Project has not failed.")
	}

	if !equality.Semantic.DeepEqual(originalStatus, project.Status) {
		if err := r.Status().Update(ctx, &project); err != nil {
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

func (r *ProjectReconciler) populateUsageSummary(ctx context.Context, project *platformv1alpha1.Project) error {
	var instances platformv1alpha1.ServiceInstanceList
	if err := r.List(ctx, &instances); err != nil {
		return err
	}

	project.Status.UsageSummary = platformv1alpha1.UsageSummary{}
	namespaces := map[string]struct{}{}
	for _, instance := range instances.Items {
		if instance.DeletionTimestamp != nil || instance.Spec.ProjectRef.Name != project.Name {
			continue
		}
		project.Status.UsageSummary.ServiceInstances++
		namespace := instance.Status.Placement.Namespace
		if namespace == "" {
			namespace = resolvedNamespace(project, &instance)
		}
		if namespace != "" {
			namespaces[namespace] = struct{}{}
		}
	}
	project.Status.UsageSummary.Namespaces = int32(len(namespaces))
	return nil
}

func (r *ProjectReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&platformv1alpha1.Project{}).
		Complete(r)
}

func (r *ProjectReconciler) handleDependencyError(ctx context.Context, project *platformv1alpha1.Project, originalStatus platformv1alpha1.ProjectStatus, reason, message string, err error) (ctrl.Result, error) {
	if err != nil && !apierrors.IsNotFound(err) {
		return ctrl.Result{}, err
	}

	project.Status.Phase = "Failed"
	setStatusCondition(&project.Status.Conditions, project.Generation, "Accepted", metav1.ConditionFalse, reason, message)
	setStatusCondition(&project.Status.Conditions, project.Generation, "Placed", metav1.ConditionFalse, reason, message)
	setStatusCondition(&project.Status.Conditions, project.Generation, "Ready", metav1.ConditionFalse, reason, message)
	setStatusCondition(&project.Status.Conditions, project.Generation, "Failed", metav1.ConditionTrue, reason, message)
	if !equality.Semantic.DeepEqual(originalStatus, project.Status) {
		if updateErr := r.Status().Update(ctx, project); updateErr != nil {
			return ctrl.Result{}, updateErr
		}
	}
	return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
}
