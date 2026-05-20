package controllers

import (
	"encoding/json"

	platformv1alpha1 "github.com/sindef/servicer/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/sindef/servicer/internal/adapters"
)

func newMySQLActionRequestReconciler(t testLike, instance *platformv1alpha1.ServiceInstance, actionRequest *platformv1alpha1.ActionRequest) *ActionRequestReconciler {
	scheme := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(scheme)
	_ = platformv1alpha1.AddToScheme(scheme)
	registry, _ := adapters.NewRegistry(adapters.NewMySQLAdapter())
	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&platformv1alpha1.ActionRequest{}).
		WithStatusSubresource(&platformv1alpha1.ServiceInstance{}).
		WithObjects(
			mysqlTenantFixture(),
			mysqlProjectFixture(),
			mysqlClusterTargetFixture(),
			mysqlServiceClassFixture(),
			mysqlServicePlanFixture(),
			instance,
			mysqlStatefulSetFixture(),
			mysqlSecretFixture(),
			actionRequest,
		).
		Build()
	return &ActionRequestReconciler{Client: client, Scheme: scheme, Adapters: registry}
}

type testLike interface {
	Helper()
	Fatalf(string, ...any)
}

func mysqlTenantFixture() *platformv1alpha1.Tenant {
	tenant := tenantFixture()
	tenant.Spec.AllowedServiceClasses = []string{"mysql"}
	return tenant
}

func mysqlProjectFixture() *platformv1alpha1.Project {
	return projectFixture()
}

func mysqlClusterTargetFixture() *platformv1alpha1.ClusterTarget {
	return clusterTargetFixture()
}

func mysqlServiceClassFixture() *platformv1alpha1.ServiceClass {
	return &platformv1alpha1.ServiceClass{
		ObjectMeta: metav1.ObjectMeta{Name: "mysql"},
		Spec: platformv1alpha1.ServiceClassSpec{
			DisplayName:           "MySQL",
			Driver:                "servicer-mysql",
			SupportedVersions:     []string{"8.4", "8.0"},
			AllowsVersionOverride: true,
		},
	}
}

func mysqlServicePlanFixture() *platformv1alpha1.ServicePlan {
	return &platformv1alpha1.ServicePlan{
		ObjectMeta: metav1.ObjectMeta{Name: "mysql-active-passive"},
		Spec: platformv1alpha1.ServicePlanSpec{
			ServiceClassRef:       platformv1alpha1.LocalObjectReference{Name: "mysql"},
			DisplayName:           "Multi-Region Active-Passive",
			Topology:              "multi-region",
			DefaultVersion:        "8.4",
			AllowsVersionOverride: true,
		},
	}
}

func mysqlServiceInstanceFixture() *platformv1alpha1.ServiceInstance {
	params, _ := json.Marshal(map[string]any{
		"replicationMode": "active-passive",
		"primaryCluster":  "east-1",
		"standbyClusters": []string{"west-2"},
		"databaseName":    "orders mysql",
	})
	return &platformv1alpha1.ServiceInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "orders-mysql"},
		Spec: platformv1alpha1.ServiceInstanceSpec{
			ProjectRef:      platformv1alpha1.LocalObjectReference{Name: "acme-prod"},
			ServiceClassRef: platformv1alpha1.LocalObjectReference{Name: "mysql"},
			ServicePlanRef:  platformv1alpha1.LocalObjectReference{Name: "mysql-active-passive"},
			Version:         "8.4",
			Parameters:      &apiextensionsv1.JSON{Raw: params},
			Exposure:        platformv1alpha1.ExposureSpec{Mode: platformv1alpha1.ExposureModeClusterInternal},
			SecretPolicy:    platformv1alpha1.SecretPolicySpec{DeliveryMode: platformv1alpha1.SecretDeliveryModeExternalSecret},
		},
		Status: platformv1alpha1.ServiceInstanceStatus{Placement: platformv1alpha1.PlacementStatus{ClusterName: "east-1", Namespace: "acme-prod-orders-mysql"}},
	}
}

func mysqlStatefulSetFixture() *appsv1.StatefulSet {
	replicas := int32(1)
	return &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "orders-mysql", Namespace: "acme-prod-orders-mysql"},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas,
			Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{}}},
		},
	}
}

func mysqlSecretFixture() *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "orders-mysql-auth", Namespace: "acme-prod-orders-mysql"},
		Data: map[string][]byte{
			"username": []byte("orders_mysql"),
			"password": []byte("old-password"),
			"database": []byte("orders_mysql"),
		},
	}
}
