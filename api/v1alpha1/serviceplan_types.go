package v1alpha1

import (
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ServicePlanSpec defines the desired state of a ServicePlan.
type ServicePlanSpec struct {
	// ServiceClassRef references the parent service class for the plan.
	ServiceClassRef LocalObjectReference `json:"serviceClassRef"`
	// DisplayName is the human-readable name for the plan.
	// +kubebuilder:validation:MinLength=1
	DisplayName string `json:"displayName"`
	// Tier identifies the commercial or operational tier such as starter or ha.
	Tier string `json:"tier,omitempty"`
	// Topology identifies the intended runtime topology such as single-primary or high-availability.
	Topology string `json:"topology,omitempty"`
	// DefaultVersion identifies the default engine version for this plan.
	DefaultVersion string `json:"defaultVersion,omitempty"`
	// AllowsVersionOverride indicates whether instances may override the plan's version.
	AllowsVersionOverride bool `json:"allowsVersionOverride,omitempty"`
	// DefaultParameters provides plan-scoped default parameters.
	// +kubebuilder:pruning:PreserveUnknownFields
	DefaultParameters *apiextensionsv1.JSON `json:"defaultParameters,omitempty"`
	// Constraints captures plan-specific validation constraints.
	// +kubebuilder:pruning:PreserveUnknownFields
	Constraints *apiextensionsv1.JSON `json:"constraints,omitempty"`
	// PolicyRefs references policy bundles applied by default to instances of this plan.
	PolicyRefs []PolicyReference `json:"policyRefs,omitempty"`
}

// ServicePlanStatus defines the observed state of a ServicePlan.
type ServicePlanStatus struct {
	// ObservedGeneration is the most recent processed generation.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// Phase is the coarse lifecycle summary.
	Phase string `json:"phase,omitempty"`
	// Conditions contains the current status conditions.
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	// Published is the observed publication state of the plan.
	Published bool `json:"published,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,shortName=splan
// +kubebuilder:printcolumn:name="Class",type=string,JSONPath=`.spec.serviceClassRef.name`
// +kubebuilder:printcolumn:name="Topology",type=string,JSONPath=`.spec.topology`
// +kubebuilder:printcolumn:name="Published",type=boolean,JSONPath=`.status.published`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`

// ServicePlan defines a curated plan for a service class.
type ServicePlan struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ServicePlanSpec   `json:"spec,omitempty"`
	Status ServicePlanStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ServicePlanList contains a list of ServicePlan resources.
type ServicePlanList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ServicePlan `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ServicePlan{}, &ServicePlanList{})
}
