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
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
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
		Data: map[string][]byte{"kubeconfig": []byte(`apiVersion: v1
kind: Config
clusters:
- cluster:
    server: https://local-dev.example.invalid
  name: local-dev
contexts:
- context:
    cluster: local-dev
    user: local-dev
  name: local-dev
current-context: local-dev
users:
- name: local-dev
  user:
    token: demo-token
`)},
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

func TestClusterTargetReconcilerAcceptsValueKeyConnectionSecret(t *testing.T) {
	scheme := inventoryTestScheme(t)
	clusterTarget := &platformv1alpha1.ClusterTarget{
		ObjectMeta: metav1.ObjectMeta{Name: "local-dev"},
		Spec: platformv1alpha1.ClusterTargetSpec{
			DisplayName:   "Local Development",
			ConnectionRef: platformv1alpha1.NamespacedObjectReference{Name: "local-dev-kubeconfig", Namespace: "servicer-system"},
		},
	}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "local-dev-kubeconfig", Namespace: "servicer-system"},
		Data: map[string][]byte{"value": []byte(`apiVersion: v1
kind: Config
clusters:
- cluster:
    server: https://local-dev.example.invalid
  name: local-dev
contexts:
- context:
    cluster: local-dev
    user: local-dev
  name: local-dev
current-context: local-dev
users:
- name: local-dev
  user:
    token: demo-token
`)},
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
}

func TestClusterTargetReconcilerMapsConnectionSecretToTarget(t *testing.T) {
	scheme := inventoryTestScheme(t)
	clusterTarget := &platformv1alpha1.ClusterTarget{
		ObjectMeta: metav1.ObjectMeta{Name: "local-dev"},
		Spec: platformv1alpha1.ClusterTargetSpec{
			ConnectionRef: platformv1alpha1.NamespacedObjectReference{Name: "local-dev-kubeconfig", Namespace: "servicer-system"},
		},
	}
	otherTarget := &platformv1alpha1.ClusterTarget{
		ObjectMeta: metav1.ObjectMeta{Name: "other"},
		Spec: platformv1alpha1.ClusterTargetSpec{
			ConnectionRef: platformv1alpha1.NamespacedObjectReference{Name: "other-kubeconfig", Namespace: "servicer-system"},
		},
	}
	reconciler := &ClusterTargetReconciler{
		Client: fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(clusterTarget, otherTarget).
			Build(),
		Scheme: scheme,
	}

	requests := reconciler.clusterTargetsForConnectionSecret(context.Background(), &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "local-dev-kubeconfig", Namespace: "servicer-system"},
	})
	if len(requests) != 1 {
		t.Fatalf("expected 1 reconcile request, got %#v", requests)
	}
	if requests[0].NamespacedName.Name != "local-dev" {
		t.Fatalf("expected request for local-dev, got %#v", requests[0])
	}
}

func TestEffectiveRequiredPackagesForClusterTargetIncludesPublishedServiceClasses(t *testing.T) {
	scheme := inventoryTestScheme(t)
	target := &platformv1alpha1.ClusterTarget{
		ObjectMeta: metav1.ObjectMeta{Name: "local-dev"},
		Spec: platformv1alpha1.ClusterTargetSpec{
			RequiredPackages: []string{"external-secrets"},
		},
	}
	published := &platformv1alpha1.ServiceClass{
		ObjectMeta: metav1.ObjectMeta{Name: "yugabyte"},
		Spec: platformv1alpha1.ServiceClassSpec{
			DisplayName:      "YugabyteDB",
			Driver:           "yb-operator",
			Published:        true,
			RequiredPackages: []string{"yugabyte"},
		},
	}
	draft := &platformv1alpha1.ServiceClass{
		ObjectMeta: metav1.ObjectMeta{Name: "cassandra"},
		Spec: platformv1alpha1.ServiceClassSpec{
			DisplayName:      "Cassandra",
			Driver:           "k8ssandra",
			Published:        false,
			RequiredPackages: []string{"k8ssandra"},
		},
	}

	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(target, published, draft).
		Build()

	packages, err := effectiveRequiredPackagesForClusterTarget(context.Background(), k8sClient, target)
	if err != nil {
		t.Fatalf("effectiveRequiredPackagesForClusterTarget returned error: %v", err)
	}
	if strings.Join(packages, ",") != "external-secrets,yugabyte" {
		t.Fatalf("unexpected package set %#v", packages)
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

func TestProjectReconcilerAppliesTenantQuotaProfile(t *testing.T) {
	scheme := inventoryTestScheme(t)
	project := &platformv1alpha1.Project{
		ObjectMeta: metav1.ObjectMeta{Name: "acme-prod"},
		Spec: platformv1alpha1.ProjectSpec{
			TenantRef:         platformv1alpha1.LocalObjectReference{Name: "acme"},
			DisplayName:       "Acme Production",
			Environment:       platformv1alpha1.EnvironmentProduction,
			TargetSelector:    platformv1alpha1.TargetSelectorSpec{ClusterRef: &platformv1alpha1.LocalObjectReference{Name: "local-dev"}},
			NamespaceStrategy: platformv1alpha1.NamespaceStrategySpec{Mode: platformv1alpha1.NamespaceStrategyDedicated, Prefix: "acme-prod"},
			Quotas:            platformv1alpha1.ProjectQuotasSpec{MaxNamespaces: int32PtrTest(4)},
		},
	}
	tenant := &platformv1alpha1.Tenant{
		ObjectMeta: metav1.ObjectMeta{Name: "acme"},
		Spec: platformv1alpha1.TenantSpec{
			DisplayName:           "Acme Corp",
			Owners:                platformv1alpha1.OwnersSpec{Users: []string{"alice@example.com"}},
			QuotaProfileRef:       platformv1alpha1.LocalObjectReference{Name: "tiny"},
			AllowedServiceClasses: []string{"postgresql"},
			Lifecycle:             platformv1alpha1.TenantLifecycleSpec{Phase: platformv1alpha1.TenantLifecyclePhaseActive},
		},
	}
	clusterTarget := &platformv1alpha1.ClusterTarget{
		ObjectMeta: metav1.ObjectMeta{Name: "local-dev", Generation: 1},
		Status: platformv1alpha1.ClusterTargetStatus{
			ObservedGeneration: 1,
			Reachable:          true,
			Conditions: []metav1.Condition{
				{Type: "Ready", Status: metav1.ConditionTrue, ObservedGeneration: 1, Reason: "ClusterValidated", Message: "ready"},
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
	if updated.Status.EffectiveQuota.MaxServices == nil || *updated.Status.EffectiveQuota.MaxServices != 1 {
		t.Fatalf("expected inherited maxServices 1, got %#v", updated.Status.EffectiveQuota)
	}
	if updated.Status.EffectiveQuota.MaxNamespaces == nil || *updated.Status.EffectiveQuota.MaxNamespaces != 4 {
		t.Fatalf("expected project maxNamespaces override 4, got %#v", updated.Status.EffectiveQuota)
	}
}

func TestServiceInstanceReconcilerRejectsInheritedQuotaExceeded(t *testing.T) {
	scheme := inventoryTestScheme(t)
	registry, err := adapters.NewRegistry(adapters.NewValkeyAdapter())
	if err != nil {
		t.Fatalf("NewRegistry returned error: %v", err)
	}
	fixture := valkeyInventoryFixture()
	fixture.tenant.Spec.QuotaProfileRef = platformv1alpha1.LocalObjectReference{Name: "tiny"}
	fixture.project.Status.EffectiveQuota = platformv1alpha1.ProjectQuotasSpec{}
	existing := fixture.instance.DeepCopy()
	existing.Name = "existing-cache"
	existing.Status.Placement.Namespace = "acme-prod-existing-cache"
	reconciler := &ServiceInstanceReconciler{
		Client: fake.NewClientBuilder().
			WithScheme(scheme).
			WithStatusSubresource(&platformv1alpha1.ServiceInstance{}).
			WithObjects(fixture.tenant, fixture.project, fixture.serviceClass, fixture.servicePlan, fixture.clusterTarget, existing, fixture.instance).
			Build(),
		Scheme:       scheme,
		Adapters:     registry,
		Materializer: materializer.New(t.TempDir()),
	}

	for i := 0; i < 2; i++ {
		if _, err := reconciler.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKey{Name: "session-cache"}}); err != nil {
			t.Fatalf("reconcile %d returned error: %v", i+1, err)
		}
	}

	var updated platformv1alpha1.ServiceInstance
	if err := reconciler.Get(context.Background(), client.ObjectKey{Name: "session-cache"}, &updated); err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if updated.Status.Phase != "Failed" {
		t.Fatalf("expected quota failure, got phase %q", updated.Status.Phase)
	}
	failed := statusConditionByType(updated.Status.Conditions, "Failed")
	if failed == nil || !strings.Contains(failed.Message, "allows at most 1 service instances") {
		t.Fatalf("expected inherited quota message, got %#v", updated.Status.Conditions)
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
			Packages: []platformv1alpha1.PackageStatus{
				{Name: externalSecretsOperatorPackageName, Phase: platformv1alpha1.PackagePhaseInstalled, Message: "All CRD probes passed."},
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

	for i := 0; i < 3; i++ {
		if _, err := reconciler.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKey{Name: "orders-db"}}); err != nil {
			t.Fatalf("reconcile %d returned error: %v", i+1, err)
		}
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

func TestServiceInstanceReconcilerRejectsTenantPolicyViolation(t *testing.T) {
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
			PolicyRefs:            []platformv1alpha1.PolicyReference{{Name: "deny-public-ingress"}},
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
			Packages: []platformv1alpha1.PackageStatus{
				{Name: externalSecretsOperatorPackageName, Phase: platformv1alpha1.PackagePhaseInstalled, Message: "All CRD probes passed."},
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
			Exposure:        platformv1alpha1.ExposureSpec{Mode: platformv1alpha1.ExposureModePublicIngress},
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
	for i := 0; i < 2; i++ {
		if _, err := reconciler.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKey{Name: "orders-db"}}); err != nil {
			t.Fatalf("Reconcile returned error: %v", err)
		}
	}
	var updated platformv1alpha1.ServiceInstance
	if err := reconciler.Get(context.Background(), client.ObjectKey{Name: "orders-db"}, &updated); err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if updated.Status.Phase != "Failed" {
		t.Fatalf("expected phase Failed, got %q", updated.Status.Phase)
	}
}

func TestServiceInstanceReconcilerWaitsForExternalSecretsOperatorPackage(t *testing.T) {
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
		Spec: platformv1alpha1.ClusterTargetSpec{
			RequiredPackages: []string{externalSecretsOperatorPackageName},
		},
		Status: platformv1alpha1.ClusterTargetStatus{
			Reachable: true,
			Conditions: []metav1.Condition{
				{Type: "Ready", Status: metav1.ConditionTrue, Reason: "ClusterValidated", Message: "ready"},
			},
			Packages: []platformv1alpha1.PackageStatus{{
				Name:    externalSecretsOperatorPackageName,
				Phase:   platformv1alpha1.PackagePhaseDeploying,
				Message: "Manifests applied; waiting for operator to become ready.",
			}},
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

	for i := 0; i < 2; i++ {
		if _, err := reconciler.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKey{Name: "orders-db"}}); err != nil {
			t.Fatalf("reconcile %d returned error: %v", i+1, err)
		}
	}

	var updated platformv1alpha1.ServiceInstance
	if err := reconciler.Get(context.Background(), client.ObjectKey{Name: "orders-db"}, &updated); err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if updated.Status.Phase != "PendingDependencies" {
		t.Fatalf("expected phase PendingDependencies, got %q", updated.Status.Phase)
	}
	if isStatusConditionTrue(updated.Status.Conditions, "Ready") {
		t.Fatalf("expected Ready condition to remain false while package is deploying")
	}
	if !conditionMessagesContain(updated.Status.Conditions, "external-secrets") {
		t.Fatalf("expected dependency message to mention external-secrets, got %#v", updated.Status.Conditions)
	}
}

func TestServiceInstanceReconcilerWaitsForServiceClassOperatorPackage(t *testing.T) {
	scheme := inventoryTestScheme(t)
	registry, err := adapters.NewRegistry(adapters.NewYugabyteAdapter())
	if err != nil {
		t.Fatalf("NewRegistry returned error: %v", err)
	}
	tenant := &platformv1alpha1.Tenant{
		ObjectMeta: metav1.ObjectMeta{Name: "acme"},
		Spec: platformv1alpha1.TenantSpec{
			DisplayName:           "Acme Corp",
			Owners:                platformv1alpha1.OwnersSpec{Users: []string{"alice@example.com"}},
			QuotaProfileRef:       platformv1alpha1.LocalObjectReference{Name: "standard-tenant"},
			AllowedServiceClasses: []string{"yugabyte"},
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
		ObjectMeta: metav1.ObjectMeta{Name: "yugabyte", Generation: 1},
		Spec: platformv1alpha1.ServiceClassSpec{
			DisplayName:      "YugabyteDB",
			Driver:           "yb-operator",
			Published:        true,
			RequiredPackages: []string{"yugabyte"},
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
		ObjectMeta: metav1.ObjectMeta{Name: "yugabyte-dev", Generation: 1},
		Spec: platformv1alpha1.ServicePlanSpec{
			ServiceClassRef: platformv1alpha1.LocalObjectReference{Name: "yugabyte"},
			DisplayName:     "Development",
			Topology:        "single-cluster",
			DefaultVersion:  "2.20.1.3-b3",
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
			Packages: []platformv1alpha1.PackageStatus{{
				Name:    "yugabyte",
				Phase:   platformv1alpha1.PackagePhaseDeploying,
				Message: "Manifests applied; waiting for operator to become ready.",
			}},
		},
	}
	instance := &platformv1alpha1.ServiceInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "ydb"},
		Spec: platformv1alpha1.ServiceInstanceSpec{
			ProjectRef:      platformv1alpha1.LocalObjectReference{Name: "acme-prod"},
			ServiceClassRef: platformv1alpha1.LocalObjectReference{Name: "yugabyte"},
			ServicePlanRef:  platformv1alpha1.LocalObjectReference{Name: "yugabyte-dev"},
			Version:         "2.20.1.3-b3",
			Exposure:        platformv1alpha1.ExposureSpec{Mode: platformv1alpha1.ExposureModeClusterInternal},
			SecretPolicy:    platformv1alpha1.SecretPolicySpec{DeliveryMode: platformv1alpha1.SecretDeliveryModeManual},
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

	for i := 0; i < 2; i++ {
		if _, err := reconciler.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKey{Name: "ydb"}}); err != nil {
			t.Fatalf("reconcile %d returned error: %v", i+1, err)
		}
	}

	var updated platformv1alpha1.ServiceInstance
	if err := reconciler.Get(context.Background(), client.ObjectKey{Name: "ydb"}, &updated); err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if updated.Status.Phase != "PendingDependencies" {
		t.Fatalf("expected phase PendingDependencies, got %q", updated.Status.Phase)
	}
	if !conditionMessagesContain(updated.Status.Conditions, "yugabyte") {
		t.Fatalf("expected dependency message to mention yugabyte, got %#v", updated.Status.Conditions)
	}
}

func TestPolicyReconcilerMarksUserDefinedPolicyReady(t *testing.T) {
	scheme := inventoryTestScheme(t)
	policy := &platformv1alpha1.Policy{
		ObjectMeta: metav1.ObjectMeta{Name: "require-private-ingress"},
		Spec: platformv1alpha1.PolicySpec{
			DisplayName: "Require Private Ingress",
			TargetKinds: []platformv1alpha1.PolicyTargetKind{platformv1alpha1.PolicyTargetServiceInstance},
			Rules: []platformv1alpha1.PolicyRule{{
				Name:     "forbid-public",
				Path:     "spec.exposure.mode",
				Operator: platformv1alpha1.PolicyOperatorEquals,
				Value:    "public-ingress",
				Message:  "public ingress is not allowed",
			}},
		},
	}
	reconciler := &PolicyReconciler{
		Client: fake.NewClientBuilder().
			WithScheme(scheme).
			WithStatusSubresource(&platformv1alpha1.Policy{}).
			WithObjects(policy).
			Build(),
		Scheme: scheme,
	}

	if _, err := reconciler.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKey{Name: "require-private-ingress"}}); err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	var updated platformv1alpha1.Policy
	if err := reconciler.Get(context.Background(), client.ObjectKey{Name: "require-private-ingress"}, &updated); err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if updated.Status.Phase != "Ready" {
		t.Fatalf("expected phase Ready, got %q", updated.Status.Phase)
	}
	if updated.Status.RuleCount != 1 {
		t.Fatalf("expected one accepted rule, got %#v", updated.Status)
	}
}

func TestServiceInstanceReconcilerRejectsCustomPolicyViolation(t *testing.T) {
	scheme := inventoryTestScheme(t)
	registry, err := adapters.NewRegistry(adapters.NewCNPGAdapter())
	if err != nil {
		t.Fatalf("NewRegistry returned error: %v", err)
	}
	policy := &platformv1alpha1.Policy{
		ObjectMeta: metav1.ObjectMeta{Name: "require-backup-profile"},
		Spec: platformv1alpha1.PolicySpec{
			DisplayName: "Require backup profile",
			TargetKinds: []platformv1alpha1.PolicyTargetKind{platformv1alpha1.PolicyTargetServiceInstance},
			Rules: []platformv1alpha1.PolicyRule{{
				Name:     "backup-profile-present",
				Path:     "parameters.backupProfile",
				Operator: platformv1alpha1.PolicyOperatorEmpty,
				Message:  "backupProfile must be set by policy",
			}},
		},
	}
	tenant := &platformv1alpha1.Tenant{
		ObjectMeta: metav1.ObjectMeta{Name: "acme"},
		Spec: platformv1alpha1.TenantSpec{
			DisplayName:           "Acme Corp",
			Owners:                platformv1alpha1.OwnersSpec{Users: []string{"alice@example.com"}},
			QuotaProfileRef:       platformv1alpha1.LocalObjectReference{Name: "standard-tenant"},
			AllowedServiceClasses: []string{"postgresql"},
			PolicyRefs:            []platformv1alpha1.PolicyReference{{Name: "require-backup-profile"}},
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
			Packages: []platformv1alpha1.PackageStatus{
				{Name: externalSecretsOperatorPackageName, Phase: platformv1alpha1.PackagePhaseInstalled, Message: "All CRD probes passed."},
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
			WithObjects(policy, tenant, project, serviceClass, servicePlan, clusterTarget, instance).
			Build(),
		Scheme:   scheme,
		Adapters: registry,
	}
	for i := 0; i < 2; i++ {
		if _, err := reconciler.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKey{Name: "orders-db"}}); err != nil {
			t.Fatalf("Reconcile returned error: %v", err)
		}
	}
	var updated platformv1alpha1.ServiceInstance
	if err := reconciler.Get(context.Background(), client.ObjectKey{Name: "orders-db"}, &updated); err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if updated.Status.Phase != "Failed" {
		t.Fatalf("expected phase Failed, got %q", updated.Status.Phase)
	}
}

func TestServiceInstanceReconcilerRejectsPlanConstraintViolation(t *testing.T) {
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
			DefaultVersion:  "16",
			Constraints:     rawJSON(t, map[string]any{"requiredSecretDeliveryMode": "external-secret", "maxReplicas": 2}),
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
			SecretPolicy:    platformv1alpha1.SecretPolicySpec{DeliveryMode: platformv1alpha1.SecretDeliveryModeDirectSecretRef},
			Parameters:      rawJSON(t, map[string]any{"replicas": 3}),
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
	for i := 0; i < 2; i++ {
		if _, err := reconciler.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKey{Name: "orders-db"}}); err != nil {
			t.Fatalf("Reconcile returned error: %v", err)
		}
	}
	var updated platformv1alpha1.ServiceInstance
	if err := reconciler.Get(context.Background(), client.ObjectKey{Name: "orders-db"}, &updated); err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if updated.Status.Phase != "Failed" {
		t.Fatalf("expected phase Failed, got %q", updated.Status.Phase)
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
			RequiredPackages:      []string{"cnpg"},
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
			Packages: []platformv1alpha1.PackageStatus{
				{Name: "cnpg", Phase: platformv1alpha1.PackagePhaseInstalled, Message: "All CRD probes passed."},
				{Name: externalSecretsOperatorPackageName, Phase: platformv1alpha1.PackagePhaseInstalled, Message: "All CRD probes passed."},
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

	for i := 0; i < 2; i++ {
		if _, err := reconciler.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKey{Name: "orders-db"}}); err != nil {
			t.Fatalf("reconcile %d returned error: %v", i+1, err)
		}
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
	if updated.Status.Artifact.Count != 7 || len(updated.Status.Artifact.Artifacts) != 7 {
		t.Fatalf("expected 7 artifact records including external-secret projection, got %#v", updated.Status.Artifact)
	}
	if updated.Status.Sync.Phase != string(adapters.SyncPhaseMaterialized) || updated.Status.Sync.ApplicationName != "acme-prod-orders-db" {
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

func TestServiceInstanceReconcilerEnsureArgoApplicationWhenConfigured(t *testing.T) {
	scheme := inventoryTestScheme(t)
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
			Packages: []platformv1alpha1.PackageStatus{
				{Name: externalSecretsOperatorPackageName, Phase: platformv1alpha1.PackagePhaseInstalled, Message: "All CRD probes passed."},
			},
		},
	}
	appCRD := &unstructured.Unstructured{}
	appCRD.SetGroupVersionKind(schema.GroupVersionKind{Group: "apiextensions.k8s.io", Version: "v1", Kind: "CustomResourceDefinition"})
	appCRD.SetName("applications.argoproj.io")
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
			WithObjects(tenant, project, serviceClass, servicePlan, clusterTarget, appCRD, instance).
			Build(),
		Scheme:           scheme,
		Materializer:     materializer.New(deliveryRoot),
		ArgoCDNamespace:  "argocd",
		ArgoCDProject:    "default",
		DeliveryRepoURL:  "https://github.com/example/servicer-config.git",
		DeliveryRepoRef:  "main",
		DeliveryRepoPath: materializer.DefaultRoot,
	}

	instance.Status.Placement.ClusterName = "local-dev"
	instance.Status.Placement.Namespace = "acme-prod-orders-db"
	instance.Status.Sync.ApplicationName = "acme-prod-orders-db"
	if err := reconciler.ensureArgoApplication(context.Background(), project, instance, "clusters/local-dev/tenants/acme/projects/acme-prod/services/orders-db", nil); err != nil {
		t.Fatalf("ensureArgoApplication returned error: %v", err)
	}

	app := &unstructured.Unstructured{}
	app.SetGroupVersionKind(schema.GroupVersionKind{Group: "argoproj.io", Version: "v1alpha1", Kind: "Application"})
	if err := reconciler.Get(context.Background(), client.ObjectKey{Name: "acme-prod-orders-db", Namespace: "argocd"}, app); err != nil {
		t.Fatalf("expected Argo application to be created: %v", err)
	}
	path, _, _ := unstructured.NestedString(app.Object, "spec", "source", "path")
	repoURL, _, _ := unstructured.NestedString(app.Object, "spec", "source", "repoURL")
	destName, _, _ := unstructured.NestedString(app.Object, "spec", "destination", "name")
	if repoURL != "https://github.com/example/servicer-config.git" {
		t.Fatalf("unexpected repoURL %q", repoURL)
	}
	if path != "generated/delivery/clusters/local-dev/tenants/acme/projects/acme-prod/services/orders-db" {
		t.Fatalf("unexpected app source path %q", path)
	}
	if destName != "local-dev" {
		t.Fatalf("unexpected destination name %q", destName)
	}
}

func TestServiceInstanceReconcilerEnsureArgoApplicationSetWhenMultiplePackagePaths(t *testing.T) {
	scheme := inventoryTestScheme(t)
	project := &platformv1alpha1.Project{ObjectMeta: metav1.ObjectMeta{Name: "acme-prod"}}
	instance := &platformv1alpha1.ServiceInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "session-cache"},
		Status: platformv1alpha1.ServiceInstanceStatus{
			Placement: platformv1alpha1.PlacementStatus{Namespace: "acme-prod-session-cache"},
			Sync:      platformv1alpha1.DeliverySyncStatus{ApplicationName: "acme-prod-session-cache"},
		},
	}
	appCRD := &unstructured.Unstructured{}
	appCRD.SetGroupVersionKind(schema.GroupVersionKind{Group: "apiextensions.k8s.io", Version: "v1", Kind: "CustomResourceDefinition"})
	appCRD.SetName("applications.argoproj.io")
	appSetCRD := &unstructured.Unstructured{}
	appSetCRD.SetGroupVersionKind(schema.GroupVersionKind{Group: "apiextensions.k8s.io", Version: "v1", Kind: "CustomResourceDefinition"})
	appSetCRD.SetName("applicationsets.argoproj.io")
	reconciler := &ServiceInstanceReconciler{
		Client: fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(project, instance, appCRD, appSetCRD).
			Build(),
		Scheme:           scheme,
		ArgoCDNamespace:  "argocd",
		ArgoCDProject:    "default",
		DeliveryRepoURL:  "https://github.com/example/servicer-config.git",
		DeliveryRepoRef:  "main",
		DeliveryRepoPath: materializer.DefaultRoot,
	}

	packagePaths := []string{
		"clusters/east-1/tenants/acme/projects/acme-prod/services/session-cache",
		"clusters/west-2/tenants/acme/projects/acme-prod/services/session-cache",
	}
	if err := reconciler.ensureArgoApplication(context.Background(), project, instance, packagePaths[0], packagePaths); err != nil {
		t.Fatalf("ensureArgoApplication returned error: %v", err)
	}

	appSet := &unstructured.Unstructured{}
	appSet.SetGroupVersionKind(schema.GroupVersionKind{Group: "argoproj.io", Version: "v1alpha1", Kind: "ApplicationSet"})
	if err := reconciler.Get(context.Background(), client.ObjectKey{Name: "acme-prod-session-cache", Namespace: "argocd"}, appSet); err != nil {
		t.Fatalf("expected Argo ApplicationSet to be created: %v", err)
	}
	generators, found, err := unstructured.NestedSlice(appSet.Object, "spec", "generators")
	if err != nil || !found || len(generators) != 1 {
		t.Fatalf("expected one ApplicationSet generator, got found=%v len=%d err=%v", found, len(generators), err)
	}
	generator, ok := generators[0].(map[string]any)
	if !ok {
		t.Fatalf("expected generator object, got %#v", generators[0])
	}
	listConfig, ok := generator["list"].(map[string]any)
	if !ok {
		t.Fatalf("expected list generator config, got %#v", generator)
	}
	elements, ok := listConfig["elements"].([]any)
	if !ok || len(elements) != 2 {
		t.Fatalf("expected two ApplicationSet list elements, got %#v", listConfig["elements"])
	}
	path, _, _ := unstructured.NestedString(appSet.Object, "spec", "template", "spec", "source", "path")
	destName, _, _ := unstructured.NestedString(appSet.Object, "spec", "template", "spec", "destination", "name")
	if path != "{{path}}" || destName != "{{cluster}}" {
		t.Fatalf("unexpected ApplicationSet template source/destination path=%q dest=%q", path, destName)
	}
}

func TestServiceInstanceReconcilerApplyArgoObservedStatus(t *testing.T) {
	scheme := inventoryTestScheme(t)
	app := &unstructured.Unstructured{}
	app.SetGroupVersionKind(schema.GroupVersionKind{Group: "argoproj.io", Version: "v1alpha1", Kind: "Application"})
	app.SetName("acme-prod-orders-db")
	app.SetNamespace("argocd")
	app.Object["status"] = map[string]any{
		"sync": map[string]any{
			"status": "Synced",
		},
		"health": map[string]any{
			"status":  "Healthy",
			"message": "Application is healthy.",
		},
	}

	reconciler := &ServiceInstanceReconciler{
		Client: fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(app).
			Build(),
		Scheme:          scheme,
		ArgoCDNamespace: "argocd",
	}

	instance := &platformv1alpha1.ServiceInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "orders-db", Generation: 2},
		Status: platformv1alpha1.ServiceInstanceStatus{
			Sync: platformv1alpha1.DeliverySyncStatus{
				ApplicationName: "acme-prod-orders-db",
				Phase:           string(adapters.SyncPhasePending),
			},
			Health: platformv1alpha1.HealthStatus{Summary: "Awaiting Argo"},
		},
	}

	reconciler.applyArgoObservedStatus(context.Background(), instance)

	if instance.Status.Sync.Phase != string(adapters.SyncPhaseSynced) {
		t.Fatalf("expected synced phase, got %q", instance.Status.Sync.Phase)
	}
	if instance.Status.Sync.Message != "sync=Synced, health=Healthy" {
		t.Fatalf("unexpected sync message %q", instance.Status.Sync.Message)
	}
	if !isStatusConditionTrue(instance.Status.Conditions, "Synced") {
		t.Fatalf("expected Synced condition to be true")
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
			Packages: []platformv1alpha1.PackageStatus{
				{Name: externalSecretsOperatorPackageName, Phase: platformv1alpha1.PackagePhaseInstalled, Message: "All CRD probes passed."},
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

	for i := 0; i < 2; i++ {
		if _, err := reconciler.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKey{Name: "session-cache"}}); err != nil {
			t.Fatalf("reconcile %d returned error: %v", i+1, err)
		}
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

	for i := 0; i < 2; i++ {
		if _, err := reconciler.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKey{Name: "session-cache"}}); err != nil {
			t.Fatalf("reconcile %d returned error: %v", i+1, err)
		}
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
	if len(updated.Status.CredentialRefs) != 1 || updated.Status.CredentialRefs[0].Name != "session-cache-auth-projected" {
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

func TestServiceInstanceReconcilerGeneratesCNPGCredentialSecret(t *testing.T) {
	scheme := inventoryTestScheme(t)
	instance := &platformv1alpha1.ServiceInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "orders-db"},
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
		RuntimeDriver: "cnpg",
		CredentialRefs: []platformv1alpha1.NamespacedObjectReference{
			{Name: "orders-db-app", Namespace: "acme-acme-prod-orders-db"},
		},
	})
	if err != nil {
		t.Fatalf("ensureCredentialSecrets returned error: %v", err)
	}

	var secret corev1.Secret
	if err := reconciler.Get(context.Background(), client.ObjectKey{Name: "orders-db-app", Namespace: "acme-acme-prod-orders-db"}, &secret); err != nil {
		t.Fatalf("expected generated CNPG credential Secret: %v", err)
	}
	if string(secret.Data["username"]) != "orders_db" || string(secret.Data["database"]) != "orders_db" || len(secret.Data["password"]) == 0 {
		t.Fatalf("expected CNPG credential data, got %#v", secret.Data)
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

func TestServiceBindingReconcilerProjectsSourceCredentials(t *testing.T) {
	scheme := inventoryTestScheme(t)
	project := &platformv1alpha1.Project{
		ObjectMeta: metav1.ObjectMeta{Name: "acme-prod"},
		Status: platformv1alpha1.ProjectStatus{
			Placement: platformv1alpha1.PlacementStatus{ClusterName: "local-dev"},
		},
	}
	source := &platformv1alpha1.ServiceInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "orders-db"},
		Spec: platformv1alpha1.ServiceInstanceSpec{
			ProjectRef: platformv1alpha1.LocalObjectReference{Name: "acme-prod"},
		},
		Status: platformv1alpha1.ServiceInstanceStatus{
			Placement: platformv1alpha1.PlacementStatus{Namespace: "acme-prod-orders-db"},
			CredentialRefs: []platformv1alpha1.NamespacedObjectReference{{
				Name:      "orders-db-auth",
				Namespace: "acme-prod-orders-db",
			}},
		},
	}
	sourceSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "orders-db-auth", Namespace: "acme-prod-orders-db"},
		Data: map[string][]byte{
			"username": []byte("orders"),
			"password": []byte("supersecret"),
		},
	}
	binding := &platformv1alpha1.ServiceBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "orders-api"},
		Spec: platformv1alpha1.ServiceBindingSpec{
			ProjectRef: platformv1alpha1.LocalObjectReference{Name: "acme-prod"},
			SourceRef: platformv1alpha1.TypedObjectReference{
				APIVersion: platformv1alpha1.GroupVersion.String(),
				Kind:       "ServiceInstance",
				Name:       "orders-db",
			},
			TargetRef: platformv1alpha1.TypedObjectReference{
				APIVersion: "v1",
				Kind:       "Namespace",
				Name:       "orders-api",
				Namespace:  "acme-prod-api",
			},
			SecretPolicy: platformv1alpha1.SecretPolicySpec{DeliveryMode: platformv1alpha1.SecretDeliveryModeDirectSecretRef},
		},
	}

	reconciler := &ServiceBindingReconciler{
		Client: fake.NewClientBuilder().
			WithScheme(scheme).
			WithStatusSubresource(&platformv1alpha1.ServiceBinding{}).
			WithObjects(project, source, sourceSecret, binding).
			Build(),
		Scheme: scheme,
	}

	if _, err := reconciler.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKey{Name: "orders-api"}}); err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	var projected corev1.Secret
	if err := reconciler.Get(context.Background(), client.ObjectKey{Name: "orders-api-binding", Namespace: "acme-prod-api"}, &projected); err != nil {
		t.Fatalf("expected projected secret: %v", err)
	}
	if string(projected.Data["username"]) != "orders" {
		t.Fatalf("expected username copy, got %#v", projected.Data)
	}
	var updated platformv1alpha1.ServiceBinding
	if err := reconciler.Get(context.Background(), client.ObjectKey{Name: "orders-api"}, &updated); err != nil {
		t.Fatalf("get binding: %v", err)
	}
	if updated.Status.Phase != "Ready" {
		t.Fatalf("expected Ready phase, got %q", updated.Status.Phase)
	}
}

func TestServiceBindingReconcilerProjectsSourceCredentialsViaExternalSecret(t *testing.T) {
	scheme := inventoryTestScheme(t)
	project := &platformv1alpha1.Project{
		ObjectMeta: metav1.ObjectMeta{Name: "acme-prod"},
		Status: platformv1alpha1.ProjectStatus{
			Placement: platformv1alpha1.PlacementStatus{ClusterName: "local-dev"},
		},
	}
	source := &platformv1alpha1.ServiceInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "orders-db"},
		Spec: platformv1alpha1.ServiceInstanceSpec{
			ProjectRef: platformv1alpha1.LocalObjectReference{Name: "acme-prod"},
		},
		Status: platformv1alpha1.ServiceInstanceStatus{
			Placement: platformv1alpha1.PlacementStatus{Namespace: "acme-prod-orders-db"},
			CredentialRefs: []platformv1alpha1.NamespacedObjectReference{{
				Name:      "orders-db-app-projected",
				Namespace: "acme-prod-orders-db",
			}},
		},
	}
	sourceSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "orders-db-app-projected", Namespace: "acme-prod-orders-db"},
		Data: map[string][]byte{
			"username": []byte("orders"),
			"password": []byte("supersecret"),
		},
	}
	binding := &platformv1alpha1.ServiceBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "orders-api"},
		Spec: platformv1alpha1.ServiceBindingSpec{
			ProjectRef: platformv1alpha1.LocalObjectReference{Name: "acme-prod"},
			SourceRef: platformv1alpha1.TypedObjectReference{
				APIVersion: platformv1alpha1.GroupVersion.String(),
				Kind:       "ServiceInstance",
				Name:       "orders-db",
			},
			TargetRef: platformv1alpha1.TypedObjectReference{
				APIVersion: "v1",
				Kind:       "Namespace",
				Name:       "orders-api",
				Namespace:  "acme-prod-api",
			},
			SecretPolicy: platformv1alpha1.SecretPolicySpec{DeliveryMode: platformv1alpha1.SecretDeliveryModeExternalSecret},
		},
	}

	reconciler := &ServiceBindingReconciler{
		Client: fake.NewClientBuilder().
			WithScheme(scheme).
			WithStatusSubresource(&platformv1alpha1.ServiceBinding{}).
			WithObjects(project, source, sourceSecret, binding).
			Build(),
		Scheme: scheme,
	}

	if _, err := reconciler.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKey{Name: "orders-api"}}); err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	var externalSecret unstructured.Unstructured
	externalSecret.SetGroupVersionKind(schema.GroupVersionKind{Group: "external-secrets.io", Version: "v1", Kind: "ExternalSecret"})
	if err := reconciler.Get(context.Background(), client.ObjectKey{Name: "orders-api-binding", Namespace: "acme-prod-api"}, &externalSecret); err != nil {
		t.Fatalf("expected ExternalSecret projection: %v", err)
	}
	var projectedSecret corev1.Secret
	if err := reconciler.Get(context.Background(), client.ObjectKey{Name: "orders-api-binding", Namespace: "acme-prod-api"}, &projectedSecret); err == nil {
		t.Fatalf("did not expect direct Secret projection when external-secret mode enabled")
	}
	var updated platformv1alpha1.ServiceBinding
	if err := reconciler.Get(context.Background(), client.ObjectKey{Name: "orders-api"}, &updated); err != nil {
		t.Fatalf("get binding: %v", err)
	}
	if updated.Status.Phase != "Ready" {
		t.Fatalf("expected Ready phase, got %q", updated.Status.Phase)
	}
}

func TestServiceBindingReconcilerWaitsForExternalSecretsOperatorPackage(t *testing.T) {
	scheme := inventoryTestScheme(t)
	project := &platformv1alpha1.Project{
		ObjectMeta: metav1.ObjectMeta{Name: "acme-prod"},
		Status: platformv1alpha1.ProjectStatus{
			Placement: platformv1alpha1.PlacementStatus{ClusterName: "local-dev"},
		},
	}
	clusterTarget := &platformv1alpha1.ClusterTarget{
		ObjectMeta: metav1.ObjectMeta{Name: "local-dev"},
		Spec: platformv1alpha1.ClusterTargetSpec{
			RequiredPackages: []string{externalSecretsOperatorPackageName},
		},
		Status: platformv1alpha1.ClusterTargetStatus{
			Packages: []platformv1alpha1.PackageStatus{{
				Name:    externalSecretsOperatorPackageName,
				Phase:   platformv1alpha1.PackagePhaseDeploying,
				Message: "Manifests applied; waiting for operator to become ready.",
			}},
		},
	}
	source := &platformv1alpha1.ServiceInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "orders-db"},
		Spec: platformv1alpha1.ServiceInstanceSpec{
			ProjectRef: platformv1alpha1.LocalObjectReference{Name: "acme-prod"},
		},
		Status: platformv1alpha1.ServiceInstanceStatus{
			Placement: platformv1alpha1.PlacementStatus{
				ClusterName: "local-dev",
				Namespace:   "acme-prod-orders-db",
			},
			CredentialRefs: []platformv1alpha1.NamespacedObjectReference{{
				Name:      "orders-db-app-projected",
				Namespace: "acme-prod-orders-db",
			}},
		},
	}
	sourceSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "orders-db-app-projected", Namespace: "acme-prod-orders-db"},
		Data: map[string][]byte{
			"username": []byte("orders"),
			"password": []byte("supersecret"),
		},
	}
	binding := &platformv1alpha1.ServiceBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "orders-api"},
		Spec: platformv1alpha1.ServiceBindingSpec{
			ProjectRef: platformv1alpha1.LocalObjectReference{Name: "acme-prod"},
			SourceRef: platformv1alpha1.TypedObjectReference{
				APIVersion: platformv1alpha1.GroupVersion.String(),
				Kind:       "ServiceInstance",
				Name:       "orders-db",
			},
			TargetRef: platformv1alpha1.TypedObjectReference{
				APIVersion: "v1",
				Kind:       "Namespace",
				Name:       "orders-api",
				Namespace:  "acme-prod-api",
			},
			SecretPolicy: platformv1alpha1.SecretPolicySpec{DeliveryMode: platformv1alpha1.SecretDeliveryModeExternalSecret},
		},
	}

	reconciler := &ServiceBindingReconciler{
		Client: fake.NewClientBuilder().
			WithScheme(scheme).
			WithStatusSubresource(&platformv1alpha1.ServiceBinding{}).
			WithObjects(project, clusterTarget, source, sourceSecret, binding).
			Build(),
		Scheme: scheme,
	}

	if _, err := reconciler.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKey{Name: "orders-api"}}); err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	var updated platformv1alpha1.ServiceBinding
	if err := reconciler.Get(context.Background(), client.ObjectKey{Name: "orders-api"}, &updated); err != nil {
		t.Fatalf("get binding: %v", err)
	}
	if updated.Status.Phase != "PendingDependencies" {
		t.Fatalf("expected PendingDependencies phase, got %q", updated.Status.Phase)
	}
	if isStatusConditionTrue(updated.Status.Conditions, "Ready") {
		t.Fatalf("expected Ready condition to remain false while package is deploying")
	}
	if !conditionMessagesContain(updated.Status.Conditions, "external-secrets") {
		t.Fatalf("expected dependency message to mention external-secrets, got %#v", updated.Status.Conditions)
	}
}

func TestServiceBindingReconcilerIntegratesDeploymentTarget(t *testing.T) {
	scheme := inventoryTestScheme(t)
	project := &platformv1alpha1.Project{
		ObjectMeta: metav1.ObjectMeta{Name: "acme-prod"},
		Status: platformv1alpha1.ProjectStatus{
			Placement: platformv1alpha1.PlacementStatus{ClusterName: "local-dev"},
		},
	}
	source := &platformv1alpha1.ServiceInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "orders-db"},
		Spec: platformv1alpha1.ServiceInstanceSpec{
			ProjectRef: platformv1alpha1.LocalObjectReference{Name: "acme-prod"},
		},
		Status: platformv1alpha1.ServiceInstanceStatus{
			Placement: platformv1alpha1.PlacementStatus{Namespace: "acme-prod-orders-db"},
			CredentialRefs: []platformv1alpha1.NamespacedObjectReference{{
				Name:      "orders-db-auth",
				Namespace: "acme-prod-orders-db",
			}},
		},
	}
	sourceSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "orders-db-auth", Namespace: "acme-prod-orders-db"},
		Data: map[string][]byte{
			"username": []byte("orders"),
			"password": []byte("supersecret"),
		},
	}
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "orders-api", Namespace: "acme-prod-api"},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "orders-api"}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "orders-api"}},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "api", Image: "demo"}},
				},
			},
		},
	}
	binding := &platformv1alpha1.ServiceBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "orders-api"},
		Spec: platformv1alpha1.ServiceBindingSpec{
			ProjectRef: platformv1alpha1.LocalObjectReference{Name: "acme-prod"},
			SourceRef: platformv1alpha1.TypedObjectReference{
				APIVersion: platformv1alpha1.GroupVersion.String(),
				Kind:       "ServiceInstance",
				Name:       "orders-db",
			},
			TargetRef: platformv1alpha1.TypedObjectReference{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
				Name:       "orders-api",
				Namespace:  "acme-prod-api",
			},
			SecretPolicy: platformv1alpha1.SecretPolicySpec{DeliveryMode: platformv1alpha1.SecretDeliveryModeDirectSecretRef},
		},
	}

	reconciler := &ServiceBindingReconciler{
		Client: fake.NewClientBuilder().
			WithScheme(scheme).
			WithStatusSubresource(&platformv1alpha1.ServiceBinding{}).
			WithObjects(project, source, sourceSecret, deployment, binding).
			Build(),
		Scheme: scheme,
	}

	if _, err := reconciler.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKey{Name: "orders-api"}}); err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	var updatedDeployment appsv1.Deployment
	if err := reconciler.Get(context.Background(), client.ObjectKey{Name: "orders-api", Namespace: "acme-prod-api"}, &updatedDeployment); err != nil {
		t.Fatalf("get deployment: %v", err)
	}
	envFrom := updatedDeployment.Spec.Template.Spec.Containers[0].EnvFrom
	if len(envFrom) != 1 || envFrom[0].SecretRef == nil || envFrom[0].SecretRef.Name != "orders-api-binding" {
		t.Fatalf("expected binding secret envFrom, got %#v", envFrom)
	}
	if updatedDeployment.Spec.Template.Annotations["servicer.io/binding-secret"] != "orders-api-binding" {
		t.Fatalf("expected binding annotation, got %#v", updatedDeployment.Spec.Template.Annotations)
	}
}

func TestServiceBindingReconcilerIntegratesDeploymentTargetViaExternalSecret(t *testing.T) {
	scheme := inventoryTestScheme(t)
	project := &platformv1alpha1.Project{
		ObjectMeta: metav1.ObjectMeta{Name: "acme-prod"},
		Status: platformv1alpha1.ProjectStatus{
			Placement: platformv1alpha1.PlacementStatus{ClusterName: "local-dev"},
		},
	}
	source := &platformv1alpha1.ServiceInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "orders-db"},
		Spec: platformv1alpha1.ServiceInstanceSpec{
			ProjectRef: platformv1alpha1.LocalObjectReference{Name: "acme-prod"},
		},
		Status: platformv1alpha1.ServiceInstanceStatus{
			Placement: platformv1alpha1.PlacementStatus{Namespace: "acme-prod-orders-db"},
			CredentialRefs: []platformv1alpha1.NamespacedObjectReference{{
				Name:      "orders-db-app-projected",
				Namespace: "acme-prod-orders-db",
			}},
		},
	}
	sourceSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "orders-db-app-projected", Namespace: "acme-prod-orders-db"},
		Data: map[string][]byte{
			"username": []byte("orders"),
			"password": []byte("supersecret"),
		},
	}
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "orders-api", Namespace: "acme-prod-api"},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "orders-api"}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "orders-api"}},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "api", Image: "demo"}},
				},
			},
		},
	}
	binding := &platformv1alpha1.ServiceBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "orders-api"},
		Spec: platformv1alpha1.ServiceBindingSpec{
			ProjectRef: platformv1alpha1.LocalObjectReference{Name: "acme-prod"},
			SourceRef: platformv1alpha1.TypedObjectReference{
				APIVersion: platformv1alpha1.GroupVersion.String(),
				Kind:       "ServiceInstance",
				Name:       "orders-db",
			},
			TargetRef: platformv1alpha1.TypedObjectReference{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
				Name:       "orders-api",
				Namespace:  "acme-prod-api",
			},
			SecretPolicy: platformv1alpha1.SecretPolicySpec{DeliveryMode: platformv1alpha1.SecretDeliveryModeExternalSecret},
		},
	}

	reconciler := &ServiceBindingReconciler{
		Client: fake.NewClientBuilder().
			WithScheme(scheme).
			WithStatusSubresource(&platformv1alpha1.ServiceBinding{}).
			WithObjects(project, source, sourceSecret, deployment, binding).
			Build(),
		Scheme: scheme,
	}

	if _, err := reconciler.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKey{Name: "orders-api"}}); err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	var updatedDeployment appsv1.Deployment
	if err := reconciler.Get(context.Background(), client.ObjectKey{Name: "orders-api", Namespace: "acme-prod-api"}, &updatedDeployment); err != nil {
		t.Fatalf("get deployment: %v", err)
	}
	envFrom := updatedDeployment.Spec.Template.Spec.Containers[0].EnvFrom
	if len(envFrom) != 1 || envFrom[0].SecretRef == nil || envFrom[0].SecretRef.Name != "orders-api-binding" {
		t.Fatalf("expected external-secret target wiring, got %#v", envFrom)
	}
	var externalSecret unstructured.Unstructured
	externalSecret.SetGroupVersionKind(schema.GroupVersionKind{Group: "external-secrets.io", Version: "v1", Kind: "ExternalSecret"})
	if err := reconciler.Get(context.Background(), client.ObjectKey{Name: "orders-api-binding", Namespace: "acme-prod-api"}, &externalSecret); err != nil {
		t.Fatalf("expected ExternalSecret projection: %v", err)
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
	scheme.AddKnownTypeWithName(schema.GroupVersionKind{Group: "argoproj.io", Version: "v1alpha1", Kind: "Application"}, &unstructured.Unstructured{})
	scheme.AddKnownTypeWithName(schema.GroupVersionKind{Group: "argoproj.io", Version: "v1alpha1", Kind: "ApplicationList"}, &unstructured.UnstructuredList{})
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
				Packages: []platformv1alpha1.PackageStatus{
					{Name: externalSecretsOperatorPackageName, Phase: platformv1alpha1.PackagePhaseInstalled, Message: "All CRD probes passed."},
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

func statusConditionByType(conditions []metav1.Condition, conditionType string) *metav1.Condition {
	for i := range conditions {
		if conditions[i].Type == conditionType {
			return &conditions[i]
		}
	}
	return nil
}

func conditionMessagesContain(conditions []metav1.Condition, needle string) bool {
	for _, condition := range conditions {
		if strings.Contains(condition.Message, needle) {
			return true
		}
	}
	return false
}
