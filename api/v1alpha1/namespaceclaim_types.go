package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// NamespaceClaimSpec defines the desired state of a NamespaceClaim.
type NamespaceClaimSpec struct {
	// ProjectRef references the owning project.
	ProjectRef LocalObjectReference `json:"projectRef"`
	// DisplayName is the human-readable name for the requested namespace.
	DisplayName string `json:"displayName,omitempty"`
	// Quotas captures namespace-local resource quota intent.
	Quotas map[string]string `json:"quotas,omitempty"`
	// Labels are propagated to the generated namespace.
	Labels map[string]string `json:"labels,omitempty"`
	// DeletionPolicy defines how the namespace should be removed.
	DeletionPolicy DeletionPolicy `json:"deletionPolicy,omitempty"`
}

// NamespaceClaimStatus defines the observed state of a NamespaceClaim.
type NamespaceClaimStatus struct {
	// ObservedGeneration is the most recent processed generation.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// Phase is the coarse lifecycle summary.
	Phase string `json:"phase,omitempty"`
	// Conditions contains the current status conditions.
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	// Placement captures the resolved placement state.
	Placement PlacementStatus `json:"placement,omitempty"`
	// Artifact captures derived delivery metadata.
	Artifact ArtifactStatus `json:"artifact,omitempty"`
	// Sync summarizes readiness for Argo CD delivery.
	Sync DeliverySyncStatus `json:"sync,omitempty"`
	// Health summarizes the runtime condition.
	Health HealthStatus `json:"health,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,shortName=nsclaim
// +kubebuilder:printcolumn:name="Project",type=string,JSONPath=`.spec.projectRef.name`
// +kubebuilder:printcolumn:name="Cluster",type=string,JSONPath=`.status.placement.clusterName`
// +kubebuilder:printcolumn:name="Namespace",type=string,JSONPath=`.status.placement.namespace`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`

// NamespaceClaim is a first-class namespace product request.
type NamespaceClaim struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   NamespaceClaimSpec   `json:"spec,omitempty"`
	Status NamespaceClaimStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// NamespaceClaimList contains a list of NamespaceClaim resources.
type NamespaceClaimList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []NamespaceClaim `json:"items"`
}

func init() {
	SchemeBuilder.Register(&NamespaceClaim{}, &NamespaceClaimList{})
}
