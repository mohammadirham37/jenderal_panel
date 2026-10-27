package hardening

import (
	"context"
	"strings"
	"testing"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
)

type nginxHost struct {
	installed, adminTokens, configBroken bool
	files                                map[string]string
	calls                                []string
}

func (h *nginxHost) exec() *executor.MockExecutor {
	return &executor.MockExecutor{
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			h.calls = append(h.calls, name+" "+strings.Join(args, " "))
			switch name {
			case "test":
				if h.installed {
					return &executor.Result{}, nil
				}
				return &executor.Result{ExitCode: 1}, nil
			case "cat":
				if v, ok := h.files[args[len(args)-1]]; ok {
					return &executor.Result{Stdout: v}, nil
				}
				return &executor.Result{ExitCode: 1}, nil
			case "grep":
				if h.adminTokens {
					return &executor.Result{}, nil
				}
				return &executor.Result{ExitCode: 1}, nil
			case "nginx":
				if h.configBroken {
					return &executor.Result{ExitCode: 1, Stderr: "duplicate"}, nil
				}
			case "rm":
				delete(h.files, args[len(args)-1])
			}
			return &executor.Result{}, nil
		},
		RunSudoWithInputFunc: func(ctx context.Context, input, name string, args ...string) (*executor.Result, error) {
			h.files[args[len(args)-1]] = input
			return &executor.Result{}, nil
		},
	}
}

func TestServerTokensOffIsInstalledOnceAndReloads(t *testing.T) {
	host := &nginxHost{installed: true, files: map[string]string{}}
	changed, err := EnsureNginxServerTokensOff(context.Background(), host.exec())
	if err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	if !strings.Contains(host.files[NginxTokensPath], "server_tokens off;") {
		t.Error("drop-in not written")
	}
	if !strings.Contains(strings.Join(host.calls, "\n"), "systemctl reload nginx") {
		t.Error("nginx not reloaded")
	}
	host.calls = nil
	if changed, _ := EnsureNginxServerTokensOff(context.Background(), host.exec()); changed {
		t.Error("second run must be a no-op")
	}
}

func TestServerTokensRespectsAdminSettingAndRollsBack(t *testing.T) {
	admin := &nginxHost{installed: true, adminTokens: true, files: map[string]string{}}
	if changed, err := EnsureNginxServerTokensOff(context.Background(), admin.exec()); changed || err != nil {
		t.Errorf("an existing server_tokens must be left alone: %v %v", changed, err)
	}
	broken := &nginxHost{installed: true, configBroken: true, files: map[string]string{}}
	if _, err := EnsureNginxServerTokensOff(context.Background(), broken.exec()); err == nil {
		t.Error("a failed nginx -t must be reported")
	}
	if _, ok := broken.files[NginxTokensPath]; ok {
		t.Error("the drop-in must be removed when nginx rejects it")
	}
	none := &nginxHost{files: map[string]string{}}
	if changed, err := EnsureNginxServerTokensOff(context.Background(), none.exec()); changed || err != nil {
		t.Error("servers without nginx must be left alone")
	}
}
