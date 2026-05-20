package materializer

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/sindef/servicer/internal/adapters"
)

func TestMaterializeWritesDeterministicPackage(t *testing.T) {
	root := t.TempDir()
	m := New(root)
	request := Request{
		PackagePath: "clusters/east-1/tenants/acme/projects/acme-prod/services/orders-db",
		Artifacts: []adapters.RenderedArtifact{
			{Path: "clusters/east-1/tenants/acme/projects/acme-prod/services/orders-db/cnpg-cluster.yaml", Content: []byte("kind: Cluster\n")},
			{Path: "clusters/east-1/tenants/acme/projects/acme-prod/services/orders-db/namespace.yaml", Content: []byte("kind: Namespace\n")},
		},
	}

	first, err := m.Materialize(context.Background(), request)
	if err != nil {
		t.Fatalf("Materialize returned error: %v", err)
	}
	second, err := m.Materialize(context.Background(), request)
	if err != nil {
		t.Fatalf("Materialize returned error on second run: %v", err)
	}
	if first.Revision == "" || first.Revision != second.Revision {
		t.Fatalf("expected stable non-empty revision, got first=%q second=%q", first.Revision, second.Revision)
	}
	if first.Path != request.PackagePath {
		t.Fatalf("expected package path %q, got %q", request.PackagePath, first.Path)
	}
	if len(first.Artifacts) != 2 {
		t.Fatalf("expected 2 materialized artifact records, got %d", len(first.Artifacts))
	}

	clusterPath := filepath.Join(root, "clusters/east-1/tenants/acme/projects/acme-prod/services/orders-db/cnpg-cluster.yaml")
	content, err := os.ReadFile(clusterPath)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	if string(content) != "kind: Cluster\n" {
		t.Fatalf("unexpected materialized content:\n%s", string(content))
	}
}

func TestMaterializeRejectsArtifactsOutsidePackage(t *testing.T) {
	m := New(t.TempDir())
	_, err := m.Materialize(context.Background(), Request{
		PackagePath: "clusters/east-1/tenants/acme/projects/acme-prod/services/orders-db",
		Artifacts: []adapters.RenderedArtifact{
			{Path: "clusters/east-1/other.yaml", Content: []byte("kind: ConfigMap\n")},
		},
	})
	if err == nil {
		t.Fatalf("expected error for artifact outside package")
	}
}

func TestMaterializeWritesMultipleDeterministicPackages(t *testing.T) {
	root := t.TempDir()
	m := New(root)
	request := Request{
		PackagePath: "clusters/east-1/tenants/acme/projects/acme-prod/services/session-cache",
		PackagePaths: []string{
			"clusters/west-2/tenants/acme/projects/acme-prod/services/session-cache",
			"clusters/east-1/tenants/acme/projects/acme-prod/services/session-cache",
		},
		Artifacts: []adapters.RenderedArtifact{
			{Path: "clusters/east-1/tenants/acme/projects/acme-prod/services/session-cache/statefulset.yaml", Content: []byte("role: primary\n")},
			{Path: "clusters/west-2/tenants/acme/projects/acme-prod/services/session-cache/statefulset.yaml", Content: []byte("role: standby\n")},
		},
	}

	result, err := m.Materialize(context.Background(), request)
	if err != nil {
		t.Fatalf("Materialize returned error: %v", err)
	}
	if result.Path != request.PackagePath {
		t.Fatalf("expected primary package path %q, got %q", request.PackagePath, result.Path)
	}
	if len(result.Artifacts) != 2 {
		t.Fatalf("expected 2 materialized artifacts, got %d", len(result.Artifacts))
	}
	for _, path := range []string{
		"clusters/east-1/tenants/acme/projects/acme-prod/services/session-cache/statefulset.yaml",
		"clusters/west-2/tenants/acme/projects/acme-prod/services/session-cache/statefulset.yaml",
	} {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(path))); err != nil {
			t.Fatalf("expected materialized file %q: %v", path, err)
		}
	}
}
