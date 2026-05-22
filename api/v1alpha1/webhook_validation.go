package v1alpha1

import (
	"context"
	"fmt"
	"strings"

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
	defaultLifecyclePhase    = TenantLifecyclePhaseActive
	defaultNamespaceMode     = NamespaceStrategyDedicated
	defaultApprovalMode      = ApprovalModeAuto
	defaultRequestSource     = RequestSourceAPI
	defaultOperatorRevision  = "HEAD"
	defaultOperatorNamespace = "operators"
	actionRequestKind        = "ActionRequest"
	clusterTargetKind        = "ClusterTarget"
	operatorPackageKind      = "OperatorPackage"
	policyKind               = "Policy"
	projectKind              = "Project"
	authProviderKind         = "AuthProvider"
	userKind                 = "User"
	groupKind                = "Group"
	roleBindingKind          = "RoleBinding"
	serviceInstanceKind      = "ServiceInstance"
	serviceClassKind         = "ServiceClass"
	servicePlanKind          = "ServicePlan"
	namespaceClaimKind       = "NamespaceClaim"
	serviceBindingKind       = "ServiceBinding"
	tenantKind               = "Tenant"
	virtualMachineClaimKind  = "VirtualMachineClaim"
	serviceInstanceAPIString = "platform.servicer.io/v1alpha1"
)

var (
	_ webhook.CustomDefaulter = &ActionRequest{}
	_ webhook.CustomValidator = &ActionRequest{}
	_ webhook.CustomDefaulter = &ClusterTarget{}
	_ webhook.CustomValidator = &ClusterTarget{}
	_ webhook.CustomDefaulter = &OperatorPackage{}
	_ webhook.CustomValidator = &OperatorPackage{}
	_ webhook.CustomDefaulter = &Policy{}
	_ webhook.CustomValidator = &Policy{}
	_ webhook.CustomDefaulter = &Project{}
	_ webhook.CustomValidator = &Project{}
	_ webhook.CustomDefaulter = &AuthProvider{}
	_ webhook.CustomValidator = &AuthProvider{}
	_ webhook.CustomDefaulter = &User{}
	_ webhook.CustomValidator = &User{}
	_ webhook.CustomDefaulter = &Group{}
	_ webhook.CustomValidator = &Group{}
	_ webhook.CustomDefaulter = &RoleBinding{}
	_ webhook.CustomValidator = &RoleBinding{}
	_ webhook.CustomDefaulter = &ServiceInstance{}
	_ webhook.CustomValidator = &ServiceInstance{}
	_ webhook.CustomDefaulter = &ServiceClass{}
	_ webhook.CustomValidator = &ServiceClass{}
	_ webhook.CustomDefaulter = &ServicePlan{}
	_ webhook.CustomValidator = &ServicePlan{}
	_ webhook.CustomDefaulter = &Tenant{}
	_ webhook.CustomValidator = &Tenant{}
	_ webhook.CustomDefaulter = &NamespaceClaim{}
	_ webhook.CustomValidator = &NamespaceClaim{}
	_ webhook.CustomDefaulter = &ServiceBinding{}
	_ webhook.CustomValidator = &ServiceBinding{}
	_ webhook.CustomDefaulter = &VirtualMachineClaim{}
	_ webhook.CustomValidator = &VirtualMachineClaim{}
)

// +kubebuilder:webhook:path=/mutate-platform-servicer-io-v1alpha1-actionrequest,mutating=true,failurePolicy=fail,sideEffects=None,groups=platform.servicer.io,resources=actionrequests,verbs=create;update,versions=v1alpha1,name=mactionrequest.platform.servicer.io,admissionReviewVersions=v1
// +kubebuilder:webhook:path=/validate-platform-servicer-io-v1alpha1-actionrequest,mutating=false,failurePolicy=fail,sideEffects=None,groups=platform.servicer.io,resources=actionrequests,verbs=create;update,versions=v1alpha1,name=vactionrequest.platform.servicer.io,admissionReviewVersions=v1
func (r *ActionRequest) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return builder.WebhookManagedBy(mgr).For(r).WithDefaulter(r).WithValidator(r).Complete()
}

func (r *ActionRequest) Default(_ context.Context, obj runtime.Object) error {
	request, ok := obj.(*ActionRequest)
	if !ok {
		return apierrors.NewBadRequest("expected an ActionRequest for defaulting")
	}
	if request.Spec.Approval.Mode == "" {
		request.Spec.Approval.Mode = defaultApprovalMode
	}
	if request.Spec.RequestedBy.Source == "" {
		request.Spec.RequestedBy.Source = defaultRequestSource
	}
	return nil
}

func (r *ActionRequest) ValidateCreate(_ context.Context, obj runtime.Object) (admission.Warnings, error) {
	request, ok := obj.(*ActionRequest)
	if !ok {
		return nil, apierrors.NewBadRequest("expected an ActionRequest for create validation")
	}
	return nil, validateActionRequest(request)
}

func (r *ActionRequest) ValidateUpdate(_ context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	previous, ok := oldObj.(*ActionRequest)
	if !ok {
		return nil, apierrors.NewBadRequest("expected an ActionRequest for old update validation object")
	}
	current, ok := newObj.(*ActionRequest)
	if !ok {
		return nil, apierrors.NewBadRequest("expected an ActionRequest for new update validation object")
	}
	return nil, validateActionRequestUpdate(previous, current)
}

func (r *ActionRequest) ValidateDelete(_ context.Context, _ runtime.Object) (admission.Warnings, error) {
	return nil, nil
}

// +kubebuilder:webhook:path=/mutate-platform-servicer-io-v1alpha1-clustertarget,mutating=true,failurePolicy=fail,sideEffects=None,groups=platform.servicer.io,resources=clustertargets,verbs=create;update,versions=v1alpha1,name=mclustertarget.platform.servicer.io,admissionReviewVersions=v1
// +kubebuilder:webhook:path=/validate-platform-servicer-io-v1alpha1-clustertarget,mutating=false,failurePolicy=fail,sideEffects=None,groups=platform.servicer.io,resources=clustertargets,verbs=create;update,versions=v1alpha1,name=vclustertarget.platform.servicer.io,admissionReviewVersions=v1
func (r *ClusterTarget) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return builder.WebhookManagedBy(mgr).For(r).WithDefaulter(r).WithValidator(r).Complete()
}

func (r *ClusterTarget) Default(_ context.Context, _ runtime.Object) error {
	return nil
}

func (r *ClusterTarget) ValidateCreate(_ context.Context, obj runtime.Object) (admission.Warnings, error) {
	target, ok := obj.(*ClusterTarget)
	if !ok {
		return nil, apierrors.NewBadRequest("expected a ClusterTarget for create validation")
	}
	return nil, validateClusterTarget(target)
}

func (r *ClusterTarget) ValidateUpdate(_ context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	previous, ok := oldObj.(*ClusterTarget)
	if !ok {
		return nil, apierrors.NewBadRequest("expected a ClusterTarget for old update validation object")
	}
	current, ok := newObj.(*ClusterTarget)
	if !ok {
		return nil, apierrors.NewBadRequest("expected a ClusterTarget for new update validation object")
	}
	return nil, validateClusterTargetUpdate(previous, current)
}

func (r *ClusterTarget) ValidateDelete(_ context.Context, _ runtime.Object) (admission.Warnings, error) {
	return nil, nil
}

// +kubebuilder:webhook:path=/mutate-platform-servicer-io-v1alpha1-operatorpackage,mutating=true,failurePolicy=fail,sideEffects=None,groups=platform.servicer.io,resources=operatorpackages,verbs=create;update,versions=v1alpha1,name=moperatorpackage.platform.servicer.io,admissionReviewVersions=v1
// +kubebuilder:webhook:path=/validate-platform-servicer-io-v1alpha1-operatorpackage,mutating=false,failurePolicy=fail,sideEffects=None,groups=platform.servicer.io,resources=operatorpackages,verbs=create;update,versions=v1alpha1,name=voperatorpackage.platform.servicer.io,admissionReviewVersions=v1
func (r *OperatorPackage) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return builder.WebhookManagedBy(mgr).For(r).WithDefaulter(r).WithValidator(r).Complete()
}

func (r *OperatorPackage) Default(_ context.Context, obj runtime.Object) error {
	pkg, ok := obj.(*OperatorPackage)
	if !ok {
		return apierrors.NewBadRequest("expected an OperatorPackage for defaulting")
	}
	if pkg.Spec.TargetNamespace == "" {
		pkg.Spec.TargetNamespace = defaultOperatorNamespace
	}
	if pkg.Spec.Source.TargetRevision == "" {
		pkg.Spec.Source.TargetRevision = defaultOperatorRevision
	}
	return nil
}

func (r *OperatorPackage) ValidateCreate(_ context.Context, obj runtime.Object) (admission.Warnings, error) {
	pkg, ok := obj.(*OperatorPackage)
	if !ok {
		return nil, apierrors.NewBadRequest("expected an OperatorPackage for create validation")
	}
	return nil, validateOperatorPackage(pkg)
}

func (r *OperatorPackage) ValidateUpdate(_ context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	previous, ok := oldObj.(*OperatorPackage)
	if !ok {
		return nil, apierrors.NewBadRequest("expected an OperatorPackage for old update validation object")
	}
	current, ok := newObj.(*OperatorPackage)
	if !ok {
		return nil, apierrors.NewBadRequest("expected an OperatorPackage for new update validation object")
	}
	return nil, validateOperatorPackageUpdate(previous, current)
}

func (r *OperatorPackage) ValidateDelete(_ context.Context, _ runtime.Object) (admission.Warnings, error) {
	return nil, nil
}

// +kubebuilder:webhook:path=/mutate-platform-servicer-io-v1alpha1-policy,mutating=true,failurePolicy=fail,sideEffects=None,groups=platform.servicer.io,resources=policies,verbs=create;update,versions=v1alpha1,name=mpolicy.platform.servicer.io,admissionReviewVersions=v1
// +kubebuilder:webhook:path=/validate-platform-servicer-io-v1alpha1-policy,mutating=false,failurePolicy=fail,sideEffects=None,groups=platform.servicer.io,resources=policies,verbs=create;update,versions=v1alpha1,name=vpolicy.platform.servicer.io,admissionReviewVersions=v1
func (r *Policy) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return builder.WebhookManagedBy(mgr).For(r).WithDefaulter(r).WithValidator(r).Complete()
}

func (r *Policy) Default(_ context.Context, _ runtime.Object) error {
	return nil
}

func (r *Policy) ValidateCreate(_ context.Context, obj runtime.Object) (admission.Warnings, error) {
	policy, ok := obj.(*Policy)
	if !ok {
		return nil, apierrors.NewBadRequest("expected a Policy for create validation")
	}
	return nil, validatePolicy(policy)
}

func (r *Policy) ValidateUpdate(_ context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	current, ok := newObj.(*Policy)
	if !ok {
		return nil, apierrors.NewBadRequest("expected a Policy for update validation")
	}
	return nil, validatePolicy(current)
}

func (r *Policy) ValidateDelete(_ context.Context, _ runtime.Object) (admission.Warnings, error) {
	return nil, nil
}

// +kubebuilder:webhook:path=/mutate-platform-servicer-io-v1alpha1-project,mutating=true,failurePolicy=fail,sideEffects=None,groups=platform.servicer.io,resources=projects,verbs=create;update,versions=v1alpha1,name=mproject.platform.servicer.io,admissionReviewVersions=v1
// +kubebuilder:webhook:path=/validate-platform-servicer-io-v1alpha1-project,mutating=false,failurePolicy=fail,sideEffects=None,groups=platform.servicer.io,resources=projects,verbs=create;update,versions=v1alpha1,name=vproject.platform.servicer.io,admissionReviewVersions=v1
func (r *Project) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return builder.WebhookManagedBy(mgr).For(r).WithDefaulter(r).WithValidator(r).Complete()
}

func (r *Project) Default(_ context.Context, obj runtime.Object) error {
	project, ok := obj.(*Project)
	if !ok {
		return apierrors.NewBadRequest("expected a Project for defaulting")
	}
	if project.Spec.NamespaceStrategy.Mode == "" {
		project.Spec.NamespaceStrategy.Mode = defaultNamespaceMode
	}
	if project.Spec.NamespaceStrategy.Mode == NamespaceStrategyDedicated && project.Spec.NamespaceStrategy.Prefix == "" {
		project.Spec.NamespaceStrategy.Prefix = project.Name
	}
	return nil
}

func (r *Project) ValidateCreate(_ context.Context, obj runtime.Object) (admission.Warnings, error) {
	project, ok := obj.(*Project)
	if !ok {
		return nil, apierrors.NewBadRequest("expected a Project for create validation")
	}
	return nil, validateProject(project)
}

func (r *Project) ValidateUpdate(_ context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	previous, ok := oldObj.(*Project)
	if !ok {
		return nil, apierrors.NewBadRequest("expected a Project for old update validation object")
	}
	current, ok := newObj.(*Project)
	if !ok {
		return nil, apierrors.NewBadRequest("expected a Project for new update validation object")
	}
	return nil, validateProjectUpdate(previous, current)
}

func (r *Project) ValidateDelete(_ context.Context, _ runtime.Object) (admission.Warnings, error) {
	return nil, nil
}

// +kubebuilder:webhook:path=/mutate-platform-servicer-io-v1alpha1-authprovider,mutating=true,failurePolicy=fail,sideEffects=None,groups=platform.servicer.io,resources=authproviders,verbs=create;update,versions=v1alpha1,name=mauthprovider.platform.servicer.io,admissionReviewVersions=v1
// +kubebuilder:webhook:path=/validate-platform-servicer-io-v1alpha1-authprovider,mutating=false,failurePolicy=fail,sideEffects=None,groups=platform.servicer.io,resources=authproviders,verbs=create;update,versions=v1alpha1,name=vauthprovider.platform.servicer.io,admissionReviewVersions=v1
func (r *AuthProvider) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return builder.WebhookManagedBy(mgr).For(r).WithDefaulter(r).WithValidator(r).Complete()
}

func (r *AuthProvider) Default(_ context.Context, _ runtime.Object) error {
	return nil
}

func (r *AuthProvider) ValidateCreate(_ context.Context, obj runtime.Object) (admission.Warnings, error) {
	provider, ok := obj.(*AuthProvider)
	if !ok {
		return nil, apierrors.NewBadRequest("expected an AuthProvider for create validation")
	}
	return nil, validateAuthProvider(provider)
}

func (r *AuthProvider) ValidateUpdate(_ context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	previous, ok := oldObj.(*AuthProvider)
	if !ok {
		return nil, apierrors.NewBadRequest("expected an AuthProvider for old update validation object")
	}
	current, ok := newObj.(*AuthProvider)
	if !ok {
		return nil, apierrors.NewBadRequest("expected an AuthProvider for new update validation object")
	}
	return nil, validateAuthProviderUpdate(previous, current)
}

func (r *AuthProvider) ValidateDelete(_ context.Context, _ runtime.Object) (admission.Warnings, error) {
	return nil, nil
}

// +kubebuilder:webhook:path=/mutate-platform-servicer-io-v1alpha1-user,mutating=true,failurePolicy=fail,sideEffects=None,groups=platform.servicer.io,resources=users,verbs=create;update,versions=v1alpha1,name=muser.platform.servicer.io,admissionReviewVersions=v1
// +kubebuilder:webhook:path=/validate-platform-servicer-io-v1alpha1-user,mutating=false,failurePolicy=fail,sideEffects=None,groups=platform.servicer.io,resources=users,verbs=create;update,versions=v1alpha1,name=vuser.platform.servicer.io,admissionReviewVersions=v1
func (r *User) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return builder.WebhookManagedBy(mgr).For(r).WithDefaulter(r).WithValidator(r).Complete()
}

func (r *User) Default(_ context.Context, _ runtime.Object) error {
	return nil
}

func (r *User) ValidateCreate(_ context.Context, obj runtime.Object) (admission.Warnings, error) {
	user, ok := obj.(*User)
	if !ok {
		return nil, apierrors.NewBadRequest("expected a User for create validation")
	}
	return nil, validateUser(user)
}

func (r *User) ValidateUpdate(_ context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	previous, ok := oldObj.(*User)
	if !ok {
		return nil, apierrors.NewBadRequest("expected a User for old update validation object")
	}
	current, ok := newObj.(*User)
	if !ok {
		return nil, apierrors.NewBadRequest("expected a User for new update validation object")
	}
	return nil, validateUserUpdate(previous, current)
}

func (r *User) ValidateDelete(_ context.Context, _ runtime.Object) (admission.Warnings, error) {
	return nil, nil
}

// +kubebuilder:webhook:path=/mutate-platform-servicer-io-v1alpha1-group,mutating=true,failurePolicy=fail,sideEffects=None,groups=platform.servicer.io,resources=groups,verbs=create;update,versions=v1alpha1,name=mgroup.platform.servicer.io,admissionReviewVersions=v1
// +kubebuilder:webhook:path=/validate-platform-servicer-io-v1alpha1-group,mutating=false,failurePolicy=fail,sideEffects=None,groups=platform.servicer.io,resources=groups,verbs=create;update,versions=v1alpha1,name=vgroup.platform.servicer.io,admissionReviewVersions=v1
func (r *Group) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return builder.WebhookManagedBy(mgr).For(r).WithDefaulter(r).WithValidator(r).Complete()
}

func (r *Group) Default(_ context.Context, _ runtime.Object) error {
	return nil
}

func (r *Group) ValidateCreate(_ context.Context, obj runtime.Object) (admission.Warnings, error) {
	group, ok := obj.(*Group)
	if !ok {
		return nil, apierrors.NewBadRequest("expected a Group for create validation")
	}
	return nil, validateGroup(group)
}

func (r *Group) ValidateUpdate(_ context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	current, ok := newObj.(*Group)
	if !ok {
		return nil, apierrors.NewBadRequest("expected a Group for update validation")
	}
	return nil, validateGroup(current)
}

func (r *Group) ValidateDelete(_ context.Context, _ runtime.Object) (admission.Warnings, error) {
	return nil, nil
}

// +kubebuilder:webhook:path=/mutate-platform-servicer-io-v1alpha1-rolebinding,mutating=true,failurePolicy=fail,sideEffects=None,groups=platform.servicer.io,resources=rolebindings,verbs=create;update,versions=v1alpha1,name=mrolebinding.platform.servicer.io,admissionReviewVersions=v1
// +kubebuilder:webhook:path=/validate-platform-servicer-io-v1alpha1-rolebinding,mutating=false,failurePolicy=fail,sideEffects=None,groups=platform.servicer.io,resources=rolebindings,verbs=create;update,versions=v1alpha1,name=vrolebinding.platform.servicer.io,admissionReviewVersions=v1
func (r *RoleBinding) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return builder.WebhookManagedBy(mgr).For(r).WithDefaulter(r).WithValidator(r).Complete()
}

func (r *RoleBinding) Default(_ context.Context, _ runtime.Object) error {
	return nil
}

func (r *RoleBinding) ValidateCreate(_ context.Context, obj runtime.Object) (admission.Warnings, error) {
	binding, ok := obj.(*RoleBinding)
	if !ok {
		return nil, apierrors.NewBadRequest("expected a RoleBinding for create validation")
	}
	return nil, validateRoleBinding(binding)
}

func (r *RoleBinding) ValidateUpdate(_ context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	previous, ok := oldObj.(*RoleBinding)
	if !ok {
		return nil, apierrors.NewBadRequest("expected a RoleBinding for old update validation object")
	}
	current, ok := newObj.(*RoleBinding)
	if !ok {
		return nil, apierrors.NewBadRequest("expected a RoleBinding for new update validation object")
	}
	return nil, validateRoleBindingUpdate(previous, current)
}

func (r *RoleBinding) ValidateDelete(_ context.Context, _ runtime.Object) (admission.Warnings, error) {
	return nil, nil
}

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

// +kubebuilder:webhook:path=/mutate-platform-servicer-io-v1alpha1-serviceclass,mutating=true,failurePolicy=fail,sideEffects=None,groups=platform.servicer.io,resources=serviceclasses,verbs=create;update,versions=v1alpha1,name=mserviceclass.platform.servicer.io,admissionReviewVersions=v1
// +kubebuilder:webhook:path=/validate-platform-servicer-io-v1alpha1-serviceclass,mutating=false,failurePolicy=fail,sideEffects=None,groups=platform.servicer.io,resources=serviceclasses,verbs=create;update,versions=v1alpha1,name=vserviceclass.platform.servicer.io,admissionReviewVersions=v1
func (r *ServiceClass) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return builder.WebhookManagedBy(mgr).For(r).WithDefaulter(r).WithValidator(r).Complete()
}

func (r *ServiceClass) Default(_ context.Context, _ runtime.Object) error {
	return nil
}

func (r *ServiceClass) ValidateCreate(_ context.Context, obj runtime.Object) (admission.Warnings, error) {
	serviceClass, ok := obj.(*ServiceClass)
	if !ok {
		return nil, apierrors.NewBadRequest("expected a ServiceClass for create validation")
	}
	return nil, validateServiceClass(serviceClass)
}

func (r *ServiceClass) ValidateUpdate(_ context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	previous, ok := oldObj.(*ServiceClass)
	if !ok {
		return nil, apierrors.NewBadRequest("expected a ServiceClass for old update validation object")
	}
	current, ok := newObj.(*ServiceClass)
	if !ok {
		return nil, apierrors.NewBadRequest("expected a ServiceClass for new update validation object")
	}
	return nil, validateServiceClassUpdate(previous, current)
}

func (r *ServiceClass) ValidateDelete(_ context.Context, _ runtime.Object) (admission.Warnings, error) {
	return nil, nil
}

// +kubebuilder:webhook:path=/mutate-platform-servicer-io-v1alpha1-serviceplan,mutating=true,failurePolicy=fail,sideEffects=None,groups=platform.servicer.io,resources=serviceplans,verbs=create;update,versions=v1alpha1,name=mserviceplan.platform.servicer.io,admissionReviewVersions=v1
// +kubebuilder:webhook:path=/validate-platform-servicer-io-v1alpha1-serviceplan,mutating=false,failurePolicy=fail,sideEffects=None,groups=platform.servicer.io,resources=serviceplans,verbs=create;update,versions=v1alpha1,name=vserviceplan.platform.servicer.io,admissionReviewVersions=v1
func (r *ServicePlan) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return builder.WebhookManagedBy(mgr).For(r).WithDefaulter(r).WithValidator(r).Complete()
}

func (r *ServicePlan) Default(_ context.Context, _ runtime.Object) error {
	return nil
}

func (r *ServicePlan) ValidateCreate(_ context.Context, obj runtime.Object) (admission.Warnings, error) {
	plan, ok := obj.(*ServicePlan)
	if !ok {
		return nil, apierrors.NewBadRequest("expected a ServicePlan for create validation")
	}
	return nil, validateServicePlan(plan)
}

func (r *ServicePlan) ValidateUpdate(_ context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	previous, ok := oldObj.(*ServicePlan)
	if !ok {
		return nil, apierrors.NewBadRequest("expected a ServicePlan for old update validation object")
	}
	current, ok := newObj.(*ServicePlan)
	if !ok {
		return nil, apierrors.NewBadRequest("expected a ServicePlan for new update validation object")
	}
	return nil, validateServicePlanUpdate(previous, current)
}

func (r *ServicePlan) ValidateDelete(_ context.Context, _ runtime.Object) (admission.Warnings, error) {
	return nil, nil
}

// +kubebuilder:webhook:path=/mutate-platform-servicer-io-v1alpha1-tenant,mutating=true,failurePolicy=fail,sideEffects=None,groups=platform.servicer.io,resources=tenants,verbs=create;update,versions=v1alpha1,name=mtenant.platform.servicer.io,admissionReviewVersions=v1
// +kubebuilder:webhook:path=/validate-platform-servicer-io-v1alpha1-tenant,mutating=false,failurePolicy=fail,sideEffects=None,groups=platform.servicer.io,resources=tenants,verbs=create;update,versions=v1alpha1,name=vtenant.platform.servicer.io,admissionReviewVersions=v1
func (r *Tenant) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return builder.WebhookManagedBy(mgr).For(r).WithDefaulter(r).WithValidator(r).Complete()
}

func (r *Tenant) Default(_ context.Context, obj runtime.Object) error {
	tenant, ok := obj.(*Tenant)
	if !ok {
		return apierrors.NewBadRequest("expected a Tenant for defaulting")
	}
	if tenant.Spec.Lifecycle.Phase == "" {
		tenant.Spec.Lifecycle.Phase = defaultLifecyclePhase
	}
	return nil
}

func (r *Tenant) ValidateCreate(_ context.Context, obj runtime.Object) (admission.Warnings, error) {
	tenant, ok := obj.(*Tenant)
	if !ok {
		return nil, apierrors.NewBadRequest("expected a Tenant for create validation")
	}
	return nil, validateTenant(tenant)
}

func (r *Tenant) ValidateUpdate(_ context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	previous, ok := oldObj.(*Tenant)
	if !ok {
		return nil, apierrors.NewBadRequest("expected a Tenant for old update validation object")
	}
	current, ok := newObj.(*Tenant)
	if !ok {
		return nil, apierrors.NewBadRequest("expected a Tenant for new update validation object")
	}
	return nil, validateTenantUpdate(previous, current)
}

func (r *Tenant) ValidateDelete(_ context.Context, _ runtime.Object) (admission.Warnings, error) {
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

func validateActionRequest(request *ActionRequest) error {
	allErrs := validateActionRequestSpec(request.Spec)
	if len(allErrs) > 0 {
		return apierrors.NewInvalid(schema.GroupKind{Group: GroupVersion.Group, Kind: actionRequestKind}, request.Name, allErrs)
	}
	return nil
}

func validateActionRequestUpdate(previous, current *ActionRequest) error {
	allErrs := validateActionRequestSpec(current.Spec)
	allErrs = append(allErrs, validateImmutableTypedRef(previous.Spec.TargetRef, current.Spec.TargetRef, field.NewPath("spec", "targetRef"))...)
	allErrs = append(allErrs, validateImmutableString(previous.Spec.Action, current.Spec.Action, field.NewPath("spec", "action"))...)
	allErrs = append(allErrs, validateImmutableString(previous.Spec.IdempotencyKey, current.Spec.IdempotencyKey, field.NewPath("spec", "idempotencyKey"))...)
	if len(allErrs) > 0 {
		return apierrors.NewInvalid(schema.GroupKind{Group: GroupVersion.Group, Kind: actionRequestKind}, current.Name, allErrs)
	}
	return nil
}

func validateActionRequestSpec(spec ActionRequestSpec) field.ErrorList {
	allErrs := validateTypedRef(spec.TargetRef, field.NewPath("spec", "targetRef"))
	if strings.TrimSpace(spec.Action) == "" {
		allErrs = append(allErrs, field.Required(field.NewPath("spec", "action"), "action is required"))
	}
	if strings.TrimSpace(spec.IdempotencyKey) == "" {
		allErrs = append(allErrs, field.Required(field.NewPath("spec", "idempotencyKey"), "idempotencyKey is required"))
	}
	if spec.Approval.Mode == "" {
		allErrs = append(allErrs, field.Required(field.NewPath("spec", "approval", "mode"), "mode is required"))
	}
	if strings.TrimSpace(spec.RequestedBy.Subject) == "" {
		allErrs = append(allErrs, field.Required(field.NewPath("spec", "requestedBy", "subject"), "subject is required"))
	}
	if spec.RequestedBy.Source == "" {
		allErrs = append(allErrs, field.Required(field.NewPath("spec", "requestedBy", "source"), "source is required"))
	}
	return allErrs
}

func validateClusterTarget(target *ClusterTarget) error {
	allErrs := validateClusterTargetSpec(target.Spec)
	if len(allErrs) > 0 {
		return apierrors.NewInvalid(schema.GroupKind{Group: GroupVersion.Group, Kind: clusterTargetKind}, target.Name, allErrs)
	}
	return nil
}

func validateClusterTargetUpdate(previous, current *ClusterTarget) error {
	allErrs := validateClusterTargetSpec(current.Spec)
	allErrs = append(allErrs, validateImmutableLocalRef(
		LocalObjectReference{Name: previous.Spec.ConnectionRef.Name},
		LocalObjectReference{Name: current.Spec.ConnectionRef.Name},
		field.NewPath("spec", "connectionRef"),
	)...)
	if previous.Spec.ConnectionRef.Namespace != current.Spec.ConnectionRef.Namespace {
		allErrs = append(allErrs, field.Invalid(field.NewPath("spec", "connectionRef", "namespace"), current.Spec.ConnectionRef.Namespace, "field is immutable"))
	}
	if len(allErrs) > 0 {
		return apierrors.NewInvalid(schema.GroupKind{Group: GroupVersion.Group, Kind: clusterTargetKind}, current.Name, allErrs)
	}
	return nil
}

func validateClusterTargetSpec(spec ClusterTargetSpec) field.ErrorList {
	allErrs := validateNamespacedRef(spec.ConnectionRef, field.NewPath("spec", "connectionRef"))
	allErrs = append(allErrs, validatePolicyRefs(spec.DefaultPolicyRefs, field.NewPath("spec", "defaultPolicyRefs"))...)
	for key, value := range spec.Capabilities {
		if strings.TrimSpace(key) == "" {
			allErrs = append(allErrs, field.Invalid(field.NewPath("spec", "capabilities"), spec.Capabilities, "capability keys must not be empty"))
			break
		}
		if strings.TrimSpace(value) == "" {
			allErrs = append(allErrs, field.Invalid(field.NewPath("spec", "capabilities").Key(key), value, "capability values must not be empty"))
		}
	}
	for i, requiredPackage := range spec.RequiredPackages {
		if strings.TrimSpace(requiredPackage) == "" {
			allErrs = append(allErrs, field.Required(field.NewPath("spec", "requiredPackages").Index(i), "package name is required"))
		}
	}
	return allErrs
}

func validateOperatorPackage(pkg *OperatorPackage) error {
	allErrs := validateOperatorPackageSpec(pkg.Spec)
	if len(allErrs) > 0 {
		return apierrors.NewInvalid(schema.GroupKind{Group: GroupVersion.Group, Kind: operatorPackageKind}, pkg.Name, allErrs)
	}
	return nil
}

func validateOperatorPackageUpdate(previous, current *OperatorPackage) error {
	allErrs := validateOperatorPackageSpec(current.Spec)
	if previous.Spec.Source.RepoURL != current.Spec.Source.RepoURL {
		allErrs = append(allErrs, field.Invalid(field.NewPath("spec", "source", "repoURL"), current.Spec.Source.RepoURL, "field is immutable"))
	}
	if previous.Spec.Source.Path != current.Spec.Source.Path {
		allErrs = append(allErrs, field.Invalid(field.NewPath("spec", "source", "path"), current.Spec.Source.Path, "field is immutable"))
	}
	if previous.Spec.Source.ChartArchiveURL != current.Spec.Source.ChartArchiveURL {
		allErrs = append(allErrs, field.Invalid(field.NewPath("spec", "source", "chartArchiveURL"), current.Spec.Source.ChartArchiveURL, "field is immutable"))
	}
	if previous.Spec.Source.ChartPath != current.Spec.Source.ChartPath {
		allErrs = append(allErrs, field.Invalid(field.NewPath("spec", "source", "chartPath"), current.Spec.Source.ChartPath, "field is immutable"))
	}
	if len(allErrs) > 0 {
		return apierrors.NewInvalid(schema.GroupKind{Group: GroupVersion.Group, Kind: operatorPackageKind}, current.Name, allErrs)
	}
	return nil
}

func validateOperatorPackageSpec(spec OperatorPackageSpec) field.ErrorList {
	allErrs := field.ErrorList{}
	if strings.TrimSpace(spec.DisplayName) == "" {
		allErrs = append(allErrs, field.Required(field.NewPath("spec", "displayName"), "displayName is required"))
	}
	hasArgoSource := strings.TrimSpace(spec.Source.RepoURL) != "" || strings.TrimSpace(spec.Source.Path) != ""
	hasChartSource := strings.TrimSpace(spec.Source.ChartArchiveURL) != "" || strings.TrimSpace(spec.Source.ChartPath) != "" || len(spec.Source.HelmValues) > 0
	hasDirectSource := strings.TrimSpace(spec.Source.ManifestURL) != "" || hasChartSource
	if !hasDirectSource && !hasArgoSource {
		allErrs = append(allErrs, field.Required(field.NewPath("spec", "source"), "one direct-install or Argo source is required"))
	}
	if strings.TrimSpace(spec.Source.RepoURL) == "" && strings.TrimSpace(spec.Source.Path) != "" {
		allErrs = append(allErrs, field.Required(field.NewPath("spec", "source", "repoURL"), "repoURL is required when path is set"))
	}
	if strings.TrimSpace(spec.Source.Path) == "" && strings.TrimSpace(spec.Source.RepoURL) != "" {
		allErrs = append(allErrs, field.Required(field.NewPath("spec", "source", "path"), "path is required when repoURL is set"))
	}
	if strings.TrimSpace(spec.Source.ChartArchiveURL) == "" && (strings.TrimSpace(spec.Source.ChartPath) != "" || len(spec.Source.HelmValues) > 0) {
		allErrs = append(allErrs, field.Required(field.NewPath("spec", "source", "chartArchiveURL"), "chartArchiveURL is required when chartPath or helmValues are set"))
	}
	if strings.TrimSpace(spec.Source.ChartArchiveURL) != "" && strings.TrimSpace(spec.Source.ChartPath) == "" {
		allErrs = append(allErrs, field.Required(field.NewPath("spec", "source", "chartPath"), "chartPath is required when chartArchiveURL is set"))
	}
	if len(spec.Probes) == 0 {
		allErrs = append(allErrs, field.Required(field.NewPath("spec", "probes"), "at least one probe is required"))
	}
	for i, probe := range spec.Probes {
		if strings.TrimSpace(probe.CRD) == "" {
			allErrs = append(allErrs, field.Required(field.NewPath("spec", "probes").Index(i).Child("crd"), "crd is required"))
		}
	}
	return allErrs
}

func validatePolicy(policy *Policy) error {
	if err := validatePolicyWebhookSpec(policy); err != nil {
		return apierrors.NewInvalid(schema.GroupKind{Group: GroupVersion.Group, Kind: policyKind}, policy.Name, field.ErrorList{field.Invalid(field.NewPath("spec"), policy.Spec, err.Error())})
	}
	return nil
}

func validatePolicyWebhookSpec(policy *Policy) error {
	if policy == nil {
		return nil
	}
	if len(policy.Spec.TargetKinds) == 0 {
		return fmt.Errorf("at least one target kind is required")
	}
	if len(policy.Spec.Rules) == 0 {
		return fmt.Errorf("at least one rule is required")
	}
	for _, rule := range policy.Spec.Rules {
		if strings.TrimSpace(rule.Name) == "" {
			return fmt.Errorf("policy rules must declare a name")
		}
		switch rule.Operator {
		case PolicyOperatorExists, PolicyOperatorNotExists, PolicyOperatorEmpty, PolicyOperatorNotEmpty:
			if strings.TrimSpace(rule.Path) == "" {
				return fmt.Errorf("rule %q must declare a path", rule.Name)
			}
		case PolicyOperatorIn, PolicyOperatorNotIn:
			if strings.TrimSpace(rule.Path) == "" || len(rule.Values) == 0 {
				return fmt.Errorf("rule %q must declare a path and non-empty values", rule.Name)
			}
		default:
			if strings.TrimSpace(rule.Path) == "" || strings.TrimSpace(rule.Value) == "" {
				return fmt.Errorf("rule %q must declare a path and value", rule.Name)
			}
		}
	}
	return nil
}

func validateProject(project *Project) error {
	allErrs := validateProjectSpec(project.Spec)
	if len(allErrs) > 0 {
		return apierrors.NewInvalid(schema.GroupKind{Group: GroupVersion.Group, Kind: projectKind}, project.Name, allErrs)
	}
	return nil
}

func validateProjectUpdate(previous, current *Project) error {
	allErrs := validateProjectSpec(current.Spec)
	allErrs = append(allErrs, validateImmutableLocalRef(previous.Spec.TenantRef, current.Spec.TenantRef, field.NewPath("spec", "tenantRef"))...)
	if len(allErrs) > 0 {
		return apierrors.NewInvalid(schema.GroupKind{Group: GroupVersion.Group, Kind: projectKind}, current.Name, allErrs)
	}
	return nil
}

func validateProjectSpec(spec ProjectSpec) field.ErrorList {
	allErrs := validateLocalRef(spec.TenantRef, field.NewPath("spec", "tenantRef"))
	if spec.NamespaceStrategy.Mode == "" {
		allErrs = append(allErrs, field.Required(field.NewPath("spec", "namespaceStrategy", "mode"), "mode is required"))
	}
	if spec.NamespaceStrategy.Mode == NamespaceStrategyDedicated && strings.TrimSpace(spec.NamespaceStrategy.Prefix) == "" {
		allErrs = append(allErrs, field.Required(field.NewPath("spec", "namespaceStrategy", "prefix"), "prefix is required for dedicated namespaces"))
	}
	allErrs = append(allErrs, validatePolicyRefs(spec.PolicyRefs, field.NewPath("spec", "policyRefs"))...)
	allErrs = append(allErrs, validateLabelMap(spec.Labels, field.NewPath("spec", "labels"))...)
	return allErrs
}

func validateAuthProvider(provider *AuthProvider) error {
	allErrs := validateAuthProviderSpec(provider.Spec)
	if len(allErrs) > 0 {
		return apierrors.NewInvalid(schema.GroupKind{Group: GroupVersion.Group, Kind: authProviderKind}, provider.Name, allErrs)
	}
	return nil
}

func validateAuthProviderUpdate(previous, current *AuthProvider) error {
	allErrs := validateAuthProviderSpec(current.Spec)
	if previous.Spec.Type != current.Spec.Type {
		allErrs = append(allErrs, field.Invalid(field.NewPath("spec", "type"), current.Spec.Type, "field is immutable"))
	}
	if len(allErrs) > 0 {
		return apierrors.NewInvalid(schema.GroupKind{Group: GroupVersion.Group, Kind: authProviderKind}, current.Name, allErrs)
	}
	return nil
}

func validateAuthProviderSpec(spec AuthProviderSpec) field.ErrorList {
	allErrs := field.ErrorList{}
	if strings.TrimSpace(spec.DisplayName) == "" {
		allErrs = append(allErrs, field.Required(field.NewPath("spec", "displayName"), "displayName is required"))
	}
	if spec.Default && !spec.Enabled {
		allErrs = append(allErrs, field.Forbidden(field.NewPath("spec", "default"), "default providers must be enabled"))
	}
	switch spec.Type {
	case AuthProviderTypeLocal:
		if spec.OIDC != nil {
			allErrs = append(allErrs, field.Forbidden(field.NewPath("spec", "oidc"), "oidc config is only valid when type=oidc"))
		}
		if spec.LDAP != nil {
			allErrs = append(allErrs, field.Forbidden(field.NewPath("spec", "ldap"), "ldap config is only valid when type=ldap"))
		}
	case AuthProviderTypeOIDC:
		if spec.OIDC == nil {
			allErrs = append(allErrs, field.Required(field.NewPath("spec", "oidc"), "oidc config is required when type=oidc"))
			return allErrs
		}
		if spec.LDAP != nil {
			allErrs = append(allErrs, field.Forbidden(field.NewPath("spec", "ldap"), "ldap config is only valid when type=ldap"))
		}
		if strings.TrimSpace(spec.OIDC.IssuerURL) == "" {
			allErrs = append(allErrs, field.Required(field.NewPath("spec", "oidc", "issuerUrl"), "issuerUrl is required"))
		}
		if strings.TrimSpace(spec.OIDC.ClientID) == "" {
			allErrs = append(allErrs, field.Required(field.NewPath("spec", "oidc", "clientId"), "clientId is required"))
		}
		allErrs = append(allErrs, validateNamespacedRef(spec.OIDC.ClientSecretRef, field.NewPath("spec", "oidc", "clientSecretRef"))...)
	case AuthProviderTypeLDAP:
		if spec.LDAP == nil {
			allErrs = append(allErrs, field.Required(field.NewPath("spec", "ldap"), "ldap config is required when type=ldap"))
			return allErrs
		}
		if spec.OIDC != nil {
			allErrs = append(allErrs, field.Forbidden(field.NewPath("spec", "oidc"), "oidc config is only valid when type=oidc"))
		}
		if strings.TrimSpace(spec.LDAP.URL) == "" {
			allErrs = append(allErrs, field.Required(field.NewPath("spec", "ldap", "url"), "url is required"))
		}
		allErrs = append(allErrs, validateNamespacedRef(spec.LDAP.BindSecretRef, field.NewPath("spec", "ldap", "bindSecretRef"))...)
		if strings.TrimSpace(spec.LDAP.UserBaseDN) == "" {
			allErrs = append(allErrs, field.Required(field.NewPath("spec", "ldap", "userBaseDn"), "userBaseDn is required"))
		}
		if strings.TrimSpace(spec.LDAP.UserFilter) == "" {
			allErrs = append(allErrs, field.Required(field.NewPath("spec", "ldap", "userFilter"), "userFilter is required"))
		}
	default:
		allErrs = append(allErrs, field.NotSupported(field.NewPath("spec", "type"), spec.Type, []string{string(AuthProviderTypeLocal), string(AuthProviderTypeOIDC), string(AuthProviderTypeLDAP)}))
	}
	return allErrs
}

func validateUser(user *User) error {
	allErrs := validateUserSpec(user.Spec)
	if len(allErrs) > 0 {
		return apierrors.NewInvalid(schema.GroupKind{Group: GroupVersion.Group, Kind: userKind}, user.Name, allErrs)
	}
	return nil
}

func validateUserUpdate(_, current *User) error {
	return validateUser(current)
}

func validateUserSpec(spec UserSpec) field.ErrorList {
	allErrs := field.ErrorList{}
	if spec.LocalAuth == nil && len(spec.ExternalIdentities) == 0 {
		allErrs = append(allErrs, field.Required(field.NewPath("spec"), "at least one local or external identity is required"))
	}
	if spec.LocalAuth != nil {
		if !spec.LocalAuth.Enabled {
			allErrs = append(allErrs, field.Forbidden(field.NewPath("spec", "localAuth", "enabled"), "localAuth must be enabled when configured"))
		}
		allErrs = append(allErrs, validateNamespacedRef(spec.LocalAuth.PasswordHashSecretRef, field.NewPath("spec", "localAuth", "passwordHashSecretRef"))...)
	}
	for i, identity := range spec.ExternalIdentities {
		allErrs = append(allErrs, validateLocalRef(identity.ProviderRef, field.NewPath("spec", "externalIdentities").Index(i).Child("providerRef"))...)
		if strings.TrimSpace(identity.Subject) == "" {
			allErrs = append(allErrs, field.Required(field.NewPath("spec", "externalIdentities").Index(i).Child("subject"), "subject is required"))
		}
	}
	return allErrs
}

func validateGroup(group *Group) error {
	allErrs := validateGroupSpec(group.Spec)
	if len(allErrs) > 0 {
		return apierrors.NewInvalid(schema.GroupKind{Group: GroupVersion.Group, Kind: groupKind}, group.Name, allErrs)
	}
	return nil
}

func validateGroupSpec(spec GroupSpec) field.ErrorList {
	allErrs := field.ErrorList{}
	if len(spec.Members) == 0 && len(spec.ExternalGroups) == 0 {
		allErrs = append(allErrs, field.Required(field.NewPath("spec"), "at least one member or external group is required"))
	}
	for i, member := range spec.Members {
		allErrs = append(allErrs, validateLocalRef(member.UserRef, field.NewPath("spec", "members").Index(i).Child("userRef"))...)
	}
	for i, externalGroup := range spec.ExternalGroups {
		allErrs = append(allErrs, validateLocalRef(externalGroup.ProviderRef, field.NewPath("spec", "externalGroups").Index(i).Child("providerRef"))...)
		if strings.TrimSpace(externalGroup.Name) == "" {
			allErrs = append(allErrs, field.Required(field.NewPath("spec", "externalGroups").Index(i).Child("name"), "name is required"))
		}
	}
	return allErrs
}

func validateRoleBinding(binding *RoleBinding) error {
	allErrs := validateRoleBindingSpec(binding.Spec)
	if len(allErrs) > 0 {
		return apierrors.NewInvalid(schema.GroupKind{Group: GroupVersion.Group, Kind: roleBindingKind}, binding.Name, allErrs)
	}
	return nil
}

func validateRoleBindingUpdate(previous, current *RoleBinding) error {
	allErrs := validateRoleBindingSpec(current.Spec)
	if previous.Spec.Scope != current.Spec.Scope {
		allErrs = append(allErrs, field.Invalid(field.NewPath("spec", "scope"), current.Spec.Scope, "field is immutable"))
	}
	switch {
	case previous.Spec.TenantRef == nil && current.Spec.TenantRef != nil:
		allErrs = append(allErrs, field.Invalid(field.NewPath("spec", "tenantRef"), current.Spec.TenantRef.Name, "field is immutable"))
	case previous.Spec.TenantRef != nil && current.Spec.TenantRef == nil:
		allErrs = append(allErrs, field.Invalid(field.NewPath("spec", "tenantRef"), "", "field is immutable"))
	case previous.Spec.TenantRef != nil && current.Spec.TenantRef != nil:
		allErrs = append(allErrs, validateImmutableLocalRef(*previous.Spec.TenantRef, *current.Spec.TenantRef, field.NewPath("spec", "tenantRef"))...)
	}
	if len(allErrs) > 0 {
		return apierrors.NewInvalid(schema.GroupKind{Group: GroupVersion.Group, Kind: roleBindingKind}, current.Name, allErrs)
	}
	return nil
}

func validateRoleBindingSpec(spec RoleBindingSpec) field.ErrorList {
	allErrs := field.ErrorList{}
	if len(spec.Subjects) == 0 {
		allErrs = append(allErrs, field.Required(field.NewPath("spec", "subjects"), "at least one subject is required"))
	}
	if len(spec.Roles) == 0 {
		allErrs = append(allErrs, field.Required(field.NewPath("spec", "roles"), "at least one role is required"))
	}
	for i, subject := range spec.Subjects {
		if strings.TrimSpace(subject.Name) == "" {
			allErrs = append(allErrs, field.Required(field.NewPath("spec", "subjects").Index(i).Child("name"), "name is required"))
		}
	}
	switch spec.Scope {
	case AccessScopePlatform:
		if spec.TenantRef != nil {
			allErrs = append(allErrs, field.Forbidden(field.NewPath("spec", "tenantRef"), "tenantRef is only valid when scope=tenant"))
		}
	case AccessScopeTenant:
		if spec.TenantRef == nil {
			allErrs = append(allErrs, field.Required(field.NewPath("spec", "tenantRef"), "tenantRef is required when scope=tenant"))
		} else {
			allErrs = append(allErrs, validateLocalRef(*spec.TenantRef, field.NewPath("spec", "tenantRef"))...)
		}
	default:
		allErrs = append(allErrs, field.NotSupported(field.NewPath("spec", "scope"), spec.Scope, []string{string(AccessScopePlatform), string(AccessScopeTenant)}))
	}
	return allErrs
}

func validateServiceClass(serviceClass *ServiceClass) error {
	allErrs := validateServiceClassSpec(serviceClass.Spec)
	if len(allErrs) > 0 {
		return apierrors.NewInvalid(schema.GroupKind{Group: GroupVersion.Group, Kind: serviceClassKind}, serviceClass.Name, allErrs)
	}
	return nil
}

func validateServiceClassUpdate(previous, current *ServiceClass) error {
	allErrs := validateServiceClassSpec(current.Spec)
	if previous.Spec.Driver != current.Spec.Driver {
		allErrs = append(allErrs, field.Invalid(field.NewPath("spec", "driver"), current.Spec.Driver, "field is immutable"))
	}
	if len(allErrs) > 0 {
		return apierrors.NewInvalid(schema.GroupKind{Group: GroupVersion.Group, Kind: serviceClassKind}, current.Name, allErrs)
	}
	return nil
}

func validateServiceClassSpec(spec ServiceClassSpec) field.ErrorList {
	allErrs := field.ErrorList{}
	if strings.TrimSpace(spec.DisplayName) == "" {
		allErrs = append(allErrs, field.Required(field.NewPath("spec", "displayName"), "displayName is required"))
	}
	if strings.TrimSpace(spec.Driver) == "" {
		allErrs = append(allErrs, field.Required(field.NewPath("spec", "driver"), "driver is required"))
	}
	for i, version := range spec.SupportedVersions {
		if strings.TrimSpace(version) == "" {
			allErrs = append(allErrs, field.Required(field.NewPath("spec", "supportedVersions").Index(i), "supported version must not be empty"))
		}
	}
	for i, requiredPackage := range spec.RequiredPackages {
		if strings.TrimSpace(requiredPackage) == "" {
			allErrs = append(allErrs, field.Required(field.NewPath("spec", "requiredPackages").Index(i), "package name is required"))
		}
	}
	return allErrs
}

func validateServicePlan(plan *ServicePlan) error {
	allErrs := validateServicePlanSpec(plan.Spec)
	if len(allErrs) > 0 {
		return apierrors.NewInvalid(schema.GroupKind{Group: GroupVersion.Group, Kind: servicePlanKind}, plan.Name, allErrs)
	}
	return nil
}

func validateServicePlanUpdate(previous, current *ServicePlan) error {
	allErrs := validateServicePlanSpec(current.Spec)
	allErrs = append(allErrs, validateImmutableLocalRef(previous.Spec.ServiceClassRef, current.Spec.ServiceClassRef, field.NewPath("spec", "serviceClassRef"))...)
	if len(allErrs) > 0 {
		return apierrors.NewInvalid(schema.GroupKind{Group: GroupVersion.Group, Kind: servicePlanKind}, current.Name, allErrs)
	}
	return nil
}

func validateServicePlanSpec(spec ServicePlanSpec) field.ErrorList {
	allErrs := validateLocalRef(spec.ServiceClassRef, field.NewPath("spec", "serviceClassRef"))
	allErrs = append(allErrs, validatePolicyRefs(spec.PolicyRefs, field.NewPath("spec", "policyRefs"))...)
	return allErrs
}

func validateTenant(tenant *Tenant) error {
	allErrs := validateTenantSpec(tenant.Spec)
	if len(allErrs) > 0 {
		return apierrors.NewInvalid(schema.GroupKind{Group: GroupVersion.Group, Kind: tenantKind}, tenant.Name, allErrs)
	}
	return nil
}

func validateTenantUpdate(previous, current *Tenant) error {
	allErrs := validateTenantSpec(current.Spec)
	allErrs = append(allErrs, validateImmutableLocalRef(previous.Spec.QuotaProfileRef, current.Spec.QuotaProfileRef, field.NewPath("spec", "quotaProfileRef"))...)
	if len(allErrs) > 0 {
		return apierrors.NewInvalid(schema.GroupKind{Group: GroupVersion.Group, Kind: tenantKind}, current.Name, allErrs)
	}
	return nil
}

func validateTenantSpec(spec TenantSpec) field.ErrorList {
	allErrs := validateLocalRef(spec.QuotaProfileRef, field.NewPath("spec", "quotaProfileRef"))
	if len(spec.AllowedServiceClasses) == 0 {
		allErrs = append(allErrs, field.Required(field.NewPath("spec", "allowedServiceClasses"), "at least one service class is required"))
	}
	for i, allowed := range spec.AllowedServiceClasses {
		if strings.TrimSpace(allowed) == "" {
			allErrs = append(allErrs, field.Required(field.NewPath("spec", "allowedServiceClasses").Index(i), "service class name is required"))
		}
	}
	if len(spec.Owners.Users) == 0 && len(spec.Owners.Groups) == 0 {
		allErrs = append(allErrs, field.Required(field.NewPath("spec", "owners"), "at least one owner user or group is required"))
	}
	for i, contact := range spec.Contacts {
		if strings.TrimSpace(contact.Type) == "" {
			allErrs = append(allErrs, field.Required(field.NewPath("spec", "contacts").Index(i).Child("type"), "type is required"))
		}
		if strings.TrimSpace(contact.Value) == "" {
			allErrs = append(allErrs, field.Required(field.NewPath("spec", "contacts").Index(i).Child("value"), "value is required"))
		}
	}
	allErrs = append(allErrs, validatePolicyRefs(spec.PolicyRefs, field.NewPath("spec", "policyRefs"))...)
	return allErrs
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
	allowedTargetKinds := []string{"Namespace", "Deployment", "StatefulSet", "DaemonSet", "Job", "CronJob", "Pod"}
	if spec.TargetRef.Kind != "" && !stringInSlice(spec.TargetRef.Kind, allowedTargetKinds) {
		allErrs = append(allErrs, field.NotSupported(field.NewPath("spec", "targetRef", "kind"), spec.TargetRef.Kind, allowedTargetKinds))
	}
	if spec.TargetRef.Kind != "" && spec.TargetRef.Kind != "Namespace" && strings.TrimSpace(spec.TargetRef.Name) == "" {
		allErrs = append(allErrs, field.Required(field.NewPath("spec", "targetRef", "name"), "name is required for workload targets"))
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
	if policy.DeliveryMode == SecretDeliveryModeExternalSecret && policy.ExternalSecretProvider == "" {
		policy.ExternalSecretProvider = ExternalSecretProviderKubernetes
	}
	if policy.Vault != nil {
		if policy.Vault.Path == "" {
			policy.Vault.Path = "secret"
		}
		if policy.Vault.Version == "" {
			policy.Vault.Version = "v2"
		}
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

func validateNamespacedRef(ref NamespacedObjectReference, path *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	if strings.TrimSpace(ref.Name) == "" {
		allErrs = append(allErrs, field.Required(path.Child("name"), "name is required"))
	}
	if strings.TrimSpace(ref.Namespace) == "" {
		allErrs = append(allErrs, field.Required(path.Child("namespace"), "namespace is required"))
	}
	return allErrs
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
	allErrs := field.ErrorList{}
	if policy.DeliveryMode == "" {
		return field.ErrorList{field.Required(path.Child("deliveryMode"), "deliveryMode is required")}
	}
	if policy.ExternalSecretProvider != "" && policy.DeliveryMode != SecretDeliveryModeExternalSecret {
		allErrs = append(allErrs, field.Forbidden(path.Child("externalSecretProvider"), "externalSecretProvider is only valid when deliveryMode=external-secret"))
	}
	if policy.DeliveryMode == SecretDeliveryModeExternalSecret {
		switch policy.ExternalSecretProvider {
		case "", ExternalSecretProviderKubernetes:
			if policy.Vault != nil {
				allErrs = append(allErrs, field.Forbidden(path.Child("vault"), "vault config requires externalSecretProvider=vault"))
			}
		case ExternalSecretProviderVault:
			if policy.Vault == nil {
				allErrs = append(allErrs, field.Required(path.Child("vault"), "vault config is required when externalSecretProvider=vault"))
				return allErrs
			}
			if strings.TrimSpace(policy.Vault.Server) == "" {
				allErrs = append(allErrs, field.Required(path.Child("vault", "server"), "server is required"))
			}
			if strings.TrimSpace(policy.Vault.Path) == "" {
				allErrs = append(allErrs, field.Required(path.Child("vault", "path"), "path is required"))
			}
			if policy.Vault.Version != "" && policy.Vault.Version != "v1" && policy.Vault.Version != "v2" {
				allErrs = append(allErrs, field.NotSupported(path.Child("vault", "version"), policy.Vault.Version, []string{"v1", "v2"}))
			}
			if strings.TrimSpace(policy.Vault.AuthSecretRef.Name) == "" {
				allErrs = append(allErrs, field.Required(path.Child("vault", "authSecretRef", "name"), "name is required"))
			}
		default:
			allErrs = append(allErrs, field.NotSupported(path.Child("externalSecretProvider"), policy.ExternalSecretProvider, []string{string(ExternalSecretProviderKubernetes), string(ExternalSecretProviderVault)}))
		}
	}
	return allErrs
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

func validatePolicyRefs(refs []PolicyReference, path *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	for i, ref := range refs {
		if strings.TrimSpace(ref.Name) == "" {
			allErrs = append(allErrs, field.Required(path.Index(i).Child("name"), "name is required"))
		}
	}
	return allErrs
}

func stringInSlice(target string, items []string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}
