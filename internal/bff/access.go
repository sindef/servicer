package bff

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	platformv1alpha1 "github.com/sindef/servicer/api/v1alpha1"
	"github.com/sindef/servicer/internal/adapters"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

const projectedCredentialSuffix = "-projected"

func (s *Server) handleDownloadNamespaceKubeconfig(w http.ResponseWriter, r *http.Request) {
	actor, ok := requireRole(w, r, rolePlatformAdmin, roleTenantOperator, roleServiceConsumer)
	if !ok {
		return
	}
	instanceName := strings.TrimSpace(r.PathValue("name"))
	actionName := strings.TrimSpace(r.PathValue("action"))
	if instanceName == "" || actionName == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "instance and action names are required"})
		return
	}

	var instance platformv1alpha1.ServiceInstance
	if err := s.client.Get(r.Context(), types.NamespacedName{Name: instanceName}, &instance); err != nil {
		writeError(w, err)
		return
	}
	if instance.Spec.ServiceClassRef.Name != string(adapters.ServiceClassNamespace) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "kubeconfig downloads are only available for namespace products"})
		return
	}

	var action platformv1alpha1.ActionRequest
	if err := s.client.Get(r.Context(), types.NamespacedName{Name: actionName}, &action); err != nil {
		writeError(w, err)
		return
	}
	if action.Spec.TargetRef.Name != instanceName || action.Spec.Action != string(adapters.ActionGrantAccess) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "action is not a grant-access request for this instance"})
		return
	}
	if action.Status.Phase != "Succeeded" {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "grant-access action has not completed yet"})
		return
	}
	if action.Status.OperationRef == nil || action.Status.OperationRef.Kind != "Secret" || action.Status.OperationRef.Name == "" || action.Status.OperationRef.Namespace == "" {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "grant-access action did not publish a kubeconfig reference"})
		return
	}

	var secret corev1.Secret
	if err := s.client.Get(r.Context(), types.NamespacedName{Name: action.Status.OperationRef.Name, Namespace: action.Status.OperationRef.Namespace}, &secret); err != nil {
		if apierrors.IsNotFound(err) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "kubeconfig Secret was not found"})
			return
		}
		writeError(w, err)
		return
	}
	subject := strings.TrimSpace(string(secret.Data["subject"]))
	if subject == "" {
		subject = strings.TrimSpace(action.Spec.RequestedBy.Subject)
	}
	if subject != "" && !actor.hasAny(rolePlatformAdmin, roleTenantOperator) && actor.Name != subject {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "kubeconfig belongs to a different subject"})
		return
	}
	if !s.authorizeInstance(r.Context(), actor, &instance) && actor.Name != subject {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "instance is outside your authorized tenancy"})
		return
	}
	kubeconfig := secret.Data["kubeconfig"]
	if len(kubeconfig) == 0 {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "kubeconfig Secret does not contain kubeconfig data"})
		return
	}

	filename := fmt.Sprintf("%s-%s.kubeconfig", instanceName, safeDownloadName(subject, "access"))
	w.Header().Set("Content-Type", "application/x-yaml")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(kubeconfig)
}

func (s *Server) handleCredentialDetail(w http.ResponseWriter, r *http.Request) {
	_, _, secret, ok := s.resolveInstanceCredential(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, CredentialDetail{
		Name:      secret.Name,
		Namespace: secret.Namespace,
		Data:      secretStringData(secret),
	})
}

func (s *Server) handleDownloadCredential(w http.ResponseWriter, r *http.Request) {
	_, instance, secret, ok := s.resolveInstanceCredential(w, r)
	if !ok {
		return
	}
	payload, err := json.MarshalIndent(CredentialDetail{
		Name:      secret.Name,
		Namespace: secret.Namespace,
		Data:      secretStringData(secret),
	}, "", "  ")
	if err != nil {
		writeError(w, err)
		return
	}
	filename := fmt.Sprintf("%s-%s.credentials.json", instance.Name, safeDownloadName(secret.Name, "credentials"))
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(payload)
}

func (s *Server) resolveInstanceCredential(w http.ResponseWriter, r *http.Request) (actor, *platformv1alpha1.ServiceInstance, *corev1.Secret, bool) {
	actor, ok := requireRole(w, r, rolePlatformAdmin, roleTenantOperator, roleServiceConsumer)
	if !ok {
		return actor, nil, nil, false
	}
	instanceName := strings.TrimSpace(r.PathValue("name"))
	namespace := strings.TrimSpace(r.PathValue("namespace"))
	credentialName := strings.TrimSpace(r.PathValue("credential"))
	if instanceName == "" || namespace == "" || credentialName == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "instance, namespace, and credential names are required"})
		return actor, nil, nil, false
	}

	var instance platformv1alpha1.ServiceInstance
	if err := s.client.Get(r.Context(), types.NamespacedName{Name: instanceName}, &instance); err != nil {
		writeError(w, err)
		return actor, nil, nil, false
	}
	if !s.authorizeInstance(r.Context(), actor, &instance) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "instance is outside your authorized tenancy"})
		return actor, nil, nil, false
	}
	if !instanceReferencesCredential(&instance, namespace, credentialName) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "credential is not published for this instance"})
		return actor, nil, nil, false
	}

	var secret corev1.Secret
	if err := s.client.Get(r.Context(), types.NamespacedName{Name: credentialName, Namespace: namespace}, &secret); err != nil {
		if apierrors.IsNotFound(err) {
			// During external-secret delivery, projected Secrets may lag behind their
			// source Secret. Allow reveal/download to fall back to the source Secret.
			if sourceName, ok := sourceCredentialNameFromProjected(credentialName); ok {
				var sourceSecret corev1.Secret
				if sourceErr := s.client.Get(r.Context(), types.NamespacedName{Name: sourceName, Namespace: namespace}, &sourceSecret); sourceErr == nil {
					return actor, &instance, &sourceSecret, true
				}
			}

			// Credentials are created in the runtime target cluster. If they aren't
			// present on the control-plane cluster, fetch directly from the placed
			// cluster using ClusterTarget connection credentials.
			if clusterName := strings.TrimSpace(instance.Status.Placement.ClusterName); clusterName != "" {
				if remoteSecret, remoteErr := s.getRemoteCredentialSecret(r.Context(), clusterName, namespace, credentialName); remoteErr == nil {
					return actor, &instance, remoteSecret, true
				} else if !apierrors.IsNotFound(remoteErr) {
					writeError(w, remoteErr)
					return actor, nil, nil, false
				}
				if sourceName, ok := sourceCredentialNameFromProjected(credentialName); ok {
					if remoteSourceSecret, remoteSourceErr := s.getRemoteCredentialSecret(r.Context(), clusterName, namespace, sourceName); remoteSourceErr == nil {
						return actor, &instance, remoteSourceSecret, true
					} else if !apierrors.IsNotFound(remoteSourceErr) {
						writeError(w, remoteSourceErr)
						return actor, nil, nil, false
					}
				}
			}

			writeJSON(w, http.StatusNotFound, map[string]string{"error": "credential Secret was not found"})
			return actor, nil, nil, false
		}
		writeError(w, err)
		return actor, nil, nil, false
	}
	return actor, &instance, &secret, true
}

func sourceCredentialNameFromProjected(name string) (string, bool) {
	name = strings.TrimSpace(name)
	if !strings.HasSuffix(name, projectedCredentialSuffix) {
		return "", false
	}
	base := strings.TrimSuffix(name, projectedCredentialSuffix)
	if strings.TrimSpace(base) == "" {
		return "", false
	}
	return base, true
}

func (s *Server) getRemoteCredentialSecret(ctx context.Context, clusterName, namespace, credentialName string) (*corev1.Secret, error) {
	clientset, err := s.clientsetForClusterTarget(ctx, clusterName)
	if err != nil {
		return nil, err
	}
	return clientset.CoreV1().Secrets(namespace).Get(ctx, credentialName, metav1.GetOptions{})
}

func (s *Server) clientsetForClusterTarget(ctx context.Context, clusterName string) (*kubernetes.Clientset, error) {
	var target platformv1alpha1.ClusterTarget
	if err := s.client.Get(ctx, types.NamespacedName{Name: clusterName}, &target); err != nil {
		return nil, err
	}
	connectionRef := target.Spec.ConnectionRef
	if strings.TrimSpace(connectionRef.Name) == "" || strings.TrimSpace(connectionRef.Namespace) == "" {
		return nil, fmt.Errorf("cluster target %q does not define a connectionRef", clusterName)
	}

	var secret corev1.Secret
	if err := s.client.Get(ctx, types.NamespacedName{Name: connectionRef.Name, Namespace: connectionRef.Namespace}, &secret); err != nil {
		return nil, err
	}
	kubeconfig := clusterConnectionData(&secret)
	if len(kubeconfig) == 0 {
		return nil, fmt.Errorf("cluster connection Secret %s/%s does not contain kubeconfig data", connectionRef.Namespace, connectionRef.Name)
	}

	restConfig, err := clientcmd.RESTConfigFromKubeConfig(kubeconfig)
	if err != nil {
		return nil, err
	}
	return kubernetes.NewForConfig(restConfig)
}

func clusterConnectionData(secret *corev1.Secret) []byte {
	if secret == nil {
		return nil
	}
	if value := secret.Data["kubeconfig"]; len(value) > 0 {
		return value
	}
	if value := secret.Data["value"]; len(value) > 0 {
		return value
	}
	return nil
}

func instanceReferencesCredential(instance *platformv1alpha1.ServiceInstance, namespace, name string) bool {
	for _, ref := range instance.Status.CredentialRefs {
		if ref.Namespace == namespace && ref.Name == name {
			return true
		}
	}
	return false
}

func secretStringData(secret *corev1.Secret) map[string]string {
	result := make(map[string]string, len(secret.Data))
	for key, value := range secret.Data {
		result[key] = string(value)
	}
	return result
}

func safeDownloadName(value, fallback string) string {
	name := strings.ToLower(strings.TrimSpace(value))
	var builder strings.Builder
	lastDash := false
	for _, char := range name {
		valid := (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9')
		if valid {
			builder.WriteRune(char)
			lastDash = false
			continue
		}
		if !lastDash {
			builder.WriteByte('-')
			lastDash = true
		}
	}
	name = strings.Trim(builder.String(), "-")
	if len(name) > 48 {
		name = strings.Trim(name[:48], "-")
	}
	if name == "" {
		return fallback
	}
	return name
}
