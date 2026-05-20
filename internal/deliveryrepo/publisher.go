package deliveryrepo

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/sindef/servicer/internal/adapters"
)

type Publisher struct {
	Worktree   string
	Root       string
	AutoCommit bool
	AutoPush   bool
	Remote     string
	Branch     string
}

type Request struct {
	PackagePath  string
	PackagePaths []string
	Artifacts    []adapters.RenderedArtifact
	Revision     string
	Message      string
}

type Result struct {
	PublishedPath string
	Commit        string
	Pushed        bool
	Remote        string
	Branch        string
}

func New(worktree, root string, autoCommit, autoPush bool, remote, branch string) *Publisher {
	return &Publisher{
		Worktree:   filepath.Clean(strings.TrimSpace(worktree)),
		Root:       filepath.Clean(strings.TrimSpace(root)),
		AutoCommit: autoCommit,
		AutoPush:   autoPush,
		Remote:     firstNonEmptyTrimmed(remote, "origin"),
		Branch:     strings.TrimSpace(branch),
	}
}

func (p *Publisher) Enabled() bool {
	return strings.TrimSpace(p.Worktree) != ""
}

func (p *Publisher) Publish(ctx context.Context, request Request) (Result, error) {
	if !p.Enabled() {
		return Result{}, nil
	}
	if len(request.Artifacts) == 0 {
		return Result{}, fmt.Errorf("at least one artifact is required")
	}

	root := strings.Trim(filepath.ToSlash(strings.TrimSpace(p.Root)), "/")
	publishedPath := request.PackagePath
	if root != "" && root != "." {
		publishedPath = filepath.ToSlash(filepath.Join(root, filepath.FromSlash(request.PackagePath)))
	}

	for _, packagePath := range uniquePackagePaths(request.PackagePath, request.PackagePaths) {
		target := packagePath
		if root != "" && root != "." {
			target = filepath.ToSlash(filepath.Join(root, filepath.FromSlash(packagePath)))
		}
		if err := os.RemoveAll(filepath.Join(p.Worktree, filepath.FromSlash(target))); err != nil {
			return Result{}, fmt.Errorf("clean published package %q: %w", target, err)
		}
	}

	for _, artifact := range request.Artifacts {
		relativePath := artifact.Path
		if root != "" && root != "." {
			relativePath = filepath.ToSlash(filepath.Join(root, filepath.FromSlash(artifact.Path)))
		}
		fullPath := filepath.Join(p.Worktree, filepath.FromSlash(relativePath))
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
			return Result{}, fmt.Errorf("create publish directory: %w", err)
		}
		if err := writeFileIfChanged(fullPath, artifact.Content); err != nil {
			return Result{}, err
		}
	}

	result := Result{PublishedPath: publishedPath}
	if !p.AutoCommit {
		return result, nil
	}
	changed, err := p.hasChanges(ctx)
	if err != nil {
		return Result{}, err
	}
	if !changed {
		commit, err := p.currentCommit(ctx)
		if err != nil {
			return result, nil
		}
		result.Commit = commit
		return p.pushIfEnabled(ctx, result)
	}
	if err := p.git(ctx, "add", "--all", "--", "."); err != nil {
		return Result{}, err
	}
	if err := p.git(ctx, "commit", "-m", p.commitMessage(request)); err != nil {
		return Result{}, err
	}
	commit, err := p.currentCommit(ctx)
	if err != nil {
		return Result{}, err
	}
	result.Commit = commit
	return p.pushIfEnabled(ctx, result)
}

func (p *Publisher) hasChanges(ctx context.Context) (bool, error) {
	output, err := p.gitOutput(ctx, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(output) != "", nil
}

func (p *Publisher) currentCommit(ctx context.Context) (string, error) {
	output, err := p.gitOutput(ctx, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(output), nil
}

func (p *Publisher) currentBranch(ctx context.Context) (string, error) {
	output, err := p.gitOutput(ctx, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", err
	}
	branch := strings.TrimSpace(output)
	if branch == "" || branch == "HEAD" {
		return "", fmt.Errorf("git current branch: detached HEAD")
	}
	return branch, nil
}

func (p *Publisher) commitMessage(request Request) string {
	message := strings.TrimSpace(request.Message)
	if message != "" {
		return message
	}
	if request.Revision != "" {
		return fmt.Sprintf("servicer: publish %s", request.Revision)
	}
	return "servicer: publish delivery artifacts"
}

func (p *Publisher) pushIfEnabled(ctx context.Context, result Result) (Result, error) {
	if !p.AutoPush {
		return result, nil
	}
	if strings.TrimSpace(result.Commit) == "" {
		return result, fmt.Errorf("cannot push delivery repo without a commit")
	}
	branch := p.Branch
	if branch == "" {
		currentBranch, err := p.currentBranch(ctx)
		if err != nil {
			return result, err
		}
		branch = currentBranch
	}
	if err := p.git(ctx, "push", p.Remote, "HEAD:"+branch); err != nil {
		return result, err
	}
	result.Pushed = true
	result.Remote = p.Remote
	result.Branch = branch
	return result, nil
}

func (p *Publisher) git(ctx context.Context, args ...string) error {
	_, err := p.gitOutput(ctx, args...)
	return err
}

func (p *Publisher) gitOutput(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = p.Worktree
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = strings.TrimSpace(stdout.String())
		}
		if message == "" {
			message = err.Error()
		}
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), message)
	}
	return stdout.String(), nil
}

func uniquePackagePaths(primary string, extra []string) []string {
	seen := map[string]struct{}{}
	paths := make([]string, 0, len(extra)+1)
	appendPath := func(path string) {
		path = filepath.ToSlash(strings.TrimSpace(path))
		if path == "" {
			return
		}
		if _, ok := seen[path]; ok {
			return
		}
		seen[path] = struct{}{}
		paths = append(paths, path)
	}
	appendPath(primary)
	for _, path := range extra {
		appendPath(path)
	}
	return paths
}

func firstNonEmptyTrimmed(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func writeFileIfChanged(path string, content []byte) error {
	existing, err := os.ReadFile(path)
	if err == nil && bytes.Equal(existing, content) {
		return nil
	}
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read existing artifact %q: %w", path, err)
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		return fmt.Errorf("write artifact %q: %w", path, err)
	}
	return nil
}
