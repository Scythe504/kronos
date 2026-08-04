package builder

import (
	"runtime"

	"github.com/scythe504/kronos/internal/utils"
)

// BuilderConfig holds path configurations and target architecture parameters for the build manager.
type BuilderConfig struct {
	RootDir            string
	CacheDir           string
	TargetOS           string
	TargetArch         string
	KeepBuildArtifacts bool
}

// NewDefaultConfig returns a BuilderConfig with platform-native default storage paths.
func NewDefaultConfig() BuilderConfig {
	return BuilderConfig{
		RootDir:            utils.GetKronosBuildDir(),
		CacheDir:           utils.GetKronosCacheDir(),
		TargetOS:           runtime.GOOS,
		TargetArch:         runtime.GOARCH,
		KeepBuildArtifacts: false,
	}
}
