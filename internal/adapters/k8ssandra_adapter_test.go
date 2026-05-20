package adapters

import (
	"context"
	"strings"
	"testing"

	platformv1alpha1 "github.com/sindef/servicer/api/v1alpha1"
)

func TestK8ssandraAdapterValidateRequiresAdditionalDatacenterForMultiDC(t *testing.T) {
	adapter := NewK8ssandraAdapter()
	ctx := sampleK8ssandraContext(t)
	ctx.Plan.Spec.Topology = "multi-datacenter"
	ctx.Instance.Spec.Parameters = rawJSON(t, map[string]any{
		"storageSize": "100Gi",
	})

	result, err := adapter.Validate(context.Background(), ValidationRequest{Context: ctx})
	if err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}
	if result.Valid {
		t.Fatalf("expected validation to fail, got %#v", result)
	}
	if len(result.Issues) == 0 || result.Issues[0].Path != "parameters.datacenters" {
		t.Fatalf("expected datacenters validation issue, got %#v", result.Issues)
	}
}

func TestK8ssandraAdapterRenderProducesK8ssandraClusterManifest(t *testing.T) {
	adapter := NewK8ssandraAdapter()
	ctx := sampleK8ssandraContext(t)

	result, err := adapter.Render(context.Background(), RenderRequest{Context: ctx})
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}
	if result.RuntimeDriver != k8ssandraRuntimeDriver {
		t.Fatalf("expected runtime driver %q, got %q", k8ssandraRuntimeDriver, result.RuntimeDriver)
	}
	if result.PrimaryResource == nil || result.PrimaryResource.Kind != k8ssandraRuntimeKind {
		t.Fatalf("expected K8ssandraCluster primary resource, got %#v", result.PrimaryResource)
	}
	rendered := renderedArtifacts(result)
	for _, expected := range []string{
		"kind: K8ssandraCluster",
		"serverVersion: 4.1.3",
		"name: orders-cassandra-east-1",
		"name: orders-cassandra-west-2",
		"storage: 100Gi",
		"backupProfile: daily-7d",
	} {
		if !strings.Contains(rendered, expected) {
			t.Fatalf("expected rendered output to contain %q:\n%s", expected, rendered)
		}
	}
}

func sampleK8ssandraContext(t *testing.T) ServiceContext {
	t.Helper()
	ctx := samplePostgreSQLContext(t)
	ctx.Class = &platformv1alpha1.ServiceClass{
		Spec: platformv1alpha1.ServiceClassSpec{
			DisplayName:           "Cassandra",
			Driver:                k8ssandraRuntimeDriver,
			SupportedVersions:     []string{"4.1.3"},
			AllowsVersionOverride: true,
			Published:             true,
		},
	}
	ctx.Plan = &platformv1alpha1.ServicePlan{
		Spec: platformv1alpha1.ServicePlanSpec{
			ServiceClassRef:       platformv1alpha1.LocalObjectReference{Name: "cassandra"},
			DisplayName:           "Multi-DC",
			Topology:              "multi-datacenter",
			DefaultVersion:        "4.1.3",
			AllowsVersionOverride: true,
			DefaultParameters: rawJSON(t, map[string]any{
				"replicas":      3,
				"storageSize":   "100Gi",
				"backupProfile": "daily-7d",
			}),
		},
	}
	ctx.Instance.ObjectMeta = metav1ObjectMeta("orders-cassandra")
	ctx.Instance.Spec.ServiceClassRef = platformv1alpha1.LocalObjectReference{Name: "cassandra"}
	ctx.Instance.Spec.ServicePlanRef = platformv1alpha1.LocalObjectReference{Name: "cassandra-ha"}
	ctx.Instance.Spec.Version = "4.1.3"
	ctx.Instance.Spec.Parameters = rawJSON(t, map[string]any{
		"datacenters":    []string{"west-2"},
		"storageClass":   "fast-ssd",
		"backupProfile":  "daily-7d",
		"repairSchedule": "0 2 * * *",
	})
	ctx.Instance.Status.Placement.Namespace = "acme-prod-orders-cassandra"
	return ctx
}
