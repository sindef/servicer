package controllers

import (
	"crypto/sha1"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	platformv1alpha1 "github.com/sindef/servicer/api/v1alpha1"
	"github.com/sindef/servicer/internal/adapters"
	"sigs.k8s.io/yaml"
)

const (
	externalSecretsAPIVersion = "external-secrets.io/v1beta1"
)

func projectedCredentialRefs(instance *platformv1alpha1.ServiceInstance, sourceRefs []platformv1alpha1.NamespacedObjectReference) []platformv1alpha1.NamespacedObjectReference {
	if instance == nil {
		return nil
	}
	projected := make([]platformv1alpha1.NamespacedObjectReference, 0, len(sourceRefs))
	targetNamespace := instance.Status.Placement.Namespace
	for _, ref := range sourceRefs {
		namespace := targetNamespace
		if namespace == "" {
			namespace = ref.Namespace
		}
		projected = append(projected, platformv1alpha1.NamespacedObjectReference{
			Name:      projectedCredentialName(ref.Name),
			Namespace: namespace,
		})
	}
	return projected
}

func renderExternalSecretArtifacts(instance *platformv1alpha1.ServiceInstance, packagePath string, sourceRefs, projectedRefs []platformv1alpha1.NamespacedObjectReference) ([]adapters.RenderedArtifact, error) {
	if instance == nil || len(sourceRefs) == 0 || len(sourceRefs) != len(projectedRefs) {
		return nil, nil
	}

	artifacts := make([]adapters.RenderedArtifact, 0, len(sourceRefs)+4)
	if externalSecretProvider(instance.Spec.SecretPolicy) == platformv1alpha1.ExternalSecretProviderVault {
		return renderVaultExternalSecretArtifacts(instance, packagePath, sourceRefs, projectedRefs)
	}
	serviceAccountName := fmt.Sprintf("%s-eso-reader", instance.Name)
	storeBySourceNamespace := map[string]string{}
	targetNamespace := firstNonEmptyTrimmed(instance.Status.Placement.Namespace)
	if targetNamespace == "" {
		targetNamespace = sourceRefs[0].Namespace
	}

	serviceAccountYAML, err := yaml.Marshal(map[string]any{
		"apiVersion": "v1",
		"kind":       "ServiceAccount",
		"metadata": map[string]any{
			"name":      serviceAccountName,
			"namespace": targetNamespace,
			"labels": map[string]string{
				"servicer.io/managed-by":       "servicer",
				"servicer.io/service-instance": instance.Name,
				"servicer.io/secret-delivery":  "external-secret",
			},
		},
	})
	if err != nil {
		return nil, err
	}
	artifacts = append(artifacts, adapters.RenderedArtifact{
		Path:    filepath.ToSlash(filepath.Join(packagePath, "credentials", "serviceaccount.yaml")),
		Content: serviceAccountYAML,
	})

	sourceNamespaces := uniqueSourceNamespaces(sourceRefs)
	for _, sourceNamespace := range sourceNamespaces {
		storeName := secretStoreName(instance.Name, sourceNamespace)
		storeBySourceNamespace[sourceNamespace] = storeName

		roleYAML, err := yaml.Marshal(map[string]any{
			"apiVersion": "rbac.authorization.k8s.io/v1",
			"kind":       "Role",
			"metadata": map[string]any{
				"name":      storeName,
				"namespace": sourceNamespace,
				"labels": map[string]string{
					"servicer.io/managed-by":       "servicer",
					"servicer.io/service-instance": instance.Name,
					"servicer.io/secret-delivery":  "external-secret",
				},
			},
			"rules": []map[string]any{
				{
					"apiGroups": []string{""},
					"resources": []string{"secrets"},
					"verbs":     []string{"get", "list", "watch"},
				},
				{
					"apiGroups": []string{"authorization.k8s.io"},
					"resources": []string{"selfsubjectrulesreviews"},
					"verbs":     []string{"create"},
				},
			},
		})
		if err != nil {
			return nil, err
		}
		roleBindingYAML, err := yaml.Marshal(map[string]any{
			"apiVersion": "rbac.authorization.k8s.io/v1",
			"kind":       "RoleBinding",
			"metadata": map[string]any{
				"name":      storeName,
				"namespace": sourceNamespace,
				"labels": map[string]string{
					"servicer.io/managed-by":       "servicer",
					"servicer.io/service-instance": instance.Name,
					"servicer.io/secret-delivery":  "external-secret",
				},
			},
			"subjects": []map[string]any{
				{
					"kind":      "ServiceAccount",
					"name":      serviceAccountName,
					"namespace": targetNamespace,
				},
			},
			"roleRef": map[string]any{
				"apiGroup": "rbac.authorization.k8s.io",
				"kind":     "Role",
				"name":     storeName,
			},
		})
		if err != nil {
			return nil, err
		}
		secretStoreYAML, err := yaml.Marshal(map[string]any{
			"apiVersion": externalSecretsAPIVersion,
			"kind":       "SecretStore",
			"metadata": map[string]any{
				"name":      storeName,
				"namespace": targetNamespace,
				"labels": map[string]string{
					"servicer.io/managed-by":       "servicer",
					"servicer.io/service-instance": instance.Name,
					"servicer.io/secret-delivery":  "external-secret",
				},
			},
			"spec": map[string]any{
				"provider": map[string]any{
					"kubernetes": map[string]any{
						"remoteNamespace": sourceNamespace,
						"server": map[string]any{
							"caProvider": map[string]any{
								"type": "ConfigMap",
								"name": "kube-root-ca.crt",
								"key":  "ca.crt",
							},
						},
						"auth": map[string]any{
							"serviceAccount": map[string]any{
								"name": serviceAccountName,
							},
						},
					},
				},
			},
		})
		if err != nil {
			return nil, err
		}
		artifacts = append(artifacts,
			adapters.RenderedArtifact{
				Path:    filepath.ToSlash(filepath.Join(packagePath, "credentials", fmt.Sprintf("role-%s.yaml", sourceNamespace))),
				Content: roleYAML,
			},
			adapters.RenderedArtifact{
				Path:    filepath.ToSlash(filepath.Join(packagePath, "credentials", fmt.Sprintf("rolebinding-%s.yaml", sourceNamespace))),
				Content: roleBindingYAML,
			},
			adapters.RenderedArtifact{
				Path:    filepath.ToSlash(filepath.Join(packagePath, "credentials", fmt.Sprintf("secretstore-%s.yaml", sourceNamespace))),
				Content: secretStoreYAML,
			},
		)
	}

	for i, sourceRef := range sourceRefs {
		projectedRef := projectedRefs[i]
		storeName := storeBySourceNamespace[sourceRef.Namespace]
		externalSecretYAML, err := yaml.Marshal(map[string]any{
			"apiVersion": externalSecretsAPIVersion,
			"kind":       "ExternalSecret",
			"metadata": map[string]any{
				"name":      projectedRef.Name,
				"namespace": projectedRef.Namespace,
				"labels": map[string]string{
					"servicer.io/managed-by":       "servicer",
					"servicer.io/service-instance": instance.Name,
					"servicer.io/secret-delivery":  "external-secret",
				},
			},
			"spec": map[string]any{
				"refreshInterval": "1h",
				"secretStoreRef": map[string]any{
					"kind": "SecretStore",
					"name": storeName,
				},
				"target": map[string]any{
					"name":           projectedRef.Name,
					"creationPolicy": "Owner",
					"deletionPolicy": "Delete",
				},
				"dataFrom": []map[string]any{
					{
						"extract": map[string]any{
							"key": sourceRef.Name,
						},
					},
				},
			},
		})
		if err != nil {
			return nil, err
		}
		artifacts = append(artifacts, adapters.RenderedArtifact{
			Path:    filepath.ToSlash(filepath.Join(packagePath, "credentials", fmt.Sprintf("externalsecret-%s.yaml", projectedRef.Name))),
			Content: externalSecretYAML,
		})
	}

	sort.Slice(artifacts, func(i, j int) bool { return artifacts[i].Path < artifacts[j].Path })
	return artifacts, nil
}

func renderVaultExternalSecretArtifacts(instance *platformv1alpha1.ServiceInstance, packagePath string, sourceRefs, projectedRefs []platformv1alpha1.NamespacedObjectReference) ([]adapters.RenderedArtifact, error) {
	vault := instance.Spec.SecretPolicy.Vault
	if instance == nil || vault == nil {
		return nil, nil
	}
	targetNamespace := firstNonEmptyTrimmed(instance.Status.Placement.Namespace)
	if targetNamespace == "" {
		targetNamespace = projectedRefs[0].Namespace
	}
	storeName := fmt.Sprintf("%s-vault", instance.Name)
	authSecretNamespace := firstNonEmptyTrimmed(vault.AuthSecretRef.Namespace, targetNamespace)
	secretStoreYAML, err := yaml.Marshal(map[string]any{
		"apiVersion": externalSecretsAPIVersion,
		"kind":       "SecretStore",
		"metadata": map[string]any{
			"name":      storeName,
			"namespace": targetNamespace,
			"labels": map[string]string{
				"servicer.io/managed-by":       "servicer",
				"servicer.io/service-instance": instance.Name,
				"servicer.io/secret-delivery":  "external-secret",
				"servicer.io/secret-provider":  "vault",
			},
		},
		"spec": map[string]any{
			"provider": map[string]any{
				"vault": map[string]any{
					"server":  vault.Server,
					"path":    vault.Path,
					"version": firstNonEmptyTrimmed(vault.Version, "v2"),
					"auth": map[string]any{
						"tokenSecretRef": map[string]any{
							"name":      vault.AuthSecretRef.Name,
							"key":       "token",
							"namespace": authSecretNamespace,
						},
					},
				},
			},
		},
	})
	if err != nil {
		return nil, err
	}
	artifacts := []adapters.RenderedArtifact{{
		Path:    filepath.ToSlash(filepath.Join(packagePath, "credentials", "secretstore-vault.yaml")),
		Content: secretStoreYAML,
	}}
	for i, sourceRef := range sourceRefs {
		projectedRef := projectedRefs[i]
		externalSecretYAML, err := yaml.Marshal(map[string]any{
			"apiVersion": externalSecretsAPIVersion,
			"kind":       "ExternalSecret",
			"metadata": map[string]any{
				"name":      projectedRef.Name,
				"namespace": projectedRef.Namespace,
				"labels": map[string]string{
					"servicer.io/managed-by":       "servicer",
					"servicer.io/service-instance": instance.Name,
					"servicer.io/secret-delivery":  "external-secret",
					"servicer.io/secret-provider":  "vault",
				},
			},
			"spec": map[string]any{
				"refreshInterval": "1h",
				"secretStoreRef": map[string]any{
					"kind": "SecretStore",
					"name": storeName,
				},
				"target": map[string]any{
					"name":           projectedRef.Name,
					"creationPolicy": "Owner",
					"deletionPolicy": "Delete",
				},
				"dataFrom": []map[string]any{
					{
						"extract": map[string]any{
							"key": vaultRemoteSecretKey(vault.Path, sourceRef.Name),
						},
					},
				},
			},
		})
		if err != nil {
			return nil, err
		}
		artifacts = append(artifacts, adapters.RenderedArtifact{
			Path:    filepath.ToSlash(filepath.Join(packagePath, "credentials", fmt.Sprintf("externalsecret-%s.yaml", projectedRef.Name))),
			Content: externalSecretYAML,
		})
	}
	sort.Slice(artifacts, func(i, j int) bool { return artifacts[i].Path < artifacts[j].Path })
	return artifacts, nil
}

func projectedCredentialName(name string) string {
	return fmt.Sprintf("%s-projected", name)
}

func secretStoreName(instanceName, sourceNamespace string) string {
	sum := sha1.Sum([]byte(sourceNamespace))
	return fmt.Sprintf("%s-eso-%x", instanceName, sum[:4])
}

func uniqueSourceNamespaces(refs []platformv1alpha1.NamespacedObjectReference) []string {
	set := map[string]struct{}{}
	for _, ref := range refs {
		if ref.Namespace == "" {
			continue
		}
		set[ref.Namespace] = struct{}{}
	}
	namespaces := make([]string, 0, len(set))
	for namespace := range set {
		namespaces = append(namespaces, namespace)
	}
	sort.Strings(namespaces)
	return namespaces
}

func externalSecretProvider(policy platformv1alpha1.SecretPolicySpec) platformv1alpha1.ExternalSecretProviderType {
	if policy.ExternalSecretProvider == "" {
		return platformv1alpha1.ExternalSecretProviderKubernetes
	}
	return policy.ExternalSecretProvider
}

func vaultRemoteSecretKey(mountPath, secretName string) string {
	mountPath = strings.Trim(strings.TrimSpace(mountPath), "/")
	secretName = strings.Trim(strings.TrimSpace(secretName), "/")
	if mountPath == "" {
		return secretName
	}
	if secretName == "" {
		return mountPath
	}
	return mountPath + "/" + secretName
}
