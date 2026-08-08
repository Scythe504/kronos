package builder

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/scythe504/kronos/internal/database"
	"github.com/scythe504/kronos/internal/telemetry"
)

// Manager manages worker workspace directories, build caching, and container compilation.
type Manager struct {
	config BuilderConfig
	tel    telemetry.TelemetryProvider
}

// NewManager initializes a Manager instance and creates the necessary build & cache directories.
func NewManager(cfg BuilderConfig, tel telemetry.TelemetryProvider) (*Manager, error) {
	if cfg.RootDir == "" {
		cfg = NewDefaultConfig()
	}

	if err := os.MkdirAll(cfg.RootDir, 0755); err != nil {
		return nil, fmt.Errorf("failed creating root build directory %s: %w", cfg.RootDir, err)
	}
	if err := os.MkdirAll(cfg.CacheDir, 0755); err != nil {
		return nil, fmt.Errorf("failed creating cache directory %s: %w", cfg.CacheDir, err)
	}

	return &Manager{
		config: cfg,
		tel:    tel,
	}, nil
}

// GetWorkspacePath generates a clean workspace directory path for a specific worker slug and cache key.
func (m *Manager) GetWorkspacePath(slug string, cacheKey string) string {
	return filepath.Join(m.config.RootDir, slug, cacheKey)
}

// IsImageCached inspects the local Docker daemon to check if the target image tag already exists.
func (m *Manager) IsImageCached(ctx context.Context, imageTag string) bool {
	if _, err := exec.LookPath("docker"); err != nil {
		return false
	}
	cmd := exec.CommandContext(ctx, "docker", "image", "inspect", imageTag)
	return cmd.Run() == nil
}

// Build handles workspace setup, repository cloning, container compilation, and cleanup.
func (m *Manager) Build(ctx context.Context, worker database.Worker) (*BuildWorkerResult, error) {
	if worker.RepoURL == "" {
		return nil, fmt.Errorf("worker %s has empty repo_url", worker.Slug)
	}

	cacheKey := ComputeBuildCacheKey(ctx, worker)
	imageTag := fmt.Sprintf("kronos-worker:%s-%s", worker.Slug, cacheKey)

	if m.IsImageCached(ctx, imageTag) {
		m.tel.LogInfo(ctx, "Build cache hit, skipping compilation", "slug", worker.Slug, "image", imageTag, "cache_key", cacheKey)
		return &BuildWorkerResult{
			ImageTag: imageTag,
			BuildDir: "",
			Cached:   true,
		}, nil
	}

	// Invalidate & remove old image versions for this slug since cache key changed
	m.PruneStaleImagesForSlug(ctx, worker.Slug, imageTag)

	workspaceDir := m.GetWorkspacePath(worker.Slug, cacheKey)
	if err := os.MkdirAll(workspaceDir, 0755); err != nil {
		return nil, fmt.Errorf("failed creating workspace directory %s: %w", workspaceDir, err)
	}

	m.tel.LogInfo(ctx, "Starting worker git clone", "slug", worker.Slug, "repo_url", worker.RepoURL, "repo_ref", worker.RepoRef, "target_dir", workspaceDir)

	user, token := resolveGitCredentials(worker.EnvVars)
	cloneURL := formatAuthenticatedURL(worker.RepoURL, user, token)

	if err := CloneRepo(ctx, cloneURL, worker.RepoRef, workspaceDir); err != nil {
		if !m.config.KeepBuildArtifacts {
			os.RemoveAll(workspaceDir)
		}
		return nil, err
	}

	res, err := executeContainerBuild(ctx, worker, imageTag, workspaceDir, m.tel)
	if err != nil {
		if !m.config.KeepBuildArtifacts {
			os.RemoveAll(workspaceDir)
		}
		return nil, err
	}

	if !m.config.KeepBuildArtifacts {
		os.RemoveAll(workspaceDir)
		res.BuildDir = ""
	}

	return res, nil
}

// PruneStaleImagesForSlug removes older Docker images for a worker slug when a new version is compiled.
func (m *Manager) PruneStaleImagesForSlug(ctx context.Context, slug string, currentImageTag string) {
	cmd := exec.CommandContext(ctx, "docker", "images", "--format", "{{.Repository}}:{{.Tag}}", "--filter",
		"reference=kronos-worker:"+slug+"-*")
	out, err := cmd.Output()
	if err != nil {
		return
	}
	for line := range strings.FieldsSeq(string(out)) {
		if line == currentImageTag {
			continue
		}
		rmCmd := exec.CommandContext(ctx, "docker", "rmi", "-f", line)
		if err := rmCmd.Run(); err == nil {
			m.tel.LogInfo(ctx, "Invalidated old worker image cache", "image", line)
		}
	}
}

// PruneRemovedSlugs removes Docker images and workspace directories for any worker
// slug that is no longer present in the provided allowedSlugs set.
func (m *Manager) PruneRemovedSlugs(ctx context.Context, allowedSlugs []string) {
	allowed := make(map[string]struct{}, len(allowedSlugs))
	for _, s := range allowedSlugs {
		allowed[s] = struct{}{}
	}

	entries, err := os.ReadDir(m.config.RootDir)
	if err != nil {
		return
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		slug := entry.Name()
		if _, ok := allowed[slug]; ok {
			continue
		}

		// Remove workspace directory for the evicted slug.
		slugDir := filepath.Join(m.config.RootDir, slug)
		if err := os.RemoveAll(slugDir); err != nil {
			m.tel.LogErrorln(ctx, "Failed to prune workspace dir for removed slug", "slug", slug, "error", err)
		} else {
			m.tel.LogInfo(ctx, "Pruned workspace dir for removed slug", "slug", slug)
		}

		// Remove associated Docker images (kronos-worker:<slug>-*).
		cmd := exec.CommandContext(ctx, "docker", "images", "--format", "{{.Repository}}:{{.Tag}}", "--filter",
			"reference=kronos-worker:"+slug+"-*")
		out, err := cmd.Output()
		if err != nil {
			continue
		}
		for line := range strings.FieldsSeq(string(out)) {
			rmCmd := exec.CommandContext(ctx, "docker", "rmi", "-f", line)
			if err := rmCmd.Run(); err != nil {
				m.tel.LogErrorln(ctx, "Failed to remove Docker image for removed slug", "image", line, "error", err)
			} else {
				m.tel.LogInfo(ctx, "Removed Docker image for evicted slug", "image", line)
			}
		}
	}
}
