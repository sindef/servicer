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
	mysqlRuntimeDriver = "servicer-mysql"
	mysqlRuntimeKind   = "StatefulSet"
	mysqlRuntimeAPI    = "apps/v1"
	mysqlModeGalera    = "galera"
	mysqlModeActive    = "active-passive"
	mysqlModeSingle    = "single-primary"
)

type mysqlParameters struct {
	Replicas                *int32   `json:"replicas,omitempty"`
	DatabaseName            string   `json:"databaseName,omitempty"`
	CPU                     string   `json:"cpu,omitempty"`
	Memory                  string   `json:"memory,omitempty"`
	StorageClass            string   `json:"storageClass,omitempty"`
	StorageSize             string   `json:"storageSize,omitempty"`
	BackupProfile           string   `json:"backupProfile,omitempty"`
	ReplicationMode         string   `json:"replicationMode,omitempty"`
	PrimaryCluster          string   `json:"primaryCluster,omitempty"`
	StandbyClusters         []string `json:"standbyClusters,omitempty"`
	ServiceType             string   `json:"serviceType,omitempty"`
	ExternalDNSHostname     string   `json:"externalDnsHostname,omitempty"`
	FailoverMonitorInterval string   `json:"failoverMonitorInterval,omitempty"`
}

var MySQLContract = ProductContract{
	ServiceClass:            ServiceClassMySQL,
	FriendlyName:            "MySQL",
	RuntimeDriver:           mysqlRuntimeDriver,
	SupportsVersionOverride: true,
	SupportsMultiCluster:    true,
	TopologyModes: []string{
		"single-cluster",
		"multi-region",
	},
	StatusSignals: []StatusSignalDescriptor{
		{Key: "primary-ready", DisplayName: "Primary Ready", Description: "Whether the writable MySQL primary is ready.", Severity: HealthSeverityCritical},
		{Key: "replica-posture", DisplayName: "Replica Posture", Description: "Whether MySQL replicas or peers match the selected topology model.", Severity: HealthSeverityWarning},
		{Key: "credential-health", DisplayName: "Credential Health", Description: "Whether the application credential secret is projected and rotation-ready.", Severity: HealthSeverityCritical},
		{Key: "failover-readiness", DisplayName: "Failover Readiness", Description: "Whether the active-passive topology has a promotion candidate available.", Severity: HealthSeverityWarning},
	},
	Actions: []ActionCapability{
		{Name: ActionScale, DisplayName: "Scale", RequiresApproval: false, Disruptive: true},
		{Name: ActionRestart, DisplayName: "Restart", RequiresApproval: false, Disruptive: true},
		{Name: ActionRotateCredentials, DisplayName: "Rotate Credentials", RequiresApproval: false, Disruptive: false},
		{Name: ActionFailover, DisplayName: "Failover", RequiresApproval: true, Disruptive: true},
		{Name: ActionRollbackFailover, DisplayName: "Rollback Failover", RequiresApproval: true, Disruptive: true},
	},
}

const DefaultMySQLDeletionPolicy = platformv1alpha1.DeletionPolicySnapshot

type MySQLAdapter struct{}

func NewMySQLAdapter() *MySQLAdapter {
	return &MySQLAdapter{}
}

func (a *MySQLAdapter) Contract() ProductContract {
	return MySQLContract
}

func (a *MySQLAdapter) Validate(_ context.Context, request ValidationRequest) (ValidationResult, error) {
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
	if ctx.Class.Spec.Driver != mysqlRuntimeDriver {
		issues = append(issues, ValidationIssue{Path: "serviceClass.driver", Message: fmt.Sprintf("expected driver %q, got %q", mysqlRuntimeDriver, ctx.Class.Spec.Driver), Severity: HealthSeverityCritical})
	}
	if ctx.Plan.Spec.ServiceClassRef.Name != ctx.Instance.Spec.ServiceClassRef.Name {
		issues = append(issues, ValidationIssue{Path: "servicePlanRef", Message: "service plan does not belong to the selected service class", Severity: HealthSeverityCritical})
	}
	if !containsString(a.Contract().TopologyModes, ctx.Plan.Spec.Topology) {
		issues = append(issues, ValidationIssue{Path: "servicePlan.topology", Message: fmt.Sprintf("topology %q is not supported for MySQL", ctx.Plan.Spec.Topology), Severity: HealthSeverityCritical})
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
	} else {
		issues = append(issues, a.validateParameters(ctx, params)...)
	}
	return ValidationResult{Valid: len(issues) == 0, Issues: issues}, nil
}

func (a *MySQLAdapter) Render(_ context.Context, request RenderRequest) (RenderResult, error) {
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
		version = "8.4"
	}
	databaseName := ResolveDatabaseName(ctx.Instance.Name, params.DatabaseName)
	mode := a.replicationMode(ctx, params)
	primaryCluster := a.primaryCluster(ctx, params)
	clusterRoles := a.renderClusterRoles(ctx, params)
	packagePaths := make([]string, 0, len(clusterRoles))
	artifacts := make([]RenderedArtifact, 0, len(clusterRoles)*8)
	for _, role := range clusterRoles {
		packagePath := mysqlPackagePath(ctx, role.cluster)
		packagePaths = append(packagePaths, packagePath)
		clusterArtifacts, err := a.renderClusterArtifacts(ctx, params, version, databaseName, primaryCluster, role.cluster, role.role, packagePath)
		if err != nil {
			return RenderResult{}, err
		}
		artifacts = append(artifacts, clusterArtifacts...)
	}
	namespace := instanceNamespace(ctx)
	endpoints := []Endpoint{
		{Name: "primary", Address: fmt.Sprintf("%s.%s.svc.cluster.local:3306", ctx.Instance.Name, namespace), Port: 3306, Protocol: "tcp", Visibility: EndpointVisibilityClusterInternal},
	}
	if a.defaultReplicas(ctx, params) > 1 || mode == mysqlModeGalera {
		endpoints = append(endpoints, Endpoint{Name: "read", Address: fmt.Sprintf("%s-read.%s.svc.cluster.local:3306", ctx.Instance.Name, namespace), Port: 3306, Protocol: "tcp", Visibility: EndpointVisibilityClusterInternal})
	}
	if ctx.Plan != nil && ctx.Plan.Spec.Topology == "multi-region" {
		endpoints = append(endpoints, Endpoint{Name: "traffic", Address: fmt.Sprintf("%s.%s.mysql.servicer.local:3306", ctx.Instance.Name, ctx.Project.Name), Port: 3306, Protocol: "tcp", Visibility: EndpointVisibilityPrivate})
	}
	return RenderResult{
		RuntimeDriver: a.Contract().RuntimeDriver,
		PrimaryResource: &platformv1alpha1.TypedObjectReference{
			APIVersion: mysqlRuntimeAPI,
			Kind:       mysqlRuntimeKind,
			Name:       ctx.Instance.Name,
			Namespace:  namespace,
		},
		PackagePath:  mysqlPackagePath(ctx, primaryCluster),
		PackagePaths: packagePaths,
		Endpoints:    endpoints,
		CredentialRefs: []platformv1alpha1.NamespacedObjectReference{
			{Name: fmt.Sprintf("%s-auth", ctx.Instance.Name), Namespace: namespace},
		},
		Artifacts: artifacts,
	}, nil
}

func (a *MySQLAdapter) Observe(_ context.Context, request ObserveRequest) (NormalizedStatus, error) {
	ctx := request.Context
	if ctx.Instance == nil {
		return NormalizedStatus{}, fmt.Errorf("service instance context is required")
	}
	params, err := a.parameters(ctx)
	if err != nil {
		return NormalizedStatus{}, err
	}
	mode := a.replicationMode(ctx, params)
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
			{Key: "replica-posture", Status: "Unknown", Severity: HealthSeverityWarning, Message: "Replica or peer posture has not been observed yet."},
			{Key: "credential-health", Status: "Pending", Severity: HealthSeverityCritical, Message: "Credential projection is pending."},
			{Key: "failover-readiness", Status: "NotApplicable", Severity: HealthSeverityInfo, Message: "Failover applies only to active-passive multi-region MySQL."},
		},
	}
	if request.Runtime.CredentialSecretPresent {
		status.HealthSignals[2] = HealthSignal{Key: "credential-health", Status: "Ready", Severity: HealthSeverityInfo, Message: "Application credentials are present."}
	}
	if request.Runtime.Workload != nil && request.Runtime.Workload.Observed {
		w := request.Runtime.Workload
		if w.DesiredReplicas > 0 && w.ReadyReplicas >= w.DesiredReplicas {
			status.Phase = "Ready"
			status.Sync.Phase = SyncPhaseSynced
			status.Sync.Message = "Runtime resources observed and healthy."
			switch mode {
			case mysqlModeGalera:
				status.Summary = fmt.Sprintf("MySQL Galera cluster healthy: %d/%d peers ready.", w.ReadyReplicas, w.DesiredReplicas)
				status.HealthSignals[1] = HealthSignal{Key: "replica-posture", Status: "Healthy", Severity: HealthSeverityInfo, Message: "Galera peers are ready for quorum-based writes."}
			case mysqlModeActive:
				status.Summary = fmt.Sprintf("MySQL active-passive primary healthy: %d/%d replicas ready.", w.ReadyReplicas, w.DesiredReplicas)
				status.HealthSignals[1] = HealthSignal{Key: "replica-posture", Status: "Configured", Severity: HealthSeverityInfo, Message: fmt.Sprintf("%d standby cluster(s) are materialized for replication.", len(a.standbyClusters(ctx, params)))}
				if len(a.standbyClusters(ctx, params)) > 0 {
					status.HealthSignals[3] = HealthSignal{Key: "failover-readiness", Status: "Ready", Severity: HealthSeverityInfo, Message: fmt.Sprintf("Primary cluster %q has %d standby candidate(s).", a.primaryCluster(ctx, params), len(a.standbyClusters(ctx, params)))}
				} else {
					status.HealthSignals[3] = HealthSignal{Key: "failover-readiness", Status: "Blocked", Severity: HealthSeverityWarning, Message: "No standby clusters are configured for failover."}
				}
			default:
				status.Summary = fmt.Sprintf("MySQL runtime healthy: %d/%d replicas ready.", w.ReadyReplicas, w.DesiredReplicas)
				if w.DesiredReplicas > 1 {
					status.HealthSignals[1] = HealthSignal{Key: "replica-posture", Status: "Healthy", Severity: HealthSeverityInfo, Message: "Same-cluster replicas are ready."}
				} else {
					status.HealthSignals[1] = HealthSignal{Key: "replica-posture", Status: "SinglePrimary", Severity: HealthSeverityInfo, Message: "Single-primary MySQL deployment selected."}
				}
			}
			status.HealthSignals[0] = HealthSignal{Key: "primary-ready", Status: "Ready", Severity: HealthSeverityInfo, Message: status.Summary}
		} else {
			status.Phase = "Provisioning"
			status.Summary = fmt.Sprintf("MySQL provisioning: %d/%d replicas ready.", w.ReadyReplicas, w.DesiredReplicas)
			status.HealthSignals[0] = HealthSignal{Key: "primary-ready", Status: "Pending", Severity: HealthSeverityCritical, Message: status.Summary}
		}
	}
	if len(ctx.Instance.Status.Endpoints) > 0 {
		for name, address := range ctx.Instance.Status.Endpoints {
			status.Endpoints = append(status.Endpoints, Endpoint{Name: name, Address: address, Port: 3306, Protocol: "tcp", Visibility: EndpointVisibilityClusterInternal})
		}
	}
	if ctx.Instance.Status.Runtime.ObjectRef != nil {
		status.ObservedResources = append(status.ObservedResources, *ctx.Instance.Status.Runtime.ObjectRef)
	}
	return status, nil
}

func (a *MySQLAdapter) Delete(_ context.Context, request DeleteRequest) (DeleteResult, error) {
	if request.Context.Instance == nil {
		return DeleteResult{}, fmt.Errorf("service instance context is required")
	}
	if request.Context.Instance.Spec.DeletionPolicy == platformv1alpha1.DeletionPolicySnapshot {
		return DeleteResult{Message: "Snapshot before deletion is requested for this MySQL instance."}, nil
	}
	return DeleteResult{Message: "Delete requested for MySQL instance."}, nil
}

func (a *MySQLAdapter) SupportedActions(_ context.Context, ctx ServiceContext) []ActionCapability {
	params, err := a.parameters(ctx)
	if err != nil {
		return append([]ActionCapability(nil), a.Contract().Actions[:3]...)
	}
	mode := a.replicationMode(ctx, params)
	actions := make([]ActionCapability, 0, len(a.Contract().Actions))
	for _, action := range a.Contract().Actions {
		if (action.Name == ActionFailover || action.Name == ActionRollbackFailover) && mode != mysqlModeActive {
			continue
		}
		actions = append(actions, action)
	}
	return actions
}

func (a *MySQLAdapter) ExecuteAction(_ context.Context, request ExecuteActionRequest) (ActionExecutionResult, error) {
	if request.Context.Instance == nil || request.Action == nil {
		return ActionExecutionResult{}, fmt.Errorf("service instance and action request context are required")
	}
	switch request.Action.Spec.Action {
	default:
		return ActionExecutionResult{}, fmt.Errorf("unsupported MySQL action %q", request.Action.Spec.Action)
	}
}

func (a *MySQLAdapter) parameters(ctx ServiceContext) (mysqlParameters, error) {
	params := mysqlParameters{}
	if ctx.Plan != nil && ctx.Plan.Spec.DefaultParameters != nil {
		if err := json.Unmarshal(ctx.Plan.Spec.DefaultParameters.Raw, &params); err != nil {
			return mysqlParameters{}, fmt.Errorf("decode plan defaults: %w", err)
		}
	}
	if ctx.Instance != nil && ctx.Instance.Spec.Parameters != nil {
		if err := json.Unmarshal(ctx.Instance.Spec.Parameters.Raw, &params); err != nil {
			return mysqlParameters{}, fmt.Errorf("decode instance parameters: %w", err)
		}
	}
	if params.StorageSize == "" {
		params.StorageSize = "100Gi"
	}
	if params.CPU == "" {
		params.CPU = "1"
	}
	if params.Memory == "" {
		params.Memory = "2Gi"
	}
	if params.ServiceType == "" {
		params.ServiceType = "ClusterIP"
	}
	if params.FailoverMonitorInterval == "" {
		params.FailoverMonitorInterval = "10s"
	}
	return params, nil
}

func (a *MySQLAdapter) validateParameters(ctx ServiceContext, params mysqlParameters) []ValidationIssue {
	issues := make([]ValidationIssue, 0)
	mode := a.replicationMode(ctx, params)
	switch mode {
	case mysqlModeSingle, mysqlModeGalera, mysqlModeActive:
	default:
		issues = append(issues, ValidationIssue{Path: "parameters.replicationMode", Message: fmt.Sprintf("replicationMode %q is unsupported", mode), Severity: HealthSeverityCritical})
	}
	if ctx.Plan != nil && ctx.Plan.Spec.Topology == "multi-region" {
		if len(a.standbyClusters(ctx, params)) == 0 {
			issues = append(issues, ValidationIssue{Path: "parameters.standbyClusters", Message: "multi-region MySQL plans require at least one standby or peer cluster", Severity: HealthSeverityCritical})
		}
		if mode == mysqlModeSingle {
			issues = append(issues, ValidationIssue{Path: "parameters.replicationMode", Message: "multi-region MySQL plans require galera or active-passive replicationMode", Severity: HealthSeverityCritical})
		}
		if mode == mysqlModeGalera {
			// Galera replication gossips on pod IPs; a cross-cluster network fabric is required.
			issues = append(issues, requiresPodMesh(ctx)...)
		}
	}
	if params.Replicas != nil && *params.Replicas < 1 {
		issues = append(issues, ValidationIssue{Path: "parameters.replicas", Message: "replicas must be at least 1", Severity: HealthSeverityCritical})
	}
	if mode == mysqlModeGalera && a.defaultReplicas(ctx, params) < 3 {
		issues = append(issues, ValidationIssue{Path: "parameters.replicas", Message: "Galera plans should run at least 3 peers for quorum", Severity: HealthSeverityWarning})
	}
	return issues
}

type mysqlRenderClusterRole struct {
	cluster string
	role    string
}

func (a *MySQLAdapter) renderClusterRoles(ctx ServiceContext, params mysqlParameters) []mysqlRenderClusterRole {
	primaryCluster := a.primaryCluster(ctx, params)
	roles := []mysqlRenderClusterRole{{cluster: primaryCluster, role: "primary"}}
	if ctx.Plan == nil || ctx.Plan.Spec.Topology != "multi-region" {
		return roles
	}
	for _, standby := range a.standbyClusters(ctx, params) {
		if standby == primaryCluster {
			continue
		}
		role := "standby"
		if a.replicationMode(ctx, params) == mysqlModeGalera {
			role = "peer"
		}
		roles = append(roles, mysqlRenderClusterRole{cluster: standby, role: role})
	}
	return roles
}

func (a *MySQLAdapter) renderClusterArtifacts(ctx ServiceContext, params mysqlParameters, version, databaseName, primaryCluster, clusterName, role, basePath string) ([]RenderedArtifact, error) {
	namespace := instanceNamespace(ctx)
	replicas := a.defaultReplicas(ctx, params)
	mode := a.replicationMode(ctx, params)
	authSecretName := fmt.Sprintf("%s-auth", ctx.Instance.Name)
	configName := fmt.Sprintf("%s-config", ctx.Instance.Name)
	headlessServiceName := fmt.Sprintf("%s-headless", ctx.Instance.Name)
	readServiceName := fmt.Sprintf("%s-read", ctx.Instance.Name)
	labels := map[string]string{
		"servicer.io/managed-by":       "servicer",
		"servicer.io/project":          ctx.Project.Name,
		"servicer.io/runtime":          "mysql",
		"servicer.io/service-instance": ctx.Instance.Name,
		"servicer.io/mysql-role":       role,
		"servicer.io/mysql-mode":       mode,
		"servicer.io/target-cluster":   clusterName,
		"servicer.io/primary-cluster":  primaryCluster,
	}
	selector := map[string]string{
		"servicer.io/runtime":          "mysql",
		"servicer.io/service-instance": ctx.Instance.Name,
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
	serviceAnnotations := map[string]string{}
	if params.ExternalDNSHostname != "" && (params.ServiceType == "LoadBalancer" || params.ServiceType == "NodePort") {
		serviceAnnotations["external-dns.alpha.kubernetes.io/hostname"] = params.ExternalDNSHostname
	}
	serviceManifest := map[string]any{
		"apiVersion": "v1",
		"kind":       "Service",
		"metadata": map[string]any{
			"name":        ctx.Instance.Name,
			"namespace":   namespace,
			"labels":      labels,
			"annotations": serviceAnnotations,
		},
		"spec": map[string]any{
			"type":     params.ServiceType,
			"selector": selector,
			"ports": []map[string]any{{
				"name":       "mysql",
				"port":       3306,
				"targetPort": 3306,
				"protocol":   "TCP",
			}},
		},
	}
	readServiceManifest := map[string]any{
		"apiVersion": "v1",
		"kind":       "Service",
		"metadata": map[string]any{
			"name":      readServiceName,
			"namespace": namespace,
			"labels":    labels,
		},
		"spec": map[string]any{
			"selector": selector,
			"ports": []map[string]any{{
				"name":       "mysql",
				"port":       3306,
				"targetPort": 3306,
				"protocol":   "TCP",
			}},
		},
	}
	headlessManifest := map[string]any{
		"apiVersion": "v1",
		"kind":       "Service",
		"metadata": map[string]any{
			"name":      headlessServiceName,
			"namespace": namespace,
			"labels":    labels,
		},
		"spec": map[string]any{
			"clusterIP": "None",
			"selector":  selector,
			"ports": []map[string]any{{
				"name":       "mysql",
				"port":       3306,
				"targetPort": 3306,
				"protocol":   "TCP",
			}},
		},
	}
	configManifest := map[string]any{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata": map[string]any{
			"name":      configName,
			"namespace": namespace,
			"labels":    labels,
		},
		"data": map[string]string{
			"my.cnf": a.mysqlConfig(ctx, params, role, primaryCluster, clusterName),
			"servicer-topology.txt": strings.Join([]string{
				fmt.Sprintf("mode=%s", mode),
				fmt.Sprintf("role=%s", role),
				fmt.Sprintf("primaryCluster=%s", primaryCluster),
				fmt.Sprintf("cluster=%s", clusterName),
				fmt.Sprintf("database=%s", databaseName),
			}, "\n") + "\n",
		},
	}
	statefulSetManifest := map[string]any{
		"apiVersion": mysqlRuntimeAPI,
		"kind":       mysqlRuntimeKind,
		"metadata": map[string]any{
			"name":      ctx.Instance.Name,
			"namespace": namespace,
			"labels":    labels,
		},
		"spec": map[string]any{
			"serviceName": headlessServiceName,
			"replicas":    replicas,
			"selector": map[string]any{
				"matchLabels": selector,
			},
			"template": map[string]any{
				"metadata": map[string]any{
					"labels": selector,
					"annotations": map[string]string{
						"servicer.io/mysql-role":      role,
						"servicer.io/mysql-mode":      mode,
						"servicer.io/primary-cluster": primaryCluster,
					},
				},
				"spec": map[string]any{
					"containers": []map[string]any{{
						"name":  "mysql",
						"image": fmt.Sprintf("mysql:%s", version),
						"ports": []map[string]any{
							{"containerPort": 3306, "name": "mysql"},
							{"containerPort": 33060, "name": "xprotocol"},
							{"containerPort": 4567, "name": "galera"},
						},
						"env": []map[string]any{
							{"name": "MYSQL_DATABASE", "value": databaseName},
							{"name": "MYSQL_USER", "valueFrom": map[string]any{"secretKeyRef": map[string]any{"name": authSecretName, "key": "username"}}},
							{"name": "MYSQL_PASSWORD", "valueFrom": map[string]any{"secretKeyRef": map[string]any{"name": authSecretName, "key": "password"}}},
							{"name": "MYSQL_ROOT_PASSWORD", "valueFrom": map[string]any{"secretKeyRef": map[string]any{"name": authSecretName, "key": "password"}}},
						},
						"resources": map[string]any{
							"requests": map[string]string{
								"cpu":    params.CPU,
								"memory": params.Memory,
							},
							"limits": map[string]string{
								"cpu":    params.CPU,
								"memory": params.Memory,
							},
						},
						"volumeMounts": []map[string]any{
							{"name": "config", "mountPath": "/etc/mysql/conf.d"},
							{"name": "data", "mountPath": "/var/lib/mysql"},
						},
						"readinessProbe": map[string]any{
							"exec": map[string]any{
								"command": []string{"/bin/sh", "-c", "mysqladmin ping -h 127.0.0.1 -uroot -p\"$MYSQL_ROOT_PASSWORD\""},
							},
							"initialDelaySeconds": 20,
							"periodSeconds":       10,
						},
						"livenessProbe": map[string]any{
							"tcpSocket":           map[string]any{"port": 3306},
							"initialDelaySeconds": 30,
							"periodSeconds":       15,
						},
					}},
					"volumes": []map[string]any{{
						"name": "config",
						"configMap": map[string]any{
							"name": configName,
						},
					}},
				},
			},
			"volumeClaimTemplates": []map[string]any{{
				"metadata": map[string]any{
					"name": "data",
				},
				"spec": map[string]any{
					"accessModes": []string{"ReadWriteOnce"},
					"resources": map[string]any{
						"requests": map[string]string{
							"storage": params.StorageSize,
						},
					},
				},
			}},
		},
	}
	if params.StorageClass != "" {
		statefulSetManifest["spec"].(map[string]any)["volumeClaimTemplates"].([]map[string]any)[0]["spec"].(map[string]any)["storageClassName"] = params.StorageClass
	}
	manifests := []struct {
		name string
		body map[string]any
	}{
		{name: "namespace.yaml", body: namespaceManifest},
		{name: "service.yaml", body: serviceManifest},
		{name: "headless-service.yaml", body: headlessManifest},
		{name: "read-service.yaml", body: readServiceManifest},
		{name: "configmap.yaml", body: configManifest},
		{name: "statefulset.yaml", body: statefulSetManifest},
	}
	if params.BackupProfile != "" {
		backupPVC, backupCronJob := a.backupManifests(ctx, params, version, databaseName, namespace, authSecretName, labels)
		manifests = append(manifests,
			struct {
				name string
				body map[string]any
			}{name: "backup-pvc.yaml", body: backupPVC},
			struct {
				name string
				body map[string]any
			}{name: "backup-cronjob.yaml", body: backupCronJob},
		)
	}
	result := make([]RenderedArtifact, 0, len(manifests))
	for _, manifest := range manifests {
		content, err := yaml.Marshal(manifest.body)
		if err != nil {
			return nil, err
		}
		result = append(result, RenderedArtifact{
			Path:    fmt.Sprintf("%s/%s", basePath, manifest.name),
			Content: content,
		})
	}
	return result, nil
}

func (a *MySQLAdapter) backupManifests(ctx ServiceContext, params mysqlParameters, version, databaseName, namespace, authSecretName string, labels map[string]string) (map[string]any, map[string]any) {
	backupName := fmt.Sprintf("%s-backups", ctx.Instance.Name)
	pvc := map[string]any{
		"apiVersion": "v1",
		"kind":       "PersistentVolumeClaim",
		"metadata": map[string]any{
			"name":      backupName,
			"namespace": namespace,
			"labels":    labels,
			"annotations": map[string]string{
				"servicer.io/backup-profile": params.BackupProfile,
			},
		},
		"spec": map[string]any{
			"accessModes": []string{"ReadWriteOnce"},
			"resources": map[string]any{
				"requests": map[string]string{"storage": mysqlBackupStorageSize(params)},
			},
		},
	}
	if params.StorageClass != "" {
		pvc["spec"].(map[string]any)["storageClassName"] = params.StorageClass
	}

	cronJob := map[string]any{
		"apiVersion": "batch/v1",
		"kind":       "CronJob",
		"metadata": map[string]any{
			"name":      fmt.Sprintf("%s-backup", ctx.Instance.Name),
			"namespace": namespace,
			"labels":    labels,
			"annotations": map[string]string{
				"servicer.io/backup-profile": params.BackupProfile,
				"servicer.io/retention-days": mysqlBackupRetentionDays(params.BackupProfile),
			},
		},
		"spec": map[string]any{
			"schedule":                   mysqlBackupSchedule(params.BackupProfile),
			"successfulJobsHistoryLimit": int32(3),
			"failedJobsHistoryLimit":     int32(3),
			"jobTemplate": map[string]any{
				"spec": map[string]any{
					"template": map[string]any{
						"spec": map[string]any{
							"restartPolicy": "OnFailure",
							"containers": []map[string]any{{
								"name":  "mysqldump",
								"image": fmt.Sprintf("mysql:%s", version),
								"env": []map[string]any{
									{"name": "MYSQL_USER", "valueFrom": map[string]any{"secretKeyRef": map[string]any{"name": authSecretName, "key": "username"}}},
									{"name": "MYSQL_PASSWORD", "valueFrom": map[string]any{"secretKeyRef": map[string]any{"name": authSecretName, "key": "password"}}},
								},
								"command": []string{"/bin/sh", "-c", fmt.Sprintf("set -eu; ts=$(date +%%Y%%m%%d%%H%%M%%S); mysqldump -h %s.%s.svc.cluster.local -u \"$MYSQL_USER\" -p\"$MYSQL_PASSWORD\" --single-transaction --databases %s | gzip > /backups/%s-${ts}.sql.gz", ctx.Instance.Name, namespace, databaseName, databaseName)},
								"volumeMounts": []map[string]any{{
									"name":      "backups",
									"mountPath": "/backups",
								}},
							}},
							"volumes": []map[string]any{{
								"name": "backups",
								"persistentVolumeClaim": map[string]string{
									"claimName": backupName,
								},
							}},
						},
					},
				},
			},
		},
	}
	return pvc, cronJob
}

func (a *MySQLAdapter) mysqlConfig(ctx ServiceContext, params mysqlParameters, role, primaryCluster, clusterName string) string {
	mode := a.replicationMode(ctx, params)
	lines := []string{
		"[mysqld]",
		"skip-name-resolve=ON",
		"bind-address=0.0.0.0",
		"log-bin=mysql-bin",
		"binlog_format=ROW",
		"gtid_mode=ON",
		"enforce_gtid_consistency=ON",
		"innodb_flush_log_at_trx_commit=1",
		"sync_binlog=1",
	}
	switch mode {
	case mysqlModeGalera:
		lines = append(lines,
			"wsrep_on=ON",
			"wsrep_provider=/usr/lib/galera/libgalera_smm.so",
			fmt.Sprintf("wsrep_cluster_name=%s", ctx.Instance.Name),
			fmt.Sprintf("wsrep_cluster_address=gcomm://%s", strings.Join(a.galeraPeers(ctx, params), ",")),
			fmt.Sprintf("wsrep_node_name=%s-%s", clusterName, role),
			"wsrep_sst_method=rsync",
			"default_storage_engine=InnoDB",
			"innodb_autoinc_lock_mode=2",
		)
	case mysqlModeActive:
		lines = append(lines,
			fmt.Sprintf("read_only=%t", role != "primary"),
			fmt.Sprintf("super_read_only=%t", role != "primary"),
			fmt.Sprintf("report_host=%s", clusterName),
			fmt.Sprintf("# servicer_primary_cluster=%s", primaryCluster),
			fmt.Sprintf("# servicer_failover_monitor_interval=%s", params.FailoverMonitorInterval),
		)
	default:
		lines = append(lines,
			"read_only=OFF",
			"super_read_only=OFF",
		)
	}
	return strings.Join(lines, "\n") + "\n"
}

func mysqlBackupSchedule(profile string) string {
	switch strings.TrimSpace(profile) {
	case "hourly-24h":
		return "0 * * * *"
	case "weekly-4w":
		return "0 3 * * 0"
	default:
		return "0 2 * * *"
	}
}

func mysqlBackupRetentionDays(profile string) string {
	switch strings.TrimSpace(profile) {
	case "hourly-24h":
		return "1"
	case "weekly-4w":
		return "28"
	default:
		return "7"
	}
}

func mysqlBackupStorageSize(params mysqlParameters) string {
	if params.StorageSize == "" {
		return "20Gi"
	}
	return params.StorageSize
}

func (a *MySQLAdapter) galeraPeers(ctx ServiceContext, params mysqlParameters) []string {
	roles := a.renderClusterRoles(ctx, params)
	namespace := instanceNamespace(ctx)
	headlessSvc := fmt.Sprintf("%s-headless", ctx.Instance.Name)
	peers := make([]string, 0, len(roles))
	for _, role := range roles {
		// Single-cluster: resolves correctly within the cluster.
		// Multi-cluster: cross-cluster peer DNS requires a flat network or
		// service mesh (e.g. Cilium Cluster Mesh, Submariner clusterset.local).
		// The cluster field is recorded as a label but does not change the DNS
		// address until a cross-cluster networking layer is in place.
		_ = role
		peers = append(peers, fmt.Sprintf("%s.%s.%s.svc.cluster.local",
			ctx.Instance.Name, headlessSvc, namespace))
	}
	return peers
}

func (a *MySQLAdapter) defaultReplicas(ctx ServiceContext, params mysqlParameters) int32 {
	if params.Replicas != nil && *params.Replicas > 0 {
		return *params.Replicas
	}
	mode := a.replicationMode(ctx, params)
	if mode == mysqlModeGalera {
		return 3
	}
	if ctx.Plan != nil && ctx.Plan.Spec.Topology == "single-cluster" {
		return 3
	}
	return 1
}

func (a *MySQLAdapter) replicationMode(ctx ServiceContext, params mysqlParameters) string {
	mode := strings.TrimSpace(params.ReplicationMode)
	if mode != "" {
		return mode
	}
	if ctx.Plan != nil && ctx.Plan.Spec.Topology == "multi-region" {
		return mysqlModeActive
	}
	return mysqlModeSingle
}

func (a *MySQLAdapter) primaryCluster(ctx ServiceContext, params mysqlParameters) string {
	if strings.TrimSpace(params.PrimaryCluster) != "" {
		return strings.TrimSpace(params.PrimaryCluster)
	}
	return instanceCluster(ctx)
}

func (a *MySQLAdapter) standbyClusters(ctx ServiceContext, params mysqlParameters) []string {
	seen := map[string]struct{}{}
	clusters := make([]string, 0, len(params.StandbyClusters))
	primary := a.primaryCluster(ctx, params)
	for _, standby := range params.StandbyClusters {
		name := strings.TrimSpace(standby)
		if name == "" || name == primary {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		clusters = append(clusters, name)
	}
	return clusters
}

func mysqlPackagePath(ctx ServiceContext, clusterName string) string {
	return fmt.Sprintf("clusters/%s/tenants/%s/projects/%s/services/%s", clusterName, ctx.Tenant.Name, ctx.Project.Name, ctx.Instance.Name)
}
