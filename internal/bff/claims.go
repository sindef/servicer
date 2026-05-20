package bff

import (
	"net/http"
	"sort"

	platformv1alpha1 "github.com/sindef/servicer/api/v1alpha1"
)

func (s *Server) handleNamespaceClaims(w http.ResponseWriter, r *http.Request) {
	actor, ok := requireRole(w, r, rolePlatformAdmin, roleTenantOperator, roleServiceConsumer)
	if !ok {
		return
	}

	var tenants platformv1alpha1.TenantList
	var projects platformv1alpha1.ProjectList
	var claims platformv1alpha1.NamespaceClaimList
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
	response := make([]NamespaceClaimSummary, 0, len(claims.Items))
	for _, claim := range claims.Items {
		if _, ok := allowedProjects[claim.Spec.ProjectRef.Name]; !ok && !actor.isPlatformAdmin() {
			continue
		}
		response = append(response, NamespaceClaimSummary{
			Name:        claim.Name,
			ProjectName: claim.Spec.ProjectRef.Name,
			Phase:       claim.Status.Phase,
			ClusterName: claim.Status.Placement.ClusterName,
			Namespace:   claim.Status.Placement.Namespace,
			Health:      claim.Status.Health.Summary,
		})
	}
	sort.Slice(response, func(i, j int) bool { return response[i].Name < response[j].Name })
	writeJSON(w, http.StatusOK, response)
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
