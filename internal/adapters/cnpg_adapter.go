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

type cnpgObjectStoreConfig struct {
	EndpointURL       string `json:"endpointUrl,omitempty"`
	Bucket            string `json:"bucket,omitempty"`
	Path              string `json:"path,omitempty"`
	Region            string `json:"region,omitempty"`
	CredentialsSecret string `json:"credentialsSecret,omitempty"`
}

type cnpgBackupConfig struct {
	ObjectStore *cnpgObjectStoreConfig `json:"objectStore,omitempty"`
	Schedule    string                 `json:"schedule,omitempty"`
	Retention   string                 `json:"retention,omitempty"`
}

type cnpgParameters struct {
	Instances           *int32            `json:"instances,omitempty"`
	DatabaseName        string            `json:"databaseName,omitempty"`
	StorageClass        string            `json:"storageClass,omitempty"`
	StorageSize         string            `json:"storageSize,omitempty"`
	Backup              *cnpgBackupConfig `json:"backup,omitempty"`
	ServiceType         string            `json:"serviceType,omitempty"`
	ExternalDNSHostname string            `json:"externalDnsHostname,omitempty"`
}

// CNPGAdapter is the first concrete runtime adapter implementation in Servicer.
type CNPGAdapter struct{}

// NewCNPGAdapter creates a CNPG-backed PostgreSQL adapter.
func NewCNPGAdapter() *CNPGAdapter {
	return &CNPGAdapter{}
}

func (a *CNPGAdapter) Contract() ProductContract {
	return PostgreSQLContract
}

func (a *CNPGAdapter) Validate(_ context.Context, request ValidationRequest) (ValidationResult, error) {
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

	if ctx.Class.Spec.Driver != "cnpg" {
		issues = append(issues, ValidationIssue{Path: "serviceClass.driver", Message: fmt.Sprintf("expected driver %q, got %q", "cnpg", ctx.Class.Spec.Driver), Severity: HealthSeverityCritical})
	}
	if ctx.Plan.Spec.ServiceClassRef.Name != ctx.Instance.Spec.ServiceClassRef.Name {
		issues = append(issues, ValidationIssue{Path: "servicePlanRef", Message: "service plan does not belong to the selected service class", Severity: HealthSeverityCritical})
	}
	if version := ctx.Instance.Spec.Version; version != "" {
		if !ctx.Class.Spec.AllowsVersionOverride && !ctx.Plan.Spec.AllowsVersionOverride && ctx.Plan.Spec.DefaultVersion != "" && version != ctx.Plan.Spec.DefaultVersion {
			issues = append(issues, ValidationIssue{Path: "version", Message: "version override is not allowed by the selected class and plan", Severity: HealthSeverityCritical})
		}
		if len(ctx.Class.Spec.SupportedVersions) > 0 && !containsString(ctx.Class.Spec.SupportedVersions, version) {
			issues = append(issues, ValidationIssue{Path: "version", Message: fmt.Sprintf("version %q is not in the supported versions list", version), Severity: HealthSeverityCritical})
		}
	}
	if _, err := a.parameters(ctx); err != nil {
		issues = append(issues, ValidationIssue{Path: "parameters", Message: err.Error(), Severity: HealthSeverityCritical})
	}

	return ValidationResult{Valid: len(issues) == 0, Issues: issues}, nil
}

func (a *CNPGAdapter) Render(_ context.Context, request RenderRequest) (RenderResult, error) {
	ctx := request.Context
	if ctx.Instance == nil || ctx.Project == nil || ctx.Tenant == nil {
		return RenderResult{}, fmt.Errorf("tenant, project and instance context are required")
	}
	if ctx.Tenant.Name == "" {
		return RenderResult{}, fmt.Errorf("tenant name is required")
	}

	parameters, err := a.parameters(ctx)
	if err != nil {
		return RenderResult{}, err
	}

	namespace := instanceNamespace(ctx)
	clusterName := instanceCluster(ctx)
	version := effectiveVersion(ctx)
	instances := a.defaultInstances(ctx.Plan, parameters)
	databaseName := ResolveDatabaseName(ctx.Instance.Name, parameters.DatabaseName)
	primaryResource := &platformv1alpha1.TypedObjectReference{
		APIVersion: "postgresql.cnpg.io/v1",
		Kind:       "Cluster",
		Name:       ctx.Instance.Name,
		Namespace:  namespace,
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

	clusterSpec := map[string]any{
		"instances": instances,
		"bootstrap": map[string]any{
			"initdb": map[string]any{
				"database": databaseName,
				"owner":    databaseName,
				"secret": map[string]any{
					"name": fmt.Sprintf("%s-app", ctx.Instance.Name),
				},
			},
		},
		"storage": map[string]any{
			"size": parameters.StorageSize,
		},
	}
	if parameters.StorageClass != "" {
		clusterSpec["storage"].(map[string]any)["storageClass"] = parameters.StorageClass
	}
	if version != "" {
		clusterSpec["imageName"] = fmt.Sprintf("ghcr.io/cloudnative-pg/postgresql:%s", version)
	}

	clusterManifest := map[string]any{
		"apiVersion": "postgresql.cnpg.io/v1",
		"kind":       "Cluster",
		"metadata": map[string]any{
			"name":      ctx.Instance.Name,
			"namespace": namespace,
			"labels": map[string]string{
				"servicer.io/managed-by":       "servicer",
				"servicer.io/project":          ctx.Project.Name,
				"servicer.io/service-instance": ctx.Instance.Name,
			},
		},
		"spec": clusterSpec,
	}

	artifacts := []RenderedArtifact{}
	basePath := fmt.Sprintf("clusters/%s/tenants/%s/projects/%s/services/%s", clusterName, ctx.Tenant.Name, ctx.Project.Name, ctx.Instance.Name)

	if b := parameters.Backup; b != nil && b.ObjectStore != nil && b.ObjectStore.Bucket != "" && b.ObjectStore.CredentialsSecret != "" {
		destinationPath := fmt.Sprintf("s3://%s", b.ObjectStore.Bucket)
		if b.ObjectStore.Path != "" {
			destinationPath = fmt.Sprintf("%s/%s", destinationPath, strings.TrimPrefix(b.ObjectStore.Path, "/"))
		}
		barman := map[string]any{
			"destinationPath": destinationPath,
			"s3Credentials": map[string]any{
				"accessKeyId": map[string]string{
					"name": b.ObjectStore.CredentialsSecret,
					"key":  "ACCESS_KEY_ID",
				},
				"secretAccessKey": map[string]string{
					"name": b.ObjectStore.CredentialsSecret,
					"key":  "ACCESS_SECRET_KEY",
				},
			},
			"wal":  map[string]any{"compression": "gzip"},
			"data": map[string]any{"compression": "gzip"},
		}
		if b.ObjectStore.EndpointURL != "" {
			barman["endpointURL"] = b.ObjectStore.EndpointURL
		}
		if b.ObjectStore.Region != "" {
			barman["s3Credentials"].(map[string]any)["region"] = b.ObjectStore.Region
		}
		backupSpec := map[string]any{
			"barmanObjectStore": barman,
			"target":            "prefer-standby",
		}
		if b.Retention != "" {
			backupSpec["retentionPolicy"] = b.Retention
		}
		clusterSpec["backup"] = backupSpec

		if b.Schedule != "" {
			scheduledBackupManifest := map[string]any{
				"apiVersion": "postgresql.cnpg.io/v1",
				"kind":       "ScheduledBackup",
				"metadata": map[string]any{
					"name":      fmt.Sprintf("%s-backup", ctx.Instance.Name),
					"namespace": namespace,
					"labels": map[string]string{
						"servicer.io/managed-by":       "servicer",
						"servicer.io/service-instance": ctx.Instance.Name,
					},
				},
				"spec": map[string]any{
					"schedule":             b.Schedule,
					"backupOwnerReference": "self",
					"cluster": map[string]string{
						"name": ctx.Instance.Name,
					},
					"immediate": false,
				},
			}
			scheduledBackupYAML, err := yaml.Marshal(scheduledBackupManifest)
			if err != nil {
				return RenderResult{}, err
			}
			artifacts = append(artifacts, RenderedArtifact{
				Path:    fmt.Sprintf("%s/scheduled-backup.yaml", basePath),
				Content: scheduledBackupYAML,
			})
		}
	}

	namespaceYAML, err := yaml.Marshal(namespaceManifest)
	if err != nil {
		return RenderResult{}, err
	}
	clusterYAML, err := yaml.Marshal(clusterManifest)
	if err != nil {
		return RenderResult{}, err
	}
	artifacts = append([]RenderedArtifact{
		{Path: fmt.Sprintf("%s/namespace.yaml", basePath), Content: namespaceYAML},
		{Path: fmt.Sprintf("%s/cnpg-cluster.yaml", basePath), Content: clusterYAML},
	}, artifacts...)

	return RenderResult{
		RuntimeDriver:   a.Contract().RuntimeDriver,
		PrimaryResource: primaryResource,
		PackagePath:     basePath,
		Endpoints:       cnpgEndpoints(ctx, namespace),
		CredentialRefs: []platformv1alpha1.NamespacedObjectReference{
			{Name: fmt.Sprintf("%s-app", ctx.Instance.Name), Namespace: namespace},
		},
		Artifacts: artifacts,
	}, nil
}

func (a *CNPGAdapter) Observe(_ context.Context, request ObserveRequest) (NormalizedStatus, error) {
	ctx := request.Context
	if ctx.Instance == nil {
		return NormalizedStatus{}, fmt.Errorf("service instance context is required")
	}

	status := NormalizedStatus{
		Phase:             ctx.Instance.Status.Phase,
		Summary:           ctx.Instance.Status.Health.Summary,
		Conditions:        append([]metav1.Condition(nil), ctx.Instance.Status.Conditions...),
		CredentialRefs:    append([]platformv1alpha1.NamespacedObjectReference(nil), ctx.Instance.Status.CredentialRefs...),
		ObservedResources: append([]platformv1alpha1.TypedObjectReference(nil), request.ObservedResources...),
		Sync: SyncStatus{
			Phase:            SyncPhaseMaterialized,
			ArtifactRevision: request.ArtifactRevision,
			ApplicationName:  request.ApplicationName,
			Message:          "Waiting for Argo CD reconciliation.",
		},
		HealthSignals: []HealthSignal{
			{Key: "primary-ready", Status: "Unknown", Severity: HealthSeverityCritical, Message: "Runtime health has not been observed yet."},
		},
	}

	if request.Runtime.Blocked {
		status.Phase = "Blocked"
		status.Summary = firstNonEmptyTrimmedAdapter(request.Runtime.Message, "CloudNative PG operator (postgresql.cnpg.io/v1) is not installed in the target cluster.")
		status.Sync.Phase = SyncPhaseMaterialized
		status.Sync.Message = status.Summary
		status.HealthSignals[0] = HealthSignal{Key: "primary-ready", Status: "Blocked", Severity: HealthSeverityCritical, Message: status.Summary}
	} else if w := request.Runtime.Workload; w != nil && w.Observed {
		if w.DesiredReplicas > 0 && w.ReadyReplicas >= w.DesiredReplicas {
			status.Phase = "Ready"
			status.Summary = fmt.Sprintf("PostgreSQL cluster healthy: %d/%d instances ready.", w.ReadyReplicas, w.DesiredReplicas)
			status.Sync.Phase = SyncPhaseSynced
			status.Sync.Message = "Runtime resources observed and healthy."
			status.HealthSignals[0] = HealthSignal{Key: "primary-ready", Status: "Ready", Severity: HealthSeverityInfo, Message: status.Summary}
		} else {
			status.Phase = "Provisioning"
			status.Summary = fmt.Sprintf("PostgreSQL cluster provisioning: %d/%d instances ready.", w.ReadyReplicas, w.DesiredReplicas)
			status.HealthSignals[0] = HealthSignal{Key: "primary-ready", Status: "Pending", Severity: HealthSeverityCritical, Message: status.Summary}
		}
	}

	if len(ctx.Instance.Status.Endpoints) > 0 {
		for name, address := range ctx.Instance.Status.Endpoints {
			status.Endpoints = append(status.Endpoints, Endpoint{Name: name, Address: address, Port: 5432, Protocol: "tcp", Visibility: EndpointVisibilityClusterInternal})
		}
	} else {
		status.Endpoints = cnpgEndpoints(ctx, instanceNamespace(ctx))
	}
	if ctx.Instance.Status.Runtime.ObjectRef != nil {
		status.ObservedResources = append(status.ObservedResources, *ctx.Instance.Status.Runtime.ObjectRef)
	}

	return status, nil
}

func cnpgEndpoints(ctx ServiceContext, namespace string) []Endpoint {
	if ctx.Instance == nil || namespace == "" {
		return nil
	}
	return []Endpoint{
		{Name: "primary", Address: fmt.Sprintf("%s-rw.%s.svc.cluster.local:5432", ctx.Instance.Name, namespace), Port: 5432, Protocol: "tcp", Visibility: EndpointVisibilityClusterInternal},
		{Name: "replicas", Address: fmt.Sprintf("%s-ro.%s.svc.cluster.local:5432", ctx.Instance.Name, namespace), Port: 5432, Protocol: "tcp", Visibility: EndpointVisibilityClusterInternal},
		{Name: "read-any", Address: fmt.Sprintf("%s-r.%s.svc.cluster.local:5432", ctx.Instance.Name, namespace), Port: 5432, Protocol: "tcp", Visibility: EndpointVisibilityClusterInternal},
	}
}

func (a *CNPGAdapter) Delete(_ context.Context, request DeleteRequest) (DeleteResult, error) {
	if request.Context.Instance == nil {
		return DeleteResult{}, fmt.Errorf("service instance context is required")
	}
	if request.Context.Instance.Spec.DeletionPolicy == platformv1alpha1.DeletionPolicySnapshot {
		return DeleteResult{Message: "Snapshot before deletion is requested for this PostgreSQL instance."}, nil
	}
	return DeleteResult{Message: "Delete requested for PostgreSQL instance."}, nil
}

func (a *CNPGAdapter) SupportedActions(_ context.Context, _ ServiceContext) []ActionCapability {
	return append([]ActionCapability(nil), a.Contract().Actions...)
}

func (a *CNPGAdapter) ExecuteAction(_ context.Context, request ExecuteActionRequest) (ActionExecutionResult, error) {
	if request.Context.Instance == nil || request.Action == nil {
		return ActionExecutionResult{}, fmt.Errorf("service instance and action request context are required")
	}

	namespace := instanceNamespace(request.Context)
	switch request.Action.Spec.Action {
	case string(ActionBackup):
		return ActionExecutionResult{
			Phase: "Queued",
			OperationRef: &platformv1alpha1.TypedObjectReference{
				APIVersion: "postgresql.cnpg.io/v1",
				Kind:       "Backup",
				Name:       request.Action.Name,
				Namespace:  namespace,
			},
			Message:   "CNPG backup request queued.",
			Retryable: true,
		}, nil
	case string(ActionFailover):
		return ActionExecutionResult{
			Phase: "Queued",
			OperationRef: &platformv1alpha1.TypedObjectReference{
				APIVersion: "postgresql.cnpg.io/v1",
				Kind:       "Cluster",
				Name:       request.Context.Instance.Name,
				Namespace:  namespace,
			},
			Message:   "CNPG failover request queued against the cluster.",
			Retryable: false,
		}, nil
	default:
		return ActionExecutionResult{Phase: "Queued", Message: fmt.Sprintf("Action %q queued for adapter implementation.", request.Action.Spec.Action), Retryable: true}, nil
	}
}

func (a *CNPGAdapter) parameters(ctx ServiceContext) (cnpgParameters, error) {
	parameters := cnpgParameters{}
	if ctx.Plan != nil && ctx.Plan.Spec.DefaultParameters != nil {
		if err := json.Unmarshal(ctx.Plan.Spec.DefaultParameters.Raw, &parameters); err != nil {
			return cnpgParameters{}, fmt.Errorf("decode plan defaults: %w", err)
		}
	}
	if ctx.Instance != nil && ctx.Instance.Spec.Parameters != nil {
		if err := json.Unmarshal(ctx.Instance.Spec.Parameters.Raw, &parameters); err != nil {
			return cnpgParameters{}, fmt.Errorf("decode instance parameters: %w", err)
		}
	}
	if parameters.StorageSize == "" {
		parameters.StorageSize = "20Gi"
	}
	return parameters, nil
}

func (a *CNPGAdapter) defaultInstances(plan *platformv1alpha1.ServicePlan, parameters cnpgParameters) int32 {
	if parameters.Instances != nil && *parameters.Instances > 0 {
		return *parameters.Instances
	}
	if plan != nil && strings.Contains(plan.Spec.Topology, "single") {
		return 1
	}
	return 3
}

func effectiveVersion(ctx ServiceContext) string {
	if ctx.Instance != nil && ctx.Instance.Spec.Version != "" {
		return ctx.Instance.Spec.Version
	}
	if ctx.Plan != nil && ctx.Plan.Spec.DefaultVersion != "" {
		return ctx.Plan.Spec.DefaultVersion
	}
	if ctx.Class != nil && len(ctx.Class.Spec.SupportedVersions) > 0 {
		return ctx.Class.Spec.SupportedVersions[0]
	}
	return ""
}

func instanceNamespace(ctx ServiceContext) string {
	if ctx.Instance != nil && ctx.Instance.Status.Placement.Namespace != "" {
		return ctx.Instance.Status.Placement.Namespace
	}
	if ctx.Project == nil || ctx.Instance == nil {
		return ""
	}
	tenant := ctx.Project.Spec.TenantRef.Name
	if tenant == "" && ctx.Tenant != nil {
		tenant = ctx.Tenant.Name
	}
	if tenant == "" {
		tenant = "tenant"
	}
	return fmt.Sprintf("%s-%s-%s", tenant, ctx.Project.Name, ctx.Instance.Name)
}

func instanceCluster(ctx ServiceContext) string {
	if ctx.Instance != nil && ctx.Instance.Status.Placement.ClusterName != "" {
		return ctx.Instance.Status.Placement.ClusterName
	}
	if ctx.Project != nil && ctx.Project.Status.Placement.ClusterName != "" {
		return ctx.Project.Status.Placement.ClusterName
	}
	if ctx.Project != nil && ctx.Project.Spec.TargetSelector.ClusterRef != nil {
		return ctx.Project.Spec.TargetSelector.ClusterRef.Name
	}
	return "unplaced"
}

func containsString(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}
