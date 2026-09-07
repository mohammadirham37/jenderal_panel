//go:build linux

package system

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func readCPU() float64 {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return 0
	}

	lines := strings.Split(string(data), "\n")
	if len(lines) == 0 {
		return 0
	}

	fields := strings.Fields(lines[0])
	if len(fields) < 8 || fields[0] != "cpu" {
		return 0
	}

	var vals [7]uint64
	for i := 0; i < 7 && i+1 < len(fields); i++ {
		vals[i], _ = strconv.ParseUint(fields[i+1], 10, 64)
	}

	idle := vals[3]
	total := vals[0] + vals[1] + vals[2] + vals[3] + vals[4] + vals[5] + vals[6]

	if total == 0 {
		return 0
	}

	return float64(total-idle) / float64(total) * 100
}

func readMemory() (used, total uint64) {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, 0
	}

	info := parseMeminfo(string(data))
	total = info["MemTotal"] * 1024
	free := info["MemFree"] * 1024
	buffers := info["Buffers"] * 1024
	cached := info["Cached"] * 1024
	used = total - free - buffers - cached

	return used, total
}

func readSwap() (used, total uint64) {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, 0
	}

	info := parseMeminfo(string(data))
	total = info["SwapTotal"] * 1024
	free := info["SwapFree"] * 1024
	used = total - free

	return used, total
}

func parseMeminfo(content string) map[string]uint64 {
	result := make(map[string]uint64)
	for _, line := range strings.Split(content, "\n") {
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		valStr := strings.TrimSpace(parts[1])
		valStr = strings.TrimSuffix(valStr, " kB")
		val, _ := strconv.ParseUint(strings.TrimSpace(valStr), 10, 64)
		result[key] = val
	}
	return result
}

func readDisk() (used, total uint64) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs("/", &stat); err != nil {
		return 0, 0
	}
	total = stat.Blocks * uint64(stat.Bsize)
	free := stat.Bfree * uint64(stat.Bsize)
	used = total - free
	return used, total
}

func readLoadAvg() (load1, load5, load15 float64) {
	data, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return 0, 0, 0
	}
	fmt.Sscanf(string(data), "%f %f %f", &load1, &load5, &load15)
	return load1, load5, load15
}

func readNetwork() (rx, tx uint64) {
	data, err := os.ReadFile("/proc/net/dev")
	if err != nil {
		return 0, 0
	}

	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "lo:") || !strings.Contains(line, ":") {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		fields := strings.Fields(parts[1])
		if len(fields) < 10 {
			continue
		}
		r, _ := strconv.ParseUint(fields[0], 10, 64)
		t, _ := strconv.ParseUint(fields[8], 10, 64)
		rx += r
		tx += t
	}
	return rx, tx
}

func readUptime() time.Duration {
	data, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return 0
	}
	var secs float64
	fmt.Sscanf(string(data), "%f", &secs)
	return time.Duration(secs * float64(time.Second))
}
