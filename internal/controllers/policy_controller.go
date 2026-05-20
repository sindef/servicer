package controllers

import (
	"context"
	"fmt"

	platformv1alpha1 "github.com/sindef/servicer/api/v1alpha1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// PolicyReconciler reconciles Policy resources.
type PolicyReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

func (r *PolicyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var policy platformv1alpha1.Policy
	if err := r.Get(ctx, req.NamespacedName, &policy); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	originalStatus := policy.Status
	policy.Status.ObservedGeneration = policy.Generation
	policy.Status.RuleCount = int32(len(policy.Spec.Rules))
	policy.Status.TargetKinds = policy.Status.TargetKinds[:0]
	for _, targetKind := range policy.Spec.TargetKinds {
		policy.Status.TargetKinds = append(policy.Status.TargetKinds, string(targetKind))
	}

	if len(policy.Spec.TargetKinds) == 0 || len(policy.Spec.Rules) == 0 {
		policy.Status.Phase = "Failed"
		setStatusCondition(&policy.Status.Conditions, policy.Generation, "Accepted", metav1.ConditionFalse, "InvalidPolicy", "Policy must declare at least one target kind and rule.")
		setStatusCondition(&policy.Status.Conditions, policy.Generation, "Ready", metav1.ConditionFalse, "InvalidPolicy", "Policy is not ready for use.")
		setStatusCondition(&policy.Status.Conditions, policy.Generation, "Failed", metav1.ConditionTrue, "InvalidPolicy", "Policy specification is incomplete.")
	} else if err := validatePolicySpec(&policy); err != nil {
		policy.Status.Phase = "Failed"
		setStatusCondition(&policy.Status.Conditions, policy.Generation, "Accepted", metav1.ConditionFalse, "InvalidPolicy", err.Error())
		setStatusCondition(&policy.Status.Conditions, policy.Generation, "Ready", metav1.ConditionFalse, "InvalidPolicy", "Policy is not ready for use.")
		setStatusCondition(&policy.Status.Conditions, policy.Generation, "Failed", metav1.ConditionTrue, "InvalidPolicy", err.Error())
	} else {
		policy.Status.Phase = "Ready"
		setStatusCondition(&policy.Status.Conditions, policy.Generation, "Accepted", metav1.ConditionTrue, "PolicyAccepted", "Policy specification accepted.")
		setStatusCondition(&policy.Status.Conditions, policy.Generation, "Ready", metav1.ConditionTrue, "PolicyReady", fmt.Sprintf("Policy exposes %d rule(s).", len(policy.Spec.Rules)))
		setStatusCondition(&policy.Status.Conditions, policy.Generation, "Failed", metav1.ConditionFalse, "PolicyReady", "Policy has not failed.")
	}

	if !equality.Semantic.DeepEqual(originalStatus, policy.Status) {
		if err := r.Status().Update(ctx, &policy); err != nil {
			return ctrl.Result{}, err
		}
	}
	return ctrl.Result{}, nil
}

func (r *PolicyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&platformv1alpha1.Policy{}).
		Complete(r)
}
