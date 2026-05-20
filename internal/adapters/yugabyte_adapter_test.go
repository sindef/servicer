package adapters

import (
	"context"
	"strings"
	"testing"

	platformv1alpha1 "github.com/sindef/servicer/api/v1alpha1"
)

func TestYugabyteAdapterRenderProducesUniverseManifest(t *testing.T) {
	adapter := NewYugabyteAdapter()
	ctx := sampleYugabyteContext(t)

	result, err := adapter.Render(context.Background(), RenderRequest{Context: ctx})
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}
	if result.RuntimeDriver != "yb-operator" {
		t.Fatalf("expected runtime driver yb-operator, got %q", result.RuntimeDriver)
	}
	if result.PrimaryResource == nil || result.PrimaryResource.Kind != "YBUniverse" {
		t.Fatalf("expected primary resource to be a YBUniverse, got %#v", result.PrimaryResource)
	}
	rendered := renderedArtifacts(result)
	for _, expected := range []string{
		"kind: YBUniverse",
		"apiVersion: operator.yugabyte.io/v1alpha1",
		"numNodes: 1",
		"replicationFactor: 1",
		"servicer.io/database-name: testdb",
		"kubernetesOverrides:",
		"cpu: 500m",
		"memory: 1Gi",
		"volumeSize: 10",
		"ybSoftwareVersion: 2.20.1.3-b3",
	} {
		if !strings.Contains(rendered, expected) {
			t.Fatalf("expected rendered output to contain %q:\n%s", expected, rendered)
		}
	}
	if strings.Contains(rendered, "kind: YBCluster") || strings.Contains(rendered, "ybclusters.operator.yugabyte.io") {
		t.Fatalf("render output used the obsolete YBCluster CRD:\n%s", rendered)
	}
}

func TestYugabyteAdapterRenderHonorsExplicitDatabaseName(t *testing.T) {
	adapter := NewYugabyteAdapter()
	ctx := sampleYugabyteContext(t)
	ctx.Instance.Spec.Parameters = rawJSON(t, map[string]any{
		"databaseName": "Customer 360",
	})

	result, err := adapter.Render(context.Background(), RenderRequest{Context: ctx})
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}

	rendered := renderedArtifacts(result)
	if !strings.Contains(rendered, "servicer.io/database-name: customer_360") {
		t.Fatalf("expected rendered output to carry normalized database name annotation, got:\n%s", rendered)
	}
}

func sampleYugabyteContext(t *testing.T) ServiceContext {
	t.Helper()
	ctx := samplePostgreSQLContext(t)
	ctx.Class = &platformv1alpha1.ServiceClass{
		Spec: platformv1alpha1.ServiceClassSpec{
			DisplayName:           "YugabyteDB",
			Driver:                "yb-operator",
			SupportedVersions:     []string{"2.20"},
			AllowsVersionOverride: true,
			Published:             true,
		},
	}
	ctx.Plan = &platformv1alpha1.ServicePlan{
		Spec: platformv1alpha1.ServicePlanSpec{
			ServiceClassRef:       platformv1alpha1.LocalObjectReference{Name: "yugabyte"},
			DisplayName:           "Development",
			Topology:              "single-cluster",
			DefaultVersion:        "2.20",
			AllowsVersionOverride: true,
			DefaultParameters: rawJSON(t, map[string]any{
				"replicationFactor": 1,
				"tserverReplicas":   1,
				"cpu":               "500m",
				"memory":            "1Gi",
				"storageSize":       "10Gi",
			}),
		},
	}
	ctx.Instance.ObjectMeta = metav1ObjectMeta("testdb")
	ctx.Instance.Spec.ServiceClassRef = platformv1alpha1.LocalObjectReference{Name: "yugabyte"}
	ctx.Instance.Spec.ServicePlanRef = platformv1alpha1.LocalObjectReference{Name: "yugabyte-dev"}
	ctx.Instance.Spec.Version = "2.20"
	ctx.Instance.Status.Placement.Namespace = "acme-prod-testdb"
	return ctx
}
