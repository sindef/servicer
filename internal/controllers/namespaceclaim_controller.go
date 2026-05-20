package controllers

import (
	"context"

	platformv1alpha1 "github.com/sindef/servicer/api/v1alpha1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// NamespaceClaimReconciler reconciles NamespaceClaim resources through backing namespace ServiceInstances.
type NamespaceClaimReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

func (r *NamespaceClaimReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var claim platformv1alpha1.NamespaceClaim
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
			setStatusCondition(&claim.Status.Conditions, claim.Generation, "Failed", metav1.ConditionTrue, "ProjectUnavailable", "Namespace claim cannot reconcile without its project.")
			return r.updateStatusIfChanged(ctx, &claim, originalStatus)
		}
		return ctrl.Result{}, err
	}

	backing := desiredNamespaceClaimInstance(&claim)
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
	claim.Status.Artifact = existing.Status.Artifact
	claim.Status.Sync = existing.Status.Sync
	claim.Status.Health = existing.Status.Health
	claim.Status.Conditions = append([]metav1.Condition(nil), existing.Status.Conditions...)
	setStatusCondition(&claim.Status.Conditions, claim.Generation, "Accepted", metav1.ConditionTrue, "BackedByServiceInstance", "Namespace claim is reconciled through a backing namespace service instance.")

	return r.updateStatusIfChanged(ctx, &claim, originalStatus)
}

func (r *NamespaceClaimReconciler) updateStatusIfChanged(ctx context.Context, claim *platformv1alpha1.NamespaceClaim, original platformv1alpha1.NamespaceClaimStatus) (ctrl.Result, error) {
	if equality.Semantic.DeepEqual(original, claim.Status) {
		return ctrl.Result{}, nil
	}
	return ctrl.Result{}, r.Status().Update(ctx, claim)
}

func (r *NamespaceClaimReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&platformv1alpha1.NamespaceClaim{}).
		Complete(r)
}

func desiredNamespaceClaimInstance(claim *platformv1alpha1.NamespaceClaim) *platformv1alpha1.ServiceInstance {
	parameters := map[string]string{}
	for key, value := range claim.Spec.Quotas {
		parameters[key] = value
	}
	rawParams := mapToJSON(parameters)
	return &platformv1alpha1.ServiceInstance{
		ObjectMeta: metav1.ObjectMeta{
			Name: claim.Name,
			Labels: map[string]string{
				"servicer.io/managed-by":      "namespaceclaim-controller",
				"servicer.io/namespace-claim": claim.Name,
			},
		},
		Spec: platformv1alpha1.ServiceInstanceSpec{
			ProjectRef:      claim.Spec.ProjectRef,
			ServiceClassRef: platformv1alpha1.LocalObjectReference{Name: "namespace"},
			ServicePlanRef:  platformv1alpha1.LocalObjectReference{Name: "namespace-team"},
			Parameters:      rawParams,
			Exposure:        platformv1alpha1.ExposureSpec{Mode: platformv1alpha1.ExposureModeClusterInternal},
			SecretPolicy:    platformv1alpha1.SecretPolicySpec{DeliveryMode: platformv1alpha1.SecretDeliveryModeManual},
			DeletionPolicy:  claim.Spec.DeletionPolicy,
		},
	}
}
