package adapters

import (
	"context"
	"encoding/json"
	"fmt"

	platformv1alpha1 "github.com/sindef/servicer/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

const (
	kubeVirtRuntimeDriver = "kubevirt"
	kubeVirtRuntimeAPI    = "kubevirt.io/v1"
	kubeVirtRuntimeKind   = "VirtualMachine"
	kubeVirtPoolAPI       = "pool.kubevirt.io/v1alpha1"
	kubeVirtPoolKind      = "VirtualMachinePool"
	kubeVirtDataVolumeAPI = "cdi.kubevirt.io/v1beta1"
)

type kubeVirtParameters struct {
	Image             string                      `json:"image,omitempty"`
	CPU               string                      `json:"cpu,omitempty"`
	Memory            string                      `json:"memory,omitempty"`
	StorageClass      string                      `json:"storageClass,omitempty"`
	StorageSize       string                      `json:"storageSize,omitempty"`
	RunStrategy       string                      `json:"runStrategy,omitempty"`
	SSHAuthorizedKeys []string                    `json:"sshAuthorizedKeys,omitempty"`
	CloudInitUserData string                      `json:"cloudInitUserData,omitempty"`
	Networks          []kubeVirtNetworkParameters `json:"networks,omitempty"`
	Disks             []kubeVirtDiskParameters    `json:"disks,omitempty"`
	WorkloadType      string                      `json:"workloadType,omitempty"`
	PoolReplicas      int32                       `json:"poolReplicas,omitempty"`
}

type kubeVirtNetworkParameters struct {
	Name              string `json:"name,omitempty"`
	Type              string `json:"type,omitempty"`
	BindingMethod     string `json:"bindingMethod,omitempty"`
	MultusNetworkName string `json:"multusNetworkName,omitempty"`
	Model             string `json:"model,omitempty"`
}

type kubeVirtDiskParameters struct {
	Name         string `json:"name,omitempty"`
	Image        string `json:"image,omitempty"`
	Size         string `json:"size,omitempty"`
	StorageClass string `json:"storageClass,omitempty"`
	Bus          string `json:"bus,omitempty"`
}

// KubeVirtContract describes the normalized platform contract for virtual machines.
var KubeVirtContract = ProductContract{
	ServiceClass:            ServiceClassKubeVirt,
	FriendlyName:            "Virtual Machine",
	RuntimeDriver:           kubeVirtRuntimeDriver,
	SupportsVersionOverride: false,
	SupportsMultiCluster:    false,
	TopologyModes:           []string{"single-cluster"},
	StatusSignals: []StatusSignalDescriptor{
		{Key: "vm-ready", DisplayName: "VM Ready", Description: "Whether the KubeVirt VirtualMachine is ready.", Severity: HealthSeverityCritical},
		{Key: "guest-access", DisplayName: "Guest Access", Description: "Whether guest bootstrap and access material are configured.", Severity: HealthSeverityWarning},
		{Key: "storage-posture", DisplayName: "Storage Posture", Description: "Whether persistent VM storage intent is materialized.", Severity: HealthSeverityWarning},
	},
	Actions: []ActionCapability{},
}

type KubeVirtAdapter struct{}

func NewKubeVirtAdapter() *KubeVirtAdapter {
	return &KubeVirtAdapter{}
}

func (a *KubeVirtAdapter) Contract() ProductContract {
	return KubeVirtContract
}

func (a *KubeVirtAdapter) Validate(_ context.Context, request ValidationRequest) (ValidationResult, error) {
	issues := make([]ValidationIssue, 0)
	ctx := request.Context
	if ctx.Instance == nil {
		issues = append(issues, ValidationIssue{Path: "instance", Message: "service instance context is required", Severity: HealthSeverityCritical})
	}
	if ctx.Class == nil {
		issues = append(issues, ValidationIssue{Path: "serviceClassRef", Message: "service class is required", Severity: HealthSeverityCritical})
	}
	if ctx.Plan == nil {
		issues = append(issues, ValidationIssue{Path: "servicePlanRef", Message: "service plan is required", Severity: HealthSeverityCritical})
	}
	if len(issues) > 0 {
		return ValidationResult{Valid: false, Issues: issues}, nil
	}
	if ctx.Class.Spec.Driver != kubeVirtRuntimeDriver {
		issues = append(issues, ValidationIssue{Path: "serviceClass.driver", Message: fmt.Sprintf("expected driver %q, got %q", kubeVirtRuntimeDriver, ctx.Class.Spec.Driver), Severity: HealthSeverityCritical})
	}
	if ctx.Plan.Spec.ServiceClassRef.Name != ctx.Instance.Spec.ServiceClassRef.Name {
		issues = append(issues, ValidationIssue{Path: "servicePlanRef", Message: "service plan does not belong to the selected service class", Severity: HealthSeverityCritical})
	}
	if !containsString(a.Contract().TopologyModes, ctx.Plan.Spec.Topology) {
		issues = append(issues, ValidationIssue{Path: "servicePlan.topology", Message: fmt.Sprintf("topology %q is not supported for virtual machines", ctx.Plan.Spec.Topology), Severity: HealthSeverityCritical})
	}
	params, err := a.parameters(ctx)
	if err != nil {
		issues = append(issues, ValidationIssue{Path: "parameters", Message: err.Error(), Severity: HealthSeverityCritical})
	} else if params.Image == "" && !hasKubeVirtDiskImage(params.Disks) {
		issues = append(issues, ValidationIssue{Path: "parameters.image", Message: "virtual machine image is required", Severity: HealthSeverityCritical})
	} else if params.WorkloadType == "vmp" && params.PoolReplicas < 1 {
		issues = append(issues, ValidationIssue{Path: "parameters.poolReplicas", Message: "pool replicas must be at least 1 when workloadType=vmp", Severity: HealthSeverityCritical})
	}
	return ValidationResult{Valid: len(issues) == 0, Issues: issues}, nil
}

func (a *KubeVirtAdapter) Render(_ context.Context, request RenderRequest) (RenderResult, error) {
	ctx := request.Context
	if ctx.Instance == nil || ctx.Project == nil || ctx.Tenant == nil {
		return RenderResult{}, fmt.Errorf("tenant, project and instance context are required")
	}
	params, err := a.parameters(ctx)
	if err != nil {
		return RenderResult{}, err
	}
	namespace := instanceNamespace(ctx)
	clusterName := instanceCluster(ctx)
	basePath := fmt.Sprintf("clusters/%s/tenants/%s/projects/%s/services/%s", clusterName, ctx.Tenant.Name, ctx.Project.Name, ctx.Instance.Name)
	labels := map[string]string{
		"servicer.io/managed-by":       "servicer",
		"servicer.io/project":          ctx.Project.Name,
		"servicer.io/service-instance": ctx.Instance.Name,
		"servicer.io/runtime":          "kubevirt",
	}

	namespaceManifest := map[string]any{
		"apiVersion": "v1",
		"kind":       "Namespace",
		"metadata": map[string]any{
			"name": namespace,
			"labels": map[string]string{
				"servicer.io/managed-by": "servicer",
				"servicer.io/project":    ctx.Project.Name,
				"servicer.io/tenant":     ctx.Tenant.Name,
			},
		},
	}
	cloudInitName := fmt.Sprintf("%s-cloudinit", ctx.Instance.Name)
	cloudInitManifest := map[string]any{
		"apiVersion": "v1",
		"kind":       "Secret",
		"metadata": map[string]any{
			"name":      cloudInitName,
			"namespace": namespace,
			"labels":    labels,
		},
		"stringData": map[string]string{
			"userdata": a.cloudInitUserData(params),
		},
	}
	isPoolWorkload := params.WorkloadType == "vmp"
	vmDisks := make([]map[string]any, 0, len(params.Disks)+1)
	vmVolumes := make([]map[string]any, 0, len(params.Disks)+1)
	dataVolumeArtifacts := make([]RenderedArtifact, 0, len(params.Disks))
	dataVolumeTemplates := make([]map[string]any, 0, len(params.Disks))
	for _, disk := range params.Disks {
		dvName := fmt.Sprintf("%s-%s", ctx.Instance.Name, disk.Name)
		dvSpec := map[string]any{
			"source": map[string]any{
				"registry": map[string]string{"url": disk.Image},
			},
			"pvc": map[string]any{
				"accessModes": []string{"ReadWriteOnce"},
				"resources": map[string]any{
					"requests": map[string]string{"storage": disk.Size},
				},
			},
		}
		if disk.StorageClass != "" {
			dvSpec["pvc"].(map[string]any)["storageClassName"] = disk.StorageClass
		}
		if isPoolWorkload {
			dvName = disk.Name
			dataVolumeTemplates = append(dataVolumeTemplates, map[string]any{
				"metadata": map[string]any{
					"name": dvName,
				},
				"spec": dvSpec,
			})
		} else {
			dataVolumeManifest := map[string]any{
				"apiVersion": kubeVirtDataVolumeAPI,
				"kind":       "DataVolume",
				"metadata": map[string]any{
					"name":      dvName,
					"namespace": namespace,
					"labels":    labels,
					"annotations": map[string]string{
						"servicer.io/source-image": disk.Image,
					},
				},
				"spec": dvSpec,
			}
			content, err := yaml.Marshal(dataVolumeManifest)
			if err != nil {
				return RenderResult{}, err
			}
			dataVolumeArtifacts = append(dataVolumeArtifacts, RenderedArtifact{Path: fmt.Sprintf("%s/datavolume-%s.yaml", basePath, disk.Name), Content: content})
		}

		vmDisks = append(vmDisks, map[string]any{
			"name": disk.Name,
			"disk": map[string]any{
				"bus": disk.Bus,
			},
		})
		vmVolumes = append(vmVolumes, map[string]any{
			"name":       disk.Name,
			"dataVolume": map[string]string{"name": dvName},
		})
	}

	interfaces := make([]map[string]any, 0, len(params.Networks))
	networks := make([]map[string]any, 0, len(params.Networks))
	for _, network := range params.Networks {
		iface := map[string]any{"name": network.Name}
		switch network.BindingMethod {
		case "bridge":
			iface["bridge"] = map[string]any{}
		case "sriov":
			iface["sriov"] = map[string]any{}
		default:
			iface["masquerade"] = map[string]any{}
		}
		if network.Model != "" {
			iface["model"] = network.Model
		}
		interfaces = append(interfaces, iface)

		networkSpec := map[string]any{"name": network.Name}
		if network.Type == "multus" && network.MultusNetworkName != "" {
			networkSpec["multus"] = map[string]any{"networkName": network.MultusNetworkName}
		} else {
			networkSpec["pod"] = map[string]any{}
		}
		networks = append(networks, networkSpec)
	}

	vmDisks = append(vmDisks, map[string]any{"name": "cloudinit", "disk": map[string]any{"bus": "virtio"}})
	vmVolumes = append(vmVolumes, map[string]any{"name": "cloudinit", "cloudInitNoCloud": map[string]string{"secretRef": cloudInitName}})

	vmTemplateSpec := map[string]any{
		"runStrategy": params.RunStrategy,
		"template": map[string]any{
			"metadata": map[string]any{"labels": labels},
			"spec": map[string]any{
				"domain": map[string]any{
					"devices": map[string]any{
						"disks":      vmDisks,
						"interfaces": interfaces,
					},
					"resources": map[string]any{
						"requests": map[string]string{
							"cpu":    params.CPU,
							"memory": params.Memory,
						},
					},
				},
				"networks": networks,
				"volumes":  vmVolumes,
			},
		},
	}
	runtimeAPIVersion := kubeVirtRuntimeAPI
	runtimeKind := kubeVirtRuntimeKind
	runtimeManifestName := "virtualmachine.yaml"
	runtimeManifest := map[string]any{
		"apiVersion": kubeVirtRuntimeAPI,
		"kind":       kubeVirtRuntimeKind,
		"metadata": map[string]any{
			"name":      ctx.Instance.Name,
			"namespace": namespace,
			"labels":    labels,
			"annotations": map[string]string{
				"servicer.io/image": params.Image,
			},
		},
		"spec": vmTemplateSpec,
	}
	if params.WorkloadType == "vmp" {
		runtimeAPIVersion = kubeVirtPoolAPI
		runtimeKind = kubeVirtPoolKind
		runtimeManifestName = "virtualmachinepool.yaml"
		runtimeManifest = map[string]any{
			"apiVersion": kubeVirtPoolAPI,
			"kind":       kubeVirtPoolKind,
			"metadata": map[string]any{
				"name":      ctx.Instance.Name,
				"namespace": namespace,
				"labels": map[string]string{
					"servicer.io/managed-by":       "servicer",
					"servicer.io/project":          ctx.Project.Name,
					"servicer.io/service-instance": ctx.Instance.Name,
					"kubevirt.io/vmpool":           ctx.Instance.Name,
				},
				"annotations": map[string]string{
					"servicer.io/image": params.Image,
				},
			},
			"spec": map[string]any{
				"replicas": params.PoolReplicas,
				"selector": map[string]any{
					"matchLabels": map[string]string{
						"kubevirt.io/vmpool": ctx.Instance.Name,
					},
				},
				"virtualMachineTemplate": map[string]any{
					"metadata": map[string]any{
						"labels": map[string]string{
							"kubevirt.io/vmpool": ctx.Instance.Name,
						},
					},
					"spec": vmTemplateSpec,
				},
			},
		}
		runtimeManifest["spec"].(map[string]any)["virtualMachineTemplate"].(map[string]any)["spec"].(map[string]any)["dataVolumeTemplates"] = dataVolumeTemplates
	}

	artifacts := []RenderedArtifact{}
	for _, manifest := range []struct {
		name string
		body map[string]any
	}{
		{name: "namespace.yaml", body: namespaceManifest},
		{name: "cloudinit-secret.yaml", body: cloudInitManifest},
		{name: runtimeManifestName, body: runtimeManifest},
	} {
		content, err := yaml.Marshal(manifest.body)
		if err != nil {
			return RenderResult{}, err
		}
		artifacts = append(artifacts, RenderedArtifact{Path: fmt.Sprintf("%s/%s", basePath, manifest.name), Content: content})
	}
	artifacts = append(artifacts, dataVolumeArtifacts...)

	return RenderResult{
		RuntimeDriver: kubeVirtRuntimeDriver,
		PrimaryResource: &platformv1alpha1.TypedObjectReference{
			APIVersion: runtimeAPIVersion,
			Kind:       runtimeKind,
			Name:       ctx.Instance.Name,
			Namespace:  namespace,
		},
		PackagePath: basePath,
		Endpoints: []Endpoint{
			{Name: "pod-network", Address: fmt.Sprintf("%s.%s", ctx.Instance.Name, namespace), Protocol: "tcp", Visibility: EndpointVisibilityClusterInternal},
		},
		Artifacts: artifacts,
	}, nil
}

func (a *KubeVirtAdapter) Observe(_ context.Context, request ObserveRequest) (NormalizedStatus, error) {
	ctx := request.Context
	if ctx.Instance == nil {
		return NormalizedStatus{}, fmt.Errorf("service instance context is required")
	}
	params, err := a.parameters(ctx)
	if err != nil {
		return NormalizedStatus{}, err
	}
	expectedAPI := kubeVirtRuntimeAPI
	expectedKind := kubeVirtRuntimeKind
	workloadLabel := "VirtualMachine"
	if params.WorkloadType == "vmp" {
		expectedAPI = kubeVirtPoolAPI
		expectedKind = kubeVirtPoolKind
		workloadLabel = "VirtualMachinePool"
	}
	phase := "Provisioning"
	summary := fmt.Sprintf("Waiting for KubeVirt %s readiness.", workloadLabel)
	if request.Runtime.Blocked {
		phase = "Blocked"
		summary = firstNonEmptyTrimmedAdapter(request.Runtime.Message, "KubeVirt runtime dependency is missing in the target cluster.")
	}
	if runtimeResourceObserved(request.ObservedResources, expectedAPI, expectedKind, ctx.Instance.Name) {
		phase = "Ready"
		summary = fmt.Sprintf("KubeVirt %s is materialized.", workloadLabel)
	}
	return NormalizedStatus{
		Phase:             phase,
		Summary:           summary,
		Conditions:        append([]metav1.Condition(nil), ctx.Instance.Status.Conditions...),
		ObservedResources: append([]platformv1alpha1.TypedObjectReference(nil), request.ObservedResources...),
		Sync: SyncStatus{
			Phase:            SyncPhaseMaterialized,
			ArtifactRevision: request.ArtifactRevision,
			ApplicationName:  request.ApplicationName,
			Message:          "Waiting for Argo CD reconciliation.",
		},
		HealthSignals: []HealthSignal{
			{Key: "vm-ready", Status: phase, Severity: HealthSeverityCritical, Message: summary},
			{Key: "guest-access", Status: "Configured", Severity: HealthSeverityInfo, Message: "Cloud-init bootstrap material is rendered."},
			{Key: "storage-posture", Status: "Configured", Severity: HealthSeverityInfo, Message: "Persistent DataVolume root disk intent is materialized."},
		},
	}, nil
}

func (a *KubeVirtAdapter) Delete(_ context.Context, request DeleteRequest) (DeleteResult, error) {
	if request.Context.Instance == nil {
		return DeleteResult{}, fmt.Errorf("service instance context is required")
	}
	return DeleteResult{Message: "Delete requested for KubeVirt virtual machine."}, nil
}

func (a *KubeVirtAdapter) SupportedActions(_ context.Context, _ ServiceContext) []ActionCapability {
	return append([]ActionCapability(nil), a.Contract().Actions...)
}

func (a *KubeVirtAdapter) ExecuteAction(_ context.Context, request ExecuteActionRequest) (ActionExecutionResult, error) {
	if request.Context.Instance == nil || request.Action == nil {
		return ActionExecutionResult{}, fmt.Errorf("service instance and action request context are required")
	}
	return ActionExecutionResult{Phase: "Queued", Message: fmt.Sprintf("Virtual machine action %q queued for KubeVirt reconciliation.", request.Action.Spec.Action), Retryable: true}, nil
}

func (a *KubeVirtAdapter) parameters(ctx ServiceContext) (kubeVirtParameters, error) {
	params := kubeVirtParameters{}
	if ctx.Plan != nil && ctx.Plan.Spec.DefaultParameters != nil {
		if err := json.Unmarshal(ctx.Plan.Spec.DefaultParameters.Raw, &params); err != nil {
			return kubeVirtParameters{}, fmt.Errorf("decode plan defaults: %w", err)
		}
	}
	if ctx.Instance != nil && ctx.Instance.Spec.Parameters != nil {
		if err := json.Unmarshal(ctx.Instance.Spec.Parameters.Raw, &params); err != nil {
			return kubeVirtParameters{}, fmt.Errorf("decode instance parameters: %w", err)
		}
	}
	if params.CPU == "" {
		params.CPU = "1"
	}
	if params.Memory == "" {
		params.Memory = "1Gi"
	}
	if params.StorageSize == "" {
		params.StorageSize = "20Gi"
	}
	if params.RunStrategy == "" {
		params.RunStrategy = "RerunOnFailure"
	}
	if params.WorkloadType == "" {
		params.WorkloadType = "vm"
	}
	if params.WorkloadType != "vm" && params.WorkloadType != "vmp" {
		params.WorkloadType = "vm"
	}
	if params.PoolReplicas < 1 {
		params.PoolReplicas = 1
	}
	if len(params.Disks) == 0 && params.Image != "" {
		params.Disks = []kubeVirtDiskParameters{{
			Name:         "rootdisk",
			Image:        params.Image,
			Size:         params.StorageSize,
			StorageClass: params.StorageClass,
			Bus:          "virtio",
		}}
	}
	validDisks := make([]kubeVirtDiskParameters, 0, len(params.Disks))
	for idx, disk := range params.Disks {
		if disk.Name == "" {
			if idx == 0 {
				disk.Name = "rootdisk"
			} else {
				disk.Name = fmt.Sprintf("disk-%d", idx+1)
			}
		}
		if disk.Image == "" {
			disk.Image = params.Image
		}
		if disk.Size == "" {
			disk.Size = params.StorageSize
		}
		if disk.StorageClass == "" {
			disk.StorageClass = params.StorageClass
		}
		if disk.Bus == "" {
			disk.Bus = "virtio"
		}
		if disk.Image == "" {
			continue
		}
		validDisks = append(validDisks, disk)
	}
	params.Disks = validDisks
	if params.Image == "" && len(params.Disks) > 0 {
		params.Image = params.Disks[0].Image
	}
	if len(params.Networks) == 0 {
		params.Networks = []kubeVirtNetworkParameters{{Name: "default", Type: "pod", BindingMethod: "masquerade", Model: "virtio"}}
	}
	for idx := range params.Networks {
		if params.Networks[idx].Name == "" {
			if idx == 0 {
				params.Networks[idx].Name = "default"
			} else {
				params.Networks[idx].Name = fmt.Sprintf("net-%d", idx+1)
			}
		}
		if params.Networks[idx].Type == "" {
			params.Networks[idx].Type = "pod"
		}
		if params.Networks[idx].BindingMethod == "" {
			params.Networks[idx].BindingMethod = "masquerade"
		}
		if params.Networks[idx].Model == "" {
			params.Networks[idx].Model = "virtio"
		}
		if params.Networks[idx].Type == "multus" && params.Networks[idx].MultusNetworkName == "" {
			params.Networks[idx].Type = "pod"
		}
	}
	return params, nil
}

func hasKubeVirtDiskImage(disks []kubeVirtDiskParameters) bool {
	for _, disk := range disks {
		if disk.Image != "" {
			return true
		}
	}
	return false
}

func (a *KubeVirtAdapter) cloudInitUserData(params kubeVirtParameters) string {
	if params.CloudInitUserData != "" {
		return params.CloudInitUserData
	}
	userData := "#cloud-config\n"
	if len(params.SSHAuthorizedKeys) > 0 {
		userData += "ssh_authorized_keys:\n"
		for _, key := range params.SSHAuthorizedKeys {
			userData += fmt.Sprintf("  - %s\n", key)
		}
	}
	return userData
}
