package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// TenantSpec defines the desired state of a Tenant.
type TenantSpec struct {
	// DisplayName is the human-readable name for the tenant.
	// +kubebuilder:validation:MinLength=1
	DisplayName string `json:"displayName"`
	// Owners defines the ownership boundary for the tenant.
	Owners OwnersSpec `json:"owners"`
	// QuotaProfileRef references a curated quota profile.
	QuotaProfileRef LocalObjectReference `json:"quotaProfileRef"`
	// AllowedServiceClasses lists the service classes this tenant may request.
	// +kubebuilder:validation:MinItems=1
	AllowedServiceClasses []string `json:"allowedServiceClasses"`
	// PolicyRefs references policy bundles applied to child resources.
	PolicyRefs []PolicyReference `json:"policyRefs,omitempty"`
	// Contacts provides operational contact paths for the tenant.
	Contacts []ContactSpec `json:"contacts,omitempty"`
	// Lifecycle captures the desired lifecycle state.
	Lifecycle TenantLifecycleSpec `json:"lifecycle"`
}

// TenantStatus defines the observed state of a Tenant.
type TenantStatus struct {
	// ObservedGeneration is the most recent processed generation.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// Phase is the coarse lifecycle summary.
	Phase string `json:"phase,omitempty"`
	// Conditions contains the current status conditions.
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	// UsageSummary aggregates child project and service counts.
	UsageSummary UsageSummary `json:"usageSummary,omitempty"`
	// EffectivePolicies lists the resolved policy bundles.
	EffectivePolicies []string `json:"effectivePolicies,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,shortName=ten
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Projects",type=integer,JSONPath=`.status.usageSummary.projects`
// +kubebuilder:printcolumn:name="Services",type=integer,JSONPath=`.status.usageSummary.serviceInstances`

// Tenant is the top-level tenancy boundary for the Servicer platform.
type Tenant struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TenantSpec   `json:"spec,omitempty"`
	Status TenantStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// TenantList contains a list of Tenant resources.
type TenantList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Tenant `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Tenant{}, &TenantList{})
}
