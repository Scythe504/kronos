package utils

import (
	"bytes"
	"context"
	"crypto/rand"
	"fmt"
	"math/big"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// GetLocalIP finds the primary outbound IP address.
// Under WSL, it attempts to resolve the actual Windows host's LAN IP address.
func GetLocalIP() string {
	ctx := context.Background()

	// If running under WSL, try to resolve the Windows host's LAN IP
	if isWSL() {
		if hostIP, err := getWindowsHostIP(ctx); err == nil && hostIP != "" {
			return hostIP
		}
	}

	// Try the UDP dial trick first to get the local IP used for outbound routing.
	// This avoids getting virtual bridge IPs like docker, tailscale, etc.
	// The external address does not need to be reachable; the OS just needs to resolve the routing path.
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err == nil {
		defer conn.Close()
		localAddr, ok := conn.LocalAddr().(*net.UDPAddr)
		if ok && localAddr != nil && localAddr.IP != nil {
			ip4 := localAddr.IP.To4()
			if ip4 != nil && !ip4.IsLoopback() {
				return ip4.String()
			}
		}
	}

	// Fallback to interface scanning if UDP dial fails
	ifaces, err := net.Interfaces()
	if err != nil {
		return "127.0.0.1"
	}

	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil || ip.IsLoopback() {
				continue
			}
			ip = ip.To4()
			if ip == nil {
				continue
			}
			return ip.String()
		}
	}
	return "127.0.0.1"
}

// isWSL detects if the current environment is running inside WSL.
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

// getWindowsHostIP executes ipconfig.exe and filters physical LAN adapters to find the host IP.
func getWindowsHostIP(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "ipconfig.exe")
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "", err
	}

	lines := strings.Split(out.String(), "\n")
	var currentAdapter string
	var fallbackIP string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		// Detect adapter section start
		if !strings.HasPrefix(line, " ") && strings.Contains(line, "adapter") {
			currentAdapter = strings.ToLower(trimmed)
			continue
		}

		// Look for IP Address
		if strings.Contains(trimmed, "IPv4 Address") {
			parts := strings.Split(trimmed, ":")
			if len(parts) < 2 {
				continue
			}
			ip := strings.TrimSpace(parts[1])
			ip = strings.TrimSuffix(ip, "\r")

			// Check if this is a virtual adapter (e.g. WSL virtual switch, VirtualBox)
			isVirtual := strings.Contains(currentAdapter, "vethernet") ||
				strings.Contains(currentAdapter, "virtualbox") ||
				strings.Contains(currentAdapter, "vmware") ||
				strings.Contains(currentAdapter, "host-only") ||
				strings.Contains(currentAdapter, "loopback")

			if !isVirtual && ip != "" && !strings.HasPrefix(ip, "127.") {
				return ip, nil
			}
			if fallbackIP == "" && ip != "" && !strings.HasPrefix(ip, "127.") {
				fallbackIP = ip
			}
		}
	}

	if fallbackIP != "" {
		return fallbackIP, nil
	}
	return "", fmt.Errorf("no physical host IP found")
}

func RandInt(max *big.Int) int64 {
	r, err := rand.Int(rand.Reader, max)
	if err != nil {
		panic(err)
	}

	return r.Int64()
}
