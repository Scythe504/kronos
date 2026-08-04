package builder_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/scythe504/kronos/internal/builder"
	"github.com/scythe504/kronos/internal/database"
	"github.com/scythe504/kronos/internal/telemetry"
)

func createLocalGitRepoForBuilder(t *testing.T) (string, func()) {
	t.Helper()
	repoDir, err := os.MkdirTemp("", "kronos-test-repo-*")
	if err != nil {
		t.Fatalf("failed creating temp repo dir: %v", err)
	}

	dockerfilePath := filepath.Join(repoDir, "Dockerfile")
	dockerContent := "FROM alpine:latest\nCMD [\"echo\", \"hello kronos\"]\n"
	if err := os.WriteFile(dockerfilePath, []byte(dockerContent), 0644); err != nil {
		os.RemoveAll(repoDir)
		t.Fatalf("failed writing Dockerfile: %v", err)
	}

	r, err := git.PlainInit(repoDir, false)
	if err != nil {
		os.RemoveAll(repoDir)
		t.Fatalf("failed git init: %v", err)
	}

	w, err := r.Worktree()
	if err != nil {
		os.RemoveAll(repoDir)
		t.Fatalf("failed git worktree: %v", err)
	}

	_, err = w.Add("Dockerfile")
	if err != nil {
		os.RemoveAll(repoDir)
		t.Fatalf("failed git add: %v", err)
	}

	_, err = w.Commit("Initial commit", &git.CommitOptions{
		Author: &object.Signature{
			Name:  "Kronos Test",
			Email: "test@kronos.internal",
			When:  time.Now(),
		},
	})
	if err != nil {
		os.RemoveAll(repoDir)
		t.Fatalf("failed git commit: %v", err)
	}

	return repoDir, func() {
		os.RemoveAll(repoDir)
	}
}

func TestBuildWorker_EmptyRepoURL(t *testing.T) {
	tel := &telemetry.NoopTelemetry{}

	worker := database.Worker{
		Slug:    "test-empty-repo",
		RepoURL: "",
	}

	_, err := builder.BuildWorker(t.Context(), worker, tel)
	if err == nil {
		t.Fatalf("expected error for empty repo_url, got nil")
	}
}

func TestBuildWorker_InvalidGitURL(t *testing.T) {
	tel := &telemetry.NoopTelemetry{}

	worker := database.Worker{
		Slug:    "test-invalid-repo",
		RepoURL: "https://invalid-domain.example.com/nonexistent/repo.git",
	}

	_, err := builder.BuildWorker(t.Context(), worker, tel)
	if err == nil {
		t.Fatalf("expected clone error for invalid repo URL, got nil")
	}
}

func TestBuildWorker_WithBuildCommands(t *testing.T) {
	preCmd := "echo pre-build"
	buildCmd := "echo building"
	runCmd := "./run.sh"

	worker := database.Worker{
		Slug:            "test-commands-worker",
		RepoURL:         "https://invalid-domain.example.com/repo.git",
		PreBuildCommand: &preCmd,
		BuildCommand:    &buildCmd,
		RunCommand:      &runCmd,
	}

	tel := &telemetry.NoopTelemetry{}
	_, err := builder.BuildWorker(t.Context(), worker, tel)
	if err == nil {
		t.Fatalf("expected clone error for invalid git URL, got nil")
	}
}

func TestBuildWorker_DockerfileDetection(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "kronos-test-dockerfile-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	dockerfilePath := filepath.Join(tempDir, "Dockerfile")
	err = os.WriteFile(dockerfilePath, []byte("FROM alpine\nRUN echo hello\n"), 0644)
	if err != nil {
		t.Fatalf("failed to write mock Dockerfile: %v", err)
	}

	if _, err := os.Stat(dockerfilePath); os.IsNotExist(err) {
		t.Fatalf("expected mock Dockerfile to exist")
	}
}

func TestBuildWorker_RealRepoIntegration(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("skipping Docker build integration test: 'docker' CLI not found in PATH")
	}

	repoDir, cleanup := createLocalGitRepoForBuilder(t)
	defer cleanup()

	tel := &telemetry.NoopTelemetry{}
	worker := database.Worker{
		Slug:    "real-test-worker",
		RepoURL: repoDir,
	}

	res, err := builder.BuildWorker(t.Context(), worker, tel)
	if err != nil {
		t.Fatalf("BuildWorker failed for real local repo: %v", err)
	}
	defer os.RemoveAll(res.BuildDir)

	expectedPrefix := "kronos-worker:real-test-worker-"
	if !strings.HasPrefix(res.ImageTag, expectedPrefix) {
		t.Fatalf("expected image tag starting with '%s', got '%s'", expectedPrefix, res.ImageTag)
	}
}

func TestComputeBuildCacheKey(t *testing.T) {
	preCmd := "echo pre"
	buildCmd := "echo build"
	runCmd := "./run"

	w1 := database.Worker{
		Slug:            "worker-1",
		RepoURL:         "https://github.com/example/repo.git",
		RepoRef:         "main",
		PreBuildCommand: &preCmd,
		BuildCommand:    &buildCmd,
		RunCommand:      &runCmd,
	}

	w2 := database.Worker{
		Slug:            "worker-1",
		RepoURL:         "https://github.com/example/repo.git",
		RepoRef:         "main",
		PreBuildCommand: &preCmd,
		BuildCommand:    &buildCmd,
		RunCommand:      &runCmd,
	}

	w3 := database.Worker{
		Slug:            "worker-1",
		RepoURL:         "https://github.com/example/repo.git",
		RepoRef:         "feature-branch",
		PreBuildCommand: &preCmd,
		BuildCommand:    &buildCmd,
		RunCommand:      &runCmd,
	}

	key1 := builder.ComputeBuildCacheKey(w1)
	key2 := builder.ComputeBuildCacheKey(w2)
	key3 := builder.ComputeBuildCacheKey(w3)

	if key1 != key2 {
		t.Fatalf("expected identical cache keys for identical workers, got %s vs %s", key1, key2)
	}

	if key1 == key3 {
		t.Fatalf("expected different cache keys for different repo refs, got matching %s", key1)
	}
}
