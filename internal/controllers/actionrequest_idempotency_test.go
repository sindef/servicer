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
