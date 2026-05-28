package bff

import (
	"context"
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	platformv1alpha1 "github.com/sindef/servicer/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestResolveActorFailsWithoutExplicitExternalIdentity(t *testing.T) {
	runtime := newAuthRuntimeForTests(t,
		&platformv1alpha1.User{
			ObjectMeta: metav1.ObjectMeta{Name: "alice"},
			Spec: platformv1alpha1.UserSpec{
				DisplayName: "Alice",
				Email:       "shared@example.com",
			},
		},
		&platformv1alpha1.User{
			ObjectMeta: metav1.ObjectMeta{Name: "bob"},
			Spec: platformv1alpha1.UserSpec{
				DisplayName: "Bob",
				Email:       "shared@example.com",
			},
		},
	)

	_, err := runtime.resolveActor(context.Background(), resolvedIdentity{
		ProviderName: "corp-oidc",
		ProviderType: string(platformv1alpha1.AuthProviderTypeOIDC),
		Subject:      oidcSubject("https://issuer.example.com", "oidc-user-789"),
		AltSubjects:  []string{"oidc-user-789"},
		Name:         "alice",
		Email:        "shared@example.com",
	})
	if err == nil || !strings.Contains(err.Error(), "not linked") {
		t.Fatalf("expected unresolved external identity to fail closed, got %v", err)
	}
}

func TestResolveActorSameEmailCannotCrossLinkUsers(t *testing.T) {
	runtime := newAuthRuntimeForTests(t,
		&platformv1alpha1.User{
			ObjectMeta: metav1.ObjectMeta{Name: "alice"},
			Spec: platformv1alpha1.UserSpec{
				DisplayName: "Alice",
				Email:       "shared@example.com",
				ExternalIdentities: []platformv1alpha1.ExternalIdentitySpec{
					{
						ProviderRef: platformv1alpha1.LocalObjectReference{Name: "corp-oidc"},
						Subject:     oidcSubject("https://issuer.example.com", "alice-sub"),
					},
				},
			},
		},
		&platformv1alpha1.User{
			ObjectMeta: metav1.ObjectMeta{Name: "mallory"},
			Spec: platformv1alpha1.UserSpec{
				DisplayName: "Mallory",
				Email:       "shared@example.com",
			},
		},
	)

	_, err := runtime.resolveActor(context.Background(), resolvedIdentity{
		ProviderName: "corp-oidc",
		ProviderType: string(platformv1alpha1.AuthProviderTypeOIDC),
		Subject:      oidcSubject("https://issuer.example.com", "mallory-sub"),
		AltSubjects:  []string{"mallory-sub"},
		Name:         "mallory",
		Email:        "shared@example.com",
	})
	if err == nil || !strings.Contains(err.Error(), "not linked") {
		t.Fatalf("expected no implicit linking by shared email or name, got %v", err)
	}
}

func TestResolveActorExplicitExternalIdentityStillWorksAfterUsernameChange(t *testing.T) {
	runtime := newAuthRuntimeForTests(t,
		&platformv1alpha1.User{
			ObjectMeta: metav1.ObjectMeta{Name: "alice"},
			Spec: platformv1alpha1.UserSpec{
				DisplayName: "Alice Liddell",
				Email:       "alice@example.com",
				ExternalIdentities: []platformv1alpha1.ExternalIdentitySpec{
					{
						ProviderRef: platformv1alpha1.LocalObjectReference{Name: "corp-oidc"},
						Subject:     oidcSubject("https://issuer.example.com", "stable-subject-123"),
					},
				},
			},
		},
	)

	current, err := runtime.resolveActor(context.Background(), resolvedIdentity{
		ProviderName: "corp-oidc",
		ProviderType: string(platformv1alpha1.AuthProviderTypeOIDC),
		Subject:      oidcSubject("https://issuer.example.com", "stable-subject-123"),
		AltSubjects:  []string{"stable-subject-123"},
		Name:         "alice-renamed",
		Email:        "renamed@example.com",
	})
	if err != nil {
		t.Fatalf("expected explicit external identity to resolve, got %v", err)
	}
	if current.UserName != "alice" {
		t.Fatalf("expected actor to map to linked user, got %#v", current)
	}
}

func TestResolveActorSupportsLegacyExplicitOIDCSub(t *testing.T) {
	runtime := newAuthRuntimeForTests(t,
		&platformv1alpha1.User{
			ObjectMeta: metav1.ObjectMeta{Name: "alice"},
			Spec: platformv1alpha1.UserSpec{
				ExternalIdentities: []platformv1alpha1.ExternalIdentitySpec{
					{
						ProviderRef: platformv1alpha1.LocalObjectReference{Name: "corp-oidc"},
						Subject:     "legacy-sub-42",
					},
				},
			},
		},
	)

	current, err := runtime.resolveActor(context.Background(), resolvedIdentity{
		ProviderName: "corp-oidc",
		ProviderType: string(platformv1alpha1.AuthProviderTypeOIDC),
		Subject:      oidcSubject("https://issuer.example.com", "legacy-sub-42"),
		AltSubjects:  []string{"legacy-sub-42"},
		Name:         "alice",
	})
	if err != nil {
		t.Fatalf("expected legacy explicit OIDC sub mapping to continue working, got %v", err)
	}
	if current.UserName != "alice" {
		t.Fatalf("expected actor to map to linked user, got %#v", current)
	}
}

func TestSafeLocalReturnPathRejectsMaliciousTargets(t *testing.T) {
	t.Parallel()

	rejected := []string{
		"",
		" ",
		"//evil.example",
		`\\evil.example`,
		"https://evil.example",
		"/https://evil.example",
		"/%2f%2fevil.example",
		"/%5c%5cevil.example",
		"/foo://bar",
		"/foo\\bar",
		"/foo\nbar",
		"/foo\rbar",
	}
	for _, target := range rejected {
		target := target
		t.Run(target, func(t *testing.T) {
			t.Parallel()
			if _, ok := safeLocalReturnPath(target); ok {
				t.Fatalf("expected %q to be rejected", target)
			}
		})
	}
}

func TestSafeLocalReturnPathAcceptsNormalRelativePath(t *testing.T) {
	t.Parallel()

	accepted := []string{
		"/",
		"/console",
		"/console/instances?tenant=acme",
	}
	for _, target := range accepted {
		target := target
		t.Run(target, func(t *testing.T) {
			t.Parallel()
			got, ok := safeLocalReturnPath(target)
			if !ok || got != target {
				t.Fatalf("expected %q to be accepted, got %q ok=%t", target, got, ok)
			}
		})
	}
}

func TestLogoutRedirectURLRejectsUnsafeReturnTarget(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequest("GET", "/api/auth/logout?returnTo=//evil.example", nil)
	runtime := &authRuntime{}
	if got := runtime.LogoutRedirectURL(context.Background(), request); got != "/" {
		t.Fatalf("expected unsafe logout target to fall back to /, got %q", got)
	}
}

func TestAbsoluteRedirectURLForwardedHostTrustRules(t *testing.T) {
	request := httptest.NewRequest("GET", "https://internal.servicer.local/api/auth/login", nil)
	request.TLS = &tls.ConnectionState{}
	request.Header.Set("X-Forwarded-Host", "evil.example")

	t.Setenv("SERVICER_TRUSTED_PROXY_HEADERS", "")
	t.Setenv("SERVICER_AUTH_EXTERNAL_BASE_URL", "")
	if got := absoluteRedirectURL(request, "/api/auth/callback"); got != "https://internal.servicer.local/api/auth/callback" {
		t.Fatalf("expected untrusted forwarded host to be ignored, got %q", got)
	}

	t.Setenv("SERVICER_TRUSTED_PROXY_HEADERS", "true")
	if got := absoluteRedirectURL(request, "/api/auth/callback"); got != "https://evil.example/api/auth/callback" {
		t.Fatalf("expected trusted forwarded host to be used, got %q", got)
	}

	request.Header.Set("X-Forwarded-Host", "evil.example/path")
	if got := absoluteRedirectURL(request, "/api/auth/callback"); got != "https://internal.servicer.local/api/auth/callback" {
		t.Fatalf("expected invalid forwarded host to be ignored, got %q", got)
	}
}

func TestAbsoluteRedirectURLUsesConfiguredExternalBaseURL(t *testing.T) {
	t.Setenv("SERVICER_AUTH_EXTERNAL_BASE_URL", "https://servicer.example.com")
	t.Setenv("SERVICER_TRUSTED_PROXY_HEADERS", "true")

	request := httptest.NewRequest("GET", "http://internal.servicer.local/api/auth/login", nil)
	request.Header.Set("X-Forwarded-Host", "evil.example")
	if got := absoluteRedirectURL(request, "/api/auth/callback"); got != "https://servicer.example.com/api/auth/callback" {
		t.Fatalf("expected configured external base URL to win, got %q", got)
	}
}

func TestAbsoluteRedirectURLRejectsUnsafePath(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequest("GET", "https://servicer.local/api/auth/login", nil)
	request.TLS = &tls.ConnectionState{}
	if got := absoluteRedirectURL(request, "//evil.example"); got != "https://servicer.local/" {
		t.Fatalf("expected unsafe redirect path fallback, got %q", got)
	}
}

func TestWriteAuthCallbackSuccessResponseSetsSessionAndCSRFCookies(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequest(http.MethodGet, "/api/auth/callback", nil)
	request.TLS = &tls.ConnectionState{}
	response := httptest.NewRecorder()

	writeAuthCallbackSuccessResponse(response, request, "encoded-session", "/console")

	if response.Code != http.StatusFound {
		t.Fatalf("expected redirect status, got %d", response.Code)
	}
	if location := response.Header().Get("Location"); location != "/console" {
		t.Fatalf("expected redirect to /console, got %q", location)
	}
	setCookies := response.Result().Cookies()
	foundSession := false
	foundCSRF := false
	for _, cookie := range setCookies {
		if cookie.Name == authSessionCookieName && cookie.Value == "encoded-session" {
			foundSession = true
		}
		if cookie.Name == csrfCookieName && cookie.Value != "" {
			foundCSRF = true
		}
	}
	if !foundSession || !foundCSRF {
		t.Fatalf("expected session and csrf cookies, got %#v", setCookies)
	}
}

func newAuthRuntimeForTests(t *testing.T, objects ...client.Object) *authRuntime {
	t.Helper()

	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("add client-go scheme: %v", err)
	}
	if err := platformv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("add platform scheme: %v", err)
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objects...).
		Build()

	return &authRuntime{client: fakeClient}
}
