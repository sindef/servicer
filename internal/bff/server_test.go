package bff

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	platformv1alpha1 "github.com/sindef/servicer/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

func TestCatalogReturnsProductShapedEntries(t *testing.T) {
	server := testServer(t)
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/catalog", nil)
	request.Header.Set("X-Servicer-User", "alice@example.com")
	request.Header.Set("X-Servicer-Roles", "service-consumer")

	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", response.Code, response.Body.String())
	}
	var entries []CatalogEntry
	if err := json.Unmarshal(response.Body.Bytes(), &entries); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("expected tenant-scoped published products only, got %#v", entries)
	}
	var namespaceEntry *CatalogEntry
	var virtualMachineEntry *CatalogEntry
	for i := range entries {
		if entries[i].Name == "namespace" {
			namespaceEntry = &entries[i]
		}
		if entries[i].Name == "virtual-machine" {
			virtualMachineEntry = &entries[i]
		}
	}
	if namespaceEntry == nil || len(namespaceEntry.Plans) != 1 || len(namespaceEntry.Actions) == 0 {
		t.Fatalf("expected namespace catalog entry with plans/actions, got %#v", entries)
	}
	if virtualMachineEntry == nil || len(virtualMachineEntry.Plans) != 1 {
		t.Fatalf("expected virtual-machine catalog entry with plan visibility, got %#v", entries)
	}
}

func TestCatalogHidesUnpublishedClasses(t *testing.T) {
	server := testServer(t)

	var valkeyClass platformv1alpha1.ServiceClass
	if err := server.client.Get(context.Background(), client.ObjectKey{Name: "valkey"}, &valkeyClass); err != nil {
		t.Fatalf("get valkey class: %v", err)
	}
	valkeyClass.Spec.Published = false
	valkeyClass.Status.Published = false
	if err := server.client.Update(context.Background(), &valkeyClass); err != nil {
		t.Fatalf("update valkey class: %v", err)
	}

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/catalog", nil)
	request.Header.Set("X-Servicer-User", "alice@example.com")
	request.Header.Set("X-Servicer-Roles", "service-consumer")
	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", response.Code, response.Body.String())
	}
	var entries []CatalogEntry
	if err := json.Unmarshal(response.Body.Bytes(), &entries); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	for _, entry := range entries {
		if entry.Name == "valkey" {
			t.Fatalf("expected unpublished class to be hidden, got %#v", entries)
		}
	}
}

func TestServiceClassDisplayNameNormalizesLegacyArgoName(t *testing.T) {
	got := serviceClassDisplayName("argo-application", "Argo CD Application")
	if got != "Managed Application" {
		t.Fatalf("expected normalized name, got %q", got)
	}
}

func TestServiceClassCapabilitiesFallsBackForArgo(t *testing.T) {
	class := platformv1alpha1.ServiceClass{
		ObjectMeta: metav1.ObjectMeta{Name: "argo-application"},
		Spec:       platformv1alpha1.ServiceClassSpec{},
	}
	got := serviceClassCapabilities(class)
	if strings.Join(got, ",") != "manifests,helm" {
		t.Fatalf("expected argo capability defaults, got %#v", got)
	}
}

func TestTenantScopedRoleInheritance(t *testing.T) {
	current := actor{
		Roles: map[string]struct{}{},
		TenantRoles: map[string]map[string]struct{}{
			"acme": {roleTenantAdmin: {}},
		},
	}
	if !current.hasAny(roleTenantOperator) || !current.hasAny(roleServiceConsumer) {
		t.Fatalf("expected tenant-admin to satisfy tenant-operator and service-consumer")
	}
	if !current.hasTenantRole("acme", roleTenantOperator) || !current.hasTenantRole("acme", roleServiceConsumer) {
		t.Fatalf("expected tenant-admin to satisfy tenant scoped access checks")
	}
	if current.hasAny(rolePlatformAdmin) {
		t.Fatalf("tenant-admin must not satisfy platform-admin")
	}

	current.Roles = map[string]struct{}{roleTenantAdmin: {}}
	current.TenantRoles = map[string]map[string]struct{}{}
	if !current.hasAny(roleTenantOperator) || current.hasAny(rolePlatformAdmin) {
		t.Fatalf("expected global tenant-admin to inherit tenant roles only")
	}

	current.Roles = map[string]struct{}{}
	current.TenantRoles["acme"] = map[string]struct{}{roleTenantOperator: {}}
	if !current.hasAny(roleServiceConsumer) {
		t.Fatalf("expected tenant-operator to satisfy service-consumer")
	}
}

func TestCustomRoleExpansion(t *testing.T) {
	expander := newRoleExpander([]RoleSummary{
		{Name: roleTenantOperator, Scope: "tenant", BuiltIn: true, Permissions: []string{roleTenantOperator}},
		{Name: "database-operator", Scope: "tenant", Permissions: []string{roleTenantOperator}},
	})
	expanded := expander.expand("database-operator", "tenant")
	if strings.Join(expanded, ",") != "database-operator,tenant-operator" {
		t.Fatalf("unexpected custom role expansion %#v", expanded)
	}
}

func TestRoleDefinitionLifecycle(t *testing.T) {
	server := testServer(t)
	body := strings.NewReader(`{
		"name":"database-operator",
		"displayName":"Database Operator",
		"description":"Operate database products.",
		"scope":"tenant",
		"permissions":["tenant-operator"]
	}`)
	create := httptest.NewRecorder()
	createRequest := httptest.NewRequest(http.MethodPost, "/api/admin/auth/roles", body)
	createRequest.Header.Set("X-Servicer-User", "admin@example.com")
	createRequest.Header.Set("X-Servicer-Roles", "platform-admin")
	server.Handler().ServeHTTP(create, createRequest)
	if create.Code != http.StatusCreated {
		t.Fatalf("expected create status 201, got %d: %s", create.Code, create.Body.String())
	}

	list := httptest.NewRecorder()
	listRequest := httptest.NewRequest(http.MethodGet, "/api/admin/auth/roles", nil)
	listRequest.Header.Set("X-Servicer-User", "admin@example.com")
	listRequest.Header.Set("X-Servicer-Roles", "platform-admin")
	server.Handler().ServeHTTP(list, listRequest)
	if list.Code != http.StatusOK {
		t.Fatalf("expected list status 200, got %d: %s", list.Code, list.Body.String())
	}
	var roles []RoleSummary
	if err := json.Unmarshal(list.Body.Bytes(), &roles); err != nil {
		t.Fatalf("decode roles: %v", err)
	}
	foundCustom := false
	foundBuiltIn := false
	for _, role := range roles {
		if role.Name == "database-operator" && !role.BuiltIn && strings.Join(role.Permissions, ",") == "tenant-operator" {
			foundCustom = true
		}
		if role.Name == rolePlatformAdmin && role.BuiltIn {
			foundBuiltIn = true
		}
	}
	if !foundCustom || !foundBuiltIn {
		t.Fatalf("expected custom and built-in roles, got %#v", roles)
	}
}

func TestAuthConfigEndpointReturnsConfiguredProviders(t *testing.T) {
	server := testServer(t)
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/auth/config", nil)

	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", response.Code, response.Body.String())
	}
	var config AuthConfigResponse
	if err := json.Unmarshal(response.Body.Bytes(), &config); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if config.Mode != "multi" {
		t.Fatalf("expected multi auth mode, got %#v", config)
	}
}

func TestAuthSessionEndpointReturnsActorContext(t *testing.T) {
	server := testServer(t)
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/auth/session", nil)
	request.Header.Set("X-Servicer-User", "alice@example.com")
	request.Header.Set("X-Servicer-Roles", "tenant-operator,service-consumer")
	request.Header.Set("X-Servicer-Groups", "ops,acme")

	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", response.Code, response.Body.String())
	}
	var session AuthSessionResponse
	if err := json.Unmarshal(response.Body.Bytes(), &session); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if session.Name != "alice@example.com" || !session.Authenticated {
		t.Fatalf("unexpected auth session %#v", session)
	}
	if len(session.Roles) != 2 || len(session.Groups) != 2 {
		t.Fatalf("expected roles and groups, got %#v", session)
	}
}

func TestAuthSessionEndpointMintsCSRFCookieForExistingSession(t *testing.T) {
	server := testServer(t)
	server.auth.allowTestHeaders = false

	encodedSession, err := server.auth.sessionCodec.Encode(authSessionState{
		ProviderName: "local",
		ProviderType: string(platformv1alpha1.AuthProviderTypeLocal),
		Subject:      "alice@example.com",
		Name:         "alice@example.com",
		Email:        "alice@example.com",
	})
	if err != nil {
		t.Fatalf("encode session cookie: %v", err)
	}

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/auth/session", nil)
	request.AddCookie(&http.Cookie{Name: authSessionCookieName, Value: encodedSession})

	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", response.Code, response.Body.String())
	}

	setCookies := response.Result().Cookies()
	foundCSRF := false
	for _, cookie := range setCookies {
		if cookie.Name == csrfCookieName {
			foundCSRF = cookie.Value != ""
			break
		}
	}
	if !foundCSRF {
		t.Fatalf("expected %q cookie to be issued for authenticated session, got %#v", csrfCookieName, setCookies)
	}
}

func TestAuthSessionResponseIncludesEmptyRoleArrays(t *testing.T) {
	payload, err := json.Marshal(authSessionResponse(actor{
		Name:          "test",
		Authenticated: true,
		Roles:         map[string]struct{}{},
		Groups:        map[string]struct{}{},
		TenantRoles:   map[string]map[string]struct{}{},
	}))
	if err != nil {
		t.Fatalf("marshal auth session: %v", err)
	}
	body := string(payload)
	if !strings.Contains(body, `"roles":[]`) || !strings.Contains(body, `"groups":[]`) {
		t.Fatalf("expected empty roles/groups arrays, got %s", body)
	}
}

func TestInstanceDetailAggregatesProductStatus(t *testing.T) {
	server := testServer(t)
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/instances/session-cache", nil)
	request.Header.Set("X-Servicer-User", "alice@example.com")
	request.Header.Set("X-Servicer-Roles", "service-consumer")

	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", response.Code, response.Body.String())
	}
	var detail InstanceDetail
	if err := json.Unmarshal(response.Body.Bytes(), &detail); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if detail.Name != "session-cache" || detail.ProductName != "Valkey" {
		t.Fatalf("unexpected detail identity: %#v", detail.InstanceSummary)
	}
	if detail.Runtime.Driver != "servicer-valkey" {
		t.Fatalf("expected runtime driver servicer-valkey, got %#v", detail.Runtime)
	}
	if detail.Topology == nil || detail.Topology.PrimaryCluster != "east-1" {
		t.Fatalf("expected cache topology summary, got %#v", detail.Topology)
	}
	if len(detail.AvailableActions) == 0 {
		t.Fatalf("expected available actions")
	}
}

func TestInstanceDetailHidesEndpointsForBlockedRuntime(t *testing.T) {
	server := testServer(t)
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/instances/blocked-db", nil)
	request.Header.Set("X-Servicer-User", "alice@example.com")
	request.Header.Set("X-Servicer-Roles", "service-consumer")

	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", response.Code, response.Body.String())
	}
	var detail InstanceDetail
	if err := json.Unmarshal(response.Body.Bytes(), &detail); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if detail.Delivery.RuntimeStatus != "Blocked" {
		t.Fatalf("expected blocked runtime status, got %#v", detail.Delivery)
	}
	if len(detail.Endpoints) != 0 {
		t.Fatalf("expected blocked runtime endpoints to be hidden, got %#v", detail.Endpoints)
	}
}

func TestCreateProductRequestRequiresWriteRoleAndCreatesInstance(t *testing.T) {
	server := testServer(t)
	body := []byte(`{"name":"team-space-new","projectName":"acme-prod","serviceClass":"namespace","servicePlan":"namespace-team"}`)
	forbidden := httptest.NewRecorder()
	forbiddenRequest := httptest.NewRequest(http.MethodPost, "/api/requests", bytes.NewReader(body))
	server.Handler().ServeHTTP(forbidden, forbiddenRequest)
	if forbidden.Code != http.StatusForbidden {
		t.Fatalf("expected forbidden without role, got %d", forbidden.Code)
	}

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/requests", bytes.NewReader(body))
	request.Header.Set("X-Servicer-User", "alice@example.com")
	request.Header.Set("X-Servicer-Roles", "service-consumer")
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d: %s", response.Code, response.Body.String())
	}
}

func TestCreateProductRequestRejectsInvalidRuntimeName(t *testing.T) {
	server := testServer(t)
	body := []byte(`{"name":"11","projectName":"acme-prod","serviceClass":"postgresql","servicePlan":"postgresql-ha"}`)
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/requests", bytes.NewReader(body))
	request.Header.Set("X-Servicer-User", "alice@example.com")
	request.Header.Set("X-Servicer-Roles", "service-consumer")

	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid runtime name to be rejected, got %d: %s", response.Code, response.Body.String())
	}
}

func TestCreateUserCreatesLocalAuthSecret(t *testing.T) {
	server := testServer(t)
	body := []byte(`{
		"name":"alice",
		"displayName":"Alice Johnson",
		"email":"alice@example.com",
		"localAuthEnabled":true,
		"password":"super-secret"
	}`)
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/admin/auth/users", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Servicer-User", "alice@example.com")
	request.Header.Set("X-Servicer-Roles", "platform-admin")

	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d: %s", response.Code, response.Body.String())
	}

	var user platformv1alpha1.User
	if err := server.client.Get(request.Context(), types.NamespacedName{Name: "alice"}, &user); err != nil {
		t.Fatalf("expected user to be created: %v", err)
	}
	if user.Spec.LocalAuth == nil || user.Spec.LocalAuth.PasswordHashSecretRef.Name == "" {
		t.Fatalf("expected local auth secret ref, got %#v", user.Spec.LocalAuth)
	}

	var secret corev1.Secret
	key := types.NamespacedName{Name: localUserSecretName("alice"), Namespace: authSecretNamespace}
	if err := server.client.Get(request.Context(), key, &secret); err != nil {
		t.Fatalf("expected secret to be created: %v", err)
	}
	if len(secret.Data["passwordHash"]) == 0 {
		t.Fatalf("expected password hash in secret, got %#v", secret.Data)
	}
}

func TestCreateOIDCAuthProviderRequiresClientSecret(t *testing.T) {
	server := testServer(t)
	body := []byte(`{
		"name":"corp",
		"displayName":"Corporate OIDC",
		"type":"oidc",
		"enabled":true,
		"oidcIssuerUrl":"https://issuer.example.com",
		"oidcClientId":"servicer"
	}`)
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/admin/auth/providers", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Servicer-User", "admin@example.com")
	request.Header.Set("X-Servicer-Roles", "platform-admin")

	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d: %s", response.Code, response.Body.String())
	}
	var validation authAdminValidationResponse
	if err := json.Unmarshal(response.Body.Bytes(), &validation); err != nil {
		t.Fatalf("decode validation response: %v", err)
	}
	if validation.Code != "validation_failed" || !hasFieldError(validation.FieldErrors, "oidcClientSecret") {
		t.Fatalf("expected OIDC client secret validation error, got %#v", validation)
	}
	var provider platformv1alpha1.AuthProvider
	if err := server.client.Get(request.Context(), types.NamespacedName{Name: "corp"}, &provider); err == nil {
		t.Fatalf("expected auth provider not to be created")
	}
}

func TestUpdateUserEnablesLocalAuthWithPassword(t *testing.T) {
	server := testServer(t)
	createExternalOnlyTestUser(t, server, "carol")
	body := []byte(`{
		"displayName":"Carol Singer",
		"email":"carol@example.com",
		"localAuthEnabled":true,
		"password":"new-secret"
	}`)
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPut, "/api/admin/auth/users/carol", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Servicer-User", "admin@example.com")
	request.Header.Set("X-Servicer-Roles", "platform-admin")

	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", response.Code, response.Body.String())
	}
	var user platformv1alpha1.User
	if err := server.client.Get(request.Context(), types.NamespacedName{Name: "carol"}, &user); err != nil {
		t.Fatalf("expected user to be updated: %v", err)
	}
	if user.Spec.LocalAuth == nil || user.Spec.LocalAuth.PasswordHashSecretRef.Name != localUserSecretName("carol") {
		t.Fatalf("expected local auth secret ref, got %#v", user.Spec.LocalAuth)
	}
	var secret corev1.Secret
	key := types.NamespacedName{Name: localUserSecretName("carol"), Namespace: authSecretNamespace}
	if err := server.client.Get(request.Context(), key, &secret); err != nil {
		t.Fatalf("expected secret to be created: %v", err)
	}
	if len(secret.Data["passwordHash"]) == 0 {
		t.Fatalf("expected password hash in secret, got %#v", secret.Data)
	}
}

func TestUpdateUserEnablingLocalAuthRequiresPassword(t *testing.T) {
	server := testServer(t)
	createExternalOnlyTestUser(t, server, "dana")
	body := []byte(`{
		"displayName":"Dana Scully",
		"email":"dana@example.com",
		"localAuthEnabled":true
	}`)
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPut, "/api/admin/auth/users/dana", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Servicer-User", "admin@example.com")
	request.Header.Set("X-Servicer-Roles", "platform-admin")

	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d: %s", response.Code, response.Body.String())
	}
	var validation authAdminValidationResponse
	if err := json.Unmarshal(response.Body.Bytes(), &validation); err != nil {
		t.Fatalf("decode validation response: %v", err)
	}
	if validation.Code != "validation_failed" || !hasFieldError(validation.FieldErrors, "password") {
		t.Fatalf("expected password validation error, got %#v", validation)
	}
	var user platformv1alpha1.User
	if err := server.client.Get(request.Context(), types.NamespacedName{Name: "dana"}, &user); err != nil {
		t.Fatalf("expected user to remain: %v", err)
	}
	if user.Spec.LocalAuth != nil {
		t.Fatalf("expected local auth to remain disabled, got %#v", user.Spec.LocalAuth)
	}
	var secret corev1.Secret
	key := types.NamespacedName{Name: localUserSecretName("dana"), Namespace: authSecretNamespace}
	if err := server.client.Get(request.Context(), key, &secret); err == nil {
		t.Fatalf("expected no password secret to be created")
	}
}

func TestUpdateUserDisablesLocalAuth(t *testing.T) {
	server := testServer(t)
	secretName := localUserSecretName("erin")
	if err := server.client.Create(context.Background(), &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: authSecretNamespace},
		Data:       map[string][]byte{"passwordHash": []byte("existing-hash")},
	}); err != nil {
		t.Fatalf("create password secret: %v", err)
	}
	if err := server.client.Create(context.Background(), &platformv1alpha1.User{
		ObjectMeta: metav1.ObjectMeta{Name: "erin"},
		Spec: platformv1alpha1.UserSpec{
			DisplayName: "Erin Carter",
			Email:       "erin@example.com",
			LocalAuth: &platformv1alpha1.LocalAuthSpec{
				Enabled: true,
				PasswordHashSecretRef: platformv1alpha1.NamespacedObjectReference{
					Name:      secretName,
					Namespace: authSecretNamespace,
				},
			},
		},
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	body := []byte(`{
		"displayName":"Erin Carter",
		"email":"erin@example.com",
		"localAuthEnabled":false,
		"externalIdentities":[{"provider":"oidc","subject":"erin-sub"}]
	}`)
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPut, "/api/admin/auth/users/erin", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Servicer-User", "admin@example.com")
	request.Header.Set("X-Servicer-Roles", "platform-admin")

	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", response.Code, response.Body.String())
	}
	var user platformv1alpha1.User
	if err := server.client.Get(request.Context(), types.NamespacedName{Name: "erin"}, &user); err != nil {
		t.Fatalf("expected user to be updated: %v", err)
	}
	if user.Spec.LocalAuth != nil {
		t.Fatalf("expected local auth to be disabled, got %#v", user.Spec.LocalAuth)
	}
	if len(user.Spec.ExternalIdentities) != 1 || user.Spec.ExternalIdentities[0].Subject != "erin-sub" {
		t.Fatalf("expected external identity to remain configured, got %#v", user.Spec.ExternalIdentities)
	}
}

func TestUpdateUserWithoutPriorLocalAuthKeepsItDisabled(t *testing.T) {
	server := testServer(t)
	createExternalOnlyTestUser(t, server, "frank")
	body := []byte(`{
		"displayName":"Frank Black",
		"email":"frank@example.com",
		"localAuthEnabled":false,
		"externalIdentities":[{"provider":"oidc","subject":"frank-updated"}]
	}`)
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPut, "/api/admin/auth/users/frank", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Servicer-User", "admin@example.com")
	request.Header.Set("X-Servicer-Roles", "platform-admin")

	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", response.Code, response.Body.String())
	}
	var user platformv1alpha1.User
	if err := server.client.Get(request.Context(), types.NamespacedName{Name: "frank"}, &user); err != nil {
		t.Fatalf("expected user to be updated: %v", err)
	}
	if user.Spec.LocalAuth != nil {
		t.Fatalf("expected local auth to remain disabled, got %#v", user.Spec.LocalAuth)
	}
	if len(user.Spec.ExternalIdentities) != 1 || user.Spec.ExternalIdentities[0].Subject != "frank-updated" {
		t.Fatalf("expected external identity update, got %#v", user.Spec.ExternalIdentities)
	}
	var secret corev1.Secret
	key := types.NamespacedName{Name: localUserSecretName("frank"), Namespace: authSecretNamespace}
	if err := server.client.Get(request.Context(), key, &secret); err == nil {
		t.Fatalf("expected no password secret to be created")
	}
}

func createExternalOnlyTestUser(t *testing.T, server *Server, name string) {
	t.Helper()
	if err := server.client.Create(context.Background(), &platformv1alpha1.User{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: platformv1alpha1.UserSpec{
			DisplayName: name,
			Email:       name + "@example.com",
			ExternalIdentities: []platformv1alpha1.ExternalIdentitySpec{{
				ProviderRef: platformv1alpha1.LocalObjectReference{Name: "oidc"},
				Subject:     name + "-sub",
			}},
		},
	}); err != nil {
		t.Fatalf("create external-only user: %v", err)
	}
}

func hasFieldError(fields []authAdminFieldValidationError, field string) bool {
	for _, candidate := range fields {
		if candidate.Field == field {
			return true
		}
	}
	return false
}

func TestUpdateProductRequestChangesPlan(t *testing.T) {
	server := testServer(t)
	body := []byte(`{"name":"session-cache","projectName":"acme-prod","serviceClass":"valkey","servicePlan":"valkey-replicated","version":"8.0"}`)
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPut, "/api/instances/session-cache", bytes.NewReader(body))
	request.Header.Set("X-Servicer-User", "alice@example.com")
	request.Header.Set("X-Servicer-Roles", "service-consumer")

	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", response.Code, response.Body.String())
	}
}

func TestProjectRepositoryLifecycle(t *testing.T) {
	server := testServer(t)
	body := []byte(`{
		"name":"storefront-app",
		"displayName":"Storefront App",
		"url":"https://github.com/acme/storefront.git",
		"authType":"http",
		"username":"git",
		"password":"token"
	}`)

	createResponse := httptest.NewRecorder()
	createRequest := httptest.NewRequest(http.MethodPost, "/api/projects/acme-prod/repositories", bytes.NewReader(body))
	createRequest.Header.Set("X-Servicer-User", "alice@example.com")
	createRequest.Header.Set("X-Servicer-Roles", "tenant-operator")
	server.Handler().ServeHTTP(createResponse, createRequest)
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d: %s", createResponse.Code, createResponse.Body.String())
	}

	listResponse := httptest.NewRecorder()
	listRequest := httptest.NewRequest(http.MethodGet, "/api/projects/acme-prod/repositories", nil)
	listRequest.Header.Set("X-Servicer-User", "alice@example.com")
	listRequest.Header.Set("X-Servicer-Roles", "service-consumer")
	server.Handler().ServeHTTP(listResponse, listRequest)
	if listResponse.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", listResponse.Code, listResponse.Body.String())
	}

	var repos []RepositorySummary
	if err := json.Unmarshal(listResponse.Body.Bytes(), &repos); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(repos) != 1 {
		t.Fatalf("expected one repository, got %#v", repos)
	}
	if repos[0].Name != "storefront-app" || repos[0].URL != "https://github.com/acme/storefront.git" || repos[0].AuthType != "http" {
		t.Fatalf("unexpected repository summary %#v", repos[0])
	}
	if repos[0].Scope != "project" || repos[0].ProjectName != "acme-prod" || repos[0].TenantName != "" {
		t.Fatalf("unexpected repository scope %#v", repos[0])
	}

	var argoSecret corev1.Secret
	argoSecretName := repositoryMirrorNameForTest(t, CreateRepositoryRequest{
		Name:        "storefront-app",
		DisplayName: "Storefront App",
		Scope:       "project",
		ProjectName: "acme-prod",
		URL:         "https://github.com/acme/storefront.git",
		AuthType:    "http",
		Username:    "git",
		Password:    "token",
	})
	if err := server.client.Get(createRequest.Context(), client.ObjectKey{Name: argoSecretName, Namespace: "argocd"}, &argoSecret); err != nil {
		t.Fatalf("expected mirrored Argo CD repository secret: %v", err)
	}
	if got := string(argoSecret.Data["url"]); got != "https://github.com/acme/storefront.git" {
		t.Fatalf("expected Argo CD secret url to match, got %q", got)
	}
	if argoSecret.Labels[repositoryMirrorLabel] != "true" || argoSecret.Labels[repoSecretProjectKey] != "acme-prod" {
		t.Fatalf("expected scoped Argo CD mirror labels, got %#v", argoSecret.Labels)
	}

	deleteResponse := httptest.NewRecorder()
	deleteRequest := httptest.NewRequest(http.MethodDelete, "/api/projects/acme-prod/repositories/storefront-app", nil)
	deleteRequest.Header.Set("X-Servicer-User", "alice@example.com")
	deleteRequest.Header.Set("X-Servicer-Roles", "tenant-operator")
	server.Handler().ServeHTTP(deleteResponse, deleteRequest)
	if deleteResponse.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", deleteResponse.Code, deleteResponse.Body.String())
	}
	if err := server.client.Get(deleteRequest.Context(), client.ObjectKey{Name: argoSecretName, Namespace: "argocd"}, &argoSecret); !apierrors.IsNotFound(err) {
		t.Fatalf("expected mirrored Argo CD repository secret to be removed, got %v", err)
	}
}

func TestTenantRepositoryLifecycle(t *testing.T) {
	server := testServer(t)
	body := []byte(`{
		"name":"tenant-platform",
		"displayName":"Tenant Platform",
		"url":"https://github.com/acme/platform.git",
		"authType":"none"
	}`)

	createResponse := httptest.NewRecorder()
	createRequest := httptest.NewRequest(http.MethodPost, "/api/tenants/acme/repositories", bytes.NewReader(body))
	createRequest.Header.Set("X-Servicer-User", "alice@example.com")
	createRequest.Header.Set("X-Servicer-Roles", "tenant-admin")
	server.Handler().ServeHTTP(createResponse, createRequest)
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d: %s", createResponse.Code, createResponse.Body.String())
	}

	listResponse := httptest.NewRecorder()
	listRequest := httptest.NewRequest(http.MethodGet, "/api/tenants/acme/repositories", nil)
	listRequest.Header.Set("X-Servicer-User", "alice@example.com")
	listRequest.Header.Set("X-Servicer-Roles", "service-consumer")
	server.Handler().ServeHTTP(listResponse, listRequest)
	if listResponse.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", listResponse.Code, listResponse.Body.String())
	}

	var repos []RepositorySummary
	if err := json.Unmarshal(listResponse.Body.Bytes(), &repos); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(repos) != 1 {
		t.Fatalf("expected one repository, got %#v", repos)
	}
	if repos[0].Name != "tenant-platform" || repos[0].TenantName != "acme" || repos[0].Scope != "tenant" || repos[0].ProjectName != "" {
		t.Fatalf("unexpected repository summary %#v", repos[0])
	}

	deleteResponse := httptest.NewRecorder()
	deleteRequest := httptest.NewRequest(http.MethodDelete, "/api/tenants/acme/repositories/tenant-platform", nil)
	deleteRequest.Header.Set("X-Servicer-User", "alice@example.com")
	deleteRequest.Header.Set("X-Servicer-Roles", "tenant-admin")
	server.Handler().ServeHTTP(deleteResponse, deleteRequest)
	if deleteResponse.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", deleteResponse.Code, deleteResponse.Body.String())
	}
}

func TestProjectRepositoryEndpointsRejectUnauthorizedProject(t *testing.T) {
	server := testServer(t)

	listResponse := httptest.NewRecorder()
	listRequest := httptest.NewRequest(http.MethodGet, "/api/projects/rogue-prod/repositories", nil)
	listRequest.Header.Set("X-Servicer-User", "alice@example.com")
	listRequest.Header.Set("X-Servicer-Roles", "service-consumer")
	server.Handler().ServeHTTP(listResponse, listRequest)
	if listResponse.Code != http.StatusForbidden {
		t.Fatalf("expected status 403, got %d: %s", listResponse.Code, listResponse.Body.String())
	}

	createResponse := httptest.NewRecorder()
	createRequest := httptest.NewRequest(http.MethodPost, "/api/projects/rogue-prod/repositories", strings.NewReader(`{
		"name":"rogue-app",
		"displayName":"Rogue App",
		"url":"https://github.com/rogue/app.git"
	}`))
	createRequest.Header.Set("X-Servicer-User", "alice@example.com")
	createRequest.Header.Set("X-Servicer-Roles", "tenant-operator")
	server.Handler().ServeHTTP(createResponse, createRequest)
	if createResponse.Code != http.StatusForbidden {
		t.Fatalf("expected status 403, got %d: %s", createResponse.Code, createResponse.Body.String())
	}
}

func TestTenantRepositoryEndpointsRejectUnauthorizedTenant(t *testing.T) {
	server := testServer(t)

	listResponse := httptest.NewRecorder()
	listRequest := httptest.NewRequest(http.MethodGet, "/api/tenants/rogue/repositories", nil)
	listRequest.Header.Set("X-Servicer-User", "alice@example.com")
	listRequest.Header.Set("X-Servicer-Roles", "service-consumer")
	server.Handler().ServeHTTP(listResponse, listRequest)
	if listResponse.Code != http.StatusForbidden {
		t.Fatalf("expected status 403, got %d: %s", listResponse.Code, listResponse.Body.String())
	}

	createResponse := httptest.NewRecorder()
	createRequest := httptest.NewRequest(http.MethodPost, "/api/tenants/rogue/repositories", strings.NewReader(`{
		"name":"rogue-platform",
		"displayName":"Rogue Platform",
		"url":"https://github.com/rogue/platform.git"
	}`))
	createRequest.Header.Set("X-Servicer-User", "alice@example.com")
	createRequest.Header.Set("X-Servicer-Roles", "tenant-operator")
	server.Handler().ServeHTTP(createResponse, createRequest)
	if createResponse.Code != http.StatusForbidden {
		t.Fatalf("expected status 403, got %d: %s", createResponse.Code, createResponse.Body.String())
	}
}

func TestCreateProjectRepositoryRejectsInvalidName(t *testing.T) {
	server := testServer(t)

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/projects/acme-prod/repositories", strings.NewReader(`{
		"name":"11",
		"displayName":"Broken Repo",
		"url":"https://github.com/acme/broken.git"
	}`))
	request.Header.Set("X-Servicer-User", "alice@example.com")
	request.Header.Set("X-Servicer-Roles", "tenant-operator")
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d: %s", response.Code, response.Body.String())
	}
}

func TestCreateProjectRepositoryRejectsBadURL(t *testing.T) {
	server := testServer(t)

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/projects/acme-prod/repositories", strings.NewReader(`{
		"name":"broken-repo",
		"displayName":"Broken Repo",
		"url":"http://github.com/acme/broken.git",
		"authType":"http",
		"username":"git",
		"password":"token"
	}`))
	request.Header.Set("X-Servicer-User", "alice@example.com")
	request.Header.Set("X-Servicer-Roles", "tenant-operator")
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d: %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "plain http is not allowed") {
		t.Fatalf("expected clear URL validation error, got %s", response.Body.String())
	}
}

func TestCreateProjectRepositoryRollsBackWhenArgoMirrorFails(t *testing.T) {
	server := testServer(t)
	baseClient, ok := server.client.(client.WithWatch)
	if !ok {
		t.Fatalf("test client does not implement client.WithWatch")
	}
	server.client = interceptor.NewClient(baseClient, interceptor.Funcs{
		Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
			secret, ok := obj.(*corev1.Secret)
			if ok && secret.Namespace == argocdNamespace && secret.Labels["argocd.argoproj.io/secret-type"] == "repository" {
				return errors.New("argocd mirror unavailable")
			}
			return c.Create(ctx, obj, opts...)
		},
	})

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/projects/acme-prod/repositories", strings.NewReader(`{
		"name":"storefront-app",
		"displayName":"Storefront App",
		"url":"https://github.com/acme/storefront.git",
		"authType":"none"
	}`))
	request.Header.Set("X-Servicer-User", "alice@example.com")
	request.Header.Set("X-Servicer-Roles", "tenant-operator")
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d: %s", response.Code, response.Body.String())
	}

	var repoSecret corev1.Secret
	err := server.client.Get(request.Context(), client.ObjectKey{Name: projectRepoSecretName("storefront-app"), Namespace: repoSecretNamespace}, &repoSecret)
	if !apierrors.IsNotFound(err) {
		t.Fatalf("expected repository Secret rollback after mirror failure, got %v", err)
	}
}

func TestDeleteProjectRepositoryRejectsActiveReferences(t *testing.T) {
	server := testServer(t)
	createResponse := httptest.NewRecorder()
	createRequest := httptest.NewRequest(http.MethodPost, "/api/projects/acme-prod/repositories", strings.NewReader(`{
		"name":"storefront-app",
		"displayName":"Storefront App",
		"url":"https://github.com/acme/storefront.git",
		"authType":"none"
	}`))
	createRequest.Header.Set("X-Servicer-User", "alice@example.com")
	createRequest.Header.Set("X-Servicer-Roles", "tenant-operator")
	server.Handler().ServeHTTP(createResponse, createRequest)
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d: %s", createResponse.Code, createResponse.Body.String())
	}

	instance := &platformv1alpha1.ServiceInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "storefront-managed-app"},
		Spec: platformv1alpha1.ServiceInstanceSpec{
			ProjectRef:      platformv1alpha1.LocalObjectReference{Name: "acme-prod"},
			ServiceClassRef: platformv1alpha1.LocalObjectReference{Name: "argo-application"},
			ServicePlanRef:  platformv1alpha1.LocalObjectReference{Name: "argo-application"},
			Parameters: &apiextensionsv1.JSON{Raw: []byte(`{
				"repoRef":"storefront-app",
				"repoURL":"https://github.com/acme/storefront.git"
			}`)},
		},
	}
	if err := server.client.Create(context.Background(), instance); err != nil {
		t.Fatalf("create referencing service instance: %v", err)
	}

	deleteResponse := httptest.NewRecorder()
	deleteRequest := httptest.NewRequest(http.MethodDelete, "/api/projects/acme-prod/repositories/storefront-app", nil)
	deleteRequest.Header.Set("X-Servicer-User", "alice@example.com")
	deleteRequest.Header.Set("X-Servicer-Roles", "tenant-operator")
	server.Handler().ServeHTTP(deleteResponse, deleteRequest)
	if deleteResponse.Code != http.StatusConflict {
		t.Fatalf("expected status 409, got %d: %s", deleteResponse.Code, deleteResponse.Body.String())
	}
	var conflict repositoryDependencyConflictResponse
	if err := json.Unmarshal(deleteResponse.Body.Bytes(), &conflict); err != nil {
		t.Fatalf("decode conflict response: %v", err)
	}
	if conflict.Code != "repository_in_use" || strings.Join(conflict.Dependencies, ",") != "managedapplications/storefront-managed-app" {
		t.Fatalf("unexpected dependency conflict %#v", conflict)
	}

	var repoSecret corev1.Secret
	if err := server.client.Get(deleteRequest.Context(), client.ObjectKey{Name: projectRepoSecretName("storefront-app"), Namespace: repoSecretNamespace}, &repoSecret); err != nil {
		t.Fatalf("expected referenced repository Secret to remain: %v", err)
	}
}

func TestCreateProjectRepositoriesUseHashSuffixForLongURLCollisions(t *testing.T) {
	server := testServer(t)
	longPrefix := "https://github.com/acme/" + strings.Repeat("very-long-shared-path-segment-", 4)
	urlOne := longPrefix + "one.git"
	urlTwo := longPrefix + "two.git"

	for _, tc := range []struct {
		name       string
		display    string
		repository string
	}{
		{name: "long-one", display: "Long One", repository: urlOne},
		{name: "long-two", display: "Long Two", repository: urlTwo},
	} {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/api/projects/acme-prod/repositories", strings.NewReader(`{
			"name":"`+tc.name+`",
			"displayName":"`+tc.display+`",
			"url":"`+tc.repository+`",
			"authType":"none"
		}`))
		request.Header.Set("X-Servicer-User", "alice@example.com")
		request.Header.Set("X-Servicer-Roles", "tenant-operator")
		server.Handler().ServeHTTP(response, request)
		if response.Code != http.StatusCreated {
			t.Fatalf("expected status 201 for %s, got %d: %s", tc.name, response.Code, response.Body.String())
		}
	}

	nameOne := repositoryMirrorNameForTest(t, CreateRepositoryRequest{Name: "long-one", DisplayName: "Long One", Scope: "project", ProjectName: "acme-prod", URL: urlOne, AuthType: "none"})
	nameTwo := repositoryMirrorNameForTest(t, CreateRepositoryRequest{Name: "long-two", DisplayName: "Long Two", Scope: "project", ProjectName: "acme-prod", URL: urlTwo, AuthType: "none"})
	if nameOne == nameTwo {
		t.Fatalf("expected different Argo Secret names for long URLs, got %q", nameOne)
	}
	if len(nameOne) > 63 || len(nameTwo) > 63 {
		t.Fatalf("expected Kubernetes-safe Secret names, got %q (%d), %q (%d)", nameOne, len(nameOne), nameTwo, len(nameTwo))
	}
	for _, name := range []string{nameOne, nameTwo} {
		var argoSecret corev1.Secret
		if err := server.client.Get(context.Background(), client.ObjectKey{Name: name, Namespace: argocdNamespace}, &argoSecret); err != nil {
			t.Fatalf("expected mirrored Argo CD Secret %q: %v", name, err)
		}
	}
}

func repositoryMirrorNameForTest(t *testing.T, req CreateRepositoryRequest) string {
	t.Helper()
	if err := validateRepositoryRequest(&req); err != nil {
		t.Fatalf("validate repository request: %v", err)
	}
	return argoRepoSecretName(req)
}

func TestSubmitSensitiveActionRequiresApproverRole(t *testing.T) {
	server := testServer(t)
	body := []byte(`{"action":"failover","parameters":{"candidateCluster":"west-2"}}`)
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/instances/session-cache/actions", bytes.NewReader(body))
	request.Header.Set("X-Servicer-User", "alice@example.com")
	request.Header.Set("X-Servicer-Roles", "service-consumer")
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("expected forbidden for sensitive action, got %d: %s", response.Code, response.Body.String())
	}
}

func TestSubmitActionCreatesActionRequest(t *testing.T) {
	server := testServer(t)
	body := []byte(`{"action":"restart","reason":"smoke test"}`)
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/instances/session-cache/actions", bytes.NewReader(body))
	request.Header.Set("X-Servicer-User", "alice@example.com")
	request.Header.Set("X-Servicer-Roles", "service-consumer")

	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d: %s", response.Code, response.Body.String())
	}
}

func TestApproveActionUpdatesPendingApprovalRequest(t *testing.T) {
	server := testServer(t)
	body := []byte(`{"decision":"approve","reason":"looks good"}`)
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/actions/session-cache-failover/approval", bytes.NewReader(body))
	request.Header.Set("X-Servicer-User", "trent@example.com")
	request.Header.Set("X-Servicer-Roles", "platform-admin")

	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", response.Code, response.Body.String())
	}

	var action platformv1alpha1.ActionRequest
	if err := server.client.Get(request.Context(), client.ObjectKey{Name: "session-cache-failover"}, &action); err != nil {
		t.Fatalf("get action request: %v", err)
	}
	if action.Spec.Approval.Mode != platformv1alpha1.ApprovalModeApproved {
		t.Fatalf("expected action approval mode approved, got %q", action.Spec.Approval.Mode)
	}
	if len(action.Spec.Approval.ApprovedBy) != 1 || action.Spec.Approval.ApprovedBy[0] != "trent@example.com" {
		t.Fatalf("expected approver to be recorded, got %#v", action.Spec.Approval.ApprovedBy)
	}
}

func TestApproveActionRejectsSelfApproval(t *testing.T) {
	server := testServer(t)
	body := []byte(`{"decision":"approve"}`)
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/actions/session-cache-failover/approval", bytes.NewReader(body))
	request.Header.Set("X-Servicer-User", "alice@example.com")
	request.Header.Set("X-Servicer-Roles", "tenant-operator")

	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf("expected status 403, got %d: %s", response.Code, response.Body.String())
	}
}

func TestApproveActionRequiresPendingPhaseAndRequiredMode(t *testing.T) {
	server := testServer(t)

	invalidMode := testPendingApprovalAction()
	invalidMode.Name = "session-cache-failover-invalid-mode"
	invalidMode.Spec.Approval.Mode = platformv1alpha1.ApprovalModeAuto
	if err := server.client.Create(context.Background(), invalidMode); err != nil {
		t.Fatalf("create invalid-mode action: %v", err)
	}

	invalidPhase := testPendingApprovalAction()
	invalidPhase.Name = "session-cache-failover-invalid-phase"
	invalidPhase.Status.Phase = "Running"
	if err := server.client.Create(context.Background(), invalidPhase); err != nil {
		t.Fatalf("create invalid-phase action: %v", err)
	}

	for _, name := range []string{"session-cache-failover-invalid-mode", "session-cache-failover-invalid-phase"} {
		body := []byte(`{"decision":"approve"}`)
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/api/actions/"+name+"/approval", bytes.NewReader(body))
		request.Header.Set("X-Servicer-User", "trent@example.com")
		request.Header.Set("X-Servicer-Roles", "platform-admin")

		server.Handler().ServeHTTP(response, request)
		if response.Code != http.StatusConflict {
			t.Fatalf("expected status 409 for %s, got %d: %s", name, response.Code, response.Body.String())
		}
	}
}

func TestApproveActionRejectsSelfApprovalByImmutableIdentity(t *testing.T) {
	server := testServer(t)
	action := testPendingApprovalAction()
	action.Name = "session-cache-failover-immutable-self"
	action.Spec.RequestedBy.Subject = "corp-oidc:alice-subject"
	if err := server.client.Create(context.Background(), action); err != nil {
		t.Fatalf("create action: %v", err)
	}

	body := []byte(`{"decision":"approve"}`)
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/actions/session-cache-failover-immutable-self/approval", bytes.NewReader(body))
	request.SetPathValue("name", "session-cache-failover-immutable-self")
	request = withActor(request, actor{
		Name:          "alice@example.com",
		Provider:      "corp-oidc",
		Subject:       "alice-subject",
		Authenticated: true,
		Roles:         map[string]struct{}{roleTenantOperator: {}},
		TenantRoles:   map[string]map[string]struct{}{"acme": {roleTenantOperator: {}}},
		Groups:        map[string]struct{}{},
	})

	server.handleActionApproval(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("expected status 403, got %d: %s", response.Code, response.Body.String())
	}
}

func TestAuditEndpointReturnsActionsAndEvents(t *testing.T) {
	server := testServer(t)
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/audit?q=restart", nil)
	request.Header.Set("X-Servicer-User", "alice@example.com")
	request.Header.Set("X-Servicer-Roles", "service-consumer")
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", response.Code, response.Body.String())
	}
	var events []AuditEventSummary
	if err := json.Unmarshal(response.Body.Bytes(), &events); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(events) == 0 || events[0].Action != "restart" {
		t.Fatalf("expected restart action audit event, got %#v", events)
	}
}

func TestAuditEndpointAllowsAuditorRole(t *testing.T) {
	server := testServer(t)
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/audit", nil)
	request.Header.Set("X-Servicer-User", "audit@example.com")
	request.Header.Set("X-Servicer-Roles", "auditor")

	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected auditor status 200, got %d: %s", response.Code, response.Body.String())
	}
}

func TestMutatingEndpointWritesAuditEvent(t *testing.T) {
	server := testServer(t)
	body := strings.NewReader(`{"name":"newco","displayName":"New Co","allowedServiceClasses":["namespace"],"owners":["owner@example.com"]}`)
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/admin/tenants", body)
	request.Header.Set("X-Servicer-User", "admin@example.com")
	request.Header.Set("X-Servicer-Roles", "platform-admin")

	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("expected create status 201, got %d: %s", response.Code, response.Body.String())
	}
	var stored corev1.ConfigMapList
	if err := server.client.List(request.Context(), &stored, client.InNamespace(defaultAuditNamespace), client.MatchingLabels{auditEventLabelKey: auditEventLabelValue}); err != nil {
		t.Fatalf("list stored audit events: %v", err)
	}
	for _, configMap := range stored.Items {
		payload := configMap.Data["event.json"]
		if strings.Contains(payload, `"type":"BFFRequest"`) && strings.Contains(payload, `/api/admin/tenants`) {
			return
		}
	}
	t.Fatalf("expected mutating request audit event, got %#v", stored.Items)
}

func TestAuditEndpointSupportsStructuredFilters(t *testing.T) {
	server := testServer(t)
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/audit?type=ActionRequest&actor=alice@example.com&action=restart&phase=Succeeded&limit=1", nil)
	request.Header.Set("X-Servicer-User", "alice@example.com")
	request.Header.Set("X-Servicer-Roles", "service-consumer")

	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", response.Code, response.Body.String())
	}
	var events []AuditEventSummary
	if err := json.Unmarshal(response.Body.Bytes(), &events); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected one filtered event, got %#v", events)
	}
	if events[0].Type != "ActionRequest" || events[0].Action != "restart" || events[0].Actor != "alice@example.com" {
		t.Fatalf("unexpected filtered event %#v", events[0])
	}
}

func TestAuditEndpointRetainsEventsInConfigMaps(t *testing.T) {
	server := testServer(t)
	first := httptest.NewRecorder()
	firstRequest := httptest.NewRequest(http.MethodGet, "/api/audit?q=restart", nil)
	firstRequest.Header.Set("X-Servicer-User", "alice@example.com")
	firstRequest.Header.Set("X-Servicer-Roles", "service-consumer")
	server.Handler().ServeHTTP(first, firstRequest)
	if first.Code != http.StatusOK {
		t.Fatalf("expected initial audit status 200, got %d: %s", first.Code, first.Body.String())
	}

	var stored corev1.ConfigMapList
	if err := server.client.List(firstRequest.Context(), &stored, client.InNamespace(defaultAuditNamespace), client.MatchingLabels{auditEventLabelKey: auditEventLabelValue}); err != nil {
		t.Fatalf("list stored audit events: %v", err)
	}
	if len(stored.Items) == 0 {
		t.Fatalf("expected audit events to be persisted")
	}

	var action platformv1alpha1.ActionRequest
	if err := server.client.Get(firstRequest.Context(), client.ObjectKey{Name: "session-cache-restart"}, &action); err != nil {
		t.Fatalf("get source action: %v", err)
	}
	if err := server.client.Delete(firstRequest.Context(), &action); err != nil {
		t.Fatalf("delete source action: %v", err)
	}

	second := httptest.NewRecorder()
	secondRequest := httptest.NewRequest(http.MethodGet, "/api/audit?q=restart&type=ActionRequest", nil)
	secondRequest.Header.Set("X-Servicer-User", "alice@example.com")
	secondRequest.Header.Set("X-Servicer-Roles", "service-consumer")
	server.Handler().ServeHTTP(second, secondRequest)
	if second.Code != http.StatusOK {
		t.Fatalf("expected retained audit status 200, got %d: %s", second.Code, second.Body.String())
	}
	var events []AuditEventSummary
	if err := json.Unmarshal(second.Body.Bytes(), &events); err != nil {
		t.Fatalf("decode retained events: %v", err)
	}
	if len(events) == 0 || events[0].Subject != "session-cache-restart" {
		t.Fatalf("expected retained restart action event, got %#v", events)
	}
}

func TestInstanceDetailRejectsUnauthorizedTenantAccess(t *testing.T) {
	server := testServer(t)
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/instances/session-cache", nil)
	request.Header.Set("X-Servicer-User", "mallory@example.com")
	request.Header.Set("X-Servicer-Roles", "service-consumer")

	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf("expected status 403, got %d: %s", response.Code, response.Body.String())
	}
}

func TestInstancesListFiltersToAuthorizedTenancy(t *testing.T) {
	server := testServer(t)
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/instances", nil)
	request.Header.Set("X-Servicer-User", "mallory@example.com")
	request.Header.Set("X-Servicer-Roles", "service-consumer")

	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", response.Code, response.Body.String())
	}
	var instances []InstanceSummary
	if err := json.Unmarshal(response.Body.Bytes(), &instances); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(instances) != 1 || instances[0].Name != "rogue-cache" {
		t.Fatalf("expected only rogue tenant instance, got %#v", instances)
	}
}

func TestMetricsEndpointExposesPrometheusMetrics(t *testing.T) {
	server := testServer(t)
	apiResponse := httptest.NewRecorder()
	apiRequest := httptest.NewRequest(http.MethodGet, "/api/healthz", nil)
	server.Handler().ServeHTTP(apiResponse, apiRequest)

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/metrics", nil)

	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	if !strings.Contains(body, "servicer_bff_http_requests_total") {
		t.Fatalf("expected http request metric, got %s", body)
	}
	if !strings.Contains(body, "servicer_bff_authentication_failures_total") {
		t.Fatalf("expected auth failure metric, got %s", body)
	}
}

func TestProductionModeRequiresStrongSessionSecret(t *testing.T) {
	t.Setenv("SERVICER_PRODUCTION", "true")
	t.Setenv("SERVICER_SESSION_SECRET", "")
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme returned error: %v", err)
	}
	if err := platformv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme returned error: %v", err)
	}
	kubeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	if _, err := newAuthRuntime(kubeClient); err == nil {
		t.Fatalf("expected production auth runtime to require SERVICER_SESSION_SECRET")
	}
}

func TestNewServerWithConfigReturnsAuthInitError(t *testing.T) {
	t.Setenv("SERVICER_PRODUCTION", "true")
	t.Setenv("SERVICER_SESSION_SECRET", "")
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme returned error: %v", err)
	}
	if err := platformv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme returned error: %v", err)
	}
	kubeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	server, err := NewServerWithConfig(kubeClient, nil)
	if err == nil {
		t.Fatalf("expected server initialization error")
	}
	if server != nil {
		t.Fatalf("expected nil server on auth runtime init failure")
	}
}

func TestWriteErrorReturnsStablePublicMessageAndCode(t *testing.T) {
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/overview", nil)
	writeError(response, request, errors.New("kube secret servicer-system/client-secret not found"))
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d", response.Code)
	}
	var payload map[string]string
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if payload["code"] != "internal_error" {
		t.Fatalf("expected stable error code, got %#v", payload)
	}
	if payload["error"] != "internal server error" {
		t.Fatalf("expected stable public message, got %#v", payload)
	}
	body := response.Body.String()
	if strings.Contains(body, "client-secret") || strings.Contains(body, "servicer-system") {
		t.Fatalf("expected internal error details to be hidden, got %s", body)
	}
}

func TestForwardedHTTPSRequiresTrustedProxyHeaders(t *testing.T) {
	t.Setenv("SERVICER_TRUSTED_PROXY_HEADERS", "")
	request := httptest.NewRequest(http.MethodGet, "/api/auth/session", nil)
	request.Header.Set("X-Forwarded-Proto", "https")
	if requestIsSecure(request) {
		t.Fatalf("expected forwarded proto to be ignored without trusted proxy setting")
	}

	t.Setenv("SERVICER_TRUSTED_PROXY_HEADERS", "true")
	if !requestIsSecure(request) {
		t.Fatalf("expected trusted forwarded proto to mark request secure")
	}
}

func TestMutatingBrowserRequestRequiresCSRFToken(t *testing.T) {
	server := testServer(t)
	request := httptest.NewRequest(http.MethodPost, "/api/namespaceclaims", strings.NewReader(`{}`))
	request.Header.Set("X-Servicer-User", "alice@example.com")
	request.Header.Set("X-Servicer-Roles", "service-consumer")
	request.AddCookie(&http.Cookie{Name: authSessionCookieName, Value: "session"})
	response := httptest.NewRecorder()

	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf("expected status 403, got %d: %s", response.Code, response.Body.String())
	}
}

func TestMutatingBrowserRequestAcceptsCSRFToken(t *testing.T) {
	server := testServer(t)
	request := httptest.NewRequest(http.MethodPost, "/api/namespaceclaims", strings.NewReader(`{}`))
	request.Header.Set("X-Servicer-User", "alice@example.com")
	request.Header.Set("X-Servicer-Roles", "service-consumer")
	request.Header.Set("X-CSRF-Token", "csrf-token")
	request.AddCookie(&http.Cookie{Name: authSessionCookieName, Value: "session"})
	request.AddCookie(&http.Cookie{Name: csrfCookieName, Value: "csrf-token"})
	response := httptest.NewRecorder()

	server.Handler().ServeHTTP(response, request)

	if response.Code == http.StatusForbidden {
		t.Fatalf("expected CSRF to pass, got %d: %s", response.Code, response.Body.String())
	}
}

func TestNamespaceClaimsEndpointReturnsVisibleClaims(t *testing.T) {
	server := testServer(t)
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/namespaceclaims", nil)
	request.Header.Set("X-Servicer-User", "alice@example.com")
	request.Header.Set("X-Servicer-Roles", "service-consumer")

	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", response.Code, response.Body.String())
	}
	var claims []NamespaceClaimSummary
	if err := json.Unmarshal(response.Body.Bytes(), &claims); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(claims) != 2 || claims[0].Name != "team-space" || claims[1].Name != "team-space-claim" {
		t.Fatalf("unexpected claims %#v", claims)
	}
}

func TestNamespaceClaimDetailReturnsSpecAndStatus(t *testing.T) {
	server := testServer(t)
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/namespaceclaims/team-space-claim", nil)
	request.Header.Set("X-Servicer-User", "alice@example.com")
	request.Header.Set("X-Servicer-Roles", "service-consumer")

	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", response.Code, response.Body.String())
	}
	var claim NamespaceClaimDetail
	if err := json.Unmarshal(response.Body.Bytes(), &claim); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if claim.Name != "team-space-claim" || claim.ProjectName != "acme-prod" {
		t.Fatalf("unexpected detail %#v", claim)
	}
	if claim.Namespace != "acme-prod-team-space" || claim.Quotas["limits.memory"] != "8Gi" {
		t.Fatalf("expected quota and namespace detail, got %#v", claim)
	}
}

func TestNamespaceClaimDetailFallsBackToNamespaceInstance(t *testing.T) {
	server := testServer(t)
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/namespaceclaims/team-space", nil)
	request.Header.Set("X-Servicer-User", "alice@example.com")
	request.Header.Set("X-Servicer-Roles", "service-consumer")

	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", response.Code, response.Body.String())
	}
	var claim NamespaceClaimDetail
	if err := json.Unmarshal(response.Body.Bytes(), &claim); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if claim.Name != "team-space" || claim.ProjectName != "acme-prod" {
		t.Fatalf("unexpected detail %#v", claim)
	}
	if claim.Quotas["requests.cpu"] != "2" || claim.Quotas["requests.memory"] != "8Gi" || claim.Quotas["pods"] != "40" {
		t.Fatalf("expected namespace instance quotas, got %#v", claim.Quotas)
	}
	if claim.Labels["owner"] != "platform" {
		t.Fatalf("expected namespace instance labels, got %#v", claim.Labels)
	}
}

func TestCreateNamespaceClaimCreatesClaim(t *testing.T) {
	server := testServer(t)
	body := strings.NewReader(`{
		"name":"analytics-space",
		"projectName":"acme-prod",
		"displayName":"Analytics",
		"deletionPolicy":"orphan",
		"quotas":{"requests.cpu":"2","limits.memory":"4Gi"},
		"labels":{"team":"analytics"}
	}`)
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/namespaceclaims", body)
	request.Header.Set("X-Servicer-User", "alice@example.com")
	request.Header.Set("X-Servicer-Roles", "service-consumer")

	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d: %s", response.Code, response.Body.String())
	}
	var claim platformv1alpha1.NamespaceClaim
	if err := server.client.Get(request.Context(), client.ObjectKey{Name: "analytics-space"}, &claim); err != nil {
		t.Fatalf("get created claim: %v", err)
	}
	if claim.Spec.DisplayName != "Analytics" || claim.Spec.DeletionPolicy != platformv1alpha1.DeletionPolicyOrphan {
		t.Fatalf("unexpected created claim spec %#v", claim.Spec)
	}
}

func TestUpdateNamespaceClaimUpdatesSpec(t *testing.T) {
	server := testServer(t)
	body := strings.NewReader(`{
		"name":"team-space-claim",
		"projectName":"acme-prod",
		"displayName":"Team Space",
		"deletionPolicy":"snapshot",
		"quotas":{"limits.memory":"16Gi"},
		"labels":{"owner":"platform"}
	}`)
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPut, "/api/namespaceclaims/team-space-claim", body)
	request.Header.Set("X-Servicer-User", "alice@example.com")
	request.Header.Set("X-Servicer-Roles", "tenant-operator")

	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", response.Code, response.Body.String())
	}
	var claim platformv1alpha1.NamespaceClaim
	if err := server.client.Get(request.Context(), client.ObjectKey{Name: "team-space-claim"}, &claim); err != nil {
		t.Fatalf("get updated claim: %v", err)
	}
	if claim.Spec.Quotas["limits.memory"] != "16Gi" || claim.Spec.Labels["owner"] != "platform" {
		t.Fatalf("expected updated claim spec, got %#v", claim.Spec)
	}
}

func TestUpdateNamespaceClaimUpdatesNamespaceInstanceWhenNoClaimExists(t *testing.T) {
	server := testServer(t)
	body := strings.NewReader(`{
		"name":"team-space",
		"projectName":"acme-prod",
		"displayName":"Team Space",
		"deletionPolicy":"orphan",
		"quotas":{"requests.cpu":"3","requests.memory":"12Gi","pods":"60"},
		"labels":{"owner":"shared-services"}
	}`)
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPut, "/api/namespaceclaims/team-space", body)
	request.Header.Set("X-Servicer-User", "alice@example.com")
	request.Header.Set("X-Servicer-Roles", "tenant-operator")

	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", response.Code, response.Body.String())
	}
	var instance platformv1alpha1.ServiceInstance
	if err := server.client.Get(request.Context(), client.ObjectKey{Name: "team-space"}, &instance); err != nil {
		t.Fatalf("get updated namespace instance: %v", err)
	}
	params := parametersMap(instance)
	if params["cpu"] != "3" || params["memory"] != "12Gi" || params["pods"] != "60" {
		t.Fatalf("expected updated namespace instance params, got %#v", params)
	}
	labels, ok := params["labels"].(map[string]any)
	if !ok || labels["owner"] != "shared-services" {
		t.Fatalf("expected updated labels, got %#v", params["labels"])
	}
	if instance.Spec.DeletionPolicy != platformv1alpha1.DeletionPolicyOrphan {
		t.Fatalf("expected orphan deletion policy, got %q", instance.Spec.DeletionPolicy)
	}
}

func TestDeleteNamespaceClaimRequiresOperatorRole(t *testing.T) {
	server := testServer(t)
	forbidden := httptest.NewRecorder()
	forbiddenRequest := httptest.NewRequest(http.MethodDelete, "/api/namespaceclaims/team-space-claim", nil)
	forbiddenRequest.Header.Set("X-Servicer-User", "alice@example.com")
	forbiddenRequest.Header.Set("X-Servicer-Roles", "service-consumer")
	server.Handler().ServeHTTP(forbidden, forbiddenRequest)
	if forbidden.Code != http.StatusForbidden {
		t.Fatalf("expected status 403, got %d: %s", forbidden.Code, forbidden.Body.String())
	}

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodDelete, "/api/namespaceclaims/team-space-claim", nil)
	request.Header.Set("X-Servicer-User", "alice@example.com")
	request.Header.Set("X-Servicer-Roles", "tenant-operator")
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", response.Code, response.Body.String())
	}
}

func TestDeleteNamespaceClaimDeletesNamespaceInstanceWhenNoClaimExists(t *testing.T) {
	server := testServer(t)
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodDelete, "/api/namespaceclaims/team-space", nil)
	request.Header.Set("X-Servicer-User", "alice@example.com")
	request.Header.Set("X-Servicer-Roles", "tenant-operator")

	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", response.Code, response.Body.String())
	}
	var instance platformv1alpha1.ServiceInstance
	if err := server.client.Get(request.Context(), client.ObjectKey{Name: "team-space"}, &instance); err == nil {
		t.Fatalf("expected namespace instance to be deleted")
	}
}

func TestServiceBindingsEndpointReturnsVisibleBindings(t *testing.T) {
	server := testServer(t)
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/servicebindings", nil)
	request.Header.Set("X-Servicer-User", "alice@example.com")
	request.Header.Set("X-Servicer-Roles", "service-consumer")

	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", response.Code, response.Body.String())
	}
	var bindings []ServiceBindingSummary
	if err := json.Unmarshal(response.Body.Bytes(), &bindings); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(bindings) != 1 || bindings[0].Name != "orders-api" {
		t.Fatalf("unexpected bindings %#v", bindings)
	}
}

func TestCredentialDetailRejectsUnauthorizedTenantAccess(t *testing.T) {
	server := testServer(t)
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/instances/session-cache/credentials/acme-prod-session-cache/session-cache-auth", nil)
	request.Header.Set("X-Servicer-User", "mallory@example.com")
	request.Header.Set("X-Servicer-Roles", "service-consumer")

	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf("expected status 403, got %d: %s", response.Code, response.Body.String())
	}
}

func TestCreateProductRequestRejectsUnauthorizedProject(t *testing.T) {
	server := testServer(t)
	body := []byte(`{"name":"team-space-new","projectName":"acme-prod","serviceClass":"namespace","servicePlan":"namespace-team"}`)
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/requests", bytes.NewReader(body))
	request.Header.Set("X-Servicer-User", "mallory@example.com")
	request.Header.Set("X-Servicer-Roles", "service-consumer")

	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d: %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "outside your authorized tenancy") {
		t.Fatalf("expected tenancy authorization error, got %s", response.Body.String())
	}
}

func TestDownloadNamespaceKubeconfigReturnsCompletedGrant(t *testing.T) {
	server := testServer(t)
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/instances/team-space/actions/team-space-access/kubeconfig", nil)
	request.Header.Set("X-Servicer-User", "bob@example.com")
	request.Header.Set("X-Servicer-Roles", "service-consumer")

	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", response.Code, response.Body.String())
	}
	if contentType := response.Header().Get("Content-Type"); contentType != "application/x-yaml" {
		t.Fatalf("expected kubeconfig content type, got %q", contentType)
	}
	if !strings.Contains(response.Body.String(), "server: https://servicer.example.com/api/kubernetes/namespaces/acme-prod-team-space") {
		t.Fatalf("expected namespace proxy kubeconfig, got:\n%s", response.Body.String())
	}
}

func TestCredentialDetailReturnsPublishedSecretData(t *testing.T) {
	server := testServer(t)
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/instances/session-cache/credentials/acme-prod-session-cache/session-cache-auth", nil)
	request.Header.Set("X-Servicer-User", "alice@example.com")
	request.Header.Set("X-Servicer-Roles", "service-consumer")

	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", response.Code, response.Body.String())
	}
	var detail CredentialDetail
	if err := json.Unmarshal(response.Body.Bytes(), &detail); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if detail.Name != "session-cache-auth" || detail.Namespace != "acme-prod-session-cache" {
		t.Fatalf("unexpected credential identity: %#v", detail)
	}
	if detail.Data["password"] != "super-secret-password" {
		t.Fatalf("expected decoded password, got %#v", detail.Data)
	}
}

func TestCredentialDetailFallsBackToSourceSecretWhenProjectedMissing(t *testing.T) {
	server := testServer(t)

	var instance platformv1alpha1.ServiceInstance
	if err := server.client.Get(context.Background(), client.ObjectKey{Name: "session-cache"}, &instance); err != nil {
		t.Fatalf("get instance: %v", err)
	}
	instance.Status.CredentialRefs = []platformv1alpha1.NamespacedObjectReference{
		{Name: "session-cache-auth-projected", Namespace: "acme-prod-session-cache"},
	}
	if err := server.client.Update(context.Background(), &instance); err != nil {
		t.Fatalf("update instance: %v", err)
	}

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/instances/session-cache/credentials/acme-prod-session-cache/session-cache-auth-projected", nil)
	request.Header.Set("X-Servicer-User", "alice@example.com")
	request.Header.Set("X-Servicer-Roles", "service-consumer")

	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", response.Code, response.Body.String())
	}
	var detail CredentialDetail
	if err := json.Unmarshal(response.Body.Bytes(), &detail); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if detail.Name != "session-cache-auth" || detail.Namespace != "acme-prod-session-cache" {
		t.Fatalf("unexpected credential identity: %#v", detail)
	}
	if detail.Data["password"] != "super-secret-password" {
		t.Fatalf("expected decoded password, got %#v", detail.Data)
	}
}

func TestCredentialDetailRejectsUnpublishedSecret(t *testing.T) {
	server := testServer(t)
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/instances/session-cache/credentials/acme-prod-team-space/servicer-access-bob-example-com-kubeconfig", nil)
	request.Header.Set("X-Servicer-User", "alice@example.com")
	request.Header.Set("X-Servicer-Roles", "service-consumer")

	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d: %s", response.Code, response.Body.String())
	}
}

func TestNamespaceProxyForwardsGrantedReadOnlyRequest(t *testing.T) {
	var upstreamPath string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"kind":"PodList","items":[]}`))
	}))
	defer upstream.Close()

	server := testServerWithConfig(t, &rest.Config{Host: upstream.URL})
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/kubernetes/namespaces/acme-prod-team-space/api/v1/namespaces/acme-prod-team-space/pods", nil)
	request.Header.Set("Authorization", "Bearer test-access-token")

	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", response.Code, response.Body.String())
	}
	if upstreamPath != "/api/v1/namespaces/acme-prod-team-space/pods" {
		t.Fatalf("expected upstream namespaced pod path, got %q", upstreamPath)
	}
}

func TestKubernetesRootProxySupportsKubectlDiscovery(t *testing.T) {
	var upstreamPath string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"kind":"APIVersions","versions":["v1"]}`))
	}))
	defer upstream.Close()

	server := testServerWithConfig(t, &rest.Config{Host: upstream.URL})
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api", nil)
	request.Header.Set("Authorization", "Bearer test-access-token")
	request.Header.Set("User-Agent", "kubectl/v1.29")

	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", response.Code, response.Body.String())
	}
	if upstreamPath != "/api" {
		t.Fatalf("expected upstream discovery path, got %q", upstreamPath)
	}
}

func TestKubernetesRootProxyRejectsDifferentNamespace(t *testing.T) {
	server := testServerWithConfig(t, &rest.Config{Host: "https://example.invalid"})
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/namespaces/other/pods", nil)
	request.Header.Set("Authorization", "Bearer test-access-token")

	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf("expected status 403, got %d: %s", response.Code, response.Body.String())
	}
}

func testServer(t *testing.T) *Server {
	return testServerWithConfig(t, nil)
}

func testServerWithConfig(t *testing.T, restConfig *rest.Config) *Server {
	t.Helper()
	for _, key := range []string{
		"SERVICER_AUTH_MODE",
		"SERVICER_AUTH_ALLOW_DEMO_HEADERS",
		"SERVICER_OIDC_ISSUER_URL",
		"SERVICER_OIDC_CLIENT_ID",
		"SERVICER_OIDC_USERNAME_CLAIM",
		"SERVICER_OIDC_ROLES_CLAIM",
		"SERVICER_OIDC_GROUPS_CLAIM",
		"SERVICER_OIDC_SCOPES",
		"SERVICER_OIDC_REDIRECT_PATH",
		"SERVICER_OIDC_END_SESSION_URL",
		"SERVICER_SESSION_SECRET",
		"SERVICER_PRODUCTION",
		"SERVICER_TRUSTED_PROXY_HEADERS",
		"SERVICER_AUTH_EXTERNAL_BASE_URL",
		"SERVICER_LOGIN_RATE_LIMIT_BACKEND",
		"SERVICER_LOGIN_RATE_LIMIT_ACCEPT_IN_MEMORY",
		"SERVICER_AUDIT_STDOUT",
	} {
		_ = os.Unsetenv(key)
	}
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme returned error: %v", err)
	}
	if err := platformv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme returned error: %v", err)
	}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(
			testTenant(),
			testOtherTenant(),
			testProject(),
			testOtherProject(),
			testNamespaceClass(),
			testNamespacePlan(),
			testValkeyClass(),
			testValkeyPlan(),
			testValkeyReplicatedPlan(),
			testYugabyteClass(),
			testYugabytePlan(),
			testVirtualMachineClass(),
			testVirtualMachinePlan(),
			testCassandraClass(),
			testInstance(),
			testOtherTenantInstance(),
			testNamespaceInstance(),
			testNamespaceClaim(),
			testServiceBinding(),
			testBlockedYugabyteInstance(),
			testAction(),
			testPendingApprovalAction(),
			testGrantAccessAction(),
			testGrantAccessSecret(),
			testValkeyCredentialSecret(),
		).
		Build()
	server, err := NewServerWithConfig(client, restConfig)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	server.auth.allowTestHeaders = true
	return server
}

func testTenant() *platformv1alpha1.Tenant {
	return &platformv1alpha1.Tenant{
		ObjectMeta: metav1.ObjectMeta{Name: "acme"},
		Spec: platformv1alpha1.TenantSpec{
			DisplayName:           "Acme",
			Owners:                platformv1alpha1.OwnersSpec{Users: []string{"alice@example.com"}},
			QuotaProfileRef:       platformv1alpha1.LocalObjectReference{Name: "standard"},
			AllowedServiceClasses: []string{"namespace", "valkey", "virtual-machine"},
			Lifecycle:             platformv1alpha1.TenantLifecycleSpec{Phase: platformv1alpha1.TenantLifecyclePhaseActive},
		},
		Status: platformv1alpha1.TenantStatus{Phase: "Ready"},
	}
}

func testProject() *platformv1alpha1.Project {
	return &platformv1alpha1.Project{
		ObjectMeta: metav1.ObjectMeta{Name: "acme-prod"},
		Spec: platformv1alpha1.ProjectSpec{
			TenantRef:         platformv1alpha1.LocalObjectReference{Name: "acme"},
			DisplayName:       "Acme Production",
			Environment:       platformv1alpha1.EnvironmentProduction,
			NamespaceStrategy: platformv1alpha1.NamespaceStrategySpec{Mode: platformv1alpha1.NamespaceStrategyDedicated, Prefix: "acme-prod"},
		},
		Status: platformv1alpha1.ProjectStatus{Phase: "Ready", Placement: platformv1alpha1.PlacementStatus{ClusterName: "east-1"}},
	}
}

func testOtherTenant() *platformv1alpha1.Tenant {
	return &platformv1alpha1.Tenant{
		ObjectMeta: metav1.ObjectMeta{Name: "rogue"},
		Spec: platformv1alpha1.TenantSpec{
			DisplayName:           "Rogue",
			Owners:                platformv1alpha1.OwnersSpec{Users: []string{"mallory@example.com"}},
			QuotaProfileRef:       platformv1alpha1.LocalObjectReference{Name: "standard"},
			AllowedServiceClasses: []string{"valkey"},
			Lifecycle:             platformv1alpha1.TenantLifecycleSpec{Phase: platformv1alpha1.TenantLifecyclePhaseActive},
		},
		Status: platformv1alpha1.TenantStatus{Phase: "Ready"},
	}
}

func testNamespaceClass() *platformv1alpha1.ServiceClass {
	return &platformv1alpha1.ServiceClass{
		ObjectMeta: metav1.ObjectMeta{Name: "namespace"},
		Spec:       platformv1alpha1.ServiceClassSpec{DisplayName: "Kubernetes Namespace", Category: "platform", Driver: "kubernetes-namespace", Published: true},
		Status:     platformv1alpha1.ServiceClassStatus{Published: true, Phase: "Ready"},
	}
}

func testNamespacePlan() *platformv1alpha1.ServicePlan {
	return &platformv1alpha1.ServicePlan{
		ObjectMeta: metav1.ObjectMeta{Name: "namespace-team"},
		Spec:       platformv1alpha1.ServicePlanSpec{ServiceClassRef: platformv1alpha1.LocalObjectReference{Name: "namespace"}, DisplayName: "Team Namespace", Topology: "dedicated"},
		Status:     platformv1alpha1.ServicePlanStatus{Published: true, Phase: "Ready"},
	}
}

func testOtherProject() *platformv1alpha1.Project {
	return &platformv1alpha1.Project{
		ObjectMeta: metav1.ObjectMeta{Name: "rogue-prod"},
		Spec: platformv1alpha1.ProjectSpec{
			TenantRef:         platformv1alpha1.LocalObjectReference{Name: "rogue"},
			DisplayName:       "Rogue Production",
			Environment:       platformv1alpha1.EnvironmentProduction,
			NamespaceStrategy: platformv1alpha1.NamespaceStrategySpec{Mode: platformv1alpha1.NamespaceStrategyDedicated, Prefix: "rogue-prod"},
		},
		Status: platformv1alpha1.ProjectStatus{Phase: "Ready", Placement: platformv1alpha1.PlacementStatus{ClusterName: "west-1"}},
	}
}

func testValkeyClass() *platformv1alpha1.ServiceClass {
	return &platformv1alpha1.ServiceClass{
		ObjectMeta: metav1.ObjectMeta{Name: "valkey"},
		Spec:       platformv1alpha1.ServiceClassSpec{DisplayName: "Valkey", Category: "cache", Driver: "servicer-valkey", Published: true},
		Status:     platformv1alpha1.ServiceClassStatus{Published: true, Phase: "Ready"},
	}
}

func testValkeyPlan() *platformv1alpha1.ServicePlan {
	raw := []byte(`{"memoryProfile":"small"}`)
	return &platformv1alpha1.ServicePlan{
		ObjectMeta: metav1.ObjectMeta{Name: "valkey-dev"},
		Spec: platformv1alpha1.ServicePlanSpec{
			ServiceClassRef:   platformv1alpha1.LocalObjectReference{Name: "valkey"},
			DisplayName:       "Development",
			Topology:          "single-node",
			DefaultParameters: &apiextensionsv1.JSON{Raw: raw},
		},
		Status: platformv1alpha1.ServicePlanStatus{Published: true, Phase: "Ready"},
	}
}

func testValkeyReplicatedPlan() *platformv1alpha1.ServicePlan {
	raw := []byte(`{"memoryProfile":"medium","replicas":3}`)
	return &platformv1alpha1.ServicePlan{
		ObjectMeta: metav1.ObjectMeta{Name: "valkey-replicated"},
		Spec: platformv1alpha1.ServicePlanSpec{
			ServiceClassRef:   platformv1alpha1.LocalObjectReference{Name: "valkey"},
			DisplayName:       "Replicated",
			Topology:          "replicated",
			DefaultParameters: &apiextensionsv1.JSON{Raw: raw},
		},
		Status: platformv1alpha1.ServicePlanStatus{Published: true, Phase: "Ready"},
	}
}

func testYugabyteClass() *platformv1alpha1.ServiceClass {
	return &platformv1alpha1.ServiceClass{
		ObjectMeta: metav1.ObjectMeta{Name: "yugabyte"},
		Spec:       platformv1alpha1.ServiceClassSpec{DisplayName: "YugabyteDB", Category: "database", Driver: "yb-operator", Published: true},
		Status:     platformv1alpha1.ServiceClassStatus{Published: true, Phase: "Ready"},
	}
}

func testYugabytePlan() *platformv1alpha1.ServicePlan {
	return &platformv1alpha1.ServicePlan{
		ObjectMeta: metav1.ObjectMeta{Name: "yugabyte-dev"},
		Spec:       platformv1alpha1.ServicePlanSpec{ServiceClassRef: platformv1alpha1.LocalObjectReference{Name: "yugabyte"}, DisplayName: "Development", Topology: "single-cluster"},
		Status:     platformv1alpha1.ServicePlanStatus{Published: true, Phase: "Ready"},
	}
}

func testVirtualMachineClass() *platformv1alpha1.ServiceClass {
	return &platformv1alpha1.ServiceClass{
		ObjectMeta: metav1.ObjectMeta{Name: "virtual-machine"},
		Spec:       platformv1alpha1.ServiceClassSpec{DisplayName: "Virtual Machine", Category: "compute", Driver: "kubevirt", Published: true},
		Status:     platformv1alpha1.ServiceClassStatus{Published: true, Phase: "Ready"},
	}
}

func testVirtualMachinePlan() *platformv1alpha1.ServicePlan {
	return &platformv1alpha1.ServicePlan{
		ObjectMeta: metav1.ObjectMeta{Name: "virtual-machine-dev"},
		Spec:       platformv1alpha1.ServicePlanSpec{ServiceClassRef: platformv1alpha1.LocalObjectReference{Name: "virtual-machine"}, DisplayName: "Development", Topology: "single-cluster"},
		Status:     platformv1alpha1.ServicePlanStatus{Published: true, Phase: "Ready"},
	}
}

func testCassandraClass() *platformv1alpha1.ServiceClass {
	return &platformv1alpha1.ServiceClass{
		ObjectMeta: metav1.ObjectMeta{Name: "cassandra"},
		Spec:       platformv1alpha1.ServiceClassSpec{DisplayName: "Cassandra", Category: "database", Driver: "k8ssandra", Published: true},
		Status:     platformv1alpha1.ServiceClassStatus{Published: true, Phase: "Ready"},
	}
}

func testInstance() *platformv1alpha1.ServiceInstance {
	return &platformv1alpha1.ServiceInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "session-cache"},
		Spec: platformv1alpha1.ServiceInstanceSpec{
			ProjectRef:      platformv1alpha1.LocalObjectReference{Name: "acme-prod"},
			ServiceClassRef: platformv1alpha1.LocalObjectReference{Name: "valkey"},
			ServicePlanRef:  platformv1alpha1.LocalObjectReference{Name: "valkey-dev"},
		},
		Status: platformv1alpha1.ServiceInstanceStatus{
			Phase:     "Ready",
			Placement: platformv1alpha1.PlacementStatus{ClusterName: "east-1", Namespace: "acme-prod-session-cache"},
			Runtime: platformv1alpha1.RuntimeStatus{
				Driver: "servicer-valkey",
				ObjectRef: &platformv1alpha1.TypedObjectReference{
					APIVersion: "apps/v1",
					Kind:       "StatefulSet",
					Name:       "session-cache",
					Namespace:  "acme-prod-session-cache",
				},
			},
			Sync:   platformv1alpha1.DeliverySyncStatus{Phase: "Synced"},
			Health: platformv1alpha1.HealthStatus{Summary: "Cache is ready."},
			CredentialRefs: []platformv1alpha1.NamespacedObjectReference{
				{Name: "session-cache-auth", Namespace: "acme-prod-session-cache"},
			},
			CacheTopology: platformv1alpha1.CacheTopologyStatus{
				Mode:              "single-node",
				PrimaryCluster:    "east-1",
				FailoverReadiness: "Unavailable",
			},
		},
	}
}

func testValkeyCredentialSecret() *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "session-cache-auth",
			Namespace: "acme-prod-session-cache",
		},
		Data: map[string][]byte{
			"username": []byte("servicer"),
			"password": []byte("super-secret-password"),
		},
	}
}

func testNamespaceInstance() *platformv1alpha1.ServiceInstance {
	return &platformv1alpha1.ServiceInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "team-space"},
		Spec: platformv1alpha1.ServiceInstanceSpec{
			ProjectRef:      platformv1alpha1.LocalObjectReference{Name: "acme-prod"},
			ServiceClassRef: platformv1alpha1.LocalObjectReference{Name: "namespace"},
			ServicePlanRef:  platformv1alpha1.LocalObjectReference{Name: "namespace-team"},
			Parameters:      &apiextensionsv1.JSON{Raw: []byte(`{"cpu":"2","memory":"8Gi","pods":"40","labels":{"owner":"platform"}}`)},
			DeletionPolicy:  platformv1alpha1.DeletionPolicyDelete,
		},
		Status: platformv1alpha1.ServiceInstanceStatus{
			Phase:     "Ready",
			Placement: platformv1alpha1.PlacementStatus{ClusterName: "east-1", Namespace: "acme-prod-team-space"},
			Runtime: platformv1alpha1.RuntimeStatus{
				Driver: "kubernetes-namespace",
				ObjectRef: &platformv1alpha1.TypedObjectReference{
					APIVersion: "v1",
					Kind:       "Namespace",
					Name:       "acme-prod-team-space",
				},
			},
			Artifact: platformv1alpha1.ArtifactStatus{
				Revision: "xyz789",
				Path:     "clusters/east-1/tenants/acme/projects/acme-prod/services/team-space",
				Count:    4,
			},
			Sync:   platformv1alpha1.DeliverySyncStatus{Phase: "Synced", Message: "Namespace product synced."},
			Health: platformv1alpha1.HealthStatus{Summary: "Namespace is ready."},
		},
	}
}

func testOtherTenantInstance() *platformv1alpha1.ServiceInstance {
	return &platformv1alpha1.ServiceInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "rogue-cache"},
		Spec: platformv1alpha1.ServiceInstanceSpec{
			ProjectRef:      platformv1alpha1.LocalObjectReference{Name: "rogue-prod"},
			ServiceClassRef: platformv1alpha1.LocalObjectReference{Name: "valkey"},
			ServicePlanRef:  platformv1alpha1.LocalObjectReference{Name: "valkey-dev"},
		},
		Status: platformv1alpha1.ServiceInstanceStatus{
			Phase:     "Ready",
			Placement: platformv1alpha1.PlacementStatus{ClusterName: "west-1", Namespace: "rogue-prod-rogue-cache"},
			Sync:      platformv1alpha1.DeliverySyncStatus{Phase: "Synced"},
			Health:    platformv1alpha1.HealthStatus{Summary: "Cache is ready."},
		},
	}
}

func testBlockedYugabyteInstance() *platformv1alpha1.ServiceInstance {
	return &platformv1alpha1.ServiceInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "blocked-db"},
		Spec: platformv1alpha1.ServiceInstanceSpec{
			ProjectRef:      platformv1alpha1.LocalObjectReference{Name: "acme-prod"},
			ServiceClassRef: platformv1alpha1.LocalObjectReference{Name: "yugabyte"},
			ServicePlanRef:  platformv1alpha1.LocalObjectReference{Name: "yugabyte-dev"},
		},
		Status: platformv1alpha1.ServiceInstanceStatus{
			Phase:     "Blocked",
			Placement: platformv1alpha1.PlacementStatus{ClusterName: "east-1", Namespace: "acme-prod-blocked-db"},
			Runtime: platformv1alpha1.RuntimeStatus{
				Driver: "yb-operator",
				ObjectRef: &platformv1alpha1.TypedObjectReference{
					APIVersion: "operator.yugabyte.io/v1alpha1",
					Kind:       "YBUniverse",
					Name:       "blocked-db",
					Namespace:  "acme-prod-blocked-db",
				},
			},
			Sync:      platformv1alpha1.DeliverySyncStatus{Phase: "OutOfSync"},
			Health:    platformv1alpha1.HealthStatus{Summary: "YugabyteDB operator CRD is not installed."},
			Endpoints: map[string]string{"ysql": "blocked-db-ysql.acme-prod-blocked-db.svc.cluster.local:5433"},
		},
	}
}

func testAction() *platformv1alpha1.ActionRequest {
	now := metav1.Now()
	return &platformv1alpha1.ActionRequest{
		ObjectMeta: metav1.ObjectMeta{Name: "session-cache-restart", CreationTimestamp: now},
		Spec: platformv1alpha1.ActionRequestSpec{
			TargetRef:   platformv1alpha1.TypedObjectReference{APIVersion: platformv1alpha1.GroupVersion.String(), Kind: "ServiceInstance", Name: "session-cache"},
			Action:      "restart",
			RequestedBy: platformv1alpha1.RequestedBySpec{Subject: "alice@example.com"},
		},
		Status: platformv1alpha1.ActionRequestStatus{Phase: "Succeeded", CompletedAt: &now},
	}
}

func testGrantAccessAction() *platformv1alpha1.ActionRequest {
	now := metav1.Now()
	return &platformv1alpha1.ActionRequest{
		ObjectMeta: metav1.ObjectMeta{Name: "team-space-access", CreationTimestamp: now},
		Spec: platformv1alpha1.ActionRequestSpec{
			TargetRef:   platformv1alpha1.TypedObjectReference{APIVersion: platformv1alpha1.GroupVersion.String(), Kind: "ServiceInstance", Name: "team-space"},
			Action:      "grant-access",
			RequestedBy: platformv1alpha1.RequestedBySpec{Subject: "bob@example.com"},
		},
		Status: platformv1alpha1.ActionRequestStatus{
			Phase:       "Succeeded",
			CompletedAt: &now,
			OperationRef: &platformv1alpha1.TypedObjectReference{
				APIVersion: "v1",
				Kind:       "Secret",
				Name:       "servicer-access-bob-example-com-kubeconfig",
				Namespace:  "acme-prod-team-space",
			},
		},
	}
}

func testPendingApprovalAction() *platformv1alpha1.ActionRequest {
	now := metav1.Now()
	return &platformv1alpha1.ActionRequest{
		ObjectMeta: metav1.ObjectMeta{Name: "session-cache-failover", CreationTimestamp: now},
		Spec: platformv1alpha1.ActionRequestSpec{
			TargetRef:   platformv1alpha1.TypedObjectReference{APIVersion: platformv1alpha1.GroupVersion.String(), Kind: "ServiceInstance", Name: "session-cache"},
			Action:      "failover",
			Approval:    platformv1alpha1.ApprovalSpec{Mode: platformv1alpha1.ApprovalModeRequired},
			RequestedBy: platformv1alpha1.RequestedBySpec{Subject: "alice@example.com"},
		},
		Status: platformv1alpha1.ActionRequestStatus{
			Phase: "PendingApproval",
			Result: platformv1alpha1.ActionResultStatus{
				Code:    "approval-required",
				Message: "Action request is waiting for approval.",
			},
		},
	}
}

func testGrantAccessSecret() *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "servicer-access-bob-example-com-kubeconfig",
			Namespace: "acme-prod-team-space",
			Labels: map[string]string{
				"servicer.io/managed-by": "servicer",
				"servicer.io/purpose":    "namespace-access",
			},
		},
		Data: map[string][]byte{
			"subject":   []byte("bob@example.com"),
			"namespace": []byte("acme-prod-team-space"),
			"token":     []byte("test-access-token"),
			"kubeconfig": []byte(`apiVersion: v1
kind: Config
clusters:
- name: servicer-platform
  cluster:
    server: https://servicer.example.com/api/kubernetes/namespaces/acme-prod-team-space
`),
		},
	}
}

func testNamespaceClaim() *platformv1alpha1.NamespaceClaim {
	return &platformv1alpha1.NamespaceClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "team-space-claim"},
		Spec: platformv1alpha1.NamespaceClaimSpec{
			ProjectRef:     platformv1alpha1.LocalObjectReference{Name: "acme-prod"},
			DisplayName:    "Team Space",
			Quotas:         map[string]string{"limits.memory": "8Gi", "requests.cpu": "1"},
			Labels:         map[string]string{"owner": "app-team"},
			DeletionPolicy: platformv1alpha1.DeletionPolicyDelete,
		},
		Status: platformv1alpha1.NamespaceClaimStatus{
			Phase: "Ready",
			Placement: platformv1alpha1.PlacementStatus{
				ClusterName: "east-1",
				Namespace:   "acme-prod-team-space",
			},
			Artifact: platformv1alpha1.ArtifactStatus{
				Revision: "abc123",
				Path:     "clusters/east-1/acme-prod/team-space",
				Count:    3,
			},
			Sync: platformv1alpha1.DeliverySyncStatus{
				Phase:           "Synced",
				ApplicationName: "team-space-claim",
				Message:         "Namespace package synced.",
			},
			Health: platformv1alpha1.HealthStatus{Summary: "Namespace claim ready."},
			Conditions: []metav1.Condition{{
				Type:    "Accepted",
				Status:  metav1.ConditionTrue,
				Reason:  "BackedByServiceInstance",
				Message: "Namespace claim is reconciled through a backing namespace service instance.",
			}},
		},
	}
}

func testServiceBinding() *platformv1alpha1.ServiceBinding {
	return &platformv1alpha1.ServiceBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "orders-api"},
		Spec: platformv1alpha1.ServiceBindingSpec{
			ProjectRef: platformv1alpha1.LocalObjectReference{Name: "acme-prod"},
			SourceRef: platformv1alpha1.TypedObjectReference{
				APIVersion: platformv1alpha1.GroupVersion.String(),
				Kind:       "ServiceInstance",
				Name:       "session-cache",
			},
			TargetRef: platformv1alpha1.TypedObjectReference{
				APIVersion: "v1",
				Kind:       "Namespace",
				Name:       "orders-api",
				Namespace:  "acme-prod-api",
			},
			SecretPolicy: platformv1alpha1.SecretPolicySpec{DeliveryMode: platformv1alpha1.SecretDeliveryModeDirectSecretRef},
		},
		Status: platformv1alpha1.ServiceBindingStatus{
			Phase: "Ready",
			CredentialRefs: []platformv1alpha1.NamespacedObjectReference{{
				Name:      "orders-api-binding",
				Namespace: "acme-prod-api",
			}},
			Health: platformv1alpha1.HealthStatus{Summary: "Binding ready."},
		},
	}
}
