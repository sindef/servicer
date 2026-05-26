package adapters

import (
	"context"
	"strings"
	"testing"

	platformv1alpha1 "github.com/sindef/servicer/api/v1alpha1"
)

func TestKubeVirtAdapterValidateRequiresImage(t *testing.T) {
	adapter := NewKubeVirtAdapter()
	ctx := sampleKubeVirtContext(t)
	ctx.Instance.Spec.Parameters = nil
	ctx.Plan.Spec.DefaultParameters = rawJSON(t, map[string]any{
		"cpu":    "1",
		"memory": "1Gi",
	})

	result, err := adapter.Validate(context.Background(), ValidationRequest{Context: ctx})
	if err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}
	if result.Valid {
		t.Fatalf("expected validation to fail without image")
	}
	if len(result.Issues) == 0 || result.Issues[0].Path != "parameters.image" {
		t.Fatalf("expected image validation issue, got %#v", result.Issues)
	}
}

func TestKubeVirtAdapterRenderProducesVirtualMachineManifest(t *testing.T) {
	adapter := NewKubeVirtAdapter()
	ctx := sampleKubeVirtContext(t)

	result, err := adapter.Render(context.Background(), RenderRequest{Context: ctx})
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}
	if result.RuntimeDriver != kubeVirtRuntimeDriver {
		t.Fatalf("expected runtime driver %q, got %q", kubeVirtRuntimeDriver, result.RuntimeDriver)
	}
	if result.PrimaryResource == nil || result.PrimaryResource.Kind != kubeVirtRuntimeKind {
		t.Fatalf("expected VirtualMachine primary resource, got %#v", result.PrimaryResource)
	}
	rendered := renderedArtifacts(result)
	for _, expected := range []string{
		"kind: VirtualMachine",
		"kind: DataVolume",
		"apiVersion: kubevirt.io/v1",
		"apiVersion: cdi.kubevirt.io/v1beta1",
		"runStrategy: Always",
		"url: quay.io/containerdisks/ubuntu:22.04",
		"storage: 20Gi",
		"dataVolume:",
		"name: devbox-rootdisk",
		"secretRef: devbox-cloudinit",
		"cpu: \"2\"",
		"memory: 4Gi",
		"ssh_authorized_keys:",
	} {
		if !strings.Contains(rendered, expected) {
			t.Fatalf("expected rendered output to contain %q:\n%s", expected, rendered)
		}
	}
}

func TestKubeVirtAdapterRenderSupportsCustomNetworksAndDisks(t *testing.T) {
	adapter := NewKubeVirtAdapter()
	ctx := sampleKubeVirtContext(t)
	ctx.Instance.Spec.Parameters = rawJSON(t, map[string]any{
		"cpu":         "4",
		"memory":      "8Gi",
		"runStrategy": "Manual",
		"networks": []map[string]any{
			{"name": "default", "type": "pod", "bindingMethod": "masquerade"},
			{"name": "vlan220", "type": "multus", "bindingMethod": "bridge", "multusNetworkName": "default/vlan220"},
		},
		"disks": []map[string]any{
			{"name": "os", "image": "quay.io/containerdisks/fedora:39", "size": "30Gi", "bus": "virtio"},
			{"name": "data", "image": "quay.io/containerdisks/fedora:39", "size": "100Gi", "storageClass": "fast-ssd", "bus": "scsi"},
		},
	})

	result, err := adapter.Render(context.Background(), RenderRequest{Context: ctx})
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}
	rendered := renderedArtifacts(result)
	for _, expected := range []string{
		"networkName: default/vlan220",
		"name: vlan220",
		"bridge:",
		"name: os",
		"name: data",
		"storageClassName: fast-ssd",
		"storage: 100Gi",
	} {
		if !strings.Contains(rendered, expected) {
			t.Fatalf("expected rendered output to contain %q:\n%s", expected, rendered)
		}
	}
}

func TestKubeVirtAdapterRenderSupportsVirtualMachinePool(t *testing.T) {
	adapter := NewKubeVirtAdapter()
	ctx := sampleKubeVirtContext(t)
	ctx.Instance.Spec.Parameters = rawJSON(t, map[string]any{
		"workloadType": "vmp",
		"poolReplicas": 3,
		"cpu":          "2",
		"memory":       "4Gi",
		"runStrategy":  "Always",
		"networks": []map[string]any{
			{"name": "default", "type": "pod", "bindingMethod": "masquerade"},
		},
		"disks": []map[string]any{
			{"name": "rootdisk", "image": "quay.io/containerdisks/ubuntu:22.04", "size": "20Gi", "bus": "virtio"},
		},
	})

	result, err := adapter.Render(context.Background(), RenderRequest{Context: ctx})
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}
	if result.PrimaryResource == nil || result.PrimaryResource.Kind != kubeVirtPoolKind {
		t.Fatalf("expected primary kind %q, got %#v", kubeVirtPoolKind, result.PrimaryResource)
	}
	rendered := renderedArtifacts(result)
	for _, expected := range []string{
		"kind: VirtualMachinePool",
		"apiVersion: pool.kubevirt.io/v1alpha1",
		"replicas: 3",
		"kubevirt.io/vmpool: devbox",
		"virtualMachineTemplate:",
		"dataVolumeTemplates:",
	} {
		if !strings.Contains(rendered, expected) {
			t.Fatalf("expected rendered output to contain %q:\n%s", expected, rendered)
		}
	}
}

func sampleKubeVirtContext(t *testing.T) ServiceContext {
	t.Helper()
	ctx := samplePostgreSQLContext(t)
	ctx.Class = &platformv1alpha1.ServiceClass{
		Spec: platformv1alpha1.ServiceClassSpec{
			DisplayName: "Virtual Machine",
			Driver:      kubeVirtRuntimeDriver,
			Published:   true,
		},
	}
	ctx.Plan = &platformv1alpha1.ServicePlan{
		Spec: platformv1alpha1.ServicePlanSpec{
			ServiceClassRef: platformv1alpha1.LocalObjectReference{Name: "virtual-machine"},
			DisplayName:     "Virtual Machine",
			Topology:        "single-cluster",
			DefaultParameters: rawJSON(t, map[string]any{
				"cpu":         "1",
				"memory":      "1Gi",
				"storageSize": "20Gi",
			}),
		},
	}
	ctx.Instance.ObjectMeta = metav1ObjectMeta("devbox")
	ctx.Instance.Spec.ServiceClassRef = platformv1alpha1.LocalObjectReference{Name: "virtual-machine"}
	ctx.Instance.Spec.ServicePlanRef = platformv1alpha1.LocalObjectReference{Name: "virtual-machine-dev"}
	ctx.Instance.Spec.Parameters = rawJSON(t, map[string]any{
		"image":             "quay.io/containerdisks/ubuntu:22.04",
		"cpu":               "2",
		"memory":            "4Gi",
		"runStrategy":       "Always",
		"sshAuthorizedKeys": []string{"ssh-ed25519 AAAATEST dev@example.com"},
	})
	ctx.Instance.Status.Placement.Namespace = "acme-prod-devbox"
	return ctx
}
