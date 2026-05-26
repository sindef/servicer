package bff

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	platformv1alpha1 "github.com/sindef/servicer/api/v1alpha1"
	"github.com/sindef/servicer/internal/adapters"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ── Clusters ─────────────────────────────────────────────────────────────────

func (s *Server) handleListClusters(w http.ResponseWriter, r *http.Request) {
	if _, ok := requirePlatformRole(w, r, rolePlatformAdmin, roleClusterAdmin); !ok {
		return
	}
	var list platformv1alpha1.ClusterTargetList
	if err := s.client.List(r.Context(), &list); err != nil {
		writeError(w, err)
		return
	}
	response := make([]ClusterTargetSummary, 0, len(list.Items))
	for _, ct := range list.Items {
		response = append(response, toClusterTargetSummary(ct))
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleCreateCluster(w http.ResponseWriter, r *http.Request) {
	actor, ok := requirePlatformRole(w, r, rolePlatformAdmin, roleClusterAdmin)
	if !ok {
		return
	}
	var req CreateClusterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if req.Name == "" || req.DisplayName == "" || req.Region == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name, displayName and region are required"})
		return
	}
	if req.ConnectionSecretName == "" || req.ConnectionSecretNamespace == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "connectionSecretName and connectionSecretNamespace are required"})
		return
	}
	ct := &platformv1alpha1.ClusterTarget{
		ObjectMeta: metav1.ObjectMeta{
			Name: req.Name,
			Annotations: map[string]string{
				"servicer.io/created-by": actor.Name,
				"servicer.io/created-at": time.Now().UTC().Format(time.RFC3339),
			},
		},
		Spec: platformv1alpha1.ClusterTargetSpec{
			DisplayName:   req.DisplayName,
			Region:        req.Region,
			IngressDomain: req.IngressDomain,
			Capabilities:  req.Capabilities,
			ConnectionRef: platformv1alpha1.NamespacedObjectReference{
				Name:      req.ConnectionSecretName,
				Namespace: req.ConnectionSecretNamespace,
			},
		},
	}
	if err := s.client.Create(r.Context(), ct); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, WriteResponse{Name: ct.Name, Message: "Cluster registered."})
}

func (s *Server) handleUpdateCluster(w http.ResponseWriter, r *http.Request) {
	actor, ok := requirePlatformRole(w, r, rolePlatformAdmin, roleClusterAdmin)
	if !ok {
		return
	}
	name := strings.TrimSpace(r.PathValue("name"))
	var req UpdateClusterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	var existing platformv1alpha1.ClusterTarget
	if err := s.client.Get(r.Context(), types.NamespacedName{Name: name}, &existing); err != nil {
		writeError(w, err)
		return
	}
	existing.Spec.DisplayName = req.DisplayName
	existing.Spec.Region = req.Region
	existing.Spec.IngressDomain = req.IngressDomain
	existing.Spec.Capabilities = req.Capabilities
	if existing.Annotations == nil {
		existing.Annotations = map[string]string{}
	}
	existing.Annotations["servicer.io/updated-by"] = actor.Name
	existing.Annotations["servicer.io/updated-at"] = time.Now().UTC().Format(time.RFC3339)
	if err := s.client.Update(r.Context(), &existing); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, WriteResponse{Name: existing.Name, Message: "Cluster updated."})
}

// ── Tenants (admin write) ─────────────────────────────────────────────────────

func (s *Server) handleCreateTenant(w http.ResponseWriter, r *http.Request) {
	actor, ok := requireRole(w, r, rolePlatformAdmin)
	if !ok {
		return
	}
	var req CreateTenantRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if req.Name == "" || req.DisplayName == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name and displayName are required"})
		return
	}
	if len(req.AllowedServiceClasses) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "at least one allowedServiceClass is required"})
		return
	}
	quotaRef := req.QuotaProfileRef
	if quotaRef == "" {
		quotaRef = "default"
	}
	tenant := &platformv1alpha1.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: req.Name,
			Annotations: map[string]string{
				"servicer.io/created-by": actor.Name,
				"servicer.io/created-at": time.Now().UTC().Format(time.RFC3339),
			},
		},
		Spec: platformv1alpha1.TenantSpec{
			DisplayName: req.DisplayName,
			Owners: platformv1alpha1.OwnersSpec{
				Users: req.Owners,
			},
			AllowedServiceClasses: req.AllowedServiceClasses,
			QuotaProfileRef:       platformv1alpha1.LocalObjectReference{Name: quotaRef},
			Lifecycle:             platformv1alpha1.TenantLifecycleSpec{Phase: platformv1alpha1.TenantLifecyclePhaseActive},
		},
	}
	if err := s.client.Create(r.Context(), tenant); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, WriteResponse{Name: tenant.Name, Message: "Tenant created."})
}

func (s *Server) handleUpdateTenant(w http.ResponseWriter, r *http.Request) {
	actor, ok := requireRole(w, r, rolePlatformAdmin)
	if !ok {
		return
	}
	name := strings.TrimSpace(r.PathValue("name"))
	var req UpdateTenantRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	var existing platformv1alpha1.Tenant
	if err := s.client.Get(r.Context(), types.NamespacedName{Name: name}, &existing); err != nil {
		writeError(w, err)
		return
	}
	if req.DisplayName != "" {
		existing.Spec.DisplayName = req.DisplayName
	}
	if req.AllowedServiceClasses != nil {
		existing.Spec.AllowedServiceClasses = req.AllowedServiceClasses
	}
	if req.Owners != nil {
		existing.Spec.Owners.Users = req.Owners
	}
	if existing.Annotations == nil {
		existing.Annotations = map[string]string{}
	}
	existing.Annotations["servicer.io/updated-by"] = actor.Name
	existing.Annotations["servicer.io/updated-at"] = time.Now().UTC().Format(time.RFC3339)
	if err := s.client.Update(r.Context(), &existing); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, WriteResponse{Name: existing.Name, Message: "Tenant updated."})
}

// ── Projects (admin write) ────────────────────────────────────────────────────

func (s *Server) handleCreateProject(w http.ResponseWriter, r *http.Request) {
	actor, ok := requireRole(w, r, rolePlatformAdmin)
	if !ok {
		return
	}
	var req CreateProjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if req.Name == "" || req.DisplayName == "" || req.TenantName == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name, displayName and tenantName are required"})
		return
	}
	nsMode := platformv1alpha1.NamespaceStrategyMode(req.NamespaceMode)
	if nsMode == "" {
		nsMode = platformv1alpha1.NamespaceStrategyDedicated
	}
	project := &platformv1alpha1.Project{
		ObjectMeta: metav1.ObjectMeta{
			Name: req.Name,
			Annotations: map[string]string{
				"servicer.io/created-by": actor.Name,
				"servicer.io/created-at": time.Now().UTC().Format(time.RFC3339),
			},
		},
		Spec: platformv1alpha1.ProjectSpec{
			TenantRef:   platformv1alpha1.LocalObjectReference{Name: req.TenantName},
			DisplayName: req.DisplayName,
			Environment: platformv1alpha1.EnvironmentType(req.Environment),
			NamespaceStrategy: platformv1alpha1.NamespaceStrategySpec{
				Mode: nsMode,
			},
			Quotas: platformv1alpha1.ProjectQuotasSpec{
				MaxServices: req.MaxServices,
			},
		},
	}
	if req.ClusterName != "" {
		project.Spec.TargetSelector = platformv1alpha1.TargetSelectorSpec{
			ClusterRef: &platformv1alpha1.LocalObjectReference{Name: req.ClusterName},
		}
	}
	if err := s.client.Create(r.Context(), project); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, WriteResponse{Name: project.Name, Message: "Project created."})
}

func (s *Server) handleUpdateProject(w http.ResponseWriter, r *http.Request) {
	actor, ok := requireRole(w, r, rolePlatformAdmin)
	if !ok {
		return
	}
	name := strings.TrimSpace(r.PathValue("name"))
	var req UpdateProjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	var existing platformv1alpha1.Project
	if err := s.client.Get(r.Context(), types.NamespacedName{Name: name}, &existing); err != nil {
		writeError(w, err)
		return
	}
	if req.DisplayName != "" {
		existing.Spec.DisplayName = req.DisplayName
	}
	if req.NamespaceMode != "" {
		existing.Spec.NamespaceStrategy.Mode = platformv1alpha1.NamespaceStrategyMode(req.NamespaceMode)
	}
	if req.ClusterName != "" {
		existing.Spec.TargetSelector.ClusterRef = &platformv1alpha1.LocalObjectReference{Name: req.ClusterName}
	} else {
		existing.Spec.TargetSelector.ClusterRef = nil
	}
	existing.Spec.Quotas.MaxServices = req.MaxServices
	if existing.Annotations == nil {
		existing.Annotations = map[string]string{}
	}
	existing.Annotations["servicer.io/updated-by"] = actor.Name
	existing.Annotations["servicer.io/updated-at"] = time.Now().UTC().Format(time.RFC3339)
	if err := s.client.Update(r.Context(), &existing); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, WriteResponse{Name: existing.Name, Message: "Project updated."})
}

// ── Service classes (admin) ───────────────────────────────────────────────────

func (s *Server) handleListServiceClasses(w http.ResponseWriter, r *http.Request) {
	if _, ok := requirePlatformRole(w, r, rolePlatformAdmin, roleCatalogAdmin); !ok {
		return
	}
	var list platformv1alpha1.ServiceClassList
	if err := s.client.List(r.Context(), &list, &client.ListOptions{}); err != nil {
		writeError(w, err)
		return
	}
	registered := map[string]bool{}
	response := make([]ServiceClassAdminSummary, 0, len(list.Items))
	for _, sc := range list.Items {
		registered[sc.Name] = true
		summary := ServiceClassAdminSummary{
			Name:        sc.Name,
			DisplayName: serviceClassDisplayName(sc.Name, sc.Spec.DisplayName),
			Driver:      sc.Spec.Driver,
			Category:    sc.Spec.Category,
			Published:   sc.Spec.Published,
			Registered:  true,
		}
		if sc.Spec.DefaultParameters != nil {
			summary.DefaultParameters = sc.Spec.DefaultParameters.Raw
		}
		response = append(response, summary)
	}
	// Include known contracts that don't yet have a ServiceClass CR.
	for _, contract := range adapters.KnownContracts() {
		name := string(contract.ServiceClass)
		if registered[name] {
			continue
		}
		response = append(response, ServiceClassAdminSummary{
			Name:        name,
			DisplayName: contract.FriendlyName,
			Driver:      contract.RuntimeDriver,
			Published:   false,
			Registered:  false,
		})
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleRegisterServiceClass(w http.ResponseWriter, r *http.Request) {
	actor, ok := requirePlatformRole(w, r, rolePlatformAdmin, roleCatalogAdmin)
	if !ok {
		return
	}
	var req RegisterServiceClassRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	contract, known := adapters.KnownContract(adapters.ServiceClass(req.Name))
	if !known {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown service class"})
		return
	}
	sc := platformv1alpha1.ServiceClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: req.Name,
			Annotations: map[string]string{
				"servicer.io/registered-by": actor.Name,
				"servicer.io/registered-at": time.Now().UTC().Format(time.RFC3339),
			},
		},
		Spec: platformv1alpha1.ServiceClassSpec{
			DisplayName:           contract.FriendlyName,
			Driver:                contract.RuntimeDriver,
			AllowsVersionOverride: contract.SupportsVersionOverride,
			Published:             false,
		},
	}
	if err := s.client.Create(r.Context(), &sc); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, WriteResponse{Name: sc.Name, Message: "Service class registered."})
}

func (s *Server) handleUpdateServiceClass(w http.ResponseWriter, r *http.Request) {
	actor, ok := requirePlatformRole(w, r, rolePlatformAdmin, roleCatalogAdmin)
	if !ok {
		return
	}
	name := strings.TrimSpace(r.PathValue("name"))
	var req UpdateServiceClassRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	var existing platformv1alpha1.ServiceClass
	if err := s.client.Get(r.Context(), types.NamespacedName{Name: name}, &existing); err != nil {
		writeError(w, err)
		return
	}
	existing.Spec.Published = req.Published
	if len(req.DefaultParameters) > 0 {
		existing.Spec.DefaultParameters = &apiextensionsv1.JSON{Raw: req.DefaultParameters}
	}
	if existing.Annotations == nil {
		existing.Annotations = map[string]string{}
	}
	existing.Annotations["servicer.io/updated-by"] = actor.Name
	existing.Annotations["servicer.io/updated-at"] = time.Now().UTC().Format(time.RFC3339)
	if err := s.client.Update(r.Context(), &existing); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, WriteResponse{Name: existing.Name, Message: "Service class updated."})
}

// ── Delete handlers ───────────────────────────────────────────────────────────

func (s *Server) handleDeleteTenant(w http.ResponseWriter, r *http.Request) {
	actor, ok := requireRole(w, r, rolePlatformAdmin)
	if !ok {
		return
	}
	name := strings.TrimSpace(r.PathValue("name"))
	var existing platformv1alpha1.Tenant
	if err := s.client.Get(r.Context(), types.NamespacedName{Name: name}, &existing); err != nil {
		writeError(w, err)
		return
	}
	if existing.Annotations == nil {
		existing.Annotations = map[string]string{}
	}
	existing.Annotations["servicer.io/deleted-by"] = actor.Name
	existing.Annotations["servicer.io/deleted-at"] = time.Now().UTC().Format(time.RFC3339)
	_ = s.client.Update(r.Context(), &existing)
	if err := s.client.Delete(r.Context(), &existing); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, WriteResponse{Name: name, Message: "Tenant deleted."})
}

func (s *Server) handleDeleteProject(w http.ResponseWriter, r *http.Request) {
	actor, ok := requireRole(w, r, rolePlatformAdmin)
	if !ok {
		return
	}
	name := strings.TrimSpace(r.PathValue("name"))
	var existing platformv1alpha1.Project
	if err := s.client.Get(r.Context(), types.NamespacedName{Name: name}, &existing); err != nil {
		writeError(w, err)
		return
	}
	if existing.Annotations == nil {
		existing.Annotations = map[string]string{}
	}
	existing.Annotations["servicer.io/deleted-by"] = actor.Name
	existing.Annotations["servicer.io/deleted-at"] = time.Now().UTC().Format(time.RFC3339)
	_ = s.client.Update(r.Context(), &existing)
	if err := s.client.Delete(r.Context(), &existing); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, WriteResponse{Name: name, Message: "Project deleted."})
}

func (s *Server) handleDeleteCluster(w http.ResponseWriter, r *http.Request) {
	actor, ok := requirePlatformRole(w, r, rolePlatformAdmin, roleClusterAdmin)
	if !ok {
		return
	}
	name := strings.TrimSpace(r.PathValue("name"))
	var existing platformv1alpha1.ClusterTarget
	if err := s.client.Get(r.Context(), types.NamespacedName{Name: name}, &existing); err != nil {
		writeError(w, err)
		return
	}
	if existing.Annotations == nil {
		existing.Annotations = map[string]string{}
	}
	existing.Annotations["servicer.io/deleted-by"] = actor.Name
	existing.Annotations["servicer.io/deleted-at"] = time.Now().UTC().Format(time.RFC3339)
	_ = s.client.Update(r.Context(), &existing)
	if err := s.client.Delete(r.Context(), &existing); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, WriteResponse{Name: name, Message: "Cluster deleted."})
}

// ── helpers ───────────────────────────────────────────────────────────────────

func toClusterTargetSummary(ct platformv1alpha1.ClusterTarget) ClusterTargetSummary {
	return ClusterTargetSummary{
		Name:          ct.Name,
		DisplayName:   displayName(ct.Spec.DisplayName, ct.Name),
		Region:        ct.Spec.Region,
		Phase:         ct.Status.Phase,
		Reachable:     ct.Status.Reachable,
		K8sVersion:    ct.Status.KubernetesVersion,
		IngressDomain: ct.Spec.IngressDomain,
		Capabilities:  ct.Spec.Capabilities,
	}
}
