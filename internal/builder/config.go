package builder

import (
	"os"
	"path/filepath"
	"runtime"
)

// BuilderConfig holds path configurations and target architecture parameters for the build manager.
type BuilderConfig struct {
	RootDir            string
	CacheDir           string
	TargetOS           string
	TargetArch         string
	KeepBuildArtifacts bool
}

// NewDefaultConfig returns a BuilderConfig with sensible defaults based on OS environment or standard paths.
func NewDefaultConfig() BuilderConfig {
	baseDir := os.Getenv("KRONOS_BUILD_DIR")
	if baseDir == "" {
		home, err := os.UserHomeDir()
		if err == nil {
			baseDir = filepath.Join(home, ".kronos", "builds")
		} else {
			baseDir = filepath.Join(os.TempDir(), "kronos-builds")
		}
	}

	cacheDir := os.Getenv("KRONOS_CACHE_DIR")
	if cacheDir == "" {
		cacheDir = filepath.Join(baseDir, "cache")
	}

	return BuilderConfig{
		RootDir:            baseDir,
		CacheDir:           cacheDir,
		TargetOS:           runtime.GOOS,
		TargetArch:         runtime.GOARCH,
		KeepBuildArtifacts: false,
	}
}
