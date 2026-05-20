package v1alpha1

import (
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// LocalObjectReference identifies another Servicer object by name.
type LocalObjectReference struct {
	// Name is the stable resource name.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
}

// NamespacedObjectReference identifies a resource in a Kubernetes namespace.
type NamespacedObjectReference struct {
	// Name is the resource name.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
	// Namespace is the resource namespace.
	// +kubebuilder:validation:MinLength=1
	Namespace string `json:"namespace"`
}

// TypedObjectReference identifies a typed Kubernetes object.
type TypedObjectReference struct {
	// APIVersion is the target object's apiVersion.
	// +kubebuilder:validation:MinLength=1
	APIVersion string `json:"apiVersion"`
	// Kind is the target object's kind.
	// +kubebuilder:validation:MinLength=1
	Kind string `json:"kind"`
	// Name is the target object's name.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
	// Namespace is optional for namespaced targets.
	Namespace string `json:"namespace,omitempty"`
}

// PolicyReference points to a policy bundle or policy object.
type PolicyReference struct {
	// Name identifies the referenced policy bundle.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
}

// ContactSpec captures an operational contact path for a tenant.
type ContactSpec struct {
	// Type describes the contact medium such as slack or email.
	// +kubebuilder:validation:MinLength=1
	Type string `json:"type"`
	// Value contains the contact target.
	// +kubebuilder:validation:MinLength=1
	Value string `json:"value"`
}

// OwnersSpec describes the human ownership boundary for a tenant.
type OwnersSpec struct {
	// Users are direct user identities with ownership rights.
	Users []string `json:"users,omitempty"`
	// Groups are identity-provider groups with ownership rights.
	Groups []string `json:"groups,omitempty"`
}

// TenantLifecyclePhase is the desired lifecycle phase for a tenant.
// +kubebuilder:validation:Enum=active;suspended;deleting
type TenantLifecyclePhase string

const (
	TenantLifecyclePhaseActive    TenantLifecyclePhase = "active"
	TenantLifecyclePhaseSuspended TenantLifecyclePhase = "suspended"
	TenantLifecyclePhaseDeleting  TenantLifecyclePhase = "deleting"
)

// TenantLifecycleSpec captures the requested lifecycle state for a tenant.
type TenantLifecycleSpec struct {
	// Phase is the desired lifecycle phase.
	Phase TenantLifecyclePhase `json:"phase"`
}

// EnvironmentType identifies the operational environment for a project.
// +kubebuilder:validation:Enum=development;staging;production;sandbox
type EnvironmentType string

const (
	EnvironmentDevelopment EnvironmentType = "development"
	EnvironmentStaging     EnvironmentType = "staging"
	EnvironmentProduction  EnvironmentType = "production"
	EnvironmentSandbox     EnvironmentType = "sandbox"
)

// NamespaceStrategyMode identifies how namespaces are allocated for a project.
// +kubebuilder:validation:Enum=dedicated;shared;bring-your-own
type NamespaceStrategyMode string

const (
	NamespaceStrategyDedicated    NamespaceStrategyMode = "dedicated"
	NamespaceStrategyShared       NamespaceStrategyMode = "shared"
	NamespaceStrategyBringYourOwn NamespaceStrategyMode = "bring-your-own"
)

// NamespaceStrategySpec describes how a project acquires namespaces.
type NamespaceStrategySpec struct {
	// Mode selects the namespace allocation pattern.
	Mode NamespaceStrategyMode `json:"mode"`
	// Prefix is the prefix used for generated namespaces.
	Prefix string `json:"prefix,omitempty"`
}

// TargetSelectorSpec describes explicit or label-based cluster placement intent.
type TargetSelectorSpec struct {
	// ClusterRef pins the project to a specific target cluster.
	ClusterRef *LocalObjectReference `json:"clusterRef,omitempty"`
	// MatchLabels selects a cluster by capability or policy labels.
	MatchLabels map[string]string `json:"matchLabels,omitempty"`
}

// ProjectQuotasSpec defines project-local soft limits.
type ProjectQuotasSpec struct {
	// MaxServices limits the number of child ServiceInstances.
	MaxServices *int32 `json:"maxServices,omitempty"`
	// MaxNamespaces limits the number of namespaces the project may own.
	MaxNamespaces *int32 `json:"maxNamespaces,omitempty"`
}

// ExposureMode identifies how a service is exposed to consumers.
// +kubebuilder:validation:Enum=cluster-internal;private-ingress;public-ingress
type ExposureMode string

const (
	ExposureModeClusterInternal ExposureMode = "cluster-internal"
	ExposureModePrivateIngress  ExposureMode = "private-ingress"
	ExposureModePublicIngress   ExposureMode = "public-ingress"
)

// ExposureSpec defines how consumers reach a service instance.
type ExposureSpec struct {
	// Mode is the selected exposure pattern.
	Mode ExposureMode `json:"mode"`
}

// SecretDeliveryMode identifies how credentials are delivered.
// +kubebuilder:validation:Enum=external-secret;direct-secret-ref;manual
type SecretDeliveryMode string

const (
	SecretDeliveryModeExternalSecret  SecretDeliveryMode = "external-secret"
	SecretDeliveryModeDirectSecretRef SecretDeliveryMode = "direct-secret-ref"
	SecretDeliveryModeManual          SecretDeliveryMode = "manual"
)

// SecretPolicySpec defines how credentials are projected to consumers.
type SecretPolicySpec struct {
	// DeliveryMode selects the secret delivery pattern.
	DeliveryMode SecretDeliveryMode `json:"deliveryMode"`
	// ExternalSecretProvider selects the backing provider when deliveryMode=external-secret.
	ExternalSecretProvider ExternalSecretProviderType `json:"externalSecretProvider,omitempty"`
	// Vault configures Vault-backed projection when externalSecretProvider=vault.
	Vault *VaultSecretProviderSpec `json:"vault,omitempty"`
}

// ExternalSecretProviderType identifies which provider ESO should use.
// +kubebuilder:validation:Enum=kubernetes;vault
type ExternalSecretProviderType string

const (
	ExternalSecretProviderKubernetes ExternalSecretProviderType = "kubernetes"
	ExternalSecretProviderVault      ExternalSecretProviderType = "vault"
)

// VaultSecretProviderSpec describes a Vault provider configuration for ESO projection.
type VaultSecretProviderSpec struct {
	// Server is the Vault API base URL.
	Server string `json:"server,omitempty"`
	// Path is the Vault mount path that contains the credential secret.
	Path string `json:"path,omitempty"`
	// Version is the KV engine version used by the mount.
	// +kubebuilder:validation:Enum=v1;v2
	Version string `json:"version,omitempty"`
	// AuthSecretRef points at the Secret containing the Vault token.
	AuthSecretRef NamespacedObjectReference `json:"authSecretRef,omitempty"`
}

// DeletionPolicy identifies how a service instance should be removed.
// +kubebuilder:validation:Enum=delete;orphan;snapshot
type DeletionPolicy string

const (
	DeletionPolicyDelete   DeletionPolicy = "delete"
	DeletionPolicyOrphan   DeletionPolicy = "orphan"
	DeletionPolicySnapshot DeletionPolicy = "snapshot"
)

// MaintenancePolicySpec references a maintenance window or policy.
type MaintenancePolicySpec struct {
	// WindowRef points to a named maintenance window.
	WindowRef *LocalObjectReference `json:"windowRef,omitempty"`
}

// PlacementStatus captures the resolved cluster and namespace placement.
type PlacementStatus struct {
	// ClusterName is the resolved target cluster.
	ClusterName string `json:"clusterName,omitempty"`
	// Namespace is the resolved target namespace when relevant.
	Namespace string `json:"namespace,omitempty"`
}

// ArtifactStatus captures derived-delivery metadata.
type ArtifactStatus struct {
	// Revision is the generated artifact or repo revision currently materialized.
	Revision string `json:"revision,omitempty"`
	// Path is the repository-relative package path for this instance.
	Path string `json:"path,omitempty"`
	// Count is the number of artifacts in the materialized package.
	Count int32 `json:"count,omitempty"`
	// Artifacts lists the deterministic files written for delivery.
	Artifacts []MaterializedArtifactStatus `json:"artifacts,omitempty"`
}

// MaterializedArtifactStatus identifies one generated delivery artifact.
type MaterializedArtifactStatus struct {
	// Path is the repository-relative artifact path.
	Path string `json:"path,omitempty"`
	// Digest is the sha256 digest of the artifact content.
	Digest string `json:"digest,omitempty"`
}

// DeliverySyncStatus summarizes readiness for Argo CD or another delivery reconciler.
type DeliverySyncStatus struct {
	// Phase captures whether generated artifacts are ready, synced, or blocked.
	Phase string `json:"phase,omitempty"`
	// ApplicationName identifies the future or current Argo CD Application.
	ApplicationName string `json:"applicationName,omitempty"`
	// Message summarizes the current sync handoff state.
	Message string `json:"message,omitempty"`
}

// RuntimeStatus identifies the primary runtime resource backing an instance.
type RuntimeStatus struct {
	// Driver is the selected runtime adapter, such as cnpg or servicer-valkey.
	Driver string `json:"driver,omitempty"`
	// ObjectRef points to the primary runtime object.
	ObjectRef *TypedObjectReference `json:"objectRef,omitempty"`
}

// HealthStatus provides a human-readable operational summary.
type HealthStatus struct {
	// Summary is the high-level health summary.
	Summary string `json:"summary,omitempty"`
}

// CacheTopologyStatus captures operator-neutral cache topology state.
type CacheTopologyStatus struct {
	// Mode identifies the cache topology such as single-node, replicated, or multi-cluster-failover.
	Mode string `json:"mode,omitempty"`
	// PrimaryCluster is the current writable cluster for failover-oriented cache topologies.
	PrimaryCluster string `json:"primaryCluster,omitempty"`
	// StandbyClusters summarizes standby cluster health and promotion posture.
	StandbyClusters []CacheStandbyStatus `json:"standbyClusters,omitempty"`
	// TrafficEndpoint is the stable endpoint that should route to the current writable primary.
	TrafficEndpoint string `json:"trafficEndpoint,omitempty"`
	// TrafficPolicyRef points at the generated traffic policy intent for delivery controllers.
	TrafficPolicyRef *TypedObjectReference `json:"trafficPolicyRef,omitempty"`
	// FailoverReadiness summarizes whether a controlled failover can be attempted.
	FailoverReadiness string `json:"failoverReadiness,omitempty"`
	// Message summarizes the current topology posture.
	Message string `json:"message,omitempty"`
}

// CacheStandbyStatus captures one standby cluster in a failover-oriented cache topology.
type CacheStandbyStatus struct {
	// ClusterName identifies the standby cluster.
	ClusterName string `json:"clusterName,omitempty"`
	// Ready indicates whether this standby passes current promotion preflight checks.
	Ready bool `json:"ready,omitempty"`
	// ResyncRequired indicates that the standby must be rebuilt from the active primary before promotion.
	ResyncRequired bool `json:"resyncRequired,omitempty"`
	// LagObserved indicates whether replication lag has been observed for this standby.
	LagObserved bool `json:"lagObserved,omitempty"`
	// ReplicationLagSeconds is the observed or reported replication lag.
	ReplicationLagSeconds int32 `json:"replicationLagSeconds,omitempty"`
	// Message summarizes this standby's posture.
	Message string `json:"message,omitempty"`
}

// ApprovalMode identifies how an action request is authorized.
// +kubebuilder:validation:Enum=auto;required;approved;rejected
type ApprovalMode string

const (
	ApprovalModeAuto     ApprovalMode = "auto"
	ApprovalModeRequired ApprovalMode = "required"
	ApprovalModeApproved ApprovalMode = "approved"
	ApprovalModeRejected ApprovalMode = "rejected"
)

// RequestSource identifies where a platform action originated.
// +kubebuilder:validation:Enum=ui;api;automation
type RequestSource string

const (
	RequestSourceUI         RequestSource = "ui"
	RequestSourceAPI        RequestSource = "api"
	RequestSourceAutomation RequestSource = "automation"
)

// ApprovalSpec describes the approval state for an action request.
type ApprovalSpec struct {
	// Mode captures the requested or current approval state.
	Mode ApprovalMode `json:"mode"`
	// ApprovedBy records the approvers once approval occurs.
	ApprovedBy []string `json:"approvedBy,omitempty"`
}

// RequestedBySpec describes the actor that initiated a request.
type RequestedBySpec struct {
	// Subject identifies the user or service account.
	// +kubebuilder:validation:MinLength=1
	Subject string `json:"subject"`
	// Source identifies the initiating path.
	Source RequestSource `json:"source"`
}

// UsageSummary captures aggregate counters for a resource.
type UsageSummary struct {
	// Projects is the number of child projects.
	Projects int32 `json:"projects,omitempty"`
	// ServiceInstances is the number of child service instances.
	ServiceInstances int32 `json:"serviceInstances,omitempty"`
	// Namespaces is the number of child namespaces.
	Namespaces int32 `json:"namespaces,omitempty"`
	// Services is the number of child services.
	Services int32 `json:"services,omitempty"`
}

// ActionResultStatus captures the outcome of an action execution.
type ActionResultStatus struct {
	// Code is a stable machine-readable result code.
	Code string `json:"code,omitempty"`
	// Message is a concise operator-facing summary.
	Message string `json:"message,omitempty"`
	// Retryable indicates whether the action may be safely retried.
	Retryable bool `json:"retryable,omitempty"`
}

// ParametersObject stores schemaless adapter parameters.
type ParametersObject = apiextensionsv1.JSON

// ConditionList contains status conditions.
// +listType=map
// +listMapKey=type
type ConditionList []metav1.Condition
