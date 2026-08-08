package builder

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/scythe504/kronos/internal/database"
	"github.com/scythe504/kronos/internal/telemetry"
)

// BuildWorkerResult contains the output image tag and build directory reference of a completed build.
type BuildWorkerResult struct {
	ImageTag string
	BuildDir string
	Cached   bool
}

// ComputeBuildCacheKey generates a deterministic SHA-256 hash representing a worker build configuration.
func ComputeBuildCacheKey(ctx context.Context, worker database.Worker) string {
	h := sha256.New()
	h.Write([]byte(worker.RepoURL))
	h.Write([]byte("|"))
	h.Write([]byte(worker.RepoRef))
	h.Write([]byte("|"))

	// Query remote Git HEAD SHA to invalidate cache whenever new commits are pushed
	user, token := resolveGitCredentials(worker.EnvVars)
	authURL := formatAuthenticatedURL(worker.RepoURL, user, token)
	remoteCommitSHA := FetchRemoteHeadCommit(ctx, authURL, worker.RepoRef)
	if remoteCommitSHA != "" {
		h.Write([]byte(remoteCommitSHA))
	}
	h.Write([]byte("|"))

	if len(worker.EnvVars) > 0 {
		h.Write(worker.EnvVars)
	}
	h.Write([]byte("|"))
	if worker.DockerfilePath != nil {
		h.Write([]byte(*worker.DockerfilePath))
	}
	h.Write([]byte("|"))
	h.Write([]byte(worker.UpdatedAt.Format("2006-01-02T15:04:05.999999999Z07:00")))
	hashBytes := h.Sum(nil)
	return hex.EncodeToString(hashBytes)[:16]
}

// resolveGitCredentials parses plaintext environment variables for private git credentials.
// env_vars are pre-decrypted by the Master server before being served to the orchestrator.
func resolveGitCredentials(envVars []byte) (string, string) {
	if len(envVars) == 0 {
		return "", ""
	}

	var envMap map[string]string
	if err := json.Unmarshal(envVars, &envMap); err != nil {
		envStr := string(envVars)
		for line := range strings.SplitSeq(envStr, "\n") {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				k, v := strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
				if k == "GIT_TOKEN" || k == "GITHUB_TOKEN" {
					return "", v
				}
			}
		}
		return "", ""
	}

	if token, ok := envMap["GITHUB_TOKEN"]; ok && token != "" {
		return "", token
	}
	if token, ok := envMap["GIT_TOKEN"]; ok && token != "" {
		return "", token
	}
	if user, ok := envMap["GIT_USERNAME"]; ok && user != "" {
		pass := envMap["GIT_PASSWORD"]
		return user, pass
	}

	return "", ""
}

// executeContainerBuild compiles a container image using Dockerfile only.
func executeContainerBuild(ctx context.Context, worker database.Worker, imageTag string, appPath string, tel telemetry.TelemetryProvider) (*BuildWorkerResult, error) {
	dockerfilePath := ""
	if worker.DockerfilePath != nil && *worker.DockerfilePath != "" {
		dockerfilePath = filepath.Join(appPath, *worker.DockerfilePath)
	} else if _, err := os.Stat(filepath.Join(appPath, "Dockerfile")); err == nil {
		dockerfilePath = filepath.Join(appPath, "Dockerfile")
	}

	if dockerfilePath == "" {
		return nil, fmt.Errorf("no Dockerfile found at root or dockerfile_path for worker %s", worker.Slug)
	}

	tel.LogInfo(ctx, "Building worker with Dockerfile", "slug", worker.Slug, "dockerfile", dockerfilePath)
	cmd := exec.CommandContext(ctx, "docker", "build", "-t", imageTag, "-f", dockerfilePath, appPath)
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("docker build failed for %s: %w (stderr: %s)", worker.Slug, err, errBuf.String())
	}

	tel.LogInfo(ctx, "Worker build completed successfully", "slug", worker.Slug, "image", imageTag)

	return &BuildWorkerResult{
		ImageTag: imageTag,
		BuildDir: appPath,
		Cached:   false,
	}, nil
}

