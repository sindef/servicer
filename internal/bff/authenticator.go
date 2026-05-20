package bff

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/coreos/go-oidc/v3/oidc"
)

type authenticator interface {
	Authenticate(context.Context, *http.Request) (actor, error)
	Mode() string
}

type headerAuthenticator struct{}

func (headerAuthenticator) Authenticate(_ context.Context, r *http.Request) (actor, error) {
	return actorFromHeaders(r), nil
}

func (headerAuthenticator) Mode() string {
	return "header"
}

type oidcAuthenticator struct {
	verifier      *oidc.IDTokenVerifier
	usernameClaim string
	rolesClaim    string
	groupsClaim   string
	allowHeaders  bool
}

func newAuthenticatorFromEnv(ctx context.Context) (authenticator, error) {
	mode := strings.ToLower(strings.TrimSpace(os.Getenv("SERVICER_AUTH_MODE")))
	if mode == "" || mode == "header" || mode == "demo" {
		return headerAuthenticator{}, nil
	}
	if mode != "oidc" {
		return nil, fmt.Errorf("unsupported SERVICER_AUTH_MODE %q", mode)
	}

	issuerURL := strings.TrimSpace(os.Getenv("SERVICER_OIDC_ISSUER_URL"))
	clientID := strings.TrimSpace(os.Getenv("SERVICER_OIDC_CLIENT_ID"))
	if issuerURL == "" || clientID == "" {
		return nil, errors.New("SERVICER_OIDC_ISSUER_URL and SERVICER_OIDC_CLIENT_ID are required when SERVICER_AUTH_MODE=oidc")
	}

	provider, err := oidc.NewProvider(ctx, issuerURL)
	if err != nil {
		return nil, fmt.Errorf("create OIDC provider: %w", err)
	}

	return &oidcAuthenticator{
		verifier:      provider.Verifier(&oidc.Config{ClientID: clientID}),
		usernameClaim: firstNonEmpty(strings.TrimSpace(os.Getenv("SERVICER_OIDC_USERNAME_CLAIM")), "email", "preferred_username", "sub"),
		rolesClaim:    firstNonEmpty(strings.TrimSpace(os.Getenv("SERVICER_OIDC_ROLES_CLAIM")), "roles"),
		groupsClaim:   firstNonEmpty(strings.TrimSpace(os.Getenv("SERVICER_OIDC_GROUPS_CLAIM")), "groups"),
		allowHeaders:  strings.EqualFold(strings.TrimSpace(os.Getenv("SERVICER_AUTH_ALLOW_DEMO_HEADERS")), "true"),
	}, nil
}

func (a *oidcAuthenticator) Authenticate(ctx context.Context, r *http.Request) (actor, error) {
	token := authorizationBearerToken(r.Header.Get("Authorization"))
	if token == "" {
		if a.allowHeaders {
			return actorFromHeaders(r), nil
		}
		return actor{}, errors.New("missing bearer token")
	}

	idToken, err := a.verifier.Verify(ctx, token)
	if err != nil {
		return actor{}, fmt.Errorf("verify bearer token: %w", err)
	}

	claims := map[string]any{}
	if err := idToken.Claims(&claims); err != nil {
		return actor{}, fmt.Errorf("decode token claims: %w", err)
	}

	subject := firstNonEmpty(stringClaim(claims, a.usernameClaim), stringClaim(claims, "preferred_username"), stringClaim(claims, "email"), idToken.Subject)
	if subject == "" {
		return actor{}, errors.New("token did not contain a usable subject claim")
	}

	return actor{
		Name:   subject,
		Roles:  setFromClaim(claims[a.rolesClaim]),
		Groups: setFromClaim(claims[a.groupsClaim]),
	}, nil
}

func (a *oidcAuthenticator) Mode() string {
	return "oidc"
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
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	default:
		return ""
	}
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
