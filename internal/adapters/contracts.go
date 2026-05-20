package adapters

import (
	"context"

	platformv1alpha1 "github.com/sindef/servicer/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ServiceClass identifies the product class handled by an adapter.
type ServiceClass string

const (
	ServiceClassPostgreSQL ServiceClass = "postgresql"
	ServiceClassMySQL      ServiceClass = "mysql"
	ServiceClassNamespace  ServiceClass = "namespace"
	ServiceClassValkey     ServiceClass = "valkey"
	ServiceClassNATS       ServiceClass = "nats"
	ServiceClassK8ssandra  ServiceClass = "cassandra"
	ServiceClassYugabyte   ServiceClass = "yugabyte"
	ServiceClassArgoApp    ServiceClass = "argo-application"
)

// SyncPhase captures the delivery-state summary for an instance.
type SyncPhase string

const (
	SyncPhasePending      SyncPhase = "Pending"
	SyncPhaseMaterialized SyncPhase = "Materialized"
	SyncPhaseSynced       SyncPhase = "Synced"
	SyncPhaseOutOfSync    SyncPhase = "OutOfSync"
	SyncPhaseUnknown      SyncPhase = "Unknown"
)

// HealthSeverity identifies the importance of a health signal.
type HealthSeverity string

const (
	HealthSeverityInfo     HealthSeverity = "Info"
	HealthSeverityWarning  HealthSeverity = "Warning"
	HealthSeverityCritical HealthSeverity = "Critical"
)

// EndpointVisibility identifies how a service endpoint is exposed.
type EndpointVisibility string

const (
	EndpointVisibilityClusterInternal EndpointVisibility = "ClusterInternal"
	EndpointVisibilityPrivate         EndpointVisibility = "Private"
	EndpointVisibilityPublic          EndpointVisibility = "Public"
)

// ActionName identifies a platform action exposed by an adapter.
type ActionName string

const (
	ActionBackup            ActionName = "backup"
	ActionRestore           ActionName = "restore"
	ActionFailover          ActionName = "failover"
	ActionRollbackFailover  ActionName = "rollback-failover"
	ActionResyncStandby     ActionName = "resync-standby"
	ActionUpdateQuota       ActionName = "update-quota"
	ActionGrantAccess       ActionName = "grant-access"
	ActionApplyPolicy       ActionName = "apply-policy"
	ActionScale             ActionName = "scale"
	ActionRestart           ActionName = "restart"
	ActionRotateCredentials ActionName = "rotate-credentials"
	ActionPause             ActionName = "pause"
	ActionResume            ActionName = "resume"
	ActionRepair            ActionName = "repair"
	ActionDeleteStream      ActionName = "delete-stream"
	ActionPurgeStream       ActionName = "purge-stream"
	ActionDeleteConsumer    ActionName = "delete-consumer"
)

// TopologyRole identifies the role of a node in a service topology.
type TopologyRole string

const (
	TopologyRolePrimary    TopologyRole = "Primary"
	TopologyRoleReplica    TopologyRole = "Replica"
	TopologyRoleBroker     TopologyRole = "Broker"
	TopologyRoleSeed       TopologyRole = "Seed"
	TopologyRoleDatacenter TopologyRole = "Datacenter"
	TopologyRoleShard      TopologyRole = "Shard"
)

// ServiceContext provides the inputs an adapter needs to make decisions.
type ServiceContext struct {
	Tenant        *platformv1alpha1.Tenant
	Project       *platformv1alpha1.Project
	ClusterTarget *platformv1alpha1.ClusterTarget
	Class         *platformv1alpha1.ServiceClass
	Plan          *platformv1alpha1.ServicePlan
	Instance      *platformv1alpha1.ServiceInstance
}

// ValidationRequest wraps the context required for spec validation.
type ValidationRequest struct {
	Context ServiceContext
}

// RenderRequest wraps the context required to produce deterministic artifacts.
type RenderRequest struct {
	Context ServiceContext
}

// ObserveRequest wraps the context required to normalize runtime health.
type ObserveRequest struct {
	Context           ServiceContext
	ObservedResources []platformv1alpha1.TypedObjectReference
	ArtifactRevision  string
	ApplicationName   string
	Runtime           RuntimeObservation
}

// DeleteRequest wraps the context required to apply deletion behavior.
type DeleteRequest struct {
	Context ServiceContext
}

// ExecuteActionRequest wraps the context and action for day-2 operations.
type ExecuteActionRequest struct {
	Context ServiceContext
	Action  *platformv1alpha1.ActionRequest
}

// ValidationIssue is a structured validation problem.
type ValidationIssue struct {
	Path     string
	Message  string
	Severity HealthSeverity
}

// ValidationResult reports whether a request is acceptable.
type ValidationResult struct {
	Valid  bool
	Issues []ValidationIssue
}

// RenderedArtifact is a deterministic generated artifact for the delivery layer.
type RenderedArtifact struct {
	Path    string
	Content []byte
}

// RenderResult captures the output of adapter rendering.
type RenderResult struct {
	RuntimeDriver   string
	PrimaryResource *platformv1alpha1.TypedObjectReference
	PackagePath     string
	PackagePaths    []string
	Endpoints       []Endpoint
	CredentialRefs  []platformv1alpha1.NamespacedObjectReference
	Artifacts       []RenderedArtifact
}

// RuntimeObservation captures runtime state observed outside adapter rendering.
type RuntimeObservation struct {
	Workload                *WorkloadObservation
	ReadyPods               int32
	TotalPods               int32
	CredentialSecretPresent bool
	Blocked                 bool
	Message                 string
}

// WorkloadObservation captures status for an observed runtime workload.
type WorkloadObservation struct {
	DesiredReplicas int32
	ReadyReplicas   int32
	UpdatedReplicas int32
	Observed        bool
}

// DeleteResult reports the outcome of delete preparation or orchestration.
type DeleteResult struct {
	Blocked     bool
	Message     string
	SnapshotRef *platformv1alpha1.TypedObjectReference
}

// ActionExecutionResult reports the outcome of action orchestration.
type ActionExecutionResult struct {
	Phase        string
	OperationRef *platformv1alpha1.TypedObjectReference
	Message      string
	Retryable    bool
}

// ActionCapability describes a supported day-2 action.
type ActionCapability struct {
	Name             ActionName
	DisplayName      string
	RequiresApproval bool
	Disruptive       bool
}

// StatusSignalDescriptor defines one normalized health signal an adapter emits.
type StatusSignalDescriptor struct {
	Key         string
	DisplayName string
	Description string
	Severity    HealthSeverity
}

// ProductContract describes the stable platform contract for a product adapter.
type ProductContract struct {
	ServiceClass            ServiceClass
	FriendlyName            string
	RuntimeDriver           string
	SupportsVersionOverride bool
	SupportsMultiCluster    bool
	TopologyModes           []string
	StatusSignals           []StatusSignalDescriptor
	Actions                 []ActionCapability
}

// Endpoint identifies a user-meaningful service endpoint.
type Endpoint struct {
	Name       string
	Address    string
	Port       int32
	Protocol   string
	Visibility EndpointVisibility
}

// TopologyNode identifies one node, member, or logical unit in a service topology.
type TopologyNode struct {
	Name       string
	Role       TopologyRole
	Ready      bool
	Cluster    string
	Region     string
	Conditions []metav1.Condition
}

// HealthSignal is a normalized runtime health signal.
type HealthSignal struct {
	Key      string
	Status   string
	Severity HealthSeverity
	Message  string
}

// SyncStatus summarizes delivery progress from materialization through Argo sync.
type SyncStatus struct {
	Phase            SyncPhase
	ArtifactRevision string
	ApplicationName  string
	Message          string
}

// NormalizedStatus is the stable status shape returned by runtime adapters.
type NormalizedStatus struct {
	Phase             string
	Summary           string
	Conditions        []metav1.Condition
	Topology          []TopologyNode
	Endpoints         []Endpoint
	CredentialRefs    []platformv1alpha1.NamespacedObjectReference
	ObservedResources []platformv1alpha1.TypedObjectReference
	Sync              SyncStatus
	HealthSignals     []HealthSignal
	CacheTopology     *platformv1alpha1.CacheTopologyStatus
}

// ServiceAdapter defines the contract implemented by each runtime adapter.
type ServiceAdapter interface {
	Contract() ProductContract
	Validate(context.Context, ValidationRequest) (ValidationResult, error)
	Render(context.Context, RenderRequest) (RenderResult, error)
	Observe(context.Context, ObserveRequest) (NormalizedStatus, error)
	Delete(context.Context, DeleteRequest) (DeleteResult, error)
	SupportedActions(context.Context, ServiceContext) []ActionCapability
	ExecuteAction(context.Context, ExecuteActionRequest) (ActionExecutionResult, error)
}

// requiresPodMesh returns a critical ValidationIssue when the ClusterTarget does not
// advertise a pod-level cross-cluster network fabric. Call this in adapter validation
// for any topology that gossips on pod IPs across clusters (e.g. Galera, Valkey Sentinel).
func requiresPodMesh(ctx ServiceContext) []ValidationIssue {
	if ctx.ClusterTarget == nil {
		return nil
	}
	if ctx.ClusterTarget.Spec.Mesh() == platformv1alpha1.ClusterMeshNone {
		return []ValidationIssue{{
			Path:     "clusterTarget.capabilities.mesh",
			Message:  "this topology requires pod-to-pod cross-cluster networking; set capabilities.mesh to one of: calico, cilium, istio, submariner on the ClusterTarget",
			Severity: HealthSeverityCritical,
		}}
	}
	return nil
}
