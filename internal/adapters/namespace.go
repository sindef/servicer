package adapters

import (
	"context"
	"encoding/json"
	"fmt"

	platformv1alpha1 "github.com/sindef/servicer/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

const namespaceDriver = "kubernetes-namespace"

type namespaceParameters struct {
	CPU                string            `json:"cpu,omitempty"`
	Memory             string            `json:"memory,omitempty"`
	Pods               string            `json:"pods,omitempty"`
	DefaultDenyIngress bool              `json:"defaultDenyIngress,omitempty"`
	Labels             map[string]string `json:"labels,omitempty"`
}

// NamespaceContract describes the normalized platform contract for managed Kubernetes namespaces.
var NamespaceContract = ProductContract{
	ServiceClass:            ServiceClassNamespace,
	FriendlyName:            "Kubernetes Namespace",
	RuntimeDriver:           namespaceDriver,
	SupportsVersionOverride: false,
	SupportsMultiCluster:    false,
	TopologyModes:           []string{"dedicated"},
	StatusSignals: []StatusSignalDescriptor{
		{Key: "namespace-ready", DisplayName: "Namespace Ready", Description: "Whether the managed namespace has been created.", Severity: HealthSeverityCritical},
		{Key: "quota-posture", DisplayName: "Quota Posture", Description: "Whether resource quota intent is present.", Severity: HealthSeverityWarning},
		{Key: "policy-posture", DisplayName: "Policy Posture", Description: "Whether baseline namespace policy is present.", Severity: HealthSeverityWarning},
	},
	Actions: []ActionCapability{
		{Name: ActionUpdateQuota, DisplayName: "Update Quota", RequiresApproval: false, Disruptive: false},
		{Name: ActionGrantAccess, DisplayName: "Grant Access", RequiresApproval: false, Disruptive: false},
	},
}

// NamespaceAdapter renders native Kubernetes namespace tenancy primitives.
type NamespaceAdapter struct{}

// NewNamespaceAdapter creates a native namespace adapter.
func NewNamespaceAdapter() *NamespaceAdapter {
	return &NamespaceAdapter{}
}

func (a *NamespaceAdapter) Contract() ProductContract {
	return NamespaceContract
}

func (a *NamespaceAdapter) Validate(_ context.Context, request ValidationRequest) (ValidationResult, error) {
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
	if ctx.Class.Spec.Driver != namespaceDriver {
		issues = append(issues, ValidationIssue{Path: "serviceClass.driver", Message: fmt.Sprintf("expected driver %q, got %q", namespaceDriver, ctx.Class.Spec.Driver), Severity: HealthSeverityCritical})
	}
	if ctx.Plan.Spec.ServiceClassRef.Name != ctx.Instance.Spec.ServiceClassRef.Name {
		issues = append(issues, ValidationIssue{Path: "servicePlanRef", Message: "service plan does not belong to the selected service class", Severity: HealthSeverityCritical})
	}
	if !containsString(a.Contract().TopologyModes, ctx.Plan.Spec.Topology) {
		issues = append(issues, ValidationIssue{Path: "servicePlan.topology", Message: fmt.Sprintf("topology %q is not supported for namespace products", ctx.Plan.Spec.Topology), Severity: HealthSeverityCritical})
	}
	if _, err := a.parameters(ctx); err != nil {
		issues = append(issues, ValidationIssue{Path: "parameters", Message: err.Error(), Severity: HealthSeverityCritical})
	}
	return ValidationResult{Valid: len(issues) == 0, Issues: issues}, nil
}

func (a *NamespaceAdapter) Render(_ context.Context, request RenderRequest) (RenderResult, error) {
	ctx := request.Context
	if ctx.Instance == nil || ctx.Project == nil || ctx.Tenant == nil {
		return RenderResult{}, fmt.Errorf("tenant, project and instance context are required")
	}
	parameters, err := a.parameters(ctx)
	if err != nil {
		return RenderResult{}, err
	}

	namespace := instanceNamespace(ctx)
	clusterName := instanceCluster(ctx)
	labels := map[string]string{
		"servicer.io/managed-by":       "servicer",
		"servicer.io/project":          ctx.Project.Name,
		"servicer.io/service-instance": ctx.Instance.Name,
		"servicer.io/tenant":           ctx.Tenant.Name,
	}
	for key, value := range parameters.Labels {
		labels[key] = value
	}

	namespaceManifest := map[string]any{
		"apiVersion": "v1",
		"kind":       "Namespace",
		"metadata": map[string]any{
			"name":   namespace,
			"labels": labels,
		},
	}
	quotaManifest := map[string]any{
		"apiVersion": "v1",
		"kind":       "ResourceQuota",
		"metadata": map[string]any{
			"name":      fmt.Sprintf("%s-quota", ctx.Instance.Name),
			"namespace": namespace,
			"labels":    labels,
		},
		"spec": map[string]any{
			"hard": map[string]string{
				"requests.cpu":    parameters.CPU,
				"requests.memory": parameters.Memory,
				"pods":            parameters.Pods,
			},
		},
	}
	limitRangeManifest := map[string]any{
		"apiVersion": "v1",
		"kind":       "LimitRange",
		"metadata": map[string]any{
			"name":      fmt.Sprintf("%s-defaults", ctx.Instance.Name),
			"namespace": namespace,
			"labels":    labels,
		},
		"spec": map[string]any{
			"limits": []map[string]any{{
				"type": "Container",
				"defaultRequest": map[string]string{
					"cpu":    "100m",
					"memory": "128Mi",
				},
			}},
		},
	}
	networkPolicyManifest := map[string]any{
		"apiVersion": "networking.k8s.io/v1",
		"kind":       "NetworkPolicy",
		"metadata": map[string]any{
			"name":      fmt.Sprintf("%s-default-deny", ctx.Instance.Name),
			"namespace": namespace,
			"labels":    labels,
		},
		"spec": map[string]any{
			"podSelector": map[string]any{},
			"policyTypes": []string{"Ingress"},
		},
	}

	manifests := []struct {
		name string
		body map[string]any
	}{
		{name: "namespace.yaml", body: namespaceManifest},
		{name: "resourcequota.yaml", body: quotaManifest},
		{name: "limitrange.yaml", body: limitRangeManifest},
	}
	if parameters.DefaultDenyIngress {
		manifests = append(manifests, struct {
			name string
			body map[string]any
		}{name: "networkpolicy-default-deny.yaml", body: networkPolicyManifest})
	}

	basePath := fmt.Sprintf("clusters/%s/tenants/%s/projects/%s/services/%s", clusterName, ctx.Tenant.Name, ctx.Project.Name, ctx.Instance.Name)
	artifacts := make([]RenderedArtifact, 0, len(manifests))
	for _, manifest := range manifests {
		content, err := yaml.Marshal(manifest.body)
		if err != nil {
			return RenderResult{}, err
		}
		artifacts = append(artifacts, RenderedArtifact{Path: fmt.Sprintf("%s/%s", basePath, manifest.name), Content: content})
	}

	return RenderResult{
		RuntimeDriver: namespaceDriver,
		PrimaryResource: &platformv1alpha1.TypedObjectReference{
			APIVersion: "v1",
			Kind:       "Namespace",
			Name:       namespace,
		},
		PackagePath: basePath,
		Artifacts:   artifacts,
	}, nil
}

func (a *NamespaceAdapter) Observe(_ context.Context, request ObserveRequest) (NormalizedStatus, error) {
	ctx := request.Context
	if ctx.Instance == nil {
		return NormalizedStatus{}, fmt.Errorf("service instance context is required")
	}
	phase := "Provisioning"
	summary := "Waiting for managed namespace to be observed."
	if runtimeResourceObserved(request.ObservedResources, "v1", "Namespace", instanceNamespace(ctx)) {
		phase = "Ready"
		summary = "Managed namespace and baseline policy are ready."
	}
	return NormalizedStatus{
		Phase:             phase,
		Summary:           summary,
		Conditions:        append([]metav1.Condition(nil), ctx.Instance.Status.Conditions...),
		ObservedResources: append([]platformv1alpha1.TypedObjectReference(nil), request.ObservedResources...),
		Sync: SyncStatus{
			Phase:            SyncPhaseMaterialized,
			ArtifactRevision: request.ArtifactRevision,
			ApplicationName:  request.ApplicationName,
			Message:          "Waiting for Argo CD reconciliation.",
		},
		HealthSignals: []HealthSignal{
			{Key: "namespace-ready", Status: phase, Severity: HealthSeverityInfo, Message: summary},
			{Key: "quota-posture", Status: "Configured", Severity: HealthSeverityInfo, Message: "ResourceQuota intent is materialized."},
			{Key: "policy-posture", Status: "Configured", Severity: HealthSeverityInfo, Message: "Baseline namespace policy intent is materialized."},
		},
	}, nil
}

func (a *NamespaceAdapter) Delete(_ context.Context, request DeleteRequest) (DeleteResult, error) {
	if request.Context.Instance == nil {
		return DeleteResult{}, fmt.Errorf("service instance context is required")
	}
	return DeleteResult{Message: "Delete requested for managed namespace."}, nil
}

func (a *NamespaceAdapter) SupportedActions(_ context.Context, _ ServiceContext) []ActionCapability {
	return append([]ActionCapability(nil), a.Contract().Actions...)
}

func (a *NamespaceAdapter) ExecuteAction(_ context.Context, request ExecuteActionRequest) (ActionExecutionResult, error) {
	if request.Context.Instance == nil || request.Action == nil {
		return ActionExecutionResult{}, fmt.Errorf("service instance and action request context are required")
	}
	return ActionExecutionResult{Phase: "Queued", Message: fmt.Sprintf("Namespace action %q queued for controller execution.", request.Action.Spec.Action), Retryable: true}, nil
}

func (a *NamespaceAdapter) parameters(ctx ServiceContext) (namespaceParameters, error) {
	parameters := namespaceParameters{}
	if ctx.Plan != nil && ctx.Plan.Spec.DefaultParameters != nil {
		if err := json.Unmarshal(ctx.Plan.Spec.DefaultParameters.Raw, &parameters); err != nil {
			return namespaceParameters{}, fmt.Errorf("decode plan defaults: %w", err)
		}
	}
	if ctx.Instance != nil && ctx.Instance.Spec.Parameters != nil {
		if err := json.Unmarshal(ctx.Instance.Spec.Parameters.Raw, &parameters); err != nil {
			return namespaceParameters{}, fmt.Errorf("decode instance parameters: %w", err)
		}
	}
	if parameters.CPU == "" {
		parameters.CPU = "2"
	}
	if parameters.Memory == "" {
		parameters.Memory = "4Gi"
	}
	if parameters.Pods == "" {
		parameters.Pods = "20"
	}
	return parameters, nil
}

func runtimeResourceObserved(observed []platformv1alpha1.TypedObjectReference, apiVersion, kind, name string) bool {
	for _, ref := range observed {
		if ref.APIVersion == apiVersion && ref.Kind == kind && ref.Name == name {
			return true
		}
	}
	return false
}
