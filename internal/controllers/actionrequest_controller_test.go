package controllers

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	platformv1alpha1 "github.com/sindef/servicer/api/v1alpha1"
	"github.com/sindef/servicer/internal/adapters"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestActionRequestReconcilerExecutesApprovedCNPGAction(t *testing.T) {
	reconciler := newActionRequestReconciler(t, actionRequestFixture("orders-db-backup", string(adapters.ActionBackup), platformv1alpha1.ApprovalModeAuto))

	if _, err := reconciler.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKey{Name: "orders-db-backup"}}); err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	var actionRequest platformv1alpha1.ActionRequest
	if err := reconciler.Get(context.Background(), client.ObjectKey{Name: "orders-db-backup"}, &actionRequest); err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if actionRequest.Status.Phase != "Queued" {
		t.Fatalf("expected phase Queued, got %q", actionRequest.Status.Phase)
	}
	if actionRequest.Status.StartedAt == nil {
		t.Fatalf("expected StartedAt to be set")
	}
	if actionRequest.Status.CompletedAt != nil {
		t.Fatalf("expected CompletedAt to be empty for queued action")
	}
	if actionRequest.Status.Result.Code != "queued" {
		t.Fatalf("expected result code queued, got %q", actionRequest.Status.Result.Code)
	}
	if actionRequest.Status.Result.Message != "CNPG backup request queued." {
		t.Fatalf("expected queued backup message, got %q", actionRequest.Status.Result.Message)
	}
	if actionRequest.Status.OperationRef == nil || actionRequest.Status.OperationRef.Kind != "Backup" {
		t.Fatalf("expected Backup operation ref, got %#v", actionRequest.Status.OperationRef)
	}
}

func TestActionRequestReconcilerRequiresApprovalWhenCapabilityDemandsIt(t *testing.T) {
	reconciler := newActionRequestReconciler(t, actionRequestFixture("orders-db-failover", string(adapters.ActionFailover), platformv1alpha1.ApprovalModeAuto))

	if _, err := reconciler.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKey{Name: "orders-db-failover"}}); err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	var actionRequest platformv1alpha1.ActionRequest
	if err := reconciler.Get(context.Background(), client.ObjectKey{Name: "orders-db-failover"}, &actionRequest); err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if actionRequest.Status.Phase != "PendingApproval" {
		t.Fatalf("expected phase PendingApproval, got %q", actionRequest.Status.Phase)
	}
	if actionRequest.Status.StartedAt != nil {
		t.Fatalf("expected StartedAt to remain empty while pending approval")
	}
	if actionRequest.Status.Result.Code != "approval-required" {
		t.Fatalf("expected result code approval-required, got %q", actionRequest.Status.Result.Code)
	}
	if actionRequest.Status.OperationRef != nil {
		t.Fatalf("expected no operation ref while pending approval, got %#v", actionRequest.Status.OperationRef)
	}
}

func TestActionRequestReconcilerRejectsApprovedActionWithoutApproverIdentity(t *testing.T) {
	action := actionRequestFixture("orders-db-failover-approved", string(adapters.ActionFailover), platformv1alpha1.ApprovalModeApproved)
	action.Spec.Approval.ApprovedBy = nil
	reconciler := newActionRequestReconciler(t, action)

	if _, err := reconciler.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKey{Name: "orders-db-failover-approved"}}); err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	var actionRequest platformv1alpha1.ActionRequest
	if err := reconciler.Get(context.Background(), client.ObjectKey{Name: "orders-db-failover-approved"}, &actionRequest); err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if actionRequest.Status.Phase != "Failed" {
		t.Fatalf("expected phase Failed, got %q", actionRequest.Status.Phase)
	}
	if actionRequest.Status.Result.Code != "approval-invalid" {
		t.Fatalf("expected result code approval-invalid, got %q", actionRequest.Status.Result.Code)
	}
}

func TestActionRequestReconcilerAppliesValkeyScale(t *testing.T) {
	reconciler := newValkeyActionRequestReconciler(t, valkeyActionRequestFixture("session-cache-scale", string(adapters.ActionScale), map[string]any{"replicas": 5}))

	if _, err := reconciler.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKey{Name: "session-cache-scale"}}); err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	var statefulSet appsv1.StatefulSet
	if err := reconciler.Get(context.Background(), client.ObjectKey{Name: "session-cache", Namespace: "acme-prod-session-cache"}, &statefulSet); err != nil {
		t.Fatalf("Get StatefulSet returned error: %v", err)
	}
	if statefulSet.Spec.Replicas == nil || *statefulSet.Spec.Replicas != 5 {
		t.Fatalf("expected StatefulSet replicas to be 5, got %#v", statefulSet.Spec.Replicas)
	}
	var actionRequest platformv1alpha1.ActionRequest
	if err := reconciler.Get(context.Background(), client.ObjectKey{Name: "session-cache-scale"}, &actionRequest); err != nil {
		t.Fatalf("Get ActionRequest returned error: %v", err)
	}
	if actionRequest.Status.Phase != "Succeeded" {
		t.Fatalf("expected action phase Succeeded, got %q", actionRequest.Status.Phase)
	}
}

func TestActionRequestReconcilerAppliesValkeyRestart(t *testing.T) {
	reconciler := newValkeyActionRequestReconciler(t, valkeyActionRequestFixture("session-cache-restart", string(adapters.ActionRestart), nil))

	if _, err := reconciler.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKey{Name: "session-cache-restart"}}); err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	var statefulSet appsv1.StatefulSet
	if err := reconciler.Get(context.Background(), client.ObjectKey{Name: "session-cache", Namespace: "acme-prod-session-cache"}, &statefulSet); err != nil {
		t.Fatalf("Get StatefulSet returned error: %v", err)
	}
	if statefulSet.Spec.Template.Annotations["servicer.io/restart-request"] != "session-cache-restart" {
		t.Fatalf("expected restart annotation to be applied, got %#v", statefulSet.Spec.Template.Annotations)
	}
}

func TestActionRequestReconcilerRotatesValkeyCredentials(t *testing.T) {
	reconciler := newValkeyActionRequestReconciler(t, valkeyActionRequestFixture("session-cache-rotate", string(adapters.ActionRotateCredentials), nil))

	var before corev1.Secret
	if err := reconciler.Get(context.Background(), client.ObjectKey{Name: "session-cache-auth", Namespace: "acme-prod-session-cache"}, &before); err != nil {
		t.Fatalf("Get Secret returned error: %v", err)
	}
	beforePassword := string(before.Data["password"])

	if _, err := reconciler.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKey{Name: "session-cache-rotate"}}); err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	var after corev1.Secret
	if err := reconciler.Get(context.Background(), client.ObjectKey{Name: "session-cache-auth", Namespace: "acme-prod-session-cache"}, &after); err != nil {
		t.Fatalf("Get Secret returned error: %v", err)
	}
	if string(after.Data["password"]) == beforePassword {
		t.Fatalf("expected password to rotate")
	}
	if after.Annotations["servicer.io/rotation-request"] != "session-cache-rotate" {
		t.Fatalf("expected rotation annotation, got %#v", after.Annotations)
	}
}

func TestActionRequestReconcilerPromotesValkeyFailoverCandidate(t *testing.T) {
	actionRequest := valkeyActionRequestFixture("session-cache-failover", string(adapters.ActionFailover), map[string]any{"candidateCluster": "west-2"})
	actionRequest.Spec.Approval.Mode = platformv1alpha1.ApprovalModeApproved
	reconciler := newValkeyActionRequestReconciler(t, actionRequest)

	if _, err := reconciler.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKey{Name: "session-cache-failover"}}); err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	var instance platformv1alpha1.ServiceInstance
	if err := reconciler.Get(context.Background(), client.ObjectKey{Name: "session-cache"}, &instance); err != nil {
		t.Fatalf("Get ServiceInstance returned error: %v", err)
	}
	if instance.Status.CacheTopology.PrimaryCluster != "west-2" {
		t.Fatalf("expected promoted primary west-2, got %#v", instance.Status.CacheTopology)
	}
	var action platformv1alpha1.ActionRequest
	if err := reconciler.Get(context.Background(), client.ObjectKey{Name: "session-cache-failover"}, &action); err != nil {
		t.Fatalf("Get ActionRequest returned error: %v", err)
	}
	if action.Status.Phase != "Succeeded" {
		t.Fatalf("expected failover action to succeed, got %q", action.Status.Phase)
	}
}

func TestActionRequestReconcilerResyncsValkeyStandby(t *testing.T) {
	actionRequest := valkeyActionRequestFixture("session-cache-resync", string(adapters.ActionResyncStandby), map[string]any{"standbyCluster": "west-2"})
	actionRequest.Spec.Approval.Mode = platformv1alpha1.ApprovalModeApproved
	reconciler := newValkeyActionRequestReconciler(t, actionRequest)

	if _, err := reconciler.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKey{Name: "session-cache-resync"}}); err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	var instance platformv1alpha1.ServiceInstance
	if err := reconciler.Get(context.Background(), client.ObjectKey{Name: "session-cache"}, &instance); err != nil {
		t.Fatalf("Get ServiceInstance returned error: %v", err)
	}
	standby := instance.Status.CacheTopology.StandbyClusters[0]
	if !standby.ResyncRequired || standby.Ready {
		t.Fatalf("expected standby to require resync and be promotion-blocked, got %#v", standby)
	}
}

func TestActionRequestReconcilerRollsBackValkeyFailover(t *testing.T) {
	actionRequest := valkeyActionRequestFixture("session-cache-rollback", string(adapters.ActionRollbackFailover), map[string]any{"targetPrimary": "east-1"})
	actionRequest.Spec.Approval.Mode = platformv1alpha1.ApprovalModeApproved
	instance := valkeyServiceInstanceFixture()
	instance.Status.CacheTopology.PrimaryCluster = "west-2"
	instance.Status.CacheTopology.StandbyClusters = []platformv1alpha1.CacheStandbyStatus{
		{ClusterName: "east-1", Ready: true, LagObserved: true, ReplicationLagSeconds: 2, Message: "promotion-ready"},
	}
	reconciler := newValkeyActionRequestReconcilerWithInstance(t, instance, actionRequest)

	if _, err := reconciler.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKey{Name: "session-cache-rollback"}}); err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	var updated platformv1alpha1.ServiceInstance
	if err := reconciler.Get(context.Background(), client.ObjectKey{Name: "session-cache"}, &updated); err != nil {
		t.Fatalf("Get ServiceInstance returned error: %v", err)
	}
	if updated.Status.CacheTopology.PrimaryCluster != "east-1" {
		t.Fatalf("expected rollback primary east-1, got %#v", updated.Status.CacheTopology)
	}
}

func TestActionRequestReconcilerAppliesNATSScale(t *testing.T) {
	reconciler := newNATSActionRequestReconciler(t, natsActionRequestFixture("session-bus-scale", string(adapters.ActionScale), map[string]any{"replicas": 3}))

	if _, err := reconciler.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKey{Name: "session-bus-scale"}}); err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	var statefulSet appsv1.StatefulSet
	if err := reconciler.Get(context.Background(), client.ObjectKey{Name: "session-bus", Namespace: "acme-prod-session-bus"}, &statefulSet); err != nil {
		t.Fatalf("Get StatefulSet returned error: %v", err)
	}
	if statefulSet.Spec.Replicas == nil || *statefulSet.Spec.Replicas != 3 {
		t.Fatalf("expected NATS StatefulSet replicas to be 3, got %#v", statefulSet.Spec.Replicas)
	}
}

func TestActionRequestReconcilerRotatesNamedNATSCredential(t *testing.T) {
	reconciler := newNATSActionRequestReconciler(t, natsActionRequestFixture("session-bus-rotate-orders", string(adapters.ActionRotateCredentials), map[string]any{"credentialName": "orders-api"}))

	if _, err := reconciler.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKey{Name: "session-bus-rotate-orders"}}); err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	var secret corev1.Secret
	if err := reconciler.Get(context.Background(), client.ObjectKey{Name: "session-bus-orders-api-auth", Namespace: "acme-prod-session-bus"}, &secret); err != nil {
		t.Fatalf("Get Secret returned error: %v", err)
	}
	if string(secret.Data["password"]) == "orders-old-password" {
		t.Fatalf("expected app credential password to rotate")
	}

	var authConfig corev1.Secret
	if err := reconciler.Get(context.Background(), client.ObjectKey{Name: "session-bus-auth-config", Namespace: "acme-prod-session-bus"}, &authConfig); err != nil {
		t.Fatalf("Get auth config secret returned error: %v", err)
	}
	if !strings.Contains(string(authConfig.Data["users.conf"]), "orders-api") {
		t.Fatalf("expected users.conf to include rotated orders-api user")
	}
}

func TestActionRequestReconcilerUpdatesNamespaceQuota(t *testing.T) {
	reconciler := newNamespaceActionRequestReconciler(t, namespaceActionRequestFixture("team-space-quota", map[string]any{"cpu": "6", "memory": "12Gi", "pods": "60"}))

	if _, err := reconciler.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKey{Name: "team-space-quota"}}); err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	var quota corev1.ResourceQuota
	if err := reconciler.Get(context.Background(), client.ObjectKey{Name: "team-space-quota", Namespace: "acme-prod-team-space"}, &quota); err != nil {
		t.Fatalf("Get ResourceQuota returned error: %v", err)
	}
	if got := quota.Spec.Hard[corev1.ResourcePods]; got.String() != "60" {
		t.Fatalf("expected pods quota 60, got %s", got.String())
	}
	var action platformv1alpha1.ActionRequest
	if err := reconciler.Get(context.Background(), client.ObjectKey{Name: "team-space-quota"}, &action); err != nil {
		t.Fatalf("Get ActionRequest returned error: %v", err)
	}
	if action.Status.Phase != "Succeeded" {
		t.Fatalf("expected quota action to succeed, got %q", action.Status.Phase)
	}
}

func TestActionRequestReconcilerPromotesMySQLFailoverCandidate(t *testing.T) {
	actionRequest := valkeyActionRequestFixture("orders-mysql-failover", string(adapters.ActionFailover), map[string]any{"candidateCluster": "west-2"})
	actionRequest.Spec.TargetRef.Name = "orders-mysql"
	actionRequest.Spec.Approval.Mode = platformv1alpha1.ApprovalModeApproved
	reconciler := newMySQLActionRequestReconciler(t, mysqlServiceInstanceFixture(), actionRequest)

	if _, err := reconciler.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKey{Name: "orders-mysql-failover"}}); err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	var instance platformv1alpha1.ServiceInstance
	if err := reconciler.Get(context.Background(), client.ObjectKey{Name: "orders-mysql"}, &instance); err != nil {
		t.Fatalf("Get ServiceInstance returned error: %v", err)
	}
	var params map[string]any
	if err := json.Unmarshal(instance.Spec.Parameters.Raw, &params); err != nil {
		t.Fatalf("unmarshal parameters: %v", err)
	}
	if params["primaryCluster"] != "west-2" {
		t.Fatalf("expected promoted primary west-2, got %#v", params)
	}
}

func TestActionRequestReconcilerGrantsNamespaceAccess(t *testing.T) {
	reconciler := newNamespaceActionRequestReconciler(t, namespaceGrantAccessActionRequestFixture("team-space-access", map[string]any{
		"subject":    "bob@example.com",
		"defaultUrl": "https://servicer.example.com",
	}))

	if _, err := reconciler.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKey{Name: "team-space-access"}}); err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	var role rbacv1.Role
	if err := reconciler.Get(context.Background(), client.ObjectKey{Name: "servicer-access-bob-example-com", Namespace: "acme-prod-team-space"}, &role); err != nil {
		t.Fatalf("Get Role returned error: %v", err)
	}
	if len(role.Rules) == 0 || role.Rules[0].Verbs[0] != "get" {
		t.Fatalf("expected read-only namespace Role, got %#v", role.Rules)
	}

	var secret corev1.Secret
	if err := reconciler.Get(context.Background(), client.ObjectKey{Name: "servicer-access-bob-example-com-kubeconfig", Namespace: "acme-prod-team-space"}, &secret); err != nil {
		t.Fatalf("Get kubeconfig Secret returned error: %v", err)
	}
	kubeconfig := string(secret.Data["kubeconfig"])
	if !strings.Contains(kubeconfig, "https://servicer.example.com/api/kubernetes/namespaces/acme-prod-team-space") {
		t.Fatalf("expected kubeconfig to point at platform namespace proxy, got:\n%s", kubeconfig)
	}
	if len(secret.Data["token"]) == 0 || !strings.Contains(kubeconfig, string(secret.Data["token"])) {
		t.Fatalf("expected kubeconfig to contain issued Servicer access token, got secret keys %#v", secret.Data)
	}

	var action platformv1alpha1.ActionRequest
	if err := reconciler.Get(context.Background(), client.ObjectKey{Name: "team-space-access"}, &action); err != nil {
		t.Fatalf("Get ActionRequest returned error: %v", err)
	}
	if action.Status.Phase != "Succeeded" || action.Status.OperationRef == nil || action.Status.OperationRef.Kind != "Secret" {
		t.Fatalf("expected access action to succeed with Secret operation ref, got %#v", action.Status)
	}
}

func newActionRequestReconciler(t *testing.T, actionRequest *platformv1alpha1.ActionRequest) *ActionRequestReconciler {
	t.Helper()

	scheme := runtime.NewScheme()
	if err := platformv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme returned error: %v", err)
	}

	registry, err := adapters.NewRegistry(adapters.NewCNPGAdapter())
	if err != nil {
		t.Fatalf("NewRegistry returned error: %v", err)
	}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&platformv1alpha1.ActionRequest{}).
		WithStatusSubresource(&platformv1alpha1.ServiceInstance{}).
		WithObjects(
			tenantFixture(),
			projectFixture(),
			clusterTargetFixture(),
			serviceClassFixture(),
			servicePlanFixture(),
			serviceInstanceFixture(),
			actionRequest,
		).
		Build()

	return &ActionRequestReconciler{Client: client, Scheme: scheme, Adapters: registry}
}

func newValkeyActionRequestReconciler(t *testing.T, actionRequest *platformv1alpha1.ActionRequest) *ActionRequestReconciler {
	t.Helper()
	return newValkeyActionRequestReconcilerWithInstance(t, valkeyServiceInstanceFixture(), actionRequest)
}

func newValkeyActionRequestReconcilerWithInstance(t *testing.T, instance *platformv1alpha1.ServiceInstance, actionRequest *platformv1alpha1.ActionRequest) *ActionRequestReconciler {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme returned error: %v", err)
	}
	if err := platformv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme returned error: %v", err)
	}

	registry, err := adapters.NewRegistry(adapters.NewValkeyAdapter())
	if err != nil {
		t.Fatalf("NewRegistry returned error: %v", err)
	}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&platformv1alpha1.ActionRequest{}).
		WithStatusSubresource(&platformv1alpha1.ServiceInstance{}).
		WithObjects(
			valkeyTenantFixture(),
			valkeyProjectFixture(),
			valkeyClusterTargetFixture(),
			valkeyServiceClassFixture(),
			valkeyServicePlanFixture(),
			instance,
			valkeyStatefulSetFixture(),
			valkeySecretFixture(),
			actionRequest,
		).
		Build()

	return &ActionRequestReconciler{Client: client, Scheme: scheme, Adapters: registry}
}

func newNATSActionRequestReconciler(t *testing.T, actionRequest *platformv1alpha1.ActionRequest) *ActionRequestReconciler {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme returned error: %v", err)
	}
	if err := platformv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme returned error: %v", err)
	}
	registry, err := adapters.NewRegistry(adapters.NewNATSAdapter())
	if err != nil {
		t.Fatalf("NewRegistry returned error: %v", err)
	}
	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&platformv1alpha1.ActionRequest{}).
		WithStatusSubresource(&platformv1alpha1.ServiceInstance{}).
		WithObjects(
			natsTenantFixture(),
			natsProjectFixture(),
			natsClusterTargetFixture(),
			natsServiceClassFixture(),
			natsServicePlanFixture(),
			natsServiceInstanceFixture(),
			natsStatefulSetFixture(),
			natsSecretFixture(),
			natsAppSecretFixture(),
			natsAuthConfigSecretFixture(),
			actionRequest,
		).
		Build()
	return &ActionRequestReconciler{Client: client, Scheme: scheme, Adapters: registry}
}

func newNamespaceActionRequestReconciler(t *testing.T, actionRequest *platformv1alpha1.ActionRequest) *ActionRequestReconciler {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme returned error: %v", err)
	}
	if err := platformv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme returned error: %v", err)
	}
	registry, err := adapters.NewRegistry(adapters.NewNamespaceAdapter())
	if err != nil {
		t.Fatalf("NewRegistry returned error: %v", err)
	}
	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&platformv1alpha1.ActionRequest{}).
		WithStatusSubresource(&platformv1alpha1.ServiceInstance{}).
		WithObjects(
			namespaceTenantFixture(),
			namespaceProjectFixture(),
			namespaceClusterTargetFixture(),
			namespaceServiceClassFixture(),
			namespaceServicePlanFixture(),
			namespaceServiceInstanceFixture(),
			namespaceQuotaFixture(),
			actionRequest,
		).
		Build()
	return &ActionRequestReconciler{Client: client, Scheme: scheme, Adapters: registry}
}

func tenantFixture() *platformv1alpha1.Tenant {
	return &platformv1alpha1.Tenant{
		ObjectMeta: metav1.ObjectMeta{Name: "acme"},
		Spec: platformv1alpha1.TenantSpec{
			DisplayName:           "Acme Corp",
			Owners:                platformv1alpha1.OwnersSpec{Users: []string{"alice@example.com"}},
			QuotaProfileRef:       platformv1alpha1.LocalObjectReference{Name: "standard-tenant"},
			AllowedServiceClasses: []string{"postgresql"},
			Lifecycle:             platformv1alpha1.TenantLifecycleSpec{Phase: platformv1alpha1.TenantLifecyclePhaseActive},
		},
	}
}

func projectFixture() *platformv1alpha1.Project {
	return &platformv1alpha1.Project{
		ObjectMeta: metav1.ObjectMeta{Name: "acme-prod"},
		Spec: platformv1alpha1.ProjectSpec{
			TenantRef:         platformv1alpha1.LocalObjectReference{Name: "acme"},
			DisplayName:       "Acme Production",
			Environment:       platformv1alpha1.EnvironmentProduction,
			TargetSelector:    platformv1alpha1.TargetSelectorSpec{ClusterRef: &platformv1alpha1.LocalObjectReference{Name: "east-1"}},
			NamespaceStrategy: platformv1alpha1.NamespaceStrategySpec{Mode: platformv1alpha1.NamespaceStrategyDedicated, Prefix: "acme-prod"},
		},
		Status: platformv1alpha1.ProjectStatus{Placement: platformv1alpha1.PlacementStatus{ClusterName: "east-1"}},
	}
}

func clusterTargetFixture() *platformv1alpha1.ClusterTarget {
	return &platformv1alpha1.ClusterTarget{
		ObjectMeta: metav1.ObjectMeta{Name: "east-1"},
		Spec: platformv1alpha1.ClusterTargetSpec{
			DisplayName:   "East 1",
			ConnectionRef: platformv1alpha1.NamespacedObjectReference{Name: "east-1-kubeconfig", Namespace: "servicer-system"},
		},
	}
}

func serviceClassFixture() *platformv1alpha1.ServiceClass {
	return &platformv1alpha1.ServiceClass{
		ObjectMeta: metav1.ObjectMeta{Name: "postgresql"},
		Spec: platformv1alpha1.ServiceClassSpec{
			DisplayName:           "PostgreSQL",
			Driver:                "cnpg",
			SupportedVersions:     []string{"16", "15"},
			AllowsVersionOverride: true,
		},
	}
}

func servicePlanFixture() *platformv1alpha1.ServicePlan {
	return &platformv1alpha1.ServicePlan{
		ObjectMeta: metav1.ObjectMeta{Name: "postgresql-ha"},
		Spec: platformv1alpha1.ServicePlanSpec{
			ServiceClassRef:       platformv1alpha1.LocalObjectReference{Name: "postgresql"},
			DisplayName:           "Standard HA",
			Topology:              "high-availability",
			DefaultVersion:        "16",
			AllowsVersionOverride: true,
		},
	}
}

func serviceInstanceFixture() *platformv1alpha1.ServiceInstance {
	return &platformv1alpha1.ServiceInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "orders-db"},
		Spec: platformv1alpha1.ServiceInstanceSpec{
			ProjectRef:      platformv1alpha1.LocalObjectReference{Name: "acme-prod"},
			ServiceClassRef: platformv1alpha1.LocalObjectReference{Name: "postgresql"},
			ServicePlanRef:  platformv1alpha1.LocalObjectReference{Name: "postgresql-ha"},
			Version:         "16",
			Exposure:        platformv1alpha1.ExposureSpec{Mode: platformv1alpha1.ExposureModeClusterInternal},
			SecretPolicy:    platformv1alpha1.SecretPolicySpec{DeliveryMode: platformv1alpha1.SecretDeliveryModeExternalSecret},
		},
		Status: platformv1alpha1.ServiceInstanceStatus{Placement: platformv1alpha1.PlacementStatus{ClusterName: "east-1", Namespace: "acme-prod-orders-db"}},
	}
}

func actionRequestFixture(name, action string, mode platformv1alpha1.ApprovalMode) *platformv1alpha1.ActionRequest {
	return &platformv1alpha1.ActionRequest{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: platformv1alpha1.ActionRequestSpec{
			TargetRef: platformv1alpha1.TypedObjectReference{
				APIVersion: platformv1alpha1.GroupVersion.String(),
				Kind:       "ServiceInstance",
				Name:       "orders-db",
			},
			Action:         action,
			IdempotencyKey: name,
			Approval:       platformv1alpha1.ApprovalSpec{Mode: mode},
			RequestedBy:    platformv1alpha1.RequestedBySpec{Subject: "alice@example.com", Source: platformv1alpha1.RequestSourceUI},
		},
	}
}

func valkeyTenantFixture() *platformv1alpha1.Tenant {
	tenant := tenantFixture()
	tenant.Spec.AllowedServiceClasses = []string{"valkey"}
	return tenant
}

func valkeyProjectFixture() *platformv1alpha1.Project {
	project := projectFixture()
	project.Status.Placement.ClusterName = "east-1"
	return project
}

func valkeyClusterTargetFixture() *platformv1alpha1.ClusterTarget {
	return clusterTargetFixture()
}

func valkeyServiceClassFixture() *platformv1alpha1.ServiceClass {
	return &platformv1alpha1.ServiceClass{
		ObjectMeta: metav1.ObjectMeta{Name: "valkey"},
		Spec: platformv1alpha1.ServiceClassSpec{
			DisplayName:           "Valkey",
			Driver:                "servicer-valkey",
			SupportedVersions:     []string{"8.0"},
			AllowsVersionOverride: true,
		},
	}
}

func valkeyServicePlanFixture() *platformv1alpha1.ServicePlan {
	return &platformv1alpha1.ServicePlan{
		ObjectMeta: metav1.ObjectMeta{Name: "valkey-replicated"},
		Spec: platformv1alpha1.ServicePlanSpec{
			ServiceClassRef:       platformv1alpha1.LocalObjectReference{Name: "valkey"},
			DisplayName:           "Replicated",
			Topology:              "multi-cluster-failover",
			DefaultVersion:        "8.0",
			AllowsVersionOverride: true,
		},
	}
}

func valkeyServiceInstanceFixture() *platformv1alpha1.ServiceInstance {
	return &platformv1alpha1.ServiceInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "session-cache"},
		Spec: platformv1alpha1.ServiceInstanceSpec{
			ProjectRef:      platformv1alpha1.LocalObjectReference{Name: "acme-prod"},
			ServiceClassRef: platformv1alpha1.LocalObjectReference{Name: "valkey"},
			ServicePlanRef:  platformv1alpha1.LocalObjectReference{Name: "valkey-replicated"},
			Version:         "8.0",
			Exposure:        platformv1alpha1.ExposureSpec{Mode: platformv1alpha1.ExposureModeClusterInternal},
			SecretPolicy:    platformv1alpha1.SecretPolicySpec{DeliveryMode: platformv1alpha1.SecretDeliveryModeExternalSecret},
		},
		Status: platformv1alpha1.ServiceInstanceStatus{
			Placement: platformv1alpha1.PlacementStatus{ClusterName: "east-1", Namespace: "acme-prod-session-cache"},
			CacheTopology: platformv1alpha1.CacheTopologyStatus{
				Mode:              "multi-cluster-failover",
				PrimaryCluster:    "east-1",
				FailoverReadiness: "Ready",
				StandbyClusters: []platformv1alpha1.CacheStandbyStatus{
					{ClusterName: "west-2", Ready: true, LagObserved: true, ReplicationLagSeconds: 3, Message: "promotion-ready"},
				},
			},
		},
	}
}

func valkeyStatefulSetFixture() *appsv1.StatefulSet {
	replicas := int32(3)
	return &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "session-cache", Namespace: "acme-prod-session-cache"},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{}},
			},
		},
	}
}

func valkeySecretFixture() *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "session-cache-auth", Namespace: "acme-prod-session-cache"},
		Data:       map[string][]byte{"password": []byte("old-password")},
	}
}

func valkeyActionRequestFixture(name, action string, parameters map[string]any) *platformv1alpha1.ActionRequest {
	actionRequest := &platformv1alpha1.ActionRequest{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: platformv1alpha1.ActionRequestSpec{
			TargetRef: platformv1alpha1.TypedObjectReference{
				APIVersion: platformv1alpha1.GroupVersion.String(),
				Kind:       "ServiceInstance",
				Name:       "session-cache",
			},
			Action:         action,
			IdempotencyKey: name,
			Approval:       platformv1alpha1.ApprovalSpec{Mode: platformv1alpha1.ApprovalModeAuto},
			RequestedBy:    platformv1alpha1.RequestedBySpec{Subject: "alice@example.com", Source: platformv1alpha1.RequestSourceUI},
		},
	}
	if parameters != nil {
		raw, err := json.Marshal(parameters)
		if err != nil {
			panic(err)
		}
		actionRequest.Spec.Parameters = &apiextensionsv1.JSON{Raw: raw}
	}
	return actionRequest
}

func natsTenantFixture() *platformv1alpha1.Tenant {
	tenant := tenantFixture()
	tenant.Spec.AllowedServiceClasses = []string{"nats"}
	return tenant
}

func natsProjectFixture() *platformv1alpha1.Project {
	return projectFixture()
}

func natsClusterTargetFixture() *platformv1alpha1.ClusterTarget {
	return clusterTargetFixture()
}

func natsServiceClassFixture() *platformv1alpha1.ServiceClass {
	return &platformv1alpha1.ServiceClass{
		ObjectMeta: metav1.ObjectMeta{Name: "nats"},
		Spec: platformv1alpha1.ServiceClassSpec{
			DisplayName:           "NATS",
			Driver:                "servicer-nats",
			SupportedVersions:     []string{"2.10"},
			AllowsVersionOverride: true,
		},
	}
}

func natsServicePlanFixture() *platformv1alpha1.ServicePlan {
	return &platformv1alpha1.ServicePlan{
		ObjectMeta: metav1.ObjectMeta{Name: "nats-core"},
		Spec: platformv1alpha1.ServicePlanSpec{
			ServiceClassRef:       platformv1alpha1.LocalObjectReference{Name: "nats"},
			DisplayName:           "Core",
			Topology:              "single-cluster",
			DefaultVersion:        "2.10",
			AllowsVersionOverride: true,
		},
	}
}

func natsServiceInstanceFixture() *platformv1alpha1.ServiceInstance {
	return &platformv1alpha1.ServiceInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "session-bus"},
		Spec: platformv1alpha1.ServiceInstanceSpec{
			ProjectRef:      platformv1alpha1.LocalObjectReference{Name: "acme-prod"},
			ServiceClassRef: platformv1alpha1.LocalObjectReference{Name: "nats"},
			ServicePlanRef:  platformv1alpha1.LocalObjectReference{Name: "nats-core"},
			Version:         "2.10",
			Parameters:      &apiextensionsv1.JSON{Raw: []byte(`{"appCredentials":[{"name":"orders-api","permissions":{"publish":["orders.created"]}}]}`)},
			Exposure:        platformv1alpha1.ExposureSpec{Mode: platformv1alpha1.ExposureModeClusterInternal},
			SecretPolicy:    platformv1alpha1.SecretPolicySpec{DeliveryMode: platformv1alpha1.SecretDeliveryModeExternalSecret},
		},
		Status: platformv1alpha1.ServiceInstanceStatus{Placement: platformv1alpha1.PlacementStatus{ClusterName: "east-1", Namespace: "acme-prod-session-bus"}},
	}
}

func natsStatefulSetFixture() *appsv1.StatefulSet {
	replicas := int32(1)
	return &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "session-bus", Namespace: "acme-prod-session-bus"},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{}},
			},
		},
	}
}

func natsSecretFixture() *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "session-bus-auth", Namespace: "acme-prod-session-bus"},
		Data:       map[string][]byte{"username": []byte("servicer"), "password": []byte("old-password")},
	}
}

func natsAppSecretFixture() *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "session-bus-orders-api-auth", Namespace: "acme-prod-session-bus"},
		Data:       map[string][]byte{"username": []byte("orders-api"), "password": []byte("orders-old-password")},
	}
}

func natsAuthConfigSecretFixture() *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "session-bus-auth-config", Namespace: "acme-prod-session-bus"},
		Data:       map[string][]byte{"users.conf": []byte("authorization { users: [] }")},
	}
}

func natsActionRequestFixture(name, action string, parameters map[string]any) *platformv1alpha1.ActionRequest {
	actionRequest := valkeyActionRequestFixture(name, action, parameters)
	actionRequest.Spec.TargetRef.Name = "session-bus"
	return actionRequest
}

func namespaceTenantFixture() *platformv1alpha1.Tenant {
	tenant := tenantFixture()
	tenant.Spec.AllowedServiceClasses = []string{"namespace"}
	return tenant
}

func namespaceProjectFixture() *platformv1alpha1.Project {
	return projectFixture()
}

func namespaceClusterTargetFixture() *platformv1alpha1.ClusterTarget {
	return clusterTargetFixture()
}

func namespaceServiceClassFixture() *platformv1alpha1.ServiceClass {
	return &platformv1alpha1.ServiceClass{
		ObjectMeta: metav1.ObjectMeta{Name: "namespace"},
		Spec: platformv1alpha1.ServiceClassSpec{
			DisplayName: "Namespace",
			Driver:      "kubernetes-namespace",
		},
	}
}

func namespaceServicePlanFixture() *platformv1alpha1.ServicePlan {
	return &platformv1alpha1.ServicePlan{
		ObjectMeta: metav1.ObjectMeta{Name: "namespace-team"},
		Spec: platformv1alpha1.ServicePlanSpec{
			ServiceClassRef: platformv1alpha1.LocalObjectReference{Name: "namespace"},
			DisplayName:     "Team Namespace",
			Topology:        "dedicated",
		},
	}
}

func namespaceServiceInstanceFixture() *platformv1alpha1.ServiceInstance {
	return &platformv1alpha1.ServiceInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "team-space"},
		Spec: platformv1alpha1.ServiceInstanceSpec{
			ProjectRef:      platformv1alpha1.LocalObjectReference{Name: "acme-prod"},
			ServiceClassRef: platformv1alpha1.LocalObjectReference{Name: "namespace"},
			ServicePlanRef:  platformv1alpha1.LocalObjectReference{Name: "namespace-team"},
			Exposure:        platformv1alpha1.ExposureSpec{Mode: platformv1alpha1.ExposureModeClusterInternal},
			SecretPolicy:    platformv1alpha1.SecretPolicySpec{DeliveryMode: platformv1alpha1.SecretDeliveryModeManual},
		},
		Status: platformv1alpha1.ServiceInstanceStatus{Placement: platformv1alpha1.PlacementStatus{ClusterName: "east-1", Namespace: "acme-prod-team-space"}},
	}
}

func namespaceQuotaFixture() *corev1.ResourceQuota {
	return &corev1.ResourceQuota{
		ObjectMeta: metav1.ObjectMeta{Name: "team-space-quota", Namespace: "acme-prod-team-space"},
		Spec: corev1.ResourceQuotaSpec{Hard: corev1.ResourceList{
			corev1.ResourceRequestsCPU:    resource.MustParse("2"),
			corev1.ResourceRequestsMemory: resource.MustParse("4Gi"),
			corev1.ResourcePods:           resource.MustParse("20"),
		}},
	}
}

func namespaceActionRequestFixture(name string, parameters map[string]any) *platformv1alpha1.ActionRequest {
	actionRequest := valkeyActionRequestFixture(name, string(adapters.ActionUpdateQuota), parameters)
	actionRequest.Spec.TargetRef.Name = "team-space"
	return actionRequest
}

func namespaceGrantAccessActionRequestFixture(name string, parameters map[string]any) *platformv1alpha1.ActionRequest {
	actionRequest := valkeyActionRequestFixture(name, string(adapters.ActionGrantAccess), parameters)
	actionRequest.Spec.TargetRef.Name = "team-space"
	return actionRequest
}
