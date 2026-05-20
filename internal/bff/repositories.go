package bff

import (
	"encoding/json"
	"errors"
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
	repoSecretProjectKey = "servicer.io/project"
	argocdNamespace      = "argocd"
)

// handleListProjectRepositories returns all repositories stored under a project.
func (s *Server) handleListProjectRepositories(w http.ResponseWriter, r *http.Request) {
	actor, ok := requireRole(w, r, rolePlatformAdmin, roleTenantOperator, roleServiceConsumer)
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

// handleCreateProjectRepository stores a new repository credential under the project.
// It also creates or updates an Argo CD repository secret so the repo is immediately usable.
func (s *Server) handleCreateProjectRepository(w http.ResponseWriter, r *http.Request) {
	actor, ok := requireRole(w, r, rolePlatformAdmin, roleTenantOperator)
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
	req.ProjectName = projectName
	if err := validateRepositoryRequest(req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	secretName := "repo-" + req.Name
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

	sec := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: repoSecretNamespace,
			Labels: map[string]string{
				repoSecretTypeLabel:  repoSecretTypeValue,
				repoSecretProjectKey: projectName,
			},
			Annotations: map[string]string{
				"servicer.io/created-by": actor.Name,
				"servicer.io/created-at": time.Now().UTC().Format(time.RFC3339),
			},
		},
		Data: data,
	}
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

// handleDeleteProjectRepository removes a repository from the project.
func (s *Server) handleDeleteProjectRepository(w http.ResponseWriter, r *http.Request) {
	actor, ok := requireRole(w, r, rolePlatformAdmin, roleTenantOperator)
	if !ok {
		return
	}
	projectName := strings.TrimSpace(r.PathValue("project"))
	if _, err := s.authorizedRepositoryProject(r, actor, projectName); err != nil {
		writeRepositoryError(w, err)
		return
	}
	repoName := strings.TrimSpace(r.PathValue("repo"))
	secretName := "repo-" + repoName

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

	// Best-effort cleanup of the mirrored Argo CD repo secret.
	var argoSec corev1.Secret
	argoSecName := argoRepoSecretName(string(sec.Data["url"]))
	if err := s.client.Get(r.Context(), types.NamespacedName{Name: argoSecName, Namespace: argocdNamespace}, &argoSec); err == nil {
		_ = s.client.Delete(r.Context(), &argoSec)
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

func writeRepositoryError(w http.ResponseWriter, err error) {
	switch {
	case err == nil:
		return
	case apierrors.IsForbidden(err):
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "project is outside your authorized tenancy"})
	case apierrors.IsNotFound(err):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
	case isValidationError(err):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
	default:
		writeError(w, err)
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
	name := strings.TrimPrefix(sec.Name, "repo-")
	return RepositorySummary{
		Name:        name,
		DisplayName: string(sec.Data["displayName"]),
		ProjectName: sec.Labels[repoSecretProjectKey],
		URL:         string(sec.Data["url"]),
		AuthType:    string(sec.Data["authType"]),
	}
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
	if req.ProjectName == "" {
		return &validationError{"projectName is required"}
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
