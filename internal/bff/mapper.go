package bff

import (
	"encoding/json"
	"sort"
	"strings"

	platformv1alpha1 "github.com/sindef/servicer/api/v1alpha1"
	"github.com/sindef/servicer/internal/adapters"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func projectTenantMap(projects []platformv1alpha1.Project) map[string]string {
	result := map[string]string{}
	for _, project := range projects {
		result[project.Name] = project.Spec.TenantRef.Name
	}
	return result
}

func summarizeInstance(instance platformv1alpha1.ServiceInstance, projectTenants map[string]string, classes map[string]platformv1alpha1.ServiceClass, plans map[string]platformv1alpha1.ServicePlan) InstanceSummary {
	class := classes[instance.Spec.ServiceClassRef.Name]
	plan := plans[instance.Spec.ServicePlanRef.Name]
	productName := serviceClassDisplayName(instance.Spec.ServiceClassRef.Name, class.Spec.DisplayName)
	if productName == "" {
		if contract, ok := adapters.KnownContract(adapters.ServiceClass(instance.Spec.ServiceClassRef.Name)); ok {
			productName = contract.FriendlyName
		}
	}
	return InstanceSummary{
		Name:         instance.Name,
		DisplayName:  instance.Name,
		ProjectName:  instance.Spec.ProjectRef.Name,
		TenantName:   projectTenants[instance.Spec.ProjectRef.Name],
		ProductClass: instance.Spec.ServiceClassRef.Name,
		ProductName:  displayName(productName, instance.Spec.ServiceClassRef.Name),
		PlanName:     instance.Spec.ServicePlanRef.Name,
		PlanDisplay:  displayName(plan.Spec.DisplayName, instance.Spec.ServicePlanRef.Name),
		Phase:        instance.Status.Phase,
		Health:       instance.Status.Health.Summary,
		ClusterName:  instance.Status.Placement.ClusterName,
		Namespace:    instance.Status.Placement.Namespace,
		SyncPhase:    instance.Status.Sync.Phase,
		Endpoints:    liveEndpoints(instance),
	}
}

func liveEndpoints(instance platformv1alpha1.ServiceInstance) map[string]string {
	if instance.Status.Phase == "Blocked" {
		return nil
	}
	if instance.Status.Runtime.ObjectRef == nil {
		return nil
	}
	if instance.Status.Phase != "Ready" && instance.Status.Sync.Phase != string(adapters.SyncPhaseSynced) {
		return nil
	}
	return copyStringMap(instance.Status.Endpoints)
}

func productDescription(name string) string {
	switch name {
	case string(adapters.ServiceClassNamespace):
		return "Managed Kubernetes namespace with quota and baseline policy."
	case string(adapters.ServiceClassPostgreSQL):
		return "CloudNativePG-backed PostgreSQL with backup and failover workflows."
	case string(adapters.ServiceClassMySQL):
		return "Servicer-owned MySQL with single-cluster HA, multi-region Galera, and active-passive failover."
	case string(adapters.ServiceClassValkey):
		return "Servicer-owned Valkey cache runtime with credential and failover posture."
	case string(adapters.ServiceClassNATS):
		return "NATS messaging cluster with optional JetStream storage."
	case string(adapters.ServiceClassYugabyte):
		return "YugabyteDB distributed SQL/NoSQL database with native multi-region and xCluster replication."
	case string(adapters.ServiceClassArgoApp):
		return "Managed Application points to a repository of manifests that will be deployed."
	default:
		return "Servicer-managed product."
	}
}

func actionSpecs(actions []adapters.ActionCapability) []ActionSpec {
	specs := make([]ActionSpec, 0, len(actions))
	for _, action := range actions {
		specs = append(specs, ActionSpec{
			Name:             string(action.Name),
			DisplayName:      action.DisplayName,
			RequiresApproval: action.RequiresApproval,
			Disruptive:       action.Disruptive,
		})
	}
	sort.Slice(specs, func(i, j int) bool { return specs[i].Name < specs[j].Name })
	return specs
}

func availableActions(serviceClass string) []ActionSpec {
	contract, ok := adapters.KnownContract(adapters.ServiceClass(serviceClass))
	if !ok {
		return nil
	}
	return actionSpecs(contract.Actions)
}

func runtimeSummary(instance platformv1alpha1.ServiceInstance) RuntimeSummary {
	summary := RuntimeSummary{Driver: instance.Status.Runtime.Driver}
	if instance.Status.Runtime.ObjectRef != nil {
		summary.APIVersion = instance.Status.Runtime.ObjectRef.APIVersion
		summary.Kind = instance.Status.Runtime.ObjectRef.Kind
		summary.Name = instance.Status.Runtime.ObjectRef.Name
		summary.Namespace = instance.Status.Runtime.ObjectRef.Namespace
	}
	return summary
}

func parametersMap(instance platformv1alpha1.ServiceInstance) map[string]any {
	if instance.Spec.Parameters == nil || len(instance.Spec.Parameters.Raw) == 0 {
		return nil
	}
	values := map[string]any{}
	if err := json.Unmarshal(instance.Spec.Parameters.Raw, &values); err != nil {
		return nil
	}
	return values
}

func credentialSummaries(instanceName string, refs []platformv1alpha1.NamespacedObjectReference) []CredentialSummary {
	result := make([]CredentialSummary, 0, len(refs))
	for _, ref := range refs {
		basePath := "/api/instances/" + instanceName + "/credentials/" + ref.Namespace + "/" + ref.Name
		result = append(result, CredentialSummary{
			Name:      ref.Name,
			Namespace: ref.Namespace,
			RevealURL: basePath,
		})
	}
	return result
}

func conditionSummaries(conditions []metav1.Condition) []ConditionSummary {
	result := make([]ConditionSummary, 0, len(conditions))
	for _, condition := range conditions {
		result = append(result, ConditionSummary{
			Type:    condition.Type,
			Status:  string(condition.Status),
			Reason:  condition.Reason,
			Message: condition.Message,
		})
	}
	return result
}

func cacheTopologySummary(topology platformv1alpha1.CacheTopologyStatus) *CacheTopologySummary {
	if topology.Mode == "" && topology.PrimaryCluster == "" && topology.FailoverReadiness == "" {
		return nil
	}
	summary := &CacheTopologySummary{
		Mode:              topology.Mode,
		PrimaryCluster:    topology.PrimaryCluster,
		TrafficEndpoint:   topology.TrafficEndpoint,
		FailoverReadiness: topology.FailoverReadiness,
		Message:           topology.Message,
	}
	for _, standby := range topology.StandbyClusters {
		summary.Standbys = append(summary.Standbys, CacheStandbySummary{
			ClusterName:           standby.ClusterName,
			Ready:                 standby.Ready,
			ResyncRequired:        standby.ResyncRequired,
			LagObserved:           standby.LagObserved,
			ReplicationLagSeconds: standby.ReplicationLagSeconds,
			Message:               standby.Message,
		})
	}
	return summary
}

func messagingSummary(instance platformv1alpha1.ServiceInstance) *MessagingSummary {
	if instance.Spec.ServiceClassRef.Name != string(adapters.ServiceClassNATS) {
		return nil
	}
	var params struct {
		JetStream bool `json:"jetstream,omitempty"`
		Streams   []struct {
			Name      string   `json:"name,omitempty"`
			Subjects  []string `json:"subjects,omitempty"`
			Storage   string   `json:"storage,omitempty"`
			Retention string   `json:"retention,omitempty"`
		} `json:"streams,omitempty"`
		Consumers []struct {
			Name           string   `json:"name,omitempty"`
			Stream         string   `json:"stream,omitempty"`
			FilterSubjects []string `json:"filterSubjects,omitempty"`
			AckPolicy      string   `json:"ackPolicy,omitempty"`
		} `json:"consumers,omitempty"`
		AppCredentials []struct {
			Name        string `json:"name,omitempty"`
			Username    string `json:"username,omitempty"`
			Permissions struct {
				Publish        []string `json:"publish,omitempty"`
				Subscribe      []string `json:"subscribe,omitempty"`
				AllowResponses bool     `json:"allowResponses,omitempty"`
			} `json:"permissions,omitempty"`
		} `json:"appCredentials,omitempty"`
	}
	if instance.Spec.Parameters != nil && len(instance.Spec.Parameters.Raw) > 0 {
		if err := json.Unmarshal(instance.Spec.Parameters.Raw, &params); err != nil {
			return nil
		}
	}
	summary := &MessagingSummary{JetStream: params.JetStream}
	for _, stream := range params.Streams {
		if strings.TrimSpace(stream.Name) == "" {
			continue
		}
		summary.Streams = append(summary.Streams, MessagingStreamSummary{
			Name:      stream.Name,
			Subjects:  append([]string(nil), stream.Subjects...),
			Storage:   stream.Storage,
			Retention: stream.Retention,
		})
	}
	for _, consumer := range params.Consumers {
		if strings.TrimSpace(consumer.Name) == "" || strings.TrimSpace(consumer.Stream) == "" {
			continue
		}
		summary.Consumers = append(summary.Consumers, MessagingConsumerSummary{
			Name:           consumer.Name,
			Stream:         consumer.Stream,
			FilterSubjects: append([]string(nil), consumer.FilterSubjects...),
			AckPolicy:      consumer.AckPolicy,
		})
	}
	for _, credential := range params.AppCredentials {
		if strings.TrimSpace(credential.Name) == "" {
			continue
		}
		username := credential.Username
		if username == "" {
			username = credential.Name
		}
		summary.AppCredentials = append(summary.AppCredentials, MessagingCredentialSpec{
			Name:           credential.Name,
			Username:       username,
			Publish:        append([]string(nil), credential.Permissions.Publish...),
			Subscribe:      append([]string(nil), credential.Permissions.Subscribe...),
			AllowResponses: credential.Permissions.AllowResponses,
		})
	}
	if !summary.JetStream && len(summary.Streams) == 0 && len(summary.Consumers) == 0 && len(summary.AppCredentials) == 0 {
		return nil
	}
	return summary
}

func actionSummaries(actions []platformv1alpha1.ActionRequest, limit int) []ActionSummary {
	sort.Slice(actions, func(i, j int) bool {
		return actions[i].CreationTimestamp.Time.After(actions[j].CreationTimestamp.Time)
	})
	if limit > 0 && len(actions) > limit {
		actions = actions[:limit]
	}
	result := make([]ActionSummary, 0, len(actions))
	for _, action := range actions {
		result = append(result, summarizeAction(action))
	}
	return result
}

func actionsForTarget(actions []platformv1alpha1.ActionRequest, target string, limit int) []ActionSummary {
	filtered := make([]platformv1alpha1.ActionRequest, 0)
	for _, action := range actions {
		if action.Spec.TargetRef.Name == target {
			filtered = append(filtered, action)
		}
	}
	return actionSummaries(filtered, limit)
}

func summarizeAction(action platformv1alpha1.ActionRequest) ActionSummary {
	summary := ActionSummary{
		Name:       action.Name,
		TargetName: action.Spec.TargetRef.Name,
		Action:     action.Spec.Action,
		Phase:      action.Status.Phase,
		Result:     action.Status.Result.Message,
	}
	if action.Spec.Action == string(adapters.ActionGrantAccess) && action.Status.Phase == "Succeeded" && action.Status.OperationRef != nil && action.Status.OperationRef.Kind == "Secret" {
		summary.KubeconfigDownloadURL = "/api/instances/" + action.Spec.TargetRef.Name + "/actions/" + action.Name + "/kubeconfig"
	}
	if action.Status.StartedAt != nil {
		summary.StartedAt = action.Status.StartedAt.Time.Format("2006-01-02T15:04:05Z07:00")
	}
	if action.Status.CompletedAt != nil {
		summary.CompletedAt = action.Status.CompletedAt.Time.Format("2006-01-02T15:04:05Z07:00")
	}
	return summary
}

func argoStatus(instance platformv1alpha1.ServiceInstance) string {
	switch instance.Status.Sync.Phase {
	case string(adapters.SyncPhaseSynced):
		return "Healthy"
	case string(adapters.SyncPhasePending), string(adapters.SyncPhaseMaterialized):
		return "AwaitingSync"
	case string(adapters.SyncPhaseOutOfSync):
		return "OutOfSync"
	default:
		return "Unknown"
	}
}

func runtimeStatus(instance platformv1alpha1.ServiceInstance) string {
	if instance.Status.Phase == "Blocked" {
		return "Blocked"
	}
	if instance.Status.Phase == "Ready" {
		return "Ready"
	}
	if instance.Status.Runtime.ObjectRef != nil {
		return "Observed"
	}
	return "NotObserved"
}

func timestamp(value metav1.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.Time.Format("2006-01-02T15:04:05Z07:00")
}

func copyStringMap(source map[string]string) map[string]string {
	if len(source) == 0 {
		return nil
	}
	result := make(map[string]string, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

func sortedKeys(source map[string]struct{}) []string {
	if len(source) == 0 {
		return nil
	}
	items := make([]string, 0, len(source))
	for key := range source {
		items = append(items, key)
	}
	sort.Strings(items)
	return items
}
