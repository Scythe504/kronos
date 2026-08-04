package utils

import (
	"os"
	"path/filepath"
	"runtime"
)

// GetKronosConfigDir returns the OS-native root directory for Kronos configuration files.
// - Windows: %APPDATA%\Kronos
// - Linux: /etc/kronos (if root or directory exists) or ~/.kronos
// - macOS: ~/Library/Application Support/Kronos or ~/.kronos
func GetKronosConfigDir() string {
	if envDir := os.Getenv("KRONOS_CONFIG_DIR"); envDir != "" {
		return envDir
	}

	if runtime.GOOS == "windows" {
		if appData := os.Getenv("APPDATA"); appData != "" {
			return filepath.Join(appData, "Kronos")
		}
		if configDir, err := os.UserConfigDir(); err == nil {
			return filepath.Join(configDir, "Kronos")
		}
	} else if runtime.GOOS == "linux" {
		if fi, err := os.Stat("/etc/kronos"); err == nil && fi.IsDir() {
			return "/etc/kronos"
		}
		if os.Geteuid() == 0 {
			return "/etc/kronos"
		}
	}

	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".kronos")
	}

	return filepath.Join(os.TempDir(), "kronos")
}

// GetNodeIDFilePath returns the OS-native file path for the persisted node_id.
// - Windows: %APPDATA%\Kronos\.node_id
// - Linux: /etc/kronos/.node_id (or ~/.kronos/.node_id)
func GetNodeIDFilePath() string {
	return filepath.Join(GetKronosConfigDir(), ".node_id")
}

// GetAgentConfigFilePath returns the OS-native file path for agent.conf.
// - Windows: %APPDATA%\Kronos\agent.conf
// - Linux: /etc/kronos/agent.conf (or ~/.kronos/agent.conf)
func GetAgentConfigFilePath() string {
	return filepath.Join(GetKronosConfigDir(), "agent.conf")
}

// GetKronosBuildDir returns the OS-native root directory for worker build artifacts.
func GetKronosBuildDir() string {
	if envDir := os.Getenv("KRONOS_BUILD_DIR"); envDir != "" {
		return envDir
	}
	return filepath.Join(GetKronosConfigDir(), "builds")
}

// GetKronosCacheDir returns the OS-native directory for build cache.
func GetKronosCacheDir() string {
	if envDir := os.Getenv("KRONOS_CACHE_DIR"); envDir != "" {
		return envDir
	}
	if runtime.GOOS == "windows" {
		if localAppData := os.Getenv("LOCALAPPDATA"); localAppData != "" {
			return filepath.Join(localAppData, "Kronos", "cache")
		}
	}
	return filepath.Join(GetKronosConfigDir(), "cache")
}
