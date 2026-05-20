package adapters

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	platformv1alpha1 "github.com/sindef/servicer/api/v1alpha1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestCNPGAdapterValidateRejectsWrongDriver(t *testing.T) {
	adapter := NewCNPGAdapter()
	ctx := samplePostgreSQLContext(t)
	ctx.Class.Spec.Driver = "not-cnpg"

	result, err := adapter.Validate(context.Background(), ValidationRequest{Context: ctx})
	if err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}
	if result.Valid {
		t.Fatalf("expected validation to fail for wrong driver")
	}
}

func TestCNPGAdapterRenderProducesClusterManifest(t *testing.T) {
	adapter := NewCNPGAdapter()
	ctx := samplePostgreSQLContext(t)

	result, err := adapter.Render(context.Background(), RenderRequest{Context: ctx})
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}
	if result.RuntimeDriver != "cnpg" {
		t.Fatalf("expected runtime driver cnpg, got %q", result.RuntimeDriver)
	}
	if result.PrimaryResource == nil || result.PrimaryResource.Kind != "Cluster" {
		t.Fatalf("expected primary resource to be a CNPG Cluster, got %#v", result.PrimaryResource)
	}
	if len(result.Artifacts) != 2 {
		t.Fatalf("expected 2 rendered artifacts, got %d", len(result.Artifacts))
	}
	expectedPackagePath := "clusters/east-1/tenants/acme/projects/acme-prod/services/orders-db"
	if result.PackagePath != expectedPackagePath {
		t.Fatalf("expected package path %q, got %q", expectedPackagePath, result.PackagePath)
	}
	if len(result.CredentialRefs) != 1 || result.CredentialRefs[0].Name != "orders-db-app" {
		t.Fatalf("expected CNPG app credential ref, got %#v", result.CredentialRefs)
	}
	clusterManifest := string(result.Artifacts[1].Content)
	if !strings.Contains(clusterManifest, "kind: Cluster") {
		t.Fatalf("expected rendered manifest to include a Cluster kind, got:\n%s", clusterManifest)
	}
	if !strings.Contains(clusterManifest, "apiVersion: postgresql.cnpg.io/v1") {
		t.Fatalf("expected rendered manifest to include the CNPG apiVersion, got:\n%s", clusterManifest)
	}
	if !strings.Contains(clusterManifest, "instances: 3") {
		t.Fatalf("expected rendered manifest to default to 3 instances for high-availability, got:\n%s", clusterManifest)
	}
	if !strings.Contains(clusterManifest, "database: orders_db") {
		t.Fatalf("expected rendered manifest to bootstrap instance-derived database name, got:\n%s", clusterManifest)
	}
	if !strings.Contains(clusterManifest, "owner: orders_db") {
		t.Fatalf("expected rendered manifest to use derived database owner, got:\n%s", clusterManifest)
	}
}

func TestCNPGAdapterRenderHonorsExplicitDatabaseName(t *testing.T) {
	adapter := NewCNPGAdapter()
	ctx := samplePostgreSQLContext(t)
	ctx.Instance.Spec.Parameters = rawJSON(t, map[string]any{
		"storageClass":  "fast-ssd",
		"backupProfile": "daily-7d",
		"databaseName":  "Reporting DB",
	})

	result, err := adapter.Render(context.Background(), RenderRequest{Context: ctx})
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}

	clusterManifest := string(result.Artifacts[1].Content)
	if !strings.Contains(clusterManifest, "database: reporting_db") {
		t.Fatalf("expected rendered manifest to use normalized explicit database name, got:\n%s", clusterManifest)
	}
	if !strings.Contains(clusterManifest, "owner: reporting_db") {
		t.Fatalf("expected rendered manifest to use normalized explicit owner, got:\n%s", clusterManifest)
	}
}

func samplePostgreSQLContext(t *testing.T) ServiceContext {
	t.Helper()
	return ServiceContext{
		Tenant: &platformv1alpha1.Tenant{ObjectMeta: metav1ObjectMeta("acme")},
		Project: &platformv1alpha1.Project{
			ObjectMeta: metav1ObjectMeta("acme-prod"),
			Spec: platformv1alpha1.ProjectSpec{
				TenantRef: platformv1alpha1.LocalObjectReference{Name: "acme"},
				TargetSelector: platformv1alpha1.TargetSelectorSpec{
					ClusterRef: &platformv1alpha1.LocalObjectReference{Name: "east-1"},
				},
				NamespaceStrategy: platformv1alpha1.NamespaceStrategySpec{Mode: platformv1alpha1.NamespaceStrategyDedicated, Prefix: "acme-prod"},
			},
			Status: platformv1alpha1.ProjectStatus{Placement: platformv1alpha1.PlacementStatus{ClusterName: "east-1"}},
		},
		Class: &platformv1alpha1.ServiceClass{
			Spec: platformv1alpha1.ServiceClassSpec{
				DisplayName:           "PostgreSQL",
				Driver:                "cnpg",
				SupportedVersions:     []string{"16", "15"},
				AllowsVersionOverride: true,
			},
		},
		Plan: &platformv1alpha1.ServicePlan{
			Spec: platformv1alpha1.ServicePlanSpec{
				ServiceClassRef:       platformv1alpha1.LocalObjectReference{Name: "postgresql"},
				DisplayName:           "Standard HA",
				Topology:              "high-availability",
				DefaultVersion:        "16",
				AllowsVersionOverride: true,
				DefaultParameters: rawJSON(t, map[string]any{
					"storageSize": "100Gi",
				}),
			},
		},
		Instance: &platformv1alpha1.ServiceInstance{
			ObjectMeta: metav1ObjectMeta("orders-db"),
			Spec: platformv1alpha1.ServiceInstanceSpec{
				ProjectRef:      platformv1alpha1.LocalObjectReference{Name: "acme-prod"},
				ServiceClassRef: platformv1alpha1.LocalObjectReference{Name: "postgresql"},
				ServicePlanRef:  platformv1alpha1.LocalObjectReference{Name: "postgresql-ha"},
				Version:         "16",
				Parameters: rawJSON(t, map[string]any{
					"storageClass":  "fast-ssd",
					"backupProfile": "daily-7d",
				}),
				Exposure:     platformv1alpha1.ExposureSpec{Mode: platformv1alpha1.ExposureModeClusterInternal},
				SecretPolicy: platformv1alpha1.SecretPolicySpec{DeliveryMode: platformv1alpha1.SecretDeliveryModeExternalSecret},
			},
			Status: platformv1alpha1.ServiceInstanceStatus{Placement: platformv1alpha1.PlacementStatus{ClusterName: "east-1", Namespace: "acme-prod-orders-db"}},
		},
	}
}

func rawJSON(t *testing.T, value any) *apiextensionsv1.JSON {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal json: %v", err)
	}
	return &apiextensionsv1.JSON{Raw: raw}
}

func metav1ObjectMeta(name string) metav1.ObjectMeta {
	return metav1.ObjectMeta{Name: name}
}
