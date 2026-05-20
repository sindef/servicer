package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// ServiceBindingSpec defines the desired state of a ServiceBinding.
type ServiceBindingSpec struct {
	// ProjectRef references the owning project.
	ProjectRef LocalObjectReference `json:"projectRef"`
	// SourceRef identifies the service instance that will publish credentials.
	SourceRef TypedObjectReference `json:"sourceRef"`
	// TargetRef identifies the workload or namespace that will consume the binding.
	TargetRef TypedObjectReference `json:"targetRef"`
	// SecretPolicy defines how bound credentials are delivered.
	SecretPolicy SecretPolicySpec `json:"secretPolicy"`
}

// ServiceBindingStatus defines the observed state of a ServiceBinding.
type ServiceBindingStatus struct {
	// ObservedGeneration is the most recent processed generation.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// Phase is the coarse lifecycle summary.
	Phase string `json:"phase,omitempty"`
	// Conditions contains the current status conditions.
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	// CredentialRefs identifies the published binding credentials or projected secrets.
	CredentialRefs []NamespacedObjectReference `json:"credentialRefs,omitempty"`
	// Health summarizes the runtime condition.
	Health HealthStatus `json:"health,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,shortName=svcbind
// +kubebuilder:printcolumn:name="Project",type=string,JSONPath=`.spec.projectRef.name`
// +kubebuilder:printcolumn:name="Source",type=string,JSONPath=`.spec.sourceRef.name`
// +kubebuilder:printcolumn:name="Target",type=string,JSONPath=`.spec.targetRef.name`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`

// ServiceBinding is the operator-neutral API for credential sharing between products and consumers.
type ServiceBinding struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ServiceBindingSpec   `json:"spec,omitempty"`
	Status ServiceBindingStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ServiceBindingList contains a list of ServiceBinding resources.
type ServiceBindingList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ServiceBinding `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ServiceBinding{}, &ServiceBindingList{})
}
