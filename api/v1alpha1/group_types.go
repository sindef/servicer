package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// GroupMemberSpec identifies a Servicer user that belongs to a group.
type GroupMemberSpec struct {
	// UserRef identifies the local Servicer user.
	UserRef LocalObjectReference `json:"userRef"`
}

// ExternalGroupSpec maps a Servicer group to an upstream provider group.
type ExternalGroupSpec struct {
	// ProviderRef identifies the upstream auth provider.
	ProviderRef LocalObjectReference `json:"providerRef"`
	// Name is the external group name as asserted by the provider.
	Name string `json:"name"`
}

// GroupSpec defines one Servicer identity group.
type GroupSpec struct {
	// DisplayName is the human-readable name.
	DisplayName string `json:"displayName,omitempty"`
	// Members lists local Servicer users that belong to the group.
	Members []GroupMemberSpec `json:"members,omitempty"`
	// ExternalGroups maps upstream provider groups onto this local group.
	ExternalGroups []ExternalGroupSpec `json:"externalGroups,omitempty"`
}

// GroupStatus defines the observed state of a Group.
type GroupStatus struct {
	// ObservedGeneration is the most recent processed generation.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// Conditions contains current validation and readiness conditions.
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,shortName=grp

// Group defines a reusable identity group for authorization.
type Group struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   GroupSpec   `json:"spec,omitempty"`
	Status GroupStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// GroupList contains a list of Group resources.
type GroupList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Group `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Group{}, &GroupList{})
}
