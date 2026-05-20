package adapters

import (
	"context"
	"strings"
	"testing"

	platformv1alpha1 "github.com/sindef/servicer/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestValkeyContractStaysServicerOwnedAndFailoverOriented(t *testing.T) {
	adapter := NewValkeyAdapter()
	contract := adapter.Contract()

	if contract.RuntimeDriver != "servicer-valkey" {
		t.Fatalf("expected Servicer-owned runtime driver, got %q", contract.RuntimeDriver)
	}
	if !contract.SupportsMultiCluster {
		t.Fatalf("expected Valkey contract to advertise failover-oriented multi-cluster support")
	}
	if !containsString(contract.TopologyModes, "multi-region") {
		t.Fatalf("expected Valkey contract to publish multi-region topology")
	}
	foundFailover := false
	for _, capability := range contract.Actions {
		if capability.Name == ActionFailover {
			foundFailover = true
			if !capability.RequiresApproval {
				t.Fatalf("expected failover action to require approval")
			}
		}
	}
	if !foundFailover {
		t.Fatalf("expected failover action in Valkey contract")
	}
}

func TestValkeyAdapterRenderProducesServicerOwnedRuntime(t *testing.T) {
	adapter := NewValkeyAdapter()
	ctx := sampleValkeyContext(t)

	result, err := adapter.Render(context.Background(), RenderRequest{Context: ctx})
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}
	if result.RuntimeDriver != "servicer-valkey" {
		t.Fatalf("expected runtime driver servicer-valkey, got %q", result.RuntimeDriver)
	}
	if result.PrimaryResource == nil || result.PrimaryResource.Kind != "StatefulSet" || result.PrimaryResource.APIVersion != "apps/v1" {
		t.Fatalf("expected primary resource to be an apps/v1 StatefulSet, got %#v", result.PrimaryResource)
	}
	expectedPackagePath := "clusters/east-1/tenants/acme/projects/acme-prod/services/session-cache"
	if result.PackagePath != expectedPackagePath {
		t.Fatalf("expected package path %q, got %q", expectedPackagePath, result.PackagePath)
	}
	if len(result.Artifacts) != 5 {
		t.Fatalf("expected 5 rendered artifacts, got %d", len(result.Artifacts))
	}

	combined := renderedValkeyArtifacts(result)
	for _, forbidden := range []string{"ValkeyCluster", "redis.redis.opstreelabs.in", "valkey-operator"} {
		if strings.Contains(combined, forbidden) {
			t.Fatalf("render output leaked beta-operator detail %q:\n%s", forbidden, combined)
		}
	}
	if !strings.Contains(combined, "kind: StatefulSet") {
		t.Fatalf("expected rendered output to include StatefulSet, got:\n%s", combined)
	}
	if !strings.Contains(combined, "replicas: 3") {
		t.Fatalf("expected replicated plan to render 3 replicas, got:\n%s", combined)
	}
	if !strings.Contains(combined, "secretKeyRef:") || !strings.Contains(combined, "session-cache-auth") {
		t.Fatalf("expected StatefulSet to reference external credential secret without rendering secret material, got:\n%s", combined)
	}
	if strings.Contains(combined, "stringData:") || strings.Contains(combined, "password:") {
		t.Fatalf("rendered Valkey artifacts must not store password material:\n%s", combined)
	}
}

func TestValkeyAdapterValidateAcceptsMultiClusterFailoverTopology(t *testing.T) {
	adapter := NewValkeyAdapter()
	ctx := sampleValkeyContext(t)
	ctx.Plan.Spec.Topology = "multi-cluster-failover"
	ctx.Plan.Spec.DefaultParameters = rawJSON(t, map[string]any{
		"memoryProfile":         "medium",
		"persistence":           "persistent",
		"replicas":              3,
		"storageSize":           "10Gi",
		"primaryCluster":        "east-1",
		"standbyClusters":       []string{"west-2"},
		"replicationLagSeconds": map[string]int32{"west-2": 5},
	})

	result, err := adapter.Validate(context.Background(), ValidationRequest{Context: ctx})
	if err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}
	if !result.Valid {
		t.Fatalf("expected validation to succeed for failover topology, got issues %#v", result.Issues)
	}
}

func TestValkeyAdapterValidateKeepsDeclaredTopologyStableAfterPromotion(t *testing.T) {
	adapter := NewValkeyAdapter()
	ctx := sampleValkeyContext(t)
	ctx.Plan.Spec.Topology = "multi-cluster-failover"
	ctx.Plan.Spec.DefaultParameters = rawJSON(t, map[string]any{
		"primaryCluster":  "east-1",
		"standbyClusters": []string{"west-2"},
		"memoryProfile":   "medium",
		"persistence":     "persistent",
		"replicas":        3,
		"storageSize":     "10Gi",
	})
	ctx.Instance.Status.CacheTopology.PrimaryCluster = "west-2"

	result, err := adapter.Validate(context.Background(), ValidationRequest{Context: ctx})
	if err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}
	if !result.Valid {
		t.Fatalf("expected validation to remain tied to declared topology after promotion, got %#v", result.Issues)
	}
}

func TestValkeyAdapterRenderProducesFailoverPackagesForPrimaryAndStandby(t *testing.T) {
	adapter := NewValkeyAdapter()
	ctx := sampleValkeyContext(t)
	ctx.Plan.Spec.Topology = "multi-cluster-failover"
	ctx.Plan.Spec.DefaultParameters = rawJSON(t, map[string]any{
		"memoryProfile":            "medium",
		"persistence":              "persistent",
		"replicas":                 3,
		"storageSize":              "10Gi",
		"primaryCluster":           "east-1",
		"standbyClusters":          []string{"west-2"},
		"maxReplicationLagSeconds": 30,
	})

	result, err := adapter.Render(context.Background(), RenderRequest{Context: ctx})
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}
	if result.PackagePath != "clusters/east-1/tenants/acme/projects/acme-prod/services/session-cache" {
		t.Fatalf("expected primary package path, got %q", result.PackagePath)
	}
	if len(result.PackagePaths) != 2 {
		t.Fatalf("expected primary and standby package paths, got %#v", result.PackagePaths)
	}
	if len(result.Artifacts) != 12 {
		t.Fatalf("expected 12 rendered artifacts for two failover clusters, got %d", len(result.Artifacts))
	}
	combined := renderedValkeyArtifacts(result)
	for _, expected := range []string{
		"clusters/west-2/tenants/acme/projects/acme-prod/services/session-cache/statefulset.yaml",
		"servicer.io/cache-role: standby",
		"replicaof session-cache.acme-prod-session-cache.svc.cluster.local 6379",
		"name: session-cache-traffic-policy",
		"trafficEndpoint: session-cache.acme-prod.cache.servicer.local:6379",
	} {
		if !strings.Contains(combined, expected) && !renderedValkeyPathsContain(result, expected) {
			t.Fatalf("expected rendered failover output to contain %q:\n%s", expected, combined)
		}
	}
}

func TestValkeyAdapterRenderUsesPromotedPrimaryStatus(t *testing.T) {
	adapter := NewValkeyAdapter()
	ctx := sampleValkeyContext(t)
	ctx.Plan.Spec.Topology = "multi-cluster-failover"
	ctx.Plan.Spec.DefaultParameters = rawJSON(t, map[string]any{
		"primaryCluster":  "east-1",
		"standbyClusters": []string{"west-2"},
		"memoryProfile":   "medium",
		"persistence":     "persistent",
		"replicas":        3,
		"storageSize":     "10Gi",
	})
	ctx.Instance.Status.CacheTopology = platformv1alpha1.CacheTopologyStatus{
		Mode:           "multi-cluster-failover",
		PrimaryCluster: "west-2",
		StandbyClusters: []platformv1alpha1.CacheStandbyStatus{
			{ClusterName: "east-1", ResyncRequired: true, Message: "resync required"},
		},
	}

	result, err := adapter.Render(context.Background(), RenderRequest{Context: ctx})
	if err != nil {
		t.Fatalf("Render returned error after promotion: %v", err)
	}
	if result.PackagePath != "clusters/west-2/tenants/acme/projects/acme-prod/services/session-cache" {
		t.Fatalf("expected promoted primary package path, got %q", result.PackagePath)
	}
	if len(result.PackagePaths) != 2 {
		t.Fatalf("expected promoted primary and previous-primary standby packages, got %#v", result.PackagePaths)
	}
}

func TestValkeyAdapterObserveBlocksFailoverWhenLagIsUnobserved(t *testing.T) {
	adapter := NewValkeyAdapter()
	ctx := sampleValkeyContext(t)
	ctx.Plan.Spec.Topology = "multi-cluster-failover"
	ctx.Plan.Spec.DefaultParameters = rawJSON(t, map[string]any{
		"primaryCluster":  "east-1",
		"standbyClusters": []string{"west-2"},
		"memoryProfile":   "medium",
		"persistence":     "persistent",
		"replicas":        3,
		"storageSize":     "10Gi",
	})

	status, err := adapter.Observe(context.Background(), ObserveRequest{Context: ctx})
	if err != nil {
		t.Fatalf("Observe returned error: %v", err)
	}
	if status.CacheTopology == nil || status.CacheTopology.FailoverReadiness != "Blocked" {
		t.Fatalf("expected failover readiness blocked, got %#v", status.CacheTopology)
	}
	if len(status.CacheTopology.StandbyClusters) != 1 || status.CacheTopology.StandbyClusters[0].LagObserved {
		t.Fatalf("expected standby lag to be unobserved, got %#v", status.CacheTopology.StandbyClusters)
	}
}

func TestValkeyAdapterSupportedActionsAreSafeInitialSet(t *testing.T) {
	adapter := NewValkeyAdapter()
	ctx := sampleValkeyContext(t)
	actions := adapter.SupportedActions(context.Background(), ctx)

	for _, expected := range []ActionName{ActionScale, ActionRestart, ActionRotateCredentials} {
		if !hasAction(actions, expected) {
			t.Fatalf("expected action %q in supported actions %#v", expected, actions)
		}
	}
	if hasAction(actions, ActionFailover) {
		t.Fatalf("did not expect failover in first single-cluster Valkey action set")
	}
}

func TestValkeyAdapterSupportedActionsIncludeFailoverForFailoverTopology(t *testing.T) {
	adapter := NewValkeyAdapter()
	ctx := sampleValkeyContext(t)
	ctx.Plan.Spec.Topology = "multi-cluster-failover"
	actions := adapter.SupportedActions(context.Background(), ctx)

	if !hasAction(actions, ActionFailover) {
		t.Fatalf("expected failover in multi-cluster Valkey action set")
	}
	if !hasAction(actions, ActionRollbackFailover) || !hasAction(actions, ActionResyncStandby) {
		t.Fatalf("expected rollback and resync actions in multi-cluster Valkey action set")
	}
}

func TestValkeyAdapterExecuteScaleQueuesServicerOwnedOperation(t *testing.T) {
	adapter := NewValkeyAdapter()
	ctx := sampleValkeyContext(t)
	action := &platformv1alpha1.ActionRequest{
		ObjectMeta: metav1.ObjectMeta{Name: "session-cache-scale"},
		Spec: platformv1alpha1.ActionRequestSpec{
			Action: string(ActionScale),
		},
	}

	result, err := adapter.ExecuteAction(context.Background(), ExecuteActionRequest{Context: ctx, Action: action})
	if err != nil {
		t.Fatalf("ExecuteAction returned error: %v", err)
	}
	if result.Phase != "Queued" {
		t.Fatalf("expected queued action, got %q", result.Phase)
	}
	if result.OperationRef == nil || result.OperationRef.APIVersion != "apps/v1" || result.OperationRef.Kind != "StatefulSet" {
		t.Fatalf("expected action to target Servicer-owned StatefulSet runtime, got %#v", result.OperationRef)
	}
}

func sampleValkeyContext(t *testing.T) ServiceContext {
	t.Helper()
	replicas := int32(3)
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
				DisplayName:           "Valkey",
				Driver:                "servicer-valkey",
				SupportedVersions:     []string{"8.0"},
				AllowsVersionOverride: true,
			},
		},
		Plan: &platformv1alpha1.ServicePlan{
			Spec: platformv1alpha1.ServicePlanSpec{
				ServiceClassRef:       platformv1alpha1.LocalObjectReference{Name: "valkey"},
				DisplayName:           "Replicated",
				Topology:              "replicated",
				DefaultVersion:        "8.0",
				AllowsVersionOverride: true,
				DefaultParameters: rawJSON(t, map[string]any{
					"memoryProfile": "medium",
					"persistence":   "persistent",
					"replicas":      replicas,
					"storageSize":   "10Gi",
				}),
			},
		},
		Instance: &platformv1alpha1.ServiceInstance{
			ObjectMeta: metav1.ObjectMeta{Name: "session-cache"},
			Spec: platformv1alpha1.ServiceInstanceSpec{
				ProjectRef:      platformv1alpha1.LocalObjectReference{Name: "acme-prod"},
				ServiceClassRef: platformv1alpha1.LocalObjectReference{Name: "valkey"},
				ServicePlanRef:  platformv1alpha1.LocalObjectReference{Name: "valkey-replicated"},
				Version:         "8.0",
				Exposure:        platformv1alpha1.ExposureSpec{Mode: platformv1alpha1.ExposureModeClusterInternal},
				SecretPolicy:    platformv1alpha1.SecretPolicySpec{DeliveryMode: platformv1alpha1.SecretDeliveryModeExternalSecret},
			},
			Status: platformv1alpha1.ServiceInstanceStatus{Placement: platformv1alpha1.PlacementStatus{ClusterName: "east-1", Namespace: "acme-prod-session-cache"}},
		},
	}
}

func renderedValkeyArtifacts(result RenderResult) string {
	var builder strings.Builder
	for _, artifact := range result.Artifacts {
		builder.Write(artifact.Content)
		builder.WriteString("\n---\n")
	}
	return builder.String()
}

func renderedValkeyPathsContain(result RenderResult, expected string) bool {
	for _, artifact := range result.Artifacts {
		if strings.Contains(artifact.Path, expected) {
			return true
		}
	}
	return false
}

func hasAction(actions []ActionCapability, expected ActionName) bool {
	for _, action := range actions {
		if action.Name == expected {
			return true
		}
	}
	return false
}
