package deliveryrepo

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sindef/servicer/internal/adapters"
)

func TestPublisherWritesArtifactsIntoRepoRoot(t *testing.T) {
	worktree := initGitRepo(t)
	publisher := New(worktree, "published", false)

	result, err := publisher.Publish(context.Background(), Request{
		PackagePath: "clusters/local-dev/tenants/acme/projects/acme-prod/services/orders-db",
		Artifacts: []adapters.RenderedArtifact{
			{
				Path:    "clusters/local-dev/tenants/acme/projects/acme-prod/services/orders-db/service.yaml",
				Content: []byte("kind: Service\n"),
			},
		},
		Revision: "sha256:test",
	})
	if err != nil {
		t.Fatalf("Publish returned error: %v", err)
	}
	if result.PublishedPath != "published/clusters/local-dev/tenants/acme/projects/acme-prod/services/orders-db" {
		t.Fatalf("unexpected published path %q", result.PublishedPath)
	}

	content, err := os.ReadFile(filepath.Join(worktree, "published/clusters/local-dev/tenants/acme/projects/acme-prod/services/orders-db/service.yaml"))
	if err != nil {
		t.Fatalf("read published artifact: %v", err)
	}
	if string(content) != "kind: Service\n" {
		t.Fatalf("unexpected content %q", string(content))
	}
}

func TestPublisherCommitsChangesWhenEnabled(t *testing.T) {
	worktree := initGitRepo(t)
	configureGitIdentity(t, worktree)
	publisher := New(worktree, "published", true)

	result, err := publisher.Publish(context.Background(), Request{
		PackagePath: "clusters/local-dev/tenants/acme/projects/acme-prod/services/orders-db",
		Artifacts: []adapters.RenderedArtifact{
			{
				Path:    "clusters/local-dev/tenants/acme/projects/acme-prod/services/orders-db/service.yaml",
				Content: []byte("kind: Service\n"),
			},
		},
		Revision: "sha256:test",
	})
	if err != nil {
		t.Fatalf("Publish returned error: %v", err)
	}
	if strings.TrimSpace(result.Commit) == "" {
		t.Fatalf("expected commit hash, got %#v", result)
	}
}

func initGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init")
	return dir
}

func configureGitIdentity(t *testing.T, dir string) {
	t.Helper()
	runGit(t, dir, "config", "user.email", "servicer@example.com")
	runGit(t, dir, "config", "user.name", "Servicer")
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, string(output))
	}
}
