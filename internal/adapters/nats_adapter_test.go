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
	for _, expected := range []string{"kind: StatefulSet", "nats:2.10-alpine", "jetstream", "replicas: 3", "session-bus-auth", "kind: Job", "stream-ORDERS.json", "cluster {", "session-bus-0.session-bus-headless"} {
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

func TestNATSAdapterValidateRequiresStandbyClustersForGeoTopology(t *testing.T) {
	adapter := NewNATSAdapter()
	ctx := sampleNATSContext(t)
	ctx.Plan.Spec.Topology = "multi-region"
	ctx.Plan.Spec.DefaultParameters = rawJSON(t, map[string]any{
		"jetstream": true,
		"replicas":  3,
	})
	ctx.Instance.Spec.Parameters = nil

	result, err := adapter.Validate(context.Background(), ValidationRequest{Context: ctx})
	if err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}
	if result.Valid {
		t.Fatalf("expected validation to fail, got %#v", result)
	}
	found := false
	for _, issue := range result.Issues {
		if issue.Path == "parameters.standbyClusters" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected standby cluster validation issue, got %#v", result.Issues)
	}
}

func TestNATSAdapterRenderProducesGeoGatewayArtifacts(t *testing.T) {
	adapter := NewNATSAdapter()
	ctx := sampleNATSContext(t)
	ctx.Plan.Spec.Topology = "multi-region"
	ctx.Plan.Spec.DefaultParameters = rawJSON(t, map[string]any{
		"jetstream":       true,
		"replicas":        3,
		"storageSize":     "20Gi",
		"primaryCluster":  "east-1",
		"standbyClusters": []string{"west-2"},
	})

	result, err := adapter.Render(context.Background(), RenderRequest{Context: ctx})
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}
	if result.PackagePath != "clusters/east-1/tenants/acme/projects/acme-prod/services/session-bus" {
		t.Fatalf("unexpected primary package path %q", result.PackagePath)
	}
	if len(result.PackagePaths) != 2 {
		t.Fatalf("expected primary and standby package paths, got %#v", result.PackagePaths)
	}
	rendered := renderedArtifacts(result)
	for _, expected := range []string{
		"gateway {",
		"session-bus-gateway.acme-prod.west-2.nats.servicer.local:7222",
		"servicer.io/cluster-target: west-2",
	} {
		if !strings.Contains(rendered, expected) {
			t.Fatalf("expected rendered output to contain %q:\n%s", expected, rendered)
		}
	}
	foundGatewayArtifact := false
	for _, artifact := range result.Artifacts {
		if artifact.Path == "clusters/west-2/tenants/acme/projects/acme-prod/services/session-bus/gateway-service.yaml" {
			foundGatewayArtifact = true
			break
		}
	}
	if !foundGatewayArtifact {
		t.Fatalf("expected standby gateway service artifact, got %#v", result.Artifacts)
	}
}

func TestNATSAdapterObserveReportsGeoGatewayLag(t *testing.T) {
	adapter := NewNATSAdapter()
	ctx := sampleNATSContext(t)
	ctx.Plan.Spec.Topology = "multi-region"
	ctx.Plan.Spec.DefaultParameters = rawJSON(t, map[string]any{
		"jetstream":            true,
		"standbyClusters":      []string{"west-2"},
		"gatewayLagSeconds":    map[string]int32{"west-2": 45},
		"maxGatewayLagSeconds": 30,
	})

	status, err := adapter.Observe(context.Background(), ObserveRequest{Context: ctx})
	if err != nil {
		t.Fatalf("Observe returned error: %v", err)
	}
	signal := natsHealthSignal(status.HealthSignals, "gateway-health")
	if signal.Status != "Degraded" {
		t.Fatalf("expected degraded gateway health, got %#v", signal)
	}
	if !strings.Contains(signal.Message, "45s") {
		t.Fatalf("expected lag seconds in gateway message, got %q", signal.Message)
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

func natsHealthSignal(signals []HealthSignal, key string) HealthSignal {
	for _, signal := range signals {
		if signal.Key == key {
			return signal
		}
	}
	return HealthSignal{}
}
