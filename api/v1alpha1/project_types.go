package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// ProjectSpec defines the desired state of a Project.
type ProjectSpec struct {
	// TenantRef references the owning tenant.
	TenantRef LocalObjectReference `json:"tenantRef"`
	// DisplayName is the human-readable name for the project.
	// +kubebuilder:validation:MinLength=1
	DisplayName string `json:"displayName"`
	// Environment identifies the operational environment for the project.
	Environment EnvironmentType `json:"environment"`
	// TargetSelector describes explicit or rule-based cluster placement intent.
	TargetSelector TargetSelectorSpec `json:"targetSelector,omitempty"`
	// NamespaceStrategy describes how namespaces are allocated for the project.
	NamespaceStrategy NamespaceStrategySpec `json:"namespaceStrategy"`
	// Quotas captures project-local soft limits.
	Quotas ProjectQuotasSpec `json:"quotas,omitempty"`
	// Labels are propagated into generated assets and runtime metadata.
	Labels map[string]string `json:"labels,omitempty"`
	// PolicyRefs references project-specific policy bundles.
	PolicyRefs []PolicyReference `json:"policyRefs,omitempty"`
}

// ProjectStatus defines the observed state of a Project.
type ProjectStatus struct {
	// ObservedGeneration is the most recent processed generation.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// Phase is the coarse lifecycle summary.
	Phase string `json:"phase,omitempty"`
	// Conditions contains the current status conditions.
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	// Placement captures the resolved placement state.
	Placement PlacementStatus `json:"placement,omitempty"`
	// UsageSummary aggregates child namespace and service counts.
	UsageSummary UsageSummary `json:"usageSummary,omitempty"`
	// EffectiveQuota captures the resolved quota values after inheritance.
	EffectiveQuota ProjectQuotasSpec `json:"effectiveQuota,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,shortName=proj
// +kubebuilder:printcolumn:name="Tenant",type=string,JSONPath=`.spec.tenantRef.name`
// +kubebuilder:printcolumn:name="Environment",type=string,JSONPath=`.spec.environment`
// +kubebuilder:printcolumn:name="Cluster",type=string,JSONPath=`.status.placement.clusterName`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`

// Project is the primary deployment and environment boundary within a tenant.
type Project struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ProjectSpec   `json:"spec,omitempty"`
	Status ProjectStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ProjectList contains a list of Project resources.
type ProjectList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Project `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Project{}, &ProjectList{})
}
