package adapters

import (
	"context"
	"strings"
	"testing"

	platformv1alpha1 "github.com/sindef/servicer/api/v1alpha1"
)

func TestMySQLAdapterRenderProducesStatefulSetManifest(t *testing.T) {
	adapter := NewMySQLAdapter()
	ctx := sampleMySQLContext(t)

	result, err := adapter.Render(context.Background(), RenderRequest{Context: ctx})
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}
	if result.RuntimeDriver != mysqlRuntimeDriver {
		t.Fatalf("expected runtime driver %q, got %q", mysqlRuntimeDriver, result.RuntimeDriver)
	}
	if result.PrimaryResource == nil || result.PrimaryResource.Kind != "StatefulSet" {
		t.Fatalf("expected primary resource to be a StatefulSet, got %#v", result.PrimaryResource)
	}
	if len(result.CredentialRefs) != 1 || result.CredentialRefs[0].Name != "orders-mysql-auth" {
		t.Fatalf("expected MySQL auth secret ref, got %#v", result.CredentialRefs)
	}
	rendered := renderedArtifacts(result)
	for _, expected := range []string{
		"kind: StatefulSet",
		"image: mysql:8.4",
		"MYSQL_DATABASE",
		"orders_mysql",
		"servicer.io/mysql-mode: single-primary",
		"storage: 100Gi",
	} {
		if !strings.Contains(rendered, expected) {
			t.Fatalf("expected rendered output to contain %q:\n%s", expected, rendered)
		}
	}
}

func TestMySQLAdapterRenderProducesGaleraPackages(t *testing.T) {
	adapter := NewMySQLAdapter()
	ctx := sampleMySQLContext(t)
	ctx.Plan.Spec.Topology = "multi-region"
	ctx.Plan.Spec.DefaultParameters = rawJSON(t, map[string]any{
		"replicationMode": "galera",
		"replicas":        3,
		"storageSize":     "100Gi",
		"standbyClusters": []string{"west-2"},
	})
	ctx.Instance.Spec.ServicePlanRef = platformv1alpha1.LocalObjectReference{Name: "mysql-galera"}
	ctx.Instance.Spec.Parameters = rawJSON(t, map[string]any{
		"primaryCluster":  "east-1",
		"standbyClusters": []string{"west-2"},
	})

	result, err := adapter.Render(context.Background(), RenderRequest{Context: ctx})
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}
	if len(result.PackagePaths) != 2 {
		t.Fatalf("expected 2 package paths for multi-region Galera, got %#v", result.PackagePaths)
	}
	if !containsString(result.PackagePaths, "clusters/west-2/tenants/acme/projects/acme-prod/services/orders-mysql") {
		t.Fatalf("expected standby package path to be rendered, got %#v", result.PackagePaths)
	}
	rendered := renderedArtifacts(result)
	for _, expected := range []string{
		"wsrep_on=ON",
		"wsrep_cluster_name=orders-mysql",
		"servicer.io/mysql-role: peer",
	} {
		if !strings.Contains(rendered, expected) {
			t.Fatalf("expected rendered output to contain %q:\n%s", expected, rendered)
		}
	}
}

func sampleMySQLContext(t *testing.T) ServiceContext {
	t.Helper()
	ctx := samplePostgreSQLContext(t)
	ctx.Class = &platformv1alpha1.ServiceClass{
		Spec: platformv1alpha1.ServiceClassSpec{
			DisplayName:           "MySQL",
			Driver:                mysqlRuntimeDriver,
			SupportedVersions:     []string{"8.4", "8.0"},
			AllowsVersionOverride: true,
			Published:             true,
		},
	}
	ctx.Plan = &platformv1alpha1.ServicePlan{
		Spec: platformv1alpha1.ServicePlanSpec{
			ServiceClassRef:       platformv1alpha1.LocalObjectReference{Name: "mysql"},
			DisplayName:           "Standard HA",
			Topology:              "single-cluster",
			DefaultVersion:        "8.4",
			AllowsVersionOverride: true,
			DefaultParameters: rawJSON(t, map[string]any{
				"replicas":      3,
				"storageSize":   "100Gi",
				"cpu":           "1",
				"memory":        "2Gi",
				"backupProfile": "daily-7d",
			}),
		},
	}
	ctx.Instance.ObjectMeta = metav1ObjectMeta("orders-mysql")
	ctx.Instance.Spec.ServiceClassRef = platformv1alpha1.LocalObjectReference{Name: "mysql"}
	ctx.Instance.Spec.ServicePlanRef = platformv1alpha1.LocalObjectReference{Name: "mysql-ha"}
	ctx.Instance.Spec.Version = "8.4"
	ctx.Instance.Spec.Parameters = rawJSON(t, map[string]any{
		"storageClass": "fast-ssd",
	})
	ctx.Instance.Status.Placement.Namespace = "acme-prod-orders-mysql"
	return ctx
}
