package bff

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	platformv1alpha1 "github.com/sindef/servicer/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
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
	if len(entries) != 4 {
		t.Fatalf("expected only implemented seeded products, got %#v", entries)
	}
	var namespaceEntry *CatalogEntry
	for i := range entries {
		if entries[i].Name == "namespace" {
			namespaceEntry = &entries[i]
			break
		}
	}
	if namespaceEntry == nil || len(namespaceEntry.Plans) != 1 || len(namespaceEntry.Actions) == 0 {
		t.Fatalf("expected namespace catalog entry with plans/actions, got %#v", entries)
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

	var argoSecret corev1.Secret
	if err := server.client.Get(createRequest.Context(), client.ObjectKey{Name: "argocd-repo-github-com-acme-storefront-git", Namespace: "argocd"}, &argoSecret); err != nil {
		t.Fatalf("expected mirrored Argo CD repository secret: %v", err)
	}
	if got := string(argoSecret.Data["url"]); got != "https://github.com/acme/storefront.git" {
		t.Fatalf("expected Argo CD secret url to match, got %q", got)
	}

	deleteResponse := httptest.NewRecorder()
	deleteRequest := httptest.NewRequest(http.MethodDelete, "/api/projects/acme-prod/repositories/storefront-app", nil)
	deleteRequest.Header.Set("X-Servicer-User", "alice@example.com")
	deleteRequest.Header.Set("X-Servicer-Roles", "tenant-operator")
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
	if len(claims) != 1 || claims[0].Name != "team-space-claim" {
		t.Fatalf("unexpected claims %#v", claims)
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
	return NewServerWithConfig(client, restConfig)
}

func testTenant() *platformv1alpha1.Tenant {
	return &platformv1alpha1.Tenant{
		ObjectMeta: metav1.ObjectMeta{Name: "acme"},
		Spec: platformv1alpha1.TenantSpec{
			DisplayName:           "Acme",
			Owners:                platformv1alpha1.OwnersSpec{Users: []string{"alice@example.com"}},
			QuotaProfileRef:       platformv1alpha1.LocalObjectReference{Name: "standard"},
			AllowedServiceClasses: []string{"namespace", "valkey"},
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
			Sync:   platformv1alpha1.DeliverySyncStatus{Phase: "Synced"},
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
			ProjectRef: platformv1alpha1.LocalObjectReference{Name: "acme-prod"},
		},
		Status: platformv1alpha1.NamespaceClaimStatus{
			Phase: "Ready",
			Placement: platformv1alpha1.PlacementStatus{
				ClusterName: "east-1",
				Namespace:   "acme-prod-team-space",
			},
			Health: platformv1alpha1.HealthStatus{Summary: "Namespace claim ready."},
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
