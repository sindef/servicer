package bff

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"k8s.io/client-go/rest"
)

func TestNamespaceProxyPathAllowlistIsNamespaceScoped(t *testing.T) {
	const namespace = "acme-prod-team-space"
	allowed := []string{
		"/api",
		"/apis",
		"/version",
		"/api/v1",
		"/apis/apps",
		"/apis/apps/v1",
		"/openapi/v2",
		"/api/v1/namespaces/" + namespace,
		"/api/v1/namespaces/" + namespace + "/pods",
		"/api/v1/namespaces/" + namespace + "/services/session-cache",
		"/apis/apps/v1/namespaces/" + namespace + "/deployments",
		"/apis/apps/v1/namespaces/" + namespace + "/deployments/session-cache",
	}
	for _, path := range allowed {
		if !namespaceProxyPathAllowed(namespace, path) {
			t.Fatalf("expected %q to be allowed for namespace %q", path, namespace)
		}
	}

	denied := []string{
		"/api/v1/pods",
		"/api/v1/namespaces",
		"/api/v1/namespaces/other",
		"/api/v1/namespaces/other/pods",
		"/apis/apps/v1/deployments",
		"/apis/apps/v1/deployments/session-cache",
		"/apis/rbac.authorization.k8s.io/v1/clusterroles",
		"/apis/apps/v1/namespaces/other/deployments",
		"/apis/apps/v1/namespaces/" + namespace + "x/deployments",
		"/apis/apps/v1/namespaces/" + namespace + "/../other/deployments",
		"/apis/apps/v1/namespaces//deployments",
		"/apis/apps/v1/namespaces/" + namespace + "/deployments?fieldSelector=metadata.namespace=other",
		"/openapi/v2/../apis/apps/v1/deployments",
		"apis/apps/v1/deployments",
	}
	for _, path := range denied {
		if namespaceProxyPathAllowed(namespace, path) {
			t.Fatalf("expected %q to be denied for namespace %q", path, namespace)
		}
	}

	if namespaceProxyPathAllowed("", "/api/v1/namespaces/"+namespace+"/pods") {
		t.Fatalf("expected empty namespace to deny namespace-scoped reads")
	}
}

func TestNamespaceProxyRejectsClusterScopedRequests(t *testing.T) {
	forwarded := ""
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		forwarded = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"kind":"DeploymentList","items":[]}`))
	}))
	defer upstream.Close()

	server := testServerWithConfig(t, &rest.Config{Host: upstream.URL})
	for _, path := range []string{
		"/api/kubernetes/namespaces/acme-prod-team-space/apis/apps/v1/deployments",
		"/api/kubernetes/namespaces/acme-prod-team-space/apis/apps/v1/deployments?fieldSelector=metadata.namespace=rogue-prod-team-space",
		"/api/kubernetes/namespaces/acme-prod-team-space/apis/rbac.authorization.k8s.io/v1/clusterroles",
		"/api/kubernetes/namespaces/acme-prod-team-space/api/v1/pods",
		"/api/kubernetes/namespaces/acme-prod-team-space/api/v1/namespaces",
		"/api/kubernetes/namespaces/acme-prod-team-space/apis/apps/v1/namespaces/rogue-prod-team-space/deployments",
	} {
		forwarded = ""
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, path, nil)
		request.Header.Set("Authorization", "Bearer test-access-token")

		server.Handler().ServeHTTP(response, request)

		if response.Code != http.StatusForbidden {
			t.Fatalf("expected status 403 for %q, got %d: %s", path, response.Code, response.Body.String())
		}
		if forwarded != "" {
			t.Fatalf("expected no upstream forward for %q, got %q", path, forwarded)
		}
	}
}

func TestKubernetesRootProxyRejectsClusterScopedRequests(t *testing.T) {
	forwarded := ""
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		forwarded = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"kind":"DeploymentList","items":[]}`))
	}))
	defer upstream.Close()

	server := testServerWithConfig(t, &rest.Config{Host: upstream.URL})
	for _, path := range []string{
		"/apis/apps/v1/deployments",
		"/apis/apps/v1/deployments?fieldSelector=metadata.namespace=rogue-prod-team-space",
		"/apis/rbac.authorization.k8s.io/v1/clusterroles",
		"/api/v1/pods",
		"/api/v1/namespaces",
	} {
		forwarded = ""
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, path, nil)
		request.Header.Set("Authorization", "Bearer test-access-token")
		request.Header.Set("User-Agent", "kubectl/v1.29")

		server.Handler().ServeHTTP(response, request)

		if response.Code != http.StatusForbidden {
			t.Fatalf("expected status 403 for %q, got %d: %s", path, response.Code, response.Body.String())
		}
		if forwarded != "" {
			t.Fatalf("expected no upstream forward for %q, got %q", path, forwarded)
		}
	}
}
