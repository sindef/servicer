package bff

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	platformv1alpha1 "github.com/sindef/servicer/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	repoSecretNamespace  = "servicer-system"
	repoSecretTypeLabel  = "servicer.io/type"
	repoSecretTypeValue  = "repository"
	repoSecretScopeKey   = "servicer.io/scope" // #nosec G101 -- Kubernetes label keys, not credentials.
	repoSecretTenantKey  = "servicer.io/tenant"
	repoSecretProjectKey = "servicer.io/project" // #nosec G101 -- Kubernetes label keys, not credentials.
	argocdNamespace      = "argocd"
)

const repositoryMirrorLabel = "servicer.io/repository-mirror"

type repositoryDependencyConflictResponse struct {
	Error        string   `json:"error"`
	Code         string   `json:"code"`
	Dependencies []string `json:"dependencies,omitempty"`
}

// handleListProjectRepositories returns all repositories stored under a project.
func (s *Server) handleListProjectRepositories(w http.ResponseWriter, r *http.Request) {
	actor, ok := requireRole(w, r, rolePlatformAdmin, roleTenantAdmin, roleTenantOperator, roleServiceConsumer)
	if !ok {
		return
	}
	projectName := strings.TrimSpace(r.PathValue("project"))
	if _, err := s.authorizedRepositoryProject(r, actor, projectName); err != nil {
		writeRepositoryError(w, err)
		return
	}

	var secretList corev1.SecretList
	if err := s.client.List(r.Context(), &secretList,
		client.InNamespace(repoSecretNamespace),
		client.MatchingLabels{
			repoSecretTypeLabel:  repoSecretTypeValue,
			repoSecretProjectKey: projectName,
		},
	); err != nil {
		writeError(w, err)
		return
	}

	result := make([]RepositorySummary, 0, len(secretList.Items))
	for _, sec := range secretList.Items {
		result = append(result, repoSecretToSummary(sec))
	}
	writeJSON(w, http.StatusOK, result)
}

// handleListTenantRepositories returns repositories shared across a tenant.
func (s *Server) handleListTenantRepositories(w http.ResponseWriter, r *http.Request) {
	actor, ok := requireRole(w, r, rolePlatformAdmin, roleTenantAdmin, roleTenantOperator, roleServiceConsumer)
	if !ok {
		return
	}
	tenantName := strings.TrimSpace(r.PathValue("tenant"))
	if _, err := s.authorizedRepositoryTenant(r, actor, tenantName); err != nil {
		writeRepositoryError(w, err)
		return
	}

	var secretList corev1.SecretList
	if err := s.client.List(r.Context(), &secretList,
		client.InNamespace(repoSecretNamespace),
		client.MatchingLabels{
			repoSecretTypeLabel: repoSecretTypeValue,
			repoSecretScopeKey:  "tenant",
			repoSecretTenantKey: tenantName,
		},
	); err != nil {
		writeError(w, err)
		return
	}

	result := make([]RepositorySummary, 0, len(secretList.Items))
	for _, sec := range secretList.Items {
		result = append(result, repoSecretToSummary(sec))
	}
	writeJSON(w, http.StatusOK, result)
}

// handleCreateProjectRepository stores a new repository credential under the project.
// It also creates or updates an Argo CD repository secret so the repo is immediately usable.
func (s *Server) handleCreateProjectRepository(w http.ResponseWriter, r *http.Request) {
	actor, ok := requireRole(w, r, rolePlatformAdmin, roleTenantAdmin, roleTenantOperator)
	if !ok {
		return
	}
	projectName := strings.TrimSpace(r.PathValue("project"))
	if _, err := s.authorizedRepositoryProject(r, actor, projectName); err != nil {
		writeRepositoryError(w, err)
		return
	}

	var req CreateRepositoryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if req.ProjectName != "" && strings.TrimSpace(req.ProjectName) != projectName {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "projectName must match request path"})
		return
	}
	if req.TenantName != "" || strings.TrimSpace(req.Scope) == "tenant" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "tenant repositories must use the tenant repository endpoint"})
		return
	}
	req.Scope = "project"
	req.ProjectName = projectName
	if err := validateRepositoryRequest(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	sec := repositorySecret(req, actor, projectRepoSecretName(req.Name), map[string]string{
		repoSecretTypeLabel:  repoSecretTypeValue,
		repoSecretScopeKey:   "project",
		repoSecretProjectKey: projectName,
	})
	if err := s.client.Create(r.Context(), sec); err != nil {
		writeError(w, err)
		return
	}

	// Mirror credentials into an Argo CD repository secret so the repo is usable
	// without additional manual configuration.
	if err := s.ensureArgoCDRepoSecretImpl(r, req); err != nil {
		if deleteErr := s.client.Delete(r.Context(), sec); deleteErr != nil && !apierrors.IsNotFound(deleteErr) {
			writeError(w, fmt.Errorf("mirror Argo CD repository secret: %w; rollback repository secret: %v", err, deleteErr))
			return
		}
		writeError(w, fmt.Errorf("mirror Argo CD repository secret: %w", err))
		return
	}

	writeJSON(w, http.StatusCreated, WriteResponse{Name: req.Name, Message: "Repository registered."})
}

// handleCreateTenantRepository stores a repository credential available to a tenant.
func (s *Server) handleCreateTenantRepository(w http.ResponseWriter, r *http.Request) {
	actor, ok := requireRole(w, r, rolePlatformAdmin, roleTenantAdmin, roleTenantOperator)
	if !ok {
		return
	}
	tenantName := strings.TrimSpace(r.PathValue("tenant"))
	if _, err := s.authorizedRepositoryTenant(r, actor, tenantName); err != nil {
		writeRepositoryError(w, err)
		return
	}

	var req CreateRepositoryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if req.TenantName != "" && strings.TrimSpace(req.TenantName) != tenantName {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "tenantName must match request path"})
		return
	}
	if req.ProjectName != "" || strings.TrimSpace(req.Scope) == "project" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "project repositories must use the project repository endpoint"})
		return
	}
	req.Scope = "tenant"
	req.TenantName = tenantName
	if err := validateRepositoryRequest(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	sec := repositorySecret(req, actor, tenantRepoSecretName(tenantName, req.Name), map[string]string{
		repoSecretTypeLabel: repoSecretTypeValue,
		repoSecretScopeKey:  "tenant",
		repoSecretTenantKey: tenantName,
	})
	if err := s.client.Create(r.Context(), sec); err != nil {
		writeError(w, err)
		return
	}

	if err := s.ensureArgoCDRepoSecretImpl(r, req); err != nil {
		if deleteErr := s.client.Delete(r.Context(), sec); deleteErr != nil && !apierrors.IsNotFound(deleteErr) {
			writeError(w, fmt.Errorf("mirror Argo CD repository secret: %w; rollback repository secret: %v", err, deleteErr))
			return
		}
		writeError(w, fmt.Errorf("mirror Argo CD repository secret: %w", err))
		return
	}

	writeJSON(w, http.StatusCreated, WriteResponse{Name: req.Name, Message: "Repository registered."})
}

// handleDeleteProjectRepository removes a repository from the project.
func (s *Server) handleDeleteProjectRepository(w http.ResponseWriter, r *http.Request) {
	actor, ok := requireRole(w, r, rolePlatformAdmin, roleTenantAdmin, roleTenantOperator)
	if !ok {
		return
	}
	projectName := strings.TrimSpace(r.PathValue("project"))
	if _, err := s.authorizedRepositoryProject(r, actor, projectName); err != nil {
		writeRepositoryError(w, err)
		return
	}
	repoName := strings.TrimSpace(r.PathValue("repo"))
	secretName := projectRepoSecretName(repoName)

	var sec corev1.Secret
	if err := s.client.Get(r.Context(), types.NamespacedName{Name: secretName, Namespace: repoSecretNamespace}, &sec); err != nil {
		writeError(w, err)
		return
	}
	if sec.Labels[repoSecretProjectKey] != projectName {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "repository does not belong to this project"})
		return
	}
	dependencies, err := s.repositoryDependencies(r.Context(), sec)
	if err != nil {
		writeError(w, err)
		return
	}
	if len(dependencies) > 0 {
		writeJSON(w, http.StatusConflict, repositoryDependencyConflictResponse{
			Error:        fmt.Sprintf("repository %q is still referenced by active services", repoName),
			Code:         "repository_in_use",
			Dependencies: dependencies,
		})
		return
	}

	if err := s.cleanupArgoCDRepoSecretIfUnused(r, sec); err != nil {
		writeError(w, err)
		return
	}
	if err := s.client.Delete(r.Context(), &sec); err != nil {
		writeError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, WriteResponse{Name: repoName, Message: "Repository removed."})
}

// handleDeleteTenantRepository removes a repository from the tenant.
func (s *Server) handleDeleteTenantRepository(w http.ResponseWriter, r *http.Request) {
	actor, ok := requireRole(w, r, rolePlatformAdmin, roleTenantAdmin, roleTenantOperator)
	if !ok {
		return
	}
	tenantName := strings.TrimSpace(r.PathValue("tenant"))
	if _, err := s.authorizedRepositoryTenant(r, actor, tenantName); err != nil {
		writeRepositoryError(w, err)
		return
	}
	repoName := strings.TrimSpace(r.PathValue("repo"))
	secretName := tenantRepoSecretName(tenantName, repoName)

	var sec corev1.Secret
	if err := s.client.Get(r.Context(), types.NamespacedName{Name: secretName, Namespace: repoSecretNamespace}, &sec); err != nil {
		writeError(w, err)
		return
	}
	if sec.Labels[repoSecretTenantKey] != tenantName {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "repository does not belong to this tenant"})
		return
	}
	dependencies, err := s.repositoryDependencies(r.Context(), sec)
	if err != nil {
		writeError(w, err)
		return
	}
	if len(dependencies) > 0 {
		writeJSON(w, http.StatusConflict, repositoryDependencyConflictResponse{
			Error:        fmt.Sprintf("repository %q is still referenced by active services", repoName),
			Code:         "repository_in_use",
			Dependencies: dependencies,
		})
		return
	}

	if err := s.cleanupArgoCDRepoSecretIfUnused(r, sec); err != nil {
		writeError(w, err)
		return
	}
	if err := s.client.Delete(r.Context(), &sec); err != nil {
		writeError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, WriteResponse{Name: repoName, Message: "Repository removed."})
}

func (s *Server) authorizedRepositoryProject(r *http.Request, actor actor, projectName string) (*platformv1alpha1.Project, error) {
	if projectName == "" {
		return nil, &validationError{msg: "project is required"}
	}
	var project platformv1alpha1.Project
	if err := s.client.Get(r.Context(), types.NamespacedName{Name: projectName}, &project); err != nil {
		return nil, err
	}
	if !s.authorizeProject(r.Context(), actor, &project) {
		return nil, apierrors.NewForbidden(platformv1alpha1.GroupVersion.WithResource("projects").GroupResource(), projectName, nil)
	}
	return &project, nil
}

func (s *Server) authorizedRepositoryTenant(r *http.Request, actor actor, tenantName string) (*platformv1alpha1.Tenant, error) {
	if tenantName == "" {
		return nil, &validationError{msg: "tenant is required"}
	}
	var tenant platformv1alpha1.Tenant
	if err := s.client.Get(r.Context(), types.NamespacedName{Name: tenantName}, &tenant); err != nil {
		return nil, err
	}
	if !tenantVisibleToActor(actor, tenant) {
		return nil, apierrors.NewForbidden(platformv1alpha1.GroupVersion.WithResource("tenants").GroupResource(), tenantName, nil)
	}
	return &tenant, nil
}

func writeRepositoryError(w http.ResponseWriter, err error) {
	switch {
	case err == nil:
		return
	case apierrors.IsForbidden(err):
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "repository scope is outside your authorized tenancy"})
	case apierrors.IsNotFound(err):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
	case isValidationError(err):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
	default:
		writeError(w, err)
	}
}

func repositorySecret(req CreateRepositoryRequest, actor actor, secretName string, labels map[string]string) *corev1.Secret {
	data := map[string][]byte{
		"displayName": []byte(req.DisplayName),
		"url":         []byte(req.URL),
		"authType":    []byte(req.AuthType),
	}
	if req.AuthType == "http" {
		data["username"] = []byte(req.Username)
		data["password"] = []byte(req.Password)
	} else if req.AuthType == "ssh" {
		data["sshPrivateKey"] = []byte(req.SSHKey)
	}

	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: repoSecretNamespace,
			Labels:    labels,
			Annotations: map[string]string{
				"servicer.io/created-by": actor.Name,
				"servicer.io/created-at": time.Now().UTC().Format(time.RFC3339),
			},
		},
		Data: data,
	}
}

func (s *Server) cleanupArgoCDRepoSecretIfUnused(r *http.Request, removed corev1.Secret) error {
	removedReq := repositoryRequestFromSecret(removed)
	argoSecName := argoRepoSecretName(removedReq)
	var secretList corev1.SecretList
	if err := s.client.List(r.Context(), &secretList,
		client.InNamespace(repoSecretNamespace),
		client.MatchingLabels{repoSecretTypeLabel: repoSecretTypeValue},
	); err != nil {
		return fmt.Errorf("list repository secrets before mirror cleanup: %w", err)
	}
	for _, sec := range secretList.Items {
		if sec.Name != removed.Name && argoRepoSecretName(repositoryRequestFromSecret(sec)) == argoSecName {
			return nil
		}
	}

	var argoSec corev1.Secret
	if err := s.client.Get(r.Context(), types.NamespacedName{Name: argoSecName, Namespace: argocdNamespace}, &argoSec); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("get Argo CD repository mirror %q: %w", argoSecName, err)
	}
	if err := s.client.Delete(r.Context(), &argoSec); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("delete Argo CD repository mirror %q: %w", argoSecName, err)
	}
	return nil
}

// ensureArgoCDRepoSecretImpl creates or updates an Argo CD repository Secret so that
// Argo CD can authenticate to the repository without additional manual setup.
func (s *Server) ensureArgoCDRepoSecretImpl(r *http.Request, req CreateRepositoryRequest) error {
	argoSecretData := map[string][]byte{
		"type": []byte("git"),
		"url":  []byte(req.URL),
	}
	if req.AuthType == "http" {
		argoSecretData["username"] = []byte(req.Username)
		argoSecretData["password"] = []byte(req.Password)
	} else if req.AuthType == "ssh" {
		argoSecretData["sshPrivateKey"] = []byte(req.SSHKey)
	}

	argoSecName := argoRepoSecretName(req)
	argoSec := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      argoSecName,
			Namespace: argocdNamespace,
			Labels:    argoRepoSecretLabels(req),
		},
		Data: argoSecretData,
	}

	existing := &corev1.Secret{}
	if err := s.client.Get(r.Context(), types.NamespacedName{Name: argoSecName, Namespace: argocdNamespace}, existing); err != nil {
		if apierrors.IsNotFound(err) {
			return s.client.Create(r.Context(), argoSec)
		}
		return err
	}
	existing.Data = argoSecretData
	existing.Labels = mergeRepositoryLabels(existing.Labels, argoRepoSecretLabels(req))
	return s.client.Update(r.Context(), existing)
}

// repoSecretToSummary converts a K8s Secret to a RepositorySummary.
// Credentials are never included in the summary.
func repoSecretToSummary(sec corev1.Secret) RepositorySummary {
	scope := sec.Labels[repoSecretScopeKey]
	tenantName := sec.Labels[repoSecretTenantKey]
	projectName := sec.Labels[repoSecretProjectKey]
	if scope == "" {
		scope = "project"
		if tenantName != "" {
			scope = "tenant"
		}
	}
	name := strings.TrimPrefix(sec.Name, "repo-")
	if scope == "tenant" && tenantName != "" {
		name = strings.TrimPrefix(sec.Name, "repo-tenant-"+tenantName+"-")
	}
	return RepositorySummary{
		Name:        name,
		DisplayName: string(sec.Data["displayName"]),
		Scope:       scope,
		TenantName:  tenantName,
		ProjectName: projectName,
		URL:         string(sec.Data["url"]),
		AuthType:    string(sec.Data["authType"]),
	}
}

func projectRepoSecretName(repoName string) string {
	return "repo-" + repoName
}

func tenantRepoSecretName(tenantName, repoName string) string {
	return "repo-tenant-" + tenantName + "-" + repoName
}

// argoRepoSecretName derives a stable Argo CD Secret name from repository URL and scope.
func argoRepoSecretName(req CreateRepositoryRequest) string {
	canonicalURL := strings.TrimSpace(req.URL)
	if normalized, _, err := canonicalRepositoryURL(canonicalURL); err == nil {
		canonicalURL = normalized
	}
	hashInput := canonicalURL + "\n" + repositoryScopeHashKey(req)
	sum := sha256.Sum256([]byte(hashInput))
	suffix := hex.EncodeToString(sum[:])[:12]

	const prefix = "argocd-repo-"
	maxBody := 63 - len(prefix) - 1 - len(suffix)
	body := dnsSafeNamePart(canonicalURL)
	if len(body) > maxBody {
		body = strings.TrimRight(body[:maxBody], "-")
	}
	if body == "" {
		body = "git"
	}
	return prefix + body + "-" + suffix
}

func validateRepositoryRequest(req *CreateRepositoryRequest) error {
	if req == nil {
		return &validationError{"request is required"}
	}
	req.Name = strings.TrimSpace(req.Name)
	req.DisplayName = strings.TrimSpace(req.DisplayName)
	req.Scope = strings.TrimSpace(req.Scope)
	req.TenantName = strings.TrimSpace(req.TenantName)
	req.ProjectName = strings.TrimSpace(req.ProjectName)
	req.URL = strings.TrimSpace(req.URL)
	req.AuthType = strings.TrimSpace(req.AuthType)
	req.Username = strings.TrimSpace(req.Username)
	req.Password = strings.TrimSpace(req.Password)
	req.SSHKey = strings.TrimSpace(req.SSHKey)
	if req.Name == "" {
		return &validationError{"name is required"}
	}
	if !productRequestNamePattern.MatchString(req.Name) {
		return &validationError{"name must start with a lowercase letter, end with a lowercase letter or number, and contain only lowercase letters, numbers, and hyphens"}
	}
	if req.DisplayName == "" {
		return &validationError{"displayName is required"}
	}
	scope := req.Scope
	if scope == "" {
		scope = "project"
		if req.TenantName != "" {
			scope = "tenant"
		}
	}
	switch scope {
	case "project":
		if req.ProjectName == "" {
			return &validationError{"projectName is required"}
		}
		if req.TenantName != "" {
			return &validationError{"tenantName is only valid for tenant repositories"}
		}
	case "tenant":
		if req.TenantName == "" {
			return &validationError{"tenantName is required"}
		}
		if req.ProjectName != "" {
			return &validationError{"projectName is only valid for project repositories"}
		}
	default:
		return &validationError{"scope must be tenant or project"}
	}
	if req.URL == "" {
		return &validationError{"url is required"}
	}
	canonicalURL, urlKind, err := canonicalRepositoryURL(req.URL)
	if err != nil {
		return err
	}
	req.URL = canonicalURL
	switch req.AuthType {
	case "none", "":
		req.AuthType = "none"
		if urlKind != "https" {
			return &validationError{"url must use https:// when authType is none"}
		}
		if req.Username != "" || req.Password != "" || req.SSHKey != "" {
			return &validationError{"username, password, and sshKey must be empty when authType is none"}
		}
	case "http":
		if urlKind != "https" {
			return &validationError{"url must use https:// for http auth; plain http and ssh URLs are not allowed"}
		}
		if req.Username == "" || req.Password == "" {
			return &validationError{"username and password are required for http auth"}
		}
		if req.SSHKey != "" {
			return &validationError{"sshKey must be empty for http auth"}
		}
	case "ssh":
		if urlKind != "ssh" {
			return &validationError{"url must use ssh:// or git@host:path form for ssh auth"}
		}
		if req.SSHKey == "" {
			return &validationError{"sshKey is required for ssh auth"}
		}
		if !strings.Contains(req.SSHKey, "PRIVATE KEY") {
			return &validationError{"sshKey must contain an SSH private key block"}
		}
		if req.Username != "" || req.Password != "" {
			return &validationError{"username and password must be empty for ssh auth"}
		}
	default:
		return &validationError{"authType must be none, http, or ssh"}
	}
	return nil
}

func (s *Server) repositoryDependencies(ctx context.Context, repo corev1.Secret) ([]string, error) {
	scope := repo.Labels[repoSecretScopeKey]
	if scope == "" {
		scope = "project"
		if repo.Labels[repoSecretTenantKey] != "" {
			scope = "tenant"
		}
	}

	projectNames := map[string]struct{}{}
	switch scope {
	case "project":
		projectName := repo.Labels[repoSecretProjectKey]
		if projectName != "" {
			projectNames[projectName] = struct{}{}
		}
	case "tenant":
		tenantName := repo.Labels[repoSecretTenantKey]
		var projects platformv1alpha1.ProjectList
		if err := s.client.List(ctx, &projects); err != nil {
			return nil, fmt.Errorf("list projects before repository delete: %w", err)
		}
		for _, project := range projects.Items {
			if project.Spec.TenantRef.Name == tenantName {
				projectNames[project.Name] = struct{}{}
			}
		}
	default:
		return nil, &validationError{msg: "repository scope is invalid"}
	}
	if len(projectNames) == 0 {
		return nil, nil
	}

	summary := repoSecretToSummary(repo)
	repoURL := canonicalRepositoryURLFromSecret(repo)
	var instances platformv1alpha1.ServiceInstanceList
	if err := s.client.List(ctx, &instances); err != nil {
		return nil, fmt.Errorf("list service instances before repository delete: %w", err)
	}
	dependencies := make([]string, 0)
	for _, instance := range instances.Items {
		if instance.DeletionTimestamp != nil {
			continue
		}
		if _, ok := projectNames[instance.Spec.ProjectRef.Name]; !ok {
			continue
		}
		if !serviceInstanceReferencesRepository(instance, summary.Name, repoURL) {
			continue
		}
		kind := "serviceinstances"
		if instance.Spec.ServiceClassRef.Name == "argo-application" {
			kind = "managedapplications"
		}
		dependencies = append(dependencies, kind+"/"+instance.Name)
	}
	sort.Strings(dependencies)
	return dependencies, nil
}

func serviceInstanceReferencesRepository(instance platformv1alpha1.ServiceInstance, repoName, repoURL string) bool {
	if instance.Spec.Parameters == nil || len(instance.Spec.Parameters.Raw) == 0 {
		return false
	}
	var params map[string]any
	if err := json.Unmarshal(instance.Spec.Parameters.Raw, &params); err != nil {
		return false
	}
	if repositoryStringParam(params, "repoRef") == repoName {
		return true
	}
	candidateURL := repositoryStringParam(params, "repoURL")
	if candidateURL == "" {
		return false
	}
	canonicalCandidate, _, err := canonicalRepositoryURL(candidateURL)
	if err != nil {
		canonicalCandidate = strings.TrimSpace(candidateURL)
	}
	return canonicalCandidate != "" && canonicalCandidate == repoURL
}

func repositoryStringParam(params map[string]any, key string) string {
	value, ok := params[key]
	if !ok {
		return ""
	}
	text, ok := value.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(text)
}

func repositoryRequestFromSecret(sec corev1.Secret) CreateRepositoryRequest {
	return CreateRepositoryRequest{
		Name:        repoSecretToSummary(sec).Name,
		DisplayName: string(sec.Data["displayName"]),
		Scope:       sec.Labels[repoSecretScopeKey],
		TenantName:  sec.Labels[repoSecretTenantKey],
		ProjectName: sec.Labels[repoSecretProjectKey],
		URL:         string(sec.Data["url"]),
		AuthType:    string(sec.Data["authType"]),
		Username:    string(sec.Data["username"]),
		Password:    string(sec.Data["password"]),
		SSHKey:      string(sec.Data["sshPrivateKey"]),
	}
}

func canonicalRepositoryURLFromSecret(sec corev1.Secret) string {
	canonical, _, err := canonicalRepositoryURL(string(sec.Data["url"]))
	if err != nil {
		return strings.TrimSpace(string(sec.Data["url"]))
	}
	return canonical
}

func argoRepoSecretLabels(req CreateRepositoryRequest) map[string]string {
	labels := map[string]string{
		"argocd.argoproj.io/secret-type": "repository",
		repositoryMirrorLabel:            "true",
		repoSecretScopeKey:               repositoryScope(req),
	}
	if req.TenantName != "" {
		labels[repoSecretTenantKey] = req.TenantName
	}
	if req.ProjectName != "" {
		labels[repoSecretProjectKey] = req.ProjectName
	}
	return labels
}

func mergeRepositoryLabels(existing, desired map[string]string) map[string]string {
	merged := make(map[string]string, len(existing)+len(desired))
	for key, value := range existing {
		merged[key] = value
	}
	for key, value := range desired {
		merged[key] = value
	}
	return merged
}

func repositoryScope(req CreateRepositoryRequest) string {
	scope := strings.TrimSpace(req.Scope)
	if scope != "" {
		return scope
	}
	if strings.TrimSpace(req.TenantName) != "" {
		return "tenant"
	}
	return "project"
}

func repositoryScopeHashKey(req CreateRepositoryRequest) string {
	scope := repositoryScope(req)
	switch scope {
	case "tenant":
		return "tenant/" + strings.TrimSpace(req.TenantName)
	default:
		return "project/" + strings.TrimSpace(req.ProjectName)
	}
}

func canonicalRepositoryURL(raw string) (string, string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", &validationError{"url is required"}
	}
	if canonical, ok, err := canonicalSCPStyleURL(raw); ok || err != nil {
		return canonical, "ssh", err
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", "", &validationError{msg: "url must be a valid Git repository URL"}
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	switch parsed.Scheme {
	case "https":
		if parsed.User != nil {
			return "", "", &validationError{"url must not include embedded credentials"}
		}
		if err := validateParsedGitURL(parsed); err != nil {
			return "", "", err
		}
		parsed.Host = canonicalURLHost(parsed)
		return parsed.String(), "https", nil
	case "ssh":
		if err := validateParsedGitURL(parsed); err != nil {
			return "", "", err
		}
		parsed.Host = canonicalURLHost(parsed)
		return parsed.String(), "ssh", nil
	case "http":
		return "", "", &validationError{"url must use https://; plain http is not allowed"}
	default:
		return "", "", &validationError{"url scheme must be https or ssh"}
	}
}

func canonicalSCPStyleURL(raw string) (string, bool, error) {
	if strings.Contains(raw, "://") {
		return "", false, nil
	}
	at := strings.Index(raw, "@")
	if at <= 0 {
		return "", false, nil
	}
	colon := strings.Index(raw[at+1:], ":")
	if colon < 0 {
		return "", false, nil
	}
	colon += at + 1
	user := raw[:at]
	host := raw[at+1 : colon]
	repoPath := raw[colon+1:]
	if user == "" || containsControlOrSpace(user) || strings.ContainsAny(user, "/\\:") {
		return "", true, &validationError{"ssh repository URL user is invalid"}
	}
	if err := validateRepositoryHost(host); err != nil {
		return "", true, err
	}
	if err := validateRepositoryPath(repoPath); err != nil {
		return "", true, err
	}
	u := url.URL{
		Scheme: "ssh",
		User:   url.User(user),
		Host:   strings.ToLower(host),
		Path:   "/" + strings.TrimLeft(repoPath, "/"),
	}
	return u.String(), true, nil
}

func validateParsedGitURL(parsed *url.URL) error {
	if parsed == nil {
		return &validationError{"url must be a valid Git repository URL"}
	}
	if strings.TrimSpace(parsed.Host) == "" || strings.TrimSpace(parsed.Hostname()) == "" {
		return &validationError{"url host is required"}
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return &validationError{"url must not include query strings or fragments"}
	}
	if err := validateRepositoryHost(parsed.Hostname()); err != nil {
		return err
	}
	if port := parsed.Port(); port != "" {
		value, err := strconv.Atoi(port)
		if err != nil || value <= 0 || value > 65535 {
			return &validationError{"url port is invalid"}
		}
	}
	return validateRepositoryPath(parsed.Path)
}

func validateRepositoryHost(host string) error {
	host = strings.TrimSpace(strings.TrimSuffix(host, "."))
	if host == "" {
		return &validationError{"url host is required"}
	}
	if containsControlOrSpace(host) || strings.Contains(host, "_") {
		return &validationError{"url host must be a valid DNS name or IP address"}
	}
	if ip := net.ParseIP(host); ip != nil {
		return nil
	}
	labels := strings.Split(host, ".")
	for _, label := range labels {
		if label == "" || len(label) > 63 {
			return &validationError{"url host must be a valid DNS name or IP address"}
		}
		if label[0] == '-' || label[len(label)-1] == '-' {
			return &validationError{"url host must be a valid DNS name or IP address"}
		}
		for _, ch := range label {
			if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '-' {
				continue
			}
			return &validationError{"url host must be a valid DNS name or IP address"}
		}
	}
	return nil
}

func validateRepositoryPath(pathValue string) error {
	pathValue = strings.TrimSpace(pathValue)
	if pathValue == "" || pathValue == "/" {
		return &validationError{"url path is required"}
	}
	if containsControlOrSpace(pathValue) {
		return &validationError{"url path must not contain whitespace or control characters"}
	}
	return nil
}

func canonicalURLHost(parsed *url.URL) string {
	host := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	if port := parsed.Port(); port != "" {
		return net.JoinHostPort(host, port)
	}
	if strings.Contains(host, ":") {
		return "[" + host + "]"
	}
	return host
}

func containsControlOrSpace(value string) bool {
	for _, ch := range value {
		if ch <= ' ' || ch == 0x7f {
			return true
		}
	}
	return false
}

func dnsSafeNamePart(value string) string {
	var builder strings.Builder
	lastDash := false
	for _, ch := range strings.ToLower(value) {
		keep := (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9')
		if keep {
			builder.WriteRune(ch)
			lastDash = false
			continue
		}
		if !lastDash {
			builder.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(builder.String(), "-")
}

type validationError struct{ msg string }

func (e *validationError) Error() string { return e.msg }

func isValidationError(err error) bool {
	var validationErr *validationError
	return errors.As(err, &validationErr)
}
