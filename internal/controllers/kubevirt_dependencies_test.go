package controllers

import (
	"context"
	"strings"
	"testing"

	platformv1alpha1 "github.com/sindef/servicer/api/v1alpha1"
	"github.com/sindef/servicer/internal/adapters"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestKubeVirtDependenciesReadyRequiresBothCRDs(t *testing.T) {
	scheme := inventoryTestScheme(t)
	tests := []struct {
		name        string
		objects     []client.Object
		wantReady   bool
		wantMessage string
	}{
		{
			name:        "missing all CRDs",
			objects:     nil,
			wantReady:   false,
			wantMessage: kubeVirtVirtualMachineCRDName,
		},
		{
			name: "missing DataVolume CRD",
			objects: []client.Object{
				crdObject(kubeVirtVirtualMachineCRDName),
			},
			wantReady:   false,
			wantMessage: kubeVirtDataVolumeCRDName,
		},
		{
			name: "all dependencies present",
			objects: []client.Object{
				crdObject(kubeVirtVirtualMachineCRDName),
				crdObject(kubeVirtDataVolumeCRDName),
			},
			wantReady: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			builder := fake.NewClientBuilder().WithScheme(scheme)
			if len(tc.objects) > 0 {
				builder = builder.WithObjects(tc.objects...)
			}
			c := builder.Build()

			ready, message, err := kubeVirtDependenciesReady(context.Background(), c)
			if err != nil {
				t.Fatalf("kubeVirtDependenciesReady returned error: %v", err)
			}
			if ready != tc.wantReady {
				t.Fatalf("expected ready=%t, got %t (message=%q)", tc.wantReady, ready, message)
			}
			if tc.wantMessage != "" && !strings.Contains(message, tc.wantMessage) {
				t.Fatalf("expected message to mention %q, got %q", tc.wantMessage, message)
			}
		})
	}
}

func TestObserveRuntimeBlocksWhenKubeVirtDependenciesMissing(t *testing.T) {
	scheme := inventoryTestScheme(t)
	reconciler := &ServiceInstanceReconciler{
		Client: fake.NewClientBuilder().WithScheme(scheme).Build(),
		Scheme: scheme,
	}
	targetClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	ref := &platformv1alpha1.TypedObjectReference{
		APIVersion: "kubevirt.io/v1",
		Kind:       "VirtualMachine",
		Name:       "devbox",
		Namespace:  "acme-prod-devbox",
	}

	observation, observedResources, err := reconciler.observeRuntime(context.Background(), ref, nil, targetClient)
	if err != nil {
		t.Fatalf("observeRuntime returned error: %v", err)
	}
	if !observation.Blocked {
		t.Fatalf("expected runtime observation to be blocked")
	}
	if !strings.Contains(observation.Message, kubeVirtVirtualMachineCRDName) {
		t.Fatalf("expected blocked message to mention missing CRD, got %q", observation.Message)
	}
	if len(observedResources) != 0 {
		t.Fatalf("expected no observed resources when dependencies are missing, got %#v", observedResources)
	}
}

func TestServiceInstanceReconcilerBlocksKubeVirtWithoutClusterTarget(t *testing.T) {
	scheme := inventoryTestScheme(t)
	registry, err := adapters.NewRegistry(adapters.NewKubeVirtAdapter())
	if err != nil {
		t.Fatalf("NewRegistry returned error: %v", err)
	}

	tenant := &platformv1alpha1.Tenant{
		ObjectMeta: metav1.ObjectMeta{Name: "acme"},
		Spec: platformv1alpha1.TenantSpec{
			DisplayName:           "Acme",
			Owners:                platformv1alpha1.OwnersSpec{Users: []string{"owner@example.com"}},
			QuotaProfileRef:       platformv1alpha1.LocalObjectReference{Name: "standard"},
			AllowedServiceClasses: []string{"virtual-machine"},
			Lifecycle:             platformv1alpha1.TenantLifecycleSpec{Phase: platformv1alpha1.TenantLifecyclePhaseActive},
		},
	}
	project := &platformv1alpha1.Project{
		ObjectMeta: metav1.ObjectMeta{Name: "acme-prod", Generation: 1},
		Spec: platformv1alpha1.ProjectSpec{
			TenantRef:         platformv1alpha1.LocalObjectReference{Name: "acme"},
			DisplayName:       "Acme Prod",
			Environment:       platformv1alpha1.EnvironmentProduction,
			NamespaceStrategy: platformv1alpha1.NamespaceStrategySpec{Mode: platformv1alpha1.NamespaceStrategyDedicated, Prefix: "acme-prod"},
		},
		Status: platformv1alpha1.ProjectStatus{
			ObservedGeneration: 1,
			Phase:              "Ready",
			Placement:          platformv1alpha1.PlacementStatus{ClusterName: "remote-a"},
			Conditions: []metav1.Condition{
				{Type: "Ready", Status: metav1.ConditionTrue, ObservedGeneration: 1, Reason: "ProjectReady", Message: "ready"},
			},
		},
	}
	serviceClass := &platformv1alpha1.ServiceClass{
		ObjectMeta: metav1.ObjectMeta{Name: "virtual-machine", Generation: 1},
		Spec: platformv1alpha1.ServiceClassSpec{
			DisplayName: "Virtual Machine",
			Driver:      kubeVirtServiceClassDriver,
			Published:   true,
		},
		Status: platformv1alpha1.ServiceClassStatus{
			ObservedGeneration: 1,
			Published:          true,
			Conditions: []metav1.Condition{
				{Type: "Accepted", Status: metav1.ConditionTrue, ObservedGeneration: 1, Reason: "ValidationSucceeded", Message: "accepted"},
			},
		},
	}
	servicePlan := &platformv1alpha1.ServicePlan{
		ObjectMeta: metav1.ObjectMeta{Name: "virtual-machine-dev", Generation: 1},
		Spec: platformv1alpha1.ServicePlanSpec{
			ServiceClassRef: platformv1alpha1.LocalObjectReference{Name: "virtual-machine"},
			DisplayName:     "Development VM",
			Topology:        "single-vm",
		},
		Status: platformv1alpha1.ServicePlanStatus{
			ObservedGeneration: 1,
			Published:          true,
			Conditions: []metav1.Condition{
				{Type: "Accepted", Status: metav1.ConditionTrue, ObservedGeneration: 1, Reason: "ValidationSucceeded", Message: "accepted"},
			},
		},
	}
	instance := &platformv1alpha1.ServiceInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "devbox"},
		Spec: platformv1alpha1.ServiceInstanceSpec{
			ProjectRef:      platformv1alpha1.LocalObjectReference{Name: "acme-prod"},
			ServiceClassRef: platformv1alpha1.LocalObjectReference{Name: "virtual-machine"},
			ServicePlanRef:  platformv1alpha1.LocalObjectReference{Name: "virtual-machine-dev"},
			Parameters:      rawJSON(t, map[string]any{"image": "quay.io/containerdisks/ubuntu:22.04"}),
			Exposure:        platformv1alpha1.ExposureSpec{Mode: platformv1alpha1.ExposureModeClusterInternal},
			SecretPolicy:    platformv1alpha1.SecretPolicySpec{DeliveryMode: platformv1alpha1.SecretDeliveryModeDirectSecretRef},
		},
	}

	reconciler := &ServiceInstanceReconciler{
		Client: fake.NewClientBuilder().
			WithScheme(scheme).
			WithStatusSubresource(&platformv1alpha1.ServiceInstance{}).
			WithObjects(tenant, project, serviceClass, servicePlan, instance).
			Build(),
		Scheme:   scheme,
		Adapters: registry,
	}

	for i := 0; i < 2; i++ {
		if _, err := reconciler.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKey{Name: "devbox"}}); err != nil {
			t.Fatalf("reconcile %d returned error: %v", i+1, err)
		}
	}

	var updated platformv1alpha1.ServiceInstance
	if err := reconciler.Get(context.Background(), client.ObjectKey{Name: "devbox"}, &updated); err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if updated.Status.Phase != "PendingDependencies" {
		t.Fatalf("expected phase PendingDependencies, got %q", updated.Status.Phase)
	}
	if !conditionMessagesContain(updated.Status.Conditions, "KubeVirt provisioning requires placement on a reachable remote ClusterTarget.") {
		t.Fatalf("expected KubeVirt placement dependency message, got %#v", updated.Status.Conditions)
	}
}

func crdObject(name string) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "apiextensions.k8s.io/v1",
			"kind":       "CustomResourceDefinition",
			"metadata": map[string]any{
				"name": name,
			},
		},
	}
}
