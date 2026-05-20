package controllers

import (
	"context"
	"fmt"

	platformv1alpha1 "github.com/sindef/servicer/api/v1alpha1"
	"github.com/sindef/servicer/internal/adapters"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ServiceClassReconciler reconciles ServiceClass resources.
type ServiceClassReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Adapters *adapters.Registry
}

func (r *ServiceClassReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var serviceClass platformv1alpha1.ServiceClass
	if err := r.Get(ctx, req.NamespacedName, &serviceClass); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	originalStatus := serviceClass.Status
	serviceClass.Status.ObservedGeneration = serviceClass.Generation
	serviceClass.Status.Published = false

	contract, ok := adapters.KnownContract(adapters.ServiceClass(serviceClass.Name))
	if !ok {
		serviceClass.Status.Phase = "Failed"
		setStatusCondition(&serviceClass.Status.Conditions, serviceClass.Generation, "Accepted", metav1.ConditionFalse, "UnsupportedServiceClass", fmt.Sprintf("Service class %q is not recognized by the current platform catalog.", serviceClass.Name))
		setStatusCondition(&serviceClass.Status.Conditions, serviceClass.Generation, "Ready", metav1.ConditionFalse, "UnsupportedServiceClass", "Service class is not ready for publication.")
		setStatusCondition(&serviceClass.Status.Conditions, serviceClass.Generation, "Failed", metav1.ConditionTrue, "UnsupportedServiceClass", "Service class cannot be reconciled because it is unknown.")
	} else if serviceClass.Spec.Driver != contract.RuntimeDriver {
		serviceClass.Status.Phase = "Failed"
		setStatusCondition(&serviceClass.Status.Conditions, serviceClass.Generation, "Accepted", metav1.ConditionFalse, "DriverMismatch", fmt.Sprintf("Expected driver %q for service class %q, got %q.", contract.RuntimeDriver, serviceClass.Name, serviceClass.Spec.Driver))
		setStatusCondition(&serviceClass.Status.Conditions, serviceClass.Generation, "Ready", metav1.ConditionFalse, "DriverMismatch", "Service class is not ready because its driver does not match the recognized contract.")
		setStatusCondition(&serviceClass.Status.Conditions, serviceClass.Generation, "Failed", metav1.ConditionTrue, "DriverMismatch", "Service class contract validation failed.")
	} else {
		setStatusCondition(&serviceClass.Status.Conditions, serviceClass.Generation, "Accepted", metav1.ConditionTrue, "ValidationSucceeded", "Service class contract accepted.")

		_, adapterRegistered := r.Adapters.Get(adapters.ServiceClass(serviceClass.Name))
		switch {
		case !serviceClass.Spec.Published:
			serviceClass.Status.Phase = "Draft"
			setStatusCondition(&serviceClass.Status.Conditions, serviceClass.Generation, "Ready", metav1.ConditionFalse, "Draft", "Service class is stored as a draft and not published yet.")
			setStatusCondition(&serviceClass.Status.Conditions, serviceClass.Generation, "Failed", metav1.ConditionFalse, "Draft", "Service class has not failed.")
		case !adapterRegistered:
			serviceClass.Status.Phase = "PendingImplementation"
			setStatusCondition(&serviceClass.Status.Conditions, serviceClass.Generation, "Ready", metav1.ConditionFalse, "AdapterPending", "Service class publication is blocked until a runtime adapter is registered.")
			setStatusCondition(&serviceClass.Status.Conditions, serviceClass.Generation, "Failed", metav1.ConditionFalse, "AdapterPending", "Service class has not failed; implementation is pending.")
		default:
			serviceClass.Status.Phase = "Ready"
			serviceClass.Status.Published = true
			setStatusCondition(&serviceClass.Status.Conditions, serviceClass.Generation, "Ready", metav1.ConditionTrue, "ServiceClassReady", "Service class is published and backed by a runtime adapter.")
			setStatusCondition(&serviceClass.Status.Conditions, serviceClass.Generation, "Failed", metav1.ConditionFalse, "ServiceClassReady", "Service class has not failed.")
		}
	}

	if !equality.Semantic.DeepEqual(originalStatus, serviceClass.Status) {
		if err := r.Status().Update(ctx, &serviceClass); err != nil {
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

func (r *ServiceClassReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&platformv1alpha1.ServiceClass{}).
		Complete(r)
}
