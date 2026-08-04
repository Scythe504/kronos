package builder_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/scythe504/kronos/internal/builder"
	"github.com/scythe504/kronos/internal/telemetry"
)

func TestNewManager_DirectoryCreation(t *testing.T) {
	tempRootDir, err := os.MkdirTemp("", "kronos-test-mgr-root-*")
	if err != nil {
		t.Fatalf("failed creating temp root dir: %v", err)
	}
	defer os.RemoveAll(tempRootDir)

	cfg := builder.BuilderConfig{
		RootDir:            filepath.Join(tempRootDir, "builds"),
		CacheDir:           filepath.Join(tempRootDir, "cache"),
		TargetOS:           "linux",
		TargetArch:         "amd64",
		KeepBuildArtifacts: false,
	}

	tel := &telemetry.NoopTelemetry{}
	mgr, err := builder.NewManager(cfg, tel)
	if err != nil {
		t.Fatalf("NewManager error = %v", err)
	}

	if mgr.Config().RootDir != cfg.RootDir {
		t.Fatalf("expected RootDir %s, got %s", cfg.RootDir, mgr.Config().RootDir)
	}

	wsPath := mgr.GetWorkspacePath("test-worker", "abc123hash")
	expectedPath := filepath.Join(cfg.RootDir, "test-worker", "abc123hash")
	if wsPath != expectedPath {
		t.Fatalf("expected workspace path %s, got %s", expectedPath, wsPath)
	}

	if _, err := os.Stat(cfg.RootDir); os.IsNotExist(err) {
		t.Fatalf("expected root build directory to exist")
	}
	if _, err := os.Stat(cfg.CacheDir); os.IsNotExist(err) {
		t.Fatalf("expected cache directory to exist")
	}
}
