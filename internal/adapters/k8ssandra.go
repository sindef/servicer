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

const (
	k8ssandraRuntimeDriver = "k8ssandra"
	k8ssandraRuntimeAPI    = "k8ssandra.io/v1alpha1"
	k8ssandraRuntimeKind   = "K8ssandraCluster"
)

type k8ssandraParameters struct {
	Replicas       *int32   `json:"replicas,omitempty"`
	CPU            string   `json:"cpu,omitempty"`
	Memory         string   `json:"memory,omitempty"`
	HeapSize       string   `json:"heapSize,omitempty"`
	StorageClass   string   `json:"storageClass,omitempty"`
	StorageSize    string   `json:"storageSize,omitempty"`
	PrimaryCluster string   `json:"primaryCluster,omitempty"`
	Datacenters    []string `json:"datacenters,omitempty"`
	BackupProfile  string   `json:"backupProfile,omitempty"`
	RepairSchedule string   `json:"repairSchedule,omitempty"`
}

// K8ssandraContract describes the normalized platform contract for Cassandra services.
var K8ssandraContract = ProductContract{
	ServiceClass:            ServiceClassK8ssandra,
	FriendlyName:            "Cassandra",
	RuntimeDriver:           k8ssandraRuntimeDriver,
	SupportsVersionOverride: true,
	SupportsMultiCluster:    true,
	TopologyModes: []string{
		"single-datacenter",
		"multi-datacenter",
	},
	StatusSignals: []StatusSignalDescriptor{
		{Key: "ring-health", DisplayName: "Ring Health", Description: "Whether the Cassandra ring is healthy and converged.", Severity: HealthSeverityCritical},
		{Key: "repair-posture", DisplayName: "Repair Posture", Description: "Whether the cluster is within its repair and anti-entropy expectations.", Severity: HealthSeverityWarning},
		{Key: "backup-freshness", DisplayName: "Backup Freshness", Description: "Whether backups satisfy the plan's retention and recovery expectations.", Severity: HealthSeverityWarning},
	},
	Actions: []ActionCapability{},
}

// DefaultK8ssandraDeletionPolicy is the preferred default for Cassandra instances.
const DefaultK8ssandraDeletionPolicy = platformv1alpha1.DeletionPolicySnapshot

type K8ssandraAdapter struct{}

func NewK8ssandraAdapter() *K8ssandraAdapter {
	return &K8ssandraAdapter{}
}

func (a *K8ssandraAdapter) Contract() ProductContract {
	return K8ssandraContract
}

func (a *K8ssandraAdapter) Validate(_ context.Context, request ValidationRequest) (ValidationResult, error) {
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
	if ctx.Class.Spec.Driver != k8ssandraRuntimeDriver {
		issues = append(issues, ValidationIssue{Path: "serviceClass.driver", Message: fmt.Sprintf("expected driver %q, got %q", k8ssandraRuntimeDriver, ctx.Class.Spec.Driver), Severity: HealthSeverityCritical})
	}
	if ctx.Plan.Spec.ServiceClassRef.Name != ctx.Instance.Spec.ServiceClassRef.Name {
		issues = append(issues, ValidationIssue{Path: "servicePlanRef", Message: "service plan does not belong to the selected service class", Severity: HealthSeverityCritical})
	}
	if !containsString(a.Contract().TopologyModes, ctx.Plan.Spec.Topology) {
		issues = append(issues, ValidationIssue{Path: "servicePlan.topology", Message: fmt.Sprintf("topology %q is not supported for Cassandra", ctx.Plan.Spec.Topology), Severity: HealthSeverityCritical})
	}
	if version := ctx.Instance.Spec.Version; version != "" {
		if !ctx.Class.Spec.AllowsVersionOverride && !ctx.Plan.Spec.AllowsVersionOverride && ctx.Plan.Spec.DefaultVersion != "" && version != ctx.Plan.Spec.DefaultVersion {
			issues = append(issues, ValidationIssue{Path: "version", Message: "version override is not allowed by the selected class and plan", Severity: HealthSeverityCritical})
		}
		if len(ctx.Class.Spec.SupportedVersions) > 0 && !containsString(ctx.Class.Spec.SupportedVersions, version) {
			issues = append(issues, ValidationIssue{Path: "version", Message: fmt.Sprintf("version %q is not in the supported versions list", version), Severity: HealthSeverityCritical})
		}
	}
	params, err := a.parameters(ctx)
	if err != nil {
		issues = append(issues, ValidationIssue{Path: "parameters", Message: err.Error(), Severity: HealthSeverityCritical})
		return ValidationResult{Valid: false, Issues: issues}, nil
	}
	if ctx.Plan.Spec.Topology == "multi-datacenter" {
		if len(params.Datacenters) == 0 {
			issues = append(issues, ValidationIssue{Path: "parameters.datacenters", Message: "multi-datacenter Cassandra requires at least one additional datacenter", Severity: HealthSeverityCritical})
		}
		issues = append(issues, requiresPodMesh(ctx)...)
	}
	return ValidationResult{Valid: len(issues) == 0, Issues: issues}, nil
}

func (a *K8ssandraAdapter) Render(_ context.Context, request RenderRequest) (RenderResult, error) {
	ctx := request.Context
	if ctx.Instance == nil || ctx.Project == nil || ctx.Tenant == nil {
		return RenderResult{}, fmt.Errorf("tenant, project and instance context are required")
	}
	params, err := a.parameters(ctx)
	if err != nil {
		return RenderResult{}, err
	}
	version := effectiveVersion(ctx)
	if version == "" {
		version = "4.1.3"
	}
	replicas := a.replicas(params)
	namespace := instanceNamespace(ctx)
	primaryCluster := a.primaryCluster(ctx, params)
	packagePath := fmt.Sprintf("clusters/%s/tenants/%s/projects/%s/services/%s", primaryCluster, ctx.Tenant.Name, ctx.Project.Name, ctx.Instance.Name)
	datacenters := a.renderDatacenters(ctx, params, replicas)

	namespaceManifest := map[string]any{
		"apiVersion": "v1",
		"kind":       "Namespace",
		"metadata": map[string]any{
			"name": namespace,
			"labels": map[string]string{
				"servicer.io/managed-by": "servicer",
				"servicer.io/project":    ctx.Project.Name,
				"servicer.io/tenant":     ctx.Tenant.Name,
			},
		},
	}
	clusterManifest := map[string]any{
		"apiVersion": k8ssandraRuntimeAPI,
		"kind":       k8ssandraRuntimeKind,
		"metadata": map[string]any{
			"name":      ctx.Instance.Name,
			"namespace": namespace,
			"labels": map[string]string{
				"servicer.io/managed-by":       "servicer",
				"servicer.io/project":          ctx.Project.Name,
				"servicer.io/service-instance": ctx.Instance.Name,
				"servicer.io/topology":         ctx.Plan.Spec.Topology,
			},
			"annotations": map[string]string{
				"servicer.io/backup-profile": strings.TrimSpace(params.BackupProfile),
				"servicer.io/repair-window":  strings.TrimSpace(params.RepairSchedule),
			},
		},
		"spec": map[string]any{
			"cassandra": map[string]any{
				"serverVersion": version,
				"datacenters":   datacenters,
			},
		},
	}
	if params.BackupProfile != "" {
		clusterManifest["spec"].(map[string]any)["medusa"] = map[string]any{
			"enabled":       true,
			"backupProfile": params.BackupProfile,
		}
	}

	namespaceYAML, err := yaml.Marshal(namespaceManifest)
	if err != nil {
		return RenderResult{}, fmt.Errorf("marshal Cassandra namespace: %w", err)
	}
	clusterYAML, err := yaml.Marshal(clusterManifest)
	if err != nil {
		return RenderResult{}, fmt.Errorf("marshal Cassandra cluster: %w", err)
	}

	return RenderResult{
		RuntimeDriver: k8ssandraRuntimeDriver,
		PrimaryResource: &platformv1alpha1.TypedObjectReference{
			APIVersion: k8ssandraRuntimeAPI,
			Kind:       k8ssandraRuntimeKind,
			Name:       ctx.Instance.Name,
			Namespace:  namespace,
		},
		PackagePath: packagePath,
		Endpoints: []Endpoint{
			{Name: "cql", Address: fmt.Sprintf("%s.%s.svc.cluster.local:9042", ctx.Instance.Name, namespace), Port: 9042, Protocol: "tcp", Visibility: EndpointVisibilityClusterInternal},
		},
		Artifacts: []RenderedArtifact{
			{Path: fmt.Sprintf("%s/namespace.yaml", packagePath), Content: namespaceYAML},
			{Path: fmt.Sprintf("%s/k8ssandra-cluster.yaml", packagePath), Content: clusterYAML},
		},
	}, nil
}

func (a *K8ssandraAdapter) Observe(_ context.Context, request ObserveRequest) (NormalizedStatus, error) {
	ctx := request.Context
	if ctx.Instance == nil {
		return NormalizedStatus{}, fmt.Errorf("service instance context is required")
	}
	phase := "Provisioning"
	summary := "Waiting for Cassandra ring to initialise."
	healthSignals := []HealthSignal{
		{Key: "ring-health", Status: "Unknown", Severity: HealthSeverityCritical, Message: "Ring health has not been observed yet."},
		{Key: "repair-posture", Status: "Unknown", Severity: HealthSeverityWarning, Message: "Repair posture has not been observed yet."},
		{Key: "backup-freshness", Status: "Unknown", Severity: HealthSeverityWarning, Message: "Backup freshness has not been observed yet."},
	}
	syncPhase := SyncPhaseMaterialized
	syncMessage := "Waiting for Argo CD reconciliation."
	if request.Runtime.Blocked {
		phase = "Blocked"
		summary = firstNonEmptyTrimmedAdapter(request.Runtime.Message, "Cassandra runtime dependency is missing in the target cluster.")
		syncPhase = SyncPhaseOutOfSync
		syncMessage = summary
		healthSignals[0] = HealthSignal{Key: "ring-health", Status: "Blocked", Severity: HealthSeverityCritical, Message: summary}
	}
	if request.Runtime.Workload != nil && request.Runtime.Workload.Observed {
		workload := request.Runtime.Workload
		if workload.ReadyReplicas >= workload.DesiredReplicas {
			phase = "Ready"
			summary = fmt.Sprintf("Cassandra ring is ready with %d/%d node(s).", workload.ReadyReplicas, workload.DesiredReplicas)
			syncPhase = SyncPhaseSynced
			syncMessage = "Runtime resources observed and healthy."
			healthSignals[0] = HealthSignal{Key: "ring-health", Status: "Ready", Severity: HealthSeverityInfo, Message: "All Cassandra nodes are ready."}
			healthSignals[1] = HealthSignal{Key: "repair-posture", Status: "Configured", Severity: HealthSeverityInfo, Message: "Repair posture is managed through K8ssandra automation."}
		} else {
			summary = fmt.Sprintf("Waiting for Cassandra ring readiness: %d/%d node(s) ready.", workload.ReadyReplicas, workload.DesiredReplicas)
			healthSignals[0] = HealthSignal{Key: "ring-health", Status: "Pending", Severity: HealthSeverityCritical, Message: summary}
		}
	}
	params, err := a.parameters(ctx)
	if err != nil {
		return NormalizedStatus{}, err
	}
	if params.BackupProfile != "" {
		healthSignals[2] = HealthSignal{Key: "backup-freshness", Status: "Configured", Severity: HealthSeverityInfo, Message: fmt.Sprintf("Backup profile %q is attached.", params.BackupProfile)}
	} else {
		healthSignals[2] = HealthSignal{Key: "backup-freshness", Status: "Unconfigured", Severity: HealthSeverityWarning, Message: "No backup profile is configured for this Cassandra instance."}
	}
	endpoints := []Endpoint(nil)
	if phase == "Ready" {
		endpoints = []Endpoint{
			{Name: "cql", Address: fmt.Sprintf("%s.%s.svc.cluster.local:9042", ctx.Instance.Name, instanceNamespace(ctx)), Port: 9042, Protocol: "tcp", Visibility: EndpointVisibilityClusterInternal},
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

func (a *K8ssandraAdapter) Delete(_ context.Context, request DeleteRequest) (DeleteResult, error) {
	if request.Context.Instance == nil {
		return DeleteResult{}, fmt.Errorf("service instance context is required")
	}
	if request.Context.Instance.Spec.DeletionPolicy == platformv1alpha1.DeletionPolicySnapshot {
		return DeleteResult{Message: "Snapshot before deletion is requested for this Cassandra instance."}, nil
	}
	return DeleteResult{Message: "Delete requested for Cassandra instance."}, nil
}

func (a *K8ssandraAdapter) SupportedActions(_ context.Context, _ ServiceContext) []ActionCapability {
	return append([]ActionCapability(nil), a.Contract().Actions...)
}

func (a *K8ssandraAdapter) ExecuteAction(_ context.Context, request ExecuteActionRequest) (ActionExecutionResult, error) {
	if request.Context.Instance == nil || request.Action == nil {
		return ActionExecutionResult{}, fmt.Errorf("service instance and action request context are required")
	}
	return ActionExecutionResult{
		Phase: "Queued",
		OperationRef: &platformv1alpha1.TypedObjectReference{
			APIVersion: k8ssandraRuntimeAPI,
			Kind:       k8ssandraRuntimeKind,
			Name:       request.Context.Instance.Name,
			Namespace:  instanceNamespace(request.Context),
		},
		Message:   fmt.Sprintf("Cassandra action %q queued for the K8ssandra runtime controller.", request.Action.Spec.Action),
		Retryable: true,
	}, nil
}

func (a *K8ssandraAdapter) parameters(ctx ServiceContext) (k8ssandraParameters, error) {
	params := k8ssandraParameters{}
	if ctx.Plan != nil && ctx.Plan.Spec.DefaultParameters != nil {
		if err := json.Unmarshal(ctx.Plan.Spec.DefaultParameters.Raw, &params); err != nil {
			return k8ssandraParameters{}, fmt.Errorf("decode plan defaults: %w", err)
		}
	}
	if ctx.Instance != nil && ctx.Instance.Spec.Parameters != nil {
		if err := json.Unmarshal(ctx.Instance.Spec.Parameters.Raw, &params); err != nil {
			return k8ssandraParameters{}, fmt.Errorf("decode instance parameters: %w", err)
		}
	}
	if params.StorageSize == "" {
		params.StorageSize = "100Gi"
	}
	return params, nil
}

func (a *K8ssandraAdapter) replicas(params k8ssandraParameters) int32 {
	if params.Replicas != nil && *params.Replicas > 0 {
		return *params.Replicas
	}
	return 3
}

func (a *K8ssandraAdapter) primaryCluster(ctx ServiceContext, params k8ssandraParameters) string {
	if params.PrimaryCluster != "" {
		return params.PrimaryCluster
	}
	return instanceCluster(ctx)
}

func (a *K8ssandraAdapter) renderDatacenters(ctx ServiceContext, params k8ssandraParameters, replicas int32) []map[string]any {
	clusters := []string{a.primaryCluster(ctx, params)}
	for _, dc := range params.Datacenters {
		dc = strings.TrimSpace(dc)
		if dc != "" && !containsString(clusters, dc) {
			clusters = append(clusters, dc)
		}
	}
	datacenters := make([]map[string]any, 0, len(clusters))
	for _, cluster := range clusters {
		dc := map[string]any{
			"metadata": map[string]any{
				"name": fmt.Sprintf("%s-%s", ctx.Instance.Name, sanitizeK8ssandraName(cluster)),
			},
			"size": replicas,
			"storageConfig": map[string]any{
				"cassandraDataVolumeClaimSpec": map[string]any{
					"accessModes": []string{"ReadWriteOnce"},
					"resources": map[string]any{
						"requests": map[string]string{
							"storage": params.StorageSize,
						},
					},
				},
			},
		}
		if params.StorageClass != "" {
			dc["storageConfig"].(map[string]any)["cassandraDataVolumeClaimSpec"].(map[string]any)["storageClassName"] = params.StorageClass
		}
		if params.CPU != "" || params.Memory != "" || params.HeapSize != "" {
			podTemplate := map[string]any{}
			container := map[string]any{}
			if params.CPU != "" || params.Memory != "" {
				container["resources"] = map[string]any{
					"requests": compactStringMap(map[string]string{
						"cpu":    params.CPU,
						"memory": params.Memory,
					}),
				}
			}
			if params.HeapSize != "" {
				container["env"] = []map[string]string{{"name": "MAX_HEAP_SIZE", "value": params.HeapSize}}
			}
			podTemplate["spec"] = map[string]any{
				"containers": []map[string]any{container},
			}
			dc["podTemplateSpec"] = podTemplate
		}
		datacenters = append(datacenters, dc)
	}
	return datacenters
}

func sanitizeK8ssandraName(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		case !lastDash:
			b.WriteByte('-')
			lastDash = true
		}
	}
	result := strings.Trim(b.String(), "-")
	if result == "" {
		return "dc1"
	}
	return result
}

func compactStringMap(values map[string]string) map[string]string {
	compact := make(map[string]string, len(values))
	for key, value := range values {
		if strings.TrimSpace(value) != "" {
			compact[key] = value
		}
	}
	return compact
}

func firstNonEmptyTrimmedAdapter(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
