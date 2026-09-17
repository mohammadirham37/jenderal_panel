//go:build linux

package system

import "testing"

func TestParseCPULine(t *testing.T) {
	// user nice system idle iowait irq softirq
	line := "cpu  100 0 100 700 0 0 0 0 0 0"
	times, ok := parseCPULine(line)
	if !ok {
		t.Fatal("expected valid cpu line")
	}
	if times.idle != 700 {
		t.Errorf("expected idle 700, got %d", times.idle)
	}
	if times.total != 900 {
		t.Errorf("expected total 900, got %d", times.total)
	}

	if _, ok := parseCPULine("cpu0 1 2 3 4 5 6 7"); ok {
		t.Error("per-core line must be rejected")
	}
	if _, ok := parseCPULine("cpu 1 2 3"); ok {
		t.Error("truncated line must be rejected")
	}
}

func TestCPUPercent(t *testing.T) {
	// 600 busy jiffies out of 1000 between samples → 60%.
	prev := cpuTimes{idle: 600, total: 1000}
	cur := cpuTimes{idle: 1000, total: 2000}
	pct, ok := cpuPercent(prev, cur)
	if !ok || pct != 60 {
		t.Errorf("expected 60%%, got %.2f%% (ok=%v)", pct, ok)
	}

	// No elapsed jiffies → not a valid sample.
	pct, ok = cpuPercent(cur, cur)
	if ok {
		t.Errorf("expected zero-delta to be invalid, got %.2f%%", pct)
	}

	// Counter reset (reboot) → not a valid sample.
	pct, ok = cpuPercent(cur, prev)
	if ok {
		t.Errorf("expected counter reset to be invalid, got %.2f%%", pct)
	}
}
