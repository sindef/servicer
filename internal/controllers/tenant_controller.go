package controllers

import (
	"context"

	platformv1alpha1 "github.com/sindef/servicer/api/v1alpha1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// TenantReconciler reconciles Tenant resources.
type TenantReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

func (r *TenantReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var tenant platformv1alpha1.Tenant
	if err := r.Get(ctx, req.NamespacedName, &tenant); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	originalStatus := tenant.Status
	tenant.Status.ObservedGeneration = tenant.Generation
	tenant.Status.Phase = "Ready"
	tenant.Status.EffectivePolicies = tenant.Status.EffectivePolicies[:0]
	for _, policyRef := range tenant.Spec.PolicyRefs {
		tenant.Status.EffectivePolicies = append(tenant.Status.EffectivePolicies, policyRef.Name)
	}

	setStatusCondition(&tenant.Status.Conditions, tenant.Generation, "Accepted", "True", "SpecAccepted", "Tenant accepted for reconciliation.")
	setStatusCondition(&tenant.Status.Conditions, tenant.Generation, "Ready", "True", "TenantReady", "Tenant is ready for dependent resources.")

	if !equality.Semantic.DeepEqual(originalStatus, tenant.Status) {
		if err := r.Status().Update(ctx, &tenant); err != nil {
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

func (r *TenantReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&platformv1alpha1.Tenant{}).
		Complete(r)
}
