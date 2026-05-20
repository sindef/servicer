package bff

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"

	platformv1alpha1 "github.com/sindef/servicer/api/v1alpha1"
	"github.com/sindef/servicer/internal/adapters"
	"k8s.io/apimachinery/pkg/types"
)

func (s *Server) handleNamespaceClaims(w http.ResponseWriter, r *http.Request) {
	actor, ok := requireRole(w, r, rolePlatformAdmin, roleTenantOperator, roleServiceConsumer)
	if !ok {
		return
	}

	var tenants platformv1alpha1.TenantList
	var projects platformv1alpha1.ProjectList
	var claims platformv1alpha1.NamespaceClaimList
	var instances platformv1alpha1.ServiceInstanceList
	if err := s.client.List(r.Context(), &tenants); err != nil {
		writeError(w, err)
		return
	}
	if err := s.client.List(r.Context(), &projects); err != nil {
		writeError(w, err)
		return
	}
	if err := s.client.List(r.Context(), &claims); err != nil {
		writeError(w, err)
		return
	}
	if err := s.client.List(r.Context(), &instances); err != nil {
		writeError(w, err)
		return
	}

	projects.Items = visibleProjects(actor, projects.Items, tenants.Items)
	allowedProjects := projectSet(projects.Items)
	response := make([]NamespaceClaimSummary, 0, len(claims.Items))
	seen := make(map[string]struct{}, len(claims.Items))
	for _, claim := range claims.Items {
		if _, ok := allowedProjects[claim.Spec.ProjectRef.Name]; !ok && !actor.isPlatformAdmin() {
			continue
		}
		seen[claim.Name] = struct{}{}
		response = append(response, namespaceClaimSummary(claim))
	}
	for _, instance := range instances.Items {
		if !isNamespaceInstance(instance) {
			continue
		}
		if _, ok := seen[instance.Name]; ok {
			continue
		}
		if _, ok := allowedProjects[instance.Spec.ProjectRef.Name]; !ok && !actor.isPlatformAdmin() {
			continue
		}
		response = append(response, namespaceInstanceSummary(instance))
	}
	sort.Slice(response, func(i, j int) bool { return response[i].Name < response[j].Name })
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleNamespaceClaimDetail(w http.ResponseWriter, r *http.Request) {
	actor, ok := requireRole(w, r, rolePlatformAdmin, roleTenantOperator, roleServiceConsumer)
	if !ok {
		return
	}
	name := strings.TrimSpace(r.PathValue("name"))
	if name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "namespace claim name is required"})
		return
	}

	var claim platformv1alpha1.NamespaceClaim
	if err := s.client.Get(r.Context(), types.NamespacedName{Name: name}, &claim); err == nil {
		var project platformv1alpha1.Project
		if err := s.client.Get(r.Context(), types.NamespacedName{Name: claim.Spec.ProjectRef.Name}, &project); err != nil {
			writeError(w, err)
			return
		}
		if !s.authorizeProject(r.Context(), actor, &project) {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "namespace claim is outside your authorized tenancy"})
			return
		}
		writeJSON(w, http.StatusOK, namespaceClaimDetail(claim))
		return
	}

	var instance platformv1alpha1.ServiceInstance
	if err := s.client.Get(r.Context(), types.NamespacedName{Name: name}, &instance); err != nil {
		writeError(w, err)
		return
	}
	if !isNamespaceInstance(instance) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "namespace claim was not found"})
		return
	}
	if !s.authorizeInstance(r.Context(), actor, &instance) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "namespace claim is outside your authorized tenancy"})
		return
	}
	writeJSON(w, http.StatusOK, namespaceInstanceDetail(instance))
}

func (s *Server) handleServiceBindings(w http.ResponseWriter, r *http.Request) {
	actor, ok := requireRole(w, r, rolePlatformAdmin, roleTenantOperator, roleServiceConsumer)
	if !ok {
		return
	}

	var tenants platformv1alpha1.TenantList
	var projects platformv1alpha1.ProjectList
	var bindings platformv1alpha1.ServiceBindingList
	if err := s.client.List(r.Context(), &tenants); err != nil {
		writeError(w, err)
		return
	}
	if err := s.client.List(r.Context(), &projects); err != nil {
		writeError(w, err)
		return
	}
	if err := s.client.List(r.Context(), &bindings); err != nil {
		writeError(w, err)
		return
	}

	projects.Items = visibleProjects(actor, projects.Items, tenants.Items)
	allowedProjects := projectSet(projects.Items)
	response := make([]ServiceBindingSummary, 0, len(bindings.Items))
	for _, binding := range bindings.Items {
		if _, ok := allowedProjects[binding.Spec.ProjectRef.Name]; !ok && !actor.isPlatformAdmin() {
			continue
		}
		summary := ServiceBindingSummary{
			Name:        binding.Name,
			ProjectName: binding.Spec.ProjectRef.Name,
			Phase:       binding.Status.Phase,
			SourceName:  binding.Spec.SourceRef.Name,
			TargetName:  binding.Spec.TargetRef.Name,
			Health:      binding.Status.Health.Summary,
		}
		if len(binding.Status.CredentialRefs) > 0 {
			summary.ProjectedSecret = binding.Status.CredentialRefs[0].Name
			summary.ProjectedNamespace = binding.Status.CredentialRefs[0].Namespace
		}
		response = append(response, summary)
	}
	sort.Slice(response, func(i, j int) bool { return response[i].Name < response[j].Name })
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleVirtualMachineClaims(w http.ResponseWriter, r *http.Request) {
	actor, ok := requireRole(w, r, rolePlatformAdmin, roleTenantOperator, roleServiceConsumer)
	if !ok {
		return
	}

	var tenants platformv1alpha1.TenantList
	var projects platformv1alpha1.ProjectList
	var claims platformv1alpha1.VirtualMachineClaimList
	if err := s.client.List(r.Context(), &tenants); err != nil {
		writeError(w, err)
		return
	}
	if err := s.client.List(r.Context(), &projects); err != nil {
		writeError(w, err)
		return
	}
	if err := s.client.List(r.Context(), &claims); err != nil {
		writeError(w, err)
		return
	}

	projects.Items = visibleProjects(actor, projects.Items, tenants.Items)
	allowedProjects := projectSet(projects.Items)
	response := make([]VirtualMachineClaimSummary, 0, len(claims.Items))
	for _, claim := range claims.Items {
		if _, ok := allowedProjects[claim.Spec.ProjectRef.Name]; !ok && !actor.isPlatformAdmin() {
			continue
		}
		response = append(response, VirtualMachineClaimSummary{
			Name:        claim.Name,
			ProjectName: claim.Spec.ProjectRef.Name,
			Class:       claim.Spec.Class,
			Image:       claim.Spec.Image,
			Phase:       claim.Status.Phase,
			ClusterName: claim.Status.Placement.ClusterName,
			Health:      claim.Status.Health.Summary,
		})
	}
	sort.Slice(response, func(i, j int) bool { return response[i].Name < response[j].Name })
	writeJSON(w, http.StatusOK, response)
}

func projectSet(projects []platformv1alpha1.Project) map[string]struct{} {
	set := make(map[string]struct{}, len(projects))
	for _, project := range projects {
		set[project.Name] = struct{}{}
	}
	return set
}

func namespaceClaimDetail(claim platformv1alpha1.NamespaceClaim) NamespaceClaimDetail {
	return NamespaceClaimDetail{
		Name:           claim.Name,
		DisplayName:    claim.Spec.DisplayName,
		ProjectName:    claim.Spec.ProjectRef.Name,
		Phase:          claim.Status.Phase,
		ClusterName:    claim.Status.Placement.ClusterName,
		Namespace:      claim.Status.Placement.Namespace,
		Health:         claim.Status.Health.Summary,
		DeletionPolicy: string(claim.Spec.DeletionPolicy),
		Quotas:         copyStringMap(claim.Spec.Quotas),
		Labels:         copyStringMap(claim.Spec.Labels),
		Artifact: ArtifactSummary{
			Revision: claim.Status.Artifact.Revision,
			Path:     claim.Status.Artifact.Path,
			Count:    claim.Status.Artifact.Count,
		},
		Delivery: DeliverySummary{
			SyncPhase:       claim.Status.Sync.Phase,
			ApplicationName: claim.Status.Sync.ApplicationName,
			Message:         claim.Status.Sync.Message,
		},
		Conditions: conditionSummaries(claim.Status.Conditions),
	}
}

func namespaceClaimSummary(claim platformv1alpha1.NamespaceClaim) NamespaceClaimSummary {
	return NamespaceClaimSummary{
		Name:        claim.Name,
		ProjectName: claim.Spec.ProjectRef.Name,
		Phase:       claim.Status.Phase,
		ClusterName: claim.Status.Placement.ClusterName,
		Namespace:   claim.Status.Placement.Namespace,
		Health:      claim.Status.Health.Summary,
	}
}

func namespaceInstanceSummary(instance platformv1alpha1.ServiceInstance) NamespaceClaimSummary {
	return NamespaceClaimSummary{
		Name:        instance.Name,
		ProjectName: instance.Spec.ProjectRef.Name,
		Phase:       instance.Status.Phase,
		ClusterName: instance.Status.Placement.ClusterName,
		Namespace:   instance.Status.Placement.Namespace,
		Health:      instance.Status.Health.Summary,
	}
}

func namespaceInstanceDetail(instance platformv1alpha1.ServiceInstance) NamespaceClaimDetail {
	params := namespaceInstanceParameters(instance)
	return NamespaceClaimDetail{
		Name:           instance.Name,
		ProjectName:    instance.Spec.ProjectRef.Name,
		Phase:          instance.Status.Phase,
		ClusterName:    instance.Status.Placement.ClusterName,
		Namespace:      instance.Status.Placement.Namespace,
		Health:         instance.Status.Health.Summary,
		DeletionPolicy: string(instance.Spec.DeletionPolicy),
		Quotas:         namespaceClaimQuotasFromInstance(params),
		Labels:         copyStringMap(params.Labels),
		Artifact: ArtifactSummary{
			Revision: instance.Status.Artifact.Revision,
			Path:     instance.Status.Artifact.Path,
			Count:    instance.Status.Artifact.Count,
		},
		Delivery: DeliverySummary{
			SyncPhase:       instance.Status.Sync.Phase,
			ApplicationName: instance.Status.Sync.ApplicationName,
			Message:         instance.Status.Sync.Message,
			RuntimeStatus:   instance.Status.Phase,
		},
		Conditions: conditionSummaries(instance.Status.Conditions),
	}
}

func isNamespaceInstance(instance platformv1alpha1.ServiceInstance) bool {
	return instance.Spec.ServiceClassRef.Name == string(adapters.ServiceClassNamespace)
}

type namespaceInstanceParamView struct {
	CPU    string            `json:"cpu,omitempty"`
	Memory string            `json:"memory,omitempty"`
	Pods   string            `json:"pods,omitempty"`
	Labels map[string]string `json:"labels,omitempty"`
}

func namespaceInstanceParameters(instance platformv1alpha1.ServiceInstance) namespaceInstanceParamView {
	var params namespaceInstanceParamView
	if instance.Spec.Parameters == nil || len(instance.Spec.Parameters.Raw) == 0 {
		return params
	}
	_ = json.Unmarshal(instance.Spec.Parameters.Raw, &params)
	return params
}

func namespaceClaimQuotasFromInstance(params namespaceInstanceParamView) map[string]string {
	quotas := map[string]string{}
	if params.CPU != "" {
		quotas["requests.cpu"] = params.CPU
	}
	if params.Memory != "" {
		quotas["requests.memory"] = params.Memory
	}
	if params.Pods != "" {
		quotas["pods"] = params.Pods
	}
	if len(quotas) == 0 {
		return nil
	}
	return quotas
}
