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
	valkeyRuntimeAPIVersion = "apps/v1"
	valkeyRuntimeKind       = "StatefulSet"
	valkeyDriver            = "servicer-valkey"
)

type valkeyParameters struct {
	Replicas                 *int32           `json:"replicas,omitempty"`
	MemoryProfile            string           `json:"memoryProfile,omitempty"`
	MemoryLimit              string           `json:"memoryLimit,omitempty"`
	Persistence              string           `json:"persistence,omitempty"`
	StorageClass             string           `json:"storageClass,omitempty"`
	StorageSize              string           `json:"storageSize,omitempty"`
	MaxMemoryPolicy          string           `json:"maxMemoryPolicy,omitempty"`
	PrimaryCluster           string           `json:"primaryCluster,omitempty"`
	StandbyClusters          []string         `json:"standbyClusters,omitempty"`
	ReplicationLagSeconds    map[string]int32 `json:"replicationLagSeconds,omitempty"`
	MaxReplicationLagSeconds *int32           `json:"maxReplicationLagSeconds,omitempty"`
	ServiceType              string           `json:"serviceType,omitempty"`
	ExternalDNSHostname      string           `json:"externalDnsHostname,omitempty"`
}

// ValkeyContract describes the normalized platform contract for Valkey-backed cache services.
var ValkeyContract = ProductContract{
	ServiceClass:            ServiceClassValkey,
	FriendlyName:            "Valkey",
	RuntimeDriver:           valkeyDriver,
	SupportsVersionOverride: true,
	SupportsMultiCluster:    true,
	TopologyModes: []string{
		"single-cluster",
		"multi-cluster-failover",
		"multi-region",
	},
	StatusSignals: []StatusSignalDescriptor{
		{Key: "primary-ready", DisplayName: "Primary Ready", Description: "Whether the writable cache primary is ready to serve traffic.", Severity: HealthSeverityCritical},
		{Key: "replica-posture", DisplayName: "Replica Posture", Description: "Whether same-cluster replicas match the selected plan posture.", Severity: HealthSeverityWarning},
		{Key: "credential-health", DisplayName: "Credential Health", Description: "Whether cache credentials are projected and rotation-ready.", Severity: HealthSeverityCritical},
		{Key: "failover-readiness", DisplayName: "Failover Readiness", Description: "Whether the cache topology can support a controlled failover action.", Severity: HealthSeverityWarning},
	},
	Actions: []ActionCapability{
		{Name: ActionScale, DisplayName: "Scale", RequiresApproval: false, Disruptive: true},
		{Name: ActionRestart, DisplayName: "Restart", RequiresApproval: false, Disruptive: true},
		{Name: ActionRotateCredentials, DisplayName: "Rotate Credentials", RequiresApproval: false, Disruptive: false},
		{Name: ActionFailover, DisplayName: "Failover", RequiresApproval: true, Disruptive: true},
		{Name: ActionRollbackFailover, DisplayName: "Rollback Failover", RequiresApproval: true, Disruptive: true},
		{Name: ActionResyncStandby, DisplayName: "Resync Standby", RequiresApproval: true, Disruptive: true},
	},
}

// ValkeyAdapter is the first Servicer-owned runtime path for cache services.
type ValkeyAdapter struct{}

// NewValkeyAdapter creates a Servicer-owned Valkey adapter.
func NewValkeyAdapter() *ValkeyAdapter {
	return &ValkeyAdapter{}
}

func (a *ValkeyAdapter) Contract() ProductContract {
	return ValkeyContract
}

func (a *ValkeyAdapter) Validate(_ context.Context, request ValidationRequest) (ValidationResult, error) {
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

	if ctx.Class.Spec.Driver != valkeyDriver {
		issues = append(issues, ValidationIssue{Path: "serviceClass.driver", Message: fmt.Sprintf("expected driver %q, got %q", valkeyDriver, ctx.Class.Spec.Driver), Severity: HealthSeverityCritical})
	}
	if ctx.Plan.Spec.ServiceClassRef.Name != ctx.Instance.Spec.ServiceClassRef.Name {
		issues = append(issues, ValidationIssue{Path: "servicePlanRef", Message: "service plan does not belong to the selected service class", Severity: HealthSeverityCritical})
	}
	if !containsString(a.Contract().TopologyModes, ctx.Plan.Spec.Topology) {
		issues = append(issues, ValidationIssue{Path: "servicePlan.topology", Message: fmt.Sprintf("topology %q is not supported for the first Valkey release", ctx.Plan.Spec.Topology), Severity: HealthSeverityCritical})
	}
	if version := ctx.Instance.Spec.Version; version != "" {
		if !ctx.Class.Spec.AllowsVersionOverride && !ctx.Plan.Spec.AllowsVersionOverride && ctx.Plan.Spec.DefaultVersion != "" && version != ctx.Plan.Spec.DefaultVersion {
			issues = append(issues, ValidationIssue{Path: "version", Message: "version override is not allowed by the selected class and plan", Severity: HealthSeverityCritical})
		}
		if len(ctx.Class.Spec.SupportedVersions) > 0 && !containsString(ctx.Class.Spec.SupportedVersions, version) {
			issues = append(issues, ValidationIssue{Path: "version", Message: fmt.Sprintf("version %q is not in the supported versions list", version), Severity: HealthSeverityCritical})
		}
	}

	parameters, err := a.parameters(ctx)
	if err != nil {
		issues = append(issues, ValidationIssue{Path: "parameters", Message: err.Error(), Severity: HealthSeverityCritical})
	} else {
		issues = append(issues, a.validateParameters(ctx, parameters)...)
	}

	return ValidationResult{Valid: len(issues) == 0, Issues: issues}, nil
}

func (a *ValkeyAdapter) Render(_ context.Context, request RenderRequest) (RenderResult, error) {
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

	version := effectiveVersion(ctx)
	if version == "" {
		version = "8.0"
	}

	clusterRoles := a.renderClusterRoles(ctx, parameters)
	primaryCluster := a.primaryCluster(ctx, parameters)
	primaryPackagePath := valkeyPackagePath(ctx, primaryCluster)
	artifacts := make([]RenderedArtifact, 0, len(clusterRoles)*6)
	packagePaths := make([]string, 0, len(clusterRoles))
	for _, clusterRole := range clusterRoles {
		packagePath := valkeyPackagePath(ctx, clusterRole.cluster)
		packagePaths = append(packagePaths, packagePath)
		rendered, err := a.renderClusterArtifacts(ctx, parameters, version, clusterRole.cluster, primaryCluster, clusterRole.role, packagePath)
		if err != nil {
			return RenderResult{}, err
		}
		artifacts = append(artifacts, rendered...)
	}

	namespace := instanceNamespace(ctx)
	authSecretName := fmt.Sprintf("%s-auth", ctx.Instance.Name)
	endpoints := []Endpoint{
		{Name: "primary", Address: fmt.Sprintf("%s.%s.svc.cluster.local:6379", ctx.Instance.Name, namespace), Port: 6379, Protocol: "tcp", Visibility: EndpointVisibilityClusterInternal},
	}
	if isValkeyFailoverTopology(planTopology(ctx)) {
		endpoints = append(endpoints, Endpoint{Name: "traffic", Address: fmt.Sprintf("%s.%s.cache.servicer.local:6379", ctx.Instance.Name, ctx.Project.Name), Port: 6379, Protocol: "tcp", Visibility: EndpointVisibilityPrivate})
	}

	return RenderResult{
		RuntimeDriver: a.Contract().RuntimeDriver,
		PrimaryResource: &platformv1alpha1.TypedObjectReference{
			APIVersion: valkeyRuntimeAPIVersion,
			Kind:       valkeyRuntimeKind,
			Name:       ctx.Instance.Name,
			Namespace:  namespace,
		},
		PackagePath:  primaryPackagePath,
		PackagePaths: packagePaths,
		Endpoints:    endpoints,
		CredentialRefs: []platformv1alpha1.NamespacedObjectReference{
			{Name: authSecretName, Namespace: namespace},
		},
		Artifacts: artifacts,
	}, nil
}

type valkeyRenderClusterRole struct {
	cluster string
	role    string
}

func (a *ValkeyAdapter) renderClusterRoles(ctx ServiceContext, parameters valkeyParameters) []valkeyRenderClusterRole {
	primaryCluster := a.primaryCluster(ctx, parameters)
	roles := []valkeyRenderClusterRole{{cluster: primaryCluster, role: "primary"}}
	if !isValkeyFailoverTopology(planTopology(ctx)) {
		return roles
	}
	for _, standbyCluster := range a.standbyClusters(ctx, parameters) {
		if standbyCluster == primaryCluster {
			continue
		}
		roles = append(roles, valkeyRenderClusterRole{cluster: standbyCluster, role: "standby"})
	}
	return roles
}

func (a *ValkeyAdapter) renderClusterArtifacts(ctx ServiceContext, parameters valkeyParameters, version, clusterName, primaryCluster, role, basePath string) ([]RenderedArtifact, error) {
	namespace := instanceNamespace(ctx)
	replicas := a.replicas(ctx, parameters)
	labels := map[string]string{
		"servicer.io/cache-role":       role,
		"servicer.io/managed-by":       "servicer",
		"servicer.io/primary-cluster":  primaryCluster,
		"servicer.io/project":          ctx.Project.Name,
		"servicer.io/service-instance": ctx.Instance.Name,
		"servicer.io/target-cluster":   clusterName,
		"servicer.io/runtime":          "valkey",
		"servicer.io/topology":         planTopology(ctx),
	}
	selectorLabels := map[string]string{
		"servicer.io/service-instance": ctx.Instance.Name,
		"servicer.io/runtime":          "valkey",
	}
	authSecretName := fmt.Sprintf("%s-auth", ctx.Instance.Name)
	configName := fmt.Sprintf("%s-config", ctx.Instance.Name)
	headlessServiceName := fmt.Sprintf("%s-headless", ctx.Instance.Name)
	primaryEndpoint := fmt.Sprintf("%s.%s.svc.cluster.local", ctx.Instance.Name, namespace)

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
	configManifest := map[string]any{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata": map[string]any{
			"name":      configName,
			"namespace": namespace,
			"labels":    labels,
			"annotations": map[string]string{
				"servicer.io/replication-source": primaryEndpoint,
			},
		},
		"data": map[string]string{
			"valkey.conf": a.valkeyConfig(parameters, role, primaryEndpoint),
		},
	}
	serviceManifest := map[string]any{
		"apiVersion": "v1",
		"kind":       "Service",
		"metadata": map[string]any{
			"name":      ctx.Instance.Name,
			"namespace": namespace,
			"labels":    labels,
		},
		"spec": map[string]any{
			"ports":    []map[string]any{{"name": "valkey", "port": int32(6379), "targetPort": int32(6379)}},
			"selector": selectorLabels,
		},
	}
	if parameters.ServiceType != "" && parameters.ServiceType != "ClusterIP" {
		serviceManifest["spec"].(map[string]any)["type"] = parameters.ServiceType
	}
	if (parameters.ServiceType == "LoadBalancer" || parameters.ServiceType == "NodePort") && parameters.ExternalDNSHostname != "" {
		serviceManifest["metadata"].(map[string]any)["annotations"] = map[string]string{
			"external-dns.alpha.kubernetes.io/hostname": parameters.ExternalDNSHostname,
		}
	}
	headlessServiceManifest := map[string]any{
		"apiVersion": "v1",
		"kind":       "Service",
		"metadata": map[string]any{
			"name":      headlessServiceName,
			"namespace": namespace,
			"labels":    labels,
		},
		"spec": map[string]any{
			"clusterIP": "None",
			"ports":     []map[string]any{{"name": "valkey", "port": int32(6379), "targetPort": int32(6379)}},
			"selector":  selectorLabels,
		},
	}
	statefulSetManifest := map[string]any{
		"apiVersion": valkeyRuntimeAPIVersion,
		"kind":       valkeyRuntimeKind,
		"metadata": map[string]any{
			"name":      ctx.Instance.Name,
			"namespace": namespace,
			"labels":    labels,
		},
		"spec": map[string]any{
			"serviceName": headlessServiceName,
			"replicas":    replicas,
			"selector": map[string]any{
				"matchLabels": selectorLabels,
			},
			"template": map[string]any{
				"metadata": map[string]any{"labels": labels},
				"spec": map[string]any{
					"containers": []map[string]any{a.containerSpec(version, configName, authSecretName, parameters)},
					"volumes":    a.podVolumes(configName, parameters),
				},
			},
		},
	}
	if parameters.Persistence == "persistent" {
		statefulSetManifest["spec"].(map[string]any)["volumeClaimTemplates"] = []map[string]any{a.volumeClaimTemplate(parameters)}
	}
	trafficPolicyManifest := map[string]any{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata": map[string]any{
			"name":      fmt.Sprintf("%s-traffic-policy", ctx.Instance.Name),
			"namespace": namespace,
			"labels":    labels,
		},
		"data": map[string]string{
			"mode":            planTopology(ctx),
			"role":            role,
			"primaryCluster":  primaryCluster,
			"targetCluster":   clusterName,
			"trafficEndpoint": fmt.Sprintf("%s.%s.cache.servicer.local:6379", ctx.Instance.Name, ctx.Project.Name),
			"primaryEndpoint": fmt.Sprintf("%s.%s.svc.cluster.local:6379", ctx.Instance.Name, namespace),
			"standbyClusters": strings.Join(parameters.StandbyClusters, ","),
		},
	}

	manifests := []struct {
		name string
		body map[string]any
	}{
		{name: "namespace.yaml", body: namespaceManifest},
		{name: "configmap.yaml", body: configManifest},
		{name: "service.yaml", body: serviceManifest},
		{name: "headless-service.yaml", body: headlessServiceManifest},
		{name: "statefulset.yaml", body: statefulSetManifest},
	}
	if isValkeyFailoverTopology(planTopology(ctx)) {
		manifests = append(manifests, struct {
			name string
			body map[string]any
		}{name: "traffic-policy.yaml", body: trafficPolicyManifest})
	}

	artifacts := make([]RenderedArtifact, 0, len(manifests))
	for _, manifest := range manifests {
		content, err := yaml.Marshal(manifest.body)
		if err != nil {
			return nil, err
		}
		artifacts = append(artifacts, RenderedArtifact{Path: fmt.Sprintf("%s/%s", basePath, manifest.name), Content: content})
	}
	return artifacts, nil
}

func valkeyPackagePath(ctx ServiceContext, clusterName string) string {
	return fmt.Sprintf("clusters/%s/tenants/%s/projects/%s/services/%s", clusterName, ctx.Tenant.Name, ctx.Project.Name, ctx.Instance.Name)
}

func (a *ValkeyAdapter) Observe(_ context.Context, request ObserveRequest) (NormalizedStatus, error) {
	ctx := request.Context
	if ctx.Instance == nil {
		return NormalizedStatus{}, fmt.Errorf("service instance context is required")
	}

	namespace := instanceNamespace(ctx)
	parameters, err := a.parameters(ctx)
	if err != nil {
		return NormalizedStatus{}, err
	}
	phase := ctx.Instance.Status.Phase
	summary := ctx.Instance.Status.Health.Summary
	healthSignals := []HealthSignal{
		{Key: "primary-ready", Status: "Unknown", Severity: HealthSeverityCritical, Message: "Primary Valkey pod readiness has not been observed yet."},
		{Key: "replica-posture", Status: "Unknown", Severity: HealthSeverityWarning, Message: "Replica health has not been observed yet."},
		{Key: "credential-health", Status: "Unknown", Severity: HealthSeverityCritical, Message: "Credential projection has not been observed yet."},
		{Key: "failover-readiness", Status: "Unavailable", Severity: HealthSeverityWarning, Message: "Failover is reserved for a later multi-cluster failover topology."},
	}
	if request.Runtime.Workload == nil || !request.Runtime.Workload.Observed {
		summary = "Waiting for Valkey StatefulSet to be observed."
	} else {
		workload := request.Runtime.Workload
		switch {
		case workload.ReadyReplicas >= workload.DesiredReplicas && request.Runtime.CredentialSecretPresent:
			phase = "Ready"
			summary = fmt.Sprintf("Valkey cache is ready with %d/%d replica(s).", workload.ReadyReplicas, workload.DesiredReplicas)
			healthSignals[0] = HealthSignal{Key: "primary-ready", Status: "Ready", Severity: HealthSeverityInfo, Message: "At least one Valkey pod is ready."}
			healthSignals[2] = HealthSignal{Key: "credential-health", Status: "Ready", Severity: HealthSeverityInfo, Message: "Valkey credential Secret is present."}
		case !request.Runtime.CredentialSecretPresent:
			phase = "Provisioning"
			summary = "Waiting for Valkey credential Secret projection."
			healthSignals[2] = HealthSignal{Key: "credential-health", Status: "Missing", Severity: HealthSeverityCritical, Message: "Valkey credential Secret has not been created yet."}
		default:
			phase = "Provisioning"
			summary = fmt.Sprintf("Waiting for Valkey readiness: %d/%d replica(s) ready.", workload.ReadyReplicas, workload.DesiredReplicas)
			if workload.ReadyReplicas > 0 {
				healthSignals[0] = HealthSignal{Key: "primary-ready", Status: "Ready", Severity: HealthSeverityInfo, Message: "At least one Valkey pod is ready."}
			}
			healthSignals[2] = HealthSignal{Key: "credential-health", Status: "Ready", Severity: HealthSeverityInfo, Message: "Valkey credential Secret is present."}
		}
		if workload.DesiredReplicas <= 1 {
			healthSignals[1] = HealthSignal{Key: "replica-posture", Status: "SingleNode", Severity: HealthSeverityInfo, Message: "Selected Valkey plan runs a single cache node."}
		} else if workload.ReadyReplicas >= workload.DesiredReplicas {
			healthSignals[1] = HealthSignal{Key: "replica-posture", Status: "Ready", Severity: HealthSeverityInfo, Message: "All Valkey replicas are ready."}
		} else {
			healthSignals[1] = HealthSignal{Key: "replica-posture", Status: "Degraded", Severity: HealthSeverityWarning, Message: fmt.Sprintf("%d/%d Valkey replica(s) are ready.", workload.ReadyReplicas, workload.DesiredReplicas)}
		}
	}

	endpoints := []Endpoint{
		{Name: "primary", Address: fmt.Sprintf("%s.%s.svc.cluster.local:6379", ctx.Instance.Name, namespace), Port: 6379, Protocol: "tcp", Visibility: EndpointVisibilityClusterInternal},
	}
	if isValkeyFailoverTopology(planTopology(ctx)) {
		endpoints = append(endpoints, Endpoint{Name: "traffic", Address: fmt.Sprintf("%s.%s.cache.servicer.local:6379", ctx.Instance.Name, ctx.Project.Name), Port: 6379, Protocol: "tcp", Visibility: EndpointVisibilityPrivate})
	}

	status := NormalizedStatus{
		Phase:             ctx.Instance.Status.Phase,
		Summary:           summary,
		Conditions:        append([]metav1.Condition(nil), ctx.Instance.Status.Conditions...),
		CredentialRefs:    append([]platformv1alpha1.NamespacedObjectReference(nil), ctx.Instance.Status.CredentialRefs...),
		ObservedResources: append([]platformv1alpha1.TypedObjectReference(nil), request.ObservedResources...),
		Sync: SyncStatus{
			Phase:            SyncPhaseMaterialized,
			ArtifactRevision: request.ArtifactRevision,
			ApplicationName:  request.ApplicationName,
			Message:          "Waiting for Argo CD reconciliation.",
		},
		HealthSignals: healthSignals,
		Endpoints:     endpoints,
	}
	status.Phase = phase
	if ctx.Instance.Status.Runtime.ObjectRef != nil {
		status.ObservedResources = append(status.ObservedResources, *ctx.Instance.Status.Runtime.ObjectRef)
	}
	status.CacheTopology = a.cacheTopologyStatus(ctx, parameters)
	return status, nil
}

func (a *ValkeyAdapter) cacheTopologyStatus(ctx ServiceContext, parameters valkeyParameters) *platformv1alpha1.CacheTopologyStatus {
	if ctx.Plan == nil {
		return nil
	}
	status := &platformv1alpha1.CacheTopologyStatus{
		Mode: planTopology(ctx),
	}
	if !isValkeyFailoverTopology(planTopology(ctx)) {
		status.PrimaryCluster = instanceCluster(ctx)
		status.FailoverReadiness = "Unavailable"
		status.Message = "Failover is unavailable for single-cluster Valkey plans."
		return status
	}

	status.PrimaryCluster = a.primaryCluster(ctx, parameters)
	status.TrafficEndpoint = fmt.Sprintf("%s.%s.cache.servicer.local:6379", ctx.Instance.Name, ctx.Project.Name)
	status.TrafficPolicyRef = &platformv1alpha1.TypedObjectReference{
		APIVersion: "v1",
		Kind:       "ConfigMap",
		Name:       fmt.Sprintf("%s-traffic-policy", ctx.Instance.Name),
		Namespace:  instanceNamespace(ctx),
	}
	maxLag := a.maxReplicationLagSeconds(parameters)
	readyStandbys := 0
	existingStandbyStatus := map[string]platformv1alpha1.CacheStandbyStatus{}
	if ctx.Instance != nil {
		for _, standby := range ctx.Instance.Status.CacheTopology.StandbyClusters {
			existingStandbyStatus[standby.ClusterName] = standby
		}
	}
	for _, standbyCluster := range a.standbyClusters(ctx, parameters) {
		if standbyCluster == status.PrimaryCluster {
			continue
		}
		existing := existingStandbyStatus[standbyCluster]
		lag, observed := parameters.ReplicationLagSeconds[standbyCluster]
		if !observed && existing.LagObserved {
			lag = existing.ReplicationLagSeconds
			observed = true
		}
		ready := observed && lag <= maxLag
		message := fmt.Sprintf("Replication lag is %d second(s); threshold is %d second(s).", lag, maxLag)
		if existing.ResyncRequired {
			ready = false
			message = existing.Message
			if message == "" {
				message = "Standby requires resynchronization before it can be promoted."
			}
		} else if !observed {
			message = "Replication lag has not been observed for this standby."
		} else if !ready {
			message = fmt.Sprintf("Replication lag is %d second(s), above threshold %d.", lag, maxLag)
		}
		if ready {
			readyStandbys++
		}
		status.StandbyClusters = append(status.StandbyClusters, platformv1alpha1.CacheStandbyStatus{
			ClusterName:           standbyCluster,
			Ready:                 ready,
			ResyncRequired:        existing.ResyncRequired,
			LagObserved:           observed,
			ReplicationLagSeconds: lag,
			Message:               message,
		})
	}
	if readyStandbys > 0 {
		status.FailoverReadiness = "Ready"
		status.Message = fmt.Sprintf("%d standby cluster(s) are eligible for promotion.", readyStandbys)
	} else {
		status.FailoverReadiness = "Blocked"
		status.Message = "No standby clusters currently pass promotion preflight."
	}
	return status
}

func (a *ValkeyAdapter) Delete(_ context.Context, request DeleteRequest) (DeleteResult, error) {
	if request.Context.Instance == nil {
		return DeleteResult{}, fmt.Errorf("service instance context is required")
	}
	return DeleteResult{Message: "Delete requested for Valkey cache instance."}, nil
}

func (a *ValkeyAdapter) SupportedActions(_ context.Context, ctx ServiceContext) []ActionCapability {
	actions := append([]ActionCapability(nil), a.Contract().Actions...)
	if isValkeyFailoverTopology(planTopology(ctx)) {
		return actions
	}
	filtered := actions[:0]
	for _, action := range actions {
		if action.Name != ActionFailover && action.Name != ActionRollbackFailover && action.Name != ActionResyncStandby {
			filtered = append(filtered, action)
		}
	}
	return filtered
}

func (a *ValkeyAdapter) ExecuteAction(_ context.Context, request ExecuteActionRequest) (ActionExecutionResult, error) {
	if request.Context.Instance == nil || request.Action == nil {
		return ActionExecutionResult{}, fmt.Errorf("service instance and action request context are required")
	}

	namespace := instanceNamespace(request.Context)
	operationRef := &platformv1alpha1.TypedObjectReference{
		APIVersion: valkeyRuntimeAPIVersion,
		Kind:       valkeyRuntimeKind,
		Name:       request.Context.Instance.Name,
		Namespace:  namespace,
	}
	switch request.Action.Spec.Action {
	case string(ActionScale):
		return ActionExecutionResult{Phase: "Queued", OperationRef: operationRef, Message: "Valkey scale request queued for the Servicer-owned cache controller.", Retryable: true}, nil
	case string(ActionRestart):
		return ActionExecutionResult{Phase: "Queued", OperationRef: operationRef, Message: "Valkey restart request queued for the Servicer-owned cache controller.", Retryable: true}, nil
	case string(ActionRotateCredentials):
		return ActionExecutionResult{Phase: "Queued", OperationRef: operationRef, Message: "Valkey credential rotation request queued for the Servicer-owned cache controller.", Retryable: true}, nil
	case string(ActionFailover), string(ActionRollbackFailover), string(ActionResyncStandby):
		return ActionExecutionResult{Phase: "Queued", OperationRef: operationRef, Message: "Valkey topology action queued for the Servicer-owned cache controller.", Retryable: true}, nil
	default:
		return ActionExecutionResult{}, fmt.Errorf("unsupported Valkey action %q", request.Action.Spec.Action)
	}
}

func (a *ValkeyAdapter) parameters(ctx ServiceContext) (valkeyParameters, error) {
	parameters := valkeyParameters{}
	if ctx.Class != nil && ctx.Class.Spec.DefaultParameters != nil {
		if err := json.Unmarshal(ctx.Class.Spec.DefaultParameters.Raw, &parameters); err != nil {
			return valkeyParameters{}, fmt.Errorf("decode class defaults: %w", err)
		}
	}
	if ctx.Plan != nil && ctx.Plan.Spec.DefaultParameters != nil {
		if err := json.Unmarshal(ctx.Plan.Spec.DefaultParameters.Raw, &parameters); err != nil {
			return valkeyParameters{}, fmt.Errorf("decode plan defaults: %w", err)
		}
	}
	if ctx.Instance != nil && ctx.Instance.Spec.Parameters != nil {
		if err := json.Unmarshal(ctx.Instance.Spec.Parameters.Raw, &parameters); err != nil {
			return valkeyParameters{}, fmt.Errorf("decode instance parameters: %w", err)
		}
	}
	if parameters.MemoryProfile == "" {
		parameters.MemoryProfile = "small"
	}
	if parameters.MemoryLimit == "" {
		parameters.MemoryLimit = memoryLimitForProfile(parameters.MemoryProfile)
	}
	if parameters.Persistence == "" {
		parameters.Persistence = "none"
	}
	if parameters.MaxMemoryPolicy == "" {
		parameters.MaxMemoryPolicy = "allkeys-lru"
	}
	if parameters.Persistence == "persistent" && parameters.StorageSize == "" {
		parameters.StorageSize = "5Gi"
	}
	return parameters, nil
}

func (a *ValkeyAdapter) validateParameters(ctx ServiceContext, parameters valkeyParameters) []ValidationIssue {
	issues := make([]ValidationIssue, 0)
	if !containsString([]string{"small", "medium", "large"}, parameters.MemoryProfile) {
		issues = append(issues, ValidationIssue{Path: "parameters.memoryProfile", Message: "memoryProfile must be one of small, medium, or large", Severity: HealthSeverityCritical})
	}
	if parameters.MemoryLimit == "" {
		issues = append(issues, ValidationIssue{Path: "parameters.memoryLimit", Message: "memoryLimit must resolve from the selected memory profile or be provided explicitly", Severity: HealthSeverityCritical})
	}
	if !containsString([]string{"none", "ephemeral", "persistent"}, parameters.Persistence) {
		issues = append(issues, ValidationIssue{Path: "parameters.persistence", Message: "persistence must be one of none, ephemeral, or persistent", Severity: HealthSeverityCritical})
	}
	if parameters.Persistence == "persistent" && parameters.StorageSize == "" {
		issues = append(issues, ValidationIssue{Path: "parameters.storageSize", Message: "storageSize is required when persistence is persistent", Severity: HealthSeverityCritical})
	}
	if isValkeyFailoverTopology(planTopology(ctx)) {
		primaryCluster := a.primaryCluster(ctx, parameters)
		if primaryCluster == "" {
			issues = append(issues, ValidationIssue{Path: "parameters.primaryCluster", Message: "primaryCluster is required for failover Valkey plans when project placement is unresolved", Severity: HealthSeverityCritical})
		}
		if len(parameters.StandbyClusters) == 0 {
			issues = append(issues, ValidationIssue{Path: "parameters.standbyClusters", Message: "standbyClusters must include at least one standby for failover Valkey plans", Severity: HealthSeverityCritical})
		}
		// Sentinel gossips on pod IPs across clusters; a cross-cluster network fabric is required.
		issues = append(issues, requiresPodMesh(ctx)...)
		requestedPrimaryCluster := a.requestedPrimaryCluster(ctx, parameters)
		seenStandbys := map[string]struct{}{}
		for _, standbyCluster := range parameters.StandbyClusters {
			if standbyCluster == "" {
				issues = append(issues, ValidationIssue{Path: "parameters.standbyClusters", Message: "standby cluster names must not be empty", Severity: HealthSeverityCritical})
				continue
			}
			if standbyCluster == requestedPrimaryCluster {
				issues = append(issues, ValidationIssue{Path: "parameters.standbyClusters", Message: "standby cluster must not match the primary cluster", Severity: HealthSeverityCritical})
			}
			if _, ok := seenStandbys[standbyCluster]; ok {
				issues = append(issues, ValidationIssue{Path: "parameters.standbyClusters", Message: fmt.Sprintf("standby cluster %q is duplicated", standbyCluster), Severity: HealthSeverityCritical})
			}
			seenStandbys[standbyCluster] = struct{}{}
		}
	}

	replicas := a.replicas(ctx, parameters)
	if replicas > 9 {
		issues = append(issues, ValidationIssue{Path: "parameters.replicas", Message: "replicas must not exceed 9", Severity: HealthSeverityCritical})
	}

	return issues
}

func (a *ValkeyAdapter) primaryCluster(ctx ServiceContext, parameters valkeyParameters) string {
	if ctx.Instance != nil && ctx.Instance.Status.CacheTopology.PrimaryCluster != "" {
		return ctx.Instance.Status.CacheTopology.PrimaryCluster
	}
	return a.requestedPrimaryCluster(ctx, parameters)
}

func (a *ValkeyAdapter) requestedPrimaryCluster(ctx ServiceContext, parameters valkeyParameters) string {
	if parameters.PrimaryCluster != "" {
		return parameters.PrimaryCluster
	}
	return instanceCluster(ctx)
}

func (a *ValkeyAdapter) standbyClusters(ctx ServiceContext, parameters valkeyParameters) []string {
	if ctx.Instance != nil && len(ctx.Instance.Status.CacheTopology.StandbyClusters) > 0 {
		standbys := make([]string, 0, len(ctx.Instance.Status.CacheTopology.StandbyClusters))
		for _, standby := range ctx.Instance.Status.CacheTopology.StandbyClusters {
			if standby.ClusterName != "" {
				standbys = append(standbys, standby.ClusterName)
			}
		}
		return standbys
	}
	return append([]string(nil), parameters.StandbyClusters...)
}

func (a *ValkeyAdapter) maxReplicationLagSeconds(parameters valkeyParameters) int32 {
	if parameters.MaxReplicationLagSeconds != nil && *parameters.MaxReplicationLagSeconds >= 0 {
		return *parameters.MaxReplicationLagSeconds
	}
	return 30
}

func (a *ValkeyAdapter) replicas(ctx ServiceContext, parameters valkeyParameters) int32 {
	if parameters.Replicas != nil && *parameters.Replicas > 0 {
		return *parameters.Replicas
	}
	return 1
}

func planTopology(ctx ServiceContext) string {
	if ctx.Plan == nil {
		return ""
	}
	return ctx.Plan.Spec.Topology
}

func isValkeyFailoverTopology(topology string) bool {
	return topology == "multi-cluster-failover" || topology == "multi-region"
}

func (a *ValkeyAdapter) valkeyConfig(parameters valkeyParameters, role, primaryEndpoint string) string {
	lines := []string{
		"appendonly no",
		fmt.Sprintf("maxmemory %s", valkeyMemoryValue(parameters.MemoryLimit)),
		fmt.Sprintf("maxmemory-policy %s", parameters.MaxMemoryPolicy),
	}
	if parameters.Persistence == "persistent" {
		lines[0] = "appendonly yes"
	}
	if role == "standby" && primaryEndpoint != "" {
		lines = append(lines, fmt.Sprintf("replicaof %s 6379", primaryEndpoint))
	}
	return strings.Join(lines, "\n") + "\n"
}

func valkeyMemoryValue(value string) string {
	normalized := strings.TrimSpace(value)
	if normalized == "" {
		return "256mb"
	}
	replacements := []struct {
		suffix string
		unit   string
	}{
		{suffix: "Ki", unit: "kb"},
		{suffix: "Mi", unit: "mb"},
		{suffix: "Gi", unit: "gb"},
		{suffix: "Ti", unit: "tb"},
	}
	for _, replacement := range replacements {
		if strings.HasSuffix(normalized, replacement.suffix) {
			return strings.TrimSuffix(normalized, replacement.suffix) + replacement.unit
		}
	}
	return normalized
}

func (a *ValkeyAdapter) containerSpec(version, configName, authSecretName string, parameters valkeyParameters) map[string]any {
	container := map[string]any{
		"name":            "valkey",
		"image":           fmt.Sprintf("valkey/valkey:%s", version),
		"imagePullPolicy": "IfNotPresent",
		"ports":           []map[string]any{{"name": "valkey", "containerPort": int32(6379)}},
		"env": []map[string]any{
			{
				"name": "VALKEY_PASSWORD",
				"valueFrom": map[string]any{
					"secretKeyRef": map[string]any{
						"name": authSecretName,
						"key":  "password",
					},
				},
			},
		},
		"command": []string{"sh", "-c"},
		"args": []string{
			"exec valkey-server /etc/valkey/valkey.conf --requirepass \"$VALKEY_PASSWORD\"",
		},
		"resources": map[string]any{
			"requests": map[string]string{"memory": parameters.MemoryLimit},
			"limits":   map[string]string{"memory": parameters.MemoryLimit},
		},
		"volumeMounts": []map[string]any{
			{"name": "config", "mountPath": "/etc/valkey"},
		},
	}
	if parameters.Persistence == "persistent" || parameters.Persistence == "ephemeral" {
		container["volumeMounts"] = append(container["volumeMounts"].([]map[string]any), map[string]any{"name": "data", "mountPath": "/data"})
	}
	return container
}

func (a *ValkeyAdapter) podVolumes(configName string, parameters valkeyParameters) []map[string]any {
	volumes := []map[string]any{
		{
			"name": "config",
			"configMap": map[string]any{
				"name": configName,
			},
		},
	}
	if parameters.Persistence == "ephemeral" {
		volumes = append(volumes, map[string]any{
			"name":     "data",
			"emptyDir": map[string]any{},
		})
	}
	return volumes
}

func (a *ValkeyAdapter) volumeClaimTemplate(parameters valkeyParameters) map[string]any {
	template := map[string]any{
		"metadata": map[string]any{"name": "data"},
		"spec": map[string]any{
			"accessModes": []string{"ReadWriteOnce"},
			"resources": map[string]any{
				"requests": map[string]string{"storage": parameters.StorageSize},
			},
		},
	}
	if parameters.StorageClass != "" {
		template["spec"].(map[string]any)["storageClassName"] = parameters.StorageClass
	}
	return template
}

func memoryLimitForProfile(profile string) string {
	switch profile {
	case "medium":
		return "1Gi"
	case "large":
		return "4Gi"
	default:
		return "256Mi"
	}
}

// DefaultValkeyDeletionPolicy is the preferred default for cache instances.
const DefaultValkeyDeletionPolicy = platformv1alpha1.DeletionPolicyDelete
