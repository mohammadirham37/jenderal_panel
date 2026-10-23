package diskusage

import (
	"context"
	"strings"
	"testing"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
)

func TestReleaseActionFor(t *testing.T) {
	cases := map[string]string{
		"/var/log/btmp":                       ReleaseTruncate,
		"/var/log/wtmp":                       ReleaseTruncate,
		"/var/log/syslog":                     ReleaseTruncate,
		"/var/log/nginx/access.log":           ReleaseTruncate,
		"/var/log/syslog.1":                   ReleaseDelete,
		"/var/log/auth.log.2.gz":              ReleaseDelete,
		"/var/log/nginx/access.log-20260101":  ReleaseDelete,
		"/var/log/btmp.1":                     ReleaseDelete,
		"/var/log/journal/abc/system.journal": "",
		"/var/log/lastlog":                    "",
		"/var/log/../etc/shadow":              "",
		"/etc/shadow":                         "",
		"/var/lib/mysql/ibdata1":              "",
		"/var/logs/x.log":                     "",
	}
	for path, want := range cases {
		if got := releaseActionFor(path); got != want {
			t.Errorf("releaseActionFor(%q) = %q, want %q", path, got, want)
		}
	}
}

func releaseExec(resolved, kind string, calls *[]string) *executor.MockExecutor {
	return &executor.MockExecutor{
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			*calls = append(*calls, name+" "+strings.Join(args, " "))
			switch name {
			case "realpath":
				return &executor.Result{Stdout: resolved + "\n"}, nil
			case "stat":
				return &executor.Result{Stdout: kind + "\t1048576\n"}, nil
			}
			return &executor.Result{}, nil
		},
		RunSudoWithInputFunc: func(ctx context.Context, input, name string, args ...string) (*executor.Result, error) {
			*calls = append(*calls, "input "+name+" "+strings.Join(args, " ")+" "+input)
			return &executor.Result{}, nil
		},
	}
}

// btmp is emptied in place, never unlinked, and gets a size cap so a
// brute-force attack cannot fill the disk again before the monthly rotation.
func TestReleaseFileTruncatesBtmpAndCapsRotation(t *testing.T) {
	var calls []string
	svc := NewService(nil, releaseExec("/var/log/btmp", "regular file", &calls))
	result, err := svc.ReleaseFile(context.Background(), "/var/log/btmp")
	if err != nil {
		t.Fatal(err)
	}
	if result.Action != ReleaseTruncate || result.BytesFreed != 1048576 || !result.RotationCapped {
		t.Fatalf("unexpected result %+v", result)
	}
	joined := strings.Join(calls, "\n")
	if !strings.Contains(joined, "truncate -s 0 -- /var/log/btmp") {
		t.Errorf("btmp not truncated: %s", joined)
	}
	if strings.Contains(joined, "rm ") {
		t.Errorf("btmp must not be deleted: %s", joined)
	}
	if !strings.Contains(joined, "input tee /etc/logrotate.d/btmp") || !strings.Contains(joined, "maxsize 50M") ||
		!strings.Contains(joined, "create 0660 root utmp") {
		t.Errorf("btmp rotation not capped: %s", joined)
	}
}

func TestReleaseFileDeletesRotatedArchive(t *testing.T) {
	var calls []string
	svc := NewService(nil, releaseExec("/var/log/syslog.2.gz", "regular file", &calls))
	result, err := svc.ReleaseFile(context.Background(), "/var/log/syslog.2.gz")
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(calls, "\n")
	if result.Action != ReleaseDelete || !strings.Contains(joined, "rm -f -- /var/log/syslog.2.gz") {
		t.Errorf("archive not deleted: %+v %s", result, joined)
	}
	if strings.Contains(joined, "logrotate") {
		t.Errorf("rotation rule written for a non login record: %s", joined)
	}
}

func TestReleaseFileRefusesUnsafeTargets(t *testing.T) {
	cases := []struct{ path, resolved, kind string }{
		{"/etc/passwd", "/etc/passwd", "regular file"},
		{"/var/log/app/x.log", "/etc/shadow", "regular file"}, // symlink out of /var/log
		{"/var/log/app", "/var/log/app", "directory"},
	}
	for _, c := range cases {
		var calls []string
		svc := NewService(nil, releaseExec(c.resolved, c.kind, &calls))
		if _, err := svc.ReleaseFile(context.Background(), c.path); err == nil {
			t.Errorf("%s: expected refusal", c.path)
		}
		for _, call := range calls {
			if strings.HasPrefix(call, "truncate") || strings.HasPrefix(call, "rm") {
				t.Errorf("%s: destructive command ran: %s", c.path, call)
			}
		}
	}
}
