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
	publisher := New(worktree, "published", false, false, "", "")

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
	publisher := New(worktree, "published", true, false, "", "")

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

func TestPublisherPushesCommittedChangesToRemote(t *testing.T) {
	worktree := initGitRepo(t)
	configureGitIdentity(t, worktree)
	remote := filepath.Join(t.TempDir(), "delivery-remote.git")
	runGit(t, "", "init", "--bare", remote)
	runGit(t, worktree, "remote", "add", "origin", remote)

	publisher := New(worktree, "published", true, true, "origin", "main")
	result, err := publisher.Publish(context.Background(), Request{
		PackagePath: "clusters/local-dev/tenants/acme/projects/acme-prod/services/orders-db",
		Artifacts: []adapters.RenderedArtifact{{
			Path:    "clusters/local-dev/tenants/acme/projects/acme-prod/services/orders-db/service.yaml",
			Content: []byte("kind: Service\n"),
		}},
		Revision: "sha256:test",
	})
	if err != nil {
		t.Fatalf("Publish returned error: %v", err)
	}
	if !result.Pushed || result.Remote != "origin" || result.Branch != "main" {
		t.Fatalf("expected pushed result, got %#v", result)
	}

	output := gitOutput(t, "", "--git-dir", remote, "rev-parse", "refs/heads/main")
	if strings.TrimSpace(output) != strings.TrimSpace(result.Commit) {
		t.Fatalf("expected remote branch to match pushed commit, got %q want %q", strings.TrimSpace(output), strings.TrimSpace(result.Commit))
	}
}

func TestPublisherClonesEmptyWorktreeWhenRepoURLConfigured(t *testing.T) {
	remote := filepath.Join(t.TempDir(), "delivery-remote.git")
	runGit(t, "", "init", "--bare", remote)
	seed := initGitRepo(t)
	configureGitIdentity(t, seed)
	if err := os.WriteFile(filepath.Join(seed, "README.md"), []byte("seed\n"), 0o644); err != nil {
		t.Fatalf("write seed file: %v", err)
	}
	runGit(t, seed, "add", "README.md")
	runGit(t, seed, "commit", "-m", "seed")
	runGit(t, seed, "branch", "-M", "main")
	runGit(t, seed, "remote", "add", "origin", remote)
	runGit(t, seed, "push", "origin", "main")

	worktree := filepath.Join(t.TempDir(), "worktree")
	publisher := NewWithRepoURL(worktree, "published", true, true, remote, "origin", "main", "Servicer Bot", "servicer@example.com")
	result, err := publisher.Publish(context.Background(), Request{
		PackagePath: "clusters/local-dev/tenants/acme/projects/acme-prod/services/orders-db",
		Artifacts: []adapters.RenderedArtifact{{
			Path:    "clusters/local-dev/tenants/acme/projects/acme-prod/services/orders-db/service.yaml",
			Content: []byte("kind: Service\n"),
		}},
		Revision: "sha256:test",
	})
	if err != nil {
		t.Fatalf("Publish returned error: %v", err)
	}
	if !result.Pushed {
		t.Fatalf("expected cloned worktree push, got %#v", result)
	}
	if _, err := os.Stat(filepath.Join(worktree, ".git")); err != nil {
		t.Fatalf("expected cloned git worktree: %v", err)
	}
}

func TestPublisherRejectsNonGitNonEmptyWorktree(t *testing.T) {
	worktree := t.TempDir()
	if err := os.WriteFile(filepath.Join(worktree, "leftover.txt"), []byte("old\n"), 0o644); err != nil {
		t.Fatalf("write leftover: %v", err)
	}
	publisher := NewWithRepoURL(worktree, "published", true, true, "https://example.invalid/repo.git", "origin", "main", "Servicer", "servicer@example.com")
	_, err := publisher.Publish(context.Background(), Request{
		PackagePath: "clusters/local-dev/tenants/acme/projects/acme-prod/services/orders-db",
		Artifacts: []adapters.RenderedArtifact{{
			Path:    "clusters/local-dev/tenants/acme/projects/acme-prod/services/orders-db/service.yaml",
			Content: []byte("kind: Service\n"),
		}},
	})
	if err == nil || !strings.Contains(err.Error(), "not a git repository and is not empty") {
		t.Fatalf("expected non-git worktree error, got %v", err)
	}
}

func TestPublisherReportsUnavailableRemote(t *testing.T) {
	worktree := filepath.Join(t.TempDir(), "worktree")
	missingRemote := filepath.Join(t.TempDir(), "missing.git")
	publisher := NewWithRepoURL(worktree, "published", true, true, "file://"+missingRemote, "origin", "main", "Servicer", "servicer@example.com")
	_, err := publisher.Publish(context.Background(), Request{
		PackagePath: "clusters/local-dev/tenants/acme/projects/acme-prod/services/orders-db",
		Artifacts: []adapters.RenderedArtifact{{
			Path:    "clusters/local-dev/tenants/acme/projects/acme-prod/services/orders-db/service.yaml",
			Content: []byte("kind: Service\n"),
		}},
	})
	if err == nil || !strings.Contains(err.Error(), "git clone") {
		t.Fatalf("expected clone error for unavailable remote, got %v", err)
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

func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, string(output))
	}
	return string(output)
}
