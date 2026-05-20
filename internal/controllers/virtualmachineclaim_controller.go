package controllers

import (
	"context"
	"fmt"

	platformv1alpha1 "github.com/sindef/servicer/api/v1alpha1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// VirtualMachineClaimReconciler reconciles VirtualMachineClaim resources.
type VirtualMachineClaimReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

func (r *VirtualMachineClaimReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var claim platformv1alpha1.VirtualMachineClaim
	if err := r.Get(ctx, req.NamespacedName, &claim); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	originalStatus := claim.Status
	claim.Status.ObservedGeneration = claim.Generation

	var project platformv1alpha1.Project
	if err := r.Get(ctx, client.ObjectKey{Name: claim.Spec.ProjectRef.Name}, &project); err != nil {
		if apierrors.IsNotFound(err) {
			claim.Status.Phase = "Failed"
			setStatusCondition(&claim.Status.Conditions, claim.Generation, "Accepted", metav1.ConditionFalse, "ProjectUnavailable", "Referenced project is not available.")
			setStatusCondition(&claim.Status.Conditions, claim.Generation, "Failed", metav1.ConditionTrue, "ProjectUnavailable", "Virtual machine claim cannot reconcile without its project.")
			return r.updateStatusIfChanged(ctx, &claim, originalStatus)
		}
		return ctrl.Result{}, err
	}

	claim.Status.Phase = "PendingDriver"
	claim.Status.Placement.ClusterName = project.Status.Placement.ClusterName
	claim.Status.Health = platformv1alpha1.HealthStatus{Summary: fmt.Sprintf("VirtualMachineClaim %q is accepted, but no KubeVirt-backed runtime adapter is implemented yet.", claim.Name)}
	setStatusCondition(&claim.Status.Conditions, claim.Generation, "Accepted", metav1.ConditionTrue, "SpecAccepted", "Virtual machine claim accepted for future runtime reconciliation.")
	setStatusCondition(&claim.Status.Conditions, claim.Generation, "Ready", metav1.ConditionFalse, "RuntimeAdapterPending", "Virtual machine runtime adapter is not implemented yet.")
	setStatusCondition(&claim.Status.Conditions, claim.Generation, "Failed", metav1.ConditionFalse, "RuntimeAdapterPending", "Virtual machine claim has not failed; runtime support is pending.")

	return r.updateStatusIfChanged(ctx, &claim, originalStatus)
}

func (r *VirtualMachineClaimReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&platformv1alpha1.VirtualMachineClaim{}).
		Complete(r)
}

func (r *VirtualMachineClaimReconciler) updateStatusIfChanged(ctx context.Context, claim *platformv1alpha1.VirtualMachineClaim, original platformv1alpha1.VirtualMachineClaimStatus) (ctrl.Result, error) {
	if equality.Semantic.DeepEqual(original, claim.Status) {
		return ctrl.Result{}, nil
	}
	return ctrl.Result{}, r.Status().Update(ctx, claim)
}
