package controllers

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	platformv1alpha1 "github.com/sindef/servicer/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ClusterTargetReconciler reconciles ClusterTarget resources.
type ClusterTargetReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

func (r *ClusterTargetReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var target platformv1alpha1.ClusterTarget
	if err := r.Get(ctx, req.NamespacedName, &target); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	originalStatus := target.Status
	target.Status.ObservedGeneration = target.Generation
	target.Status.OperatorInventory = operatorInventoryFromCapabilities(target.Spec.Capabilities)
	target.Status.KubernetesVersion = clusterCapabilityValue(target.Spec.Capabilities, "kubernetesVersion", "kubernetes-version")
	requeueAfter := time.Duration(0)

	setStatusCondition(&target.Status.Conditions, target.Generation, "Accepted", metav1.ConditionTrue, "ValidationSucceeded", "Cluster target accepted for reconciliation.")

	var connectionSecret corev1.Secret
	err := r.Get(ctx, client.ObjectKey{Name: target.Spec.ConnectionRef.Name, Namespace: target.Spec.ConnectionRef.Namespace}, &connectionSecret)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
		target.Status.Phase = "PendingCredentials"
		target.Status.Reachable = false
		target.Status.LastValidatedAt = nil
		requeueAfter = 30 * time.Second
		setStatusCondition(&target.Status.Conditions, target.Generation, "Ready", metav1.ConditionFalse, "CredentialsPending", fmt.Sprintf("Waiting for connection Secret %s/%s.", target.Spec.ConnectionRef.Namespace, target.Spec.ConnectionRef.Name))
		setStatusCondition(&target.Status.Conditions, target.Generation, "Failed", metav1.ConditionFalse, "CredentialsPending", "Cluster target has not failed; credentials are still pending.")
	} else if !hasClusterConnectionData(&connectionSecret) {
		target.Status.Phase = "PendingCredentials"
		target.Status.Reachable = false
		target.Status.LastValidatedAt = nil
		requeueAfter = 30 * time.Second
		setStatusCondition(&target.Status.Conditions, target.Generation, "Ready", metav1.ConditionFalse, "CredentialsPending", fmt.Sprintf("Connection Secret %s/%s does not contain kubeconfig data yet.", connectionSecret.Namespace, connectionSecret.Name))
		setStatusCondition(&target.Status.Conditions, target.Generation, "Failed", metav1.ConditionFalse, "CredentialsPending", "Cluster target has not failed; credentials are incomplete.")
	} else {
		target.Status.Phase = "Ready"
		target.Status.Reachable = true
		if originalStatus.Reachable && originalStatus.LastValidatedAt != nil {
			target.Status.LastValidatedAt = originalStatus.LastValidatedAt.DeepCopy()
		} else {
			now := metav1.Now()
			target.Status.LastValidatedAt = &now
		}
		setStatusCondition(&target.Status.Conditions, target.Generation, "Ready", metav1.ConditionTrue, "ClusterValidated", "Cluster target credentials resolved for reconciliation.")
		setStatusCondition(&target.Status.Conditions, target.Generation, "Failed", metav1.ConditionFalse, "ClusterValidated", "Cluster target has not failed.")
	}

	if !equality.Semantic.DeepEqual(originalStatus, target.Status) {
		if err := r.Status().Update(ctx, &target); err != nil {
			return ctrl.Result{}, err
		}
	}

	if requeueAfter > 0 {
		return ctrl.Result{RequeueAfter: requeueAfter}, nil
	}
	return ctrl.Result{}, nil
}

func (r *ClusterTargetReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&platformv1alpha1.ClusterTarget{}).
		Complete(r)
}

func operatorInventoryFromCapabilities(capabilities map[string]string) []string {
	inventory := make([]string, 0)
	for key, value := range capabilities {
		if !strings.HasPrefix(key, "operator.") {
			continue
		}
		if value == "" || strings.EqualFold(value, "false") || strings.EqualFold(value, "disabled") {
			continue
		}
		inventory = append(inventory, strings.TrimPrefix(key, "operator."))
	}
	sort.Strings(inventory)
	return inventory
}

func clusterCapabilityValue(capabilities map[string]string, keys ...string) string {
	for _, key := range keys {
		if value := capabilities[key]; value != "" {
			return value
		}
	}
	return ""
}

func hasClusterConnectionData(secret *corev1.Secret) bool {
	if secret == nil {
		return false
	}
	if len(secret.Data["kubeconfig"]) > 0 || len(secret.Data["value"]) > 0 {
		return true
	}
	if secret.StringData != nil {
		return secret.StringData["kubeconfig"] != "" || secret.StringData["value"] != ""
	}
	return false
}
