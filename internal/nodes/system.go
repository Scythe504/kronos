package nodes

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"

	"github.com/scythe504/kronos/internal/database"
	"github.com/scythe504/kronos/internal/utils"
	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/mem"
	"sync"
)

var (
	nodeConfig   *database.Node
	nodeConfigMu sync.RWMutex
)

// ResetNodeConfigForTesting resets the cached node configuration in tests.
func ResetNodeConfigForTesting() {
	nodeConfigMu.Lock()
	defer nodeConfigMu.Unlock()
	nodeConfig = nil
}

// GetNodeConfig returns the cached Node config, probing the system if necessary.
func GetNodeConfig(ctx context.Context) *database.Node {
	nodeConfigMu.RLock()
	if nodeConfig != nil {
		defer nodeConfigMu.RUnlock()
		return nodeConfig
	}
	nodeConfigMu.RUnlock()

	nodeConfigMu.Lock()
	defer nodeConfigMu.Unlock()
	if nodeConfig != nil {
		return nodeConfig
	}

	sysInfo, err := GetSystemInfo(ctx)
	if err != nil {
		// Fallback dummy config
		nodeConfig = &database.Node{
			MachineID: "dummy-machine-id",
			Hostname:  "dummy-hostname",
		}
		return nodeConfig
	}

	nodeConfig = &database.Node{
		MachineID:    sysInfo.MachineID,
		Kernel:       sysInfo.Kernel,
		Architecture: sysInfo.Arch,
		GPURamKB:     &sysInfo.GPURamKB,
		GPUModel:     &sysInfo.GPUModel,
		CPUModel:     sysInfo.CPUModel,
		CPUCores:     sysInfo.CPUCores,
		RAMKB:        sysInfo.RAMKB,
		IPAddr:       sysInfo.IPAddr,
		Hostname:     sysInfo.Hostname,
	}

	return nodeConfig
}

type SystemInfo struct {
	MachineID string
	Kernel    string
	Arch      string
	GPURamKB  int64
	GPUModel  string
	CPUModel  string
	CPUCores  int
	RAMKB     int64
	Hostname  string
	IPAddr    string
}

// GetSystemInfo queries the host system metrics, including CPU, RAM, and GPU.
// This function supports Linux, macOS (Darwin), WSL, and Windows.
func GetSystemInfo(ctx context.Context) (*SystemInfo, error) {
	system := &SystemInfo{}

	// hostInfoStat provides HostID, KernelVersion, Hostname, etc.
	hostInfo, err := host.InfoWithContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get host info: %w", err)
	}
	system.MachineID = hostInfo.HostID
	system.Kernel = hostInfo.KernelVersion
	system.Arch = hostInfo.KernelArch
	system.Hostname = hostInfo.Hostname

	// CPU info
	cpuInfo, err := cpu.InfoWithContext(ctx)
	if err == nil && len(cpuInfo) > 0 {
		system.CPUModel = cpuInfo[0].ModelName
	}
	cpuCores, err := cpu.CountsWithContext(ctx, true)
	if err == nil && cpuCores > 0 {
		system.CPUCores = cpuCores
	} else {
		system.CPUCores = len(cpuInfo)
	}

	// RAM info
	memory, err := mem.VirtualMemoryWithContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get memory info: %w", err)
	}
	system.RAMKB = int64(memory.Total / 1024)

	// GPU info
	gpuModel, gpuVRAM, err := detectGPU(ctx)
	if err == nil {
		system.GPUModel = gpuModel
		system.GPURamKB = gpuVRAM
	} else {
		// Log or default to empty values, since CPU-only nodes are completely valid
		system.GPUModel = ""
		system.GPURamKB = 0
	}

	system.IPAddr = utils.GetLocalIP()

	return system, nil
}

// detectGPU attempts to find any GPU and its VRAM using multiple methods:
// - system_profiler SPDisplaysDataType (for macOS / Darwin)
// - nvidia-smi (most common/reliable for Nvidia on Linux, WSL, and Windows)
// - sysfs (for AMD GPUs on native Linux)
// - PowerShell query (for Windows and WSL fallback)
// - lspci (general Linux fallback for GPU model identification)
func detectGPU(ctx context.Context) (string, int64, error) {
	if nvidiaSmiPath := findNvidiaSmi(); nvidiaSmiPath != "" {
		model, vramKB, err := queryNvidiaGPU(ctx, nvidiaSmiPath)
		if err == nil {
			return model, vramKB, nil
		}
	}

	return "", 0, fmt.Errorf("no GPU detected")
}

// findNvidiaSmi looks for the nvidia-smi executable.
func findNvidiaSmi() string {
	if path, err := exec.LookPath("nvidia-smi"); err == nil {
		return path
	}
	// Common directories where nvidia-smi might reside depending on OS
	var paths []string
	if runtime.GOOS == "windows" {
		paths = []string{
			`C:\Program Files\NVIDIA Corporation\NVSMI\nvidia-smi.exe`,
			`C:\Windows\System32\nvidia-smi.exe`,
		}
	} else {
		paths = []string{
			"/usr/lib/wsl/lib/nvidia-smi", // WSL default mount path
			"/usr/bin/nvidia-smi",
			"/usr/sbin/nvidia-smi",
			"/usr/local/cuda/bin/nvidia-smi",
		}
	}
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// queryNvidiaGPU queries NVIDIA GPU details using nvidia-smi.
func queryNvidiaGPU(ctx context.Context, nvidiaSmiPath string) (string, int64, error) {
	cmd := exec.CommandContext(ctx, nvidiaSmiPath, "--query-gpu=name,memory.total", "--format=csv,noheader,nounits")
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "", 0, err
	}

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	var totalVRAM int64
	var models []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Split(line, ",")
		if len(parts) < 2 {
			continue
		}
		model := strings.TrimSpace(parts[0])
		vramMBStr := strings.TrimSpace(parts[1])
		vramMB, err := strconv.ParseInt(vramMBStr, 10, 64)
		if err == nil {
			totalVRAM += vramMB * 1024 // Convert MB to KB
			models = append(models, model)
		}
	}

	if len(models) == 0 {
		return "", 0, fmt.Errorf("failed to parse GPU info from nvidia-smi")
	}

	return formatGPUModels(models), totalVRAM, nil
}

// formatGPUModels formats a slice of models into a readable string.
func formatGPUModels(models []string) string {
	if len(models) == 0 {
		return ""
	}
	if len(models) == 1 {
		return models[0]
	}
	// Check if all models are identical
	allSame := true
	for i := 1; i < len(models); i++ {
		if models[i] != models[0] {
			allSame = false
			break
		}
	}
	if allSame {
		return fmt.Sprintf("%dx %s", len(models), models[0])
	}
	return strings.Join(models, " + ")
}
