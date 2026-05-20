package bff

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strings"

	platformv1alpha1 "github.com/sindef/servicer/api/v1alpha1"
	"github.com/sindef/servicer/internal/adapters"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var implementedProducts = map[string]struct{}{
	string(adapters.ServiceClassNamespace):  {},
	string(adapters.ServiceClassPostgreSQL): {},
	string(adapters.ServiceClassMySQL):      {},
	string(adapters.ServiceClassValkey):     {},
	string(adapters.ServiceClassNATS):       {},
	string(adapters.ServiceClassK8ssandra):  {},
	string(adapters.ServiceClassYugabyte):   {},
	string(adapters.ServiceClassArgoApp):    {},
}

type Server struct {
	client     client.Client
	kubeHost   string
	kubeClient *http.Client
	auth       authenticator
	metrics    *serverMetrics
	handler    http.Handler
}

func NewServer(client client.Client) *Server {
	return NewServerWithConfig(client, nil)
}

func NewServerWithConfig(client client.Client, restConfig *rest.Config) *Server {
	server := &Server{client: client, metrics: newServerMetrics()}
	auth, err := newAuthenticatorFromEnv(context.Background())
	if err != nil {
		panic(err)
	}
	server.auth = auth
	if restConfig != nil {
		if httpClient, err := rest.HTTPClientFor(restConfig); err == nil {
			server.kubeHost = strings.TrimRight(restConfig.Host, "/")
			server.kubeClient = httpClient
		}
	}
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", server.metrics.handler())
	mux.HandleFunc("/api/kubernetes/namespaces/", server.handleKubernetesNamespaceProxy)
	mux.HandleFunc("GET /api", server.handleKubernetesRootProxy)
	mux.HandleFunc("/api/", server.handleKubernetesRootProxy)
	mux.HandleFunc("GET /apis", server.handleKubernetesRootProxy)
	mux.HandleFunc("/apis/", server.handleKubernetesRootProxy)
	mux.HandleFunc("GET /api/healthz", server.handleHealthz)
	mux.HandleFunc("GET /api/overview", server.handleOverview)
	mux.HandleFunc("GET /api/tenants", server.handleTenants)
	mux.HandleFunc("GET /api/projects", server.handleProjects)
	mux.HandleFunc("GET /api/namespaceclaims", server.handleNamespaceClaims)
	mux.HandleFunc("GET /api/servicebindings", server.handleServiceBindings)
	mux.HandleFunc("GET /api/virtualmachineclaims", server.handleVirtualMachineClaims)
	mux.HandleFunc("GET /api/catalog", server.handleCatalog)
	mux.HandleFunc("POST /api/requests", server.handleCreateProductRequest)
	mux.HandleFunc("GET /api/instances", server.handleInstances)
	mux.HandleFunc("GET /api/instances/{name}", server.handleInstanceDetail)
	mux.HandleFunc("GET /api/instances/{name}/credentials/{namespace}/{credential}", server.handleCredentialDetail)
	mux.HandleFunc("GET /api/instances/{name}/credentials/{namespace}/{credential}/download", server.handleDownloadCredential)
	mux.HandleFunc("PUT /api/instances/{name}", server.handleUpdateProductRequest)
	mux.HandleFunc("DELETE /api/instances/{name}", server.handleDeleteProductRequest)
	mux.HandleFunc("POST /api/instances/{name}/actions", server.handleSubmitAction)
	mux.HandleFunc("POST /api/actions/{name}/approval", server.handleActionApproval)
	mux.HandleFunc("GET /api/instances/{name}/actions/{action}/kubeconfig", server.handleDownloadNamespaceKubeconfig)
	mux.HandleFunc("GET /api/audit", server.handleAudit)
	mux.HandleFunc("GET /api/admin/clusters", server.handleListClusters)
	mux.HandleFunc("POST /api/admin/clusters", server.handleCreateCluster)
	mux.HandleFunc("PUT /api/admin/clusters/{name}", server.handleUpdateCluster)
	mux.HandleFunc("DELETE /api/admin/clusters/{name}", server.handleDeleteCluster)
	mux.HandleFunc("POST /api/admin/tenants", server.handleCreateTenant)
	mux.HandleFunc("PUT /api/admin/tenants/{name}", server.handleUpdateTenant)
	mux.HandleFunc("DELETE /api/admin/tenants/{name}", server.handleDeleteTenant)
	mux.HandleFunc("POST /api/admin/projects", server.handleCreateProject)
	mux.HandleFunc("PUT /api/admin/projects/{name}", server.handleUpdateProject)
	mux.HandleFunc("DELETE /api/admin/projects/{name}", server.handleDeleteProject)
	mux.HandleFunc("GET /api/admin/serviceclasses", server.handleListServiceClasses)
	mux.HandleFunc("POST /api/admin/serviceclasses", server.handleRegisterServiceClass)
	mux.HandleFunc("PUT /api/admin/serviceclasses/{name}", server.handleUpdateServiceClass)
	mux.HandleFunc("GET /api/projects/{project}/repositories", server.handleListProjectRepositories)
	mux.HandleFunc("POST /api/projects/{project}/repositories", server.handleCreateProjectRepository)
	mux.HandleFunc("DELETE /api/projects/{project}/repositories/{repo}", server.handleDeleteProjectRepository)
	server.handler = withJSON(server.withMetrics(server.withAuthentication(mux)))
	return server
}

func (s *Server) Handler() http.Handler {
	return s.handler
}

func (s *Server) withAuthentication(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/healthz" || r.URL.Path == "/metrics" {
			next.ServeHTTP(w, r)
			return
		}
		if s.auth == nil {
			next.ServeHTTP(w, r)
			return
		}
		actor, err := s.auth.Authenticate(r.Context(), r)
		if err != nil {
			if s.metrics != nil {
				s.metrics.authFailuresTotal.Inc()
			}
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "authentication failed"})
			return
		}
		next.ServeHTTP(w, withActor(r, actor))
	})
}

func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleOverview(w http.ResponseWriter, r *http.Request) {
	actor, ok := requireRole(w, r, rolePlatformAdmin, roleTenantOperator, roleServiceConsumer)
	if !ok {
		return
	}
	ctx := r.Context()
	tenants, projects, instances, actions, err := s.loadCore(ctx)
	if err != nil {
		writeError(w, err)
		return
	}

	tenants.Items = visibleTenants(actor, tenants.Items)
	projects.Items = visibleProjects(actor, projects.Items, tenants.Items)
	instances.Items = visibleInstances(actor, instances.Items, projects.Items, tenants.Items)
	actions.Items = visibleActions(actor, actions.Items, instances.Items)

	response := OverviewResponse{
		Tenants:       int32(len(tenants.Items)),
		Projects:      int32(len(projects.Items)),
		Instances:     int32(len(instances.Items)),
		RecentActions: actionSummaries(actions.Items, 6),
	}
	for _, instance := range instances.Items {
		switch instance.Status.Phase {
		case "Ready":
			response.ReadyInstances++
			response.Health.Ready++
		case "Provisioning", "Materialized", "PendingPlacement", "PendingDependencies":
			response.Health.Provisioning++
		case "Failed":
			response.Health.Failed++
		default:
			response.Health.Other++
		}
	}
	for _, action := range actions.Items {
		switch action.Status.Phase {
		case "PendingApproval", "Queued", "Running":
			response.PendingActions++
		}
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleTenants(w http.ResponseWriter, r *http.Request) {
	actor, ok := requireRole(w, r, rolePlatformAdmin, roleTenantOperator, roleServiceConsumer)
	if !ok {
		return
	}
	ctx := r.Context()
	tenants, projects, instances, _, err := s.loadCore(ctx)
	if err != nil {
		writeError(w, err)
		return
	}
	tenants.Items = visibleTenants(actor, tenants.Items)
	projects.Items = visibleProjects(actor, projects.Items, tenants.Items)
	instances.Items = visibleInstances(actor, instances.Items, projects.Items, tenants.Items)

	projectCounts := map[string]int32{}
	for _, project := range projects.Items {
		projectCounts[project.Spec.TenantRef.Name]++
	}
	instanceCounts := map[string]int32{}
	projectTenants := map[string]string{}
	for _, project := range projects.Items {
		projectTenants[project.Name] = project.Spec.TenantRef.Name
	}
	for _, instance := range instances.Items {
		instanceCounts[projectTenants[instance.Spec.ProjectRef.Name]]++
	}

	response := make([]TenantSummary, 0, len(tenants.Items))
	for _, tenant := range tenants.Items {
		response = append(response, TenantSummary{
			Name:                  tenant.Name,
			DisplayName:           displayName(tenant.Spec.DisplayName, tenant.Name),
			Phase:                 tenant.Status.Phase,
			AllowedServiceClasses: append([]string(nil), tenant.Spec.AllowedServiceClasses...),
			ProjectCount:          projectCounts[tenant.Name],
			InstanceCount:         instanceCounts[tenant.Name],
			Owners:                append([]string(nil), tenant.Spec.Owners.Users...),
		})
	}
	sort.Slice(response, func(i, j int) bool { return response[i].Name < response[j].Name })
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleProjects(w http.ResponseWriter, r *http.Request) {
	actor, ok := requireRole(w, r, rolePlatformAdmin, roleTenantOperator, roleServiceConsumer)
	if !ok {
		return
	}
	ctx := r.Context()
	tenants, projects, instances, _, err := s.loadCore(ctx)
	if err != nil {
		writeError(w, err)
		return
	}
	projects.Items = visibleProjects(actor, projects.Items, tenants.Items)
	instances.Items = visibleInstances(actor, instances.Items, projects.Items, tenants.Items)
	instanceCounts := map[string]int32{}
	for _, instance := range instances.Items {
		instanceCounts[instance.Spec.ProjectRef.Name]++
	}

	response := make([]ProjectSummary, 0, len(projects.Items))
	for _, project := range projects.Items {
		response = append(response, ProjectSummary{
			Name:          project.Name,
			DisplayName:   displayName(project.Spec.DisplayName, project.Name),
			TenantName:    project.Spec.TenantRef.Name,
			Environment:   string(project.Spec.Environment),
			Phase:         project.Status.Phase,
			ClusterName:   project.Status.Placement.ClusterName,
			NamespaceMode: string(project.Spec.NamespaceStrategy.Mode),
			InstanceCount: instanceCounts[project.Name],
		})
	}
	sort.Slice(response, func(i, j int) bool { return response[i].Name < response[j].Name })
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleCatalog(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireRole(w, r, rolePlatformAdmin, roleTenantOperator, roleServiceConsumer); !ok {
		return
	}
	var classes platformv1alpha1.ServiceClassList
	var plans platformv1alpha1.ServicePlanList
	if err := s.client.List(r.Context(), &classes); err != nil {
		writeError(w, err)
		return
	}
	if err := s.client.List(r.Context(), &plans); err != nil {
		writeError(w, err)
		return
	}
	plansByClass := map[string][]platformv1alpha1.ServicePlan{}
	for _, plan := range plans.Items {
		plansByClass[plan.Spec.ServiceClassRef.Name] = append(plansByClass[plan.Spec.ServiceClassRef.Name], plan)
	}

	response := make([]CatalogEntry, 0, len(classes.Items))
	for _, class := range classes.Items {
		if _, ok := implementedProducts[class.Name]; !ok {
			continue
		}
		contract, _ := adapters.KnownContract(adapters.ServiceClass(class.Name))
		entry := CatalogEntry{
			Name:         class.Name,
			DisplayName:  displayName(class.Spec.DisplayName, class.Name),
			Category:     class.Spec.Category,
			Driver:       class.Spec.Driver,
			Published:    class.Status.Published || class.Spec.Published,
			Description:  productDescription(class.Name),
			Capabilities: append([]string(nil), class.Spec.CapabilityFlags...),
			Actions:      actionSpecs(contract.Actions),
		}
		for _, plan := range plansByClass[class.Name] {
			entry.Plans = append(entry.Plans, CatalogPlan{
				Name:           plan.Name,
				DisplayName:    displayName(plan.Spec.DisplayName, plan.Name),
				Tier:           plan.Spec.Tier,
				Topology:       plan.Spec.Topology,
				DefaultVersion: plan.Spec.DefaultVersion,
				Published:      plan.Status.Published || entry.Published,
			})
		}
		sort.Slice(entry.Plans, func(i, j int) bool { return entry.Plans[i].Name < entry.Plans[j].Name })
		response = append(response, entry)
	}
	sort.Slice(response, func(i, j int) bool { return response[i].Name < response[j].Name })
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleInstances(w http.ResponseWriter, r *http.Request) {
	actor, ok := requireRole(w, r, rolePlatformAdmin, roleTenantOperator, roleServiceConsumer)
	if !ok {
		return
	}
	ctx := r.Context()
	tenants, projects, instances, _, err := s.loadCore(ctx)
	if err != nil {
		writeError(w, err)
		return
	}
	projects.Items = visibleProjects(actor, projects.Items, tenants.Items)
	instances.Items = visibleInstances(actor, instances.Items, projects.Items, tenants.Items)
	classes, plans, err := s.classPlanMaps(ctx)
	if err != nil {
		writeError(w, err)
		return
	}
	projectTenants := projectTenantMap(projects.Items)

	response := make([]InstanceSummary, 0, len(instances.Items))
	for _, instance := range instances.Items {
		response = append(response, summarizeInstance(instance, projectTenants, classes, plans))
	}
	sort.Slice(response, func(i, j int) bool { return response[i].Name < response[j].Name })
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleInstanceDetail(w http.ResponseWriter, r *http.Request) {
	actor, ok := requireRole(w, r, rolePlatformAdmin, roleTenantOperator, roleServiceConsumer)
	if !ok {
		return
	}
	name := strings.TrimSpace(r.PathValue("name"))
	if name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "instance name is required"})
		return
	}
	ctx := r.Context()
	var instance platformv1alpha1.ServiceInstance
	if err := s.client.Get(ctx, client.ObjectKey{Name: name}, &instance); err != nil {
		writeError(w, err)
		return
	}
	if !s.authorizeInstance(ctx, actor, &instance) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "instance is outside your authorized tenancy"})
		return
	}
	var projects platformv1alpha1.ProjectList
	var actions platformv1alpha1.ActionRequestList
	if err := s.client.List(ctx, &projects); err != nil {
		writeError(w, err)
		return
	}
	if err := s.client.List(ctx, &actions); err != nil {
		writeError(w, err)
		return
	}
	classes, plans, err := s.classPlanMaps(ctx)
	if err != nil {
		writeError(w, err)
		return
	}

	summary := summarizeInstance(instance, projectTenantMap(projects.Items), classes, plans)
	detail := InstanceDetail{
		InstanceSummary: summary,
		Runtime:         runtimeSummary(instance),
		Delivery: DeliverySummary{
			SyncPhase:       instance.Status.Sync.Phase,
			ApplicationName: instance.Status.Sync.ApplicationName,
			Message:         instance.Status.Sync.Message,
			ArgoStatus:      argoStatus(instance),
			RuntimeStatus:   runtimeStatus(instance),
		},
		Desired: ProductRequest{
			Name:         instance.Name,
			ProjectName:  instance.Spec.ProjectRef.Name,
			ServiceClass: instance.Spec.ServiceClassRef.Name,
			ServicePlan:  instance.Spec.ServicePlanRef.Name,
			Version:      instance.Spec.Version,
			Parameters:   parametersMap(instance),
		},
		Artifact: ArtifactSummary{
			Revision: instance.Status.Artifact.Revision,
			Path:     instance.Status.Artifact.Path,
			Count:    instance.Status.Artifact.Count,
		},
		Credentials:      credentialSummaries(instance.Name, instance.Status.CredentialRefs),
		Conditions:       conditionSummaries(instance.Status.Conditions),
		Topology:         cacheTopologySummary(instance.Status.CacheTopology),
		Messaging:        messagingSummary(instance),
		AvailableActions: availableActions(instance.Spec.ServiceClassRef.Name),
		RecentActions:    actionsForTarget(actions.Items, instance.Name, 8),
		Events:           s.eventsForTarget(r, instance.Name, 8),
	}
	writeJSON(w, http.StatusOK, detail)
}

func (s *Server) loadCore(ctx context.Context) (platformv1alpha1.TenantList, platformv1alpha1.ProjectList, platformv1alpha1.ServiceInstanceList, platformv1alpha1.ActionRequestList, error) {
	var tenants platformv1alpha1.TenantList
	var projects platformv1alpha1.ProjectList
	var instances platformv1alpha1.ServiceInstanceList
	var actions platformv1alpha1.ActionRequestList
	if err := s.client.List(ctx, &tenants); err != nil {
		return tenants, projects, instances, actions, err
	}
	if err := s.client.List(ctx, &projects); err != nil {
		return tenants, projects, instances, actions, err
	}
	if err := s.client.List(ctx, &instances); err != nil {
		return tenants, projects, instances, actions, err
	}
	if err := s.client.List(ctx, &actions); err != nil {
		return tenants, projects, instances, actions, err
	}
	return tenants, projects, instances, actions, nil
}

func (s *Server) classPlanMaps(ctx context.Context) (map[string]platformv1alpha1.ServiceClass, map[string]platformv1alpha1.ServicePlan, error) {
	var classes platformv1alpha1.ServiceClassList
	var plans platformv1alpha1.ServicePlanList
	if err := s.client.List(ctx, &classes); err != nil {
		return nil, nil, err
	}
	if err := s.client.List(ctx, &plans); err != nil {
		return nil, nil, err
	}
	classMap := map[string]platformv1alpha1.ServiceClass{}
	for _, class := range classes.Items {
		classMap[class.Name] = class
	}
	planMap := map[string]platformv1alpha1.ServicePlan{}
	for _, plan := range plans.Items {
		planMap[plan.Name] = plan
	}
	return classMap, planMap, nil
}

func withJSON(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	if meta.IsNoMatchError(err) || errors.Is(err, context.Canceled) {
		status = http.StatusServiceUnavailable
	}
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

func displayName(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
