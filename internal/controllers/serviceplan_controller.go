package controllers

import (
	"context"
	"fmt"
	"time"

	platformv1alpha1 "github.com/sindef/servicer/api/v1alpha1"
	"github.com/sindef/servicer/internal/adapters"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ServicePlanReconciler reconciles ServicePlan resources.
type ServicePlanReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

func (r *ServicePlanReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var servicePlan platformv1alpha1.ServicePlan
	if err := r.Get(ctx, req.NamespacedName, &servicePlan); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	originalStatus := servicePlan.Status
	servicePlan.Status.ObservedGeneration = servicePlan.Generation
	servicePlan.Status.Published = false

	var serviceClass platformv1alpha1.ServiceClass
	if err := r.Get(ctx, client.ObjectKey{Name: servicePlan.Spec.ServiceClassRef.Name}, &serviceClass); err != nil {
		if err != nil && !apierrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
		servicePlan.Status.Phase = "PendingServiceClass"
		setStatusCondition(&servicePlan.Status.Conditions, servicePlan.Generation, "Accepted", metav1.ConditionFalse, "ServiceClassUnavailable", fmt.Sprintf("Referenced ServiceClass %q is not available.", servicePlan.Spec.ServiceClassRef.Name))
		setStatusCondition(&servicePlan.Status.Conditions, servicePlan.Generation, "Ready", metav1.ConditionFalse, "ServiceClassUnavailable", "Service plan is waiting for its parent service class.")
		setStatusCondition(&servicePlan.Status.Conditions, servicePlan.Generation, "Failed", metav1.ConditionFalse, "ServiceClassUnavailable", "Service plan has not failed; it is waiting for its parent service class.")
		if !equality.Semantic.DeepEqual(originalStatus, servicePlan.Status) {
			if updateErr := r.Status().Update(ctx, &servicePlan); updateErr != nil {
				return ctrl.Result{}, updateErr
			}
		}
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	contract, known := adapters.KnownContract(adapters.ServiceClass(serviceClass.Name))
	if known && servicePlan.Spec.Topology != "" && !servicePlanContains(contract.TopologyModes, servicePlan.Spec.Topology) {
		servicePlan.Status.Phase = "Failed"
		setStatusCondition(&servicePlan.Status.Conditions, servicePlan.Generation, "Accepted", metav1.ConditionFalse, "UnsupportedTopology", fmt.Sprintf("Topology %q is not supported for service class %q.", servicePlan.Spec.Topology, serviceClass.Name))
		setStatusCondition(&servicePlan.Status.Conditions, servicePlan.Generation, "Ready", metav1.ConditionFalse, "UnsupportedTopology", "Service plan is not ready because its topology is invalid.")
		setStatusCondition(&servicePlan.Status.Conditions, servicePlan.Generation, "Failed", metav1.ConditionTrue, "UnsupportedTopology", "Service plan validation failed.")
	} else if servicePlan.Spec.DefaultVersion != "" && len(serviceClass.Spec.SupportedVersions) > 0 && !servicePlanContains(serviceClass.Spec.SupportedVersions, servicePlan.Spec.DefaultVersion) {
		servicePlan.Status.Phase = "Failed"
		setStatusCondition(&servicePlan.Status.Conditions, servicePlan.Generation, "Accepted", metav1.ConditionFalse, "UnsupportedVersion", fmt.Sprintf("Default version %q is not supported by service class %q.", servicePlan.Spec.DefaultVersion, serviceClass.Name))
		setStatusCondition(&servicePlan.Status.Conditions, servicePlan.Generation, "Ready", metav1.ConditionFalse, "UnsupportedVersion", "Service plan is not ready because its default version is invalid.")
		setStatusCondition(&servicePlan.Status.Conditions, servicePlan.Generation, "Failed", metav1.ConditionTrue, "UnsupportedVersion", "Service plan validation failed.")
	} else {
		setStatusCondition(&servicePlan.Status.Conditions, servicePlan.Generation, "Accepted", metav1.ConditionTrue, "ValidationSucceeded", "Service plan contract accepted.")

		switch {
		case !isStatusCurrent(serviceClass.Generation, serviceClass.Status.ObservedGeneration):
			servicePlan.Status.Phase = "PendingServiceClass"
			setStatusCondition(&servicePlan.Status.Conditions, servicePlan.Generation, "Ready", metav1.ConditionFalse, "ServiceClassPending", "Service plan is waiting for its parent service class to finish reconciliation.")
			setStatusCondition(&servicePlan.Status.Conditions, servicePlan.Generation, "Failed", metav1.ConditionFalse, "ServiceClassPending", "Service plan has not failed.")
		case !isStatusConditionTrue(serviceClass.Status.Conditions, "Accepted"):
			servicePlan.Status.Phase = "Failed"
			setStatusCondition(&servicePlan.Status.Conditions, servicePlan.Generation, "Ready", metav1.ConditionFalse, "ParentServiceClassInvalid", "Service plan is blocked by an invalid parent service class.")
			setStatusCondition(&servicePlan.Status.Conditions, servicePlan.Generation, "Failed", metav1.ConditionTrue, "ParentServiceClassInvalid", "Service plan cannot be published because its parent service class is invalid.")
		case !serviceClass.Status.Published:
			servicePlan.Status.Phase = "Draft"
			setStatusCondition(&servicePlan.Status.Conditions, servicePlan.Generation, "Ready", metav1.ConditionFalse, "ParentServiceClassUnpublished", "Service plan remains unpublished until its parent service class is published.")
			setStatusCondition(&servicePlan.Status.Conditions, servicePlan.Generation, "Failed", metav1.ConditionFalse, "ParentServiceClassUnpublished", "Service plan has not failed.")
		default:
			servicePlan.Status.Phase = "Ready"
			servicePlan.Status.Published = true
			setStatusCondition(&servicePlan.Status.Conditions, servicePlan.Generation, "Ready", metav1.ConditionTrue, "ServicePlanReady", "Service plan is published and available for provisioning.")
			setStatusCondition(&servicePlan.Status.Conditions, servicePlan.Generation, "Failed", metav1.ConditionFalse, "ServicePlanReady", "Service plan has not failed.")
		}
	}

	if !equality.Semantic.DeepEqual(originalStatus, servicePlan.Status) {
		if err := r.Status().Update(ctx, &servicePlan); err != nil {
			return ctrl.Result{}, err
		}
	}

	if servicePlan.Status.Phase == "Failed" {
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}
	return ctrl.Result{}, nil
}

func (r *ServicePlanReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&platformv1alpha1.ServicePlan{}).
		Complete(r)
}

func servicePlanContains(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}
