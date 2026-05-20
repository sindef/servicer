package bff

import "encoding/json"

type OverviewResponse struct {
	Tenants        int32           `json:"tenants"`
	Projects       int32           `json:"projects"`
	Instances      int32           `json:"instances"`
	ReadyInstances int32           `json:"readyInstances"`
	PendingActions int32           `json:"pendingActions"`
	Health         HealthBreakdown `json:"health"`
	RecentActions  []ActionSummary `json:"recentActions"`
}

type AuthConfigResponse struct {
	Mode             string          `json:"mode"`
	AllowDemoHeaders bool            `json:"allowDemoHeaders,omitempty"`
	OIDC             *AuthOIDCConfig `json:"oidc,omitempty"`
}

type AuthOIDCConfig struct {
	IssuerURL    string   `json:"issuerUrl"`
	ClientID     string   `json:"clientId"`
	Scopes       []string `json:"scopes,omitempty"`
	RedirectPath string   `json:"redirectPath,omitempty"`
}

type AuthSessionResponse struct {
	Mode             string   `json:"mode"`
	Name             string   `json:"name"`
	Roles            []string `json:"roles,omitempty"`
	Groups           []string `json:"groups,omitempty"`
	Authenticated    bool     `json:"authenticated"`
	AllowDemoHeaders bool     `json:"allowDemoHeaders,omitempty"`
}

type HealthBreakdown struct {
	Ready        int32 `json:"ready"`
	Provisioning int32 `json:"provisioning"`
	Failed       int32 `json:"failed"`
	Other        int32 `json:"other"`
}

type TenantSummary struct {
	Name                  string   `json:"name"`
	DisplayName           string   `json:"displayName"`
	Phase                 string   `json:"phase"`
	AllowedServiceClasses []string `json:"allowedServiceClasses"`
	ProjectCount          int32    `json:"projectCount"`
	InstanceCount         int32    `json:"instanceCount"`
	Owners                []string `json:"owners"`
}

type ProjectSummary struct {
	Name          string `json:"name"`
	DisplayName   string `json:"displayName"`
	TenantName    string `json:"tenantName"`
	Environment   string `json:"environment"`
	Phase         string `json:"phase"`
	ClusterName   string `json:"clusterName"`
	NamespaceMode string `json:"namespaceMode"`
	InstanceCount int32  `json:"instanceCount"`
}

type NamespaceClaimSummary struct {
	Name        string `json:"name"`
	ProjectName string `json:"projectName"`
	Phase       string `json:"phase"`
	ClusterName string `json:"clusterName,omitempty"`
	Namespace   string `json:"namespace,omitempty"`
	Health      string `json:"health,omitempty"`
}

type NamespaceClaimDetail struct {
	Name           string             `json:"name"`
	DisplayName    string             `json:"displayName,omitempty"`
	ProjectName    string             `json:"projectName"`
	Phase          string             `json:"phase"`
	ClusterName    string             `json:"clusterName,omitempty"`
	Namespace      string             `json:"namespace,omitempty"`
	Health         string             `json:"health,omitempty"`
	DeletionPolicy string             `json:"deletionPolicy,omitempty"`
	Quotas         map[string]string  `json:"quotas,omitempty"`
	Labels         map[string]string  `json:"labels,omitempty"`
	Artifact       ArtifactSummary    `json:"artifact"`
	Delivery       DeliverySummary    `json:"delivery"`
	Conditions     []ConditionSummary `json:"conditions"`
}

type NamespaceClaimRequest struct {
	Name           string            `json:"name"`
	ProjectName    string            `json:"projectName"`
	DisplayName    string            `json:"displayName,omitempty"`
	Quotas         map[string]string `json:"quotas,omitempty"`
	Labels         map[string]string `json:"labels,omitempty"`
	DeletionPolicy string            `json:"deletionPolicy,omitempty"`
}

type ServiceBindingSummary struct {
	Name               string `json:"name"`
	ProjectName        string `json:"projectName"`
	Phase              string `json:"phase"`
	SourceName         string `json:"sourceName"`
	TargetName         string `json:"targetName"`
	ProjectedSecret    string `json:"projectedSecret,omitempty"`
	ProjectedNamespace string `json:"projectedNamespace,omitempty"`
	Health             string `json:"health,omitempty"`
}

type VirtualMachineClaimSummary struct {
	Name        string `json:"name"`
	ProjectName string `json:"projectName"`
	Class       string `json:"class"`
	Image       string `json:"image"`
	Phase       string `json:"phase"`
	ClusterName string `json:"clusterName,omitempty"`
	Health      string `json:"health,omitempty"`
}

type CatalogEntry struct {
	Name         string        `json:"name"`
	DisplayName  string        `json:"displayName"`
	Category     string        `json:"category"`
	Driver       string        `json:"driver"`
	Published    bool          `json:"published"`
	Description  string        `json:"description"`
	Capabilities []string      `json:"capabilities"`
	Plans        []CatalogPlan `json:"plans"`
	Actions      []ActionSpec  `json:"actions"`
}

type CatalogPlan struct {
	Name           string `json:"name"`
	DisplayName    string `json:"displayName"`
	Tier           string `json:"tier"`
	Topology       string `json:"topology"`
	DefaultVersion string `json:"defaultVersion"`
	Published      bool   `json:"published"`
}

type ActionSpec struct {
	Name             string `json:"name"`
	DisplayName      string `json:"displayName"`
	RequiresApproval bool   `json:"requiresApproval"`
	Disruptive       bool   `json:"disruptive"`
}

type InstanceSummary struct {
	Name         string            `json:"name"`
	DisplayName  string            `json:"displayName"`
	ProjectName  string            `json:"projectName"`
	TenantName   string            `json:"tenantName"`
	ProductClass string            `json:"productClass"`
	ProductName  string            `json:"productName"`
	PlanName     string            `json:"planName"`
	PlanDisplay  string            `json:"planDisplay"`
	Phase        string            `json:"phase"`
	Health       string            `json:"health"`
	ClusterName  string            `json:"clusterName"`
	Namespace    string            `json:"namespace"`
	SyncPhase    string            `json:"syncPhase"`
	Endpoints    map[string]string `json:"endpoints,omitempty"`
}

type InstanceDetail struct {
	InstanceSummary
	Runtime          RuntimeSummary        `json:"runtime"`
	Delivery         DeliverySummary       `json:"delivery"`
	Desired          ProductRequest        `json:"desired"`
	Artifact         ArtifactSummary       `json:"artifact"`
	Credentials      []CredentialSummary   `json:"credentials"`
	Conditions       []ConditionSummary    `json:"conditions"`
	Topology         *CacheTopologySummary `json:"topology,omitempty"`
	Messaging        *MessagingSummary     `json:"messaging,omitempty"`
	AvailableActions []ActionSpec          `json:"availableActions"`
	RecentActions    []ActionSummary       `json:"recentActions"`
	Events           []AuditEventSummary   `json:"events"`
}

type RuntimeSummary struct {
	Driver     string `json:"driver"`
	APIVersion string `json:"apiVersion,omitempty"`
	Kind       string `json:"kind,omitempty"`
	Name       string `json:"name,omitempty"`
	Namespace  string `json:"namespace,omitempty"`
}

type ArtifactSummary struct {
	Revision string `json:"revision,omitempty"`
	Path     string `json:"path,omitempty"`
	Count    int32  `json:"count,omitempty"`
}

type DeliverySummary struct {
	SyncPhase       string `json:"syncPhase,omitempty"`
	ApplicationName string `json:"applicationName,omitempty"`
	Message         string `json:"message,omitempty"`
	ArgoStatus      string `json:"argoStatus,omitempty"`
	RuntimeStatus   string `json:"runtimeStatus,omitempty"`
}

type CredentialSummary struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	RevealURL string `json:"revealUrl,omitempty"`
}

type CredentialDetail struct {
	Name      string            `json:"name"`
	Namespace string            `json:"namespace"`
	Data      map[string]string `json:"data"`
}

type ConditionSummary struct {
	Type    string `json:"type"`
	Status  string `json:"status"`
	Reason  string `json:"reason"`
	Message string `json:"message"`
}

type CacheTopologySummary struct {
	Mode              string                `json:"mode,omitempty"`
	PrimaryCluster    string                `json:"primaryCluster,omitempty"`
	TrafficEndpoint   string                `json:"trafficEndpoint,omitempty"`
	FailoverReadiness string                `json:"failoverReadiness,omitempty"`
	Message           string                `json:"message,omitempty"`
	Standbys          []CacheStandbySummary `json:"standbys,omitempty"`
}

type CacheStandbySummary struct {
	ClusterName           string `json:"clusterName,omitempty"`
	Ready                 bool   `json:"ready"`
	ResyncRequired        bool   `json:"resyncRequired,omitempty"`
	LagObserved           bool   `json:"lagObserved,omitempty"`
	ReplicationLagSeconds int32  `json:"replicationLagSeconds,omitempty"`
	Message               string `json:"message,omitempty"`
}

type MessagingSummary struct {
	JetStream      bool                       `json:"jetStream"`
	Streams        []MessagingStreamSummary   `json:"streams,omitempty"`
	Consumers      []MessagingConsumerSummary `json:"consumers,omitempty"`
	AppCredentials []MessagingCredentialSpec  `json:"appCredentials,omitempty"`
}

type MessagingStreamSummary struct {
	Name      string   `json:"name"`
	Subjects  []string `json:"subjects,omitempty"`
	Storage   string   `json:"storage,omitempty"`
	Retention string   `json:"retention,omitempty"`
}

type MessagingConsumerSummary struct {
	Name           string   `json:"name"`
	Stream         string   `json:"stream"`
	FilterSubjects []string `json:"filterSubjects,omitempty"`
	AckPolicy      string   `json:"ackPolicy,omitempty"`
}

type MessagingCredentialSpec struct {
	Name           string   `json:"name"`
	Username       string   `json:"username"`
	Publish        []string `json:"publish,omitempty"`
	Subscribe      []string `json:"subscribe,omitempty"`
	AllowResponses bool     `json:"allowResponses,omitempty"`
}

type ActionSummary struct {
	Name                  string `json:"name"`
	TargetName            string `json:"targetName"`
	Action                string `json:"action"`
	Phase                 string `json:"phase"`
	Result                string `json:"result,omitempty"`
	StartedAt             string `json:"startedAt,omitempty"`
	CompletedAt           string `json:"completedAt,omitempty"`
	KubeconfigDownloadURL string `json:"kubeconfigDownloadUrl,omitempty"`
}

type ProductRequest struct {
	Name         string         `json:"name"`
	ProjectName  string         `json:"projectName"`
	ServiceClass string         `json:"serviceClass"`
	ServicePlan  string         `json:"servicePlan"`
	Version      string         `json:"version,omitempty"`
	Parameters   map[string]any `json:"parameters,omitempty"`
}

type ActionSubmitRequest struct {
	Action     string         `json:"action"`
	Parameters map[string]any `json:"parameters,omitempty"`
	Reason     string         `json:"reason,omitempty"`
}

type ActionApprovalRequest struct {
	Decision string `json:"decision"`
	Reason   string `json:"reason,omitempty"`
}

type WriteResponse struct {
	Name    string `json:"name"`
	Message string `json:"message"`
}

type AuditEventSummary struct {
	Time     string `json:"time,omitempty"`
	Type     string `json:"type"`
	Subject  string `json:"subject"`
	Action   string `json:"action,omitempty"`
	Actor    string `json:"actor,omitempty"`
	Phase    string `json:"phase,omitempty"`
	Reason   string `json:"reason,omitempty"`
	Message  string `json:"message,omitempty"`
	Involved string `json:"involved,omitempty"`
}

// Admin types

type ClusterTargetSummary struct {
	Name          string            `json:"name"`
	DisplayName   string            `json:"displayName"`
	Region        string            `json:"region"`
	Phase         string            `json:"phase"`
	Reachable     bool              `json:"reachable"`
	K8sVersion    string            `json:"k8sVersion"`
	IngressDomain string            `json:"ingressDomain"`
	Capabilities  map[string]string `json:"capabilities,omitempty"`
}

type ServiceClassAdminSummary struct {
	Name              string          `json:"name"`
	DisplayName       string          `json:"displayName"`
	Driver            string          `json:"driver"`
	Category          string          `json:"category"`
	Published         bool            `json:"published"`
	Registered        bool            `json:"registered"`
	DefaultParameters json.RawMessage `json:"defaultParameters,omitempty"`
}

type RepositorySummary struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	ProjectName string `json:"projectName"`
	URL         string `json:"url"`
	AuthType    string `json:"authType"` // "none", "http", "ssh"
}

type CreateRepositoryRequest struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	ProjectName string `json:"projectName"`
	URL         string `json:"url"`
	AuthType    string `json:"authType"` // "none", "http", "ssh"
	Username    string `json:"username,omitempty"`
	Password    string `json:"password,omitempty"`
	SSHKey      string `json:"sshKey,omitempty"`
}

type CreateTenantRequest struct {
	Name                  string   `json:"name"`
	DisplayName           string   `json:"displayName"`
	Owners                []string `json:"owners"`
	AllowedServiceClasses []string `json:"allowedServiceClasses"`
	QuotaProfileRef       string   `json:"quotaProfileRef,omitempty"`
}

type UpdateTenantRequest struct {
	DisplayName           string   `json:"displayName"`
	Owners                []string `json:"owners"`
	AllowedServiceClasses []string `json:"allowedServiceClasses"`
}

type CreateProjectRequest struct {
	Name          string `json:"name"`
	DisplayName   string `json:"displayName"`
	TenantName    string `json:"tenantName"`
	Environment   string `json:"environment"`
	ClusterName   string `json:"clusterName,omitempty"`
	NamespaceMode string `json:"namespaceMode"`
	MaxServices   *int32 `json:"maxServices,omitempty"`
}

type UpdateProjectRequest struct {
	DisplayName   string `json:"displayName"`
	ClusterName   string `json:"clusterName"`
	NamespaceMode string `json:"namespaceMode"`
	MaxServices   *int32 `json:"maxServices,omitempty"`
}

type CreateClusterRequest struct {
	Name                      string            `json:"name"`
	DisplayName               string            `json:"displayName"`
	Region                    string            `json:"region"`
	IngressDomain             string            `json:"ingressDomain,omitempty"`
	Capabilities              map[string]string `json:"capabilities,omitempty"`
	ConnectionSecretName      string            `json:"connectionSecretName"`
	ConnectionSecretNamespace string            `json:"connectionSecretNamespace"`
}

type UpdateClusterRequest struct {
	DisplayName   string            `json:"displayName"`
	Region        string            `json:"region"`
	IngressDomain string            `json:"ingressDomain,omitempty"`
	Capabilities  map[string]string `json:"capabilities,omitempty"`
}

type UpdateServiceClassRequest struct {
	Published         bool            `json:"published"`
	DefaultParameters json.RawMessage `json:"defaultParameters,omitempty"`
}

type RegisterServiceClassRequest struct {
	Name string `json:"name"`
}
