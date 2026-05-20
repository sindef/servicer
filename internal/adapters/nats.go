package adapters

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	platformv1alpha1 "github.com/sindef/servicer/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

const natsDriver = "servicer-nats"

type natsParameters struct {
	Replicas             *int32                  `json:"replicas,omitempty"`
	JetStream            bool                    `json:"jetstream,omitempty"`
	Streams              []natsStreamSpec        `json:"streams,omitempty"`
	Consumers            []natsConsumerSpec      `json:"consumers,omitempty"`
	AppCredentials       []natsAppCredentialSpec `json:"appCredentials,omitempty"`
	StorageClass         string                  `json:"storageClass,omitempty"`
	StorageSize          string                  `json:"storageSize,omitempty"`
	MaxPayload           string                  `json:"maxPayload,omitempty"`
	MemoryLimit          string                  `json:"memoryLimit,omitempty"`
	PrimaryCluster       string                  `json:"primaryCluster,omitempty"`
	StandbyClusters      []string                `json:"standbyClusters,omitempty"`
	GatewayLagSeconds    map[string]int32        `json:"gatewayLagSeconds,omitempty"`
	MaxGatewayLagSeconds *int32                  `json:"maxGatewayLagSeconds,omitempty"`
	ServiceType          string                  `json:"serviceType,omitempty"`
	ExternalDNSHostname  string                  `json:"externalDnsHostname,omitempty"`
}

// NATSContract describes the normalized platform contract for NATS-backed messaging services.
var NATSContract = ProductContract{
	ServiceClass:            ServiceClassNATS,
	FriendlyName:            "NATS",
	RuntimeDriver:           natsDriver,
	SupportsVersionOverride: true,
	SupportsMultiCluster:    true,
	TopologyModes: []string{
		"single-cluster",
		"multi-region",
	},
	StatusSignals: []StatusSignalDescriptor{
		{Key: "broker-readiness", DisplayName: "Broker Readiness", Description: "Whether all NATS broker members are ready.", Severity: HealthSeverityCritical},
		{Key: "jetstream-health", DisplayName: "JetStream Health", Description: "Whether stream persistence and quorum are healthy when JetStream is enabled.", Severity: HealthSeverityWarning},
		{Key: "gateway-health", DisplayName: "Gateway Health", Description: "Whether geo gateways are linked and replication lag is within threshold.", Severity: HealthSeverityWarning},
		{Key: "credential-health", DisplayName: "Credential Health", Description: "Whether NATS credentials are projected and rotation-ready.", Severity: HealthSeverityWarning},
	},
	Actions: []ActionCapability{
		{Name: ActionScale, DisplayName: "Scale", RequiresApproval: false, Disruptive: true},
		{Name: ActionRestart, DisplayName: "Rolling Restart", RequiresApproval: false, Disruptive: true},
		{Name: ActionRotateCredentials, DisplayName: "Rotate Credentials", RequiresApproval: false, Disruptive: false},
		{Name: ActionDeleteStream, DisplayName: "Delete Stream", RequiresApproval: false, Disruptive: true},
		{Name: ActionPurgeStream, DisplayName: "Purge Stream", RequiresApproval: false, Disruptive: true},
		{Name: ActionDeleteConsumer, DisplayName: "Delete Consumer", RequiresApproval: false, Disruptive: true},
	},
}

// NATSAdapter renders a narrow, Servicer-owned NATS runtime path.
type NATSAdapter struct{}

// NewNATSAdapter creates a Servicer-owned NATS adapter.
func NewNATSAdapter() *NATSAdapter {
	return &NATSAdapter{}
}

func (a *NATSAdapter) Contract() ProductContract {
	return NATSContract
}

func (a *NATSAdapter) Validate(_ context.Context, request ValidationRequest) (ValidationResult, error) {
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
	if ctx.Class.Spec.Driver != natsDriver {
		issues = append(issues, ValidationIssue{Path: "serviceClass.driver", Message: fmt.Sprintf("expected driver %q, got %q", natsDriver, ctx.Class.Spec.Driver), Severity: HealthSeverityCritical})
	}
	if ctx.Plan.Spec.ServiceClassRef.Name != ctx.Instance.Spec.ServiceClassRef.Name {
		issues = append(issues, ValidationIssue{Path: "servicePlanRef", Message: "service plan does not belong to the selected service class", Severity: HealthSeverityCritical})
	}
	if !containsString(a.Contract().TopologyModes, ctx.Plan.Spec.Topology) {
		issues = append(issues, ValidationIssue{Path: "servicePlan.topology", Message: fmt.Sprintf("topology %q is not supported for NATS", ctx.Plan.Spec.Topology), Severity: HealthSeverityCritical})
	}
	parameters, err := a.parameters(ctx)
	if err != nil {
		issues = append(issues, ValidationIssue{Path: "parameters", Message: err.Error(), Severity: HealthSeverityCritical})
	} else if ctx.Plan.Spec.Topology == "multi-region" && !parameters.JetStream {
		issues = append(issues, ValidationIssue{Path: "parameters.jetstream", Message: "geo-distributed NATS plans require JetStream to be enabled", Severity: HealthSeverityCritical})
	} else {
		if ctx.Plan.Spec.Topology == "multi-region" && len(parameters.StandbyClusters) == 0 {
			issues = append(issues, ValidationIssue{Path: "parameters.standbyClusters", Message: "geo-distributed NATS plans require at least one standby cluster", Severity: HealthSeverityCritical})
		}
		issues = append(issues, a.validateManagedResources(parameters)...)
	}
	return ValidationResult{Valid: len(issues) == 0, Issues: issues}, nil
}

func (a *NATSAdapter) Render(_ context.Context, request RenderRequest) (RenderResult, error) {
	ctx := request.Context
	if ctx.Instance == nil || ctx.Project == nil || ctx.Tenant == nil {
		return RenderResult{}, fmt.Errorf("tenant, project and instance context are required")
	}
	parameters, err := a.parameters(ctx)
	if err != nil {
		return RenderResult{}, err
	}
	version := effectiveVersion(ctx)
	if version == "" {
		version = "2.10"
	}

	primaryCluster := a.primaryCluster(ctx, parameters)
	type clusterRole struct{ cluster, role string }
	clusterRoles := []clusterRole{{cluster: primaryCluster, role: "primary"}}
	if planTopology(ctx) == "multi-region" {
		for _, standby := range parameters.StandbyClusters {
			if standby != primaryCluster {
				clusterRoles = append(clusterRoles, clusterRole{cluster: standby, role: "standby"})
			}
		}
	}

	allClusters := make([]string, 0, len(clusterRoles))
	for _, cr := range clusterRoles {
		allClusters = append(allClusters, cr.cluster)
	}

	namespace := instanceNamespace(ctx)
	authSecretName := fmt.Sprintf("%s-auth", ctx.Instance.Name)
	authConfigSecretName := fmt.Sprintf("%s-auth-config", ctx.Instance.Name)
	primaryPackagePath := natsPackagePath(ctx, primaryCluster)
	packagePaths := make([]string, 0, len(clusterRoles))
	allArtifacts := make([]RenderedArtifact, 0)
	resourceDigest := natsResourceDigest(parameters)

	for _, cr := range clusterRoles {
		peerClusters := make([]string, 0, len(allClusters)-1)
		for _, c := range allClusters {
			if c != cr.cluster {
				peerClusters = append(peerClusters, c)
			}
		}
		basePath := natsPackagePath(ctx, cr.cluster)
		packagePaths = append(packagePaths, basePath)
		rendered, err := a.renderClusterArtifacts(ctx, parameters, version, cr.cluster, cr.role, namespace, authSecretName, authConfigSecretName, peerClusters, basePath, resourceDigest)
		if err != nil {
			return RenderResult{}, err
		}
		allArtifacts = append(allArtifacts, rendered...)
	}

	credentialRefs := []platformv1alpha1.NamespacedObjectReference{{Name: authSecretName, Namespace: namespace}}
	for _, credential := range parameters.AppCredentials {
		credentialRefs = append(credentialRefs, platformv1alpha1.NamespacedObjectReference{
			Name:      natsAppCredentialSecretName(ctx.Instance.Name, credential.Name),
			Namespace: namespace,
		})
	}

	return RenderResult{
		RuntimeDriver: natsDriver,
		PrimaryResource: &platformv1alpha1.TypedObjectReference{
			APIVersion: "apps/v1",
			Kind:       "StatefulSet",
			Name:       ctx.Instance.Name,
			Namespace:  namespace,
		},
		PackagePath:  primaryPackagePath,
		PackagePaths: packagePaths,
		Endpoints: []Endpoint{
			{Name: "client", Address: fmt.Sprintf("%s.%s.svc.cluster.local:4222", ctx.Instance.Name, namespace), Port: 4222, Protocol: "tcp", Visibility: EndpointVisibilityClusterInternal},
			{Name: "monitor", Address: fmt.Sprintf("%s.%s.svc.cluster.local:8222", ctx.Instance.Name, namespace), Port: 8222, Protocol: "http", Visibility: EndpointVisibilityClusterInternal},
		},
		CredentialRefs: credentialRefs,
		Artifacts:      allArtifacts,
	}, nil
}

func (a *NATSAdapter) renderClusterArtifacts(ctx ServiceContext, parameters natsParameters, version, clusterName, role, namespace, authSecretName, authConfigSecretName string, peerClusters []string, basePath, resourceDigest string) ([]RenderedArtifact, error) {
	replicas := a.replicas(ctx, parameters)
	configName := fmt.Sprintf("%s-config", ctx.Instance.Name)
	headlessServiceName := fmt.Sprintf("%s-headless", ctx.Instance.Name)
	resourceConfigName := fmt.Sprintf("%s-managed-resources", ctx.Instance.Name)
	labels := map[string]string{
		"servicer.io/managed-by":       "servicer",
		"servicer.io/cluster-target":   clusterName,
		"servicer.io/nats-role":        role,
		"servicer.io/project":          ctx.Project.Name,
		"servicer.io/service-instance": ctx.Instance.Name,
		"servicer.io/runtime":          "nats",
	}
	selectorLabels := map[string]string{
		"servicer.io/service-instance": ctx.Instance.Name,
		"servicer.io/runtime":          "nats",
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
	configManifest := map[string]any{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata": map[string]any{
			"name":      configName,
			"namespace": namespace,
			"labels":    labels,
		},
		"data": map[string]string{
			"nats.conf": a.natsConfig(ctx, parameters, clusterName, peerClusters),
		},
	}
	resourceConfigManifest := map[string]any{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata": map[string]any{
			"name":      resourceConfigName,
			"namespace": namespace,
			"labels":    labels,
		},
		"data": a.resourceConfigData(parameters),
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
			"ports": []map[string]any{
				{"name": "client", "port": int32(4222), "targetPort": int32(4222)},
				{"name": "monitor", "port": int32(8222), "targetPort": int32(8222)},
			},
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
			"ports":     []map[string]any{{"name": "cluster", "port": int32(6222), "targetPort": int32(6222)}},
			"selector":  selectorLabels,
		},
	}
	var gatewayServiceManifest map[string]any
	if len(peerClusters) > 0 {
		gatewayServiceManifest = map[string]any{
			"apiVersion": "v1",
			"kind":       "Service",
			"metadata": map[string]any{
				"name":      fmt.Sprintf("%s-gateway", ctx.Instance.Name),
				"namespace": namespace,
				"labels":    labels,
				"annotations": map[string]string{
					"servicer.io/gateway-address": natsGatewayAddress(ctx, clusterName),
				},
			},
			"spec": map[string]any{
				"ports": []map[string]any{
					{"name": "gateway", "port": int32(7222), "targetPort": int32(7222)},
				},
				"selector": selectorLabels,
			},
		}
	}
	statefulSetManifest := map[string]any{
		"apiVersion": "apps/v1",
		"kind":       "StatefulSet",
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
					"containers": []map[string]any{a.containerSpec(version, configName, parameters, resourceDigest)},
					"volumes": []map[string]any{{
						"name": "config",
						"configMap": map[string]any{
							"name": configName,
						},
					}, {
						"name": "auth",
						"secret": map[string]any{
							"secretName": authConfigSecretName,
						},
					}},
				},
			},
		},
	}
	if parameters.JetStream {
		statefulSetManifest["spec"].(map[string]any)["volumeClaimTemplates"] = []map[string]any{a.volumeClaimTemplate(parameters)}
	}

	manifests := []struct {
		name string
		body map[string]any
	}{
		{name: "namespace.yaml", body: namespaceManifest},
		{name: "configmap.yaml", body: configManifest},
		{name: "managed-resources-configmap.yaml", body: resourceConfigManifest},
		{name: "service.yaml", body: serviceManifest},
		{name: "headless-service.yaml", body: headlessServiceManifest},
		{name: "statefulset.yaml", body: statefulSetManifest},
	}
	if gatewayServiceManifest != nil {
		manifests = append(manifests, struct {
			name string
			body map[string]any
		}{name: "gateway-service.yaml", body: gatewayServiceManifest})
	}

	if len(parameters.Streams) > 0 || len(parameters.Consumers) > 0 {
		jobName := fmt.Sprintf("%s-managed-%s", ctx.Instance.Name, resourceDigest)
		jobManifest := a.managedResourceJob(ctx, namespace, authSecretName, resourceConfigName, jobName, labels)
		manifests = append(manifests, struct {
			name string
			body map[string]any
		}{name: "managed-resources-job.yaml", body: jobManifest})
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

func natsPackagePath(ctx ServiceContext, clusterName string) string {
	return fmt.Sprintf("clusters/%s/tenants/%s/projects/%s/services/%s", clusterName, ctx.Tenant.Name, ctx.Project.Name, ctx.Instance.Name)
}

func (a *NATSAdapter) Observe(_ context.Context, request ObserveRequest) (NormalizedStatus, error) {
	ctx := request.Context
	if ctx.Instance == nil {
		return NormalizedStatus{}, fmt.Errorf("service instance context is required")
	}
	namespace := instanceNamespace(ctx)
	phase := "Provisioning"
	summary := "Waiting for NATS StatefulSet to be observed."
	healthSignals := []HealthSignal{
		{Key: "broker-readiness", Status: "Unknown", Severity: HealthSeverityCritical, Message: "NATS broker readiness has not been observed yet."},
		{Key: "jetstream-health", Status: "Unknown", Severity: HealthSeverityWarning, Message: "JetStream health has not been observed yet."},
		{Key: "gateway-health", Status: "N/A", Severity: HealthSeverityInfo, Message: "Single-cluster topology — NATS gateways are not applicable."},
		{Key: "credential-health", Status: "Unknown", Severity: HealthSeverityWarning, Message: "NATS credentials have not been observed yet."},
	}
	if request.Runtime.Workload != nil && request.Runtime.Workload.Observed {
		workload := request.Runtime.Workload
		if workload.ReadyReplicas >= workload.DesiredReplicas && request.Runtime.CredentialSecretPresent {
			phase = "Ready"
			summary = fmt.Sprintf("NATS cluster is ready with %d/%d broker(s).", workload.ReadyReplicas, workload.DesiredReplicas)
			healthSignals[0] = HealthSignal{Key: "broker-readiness", Status: "Ready", Severity: HealthSeverityInfo, Message: "All NATS brokers are ready."}
			healthSignals[3] = HealthSignal{Key: "credential-health", Status: "Ready", Severity: HealthSeverityInfo, Message: "NATS credential Secret is present."}
		} else {
			summary = fmt.Sprintf("Waiting for NATS readiness: %d/%d broker(s) ready.", workload.ReadyReplicas, workload.DesiredReplicas)
			if request.Runtime.CredentialSecretPresent {
				healthSignals[3] = HealthSignal{Key: "credential-health", Status: "Ready", Severity: HealthSeverityInfo, Message: "NATS credential Secret is present."}
			}
		}
	}
	params, _ := a.parameters(ctx)
	if params.JetStream {
		message := "JetStream storage intent is materialized."
		if planTopology(ctx) == "multi-region" && len(params.StandbyClusters) > 0 {
			message = fmt.Sprintf("JetStream federation is configured across %d cluster(s).", len(params.StandbyClusters)+1)
		}
		healthSignals[1] = HealthSignal{Key: "jetstream-health", Status: "Configured", Severity: HealthSeverityInfo, Message: message}
	} else {
		healthSignals[1] = HealthSignal{Key: "jetstream-health", Status: "Disabled", Severity: HealthSeverityInfo, Message: "JetStream is not enabled for this plan."}
	}
	if planTopology(ctx) == "multi-region" {
		healthSignals[2] = natsGatewayHealthSignal(params)
	}

	return NormalizedStatus{
		Phase:             phase,
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
		Endpoints: []Endpoint{
			{Name: "client", Address: fmt.Sprintf("%s.%s.svc.cluster.local:4222", ctx.Instance.Name, namespace), Port: 4222, Protocol: "tcp", Visibility: EndpointVisibilityClusterInternal},
			{Name: "monitor", Address: fmt.Sprintf("%s.%s.svc.cluster.local:8222", ctx.Instance.Name, namespace), Port: 8222, Protocol: "http", Visibility: EndpointVisibilityClusterInternal},
		},
	}, nil
}

func (a *NATSAdapter) Delete(_ context.Context, request DeleteRequest) (DeleteResult, error) {
	if request.Context.Instance == nil {
		return DeleteResult{}, fmt.Errorf("service instance context is required")
	}
	return DeleteResult{Message: "Delete requested for NATS instance."}, nil
}

func (a *NATSAdapter) SupportedActions(_ context.Context, _ ServiceContext) []ActionCapability {
	return append([]ActionCapability(nil), a.Contract().Actions...)
}

func (a *NATSAdapter) ExecuteAction(_ context.Context, request ExecuteActionRequest) (ActionExecutionResult, error) {
	if request.Context.Instance == nil || request.Action == nil {
		return ActionExecutionResult{}, fmt.Errorf("service instance and action request context are required")
	}
	namespace := instanceNamespace(request.Context)
	return ActionExecutionResult{
		Phase: "Queued",
		OperationRef: &platformv1alpha1.TypedObjectReference{
			APIVersion: "apps/v1",
			Kind:       "StatefulSet",
			Name:       request.Context.Instance.Name,
			Namespace:  namespace,
		},
		Message:   fmt.Sprintf("NATS action %q queued for the Servicer-owned runtime controller.", request.Action.Spec.Action),
		Retryable: true,
	}, nil
}

func (a *NATSAdapter) parameters(ctx ServiceContext) (natsParameters, error) {
	parameters := natsParameters{}
	if ctx.Plan != nil && ctx.Plan.Spec.DefaultParameters != nil {
		if err := json.Unmarshal(ctx.Plan.Spec.DefaultParameters.Raw, &parameters); err != nil {
			return natsParameters{}, fmt.Errorf("decode plan defaults: %w", err)
		}
	}
	if ctx.Instance != nil && ctx.Instance.Spec.Parameters != nil {
		if err := json.Unmarshal(ctx.Instance.Spec.Parameters.Raw, &parameters); err != nil {
			return natsParameters{}, fmt.Errorf("decode instance parameters: %w", err)
		}
	}
	if parameters.StorageSize == "" {
		parameters.StorageSize = "10Gi"
	}
	if parameters.MaxPayload == "" {
		parameters.MaxPayload = "1MiB"
	}
	if parameters.MemoryLimit == "" {
		parameters.MemoryLimit = "512Mi"
	}
	for i := range parameters.Streams {
		parameters.Streams[i] = parameters.Streams[i].normalized()
	}
	sort.Slice(parameters.Streams, func(i, j int) bool { return parameters.Streams[i].Name < parameters.Streams[j].Name })
	for i := range parameters.Consumers {
		parameters.Consumers[i] = parameters.Consumers[i].normalized()
	}
	sort.Slice(parameters.Consumers, func(i, j int) bool {
		if parameters.Consumers[i].Stream == parameters.Consumers[j].Stream {
			return parameters.Consumers[i].Name < parameters.Consumers[j].Name
		}
		return parameters.Consumers[i].Stream < parameters.Consumers[j].Stream
	})
	for i := range parameters.AppCredentials {
		parameters.AppCredentials[i] = parameters.AppCredentials[i].normalized()
	}
	sort.Slice(parameters.AppCredentials, func(i, j int) bool { return parameters.AppCredentials[i].Name < parameters.AppCredentials[j].Name })
	return parameters, nil
}

func (a *NATSAdapter) replicas(ctx ServiceContext, parameters natsParameters) int32 {
	if parameters.Replicas != nil && *parameters.Replicas > 0 {
		return *parameters.Replicas
	}
	return 1
}

func (a *NATSAdapter) natsConfig(ctx ServiceContext, parameters natsParameters, clusterName string, gatewayClusters []string) string {
	config := fmt.Sprintf("port: 4222\nhttp: 8222\nmax_payload: %s\ninclude ../nats-auth/users.conf\n", parameters.MaxPayload)
	config += fmt.Sprintf("server_name: %s-%s\n", ctx.Instance.Name, sanitizeK8ssandraName(clusterName))
	config += "cluster {\n"
	config += fmt.Sprintf("  name: %s\n", clusterName)
	config += "  listen: 0.0.0.0:6222\n"
	routes := a.clusterRoutes(ctx, parameters)
	if len(routes) > 0 {
		config += "  routes: [\n"
		for _, route := range routes {
			config += fmt.Sprintf("    %q,\n", route)
		}
		config += "  ]\n"
	}
	config += "}\n"
	if len(gatewayClusters) > 0 {
		config += fmt.Sprintf("gateway {\n  name: %s\n  listen: 0.0.0.0:7222\n  gateways: [\n", clusterName)
		for _, peer := range gatewayClusters {
			config += fmt.Sprintf("    { name: \"%s\", urls: [\"nats://%s\"] }\n", peer, natsGatewayAddress(ctx, peer))
		}
		config += "  ]\n}\n"
	}
	if parameters.JetStream {
		config += "jetstream {\n  store_dir: /data/jetstream\n}\n"
	}
	return config
}

func (a *NATSAdapter) clusterRoutes(ctx ServiceContext, parameters natsParameters) []string {
	replicas := a.replicas(ctx, parameters)
	if replicas <= 1 {
		return nil
	}
	namespace := instanceNamespace(ctx)
	headlessServiceName := fmt.Sprintf("%s-headless", ctx.Instance.Name)
	routes := make([]string, 0, replicas)
	for ordinal := int32(0); ordinal < replicas; ordinal++ {
		routes = append(routes, fmt.Sprintf("nats://%s-%d.%s.%s.svc.cluster.local:6222", ctx.Instance.Name, ordinal, headlessServiceName, namespace))
	}
	return routes
}

func (a *NATSAdapter) containerSpec(version, configName string, parameters natsParameters, resourceDigest string) map[string]any {
	container := map[string]any{
		"name":            "nats",
		"image":           fmt.Sprintf("nats:%s-alpine", version),
		"imagePullPolicy": "IfNotPresent",
		"ports": []map[string]any{
			{"name": "client", "containerPort": int32(4222)},
			{"name": "cluster", "containerPort": int32(6222)},
			{"name": "gateway", "containerPort": int32(7222)},
			{"name": "monitor", "containerPort": int32(8222)},
		},
		"args": []string{"-c", "/etc/nats/nats.conf"},
		"resources": map[string]any{
			"requests": map[string]string{"memory": parameters.MemoryLimit},
			"limits":   map[string]string{"memory": parameters.MemoryLimit},
		},
		"volumeMounts": []map[string]any{
			{"name": "config", "mountPath": "/etc/nats"},
			{"name": "auth", "mountPath": "/etc/nats-auth", "readOnly": true},
		},
	}
	container["env"] = []map[string]any{{"name": "SERVICER_RESOURCE_DIGEST", "value": resourceDigest}}
	if parameters.JetStream {
		container["volumeMounts"] = append(container["volumeMounts"].([]map[string]any), map[string]any{"name": "data", "mountPath": "/data"})
	}
	return container
}

func (a *NATSAdapter) volumeClaimTemplate(parameters natsParameters) map[string]any {
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

func (a *NATSAdapter) primaryCluster(ctx ServiceContext, parameters natsParameters) string {
	if parameters.PrimaryCluster != "" {
		return parameters.PrimaryCluster
	}
	return instanceCluster(ctx)
}

func natsGatewayAddress(ctx ServiceContext, clusterName string) string {
	return fmt.Sprintf("%s-gateway.%s.%s.nats.servicer.local:7222", ctx.Instance.Name, ctx.Project.Name, clusterName)
}

func natsGatewayHealthSignal(parameters natsParameters) HealthSignal {
	if len(parameters.StandbyClusters) == 0 {
		return HealthSignal{Key: "gateway-health", Status: "Pending", Severity: HealthSeverityWarning, Message: "Geo topology requires at least one standby gateway peer."}
	}
	if len(parameters.GatewayLagSeconds) == 0 {
		return HealthSignal{Key: "gateway-health", Status: "Configured", Severity: HealthSeverityInfo, Message: "NATS gateways are configured; replication lag has not been reported yet."}
	}
	maxAllowed := int32(30)
	if parameters.MaxGatewayLagSeconds != nil && *parameters.MaxGatewayLagSeconds >= 0 {
		maxAllowed = *parameters.MaxGatewayLagSeconds
	}
	worstCluster := ""
	var worstLag int32
	for _, standby := range parameters.StandbyClusters {
		lag, ok := parameters.GatewayLagSeconds[standby]
		if !ok {
			return HealthSignal{Key: "gateway-health", Status: "Unknown", Severity: HealthSeverityWarning, Message: fmt.Sprintf("Gateway replication lag has not been reported for standby cluster %q.", standby)}
		}
		if lag >= worstLag {
			worstCluster = standby
			worstLag = lag
		}
	}
	if worstLag > maxAllowed {
		return HealthSignal{Key: "gateway-health", Status: "Degraded", Severity: HealthSeverityWarning, Message: fmt.Sprintf("Gateway replication lag on standby cluster %q is %ds, above %ds threshold.", worstCluster, worstLag, maxAllowed)}
	}
	return HealthSignal{Key: "gateway-health", Status: "Ready", Severity: HealthSeverityInfo, Message: fmt.Sprintf("NATS gateway replication lag is within threshold; worst standby %q is %ds.", worstCluster, worstLag)}
}

func (a *NATSAdapter) validateManagedResources(parameters natsParameters) []ValidationIssue {
	issues := []ValidationIssue{}
	if (!parameters.JetStream) && (len(parameters.Streams) > 0 || len(parameters.Consumers) > 0) {
		issues = append(issues, ValidationIssue{Path: "parameters.jetstream", Message: "JetStream must be enabled when streams or consumers are declared", Severity: HealthSeverityCritical})
	}
	streams := map[string]struct{}{}
	for _, stream := range parameters.Streams {
		if strings.TrimSpace(stream.Name) == "" {
			issues = append(issues, ValidationIssue{Path: "parameters.streams", Message: "each stream requires a name", Severity: HealthSeverityCritical})
			continue
		}
		if len(stream.Subjects) == 0 {
			issues = append(issues, ValidationIssue{Path: "parameters.streams." + stream.Name, Message: "each stream requires at least one subject", Severity: HealthSeverityCritical})
		}
		if _, exists := streams[stream.Name]; exists {
			issues = append(issues, ValidationIssue{Path: "parameters.streams." + stream.Name, Message: "stream names must be unique", Severity: HealthSeverityCritical})
		}
		streams[stream.Name] = struct{}{}
	}
	consumers := map[string]struct{}{}
	for _, consumer := range parameters.Consumers {
		key := consumer.Stream + "/" + consumer.Name
		if strings.TrimSpace(consumer.Name) == "" || strings.TrimSpace(consumer.Stream) == "" {
			issues = append(issues, ValidationIssue{Path: "parameters.consumers", Message: "each consumer requires both stream and name", Severity: HealthSeverityCritical})
			continue
		}
		if _, ok := streams[consumer.Stream]; !ok {
			issues = append(issues, ValidationIssue{Path: "parameters.consumers." + consumer.Name, Message: fmt.Sprintf("consumer stream %q is not declared", consumer.Stream), Severity: HealthSeverityCritical})
		}
		if _, exists := consumers[key]; exists {
			issues = append(issues, ValidationIssue{Path: "parameters.consumers." + consumer.Name, Message: "consumer names must be unique per stream", Severity: HealthSeverityCritical})
		}
		consumers[key] = struct{}{}
	}
	credentials := map[string]struct{}{}
	usernames := map[string]struct{}{}
	for _, credential := range parameters.AppCredentials {
		if strings.TrimSpace(credential.Name) == "" {
			issues = append(issues, ValidationIssue{Path: "parameters.appCredentials", Message: "each app credential requires a name", Severity: HealthSeverityCritical})
			continue
		}
		if _, exists := credentials[credential.Name]; exists {
			issues = append(issues, ValidationIssue{Path: "parameters.appCredentials." + credential.Name, Message: "app credential names must be unique", Severity: HealthSeverityCritical})
		}
		credentials[credential.Name] = struct{}{}
		if credential.Username == "" {
			continue
		}
		if _, exists := usernames[credential.Username]; exists {
			issues = append(issues, ValidationIssue{Path: "parameters.appCredentials." + credential.Name, Message: "app credential usernames must be unique", Severity: HealthSeverityCritical})
		}
		usernames[credential.Username] = struct{}{}
	}
	return issues
}

func (a *NATSAdapter) resourceConfigData(parameters natsParameters) map[string]string {
	data := map[string]string{
		"apply.sh": a.reconcileScript(parameters),
	}
	for _, stream := range parameters.Streams {
		body, _ := json.MarshalIndent(a.streamConfig(stream), "", "  ")
		data["stream-"+stream.Name+".json"] = string(body)
	}
	for _, consumer := range parameters.Consumers {
		body, _ := json.MarshalIndent(a.consumerConfig(consumer), "", "  ")
		data[fmt.Sprintf("consumer-%s-%s.json", consumer.Stream, consumer.Name)] = string(body)
	}
	return data
}

func (a *NATSAdapter) streamConfig(stream natsStreamSpec) map[string]any {
	cfg := map[string]any{
		"name":        stream.Name,
		"description": stream.Description,
		"subjects":    stream.Subjects,
		"storage":     stream.Storage,
		"retention":   stream.Retention,
		"max_age":     stream.MaxAge,
	}
	if stream.MaxMsgs != 0 {
		cfg["max_msgs"] = stream.MaxMsgs
	}
	if stream.MaxBytes != 0 {
		cfg["max_bytes"] = stream.MaxBytes
	}
	if stream.Replicas != nil {
		cfg["num_replicas"] = *stream.Replicas
	}
	return cfg
}

func (a *NATSAdapter) consumerConfig(consumer natsConsumerSpec) map[string]any {
	cfg := map[string]any{
		"durable_name":    consumer.Name,
		"description":     consumer.Description,
		"ack_policy":      consumer.AckPolicy,
		"deliver_policy":  consumer.DeliverPolicy,
		"replay_policy":   consumer.ReplayPolicy,
		"max_ack_pending": consumer.MaxAckPending,
	}
	if len(consumer.FilterSubjects) == 1 {
		cfg["filter_subject"] = consumer.FilterSubjects[0]
	}
	if len(consumer.FilterSubjects) > 1 {
		cfg["filter_subjects"] = consumer.FilterSubjects
	}
	return cfg
}

func (a *NATSAdapter) reconcileScript(parameters natsParameters) string {
	lines := []string{
		"#!/bin/sh",
		"set -eu",
		"SERVER=\"nats://127.0.0.1:4222\"",
		"until nats --server \"$SERVER\" --user \"$NATS_USER\" --password \"$NATS_PASSWORD\" account info >/dev/null 2>&1; do sleep 2; done",
	}
	for _, stream := range parameters.Streams {
		path := "/config/stream-" + stream.Name + ".json"
		lines = append(lines,
			fmt.Sprintf("if nats --server \"$SERVER\" --user \"$NATS_USER\" --password \"$NATS_PASSWORD\" stream info %q >/dev/null 2>&1; then", stream.Name),
			fmt.Sprintf("  nats --server \"$SERVER\" --user \"$NATS_USER\" --password \"$NATS_PASSWORD\" stream edit %q --config %q", stream.Name, path),
			"else",
			fmt.Sprintf("  nats --server \"$SERVER\" --user \"$NATS_USER\" --password \"$NATS_PASSWORD\" stream add %q --config %q", stream.Name, path),
			"fi",
		)
	}
	for _, consumer := range parameters.Consumers {
		path := fmt.Sprintf("/config/consumer-%s-%s.json", consumer.Stream, consumer.Name)
		lines = append(lines,
			fmt.Sprintf("if nats --server \"$SERVER\" --user \"$NATS_USER\" --password \"$NATS_PASSWORD\" consumer info %q %q >/dev/null 2>&1; then", consumer.Stream, consumer.Name),
			fmt.Sprintf("  nats --server \"$SERVER\" --user \"$NATS_USER\" --password \"$NATS_PASSWORD\" consumer edit %q %q --config %q", consumer.Stream, consumer.Name, path),
			"else",
			fmt.Sprintf("  nats --server \"$SERVER\" --user \"$NATS_USER\" --password \"$NATS_PASSWORD\" consumer add %q %q --config %q", consumer.Stream, consumer.Name, path),
			"fi",
		)
	}
	return strings.Join(lines, "\n") + "\n"
}

func (a *NATSAdapter) managedResourceJob(ctx ServiceContext, namespace, authSecretName, resourceConfigName, jobName string, labels map[string]string) map[string]any {
	return map[string]any{
		"apiVersion": "batch/v1",
		"kind":       "Job",
		"metadata": map[string]any{
			"name":      jobName,
			"namespace": namespace,
			"labels":    labels,
		},
		"spec": map[string]any{
			"ttlSecondsAfterFinished": int32(600),
			"template": map[string]any{
				"metadata": map[string]any{"labels": labels},
				"spec": map[string]any{
					"restartPolicy": "OnFailure",
					"containers": []map[string]any{{
						"name":    "nats-resource-manager",
						"image":   "natsio/nats-box:0.19.5",
						"command": []string{"/bin/sh", "/config/apply.sh"},
						"env": []map[string]any{
							{"name": "NATS_USER", "value": "servicer"},
							{"name": "NATS_PASSWORD", "valueFrom": map[string]any{"secretKeyRef": map[string]any{"name": authSecretName, "key": "password"}}},
						},
						"volumeMounts": []map[string]any{{"name": "config", "mountPath": "/config"}},
					}},
					"volumes": []map[string]any{{
						"name": "config",
						"configMap": map[string]any{
							"name":        resourceConfigName,
							"defaultMode": 0755,
						},
					}},
				},
			},
		},
	}
}

func natsAppCredentialSecretName(instanceName, credentialName string) string {
	return fmt.Sprintf("%s-%s-auth", instanceName, credentialName)
}

// DefaultNATSDeletionPolicy is the preferred default for NATS instances.
const DefaultNATSDeletionPolicy = platformv1alpha1.DeletionPolicyDelete
