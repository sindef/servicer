package controllers

import (
	"strings"
	"testing"

	platformv1alpha1 "github.com/sindef/servicer/api/v1alpha1"
)

func TestProjectedCredentialRefs(t *testing.T) {
	instance := &platformv1alpha1.ServiceInstance{}
	instance.Status.Placement.Namespace = "demo-prod-orders"
	sourceRefs := []platformv1alpha1.NamespacedObjectReference{
		{Name: "orders-db-app", Namespace: "demo-prod-orders"},
	}

	projected := projectedCredentialRefs(instance, sourceRefs)
	if len(projected) != 1 {
		t.Fatalf("expected 1 projected ref, got %d", len(projected))
	}
	if projected[0].Name != "orders-db-app-projected" {
		t.Fatalf("expected projected name, got %#v", projected[0])
	}
	if projected[0].Namespace != "demo-prod-orders" {
		t.Fatalf("expected projected namespace, got %#v", projected[0])
	}
}

func TestRenderExternalSecretArtifacts(t *testing.T) {
	instance := &platformv1alpha1.ServiceInstance{}
	instance.Name = "orders-db"
	instance.Status.Placement.Namespace = "demo-prod-orders"
	instance.Spec.SecretPolicy.DeliveryMode = platformv1alpha1.SecretDeliveryModeExternalSecret
	sourceRefs := []platformv1alpha1.NamespacedObjectReference{
		{Name: "orders-db-app", Namespace: "demo-prod-orders"},
	}
	projectedRefs := projectedCredentialRefs(instance, sourceRefs)

	artifacts, err := renderExternalSecretArtifacts(instance, "clusters/local-dev/projects/demo/orders-db", sourceRefs, projectedRefs, map[string][]string{
		"demo-prod-orders/orders-db-app": {"username", "password"},
	})
	if err != nil {
		t.Fatalf("renderExternalSecretArtifacts returned error: %v", err)
	}
	if len(artifacts) < 5 {
		t.Fatalf("expected at least 5 artifacts, got %d", len(artifacts))
	}

	joined := make([]string, 0, len(artifacts))
	for _, artifact := range artifacts {
		joined = append(joined, artifact.Path+"\n"+string(artifact.Content))
	}
	all := strings.Join(joined, "\n---\n")
	if !strings.Contains(all, "kind: ExternalSecret") {
		t.Fatalf("expected ExternalSecret manifest, got %s", all)
	}
	if !strings.Contains(all, "kind: SecretStore") {
		t.Fatalf("expected SecretStore manifest, got %s", all)
	}
	if !strings.Contains(all, "kind: ServiceAccount") {
		t.Fatalf("expected ServiceAccount manifest, got %s", all)
	}
	if !strings.Contains(all, "orders-db-app-projected") {
		t.Fatalf("expected projected target secret name, got %s", all)
	}
	if !strings.Contains(all, "remoteNamespace: demo-prod-orders") {
		t.Fatalf("expected source namespace in SecretStore, got %s", all)
	}
	if !strings.Contains(all, "property: username") || !strings.Contains(all, "property: password") {
		t.Fatalf("expected explicit property mappings, got %s", all)
	}
}

func TestRenderExternalSecretArtifactsVaultProvider(t *testing.T) {
	instance := &platformv1alpha1.ServiceInstance{}
	instance.Name = "orders-db"
	instance.Status.Placement.Namespace = "demo-prod-orders"
	instance.Spec.SecretPolicy = platformv1alpha1.SecretPolicySpec{
		DeliveryMode:           platformv1alpha1.SecretDeliveryModeExternalSecret,
		ExternalSecretProvider: platformv1alpha1.ExternalSecretProviderVault,
		Vault: &platformv1alpha1.VaultSecretProviderSpec{
			Server:  "https://vault.example.com",
			Path:    "kv/apps",
			Version: "v2",
			AuthSecretRef: platformv1alpha1.NamespacedObjectReference{
				Name:      "vault-token",
				Namespace: "demo-prod-orders",
			},
		},
	}
	sourceRefs := []platformv1alpha1.NamespacedObjectReference{
		{Name: "orders-db-app", Namespace: "demo-prod-orders"},
	}
	projectedRefs := projectedCredentialRefs(instance, sourceRefs)

	artifacts, err := renderExternalSecretArtifacts(instance, "clusters/local-dev/projects/demo/orders-db", sourceRefs, projectedRefs, nil)
	if err != nil {
		t.Fatalf("renderExternalSecretArtifacts returned error: %v", err)
	}
	if len(artifacts) != 2 {
		t.Fatalf("expected 2 artifacts for vault projection, got %d", len(artifacts))
	}
	joined := make([]string, 0, len(artifacts))
	for _, artifact := range artifacts {
		joined = append(joined, artifact.Path+"\n"+string(artifact.Content))
	}
	all := strings.Join(joined, "\n---\n")
	if !strings.Contains(all, "vault:") || !strings.Contains(all, "https://vault.example.com") {
		t.Fatalf("expected vault provider config, got %s", all)
	}
	if !strings.Contains(all, "kv/apps/orders-db-app") {
		t.Fatalf("expected vault remote key, got %s", all)
	}
	if strings.Contains(all, "kind: ServiceAccount") {
		t.Fatalf("did not expect kubernetes-provider artifacts in vault mode: %s", all)
	}
}
