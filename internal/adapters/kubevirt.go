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
	kubeVirtDataVolumeAPI = "cdi.kubevirt.io/v1beta1"
)

type kubeVirtParameters struct {
	Image             string   `json:"image,omitempty"`
	CPU               string   `json:"cpu,omitempty"`
	Memory            string   `json:"memory,omitempty"`
	StorageClass      string   `json:"storageClass,omitempty"`
	StorageSize       string   `json:"storageSize,omitempty"`
	RunStrategy       string   `json:"runStrategy,omitempty"`
	SSHAuthorizedKeys []string `json:"sshAuthorizedKeys,omitempty"`
	CloudInitUserData string   `json:"cloudInitUserData,omitempty"`
}

// KubeVirtContract describes the normalized platform contract for virtual machines.
var KubeVirtContract = ProductContract{
	ServiceClass:            ServiceClassKubeVirt,
	FriendlyName:            "Virtual Machine",
	RuntimeDriver:           kubeVirtRuntimeDriver,
	SupportsVersionOverride: false,
	SupportsMultiCluster:    false,
	TopologyModes:           []string{"single-vm"},
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
	} else if params.Image == "" {
		issues = append(issues, ValidationIssue{Path: "parameters.image", Message: "virtual machine image is required", Severity: HealthSeverityCritical})
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
	rootDiskName := fmt.Sprintf("%s-rootdisk", ctx.Instance.Name)
	dataVolumeManifest := map[string]any{
		"apiVersion": kubeVirtDataVolumeAPI,
		"kind":       "DataVolume",
		"metadata": map[string]any{
			"name":      rootDiskName,
			"namespace": namespace,
			"labels":    labels,
			"annotations": map[string]string{
				"servicer.io/source-image": params.Image,
			},
		},
		"spec": map[string]any{
			"source": map[string]any{
				"registry": map[string]string{"url": params.Image},
			},
			"pvc": map[string]any{
				"accessModes": []string{"ReadWriteOnce"},
				"resources": map[string]any{
					"requests": map[string]string{"storage": params.StorageSize},
				},
			},
		},
	}
	if params.StorageClass != "" {
		dataVolumeManifest["spec"].(map[string]any)["pvc"].(map[string]any)["storageClassName"] = params.StorageClass
	}
	vmManifest := map[string]any{
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
		"spec": map[string]any{
			"runStrategy": params.RunStrategy,
			"template": map[string]any{
				"metadata": map[string]any{"labels": labels},
				"spec": map[string]any{
					"domain": map[string]any{
						"devices": map[string]any{
							"disks": []map[string]any{
								{"name": "rootdisk", "disk": map[string]any{"bus": "virtio"}},
								{"name": "cloudinit", "disk": map[string]any{"bus": "virtio"}},
							},
							"interfaces": []map[string]any{{"name": "default", "masquerade": map[string]any{}}},
						},
						"resources": map[string]any{
							"requests": map[string]string{
								"cpu":    params.CPU,
								"memory": params.Memory,
							},
						},
					},
					"networks": []map[string]any{{"name": "default", "pod": map[string]any{}}},
					"volumes": []map[string]any{
						{"name": "rootdisk", "dataVolume": map[string]string{"name": rootDiskName}},
						{"name": "cloudinit", "cloudInitNoCloud": map[string]string{"secretRef": cloudInitName}},
					},
				},
			},
		},
	}

	artifacts := []RenderedArtifact{}
	for _, manifest := range []struct {
		name string
		body map[string]any
	}{
		{name: "namespace.yaml", body: namespaceManifest},
		{name: "cloudinit-secret.yaml", body: cloudInitManifest},
		{name: "datavolume-rootdisk.yaml", body: dataVolumeManifest},
		{name: "virtualmachine.yaml", body: vmManifest},
	} {
		content, err := yaml.Marshal(manifest.body)
		if err != nil {
			return RenderResult{}, err
		}
		artifacts = append(artifacts, RenderedArtifact{Path: fmt.Sprintf("%s/%s", basePath, manifest.name), Content: content})
	}

	return RenderResult{
		RuntimeDriver: kubeVirtRuntimeDriver,
		PrimaryResource: &platformv1alpha1.TypedObjectReference{
			APIVersion: kubeVirtRuntimeAPI,
			Kind:       kubeVirtRuntimeKind,
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
	phase := "Provisioning"
	summary := "Waiting for KubeVirt VirtualMachine readiness."
	if request.Runtime.Blocked {
		phase = "Blocked"
		summary = firstNonEmptyTrimmedAdapter(request.Runtime.Message, "KubeVirt runtime dependency is missing in the target cluster.")
	}
	if runtimeResourceObserved(request.ObservedResources, kubeVirtRuntimeAPI, kubeVirtRuntimeKind, ctx.Instance.Name) {
		phase = "Ready"
		summary = "KubeVirt VirtualMachine is materialized."
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
	return params, nil
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
