package bff

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"

	platformv1alpha1 "github.com/sindef/servicer/api/v1alpha1"
	"golang.org/x/crypto/bcrypt"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const authSecretNamespace = "servicer-system"
const (
	roleDefinitionLabelKey   = "servicer.io/type"
	roleDefinitionLabelValue = "auth-role"
	roleDefinitionDataKey    = "role.json"
	roleDefinitionPrefix     = "servicer-role-"
)

type authAdminValidationResponse struct {
	Error       string                          `json:"error"`
	Code        string                          `json:"code"`
	FieldErrors []authAdminFieldValidationError `json:"fieldErrors,omitempty"`
}

type authAdminFieldValidationError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

type authAdminValidationError struct {
	message string
	fields  []authAdminFieldValidationError
}

func (e *authAdminValidationError) Error() string {
	if e == nil {
		return ""
	}
	return e.message
}

func newAuthAdminValidationError(message string, fields ...authAdminFieldValidationError) *authAdminValidationError {
	if message == "" && len(fields) == 1 {
		message = fields[0].Message
	}
	if message == "" {
		message = "validation failed"
	}
	return &authAdminValidationError{message: message, fields: fields}
}

func authAdminFieldError(field, message string) authAdminFieldValidationError {
	return authAdminFieldValidationError{Field: field, Message: message}
}

func writeInvalidAuthAdminRequest(w http.ResponseWriter) {
	writeJSON(w, http.StatusBadRequest, authAdminValidationResponse{
		Error: "invalid request body",
		Code:  "invalid_request_body",
	})
}

func writeAuthAdminValidationError(w http.ResponseWriter, err error) {
	if err == nil {
		err = newAuthAdminValidationError("validation failed")
	}
	var validationErr *authAdminValidationError
	if errors.As(err, &validationErr) {
		writeJSON(w, http.StatusBadRequest, authAdminValidationResponse{
			Error:       validationErr.Error(),
			Code:        "validation_failed",
			FieldErrors: validationErr.fields,
		})
		return
	}
	writeJSON(w, http.StatusBadRequest, authAdminValidationResponse{
		Error: err.Error(),
		Code:  "validation_failed",
	})
}

type authAdminDependencyConflictResponse struct {
	Error        string   `json:"error"`
	Code         string   `json:"code"`
	Dependencies []string `json:"dependencies,omitempty"`
}

func writeAuthAdminDependencyConflict(w http.ResponseWriter, resource, name string, dependencies []string) {
	sort.Strings(dependencies)
	writeJSON(w, http.StatusConflict, authAdminDependencyConflictResponse{
		Error:        fmt.Sprintf("%s %q is still referenced", resource, name),
		Code:         "dependency_conflict",
		Dependencies: dependencies,
	})
}

func (s *Server) handleListAuthProviders(w http.ResponseWriter, r *http.Request) {
	if _, ok := requirePlatformRole(w, r, rolePlatformAdmin); !ok {
		return
	}
	var list platformv1alpha1.AuthProviderList
	if err := s.client.List(r.Context(), &list); err != nil {
		writeError(w, err)
		return
	}
	response := make([]AuthProviderSummary, 0, len(list.Items))
	for _, provider := range list.Items {
		summary := AuthProviderSummary{
			Name:        provider.Name,
			DisplayName: provider.Spec.DisplayName,
			Type:        string(provider.Spec.Type),
			Enabled:     provider.Spec.Enabled,
			Default:     provider.Spec.Default,
			Phase:       provider.Status.Phase,
		}
		switch provider.Spec.Type {
		case platformv1alpha1.AuthProviderTypeOIDC:
			if provider.Spec.OIDC != nil {
				summary.OIDCIssuerURL = provider.Spec.OIDC.IssuerURL
				summary.OIDCClientID = provider.Spec.OIDC.ClientID
				summary.OIDCScopes = append([]string(nil), provider.Spec.OIDC.Scopes...)
				summary.OIDCUsernameClaim = provider.Spec.OIDC.UsernameClaim
				summary.OIDCEmailClaim = provider.Spec.OIDC.EmailClaim
				summary.OIDCRolesClaim = provider.Spec.OIDC.RolesClaim
				summary.OIDCGroupsClaim = provider.Spec.OIDC.GroupsClaim
				summary.OIDCRedirectPath = provider.Spec.OIDC.RedirectPath
				summary.OIDCEndSessionURL = provider.Spec.OIDC.EndSessionURL
				summary.SecretConfigured = provider.Spec.OIDC.ClientSecretRef.Name != ""
			}
		case platformv1alpha1.AuthProviderTypeLDAP:
			if provider.Spec.LDAP != nil {
				summary.LDAPURL = provider.Spec.LDAP.URL
				summary.LDAPUserBaseDN = provider.Spec.LDAP.UserBaseDN
				summary.LDAPUserFilter = provider.Spec.LDAP.UserFilter
				summary.LDAPUsernameAttribute = provider.Spec.LDAP.UsernameAttribute
				summary.LDAPEmailAttribute = provider.Spec.LDAP.EmailAttribute
				summary.LDAPGroupBaseDN = provider.Spec.LDAP.GroupBaseDN
				summary.LDAPGroupFilter = provider.Spec.LDAP.GroupFilter
				summary.LDAPGroupNameAttr = provider.Spec.LDAP.GroupNameAttribute
				summary.LDAPStartTLS = provider.Spec.LDAP.StartTLS
				summary.InsecureSkipVerify = provider.Spec.LDAP.InsecureSkipVerify
				summary.SecretConfigured = provider.Spec.LDAP.BindSecretRef.Name != ""
			}
		}
		response = append(response, summary)
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleCreateAuthProvider(w http.ResponseWriter, r *http.Request) {
	if _, ok := requirePlatformRole(w, r, rolePlatformAdmin); !ok {
		return
	}
	var req AuthProviderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeInvalidAuthAdminRequest(w)
		return
	}
	provider, secrets, err := authProviderFromRequest(req)
	if err != nil {
		writeAuthAdminValidationError(w, err)
		return
	}
	if err := applyAuthProviderSecretRequirements(req, provider, nil); err != nil {
		writeAuthAdminValidationError(w, err)
		return
	}
	for _, secret := range secrets {
		if err := s.upsertSecret(r.Context(), secret); err != nil {
			writeError(w, err)
			return
		}
	}
	if err := s.client.Create(r.Context(), provider); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, WriteResponse{Name: provider.Name, Message: "Auth provider created."})
}

func (s *Server) handleUpdateAuthProvider(w http.ResponseWriter, r *http.Request) {
	if _, ok := requirePlatformRole(w, r, rolePlatformAdmin); !ok {
		return
	}
	name := strings.TrimSpace(r.PathValue("name"))
	var req AuthProviderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeInvalidAuthAdminRequest(w)
		return
	}
	req.Name = name
	provider, secrets, err := authProviderFromRequest(req)
	if err != nil {
		writeAuthAdminValidationError(w, err)
		return
	}
	var existing platformv1alpha1.AuthProvider
	if err := s.client.Get(r.Context(), types.NamespacedName{Name: name}, &existing); err != nil {
		writeError(w, err)
		return
	}
	if err := applyAuthProviderSecretRequirements(req, provider, &existing); err != nil {
		writeAuthAdminValidationError(w, err)
		return
	}
	for _, secret := range secrets {
		if err := s.upsertSecret(r.Context(), secret); err != nil {
			writeError(w, err)
			return
		}
	}
	existing.Spec = provider.Spec
	if err := s.client.Update(r.Context(), &existing); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, WriteResponse{Name: existing.Name, Message: "Auth provider updated."})
}

func (s *Server) handleDeleteAuthProvider(w http.ResponseWriter, r *http.Request) {
	if _, ok := requirePlatformRole(w, r, rolePlatformAdmin); !ok {
		return
	}
	name := strings.TrimSpace(r.PathValue("name"))
	var existing platformv1alpha1.AuthProvider
	if err := s.client.Get(r.Context(), types.NamespacedName{Name: name}, &existing); err != nil {
		writeError(w, err)
		return
	}
	dependencies, err := s.authProviderDeleteDependencies(r.Context(), existing)
	if err != nil {
		writeError(w, err)
		return
	}
	if len(dependencies) > 0 {
		writeAuthAdminDependencyConflict(w, "auth provider", name, dependencies)
		return
	}
	if err := s.client.Delete(r.Context(), &existing); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, WriteResponse{Name: name, Message: "Auth provider deleted."})
}

func authProviderFromRequest(req AuthProviderRequest) (*platformv1alpha1.AuthProvider, []*corev1.Secret, error) {
	name := strings.TrimSpace(req.Name)
	displayName := strings.TrimSpace(req.DisplayName)
	providerType := platformv1alpha1.AuthProviderType(strings.TrimSpace(req.Type))
	fields := []authAdminFieldValidationError{}
	if name == "" {
		fields = append(fields, authAdminFieldError("name", "name is required"))
	}
	if displayName == "" {
		fields = append(fields, authAdminFieldError("displayName", "displayName is required"))
	}
	if req.Default && !req.Enabled {
		fields = append(fields, authAdminFieldError("default", "default providers must be enabled"))
	}
	switch providerType {
	case platformv1alpha1.AuthProviderTypeLocal:
	case platformv1alpha1.AuthProviderTypeOIDC:
		if strings.TrimSpace(req.OIDCIssuerURL) == "" {
			fields = append(fields, authAdminFieldError("oidcIssuerUrl", "issuer URL is required"))
		}
		if strings.TrimSpace(req.OIDCClientID) == "" {
			fields = append(fields, authAdminFieldError("oidcClientId", "client ID is required"))
		}
	case platformv1alpha1.AuthProviderTypeLDAP:
		if strings.TrimSpace(req.LDAPURL) == "" {
			fields = append(fields, authAdminFieldError("ldapUrl", "LDAP URL is required"))
		}
		if strings.TrimSpace(req.LDAPUserBaseDN) == "" {
			fields = append(fields, authAdminFieldError("ldapUserBaseDn", "LDAP user base DN is required"))
		}
		if strings.TrimSpace(req.LDAPUserFilter) == "" {
			fields = append(fields, authAdminFieldError("ldapUserFilter", "LDAP user filter is required"))
		}
	default:
		fields = append(fields, authAdminFieldError("type", fmt.Sprintf("unsupported provider type %q", req.Type)))
	}
	if len(fields) > 0 {
		return nil, nil, newAuthAdminValidationError("auth provider validation failed", fields...)
	}
	provider := &platformv1alpha1.AuthProvider{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: platformv1alpha1.AuthProviderSpec{
			DisplayName: displayName,
			Type:        providerType,
			Enabled:     req.Enabled,
			Default:     req.Default,
		},
	}
	secrets := []*corev1.Secret{}
	switch providerType {
	case platformv1alpha1.AuthProviderTypeLocal:
	case platformv1alpha1.AuthProviderTypeOIDC:
		provider.Spec.OIDC = &platformv1alpha1.OIDCAuthProviderSpec{
			IssuerURL:     req.OIDCIssuerURL,
			ClientID:      req.OIDCClientID,
			UsernameClaim: req.OIDCUsernameClaim,
			EmailClaim:    req.OIDCEmailClaim,
			RolesClaim:    req.OIDCRolesClaim,
			GroupsClaim:   req.OIDCGroupsClaim,
			Scopes:        append([]string(nil), req.OIDCScopes...),
			RedirectPath:  req.OIDCRedirectPath,
			EndSessionURL: req.OIDCEndSessionURL,
		}
		if secret := strings.TrimSpace(req.OIDCClientSecret); secret != "" {
			secretName := fmt.Sprintf("auth-provider-%s-oidc", name)
			secrets = append(secrets, &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: authSecretNamespace},
				StringData: map[string]string{"clientSecret": secret},
			})
			provider.Spec.OIDC.ClientSecretRef = platformv1alpha1.NamespacedObjectReference{Name: secretName, Namespace: authSecretNamespace}
		}
	case platformv1alpha1.AuthProviderTypeLDAP:
		provider.Spec.LDAP = &platformv1alpha1.LDAPAuthProviderSpec{
			URL:                req.LDAPURL,
			UserBaseDN:         req.LDAPUserBaseDN,
			UserFilter:         req.LDAPUserFilter,
			UsernameAttribute:  req.LDAPUsernameAttribute,
			EmailAttribute:     req.LDAPEmailAttribute,
			GroupBaseDN:        req.LDAPGroupBaseDN,
			GroupFilter:        req.LDAPGroupFilter,
			GroupNameAttribute: req.LDAPGroupNameAttr,
			StartTLS:           req.LDAPStartTLS,
			InsecureSkipVerify: req.InsecureSkipVerify,
		}
		if strings.TrimSpace(req.LDAPBindUsername) != "" || strings.TrimSpace(req.LDAPBindPassword) != "" {
			secretName := fmt.Sprintf("auth-provider-%s-ldap", name)
			secrets = append(secrets, &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: authSecretNamespace},
				StringData: map[string]string{
					"username": req.LDAPBindUsername,
					"password": req.LDAPBindPassword,
				},
			})
			provider.Spec.LDAP.BindSecretRef = platformv1alpha1.NamespacedObjectReference{Name: secretName, Namespace: authSecretNamespace}
		}
	}
	return provider, secrets, nil
}

func applyAuthProviderSecretRequirements(req AuthProviderRequest, provider *platformv1alpha1.AuthProvider, existing *platformv1alpha1.AuthProvider) error {
	switch provider.Spec.Type {
	case platformv1alpha1.AuthProviderTypeOIDC:
		if provider.Spec.OIDC == nil {
			return newAuthAdminValidationError("OIDC config is required", authAdminFieldError("type", "OIDC config is required"))
		}
		if provider.Spec.OIDC.ClientSecretRef.Name != "" {
			return nil
		}
		if existing != nil && existing.Spec.Type == platformv1alpha1.AuthProviderTypeOIDC && existing.Spec.OIDC != nil && existing.Spec.OIDC.ClientSecretRef.Name != "" {
			provider.Spec.OIDC.ClientSecretRef = existing.Spec.OIDC.ClientSecretRef
			return nil
		}
		return newAuthAdminValidationError(
			"OIDC client secret is required",
			authAdminFieldError("oidcClientSecret", "client secret is required when creating or switching to an OIDC provider"),
		)
	case platformv1alpha1.AuthProviderTypeLDAP:
		if provider.Spec.LDAP == nil {
			return newAuthAdminValidationError("LDAP config is required", authAdminFieldError("type", "LDAP config is required"))
		}
		bindUsernameSet := strings.TrimSpace(req.LDAPBindUsername) != ""
		bindPasswordSet := strings.TrimSpace(req.LDAPBindPassword) != ""
		if bindUsernameSet != bindPasswordSet {
			fields := []authAdminFieldValidationError{}
			if !bindUsernameSet {
				fields = append(fields, authAdminFieldError("ldapBindUsername", "bind username is required when bind password is provided"))
			}
			if !bindPasswordSet {
				fields = append(fields, authAdminFieldError("ldapBindPassword", "bind password is required when bind username is provided"))
			}
			return newAuthAdminValidationError("LDAP bind credentials are incomplete", fields...)
		}
		if provider.Spec.LDAP.BindSecretRef.Name != "" {
			return nil
		}
		if existing != nil && existing.Spec.Type == platformv1alpha1.AuthProviderTypeLDAP && existing.Spec.LDAP != nil && existing.Spec.LDAP.BindSecretRef.Name != "" {
			provider.Spec.LDAP.BindSecretRef = existing.Spec.LDAP.BindSecretRef
			return nil
		}
		return newAuthAdminValidationError(
			"LDAP bind credentials are required",
			authAdminFieldError("ldapBindUsername", "bind username is required when creating or switching to an LDAP provider"),
			authAdminFieldError("ldapBindPassword", "bind password is required when creating or switching to an LDAP provider"),
		)
	default:
		return nil
	}
}

func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	if _, ok := requirePlatformRole(w, r, rolePlatformAdmin); !ok {
		return
	}
	var list platformv1alpha1.UserList
	if err := s.client.List(r.Context(), &list); err != nil {
		writeError(w, err)
		return
	}
	response := make([]UserSummary, 0, len(list.Items))
	for _, user := range list.Items {
		summary := UserSummary{
			Name:             user.Name,
			DisplayName:      user.Spec.DisplayName,
			Email:            user.Spec.Email,
			LocalAuthEnabled: user.Spec.LocalAuth != nil && user.Spec.LocalAuth.Enabled,
		}
		for _, external := range user.Spec.ExternalIdentities {
			summary.ExternalIdentities = append(summary.ExternalIdentities, ExternalIdentitySummary{
				Provider: external.ProviderRef.Name,
				Subject:  external.Subject,
			})
		}
		response = append(response, summary)
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	if _, ok := requirePlatformRole(w, r, rolePlatformAdmin); !ok {
		return
	}
	var req UserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeInvalidAuthAdminRequest(w)
		return
	}
	if err := validateCreateUserRequest(req); err != nil {
		writeAuthAdminValidationError(w, err)
		return
	}
	user, secret, err := userFromRequest(req)
	if err != nil {
		writeAuthAdminValidationError(w, err)
		return
	}
	if secret != nil {
		if err := s.upsertSecret(r.Context(), secret); err != nil {
			writeError(w, err)
			return
		}
	}
	if err := s.client.Create(r.Context(), user); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, WriteResponse{Name: user.Name, Message: "User created."})
}

func (s *Server) handleUpdateUser(w http.ResponseWriter, r *http.Request) {
	if _, ok := requirePlatformRole(w, r, rolePlatformAdmin); !ok {
		return
	}
	name := strings.TrimSpace(r.PathValue("name"))
	var req UserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeInvalidAuthAdminRequest(w)
		return
	}
	req.Name = name
	var existing platformv1alpha1.User
	if err := s.client.Get(r.Context(), types.NamespacedName{Name: name}, &existing); err != nil {
		writeError(w, err)
		return
	}
	if err := validateUpdateUserRequest(req, existing); err != nil {
		writeAuthAdminValidationError(w, err)
		return
	}
	user, secret, err := userFromRequest(req)
	if err != nil {
		writeAuthAdminValidationError(w, err)
		return
	}
	if user.Spec.LocalAuth != nil && secret == nil {
		user.Spec.LocalAuth.PasswordHashSecretRef = existing.Spec.LocalAuth.PasswordHashSecretRef
	}
	if secret != nil {
		if err := s.upsertSecret(r.Context(), secret); err != nil {
			writeError(w, err)
			return
		}
	}
	existing.Spec = user.Spec
	if err := s.client.Update(r.Context(), &existing); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, WriteResponse{Name: existing.Name, Message: "User updated."})
}

func (s *Server) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	if _, ok := requirePlatformRole(w, r, rolePlatformAdmin); !ok {
		return
	}
	name := strings.TrimSpace(r.PathValue("name"))
	var existing platformv1alpha1.User
	if err := s.client.Get(r.Context(), types.NamespacedName{Name: name}, &existing); err != nil {
		writeError(w, err)
		return
	}
	dependencies, err := s.userDeleteDependencies(r.Context(), name)
	if err != nil {
		writeError(w, err)
		return
	}
	if len(dependencies) > 0 {
		writeAuthAdminDependencyConflict(w, "user", name, dependencies)
		return
	}
	if err := s.client.Delete(r.Context(), &existing); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, WriteResponse{Name: name, Message: "User deleted."})
}

func userFromRequest(req UserRequest) (*platformv1alpha1.User, *corev1.Secret, error) {
	if strings.TrimSpace(req.Name) == "" {
		return nil, nil, newAuthAdminValidationError("name is required", authAdminFieldError("name", "name is required"))
	}
	user := &platformv1alpha1.User{
		ObjectMeta: metav1.ObjectMeta{Name: req.Name},
		Spec: platformv1alpha1.UserSpec{
			DisplayName: req.DisplayName,
			Email:       req.Email,
		},
	}
	for _, external := range req.ExternalIdentities {
		if strings.TrimSpace(external.Provider) == "" || strings.TrimSpace(external.Subject) == "" {
			continue
		}
		user.Spec.ExternalIdentities = append(user.Spec.ExternalIdentities, platformv1alpha1.ExternalIdentitySpec{
			ProviderRef: platformv1alpha1.LocalObjectReference{Name: external.Provider},
			Subject:     external.Subject,
		})
	}
	var secret *corev1.Secret
	if req.LocalAuthEnabled {
		secretName := localUserSecretName(req.Name)
		if strings.TrimSpace(req.Password) != "" {
			hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
			if err != nil {
				return nil, nil, err
			}
			secret = &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: authSecretNamespace},
				StringData: map[string]string{"passwordHash": string(hash)},
			}
		}
		user.Spec.LocalAuth = &platformv1alpha1.LocalAuthSpec{
			Enabled:               true,
			PasswordHashSecretRef: platformv1alpha1.NamespacedObjectReference{Name: secretName, Namespace: authSecretNamespace},
		}
	}
	return user, secret, nil
}

func validateCreateUserRequest(req UserRequest) error {
	fields := validateUserRequest(req)
	if req.LocalAuthEnabled && strings.TrimSpace(req.Password) == "" {
		fields = append(fields, authAdminFieldError("password", "password is required when local auth is enabled"))
	}
	if len(fields) > 0 {
		return newAuthAdminValidationError("user validation failed", fields...)
	}
	return nil
}

func validateUpdateUserRequest(req UserRequest, existing platformv1alpha1.User) error {
	fields := validateUserRequest(req)
	if req.LocalAuthEnabled && strings.TrimSpace(req.Password) == "" && !hasExistingLocalAuthSecret(existing) {
		fields = append(fields, authAdminFieldError("password", "password is required when enabling local auth for a user without an existing password secret"))
	}
	if len(fields) > 0 {
		return newAuthAdminValidationError("user validation failed", fields...)
	}
	return nil
}

func validateUserRequest(req UserRequest) []authAdminFieldValidationError {
	fields := []authAdminFieldValidationError{}
	if strings.TrimSpace(req.Name) == "" {
		fields = append(fields, authAdminFieldError("name", "name is required"))
	}
	externalIdentityCount, externalIdentityErrors := validateExternalIdentities(req.ExternalIdentities)
	fields = append(fields, externalIdentityErrors...)
	if !req.LocalAuthEnabled && externalIdentityCount == 0 {
		fields = append(fields, authAdminFieldError("externalIdentities", "at least one local or external identity is required"))
	}
	return fields
}

func validateExternalIdentities(identities []ExternalIdentitySummary) (int, []authAdminFieldValidationError) {
	fields := []authAdminFieldValidationError{}
	count := 0
	for i, identity := range identities {
		provider := strings.TrimSpace(identity.Provider)
		subject := strings.TrimSpace(identity.Subject)
		if provider == "" && subject == "" {
			continue
		}
		fieldPrefix := fmt.Sprintf("externalIdentities[%d]", i)
		if provider == "" {
			fields = append(fields, authAdminFieldError(fieldPrefix+".provider", "provider is required"))
		}
		if subject == "" {
			fields = append(fields, authAdminFieldError(fieldPrefix+".subject", "subject is required"))
		}
		if provider != "" && subject != "" {
			count++
		}
	}
	return count, fields
}

func hasExistingLocalAuthSecret(user platformv1alpha1.User) bool {
	return user.Spec.LocalAuth != nil &&
		user.Spec.LocalAuth.Enabled &&
		strings.TrimSpace(user.Spec.LocalAuth.PasswordHashSecretRef.Name) != "" &&
		strings.TrimSpace(user.Spec.LocalAuth.PasswordHashSecretRef.Namespace) != ""
}

func (s *Server) authProviderDeleteDependencies(ctx context.Context, provider platformv1alpha1.AuthProvider) ([]string, error) {
	dependencies := []string{}
	localProviderRequired := false
	if provider.Spec.Type == platformv1alpha1.AuthProviderTypeLocal && provider.Spec.Enabled {
		hasAlternate, err := s.hasAlternateEnabledLocalProvider(ctx, provider.Name)
		if err != nil {
			return nil, err
		}
		localProviderRequired = !hasAlternate
	}
	var users platformv1alpha1.UserList
	if err := s.client.List(ctx, &users); err != nil {
		return nil, err
	}
	for _, user := range users.Items {
		if localProviderRequired && user.Spec.LocalAuth != nil && user.Spec.LocalAuth.Enabled {
			dependencies = append(dependencies, fmt.Sprintf("users/%s local auth", user.Name))
		}
		for _, identity := range user.Spec.ExternalIdentities {
			if identity.ProviderRef.Name == provider.Name {
				dependencies = append(dependencies, fmt.Sprintf("users/%s external identity", user.Name))
				break
			}
		}
	}

	var groups platformv1alpha1.GroupList
	if err := s.client.List(ctx, &groups); err != nil {
		return nil, err
	}
	for _, group := range groups.Items {
		for _, external := range group.Spec.ExternalGroups {
			if external.ProviderRef.Name == provider.Name {
				dependencies = append(dependencies, fmt.Sprintf("groups/%s external group", group.Name))
				break
			}
		}
	}
	return uniqueSortedStrings(dependencies), nil
}

func (s *Server) hasAlternateEnabledLocalProvider(ctx context.Context, name string) (bool, error) {
	var providers platformv1alpha1.AuthProviderList
	if err := s.client.List(ctx, &providers); err != nil {
		return false, err
	}
	for _, provider := range providers.Items {
		if provider.Name != name && provider.Spec.Type == platformv1alpha1.AuthProviderTypeLocal && provider.Spec.Enabled {
			return true, nil
		}
	}
	return false, nil
}

func (s *Server) userDeleteDependencies(ctx context.Context, name string) ([]string, error) {
	dependencies := []string{}
	var groups platformv1alpha1.GroupList
	if err := s.client.List(ctx, &groups); err != nil {
		return nil, err
	}
	for _, group := range groups.Items {
		for _, member := range group.Spec.Members {
			if member.UserRef.Name == name {
				dependencies = append(dependencies, fmt.Sprintf("groups/%s member", group.Name))
				break
			}
		}
	}

	var bindings platformv1alpha1.RoleBindingList
	if err := s.client.List(ctx, &bindings); err != nil {
		return nil, err
	}
	for _, binding := range bindings.Items {
		for _, subject := range binding.Spec.Subjects {
			if subject.Kind == platformv1alpha1.SubjectKindUser && subject.Name == name {
				dependencies = append(dependencies, fmt.Sprintf("rolebindings/%s subject", binding.Name))
				break
			}
		}
	}
	return uniqueSortedStrings(dependencies), nil
}

func (s *Server) groupDeleteDependencies(ctx context.Context, name string) ([]string, error) {
	dependencies := []string{}
	var bindings platformv1alpha1.RoleBindingList
	if err := s.client.List(ctx, &bindings); err != nil {
		return nil, err
	}
	for _, binding := range bindings.Items {
		for _, subject := range binding.Spec.Subjects {
			if subject.Kind == platformv1alpha1.SubjectKindGroup && subject.Name == name {
				dependencies = append(dependencies, fmt.Sprintf("rolebindings/%s subject", binding.Name))
				break
			}
		}
	}
	return uniqueSortedStrings(dependencies), nil
}

func (s *Server) roleDeleteDependencies(ctx context.Context, name string) ([]string, error) {
	dependencies := []string{}
	var bindings platformv1alpha1.RoleBindingList
	if err := s.client.List(ctx, &bindings); err != nil {
		return nil, err
	}
	for _, binding := range bindings.Items {
		for _, role := range binding.Spec.Roles {
			if string(role) == name {
				dependencies = append(dependencies, fmt.Sprintf("rolebindings/%s role", binding.Name))
				break
			}
		}
	}
	return uniqueSortedStrings(dependencies), nil
}

func (s *Server) handleListGroups(w http.ResponseWriter, r *http.Request) {
	if _, ok := requirePlatformRole(w, r, rolePlatformAdmin); !ok {
		return
	}
	var list platformv1alpha1.GroupList
	if err := s.client.List(r.Context(), &list); err != nil {
		writeError(w, err)
		return
	}
	response := make([]GroupSummary, 0, len(list.Items))
	for _, group := range list.Items {
		summary := GroupSummary{Name: group.Name, DisplayName: group.Spec.DisplayName}
		for _, member := range group.Spec.Members {
			summary.Members = append(summary.Members, member.UserRef.Name)
		}
		for _, external := range group.Spec.ExternalGroups {
			summary.ExternalGroups = append(summary.ExternalGroups, ExternalGroupSummary{Provider: external.ProviderRef.Name, Name: external.Name})
		}
		response = append(response, summary)
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleCreateGroup(w http.ResponseWriter, r *http.Request) {
	if _, ok := requirePlatformRole(w, r, rolePlatformAdmin); !ok {
		return
	}
	var req GroupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeInvalidAuthAdminRequest(w)
		return
	}
	if err := validateGroupRequest(req); err != nil {
		writeAuthAdminValidationError(w, err)
		return
	}
	group := groupFromRequest(req)
	if err := s.client.Create(r.Context(), group); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, WriteResponse{Name: group.Name, Message: "Group created."})
}

func (s *Server) handleUpdateGroup(w http.ResponseWriter, r *http.Request) {
	if _, ok := requirePlatformRole(w, r, rolePlatformAdmin); !ok {
		return
	}
	name := strings.TrimSpace(r.PathValue("name"))
	var req GroupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeInvalidAuthAdminRequest(w)
		return
	}
	req.Name = name
	if err := validateGroupRequest(req); err != nil {
		writeAuthAdminValidationError(w, err)
		return
	}
	group := groupFromRequest(req)
	var existing platformv1alpha1.Group
	if err := s.client.Get(r.Context(), types.NamespacedName{Name: name}, &existing); err != nil {
		writeError(w, err)
		return
	}
	existing.Spec = group.Spec
	if err := s.client.Update(r.Context(), &existing); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, WriteResponse{Name: existing.Name, Message: "Group updated."})
}

func (s *Server) handleDeleteGroup(w http.ResponseWriter, r *http.Request) {
	if _, ok := requirePlatformRole(w, r, rolePlatformAdmin); !ok {
		return
	}
	name := strings.TrimSpace(r.PathValue("name"))
	var existing platformv1alpha1.Group
	if err := s.client.Get(r.Context(), types.NamespacedName{Name: name}, &existing); err != nil {
		writeError(w, err)
		return
	}
	dependencies, err := s.groupDeleteDependencies(r.Context(), name)
	if err != nil {
		writeError(w, err)
		return
	}
	if len(dependencies) > 0 {
		writeAuthAdminDependencyConflict(w, "group", name, dependencies)
		return
	}
	if err := s.client.Delete(r.Context(), &existing); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, WriteResponse{Name: name, Message: "Group deleted."})
}

func (s *Server) handleListRoles(w http.ResponseWriter, r *http.Request) {
	if _, ok := requirePlatformRole(w, r, rolePlatformAdmin); !ok {
		return
	}
	roles, err := s.authRoles(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, roles)
}

func (s *Server) handleCreateRole(w http.ResponseWriter, r *http.Request) {
	if _, ok := requirePlatformRole(w, r, rolePlatformAdmin); !ok {
		return
	}
	var req RoleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeInvalidAuthAdminRequest(w)
		return
	}
	role, err := roleFromRequest(req)
	if err != nil {
		writeAuthAdminValidationError(w, err)
		return
	}
	if _, ok := builtInRoleMap()[role.Name]; ok {
		writeAuthAdminValidationError(w, newAuthAdminValidationError("built-in roles cannot be replaced", authAdminFieldError("name", "built-in roles cannot be replaced")))
		return
	}
	configMap, err := roleConfigMap(role)
	if err != nil {
		writeAuthAdminValidationError(w, err)
		return
	}
	if err := s.client.Create(r.Context(), configMap); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, WriteResponse{Name: role.Name, Message: "Role created."})
}

func (s *Server) handleUpdateRole(w http.ResponseWriter, r *http.Request) {
	if _, ok := requirePlatformRole(w, r, rolePlatformAdmin); !ok {
		return
	}
	name := strings.TrimSpace(r.PathValue("name"))
	if _, ok := builtInRoleMap()[name]; ok {
		writeAuthAdminValidationError(w, newAuthAdminValidationError("built-in roles are locked", authAdminFieldError("name", "built-in roles are locked")))
		return
	}
	var req RoleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeInvalidAuthAdminRequest(w)
		return
	}
	req.Name = name
	role, err := roleFromRequest(req)
	if err != nil {
		writeAuthAdminValidationError(w, err)
		return
	}
	configMap, err := roleConfigMap(role)
	if err != nil {
		writeAuthAdminValidationError(w, err)
		return
	}
	var existing corev1.ConfigMap
	if err := s.client.Get(r.Context(), types.NamespacedName{Name: configMap.Name, Namespace: configMap.Namespace}, &existing); err != nil {
		writeError(w, err)
		return
	}
	existing.Labels = configMap.Labels
	existing.Data = configMap.Data
	if err := s.client.Update(r.Context(), &existing); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, WriteResponse{Name: role.Name, Message: "Role updated."})
}

func (s *Server) handleDeleteRole(w http.ResponseWriter, r *http.Request) {
	if _, ok := requirePlatformRole(w, r, rolePlatformAdmin); !ok {
		return
	}
	name := strings.TrimSpace(r.PathValue("name"))
	if _, ok := builtInRoleMap()[name]; ok {
		writeAuthAdminValidationError(w, newAuthAdminValidationError("built-in roles are locked", authAdminFieldError("name", "built-in roles are locked")))
		return
	}
	dependencies, err := s.roleDeleteDependencies(r.Context(), name)
	if err != nil {
		writeError(w, err)
		return
	}
	if len(dependencies) > 0 {
		writeAuthAdminDependencyConflict(w, "role", name, dependencies)
		return
	}
	var existing corev1.ConfigMap
	if err := s.client.Get(r.Context(), types.NamespacedName{Name: roleDefinitionPrefix + name, Namespace: authSecretNamespace}, &existing); err != nil {
		writeError(w, err)
		return
	}
	if err := s.client.Delete(r.Context(), &existing); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, WriteResponse{Name: name, Message: "Role deleted."})
}

func groupFromRequest(req GroupRequest) *platformv1alpha1.Group {
	group := &platformv1alpha1.Group{
		ObjectMeta: metav1.ObjectMeta{Name: req.Name},
		Spec:       platformv1alpha1.GroupSpec{DisplayName: req.DisplayName},
	}
	for _, member := range req.Members {
		if member = strings.TrimSpace(member); member != "" {
			group.Spec.Members = append(group.Spec.Members, platformv1alpha1.GroupMemberSpec{UserRef: platformv1alpha1.LocalObjectReference{Name: member}})
		}
	}
	for _, external := range req.ExternalGroups {
		if strings.TrimSpace(external.Provider) == "" || strings.TrimSpace(external.Name) == "" {
			continue
		}
		group.Spec.ExternalGroups = append(group.Spec.ExternalGroups, platformv1alpha1.ExternalGroupSpec{
			ProviderRef: platformv1alpha1.LocalObjectReference{Name: external.Provider},
			Name:        external.Name,
		})
	}
	return group
}

func validateGroupRequest(req GroupRequest) error {
	fields := []authAdminFieldValidationError{}
	if strings.TrimSpace(req.Name) == "" {
		fields = append(fields, authAdminFieldError("name", "name is required"))
	}
	memberCount := 0
	for _, member := range req.Members {
		if strings.TrimSpace(member) != "" {
			memberCount++
		}
	}
	externalGroupCount := 0
	for i, external := range req.ExternalGroups {
		provider := strings.TrimSpace(external.Provider)
		name := strings.TrimSpace(external.Name)
		if provider == "" && name == "" {
			continue
		}
		fieldPrefix := fmt.Sprintf("externalGroups[%d]", i)
		if provider == "" {
			fields = append(fields, authAdminFieldError(fieldPrefix+".provider", "provider is required"))
		}
		if name == "" {
			fields = append(fields, authAdminFieldError(fieldPrefix+".name", "name is required"))
		}
		if provider != "" && name != "" {
			externalGroupCount++
		}
	}
	if memberCount == 0 && externalGroupCount == 0 {
		fields = append(fields, authAdminFieldError("members", "at least one member or external group is required"))
	}
	if len(fields) > 0 {
		return newAuthAdminValidationError("group validation failed", fields...)
	}
	return nil
}

func (s *Server) handleListRoleBindings(w http.ResponseWriter, r *http.Request) {
	if _, ok := requirePlatformRole(w, r, rolePlatformAdmin); !ok {
		return
	}
	var list platformv1alpha1.RoleBindingList
	if err := s.client.List(r.Context(), &list); err != nil {
		writeError(w, err)
		return
	}
	response := make([]RoleBindingSummary, 0, len(list.Items))
	for _, binding := range list.Items {
		summary := RoleBindingSummary{
			Name:        binding.Name,
			DisplayName: binding.Spec.DisplayName,
			Scope:       string(binding.Spec.Scope),
		}
		if binding.Spec.TenantRef != nil {
			summary.TenantName = binding.Spec.TenantRef.Name
		}
		for _, subject := range binding.Spec.Subjects {
			summary.Subjects = append(summary.Subjects, RoleBindingSubject{Kind: string(subject.Kind), Name: subject.Name})
		}
		for _, role := range binding.Spec.Roles {
			summary.Roles = append(summary.Roles, string(role))
		}
		response = append(response, summary)
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleCreateRoleBinding(w http.ResponseWriter, r *http.Request) {
	if _, ok := requirePlatformRole(w, r, rolePlatformAdmin); !ok {
		return
	}
	var req RoleBindingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeInvalidAuthAdminRequest(w)
		return
	}
	binding, err := roleBindingFromRequest(req)
	if err != nil {
		writeAuthAdminValidationError(w, err)
		return
	}
	if err := s.client.Create(r.Context(), binding); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, WriteResponse{Name: binding.Name, Message: "Role binding created."})
}

func (s *Server) handleUpdateRoleBinding(w http.ResponseWriter, r *http.Request) {
	if _, ok := requirePlatformRole(w, r, rolePlatformAdmin); !ok {
		return
	}
	name := strings.TrimSpace(r.PathValue("name"))
	var req RoleBindingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeInvalidAuthAdminRequest(w)
		return
	}
	req.Name = name
	binding, err := roleBindingFromRequest(req)
	if err != nil {
		writeAuthAdminValidationError(w, err)
		return
	}
	var existing platformv1alpha1.RoleBinding
	if err := s.client.Get(r.Context(), types.NamespacedName{Name: name}, &existing); err != nil {
		writeError(w, err)
		return
	}
	existing.Spec = binding.Spec
	if err := s.client.Update(r.Context(), &existing); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, WriteResponse{Name: existing.Name, Message: "Role binding updated."})
}

func (s *Server) handleDeleteRoleBinding(w http.ResponseWriter, r *http.Request) {
	if _, ok := requirePlatformRole(w, r, rolePlatformAdmin); !ok {
		return
	}
	name := strings.TrimSpace(r.PathValue("name"))
	var existing platformv1alpha1.RoleBinding
	if err := s.client.Get(r.Context(), types.NamespacedName{Name: name}, &existing); err != nil {
		writeError(w, err)
		return
	}
	if err := s.client.Delete(r.Context(), &existing); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, WriteResponse{Name: name, Message: "Role binding deleted."})
}

func roleBindingFromRequest(req RoleBindingRequest) (*platformv1alpha1.RoleBinding, error) {
	fields := validateRoleBindingRequest(req)
	if len(fields) > 0 {
		return nil, newAuthAdminValidationError("role binding validation failed", fields...)
	}
	binding := &platformv1alpha1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: strings.TrimSpace(req.Name)},
		Spec: platformv1alpha1.RoleBindingSpec{
			DisplayName: req.DisplayName,
			Scope:       platformv1alpha1.AccessScope(req.Scope),
		},
	}
	if req.TenantName != "" {
		binding.Spec.TenantRef = &platformv1alpha1.LocalObjectReference{Name: req.TenantName}
	}
	for _, subject := range req.Subjects {
		if strings.TrimSpace(subject.Name) == "" {
			continue
		}
		binding.Spec.Subjects = append(binding.Spec.Subjects, platformv1alpha1.SubjectReference{
			Kind: platformv1alpha1.SubjectKind(subject.Kind),
			Name: subject.Name,
		})
	}
	for _, role := range req.Roles {
		if role = strings.TrimSpace(role); role != "" {
			binding.Spec.Roles = append(binding.Spec.Roles, platformv1alpha1.ServicerRole(role))
		}
	}
	return binding, nil
}

func validateRoleBindingRequest(req RoleBindingRequest) []authAdminFieldValidationError {
	fields := []authAdminFieldValidationError{}
	if strings.TrimSpace(req.Name) == "" {
		fields = append(fields, authAdminFieldError("name", "name is required"))
	}
	scope := strings.TrimSpace(req.Scope)
	switch scope {
	case string(platformv1alpha1.AccessScopePlatform):
		if strings.TrimSpace(req.TenantName) != "" {
			fields = append(fields, authAdminFieldError("tenantName", "tenantName is only valid when scope is tenant"))
		}
	case string(platformv1alpha1.AccessScopeTenant):
		if strings.TrimSpace(req.TenantName) == "" {
			fields = append(fields, authAdminFieldError("tenantName", "tenantName is required when scope is tenant"))
		}
	default:
		fields = append(fields, authAdminFieldError("scope", "scope must be platform or tenant"))
	}
	subjectCount := 0
	for i, subject := range req.Subjects {
		kind := strings.TrimSpace(subject.Kind)
		name := strings.TrimSpace(subject.Name)
		if kind == "" && name == "" {
			continue
		}
		fieldPrefix := fmt.Sprintf("subjects[%d]", i)
		if kind != string(platformv1alpha1.SubjectKindUser) && kind != string(platformv1alpha1.SubjectKindGroup) {
			fields = append(fields, authAdminFieldError(fieldPrefix+".kind", "subject kind must be User or Group"))
		}
		if name == "" {
			fields = append(fields, authAdminFieldError(fieldPrefix+".name", "subject name is required"))
		}
		if (kind == string(platformv1alpha1.SubjectKindUser) || kind == string(platformv1alpha1.SubjectKindGroup)) && name != "" {
			subjectCount++
		}
	}
	if subjectCount == 0 {
		fields = append(fields, authAdminFieldError("subjects", "at least one subject is required"))
	}
	roleCount := 0
	for _, role := range req.Roles {
		if strings.TrimSpace(role) != "" {
			roleCount++
		}
	}
	if roleCount == 0 {
		fields = append(fields, authAdminFieldError("roles", "at least one role is required"))
	}
	return fields
}

func (s *Server) authRoles(ctx context.Context) ([]RoleSummary, error) {
	return authRolesFromClient(ctx, s.client)
}

func authRolesFromClient(ctx context.Context, c client.Client) ([]RoleSummary, error) {
	roles := builtInRoles()
	var list corev1.ConfigMapList
	if err := c.List(ctx, &list, client.InNamespace(authSecretNamespace), client.MatchingLabels{roleDefinitionLabelKey: roleDefinitionLabelValue}); err != nil {
		return nil, err
	}
	for _, configMap := range list.Items {
		role, err := roleFromConfigMap(configMap)
		if err != nil {
			continue
		}
		if _, builtIn := builtInRoleMap()[role.Name]; builtIn {
			continue
		}
		roles = append(roles, role)
	}
	sort.Slice(roles, func(i, j int) bool {
		if roles[i].BuiltIn != roles[j].BuiltIn {
			return roles[i].BuiltIn
		}
		if roles[i].Scope != roles[j].Scope {
			return roles[i].Scope < roles[j].Scope
		}
		return roles[i].Name < roles[j].Name
	})
	return roles, nil
}

func builtInRoles() []RoleSummary {
	return []RoleSummary{
		{Name: rolePlatformAdmin, DisplayName: "Platform Admin", Scope: "platform", BuiltIn: true, Description: "Full platform control, including auth administration, tenancy, catalog, clusters, repositories, products, and audit access.", Permissions: []string{rolePlatformAdmin}},
		{Name: roleCatalogAdmin, DisplayName: "Catalog Admin", Scope: "platform", BuiltIn: true, Description: "Manage service catalog definitions, product publication settings, and platform-wide catalog defaults.", Permissions: []string{roleCatalogAdmin}},
		{Name: roleClusterAdmin, DisplayName: "Cluster Admin", Scope: "platform", BuiltIn: true, Description: "Manage cluster targets, cluster metadata, deployment destinations, and platform cluster availability.", Permissions: []string{roleClusterAdmin}},
		{Name: roleAuditor, DisplayName: "Auditor", Scope: "platform", BuiltIn: true, Description: "Read audit events and compliance history across visible platform and tenant resources without write access.", Permissions: []string{roleAuditor}},
		{Name: roleTenantAdmin, DisplayName: "Tenant Admin", Scope: "tenant", BuiltIn: true, Description: "Manage assigned tenant access, tenant repositories, tenant products, and tenant operational settings.", Permissions: []string{roleTenantAdmin}},
		{Name: roleTenantOperator, DisplayName: "Tenant Operator", Scope: "tenant", BuiltIn: true, Description: "Operate assigned tenant products and repositories, including product lifecycle actions within that tenant.", Permissions: []string{roleTenantOperator}},
		{Name: roleServiceConsumer, DisplayName: "Service Consumer", Scope: "tenant", BuiltIn: true, Description: "View assigned tenant catalog entries and request or inspect products available to that tenant.", Permissions: []string{roleServiceConsumer}},
	}
}

func builtInRoleMap() map[string]RoleSummary {
	roles := map[string]RoleSummary{}
	for _, role := range builtInRoles() {
		roles[role.Name] = role
	}
	return roles
}

func roleFromRequest(req RoleRequest) (RoleSummary, error) {
	role := RoleSummary{
		Name:        strings.TrimSpace(req.Name),
		DisplayName: strings.TrimSpace(req.DisplayName),
		Description: strings.TrimSpace(req.Description),
		Scope:       strings.TrimSpace(req.Scope),
		Permissions: uniquePermissionNames(req.Permissions),
	}
	fields := []authAdminFieldValidationError{}
	if role.Name == "" {
		fields = append(fields, authAdminFieldError("name", "name is required"))
	}
	if role.Scope != string(platformv1alpha1.AccessScopePlatform) && role.Scope != string(platformv1alpha1.AccessScopeTenant) {
		fields = append(fields, authAdminFieldError("scope", "scope must be platform or tenant"))
	}
	if len(role.Permissions) == 0 {
		fields = append(fields, authAdminFieldError("permissions", "at least one permission is required"))
	}
	if role.Scope == string(platformv1alpha1.AccessScopePlatform) || role.Scope == string(platformv1alpha1.AccessScopeTenant) {
		allowed := allowedRolePermissions(role.Scope)
		for _, permission := range role.Permissions {
			if _, ok := allowed[permission]; !ok {
				fields = append(fields, authAdminFieldError("permissions", fmt.Sprintf("permission %q is not valid for %s roles", permission, role.Scope)))
			}
		}
	}
	if len(fields) > 0 {
		return role, newAuthAdminValidationError("role validation failed", fields...)
	}
	return role, nil
}

func allowedRolePermissions(scope string) map[string]struct{} {
	allowed := map[string]struct{}{}
	for _, role := range builtInRoles() {
		if role.Scope == scope {
			allowed[role.Name] = struct{}{}
		}
	}
	return allowed
}

func uniquePermissionNames(values []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func uniqueSortedStrings(values []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func roleConfigMap(role RoleSummary) (*corev1.ConfigMap, error) {
	payload, err := json.Marshal(role)
	if err != nil {
		return nil, err
	}
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      roleDefinitionPrefix + role.Name,
			Namespace: authSecretNamespace,
			Labels: map[string]string{
				roleDefinitionLabelKey: roleDefinitionLabelValue,
			},
		},
		Data: map[string]string{roleDefinitionDataKey: string(payload)},
	}, nil
}

func roleFromConfigMap(configMap corev1.ConfigMap) (RoleSummary, error) {
	var role RoleSummary
	if err := json.Unmarshal([]byte(configMap.Data[roleDefinitionDataKey]), &role); err != nil {
		return role, err
	}
	role.BuiltIn = false
	return role, nil
}

func (s *Server) upsertSecret(ctx context.Context, desired *corev1.Secret) error {
	desired = desired.DeepCopy()
	if len(desired.StringData) > 0 {
		if desired.Data == nil {
			desired.Data = map[string][]byte{}
		}
		for key, value := range desired.StringData {
			desired.Data[key] = []byte(value)
		}
	}

	var existing corev1.Secret
	key := types.NamespacedName{Name: desired.Name, Namespace: desired.Namespace}
	if err := s.client.Get(ctx, key, &existing); err != nil {
		if apierrors.IsNotFound(err) {
			return s.client.Create(ctx, desired)
		}
		return err
	}
	existing.StringData = desired.StringData
	existing.Data = desired.Data
	existing.Type = desired.Type
	existing.Immutable = desired.Immutable
	existing.Labels = desired.Labels
	existing.Annotations = desired.Annotations
	return s.client.Update(ctx, &existing)
}
