package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// ClusterMesh identifies the cross-cluster networking fabric declared on a ClusterTarget.
// Topologies that require pod-to-pod reachability across clusters (e.g. Galera, Valkey Sentinel)
// check this capability before provisioning.
type ClusterMesh string

const (
	// ClusterMeshCalico indicates Calico BGP or WireGuard cross-cluster overlay is available.
	ClusterMeshCalico ClusterMesh = "calico"
	// ClusterMeshCilium indicates Cilium Cluster Mesh is configured on this cluster.
	ClusterMeshCilium ClusterMesh = "cilium"
	// ClusterMeshIstio indicates an Istio east-west gateway provides cross-cluster pod routing.
	ClusterMeshIstio ClusterMesh = "istio"
	// ClusterMeshSubmariner indicates Submariner manages cross-cluster pod-level connectivity.
	ClusterMeshSubmariner ClusterMesh = "submariner"
	// ClusterMeshNone indicates no cross-cluster pod networking fabric is present.
	ClusterMeshNone ClusterMesh = "none"
)

// ClusterTargetSpec defines the desired state of a ClusterTarget.
type ClusterTargetSpec struct {
	// DisplayName is the human-readable name for the cluster target.
	// +kubebuilder:validation:MinLength=1
	DisplayName string `json:"displayName"`
	// ConnectionRef identifies the Kubernetes Secret holding cluster access credentials.
	ConnectionRef NamespacedObjectReference `json:"connectionRef"`
	// Region identifies the logical region or site of the cluster.
	Region string `json:"region,omitempty"`
	// Capabilities describes cluster capabilities used for placement decisions.
	Capabilities map[string]string `json:"capabilities,omitempty"`
	// DefaultPolicyRefs references policy bundles applied by default to products placed here.
	DefaultPolicyRefs []PolicyReference `json:"defaultPolicyRefs,omitempty"`
	// IngressDomain identifies the default external DNS suffix for products on this cluster.
	IngressDomain string `json:"ingressDomain,omitempty"`
	// RequiredPackages lists OperatorPackage names that the platform should ensure are
	// installed on this cluster. The ClusterTargetReconciler probes for each package and
	// creates an Argo CD Application to install any that are missing.
	RequiredPackages []string `json:"requiredPackages,omitempty"`
}

// Mesh returns the cross-cluster networking fabric declared in Capabilities["mesh"].
// Returns ClusterMeshNone when the capability is absent or empty.
func (s *ClusterTargetSpec) Mesh() ClusterMesh {
	if v, ok := s.Capabilities["mesh"]; ok && v != "" {
		return ClusterMesh(v)
	}
	return ClusterMeshNone
}

// PackagePhase describes the installation phase of an operator package on a cluster.
type PackagePhase string

const (
	// PackagePhaseInstalled means all CRD probes passed — the operator is running.
	PackagePhaseInstalled PackagePhase = "Installed"
	// PackagePhaseDeploying means an Argo CD Application was created but CRD probes have not yet passed.
	PackagePhaseDeploying PackagePhase = "Deploying"
	// PackagePhaseMissing means the operator is not installed and no delivery mechanism is configured.
	PackagePhaseMissing PackagePhase = "Missing"
	// PackagePhaseError means the reconciler encountered an error probing or installing the package.
	PackagePhaseError PackagePhase = "Error"
)

// PackageStatus describes the observed installation state of one OperatorPackage on this cluster.
type PackageStatus struct {
	// Name is the OperatorPackage resource name.
	Name string `json:"name"`
	// Phase is the coarse lifecycle state of this package on the cluster.
	Phase PackagePhase `json:"phase"`
	// Message is a human-readable explanation of the current phase.
	Message string `json:"message,omitempty"`
	// ArgoAppName is the name of the Argo CD Application created to install this package, if any.
	ArgoAppName string `json:"argoAppName,omitempty"`
	// LastProbeTime is when the CRD probes were last evaluated.
	LastProbeTime *metav1.Time `json:"lastProbeTime,omitempty"`
}

// ClusterTargetStatus defines the observed state of a ClusterTarget.
type ClusterTargetStatus struct {
	// ObservedGeneration is the most recent processed generation.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// Phase is the coarse lifecycle summary.
	Phase string `json:"phase,omitempty"`
	// Conditions contains the current status conditions.
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	// Reachable indicates whether the cluster is currently reachable.
	Reachable bool `json:"reachable,omitempty"`
	// OperatorInventory lists the discovered operators available on the target cluster.
	OperatorInventory []string `json:"operatorInventory,omitempty"`
	// Packages lists the observed installation state of each required OperatorPackage.
	Packages []PackageStatus `json:"packages,omitempty"`
	// KubernetesVersion records the discovered Kubernetes server version.
	KubernetesVersion string `json:"kubernetesVersion,omitempty"`
	// LastValidatedAt records the last successful connectivity or capability validation time.
	LastValidatedAt *metav1.Time `json:"lastValidatedAt,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,shortName=ctarget
// +kubebuilder:printcolumn:name="Region",type=string,JSONPath=`.spec.region`
// +kubebuilder:printcolumn:name="Reachable",type=boolean,JSONPath=`.status.reachable`
// +kubebuilder:printcolumn:name="Version",type=string,JSONPath=`.status.kubernetesVersion`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`

// ClusterTarget represents a workload or management cluster the platform can place products onto.
type ClusterTarget struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ClusterTargetSpec   `json:"spec,omitempty"`
	Status ClusterTargetStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ClusterTargetList contains a list of ClusterTarget resources.
type ClusterTargetList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ClusterTarget `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ClusterTarget{}, &ClusterTargetList{})
}
