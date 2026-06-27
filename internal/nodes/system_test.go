package nodes

import (
	"context"
	"testing"
)

func TestGetSystemInfo(t *testing.T) {
	info, err := GetSystemInfo(context.Background())
	if err != nil {
		t.Fatalf("failed to get system info: %v", err)
	}

	if info == nil {
		t.Fatal("expected to get some systemInfo but got nil")
	}

	t.Logf("MachineID: %s", info.MachineID)
	t.Logf("Kernel: %s", info.Kernel)
	t.Logf("Hostname: %s", info.Hostname)
	t.Logf("CPUModel: %s", info.CPUModel)
	t.Logf("CPUCores: %d", info.CPUCores)
	t.Logf("RAMKB: %d KB (%.2f GB)", info.RAMKB, float64(info.RAMKB)/(1024*1024))
	t.Logf("GPUModel: %s", info.GPUModel)
	t.Logf("GPURamKB: %d KB (%.2f GB)", info.GPURamKB, float64(info.GPURamKB)/(1024*1024))
	t.Logf("IPV4Addr: %s", info.IPAddr)
}
