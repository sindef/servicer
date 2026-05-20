package adapters

import (
	"context"
	"strings"
	"testing"

	platformv1alpha1 "github.com/sindef/servicer/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestNamespaceAdapterRenderProducesNativeTenancyArtifacts(t *testing.T) {
	adapter := NewNamespaceAdapter()
	ctx := sampleNamespaceContext(t)

	result, err := adapter.Render(context.Background(), RenderRequest{Context: ctx})
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}
	if result.RuntimeDriver != "kubernetes-namespace" {
		t.Fatalf("expected runtime driver kubernetes-namespace, got %q", result.RuntimeDriver)
	}
	if result.PrimaryResource == nil || result.PrimaryResource.Kind != "Namespace" || result.PrimaryResource.Name != "acme-acme-prod-team-space" {
		t.Fatalf("expected Namespace primary resource, got %#v", result.PrimaryResource)
	}
	if len(result.Artifacts) != 4 {
		t.Fatalf("expected 4 artifacts, got %d", len(result.Artifacts))
	}
	rendered := renderedArtifacts(result)
	for _, expected := range []string{"kind: Namespace", "kind: ResourceQuota", "kind: LimitRange", "kind: NetworkPolicy", "requests.cpu: \"4\""} {
		if !strings.Contains(rendered, expected) {
			t.Fatalf("expected rendered output to contain %q:\n%s", expected, rendered)
		}
	}
}

func sampleNamespaceContext(t *testing.T) ServiceContext {
	t.Helper()
	return ServiceContext{
		Tenant: &platformv1alpha1.Tenant{ObjectMeta: metav1.ObjectMeta{Name: "acme"}},
		Project: &platformv1alpha1.Project{
			ObjectMeta: metav1.ObjectMeta{Name: "acme-prod"},
			Spec: platformv1alpha1.ProjectSpec{
				TenantRef:         platformv1alpha1.LocalObjectReference{Name: "acme"},
				TargetSelector:    platformv1alpha1.TargetSelectorSpec{ClusterRef: &platformv1alpha1.LocalObjectReference{Name: "east-1"}},
				NamespaceStrategy: platformv1alpha1.NamespaceStrategySpec{Mode: platformv1alpha1.NamespaceStrategyDedicated, Prefix: "acme-prod"},
			},
			Status: platformv1alpha1.ProjectStatus{Placement: platformv1alpha1.PlacementStatus{ClusterName: "east-1"}},
		},
		Class: &platformv1alpha1.ServiceClass{
			Spec: platformv1alpha1.ServiceClassSpec{
				DisplayName: "Namespace",
				Driver:      "kubernetes-namespace",
				Published:   true,
			},
		},
		Plan: &platformv1alpha1.ServicePlan{
			Spec: platformv1alpha1.ServicePlanSpec{
				ServiceClassRef: platformv1alpha1.LocalObjectReference{Name: "namespace"},
				DisplayName:     "Team Namespace",
				Topology:        "dedicated",
				DefaultParameters: rawJSON(t, map[string]any{
					"cpu":                "4",
					"memory":             "8Gi",
					"pods":               "40",
					"defaultDenyIngress": true,
				}),
			},
		},
		Instance: &platformv1alpha1.ServiceInstance{
			ObjectMeta: metav1.ObjectMeta{Name: "team-space"},
			Spec: platformv1alpha1.ServiceInstanceSpec{
				ProjectRef:      platformv1alpha1.LocalObjectReference{Name: "acme-prod"},
				ServiceClassRef: platformv1alpha1.LocalObjectReference{Name: "namespace"},
				ServicePlanRef:  platformv1alpha1.LocalObjectReference{Name: "namespace-team"},
				Exposure:        platformv1alpha1.ExposureSpec{Mode: platformv1alpha1.ExposureModeClusterInternal},
				SecretPolicy:    platformv1alpha1.SecretPolicySpec{DeliveryMode: platformv1alpha1.SecretDeliveryModeManual},
			},
		},
	}
}

func renderedArtifacts(result RenderResult) string {
	var builder strings.Builder
	for _, artifact := range result.Artifacts {
		builder.Write(artifact.Content)
		builder.WriteString("\n---\n")
	}
	return builder.String()
}
