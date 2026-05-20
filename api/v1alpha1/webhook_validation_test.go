package v1alpha1

import (
	"context"
	"testing"
)

func TestServiceInstanceDefaulting(t *testing.T) {
	instance := &ServiceInstance{}
	if err := instance.Default(context.Background(), instance); err != nil {
		t.Fatalf("default failed: %v", err)
	}

	if instance.Spec.Exposure.Mode != ExposureModeClusterInternal {
		t.Fatalf("expected default exposure mode %q, got %q", ExposureModeClusterInternal, instance.Spec.Exposure.Mode)
	}
	if instance.Spec.SecretPolicy.DeliveryMode != SecretDeliveryModeExternalSecret {
		t.Fatalf("expected default secret delivery %q, got %q", SecretDeliveryModeExternalSecret, instance.Spec.SecretPolicy.DeliveryMode)
	}
	if instance.Spec.SecretPolicy.ExternalSecretProvider != ExternalSecretProviderKubernetes {
		t.Fatalf("expected default external secret provider %q, got %q", ExternalSecretProviderKubernetes, instance.Spec.SecretPolicy.ExternalSecretProvider)
	}
	if instance.Spec.DeletionPolicy != DeletionPolicyDelete {
		t.Fatalf("expected default deletion policy %q, got %q", DeletionPolicyDelete, instance.Spec.DeletionPolicy)
	}
}

func TestServiceBindingRejectsUnsupportedSourceKind(t *testing.T) {
	binding := &ServiceBinding{
		Spec: ServiceBindingSpec{
			ProjectRef: LocalObjectReference{Name: "demo"},
			SourceRef: TypedObjectReference{
				APIVersion: GroupVersion.String(),
				Kind:       "Secret",
				Name:       "db",
			},
			TargetRef: TypedObjectReference{
				APIVersion: "v1",
				Kind:       "Namespace",
				Name:       "app",
			},
			SecretPolicy: SecretPolicySpec{DeliveryMode: SecretDeliveryModeExternalSecret},
		},
	}

	if _, err := binding.ValidateCreate(context.Background(), binding); err == nil {
		t.Fatal("expected validation error for unsupported source kind")
	}
}

func TestNamespaceClaimRejectsProjectRefMutation(t *testing.T) {
	oldClaim := &NamespaceClaim{
		Spec: NamespaceClaimSpec{
			ProjectRef:     LocalObjectReference{Name: "demo"},
			DeletionPolicy: DeletionPolicyDelete,
		},
	}
	newClaim := &NamespaceClaim{
		Spec: NamespaceClaimSpec{
			ProjectRef:     LocalObjectReference{Name: "other"},
			DeletionPolicy: DeletionPolicyDelete,
		},
	}

	if _, err := newClaim.ValidateUpdate(context.Background(), oldClaim, newClaim); err == nil {
		t.Fatal("expected validation error for immutable projectRef")
	}
}

func TestVirtualMachineClaimDefaultsAndValidatesPowerState(t *testing.T) {
	claim := &VirtualMachineClaim{}
	if err := claim.Default(context.Background(), claim); err != nil {
		t.Fatalf("default failed: %v", err)
	}
	if claim.Spec.PowerState != defaultVMPowerState {
		t.Fatalf("expected default power state %q, got %q", defaultVMPowerState, claim.Spec.PowerState)
	}

	claim = &VirtualMachineClaim{
		Spec: VirtualMachineClaimSpec{
			ProjectRef:     LocalObjectReference{Name: "demo"},
			Class:          "general-purpose",
			Image:          "ubuntu-24-04",
			Exposure:       ExposureSpec{Mode: ExposureModeClusterInternal},
			SecretPolicy:   SecretPolicySpec{DeliveryMode: SecretDeliveryModeExternalSecret},
			DeletionPolicy: DeletionPolicyDelete,
			PowerState:     "hibernate",
		},
	}
	if _, err := claim.ValidateCreate(context.Background(), claim); err == nil {
		t.Fatal("expected validation error for unsupported powerState")
	}
}

func TestServiceBindingRejectsVaultProviderWithoutConfig(t *testing.T) {
	binding := &ServiceBinding{
		Spec: ServiceBindingSpec{
			ProjectRef: LocalObjectReference{Name: "demo"},
			SourceRef: TypedObjectReference{
				APIVersion: GroupVersion.String(),
				Kind:       "ServiceInstance",
				Name:       "db",
			},
			TargetRef: TypedObjectReference{
				APIVersion: "v1",
				Kind:       "Namespace",
				Name:       "app",
				Namespace:  "demo-app",
			},
			SecretPolicy: SecretPolicySpec{
				DeliveryMode:           SecretDeliveryModeExternalSecret,
				ExternalSecretProvider: ExternalSecretProviderVault,
			},
		},
	}

	if _, err := binding.ValidateCreate(context.Background(), binding); err == nil {
		t.Fatal("expected validation error for missing vault config")
	}
}
