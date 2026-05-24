package adapters

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	platformv1alpha1 "github.com/sindef/servicer/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

const yugabyteDriver = "yb-operator"
const yugabyteOperatorNamespace = "yugabyte-system"

type yugabyteParameters struct {
	TServerReplicas          *int32           `json:"tserverReplicas,omitempty"`
	MasterReplicas           *int32           `json:"masterReplicas,omitempty"`
	ReplicationFactor        *int32           `json:"replicationFactor,omitempty"`
	DatabaseName             string           `json:"databaseName,omitempty"`
	CPU                      string           `json:"cpu,omitempty"`
	Memory                   string           `json:"memory,omitempty"`
	MasterCPU                string           `json:"masterCpu,omitempty"`
	MasterMemory             string           `json:"masterMemory,omitempty"`
	TServerCPU               string           `json:"tserverCpu,omitempty"`
	TServerMemory            string           `json:"tserverMemory,omitempty"`
	StorageSize              string           `json:"storageSize,omitempty"`
	StorageClass             string           `json:"storageClass,omitempty"`
	EnableYSQL               *bool            `json:"enableYSQL,omitempty"`
	EnableYCQL               *bool            `json:"enableYCQL,omitempty"`
	BackupProfile            string           `json:"backupProfile,omitempty"`
	PrimaryCluster           string           `json:"primaryCluster,omitempty"`
	StandbyClusters          []string         `json:"standbyClusters,omitempty"`
	ReplicationLagSeconds    map[string]int32 `json:"replicationLagSeconds,omitempty"`
	MaxReplicationLagSeconds *int32           `json:"maxReplicationLagSeconds,omitempty"`
	ServiceType              string           `json:"serviceType,omitempty"`
	ExternalDNSHostname      string           `json:"externalDnsHostname,omitempty"`
}

// YugabyteContract describes the normalized platform contract for YugabyteDB-backed distributed SQL/NoSQL.
var YugabyteContract = ProductContract{
	ServiceClass:            ServiceClassYugabyte,
	FriendlyName:            "YugabyteDB",
	RuntimeDriver:           yugabyteDriver,
	SupportsVersionOverride: true,
	SupportsMultiCluster:    true,
	TopologyModes: []string{
		"single-cluster",
		"multi-region",
	},
	StatusSignals: []StatusSignalDescriptor{
		{Key: "tserver-readiness", DisplayName: "TServer Readiness", Description: "Whether all YB-TServer pods are ready and serving tablets.", Severity: HealthSeverityCritical},
		{Key: "master-quorum", DisplayName: "Master Quorum", Description: "Whether the YB-Master quorum is healthy and has an elected leader.", Severity: HealthSeverityCritical},
		{Key: "replication-lag", DisplayName: "Replication Lag", Description: "Whether cross-cluster replication lag is within acceptable bounds.", Severity: HealthSeverityWarning},
		{Key: "backup-freshness", DisplayName: "Backup Freshness", Description: "Whether backups satisfy the configured retention policy.", Severity: HealthSeverityWarning},
	},
	Actions: []ActionCapability{},
}

// DefaultYugabyteDeletionPolicy uses snapshot-first deletion, consistent with stateful database products.
const DefaultYugabyteDeletionPolicy = platformv1alpha1.DeletionPolicySnapshot

// YugabyteAdapter renders YugabyteDB clusters via the YugabyteDB Kubernetes Operator.
type YugabyteAdapter struct{}

// NewYugabyteAdapter creates a YugabyteDB adapter backed by the YugabyteDB Kubernetes Operator.
func NewYugabyteAdapter() *YugabyteAdapter {
	return &YugabyteAdapter{}
}

func (a *YugabyteAdapter) Contract() ProductContract {
	return YugabyteContract
}

func (a *YugabyteAdapter) Validate(_ context.Context, request ValidationRequest) (ValidationResult, error) {
	issues := make([]ValidationIssue, 0)
	ctx := request.Context
	if ctx.Instance == nil {
		issues = append(issues, ValidationIssue{Path: "instance", Message: "service instance context is required", Severity: HealthSeverityCritical})
	}
	if ctx.Class == nil {
		issues = append(issues, ValidationIssue{Path: "serviceClassRef", Message: "service class is required", Severity: HealthSeverityCritical})
	}
	if ctx.Plan == nil {
		issues = append(issues, ValidationIssue{Path: "servicePlanRef", Message: "service plan is required", Severity: HealthSeverityCritical})
	}
	if len(issues) > 0 {
		return ValidationResult{Valid: false, Issues: issues}, nil
	}
	if ctx.Class.Spec.Driver != yugabyteDriver {
		issues = append(issues, ValidationIssue{Path: "serviceClass.driver", Message: fmt.Sprintf("expected driver %q, got %q", yugabyteDriver, ctx.Class.Spec.Driver), Severity: HealthSeverityCritical})
	}
	if ctx.Plan.Spec.ServiceClassRef.Name != ctx.Instance.Spec.ServiceClassRef.Name {
		issues = append(issues, ValidationIssue{Path: "servicePlanRef", Message: "service plan does not belong to the selected service class", Severity: HealthSeverityCritical})
	}
	if !containsString(a.Contract().TopologyModes, ctx.Plan.Spec.Topology) {
		issues = append(issues, ValidationIssue{Path: "servicePlan.topology", Message: fmt.Sprintf("topology %q is not supported for YugabyteDB", ctx.Plan.Spec.Topology), Severity: HealthSeverityCritical})
	}
	params, err := a.parameters(ctx)
	if err != nil {
		issues = append(issues, ValidationIssue{Path: "parameters", Message: err.Error(), Severity: HealthSeverityCritical})
	} else if ctx.Plan.Spec.Topology == "multi-region" && len(params.StandbyClusters) == 0 && params.PrimaryCluster == "" {
		issues = append(issues, ValidationIssue{
			Path:     "parameters.standbyClusters",
			Message:  "multi-region topology requires at least one standby cluster",
			Severity: HealthSeverityWarning,
		})
	}
	return ValidationResult{Valid: len(issues) == 0, Issues: issues}, nil
}

func (a *YugabyteAdapter) Render(_ context.Context, request RenderRequest) (RenderResult, error) {
	ctx := request.Context
	if ctx.Instance == nil || ctx.Project == nil || ctx.Tenant == nil {
		return RenderResult{}, fmt.Errorf("tenant, project and instance context are required")
	}
	params, err := a.parameters(ctx)
	if err != nil {
		return RenderResult{}, err
	}

	namespace := instanceNamespace(ctx)
	primaryCluster := a.resolveCluster(ctx, params)
	version := effectiveVersion(ctx)
	databaseName := ResolveDatabaseName(ctx.Instance.Name, params.DatabaseName)
	if version == "" {
		version = "2.20.1.3-b3"
	}
	version = yugabyteSoftwareVersion(version)
	tserverReplicas := a.tserverReplicas(ctx, params)
	rf := a.replicationFactor(ctx, params)
	basePath := fmt.Sprintf("clusters/%s/tenants/%s/projects/%s/services/%s", primaryCluster, ctx.Tenant.Name, ctx.Project.Name, ctx.Instance.Name)
	labels := map[string]string{
		"servicer.io/managed-by":       "servicer",
		"servicer.io/database-name":    databaseName,
		"servicer.io/project":          ctx.Project.Name,
		"servicer.io/service-instance": ctx.Instance.Name,
		"servicer.io/tenant":           ctx.Tenant.Name,
	}

	namespaceManifest := map[string]any{
		"apiVersion": "v1",
		"kind":       "Namespace",
		"metadata": map[string]any{
			"name": namespace,
			"labels": map[string]string{
				"platform.mnorris.dev/profile": "workload",
				"servicer.io/managed-by":       "servicer",
				"servicer.io/project":          ctx.Project.Name,
				"servicer.io/tenant":           ctx.Tenant.Name,
			},
		},
	}

	clusterManifest := a.ybUniverseManifest(ctx.Instance.Name, yugabyteOperatorNamespace, version, rf, tserverReplicas, params, labels)

	type clusterArtifact struct {
		cluster string
		name    string
		body    map[string]any
	}
	artifacts := []clusterArtifact{
		{cluster: primaryCluster, name: "namespace.yaml", body: namespaceManifest},
		{cluster: primaryCluster, name: "ybuniverse.yaml", body: clusterManifest},
	}

	// For multi-region, render a linked YBUniverse on each standby cluster.
	if ctx.Plan != nil && ctx.Plan.Spec.Topology == "multi-region" {
		for _, standby := range params.StandbyClusters {
			standbyManifest := a.ybUniverseManifest(
				fmt.Sprintf("%s-standby", ctx.Instance.Name), yugabyteOperatorNamespace,
				version, rf, tserverReplicas, params, labels,
			)
			standbyManifest["metadata"].(map[string]any)["annotations"] = map[string]string{
				"servicer.io/xclustr-role":    "standby",
				"servicer.io/xclustr-primary": fmt.Sprintf("%s/%s", primaryCluster, ctx.Instance.Name),
			}
			artifacts = append(artifacts, clusterArtifact{cluster: standby, name: "ybuniverse.yaml", body: standbyManifest})
		}
		if len(params.StandbyClusters) > 0 {
			artifacts = append(artifacts, clusterArtifact{
				cluster: primaryCluster,
				name:    "xcluster-replication-job.yaml",
				body:    a.xClusterReplicationJob(ctx, namespace, databaseName, primaryCluster, params),
			})
		}
	}

	rendered := make([]RenderedArtifact, 0, len(artifacts))
	for _, art := range artifacts {
		path := fmt.Sprintf("clusters/%s/tenants/%s/projects/%s/services/%s/%s",
			art.cluster, ctx.Tenant.Name, ctx.Project.Name, ctx.Instance.Name, art.name)
		content, err := yaml.Marshal(art.body)
		if err != nil {
			return RenderResult{}, err
		}
		rendered = append(rendered, RenderedArtifact{Path: path, Content: content})
	}

	credSecretName := fmt.Sprintf("%s-credentials", ctx.Instance.Name)
	return RenderResult{
		RuntimeDriver: yugabyteDriver,
		PrimaryResource: &platformv1alpha1.TypedObjectReference{
			APIVersion: "operator.yugabyte.io/v1alpha1",
			Kind:       "YBUniverse",
			Name:       ctx.Instance.Name,
			Namespace:  yugabyteOperatorNamespace,
		},
		PackagePath: basePath,
		Endpoints: []Endpoint{
			{Name: "ysql", Address: fmt.Sprintf("%s-ysql.%s.svc.cluster.local:5433", ctx.Instance.Name, namespace), Port: 5433, Protocol: "tcp", Visibility: EndpointVisibilityClusterInternal},
			{Name: "ycql", Address: fmt.Sprintf("%s-ycql.%s.svc.cluster.local:9042", ctx.Instance.Name, namespace), Port: 9042, Protocol: "tcp", Visibility: EndpointVisibilityClusterInternal},
			{Name: "master-ui", Address: fmt.Sprintf("%s-master-ui.%s.svc.cluster.local:7000", ctx.Instance.Name, namespace), Port: 7000, Protocol: "http", Visibility: EndpointVisibilityClusterInternal},
			{Name: "tserver-ui", Address: fmt.Sprintf("%s-tserver-ui.%s.svc.cluster.local:9000", ctx.Instance.Name, namespace), Port: 9000, Protocol: "http", Visibility: EndpointVisibilityClusterInternal},
		},
		CredentialRefs: []platformv1alpha1.NamespacedObjectReference{{Name: credSecretName, Namespace: namespace}},
		Artifacts:      rendered,
	}, nil
}

func (a *YugabyteAdapter) Observe(_ context.Context, request ObserveRequest) (NormalizedStatus, error) {
	ctx := request.Context
	if ctx.Instance == nil {
		return NormalizedStatus{}, fmt.Errorf("service instance context is required")
	}
	namespace := instanceNamespace(ctx)
	phase := "Provisioning"
	summary := "Waiting for YugabyteDB cluster to initialise."
	healthSignals := []HealthSignal{
		{Key: "tserver-readiness", Status: "Unknown", Severity: HealthSeverityCritical, Message: "TServer readiness has not been observed yet."},
		{Key: "master-quorum", Status: "Unknown", Severity: HealthSeverityCritical, Message: "Master quorum has not been observed yet."},
		{Key: "replication-lag", Status: "Unknown", Severity: HealthSeverityWarning, Message: "Replication lag has not been observed yet."},
		{Key: "backup-freshness", Status: "Unknown", Severity: HealthSeverityWarning, Message: "Backup freshness has not been observed yet."},
	}
	syncPhase := SyncPhaseMaterialized
	syncMessage := "Waiting for Argo CD reconciliation."
	if request.Runtime.Blocked {
		phase = "Blocked"
		if request.Runtime.Message != "" {
			summary = request.Runtime.Message
		} else {
			summary = "YugabyteDB runtime dependency is missing in the target cluster."
		}
		syncPhase = SyncPhaseOutOfSync
		syncMessage = summary
		healthSignals[0] = HealthSignal{Key: "tserver-readiness", Status: "Blocked", Severity: HealthSeverityCritical, Message: summary}
		healthSignals[1] = HealthSignal{Key: "master-quorum", Status: "Blocked", Severity: HealthSeverityCritical, Message: summary}
	}
	if request.Runtime.Workload != nil && request.Runtime.Workload.Observed {
		workload := request.Runtime.Workload
		if workload.ReadyReplicas >= workload.DesiredReplicas {
			phase = "Ready"
			summary = fmt.Sprintf("YugabyteDB cluster is ready with %d/%d TServer(s).", workload.ReadyReplicas, workload.DesiredReplicas)
			healthSignals[0] = HealthSignal{Key: "tserver-readiness", Status: "Ready", Severity: HealthSeverityInfo, Message: "All TServers are ready."}
			healthSignals[1] = HealthSignal{Key: "master-quorum", Status: "Ready", Severity: HealthSeverityInfo, Message: "Master quorum is healthy."}
		} else {
			summary = fmt.Sprintf("Waiting for TServer readiness: %d/%d ready.", workload.ReadyReplicas, workload.DesiredReplicas)
		}
	}
	if ctx.Plan != nil && ctx.Plan.Spec.Topology == "multi-region" {
		params, err := a.parameters(ctx)
		if err != nil {
			return NormalizedStatus{}, err
		}
		healthSignals[2] = yugabyteReplicationLagSignal(params)
	} else {
		healthSignals[2] = HealthSignal{Key: "replication-lag", Status: "N/A", Severity: HealthSeverityInfo, Message: "Single-cluster topology — xCluster replication not applicable."}
	}

	endpoints := []Endpoint(nil)
	if phase == "Ready" {
		endpoints = []Endpoint{
			{Name: "ysql", Address: fmt.Sprintf("%s-ysql.%s.svc.cluster.local:5433", ctx.Instance.Name, namespace), Port: 5433, Protocol: "tcp", Visibility: EndpointVisibilityClusterInternal},
			{Name: "ycql", Address: fmt.Sprintf("%s-ycql.%s.svc.cluster.local:9042", ctx.Instance.Name, namespace), Port: 9042, Protocol: "tcp", Visibility: EndpointVisibilityClusterInternal},
		}
	}

	return NormalizedStatus{
		Phase:             phase,
		Summary:           summary,
		Conditions:        append([]metav1.Condition(nil), ctx.Instance.Status.Conditions...),
		CredentialRefs:    append([]platformv1alpha1.NamespacedObjectReference(nil), ctx.Instance.Status.CredentialRefs...),
		ObservedResources: append([]platformv1alpha1.TypedObjectReference(nil), request.ObservedResources...),
		Sync: SyncStatus{
			Phase:            syncPhase,
			ArtifactRevision: request.ArtifactRevision,
			ApplicationName:  request.ApplicationName,
			Message:          syncMessage,
		},
		HealthSignals: healthSignals,
		Endpoints:     endpoints,
	}, nil
}

func (a *YugabyteAdapter) Delete(_ context.Context, request DeleteRequest) (DeleteResult, error) {
	if request.Context.Instance == nil {
		return DeleteResult{}, fmt.Errorf("service instance context is required")
	}
	return DeleteResult{Message: "Delete requested for YugabyteDB instance."}, nil
}

func (a *YugabyteAdapter) SupportedActions(_ context.Context, _ ServiceContext) []ActionCapability {
	return append([]ActionCapability(nil), a.Contract().Actions...)
}

func (a *YugabyteAdapter) ExecuteAction(_ context.Context, request ExecuteActionRequest) (ActionExecutionResult, error) {
	if request.Context.Instance == nil || request.Action == nil {
		return ActionExecutionResult{}, fmt.Errorf("service instance and action request context are required")
	}
	return ActionExecutionResult{
		Phase: "Queued",
		OperationRef: &platformv1alpha1.TypedObjectReference{
			APIVersion: "operator.yugabyte.io/v1alpha1",
			Kind:       "YBUniverse",
			Name:       request.Context.Instance.Name,
			Namespace:  yugabyteOperatorNamespace,
		},
		Message:   fmt.Sprintf("YugabyteDB action %q queued for the operator-assisted runtime controller.", request.Action.Spec.Action),
		Retryable: true,
	}, nil
}

func (a *YugabyteAdapter) parameters(ctx ServiceContext) (yugabyteParameters, error) {
	params := yugabyteParameters{}
	if ctx.Plan != nil && ctx.Plan.Spec.DefaultParameters != nil {
		if err := json.Unmarshal(ctx.Plan.Spec.DefaultParameters.Raw, &params); err != nil {
			return yugabyteParameters{}, fmt.Errorf("decode plan defaults: %w", err)
		}
	}
	if ctx.Instance != nil && ctx.Instance.Spec.Parameters != nil {
		if err := json.Unmarshal(ctx.Instance.Spec.Parameters.Raw, &params); err != nil {
			return yugabyteParameters{}, fmt.Errorf("decode instance parameters: %w", err)
		}
	}
	if params.StorageSize == "" {
		params.StorageSize = "10Gi"
	}
	return params, nil
}

func (a *YugabyteAdapter) resolveCluster(ctx ServiceContext, params yugabyteParameters) string {
	if params.PrimaryCluster != "" {
		return params.PrimaryCluster
	}
	return instanceCluster(ctx)
}

func (a *YugabyteAdapter) tserverReplicas(ctx ServiceContext, params yugabyteParameters) int32 {
	if params.TServerReplicas != nil {
		return *params.TServerReplicas
	}
	if ctx.Plan != nil && ctx.Plan.Spec.Topology == "multi-region" {
		return 3
	}
	return 1
}

func (a *YugabyteAdapter) masterReplicas(ctx ServiceContext, params yugabyteParameters) int32 {
	if params.MasterReplicas != nil {
		return *params.MasterReplicas
	}
	return a.tserverReplicas(ctx, params)
}

func (a *YugabyteAdapter) replicationFactor(ctx ServiceContext, params yugabyteParameters) int32 {
	if params.ReplicationFactor != nil {
		return *params.ReplicationFactor
	}
	if ctx.Plan != nil && ctx.Plan.Spec.Topology == "multi-region" {
		return 3
	}
	return 1
}

func (a *YugabyteAdapter) ybUniverseManifest(name, namespace, version string, rf, tserverReplicas int32, params yugabyteParameters, labels map[string]string) map[string]any {
	deviceInfo := map[string]any{
		"numVolumes": 1,
		"volumeSize": ybVolumeSizeGi(params.StorageSize),
	}
	if params.StorageClass != "" {
		deviceInfo["storageClass"] = params.StorageClass
	}

	enableYSQL := true
	if params.EnableYSQL != nil {
		enableYSQL = *params.EnableYSQL
	}
	enableYCQL := false
	if params.EnableYCQL != nil {
		enableYCQL = *params.EnableYCQL
	}

	metadata := map[string]any{
		"name":      name,
		"namespace": namespace,
		"labels":    labels,
	}
	if params.BackupProfile != "" {
		metadata["annotations"] = map[string]string{
			"servicer.io/backup-profile": params.BackupProfile,
		}
	}

	return map[string]any{
		"apiVersion": "operator.yugabyte.io/v1alpha1",
		"kind":       "YBUniverse",
		"metadata":   metadata,
		"spec": map[string]any{
			"universeName":              name,
			"numNodes":                  tserverReplicas,
			"replicationFactor":         rf,
			"ybSoftwareVersion":         version,
			"enableYSQL":                enableYSQL,
			"enableYSQLAuth":            false,
			"enableYCQL":                enableYCQL,
			"enableYCQLAuth":            false,
			"enableLoadBalancer":        params.ServiceType == "LoadBalancer",
			"enableNodeToNodeEncrypt":   false,
			"enableClientToNodeEncrypt": false,
			"deviceInfo":                deviceInfo,
			"gFlags": map[string]any{
				"masterGFlags":  map[string]string{},
				"tserverGFlags": map[string]string{},
			},
			"kubernetesOverrides": a.kubernetesOverrides(params),
		},
	}
}

func (a *YugabyteAdapter) xClusterReplicationJob(ctx ServiceContext, namespace, databaseName, primaryCluster string, params yugabyteParameters) map[string]any {
	replicationName := fmt.Sprintf("%s-xcluster", ctx.Instance.Name)
	standbyMasters := make([]string, 0, len(params.StandbyClusters))
	for range params.StandbyClusters {
		standbyMasters = append(standbyMasters, fmt.Sprintf("%s-master.%s.svc.cluster.local:7100", standbyYugabyteUniverseName(ctx.Instance.Name), namespace))
	}
	command := fmt.Sprintf(
		`set -eu
yb-admin --master_addresses %s-master.%s.svc.cluster.local:7100 setup_universe_replication %s %s %s
yb-admin --master_addresses %s-master.%s.svc.cluster.local:7100 alter_universe_replication %s set_tables %s
`,
		ctx.Instance.Name, namespace,
		replicationName, stringsJoinShellArgs(standbyMasters), databaseName,
		ctx.Instance.Name, namespace,
		replicationName, databaseName,
	)
	return map[string]any{
		"apiVersion": "batch/v1",
		"kind":       "Job",
		"metadata": map[string]any{
			"name":      replicationName,
			"namespace": yugabyteOperatorNamespace,
			"labels": map[string]string{
				"servicer.io/managed-by":       "servicer",
				"servicer.io/project":          ctx.Project.Name,
				"servicer.io/service-instance": ctx.Instance.Name,
				"servicer.io/tenant":           ctx.Tenant.Name,
				"servicer.io/xcluster-primary": primaryCluster,
			},
			"annotations": map[string]string{
				"servicer.io/xcluster-standbys": stringsJoinShellArgs(params.StandbyClusters),
			},
		},
		"spec": map[string]any{
			"backoffLimit": int32(6),
			"template": map[string]any{
				"metadata": map[string]any{
					"labels": map[string]string{
						"servicer.io/service-instance": ctx.Instance.Name,
						"servicer.io/xcluster-job":     replicationName,
					},
				},
				"spec": map[string]any{
					"restartPolicy": "OnFailure",
					"containers": []map[string]any{{
						"name":    "yb-admin",
						"image":   "yugabytedb/yugabyte:latest",
						"command": []string{"/bin/sh", "-c", command},
					}},
				},
			},
		},
	}
}

func (a *YugabyteAdapter) kubernetesOverrides(params yugabyteParameters) map[string]any {
	masterCPU := firstNonEmpty(params.MasterCPU, params.CPU, "500m")
	masterMemory := firstNonEmpty(params.MasterMemory, params.Memory, "1Gi")
	tserverCPU := firstNonEmpty(params.TServerCPU, params.CPU, "500m")
	tserverMemory := firstNonEmpty(params.TServerMemory, params.Memory, "1Gi")
	overrides := map[string]any{
		"resource": map[string]any{
			"master":  ybResourceSpec(masterCPU, masterMemory),
			"tserver": ybResourceSpec(tserverCPU, tserverMemory),
		},
	}
	if (params.ServiceType == "LoadBalancer" || params.ServiceType == "NodePort") && params.ExternalDNSHostname != "" {
		overrides["serviceEndpoints"] = []map[string]any{
			{
				"name": "ysql",
				"annotations": map[string]string{
					"external-dns.alpha.kubernetes.io/hostname": params.ExternalDNSHostname,
				},
			},
		}
	}
	return overrides
}

func yugabyteReplicationLagSignal(params yugabyteParameters) HealthSignal {
	if len(params.StandbyClusters) == 0 {
		return HealthSignal{Key: "replication-lag", Status: "Pending", Severity: HealthSeverityWarning, Message: "xCluster replication requires at least one standby cluster."}
	}
	if len(params.ReplicationLagSeconds) == 0 {
		return HealthSignal{Key: "replication-lag", Status: "Configured", Severity: HealthSeverityInfo, Message: "xCluster replication intent is materialized; lag has not been reported yet."}
	}
	maxAllowed := int32(30)
	if params.MaxReplicationLagSeconds != nil && *params.MaxReplicationLagSeconds >= 0 {
		maxAllowed = *params.MaxReplicationLagSeconds
	}
	worstCluster := ""
	var worstLag int32
	for _, standby := range params.StandbyClusters {
		lag, ok := params.ReplicationLagSeconds[standby]
		if !ok {
			return HealthSignal{Key: "replication-lag", Status: "Unknown", Severity: HealthSeverityWarning, Message: fmt.Sprintf("Replication lag has not been reported for standby cluster %q.", standby)}
		}
		if lag >= worstLag {
			worstCluster = standby
			worstLag = lag
		}
	}
	if worstLag > maxAllowed {
		return HealthSignal{Key: "replication-lag", Status: "Degraded", Severity: HealthSeverityWarning, Message: fmt.Sprintf("Replication lag on standby cluster %q is %ds, above %ds threshold.", worstCluster, worstLag, maxAllowed)}
	}
	return HealthSignal{Key: "replication-lag", Status: "Ready", Severity: HealthSeverityInfo, Message: fmt.Sprintf("xCluster replication lag is within threshold; worst standby %q is %ds.", worstCluster, worstLag)}
}

func standbyYugabyteUniverseName(instanceName string) string {
	return fmt.Sprintf("%s-standby", instanceName)
}

func stringsJoinShellArgs(values []string) string {
	return strings.Join(values, ",")
}

func ybResourceSpec(cpu, memory string) map[string]any {
	return map[string]any{
		"requests": map[string]string{
			"cpu":    cpu,
			"memory": memory,
		},
		"limits": map[string]string{
			"cpu":    cpu,
			"memory": memory,
		},
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func ybVolumeSizeGi(value string) int32 {
	var size int32
	if _, err := fmt.Sscanf(value, "%dGi", &size); err == nil && size > 0 {
		return size
	}
	if _, err := fmt.Sscanf(value, "%dG", &size); err == nil && size > 0 {
		return size
	}
	if _, err := fmt.Sscanf(value, "%d", &size); err == nil && size > 0 {
		return size
	}
	return 10
}

func yugabyteSoftwareVersion(value string) string {
	switch value {
	case "", "2.20":
		return "2.20.1.3-b3"
	case "2.25":
		return "2.25.2.0-b359"
	default:
		return value
	}
}
