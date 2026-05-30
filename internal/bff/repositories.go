package bff

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"net/http"
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
	if err := validateRepositoryRequest(req); err != nil {
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
		// Non-fatal: Argo CD namespace may not be present in all environments.
		_ = err
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
	if err := validateRepositoryRequest(req); err != nil {
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
		_ = err
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
	if err := s.client.Delete(r.Context(), &sec); err != nil {
		writeError(w, err)
		return
	}

	s.cleanupArgoCDRepoSecretIfUnused(r, sec)

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
	if err := s.client.Delete(r.Context(), &sec); err != nil {
		writeError(w, err)
		return
	}

	s.cleanupArgoCDRepoSecretIfUnused(r, sec)

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

func (s *Server) cleanupArgoCDRepoSecretIfUnused(r *http.Request, removed corev1.Secret) {
	url := string(removed.Data["url"])
	if url == "" {
		return
	}
	var secretList corev1.SecretList
	if err := s.client.List(r.Context(), &secretList,
		client.InNamespace(repoSecretNamespace),
		client.MatchingLabels{repoSecretTypeLabel: repoSecretTypeValue},
	); err != nil {
		return
	}
	for _, sec := range secretList.Items {
		if sec.Name != removed.Name && string(sec.Data["url"]) == url {
			return
		}
	}

	var argoSec corev1.Secret
	argoSecName := argoRepoSecretName(url)
	if err := s.client.Get(r.Context(), types.NamespacedName{Name: argoSecName, Namespace: argocdNamespace}, &argoSec); err == nil {
		_ = s.client.Delete(r.Context(), &argoSec)
	}
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

	argoSecName := argoRepoSecretName(req.URL)
	argoSec := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      argoSecName,
			Namespace: argocdNamespace,
			Labels: map[string]string{
				"argocd.argoproj.io/secret-type": "repository",
			},
		},
		Data: argoSecretData,
	}

	existing := &corev1.Secret{}
	if err := s.client.Get(r.Context(), types.NamespacedName{Name: argoSecName, Namespace: argocdNamespace}, existing); err != nil {
		return s.client.Create(r.Context(), argoSec)
	}
	existing.Data = argoSecretData
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

// argoRepoSecretName derives a stable Argo CD Secret name from a repository URL.
func argoRepoSecretName(url string) string {
	// Strip scheme and replace non-alphanumeric chars for a valid k8s name.
	url = strings.TrimPrefix(url, "https://")
	url = strings.TrimPrefix(url, "http://")
	url = strings.TrimPrefix(url, "git@")
	replacer := strings.NewReplacer("/", "-", ":", "-", ".", "-", "_", "-")
	name := "argocd-repo-" + replacer.Replace(url)
	if len(name) > 63 {
		name = name[:63]
	}
	name = strings.TrimRight(name, "-")
	return name
}

func validateRepositoryRequest(req CreateRepositoryRequest) error {
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
	switch req.AuthType {
	case "none", "":
	case "http":
		if req.Username == "" || req.Password == "" {
			return &validationError{"username and password are required for http auth"}
		}
	case "ssh":
		if req.SSHKey == "" {
			return &validationError{"sshKey is required for ssh auth"}
		}
	default:
		return &validationError{"authType must be none, http, or ssh"}
	}
	return nil
}

type validationError struct{ msg string }

func (e *validationError) Error() string { return e.msg }

func isValidationError(err error) bool {
	var validationErr *validationError
	return errors.As(err, &validationErr)
}
