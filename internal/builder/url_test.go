package builder

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing/object"
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

func TestFetchRemoteHeadCommit(t *testing.T) {
	ctx := t.Context()

	commitSHA := FetchRemoteHeadCommit(ctx, testGitUrl, "main")
	if commitSHA == "" {
		t.Fatalf("expected non-empty commit SHA")
	}
}

func TestFetchRemoteHeadCommit_InvalidURL(t *testing.T) {
	sha := FetchRemoteHeadCommit(t.Context(), "https://invalid-domain.example.com/repo.git", "main")
	if sha != "" {
		t.Fatalf("expected empty SHA for invalid git repo URL")
	}
}

func TestCloneRepo(t *testing.T) {
	ctx := t.Context()
	tempDir, err := os.MkdirTemp("", "kronos-test-clone-url-*")
	if err != nil {
		t.Fatalf("failed creating temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	err = CloneRepo(ctx, testGitUrl, "main", tempDir)
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

	err = CloneRepo(ctx, "https://invalid-domain.example.com/repo.git", "main", tempDir)
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

	err = CloneRepo(t.Context(), repoDir, "", targetDir)
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
	urlWithToken := formatAuthenticatedURL(rawURL, "", "ghp_secrettoken123")
	if urlWithToken != "https://ghp_secrettoken123@github.com/myorg/private-repo.git" {
		t.Fatalf("unexpected URL with token: %s", urlWithToken)
	}

	// Case 2: Username and password
	urlWithUserPass := formatAuthenticatedURL(rawURL, "myuser", "mypassword")
	if urlWithUserPass != "https://myuser:mypassword@github.com/myorg/private-repo.git" {
		t.Fatalf("unexpected URL with user/pass: %s", urlWithUserPass)
	}

	// Case 3: No credentials
	urlUnchanged := formatAuthenticatedURL(rawURL, "", "")
	if urlUnchanged != rawURL {
		t.Fatalf("expected unchanged URL, got: %s", urlUnchanged)
	}
}
