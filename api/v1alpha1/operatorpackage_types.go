package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// OperatorPackageSource describes where an operator's manifests live for ArgoCD delivery.
type OperatorPackageSource struct {
	// RepoURL is the Git repository URL ArgoCD should use for this operator.
	RepoURL string `json:"repoURL"`
	// Path is the directory within the repository containing the operator manifests or kustomize overlay.
	Path string `json:"path"`
	// TargetRevision is the branch, tag, or commit SHA to track. Defaults to HEAD.
	// +kubebuilder:default=HEAD
	TargetRevision string `json:"targetRevision,omitempty"`
}

// OperatorProbe defines a CRD whose presence indicates the operator is installed.
type OperatorProbe struct {
	// CRD is the fully-qualified CustomResourceDefinition name to probe for.
	// Example: "clusters.postgresql.cnpg.io"
	CRD string `json:"crd"`
}

// OperatorPackageSpec defines the desired state of an OperatorPackage.
type OperatorPackageSpec struct {
	// DisplayName is the human-readable name for this operator package.
	DisplayName string `json:"displayName"`
	// Description provides a longer description of what this operator does.
	Description string `json:"description,omitempty"`
	// Version is the operator package version label.
	Version string `json:"version,omitempty"`
	// TargetNamespace is the namespace the operator should be installed into on each cluster.
	// +kubebuilder:default=operators
	TargetNamespace string `json:"targetNamespace,omitempty"`
	// Probes lists CRD names whose presence indicates the operator is already installed.
	// All probes must pass for the operator to be considered installed.
	// +kubebuilder:validation:MinItems=1
	Probes []OperatorProbe `json:"probes"`
	// Source defines where the operator manifests live for ArgoCD delivery.
	Source OperatorPackageSource `json:"source"`
}

// OperatorPackageStatus defines the observed state of an OperatorPackage.
type OperatorPackageStatus struct {
	// ObservedGeneration is the most recently processed generation.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// Conditions contains the current status conditions.
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,shortName=oppkg
// +kubebuilder:printcolumn:name="Display Name",type=string,JSONPath=`.spec.displayName`
// +kubebuilder:printcolumn:name="Version",type=string,JSONPath=`.spec.version`
// +kubebuilder:printcolumn:name="Namespace",type=string,JSONPath=`.spec.targetNamespace`

// OperatorPackage is a cluster-scoped catalog entry describing an operator that
// the platform can ensure is installed on ClusterTargets that require it.
type OperatorPackage struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   OperatorPackageSpec   `json:"spec,omitempty"`
	Status OperatorPackageStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// OperatorPackageList contains a list of OperatorPackage resources.
type OperatorPackageList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []OperatorPackage `json:"items"`
}

func init() {
	SchemeBuilder.Register(&OperatorPackage{}, &OperatorPackageList{})
}
