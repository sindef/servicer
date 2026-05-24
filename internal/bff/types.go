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
	Mode            string                  `json:"mode"`
	LoginPath       string                  `json:"loginPath,omitempty"`
	LogoutPath      string                  `json:"logoutPath,omitempty"`
	CallbackPath    string                  `json:"callbackPath,omitempty"`
	DefaultProvider string                  `json:"defaultProvider,omitempty"`
	Providers       []AuthProviderLoginView `json:"providers,omitempty"`
}

type AuthProviderLoginView struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	Type        string `json:"type"`
	Default     bool   `json:"default,omitempty"`
}

type AuthSessionResponse struct {
	Mode          string              `json:"mode"`
	Name          string              `json:"name"`
	Email         string              `json:"email,omitempty"`
	UserName      string              `json:"userName,omitempty"`
	Provider      string              `json:"provider,omitempty"`
	Roles         []string            `json:"roles,omitempty"`
	Groups        []string            `json:"groups,omitempty"`
	Tenants       []TenantRoleSummary `json:"tenants,omitempty"`
	Authenticated bool                `json:"authenticated"`
}

type TenantRoleSummary struct {
	TenantName string   `json:"tenantName"`
	Roles      []string `json:"roles"`
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
	Scope       string `json:"scope"`
	TenantName  string `json:"tenantName,omitempty"`
	ProjectName string `json:"projectName,omitempty"`
	URL         string `json:"url"`
	AuthType    string `json:"authType"` // "none", "http", "ssh"
}

type CreateRepositoryRequest struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	Scope       string `json:"scope,omitempty"`
	TenantName  string `json:"tenantName,omitempty"`
	ProjectName string `json:"projectName,omitempty"`
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

type AuthProviderSummary struct {
	Name                  string   `json:"name"`
	DisplayName           string   `json:"displayName"`
	Type                  string   `json:"type"`
	Enabled               bool     `json:"enabled"`
	Default               bool     `json:"default"`
	Phase                 string   `json:"phase,omitempty"`
	OIDCIssuerURL         string   `json:"oidcIssuerUrl,omitempty"`
	OIDCClientID          string   `json:"oidcClientId,omitempty"`
	OIDCScopes            []string `json:"oidcScopes,omitempty"`
	OIDCUsernameClaim     string   `json:"oidcUsernameClaim,omitempty"`
	OIDCEmailClaim        string   `json:"oidcEmailClaim,omitempty"`
	OIDCRolesClaim        string   `json:"oidcRolesClaim,omitempty"`
	OIDCGroupsClaim       string   `json:"oidcGroupsClaim,omitempty"`
	OIDCRedirectPath      string   `json:"oidcRedirectPath,omitempty"`
	OIDCEndSessionURL     string   `json:"oidcEndSessionUrl,omitempty"`
	LDAPURL               string   `json:"ldapUrl,omitempty"`
	LDAPUserBaseDN        string   `json:"ldapUserBaseDn,omitempty"`
	LDAPUserFilter        string   `json:"ldapUserFilter,omitempty"`
	LDAPUsernameAttribute string   `json:"ldapUsernameAttribute,omitempty"`
	LDAPEmailAttribute    string   `json:"ldapEmailAttribute,omitempty"`
	LDAPGroupBaseDN       string   `json:"ldapGroupBaseDn,omitempty"`
	LDAPGroupFilter       string   `json:"ldapGroupFilter,omitempty"`
	LDAPGroupNameAttr     string   `json:"ldapGroupNameAttribute,omitempty"`
	LDAPStartTLS          bool     `json:"ldapStartTls,omitempty"`
	InsecureSkipVerify    bool     `json:"insecureSkipVerify,omitempty"`
	SecretConfigured      bool     `json:"secretConfigured,omitempty"`
}

type AuthProviderRequest struct {
	Name                  string   `json:"name"`
	DisplayName           string   `json:"displayName"`
	Type                  string   `json:"type"`
	Enabled               bool     `json:"enabled"`
	Default               bool     `json:"default"`
	OIDCIssuerURL         string   `json:"oidcIssuerUrl,omitempty"`
	OIDCClientID          string   `json:"oidcClientId,omitempty"`
	OIDCClientSecret      string   `json:"oidcClientSecret,omitempty"`
	OIDCScopes            []string `json:"oidcScopes,omitempty"`
	OIDCUsernameClaim     string   `json:"oidcUsernameClaim,omitempty"`
	OIDCEmailClaim        string   `json:"oidcEmailClaim,omitempty"`
	OIDCRolesClaim        string   `json:"oidcRolesClaim,omitempty"`
	OIDCGroupsClaim       string   `json:"oidcGroupsClaim,omitempty"`
	OIDCRedirectPath      string   `json:"oidcRedirectPath,omitempty"`
	OIDCEndSessionURL     string   `json:"oidcEndSessionUrl,omitempty"`
	LDAPURL               string   `json:"ldapUrl,omitempty"`
	LDAPBindUsername      string   `json:"ldapBindUsername,omitempty"`
	LDAPBindPassword      string   `json:"ldapBindPassword,omitempty"`
	LDAPUserBaseDN        string   `json:"ldapUserBaseDn,omitempty"`
	LDAPUserFilter        string   `json:"ldapUserFilter,omitempty"`
	LDAPUsernameAttribute string   `json:"ldapUsernameAttribute,omitempty"`
	LDAPEmailAttribute    string   `json:"ldapEmailAttribute,omitempty"`
	LDAPGroupBaseDN       string   `json:"ldapGroupBaseDn,omitempty"`
	LDAPGroupFilter       string   `json:"ldapGroupFilter,omitempty"`
	LDAPGroupNameAttr     string   `json:"ldapGroupNameAttribute,omitempty"`
	LDAPStartTLS          bool     `json:"ldapStartTls,omitempty"`
	InsecureSkipVerify    bool     `json:"insecureSkipVerify,omitempty"`
}

type UserSummary struct {
	Name               string                    `json:"name"`
	DisplayName        string                    `json:"displayName,omitempty"`
	Email              string                    `json:"email,omitempty"`
	LocalAuthEnabled   bool                      `json:"localAuthEnabled"`
	ExternalIdentities []ExternalIdentitySummary `json:"externalIdentities,omitempty"`
}

type ExternalIdentitySummary struct {
	Provider string `json:"provider"`
	Subject  string `json:"subject"`
}

type UserRequest struct {
	Name               string                    `json:"name"`
	DisplayName        string                    `json:"displayName,omitempty"`
	Email              string                    `json:"email,omitempty"`
	LocalAuthEnabled   bool                      `json:"localAuthEnabled"`
	Password           string                    `json:"password,omitempty"`
	ExternalIdentities []ExternalIdentitySummary `json:"externalIdentities,omitempty"`
}

type GroupSummary struct {
	Name           string                 `json:"name"`
	DisplayName    string                 `json:"displayName,omitempty"`
	Members        []string               `json:"members,omitempty"`
	ExternalGroups []ExternalGroupSummary `json:"externalGroups,omitempty"`
}

type ExternalGroupSummary struct {
	Provider string `json:"provider"`
	Name     string `json:"name"`
}

type GroupRequest struct {
	Name           string                 `json:"name"`
	DisplayName    string                 `json:"displayName,omitempty"`
	Members        []string               `json:"members,omitempty"`
	ExternalGroups []ExternalGroupSummary `json:"externalGroups,omitempty"`
}

type RoleBindingSummary struct {
	Name        string               `json:"name"`
	DisplayName string               `json:"displayName,omitempty"`
	Scope       string               `json:"scope"`
	TenantName  string               `json:"tenantName,omitempty"`
	Subjects    []RoleBindingSubject `json:"subjects"`
	Roles       []string             `json:"roles"`
}

type RoleBindingSubject struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
}

type RoleBindingRequest struct {
	Name        string               `json:"name"`
	DisplayName string               `json:"displayName,omitempty"`
	Scope       string               `json:"scope"`
	TenantName  string               `json:"tenantName,omitempty"`
	Subjects    []RoleBindingSubject `json:"subjects"`
	Roles       []string             `json:"roles"`
}
