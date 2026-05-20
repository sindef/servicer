package materializer

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sindef/servicer/internal/adapters"
)

const DefaultRoot = "generated/delivery"

type Materializer struct {
	Root string
}

type Request struct {
	PackagePath  string
	PackagePaths []string
	Artifacts    []adapters.RenderedArtifact
}

type Result struct {
	Revision  string
	Path      string
	Artifacts []Artifact
}

type Artifact struct {
	Path   string
	Digest string
}

func New(root string) *Materializer {
	if root == "" {
		root = DefaultRoot
	}
	return &Materializer{Root: filepath.Clean(root)}
}

func (m *Materializer) Materialize(ctx context.Context, request Request) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if len(request.Artifacts) == 0 {
		return Result{}, fmt.Errorf("at least one artifact is required")
	}
	packagePaths, err := requestPackagePaths(request)
	if err != nil {
		return Result{}, err
	}
	primaryPackagePath := packagePaths[0]

	artifacts := append([]adapters.RenderedArtifact(nil), request.Artifacts...)
	sort.Slice(artifacts, func(i, j int) bool {
		return artifacts[i].Path < artifacts[j].Path
	})

	statusArtifacts := make([]Artifact, 0, len(artifacts))
	revisionHash := sha256.New()
	seen := map[string]struct{}{}

	for _, artifact := range artifacts {
		path, err := cleanRelativePath(artifact.Path)
		if err != nil {
			return Result{}, fmt.Errorf("artifact path %q: %w", artifact.Path, err)
		}
		if !pathInAnyPackage(path, packagePaths) {
			return Result{}, fmt.Errorf("artifact path %q is outside package paths %q", path, strings.Join(packagePaths, ", "))
		}
		if _, ok := seen[path]; ok {
			return Result{}, fmt.Errorf("duplicate artifact path %q", path)
		}
		seen[path] = struct{}{}

		digestBytes := sha256.Sum256(artifact.Content)
		digest := "sha256:" + hex.EncodeToString(digestBytes[:])
		statusArtifacts = append(statusArtifacts, Artifact{Path: path, Digest: digest})

		_, _ = revisionHash.Write([]byte(path))
		_, _ = revisionHash.Write([]byte{0})
		_, _ = revisionHash.Write(artifact.Content)
		_, _ = revisionHash.Write([]byte{0})
	}

	for _, packagePath := range packagePaths {
		packageDir := filepath.Join(m.Root, filepath.FromSlash(packagePath))
		if err := os.RemoveAll(packageDir); err != nil {
			return Result{}, fmt.Errorf("clean package directory %q: %w", packagePath, err)
		}
	}
	for _, artifact := range artifacts {
		if err := ctx.Err(); err != nil {
			return Result{}, err
		}
		path, _ := cleanRelativePath(artifact.Path)
		fullPath := filepath.Join(m.Root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
			return Result{}, fmt.Errorf("create artifact directory: %w", err)
		}
		if err := writeFileIfChanged(fullPath, artifact.Content); err != nil {
			return Result{}, err
		}
	}

	return Result{
		Revision:  "sha256:" + hex.EncodeToString(revisionHash.Sum(nil)),
		Path:      primaryPackagePath,
		Artifacts: statusArtifacts,
	}, nil
}

// Purge removes all materialized artifacts for the given package path.
func (m *Materializer) Purge(packagePath string) error {
	path, err := cleanRelativePath(packagePath)
	if err != nil {
		return fmt.Errorf("package path %q: %w", packagePath, err)
	}
	return os.RemoveAll(filepath.Join(m.Root, filepath.FromSlash(path)))
}

func requestPackagePaths(request Request) ([]string, error) {
	rawPaths := append([]string(nil), request.PackagePaths...)
	if request.PackagePath != "" {
		rawPaths = append(rawPaths, request.PackagePath)
	}
	if len(rawPaths) == 0 {
		return nil, fmt.Errorf("package path is required")
	}

	seen := map[string]struct{}{}
	paths := make([]string, 0, len(rawPaths))
	for _, rawPath := range rawPaths {
		path, err := cleanRelativePath(rawPath)
		if err != nil {
			return nil, fmt.Errorf("package path: %w", err)
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		paths = append(paths, path)
	}
	sort.Strings(paths)
	if request.PackagePath != "" {
		primary, err := cleanRelativePath(request.PackagePath)
		if err != nil {
			return nil, fmt.Errorf("package path: %w", err)
		}
		for i, path := range paths {
			if path == primary {
				paths[0], paths[i] = paths[i], paths[0]
				break
			}
		}
	}
	return paths, nil
}

func pathInAnyPackage(path string, packagePaths []string) bool {
	for _, packagePath := range packagePaths {
		if path == packagePath || strings.HasPrefix(path, packagePath+"/") {
			return true
		}
	}
	return false
}

func cleanRelativePath(path string) (string, error) {
	path = filepath.ToSlash(strings.TrimSpace(path))
	if path == "" || path == "." {
		return "", fmt.Errorf("path must not be empty")
	}
	if strings.HasPrefix(path, "/") {
		return "", fmt.Errorf("absolute paths are not allowed")
	}
	cleaned := filepath.ToSlash(filepath.Clean(path))
	if cleaned == "." || cleaned == "" {
		return "", fmt.Errorf("path must not be empty")
	}
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") || strings.Contains(cleaned, "/../") {
		return "", fmt.Errorf("path traversal is not allowed")
	}
	return cleaned, nil
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
