package adapters

import (
	"context"
	"strings"
	"testing"

	platformv1alpha1 "github.com/sindef/servicer/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestNATSAdapterRenderProducesServicerOwnedRuntime(t *testing.T) {
	adapter := NewNATSAdapter()
	ctx := sampleNATSContext(t)

	result, err := adapter.Render(context.Background(), RenderRequest{Context: ctx})
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}
	if result.RuntimeDriver != "servicer-nats" {
		t.Fatalf("expected runtime driver servicer-nats, got %q", result.RuntimeDriver)
	}
	if result.PrimaryResource == nil || result.PrimaryResource.Kind != "StatefulSet" {
		t.Fatalf("expected StatefulSet primary resource, got %#v", result.PrimaryResource)
	}
	if len(result.Artifacts) != 7 {
		t.Fatalf("expected 7 artifacts, got %d", len(result.Artifacts))
	}
	rendered := renderedArtifacts(result)
	for _, expected := range []string{"kind: StatefulSet", "nats:2.10-alpine", "jetstream", "replicas: 3", "session-bus-auth", "kind: Job", "stream-ORDERS.json"} {
		if !strings.Contains(rendered, expected) {
			t.Fatalf("expected rendered output to contain %q:\n%s", expected, rendered)
		}
	}
	if len(result.CredentialRefs) != 2 {
		t.Fatalf("expected admin and app credential refs, got %#v", result.CredentialRefs)
	}
	if strings.Contains(rendered, "NatsCluster") || strings.Contains(rendered, "nats.io/v1alpha2") {
		t.Fatalf("NATS render leaked operator CRD details:\n%s", rendered)
	}
}

func sampleNATSContext(t *testing.T) ServiceContext {
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
				DisplayName:           "NATS",
				Driver:                "servicer-nats",
				SupportedVersions:     []string{"2.10"},
				AllowsVersionOverride: true,
				Published:             true,
			},
		},
		Plan: &platformv1alpha1.ServicePlan{
			Spec: platformv1alpha1.ServicePlanSpec{
				ServiceClassRef:       platformv1alpha1.LocalObjectReference{Name: "nats"},
				DisplayName:           "JetStream",
				Topology:              "jetstream-cluster",
				DefaultVersion:        "2.10",
				AllowsVersionOverride: true,
				DefaultParameters: rawJSON(t, map[string]any{
					"jetstream":   true,
					"replicas":    3,
					"storageSize": "20Gi",
				}),
			},
		},
		Instance: &platformv1alpha1.ServiceInstance{
			ObjectMeta: metav1.ObjectMeta{Name: "session-bus"},
			Spec: platformv1alpha1.ServiceInstanceSpec{
				ProjectRef:      platformv1alpha1.LocalObjectReference{Name: "acme-prod"},
				ServiceClassRef: platformv1alpha1.LocalObjectReference{Name: "nats"},
				ServicePlanRef:  platformv1alpha1.LocalObjectReference{Name: "nats-jetstream"},
				Version:         "2.10",
				Parameters: rawJSON(t, map[string]any{
					"streams": []map[string]any{
						{"name": "ORDERS", "subjects": []string{"orders.>"}, "storage": "file"},
					},
					"consumers": []map[string]any{
						{"name": "DISPATCH", "stream": "ORDERS", "filterSubjects": []string{"orders.created"}, "ackPolicy": "explicit"},
					},
					"appCredentials": []map[string]any{
						{"name": "orders-api", "permissions": map[string]any{"publish": []string{"orders.created"}, "subscribe": []string{"orders.>"}, "allowResponses": true}},
					},
				}),
				Exposure:     platformv1alpha1.ExposureSpec{Mode: platformv1alpha1.ExposureModeClusterInternal},
				SecretPolicy: platformv1alpha1.SecretPolicySpec{DeliveryMode: platformv1alpha1.SecretDeliveryModeExternalSecret},
			},
		},
	}
}
