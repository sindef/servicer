package bff

import (
	"crypto/subtle"
	"io"
	"net/http"
	"net/url"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const namespaceProxyPrefix = "/api/kubernetes/namespaces/"

func (s *Server) handleKubernetesNamespaceProxy(w http.ResponseWriter, r *http.Request) {
	if s.kubeClient == nil || s.kubeHost == "" {
		if s.metrics != nil {
			s.metrics.upstreamFailuresTotal.Inc()
		}
		http.Error(w, "Kubernetes proxy is not configured", http.StatusServiceUnavailable)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		if s.metrics != nil {
			s.metrics.namespaceProxyDenialsTotal.Inc()
		}
		http.Error(w, "Namespace proxy is read-only", http.StatusMethodNotAllowed)
		return
	}
	namespace, upstreamPath, ok := namespaceProxyTarget(r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}
	secret, ok := s.namespaceAccessSecretForToken(r, namespace)
	if !ok {
		if s.metrics != nil {
			s.metrics.namespaceProxyDenialsTotal.Inc()
		}
		http.Error(w, "Invalid namespace access token", http.StatusUnauthorized)
		return
	}
	secretNamespace := strings.TrimSpace(string(secret.Data["namespace"]))
	if secretNamespace != "" && secretNamespace != namespace {
		if s.metrics != nil {
			s.metrics.namespaceProxyDenialsTotal.Inc()
		}
		http.Error(w, "Access token is not valid for this namespace", http.StatusForbidden)
		return
	}
	if !namespaceProxyPathAllowed(namespace, upstreamPath) {
		if s.metrics != nil {
			s.metrics.namespaceProxyDenialsTotal.Inc()
		}
		http.Error(w, "Namespace access is limited to discovery and granted namespace reads", http.StatusForbidden)
		return
	}
	s.forwardKubernetesRequest(w, r, upstreamPath)
}

func (s *Server) handleKubernetesRootProxy(w http.ResponseWriter, r *http.Request) {
	if s.kubeClient == nil || s.kubeHost == "" || !looksLikeKubernetesClient(r) {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		if s.metrics != nil {
			s.metrics.namespaceProxyDenialsTotal.Inc()
		}
		http.Error(w, "Namespace proxy is read-only", http.StatusMethodNotAllowed)
		return
	}
	secret, ok := s.namespaceAccessSecretForToken(r, "")
	if !ok {
		if s.metrics != nil {
			s.metrics.namespaceProxyDenialsTotal.Inc()
		}
		http.Error(w, "Invalid namespace access token", http.StatusUnauthorized)
		return
	}
	namespace := strings.TrimSpace(string(secret.Data["namespace"]))
	if namespace == "" {
		namespace = secret.Namespace
	}
	if !namespaceProxyPathAllowed(namespace, r.URL.Path) {
		if s.metrics != nil {
			s.metrics.namespaceProxyDenialsTotal.Inc()
		}
		http.Error(w, "Namespace access is limited to discovery and granted namespace reads", http.StatusForbidden)
		return
	}
	s.forwardKubernetesRequest(w, r, r.URL.Path)
}

func looksLikeKubernetesClient(r *http.Request) bool {
	return bearerToken(r.Header.Get("Authorization")) != ""
}

func namespaceProxyTarget(path string) (string, string, bool) {
	remaining := strings.TrimPrefix(path, namespaceProxyPrefix)
	if remaining == path {
		return "", "", false
	}
	parts := strings.SplitN(remaining, "/", 2)
	namespace := strings.TrimSpace(parts[0])
	if namespace == "" {
		return "", "", false
	}
	if len(parts) == 1 || parts[1] == "" {
		return namespace, "/api", true
	}
	return namespace, "/" + parts[1], true
}

func (s *Server) namespaceAccessSecretForToken(r *http.Request, namespace string) (*corev1.Secret, bool) {
	token := bearerToken(r.Header.Get("Authorization"))
	if token == "" {
		return nil, false
	}
	var secrets corev1.SecretList
	options := []client.ListOption{client.MatchingLabels{
		"servicer.io/managed-by": "servicer",
		"servicer.io/purpose":    "namespace-access",
	}}
	if namespace != "" {
		options = append(options, client.InNamespace(namespace))
	}
	if err := s.client.List(r.Context(), &secrets, options...); err != nil {
		return nil, false
	}
	for i := range secrets.Items {
		secretToken := strings.TrimSpace(string(secrets.Items[i].Data["token"]))
		if secretToken != "" && subtle.ConstantTimeCompare([]byte(token), []byte(secretToken)) == 1 {
			return &secrets.Items[i], true
		}
	}
	return nil, false
}

func bearerToken(header string) string {
	parts := strings.Fields(header)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return strings.TrimSpace(parts[1])
}

// namespaceProxyPathAllowed reports whether upstreamPath is on the namespace
// access allowlist for namespace. The allowlist is structural: cluster
// discovery documents plus read paths that are explicitly scoped to the
// token's own namespace. Anything else, including cluster-scoped collections
// such as /apis/apps/v1/deployments or
// /apis/rbac.authorization.k8s.io/v1/clusterroles, is rejected even when the
// caller holds a valid namespace access token.
func namespaceProxyPathAllowed(namespace, upstreamPath string) bool {
	if namespace == "" || !strings.HasPrefix(upstreamPath, "/") {
		return false
	}
	segments, ok := proxyPathSegments(upstreamPath)
	if !ok {
		return false
	}
	if segments[0] == "openapi" {
		return true
	}
	switch segments[0] {
	case "version":
		return len(segments) == 1
	case "api":
		if len(segments) == 1 {
			return true
		}
		if segments[1] != "v1" {
			return false
		}
		if len(segments) == 2 {
			return true
		}
		return namespaceScopedProxyPath(segments[2:], namespace)
	case "apis":
		// /apis, /apis/<group> and /apis/<group>/<version> are discovery only.
		if len(segments) <= 3 {
			return true
		}
		return namespaceScopedProxyPath(segments[3:], namespace)
	default:
		return false
	}
}

// namespaceScopedProxyPath reports whether the remaining path segments start
// with an explicit "namespaces/<token namespace>" scope.
func namespaceScopedProxyPath(remaining []string, namespace string) bool {
	if len(remaining) < 2 || remaining[0] != "namespaces" {
		return false
	}
	return remaining[1] == namespace
}

// proxyPathSegments splits a proxy path into segments and rejects empty,
// relative or query-bearing segments so a forwarded path cannot walk out of
// the allowed namespace scope.
func proxyPathSegments(upstreamPath string) ([]string, bool) {
	trimmed := strings.Trim(upstreamPath, "/")
	if trimmed == "" {
		return nil, false
	}
	segments := strings.Split(trimmed, "/")
	for _, segment := range segments {
		if segment == "" || segment == "." || segment == ".." {
			return nil, false
		}
		if strings.ContainsAny(segment, "?#") {
			return nil, false
		}
	}
	return segments, true
}

func (s *Server) forwardKubernetesRequest(w http.ResponseWriter, r *http.Request, upstreamPath string) {
	upstreamURL, err := url.Parse(s.kubeHost)
	if err != nil {
		if s.metrics != nil {
			s.metrics.upstreamFailuresTotal.Inc()
		}
		http.Error(w, "Kubernetes proxy is misconfigured", http.StatusServiceUnavailable)
		return
	}
	upstreamURL.Path = upstreamPath
	upstreamURL.RawQuery = r.URL.RawQuery
	request, err := http.NewRequestWithContext(r.Context(), r.Method, upstreamURL.String(), nil)
	if err != nil {
		if s.metrics != nil {
			s.metrics.upstreamFailuresTotal.Inc()
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	copyProxyHeader(request.Header, r.Header, "Accept")
	copyProxyHeader(request.Header, r.Header, "User-Agent")
	copyProxyHeader(request.Header, r.Header, "Accept-Encoding")
	response, err := s.kubeClient.Do(request) // #nosec G704 -- Upstream host is fixed from kube config and path is namespace-validated before forwarding.
	if err != nil {
		if s.metrics != nil {
			s.metrics.upstreamFailuresTotal.Inc()
		}
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer response.Body.Close()
	for key, values := range response.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	w.WriteHeader(response.StatusCode)
	if r.Method == http.MethodHead {
		return
	}
	_, _ = io.Copy(w, response.Body)
}

func copyProxyHeader(target, source http.Header, key string) {
	for _, value := range source.Values(key) {
		target.Add(key, value)
	}
}
