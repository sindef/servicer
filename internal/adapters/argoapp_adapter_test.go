package adapters

import (
	"context"
	"strings"
	"testing"

	platformv1alpha1 "github.com/sindef/servicer/api/v1alpha1"
)

func TestArgoApplicationAdapterValidateRequiresRepoPathAndNamespace(t *testing.T) {
	adapter := NewArgoApplicationAdapter()
	ctx := sampleArgoApplicationContext(t)
	ctx.Instance.Spec.Parameters = rawJSON(t, map[string]any{
		"repoURL": "",
		"path":    "",
	})

	result, err := adapter.Validate(context.Background(), ValidationRequest{Context: ctx})
	if err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}
	if result.Valid {
		t.Fatalf("expected validation to fail, got %#v", result)
	}
	if len(result.Issues) != 3 {
		t.Fatalf("expected three validation issues, got %#v", result.Issues)
	}
}

func TestArgoApplicationAdapterValidateRejectsInvalidSourceType(t *testing.T) {
	adapter := NewArgoApplicationAdapter()
	ctx := sampleArgoApplicationContext(t)
	ctx.Instance.Spec.Parameters = rawJSON(t, map[string]any{
		"repoURL":         "https://github.com/acme/storefront.git",
		"path":            "apps/storefront",
		"targetNamespace": "storefront",
		"sourceType":      "kustomize",
	})

	result, err := adapter.Validate(context.Background(), ValidationRequest{Context: ctx})
	if err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}
	if result.Valid {
		t.Fatalf("expected invalid sourceType to fail validation")
	}
	if !containsValidationIssue(result.Issues, "parameters.sourceType") {
		t.Fatalf("expected sourceType validation issue, got %#v", result.Issues)
	}
}

func TestArgoApplicationAdapterValidateRejectsPathTraversal(t *testing.T) {
	adapter := NewArgoApplicationAdapter()
	ctx := sampleArgoApplicationContext(t)
	ctx.Instance.Spec.Parameters = rawJSON(t, map[string]any{
		"repoURL":         "https://github.com/acme/storefront.git",
		"path":            "../secrets",
		"targetNamespace": "storefront",
		"sourceType":      "manifests",
	})

	result, err := adapter.Validate(context.Background(), ValidationRequest{Context: ctx})
	if err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}
	if result.Valid {
		t.Fatalf("expected traversal path to fail validation")
	}
	if !containsValidationIssue(result.Issues, "parameters.path") {
		t.Fatalf("expected path validation issue, got %#v", result.Issues)
	}
}

func TestArgoApplicationAdapterValidateRejectsHelmFieldsForManifestSource(t *testing.T) {
	adapter := NewArgoApplicationAdapter()
	ctx := sampleArgoApplicationContext(t)
	ctx.Instance.Spec.Parameters = rawJSON(t, map[string]any{
		"repoURL":         "https://github.com/acme/storefront.git",
		"path":            "apps/storefront",
		"targetNamespace": "storefront",
		"sourceType":      "manifests",
		"helmReleaseName": "storefront",
	})

	result, err := adapter.Validate(context.Background(), ValidationRequest{Context: ctx})
	if err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}
	if result.Valid {
		t.Fatalf("expected manifests source with helm fields to fail validation")
	}
	if !containsValidationIssue(result.Issues, "parameters.sourceType") {
		t.Fatalf("expected sourceType validation issue, got %#v", result.Issues)
	}
}

func TestArgoApplicationAdapterValidateRejectsInvalidHelmValuesYAML(t *testing.T) {
	adapter := NewArgoApplicationAdapter()
	ctx := sampleArgoApplicationContext(t)
	ctx.Instance.Spec.Parameters = rawJSON(t, map[string]any{
		"repoURL":         "https://github.com/acme/storefront.git",
		"path":            "charts/storefront",
		"targetNamespace": "storefront",
		"sourceType":      "helm",
		"helmValuesYAML":  "invalid: [",
	})

	result, err := adapter.Validate(context.Background(), ValidationRequest{Context: ctx})
	if err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}
	if result.Valid {
		t.Fatalf("expected invalid helm values YAML to fail validation")
	}
	if !containsValidationIssue(result.Issues, "parameters.helmValuesYAML") {
		t.Fatalf("expected helmValuesYAML validation issue, got %#v", result.Issues)
	}
}

func TestArgoApplicationAdapterRenderProducesApplicationManifest(t *testing.T) {
	adapter := NewArgoApplicationAdapter()
	ctx := sampleArgoApplicationContext(t)

	result, err := adapter.Render(context.Background(), RenderRequest{Context: ctx})
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}
	if result.RuntimeDriver != argoAppDriver {
		t.Fatalf("expected runtime driver %q, got %q", argoAppDriver, result.RuntimeDriver)
	}
	if result.PrimaryResource == nil || result.PrimaryResource.Kind != "Application" || result.PrimaryResource.Namespace != "argocd" {
		t.Fatalf("expected Argo CD Application primary resource, got %#v", result.PrimaryResource)
	}
	if result.PackagePath != "clusters/east-1/argo-apps/storefront" {
		t.Fatalf("unexpected package path %q", result.PackagePath)
	}
	rendered := renderedArtifacts(result)
	for _, expected := range []string{
		"kind: Application",
		"repoURL: https://github.com/acme/storefront.git",
		"path: charts/storefront",
		"targetRevision: main",
		"namespace: storefront-prod",
		"CreateNamespace=true",
		"releaseName: storefront",
		"replicaCount: 2",
	} {
		if !strings.Contains(rendered, expected) {
			t.Fatalf("expected rendered output to contain %q:\n%s", expected, rendered)
		}
	}
}

func TestArgoApplicationAdapterRenderOmitsHelmForManifestSource(t *testing.T) {
	adapter := NewArgoApplicationAdapter()
	ctx := sampleArgoApplicationContext(t)
	ctx.Instance.Spec.Parameters = rawJSON(t, map[string]any{
		"repoURL":         "https://github.com/acme/storefront.git",
		"path":            "apps/storefront",
		"targetRevision":  "main",
		"targetNamespace": "storefront-prod",
		"syncPolicy":      "manual",
		"sourceType":      "manifests",
	})

	result, err := adapter.Render(context.Background(), RenderRequest{Context: ctx})
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}
	rendered := renderedArtifacts(result)
	if strings.Contains(rendered, "helm:") {
		t.Fatalf("expected manifest source to omit helm source settings:\n%s", rendered)
	}
}

func sampleArgoApplicationContext(t *testing.T) ServiceContext {
	t.Helper()
	ctx := sampleNamespaceContext(t)
	ctx.Class = &platformv1alpha1.ServiceClass{
		Spec: platformv1alpha1.ServiceClassSpec{
			DisplayName: "Managed Application",
			Driver:      argoAppDriver,
			Published:   true,
		},
	}
	ctx.Plan = &platformv1alpha1.ServicePlan{
		Spec: platformv1alpha1.ServicePlanSpec{
			ServiceClassRef: platformv1alpha1.LocalObjectReference{Name: "argo-application"},
			DisplayName:     "Standard",
			Topology:        "dedicated",
		},
	}
	ctx.Instance.ObjectMeta = metav1ObjectMeta("storefront")
	ctx.Instance.Spec.ServiceClassRef = platformv1alpha1.LocalObjectReference{Name: "argo-application"}
	ctx.Instance.Spec.ServicePlanRef = platformv1alpha1.LocalObjectReference{Name: "argo-application-standard"}
	ctx.Instance.Spec.Parameters = rawJSON(t, map[string]any{
		"repoURL":         "https://github.com/acme/storefront.git",
		"path":            "charts/storefront",
		"targetRevision":  "main",
		"targetNamespace": "storefront-prod",
		"syncPolicy":      "auto",
		"createNamespace": true,
		"sourceType":      "helm",
		"helmReleaseName": "storefront",
		"helmValuesYAML":  "replicaCount: 2",
	})
	ctx.Instance.Status.Placement.ClusterName = "east-1"
	return ctx
}

func containsValidationIssue(issues []ValidationIssue, path string) bool {
	for _, issue := range issues {
		if issue.Path == path {
			return true
		}
	}
	return false
}
