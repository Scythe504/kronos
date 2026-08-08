package nodes

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
	// macOS / Darwin
	if runtime.GOOS == "darwin" {
		return queryDarwinGPU(ctx)
	}

	// Try Nvidia-smi (most common for Nvidia cards on Linux/WSL/Windows)
	if nvidiaSmiPath := findNvidiaSmi(); nvidiaSmiPath != "" {
		model, vramKB, err := queryNvidiaGPU(ctx, nvidiaSmiPath)
		if err == nil {
			return model, vramKB, nil
		}
	}

	// Try AMD specific sysfs check (Linux only)
	if runtime.GOOS == "linux" {
		if model, vramKB, err := queryAMDGPU(); err == nil {
			return model, vramKB, nil
		}
	}

	// Try Windows/WSL PowerShell check (if Windows or running inside WSL)
	if runtime.GOOS == "windows" || (runtime.GOOS == "linux" && isWSL()) {
		if model, vramKB, err := queryWSLPowerShellGPU(ctx); err == nil {
			return model, vramKB, nil
		}
	}

	// Fallback to general lspci check (Linux only)
	if runtime.GOOS == "linux" {
		if model, vramKB, err := queryGeneralLSPCI(); err == nil {
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

// queryAMDGPU queries AMD VRAM from sysfs and looks up the model via lspci.
func queryAMDGPU() (string, int64, error) {
	files, err := filepath.Glob("/sys/class/drm/card*/device/mem_info_vram_total")
	if err != nil || len(files) == 0 {
		return "", 0, fmt.Errorf("no AMD VRAM files found")
	}

	var totalVRAM int64
	gpuCount := 0
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		vramBytes, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
		if err == nil {
			totalVRAM += vramBytes / 1024 // Convert bytes to KB
			gpuCount++
		}
	}

	if gpuCount == 0 {
		return "", 0, fmt.Errorf("failed to read AMD VRAM from sysfs")
	}

	model := "AMD Radeon GPU"
	if lspciModel, err := queryLSPCI("AMD"); err == nil {
		model = lspciModel
	}

	if gpuCount > 1 {
		model = fmt.Sprintf("%dx %s", gpuCount, model)
	}

	return model, totalVRAM, nil
}

// isWSL detects if the current OS environment is WSL.
func isWSL() bool {
	if runtime.GOOS != "linux" {
		return false
	}
	for _, path := range []string{"/proc/sys/kernel/osrelease", "/proc/version"} {
		if data, err := os.ReadFile(path); err == nil {
			content := strings.ToLower(string(data))
			if strings.Contains(content, "microsoft") || strings.Contains(content, "wsl") {
				return true
			}
		}
	}
	return false
}

// queryWSLPowerShellGPU uses WSL Windows Interoperability to call powershell.exe on the host.
func queryWSLPowerShellGPU(ctx context.Context) (string, int64, error) {
	// Call PowerShell to get the Name and AdapterRAM of video controllers.
	// We output it comma-separated.
	cmd := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-Command", "Get-CimInstance Win32_VideoController | ForEach-Object { $_.Name + ',' + $_.AdapterRAM }")
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

		// Skip basic virtual display/remote adapters
		modelLower := strings.ToLower(model)
		if strings.Contains(modelLower, "microsoft basic display") || strings.Contains(modelLower, "remote display") {
			continue
		}

		vramBytesStr := strings.TrimSpace(parts[1])
		vramBytes, err := strconv.ParseInt(vramBytesStr, 10, 64)
		// Only count if vramBytes is positive (valid RAM report)
		if err == nil && vramBytes > 0 {
			totalVRAM += vramBytes / 1024 // Convert bytes to KB
			models = append(models, model)
		}
	}

	if len(models) == 0 {
		return "", 0, fmt.Errorf("no valid GPUs found via PowerShell")
	}

	return formatGPUModels(models), totalVRAM, nil
}

// queryGeneralLSPCI parses lspci to identify GPU devices.
func queryGeneralLSPCI() (string, int64, error) {
	model, err := queryLSPCI("")
	if err != nil {
		return "", 0, err
	}

	// Try to get VRAM from standard sysfs locations if they exist
	var vramKB int64
	files, err := filepath.Glob("/sys/class/drm/card*/device/mem_info_vram_total")
	if err == nil && len(files) > 0 {
		for _, file := range files {
			data, err := os.ReadFile(file)
			if err == nil {
				vramBytes, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
				if err == nil {
					vramKB += vramBytes / 1024
				}
			}
		}
	}

	return model, vramKB, nil
}

// queryLSPCI runs lspci and filters for graphics cards.
func queryLSPCI(filter string) (string, error) {
	path, err := exec.LookPath("lspci")
	if err != nil {
		return "", err
	}
	cmd := exec.Command(path)
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "", err
	}

	lines := strings.SplitSeq(out.String(), "\n")
	for line := range lines {
		// Look for GPU class controllers
		if strings.Contains(line, "VGA compatible controller") ||
			strings.Contains(line, "3D controller") ||
			strings.Contains(line, "Display controller") {
			if filter == "" || strings.Contains(strings.ToLower(line), strings.ToLower(filter)) {
				parts := strings.SplitN(line, ":", 3)
				if len(parts) >= 3 {
					return strings.TrimSpace(parts[2]), nil
				}
				if len(parts) == 2 {
					return strings.TrimSpace(parts[1]), nil
				}
				return strings.TrimSpace(line), nil
			}
		}
	}
	return "", fmt.Errorf("no matching GPU device found in lspci output")
}

// queryDarwinGPU retrieves GPU model and VRAM on macOS.
func queryDarwinGPU(ctx context.Context) (string, int64, error) {
	path, err := exec.LookPath("system_profiler")
	if err != nil {
		return "", 0, err
	}
	cmd := exec.CommandContext(ctx, path, "SPDisplaysDataType")
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "", 0, err
	}

	lines := strings.Split(out.String(), "\n")
	var chipsetModels []string
	var totalVRAM int64
	var isAppleSilicon bool

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		if after, ok := strings.CutPrefix(line, "Chipset Model:"); ok {
			model := strings.TrimSpace(after)
			chipsetModels = append(chipsetModels, model)
			if strings.HasPrefix(model, "Apple M") || strings.HasPrefix(model, "Apple A") {
				isAppleSilicon = true
			}
		}

		// Parse VRAM (Total) or VRAM (Dynamic, Max)
		if strings.HasPrefix(line, "VRAM (Total):") || strings.HasPrefix(line, "VRAM (Dynamic, Max):") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				vramStr := strings.TrimSpace(parts[1])
				vramKB := parseMacVRAM(vramStr)
				totalVRAM += vramKB
			}
		}
	}

	if len(chipsetModels) == 0 {
		return "", 0, fmt.Errorf("no GPU found via system_profiler")
	}

	// For Apple Silicon, since they use Unified Memory, if no separate VRAM was reported,
	// default to total system RAM since it is shared.
	if isAppleSilicon && totalVRAM == 0 {
		if memory, err := mem.VirtualMemoryWithContext(ctx); err == nil {
			totalVRAM = int64(memory.Total / 1024)
		}
	}

	return formatGPUModels(chipsetModels), totalVRAM, nil
}

// parseMacVRAM parses VRAM strings (e.g. MB or GB) into KB.
func parseMacVRAM(vramStr string) int64 {
	vramStr = strings.ToLower(vramStr)
	var multiplier int64 = 1024 // default to MB (MB -> KB)
	if strings.Contains(vramStr, "gb") {
		multiplier = 1024 * 1024 // GB -> KB
	} else if strings.Contains(vramStr, "tb") {
		multiplier = 1024 * 1024 * 1024 // TB -> KB
	} else if strings.Contains(vramStr, "kb") {
		multiplier = 1 // KB -> KB
	}

	var numStr strings.Builder
	for _, r := range vramStr {
		if (r >= '0' && r <= '9') || r == '.' || r == ',' {
			if r == ',' {
				continue
			}
			numStr.WriteRune(r)
		}
	}

	if val, err := strconv.ParseFloat(numStr.String(), 64); err == nil {
		return int64(val * float64(multiplier))
	}
	return 0
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
