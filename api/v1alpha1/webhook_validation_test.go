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

func TestTenantDefaultsLifecycleAndRequiresOwner(t *testing.T) {
	tenant := &Tenant{}
	if err := tenant.Default(context.Background(), tenant); err != nil {
		t.Fatalf("default failed: %v", err)
	}
	if tenant.Spec.Lifecycle.Phase != defaultLifecyclePhase {
		t.Fatalf("expected default lifecycle %q, got %q", defaultLifecyclePhase, tenant.Spec.Lifecycle.Phase)
	}

	tenant = &Tenant{
		Spec: TenantSpec{
			DisplayName:           "Demo",
			QuotaProfileRef:       LocalObjectReference{Name: "standard"},
			AllowedServiceClasses: []string{"postgresql"},
			Lifecycle:             TenantLifecycleSpec{Phase: TenantLifecyclePhaseActive},
		},
	}
	if _, err := tenant.ValidateCreate(context.Background(), tenant); err == nil {
		t.Fatal("expected validation error for missing owner")
	}
}

func TestProjectDefaultsNamespaceStrategyPrefix(t *testing.T) {
	project := &Project{}
	project.Name = "demo"
	if err := project.Default(context.Background(), project); err != nil {
		t.Fatalf("default failed: %v", err)
	}
	if project.Spec.NamespaceStrategy.Mode != defaultNamespaceMode {
		t.Fatalf("expected default namespace mode %q, got %q", defaultNamespaceMode, project.Spec.NamespaceStrategy.Mode)
	}
	if project.Spec.NamespaceStrategy.Prefix != "demo" {
		t.Fatalf("expected default namespace prefix demo, got %q", project.Spec.NamespaceStrategy.Prefix)
	}
}

func TestOperatorPackageDefaultsAndValidatesSource(t *testing.T) {
	pkg := &OperatorPackage{}
	if err := pkg.Default(context.Background(), pkg); err != nil {
		t.Fatalf("default failed: %v", err)
	}
	if pkg.Spec.TargetNamespace != defaultOperatorNamespace {
		t.Fatalf("expected default target namespace %q, got %q", defaultOperatorNamespace, pkg.Spec.TargetNamespace)
	}
	if pkg.Spec.Source.TargetRevision != defaultOperatorRevision {
		t.Fatalf("expected default target revision %q, got %q", defaultOperatorRevision, pkg.Spec.Source.TargetRevision)
	}

	pkg = &OperatorPackage{
		Spec: OperatorPackageSpec{
			DisplayName: "CNPG",
			Source:      OperatorPackageSource{RepoURL: "", Path: ""},
		},
	}
	if _, err := pkg.ValidateCreate(context.Background(), pkg); err == nil {
		t.Fatal("expected validation error for missing operator source")
	}
}

func TestActionRequestDefaultsAndRejectsMissingRequester(t *testing.T) {
	request := &ActionRequest{}
	if err := request.Default(context.Background(), request); err != nil {
		t.Fatalf("default failed: %v", err)
	}
	if request.Spec.Approval.Mode != defaultApprovalMode {
		t.Fatalf("expected default approval mode %q, got %q", defaultApprovalMode, request.Spec.Approval.Mode)
	}
	if request.Spec.RequestedBy.Source != defaultRequestSource {
		t.Fatalf("expected default request source %q, got %q", defaultRequestSource, request.Spec.RequestedBy.Source)
	}

	request = &ActionRequest{
		Spec: ActionRequestSpec{
			TargetRef:      TypedObjectReference{APIVersion: "v1", Kind: "Pod", Name: "demo"},
			Action:         "restart",
			IdempotencyKey: "abc",
			Approval:       ApprovalSpec{Mode: ApprovalModeAuto},
		},
	}
	if _, err := request.ValidateCreate(context.Background(), request); err == nil {
		t.Fatal("expected validation error for missing requestedBy.subject")
	}
}

func TestPolicyWebhookValidatesRuleSpec(t *testing.T) {
	policy := &Policy{
		Spec: PolicySpec{
			DisplayName: "Demo",
			TargetKinds: []PolicyTargetKind{PolicyTargetServiceInstance},
			Rules: []PolicyRule{{
				Name:     "bad",
				Operator: PolicyOperatorEquals,
			}},
		},
	}
	if _, err := policy.ValidateCreate(context.Background(), policy); err == nil {
		t.Fatal("expected validation error for incomplete rule")
	}
}
