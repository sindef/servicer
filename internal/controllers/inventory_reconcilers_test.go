package controllers

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	platformv1alpha1 "github.com/sindef/servicer/api/v1alpha1"
	"github.com/sindef/servicer/internal/adapters"
	"github.com/sindef/servicer/internal/materializer"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestClusterTargetReconcilerMarksReachableWithConnectionSecret(t *testing.T) {
	scheme := inventoryTestScheme(t)
	clusterTarget := &platformv1alpha1.ClusterTarget{
		ObjectMeta: metav1.ObjectMeta{Name: "local-dev"},
		Spec: platformv1alpha1.ClusterTargetSpec{
			DisplayName:   "Local Development",
			ConnectionRef: platformv1alpha1.NamespacedObjectReference{Name: "local-dev-kubeconfig", Namespace: "servicer-system"},
			Capabilities: map[string]string{
				"kubernetesVersion": "v1.32.0",
				"operator.cnpg":     "available",
				"operator.nats":     "available",
			},
		},
	}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "local-dev-kubeconfig", Namespace: "servicer-system"},
		Data:       map[string][]byte{"kubeconfig": []byte("apiVersion: v1")},
	}

	reconciler := &ClusterTargetReconciler{
		Client: fake.NewClientBuilder().
			WithScheme(scheme).
			WithStatusSubresource(&platformv1alpha1.ClusterTarget{}).
			WithObjects(clusterTarget, secret).
			Build(),
		Scheme: scheme,
	}

	if _, err := reconciler.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKey{Name: "local-dev"}}); err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	var updated platformv1alpha1.ClusterTarget
	if err := reconciler.Get(context.Background(), client.ObjectKey{Name: "local-dev"}, &updated); err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if updated.Status.Phase != "Ready" {
		t.Fatalf("expected phase Ready, got %q", updated.Status.Phase)
	}
	if !updated.Status.Reachable {
		t.Fatalf("expected cluster target to be reachable")
	}
	if updated.Status.KubernetesVersion != "v1.32.0" {
		t.Fatalf("expected kubernetes version to be populated, got %q", updated.Status.KubernetesVersion)
	}
	if len(updated.Status.OperatorInventory) != 2 {
		t.Fatalf("expected 2 operators in inventory, got %#v", updated.Status.OperatorInventory)
	}
}

func TestServiceClassReconcilerPublishesImplementedPostgreSQLClass(t *testing.T) {
	scheme := inventoryTestScheme(t)
	registry, err := adapters.NewRegistry(adapters.NewCNPGAdapter())
	if err != nil {
		t.Fatalf("NewRegistry returned error: %v", err)
	}
	serviceClass := &platformv1alpha1.ServiceClass{
		ObjectMeta: metav1.ObjectMeta{Name: "postgresql"},
		Spec: platformv1alpha1.ServiceClassSpec{
			DisplayName:           "PostgreSQL",
			Driver:                "cnpg",
			SupportedVersions:     []string{"16", "15"},
			AllowsVersionOverride: true,
			Published:             true,
		},
	}

	reconciler := &ServiceClassReconciler{
		Client: fake.NewClientBuilder().
			WithScheme(scheme).
			WithStatusSubresource(&platformv1alpha1.ServiceClass{}).
			WithObjects(serviceClass).
			Build(),
		Scheme:   scheme,
		Adapters: registry,
	}

	if _, err := reconciler.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKey{Name: "postgresql"}}); err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	var updated platformv1alpha1.ServiceClass
	if err := reconciler.Get(context.Background(), client.ObjectKey{Name: "postgresql"}, &updated); err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if updated.Status.Phase != "Ready" {
		t.Fatalf("expected phase Ready, got %q", updated.Status.Phase)
	}
	if !updated.Status.Published {
		t.Fatalf("expected service class to be published")
	}
	if !isStatusConditionTrue(updated.Status.Conditions, "Ready") {
		t.Fatalf("expected Ready condition to be true")
	}
}

func TestServicePlanReconcilerPublishesPlanWhenParentClassIsPublished(t *testing.T) {
	scheme := inventoryTestScheme(t)
	serviceClass := &platformv1alpha1.ServiceClass{
		ObjectMeta: metav1.ObjectMeta{Name: "postgresql", Generation: 1},
		Spec: platformv1alpha1.ServiceClassSpec{
			DisplayName:       "PostgreSQL",
			Driver:            "cnpg",
			SupportedVersions: []string{"16", "15"},
			Published:         true,
		},
		Status: platformv1alpha1.ServiceClassStatus{
			ObservedGeneration: 1,
			Phase:              "Ready",
			Published:          true,
			Conditions: []metav1.Condition{
				{Type: "Accepted", Status: metav1.ConditionTrue, ObservedGeneration: 1, Reason: "ValidationSucceeded", Message: "accepted"},
				{Type: "Ready", Status: metav1.ConditionTrue, ObservedGeneration: 1, Reason: "ServiceClassReady", Message: "ready"},
			},
		},
	}
	servicePlan := &platformv1alpha1.ServicePlan{
		ObjectMeta: metav1.ObjectMeta{Name: "postgresql-ha"},
		Spec: platformv1alpha1.ServicePlanSpec{
			ServiceClassRef: platformv1alpha1.LocalObjectReference{Name: "postgresql"},
			DisplayName:     "Standard HA",
			Topology:        "high-availability",
			DefaultVersion:  "16",
		},
	}

	reconciler := &ServicePlanReconciler{
		Client: fake.NewClientBuilder().
			WithScheme(scheme).
			WithStatusSubresource(&platformv1alpha1.ServicePlan{}).
			WithObjects(serviceClass, servicePlan).
			Build(),
		Scheme: scheme,
	}

	if _, err := reconciler.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKey{Name: "postgresql-ha"}}); err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	var updated platformv1alpha1.ServicePlan
	if err := reconciler.Get(context.Background(), client.ObjectKey{Name: "postgresql-ha"}, &updated); err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if updated.Status.Phase != "Ready" {
		t.Fatalf("expected phase Ready, got %q", updated.Status.Phase)
	}
	if !updated.Status.Published {
		t.Fatalf("expected service plan to be published")
	}
}

func TestProjectReconcilerWaitsForClusterTargetReadiness(t *testing.T) {
	scheme := inventoryTestScheme(t)
	project := &platformv1alpha1.Project{
		ObjectMeta: metav1.ObjectMeta{Name: "acme-prod"},
		Spec: platformv1alpha1.ProjectSpec{
			TenantRef:         platformv1alpha1.LocalObjectReference{Name: "acme"},
			DisplayName:       "Acme Production",
			Environment:       platformv1alpha1.EnvironmentProduction,
			TargetSelector:    platformv1alpha1.TargetSelectorSpec{ClusterRef: &platformv1alpha1.LocalObjectReference{Name: "local-dev"}},
			NamespaceStrategy: platformv1alpha1.NamespaceStrategySpec{Mode: platformv1alpha1.NamespaceStrategyDedicated, Prefix: "acme-prod"},
		},
	}
	tenant := &platformv1alpha1.Tenant{
		ObjectMeta: metav1.ObjectMeta{Name: "acme"},
		Spec: platformv1alpha1.TenantSpec{
			DisplayName:           "Acme Corp",
			Owners:                platformv1alpha1.OwnersSpec{Users: []string{"alice@example.com"}},
			QuotaProfileRef:       platformv1alpha1.LocalObjectReference{Name: "standard-tenant"},
			AllowedServiceClasses: []string{"postgresql"},
			Lifecycle:             platformv1alpha1.TenantLifecycleSpec{Phase: platformv1alpha1.TenantLifecyclePhaseActive},
		},
	}
	clusterTarget := &platformv1alpha1.ClusterTarget{
		ObjectMeta: metav1.ObjectMeta{Name: "local-dev", Generation: 1},
		Spec: platformv1alpha1.ClusterTargetSpec{
			DisplayName:   "Local Development",
			ConnectionRef: platformv1alpha1.NamespacedObjectReference{Name: "local-dev-kubeconfig", Namespace: "servicer-system"},
		},
		Status: platformv1alpha1.ClusterTargetStatus{
			ObservedGeneration: 1,
			Phase:              "PendingCredentials",
			Reachable:          false,
			Conditions: []metav1.Condition{
				{Type: "Accepted", Status: metav1.ConditionTrue, ObservedGeneration: 1, Reason: "ValidationSucceeded", Message: "accepted"},
				{Type: "Ready", Status: metav1.ConditionFalse, ObservedGeneration: 1, Reason: "CredentialsPending", Message: "pending"},
			},
		},
	}

	reconciler := &ProjectReconciler{
		Client: fake.NewClientBuilder().
			WithScheme(scheme).
			WithStatusSubresource(&platformv1alpha1.Project{}).
			WithObjects(project, tenant, clusterTarget).
			Build(),
		Scheme: scheme,
	}

	if _, err := reconciler.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKey{Name: "acme-prod"}}); err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	var updated platformv1alpha1.Project
	if err := reconciler.Get(context.Background(), client.ObjectKey{Name: "acme-prod"}, &updated); err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if updated.Status.Phase != "PendingPlacement" {
		t.Fatalf("expected phase PendingPlacement, got %q", updated.Status.Phase)
	}
	if isStatusConditionTrue(updated.Status.Conditions, "Ready") {
		t.Fatalf("expected project Ready condition to remain false")
	}
}

func TestServiceInstanceReconcilerRejectsUnpublishedServicePlan(t *testing.T) {
	scheme := inventoryTestScheme(t)
	registry, err := adapters.NewRegistry(adapters.NewCNPGAdapter())
	if err != nil {
		t.Fatalf("NewRegistry returned error: %v", err)
	}
	tenant := &platformv1alpha1.Tenant{
		ObjectMeta: metav1.ObjectMeta{Name: "acme"},
		Spec: platformv1alpha1.TenantSpec{
			DisplayName:           "Acme Corp",
			Owners:                platformv1alpha1.OwnersSpec{Users: []string{"alice@example.com"}},
			QuotaProfileRef:       platformv1alpha1.LocalObjectReference{Name: "standard-tenant"},
			AllowedServiceClasses: []string{"postgresql"},
			Lifecycle:             platformv1alpha1.TenantLifecycleSpec{Phase: platformv1alpha1.TenantLifecyclePhaseActive},
		},
	}
	project := &platformv1alpha1.Project{
		ObjectMeta: metav1.ObjectMeta{Name: "acme-prod", Generation: 1},
		Spec: platformv1alpha1.ProjectSpec{
			TenantRef:         platformv1alpha1.LocalObjectReference{Name: "acme"},
			DisplayName:       "Acme Production",
			Environment:       platformv1alpha1.EnvironmentProduction,
			TargetSelector:    platformv1alpha1.TargetSelectorSpec{ClusterRef: &platformv1alpha1.LocalObjectReference{Name: "local-dev"}},
			NamespaceStrategy: platformv1alpha1.NamespaceStrategySpec{Mode: platformv1alpha1.NamespaceStrategyDedicated, Prefix: "acme-prod"},
		},
		Status: platformv1alpha1.ProjectStatus{
			ObservedGeneration: 1,
			Phase:              "Ready",
			Placement:          platformv1alpha1.PlacementStatus{ClusterName: "local-dev"},
			Conditions: []metav1.Condition{
				{Type: "Ready", Status: metav1.ConditionTrue, ObservedGeneration: 1, Reason: "ProjectReady", Message: "ready"},
			},
		},
	}
	serviceClass := &platformv1alpha1.ServiceClass{
		ObjectMeta: metav1.ObjectMeta{Name: "postgresql", Generation: 1},
		Spec: platformv1alpha1.ServiceClassSpec{
			DisplayName:       "PostgreSQL",
			Driver:            "cnpg",
			SupportedVersions: []string{"16"},
			Published:         true,
		},
		Status: platformv1alpha1.ServiceClassStatus{
			ObservedGeneration: 1,
			Phase:              "Ready",
			Published:          true,
			Conditions: []metav1.Condition{
				{Type: "Accepted", Status: metav1.ConditionTrue, ObservedGeneration: 1, Reason: "ValidationSucceeded", Message: "accepted"},
			},
		},
	}
	servicePlan := &platformv1alpha1.ServicePlan{
		ObjectMeta: metav1.ObjectMeta{Name: "postgresql-ha", Generation: 1},
		Spec: platformv1alpha1.ServicePlanSpec{
			ServiceClassRef: platformv1alpha1.LocalObjectReference{Name: "postgresql"},
			DisplayName:     "Standard HA",
			Topology:        "high-availability",
			DefaultVersion:  "16",
		},
		Status: platformv1alpha1.ServicePlanStatus{
			ObservedGeneration: 1,
			Phase:              "Draft",
			Published:          false,
			Conditions: []metav1.Condition{
				{Type: "Accepted", Status: metav1.ConditionTrue, ObservedGeneration: 1, Reason: "ValidationSucceeded", Message: "accepted"},
			},
		},
	}
	clusterTarget := &platformv1alpha1.ClusterTarget{
		ObjectMeta: metav1.ObjectMeta{Name: "local-dev"},
		Status: platformv1alpha1.ClusterTargetStatus{
			Reachable: true,
			Conditions: []metav1.Condition{
				{Type: "Ready", Status: metav1.ConditionTrue, Reason: "ClusterValidated", Message: "ready"},
			},
		},
	}
	instance := &platformv1alpha1.ServiceInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "orders-db"},
		Spec: platformv1alpha1.ServiceInstanceSpec{
			ProjectRef:      platformv1alpha1.LocalObjectReference{Name: "acme-prod"},
			ServiceClassRef: platformv1alpha1.LocalObjectReference{Name: "postgresql"},
			ServicePlanRef:  platformv1alpha1.LocalObjectReference{Name: "postgresql-ha"},
			Version:         "16",
			Exposure:        platformv1alpha1.ExposureSpec{Mode: platformv1alpha1.ExposureModeClusterInternal},
			SecretPolicy:    platformv1alpha1.SecretPolicySpec{DeliveryMode: platformv1alpha1.SecretDeliveryModeExternalSecret},
		},
	}

	reconciler := &ServiceInstanceReconciler{
		Client: fake.NewClientBuilder().
			WithScheme(scheme).
			WithStatusSubresource(&platformv1alpha1.ServiceInstance{}).
			WithObjects(tenant, project, serviceClass, servicePlan, clusterTarget, instance).
			Build(),
		Scheme:   scheme,
		Adapters: registry,
	}

	if _, err := reconciler.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKey{Name: "orders-db"}}); err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	var updated platformv1alpha1.ServiceInstance
	if err := reconciler.Get(context.Background(), client.ObjectKey{Name: "orders-db"}, &updated); err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if updated.Status.Phase != "Failed" {
		t.Fatalf("expected phase Failed, got %q", updated.Status.Phase)
	}
	if !isStatusConditionTrue(updated.Status.Conditions, "Failed") {
		t.Fatalf("expected Failed condition to be true")
	}
}

func TestServiceInstanceReconcilerMaterializesCNPGArtifacts(t *testing.T) {
	scheme := inventoryTestScheme(t)
	registry, err := adapters.NewRegistry(adapters.NewCNPGAdapter())
	if err != nil {
		t.Fatalf("NewRegistry returned error: %v", err)
	}
	tenant := &platformv1alpha1.Tenant{
		ObjectMeta: metav1.ObjectMeta{Name: "acme"},
		Spec: platformv1alpha1.TenantSpec{
			DisplayName:           "Acme Corp",
			Owners:                platformv1alpha1.OwnersSpec{Users: []string{"alice@example.com"}},
			QuotaProfileRef:       platformv1alpha1.LocalObjectReference{Name: "standard-tenant"},
			AllowedServiceClasses: []string{"postgresql"},
			Lifecycle:             platformv1alpha1.TenantLifecycleSpec{Phase: platformv1alpha1.TenantLifecyclePhaseActive},
		},
	}
	project := &platformv1alpha1.Project{
		ObjectMeta: metav1.ObjectMeta{Name: "acme-prod", Generation: 1},
		Spec: platformv1alpha1.ProjectSpec{
			TenantRef:         platformv1alpha1.LocalObjectReference{Name: "acme"},
			DisplayName:       "Acme Production",
			Environment:       platformv1alpha1.EnvironmentProduction,
			TargetSelector:    platformv1alpha1.TargetSelectorSpec{ClusterRef: &platformv1alpha1.LocalObjectReference{Name: "local-dev"}},
			NamespaceStrategy: platformv1alpha1.NamespaceStrategySpec{Mode: platformv1alpha1.NamespaceStrategyDedicated, Prefix: "acme-prod"},
		},
		Status: platformv1alpha1.ProjectStatus{
			ObservedGeneration: 1,
			Phase:              "Ready",
			Placement:          platformv1alpha1.PlacementStatus{ClusterName: "local-dev"},
			Conditions: []metav1.Condition{
				{Type: "Ready", Status: metav1.ConditionTrue, ObservedGeneration: 1, Reason: "ProjectReady", Message: "ready"},
			},
		},
	}
	serviceClass := &platformv1alpha1.ServiceClass{
		ObjectMeta: metav1.ObjectMeta{Name: "postgresql", Generation: 1},
		Spec: platformv1alpha1.ServiceClassSpec{
			DisplayName:           "PostgreSQL",
			Driver:                "cnpg",
			SupportedVersions:     []string{"16"},
			AllowsVersionOverride: true,
			Published:             true,
		},
		Status: platformv1alpha1.ServiceClassStatus{
			ObservedGeneration: 1,
			Phase:              "Ready",
			Published:          true,
			Conditions: []metav1.Condition{
				{Type: "Accepted", Status: metav1.ConditionTrue, ObservedGeneration: 1, Reason: "ValidationSucceeded", Message: "accepted"},
			},
		},
	}
	servicePlan := &platformv1alpha1.ServicePlan{
		ObjectMeta: metav1.ObjectMeta{Name: "postgresql-ha", Generation: 1},
		Spec: platformv1alpha1.ServicePlanSpec{
			ServiceClassRef: platformv1alpha1.LocalObjectReference{Name: "postgresql"},
			DisplayName:     "Standard HA",
			Topology:        "high-availability",
			DefaultVersion:  "16",
		},
		Status: platformv1alpha1.ServicePlanStatus{
			ObservedGeneration: 1,
			Phase:              "Ready",
			Published:          true,
			Conditions: []metav1.Condition{
				{Type: "Accepted", Status: metav1.ConditionTrue, ObservedGeneration: 1, Reason: "ValidationSucceeded", Message: "accepted"},
			},
		},
	}
	clusterTarget := &platformv1alpha1.ClusterTarget{
		ObjectMeta: metav1.ObjectMeta{Name: "local-dev"},
		Status: platformv1alpha1.ClusterTargetStatus{
			Reachable: true,
			Conditions: []metav1.Condition{
				{Type: "Ready", Status: metav1.ConditionTrue, Reason: "ClusterValidated", Message: "ready"},
			},
		},
	}
	instance := &platformv1alpha1.ServiceInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "orders-db"},
		Spec: platformv1alpha1.ServiceInstanceSpec{
			ProjectRef:      platformv1alpha1.LocalObjectReference{Name: "acme-prod"},
			ServiceClassRef: platformv1alpha1.LocalObjectReference{Name: "postgresql"},
			ServicePlanRef:  platformv1alpha1.LocalObjectReference{Name: "postgresql-ha"},
			Version:         "16",
			Exposure:        platformv1alpha1.ExposureSpec{Mode: platformv1alpha1.ExposureModeClusterInternal},
			SecretPolicy:    platformv1alpha1.SecretPolicySpec{DeliveryMode: platformv1alpha1.SecretDeliveryModeExternalSecret},
		},
	}
	deliveryRoot := t.TempDir()
	reconciler := &ServiceInstanceReconciler{
		Client: fake.NewClientBuilder().
			WithScheme(scheme).
			WithStatusSubresource(&platformv1alpha1.ServiceInstance{}).
			WithObjects(tenant, project, serviceClass, servicePlan, clusterTarget, instance).
			Build(),
		Scheme:       scheme,
		Adapters:     registry,
		Materializer: materializer.New(deliveryRoot),
	}

	if _, err := reconciler.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKey{Name: "orders-db"}}); err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	var updated platformv1alpha1.ServiceInstance
	if err := reconciler.Get(context.Background(), client.ObjectKey{Name: "orders-db"}, &updated); err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if updated.Status.Phase != "Materialized" {
		t.Fatalf("expected phase Materialized, got %q", updated.Status.Phase)
	}
	if !isStatusConditionTrue(updated.Status.Conditions, "Materialized") {
		t.Fatalf("expected Materialized condition to be true")
	}
	expectedPath := "clusters/local-dev/tenants/acme/projects/acme-prod/services/orders-db"
	if updated.Status.Artifact.Path != expectedPath {
		t.Fatalf("expected artifact path %q, got %q", expectedPath, updated.Status.Artifact.Path)
	}
	if updated.Status.Artifact.Revision == "" || updated.Status.Artifact.Revision == "local-render-preview" {
		t.Fatalf("expected real artifact revision, got %q", updated.Status.Artifact.Revision)
	}
	if updated.Status.Artifact.Count != 2 || len(updated.Status.Artifact.Artifacts) != 2 {
		t.Fatalf("expected 2 artifact records, got %#v", updated.Status.Artifact)
	}
	if updated.Status.Sync.Phase != string(adapters.SyncPhasePending) || updated.Status.Sync.ApplicationName != "acme-prod-orders-db" {
		t.Fatalf("unexpected sync status: %#v", updated.Status.Sync)
	}

	clusterPath := filepath.Join(deliveryRoot, expectedPath, "cnpg-cluster.yaml")
	content, err := os.ReadFile(clusterPath)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	if !strings.Contains(string(content), "kind: Cluster") || !strings.Contains(string(content), "postgresql.cnpg.io/v1") {
		t.Fatalf("expected CNPG cluster manifest, got:\n%s", string(content))
	}
}

func TestServiceInstanceReconcilerMaterializesValkeyArtifacts(t *testing.T) {
	scheme := inventoryTestScheme(t)
	registry, err := adapters.NewRegistry(adapters.NewCNPGAdapter(), adapters.NewValkeyAdapter())
	if err != nil {
		t.Fatalf("NewRegistry returned error: %v", err)
	}
	tenant := &platformv1alpha1.Tenant{
		ObjectMeta: metav1.ObjectMeta{Name: "acme"},
		Spec: platformv1alpha1.TenantSpec{
			DisplayName:           "Acme Corp",
			Owners:                platformv1alpha1.OwnersSpec{Users: []string{"alice@example.com"}},
			QuotaProfileRef:       platformv1alpha1.LocalObjectReference{Name: "standard-tenant"},
			AllowedServiceClasses: []string{"valkey"},
			Lifecycle:             platformv1alpha1.TenantLifecycleSpec{Phase: platformv1alpha1.TenantLifecyclePhaseActive},
		},
	}
	project := &platformv1alpha1.Project{
		ObjectMeta: metav1.ObjectMeta{Name: "acme-prod", Generation: 1},
		Spec: platformv1alpha1.ProjectSpec{
			TenantRef:         platformv1alpha1.LocalObjectReference{Name: "acme"},
			DisplayName:       "Acme Production",
			Environment:       platformv1alpha1.EnvironmentProduction,
			TargetSelector:    platformv1alpha1.TargetSelectorSpec{ClusterRef: &platformv1alpha1.LocalObjectReference{Name: "local-dev"}},
			NamespaceStrategy: platformv1alpha1.NamespaceStrategySpec{Mode: platformv1alpha1.NamespaceStrategyDedicated, Prefix: "acme-prod"},
		},
		Status: platformv1alpha1.ProjectStatus{
			ObservedGeneration: 1,
			Phase:              "Ready",
			Placement:          platformv1alpha1.PlacementStatus{ClusterName: "local-dev"},
			Conditions: []metav1.Condition{
				{Type: "Ready", Status: metav1.ConditionTrue, ObservedGeneration: 1, Reason: "ProjectReady", Message: "ready"},
			},
		},
	}
	serviceClass := &platformv1alpha1.ServiceClass{
		ObjectMeta: metav1.ObjectMeta{Name: "valkey", Generation: 1},
		Spec: platformv1alpha1.ServiceClassSpec{
			DisplayName:           "Valkey",
			Driver:                "servicer-valkey",
			SupportedVersions:     []string{"8.0"},
			AllowsVersionOverride: true,
			Published:             true,
		},
		Status: platformv1alpha1.ServiceClassStatus{
			ObservedGeneration: 1,
			Phase:              "Ready",
			Published:          true,
			Conditions: []metav1.Condition{
				{Type: "Accepted", Status: metav1.ConditionTrue, ObservedGeneration: 1, Reason: "ValidationSucceeded", Message: "accepted"},
			},
		},
	}
	servicePlan := &platformv1alpha1.ServicePlan{
		ObjectMeta: metav1.ObjectMeta{Name: "valkey-replicated", Generation: 1},
		Spec: platformv1alpha1.ServicePlanSpec{
			ServiceClassRef: platformv1alpha1.LocalObjectReference{Name: "valkey"},
			DisplayName:     "Replicated",
			Topology:        "replicated",
			DefaultVersion:  "8.0",
		},
		Status: platformv1alpha1.ServicePlanStatus{
			ObservedGeneration: 1,
			Phase:              "Ready",
			Published:          true,
			Conditions: []metav1.Condition{
				{Type: "Accepted", Status: metav1.ConditionTrue, ObservedGeneration: 1, Reason: "ValidationSucceeded", Message: "accepted"},
			},
		},
	}
	clusterTarget := &platformv1alpha1.ClusterTarget{
		ObjectMeta: metav1.ObjectMeta{Name: "local-dev"},
		Status: platformv1alpha1.ClusterTargetStatus{
			Reachable: true,
			Conditions: []metav1.Condition{
				{Type: "Ready", Status: metav1.ConditionTrue, Reason: "ClusterValidated", Message: "ready"},
			},
		},
	}
	instance := &platformv1alpha1.ServiceInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "session-cache"},
		Spec: platformv1alpha1.ServiceInstanceSpec{
			ProjectRef:      platformv1alpha1.LocalObjectReference{Name: "acme-prod"},
			ServiceClassRef: platformv1alpha1.LocalObjectReference{Name: "valkey"},
			ServicePlanRef:  platformv1alpha1.LocalObjectReference{Name: "valkey-replicated"},
			Version:         "8.0",
			Exposure:        platformv1alpha1.ExposureSpec{Mode: platformv1alpha1.ExposureModeClusterInternal},
			SecretPolicy:    platformv1alpha1.SecretPolicySpec{DeliveryMode: platformv1alpha1.SecretDeliveryModeExternalSecret},
		},
	}
	deliveryRoot := t.TempDir()
	reconciler := &ServiceInstanceReconciler{
		Client: fake.NewClientBuilder().
			WithScheme(scheme).
			WithStatusSubresource(&platformv1alpha1.ServiceInstance{}).
			WithObjects(tenant, project, serviceClass, servicePlan, clusterTarget, instance).
			Build(),
		Scheme:       scheme,
		Adapters:     registry,
		Materializer: materializer.New(deliveryRoot),
	}

	if _, err := reconciler.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKey{Name: "session-cache"}}); err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	var updated platformv1alpha1.ServiceInstance
	if err := reconciler.Get(context.Background(), client.ObjectKey{Name: "session-cache"}, &updated); err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if updated.Status.Phase != "Materialized" {
		t.Fatalf("expected phase Materialized, got %q", updated.Status.Phase)
	}
	if updated.Status.Runtime.Driver != "servicer-valkey" {
		t.Fatalf("expected runtime driver servicer-valkey, got %q", updated.Status.Runtime.Driver)
	}
	if updated.Status.Runtime.ObjectRef == nil || updated.Status.Runtime.ObjectRef.Kind != "StatefulSet" {
		t.Fatalf("expected StatefulSet runtime ref, got %#v", updated.Status.Runtime.ObjectRef)
	}

	expectedPath := "clusters/local-dev/tenants/acme/projects/acme-prod/services/session-cache"
	statefulSetPath := filepath.Join(deliveryRoot, expectedPath, "statefulset.yaml")
	content, err := os.ReadFile(statefulSetPath)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	rendered := string(content)
	if !strings.Contains(rendered, "kind: StatefulSet") || !strings.Contains(rendered, "replicas: 3") {
		t.Fatalf("expected replicated StatefulSet manifest, got:\n%s", rendered)
	}
	if strings.Contains(rendered, "ValkeyCluster") || strings.Contains(rendered, "valkey-operator") {
		t.Fatalf("Valkey materialization leaked operator-specific runtime details:\n%s", rendered)
	}

	var secret corev1.Secret
	if err := reconciler.Get(context.Background(), client.ObjectKey{Name: "session-cache-auth", Namespace: "acme-acme-prod-session-cache"}, &secret); err != nil {
		t.Fatalf("expected Valkey credential Secret to be generated: %v", err)
	}
	if len(secret.Data["password"]) == 0 {
		t.Fatalf("expected generated Valkey credential Secret to include password data")
	}
}

func TestServiceInstanceReconcilerObservesReadyValkeyRuntime(t *testing.T) {
	scheme := inventoryTestScheme(t)
	registry, err := adapters.NewRegistry(adapters.NewValkeyAdapter())
	if err != nil {
		t.Fatalf("NewRegistry returned error: %v", err)
	}
	fixture := valkeyInventoryFixture()
	statefulSet := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "session-cache",
			Namespace: "acme-acme-prod-session-cache",
			Labels: map[string]string{
				"servicer.io/service-instance": "session-cache",
				"servicer.io/runtime":          "valkey",
			},
		},
		Spec: appsv1.StatefulSetSpec{Replicas: int32PtrTest(3)},
		Status: appsv1.StatefulSetStatus{
			Replicas:        3,
			ReadyReplicas:   3,
			UpdatedReplicas: 3,
		},
	}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "session-cache-0",
			Namespace: "acme-acme-prod-session-cache",
			Labels: map[string]string{
				"servicer.io/service-instance": "session-cache",
				"servicer.io/runtime":          "valkey",
			},
		},
		Status: corev1.PodStatus{
			Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}},
		},
	}
	reconciler := &ServiceInstanceReconciler{
		Client: fake.NewClientBuilder().
			WithScheme(scheme).
			WithStatusSubresource(&platformv1alpha1.ServiceInstance{}).
			WithObjects(fixture.tenant, fixture.project, fixture.serviceClass, fixture.servicePlan, fixture.clusterTarget, fixture.instance, statefulSet, pod).
			Build(),
		Scheme:       scheme,
		Adapters:     registry,
		Materializer: materializer.New(t.TempDir()),
	}

	if _, err := reconciler.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKey{Name: "session-cache"}}); err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	var updated platformv1alpha1.ServiceInstance
	if err := reconciler.Get(context.Background(), client.ObjectKey{Name: "session-cache"}, &updated); err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if updated.Status.Phase != "Ready" {
		t.Fatalf("expected phase Ready, got %q", updated.Status.Phase)
	}
	if !isStatusConditionTrue(updated.Status.Conditions, "Ready") {
		t.Fatalf("expected Ready condition to be true")
	}
	if updated.Status.Sync.Phase != string(adapters.SyncPhaseSynced) {
		t.Fatalf("expected sync phase Synced after runtime observation, got %q", updated.Status.Sync.Phase)
	}
	if len(updated.Status.CredentialRefs) != 1 || updated.Status.CredentialRefs[0].Name != "session-cache-auth" {
		t.Fatalf("expected credential ref to be published, got %#v", updated.Status.CredentialRefs)
	}
}

func TestServiceInstanceReconcilerGeneratesYugabyteCredentialSecret(t *testing.T) {
	scheme := inventoryTestScheme(t)
	instance := &platformv1alpha1.ServiceInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "testy"},
		Spec: platformv1alpha1.ServiceInstanceSpec{
			SecretPolicy: platformv1alpha1.SecretPolicySpec{DeliveryMode: platformv1alpha1.SecretDeliveryModeExternalSecret},
		},
	}

	reconciler := &ServiceInstanceReconciler{
		Client: fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(instance).
			Build(),
		Scheme: scheme,
	}

	err := reconciler.ensureCredentialSecrets(context.Background(), instance, adapters.RenderResult{
		RuntimeDriver: "yb-operator",
		CredentialRefs: []platformv1alpha1.NamespacedObjectReference{
			{Name: "testy-credentials", Namespace: "acme-acme-prod-testy"},
		},
	})
	if err != nil {
		t.Fatalf("ensureCredentialSecrets returned error: %v", err)
	}

	var secret corev1.Secret
	if err := reconciler.Get(context.Background(), client.ObjectKey{Name: "testy-credentials", Namespace: "acme-acme-prod-testy"}, &secret); err != nil {
		t.Fatalf("expected generated Yugabyte credential Secret: %v", err)
	}
	if string(secret.Data["username"]) != "yugabyte" || len(secret.Data["password"]) == 0 {
		t.Fatalf("expected Yugabyte credential data, got %#v", secret.Data)
	}
	if string(secret.Data["database"]) != "testy" {
		t.Fatalf("expected default Yugabyte database name to derive from instance, got %#v", secret.Data)
	}
}

func TestServiceInstanceReconcilerGeneratesYugabyteCredentialSecretWithExplicitDatabaseName(t *testing.T) {
	scheme := inventoryTestScheme(t)
	instance := &platformv1alpha1.ServiceInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "team-db"},
		Spec: platformv1alpha1.ServiceInstanceSpec{
			Parameters:   rawJSON(t, map[string]any{"databaseName": "Reporting DB"}),
			SecretPolicy: platformv1alpha1.SecretPolicySpec{DeliveryMode: platformv1alpha1.SecretDeliveryModeExternalSecret},
		},
	}

	reconciler := &ServiceInstanceReconciler{
		Client: fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(instance).
			Build(),
		Scheme: scheme,
	}

	err := reconciler.ensureCredentialSecrets(context.Background(), instance, adapters.RenderResult{
		RuntimeDriver: "yb-operator",
		CredentialRefs: []platformv1alpha1.NamespacedObjectReference{
			{Name: "team-db-credentials", Namespace: "acme-acme-prod-team-db"},
		},
	})
	if err != nil {
		t.Fatalf("ensureCredentialSecrets returned error: %v", err)
	}

	var secret corev1.Secret
	if err := reconciler.Get(context.Background(), client.ObjectKey{Name: "team-db-credentials", Namespace: "acme-acme-prod-team-db"}, &secret); err != nil {
		t.Fatalf("expected generated Yugabyte credential Secret: %v", err)
	}
	if string(secret.Data["database"]) != "reporting_db" {
		t.Fatalf("expected normalized explicit database name in secret, got %#v", secret.Data)
	}
}

func TestServiceInstanceReconcilerGeneratesMySQLCredentialSecret(t *testing.T) {
	scheme := inventoryTestScheme(t)
	instance := &platformv1alpha1.ServiceInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "orders-mysql"},
		Spec: platformv1alpha1.ServiceInstanceSpec{
			Parameters:   rawJSON(t, map[string]any{"databaseName": "Orders DB"}),
			SecretPolicy: platformv1alpha1.SecretPolicySpec{DeliveryMode: platformv1alpha1.SecretDeliveryModeExternalSecret},
		},
	}
	reconciler := &ServiceInstanceReconciler{
		Client: fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(instance).
			Build(),
		Scheme: scheme,
	}

	err := reconciler.ensureCredentialSecrets(context.Background(), instance, adapters.RenderResult{
		RuntimeDriver: "servicer-mysql",
		CredentialRefs: []platformv1alpha1.NamespacedObjectReference{
			{Name: "orders-mysql-auth", Namespace: "acme-acme-prod-orders-mysql"},
		},
	})
	if err != nil {
		t.Fatalf("ensureCredentialSecrets returned error: %v", err)
	}

	var secret corev1.Secret
	if err := reconciler.Get(context.Background(), client.ObjectKey{Name: "orders-mysql-auth", Namespace: "acme-acme-prod-orders-mysql"}, &secret); err != nil {
		t.Fatalf("expected generated MySQL credential Secret: %v", err)
	}
	if string(secret.Data["username"]) != "orders_db" || string(secret.Data["database"]) != "orders_db" || len(secret.Data["password"]) == 0 {
		t.Fatalf("expected MySQL credential data, got %#v", secret.Data)
	}
}

func TestServiceInstanceReconcilerGeneratesNATSCredentialSecretsAndAuthConfig(t *testing.T) {
	scheme := inventoryTestScheme(t)
	instance := &platformv1alpha1.ServiceInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "session-bus"},
		Spec: platformv1alpha1.ServiceInstanceSpec{
			Parameters: rawJSON(t, map[string]any{
				"appCredentials": []map[string]any{
					{
						"name": "orders-api",
						"permissions": map[string]any{
							"publish":        []string{"orders.created"},
							"subscribe":      []string{"orders.>"},
							"allowResponses": true,
						},
					},
				},
			}),
			SecretPolicy: platformv1alpha1.SecretPolicySpec{DeliveryMode: platformv1alpha1.SecretDeliveryModeExternalSecret},
		},
	}

	reconciler := &ServiceInstanceReconciler{
		Client: fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(instance).
			Build(),
		Scheme: scheme,
	}

	if err := reconciler.ensureNATSCredentialSecrets(context.Background(), instance, "acme-acme-prod-session-bus"); err != nil {
		t.Fatalf("ensureNATSCredentialSecrets returned error: %v", err)
	}

	var admin corev1.Secret
	if err := reconciler.Get(context.Background(), client.ObjectKey{Name: "session-bus-auth", Namespace: "acme-acme-prod-session-bus"}, &admin); err != nil {
		t.Fatalf("expected admin NATS credential secret: %v", err)
	}
	var app corev1.Secret
	if err := reconciler.Get(context.Background(), client.ObjectKey{Name: "session-bus-orders-api-auth", Namespace: "acme-acme-prod-session-bus"}, &app); err != nil {
		t.Fatalf("expected app NATS credential secret: %v", err)
	}
	if string(app.Data["username"]) != "orders-api" {
		t.Fatalf("expected app username orders-api, got %#v", app.Data)
	}
	var authConfig corev1.Secret
	if err := reconciler.Get(context.Background(), client.ObjectKey{Name: "session-bus-auth-config", Namespace: "acme-acme-prod-session-bus"}, &authConfig); err != nil {
		t.Fatalf("expected NATS auth config secret: %v", err)
	}
	usersConfig := string(authConfig.Data["users.conf"])
	for _, expected := range []string{"orders-api", "orders.created", "orders.>", "servicer"} {
		if !strings.Contains(usersConfig, expected) {
			t.Fatalf("expected users.conf to contain %q, got:\n%s", expected, usersConfig)
		}
	}
}

func inventoryTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme returned error: %v", err)
	}
	if err := platformv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme returned error: %v", err)
	}
	return scheme
}

func rawJSON(t *testing.T, value any) *apiextensionsv1.JSON {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal json: %v", err)
	}
	return &apiextensionsv1.JSON{Raw: raw}
}

type valkeyFixture struct {
	tenant        *platformv1alpha1.Tenant
	project       *platformv1alpha1.Project
	serviceClass  *platformv1alpha1.ServiceClass
	servicePlan   *platformv1alpha1.ServicePlan
	clusterTarget *platformv1alpha1.ClusterTarget
	instance      *platformv1alpha1.ServiceInstance
}

func valkeyInventoryFixture() valkeyFixture {
	return valkeyFixture{
		tenant: &platformv1alpha1.Tenant{
			ObjectMeta: metav1.ObjectMeta{Name: "acme"},
			Spec: platformv1alpha1.TenantSpec{
				DisplayName:           "Acme Corp",
				Owners:                platformv1alpha1.OwnersSpec{Users: []string{"alice@example.com"}},
				QuotaProfileRef:       platformv1alpha1.LocalObjectReference{Name: "standard-tenant"},
				AllowedServiceClasses: []string{"valkey"},
				Lifecycle:             platformv1alpha1.TenantLifecycleSpec{Phase: platformv1alpha1.TenantLifecyclePhaseActive},
			},
		},
		project: &platformv1alpha1.Project{
			ObjectMeta: metav1.ObjectMeta{Name: "acme-prod", Generation: 1},
			Spec: platformv1alpha1.ProjectSpec{
				TenantRef:         platformv1alpha1.LocalObjectReference{Name: "acme"},
				DisplayName:       "Acme Production",
				Environment:       platformv1alpha1.EnvironmentProduction,
				TargetSelector:    platformv1alpha1.TargetSelectorSpec{ClusterRef: &platformv1alpha1.LocalObjectReference{Name: "local-dev"}},
				NamespaceStrategy: platformv1alpha1.NamespaceStrategySpec{Mode: platformv1alpha1.NamespaceStrategyDedicated, Prefix: "acme-prod"},
			},
			Status: platformv1alpha1.ProjectStatus{
				ObservedGeneration: 1,
				Phase:              "Ready",
				Placement:          platformv1alpha1.PlacementStatus{ClusterName: "local-dev"},
				Conditions: []metav1.Condition{
					{Type: "Ready", Status: metav1.ConditionTrue, ObservedGeneration: 1, Reason: "ProjectReady", Message: "ready"},
				},
			},
		},
		serviceClass: &platformv1alpha1.ServiceClass{
			ObjectMeta: metav1.ObjectMeta{Name: "valkey", Generation: 1},
			Spec: platformv1alpha1.ServiceClassSpec{
				DisplayName:           "Valkey",
				Driver:                "servicer-valkey",
				SupportedVersions:     []string{"8.0"},
				AllowsVersionOverride: true,
				Published:             true,
			},
			Status: platformv1alpha1.ServiceClassStatus{
				ObservedGeneration: 1,
				Phase:              "Ready",
				Published:          true,
				Conditions: []metav1.Condition{
					{Type: "Accepted", Status: metav1.ConditionTrue, ObservedGeneration: 1, Reason: "ValidationSucceeded", Message: "accepted"},
				},
			},
		},
		servicePlan: &platformv1alpha1.ServicePlan{
			ObjectMeta: metav1.ObjectMeta{Name: "valkey-replicated", Generation: 1},
			Spec: platformv1alpha1.ServicePlanSpec{
				ServiceClassRef: platformv1alpha1.LocalObjectReference{Name: "valkey"},
				DisplayName:     "Replicated",
				Topology:        "replicated",
				DefaultVersion:  "8.0",
			},
			Status: platformv1alpha1.ServicePlanStatus{
				ObservedGeneration: 1,
				Phase:              "Ready",
				Published:          true,
				Conditions: []metav1.Condition{
					{Type: "Accepted", Status: metav1.ConditionTrue, ObservedGeneration: 1, Reason: "ValidationSucceeded", Message: "accepted"},
				},
			},
		},
		clusterTarget: &platformv1alpha1.ClusterTarget{
			ObjectMeta: metav1.ObjectMeta{Name: "local-dev"},
			Status: platformv1alpha1.ClusterTargetStatus{
				Reachable: true,
				Conditions: []metav1.Condition{
					{Type: "Ready", Status: metav1.ConditionTrue, Reason: "ClusterValidated", Message: "ready"},
				},
			},
		},
		instance: &platformv1alpha1.ServiceInstance{
			ObjectMeta: metav1.ObjectMeta{Name: "session-cache"},
			Spec: platformv1alpha1.ServiceInstanceSpec{
				ProjectRef:      platformv1alpha1.LocalObjectReference{Name: "acme-prod"},
				ServiceClassRef: platformv1alpha1.LocalObjectReference{Name: "valkey"},
				ServicePlanRef:  platformv1alpha1.LocalObjectReference{Name: "valkey-replicated"},
				Version:         "8.0",
				Exposure:        platformv1alpha1.ExposureSpec{Mode: platformv1alpha1.ExposureModeClusterInternal},
				SecretPolicy:    platformv1alpha1.SecretPolicySpec{DeliveryMode: platformv1alpha1.SecretDeliveryModeExternalSecret},
			},
		},
	}
}

func int32PtrTest(value int32) *int32 {
	return &value
}
