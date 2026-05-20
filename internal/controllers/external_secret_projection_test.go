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
	sourceRefs := []platformv1alpha1.NamespacedObjectReference{
		{Name: "orders-db-app", Namespace: "demo-prod-orders"},
	}
	projectedRefs := projectedCredentialRefs(instance, sourceRefs)

	artifacts, err := renderExternalSecretArtifacts(instance, "clusters/local-dev/projects/demo/orders-db", sourceRefs, projectedRefs)
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
}
