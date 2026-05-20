package controllers

import (
	"context"
	"encoding/json"

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

	backing := desiredVirtualMachineClaimInstance(&claim)
	var existing platformv1alpha1.ServiceInstance
	err := r.Get(ctx, client.ObjectKey{Name: backing.Name}, &existing)
	switch {
	case apierrors.IsNotFound(err):
		if createErr := r.Create(ctx, backing); createErr != nil {
			return ctrl.Result{}, createErr
		}
	case err != nil:
		return ctrl.Result{}, err
	default:
		existing.Spec = backing.Spec
		if existing.Labels == nil {
			existing.Labels = map[string]string{}
		}
		for key, value := range backing.Labels {
			existing.Labels[key] = value
		}
		if updateErr := r.Update(ctx, &existing); updateErr != nil {
			return ctrl.Result{}, updateErr
		}
	}

	if err := r.Get(ctx, client.ObjectKey{Name: backing.Name}, &existing); err != nil {
		return ctrl.Result{}, err
	}

	claim.Status.Phase = existing.Status.Phase
	claim.Status.Placement = existing.Status.Placement
	claim.Status.Runtime = existing.Status.Runtime
	claim.Status.Endpoints = copyStringMap(existing.Status.Endpoints)
	claim.Status.CredentialRefs = append([]platformv1alpha1.NamespacedObjectReference(nil), existing.Status.CredentialRefs...)
	claim.Status.Artifact = existing.Status.Artifact
	claim.Status.Sync = existing.Status.Sync
	claim.Status.Health = existing.Status.Health
	claim.Status.Conditions = append([]metav1.Condition(nil), existing.Status.Conditions...)
	setStatusCondition(&claim.Status.Conditions, claim.Generation, "Accepted", metav1.ConditionTrue, "BackedByServiceInstance", "Virtual machine claim is reconciled through a backing KubeVirt service instance.")

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

func desiredVirtualMachineClaimInstance(claim *platformv1alpha1.VirtualMachineClaim) *platformv1alpha1.ServiceInstance {
	parameters := map[string]any{}
	if claim.Spec.Parameters != nil && len(claim.Spec.Parameters.Raw) > 0 {
		_ = json.Unmarshal(claim.Spec.Parameters.Raw, &parameters)
	}
	parameters["image"] = claim.Spec.Image
	if claim.Spec.PowerState == "stopped" {
		parameters["runStrategy"] = "Halted"
	} else {
		parameters["runStrategy"] = "Always"
	}

	planName := claim.Spec.Class
	if planName == "" || planName == "development" || planName == "dev" {
		planName = "virtual-machine-dev"
	}

	return &platformv1alpha1.ServiceInstance{
		ObjectMeta: metav1.ObjectMeta{
			Name: claim.Name,
			Labels: map[string]string{
				"servicer.io/managed-by":            "virtualmachineclaim-controller",
				"servicer.io/virtual-machine-claim": claim.Name,
			},
		},
		Spec: platformv1alpha1.ServiceInstanceSpec{
			ProjectRef:      claim.Spec.ProjectRef,
			ServiceClassRef: platformv1alpha1.LocalObjectReference{Name: "virtual-machine"},
			ServicePlanRef:  platformv1alpha1.LocalObjectReference{Name: planName},
			Parameters:      mapToJSON(parameters),
			Exposure:        claim.Spec.Exposure,
			SecretPolicy:    claim.Spec.SecretPolicy,
			DeletionPolicy:  claim.Spec.DeletionPolicy,
		},
	}
}

func copyStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	copied := make(map[string]string, len(values))
	for key, value := range values {
		copied[key] = value
	}
	return copied
}
