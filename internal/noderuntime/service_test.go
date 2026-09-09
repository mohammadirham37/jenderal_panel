package noderuntime

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
)

func TestValidateVersion(t *testing.T) {
	for _, version := range []string{"", "21", "24;id", "v24", "024", "24.1"} {
		if ValidateVersion(version) == nil {
			t.Fatalf("accepted %q", version)
		}
	}
	for _, version := range []string{"20", "22", "24"} {
		if err := ValidateVersion(version); err != nil {
			t.Fatalf("rejected %q: %v", version, err)
		}
	}
}

func TestHomeRejectsUnsafeUsers(t *testing.T) {
	for _, user := range []string{"", "root", "www-data", "web_", "web_bad/name", "web_bad name", "web_a;id", "web_A"} {
		if _, err := Home(user); err == nil {
			t.Fatalf("accepted %q", user)
		}
	}
	if got, err := Home("web_example_1"); err != nil || got != "/home/web_example_1" {
		t.Fatalf("Home() = %q, %v", got, err)
	}
}

func TestExecArgsUsesIsolatedEnvironmentAndFixedPositions(t *testing.T) {
	got, err := ExecArgs("web_example", "24", "npm", "run", "build; id")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"web_example", "--", "/usr/bin/env", "-i",
		"HOME=/home/web_example", "USER=web_example", "LOGNAME=web_example",
		"NVM_DIR=/home/web_example/.nvm", "NODE_VERSION=24",
		"PATH=/usr/local/bin:/usr/bin:/bin",
		"/home/web_example/.nvm/nvm-exec", "npm", "run", "build; id",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ExecArgs() = %#v\nwant %#v", got, want)
	}
}

func TestDetectStates(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   Status
	}{
		{"missing", "nvm_state=missing\n", Status{NVMState: NVMStateMissing}},
		{"corrupt", "nvm_state=corrupt\n", Status{NVMState: NVMStateCorrupt}},
		{"mismatched", "nvm_state=mismatched\nnvm_version=v0.40.6\n", Status{NVMState: NVMStateMismatched, NVMVersion: "v0.40.6"}},
		{"runtime missing", "nvm_state=ready\nnvm_version=v0.40.7\ninstalled=false\n", Status{NVMState: NVMStateReady, NVMVersion: "v0.40.7"}},
		{"installed", "nvm_state=ready\nnvm_version=v0.40.7\ninstalled=true\nnode_version=v24.8.0\nnpm_version=11.6.0\n", Status{NVMState: NVMStateReady, NVMVersion: "v0.40.7", Installed: true, NodeVersion: "v24.8.0", NPMVersion: "11.6.0"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := fakeExecutor{runSudo: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
				deadline, ok := ctx.Deadline()
				if !ok || time.Until(deadline) > detectTimeout {
					t.Fatalf("Detect did not impose timeout: %v, %v", deadline, ok)
				}
				if name != "-u" || len(args) < 13 || args[0] != "web_example" || args[10] != "/bin/bash" || args[11] != "-c" || args[len(args)-3] != "web_example" || args[len(args)-2] != "24" || args[len(args)-1] != "/home/web_example" {
					t.Fatalf("unexpected command: %q %#v", name, args)
				}
				return &executor.Result{Stdout: tt.output}, nil
			}}
			got, err := New(fake).Detect(context.Background(), "web_example", "24")
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("Detect() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestDetectRejectsMalformedOutput(t *testing.T) {
	fake := fakeExecutor{runSudo: func(context.Context, string, ...string) (*executor.Result, error) {
		return &executor.Result{Stdout: "nvm_state=ready\ninstalled=true\nnode_version=v22.1.0\nnpm_version=10.0.0\n"}, nil
	}}
	_, err := New(fake).Detect(context.Background(), "web_example", "24")
	if err == nil || !strings.Contains(err.Error(), "requested major") {
		t.Fatalf("Detect() error = %v", err)
	}
}

func TestInstallRunsPinnedVerifiedScriptAndLogs(t *testing.T) {
	var script string
	fake := fakeExecutor{runSudo: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > installTimeout {
			t.Fatalf("Install did not impose timeout: %v, %v", deadline, ok)
		}
		if name != "-u" || args[0] != "web_example" || args[len(args)-3] != "web_example" || args[len(args)-2] != "24" || args[len(args)-1] != "/home/web_example" {
			t.Fatalf("unexpected command: %q %#v", name, args)
		}
		script = args[len(args)-5]
		return &executor.Result{Stdout: "Installing Node 24\nInstalled Node v24.8.0 with npm 11.6.0\n", Stderr: "download progress\n"}, nil
	}}
	var logs []string
	if err := New(fake).Install(context.Background(), "web_example", "24", func(s string) { logs = append(logs, s) }); err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{nvmCommit, nvmArchiveSHA256, "curl", "sha256", "nvm install", "nvm alias default", "nvm unalias default", "previous_default", "NODE_VERSION"} {
		if !strings.Contains(script, required) {
			t.Fatalf("install script missing %q", required)
		}
	}
	if strings.Contains(script, "--latest-npm") {
		t.Fatal("install must preserve bundled npm rather than mutate an existing runtime")
	}
	if got := strings.Join(logs, "|"); got != "Preparing NVM and Node 24 for web_example|Installing Node 24|Installed Node v24.8.0 with npm 11.6.0|download progress" {
		t.Fatalf("logs = %q", got)
	}
}

func TestInstallReportsFailedVerification(t *testing.T) {
	fake := fakeExecutor{runSudo: func(context.Context, string, ...string) (*executor.Result, error) {
		return &executor.Result{Stderr: "NVM archive checksum verification failed\n", ExitCode: 1}, nil
	}}
	err := New(fake).Install(context.Background(), "web_example", "24", nil)
	if err == nil || !strings.Contains(err.Error(), "checksum verification failed") {
		t.Fatalf("Install() error = %v", err)
	}
}

func TestFixedShellArgumentForwarding(t *testing.T) {
	result, err := executor.NewExecutor(time.Second).Run(context.Background(), "/bin/bash", "-c", `printf '%s\n' "$1" "$2"`, "--", "web_example", "24; id")
	if err != nil || result.ExitCode != 0 {
		t.Fatalf("shell harness: result=%#v err=%v", result, err)
	}
	if result.Stdout != "web_example\n24; id\n" {
		t.Fatalf("arguments were re-parsed: %q", result.Stdout)
	}
}

type fakeExecutor struct {
	runSudo func(context.Context, string, ...string) (*executor.Result, error)
}

func (f fakeExecutor) Run(context.Context, string, ...string) (*executor.Result, error) {
	return nil, errors.New("unexpected Run")
}
func (f fakeExecutor) RunSudo(ctx context.Context, name string, args ...string) (*executor.Result, error) {
	return f.runSudo(ctx, name, args...)
}
func (f fakeExecutor) RunSudoWithInput(context.Context, string, string, ...string) (*executor.Result, error) {
	return nil, errors.New("unexpected RunSudoWithInput")
}
