package bff

import (
	"context"
	"fmt"

	platformv1alpha1 "github.com/sindef/servicer/api/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func (s *Server) requireTenantAccess(ctx context.Context, actor actor, tenantName string) error {
	if actor.isPlatformAdmin() {
		return nil
	}
	var tenant platformv1alpha1.Tenant
	if err := s.client.Get(ctx, client.ObjectKey{Name: tenantName}, &tenant); err != nil {
		return err
	}
	if tenantVisibleToActor(actor, tenant) {
		return nil
	}
	return fmt.Errorf("forbidden")
}

func (s *Server) projectForInstance(ctx context.Context, instance *platformv1alpha1.ServiceInstance) (*platformv1alpha1.Project, error) {
	var project platformv1alpha1.Project
	if err := s.client.Get(ctx, client.ObjectKey{Name: instance.Spec.ProjectRef.Name}, &project); err != nil {
		return nil, err
	}
	return &project, nil
}

func (s *Server) authorizeProject(ctx context.Context, actor actor, project *platformv1alpha1.Project) bool {
	if actor.isPlatformAdmin() {
		return true
	}
	var tenant platformv1alpha1.Tenant
	if err := s.client.Get(ctx, client.ObjectKey{Name: project.Spec.TenantRef.Name}, &tenant); err != nil {
		return false
	}
	return tenantVisibleToActor(actor, tenant)
}

func (s *Server) authorizeInstance(ctx context.Context, actor actor, instance *platformv1alpha1.ServiceInstance) bool {
	if actor.isPlatformAdmin() {
		return true
	}
	project, err := s.projectForInstance(ctx, instance)
	if err != nil {
		return false
	}
	return s.authorizeProject(ctx, actor, project)
}

func tenantVisibleToActor(actor actor, tenant platformv1alpha1.Tenant) bool {
	if actor.isPlatformAdmin() {
		return true
	}
	if actor.hasTenantAccess(tenant.Name) {
		return true
	}
	if actor.Name != "" {
		for _, user := range tenant.Spec.Owners.Users {
			if user == actor.Name || user == actor.Email || user == actor.UserName || user == actor.Subject {
				return true
			}
		}
	}
	for _, group := range tenant.Spec.Owners.Groups {
		if _, ok := actor.Groups[group]; ok {
			return true
		}
	}
	return false
}

func visibleTenants(actor actor, tenants []platformv1alpha1.Tenant) []platformv1alpha1.Tenant {
	if actor.isPlatformAdmin() {
		return tenants
	}
	filtered := make([]platformv1alpha1.Tenant, 0, len(tenants))
	for _, tenant := range tenants {
		if tenantVisibleToActor(actor, tenant) {
			filtered = append(filtered, tenant)
		}
	}
	return filtered
}

func visibleProjects(actor actor, projects []platformv1alpha1.Project, tenants []platformv1alpha1.Tenant) []platformv1alpha1.Project {
	if actor.isPlatformAdmin() {
		return projects
	}
	allowedTenants := map[string]struct{}{}
	for _, tenant := range visibleTenants(actor, tenants) {
		allowedTenants[tenant.Name] = struct{}{}
	}
	filtered := make([]platformv1alpha1.Project, 0, len(projects))
	for _, project := range projects {
		if _, ok := allowedTenants[project.Spec.TenantRef.Name]; ok {
			filtered = append(filtered, project)
		}
	}
	return filtered
}

func visibleInstances(actor actor, instances []platformv1alpha1.ServiceInstance, projects []platformv1alpha1.Project, tenants []platformv1alpha1.Tenant) []platformv1alpha1.ServiceInstance {
	if actor.isPlatformAdmin() {
		return instances
	}
	allowedProjects := map[string]struct{}{}
	for _, project := range visibleProjects(actor, projects, tenants) {
		allowedProjects[project.Name] = struct{}{}
	}
	filtered := make([]platformv1alpha1.ServiceInstance, 0, len(instances))
	for _, instance := range instances {
		if _, ok := allowedProjects[instance.Spec.ProjectRef.Name]; ok {
			filtered = append(filtered, instance)
		}
	}
	return filtered
}

func visibleActions(actor actor, actions []platformv1alpha1.ActionRequest, instances []platformv1alpha1.ServiceInstance) []platformv1alpha1.ActionRequest {
	if actor.isPlatformAdmin() {
		return actions
	}
	allowedInstances := map[string]struct{}{}
	for _, instance := range instances {
		allowedInstances[instance.Name] = struct{}{}
	}
	filtered := make([]platformv1alpha1.ActionRequest, 0, len(actions))
	for _, action := range actions {
		if _, ok := allowedInstances[action.Spec.TargetRef.Name]; ok {
			filtered = append(filtered, action)
		}
	}
	return filtered
}
