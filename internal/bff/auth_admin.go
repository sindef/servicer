package bff

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	platformv1alpha1 "github.com/sindef/servicer/api/v1alpha1"
	"golang.org/x/crypto/bcrypt"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const authSecretNamespace = "servicer-system"

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
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	provider, secrets, err := authProviderFromRequest(req)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	for _, secret := range secrets {
		if err := s.client.Patch(r.Context(), secret, client.Apply, client.FieldOwner("servicer-auth-admin"), client.ForceOwnership); err != nil {
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
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	req.Name = name
	provider, secrets, err := authProviderFromRequest(req)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	for _, secret := range secrets {
		if err := s.client.Patch(r.Context(), secret, client.Apply, client.FieldOwner("servicer-auth-admin"), client.ForceOwnership); err != nil {
			writeError(w, err)
			return
		}
	}
	var existing platformv1alpha1.AuthProvider
	if err := s.client.Get(r.Context(), types.NamespacedName{Name: name}, &existing); err != nil {
		writeError(w, err)
		return
	}
	if existing.Spec.OIDC != nil && provider.Spec.OIDC != nil && provider.Spec.OIDC.ClientSecretRef.Name == "" {
		provider.Spec.OIDC.ClientSecretRef = existing.Spec.OIDC.ClientSecretRef
	}
	if existing.Spec.LDAP != nil && provider.Spec.LDAP != nil && provider.Spec.LDAP.BindSecretRef.Name == "" {
		provider.Spec.LDAP.BindSecretRef = existing.Spec.LDAP.BindSecretRef
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
	if err := s.client.Delete(r.Context(), &existing); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, WriteResponse{Name: name, Message: "Auth provider deleted."})
}

func authProviderFromRequest(req AuthProviderRequest) (*platformv1alpha1.AuthProvider, []*corev1.Secret, error) {
	providerType := platformv1alpha1.AuthProviderType(strings.TrimSpace(req.Type))
	if strings.TrimSpace(req.Name) == "" || strings.TrimSpace(req.DisplayName) == "" {
		return nil, nil, fmt.Errorf("name and displayName are required")
	}
	provider := &platformv1alpha1.AuthProvider{
		ObjectMeta: metav1.ObjectMeta{Name: req.Name},
		Spec: platformv1alpha1.AuthProviderSpec{
			DisplayName: req.DisplayName,
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
			secretName := fmt.Sprintf("auth-provider-%s-oidc", req.Name)
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
			secretName := fmt.Sprintf("auth-provider-%s-ldap", req.Name)
			secrets = append(secrets, &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: authSecretNamespace},
				StringData: map[string]string{
					"username": req.LDAPBindUsername,
					"password": req.LDAPBindPassword,
				},
			})
			provider.Spec.LDAP.BindSecretRef = platformv1alpha1.NamespacedObjectReference{Name: secretName, Namespace: authSecretNamespace}
		}
	default:
		return nil, nil, fmt.Errorf("unsupported provider type %q", req.Type)
	}
	return provider, secrets, nil
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
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if req.LocalAuthEnabled && strings.TrimSpace(req.Password) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "password is required when local auth is enabled"})
		return
	}
	user, secret, err := userFromRequest(req)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if secret != nil {
		if err := s.client.Patch(r.Context(), secret, client.Apply, client.FieldOwner("servicer-auth-admin"), client.ForceOwnership); err != nil {
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
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	req.Name = name
	user, secret, err := userFromRequest(req)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if secret != nil {
		if err := s.client.Patch(r.Context(), secret, client.Apply, client.FieldOwner("servicer-auth-admin"), client.ForceOwnership); err != nil {
			writeError(w, err)
			return
		}
	}
	var existing platformv1alpha1.User
	if err := s.client.Get(r.Context(), types.NamespacedName{Name: name}, &existing); err != nil {
		writeError(w, err)
		return
	}
	if user.Spec.LocalAuth != nil && secret == nil {
		user.Spec.LocalAuth.PasswordHashSecretRef = existing.Spec.LocalAuth.PasswordHashSecretRef
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
	if err := s.client.Delete(r.Context(), &existing); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, WriteResponse{Name: name, Message: "User deleted."})
}

func userFromRequest(req UserRequest) (*platformv1alpha1.User, *corev1.Secret, error) {
	if strings.TrimSpace(req.Name) == "" {
		return nil, nil, fmt.Errorf("name is required")
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
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
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
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	req.Name = name
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
	if err := s.client.Delete(r.Context(), &existing); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, WriteResponse{Name: name, Message: "Group deleted."})
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
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	binding, err := roleBindingFromRequest(req)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
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
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	req.Name = name
	binding, err := roleBindingFromRequest(req)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
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
	if strings.TrimSpace(req.Name) == "" {
		return nil, fmt.Errorf("name is required")
	}
	binding := &platformv1alpha1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: req.Name},
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
