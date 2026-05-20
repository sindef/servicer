package v1alpha1

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

const (
	defaultExposureMode      = ExposureModeClusterInternal
	defaultSecretDelivery    = SecretDeliveryModeExternalSecret
	defaultDeletionPolicy    = DeletionPolicyDelete
	defaultVMPowerState      = "running"
	serviceInstanceKind      = "ServiceInstance"
	namespaceClaimKind       = "NamespaceClaim"
	serviceBindingKind       = "ServiceBinding"
	virtualMachineClaimKind  = "VirtualMachineClaim"
	serviceInstanceAPIString = "platform.servicer.io/v1alpha1"
)

var (
	_ webhook.CustomDefaulter = &ServiceInstance{}
	_ webhook.CustomValidator = &ServiceInstance{}
	_ webhook.CustomDefaulter = &NamespaceClaim{}
	_ webhook.CustomValidator = &NamespaceClaim{}
	_ webhook.CustomDefaulter = &ServiceBinding{}
	_ webhook.CustomValidator = &ServiceBinding{}
	_ webhook.CustomDefaulter = &VirtualMachineClaim{}
	_ webhook.CustomValidator = &VirtualMachineClaim{}
)

// +kubebuilder:webhook:path=/mutate-platform-servicer-io-v1alpha1-serviceinstance,mutating=true,failurePolicy=fail,sideEffects=None,groups=platform.servicer.io,resources=serviceinstances,verbs=create;update,versions=v1alpha1,name=mserviceinstance.platform.servicer.io,admissionReviewVersions=v1
// +kubebuilder:webhook:path=/validate-platform-servicer-io-v1alpha1-serviceinstance,mutating=false,failurePolicy=fail,sideEffects=None,groups=platform.servicer.io,resources=serviceinstances,verbs=create;update,versions=v1alpha1,name=vserviceinstance.platform.servicer.io,admissionReviewVersions=v1
func (r *ServiceInstance) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return builder.WebhookManagedBy(mgr).For(r).WithDefaulter(r).WithValidator(r).Complete()
}

func (r *ServiceInstance) Default(_ context.Context, obj runtime.Object) error {
	instance, ok := obj.(*ServiceInstance)
	if !ok {
		return apierrors.NewBadRequest("expected a ServiceInstance for defaulting")
	}
	defaultExposure(&instance.Spec.Exposure)
	defaultSecretPolicy(&instance.Spec.SecretPolicy)
	defaultDeletion(&instance.Spec.DeletionPolicy)
	return nil
}

func (r *ServiceInstance) ValidateCreate(_ context.Context, obj runtime.Object) (admission.Warnings, error) {
	instance, ok := obj.(*ServiceInstance)
	if !ok {
		return nil, apierrors.NewBadRequest("expected a ServiceInstance for create validation")
	}
	return nil, validateServiceInstance(instance)
}

func (r *ServiceInstance) ValidateUpdate(_ context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	previous, ok := oldObj.(*ServiceInstance)
	if !ok {
		return nil, apierrors.NewBadRequest("expected a ServiceInstance for old update validation object")
	}
	current, ok := newObj.(*ServiceInstance)
	if !ok {
		return nil, apierrors.NewBadRequest("expected a ServiceInstance for new update validation object")
	}
	return nil, validateServiceInstanceUpdate(previous, current)
}

func (r *ServiceInstance) ValidateDelete(_ context.Context, _ runtime.Object) (admission.Warnings, error) {
	return nil, nil
}

// +kubebuilder:webhook:path=/mutate-platform-servicer-io-v1alpha1-namespaceclaim,mutating=true,failurePolicy=fail,sideEffects=None,groups=platform.servicer.io,resources=namespaceclaims,verbs=create;update,versions=v1alpha1,name=mnamespaceclaim.platform.servicer.io,admissionReviewVersions=v1
// +kubebuilder:webhook:path=/validate-platform-servicer-io-v1alpha1-namespaceclaim,mutating=false,failurePolicy=fail,sideEffects=None,groups=platform.servicer.io,resources=namespaceclaims,verbs=create;update,versions=v1alpha1,name=vnamespaceclaim.platform.servicer.io,admissionReviewVersions=v1
func (r *NamespaceClaim) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return builder.WebhookManagedBy(mgr).For(r).WithDefaulter(r).WithValidator(r).Complete()
}

func (r *NamespaceClaim) Default(_ context.Context, obj runtime.Object) error {
	claim, ok := obj.(*NamespaceClaim)
	if !ok {
		return apierrors.NewBadRequest("expected a NamespaceClaim for defaulting")
	}
	defaultDeletion(&claim.Spec.DeletionPolicy)
	return nil
}

func (r *NamespaceClaim) ValidateCreate(_ context.Context, obj runtime.Object) (admission.Warnings, error) {
	claim, ok := obj.(*NamespaceClaim)
	if !ok {
		return nil, apierrors.NewBadRequest("expected a NamespaceClaim for create validation")
	}
	return nil, validateNamespaceClaim(claim)
}

func (r *NamespaceClaim) ValidateUpdate(_ context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	previous, ok := oldObj.(*NamespaceClaim)
	if !ok {
		return nil, apierrors.NewBadRequest("expected a NamespaceClaim for old update validation object")
	}
	current, ok := newObj.(*NamespaceClaim)
	if !ok {
		return nil, apierrors.NewBadRequest("expected a NamespaceClaim for new update validation object")
	}
	return nil, validateNamespaceClaimUpdate(previous, current)
}

func (r *NamespaceClaim) ValidateDelete(_ context.Context, _ runtime.Object) (admission.Warnings, error) {
	return nil, nil
}

// +kubebuilder:webhook:path=/mutate-platform-servicer-io-v1alpha1-servicebinding,mutating=true,failurePolicy=fail,sideEffects=None,groups=platform.servicer.io,resources=servicebindings,verbs=create;update,versions=v1alpha1,name=mservicebinding.platform.servicer.io,admissionReviewVersions=v1
// +kubebuilder:webhook:path=/validate-platform-servicer-io-v1alpha1-servicebinding,mutating=false,failurePolicy=fail,sideEffects=None,groups=platform.servicer.io,resources=servicebindings,verbs=create;update,versions=v1alpha1,name=vservicebinding.platform.servicer.io,admissionReviewVersions=v1
func (r *ServiceBinding) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return builder.WebhookManagedBy(mgr).For(r).WithDefaulter(r).WithValidator(r).Complete()
}

func (r *ServiceBinding) Default(_ context.Context, obj runtime.Object) error {
	binding, ok := obj.(*ServiceBinding)
	if !ok {
		return apierrors.NewBadRequest("expected a ServiceBinding for defaulting")
	}
	defaultSecretPolicy(&binding.Spec.SecretPolicy)
	return nil
}

func (r *ServiceBinding) ValidateCreate(_ context.Context, obj runtime.Object) (admission.Warnings, error) {
	binding, ok := obj.(*ServiceBinding)
	if !ok {
		return nil, apierrors.NewBadRequest("expected a ServiceBinding for create validation")
	}
	return nil, validateServiceBinding(binding)
}

func (r *ServiceBinding) ValidateUpdate(_ context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	previous, ok := oldObj.(*ServiceBinding)
	if !ok {
		return nil, apierrors.NewBadRequest("expected a ServiceBinding for old update validation object")
	}
	current, ok := newObj.(*ServiceBinding)
	if !ok {
		return nil, apierrors.NewBadRequest("expected a ServiceBinding for new update validation object")
	}
	return nil, validateServiceBindingUpdate(previous, current)
}

func (r *ServiceBinding) ValidateDelete(_ context.Context, _ runtime.Object) (admission.Warnings, error) {
	return nil, nil
}

// +kubebuilder:webhook:path=/mutate-platform-servicer-io-v1alpha1-virtualmachineclaim,mutating=true,failurePolicy=fail,sideEffects=None,groups=platform.servicer.io,resources=virtualmachineclaims,verbs=create;update,versions=v1alpha1,name=mvirtualmachineclaim.platform.servicer.io,admissionReviewVersions=v1
// +kubebuilder:webhook:path=/validate-platform-servicer-io-v1alpha1-virtualmachineclaim,mutating=false,failurePolicy=fail,sideEffects=None,groups=platform.servicer.io,resources=virtualmachineclaims,verbs=create;update,versions=v1alpha1,name=vvirtualmachineclaim.platform.servicer.io,admissionReviewVersions=v1
func (r *VirtualMachineClaim) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return builder.WebhookManagedBy(mgr).For(r).WithDefaulter(r).WithValidator(r).Complete()
}

func (r *VirtualMachineClaim) Default(_ context.Context, obj runtime.Object) error {
	claim, ok := obj.(*VirtualMachineClaim)
	if !ok {
		return apierrors.NewBadRequest("expected a VirtualMachineClaim for defaulting")
	}
	defaultExposure(&claim.Spec.Exposure)
	defaultSecretPolicy(&claim.Spec.SecretPolicy)
	defaultDeletion(&claim.Spec.DeletionPolicy)
	if claim.Spec.PowerState == "" {
		claim.Spec.PowerState = defaultVMPowerState
	}
	return nil
}

func (r *VirtualMachineClaim) ValidateCreate(_ context.Context, obj runtime.Object) (admission.Warnings, error) {
	claim, ok := obj.(*VirtualMachineClaim)
	if !ok {
		return nil, apierrors.NewBadRequest("expected a VirtualMachineClaim for create validation")
	}
	return nil, validateVirtualMachineClaim(claim)
}

func (r *VirtualMachineClaim) ValidateUpdate(_ context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	previous, ok := oldObj.(*VirtualMachineClaim)
	if !ok {
		return nil, apierrors.NewBadRequest("expected a VirtualMachineClaim for old update validation object")
	}
	current, ok := newObj.(*VirtualMachineClaim)
	if !ok {
		return nil, apierrors.NewBadRequest("expected a VirtualMachineClaim for new update validation object")
	}
	return nil, validateVirtualMachineClaimUpdate(previous, current)
}

func (r *VirtualMachineClaim) ValidateDelete(_ context.Context, _ runtime.Object) (admission.Warnings, error) {
	return nil, nil
}

func validateServiceInstance(instance *ServiceInstance) error {
	allErrs := validateServiceInstanceSpec(instance.Spec)
	if len(allErrs) > 0 {
		return apierrors.NewInvalid(schema.GroupKind{Group: GroupVersion.Group, Kind: serviceInstanceKind}, instance.Name, allErrs)
	}
	return nil
}

func validateServiceInstanceUpdate(previous, current *ServiceInstance) error {
	allErrs := validateServiceInstanceSpec(current.Spec)
	allErrs = append(allErrs, validateImmutableLocalRef(previous.Spec.ProjectRef, current.Spec.ProjectRef, field.NewPath("spec", "projectRef"))...)
	allErrs = append(allErrs, validateImmutableLocalRef(previous.Spec.ServiceClassRef, current.Spec.ServiceClassRef, field.NewPath("spec", "serviceClassRef"))...)
	allErrs = append(allErrs, validateImmutableLocalRef(previous.Spec.ServicePlanRef, current.Spec.ServicePlanRef, field.NewPath("spec", "servicePlanRef"))...)
	if len(allErrs) > 0 {
		return apierrors.NewInvalid(schema.GroupKind{Group: GroupVersion.Group, Kind: serviceInstanceKind}, current.Name, allErrs)
	}
	return nil
}

func validateServiceInstanceSpec(spec ServiceInstanceSpec) field.ErrorList {
	allErrs := field.ErrorList{}
	allErrs = append(allErrs, validateLocalRef(spec.ProjectRef, field.NewPath("spec", "projectRef"))...)
	allErrs = append(allErrs, validateLocalRef(spec.ServiceClassRef, field.NewPath("spec", "serviceClassRef"))...)
	allErrs = append(allErrs, validateLocalRef(spec.ServicePlanRef, field.NewPath("spec", "servicePlanRef"))...)
	allErrs = append(allErrs, validateExposure(spec.Exposure, field.NewPath("spec", "exposure"))...)
	allErrs = append(allErrs, validateSecretPolicy(spec.SecretPolicy, field.NewPath("spec", "secretPolicy"))...)
	allErrs = append(allErrs, validateDeletionPolicy(spec.DeletionPolicy, field.NewPath("spec", "deletionPolicy"))...)
	return allErrs
}

func validateNamespaceClaim(claim *NamespaceClaim) error {
	allErrs := field.ErrorList{}
	allErrs = append(allErrs, validateLocalRef(claim.Spec.ProjectRef, field.NewPath("spec", "projectRef"))...)
	allErrs = append(allErrs, validateDeletionPolicy(claim.Spec.DeletionPolicy, field.NewPath("spec", "deletionPolicy"))...)
	for key, value := range claim.Spec.Quotas {
		if key == "" {
			allErrs = append(allErrs, field.Invalid(field.NewPath("spec", "quotas"), claim.Spec.Quotas, "quota keys must not be empty"))
			break
		}
		if value == "" {
			allErrs = append(allErrs, field.Invalid(field.NewPath("spec", "quotas").Key(key), value, "quota values must not be empty"))
		}
	}
	allErrs = append(allErrs, validateLabelMap(claim.Spec.Labels, field.NewPath("spec", "labels"))...)
	if len(allErrs) > 0 {
		return apierrors.NewInvalid(schema.GroupKind{Group: GroupVersion.Group, Kind: namespaceClaimKind}, claim.Name, allErrs)
	}
	return nil
}

func validateNamespaceClaimUpdate(previous, current *NamespaceClaim) error {
	allErrs := field.ErrorList{}
	allErrs = append(allErrs, validateNamespaceClaimSpec(current.Spec)...)
	allErrs = append(allErrs, validateImmutableLocalRef(previous.Spec.ProjectRef, current.Spec.ProjectRef, field.NewPath("spec", "projectRef"))...)
	if len(allErrs) > 0 {
		return apierrors.NewInvalid(schema.GroupKind{Group: GroupVersion.Group, Kind: namespaceClaimKind}, current.Name, allErrs)
	}
	return nil
}

func validateNamespaceClaimSpec(spec NamespaceClaimSpec) field.ErrorList {
	allErrs := field.ErrorList{}
	allErrs = append(allErrs, validateLocalRef(spec.ProjectRef, field.NewPath("spec", "projectRef"))...)
	allErrs = append(allErrs, validateDeletionPolicy(spec.DeletionPolicy, field.NewPath("spec", "deletionPolicy"))...)
	for key, value := range spec.Quotas {
		if key == "" {
			allErrs = append(allErrs, field.Invalid(field.NewPath("spec", "quotas"), spec.Quotas, "quota keys must not be empty"))
			break
		}
		if value == "" {
			allErrs = append(allErrs, field.Invalid(field.NewPath("spec", "quotas").Key(key), value, "quota values must not be empty"))
		}
	}
	allErrs = append(allErrs, validateLabelMap(spec.Labels, field.NewPath("spec", "labels"))...)
	return allErrs
}

func validateServiceBinding(binding *ServiceBinding) error {
	allErrs := validateServiceBindingSpec(binding.Spec)
	if len(allErrs) > 0 {
		return apierrors.NewInvalid(schema.GroupKind{Group: GroupVersion.Group, Kind: serviceBindingKind}, binding.Name, allErrs)
	}
	return nil
}

func validateServiceBindingUpdate(previous, current *ServiceBinding) error {
	allErrs := validateServiceBindingSpec(current.Spec)
	allErrs = append(allErrs, validateImmutableLocalRef(previous.Spec.ProjectRef, current.Spec.ProjectRef, field.NewPath("spec", "projectRef"))...)
	allErrs = append(allErrs, validateImmutableTypedRef(previous.Spec.SourceRef, current.Spec.SourceRef, field.NewPath("spec", "sourceRef"))...)
	allErrs = append(allErrs, validateImmutableTypedRef(previous.Spec.TargetRef, current.Spec.TargetRef, field.NewPath("spec", "targetRef"))...)
	if len(allErrs) > 0 {
		return apierrors.NewInvalid(schema.GroupKind{Group: GroupVersion.Group, Kind: serviceBindingKind}, current.Name, allErrs)
	}
	return nil
}

func validateServiceBindingSpec(spec ServiceBindingSpec) field.ErrorList {
	allErrs := field.ErrorList{}
	allErrs = append(allErrs, validateLocalRef(spec.ProjectRef, field.NewPath("spec", "projectRef"))...)
	allErrs = append(allErrs, validateSecretPolicy(spec.SecretPolicy, field.NewPath("spec", "secretPolicy"))...)
	allErrs = append(allErrs, validateTypedRef(spec.SourceRef, field.NewPath("spec", "sourceRef"))...)
	allErrs = append(allErrs, validateTypedRef(spec.TargetRef, field.NewPath("spec", "targetRef"))...)
	if spec.SourceRef.Kind != serviceInstanceKind {
		allErrs = append(allErrs, field.NotSupported(field.NewPath("spec", "sourceRef", "kind"), spec.SourceRef.Kind, []string{serviceInstanceKind}))
	}
	if spec.SourceRef.APIVersion != "" && spec.SourceRef.APIVersion != serviceInstanceAPIString {
		allErrs = append(allErrs, field.Invalid(field.NewPath("spec", "sourceRef", "apiVersion"), spec.SourceRef.APIVersion, fmt.Sprintf("must be %q", serviceInstanceAPIString)))
	}
	if spec.TargetRef.Namespace == "" && spec.TargetRef.Name == "" {
		allErrs = append(allErrs, field.Required(field.NewPath("spec", "targetRef"), "targetRef must set at least a namespace or a name"))
	}
	return allErrs
}

func validateVirtualMachineClaim(claim *VirtualMachineClaim) error {
	allErrs := validateVirtualMachineClaimSpec(claim.Spec)
	if len(allErrs) > 0 {
		return apierrors.NewInvalid(schema.GroupKind{Group: GroupVersion.Group, Kind: virtualMachineClaimKind}, claim.Name, allErrs)
	}
	return nil
}

func validateVirtualMachineClaimUpdate(previous, current *VirtualMachineClaim) error {
	allErrs := validateVirtualMachineClaimSpec(current.Spec)
	allErrs = append(allErrs, validateImmutableLocalRef(previous.Spec.ProjectRef, current.Spec.ProjectRef, field.NewPath("spec", "projectRef"))...)
	allErrs = append(allErrs, validateImmutableString(previous.Spec.Class, current.Spec.Class, field.NewPath("spec", "class"))...)
	allErrs = append(allErrs, validateImmutableString(previous.Spec.Image, current.Spec.Image, field.NewPath("spec", "image"))...)
	if len(allErrs) > 0 {
		return apierrors.NewInvalid(schema.GroupKind{Group: GroupVersion.Group, Kind: virtualMachineClaimKind}, current.Name, allErrs)
	}
	return nil
}

func validateVirtualMachineClaimSpec(spec VirtualMachineClaimSpec) field.ErrorList {
	allErrs := field.ErrorList{}
	allErrs = append(allErrs, validateLocalRef(spec.ProjectRef, field.NewPath("spec", "projectRef"))...)
	allErrs = append(allErrs, validateExposure(spec.Exposure, field.NewPath("spec", "exposure"))...)
	allErrs = append(allErrs, validateSecretPolicy(spec.SecretPolicy, field.NewPath("spec", "secretPolicy"))...)
	allErrs = append(allErrs, validateDeletionPolicy(spec.DeletionPolicy, field.NewPath("spec", "deletionPolicy"))...)
	if spec.Class == "" {
		allErrs = append(allErrs, field.Required(field.NewPath("spec", "class"), "class is required"))
	}
	if spec.Image == "" {
		allErrs = append(allErrs, field.Required(field.NewPath("spec", "image"), "image is required"))
	}
	if spec.PowerState != "" && spec.PowerState != "running" && spec.PowerState != "stopped" {
		allErrs = append(allErrs, field.NotSupported(field.NewPath("spec", "powerState"), spec.PowerState, []string{"running", "stopped"}))
	}
	return allErrs
}

func defaultExposure(exposure *ExposureSpec) {
	if exposure.Mode == "" {
		exposure.Mode = defaultExposureMode
	}
}

func defaultSecretPolicy(policy *SecretPolicySpec) {
	if policy.DeliveryMode == "" {
		policy.DeliveryMode = defaultSecretDelivery
	}
}

func defaultDeletion(policy *DeletionPolicy) {
	if *policy == "" {
		*policy = defaultDeletionPolicy
	}
}

func validateLocalRef(ref LocalObjectReference, path *field.Path) field.ErrorList {
	if ref.Name == "" {
		return field.ErrorList{field.Required(path.Child("name"), "name is required")}
	}
	return nil
}

func validateImmutableLocalRef(previous, current LocalObjectReference, path *field.Path) field.ErrorList {
	if previous.Name != current.Name {
		return field.ErrorList{field.Invalid(path.Child("name"), current.Name, "field is immutable")}
	}
	return nil
}

func validateTypedRef(ref TypedObjectReference, path *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	if ref.APIVersion == "" {
		allErrs = append(allErrs, field.Required(path.Child("apiVersion"), "apiVersion is required"))
	}
	if ref.Kind == "" {
		allErrs = append(allErrs, field.Required(path.Child("kind"), "kind is required"))
	}
	if ref.Name == "" && ref.Namespace == "" {
		allErrs = append(allErrs, field.Required(path, "target reference must set a name or namespace"))
	}
	return allErrs
}

func validateImmutableTypedRef(previous, current TypedObjectReference, path *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	if previous.APIVersion != current.APIVersion {
		allErrs = append(allErrs, field.Invalid(path.Child("apiVersion"), current.APIVersion, "field is immutable"))
	}
	if previous.Kind != current.Kind {
		allErrs = append(allErrs, field.Invalid(path.Child("kind"), current.Kind, "field is immutable"))
	}
	if previous.Name != current.Name {
		allErrs = append(allErrs, field.Invalid(path.Child("name"), current.Name, "field is immutable"))
	}
	if previous.Namespace != current.Namespace {
		allErrs = append(allErrs, field.Invalid(path.Child("namespace"), current.Namespace, "field is immutable"))
	}
	return allErrs
}

func validateImmutableString(previous, current string, path *field.Path) field.ErrorList {
	if previous != current {
		return field.ErrorList{field.Invalid(path, current, "field is immutable")}
	}
	return nil
}

func validateExposure(exposure ExposureSpec, path *field.Path) field.ErrorList {
	if exposure.Mode == "" {
		return field.ErrorList{field.Required(path.Child("mode"), "mode is required")}
	}
	return nil
}

func validateSecretPolicy(policy SecretPolicySpec, path *field.Path) field.ErrorList {
	if policy.DeliveryMode == "" {
		return field.ErrorList{field.Required(path.Child("deliveryMode"), "deliveryMode is required")}
	}
	return nil
}

func validateDeletionPolicy(policy DeletionPolicy, path *field.Path) field.ErrorList {
	if policy == "" {
		return field.ErrorList{field.Required(path, "deletionPolicy is required")}
	}
	return nil
}

func validateLabelMap(labels map[string]string, path *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	for key, value := range labels {
		if problems := validation.IsQualifiedName(key); len(problems) > 0 {
			allErrs = append(allErrs, field.Invalid(path.Key(key), key, fmt.Sprintf("invalid label key: %s", problems[0])))
		}
		if problems := validation.IsValidLabelValue(value); len(problems) > 0 {
			allErrs = append(allErrs, field.Invalid(path.Key(key), value, fmt.Sprintf("invalid label value: %s", problems[0])))
		}
	}
	return allErrs
}
