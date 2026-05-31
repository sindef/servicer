package controllers

import (
	"context"
	"fmt"
	"testing"

	platformv1alpha1 "github.com/sindef/servicer/api/v1alpha1"
	"github.com/sindef/servicer/internal/adapters"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestActionRequestReconcilerDoesNotReexecuteRestartAfterStatusConflict(t *testing.T) {
	reconciler := newValkeyActionRequestReconciler(t, valkeyActionRequestFixture("session-cache-restart-conflict", string(adapters.ActionRestart), nil))
	baseClient := reconciler.Client
	reconciler.Client = &conflictOnStatusPatchClient{Client: baseClient, failPatchCall: 2, failAllPatchesAfter: true}

	_, err := reconciler.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKey{Name: "session-cache-restart-conflict"}})
	if err == nil || !apierrors.IsConflict(err) {
		t.Fatalf("expected status conflict error on completion patch, got %v", err)
	}

	var statefulSet appsv1.StatefulSet
	if err := reconciler.Get(context.Background(), client.ObjectKey{Name: "session-cache", Namespace: "acme-prod-session-cache"}, &statefulSet); err != nil {
		t.Fatalf("Get StatefulSet returned error: %v", err)
	}
	firstRestartAt := statefulSet.Spec.Template.Annotations["servicer.io/restarted-at"]
	firstOperationID := statefulSet.Spec.Template.Annotations["servicer.io/restart-operation-id"]
	if firstRestartAt == "" || firstOperationID == "" {
		t.Fatalf("expected restart annotations to be present after first reconcile, got %#v", statefulSet.Spec.Template.Annotations)
	}

	// Restore the base client to simulate the next reconcile after a transient status persistence failure.
	reconciler.Client = baseClient
	if _, err := reconciler.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKey{Name: "session-cache-restart-conflict"}}); err != nil {
		t.Fatalf("second reconcile returned error: %v", err)
	}

	if err := reconciler.Get(context.Background(), client.ObjectKey{Name: "session-cache", Namespace: "acme-prod-session-cache"}, &statefulSet); err != nil {
		t.Fatalf("Get StatefulSet returned error: %v", err)
	}
	if statefulSet.Spec.Template.Annotations["servicer.io/restarted-at"] != firstRestartAt {
		t.Fatalf("expected restart timestamp to remain unchanged across replay, got %#v", statefulSet.Spec.Template.Annotations)
	}
	if statefulSet.Spec.Template.Annotations["servicer.io/restart-operation-id"] != firstOperationID {
		t.Fatalf("expected restart operation id to remain unchanged across replay, got %#v", statefulSet.Spec.Template.Annotations)
	}

	var actionRequest platformv1alpha1.ActionRequest
	if err := reconciler.Get(context.Background(), client.ObjectKey{Name: "session-cache-restart-conflict"}, &actionRequest); err != nil {
		t.Fatalf("Get ActionRequest returned error: %v", err)
	}
	if actionRequest.Status.Phase != "Succeeded" {
		t.Fatalf("expected action to complete after retry, got phase %q", actionRequest.Status.Phase)
	}
}

func TestActionRequestReconcilerScaleIsIdempotentWhenReplayed(t *testing.T) {
	reconciler := newValkeyActionRequestReconciler(t, valkeyActionRequestFixture("session-cache-scale-idempotent", string(adapters.ActionScale), map[string]any{"replicas": 5}))
	key := client.ObjectKey{Name: "session-cache-scale-idempotent"}

	if _, err := reconciler.Reconcile(context.Background(), ctrl.Request{NamespacedName: key}); err != nil {
		t.Fatalf("first reconcile returned error: %v", err)
	}

	var statefulSet appsv1.StatefulSet
	if err := reconciler.Get(context.Background(), client.ObjectKey{Name: "session-cache", Namespace: "acme-prod-session-cache"}, &statefulSet); err != nil {
		t.Fatalf("Get StatefulSet returned error: %v", err)
	}
	beforeResourceVersion := statefulSet.ResourceVersion

	var actionRequest platformv1alpha1.ActionRequest
	if err := reconciler.Get(context.Background(), key, &actionRequest); err != nil {
		t.Fatalf("Get ActionRequest returned error: %v", err)
	}
	actionRequest.Status.Phase = "Running"
	actionRequest.Status.OperationState = actionOperationStatePrepared
	actionRequest.Status.CompletedAt = nil
	if err := reconciler.Status().Update(context.Background(), &actionRequest); err != nil {
		t.Fatalf("Status.Update returned error: %v", err)
	}

	if _, err := reconciler.Reconcile(context.Background(), ctrl.Request{NamespacedName: key}); err != nil {
		t.Fatalf("second reconcile returned error: %v", err)
	}

	if err := reconciler.Get(context.Background(), client.ObjectKey{Name: "session-cache", Namespace: "acme-prod-session-cache"}, &statefulSet); err != nil {
		t.Fatalf("Get StatefulSet returned error: %v", err)
	}
	if statefulSet.Spec.Replicas == nil || *statefulSet.Spec.Replicas != 5 {
		t.Fatalf("expected replicas to remain at 5 after replay, got %#v", statefulSet.Spec.Replicas)
	}
	if statefulSet.ResourceVersion != beforeResourceVersion {
		t.Fatalf("expected replay to avoid additional StatefulSet writes; resourceVersion changed from %q to %q", beforeResourceVersion, statefulSet.ResourceVersion)
	}
}

func TestActionRequestReconcilerRotateIsIdempotentWhenReplayed(t *testing.T) {
	reconciler := newValkeyActionRequestReconciler(t, valkeyActionRequestFixture("session-cache-rotate-idempotent", string(adapters.ActionRotateCredentials), nil))
	key := client.ObjectKey{Name: "session-cache-rotate-idempotent"}

	if _, err := reconciler.Reconcile(context.Background(), ctrl.Request{NamespacedName: key}); err != nil {
		t.Fatalf("first reconcile returned error: %v", err)
	}

	var actionRequest platformv1alpha1.ActionRequest
	if err := reconciler.Get(context.Background(), key, &actionRequest); err != nil {
		t.Fatalf("Get ActionRequest returned error: %v", err)
	}
	operationID := actionRequest.Status.OperationID
	if operationID == "" {
		t.Fatalf("expected operation ID to be persisted")
	}

	var secret corev1.Secret
	if err := reconciler.Get(context.Background(), client.ObjectKey{Name: "session-cache-auth", Namespace: "acme-prod-session-cache"}, &secret); err != nil {
		t.Fatalf("Get Secret returned error: %v", err)
	}
	beforePassword := string(secret.Data["password"])
	beforeRotatedAt := secret.Annotations["servicer.io/rotated-at"]

	actionRequest.Status.Phase = "Running"
	actionRequest.Status.OperationState = actionOperationStatePrepared
	actionRequest.Status.CompletedAt = nil
	if err := reconciler.Status().Update(context.Background(), &actionRequest); err != nil {
		t.Fatalf("Status.Update returned error: %v", err)
	}

	if _, err := reconciler.Reconcile(context.Background(), ctrl.Request{NamespacedName: key}); err != nil {
		t.Fatalf("second reconcile returned error: %v", err)
	}

	if err := reconciler.Get(context.Background(), client.ObjectKey{Name: "session-cache-auth", Namespace: "acme-prod-session-cache"}, &secret); err != nil {
		t.Fatalf("Get Secret returned error: %v", err)
	}
	if string(secret.Data["password"]) != beforePassword {
		t.Fatalf("expected password to remain unchanged across replay")
	}
	if secret.Annotations["servicer.io/rotated-at"] != beforeRotatedAt {
		t.Fatalf("expected rotated-at annotation to remain unchanged across replay")
	}
	if secret.Annotations["servicer.io/rotation-operation-id"] != operationID {
		t.Fatalf("expected rotation-operation-id %q, got %q", operationID, secret.Annotations["servicer.io/rotation-operation-id"])
	}
}

func TestActionRequestReconcilerDoesNotFailValkeyFailoverAfterStatusConflict(t *testing.T) {
	action := valkeyActionRequestFixture("session-cache-failover-conflict", string(adapters.ActionFailover), map[string]any{"candidateCluster": "west-2"})
	action.Spec.Approval.Mode = platformv1alpha1.ApprovalModeApproved
	action.Spec.Approval.ApprovedBy = []string{"manager@example.com"}
	reconciler := newValkeyActionRequestReconciler(t, action)
	baseClient := reconciler.Client
	reconciler.Client = &conflictOnStatusPatchClient{Client: baseClient, failPatchCall: 3, failAllPatchesAfter: true}

	_, err := reconciler.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKey{Name: action.Name}})
	if err == nil || !apierrors.IsConflict(err) {
		t.Fatalf("expected status conflict error on completion patch, got %v", err)
	}

	var instance platformv1alpha1.ServiceInstance
	if err := reconciler.Get(context.Background(), client.ObjectKey{Name: "session-cache"}, &instance); err != nil {
		t.Fatalf("Get ServiceInstance returned error: %v", err)
	}
	if instance.Status.CacheTopology.PrimaryCluster != "west-2" {
		t.Fatalf("expected failover side effect before conflict, got %#v", instance.Status.CacheTopology)
	}

	reconciler.Client = baseClient
	if _, err := reconciler.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKey{Name: action.Name}}); err != nil {
		t.Fatalf("second reconcile returned error: %v", err)
	}

	var request platformv1alpha1.ActionRequest
	if err := reconciler.Get(context.Background(), client.ObjectKey{Name: action.Name}, &request); err != nil {
		t.Fatalf("Get ActionRequest returned error: %v", err)
	}
	if request.Status.Phase != "Succeeded" {
		t.Fatalf("expected action to succeed after replay, got %q", request.Status.Phase)
	}
	if request.Status.OperationState != actionOperationStateComplete {
		t.Fatalf("expected complete operation state after replay, got %q", request.Status.OperationState)
	}
}

func TestActionRequestReconcilerGrantAccessReplayKeepsIssuedToken(t *testing.T) {
	action := namespaceGrantAccessActionRequestFixture("team-space-access-conflict", map[string]any{
		"subject":    "bob@example.com",
		"defaultUrl": "https://servicer.example.com",
	})
	reconciler := newNamespaceActionRequestReconciler(t, action)
	baseClient := reconciler.Client
	reconciler.Client = &conflictOnStatusPatchClient{Client: baseClient, failPatchCall: 2, failAllPatchesAfter: true}

	_, err := reconciler.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKey{Name: action.Name}})
	if err == nil || !apierrors.IsConflict(err) {
		t.Fatalf("expected status conflict error on completion patch, got %v", err)
	}

	secretName := namespaceAccessSecretName("bob@example.com")
	var firstSecret corev1.Secret
	if err := reconciler.Get(context.Background(), client.ObjectKey{Name: secretName, Namespace: "acme-prod-team-space"}, &firstSecret); err != nil {
		t.Fatalf("Get first kubeconfig Secret returned error: %v", err)
	}
	firstToken := string(firstSecret.Data["token"])
	if firstToken == "" {
		t.Fatalf("expected issued token in first reconcile")
	}

	reconciler.Client = baseClient
	if _, err := reconciler.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKey{Name: action.Name}}); err != nil {
		t.Fatalf("second reconcile returned error: %v", err)
	}

	var secondSecret corev1.Secret
	if err := reconciler.Get(context.Background(), client.ObjectKey{Name: secretName, Namespace: "acme-prod-team-space"}, &secondSecret); err != nil {
		t.Fatalf("Get second kubeconfig Secret returned error: %v", err)
	}
	if got := string(secondSecret.Data["token"]); got != firstToken {
		t.Fatalf("expected stable namespace access token across replay, got %q then %q", firstToken, got)
	}

	var request platformv1alpha1.ActionRequest
	if err := reconciler.Get(context.Background(), client.ObjectKey{Name: action.Name}, &request); err != nil {
		t.Fatalf("Get ActionRequest returned error: %v", err)
	}
	if request.Status.Phase != "Succeeded" {
		t.Fatalf("expected access action to succeed after replay, got %q", request.Status.Phase)
	}
}

func TestActionRequestReconcilerLoadsRuntimeStatefulSetForMySQLRotate(t *testing.T) {
	actionRequest := valkeyActionRequestFixture("orders-mysql-rotate", string(adapters.ActionRotateCredentials), nil)
	actionRequest.Spec.TargetRef.Name = "orders-mysql"
	reconciler := newMySQLActionRequestReconciler(t, mysqlServiceInstanceFixture(), actionRequest)

	if _, err := reconciler.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKey{Name: "orders-mysql-rotate"}}); err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	var statefulSet appsv1.StatefulSet
	if err := reconciler.Get(context.Background(), client.ObjectKey{Name: "orders-mysql", Namespace: "acme-prod-orders-mysql"}, &statefulSet); err != nil {
		t.Fatalf("Get StatefulSet returned error: %v", err)
	}
	if statefulSet.Spec.Template.Annotations["servicer.io/credential-rotation-operation-id"] == "" {
		t.Fatalf("expected MySQL StatefulSet rollout annotation to be written, got %#v", statefulSet.Spec.Template.Annotations)
	}
}

type conflictOnStatusPatchClient struct {
	client.Client
	patchCalls          int
	failPatchCall       int
	failAllPatchesAfter bool
}

func (c *conflictOnStatusPatchClient) Status() client.SubResourceWriter {
	return &conflictOnStatusPatchWriter{
		SubResourceWriter: c.Client.Status(),
		parent:            c,
	}
}

type conflictOnStatusPatchWriter struct {
	client.SubResourceWriter
	parent *conflictOnStatusPatchClient
}

func (w *conflictOnStatusPatchWriter) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.SubResourcePatchOption) error {
	w.parent.patchCalls++
	if w.parent.patchCalls == w.parent.failPatchCall || (w.parent.failAllPatchesAfter && w.parent.patchCalls >= w.parent.failPatchCall) {
		return apierrors.NewConflict(
			schema.GroupResource{Group: platformv1alpha1.GroupVersion.Group, Resource: "actionrequests"},
			obj.GetName(),
			fmt.Errorf("simulated status conflict"),
		)
	}
	return w.SubResourceWriter.Patch(ctx, obj, patch, opts...)
}
