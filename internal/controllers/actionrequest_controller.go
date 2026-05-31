package controllers

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	platformv1alpha1 "github.com/sindef/servicer/api/v1alpha1"
	"github.com/sindef/servicer/internal/adapters"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ActionRequestReconciler reconciles ActionRequest resources.
type ActionRequestReconciler struct {
	client.Client
	Scheme                     *runtime.Scheme
	Adapters                   *adapters.Registry
	ProductionMode             bool
	NamespaceAccessExternalURL string
	NamespaceAccessCAData      []byte
}

type resolvedActionRequest struct {
	serviceContext adapters.ServiceContext
	adapter        adapters.ServiceAdapter
	capability     adapters.ActionCapability
}

type actionResolutionError struct {
	reason    string
	code      string
	message   string
	requeue   bool
	retryable bool
}

const (
	actionOperationStatePrepared = "Prepared"
	actionOperationStateApplied  = "Applied"
	actionOperationStateComplete = "Complete"
)

func (e *actionResolutionError) Error() string {
	return e.message
}

func (r *ActionRequestReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := ctrl.LoggerFrom(ctx).WithValues("actionRequest", req.Name)
	var actionRequest platformv1alpha1.ActionRequest
	if err := r.Get(ctx, req.NamespacedName, &actionRequest); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if requestID := strings.TrimSpace(actionRequest.Annotations["servicer.io/request-id"]); requestID != "" {
		logger = logger.WithValues("requestId", requestID)
	}
	logger.Info("reconciling action request", "phase", actionRequest.Status.Phase, "generation", actionRequest.Generation)
	if actionAlreadySubmitted(actionRequest.Status, actionRequest.Generation) {
		logger.Info("action request already reconciled for generation", "phase", actionRequest.Status.Phase, "operationId", actionRequest.Status.OperationID)
		return ctrl.Result{}, nil
	}

	resolved, err := r.resolveAction(ctx, &actionRequest)
	if err != nil {
		logger.Info("action request resolution failed", "error", err.Error())
		if resolutionErr, ok := err.(*actionResolutionError); ok {
			if _, _, updateErr := r.patchActionRequestStatus(ctx, req.NamespacedName, func(current *platformv1alpha1.ActionRequest) {
				current.Status.ObservedGeneration = current.Generation
				r.applyFailedStatus(current, resolutionErr.reason, resolutionErr.code, resolutionErr.message, resolutionErr.retryable)
				current.Status.OperationID = ""
				current.Status.OperationState = actionOperationStateComplete
			}); updateErr != nil {
				return ctrl.Result{}, updateErr
			}
			if resolutionErr.requeue {
				return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
			}
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	if approvalPending(&actionRequest, resolved.capability) {
		if _, _, updateErr := r.patchActionRequestStatus(ctx, req.NamespacedName, func(current *platformv1alpha1.ActionRequest) {
			current.Status.ObservedGeneration = current.Generation
			setStatusCondition(&current.Status.Conditions, current.Generation, "Accepted", metav1.ConditionTrue, "ValidationSucceeded", "Action request target and adapter contract resolved.")
			setStatusCondition(&current.Status.Conditions, current.Generation, "Failed", metav1.ConditionFalse, "ValidationSucceeded", "Action request has not failed.")
			current.Status.Phase = "PendingApproval"
			current.Status.OperationRef = nil
			current.Status.OperationID = ""
			current.Status.OperationState = ""
			current.Status.Result.Code = "approval-required"
			current.Status.Result.Message = fmt.Sprintf("Action %q requires approval before execution.", current.Spec.Action)
			current.Status.Result.Retryable = false
			setStatusCondition(&current.Status.Conditions, current.Generation, "Ready", metav1.ConditionFalse, "ApprovalRequired", "Action request is waiting for approval.")
		}); updateErr != nil {
			return ctrl.Result{}, updateErr
		}
	} else if actionRequest.Spec.Approval.Mode == platformv1alpha1.ApprovalModeRejected {
		if _, _, updateErr := r.patchActionRequestStatus(ctx, req.NamespacedName, func(current *platformv1alpha1.ActionRequest) {
			current.Status.ObservedGeneration = current.Generation
			setStatusCondition(&current.Status.Conditions, current.Generation, "Accepted", metav1.ConditionTrue, "ValidationSucceeded", "Action request target and adapter contract resolved.")
			setStatusCondition(&current.Status.Conditions, current.Generation, "Failed", metav1.ConditionFalse, "ValidationSucceeded", "Action request has not failed.")
			current.Status.Phase = "Failed"
			current.Status.OperationRef = nil
			current.Status.OperationID = ""
			current.Status.OperationState = actionOperationStateComplete
			current.Status.Result.Code = "approval-rejected"
			current.Status.Result.Message = "Action request was rejected before execution."
			current.Status.Result.Retryable = false
			setStatusCondition(&current.Status.Conditions, current.Generation, "Ready", metav1.ConditionFalse, "ApprovalRejected", "Action request was rejected.")
			setStatusCondition(&current.Status.Conditions, current.Generation, "Failed", metav1.ConditionTrue, "ApprovalRejected", "Action request was rejected.")
		}); updateErr != nil {
			return ctrl.Result{}, updateErr
		}
	} else if approvalApprovedWithoutApprover(&actionRequest, resolved.capability) {
		if _, _, updateErr := r.patchActionRequestStatus(ctx, req.NamespacedName, func(current *platformv1alpha1.ActionRequest) {
			current.Status.ObservedGeneration = current.Generation
			setStatusCondition(&current.Status.Conditions, current.Generation, "Accepted", metav1.ConditionTrue, "ValidationSucceeded", "Action request target and adapter contract resolved.")
			setStatusCondition(&current.Status.Conditions, current.Generation, "Failed", metav1.ConditionFalse, "ValidationSucceeded", "Action request has not failed.")
			current.Status.Phase = "Failed"
			current.Status.OperationRef = nil
			current.Status.OperationID = ""
			current.Status.OperationState = actionOperationStateComplete
			current.Status.Result.Code = "approval-invalid"
			current.Status.Result.Message = "Action request approval is missing approver identity."
			current.Status.Result.Retryable = false
			setStatusCondition(&current.Status.Conditions, current.Generation, "Ready", metav1.ConditionFalse, "ApprovalInvalid", "Action request approval metadata is incomplete.")
			setStatusCondition(&current.Status.Conditions, current.Generation, "Failed", metav1.ConditionTrue, "ApprovalInvalid", "Action request approval metadata is incomplete.")
		}); updateErr != nil {
			return ctrl.Result{}, updateErr
		}
	} else {
		preparedAction, changed, updateErr := r.patchActionRequestStatus(ctx, req.NamespacedName, func(current *platformv1alpha1.ActionRequest) {
			newGeneration := current.Status.ObservedGeneration != current.Generation
			current.Status.ObservedGeneration = current.Generation
			setStatusCondition(&current.Status.Conditions, current.Generation, "Accepted", metav1.ConditionTrue, "ValidationSucceeded", "Action request target and adapter contract resolved.")
			setStatusCondition(&current.Status.Conditions, current.Generation, "Failed", metav1.ConditionFalse, "ValidationSucceeded", "Action request has not failed.")
			// Reset in-flight state after a spec update so each generation gets a distinct operation ID.
			if newGeneration {
				current.Status.OperationID = ""
				current.Status.OperationState = ""
			}
			if strings.TrimSpace(current.Status.OperationID) == "" {
				current.Status.OperationID = actionOperationID(current)
			}
			if current.Status.OperationState == "" {
				current.Status.OperationState = actionOperationStatePrepared
			}
			if current.Status.OperationState == actionOperationStatePrepared {
				current.Status.Phase = "Running"
				now := metav1.Now()
				if current.Status.StartedAt == nil {
					current.Status.StartedAt = &now
				}
				current.Status.Result.Code = "running"
				current.Status.Result.Message = "Action request is running."
				current.Status.Result.Retryable = true
				setStatusCondition(&current.Status.Conditions, current.Generation, "Ready", metav1.ConditionFalse, "ActionRunning", "Action request is running.")
				setStatusCondition(&current.Status.Conditions, current.Generation, "Failed", metav1.ConditionFalse, "ActionRunning", "Action request is running.")
			}
		})
		if updateErr != nil {
			return ctrl.Result{}, updateErr
		}
		if changed {
			// Operation identity/state is now persisted and it is safe to execute side effects.
		}
		if preparedAction.Status.OperationState == actionOperationStateApplied {
			handled, err := r.reconcileInFlightAction(ctx, req.NamespacedName, resolved, &preparedAction)
			if err != nil {
				return ctrl.Result{}, err
			}
			if handled {
				return ctrl.Result{}, nil
			}
			return ctrl.Result{}, nil
		}
		if preparedAction.Status.OperationState != actionOperationStatePrepared {
			return ctrl.Result{}, nil
		}

		executionResult, execErr := r.executeAction(ctx, resolved, &preparedAction, preparedAction.Status.OperationID)
		if execErr != nil {
			_, _, err := r.patchActionRequestStatus(ctx, req.NamespacedName, func(current *platformv1alpha1.ActionRequest) {
				current.Status.ObservedGeneration = current.Generation
				r.applyFailedStatus(current, "ActionExecutionFailed", "execution-failed", execErr.Error(), true)
				current.Status.OperationState = actionOperationStateComplete
			})
			if err != nil {
				return ctrl.Result{}, err
			}
		} else {
			_, _, err := r.patchActionRequestStatus(ctx, req.NamespacedName, func(current *platformv1alpha1.ActionRequest) {
				current.Status.ObservedGeneration = current.Generation
				r.applyExecutionStatus(current, executionResult)
				switch current.Status.Phase {
				case "Succeeded", "Failed", "Cancelled":
					current.Status.OperationState = actionOperationStateComplete
				default:
					current.Status.OperationState = actionOperationStateApplied
				}
			})
			if err != nil {
				return ctrl.Result{}, err
			}
		}
	}

	return ctrl.Result{}, nil
}

func (r *ActionRequestReconciler) patchActionRequestStatus(ctx context.Context, key types.NamespacedName, mutate func(*platformv1alpha1.ActionRequest)) (platformv1alpha1.ActionRequest, bool, error) {
	var patched platformv1alpha1.ActionRequest
	changed := false
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		var current platformv1alpha1.ActionRequest
		if err := r.Get(ctx, key, &current); err != nil {
			return err
		}
		before := current.DeepCopy()
		mutate(&current)
		if equality.Semantic.DeepEqual(before.Status, current.Status) {
			patched = current
			changed = false
			return nil
		}
		if err := r.Status().Patch(ctx, &current, client.MergeFrom(before)); err != nil {
			return err
		}
		if before.Status.Phase != current.Status.Phase {
			observeActionRequestPhase(current.Status.Phase)
		}
		patched = current
		changed = true
		return nil
	})
	return patched, changed, err
}

func (r *ActionRequestReconciler) executeAction(ctx context.Context, resolved resolvedActionRequest, actionRequest *platformv1alpha1.ActionRequest, operationID string) (adapters.ActionExecutionResult, error) {
	if resolved.adapter.Contract().RuntimeDriver == "servicer-valkey" {
		return r.executeValkeyAction(ctx, resolved.serviceContext, actionRequest, operationID)
	}
	if resolved.adapter.Contract().RuntimeDriver == "servicer-nats" {
		return r.executeNATSAction(ctx, resolved.serviceContext, actionRequest, operationID)
	}
	if resolved.adapter.Contract().RuntimeDriver == "servicer-mysql" {
		return r.executeMySQLAction(ctx, resolved.serviceContext, actionRequest, operationID)
	}
	if resolved.adapter.Contract().RuntimeDriver == "cnpg" {
		return r.executeCNPGAction(ctx, resolved.serviceContext, actionRequest, operationID)
	}
	if resolved.adapter.Contract().RuntimeDriver == "kubernetes-namespace" {
		return r.executeNamespaceAction(ctx, resolved.serviceContext, actionRequest, operationID)
	}
	return resolved.adapter.ExecuteAction(ctx, adapters.ExecuteActionRequest{
		Context: resolved.serviceContext,
		Action:  actionRequest,
	})
}

func (r *ActionRequestReconciler) executeCNPGAction(ctx context.Context, serviceContext adapters.ServiceContext, actionRequest *platformv1alpha1.ActionRequest, operationID string) (adapters.ActionExecutionResult, error) {
	_ = operationID
	if actionRequest.Spec.Action != string(adapters.ActionBackup) {
		return adapters.ActionExecutionResult{}, fmt.Errorf("unsupported PostgreSQL action %q", actionRequest.Spec.Action)
	}
	if serviceContext.Instance == nil {
		return adapters.ActionExecutionResult{}, fmt.Errorf("service instance context is required")
	}
	namespace := actionRuntimeNamespace(serviceContext)
	operationRef := &platformv1alpha1.TypedObjectReference{
		APIVersion: "postgresql.cnpg.io/v1",
		Kind:       "Backup",
		Name:       fmt.Sprintf("%s-%s", serviceContext.Instance.Name, actionRequest.Name),
		Namespace:  namespace,
	}
	backup := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "postgresql.cnpg.io/v1",
		"kind":       "Backup",
		"metadata": map[string]any{
			"name":      operationRef.Name,
			"namespace": namespace,
			"labels": map[string]any{
				"servicer.io/managed-by": "servicer",
				"servicer.io/instance":   serviceContext.Instance.Name,
			},
		},
		"spec": map[string]any{
			"cluster": map[string]any{
				"name": serviceContext.Instance.Name,
			},
		},
	}}
	createErr := r.Create(ctx, backup)
	if createErr != nil && !apierrors.IsAlreadyExists(createErr) {
		return adapters.ActionExecutionResult{}, createErr
	}
	if apierrors.IsAlreadyExists(createErr) {
		backup = &unstructured.Unstructured{}
		backup.SetAPIVersion(operationRef.APIVersion)
		backup.SetKind(operationRef.Kind)
		if err := r.Get(ctx, types.NamespacedName{Name: operationRef.Name, Namespace: operationRef.Namespace}, backup); err != nil {
			if apierrors.IsNotFound(err) {
				return adapters.ActionExecutionResult{
					Phase:        "Queued",
					OperationRef: operationRef,
					Message:      fmt.Sprintf("CNPG Backup %s/%s request accepted; waiting for Backup object.", operationRef.Namespace, operationRef.Name),
					Retryable:    true,
				}, nil
			}
			return adapters.ActionExecutionResult{}, err
		}
	}
	result := cnpgBackupExecutionResult(backup, operationRef)
	if createErr == nil && (result.Phase == "Queued" || result.Phase == "Running") {
		result.Message = fmt.Sprintf("CNPG Backup %s/%s created; waiting for completion.", operationRef.Namespace, operationRef.Name)
	}
	return result, nil
}

func (r *ActionRequestReconciler) reconcileInFlightAction(ctx context.Context, key types.NamespacedName, resolved resolvedActionRequest, actionRequest *platformv1alpha1.ActionRequest) (bool, error) {
	if resolved.adapter.Contract().RuntimeDriver != "cnpg" || actionRequest.Spec.Action != string(adapters.ActionBackup) {
		return false, nil
	}
	operationRef := actionRequest.Status.OperationRef
	if operationRef == nil || operationRef.Kind != "Backup" || operationRef.APIVersion != "postgresql.cnpg.io/v1" || operationRef.Name == "" || operationRef.Namespace == "" {
		return false, nil
	}

	backup := &unstructured.Unstructured{}
	backup.SetAPIVersion(operationRef.APIVersion)
	backup.SetKind(operationRef.Kind)
	if err := r.Get(ctx, types.NamespacedName{Name: operationRef.Name, Namespace: operationRef.Namespace}, backup); err != nil {
		if apierrors.IsNotFound(err) {
			_, _, patchErr := r.patchActionRequestStatus(ctx, key, func(current *platformv1alpha1.ActionRequest) {
				if current.Status.Phase == "Succeeded" || current.Status.Phase == "Failed" || current.Status.Phase == "Cancelled" {
					return
				}
				r.applyExecutionStatus(current, adapters.ActionExecutionResult{
					Phase:        "Queued",
					OperationRef: operationRef,
					Message:      fmt.Sprintf("CNPG Backup %s/%s is not observed yet; retrying.", operationRef.Namespace, operationRef.Name),
					Retryable:    true,
				})
				current.Status.OperationState = actionOperationStateApplied
				current.Status.ObservedGeneration = current.Generation
			})
			return true, patchErr
		}
		return true, err
	}

	result := cnpgBackupExecutionResult(backup, operationRef)
	_, _, patchErr := r.patchActionRequestStatus(ctx, key, func(current *platformv1alpha1.ActionRequest) {
		if current.Status.Phase == "Succeeded" || current.Status.Phase == "Failed" || current.Status.Phase == "Cancelled" {
			return
		}
		r.applyExecutionStatus(current, result)
		switch current.Status.Phase {
		case "Succeeded", "Failed", "Cancelled":
			current.Status.OperationState = actionOperationStateComplete
		default:
			current.Status.OperationState = actionOperationStateApplied
		}
		current.Status.ObservedGeneration = current.Generation
	})
	return true, patchErr
}

func cnpgBackupExecutionResult(backup *unstructured.Unstructured, operationRef *platformv1alpha1.TypedObjectReference) adapters.ActionExecutionResult {
	phase, _, _ := unstructured.NestedString(backup.Object, "status", "phase")
	phase = strings.ToLower(strings.TrimSpace(phase))
	errorText := firstNonEmptyString(strings.TrimSpace(nestedString(backup.Object, "status", "error")), strings.TrimSpace(nestedString(backup.Object, "status", "commandError")), strings.TrimSpace(nestedString(backup.Object, "status", "failure")), strings.TrimSpace(nestedString(backup.Object, "status", "message")))

	switch phase {
	case "completed", "succeeded", "success", "done":
		return adapters.ActionExecutionResult{
			Phase:        "Succeeded",
			OperationRef: operationRef,
			Message:      fmt.Sprintf("CNPG Backup %s/%s completed successfully.", operationRef.Namespace, operationRef.Name),
			Retryable:    false,
		}
	case "failed", "error":
		if errorText == "" {
			errorText = "backup reported a failed phase"
		}
		return adapters.ActionExecutionResult{
			Phase:        "Failed",
			OperationRef: operationRef,
			Message:      fmt.Sprintf("CNPG Backup %s/%s failed: %s.", operationRef.Namespace, operationRef.Name, errorText),
			Retryable:    true,
		}
	}

	if errorText != "" {
		return adapters.ActionExecutionResult{
			Phase:        "Failed",
			OperationRef: operationRef,
			Message:      fmt.Sprintf("CNPG Backup %s/%s failed: %s.", operationRef.Namespace, operationRef.Name, errorText),
			Retryable:    true,
		}
	}
	if phase == "" || phase == "pending" || phase == "queued" || phase == "starting" {
		return adapters.ActionExecutionResult{
			Phase:        "Queued",
			OperationRef: operationRef,
			Message:      fmt.Sprintf("CNPG Backup %s/%s is queued.", operationRef.Namespace, operationRef.Name),
			Retryable:    true,
		}
	}
	return adapters.ActionExecutionResult{
		Phase:        "Running",
		OperationRef: operationRef,
		Message:      fmt.Sprintf("CNPG Backup %s/%s is running (phase=%s).", operationRef.Namespace, operationRef.Name, phase),
		Retryable:    true,
	}
}

func nestedString(obj map[string]any, fields ...string) string {
	value, _, _ := unstructured.NestedString(obj, fields...)
	return value
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func (r *ActionRequestReconciler) executeNATSAction(ctx context.Context, serviceContext adapters.ServiceContext, actionRequest *platformv1alpha1.ActionRequest, operationID string) (adapters.ActionExecutionResult, error) {
	switch actionRequest.Spec.Action {
	case string(adapters.ActionScale), string(adapters.ActionRestart):
		return r.executeStatefulSetBackedAction(ctx, serviceContext, actionRequest, operationID, "NATS")
	case string(adapters.ActionRotateCredentials):
		return r.rotateNATSCredentials(ctx, serviceContext, actionRequest, operationID)
	case string(adapters.ActionDeleteStream):
		params, err := natsStreamActionParameters(actionRequest)
		if err != nil {
			return adapters.ActionExecutionResult{}, err
		}
		return r.createNATSAdminJob(ctx, serviceContext, actionRequest, fmt.Sprintf("stream rm %q --force", params.Stream), "delete-stream")
	case string(adapters.ActionPurgeStream):
		params, err := natsStreamActionParameters(actionRequest)
		if err != nil {
			return adapters.ActionExecutionResult{}, err
		}
		return r.createNATSAdminJob(ctx, serviceContext, actionRequest, fmt.Sprintf("stream purge %q --force", params.Stream), "purge-stream")
	case string(adapters.ActionDeleteConsumer):
		params, err := natsConsumerActionParameters(actionRequest)
		if err != nil {
			return adapters.ActionExecutionResult{}, err
		}
		return r.createNATSAdminJob(ctx, serviceContext, actionRequest, fmt.Sprintf("consumer rm %q %q --force", params.Stream, params.Consumer), "delete-consumer")
	default:
		return adapters.ActionExecutionResult{}, fmt.Errorf("unsupported NATS action %q", actionRequest.Spec.Action)
	}
}

func (r *ActionRequestReconciler) executeStatefulSetBackedAction(ctx context.Context, serviceContext adapters.ServiceContext, actionRequest *platformv1alpha1.ActionRequest, operationID, productName string) (adapters.ActionExecutionResult, error) {
	if serviceContext.Instance == nil {
		return adapters.ActionExecutionResult{}, fmt.Errorf("service instance context is required")
	}
	namespace := actionRuntimeNamespace(serviceContext)
	operationRef := &platformv1alpha1.TypedObjectReference{
		APIVersion: "apps/v1",
		Kind:       "StatefulSet",
		Name:       serviceContext.Instance.Name,
		Namespace:  namespace,
	}

	var statefulSet appsv1.StatefulSet
	if statefulSetActionNeedsRuntimeWorkload(actionRequest.Spec.Action) {
		if err := r.Get(ctx, types.NamespacedName{Name: serviceContext.Instance.Name, Namespace: namespace}, &statefulSet); err != nil {
			if apierrors.IsNotFound(err) {
				return adapters.ActionExecutionResult{}, fmt.Errorf("%s StatefulSet %s/%s is not available yet", productName, namespace, serviceContext.Instance.Name)
			}
			return adapters.ActionExecutionResult{}, err
		}
	}

	switch actionRequest.Spec.Action {
	case string(adapters.ActionScale):
		replicas, err := scaleReplicas(actionRequest)
		if err != nil {
			return adapters.ActionExecutionResult{}, err
		}
		if statefulSet.Spec.Replicas != nil && *statefulSet.Spec.Replicas == replicas {
			return adapters.ActionExecutionResult{Phase: "Succeeded", OperationRef: operationRef, Message: fmt.Sprintf("%s scale already applied: desired replicas already set to %d.", productName, replicas), Retryable: true}, nil
		}
		statefulSet.Spec.Replicas = &replicas
		if err := r.Update(ctx, &statefulSet); err != nil {
			return adapters.ActionExecutionResult{}, err
		}
		return adapters.ActionExecutionResult{Phase: "Succeeded", OperationRef: operationRef, Message: fmt.Sprintf("%s scale applied: desired replicas set to %d.", productName, replicas), Retryable: true}, nil
	case string(adapters.ActionRestart):
		if statefulSet.Spec.Template.Annotations == nil {
			statefulSet.Spec.Template.Annotations = map[string]string{}
		}
		if statefulSet.Spec.Template.Annotations["servicer.io/restart-operation-id"] == operationID {
			return adapters.ActionExecutionResult{Phase: "Succeeded", OperationRef: operationRef, Message: fmt.Sprintf("%s restart already recorded for this operation.", productName), Retryable: true}, nil
		}
		statefulSet.Spec.Template.Annotations["servicer.io/restarted-at"] = metav1.Now().Format(time.RFC3339)
		statefulSet.Spec.Template.Annotations["servicer.io/restart-request"] = actionRequest.Name
		statefulSet.Spec.Template.Annotations["servicer.io/restart-operation-id"] = operationID
		if err := r.Update(ctx, &statefulSet); err != nil {
			return adapters.ActionExecutionResult{}, err
		}
		return adapters.ActionExecutionResult{Phase: "Succeeded", OperationRef: operationRef, Message: fmt.Sprintf("%s restart annotation applied to the StatefulSet pod template.", productName), Retryable: true}, nil
	case string(adapters.ActionRotateCredentials):
		secretName := fmt.Sprintf("%s-auth", serviceContext.Instance.Name)
		var secret corev1.Secret
		if err := r.Get(ctx, types.NamespacedName{Name: secretName, Namespace: namespace}, &secret); err != nil {
			if apierrors.IsNotFound(err) {
				return adapters.ActionExecutionResult{}, fmt.Errorf("%s credential Secret %s/%s is not available yet", productName, namespace, secretName)
			}
			return adapters.ActionExecutionResult{}, err
		}
		if secret.Annotations == nil {
			secret.Annotations = map[string]string{}
		}
		rotatedAt := secret.Annotations["servicer.io/rotated-at"]
		if secret.Annotations["servicer.io/rotation-operation-id"] != operationID {
			password, err := randomPassword()
			if err != nil {
				return adapters.ActionExecutionResult{}, err
			}
			if secret.Data == nil {
				secret.Data = map[string][]byte{}
			}
			secret.Data["password"] = []byte(password)
			rotatedAt = metav1.Now().Format(time.RFC3339)
			secret.Annotations["servicer.io/rotated-at"] = rotatedAt
			secret.Annotations["servicer.io/rotation-request"] = actionRequest.Name
			secret.Annotations["servicer.io/rotation-operation-id"] = operationID
			if err := r.Update(ctx, &secret); err != nil {
				return adapters.ActionExecutionResult{}, err
			}
		}
		if rotatedAt == "" {
			rotatedAt = metav1.Now().Format(time.RFC3339)
		}
		if statefulSet.Spec.Template.Annotations == nil {
			statefulSet.Spec.Template.Annotations = map[string]string{}
		}
		if statefulSet.Spec.Template.Annotations["servicer.io/credential-rotation-operation-id"] == operationID {
			return adapters.ActionExecutionResult{Phase: "Succeeded", OperationRef: &platformv1alpha1.TypedObjectReference{APIVersion: "v1", Kind: "Secret", Name: secretName, Namespace: namespace}, Message: fmt.Sprintf("%s credential Secret rotation already applied for this operation.", productName), Retryable: true}, nil
		}
		statefulSet.Spec.Template.Annotations["servicer.io/credential-rotated-at"] = rotatedAt
		statefulSet.Spec.Template.Annotations["servicer.io/credential-rotation-request"] = actionRequest.Name
		statefulSet.Spec.Template.Annotations["servicer.io/credential-rotation-operation-id"] = operationID
		if err := r.Update(ctx, &statefulSet); err != nil {
			return adapters.ActionExecutionResult{}, err
		}
		return adapters.ActionExecutionResult{Phase: "Succeeded", OperationRef: &platformv1alpha1.TypedObjectReference{APIVersion: "v1", Kind: "Secret", Name: secretName, Namespace: namespace}, Message: fmt.Sprintf("%s credential Secret rotated and StatefulSet rollout annotation applied.", productName), Retryable: true}, nil
	default:
		return adapters.ActionExecutionResult{}, fmt.Errorf("unsupported %s action %q", productName, actionRequest.Spec.Action)
	}
}

func (r *ActionRequestReconciler) executeNamespaceAction(ctx context.Context, serviceContext adapters.ServiceContext, actionRequest *platformv1alpha1.ActionRequest, operationID string) (adapters.ActionExecutionResult, error) {
	_ = operationID
	if serviceContext.Instance == nil {
		return adapters.ActionExecutionResult{}, fmt.Errorf("service instance context is required")
	}
	namespace := actionRuntimeNamespace(serviceContext)
	switch actionRequest.Spec.Action {
	case string(adapters.ActionUpdateQuota):
		hard, err := quotaHard(actionRequest)
		if err != nil {
			return adapters.ActionExecutionResult{}, err
		}
		quotaName := fmt.Sprintf("%s-quota", serviceContext.Instance.Name)
		var quota corev1.ResourceQuota
		if err := r.Get(ctx, types.NamespacedName{Name: quotaName, Namespace: namespace}, &quota); err != nil {
			if apierrors.IsNotFound(err) {
				return adapters.ActionExecutionResult{}, fmt.Errorf("ResourceQuota %s/%s is not available yet", namespace, quotaName)
			}
			return adapters.ActionExecutionResult{}, err
		}
		if quota.Spec.Hard == nil {
			quota.Spec.Hard = corev1.ResourceList{}
		}
		for name, quantity := range hard {
			quota.Spec.Hard[name] = quantity
		}
		if quota.Annotations == nil {
			quota.Annotations = map[string]string{}
		}
		quota.Annotations["servicer.io/quota-update-request"] = actionRequest.Name
		quota.Annotations["servicer.io/quota-updated-at"] = metav1.Now().Format(time.RFC3339)
		if err := r.Update(ctx, &quota); err != nil {
			return adapters.ActionExecutionResult{}, err
		}
		return adapters.ActionExecutionResult{
			Phase:        "Succeeded",
			OperationRef: &platformv1alpha1.TypedObjectReference{APIVersion: "v1", Kind: "ResourceQuota", Name: quotaName, Namespace: namespace},
			Message:      fmt.Sprintf("Namespace quota %s/%s updated.", namespace, quotaName),
			Retryable:    true,
		}, nil
	case string(adapters.ActionGrantAccess):
		params, err := r.namespaceAccessParameters(actionRequest)
		if err != nil {
			return adapters.ActionExecutionResult{}, err
		}
		if err := r.ensureNamespaceAccess(ctx, serviceContext, namespace, params, actionRequest.Name); err != nil {
			return adapters.ActionExecutionResult{}, err
		}
		secretName := namespaceAccessSecretName(params.Subject)
		return adapters.ActionExecutionResult{
			Phase:        "Succeeded",
			OperationRef: &platformv1alpha1.TypedObjectReference{APIVersion: "v1", Kind: "Secret", Name: secretName, Namespace: namespace},
			Message:      fmt.Sprintf("Namespace access kubeconfig written to Secret %s/%s for subject %q.", namespace, secretName, params.Subject),
			Retryable:    true,
		}, nil
	default:
		return adapters.ActionExecutionResult{}, fmt.Errorf("unsupported Namespace action %q", actionRequest.Spec.Action)
	}
}

func (r *ActionRequestReconciler) executeMySQLAction(ctx context.Context, serviceContext adapters.ServiceContext, actionRequest *platformv1alpha1.ActionRequest, operationID string) (adapters.ActionExecutionResult, error) {
	switch actionRequest.Spec.Action {
	case string(adapters.ActionScale), string(adapters.ActionRestart), string(adapters.ActionRotateCredentials):
		return r.executeStatefulSetBackedAction(ctx, serviceContext, actionRequest, operationID, "MySQL")
	case string(adapters.ActionFailover):
		return r.failoverMySQLPrimary(ctx, serviceContext, actionRequest)
	case string(adapters.ActionRollbackFailover):
		return r.rollbackMySQLPrimary(ctx, serviceContext, actionRequest)
	case string(adapters.ActionBackup):
		if serviceContext.Instance == nil {
			return adapters.ActionExecutionResult{}, fmt.Errorf("service instance context is required")
		}
		return adapters.ActionExecutionResult{
			Phase: "Queued",
			OperationRef: &platformv1alpha1.TypedObjectReference{
				APIVersion: "batch/v1",
				Kind:       "Job",
				Name:       fmt.Sprintf("%s-backup-%s", serviceContext.Instance.Name, actionRequest.Name),
				Namespace:  actionRuntimeNamespace(serviceContext),
			},
			Message:   "MySQL backup request queued for runtime execution.",
			Retryable: true,
		}, nil
	case string(adapters.ActionRestore):
		return adapters.ActionExecutionResult{
			Phase:     "Queued",
			Message:   "MySQL restore request queued for runtime execution.",
			Retryable: false,
		}, nil
	default:
		return adapters.ActionExecutionResult{}, fmt.Errorf("unsupported MySQL action %q", actionRequest.Spec.Action)
	}
}

func (r *ActionRequestReconciler) failoverMySQLPrimary(ctx context.Context, serviceContext adapters.ServiceContext, actionRequest *platformv1alpha1.ActionRequest) (adapters.ActionExecutionResult, error) {
	if serviceContext.Instance == nil {
		return adapters.ActionExecutionResult{}, fmt.Errorf("service instance context is required")
	}
	params, err := mysqlActionParameters(serviceContext.Instance)
	if err != nil {
		return adapters.ActionExecutionResult{}, err
	}
	if strings.TrimSpace(params.ReplicationMode) != "active-passive" {
		return adapters.ActionExecutionResult{}, fmt.Errorf("MySQL failover is only supported for active-passive replication")
	}
	candidate, err := failoverCandidate(actionRequest)
	if err != nil {
		return adapters.ActionExecutionResult{}, err
	}
	currentPrimary := params.PrimaryCluster
	if currentPrimary == "" {
		currentPrimary = serviceContext.Instance.Status.Placement.ClusterName
	}
	if !stringInSlice(candidate, params.StandbyClusters) {
		return adapters.ActionExecutionResult{}, fmt.Errorf("cluster %q is not a configured standby", candidate)
	}
	params.PreviousPrimaryCluster = currentPrimary
	params.PrimaryCluster = candidate
	params.StandbyClusters = uniqueStrings(append(removeString(params.StandbyClusters, candidate), currentPrimary))
	return r.persistMySQLActionParameters(ctx, serviceContext.Instance, params, fmt.Sprintf("MySQL failover promoted standby cluster %q.", candidate))
}

func (r *ActionRequestReconciler) rollbackMySQLPrimary(ctx context.Context, serviceContext adapters.ServiceContext, actionRequest *platformv1alpha1.ActionRequest) (adapters.ActionExecutionResult, error) {
	if serviceContext.Instance == nil {
		return adapters.ActionExecutionResult{}, fmt.Errorf("service instance context is required")
	}
	params, err := mysqlActionParameters(serviceContext.Instance)
	if err != nil {
		return adapters.ActionExecutionResult{}, err
	}
	if strings.TrimSpace(params.ReplicationMode) != "active-passive" {
		return adapters.ActionExecutionResult{}, fmt.Errorf("MySQL rollback is only supported for active-passive replication")
	}
	target := strings.TrimSpace(params.PreviousPrimaryCluster)
	if actionRequest.Spec.Parameters != nil && len(actionRequest.Spec.Parameters.Raw) > 0 {
		target, _ = rollbackTarget(actionRequest, serviceContext.Instance)
	}
	if target == "" {
		return adapters.ActionExecutionResult{}, fmt.Errorf("no previous primary cluster is recorded for rollback")
	}
	currentPrimary := params.PrimaryCluster
	if currentPrimary == "" {
		currentPrimary = serviceContext.Instance.Status.Placement.ClusterName
	}
	params.PreviousPrimaryCluster = currentPrimary
	params.PrimaryCluster = target
	params.StandbyClusters = uniqueStrings(append(removeString(params.StandbyClusters, target), currentPrimary))
	return r.persistMySQLActionParameters(ctx, serviceContext.Instance, params, fmt.Sprintf("MySQL rollback promoted cluster %q.", target))
}

type mysqlMutableParameters struct {
	ReplicationMode        string   `json:"replicationMode,omitempty"`
	PrimaryCluster         string   `json:"primaryCluster,omitempty"`
	StandbyClusters        []string `json:"standbyClusters,omitempty"`
	PreviousPrimaryCluster string   `json:"previousPrimaryCluster,omitempty"`
}

func mysqlActionParameters(instance *platformv1alpha1.ServiceInstance) (mysqlMutableParameters, error) {
	params := mysqlMutableParameters{}
	if instance == nil || instance.Spec.Parameters == nil || len(instance.Spec.Parameters.Raw) == 0 {
		return params, nil
	}
	if err := json.Unmarshal(instance.Spec.Parameters.Raw, &params); err != nil {
		return mysqlMutableParameters{}, fmt.Errorf("decode MySQL parameters: %w", err)
	}
	return params, nil
}

func (r *ActionRequestReconciler) persistMySQLActionParameters(ctx context.Context, instance *platformv1alpha1.ServiceInstance, params mysqlMutableParameters, message string) (adapters.ActionExecutionResult, error) {
	raw, err := json.Marshal(params)
	if err != nil {
		return adapters.ActionExecutionResult{}, err
	}
	instance.Spec.Parameters = &apiextensionsv1.JSON{Raw: raw}
	if err := r.Update(ctx, instance); err != nil {
		return adapters.ActionExecutionResult{}, err
	}
	return adapters.ActionExecutionResult{
		Phase: "Succeeded",
		OperationRef: &platformv1alpha1.TypedObjectReference{
			APIVersion: platformv1alpha1.GroupVersion.String(),
			Kind:       "ServiceInstance",
			Name:       instance.Name,
		},
		Message:   message,
		Retryable: false,
	}, nil
}

func (r *ActionRequestReconciler) rotateNATSCredentials(ctx context.Context, serviceContext adapters.ServiceContext, actionRequest *platformv1alpha1.ActionRequest, operationID string) (adapters.ActionExecutionResult, error) {
	if serviceContext.Instance == nil {
		return adapters.ActionExecutionResult{}, fmt.Errorf("service instance context is required")
	}
	namespace := actionRuntimeNamespace(serviceContext)
	params, err := natsRotateCredentialParameters(actionRequest)
	if err != nil {
		return adapters.ActionExecutionResult{}, err
	}
	spec, err := loadNATSCredentialMaterialization(serviceContext.Instance)
	if err != nil {
		return adapters.ActionExecutionResult{}, err
	}
	secretName := spec.AdminSecretName
	if params.CredentialName != "" {
		found := false
		for _, credential := range spec.AppCredentials {
			if credential.Name == params.CredentialName {
				secretName = credential.SecretName
				found = true
				break
			}
		}
		if !found {
			return adapters.ActionExecutionResult{}, fmt.Errorf("unknown NATS credential %q", params.CredentialName)
		}
	}

	var secret corev1.Secret
	if err := r.Get(ctx, types.NamespacedName{Name: secretName, Namespace: namespace}, &secret); err != nil {
		if apierrors.IsNotFound(err) {
			return adapters.ActionExecutionResult{}, fmt.Errorf("NATS credential Secret %s/%s is not available yet", namespace, secretName)
		}
		return adapters.ActionExecutionResult{}, err
	}
	if secret.Annotations == nil {
		secret.Annotations = map[string]string{}
	}
	rotatedAt := secret.Annotations["servicer.io/rotated-at"]
	if secret.Annotations["servicer.io/rotation-operation-id"] != operationID {
		password, err := randomPassword()
		if err != nil {
			return adapters.ActionExecutionResult{}, err
		}
		if secret.Data == nil {
			secret.Data = map[string][]byte{}
		}
		secret.Data["password"] = []byte(password)
		rotatedAt = metav1.Now().Format(time.RFC3339)
		secret.Annotations["servicer.io/rotated-at"] = rotatedAt
		secret.Annotations["servicer.io/rotation-request"] = actionRequest.Name
		secret.Annotations["servicer.io/rotation-operation-id"] = operationID
		if err := r.Update(ctx, &secret); err != nil {
			return adapters.ActionExecutionResult{}, err
		}
	}
	if rotatedAt == "" {
		rotatedAt = metav1.Now().Format(time.RFC3339)
	}

	if err := r.syncNATSAuthConfigSecret(ctx, serviceContext.Instance, namespace); err != nil {
		return adapters.ActionExecutionResult{}, err
	}

	var statefulSet appsv1.StatefulSet
	if err := r.Get(ctx, types.NamespacedName{Name: serviceContext.Instance.Name, Namespace: namespace}, &statefulSet); err != nil {
		if apierrors.IsNotFound(err) {
			return adapters.ActionExecutionResult{}, fmt.Errorf("NATS StatefulSet %s/%s is not available yet", namespace, serviceContext.Instance.Name)
		}
		return adapters.ActionExecutionResult{}, err
	}
	if statefulSet.Spec.Template.Annotations == nil {
		statefulSet.Spec.Template.Annotations = map[string]string{}
	}
	if statefulSet.Spec.Template.Annotations["servicer.io/credential-rotation-operation-id"] == operationID {
		message := "NATS credential Secret rotation already applied for this operation."
		if params.CredentialName != "" {
			message = fmt.Sprintf("NATS credential %q rotation already applied for this operation.", params.CredentialName)
		}
		return adapters.ActionExecutionResult{
			Phase:        "Succeeded",
			OperationRef: &platformv1alpha1.TypedObjectReference{APIVersion: "v1", Kind: "Secret", Name: secretName, Namespace: namespace},
			Message:      message,
			Retryable:    true,
		}, nil
	}
	statefulSet.Spec.Template.Annotations["servicer.io/credential-rotated-at"] = rotatedAt
	statefulSet.Spec.Template.Annotations["servicer.io/credential-rotation-request"] = actionRequest.Name
	statefulSet.Spec.Template.Annotations["servicer.io/credential-rotation-operation-id"] = operationID
	if err := r.Update(ctx, &statefulSet); err != nil {
		return adapters.ActionExecutionResult{}, err
	}

	message := "NATS credential Secret rotated and StatefulSet rollout annotation applied."
	if params.CredentialName != "" {
		message = fmt.Sprintf("NATS credential %q rotated and StatefulSet rollout annotation applied.", params.CredentialName)
	}
	return adapters.ActionExecutionResult{
		Phase:        "Succeeded",
		OperationRef: &platformv1alpha1.TypedObjectReference{APIVersion: "v1", Kind: "Secret", Name: secretName, Namespace: namespace},
		Message:      message,
		Retryable:    true,
	}, nil
}

func (r *ActionRequestReconciler) syncNATSAuthConfigSecret(ctx context.Context, instance *platformv1alpha1.ServiceInstance, namespace string) error {
	spec, err := loadNATSCredentialMaterialization(instance)
	if err != nil {
		return err
	}
	passwords := map[string]string{}
	for _, ref := range append([]string{spec.AdminSecretName}, secretNames(spec.AppCredentials)...) {
		var secret corev1.Secret
		if err := r.Get(ctx, types.NamespacedName{Name: ref, Namespace: namespace}, &secret); err != nil {
			return err
		}
		passwords[string(secret.Data["username"])] = string(secret.Data["password"])
	}
	var authSecret corev1.Secret
	if err := r.Get(ctx, types.NamespacedName{Name: spec.AuthConfigSecretName, Namespace: namespace}, &authSecret); err != nil {
		return err
	}
	if authSecret.Data == nil {
		authSecret.Data = map[string][]byte{}
	}
	authSecret.Data["users.conf"] = []byte(renderNATSUsersConfig(spec, passwords))
	return r.Update(ctx, &authSecret)
}

func secretNames(credentials []natsAppCredentialMaterialization) []string {
	names := make([]string, 0, len(credentials))
	for _, credential := range credentials {
		names = append(names, credential.SecretName)
	}
	return names
}

func (r *ActionRequestReconciler) createNATSAdminJob(ctx context.Context, serviceContext adapters.ServiceContext, actionRequest *platformv1alpha1.ActionRequest, natsCommand, actionKind string) (adapters.ActionExecutionResult, error) {
	if serviceContext.Instance == nil {
		return adapters.ActionExecutionResult{}, fmt.Errorf("service instance context is required")
	}
	namespace := actionRuntimeNamespace(serviceContext)
	jobName := fmt.Sprintf("%s-%s-%s", serviceContext.Instance.Name, actionKind, actionRequest.Name)
	command := []string{"/bin/sh", "-c", fmt.Sprintf("set -eu; until nats --server nats://%s.%s.svc.cluster.local:4222 --user \"$NATS_USER\" --password \"$NATS_PASSWORD\" account info >/dev/null 2>&1; do sleep 2; done; %s --server nats://%s.%s.svc.cluster.local:4222 --user \"$NATS_USER\" --password \"$NATS_PASSWORD\"", serviceContext.Instance.Name, namespace, natsCommand, serviceContext.Instance.Name, namespace)}
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: namespace,
			Labels: map[string]string{
				"servicer.io/managed-by":       "servicer",
				"servicer.io/service-instance": serviceContext.Instance.Name,
				"servicer.io/nats-action":      actionKind,
			},
		},
		Spec: batchv1.JobSpec{
			TTLSecondsAfterFinished: int32Ptr(600),
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"servicer.io/managed-by":       "servicer",
						"servicer.io/service-instance": serviceContext.Instance.Name,
						"servicer.io/nats-action":      actionKind,
					},
				},
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyOnFailure,
					Containers: []corev1.Container{{
						Name:    "nats-admin",
						Image:   "natsio/nats-box:0.19.5",
						Command: command,
						Env: []corev1.EnvVar{
							{Name: "NATS_USER", Value: "servicer"},
							{Name: "NATS_PASSWORD", ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: fmt.Sprintf("%s-auth", serviceContext.Instance.Name)}, Key: "password"}}},
						},
					}},
				},
			},
		},
	}
	createErr := r.Create(ctx, job)
	if createErr != nil && !apierrors.IsAlreadyExists(createErr) {
		return adapters.ActionExecutionResult{}, createErr
	}
	message := fmt.Sprintf("NATS %s job %s/%s created.", actionKind, namespace, jobName)
	if apierrors.IsAlreadyExists(createErr) {
		message = fmt.Sprintf("NATS %s job %s/%s already exists for this action request.", actionKind, namespace, jobName)
	}
	return adapters.ActionExecutionResult{
		Phase:        "Succeeded",
		OperationRef: &platformv1alpha1.TypedObjectReference{APIVersion: "batch/v1", Kind: "Job", Name: jobName, Namespace: namespace},
		Message:      message,
		Retryable:    true,
	}, nil
}

func (r *ActionRequestReconciler) executeValkeyAction(ctx context.Context, serviceContext adapters.ServiceContext, actionRequest *platformv1alpha1.ActionRequest, operationID string) (adapters.ActionExecutionResult, error) {
	if serviceContext.Instance == nil {
		return adapters.ActionExecutionResult{}, fmt.Errorf("service instance context is required")
	}

	switch actionRequest.Spec.Action {
	case string(adapters.ActionScale), string(adapters.ActionRestart), string(adapters.ActionRotateCredentials):
		return r.executeStatefulSetBackedAction(ctx, serviceContext, actionRequest, operationID, "Valkey")
	case string(adapters.ActionFailover):
		candidate, err := failoverCandidate(actionRequest)
		if err != nil {
			return adapters.ActionExecutionResult{}, err
		}
		if err := applyValkeyFailoverPreflight(serviceContext.Instance, candidate); err != nil {
			return adapters.ActionExecutionResult{}, err
		}
		_ = promoteValkeyPrimary(serviceContext.Instance, candidate, "Standby cluster %q promoted from previous primary %q.")
		if err := r.Status().Update(ctx, serviceContext.Instance); err != nil {
			return adapters.ActionExecutionResult{}, err
		}
		return adapters.ActionExecutionResult{
			Phase: "Succeeded",
			OperationRef: &platformv1alpha1.TypedObjectReference{
				APIVersion: platformv1alpha1.GroupVersion.String(),
				Kind:       "ServiceInstance",
				Name:       serviceContext.Instance.Name,
			},
			Message:   fmt.Sprintf("Valkey failover promoted standby cluster %q.", candidate),
			Retryable: false,
		}, nil
	case string(adapters.ActionRollbackFailover):
		targetPrimary, err := rollbackTarget(actionRequest, serviceContext.Instance)
		if err != nil {
			return adapters.ActionExecutionResult{}, err
		}
		if err := applyValkeyFailoverPreflight(serviceContext.Instance, targetPrimary); err != nil {
			return adapters.ActionExecutionResult{}, err
		}
		previousPrimary := promoteValkeyPrimary(serviceContext.Instance, targetPrimary, "Rollback promoted cluster %q from active primary %q.")
		if err := r.Status().Update(ctx, serviceContext.Instance); err != nil {
			return adapters.ActionExecutionResult{}, err
		}
		return adapters.ActionExecutionResult{
			Phase: "Succeeded",
			OperationRef: &platformv1alpha1.TypedObjectReference{
				APIVersion: platformv1alpha1.GroupVersion.String(),
				Kind:       "ServiceInstance",
				Name:       serviceContext.Instance.Name,
			},
			Message:   fmt.Sprintf("Valkey rollback promoted cluster %q from %q.", targetPrimary, previousPrimary),
			Retryable: false,
		}, nil
	case string(adapters.ActionResyncStandby):
		standbyCluster, err := standbyTarget(actionRequest)
		if err != nil {
			return adapters.ActionExecutionResult{}, err
		}
		if err := markValkeyStandbyResyncRequested(serviceContext.Instance, standbyCluster, actionRequest.Name); err != nil {
			return adapters.ActionExecutionResult{}, err
		}
		if err := r.Status().Update(ctx, serviceContext.Instance); err != nil {
			return adapters.ActionExecutionResult{}, err
		}
		return adapters.ActionExecutionResult{
			Phase: "Succeeded",
			OperationRef: &platformv1alpha1.TypedObjectReference{
				APIVersion: platformv1alpha1.GroupVersion.String(),
				Kind:       "ServiceInstance",
				Name:       serviceContext.Instance.Name,
			},
			Message:   fmt.Sprintf("Valkey standby cluster %q marked for resynchronization.", standbyCluster),
			Retryable: true,
		}, nil
	default:
		return adapters.ActionExecutionResult{}, fmt.Errorf("unsupported Valkey action %q", actionRequest.Spec.Action)
	}
}

func actionRuntimeNamespace(serviceContext adapters.ServiceContext) string {
	if serviceContext.Instance != nil && serviceContext.Instance.Status.Placement.Namespace != "" {
		return serviceContext.Instance.Status.Placement.Namespace
	}
	if serviceContext.Project != nil && serviceContext.Instance != nil {
		return resolvedNamespace(serviceContext.Project, serviceContext.Instance)
	}
	return ""
}

func actionOperationID(actionRequest *platformv1alpha1.ActionRequest) string {
	seed := string(actionRequest.UID)
	if strings.TrimSpace(seed) == "" {
		seed = actionRequest.Name
	}
	return fmt.Sprintf("%s-%d", seed, actionRequest.Generation)
}

func statefulSetActionNeedsRuntimeWorkload(action string) bool {
	switch action {
	case string(adapters.ActionScale), string(adapters.ActionRestart), string(adapters.ActionRotateCredentials):
		return true
	default:
		return false
	}
}

func removeString(items []string, target string) []string {
	result := make([]string, 0, len(items))
	for _, item := range items {
		if item != target {
			result = append(result, item)
		}
	}
	return result
}

func uniqueStrings(items []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(items))
	for _, item := range items {
		value := strings.TrimSpace(item)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func stringInSlice(target string, items []string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}

func (r *ActionRequestReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&platformv1alpha1.ActionRequest{}).
		Complete(r)
}

func (r *ActionRequestReconciler) resolveAction(ctx context.Context, actionRequest *platformv1alpha1.ActionRequest) (resolvedActionRequest, error) {
	if r.Adapters == nil {
		return resolvedActionRequest{}, fmt.Errorf("adapter registry is required")
	}
	if actionRequest.Spec.TargetRef.APIVersion != platformv1alpha1.GroupVersion.String() {
		return resolvedActionRequest{}, &actionResolutionError{
			reason:    "UnsupportedTargetAPIVersion",
			code:      "unsupported-target-api-version",
			message:   fmt.Sprintf("Target apiVersion %q is unsupported.", actionRequest.Spec.TargetRef.APIVersion),
			retryable: false,
		}
	}
	if !strings.EqualFold(actionRequest.Spec.TargetRef.Kind, "ServiceInstance") {
		return resolvedActionRequest{}, &actionResolutionError{
			reason:    "UnsupportedTargetKind",
			code:      "unsupported-target-kind",
			message:   fmt.Sprintf("Target kind %q is unsupported by the current action controller.", actionRequest.Spec.TargetRef.Kind),
			retryable: false,
		}
	}

	var instance platformv1alpha1.ServiceInstance
	if err := r.Get(ctx, client.ObjectKey{Name: actionRequest.Spec.TargetRef.Name}, &instance); err != nil {
		return resolvedActionRequest{}, r.wrapDependencyError(err, "TargetUnavailable", "target-unavailable", fmt.Sprintf("Referenced ServiceInstance %q is not available.", actionRequest.Spec.TargetRef.Name))
	}

	var project platformv1alpha1.Project
	if err := r.Get(ctx, client.ObjectKey{Name: instance.Spec.ProjectRef.Name}, &project); err != nil {
		return resolvedActionRequest{}, r.wrapDependencyError(err, "ProjectUnavailable", "project-unavailable", fmt.Sprintf("Referenced Project %q is not available.", instance.Spec.ProjectRef.Name))
	}

	var tenant platformv1alpha1.Tenant
	if err := r.Get(ctx, client.ObjectKey{Name: project.Spec.TenantRef.Name}, &tenant); err != nil {
		return resolvedActionRequest{}, r.wrapDependencyError(err, "TenantUnavailable", "tenant-unavailable", fmt.Sprintf("Referenced Tenant %q is not available.", project.Spec.TenantRef.Name))
	}

	var class platformv1alpha1.ServiceClass
	if err := r.Get(ctx, client.ObjectKey{Name: instance.Spec.ServiceClassRef.Name}, &class); err != nil {
		return resolvedActionRequest{}, r.wrapDependencyError(err, "ServiceClassUnavailable", "service-class-unavailable", fmt.Sprintf("Referenced ServiceClass %q is not available.", instance.Spec.ServiceClassRef.Name))
	}

	var plan platformv1alpha1.ServicePlan
	if err := r.Get(ctx, client.ObjectKey{Name: instance.Spec.ServicePlanRef.Name}, &plan); err != nil {
		return resolvedActionRequest{}, r.wrapDependencyError(err, "ServicePlanUnavailable", "service-plan-unavailable", fmt.Sprintf("Referenced ServicePlan %q is not available.", instance.Spec.ServicePlanRef.Name))
	}

	var clusterTarget *platformv1alpha1.ClusterTarget
	if project.Spec.TargetSelector.ClusterRef != nil {
		var target platformv1alpha1.ClusterTarget
		if err := r.Get(ctx, client.ObjectKey{Name: project.Spec.TargetSelector.ClusterRef.Name}, &target); err != nil {
			return resolvedActionRequest{}, r.wrapDependencyError(err, "ClusterTargetUnavailable", "cluster-target-unavailable", fmt.Sprintf("Referenced ClusterTarget %q is not available.", project.Spec.TargetSelector.ClusterRef.Name))
		}
		clusterTarget = &target
	}

	adapter, ok := r.Adapters.Get(adapters.ServiceClass(instance.Spec.ServiceClassRef.Name))
	if !ok {
		return resolvedActionRequest{}, &actionResolutionError{
			reason:    "UnsupportedServiceClass",
			code:      "unsupported-service-class",
			message:   fmt.Sprintf("No adapter is registered for service class %q.", instance.Spec.ServiceClassRef.Name),
			retryable: false,
		}
	}

	serviceContext := adapters.ServiceContext{
		Tenant:        &tenant,
		Project:       &project,
		ClusterTarget: clusterTarget,
		Class:         &class,
		Plan:          &plan,
		Instance:      &instance,
	}

	capability, ok := actionCapability(adapter.SupportedActions(ctx, serviceContext), actionRequest.Spec.Action)
	if !ok {
		return resolvedActionRequest{}, &actionResolutionError{
			reason:    "UnsupportedAction",
			code:      "unsupported-action",
			message:   fmt.Sprintf("Action %q is not supported for service class %q.", actionRequest.Spec.Action, instance.Spec.ServiceClassRef.Name),
			retryable: false,
		}
	}

	return resolvedActionRequest{serviceContext: serviceContext, adapter: adapter, capability: capability}, nil
}

func (r *ActionRequestReconciler) wrapDependencyError(err error, reason, code, message string) error {
	if err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	return &actionResolutionError{
		reason:    reason,
		code:      code,
		message:   message,
		requeue:   true,
		retryable: true,
	}
}

func (r *ActionRequestReconciler) applyFailedStatus(actionRequest *platformv1alpha1.ActionRequest, reason, code, message string, retryable bool) {
	actionRequest.Status.Phase = "Failed"
	actionRequest.Status.OperationRef = nil
	actionRequest.Status.Result.Code = code
	actionRequest.Status.Result.Message = message
	actionRequest.Status.Result.Retryable = retryable
	setStatusCondition(&actionRequest.Status.Conditions, actionRequest.Generation, "Accepted", metav1.ConditionFalse, reason, message)
	setStatusCondition(&actionRequest.Status.Conditions, actionRequest.Generation, "Ready", metav1.ConditionFalse, reason, message)
	setStatusCondition(&actionRequest.Status.Conditions, actionRequest.Generation, "Failed", metav1.ConditionTrue, reason, message)
}

func (r *ActionRequestReconciler) applyExecutionStatus(actionRequest *platformv1alpha1.ActionRequest, result adapters.ActionExecutionResult) {
	now := metav1.Now()
	if actionRequest.Status.StartedAt == nil {
		actionRequest.Status.StartedAt = &now
	}
	actionRequest.Status.Phase = normalizedActionPhase(result.Phase)
	actionRequest.Status.OperationRef = result.OperationRef
	actionRequest.Status.Result.Code = resultCodeForPhase(actionRequest.Status.Phase)
	actionRequest.Status.Result.Message = result.Message
	actionRequest.Status.Result.Retryable = result.Retryable

	switch actionRequest.Status.Phase {
	case "Succeeded":
		actionRequest.Status.CompletedAt = &now
		setStatusCondition(&actionRequest.Status.Conditions, actionRequest.Generation, "Ready", metav1.ConditionTrue, "ActionSucceeded", "Action request completed successfully.")
		setStatusCondition(&actionRequest.Status.Conditions, actionRequest.Generation, "Failed", metav1.ConditionFalse, "ActionSucceeded", "Action request completed successfully.")
	case "Failed", "Cancelled":
		actionRequest.Status.CompletedAt = &now
		setStatusCondition(&actionRequest.Status.Conditions, actionRequest.Generation, "Ready", metav1.ConditionFalse, "ActionFailed", result.Message)
		setStatusCondition(&actionRequest.Status.Conditions, actionRequest.Generation, "Failed", metav1.ConditionTrue, "ActionFailed", result.Message)
	case "Running":
		setStatusCondition(&actionRequest.Status.Conditions, actionRequest.Generation, "Ready", metav1.ConditionFalse, "ActionRunning", "Action request is running.")
		setStatusCondition(&actionRequest.Status.Conditions, actionRequest.Generation, "Failed", metav1.ConditionFalse, "ActionRunning", "Action request is running.")
	default:
		setStatusCondition(&actionRequest.Status.Conditions, actionRequest.Generation, "Ready", metav1.ConditionFalse, "ActionQueued", "Action request is queued for execution.")
		setStatusCondition(&actionRequest.Status.Conditions, actionRequest.Generation, "Failed", metav1.ConditionFalse, "ActionQueued", "Action request is queued for execution.")
	}
}

func approvalPending(actionRequest *platformv1alpha1.ActionRequest, capability adapters.ActionCapability) bool {
	if actionRequest.Spec.Approval.Mode == platformv1alpha1.ApprovalModeRequired {
		return true
	}
	return capability.RequiresApproval && actionRequest.Spec.Approval.Mode != platformv1alpha1.ApprovalModeApproved
}

func approvalApprovedWithoutApprover(actionRequest *platformv1alpha1.ActionRequest, capability adapters.ActionCapability) bool {
	if !capability.RequiresApproval {
		return false
	}
	if actionRequest.Spec.Approval.Mode != platformv1alpha1.ApprovalModeApproved {
		return false
	}
	return len(actionRequest.Spec.Approval.ApprovedBy) == 0
}

func actionCapability(capabilities []adapters.ActionCapability, action string) (adapters.ActionCapability, bool) {
	for _, capability := range capabilities {
		if string(capability.Name) == action {
			return capability, true
		}
	}
	return adapters.ActionCapability{}, false
}

func actionAlreadySubmitted(status platformv1alpha1.ActionRequestStatus, generation int64) bool {
	if status.ObservedGeneration != generation {
		return false
	}
	switch status.Phase {
	case "Succeeded", "Failed", "Cancelled":
		return true
	default:
		return false
	}
}

func normalizedActionPhase(phase string) string {
	trimmed := strings.TrimSpace(phase)
	switch trimmed {
	case "Running", "Succeeded", "Failed", "Cancelled":
		return trimmed
	default:
		return "Queued"
	}
}

func resultCodeForPhase(phase string) string {
	switch phase {
	case "Running":
		return "running"
	case "Succeeded":
		return "succeeded"
	case "Failed":
		return "failed"
	case "Cancelled":
		return "cancelled"
	default:
		return "queued"
	}
}

func int32Ptr(value int32) *int32 {
	return &value
}

func scaleReplicas(actionRequest *platformv1alpha1.ActionRequest) (int32, error) {
	var parameters struct {
		Replicas int32 `json:"replicas"`
	}
	if actionRequest.Spec.Parameters != nil && len(actionRequest.Spec.Parameters.Raw) > 0 {
		if err := json.Unmarshal(actionRequest.Spec.Parameters.Raw, &parameters); err != nil {
			return 0, fmt.Errorf("decode scale parameters: %w", err)
		}
	}
	if parameters.Replicas < 1 {
		return 0, fmt.Errorf("scale action requires parameters.replicas to be at least 1")
	}
	if parameters.Replicas > 9 {
		return 0, fmt.Errorf("scale action supports at most 9 replicas for the first Valkey release")
	}
	return parameters.Replicas, nil
}

func quotaHard(actionRequest *platformv1alpha1.ActionRequest) (corev1.ResourceList, error) {
	var parameters struct {
		CPU    string `json:"cpu"`
		Memory string `json:"memory"`
		Pods   string `json:"pods"`
	}
	if actionRequest.Spec.Parameters != nil && len(actionRequest.Spec.Parameters.Raw) > 0 {
		if err := json.Unmarshal(actionRequest.Spec.Parameters.Raw, &parameters); err != nil {
			return nil, fmt.Errorf("decode update-quota parameters: %w", err)
		}
	}
	hard := corev1.ResourceList{}
	if strings.TrimSpace(parameters.CPU) != "" {
		quantity, err := resource.ParseQuantity(parameters.CPU)
		if err != nil {
			return nil, fmt.Errorf("parameters.cpu is not a valid quantity: %w", err)
		}
		hard[corev1.ResourceRequestsCPU] = quantity
	}
	if strings.TrimSpace(parameters.Memory) != "" {
		quantity, err := resource.ParseQuantity(parameters.Memory)
		if err != nil {
			return nil, fmt.Errorf("parameters.memory is not a valid quantity: %w", err)
		}
		hard[corev1.ResourceRequestsMemory] = quantity
	}
	if strings.TrimSpace(parameters.Pods) != "" {
		quantity, err := resource.ParseQuantity(parameters.Pods)
		if err != nil {
			return nil, fmt.Errorf("parameters.pods is not a valid quantity: %w", err)
		}
		hard[corev1.ResourcePods] = quantity
	}
	if len(hard) == 0 {
		return nil, fmt.Errorf("update-quota action requires at least one of parameters.cpu, parameters.memory, or parameters.pods")
	}
	return hard, nil
}

type namespaceAccessParams struct {
	Subject string
	BaseURL string
	CAData  []byte
}

func (r *ActionRequestReconciler) namespaceAccessParameters(actionRequest *platformv1alpha1.ActionRequest) (namespaceAccessParams, error) {
	params := namespaceAccessParams{
		Subject: strings.TrimSpace(actionRequest.Spec.RequestedBy.Subject),
		CAData:  append([]byte(nil), r.NamespaceAccessCAData...),
	}
	requestURL := ""
	if actionRequest.Spec.Parameters != nil && len(actionRequest.Spec.Parameters.Raw) > 0 {
		raw := map[string]string{}
		if err := json.Unmarshal(actionRequest.Spec.Parameters.Raw, &raw); err != nil {
			return namespaceAccessParams{}, fmt.Errorf("decode grant-access parameters: %w", err)
		}
		if subject := strings.TrimSpace(raw["subject"]); subject != "" {
			params.Subject = subject
		}
		if defaultURL := strings.TrimSpace(raw["defaultUrl"]); defaultURL != "" {
			requestURL = defaultURL
		}
		if defaultURL := strings.TrimSpace(raw["default_url"]); defaultURL != "" {
			requestURL = defaultURL
		}
	}
	if params.Subject == "" {
		return namespaceAccessParams{}, fmt.Errorf("grant-access action requires parameters.subject or requestedBy.subject")
	}
	configuredURL := strings.TrimSpace(r.NamespaceAccessExternalURL)
	if r.ProductionMode {
		if configuredURL == "" {
			return namespaceAccessParams{}, fmt.Errorf("grant-access action requires controller external URL in production mode; set --external-url (or SERVICER_EXTERNAL_URL/SERVICER_AUTH_EXTERNAL_BASE_URL)")
		}
		normalized, err := normalizeNamespaceAccessBaseURL(configuredURL)
		if err != nil {
			return namespaceAccessParams{}, fmt.Errorf("invalid controller external URL for namespace access kubeconfig: %w", err)
		}
		params.BaseURL = normalized
		return params, nil
	}
	baseURL := configuredURL
	if baseURL == "" {
		baseURL = requestURL
	}
	if strings.TrimSpace(baseURL) == "" {
		return namespaceAccessParams{}, fmt.Errorf("grant-access action requires parameters.defaultUrl or controller external URL")
	}
	normalized, err := normalizeNamespaceAccessBaseURL(baseURL)
	if err != nil {
		return namespaceAccessParams{}, err
	}
	params.BaseURL = normalized
	return params, nil
}

func normalizeNamespaceAccessBaseURL(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", fmt.Errorf("external URL is required")
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("external URL must be a valid URL")
	}
	if !parsed.IsAbs() || strings.TrimSpace(parsed.Host) == "" {
		return "", fmt.Errorf("external URL must be an absolute HTTPS URL")
	}
	if !strings.EqualFold(parsed.Scheme, "https") {
		return "", fmt.Errorf("external URL must use https")
	}
	if parsed.User != nil {
		return "", fmt.Errorf("external URL must not include embedded credentials")
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("external URL must not include query string or fragment")
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	return strings.TrimRight(parsed.String(), "/"), nil
}

type natsStreamActionParams struct {
	Stream string `json:"stream"`
}

type natsConsumerActionParams struct {
	Stream   string `json:"stream"`
	Consumer string `json:"consumer"`
}

type natsRotateCredentialParams struct {
	CredentialName string `json:"credentialName"`
}

func natsStreamActionParameters(actionRequest *platformv1alpha1.ActionRequest) (natsStreamActionParams, error) {
	params := natsStreamActionParams{}
	if actionRequest.Spec.Parameters != nil && len(actionRequest.Spec.Parameters.Raw) > 0 {
		if err := json.Unmarshal(actionRequest.Spec.Parameters.Raw, &params); err != nil {
			return params, fmt.Errorf("decode NATS stream action parameters: %w", err)
		}
	}
	params.Stream = strings.TrimSpace(params.Stream)
	if params.Stream == "" {
		return params, fmt.Errorf("NATS stream action requires parameters.stream")
	}
	return params, nil
}

func natsConsumerActionParameters(actionRequest *platformv1alpha1.ActionRequest) (natsConsumerActionParams, error) {
	params := natsConsumerActionParams{}
	if actionRequest.Spec.Parameters != nil && len(actionRequest.Spec.Parameters.Raw) > 0 {
		if err := json.Unmarshal(actionRequest.Spec.Parameters.Raw, &params); err != nil {
			return params, fmt.Errorf("decode NATS consumer action parameters: %w", err)
		}
	}
	params.Stream = strings.TrimSpace(params.Stream)
	params.Consumer = strings.TrimSpace(params.Consumer)
	if params.Stream == "" || params.Consumer == "" {
		return params, fmt.Errorf("NATS consumer action requires parameters.stream and parameters.consumer")
	}
	return params, nil
}

func natsRotateCredentialParameters(actionRequest *platformv1alpha1.ActionRequest) (natsRotateCredentialParams, error) {
	params := natsRotateCredentialParams{}
	if actionRequest.Spec.Parameters != nil && len(actionRequest.Spec.Parameters.Raw) > 0 {
		if err := json.Unmarshal(actionRequest.Spec.Parameters.Raw, &params); err != nil {
			return params, fmt.Errorf("decode rotate-credentials parameters: %w", err)
		}
	}
	params.CredentialName = strings.TrimSpace(params.CredentialName)
	return params, nil
}

func (r *ActionRequestReconciler) ensureNamespaceAccess(ctx context.Context, serviceContext adapters.ServiceContext, namespace string, params namespaceAccessParams, actionName string) error {
	name := namespaceAccessResourceName(params.Subject)
	labels := namespaceAccessLabels(serviceContext)
	token, err := r.namespaceAccessToken(ctx, namespace, params.Subject)
	if err != nil {
		return err
	}

	serviceAccount := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    labels,
		},
	}
	if err := r.applyServiceAccount(ctx, serviceAccount); err != nil {
		return err
	}

	role := &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    labels,
		},
		Rules: []rbacv1.PolicyRule{
			{APIGroups: []string{""}, Resources: []string{"configmaps", "events", "limitranges", "persistentvolumeclaims", "pods", "pods/log", "resourcequotas", "services"}, Verbs: []string{"get", "list", "watch"}},
			{APIGroups: []string{"apps"}, Resources: []string{"daemonsets", "deployments", "replicasets", "statefulsets"}, Verbs: []string{"get", "list", "watch"}},
			{APIGroups: []string{"batch"}, Resources: []string{"cronjobs", "jobs"}, Verbs: []string{"get", "list", "watch"}},
			{APIGroups: []string{"networking.k8s.io"}, Resources: []string{"networkpolicies"}, Verbs: []string{"get", "list", "watch"}},
		},
	}
	if err := r.applyRole(ctx, role); err != nil {
		return err
	}

	roleBinding := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    labels,
		},
		Subjects: []rbacv1.Subject{{
			Kind:      rbacv1.ServiceAccountKind,
			Name:      name,
			Namespace: namespace,
		}},
		RoleRef: rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     "Role",
			Name:     name,
		},
	}
	if err := r.applyRoleBinding(ctx, roleBinding); err != nil {
		return err
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      namespaceAccessSecretName(params.Subject),
			Namespace: namespace,
			Labels:    labels,
			Annotations: map[string]string{
				"servicer.io/access-subject": params.Subject,
				"servicer.io/default-url":    params.BaseURL,
				"servicer.io/action-request": actionName,
			},
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"default_url": []byte(params.BaseURL),
			"namespace":   []byte(namespace),
			"subject":     []byte(params.Subject),
			"token":       []byte(token),
		},
	}
	kubeconfig, err := namespaceAccessKubeconfig(params, namespace, token)
	if err != nil {
		return err
	}
	secret.Data["kubeconfig"] = kubeconfig
	return r.applySecret(ctx, secret)
}

func (r *ActionRequestReconciler) namespaceAccessToken(ctx context.Context, namespace, subject string) (string, error) {
	var existing corev1.Secret
	err := r.Get(ctx, types.NamespacedName{Name: namespaceAccessSecretName(subject), Namespace: namespace}, &existing)
	if err == nil {
		if token := strings.TrimSpace(string(existing.Data["token"])); token != "" {
			return token, nil
		}
	}
	if err != nil && !apierrors.IsNotFound(err) {
		return "", err
	}
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}

func (r *ActionRequestReconciler) applyServiceAccount(ctx context.Context, desired *corev1.ServiceAccount) error {
	var existing corev1.ServiceAccount
	if err := r.Get(ctx, types.NamespacedName{Name: desired.Name, Namespace: desired.Namespace}, &existing); err != nil {
		if apierrors.IsNotFound(err) {
			return r.Create(ctx, desired)
		}
		return err
	}
	existing.Labels = desired.Labels
	return r.Update(ctx, &existing)
}

func (r *ActionRequestReconciler) applyRole(ctx context.Context, desired *rbacv1.Role) error {
	var existing rbacv1.Role
	if err := r.Get(ctx, types.NamespacedName{Name: desired.Name, Namespace: desired.Namespace}, &existing); err != nil {
		if apierrors.IsNotFound(err) {
			return r.Create(ctx, desired)
		}
		return err
	}
	existing.Labels = desired.Labels
	existing.Rules = desired.Rules
	return r.Update(ctx, &existing)
}

func (r *ActionRequestReconciler) applyRoleBinding(ctx context.Context, desired *rbacv1.RoleBinding) error {
	var existing rbacv1.RoleBinding
	if err := r.Get(ctx, types.NamespacedName{Name: desired.Name, Namespace: desired.Namespace}, &existing); err != nil {
		if apierrors.IsNotFound(err) {
			return r.Create(ctx, desired)
		}
		return err
	}
	existing.Labels = desired.Labels
	existing.Subjects = desired.Subjects
	existing.RoleRef = desired.RoleRef
	return r.Update(ctx, &existing)
}

func (r *ActionRequestReconciler) applySecret(ctx context.Context, desired *corev1.Secret) error {
	var existing corev1.Secret
	if err := r.Get(ctx, types.NamespacedName{Name: desired.Name, Namespace: desired.Namespace}, &existing); err != nil {
		if apierrors.IsNotFound(err) {
			return r.Create(ctx, desired)
		}
		return err
	}
	existing.Labels = desired.Labels
	existing.Annotations = desired.Annotations
	existing.Type = desired.Type
	existing.Data = desired.Data
	return r.Update(ctx, &existing)
}

func namespaceAccessLabels(serviceContext adapters.ServiceContext) map[string]string {
	labels := map[string]string{
		"servicer.io/managed-by": "servicer",
		"servicer.io/purpose":    "namespace-access",
	}
	if serviceContext.Instance != nil {
		labels["servicer.io/service-instance"] = serviceContext.Instance.Name
	}
	if serviceContext.Project != nil {
		labels["servicer.io/project"] = serviceContext.Project.Name
	}
	if serviceContext.Tenant != nil {
		labels["servicer.io/tenant"] = serviceContext.Tenant.Name
	}
	return labels
}

func namespaceAccessResourceName(subject string) string {
	return "servicer-access-" + safeKubernetesName(subject, 36)
}

func namespaceAccessSecretName(subject string) string {
	return namespaceAccessResourceName(subject) + "-kubeconfig"
}

func safeKubernetesName(value string, maxLength int) string {
	lower := strings.ToLower(strings.TrimSpace(value))
	var builder strings.Builder
	lastDash := false
	for _, char := range lower {
		valid := (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9')
		if valid {
			builder.WriteRune(char)
			lastDash = false
			continue
		}
		if !lastDash {
			builder.WriteByte('-')
			lastDash = true
		}
	}
	name := strings.Trim(builder.String(), "-")
	if name == "" {
		name = "user"
	}
	if maxLength > 0 && len(name) > maxLength {
		name = strings.Trim(name[:maxLength], "-")
	}
	return name
}

func namespaceAccessKubeconfig(params namespaceAccessParams, namespace, token string) ([]byte, error) {
	server := fmt.Sprintf("%s/api/kubernetes/namespaces/%s", strings.TrimRight(params.BaseURL, "/"), namespace)
	config := clientcmdapi.NewConfig()
	const clusterName = "servicer-platform"
	userName := namespaceAccessKubeconfigUserName(params.Subject)
	contextName := namespaceAccessKubeconfigContextName(namespace, params.Subject)

	config.Clusters[clusterName] = &clientcmdapi.Cluster{
		Server: server,
	}
	if len(params.CAData) > 0 {
		config.Clusters[clusterName].CertificateAuthorityData = append([]byte(nil), params.CAData...)
	}
	config.AuthInfos[userName] = &clientcmdapi.AuthInfo{Token: token}
	config.Contexts[contextName] = &clientcmdapi.Context{
		Cluster:   clusterName,
		AuthInfo:  userName,
		Namespace: namespace,
	}
	config.CurrentContext = contextName
	return clientcmd.Write(*config)
}

func namespaceAccessKubeconfigUserName(subject string) string {
	base := safeKubernetesName(subject, 32)
	return fmt.Sprintf("servicer-%s-%s", base, shortStableHash(subject, 8))
}

func namespaceAccessKubeconfigContextName(namespace, subject string) string {
	return fmt.Sprintf("ns-%s-%s", safeKubernetesName(namespace, 28), shortStableHash(subject, 8))
}

func shortStableHash(value string, size int) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(value)))
	encoded := hex.EncodeToString(sum[:])
	if size <= 0 || size >= len(encoded) {
		return encoded
	}
	return encoded[:size]
}

func failoverCandidate(actionRequest *platformv1alpha1.ActionRequest) (string, error) {
	var parameters struct {
		CandidateCluster string `json:"candidateCluster"`
	}
	if actionRequest.Spec.Parameters != nil && len(actionRequest.Spec.Parameters.Raw) > 0 {
		if err := json.Unmarshal(actionRequest.Spec.Parameters.Raw, &parameters); err != nil {
			return "", fmt.Errorf("decode failover parameters: %w", err)
		}
	}
	if strings.TrimSpace(parameters.CandidateCluster) == "" {
		return "", fmt.Errorf("failover action requires parameters.candidateCluster")
	}
	return strings.TrimSpace(parameters.CandidateCluster), nil
}

func rollbackTarget(actionRequest *platformv1alpha1.ActionRequest, instance *platformv1alpha1.ServiceInstance) (string, error) {
	var parameters struct {
		TargetPrimary string `json:"targetPrimary"`
	}
	if actionRequest.Spec.Parameters != nil && len(actionRequest.Spec.Parameters.Raw) > 0 {
		if err := json.Unmarshal(actionRequest.Spec.Parameters.Raw, &parameters); err != nil {
			return "", fmt.Errorf("decode rollback parameters: %w", err)
		}
	}
	if strings.TrimSpace(parameters.TargetPrimary) != "" {
		return strings.TrimSpace(parameters.TargetPrimary), nil
	}
	for _, standby := range instance.Status.CacheTopology.StandbyClusters {
		if standby.ResyncRequired {
			return standby.ClusterName, nil
		}
	}
	return "", fmt.Errorf("rollback-failover action requires parameters.targetPrimary when no resync-required previous primary is recorded")
}

func standbyTarget(actionRequest *platformv1alpha1.ActionRequest) (string, error) {
	var parameters struct {
		StandbyCluster string `json:"standbyCluster"`
	}
	if actionRequest.Spec.Parameters != nil && len(actionRequest.Spec.Parameters.Raw) > 0 {
		if err := json.Unmarshal(actionRequest.Spec.Parameters.Raw, &parameters); err != nil {
			return "", fmt.Errorf("decode resync parameters: %w", err)
		}
	}
	if strings.TrimSpace(parameters.StandbyCluster) == "" {
		return "", fmt.Errorf("resync-standby action requires parameters.standbyCluster")
	}
	return strings.TrimSpace(parameters.StandbyCluster), nil
}

func applyValkeyFailoverPreflight(instance *platformv1alpha1.ServiceInstance, candidateCluster string) error {
	if instance.Status.CacheTopology.Mode != "multi-cluster-failover" {
		return fmt.Errorf("Valkey failover is only available for multi-cluster-failover topology")
	}
	if instance.Status.CacheTopology.PrimaryCluster == "" {
		return fmt.Errorf("current primary cluster is unknown")
	}
	if candidateCluster == instance.Status.CacheTopology.PrimaryCluster {
		return fmt.Errorf("candidate cluster %q is already primary", candidateCluster)
	}
	for _, standby := range instance.Status.CacheTopology.StandbyClusters {
		if standby.ClusterName != candidateCluster {
			continue
		}
		if !standby.Ready {
			return fmt.Errorf("candidate standby %q is not promotion-ready: %s", candidateCluster, standby.Message)
		}
		return nil
	}
	return fmt.Errorf("candidate cluster %q is not a known standby", candidateCluster)
}

func promoteValkeyPrimary(instance *platformv1alpha1.ServiceInstance, candidateCluster, messageFormat string) string {
	previousPrimary := instance.Status.CacheTopology.PrimaryCluster
	instance.Status.CacheTopology.PrimaryCluster = candidateCluster
	instance.Status.CacheTopology.FailoverReadiness = "Promoted"
	instance.Status.CacheTopology.Message = fmt.Sprintf(messageFormat, candidateCluster, previousPrimary)

	foundPreviousPrimary := false
	for i := range instance.Status.CacheTopology.StandbyClusters {
		standby := &instance.Status.CacheTopology.StandbyClusters[i]
		switch standby.ClusterName {
		case candidateCluster:
			standby.ClusterName = previousPrimary
			standby.Ready = false
			standby.ResyncRequired = true
			standby.LagObserved = false
			standby.ReplicationLagSeconds = 0
			standby.Message = "Previous primary requires resynchronization before it can be promoted again."
		case previousPrimary:
			foundPreviousPrimary = true
		}
	}
	if !foundPreviousPrimary && previousPrimary != "" {
		instance.Status.CacheTopology.StandbyClusters = append(instance.Status.CacheTopology.StandbyClusters, platformv1alpha1.CacheStandbyStatus{
			ClusterName:    previousPrimary,
			Ready:          false,
			ResyncRequired: true,
			LagObserved:    false,
			Message:        "Previous primary requires resynchronization before it can be promoted again.",
		})
	}
	return previousPrimary
}

func markValkeyStandbyResyncRequested(instance *platformv1alpha1.ServiceInstance, standbyCluster, requestName string) error {
	if instance.Status.CacheTopology.Mode != "multi-cluster-failover" {
		return fmt.Errorf("Valkey standby resync is only available for multi-cluster-failover topology")
	}
	if standbyCluster == instance.Status.CacheTopology.PrimaryCluster {
		return fmt.Errorf("primary cluster %q cannot be resynced as a standby", standbyCluster)
	}
	for i := range instance.Status.CacheTopology.StandbyClusters {
		standby := &instance.Status.CacheTopology.StandbyClusters[i]
		if standby.ClusterName != standbyCluster {
			continue
		}
		standby.Ready = false
		standby.ResyncRequired = true
		standby.LagObserved = false
		standby.ReplicationLagSeconds = 0
		standby.Message = fmt.Sprintf("Resynchronization requested by ActionRequest %q.", requestName)
		instance.Status.CacheTopology.FailoverReadiness = "Resyncing"
		instance.Status.CacheTopology.Message = fmt.Sprintf("Standby cluster %q is being resynchronized from primary %q.", standbyCluster, instance.Status.CacheTopology.PrimaryCluster)
		return nil
	}
	return fmt.Errorf("standby cluster %q is not known", standbyCluster)
}
