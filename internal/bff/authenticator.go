package bff

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/go-ldap/ldap/v3"
	platformv1alpha1 "github.com/sindef/servicer/api/v1alpha1"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/oauth2"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	authSessionCookieName = "servicer_session"
	authFlowCookieName    = "servicer_auth_flow"
	csrfCookieName        = "servicer_csrf"
	defaultOIDCRedirect   = "/api/auth/callback"
	defaultSessionSecret  = "servicer-local-session-secret-change-me"
)

type authResult struct {
	Actor        actor
	Session      *authSessionState
	ClearSession bool
}

type authSessionState struct {
	ProviderName string   `json:"providerName"`
	ProviderType string   `json:"providerType"`
	Subject      string   `json:"subject"`
	Name         string   `json:"name,omitempty"`
	Email        string   `json:"email,omitempty"`
	Groups       []string `json:"groups,omitempty"`
	IDToken      string   `json:"idToken,omitempty"`
	RefreshToken string   `json:"refreshToken,omitempty"`
	ExpiryUnix   int64    `json:"expiryUnix,omitempty"`
}

type oidcAuthFlowState struct {
	ProviderName string `json:"providerName"`
	State        string `json:"state"`
	Verifier     string `json:"verifier"`
	ReturnTo     string `json:"returnTo"`
}

type authRuntime struct {
	client           client.Client
	sessionCodec     *sealedCookieCodec
	flowCodec        *sealedCookieCodec
	bootstrap        bootstrapAdminConfig
	allowTestHeaders bool

	mu        sync.RWMutex
	oidcCache map[string]cachedOIDCProvider
}

type bootstrapAdminConfig struct {
	Username string
	Password string
	Email    string
}

type cachedOIDCProvider struct {
	resourceVersion string
	runtime         *oidcProviderRuntime
}

type oidcProviderRuntime struct {
	provider      *platformv1alpha1.AuthProvider
	verifier      *oidc.IDTokenVerifier
	oauth2Config  oauth2.Config
	usernameClaim string
	emailClaim    string
	rolesClaim    string
	groupsClaim   string
	endSessionURL string
}

type resolvedIdentity struct {
	ProviderName string
	ProviderType string
	Subject      string
	Name         string
	Email        string
	Groups       []string
}

type loginRequest struct {
	Provider string `json:"provider,omitempty"`
	Username string `json:"username"`
	Password string `json:"password"`
}

type sealedCookieCodec struct {
	encryptionKey []byte
	signingKey    []byte
}

func newAuthRuntime(client client.Client) (*authRuntime, error) {
	sessionSecret := strings.TrimSpace(os.Getenv("SERVICER_SESSION_SECRET"))
	if sessionSecret == "" {
		if productionMode() {
			return nil, errors.New("SERVICER_SESSION_SECRET is required when SERVICER_PRODUCTION=true")
		}
		sessionSecret = defaultSessionSecret
	}
	if productionMode() && (sessionSecret == defaultSessionSecret || len(sessionSecret) < 32) {
		return nil, errors.New("SERVICER_SESSION_SECRET must be at least 32 characters and must not use the local default")
	}
	runtime := &authRuntime{
		client:       client,
		sessionCodec: newSealedCookieCodec(sessionSecret + "|session"),
		flowCodec:    newSealedCookieCodec(sessionSecret + "|flow"),
		bootstrap: bootstrapAdminConfig{
			Username: strings.TrimSpace(os.Getenv("SERVICER_BOOTSTRAP_ADMIN_USERNAME")),
			Password: strings.TrimSpace(os.Getenv("SERVICER_BOOTSTRAP_ADMIN_PASSWORD")),
			Email:    strings.TrimSpace(os.Getenv("SERVICER_BOOTSTRAP_ADMIN_EMAIL")),
		},
		oidcCache: map[string]cachedOIDCProvider{},
	}
	if err := runtime.ensureBootstrapAdmin(context.Background()); err != nil {
		return nil, err
	}
	return runtime, nil
}

func (a *authRuntime) Mode() string {
	return "multi"
}

func (a *authRuntime) Authenticate(ctx context.Context, r *http.Request) (authResult, error) {
	if a.allowTestHeaders {
		return authResult{Actor: actorFromHeaders(r)}, nil
	}
	if token := authorizationBearerToken(r.Header.Get("Authorization")); token != "" {
		session, actor, err := a.authenticateBearerToken(ctx, token)
		if err != nil {
			return authResult{}, err
		}
		return authResult{Actor: actor, Session: &session}, nil
	}
	sessionCookie, err := r.Cookie(authSessionCookieName)
	if err != nil || strings.TrimSpace(sessionCookie.Value) == "" {
		return authResult{ClearSession: true}, errors.New("no authenticated session")
	}
	var session authSessionState
	if err := a.sessionCodec.Decode(sessionCookie.Value, &session); err != nil {
		return authResult{ClearSession: true}, err
	}
	refreshed, currentActor, err := a.actorFromSession(ctx, session)
	if err != nil {
		return authResult{ClearSession: true}, err
	}
	return authResult{Actor: currentActor, Session: &refreshed}, nil
}

func (a *authRuntime) Config(ctx context.Context) (AuthConfigResponse, error) {
	providers, err := a.listEnabledProviders(ctx)
	if err != nil {
		return AuthConfigResponse{}, err
	}
	response := AuthConfigResponse{
		Mode:         a.Mode(),
		LoginPath:    "/api/auth/login",
		LogoutPath:   "/api/auth/logout",
		CallbackPath: defaultOIDCRedirect,
		Providers:    make([]AuthProviderLoginView, 0, len(providers)),
	}
	for _, provider := range providers {
		view := AuthProviderLoginView{
			Name:        provider.Name,
			DisplayName: provider.Spec.DisplayName,
			Type:        string(provider.Spec.Type),
			Default:     provider.Spec.Default,
		}
		if response.DefaultProvider == "" || view.Default {
			response.DefaultProvider = view.Name
		}
		response.Providers = append(response.Providers, view)
	}
	sort.Slice(response.Providers, func(i, j int) bool {
		if response.Providers[i].Default != response.Providers[j].Default {
			return response.Providers[i].Default
		}
		return response.Providers[i].DisplayName < response.Providers[j].DisplayName
	})
	return response, nil
}

func (a *authRuntime) SessionFromRequest(ctx context.Context, r *http.Request) (AuthSessionResponse, *authSessionState, error) {
	result, err := a.Authenticate(ctx, r)
	if err != nil {
		return AuthSessionResponse{Mode: a.Mode(), Authenticated: false}, nil, nil
	}
	return authSessionResponse(result.Actor), result.Session, nil
}

func authSessionResponse(current actor) AuthSessionResponse {
	response := AuthSessionResponse{
		Mode:          "multi",
		Name:          current.Name,
		Email:         current.Email,
		UserName:      current.UserName,
		Provider:      current.Provider,
		Roles:         nonNilStringSlice(sortedKeys(current.Roles)),
		Groups:        nonNilStringSlice(sortedKeys(current.Groups)),
		Authenticated: current.Authenticated,
	}
	tenantNames := make([]string, 0, len(current.TenantRoles))
	for tenantName := range current.TenantRoles {
		tenantNames = append(tenantNames, tenantName)
	}
	sort.Strings(tenantNames)
	for _, tenantName := range tenantNames {
		response.Tenants = append(response.Tenants, TenantRoleSummary{
			TenantName: tenantName,
			Roles:      sortedKeys(current.TenantRoles[tenantName]),
		})
	}
	return response
}

func nonNilStringSlice(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

func (a *authRuntime) StartLogin(ctx context.Context, w http.ResponseWriter, r *http.Request, providerName, redirectURI string) error {
	provider, err := a.getProvider(ctx, providerName)
	if err != nil {
		return err
	}
	if provider.Spec.Type != platformv1alpha1.AuthProviderTypeOIDC {
		return fmt.Errorf("provider %q does not support browser redirect login", providerName)
	}
	oidcProvider, err := a.oidcProvider(ctx, provider)
	if err != nil {
		return err
	}
	state, err := randomString(24)
	if err != nil {
		return err
	}
	verifier, err := randomString(48)
	if err != nil {
		return err
	}
	challenge, err := pkceChallenge(verifier)
	if err != nil {
		return err
	}
	returnTo := strings.TrimSpace(r.URL.Query().Get("returnTo"))
	if returnTo == "" || !strings.HasPrefix(returnTo, "/") {
		returnTo = "/"
	}
	encodedState, err := a.flowCodec.Encode(oidcAuthFlowState{
		ProviderName: provider.Name,
		State:        state,
		Verifier:     verifier,
		ReturnTo:     returnTo,
	})
	if err != nil {
		return err
	}
	http.SetCookie(w, authCookie(r, authFlowCookieName, encodedState, 10*time.Minute))
	authCodeURL := oidcProvider.oauth2Config.AuthCodeURL(
		state,
		oauth2.AccessTypeOffline,
		oauth2.SetAuthURLParam("code_challenge", challenge),
		oauth2.SetAuthURLParam("code_challenge_method", "S256"),
		oauth2.SetAuthURLParam("redirect_uri", redirectURI),
	)
	http.Redirect(w, r, authCodeURL, http.StatusFound)
	return nil
}

func (a *authRuntime) HandleCallback(ctx context.Context, w http.ResponseWriter, r *http.Request, redirectURI string) error {
	flowCookie, err := r.Cookie(authFlowCookieName)
	if err != nil {
		return errors.New("login flow cookie is missing")
	}
	var flow oidcAuthFlowState
	if err := a.flowCodec.Decode(flowCookie.Value, &flow); err != nil {
		return errors.New("login flow cookie is invalid")
	}
	if strings.TrimSpace(r.URL.Query().Get("state")) != flow.State {
		return errors.New("login flow state did not match")
	}
	code := strings.TrimSpace(r.URL.Query().Get("code"))
	if code == "" {
		return errors.New("authorization code is missing")
	}
	provider, err := a.getProvider(ctx, flow.ProviderName)
	if err != nil {
		return err
	}
	oidcProvider, err := a.oidcProvider(ctx, provider)
	if err != nil {
		return err
	}
	token, err := oidcProvider.oauth2Config.Exchange(
		ctx,
		code,
		oauth2.SetAuthURLParam("code_verifier", flow.Verifier),
		oauth2.SetAuthURLParam("redirect_uri", redirectURI),
	)
	if err != nil {
		return fmt.Errorf("exchange authorization code: %w", err)
	}
	session, _, err := a.sessionFromOAuthToken(ctx, oidcProvider, token)
	if err != nil {
		return err
	}
	encodedSession, err := a.sessionCodec.Encode(session)
	if err != nil {
		return err
	}
	http.SetCookie(w, clearAuthCookie(r, authFlowCookieName))
	http.SetCookie(w, authCookie(r, authSessionCookieName, encodedSession, 24*time.Hour))
	http.Redirect(w, r, flow.ReturnTo, http.StatusFound)
	return nil
}

func (a *authRuntime) CompletePasswordLogin(ctx context.Context, w http.ResponseWriter, r *http.Request, req loginRequest) error {
	providerName := strings.TrimSpace(req.Provider)
	if providerName == "" {
		providerName = a.defaultProviderName(ctx, []platformv1alpha1.AuthProviderType{platformv1alpha1.AuthProviderTypeLocal, platformv1alpha1.AuthProviderTypeLDAP})
	}
	provider, err := a.getProvider(ctx, providerName)
	if err != nil {
		return err
	}
	if provider.Spec.Type != platformv1alpha1.AuthProviderTypeLocal && provider.Spec.Type != platformv1alpha1.AuthProviderTypeLDAP {
		return fmt.Errorf("provider %q does not support password login", providerName)
	}
	var session authSessionState
	switch provider.Spec.Type {
	case platformv1alpha1.AuthProviderTypeLocal:
		session, err = a.localPasswordLogin(ctx, provider, req.Username, req.Password)
	case platformv1alpha1.AuthProviderTypeLDAP:
		session, err = a.ldapPasswordLogin(ctx, provider, req.Username, req.Password)
	}
	if err != nil {
		return err
	}
	encodedSession, err := a.sessionCodec.Encode(session)
	if err != nil {
		return err
	}
	http.SetCookie(w, authCookie(r, authSessionCookieName, encodedSession, 24*time.Hour))
	return nil
}

func (a *authRuntime) LogoutRedirectURL(ctx context.Context, r *http.Request) string {
	target := strings.TrimSpace(r.URL.Query().Get("returnTo"))
	if target == "" || !strings.HasPrefix(target, "/") {
		target = "/"
	}
	sessionCookie, err := r.Cookie(authSessionCookieName)
	if err != nil || sessionCookie.Value == "" {
		return target
	}
	var session authSessionState
	if err := a.sessionCodec.Decode(sessionCookie.Value, &session); err != nil {
		return target
	}
	if session.ProviderType != string(platformv1alpha1.AuthProviderTypeOIDC) {
		return target
	}
	provider, err := a.getProvider(ctx, session.ProviderName)
	if err != nil {
		return target
	}
	oidcProvider, err := a.oidcProvider(ctx, provider)
	if err != nil || oidcProvider.endSessionURL == "" {
		return target
	}
	redirectURL, err := url.Parse(oidcProvider.endSessionURL)
	if err != nil {
		return target
	}
	query := redirectURL.Query()
	query.Set("post_logout_redirect_uri", absoluteRedirectURL(r, target))
	redirectURL.RawQuery = query.Encode()
	return redirectURL.String()
}

func (a *authRuntime) localPasswordLogin(ctx context.Context, provider *platformv1alpha1.AuthProvider, username, password string) (authSessionState, error) {
	username = strings.TrimSpace(username)
	if username == "" || password == "" {
		return authSessionState{}, errors.New("username and password are required")
	}
	var user platformv1alpha1.User
	if err := a.client.Get(ctx, types.NamespacedName{Name: username}, &user); err != nil {
		return authSessionState{}, errors.New("invalid username or password")
	}
	if user.Spec.LocalAuth == nil || !user.Spec.LocalAuth.Enabled {
		return authSessionState{}, errors.New("local login is not enabled for this user")
	}
	hash, err := a.readSecretValue(ctx, user.Spec.LocalAuth.PasswordHashSecretRef, "passwordHash")
	if err != nil {
		return authSessionState{}, errors.New("invalid username or password")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)); err != nil {
		return authSessionState{}, errors.New("invalid username or password")
	}
	return authSessionState{
		ProviderName: provider.Name,
		ProviderType: string(provider.Spec.Type),
		Subject:      user.Name,
		Name:         firstNonEmpty(strings.TrimSpace(user.Spec.DisplayName), user.Name),
		Email:        strings.TrimSpace(user.Spec.Email),
	}, nil
}

func (a *authRuntime) ldapPasswordLogin(ctx context.Context, provider *platformv1alpha1.AuthProvider, username, password string) (authSessionState, error) {
	if provider.Spec.LDAP == nil {
		return authSessionState{}, errors.New("LDAP provider is not configured")
	}
	identity, err := a.authenticateLDAP(ctx, provider, username, password)
	if err != nil {
		return authSessionState{}, err
	}
	return authSessionState{
		ProviderName: provider.Name,
		ProviderType: string(provider.Spec.Type),
		Subject:      identity.Subject,
		Name:         identity.Name,
		Email:        identity.Email,
		Groups:       append([]string(nil), identity.Groups...),
	}, nil
}

func (a *authRuntime) authenticateLDAP(ctx context.Context, provider *platformv1alpha1.AuthProvider, username, password string) (resolvedIdentity, error) {
	if provider.Spec.LDAP == nil {
		return resolvedIdentity{}, errors.New("LDAP provider is not configured")
	}
	cfg := provider.Spec.LDAP
	username = strings.TrimSpace(username)
	if username == "" || password == "" {
		return resolvedIdentity{}, errors.New("username and password are required")
	}
	conn, err := ldap.DialURL(cfg.URL, ldap.DialWithTLSConfig(&tls.Config{InsecureSkipVerify: cfg.InsecureSkipVerify})) //nolint:gosec
	if err != nil {
		return resolvedIdentity{}, fmt.Errorf("connect to LDAP: %w", err)
	}
	defer conn.Close()
	if strings.HasPrefix(strings.ToLower(cfg.URL), "ldap://") && cfg.StartTLS {
		if err := conn.StartTLS(&tls.Config{InsecureSkipVerify: cfg.InsecureSkipVerify}); err != nil { //nolint:gosec
			return resolvedIdentity{}, fmt.Errorf("start TLS: %w", err)
		}
	}
	bindDN, bindPassword, err := a.readSecretUsernamePassword(ctx, cfg.BindSecretRef)
	if err != nil {
		return resolvedIdentity{}, fmt.Errorf("read LDAP bind secret: %w", err)
	}
	if bindDN != "" || bindPassword != "" {
		if err := conn.Bind(bindDN, bindPassword); err != nil {
			return resolvedIdentity{}, fmt.Errorf("bind LDAP search account: %w", err)
		}
	}
	userFilter := strings.ReplaceAll(cfg.UserFilter, "%s", ldap.EscapeFilter(username))
	req := ldap.NewSearchRequest(cfg.UserBaseDN, ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 2, 10, false, userFilter, []string{cfg.UsernameAttribute, cfg.EmailAttribute}, nil)
	resp, err := conn.Search(req)
	if err != nil {
		return resolvedIdentity{}, fmt.Errorf("search LDAP user: %w", err)
	}
	if len(resp.Entries) != 1 {
		return resolvedIdentity{}, errors.New("invalid username or password")
	}
	entry := resp.Entries[0]
	userDN := entry.DN
	if err := conn.Bind(userDN, password); err != nil {
		return resolvedIdentity{}, errors.New("invalid username or password")
	}
	if bindDN != "" || bindPassword != "" {
		if err := conn.Bind(bindDN, bindPassword); err != nil {
			return resolvedIdentity{}, fmt.Errorf("rebind LDAP search account: %w", err)
		}
	}
	groupNames := []string{}
	if strings.TrimSpace(cfg.GroupBaseDN) != "" && strings.TrimSpace(cfg.GroupFilter) != "" {
		groupFilter := strings.ReplaceAll(cfg.GroupFilter, "%s", ldap.EscapeFilter(userDN))
		groupFilter = strings.ReplaceAll(groupFilter, "%u", ldap.EscapeFilter(username))
		groupReq := ldap.NewSearchRequest(cfg.GroupBaseDN, ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 0, 10, false, groupFilter, []string{cfg.GroupNameAttribute}, nil)
		groupResp, err := conn.Search(groupReq)
		if err != nil {
			return resolvedIdentity{}, fmt.Errorf("search LDAP groups: %w", err)
		}
		for _, group := range groupResp.Entries {
			groupName := strings.TrimSpace(group.GetAttributeValue(cfg.GroupNameAttribute))
			if groupName != "" {
				groupNames = append(groupNames, groupName)
			}
		}
	}
	subject := strings.TrimSpace(entry.GetAttributeValue(cfg.UsernameAttribute))
	if subject == "" {
		subject = userDN
	}
	return resolvedIdentity{
		ProviderName: provider.Name,
		ProviderType: string(provider.Spec.Type),
		Subject:      subject,
		Name:         firstNonEmpty(strings.TrimSpace(entry.GetAttributeValue(cfg.UsernameAttribute)), username),
		Email:        strings.TrimSpace(entry.GetAttributeValue(cfg.EmailAttribute)),
		Groups:       groupNames,
	}, nil
}

func (a *authRuntime) authenticateBearerToken(ctx context.Context, token string) (authSessionState, actor, error) {
	providers, err := a.listEnabledProviders(ctx)
	if err != nil {
		return authSessionState{}, actor{}, err
	}
	var lastErr error
	for _, provider := range providers {
		if provider.Spec.Type != platformv1alpha1.AuthProviderTypeOIDC {
			continue
		}
		oidcProvider, err := a.oidcProvider(ctx, &provider)
		if err != nil {
			lastErr = err
			continue
		}
		session, currentActor, err := a.sessionAndActorFromIDToken(ctx, oidcProvider, token, "")
		if err == nil {
			return session, currentActor, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = errors.New("no enabled OIDC provider accepted the bearer token")
	}
	return authSessionState{}, actor{}, lastErr
}

func (a *authRuntime) actorFromSession(ctx context.Context, current authSessionState) (authSessionState, actor, error) {
	switch current.ProviderType {
	case string(platformv1alpha1.AuthProviderTypeOIDC):
		provider, err := a.getProvider(ctx, current.ProviderName)
		if err != nil {
			return authSessionState{}, actor{}, err
		}
		oidcProvider, err := a.oidcProvider(ctx, provider)
		if err != nil {
			return authSessionState{}, actor{}, err
		}
		session, currentActor, err := a.sessionAndActorFromIDToken(ctx, oidcProvider, current.IDToken, current.RefreshToken)
		if err == nil {
			return session, currentActor, nil
		}
		if current.RefreshToken != "" {
			refreshed, refreshedActor, refreshErr := a.refreshOIDCSession(ctx, oidcProvider, current)
			if refreshErr == nil {
				return refreshed, refreshedActor, nil
			}
		}
		return authSessionState{}, actor{}, err
	default:
		currentActor, err := a.resolveActor(ctx, resolvedIdentity{
			ProviderName: current.ProviderName,
			ProviderType: current.ProviderType,
			Subject:      current.Subject,
			Name:         current.Name,
			Email:        current.Email,
			Groups:       append([]string(nil), current.Groups...),
		})
		if err != nil {
			return authSessionState{}, actor{}, err
		}
		return current, currentActor, nil
	}
}

func (a *authRuntime) refreshOIDCSession(ctx context.Context, provider *oidcProviderRuntime, current authSessionState) (authSessionState, actor, error) {
	tokenSource := provider.oauth2Config.TokenSource(ctx, &oauth2.Token{RefreshToken: current.RefreshToken})
	token, err := tokenSource.Token()
	if err != nil {
		return authSessionState{}, actor{}, fmt.Errorf("refresh OIDC session: %w", err)
	}
	return a.sessionFromOAuthToken(ctx, provider, token)
}

func (a *authRuntime) sessionFromOAuthToken(ctx context.Context, provider *oidcProviderRuntime, token *oauth2.Token) (authSessionState, actor, error) {
	if token == nil {
		return authSessionState{}, actor{}, errors.New("OIDC token exchange returned no token")
	}
	idToken, _ := token.Extra("id_token").(string)
	if strings.TrimSpace(idToken) == "" {
		return authSessionState{}, actor{}, errors.New("OIDC token exchange did not return an id_token")
	}
	return a.sessionAndActorFromIDToken(ctx, provider, idToken, strings.TrimSpace(token.RefreshToken))
}

func (a *authRuntime) sessionAndActorFromIDToken(ctx context.Context, provider *oidcProviderRuntime, idTokenValue, refreshToken string) (authSessionState, actor, error) {
	idToken, err := provider.verifier.Verify(ctx, idTokenValue)
	if err != nil {
		return authSessionState{}, actor{}, err
	}
	claims := map[string]any{}
	if err := idToken.Claims(&claims); err != nil {
		return authSessionState{}, actor{}, fmt.Errorf("decode token claims: %w", err)
	}
	subject := firstNonEmpty(stringClaim(claims, provider.usernameClaim), stringClaim(claims, "preferred_username"), stringClaim(claims, provider.emailClaim), stringClaim(claims, "email"), idToken.Subject)
	if subject == "" {
		return authSessionState{}, actor{}, errors.New("token did not contain a usable subject claim")
	}
	identity := resolvedIdentity{
		ProviderName: provider.provider.Name,
		ProviderType: string(provider.provider.Spec.Type),
		Subject:      subject,
		Name:         firstNonEmpty(stringClaim(claims, provider.usernameClaim), stringClaim(claims, "preferred_username"), stringClaim(claims, provider.emailClaim), subject),
		Email:        firstNonEmpty(stringClaim(claims, provider.emailClaim), stringClaim(claims, "email")),
		Groups:       sortedKeys(setFromClaim(claims[provider.groupsClaim])),
	}
	currentActor, err := a.resolveActor(ctx, identity)
	if err != nil {
		return authSessionState{}, actor{}, err
	}
	return authSessionState{
		ProviderName: provider.provider.Name,
		ProviderType: string(provider.provider.Spec.Type),
		Subject:      subject,
		Name:         identity.Name,
		Email:        identity.Email,
		Groups:       append([]string(nil), identity.Groups...),
		IDToken:      idTokenValue,
		RefreshToken: refreshToken,
		ExpiryUnix:   idToken.Expiry.Unix(),
	}, currentActor, nil
}

func (a *authRuntime) resolveActor(ctx context.Context, identity resolvedIdentity) (actor, error) {
	users := &platformv1alpha1.UserList{}
	if err := a.client.List(ctx, users); err != nil {
		return actor{}, err
	}
	groups := &platformv1alpha1.GroupList{}
	if err := a.client.List(ctx, groups); err != nil {
		return actor{}, err
	}
	bindings := &platformv1alpha1.RoleBindingList{}
	if err := a.client.List(ctx, bindings); err != nil {
		return actor{}, err
	}
	tenants := &platformv1alpha1.TenantList{}
	if err := a.client.List(ctx, tenants); err != nil {
		return actor{}, err
	}
	roles, err := authRolesFromClient(ctx, a.client)
	if err != nil {
		return actor{}, err
	}
	roleExpander := newRoleExpander(roles)

	matchedUser := a.matchUser(users.Items, identity)
	localGroupNames := map[string]struct{}{}
	externalGroups := map[string]struct{}{}
	for _, name := range identity.Groups {
		if name = strings.TrimSpace(name); name != "" {
			externalGroups[name] = struct{}{}
		}
	}
	for _, group := range groups.Items {
		if matchedUser != nil {
			for _, member := range group.Spec.Members {
				if member.UserRef.Name == matchedUser.Name {
					localGroupNames[group.Name] = struct{}{}
					break
				}
			}
		}
		for _, external := range group.Spec.ExternalGroups {
			if external.ProviderRef.Name != identity.ProviderName {
				continue
			}
			if _, ok := externalGroups[external.Name]; ok {
				localGroupNames[group.Name] = struct{}{}
				break
			}
		}
	}

	current := actor{
		Name:          firstNonEmpty(identity.Name, identity.Subject),
		Email:         identity.Email,
		UserName:      firstNonEmpty(identity.Subject, identity.Name),
		Provider:      identity.ProviderName,
		Subject:       identity.Subject,
		Authenticated: true,
		Roles:         map[string]struct{}{},
		TenantRoles:   map[string]map[string]struct{}{},
		Groups:        map[string]struct{}{},
	}
	if matchedUser != nil {
		current.UserName = matchedUser.Name
		current.Name = firstNonEmpty(strings.TrimSpace(matchedUser.Spec.DisplayName), current.Name, matchedUser.Name)
		current.Email = firstNonEmpty(strings.TrimSpace(matchedUser.Spec.Email), current.Email)
	}
	for groupName := range localGroupNames {
		current.Groups[groupName] = struct{}{}
	}
	for groupName := range externalGroups {
		current.Groups[groupName] = struct{}{}
	}

	for _, binding := range bindings.Items {
		if !bindingAppliesToActor(binding, current, matchedUser, localGroupNames) {
			continue
		}
		switch binding.Spec.Scope {
		case platformv1alpha1.AccessScopePlatform:
			for _, role := range binding.Spec.Roles {
				for _, expanded := range roleExpander.expand(string(role), string(platformv1alpha1.AccessScopePlatform)) {
					current.Roles[expanded] = struct{}{}
				}
			}
		case platformv1alpha1.AccessScopeTenant:
			if binding.Spec.TenantRef == nil || binding.Spec.TenantRef.Name == "" {
				continue
			}
			tenantRoles := current.TenantRoles[binding.Spec.TenantRef.Name]
			if tenantRoles == nil {
				tenantRoles = map[string]struct{}{}
				current.TenantRoles[binding.Spec.TenantRef.Name] = tenantRoles
			}
			for _, role := range binding.Spec.Roles {
				for _, expanded := range roleExpander.expand(string(role), string(platformv1alpha1.AccessScopeTenant)) {
					tenantRoles[expanded] = struct{}{}
				}
			}
		}
	}
	for _, tenant := range tenants.Items {
		if tenantVisibleLegacy(current, matchedUser, tenant) {
			tenantRoles := current.TenantRoles[tenant.Name]
			if tenantRoles == nil {
				tenantRoles = map[string]struct{}{}
				current.TenantRoles[tenant.Name] = tenantRoles
			}
			tenantRoles[roleTenantAdmin] = struct{}{}
		}
	}
	return current, nil
}

func bindingAppliesToActor(binding platformv1alpha1.RoleBinding, current actor, matchedUser *platformv1alpha1.User, localGroups map[string]struct{}) bool {
	for _, subject := range binding.Spec.Subjects {
		switch subject.Kind {
		case platformv1alpha1.SubjectKindUser:
			if matchedUser != nil && subject.Name == matchedUser.Name {
				return true
			}
			if subject.Name == current.Subject || subject.Name == current.Email {
				return true
			}
		case platformv1alpha1.SubjectKindGroup:
			if _, ok := localGroups[subject.Name]; ok {
				return true
			}
		}
	}
	return false
}

func tenantVisibleLegacy(current actor, matchedUser *platformv1alpha1.User, tenant platformv1alpha1.Tenant) bool {
	for _, user := range tenant.Spec.Owners.Users {
		if matchedUser != nil && user == matchedUser.Name {
			return true
		}
		if user == current.Email || user == current.Subject || user == current.UserName {
			return true
		}
	}
	for _, group := range tenant.Spec.Owners.Groups {
		if _, ok := current.Groups[group]; ok {
			return true
		}
	}
	return false
}

func (a *authRuntime) matchUser(users []platformv1alpha1.User, identity resolvedIdentity) *platformv1alpha1.User {
	for i := range users {
		user := &users[i]
		if identity.ProviderType == string(platformv1alpha1.AuthProviderTypeLocal) && user.Name == identity.Subject {
			return user
		}
		for _, external := range user.Spec.ExternalIdentities {
			if external.ProviderRef.Name == identity.ProviderName && external.Subject == identity.Subject {
				return user
			}
		}
		if identity.Email != "" && strings.EqualFold(user.Spec.Email, identity.Email) {
			return user
		}
		if user.Name == identity.Subject || user.Name == identity.Name {
			return user
		}
	}
	return nil
}

func (a *authRuntime) listEnabledProviders(ctx context.Context) ([]platformv1alpha1.AuthProvider, error) {
	var list platformv1alpha1.AuthProviderList
	if err := a.client.List(ctx, &list); err != nil {
		return nil, err
	}
	result := make([]platformv1alpha1.AuthProvider, 0, len(list.Items))
	for _, provider := range list.Items {
		if provider.Spec.Enabled {
			result = append(result, provider)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Spec.Default != result[j].Spec.Default {
			return result[i].Spec.Default
		}
		return result[i].Name < result[j].Name
	})
	return result, nil
}

func (a *authRuntime) defaultProviderName(ctx context.Context, allowedTypes []platformv1alpha1.AuthProviderType) string {
	providers, err := a.listEnabledProviders(ctx)
	if err != nil {
		return ""
	}
	allowed := map[platformv1alpha1.AuthProviderType]struct{}{}
	for _, providerType := range allowedTypes {
		allowed[providerType] = struct{}{}
	}
	for _, provider := range providers {
		if _, ok := allowed[provider.Spec.Type]; ok {
			return provider.Name
		}
	}
	return ""
}

func (a *authRuntime) getProvider(ctx context.Context, name string) (*platformv1alpha1.AuthProvider, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("auth provider is required")
	}
	var provider platformv1alpha1.AuthProvider
	if err := a.client.Get(ctx, types.NamespacedName{Name: name}, &provider); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("auth provider %q was not found", name)
		}
		return nil, err
	}
	if !provider.Spec.Enabled {
		return nil, fmt.Errorf("auth provider %q is disabled", name)
	}
	return &provider, nil
}

func (a *authRuntime) oidcProvider(ctx context.Context, provider *platformv1alpha1.AuthProvider) (*oidcProviderRuntime, error) {
	if provider == nil || provider.Spec.OIDC == nil {
		return nil, errors.New("OIDC provider is not configured")
	}
	a.mu.RLock()
	cached, ok := a.oidcCache[provider.Name]
	a.mu.RUnlock()
	if ok && cached.resourceVersion == provider.ResourceVersion {
		return cached.runtime, nil
	}
	cfg := provider.Spec.OIDC
	clientSecret, err := a.readSecretValue(ctx, cfg.ClientSecretRef, "clientSecret")
	if err != nil {
		return nil, fmt.Errorf("read OIDC client secret: %w", err)
	}
	discovered, err := oidc.NewProvider(ctx, cfg.IssuerURL)
	if err != nil {
		return nil, fmt.Errorf("create OIDC provider: %w", err)
	}
	runtime := &oidcProviderRuntime{
		provider: provider,
		verifier: discovered.Verifier(&oidc.Config{ClientID: cfg.ClientID}),
		oauth2Config: oauth2.Config{
			ClientID:     cfg.ClientID,
			ClientSecret: clientSecret,
			Endpoint:     discovered.Endpoint(),
			Scopes:       defaultOIDCScopes(cfg.Scopes),
		},
		usernameClaim: firstNonEmpty(strings.TrimSpace(cfg.UsernameClaim), "preferred_username", "sub"),
		emailClaim:    firstNonEmpty(strings.TrimSpace(cfg.EmailClaim), "email"),
		rolesClaim:    firstNonEmpty(strings.TrimSpace(cfg.RolesClaim), "roles"),
		groupsClaim:   firstNonEmpty(strings.TrimSpace(cfg.GroupsClaim), "groups"),
		endSessionURL: strings.TrimSpace(cfg.EndSessionURL),
	}
	a.mu.Lock()
	a.oidcCache[provider.Name] = cachedOIDCProvider{resourceVersion: provider.ResourceVersion, runtime: runtime}
	a.mu.Unlock()
	return runtime, nil
}

func defaultOIDCScopes(scopes []string) []string {
	if len(scopes) == 0 {
		return []string{"openid", "profile", "email", "offline_access"}
	}
	filtered := make([]string, 0, len(scopes))
	for _, scope := range scopes {
		if scope = strings.TrimSpace(scope); scope != "" {
			filtered = append(filtered, scope)
		}
	}
	if len(filtered) == 0 {
		return []string{"openid", "profile", "email", "offline_access"}
	}
	return filtered
}

func (a *authRuntime) ensureBootstrapAdmin(ctx context.Context) error {
	if a.bootstrap.Username == "" || a.bootstrap.Password == "" {
		return nil
	}
	const (
		namespace    = "servicer-system"
		providerName = "local"
		bindingName  = "bootstrap-platform-admin"
	)
	provider := &platformv1alpha1.AuthProvider{}
	err := a.client.Get(ctx, types.NamespacedName{Name: providerName}, provider)
	if apierrors.IsNotFound(err) {
		provider = &platformv1alpha1.AuthProvider{
			ObjectMeta: metav1.ObjectMeta{Name: providerName},
			Spec: platformv1alpha1.AuthProviderSpec{
				DisplayName: "Local",
				Type:        platformv1alpha1.AuthProviderTypeLocal,
				Enabled:     true,
				Default:     true,
			},
		}
		if err := a.client.Create(ctx, provider); err != nil && !apierrors.IsAlreadyExists(err) {
			return err
		}
	} else if err != nil {
		return err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(a.bootstrap.Password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: localUserSecretName(a.bootstrap.Username), Namespace: namespace},
		StringData: map[string]string{"passwordHash": string(hash)},
	}
	if err := a.client.Patch(ctx, secret, client.Apply, client.FieldOwner("servicer-auth"), client.ForceOwnership); err != nil {
		return err
	}
	user := &platformv1alpha1.User{
		ObjectMeta: metav1.ObjectMeta{Name: a.bootstrap.Username},
		Spec: platformv1alpha1.UserSpec{
			DisplayName: a.bootstrap.Username,
			Email:       a.bootstrap.Email,
			LocalAuth: &platformv1alpha1.LocalAuthSpec{
				Enabled: true,
				PasswordHashSecretRef: platformv1alpha1.NamespacedObjectReference{
					Name:      localUserSecretName(a.bootstrap.Username),
					Namespace: namespace,
				},
			},
		},
	}
	if err := a.client.Patch(ctx, user, client.Apply, client.FieldOwner("servicer-auth"), client.ForceOwnership); err != nil {
		return err
	}
	roleBinding := &platformv1alpha1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: bindingName},
		Spec: platformv1alpha1.RoleBindingSpec{
			DisplayName: "Bootstrap platform admin",
			Scope:       platformv1alpha1.AccessScopePlatform,
			Subjects: []platformv1alpha1.SubjectReference{{
				Kind: platformv1alpha1.SubjectKindUser,
				Name: a.bootstrap.Username,
			}},
			Roles: []platformv1alpha1.ServicerRole{
				platformv1alpha1.RolePlatformAdmin,
				platformv1alpha1.RoleCatalogAdmin,
				platformv1alpha1.RoleClusterAdmin,
			},
		},
	}
	return a.client.Patch(ctx, roleBinding, client.Apply, client.FieldOwner("servicer-auth"), client.ForceOwnership)
}

func localUserSecretName(username string) string {
	return fmt.Sprintf("user-%s-local-auth", username)
}

func (a *authRuntime) readSecretValue(ctx context.Context, ref platformv1alpha1.NamespacedObjectReference, key string) (string, error) {
	var secret corev1.Secret
	if err := a.client.Get(ctx, types.NamespacedName{Name: ref.Name, Namespace: ref.Namespace}, &secret); err != nil {
		return "", err
	}
	value := strings.TrimSpace(string(secret.Data[key]))
	if value == "" {
		return "", fmt.Errorf("secret %s/%s did not contain key %q", ref.Namespace, ref.Name, key)
	}
	return value, nil
}

func (a *authRuntime) readSecretUsernamePassword(ctx context.Context, ref platformv1alpha1.NamespacedObjectReference) (string, string, error) {
	if ref.Name == "" || ref.Namespace == "" {
		return "", "", nil
	}
	var secret corev1.Secret
	if err := a.client.Get(ctx, types.NamespacedName{Name: ref.Name, Namespace: ref.Namespace}, &secret); err != nil {
		return "", "", err
	}
	return strings.TrimSpace(string(secret.Data["username"])), strings.TrimSpace(string(secret.Data["password"])), nil
}

func authorizationBearerToken(header string) string {
	header = strings.TrimSpace(header)
	if header == "" {
		return ""
	}
	parts := strings.SplitN(header, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return strings.TrimSpace(parts[1])
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func stringClaim(claims map[string]any, key string) string {
	value, ok := claims[key]
	if !ok {
		return ""
	}
	if typed, ok := value.(string); ok {
		return strings.TrimSpace(typed)
	}
	return ""
}

func setFromClaim(value any) map[string]struct{} {
	items := []string{}
	switch typed := value.(type) {
	case string:
		for _, item := range strings.Split(typed, ",") {
			if item = strings.TrimSpace(item); item != "" {
				items = append(items, item)
			}
		}
	case []any:
		for _, item := range typed {
			if itemString, ok := item.(string); ok {
				if itemString = strings.TrimSpace(itemString); itemString != "" {
					items = append(items, itemString)
				}
			}
		}
	case json.RawMessage:
		_ = json.Unmarshal(typed, &items)
	case []string:
		items = append(items, typed...)
	}
	set := make(map[string]struct{}, len(items))
	for _, item := range items {
		if item = strings.TrimSpace(item); item != "" {
			set[item] = struct{}{}
		}
	}
	return set
}

func newSealedCookieCodec(secret string) *sealedCookieCodec {
	sum := sha256.Sum256([]byte(secret))
	signing := sha256.Sum256([]byte(secret + "|sign"))
	return &sealedCookieCodec{
		encryptionKey: sum[:],
		signingKey:    signing[:],
	}
}

func (c *sealedCookieCodec) Encode(value any) (string, error) {
	plain, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(c.encryptionKey)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ciphertext := gcm.Seal(nil, nonce, plain, nil)
	mac := hmac.New(sha256.New, c.signingKey)
	mac.Write(nonce)
	mac.Write(ciphertext)
	signature := mac.Sum(nil)
	payload := append(append(nonce, ciphertext...), signature...)
	return base64.RawURLEncoding.EncodeToString(payload), nil
}

func (c *sealedCookieCodec) Decode(encoded string, out any) error {
	payload, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return err
	}
	block, err := aes.NewCipher(c.encryptionKey)
	if err != nil {
		return err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}
	nonceSize := gcm.NonceSize()
	if len(payload) <= nonceSize+sha256.Size {
		return errors.New("cookie payload is too short")
	}
	nonce := payload[:nonceSize]
	signature := payload[len(payload)-sha256.Size:]
	ciphertext := payload[nonceSize : len(payload)-sha256.Size]
	mac := hmac.New(sha256.New, c.signingKey)
	mac.Write(nonce)
	mac.Write(ciphertext)
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return errors.New("cookie signature mismatch")
	}
	plain, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return err
	}
	return json.Unmarshal(plain, out)
}

func authCookie(r *http.Request, name, value string, ttl time.Duration) *http.Cookie {
	return &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		HttpOnly: true,
		Secure:   requestIsSecure(r),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(ttl.Seconds()),
		Expires:  time.Now().Add(ttl),
	}
}

func csrfCookie(r *http.Request, value string) *http.Cookie {
	return &http.Cookie{
		Name:     csrfCookieName,
		Value:    value,
		Path:     "/",
		HttpOnly: false,
		Secure:   requestIsSecure(r),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int((24 * time.Hour).Seconds()),
		Expires:  time.Now().Add(24 * time.Hour),
	}
}

func clearAuthCookie(r *http.Request, name string) *http.Cookie {
	return &http.Cookie{
		Name:     name,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   requestIsSecure(r),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
	}
}

func requestIsSecure(r *http.Request) bool {
	if r != nil {
		if r.TLS != nil {
			return true
		}
		if trustedProxyHeadersEnabled() && strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")), "https") {
			return true
		}
	}
	return false
}

func productionMode() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv("SERVICER_PRODUCTION")), "true")
}

func trustedProxyHeadersEnabled() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv("SERVICER_TRUSTED_PROXY_HEADERS")), "true")
}

func absoluteRedirectURL(r *http.Request, path string) string {
	if r == nil {
		return path
	}
	scheme := "http"
	if requestIsSecure(r) {
		scheme = "https"
	}
	host := r.Host
	if forwardedHost := strings.TrimSpace(r.Header.Get("X-Forwarded-Host")); forwardedHost != "" {
		host = forwardedHost
	}
	return scheme + "://" + host + path
}

func randomString(byteCount int) (string, error) {
	buffer := make([]byte, byteCount)
	if _, err := io.ReadFull(rand.Reader, buffer); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}

func pkceChallenge(verifier string) (string, error) {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:]), nil
}
