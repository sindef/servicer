package controllers

import (
	"context"
	"encoding/json"
	"testing"

	platformv1alpha1 "github.com/sindef/servicer/api/v1alpha1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestDesiredVirtualMachineClaimInstanceMapsClaimToKubeVirtService(t *testing.T) {
	claim := &platformv1alpha1.VirtualMachineClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "devbox"},
		Spec: platformv1alpha1.VirtualMachineClaimSpec{
			ProjectRef: platformv1alpha1.LocalObjectReference{Name: "acme-prod"},
			Class:      "dev",
			Image:      "quay.io/containerdisks/ubuntu:22.04",
			PowerState: "running",
			Parameters: controllerRawJSON(t, map[string]any{
				"cpu":               "2",
				"memory":            "4Gi",
				"sshAuthorizedKeys": []string{"ssh-ed25519 AAAATEST dev@example.com"},
			}),
			Exposure:       platformv1alpha1.ExposureSpec{Mode: platformv1alpha1.ExposureModeClusterInternal},
			SecretPolicy:   platformv1alpha1.SecretPolicySpec{DeliveryMode: platformv1alpha1.SecretDeliveryModeManual},
			DeletionPolicy: platformv1alpha1.DeletionPolicyDelete,
		},
	}

	instance := desiredVirtualMachineClaimInstance(claim)
	if instance.Spec.ServiceClassRef.Name != "virtual-machine" {
		t.Fatalf("expected virtual-machine service class, got %q", instance.Spec.ServiceClassRef.Name)
	}
	if instance.Spec.ServicePlanRef.Name != "virtual-machine-dev" {
		t.Fatalf("expected dev VM plan, got %q", instance.Spec.ServicePlanRef.Name)
	}

	parameters := map[string]any{}
	if err := json.Unmarshal(instance.Spec.Parameters.Raw, &parameters); err != nil {
		t.Fatalf("decode parameters: %v", err)
	}
	if parameters["image"] != "quay.io/containerdisks/ubuntu:22.04" {
		t.Fatalf("expected image parameter to be copied, got %#v", parameters["image"])
	}
	if parameters["runStrategy"] != "Always" {
		t.Fatalf("expected running claim to map to Always, got %#v", parameters["runStrategy"])
	}
	if parameters["cpu"] != "2" || parameters["memory"] != "4Gi" {
		t.Fatalf("expected sizing parameters to be preserved, got %#v", parameters)
	}
}

func TestDesiredVirtualMachineClaimInstanceMapsStoppedPowerState(t *testing.T) {
	claim := &platformv1alpha1.VirtualMachineClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "devbox"},
		Spec: platformv1alpha1.VirtualMachineClaimSpec{
			ProjectRef:     platformv1alpha1.LocalObjectReference{Name: "acme-prod"},
			Class:          "virtual-machine-dev",
			Image:          "quay.io/containerdisks/fedora:39",
			PowerState:     "stopped",
			Exposure:       platformv1alpha1.ExposureSpec{Mode: platformv1alpha1.ExposureModeClusterInternal},
			SecretPolicy:   platformv1alpha1.SecretPolicySpec{DeliveryMode: platformv1alpha1.SecretDeliveryModeManual},
			DeletionPolicy: platformv1alpha1.DeletionPolicyDelete,
		},
	}

	instance := desiredVirtualMachineClaimInstance(claim)
	parameters := map[string]any{}
	if err := json.Unmarshal(instance.Spec.Parameters.Raw, &parameters); err != nil {
		t.Fatalf("decode parameters: %v", err)
	}
	if parameters["runStrategy"] != "Halted" {
		t.Fatalf("expected stopped claim to map to Halted, got %#v", parameters["runStrategy"])
	}
}

func controllerRawJSON(t *testing.T, value any) *apiextensionsv1.JSON {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal json: %v", err)
	}
	return &apiextensionsv1.JSON{Raw: raw}
}

func TestVirtualMachineClaimReconcilerPropagatesBackingInstanceStatus(t *testing.T) {
	scheme := inventoryTestScheme(t)

	project := &platformv1alpha1.Project{
		ObjectMeta: metav1.ObjectMeta{Name: "acme-prod"},
	}
	claim := &platformv1alpha1.VirtualMachineClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "devbox", Generation: 3},
		Spec: platformv1alpha1.VirtualMachineClaimSpec{
			ProjectRef: platformv1alpha1.LocalObjectReference{Name: "acme-prod"},
			Class:      "virtual-machine-dev",
			Image:      "quay.io/containerdisks/ubuntu:22.04",
			Exposure:   platformv1alpha1.ExposureSpec{Mode: platformv1alpha1.ExposureModeClusterInternal},
			SecretPolicy: platformv1alpha1.SecretPolicySpec{
				DeliveryMode: platformv1alpha1.SecretDeliveryModeManual,
			},
			DeletionPolicy: platformv1alpha1.DeletionPolicyDelete,
		},
	}
	backing := &platformv1alpha1.ServiceInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "devbox"},
		Status: platformv1alpha1.ServiceInstanceStatus{
			Phase: "PendingDependencies",
			Placement: platformv1alpha1.PlacementStatus{
				ClusterName: "remote-dev",
			},
			Runtime: platformv1alpha1.RuntimeStatus{
				Driver: "kubevirt",
				ObjectRef: &platformv1alpha1.TypedObjectReference{
					APIVersion: "kubevirt.io/v1",
					Kind:       "VirtualMachine",
					Name:       "devbox",
					Namespace:  "acme-prod",
				},
			},
			Sync: platformv1alpha1.DeliverySyncStatus{
				Phase:   "Blocked",
				Message: "KubeVirt dependency missing on target cluster: CRD \"virtualmachines.kubevirt.io\" is not installed.",
			},
			Conditions: []metav1.Condition{
				{
					Type:    "Ready",
					Status:  metav1.ConditionFalse,
					Reason:  "RuntimeDependencyMissing",
					Message: "Service instance is waiting for KubeVirt runtime dependencies.",
				},
			},
		},
	}

	reconciler := &VirtualMachineClaimReconciler{
		Client: fake.NewClientBuilder().
			WithScheme(scheme).
			WithStatusSubresource(&platformv1alpha1.VirtualMachineClaim{}).
			WithObjects(project, claim, backing).
			Build(),
		Scheme: scheme,
	}

	if _, err := reconciler.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKey{Name: "devbox"}}); err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	var updated platformv1alpha1.VirtualMachineClaim
	if err := reconciler.Get(context.Background(), client.ObjectKey{Name: "devbox"}, &updated); err != nil {
		t.Fatalf("Get returned error: %v", err)
	}

	if updated.Status.Phase != "PendingDependencies" {
		t.Fatalf("expected phase PendingDependencies, got %q", updated.Status.Phase)
	}
	if updated.Status.Placement.ClusterName != "remote-dev" {
		t.Fatalf("expected cluster placement to propagate, got %#v", updated.Status.Placement)
	}
	if updated.Status.Runtime.Driver != "kubevirt" || updated.Status.Runtime.ObjectRef == nil {
		t.Fatalf("expected runtime metadata to propagate, got %#v", updated.Status.Runtime)
	}
	if updated.Status.Sync.Message != backing.Status.Sync.Message {
		t.Fatalf("expected sync message to propagate, got %q", updated.Status.Sync.Message)
	}
	ready := apimeta.FindStatusCondition(updated.Status.Conditions, "Ready")
	if ready == nil || ready.Reason != "RuntimeDependencyMissing" {
		t.Fatalf("expected runtime dependency Ready condition to be preserved, got %#v", updated.Status.Conditions)
	}
	accepted := apimeta.FindStatusCondition(updated.Status.Conditions, "Accepted")
	if accepted == nil || accepted.Status != metav1.ConditionTrue || accepted.Reason != "BackedByServiceInstance" {
		t.Fatalf("expected Accepted condition to be set by claim reconciler, got %#v", updated.Status.Conditions)
	}
}
