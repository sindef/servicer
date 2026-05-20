package controllers

import (
	"encoding/json"
	"testing"

	platformv1alpha1 "github.com/sindef/servicer/api/v1alpha1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
