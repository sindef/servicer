// Package namespaceaccess defines the namespace-scoped Kubernetes read surface
// Servicer grants with a namespace access token.
//
// One explicit allowlist drives both sides of the grant:
//
//   - The BFF namespace proxy calls PathAllowed to decide which upstream
//     Kubernetes API paths a token may read.
//   - The manager's grant-access flow calls PolicyRules to build the namespaced
//     RBAC Role for the granted ServiceAccount.
//
// Sharing the table means the proxy can never expose more than the granted
// Role. Cluster-wide reads (for example /apis/apps/v1/deployments),
// cluster-scoped resources (for example
// /apis/rbac.authorization.k8s.io/v1/clusterroles), other namespaces, and
// ungranted in-namespace resources are all rejected by construction, because
// PathAllowed only accepts paths that repeat the token namespace as their
// literal namespace segment and name an allowlisted resource.
package namespaceaccess

import (
	"strings"

	rbacv1 "k8s.io/api/rbac/v1"
)

// ReadVerbs are the verbs granted for every allowlisted resource. The BFF
// namespace proxy is stricter and forwards read methods only.
var ReadVerbs = []string{"get", "list", "watch"}

// DiscoverPaths are the API discovery documents a namespace access token may
// read. They carry API type metadata only, never object data.
var DiscoverPaths = []string{
	"/api",
	"/apis",
	"/api/v1",
	"/version",
	"/openapi/v2",
	"/openapi/v3",
}

// Grant is one allowlisted namespace-scoped read.
type Grant struct {
	// APIGroup is the Kubernetes API group; the empty string is the core group.
	APIGroup string
	// Resource is the plural resource name, for example "deployments".
	Resource string
	// Subresource is empty for the resource itself, or a granted read
	// subresource such as the "log" subresource of pods.
	Subresource string
}

// Grants is the explicit allowlist of namespace-scoped reads. Widening it
// widens both the grant-access RBAC Role and the namespace proxy path
// allowlist, so every entry must be a deliberate product decision.
var Grants = []Grant{
	{Resource: "configmaps"},
	{Resource: "events"},
	{Resource: "limitranges"},
	{Resource: "persistentvolumeclaims"},
	{Resource: "pods"},
	{Resource: "pods", Subresource: "log"},
	{Resource: "resourcequotas"},
	{Resource: "services"},
	{APIGroup: "apps", Resource: "daemonsets"},
	{APIGroup: "apps", Resource: "deployments"},
	{APIGroup: "apps", Resource: "replicasets"},
	{APIGroup: "apps", Resource: "statefulsets"},
	{APIGroup: "batch", Resource: "cronjobs"},
	{APIGroup: "batch", Resource: "jobs"},
	{APIGroup: "networking.k8s.io", Resource: "networkpolicies"},
}

// PolicyRules returns the namespaced RBAC rules that match Grants, grouped per
// API group in grant declaration order.
func PolicyRules() []rbacv1.PolicyRule {
	rules := make([]rbacv1.PolicyRule, 0, len(Grants))
	positions := map[string]int{}
	for _, grant := range Grants {
		resource := grant.Resource
		if grant.Subresource != "" {
			resource = grant.Resource + "/" + grant.Subresource
		}
		position, ok := positions[grant.APIGroup]
		if !ok {
			rules = append(rules, rbacv1.PolicyRule{
				APIGroups: []string{grant.APIGroup},
				Resources: []string{resource},
				Verbs:     append([]string(nil), ReadVerbs...),
			})
			positions[grant.APIGroup] = len(rules) - 1
			continue
		}
		rules[position].Resources = append(rules[position].Resources, resource)
	}
	return rules
}

// PathAllowed reports whether a namespace access token scoped to namespace may
// read upstreamPath. Only API discovery documents and object paths that carry
// namespace as their literal namespace segment and name an allowlisted
// resource are accepted.
func PathAllowed(namespace, upstreamPath string) bool {
	if namespace == "" || upstreamPath == "" {
		return false
	}
	path := upstreamPath
	for len(path) > 1 && strings.HasSuffix(path, "/") {
		path = strings.TrimSuffix(path, "/")
	}
	if path == "/" || !strings.HasPrefix(path, "/") {
		return false
	}
	if discoverPathAllowed(path) {
		return true
	}
	return grantedPathAllowed(namespace, path)
}

// discoverPathAllowed accepts the discovery documents that list API types:
// /apis, /apis/<group>, /apis/<group>/<version>, and the OpenAPI documents.
// None of them expose object data for any namespace.
func discoverPathAllowed(path string) bool {
	for _, discovery := range DiscoverPaths {
		if path == discovery {
			return true
		}
	}
	if strings.HasPrefix(path, "/openapi/v3/") {
		return true
	}
	segments := splitPath(path)
	if len(segments) != 2 && len(segments) != 3 {
		return false
	}
	if segments[0] != "apis" || !validSegment(segments[1]) {
		return false
	}
	return len(segments) == 2 || validSegment(segments[2])
}

// grantedPathAllowed accepts object paths of the granted namespace only:
//
//	/api/v1/namespaces/<namespace>[/<resource>[/<name>[/<subresource>]]]
//	/apis/<group>/<version>/namespaces/<namespace>[/<resource>...]
func grantedPathAllowed(namespace, path string) bool {
	segments := splitPath(path)
	// Core group has a fixed "v1" version under /api.
	if len(segments) >= 3 && segments[0] == "api" && segments[1] == "v1" {
		return grantedNamespaceTailAllowed("", segments[2:], namespace)
	}
	// Named groups are located by the /apis prefix and validated by the
	// allowlist, not by a version string, because RBAC grants are
	// version-agnostic.
	if len(segments) >= 5 && segments[0] == "apis" && validSegment(segments[1]) && validSegment(segments[2]) {
		return grantedNamespaceTailAllowed(segments[1], segments[3:], namespace)
	}
	return false
}

// grantedNamespaceTailAllowed matches the "<namespaces>/<namespace>[/...]" tail
// that follows the group and version segments.
func grantedNamespaceTailAllowed(apiGroup string, tail []string, namespace string) bool {
	if len(tail) < 2 || tail[0] != "namespaces" || tail[1] != namespace {
		return false
	}
	rest := tail[2:]
	if len(rest) == 0 {
		// The granted namespace object itself, used by kubectl context checks.
		return true
	}
	if len(rest) > 3 {
		// resource[/name[/subresource]] is the deepest shape the grant covers.
		return false
	}
	for _, segment := range rest {
		if !validSegment(segment) {
			return false
		}
	}
	resource := rest[0]
	subresource := ""
	if len(rest) == 3 {
		subresource = rest[2]
	}
	for _, grant := range Grants {
		if grant.APIGroup == apiGroup && grant.Resource == resource && grant.Subresource == subresource {
			return true
		}
	}
	return false
}

func splitPath(path string) []string {
	return strings.Split(strings.TrimPrefix(path, "/"), "/")
}

func validSegment(segment string) bool {
	return segment != "" && segment != "." && segment != ".."
}
