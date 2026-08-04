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

	packclient "github.com/buildpacks/pack/pkg/client"
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
func ComputeBuildCacheKey(worker database.Worker) string {
	h := sha256.New()
	h.Write([]byte(worker.RepoURL))
	h.Write([]byte("|"))
	h.Write([]byte(worker.RepoRef))
	h.Write([]byte("|"))
	if worker.PreBuildCommand != nil {
		h.Write([]byte(*worker.PreBuildCommand))
	}
	h.Write([]byte("|"))
	if worker.BuildCommand != nil {
		h.Write([]byte(*worker.BuildCommand))
	}
	h.Write([]byte("|"))
	if worker.RunCommand != nil {
		h.Write([]byte(*worker.RunCommand))
	}
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
		for _, line := range strings.Split(envStr, "\n") {
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

// executeContainerBuild compiles a container image using Dockerfile or Cloud-Native Buildpacks SDK.
func executeContainerBuild(ctx context.Context, worker database.Worker, imageTag string, appPath string, tel telemetry.TelemetryProvider) (*BuildWorkerResult, error) {
	dockerfilePath := ""
	if worker.DockerfilePath != nil && *worker.DockerfilePath != "" {
		dockerfilePath = filepath.Join(appPath, *worker.DockerfilePath)
	} else if _, err := os.Stat(filepath.Join(appPath, "Dockerfile")); err == nil {
		dockerfilePath = filepath.Join(appPath, "Dockerfile")
	}

	if dockerfilePath != "" {
		tel.LogInfo(ctx, "Building worker with Dockerfile", "slug", worker.Slug, "dockerfile", dockerfilePath)
		cmd := exec.CommandContext(ctx, "docker", "build", "-t", imageTag, "-f", dockerfilePath, appPath)
		var outBuf, errBuf bytes.Buffer
		cmd.Stdout = &outBuf
		cmd.Stderr = &errBuf

		if err := cmd.Run(); err != nil {
			return nil, fmt.Errorf("docker build failed for %s: %w (stderr: %s)", worker.Slug, err, errBuf.String())
		}
	} else {
		tel.LogInfo(ctx, "Building worker with Cloud-Native Buildpacks Go SDK", "slug", worker.Slug, "image", imageTag)

		pClient, err := packclient.NewClient()
		if err != nil {
			return nil, fmt.Errorf("failed initializing pack SDK client: %w", err)
		}

		envMap := make(map[string]string)
		if len(worker.EnvVars) > 0 {
			envMap["KRONOS_WORKER_ENV"] = string(worker.EnvVars)
		}
		if worker.PreBuildCommand != nil && *worker.PreBuildCommand != "" {
			envMap["BP_PRE_BUILD_COMMAND"] = *worker.PreBuildCommand
		}
		if worker.BuildCommand != nil && *worker.BuildCommand != "" {
			envMap["BP_BUILD_COMMAND"] = *worker.BuildCommand
		}
		if worker.RunCommand != nil && *worker.RunCommand != "" {
			envMap["BP_RUN_COMMAND"] = *worker.RunCommand
		}

		defaultProcess := ""
		if worker.RunCommand != nil && *worker.RunCommand != "" {
			defaultProcess = *worker.RunCommand
		} else if worker.Entrypoint != "" {
			defaultProcess = worker.Entrypoint
		}

		buildOpts := packclient.BuildOptions{
			Image:              imageTag,
			AppPath:            appPath,
			Builder:            "paketobuildpacks/builder:base",
			Env:                envMap,
			DefaultProcessType: defaultProcess,
		}

		if err := pClient.Build(ctx, buildOpts); err != nil {
			return nil, fmt.Errorf("pack SDK build failed for %s: %w", worker.Slug, err)
		}
	}

	tel.LogInfo(ctx, "Worker build completed successfully", "slug", worker.Slug, "image", imageTag)

	return &BuildWorkerResult{
		ImageTag: imageTag,
		BuildDir: appPath,
		Cached:   false,
	}, nil
}

// BuildWorker is a convenience function that delegates build operations to a default Manager.
func BuildWorker(ctx context.Context, worker database.Worker, tel telemetry.TelemetryProvider) (*BuildWorkerResult, error) {
	mgr, err := NewManager(NewDefaultConfig(), tel)
	if err != nil {
		return nil, err
	}
	return mgr.Build(ctx, worker)
}
