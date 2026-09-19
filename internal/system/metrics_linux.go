//go:build linux

package system

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// cpuTimes holds cumulative CPU counters from one /proc/stat sample.
type cpuTimes struct {
	idle  uint64
	total uint64
}

// parseCPULine extracts aggregate idle and total jiffies from the "cpu" line.
func parseCPULine(line string) (cpuTimes, bool) {
	fields := strings.Fields(line)
	if len(fields) < 8 || fields[0] != "cpu" {
		return cpuTimes{}, false
	}

	var vals [7]uint64
	for i := 0; i < 7 && i+1 < len(fields); i++ {
		vals[i], _ = strconv.ParseUint(fields[i+1], 10, 64)
	}

	idle := vals[3]
	total := vals[0] + vals[1] + vals[2] + vals[3] + vals[4] + vals[5] + vals[6]
	if total == 0 {
		return cpuTimes{}, false
	}
	return cpuTimes{idle: idle, total: total}, true
}

// cpuPercent computes busy CPU percentage between two cumulative samples.
func cpuPercent(prev, cur cpuTimes) (float64, bool) {
	if cur.total < prev.total {
		// Counters reset (e.g. reboot) — no valid delta.
		return 0, false
	}
	dTotal := cur.total - prev.total
	if dTotal == 0 {
		return 0, false
	}
	dIdle := cur.idle - prev.idle
	busy := dTotal - dIdle
	if dIdle > dTotal {
		return 0, false
	}
	return float64(busy) / float64(dTotal) * 100, true
}

var (
	cpuMu        sync.Mutex
	lastCPUTimes cpuTimes
	hasLastCPU   bool
)

// readCPU returns CPU usage over the interval since the previous call, so the
// dashboard line reflects current load. A single /proc/stat snapshot only
// yields the cumulative average since boot, which is flat on long-uptime
// servers and made the CPU charts look frozen.
func readCPU() float64 {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return 0
	}
	lines := strings.Split(string(data), "\n")
	if len(lines) == 0 {
		return 0
	}
	times, ok := parseCPULine(lines[0])
	if !ok {
		return 0
	}

	cpuMu.Lock()
	defer cpuMu.Unlock()

	if !hasLastCPU {
		// No previous sample yet: fall back to the cumulative average;
		// the next sample produces a real interval delta.
		lastCPUTimes = times
		hasLastCPU = true
		return float64(times.total-times.idle) / float64(times.total) * 100
	}

	pct, ok := cpuPercent(lastCPUTimes, times)
	if !ok {
		lastCPUTimes = times
		return 0
	}
	lastCPUTimes = times
	return pct
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

// netCounters holds summed /proc/net/dev byte totals plus when they were read.
type netCounters struct {
	rx, tx uint64
	at     time.Time
}

// sumNetDev totals receive and transmit bytes across all non-loopback
// interfaces. The values are lifetime counters since the interfaces came up.
func sumNetDev(content string) (rx, tx uint64) {
	for _, line := range strings.Split(content, "\n") {
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

// netRatesPerSec converts two counter snapshots into average bytes per second.
func netRatesPerSec(prev, cur netCounters) (float64, float64, bool) {
	elapsed := cur.at.Sub(prev.at).Seconds()
	if elapsed <= 0 || cur.rx < prev.rx || cur.tx < prev.tx {
		// Clock went backwards or counters reset (interface/reboot) — re-baseline.
		return 0, 0, false
	}
	return float64(cur.rx-prev.rx) / elapsed, float64(cur.tx-prev.tx) / elapsed, true
}

var (
	netMu      sync.Mutex
	lastNet    netCounters
	hasLastNet bool
)

// readNetwork returns traffic as bytes per second over the interval since the
// previous call. /proc/net/dev only exposes lifetime byte totals; displaying
// those directly made the dashboard show total-since-boot bytes mislabeled as
// a per-second rate.
func readNetwork() (rxPerSec, txPerSec uint64) {
	data, err := os.ReadFile("/proc/net/dev")
	if err != nil {
		return 0, 0
	}
	rx, tx := sumNetDev(string(data))

	netMu.Lock()
	defer netMu.Unlock()

	cur := netCounters{rx: rx, tx: tx, at: time.Now()}
	if !hasLastNet {
		lastNet = cur
		hasLastNet = true
		return 0, 0
	}

	rxRate, txRate, ok := netRatesPerSec(lastNet, cur)
	lastNet = cur
	if !ok {
		return 0, 0
	}
	return uint64(rxRate), uint64(txRate)
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
