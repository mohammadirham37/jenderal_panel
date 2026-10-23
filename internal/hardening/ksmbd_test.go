package hardening

import (
	"context"
	"strings"
	"testing"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
)

type ksmbdHost struct {
	serviceActive bool
	blockContent  string
	moduleLoaded  bool
	calls         []string
}

func (h *ksmbdHost) exec() *executor.MockExecutor {
	return &executor.MockExecutor{
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			call := name + " " + strings.Join(args, " ")
			h.calls = append(h.calls, call)
			switch {
			case name == "systemctl":
				if h.serviceActive {
					return &executor.Result{}, nil
				}
				return &executor.Result{ExitCode: 3}, nil
			case name == "cat":
				if h.blockContent == "" {
					return &executor.Result{ExitCode: 1}, nil
				}
				return &executor.Result{Stdout: h.blockContent}, nil
			case name == "test":
				if h.moduleLoaded {
					return &executor.Result{}, nil
				}
				return &executor.Result{ExitCode: 1}, nil
			case name == "modprobe":
				h.moduleLoaded = false
			}
			return &executor.Result{}, nil
		},
		RunSudoWithInputFunc: func(ctx context.Context, input, name string, args ...string) (*executor.Result, error) {
			h.calls = append(h.calls, name+" "+strings.Join(args, " "))
			h.blockContent = input
			return &executor.Result{}, nil
		},
	}
}

func TestEnsureKsmbdDisabledBlocksAndUnloadsAnUnusedModule(t *testing.T) {
	host := &ksmbdHost{moduleLoaded: true}
	report, err := EnsureKsmbdDisabled(context.Background(), host.exec())
	if err != nil {
		t.Fatal(err)
	}
	if !report.Installed || !report.Unloaded || report.InUse {
		t.Errorf("unexpected report %+v", report)
	}
	if !strings.Contains(host.blockContent, "install ksmbd /bin/false") {
		t.Errorf("modprobe block missing the install override: %q", host.blockContent)
	}
	joined := strings.Join(host.calls, "\n")
	if !strings.Contains(joined, "tee "+KsmbdBlockPath) || !strings.Contains(joined, "modprobe -r ksmbd") {
		t.Errorf("unexpected calls:\n%s", joined)
	}

	// A second start finds everything in place and changes nothing.
	host.calls = nil
	report, err = EnsureKsmbdDisabled(context.Background(), host.exec())
	if err != nil || report.Installed || report.Unloaded {
		t.Errorf("second run should be a no-op: %+v, %v", report, err)
	}
}

// A server that deliberately shares files over ksmbd keeps working.
func TestEnsureKsmbdDisabledLeavesAnActiveFileServerAlone(t *testing.T) {
	host := &ksmbdHost{serviceActive: true, moduleLoaded: true}
	report, err := EnsureKsmbdDisabled(context.Background(), host.exec())
	if err != nil {
		t.Fatal(err)
	}
	if !report.InUse {
		t.Errorf("expected InUse, got %+v", report)
	}
	for _, call := range host.calls {
		if strings.HasPrefix(call, "tee") || strings.HasPrefix(call, "modprobe") {
			t.Errorf("must not touch an active ksmbd server: %s", call)
		}
	}
}
