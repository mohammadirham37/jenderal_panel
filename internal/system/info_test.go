package system

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
)

func TestInfoGet(t *testing.T) {
	mock := &executor.MockExecutor{
		RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			switch name {
			case "hostname":
				return &executor.Result{Stdout: "vps01\n", Duration: time.Millisecond}, nil
			case "uname":
				return &executor.Result{Stdout: "6.8.0-41-generic\n", Duration: time.Millisecond}, nil
			case "cat":
				if len(args) > 0 && args[0] == "/etc/os-release" {
					return &executor.Result{Stdout: "PRETTY_NAME=\"Ubuntu 24.04 LTS\"\nNAME=\"Ubuntu\"\n", Duration: time.Millisecond}, nil
				}
				return &executor.Result{Duration: time.Millisecond}, nil
			case "lscpu":
				return &executor.Result{Stdout: "Architecture:          x86_64\nModel name:            Intel Xeon E5-2680\n", Duration: time.Millisecond}, nil
			case "free":
				return &executor.Result{Stdout: "              total        used        free\nMem:          7.7Gi       3.2Gi       4.5Gi\n", Duration: time.Millisecond}, nil
			case "df":
				return &executor.Result{Stdout: "Filesystem      Size  Used Avail Use% Mounted on\n/dev/sda1        50G   20G   28G  42% /\n", Duration: time.Millisecond}, nil
			case "uptime":
				return &executor.Result{Stdout: "up 10 days, 3 hours, 22 minutes\n", Duration: time.Millisecond}, nil
			case "timedatectl":
				return &executor.Result{Stdout: "               Local time: Mon 2024-01-15 10:30:00 WIB\n           Universal time: Mon 2024-01-15 03:30:00 UTC\n                 RTC time: Mon 2024-01-15 03:30:00\n                Time zone: Asia/Jakarta (WIB, +0700)\n", Duration: time.Millisecond}, nil
			default:
				return &executor.Result{Duration: time.Millisecond}, nil
			}
		},
	}

	info := NewInfo(mock)
	result, err := info.Get(context.Background())
	if err != nil {
		t.Fatalf("Get() returned error: %v", err)
	}

	if result.Hostname != "vps01" {
		t.Errorf("Hostname = %q, want %q", result.Hostname, "vps01")
	}
	if result.Kernel != "6.8.0-41-generic" {
		t.Errorf("Kernel = %q, want %q", result.Kernel, "6.8.0-41-generic")
	}
	if result.OS != "Ubuntu 24.04 LTS" {
		t.Errorf("OS = %q, want %q", result.OS, "Ubuntu 24.04 LTS")
	}
	if result.CPU != "Intel Xeon E5-2680" {
		t.Errorf("CPU = %q, want %q", result.CPU, "Intel Xeon E5-2680")
	}
	if result.RAM != "7.7Gi" {
		t.Errorf("RAM = %q, want %q", result.RAM, "7.7Gi")
	}
	if result.Disk != "50G" {
		t.Errorf("Disk = %q, want %q", result.Disk, "50G")
	}
	if result.Timezone != "Asia/Jakarta" {
		t.Errorf("Timezone = %q, want %q", result.Timezone, "Asia/Jakarta")
	}
	if !strings.Contains(result.Uptime, "10 days") {
		t.Errorf("Uptime = %q, expected it to contain '10 days'", result.Uptime)
	}
	// IP should be populated (from real network interface)
	if result.IP == "" {
		t.Log("IP is empty (may be expected in isolated environments)")
	}
}

func TestInfoSetHostname(t *testing.T) {
	var calledWith []string
	mock := &executor.MockExecutor{
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			calledWith = append(calledWith, name)
			calledWith = append(calledWith, args...)
			return &executor.Result{Duration: time.Millisecond}, nil
		},
	}

	info := NewInfo(mock)
	err := info.SetHostname(context.Background(), "newhost")
	if err != nil {
		t.Fatalf("SetHostname() returned error: %v", err)
	}
	if len(calledWith) != 3 || calledWith[0] != "hostnamectl" || calledWith[1] != "set-hostname" || calledWith[2] != "newhost" {
		t.Errorf("SetHostname called with %v, want [hostnamectl set-hostname newhost]", calledWith)
	}
}

func TestInfoSetTimezone(t *testing.T) {
	var calledWith []string
	mock := &executor.MockExecutor{
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			calledWith = append(calledWith, name)
			calledWith = append(calledWith, args...)
			return &executor.Result{Duration: time.Millisecond}, nil
		},
	}

	info := NewInfo(mock)
	err := info.SetTimezone(context.Background(), "Asia/Jakarta")
	if err != nil {
		t.Fatalf("SetTimezone() returned error: %v", err)
	}
	if len(calledWith) != 3 || calledWith[0] != "timedatectl" || calledWith[1] != "set-timezone" || calledWith[2] != "Asia/Jakarta" {
		t.Errorf("SetTimezone called with %v, want [timedatectl set-timezone Asia/Jakarta]", calledWith)
	}
}

func TestInfoReboot(t *testing.T) {
	var calledCmd string
	mock := &executor.MockExecutor{
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			calledCmd = name
			return &executor.Result{Duration: time.Millisecond}, nil
		},
	}

	info := NewInfo(mock)
	err := info.Reboot(context.Background())
	if err != nil {
		t.Fatalf("Reboot() returned error: %v", err)
	}
	if calledCmd != "reboot" {
		t.Errorf("Reboot called %q, want %q", calledCmd, "reboot")
	}
}

func TestParsePrettyName(t *testing.T) {
	input := `NAME="Ubuntu"
VERSION="24.04 LTS (Noble Numbat)"
PRETTY_NAME="Ubuntu 24.04 LTS"
VERSION_ID="24.04"
`
	got := parsePrettyName(input)
	if got != "Ubuntu 24.04 LTS" {
		t.Errorf("parsePrettyName() = %q, want %q", got, "Ubuntu 24.04 LTS")
	}
}

func TestParseTimezone(t *testing.T) {
	input := `               Local time: Mon 2024-01-15 10:30:00 WIB
           Universal time: Mon 2024-01-15 03:30:00 UTC
                 RTC time: Mon 2024-01-15 03:30:00
                Time zone: Asia/Jakarta (WIB, +0700)
System clock synchronized: yes
              NTP service: active
          RTC in local TZ: no`
	got := parseTimezone(input)
	if got != "Asia/Jakarta" {
		t.Errorf("parseTimezone() = %q, want %q", got, "Asia/Jakarta")
	}
}
