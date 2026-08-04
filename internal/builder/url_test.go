package builder_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/scythe504/kronos/internal/builder"
)

const testGitUrl = "https://github.com/Scythe504/aniflux.git"

func createLocalTestGitRepo(t *testing.T) (string, func()) {
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

func TestGetRepoRef(t *testing.T) {
	ctx := t.Context()

	ref, err := builder.GetRepoRef(ctx, testGitUrl)
	if err != nil {
		t.Fatalf("GetRepoRef error = %v", err)
	}
	if ref == "" {
		t.Fatalf("expected non-empty repo ref")
	}
}

func TestGetRepoRef_InvalidURL(t *testing.T) {
	_, err := builder.GetRepoRef(t.Context(), "https://invalid-domain.example.com/repo.git")
	if err == nil {
		t.Fatalf("expected error for invalid git repo URL, got nil")
	}
}

func TestCloneRepo(t *testing.T) {
	ctx := t.Context()
	tempDir, err := os.MkdirTemp("", "kronos-test-clone-url-*")
	if err != nil {
		t.Fatalf("failed creating temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	err = builder.CloneRepo(ctx, testGitUrl, "main", tempDir)
	if err != nil {
		t.Fatalf("CloneRepo error = %v", err)
	}
}

func TestCloneRepo_InvalidURL(t *testing.T) {
	ctx := t.Context()
	tempDir, err := os.MkdirTemp("", "kronos-test-clone-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	err = builder.CloneRepo(ctx, "https://invalid-domain.example.com/repo.git", "main", tempDir)
	if err == nil {
		t.Fatalf("expected error when cloning invalid git repository, got nil")
	}
}

func TestCloneRepo_RealRepo(t *testing.T) {
	repoDir, cleanup := createLocalTestGitRepo(t)
	defer cleanup()

	targetDir, err := os.MkdirTemp("", "kronos-test-target-*")
	if err != nil {
		t.Fatalf("failed creating temp target dir: %v", err)
	}
	defer os.RemoveAll(targetDir)

	err = builder.CloneRepo(t.Context(), repoDir, "", targetDir)
	if err != nil {
		t.Fatalf("CloneRepo failed for real local repo: %v", err)
	}

	if _, err := os.Stat(filepath.Join(targetDir, "Dockerfile")); os.IsNotExist(err) {
		t.Fatalf("expected Dockerfile to be cloned into target directory")
	}
}

func TestFormatAuthenticatedURL(t *testing.T) {
	rawURL := "https://github.com/myorg/private-repo.git"

	// Case 1: Token only
	urlWithToken := builder.FormatAuthenticatedURL(rawURL, "", "ghp_secrettoken123")
	if urlWithToken != "https://ghp_secrettoken123@github.com/myorg/private-repo.git" {
		t.Fatalf("unexpected URL with token: %s", urlWithToken)
	}

	// Case 2: Username and password
	urlWithUserPass := builder.FormatAuthenticatedURL(rawURL, "myuser", "mypassword")
	if urlWithUserPass != "https://myuser:mypassword@github.com/myorg/private-repo.git" {
		t.Fatalf("unexpected URL with user/pass: %s", urlWithUserPass)
	}

	// Case 3: No credentials
	urlUnchanged := builder.FormatAuthenticatedURL(rawURL, "", "")
	if urlUnchanged != rawURL {
		t.Fatalf("expected unchanged URL, got: %s", urlUnchanged)
	}
}
