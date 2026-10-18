package namespaceaccess

import "testing"

func TestPathAllowedAcceptsDiscoveryAndGrantedNamespaceReads(t *testing.T) {
	const namespace = "acme-prod-team-space"
	allowed := []string{
		"/api",
		"/apis",
		"/api/v1",
		"/version",
		"/openapi/v2",
		"/openapi/v3",
		"/openapi/v3/apis/apps/v1",
		"/apis/apps",
		"/apis/apps/v1",
		"/apis/rbac.authorization.k8s.io/v1",
		"/api/v1/namespaces/" + namespace,
		"/api/v1/namespaces/" + namespace + "/",
		"/api/v1/namespaces/" + namespace + "/pods",
		"/api/v1/namespaces/" + namespace + "/pods/pod-1",
		"/api/v1/namespaces/" + namespace + "/pods/pod-1/log",
		"/api/v1/namespaces/" + namespace + "/services",
		"/api/v1/namespaces/" + namespace + "/persistentvolumeclaims",
		"/apis/apps/v1/namespaces/" + namespace + "/deployments",
		"/apis/apps/v1/namespaces/" + namespace + "/deployments/web",
		"/apis/apps/v1/namespaces/" + namespace + "/statefulsets/db-0",
		"/apis/batch/v1/namespaces/" + namespace + "/jobs",
		"/apis/networking.k8s.io/v1/namespaces/" + namespace + "/networkpolicies",
	}
	for _, path := range allowed {
		if !PathAllowed(namespace, path) {
			t.Errorf("expected path to be allowed: %q", path)
		}
	}
}

func TestPathAllowedRejectsClusterScopedAndUngrantedReads(t *testing.T) {
	const namespace = "acme-prod-team-space"
	denied := []string{
		// Cluster-wide lists: no namespace segment at all.
		"/apis/apps/v1/deployments",
		"/api/v1/pods",
		"/api/v1/namespaces",
		"/apis/apps/v1/namespaces",
		// Cluster-scoped resources.
		"/apis/rbac.authorization.k8s.io/v1/clusterroles",
		"/apis/rbac.authorization.k8s.io/v1/clusterrolebindings",
		"/api/v1/nodes",
		"/apis/storage.k8s.io/v1/storageclasses",
		"/apis/apiextensions.k8s.io/v1/customresourcedefinitions",
		// Another namespace.
		"/api/v1/namespaces/other-team-space/pods",
		"/apis/apps/v1/namespaces/other-team-space/deployments",
		"/apis/apps/v1/namespaces/acme-prod-team-space-other/deployments",
		// In-namespace but not granted.
		"/api/v1/namespaces/" + namespace + "/secrets",
		"/api/v1/namespaces/" + namespace + "/serviceaccounts",
		"/apis/rbac.authorization.k8s.io/v1/namespaces/" + namespace + "/roles",
		"/apis/apps/v1/namespaces/" + namespace + "/controllerrevisions",
		// Granted resource but not granted subresource.
		"/api/v1/namespaces/" + namespace + "/pods/pod-1/exec",
		"/api/v1/namespaces/" + namespace + "/pods/pod-1/portforward",
		"/api/v1/namespaces/" + namespace + "/pods/pod-1/log/stream",
		"/apis/apps/v1/namespaces/" + namespace + "/deployments/web/status",
		// Malformed paths.
		"/api/v1/namespaces/" + namespace + "/../other-team-space/pods",
		"/api/v1/namespaces/../pods",
		"api/v1/namespaces/" + namespace + "/pods",
		"/",
		"",
		"//api/v1/namespaces/" + namespace + "/pods",
	}
	for _, path := range denied {
		if PathAllowed(namespace, path) {
			t.Errorf("expected path to be denied: %q", path)
		}
	}
}

func TestPathAllowedRejectsEmptyNamespace(t *testing.T) {
	for _, namespace := range []string{"", " "} {
		if PathAllowed(namespace, "/api/v1/namespaces/team-space/pods") {
			t.Errorf("expected path to be denied for namespace %q", namespace)
		}
	}
}

func TestPathAllowedNeverAcceptsAggregateLists(t *testing.T) {
	// Every grant must stay reachable inside its own namespace, and the same
	// resource path without a namespace segment must stay unreachable.
	for _, grant := range Grants {
		namespaced := grantPath("acme-prod-team-space", grant)
		if !PathAllowed("acme-prod-team-space", namespaced) {
			t.Errorf("expected granted path to be allowed: %q", namespaced)
		}
		if PathAllowed("acme-prod-team-space", grantPath("", grant)) {
			t.Errorf("expected cluster-wide path to be denied: %q", grantPath("", grant))
		}
	}
}

func grantPath(namespace string, grant Grant) string {
	prefix := "/api/v1"
	if grant.APIGroup != "" {
		prefix = "/apis/" + grant.APIGroup + "/v1"
	}
	if namespace != "" {
		prefix += "/namespaces/" + namespace
	}
	path := prefix + "/" + grant.Resource
	if grant.Subresource != "" {
		path += "/sample/" + grant.Subresource
	}
	return path
}
