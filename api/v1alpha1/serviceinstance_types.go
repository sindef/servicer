package v1alpha1

import (
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ServiceInstanceSpec defines the desired state of a ServiceInstance.
type ServiceInstanceSpec struct {
	// ProjectRef references the owning project.
	ProjectRef LocalObjectReference `json:"projectRef"`
	// ServiceClassRef references the selected service class.
	ServiceClassRef LocalObjectReference `json:"serviceClassRef"`
	// ServicePlanRef references the selected service plan.
	ServicePlanRef LocalObjectReference `json:"servicePlanRef"`
	// Version optionally overrides the engine version when allowed.
	Version string `json:"version,omitempty"`
	// Parameters holds adapter-specific configuration.
	// +kubebuilder:pruning:PreserveUnknownFields
	Parameters *apiextensionsv1.JSON `json:"parameters,omitempty"`
	// Exposure defines how consumers reach the service.
	Exposure ExposureSpec `json:"exposure"`
	// MaintenancePolicy references the desired maintenance behavior.
	MaintenancePolicy MaintenancePolicySpec `json:"maintenancePolicy,omitempty"`
	// SecretPolicy defines how credentials are delivered.
	SecretPolicy SecretPolicySpec `json:"secretPolicy"`
	// DeletionPolicy defines how the instance should be removed.
	DeletionPolicy DeletionPolicy `json:"deletionPolicy,omitempty"`
}

// ServiceInstanceStatus defines the observed state of a ServiceInstance.
type ServiceInstanceStatus struct {
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
	// CacheTopology captures operator-neutral cache topology state.
	CacheTopology CacheTopologyStatus `json:"cacheTopology,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,shortName=svcinst
// +kubebuilder:printcolumn:name="Project",type=string,JSONPath=`.spec.projectRef.name`
// +kubebuilder:printcolumn:name="Class",type=string,JSONPath=`.spec.serviceClassRef.name`
// +kubebuilder:printcolumn:name="Plan",type=string,JSONPath=`.spec.servicePlanRef.name`
// +kubebuilder:printcolumn:name="Cluster",type=string,JSONPath=`.status.placement.clusterName`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`

// ServiceInstance is the common product API for operator-backed services.
type ServiceInstance struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ServiceInstanceSpec   `json:"spec,omitempty"`
	Status ServiceInstanceStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ServiceInstanceList contains a list of ServiceInstance resources.
type ServiceInstanceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ServiceInstance `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ServiceInstance{}, &ServiceInstanceList{})
}
