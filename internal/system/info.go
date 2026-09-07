package system

import (
	"context"
	"fmt"
	"net"
	"strings"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

// Info provides server information by executing system commands.
type Info struct {
	exec executor.CommandExecutor
}

// NewInfo creates a new Info with the given command executor.
func NewInfo(exec executor.CommandExecutor) *Info {
	return &Info{exec: exec}
}

// Get collects server information and returns a populated ServerInfo.
func (i *Info) Get(ctx context.Context) (model.ServerInfo, error) {
	var info model.ServerInfo

	// Hostname
	hostnameRes, err := i.exec.Run(ctx, "hostname")
	if err != nil {
		return info, fmt.Errorf("get hostname: %w", err)
	}
	info.Hostname = strings.TrimSpace(hostnameRes.Stdout)

	// Kernel
	kernelRes, err := i.exec.Run(ctx, "uname", "-r")
	if err != nil {
		return info, fmt.Errorf("get kernel: %w", err)
	}
	info.Kernel = strings.TrimSpace(kernelRes.Stdout)

	// OS (from /etc/os-release)
	osRes, err := i.exec.Run(ctx, "cat", "/etc/os-release")
	if err == nil {
		info.OS = parsePrettyName(osRes.Stdout)
	}

	// IP via outbound UDP dial (does not actually send traffic)
	info.IP = getOutboundIP()

	// CPU info
	cpuRes, err := i.exec.Run(ctx, "lscpu")
	if err == nil {
		info.CPU = parseCPUModel(cpuRes.Stdout)
	}

	// RAM info
	memRes, err := i.exec.Run(ctx, "free", "-h")
	if err == nil {
		info.RAM = parseRAMTotal(memRes.Stdout)
	}

	// Disk info
	diskRes, err := i.exec.Run(ctx, "df", "-h", "/")
	if err == nil {
		info.Disk = parseDiskTotal(diskRes.Stdout)
	}

	// Uptime
	uptimeRes, err := i.exec.Run(ctx, "uptime", "-p")
	if err == nil {
		info.Uptime = strings.TrimSpace(uptimeRes.Stdout)
	}

	// Timezone
	tzRes, err := i.exec.Run(ctx, "timedatectl")
	if err == nil {
		info.Timezone = parseTimezone(tzRes.Stdout)
	}

	return info, nil
}

// SetHostname changes the system hostname.
func (i *Info) SetHostname(ctx context.Context, hostname string) error {
	res, err := i.exec.RunSudo(ctx, "hostnamectl", "set-hostname", hostname)
	if err != nil {
		return fmt.Errorf("set hostname: %w", err)
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("set hostname: %s", strings.TrimSpace(res.Stderr))
	}
	return nil
}

// SetTimezone changes the system timezone.
func (i *Info) SetTimezone(ctx context.Context, timezone string) error {
	res, err := i.exec.RunSudo(ctx, "timedatectl", "set-timezone", timezone)
	if err != nil {
		return fmt.Errorf("set timezone: %w", err)
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("set timezone: %s", strings.TrimSpace(res.Stderr))
	}
	return nil
}

// Reboot reboots the system.
func (i *Info) Reboot(ctx context.Context) error {
	res, err := i.exec.RunSudo(ctx, "reboot")
	if err != nil {
		return fmt.Errorf("reboot: %w", err)
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("reboot: %s", strings.TrimSpace(res.Stderr))
	}
	return nil
}

// parsePrettyName extracts PRETTY_NAME from /etc/os-release content.
func parsePrettyName(content string) string {
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "PRETTY_NAME=") {
			val := strings.TrimPrefix(line, "PRETTY_NAME=")
			val = strings.Trim(val, "\"")
			return val
		}
	}
	return ""
}

// getOutboundIP determines the preferred outbound IP of this machine.
func getOutboundIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return ""
	}
	defer conn.Close()
	localAddr := conn.LocalAddr().(*net.UDPAddr)
	return localAddr.IP.String()
}

// parseCPUModel extracts the CPU model name from lscpu output.
func parseCPUModel(content string) string {
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "Model name:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "Model name:"))
		}
	}
	return ""
}

// parseRAMTotal extracts total RAM from free -h output.
func parseRAMTotal(content string) string {
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "Mem:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				return fields[1]
			}
		}
	}
	return ""
}

// parseDiskTotal extracts total disk from df -h / output.
func parseDiskTotal(content string) string {
	lines := strings.Split(strings.TrimSpace(content), "\n")
	if len(lines) >= 2 {
		fields := strings.Fields(lines[1])
		if len(fields) >= 2 {
			return fields[1]
		}
	}
	return ""
}

// parseTimezone extracts the timezone from timedatectl output.
func parseTimezone(content string) string {
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "Time zone:") {
			parts := strings.Fields(trimmed)
			if len(parts) >= 3 {
				return parts[2]
			}
		}
	}
	return ""
}
