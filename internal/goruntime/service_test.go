package goruntime

import (
	"context"
	"strings"
	"testing"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
)

func TestParseVersion(t *testing.T) {
	cases := map[string]string{
		"go version go1.27.1 linux/amd64": "1.27.1",
		"go version devel +abc":           "",
		"":                                "",
	}
	for output, want := range cases {
		if got := parseVersion(output); got != want {
			t.Errorf("parseVersion(%q) = %q, want %q", output, got, want)
		}
	}
}

func TestInstallScriptPinnedAndSafe(t *testing.T) {
	script := installScript()
	for _, want := range []string{
		"sha256sum -c",
		Version,
		GoBin,
		"unsupported architecture",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("install script missing %q", want)
		}
	}
	// No user input exists, but confirm no obvious injection surface.
	if strings.Contains(script, "eval") {
		t.Error("install script must not use eval")
	}
}

func TestInstallSyncRunsScriptAndSucceeds(t *testing.T) {
	var gotArgv []string
	mock := &executor.MockExecutor{
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			gotArgv = append([]string{name}, args...)
			return &executor.Result{ExitCode: 0, Stdout: "go version go" + Version + " linux/amd64"}, nil
		},
	}
	var written []string
	svc := NewService(mock, nil)
	if err := svc.InstallSync(context.Background(), func(line string) { written = append(written, line) }); err != nil {
		t.Fatalf("InstallSync err = %v, want nil", err)
	}
	if len(gotArgv) != 3 || gotArgv[0] != "bash" || gotArgv[1] != "-c" {
		t.Fatalf("argv = %v, want [bash -c <script>]", gotArgv)
	}
	if !strings.Contains(gotArgv[2], Version) {
		t.Errorf("script %q missing pinned version %s", gotArgv[2], Version)
	}
	if len(written) == 0 || !strings.Contains(written[0], "linux/amd64") {
		t.Errorf("write output = %v, want install stdout forwarded", written)
	}
}

func TestInstallSyncNonZeroExitFails(t *testing.T) {
	mock := &executor.MockExecutor{
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return &executor.Result{ExitCode: 1, Stderr: "checksum mismatch"}, nil
		},
	}
	svc := NewService(mock, nil)
	err := svc.InstallSync(context.Background(), func(string) {})
	if err == nil {
		t.Fatal("InstallSync err = nil, want failure")
	}
	if !strings.Contains(err.Error(), "checksum mismatch") {
		t.Errorf("err = %v, want stderr included", err)
	}
}

func TestInstallSyncDerivesDeadlineForSlowDownloads(t *testing.T) {
	var gotDeadline bool
	mock := &executor.MockExecutor{
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			_, gotDeadline = ctx.Deadline()
			return &executor.Result{ExitCode: 0, Stdout: "ok"}, nil
		},
	}
	svc := NewService(mock, nil)
	// Provisioning runs under a deadline-less context; the executor's 30s
	// default would kill the ~70MB toolchain download.
	if err := svc.InstallSync(context.Background(), func(string) {}); err != nil {
		t.Fatalf("InstallSync error = %v", err)
	}
	if !gotDeadline {
		t.Fatal("InstallSync did not derive a context deadline; slow downloads would hit the executor default timeout")
	}
}
