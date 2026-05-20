package v1alpha1

import (
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ServiceClassSpec defines the desired state of a ServiceClass.
type ServiceClassSpec struct {
	// DisplayName is the human-readable name for the service class.
	// +kubebuilder:validation:MinLength=1
	DisplayName string `json:"displayName"`
	// Category groups related product classes such as database or messaging.
	Category string `json:"category,omitempty"`
	// Driver identifies the intended runtime driver such as cnpg or k8ssandra.
	// +kubebuilder:validation:MinLength=1
	Driver string `json:"driver"`
	// SupportedVersions lists the engine versions supported by the class.
	SupportedVersions []string `json:"supportedVersions,omitempty"`
	// CapabilityFlags describes optional behaviors the class supports.
	CapabilityFlags []string `json:"capabilityFlags,omitempty"`
	// AllowsVersionOverride indicates whether instances may override the default version.
	AllowsVersionOverride bool `json:"allowsVersionOverride,omitempty"`
	// DefaultParameters provides class-wide default parameters.
	// +kubebuilder:pruning:PreserveUnknownFields
	DefaultParameters *apiextensionsv1.JSON `json:"defaultParameters,omitempty"`
	// Published indicates whether the class should be visible in the product catalog.
	Published bool `json:"published,omitempty"`
}

// ServiceClassStatus defines the observed state of a ServiceClass.
type ServiceClassStatus struct {
	// ObservedGeneration is the most recent processed generation.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// Phase is the coarse lifecycle summary.
	Phase string `json:"phase,omitempty"`
	// Conditions contains the current status conditions.
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	// Published is the observed publication state of the class.
	Published bool `json:"published,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,shortName=sclass
// +kubebuilder:printcolumn:name="Driver",type=string,JSONPath=`.spec.driver`
// +kubebuilder:printcolumn:name="Published",type=boolean,JSONPath=`.status.published`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`

// ServiceClass defines a product class that Servicer can offer.
type ServiceClass struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ServiceClassSpec   `json:"spec,omitempty"`
	Status ServiceClassStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ServiceClassList contains a list of ServiceClass resources.
type ServiceClassList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ServiceClass `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ServiceClass{}, &ServiceClassList{})
}
