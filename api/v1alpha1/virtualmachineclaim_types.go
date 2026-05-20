package v1alpha1

import (
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// VirtualMachineClaimSpec defines the desired state of a VirtualMachineClaim.
type VirtualMachineClaimSpec struct {
	// ProjectRef references the owning project.
	ProjectRef LocalObjectReference `json:"projectRef"`
	// Class identifies the curated VM product class.
	Class string `json:"class"`
	// Image identifies the requested guest image or template.
	Image string `json:"image"`
	// PowerState captures the desired machine power posture.
	PowerState string `json:"powerState,omitempty"`
	// Parameters holds platform-specific VM configuration.
	// +kubebuilder:pruning:PreserveUnknownFields
	Parameters *apiextensionsv1.JSON `json:"parameters,omitempty"`
	// Exposure defines how consumers reach the virtual machine.
	Exposure ExposureSpec `json:"exposure"`
	// SecretPolicy defines how credentials are delivered.
	SecretPolicy SecretPolicySpec `json:"secretPolicy"`
	// DeletionPolicy defines how the virtual machine should be removed.
	DeletionPolicy DeletionPolicy `json:"deletionPolicy,omitempty"`
}

// VirtualMachineClaimStatus defines the observed state of a VirtualMachineClaim.
type VirtualMachineClaimStatus struct {
	// ObservedGeneration is the most recent processed generation.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// Phase is the coarse lifecycle summary.
	Phase string `json:"phase,omitempty"`
	// Conditions contains the current status conditions.
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	// Placement captures the resolved placement state.
	Placement PlacementStatus `json:"placement,omitempty"`
	// Runtime identifies the backing runtime object.
	Runtime RuntimeStatus `json:"runtime,omitempty"`
	// Endpoints summarizes the known consumer endpoints.
	Endpoints map[string]string `json:"endpoints,omitempty"`
	// CredentialRefs identifies related Kubernetes Secrets.
	CredentialRefs []NamespacedObjectReference `json:"credentialRefs,omitempty"`
	// Artifact captures derived delivery metadata.
	Artifact ArtifactStatus `json:"artifact,omitempty"`
	// Sync summarizes readiness for Argo CD delivery.
	Sync DeliverySyncStatus `json:"sync,omitempty"`
	// Health summarizes the runtime condition.
	Health HealthStatus `json:"health,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,shortName=vmclaim
// +kubebuilder:printcolumn:name="Project",type=string,JSONPath=`.spec.projectRef.name`
// +kubebuilder:printcolumn:name="Class",type=string,JSONPath=`.spec.class`
// +kubebuilder:printcolumn:name="Image",type=string,JSONPath=`.spec.image`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`

// VirtualMachineClaim is the first-class API for curated virtual machine products.
type VirtualMachineClaim struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   VirtualMachineClaimSpec   `json:"spec,omitempty"`
	Status VirtualMachineClaimStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// VirtualMachineClaimList contains a list of VirtualMachineClaim resources.
type VirtualMachineClaimList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []VirtualMachineClaim `json:"items"`
}

func init() {
	SchemeBuilder.Register(&VirtualMachineClaim{}, &VirtualMachineClaimList{})
}
