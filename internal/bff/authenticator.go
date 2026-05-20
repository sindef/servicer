package bff

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

const (
	authSessionCookieName = "servicer_session"
	authFlowCookieName    = "servicer_auth_flow"
	defaultOIDCRedirect   = "/api/auth/callback"
)

type authSettings struct {
	Mode             string
	AllowDemoHeaders bool
	OIDC             *oidcSettings
}

type oidcSettings struct {
	IssuerURL     string
	ClientID      string
	UsernameClaim string
	RolesClaim    string
	GroupsClaim   string
	Scopes        []string
	RedirectPath  string
	SessionSecret string
	EndSessionURL string
}

type authResult struct {
	Actor        actor
	Session      *oidcSessionState
	ClearSession bool
}

type authenticator interface {
	Authenticate(context.Context, *http.Request) (authResult, error)
	Mode() string
}

type headerAuthenticator struct{}

func (headerAuthenticator) Authenticate(_ context.Context, r *http.Request) (authResult, error) {
	return authResult{Actor: actorFromHeaders(r)}, nil
}

func (headerAuthenticator) Mode() string {
	return "header"
}

type oidcAuthenticator struct {
	verifier      *oidc.IDTokenVerifier
	oauth2Config  oauth2.Config
	usernameClaim string
	rolesClaim    string
	groupsClaim   string
	allowHeaders  bool
	endSessionURL string
	sessionCodec  *sealedCookieCodec
	flowCodec     *sealedCookieCodec
}

type oidcSessionState struct {
	IDToken      string `json:"idToken"`
	RefreshToken string `json:"refreshToken,omitempty"`
	ExpiryUnix   int64  `json:"expiryUnix,omitempty"`
}

type oidcAuthFlowState struct {
	State    string `json:"state"`
	Verifier string `json:"verifier"`
	ReturnTo string `json:"returnTo"`
}

type oidcProviderMetadata struct {
	EndSessionEndpoint string `json:"end_session_endpoint"`
}

type sealedCookieCodec struct {
	encryptionKey []byte
	signingKey    []byte
}

func newAuthenticatorFromEnv(ctx context.Context) (authenticator, error) {
	settings, err := authSettingsFromEnv()
	if err != nil {
		return nil, err
	}
	if settings.Mode == "header" {
		return headerAuthenticator{}, nil
	}

	provider, err := oidc.NewProvider(ctx, settings.OIDC.IssuerURL)
	if err != nil {
		return nil, fmt.Errorf("create OIDC provider: %w", err)
	}

	var metadata oidcProviderMetadata
	_ = provider.Claims(&metadata)
	endSessionURL := strings.TrimSpace(settings.OIDC.EndSessionURL)
	if endSessionURL == "" {
		endSessionURL = strings.TrimSpace(metadata.EndSessionEndpoint)
	}

	sessionCodec := newSealedCookieCodec(settings.OIDC.SessionSecret + "|session")
	flowCodec := newSealedCookieCodec(settings.OIDC.SessionSecret + "|flow")

	return &oidcAuthenticator{
		verifier: provider.Verifier(&oidc.Config{ClientID: settings.OIDC.ClientID}),
		oauth2Config: oauth2.Config{
			ClientID: settings.OIDC.ClientID,
			Endpoint: provider.Endpoint(),
			Scopes:   append([]string(nil), settings.OIDC.Scopes...),
		},
		usernameClaim: settings.OIDC.UsernameClaim,
		rolesClaim:    settings.OIDC.RolesClaim,
		groupsClaim:   settings.OIDC.GroupsClaim,
		allowHeaders:  settings.AllowDemoHeaders,
		endSessionURL: endSessionURL,
		sessionCodec:  sessionCodec,
		flowCodec:     flowCodec,
	}, nil
}

func (a *oidcAuthenticator) Authenticate(ctx context.Context, r *http.Request) (authResult, error) {
	if token := authorizationBearerToken(r.Header.Get("Authorization")); token != "" {
		currentActor, expiry, err := a.actorFromIDToken(ctx, token)
		if err != nil {
			return authResult{}, fmt.Errorf("verify bearer token: %w", err)
		}
		return authResult{Actor: currentActor, Session: &oidcSessionState{IDToken: token, ExpiryUnix: expiry.Unix()}}, nil
	}

	if sessionCookie, err := r.Cookie(authSessionCookieName); err == nil && sessionCookie.Value != "" {
		var session oidcSessionState
		if err := a.sessionCodec.Decode(sessionCookie.Value, &session); err == nil {
			currentActor, expiry, err := a.actorFromIDToken(ctx, session.IDToken)
			if err == nil {
				session.ExpiryUnix = expiry.Unix()
				return authResult{Actor: currentActor, Session: &session}, nil
			}
			if session.RefreshToken != "" {
				refreshed, refreshedActor, refreshErr := a.refreshSession(ctx, session)
				if refreshErr == nil {
					return authResult{Actor: refreshedActor, Session: &refreshed}, nil
				}
			}
		}
	}

	if a.allowHeaders {
		return authResult{Actor: actorFromHeaders(r)}, nil
	}
	return authResult{ClearSession: true}, errors.New("no authenticated session")
}

func (a *oidcAuthenticator) Mode() string {
	return "oidc"
}

func (a *oidcAuthenticator) StartLogin(w http.ResponseWriter, r *http.Request, redirectURI string) error {
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
		State:    state,
		Verifier: verifier,
		ReturnTo: returnTo,
	})
	if err != nil {
		return err
	}
	http.SetCookie(w, authCookie(r, authFlowCookieName, encodedState, 10*time.Minute))
	authCodeURL := a.oauth2Config.AuthCodeURL(
		state,
		oauth2.AccessTypeOffline,
		oauth2.SetAuthURLParam("code_challenge", challenge),
		oauth2.SetAuthURLParam("code_challenge_method", "S256"),
		oauth2.SetAuthURLParam("redirect_uri", redirectURI),
	)
	http.Redirect(w, r, authCodeURL, http.StatusFound)
	return nil
}

func (a *oidcAuthenticator) HandleCallback(ctx context.Context, w http.ResponseWriter, r *http.Request, redirectURI string) error {
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
	token, err := a.oauth2Config.Exchange(
		ctx,
		code,
		oauth2.SetAuthURLParam("code_verifier", flow.Verifier),
		oauth2.SetAuthURLParam("redirect_uri", redirectURI),
	)
	if err != nil {
		return fmt.Errorf("exchange authorization code: %w", err)
	}
	session, _, err := a.sessionFromOAuthToken(ctx, token)
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

func (a *oidcAuthenticator) LogoutRedirectURL(r *http.Request) string {
	target := strings.TrimSpace(r.URL.Query().Get("returnTo"))
	if target == "" || !strings.HasPrefix(target, "/") {
		target = "/"
	}
	if a.endSessionURL == "" {
		return target
	}
	redirectURL, err := url.Parse(a.endSessionURL)
	if err != nil {
		return target
	}
	redirectURL.Query().Set("post_logout_redirect_uri", absoluteRedirectURL(r, target))
	redirectURL.RawQuery = redirectURL.Query().Encode()
	return redirectURL.String()
}

func (a *oidcAuthenticator) SessionFromRequest(ctx context.Context, r *http.Request) (AuthSessionResponse, *oidcSessionState, error) {
	result, err := a.Authenticate(ctx, r)
	if err != nil {
		return AuthSessionResponse{
			Mode:          "oidc",
			Name:          "",
			Authenticated: false,
		}, nil, nil
	}
	return AuthSessionResponse{
		Mode:          "oidc",
		Name:          result.Actor.Name,
		Roles:         sortedKeys(result.Actor.Roles),
		Groups:        sortedKeys(result.Actor.Groups),
		Authenticated: result.Actor.Name != "" && result.Actor.Name != "anonymous",
	}, result.Session, nil
}

func (a *oidcAuthenticator) refreshSession(ctx context.Context, current oidcSessionState) (oidcSessionState, actor, error) {
	tokenSource := a.oauth2Config.TokenSource(ctx, &oauth2.Token{RefreshToken: current.RefreshToken})
	token, err := tokenSource.Token()
	if err != nil {
		return oidcSessionState{}, actor{}, fmt.Errorf("refresh OIDC session: %w", err)
	}
	return a.sessionFromOAuthToken(ctx, token)
}

func (a *oidcAuthenticator) sessionFromOAuthToken(ctx context.Context, token *oauth2.Token) (oidcSessionState, actor, error) {
	if token == nil {
		return oidcSessionState{}, actor{}, errors.New("OIDC token exchange returned no token")
	}
	idToken, _ := token.Extra("id_token").(string)
	if strings.TrimSpace(idToken) == "" {
		return oidcSessionState{}, actor{}, errors.New("OIDC token exchange did not return an id_token")
	}
	currentActor, expiry, err := a.actorFromIDToken(ctx, idToken)
	if err != nil {
		return oidcSessionState{}, actor{}, err
	}
	refreshToken := strings.TrimSpace(token.RefreshToken)
	return oidcSessionState{
		IDToken:      idToken,
		RefreshToken: refreshToken,
		ExpiryUnix:   expiry.Unix(),
	}, currentActor, nil
}

func (a *oidcAuthenticator) actorFromIDToken(ctx context.Context, token string) (actor, time.Time, error) {
	idToken, err := a.verifier.Verify(ctx, token)
	if err != nil {
		return actor{}, time.Time{}, err
	}
	claims := map[string]any{}
	if err := idToken.Claims(&claims); err != nil {
		return actor{}, time.Time{}, fmt.Errorf("decode token claims: %w", err)
	}
	subject := firstNonEmpty(stringClaim(claims, a.usernameClaim), stringClaim(claims, "preferred_username"), stringClaim(claims, "email"), idToken.Subject)
	if subject == "" {
		return actor{}, time.Time{}, errors.New("token did not contain a usable subject claim")
	}
	return actor{
		Name:   subject,
		Roles:  setFromClaim(claims[a.rolesClaim]),
		Groups: setFromClaim(claims[a.groupsClaim]),
	}, idToken.Expiry, nil
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
			item = strings.TrimSpace(item)
			if item != "" {
				items = append(items, item)
			}
		}
	case []any:
		for _, item := range typed {
			if itemString, ok := item.(string); ok {
				itemString = strings.TrimSpace(itemString)
				if itemString != "" {
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

func authSettingsFromEnv() (authSettings, error) {
	mode := strings.ToLower(strings.TrimSpace(os.Getenv("SERVICER_AUTH_MODE")))
	if mode == "" || mode == "header" || mode == "demo" {
		return authSettings{Mode: "header"}, nil
	}
	if mode != "oidc" {
		return authSettings{}, fmt.Errorf("unsupported SERVICER_AUTH_MODE %q", mode)
	}

	issuerURL := strings.TrimSpace(os.Getenv("SERVICER_OIDC_ISSUER_URL"))
	clientID := strings.TrimSpace(os.Getenv("SERVICER_OIDC_CLIENT_ID"))
	sessionSecret := strings.TrimSpace(os.Getenv("SERVICER_SESSION_SECRET"))
	if issuerURL == "" || clientID == "" {
		return authSettings{}, errors.New("SERVICER_OIDC_ISSUER_URL and SERVICER_OIDC_CLIENT_ID are required when SERVICER_AUTH_MODE=oidc")
	}
	if sessionSecret == "" {
		return authSettings{}, errors.New("SERVICER_SESSION_SECRET is required when SERVICER_AUTH_MODE=oidc")
	}

	scopes := []string{"openid", "profile", "email", "offline_access"}
	if rawScopes := strings.TrimSpace(os.Getenv("SERVICER_OIDC_SCOPES")); rawScopes != "" {
		scopes = scopes[:0]
		for _, scope := range strings.Fields(rawScopes) {
			scope = strings.TrimSpace(scope)
			if scope != "" {
				scopes = append(scopes, scope)
			}
		}
	}
	if len(scopes) == 0 {
		scopes = []string{"openid", "offline_access"}
	}

	return authSettings{
		Mode:             "oidc",
		AllowDemoHeaders: strings.EqualFold(strings.TrimSpace(os.Getenv("SERVICER_AUTH_ALLOW_DEMO_HEADERS")), "true"),
		OIDC: &oidcSettings{
			IssuerURL:     issuerURL,
			ClientID:      clientID,
			UsernameClaim: firstNonEmpty(strings.TrimSpace(os.Getenv("SERVICER_OIDC_USERNAME_CLAIM")), "email", "preferred_username", "sub"),
			RolesClaim:    firstNonEmpty(strings.TrimSpace(os.Getenv("SERVICER_OIDC_ROLES_CLAIM")), "roles"),
			GroupsClaim:   firstNonEmpty(strings.TrimSpace(os.Getenv("SERVICER_OIDC_GROUPS_CLAIM")), "groups"),
			Scopes:        scopes,
			RedirectPath:  firstNonEmpty(strings.TrimSpace(os.Getenv("SERVICER_OIDC_REDIRECT_PATH")), defaultOIDCRedirect),
			SessionSecret: sessionSecret,
			EndSessionURL: strings.TrimSpace(os.Getenv("SERVICER_OIDC_END_SESSION_URL")),
		},
	}, nil
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
		if strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")), "https") {
			return true
		}
	}
	return false
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
