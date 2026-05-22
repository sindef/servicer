package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// AccessScope identifies where a role binding applies.
// +kubebuilder:validation:Enum=platform;tenant
type AccessScope string

const (
	AccessScopePlatform AccessScope = "platform"
	AccessScopeTenant   AccessScope = "tenant"
)

// ServicerRole identifies a coarse-grained Servicer authorization role.
// +kubebuilder:validation:Enum=platform-admin;tenant-admin;tenant-operator;service-consumer;auditor;catalog-admin;cluster-admin
type ServicerRole string

const (
	RolePlatformAdmin   ServicerRole = "platform-admin"
	RoleTenantAdmin     ServicerRole = "tenant-admin"
	RoleTenantOperator  ServicerRole = "tenant-operator"
	RoleServiceConsumer ServicerRole = "service-consumer"
	RoleAuditor         ServicerRole = "auditor"
	RoleCatalogAdmin    ServicerRole = "catalog-admin"
	RoleClusterAdmin    ServicerRole = "cluster-admin"
)

// SubjectKind identifies the kind of identity bound to a role.
// +kubebuilder:validation:Enum=User;Group
type SubjectKind string

const (
	SubjectKindUser  SubjectKind = "User"
	SubjectKindGroup SubjectKind = "Group"
)

// SubjectReference identifies one identity subject for authorization.
type SubjectReference struct {
	// Kind identifies whether the subject is a User or Group.
	Kind SubjectKind `json:"kind"`
	// Name is the referenced User or Group name.
	Name string `json:"name"`
}

// RoleBindingSpec defines one scoped role grant.
type RoleBindingSpec struct {
	// DisplayName is the human-readable binding name.
	DisplayName string `json:"displayName,omitempty"`
	// Scope selects whether the roles apply platform-wide or to one tenant.
	Scope AccessScope `json:"scope"`
	// TenantRef identifies the target tenant when scope=tenant.
	TenantRef *LocalObjectReference `json:"tenantRef,omitempty"`
	// Subjects lists the Users and Groups granted the roles.
	Subjects []SubjectReference `json:"subjects"`
	// Roles lists the coarse-grained Servicer roles granted by this binding.
	Roles []ServicerRole `json:"roles"`
}

// RoleBindingStatus defines the observed state of a RoleBinding.
type RoleBindingStatus struct {
	// ObservedGeneration is the most recent processed generation.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// Conditions contains current validation and readiness conditions.
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,shortName=srb
// +kubebuilder:printcolumn:name="Scope",type=string,JSONPath=`.spec.scope`

// RoleBinding defines one Servicer authorization binding.
type RoleBinding struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   RoleBindingSpec   `json:"spec,omitempty"`
	Status RoleBindingStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// RoleBindingList contains a list of RoleBinding resources.
type RoleBindingList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []RoleBinding `json:"items"`
}

func init() {
	SchemeBuilder.Register(&RoleBinding{}, &RoleBindingList{})
}
