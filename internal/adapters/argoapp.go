package adapters

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"strings"

	platformv1alpha1 "github.com/sindef/servicer/api/v1alpha1"
	"sigs.k8s.io/yaml"
)

const argoAppDriver = "argo-application"

const (
	argoSourceTypeManifests = "manifests"
	argoSourceTypeHelm      = "helm"
)

// argoApplicationParameters holds the adapter-specific parameters for an Argo CD Application instance.
type argoApplicationParameters struct {
	// SourceType defines whether the repository path resolves to raw manifests or a Helm chart.
	SourceType string `json:"sourceType,omitempty"`
	// RepoURL is the Git repository URL (resolved from the project repository by the BFF).
	RepoURL string `json:"repoURL"`
	// Path is the directory within the repository containing the application manifests or Helm chart.
	Path string `json:"path"`
	// TargetRevision is the branch, tag, or commit SHA to track. Defaults to HEAD.
	TargetRevision string `json:"targetRevision,omitempty"`
	// TargetNamespace is the Kubernetes namespace the application deploys into.
	TargetNamespace string `json:"targetNamespace"`
	// SyncPolicy controls whether Argo CD syncs automatically ("auto") or requires manual sync ("manual").
	SyncPolicy string `json:"syncPolicy,omitempty"`
	// CreateNamespace instructs Argo CD to auto-create the destination namespace if absent.
	CreateNamespace bool `json:"createNamespace,omitempty"`
	// HelmReleaseName overrides the Helm release name when the source is a Helm chart.
	HelmReleaseName string `json:"helmReleaseName,omitempty"`
	// HelmValuesYAML is an optional inline values override in YAML format.
	HelmValuesYAML string `json:"helmValuesYAML,omitempty"`
	// RepoRef is the project repository reference name, stored for UI reference.
	RepoRef string `json:"repoRef,omitempty"`
}

// ArgoApplicationContract describes the normalized platform contract for Argo CD Application instances.
var ArgoApplicationContract = ProductContract{
	ServiceClass:            ServiceClassArgoApp,
	FriendlyName:            "Managed Application",
	RuntimeDriver:           argoAppDriver,
	SupportsVersionOverride: false,
	SupportsMultiCluster:    false,
	TopologyModes:           []string{"dedicated"},
	StatusSignals: []StatusSignalDescriptor{
		{Key: "app-sync", DisplayName: "Sync Status", Description: "Whether the Argo CD Application is synced.", Severity: HealthSeverityCritical},
		{Key: "app-health", DisplayName: "App Health", Description: "Whether the Argo CD Application is healthy.", Severity: HealthSeverityWarning},
	},
	Actions: []ActionCapability{},
}

// ArgoApplicationAdapter renders Argo CD Application resources.
type ArgoApplicationAdapter struct{}

// NewArgoApplicationAdapter creates a new Argo Application adapter.
func NewArgoApplicationAdapter() *ArgoApplicationAdapter {
	return &ArgoApplicationAdapter{}
}

func (a *ArgoApplicationAdapter) Contract() ProductContract {
	return ArgoApplicationContract
}

func (a *ArgoApplicationAdapter) Validate(_ context.Context, request ValidationRequest) (ValidationResult, error) {
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
	if ctx.Class.Spec.Driver != argoAppDriver {
		issues = append(issues, ValidationIssue{
			Path:     "serviceClass.driver",
			Message:  fmt.Sprintf("expected driver %q, got %q", argoAppDriver, ctx.Class.Spec.Driver),
			Severity: HealthSeverityCritical,
		})
	}
	if ctx.Plan.Spec.ServiceClassRef.Name != ctx.Instance.Spec.ServiceClassRef.Name {
		issues = append(issues, ValidationIssue{
			Path:     "servicePlanRef",
			Message:  "service plan does not belong to the selected service class",
			Severity: HealthSeverityCritical,
		})
	}
	params, err := a.parameters(ctx)
	if err != nil {
		issues = append(issues, ValidationIssue{Path: "parameters", Message: err.Error(), Severity: HealthSeverityCritical})
		return ValidationResult{Valid: false, Issues: issues}, nil
	}
	if params.RepoURL == "" {
		issues = append(issues, ValidationIssue{Path: "parameters.repoURL", Message: "repository URL is required", Severity: HealthSeverityCritical})
	}
	if params.Path == "" {
		issues = append(issues, ValidationIssue{Path: "parameters.path", Message: "repository path is required", Severity: HealthSeverityCritical})
	} else if err := validateRepositoryPath(params.Path); err != nil {
		issues = append(issues, ValidationIssue{Path: "parameters.path", Message: err.Error(), Severity: HealthSeverityCritical})
	}
	if params.TargetNamespace == "" {
		issues = append(issues, ValidationIssue{Path: "parameters.targetNamespace", Message: "target namespace is required", Severity: HealthSeverityCritical})
	}
	if params.SyncPolicy != "" && params.SyncPolicy != "manual" && params.SyncPolicy != "auto" {
		issues = append(issues, ValidationIssue{Path: "parameters.syncPolicy", Message: `syncPolicy must be either "manual" or "auto"`, Severity: HealthSeverityCritical})
	}
	sourceType := normalizedArgoSourceType(params)
	if sourceType != "" && sourceType != argoSourceTypeManifests && sourceType != argoSourceTypeHelm {
		issues = append(issues, ValidationIssue{Path: "parameters.sourceType", Message: `sourceType must be either "manifests" or "helm"`, Severity: HealthSeverityCritical})
	}
	if sourceType == argoSourceTypeManifests && (strings.TrimSpace(params.HelmReleaseName) != "" || strings.TrimSpace(params.HelmValuesYAML) != "") {
		issues = append(issues, ValidationIssue{Path: "parameters.sourceType", Message: "helmReleaseName and helmValuesYAML require sourceType=helm", Severity: HealthSeverityCritical})
	}
	if strings.TrimSpace(params.HelmValuesYAML) != "" {
		var valuesDoc any
		if err := yaml.Unmarshal([]byte(params.HelmValuesYAML), &valuesDoc); err != nil {
			issues = append(issues, ValidationIssue{Path: "parameters.helmValuesYAML", Message: fmt.Sprintf("invalid Helm values YAML: %v", err), Severity: HealthSeverityCritical})
		}
	}
	return ValidationResult{Valid: len(issues) == 0, Issues: issues}, nil
}

func (a *ArgoApplicationAdapter) Render(_ context.Context, request RenderRequest) (RenderResult, error) {
	ctx := request.Context
	if ctx.Instance == nil || ctx.Project == nil || ctx.Tenant == nil {
		return RenderResult{}, fmt.Errorf("tenant, project and instance context are required")
	}
	params, err := a.parameters(ctx)
	if err != nil {
		return RenderResult{}, err
	}

	revision := params.TargetRevision
	if revision == "" {
		revision = "HEAD"
	}
	syncPolicy := params.SyncPolicy
	if syncPolicy == "" {
		syncPolicy = "manual"
	}

	clusterName := instanceCluster(ctx)

	labels := map[string]any{
		"servicer.io/managed-by":       "servicer",
		"servicer.io/project":          ctx.Project.Name,
		"servicer.io/service-instance": ctx.Instance.Name,
		"servicer.io/tenant":           ctx.Tenant.Name,
	}

	source := map[string]any{
		"repoURL":        params.RepoURL,
		"targetRevision": revision,
		"path":           params.Path,
	}
	sourceType := normalizedArgoSourceType(params)
	useHelm := sourceType == argoSourceTypeHelm || (sourceType == "" && (params.HelmReleaseName != "" || params.HelmValuesYAML != ""))
	if useHelm {
		helm := map[string]any{}
		if params.HelmReleaseName != "" {
			helm["releaseName"] = params.HelmReleaseName
		}
		if params.HelmValuesYAML != "" {
			helm["values"] = params.HelmValuesYAML
		}
		source["helm"] = helm
	}

	appSpec := map[string]any{
		"project": "default",
		"source":  source,
		"destination": map[string]any{
			"server":    "https://kubernetes.default.svc",
			"namespace": params.TargetNamespace,
		},
	}

	if syncPolicy == "auto" {
		sp := map[string]any{
			"automated": map[string]any{
				"prune":    true,
				"selfHeal": true,
			},
		}
		if params.CreateNamespace {
			sp["syncOptions"] = []string{"CreateNamespace=true"}
		}
		appSpec["syncPolicy"] = sp
	} else if params.CreateNamespace {
		appSpec["syncPolicy"] = map[string]any{
			"syncOptions": []string{"CreateNamespace=true"},
		}
	}

	appManifest := map[string]any{
		"apiVersion": "argoproj.io/v1alpha1",
		"kind":       "Application",
		"metadata": map[string]any{
			"name":      ctx.Instance.Name,
			"namespace": "argocd",
			"labels":    labels,
		},
		"spec": appSpec,
	}

	appYAML, err := yaml.Marshal(appManifest)
	if err != nil {
		return RenderResult{}, fmt.Errorf("marshal argo application: %w", err)
	}

	packagePath := fmt.Sprintf("clusters/%s/argo-apps/%s", clusterName, ctx.Instance.Name)

	return RenderResult{
		RuntimeDriver: argoAppDriver,
		PrimaryResource: &platformv1alpha1.TypedObjectReference{
			APIVersion: "argoproj.io/v1alpha1",
			Kind:       "Application",
			Name:       ctx.Instance.Name,
			Namespace:  "argocd",
		},
		PackagePath: packagePath,
		Artifacts: []RenderedArtifact{
			{Path: fmt.Sprintf("%s/application.yaml", packagePath), Content: appYAML},
		},
	}, nil
}

func (a *ArgoApplicationAdapter) Observe(_ context.Context, request ObserveRequest) (NormalizedStatus, error) {
	ctx := request.Context
	if ctx.Instance == nil {
		return NormalizedStatus{}, fmt.Errorf("instance context is required")
	}
	if request.Runtime.Blocked {
		return NormalizedStatus{
			Phase:   "Degraded",
			Summary: request.Runtime.Message,
		}, nil
	}
	phase := string(SyncPhaseMaterialized)
	summary := "Application manifest materialized"
	if request.ApplicationName != "" {
		phase = string(SyncPhaseSynced)
		summary = "Managed Application deployed"
	}
	return NormalizedStatus{
		Phase:   phase,
		Summary: summary,
	}, nil
}

func (a *ArgoApplicationAdapter) Delete(_ context.Context, _ DeleteRequest) (DeleteResult, error) {
	return DeleteResult{}, nil
}

func (a *ArgoApplicationAdapter) SupportedActions(_ context.Context, _ ServiceContext) []ActionCapability {
	return ArgoApplicationContract.Actions
}

func (a *ArgoApplicationAdapter) ExecuteAction(_ context.Context, _ ExecuteActionRequest) (ActionExecutionResult, error) {
	return ActionExecutionResult{}, fmt.Errorf("no actions defined for argo-application")
}

func (a *ArgoApplicationAdapter) parameters(ctx ServiceContext) (argoApplicationParameters, error) {
	if ctx.Instance.Spec.Parameters == nil {
		return argoApplicationParameters{}, nil
	}
	var params argoApplicationParameters
	if err := json.Unmarshal(ctx.Instance.Spec.Parameters.Raw, &params); err != nil {
		return argoApplicationParameters{}, fmt.Errorf("parse argo-application parameters: %w", err)
	}
	return params, nil
}

func normalizedArgoSourceType(params argoApplicationParameters) string {
	return strings.ToLower(strings.TrimSpace(params.SourceType))
}

func validateRepositoryPath(value string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	if strings.HasPrefix(trimmed, "/") {
		return fmt.Errorf("repository path must be relative")
	}
	cleaned := path.Clean(trimmed)
	if cleaned == "." || cleaned == "/" {
		return fmt.Errorf("repository path must resolve to a directory within the repository")
	}
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return fmt.Errorf("repository path must not traverse outside the repository root")
	}
	return nil
}
