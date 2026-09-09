package website

import (
	"context"
	"strings"
	"testing"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
)

func TestInstallationPlanRoutesNPMThroughSelectedTenantRuntime(t *testing.T) {
	w := websiteRow{
		ID: "01TESTWEBSITE", Domain: "example.com", WebUser: "web_example_com",
		PHPVersion: "8.3", NodeVersion: "22", AppType: "laravel", DocumentRoot: "/home/web_example_com/app/public",
		Framework: "laravel", FrameworkVersion: "13", FrontendStack: "inertia", InertiaAdapter: "svelte",
		ProjectVariant: "starter-kit", SetupMode: SetupAutomatic,
	}
	steps, err := installationPlan(w)
	if err != nil {
		t.Fatal(err)
	}
	var npmSteps int
	for _, step := range steps {
		joined := strings.Join(append([]string{step.Command}, step.Args...), "\x00")
		if !strings.Contains(joined, "\x00npm\x00") {
			continue
		}
		npmSteps++
		for _, required := range []string{
			"-u\x00web_example_com\x00--\x00/usr/bin/env\x00-i",
			"NVM_DIR=/home/web_example_com/.nvm", "NODE_VERSION=22",
			"/home/web_example_com/.nvm/nvm-exec\x00npm",
		} {
			if !strings.Contains(joined, required) {
				t.Fatalf("npm step missing %q: %q", required, joined)
			}
		}
		if strings.Contains(joined, "/usr/bin/npm") {
			t.Fatalf("npm step used global npm: %q", joined)
		}
	}
	if npmSteps != 2 {
		t.Fatalf("npm steps = %d, want dependency install and asset build", npmSteps)
	}
}

func TestConfigurationOnlyProvisioningNeverInstallsNVM(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	insertTestWebsite(t, db, "ws-config-node", "config-node.example.com", "php", "8.3", "pending")
	if _, err := db.Exec(`UPDATE websites SET setup_mode = 'config-only', node_version = '24' WHERE id = 'ws-config-node'`); err != nil {
		t.Fatal(err)
	}
	mock := &executor.MockExecutor{
		RunFunc: func(context.Context, string, ...string) (*executor.Result, error) {
			return &executor.Result{ExitCode: 0}, nil
		},
		RunSudoFunc: func(_ context.Context, name string, args ...string) (*executor.Result, error) {
			joined := strings.Join(append([]string{name}, args...), "\x00")
			if strings.Contains(joined, "/.nvm") || strings.Contains(joined, "NODE_VERSION=") {
				t.Fatalf("configuration-only provisioning invoked runtime command: %q", joined)
			}
			if name == "-u" && len(args) >= 4 && args[2] == "test" {
				return &executor.Result{ExitCode: 1}, nil
			}
			return &executor.Result{ExitCode: 0}, nil
		},
		RunSudoWithInputFunc: func(context.Context, string, string, ...string) (*executor.Result, error) {
			return &executor.Result{ExitCode: 0}, nil
		},
	}
	p := NewProvisioner(db, mock, nil)
	p.ipv6Available = func() bool { return false }
	p.provision(context.Background(), "ws-config-node")
	status, message := getWebsiteStatus(t, db, "ws-config-node")
	if status != "active" {
		t.Fatalf("status=%q error=%q", status, message)
	}
}

func TestAutomaticProvisioningInstallsExplicitSelectedRuntime(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	insertTestWebsite(t, db, "ws-auto-node", "auto-node.example.com", "php", "8.3", "pending")
	if _, err := db.Exec(`UPDATE websites SET setup_mode = 'auto-install', node_version = '22' WHERE id = 'ws-auto-node'`); err != nil {
		t.Fatal(err)
	}
	installCalls := 0
	mock := &executor.MockExecutor{
		RunFunc: func(context.Context, string, ...string) (*executor.Result, error) {
			return &executor.Result{ExitCode: 0}, nil
		},
		RunSudoFunc: func(_ context.Context, name string, args ...string) (*executor.Result, error) {
			joined := strings.Join(append([]string{name}, args...), "\x00")
			if strings.Contains(joined, "\x0014m\x00") && strings.Contains(joined, "NODE_VERSION=22") {
				installCalls++
			}
			if name == "-u" && len(args) >= 4 && args[2] == "test" {
				return &executor.Result{ExitCode: 1}, nil
			}
			return &executor.Result{ExitCode: 0}, nil
		},
		RunSudoWithInputFunc: func(context.Context, string, string, ...string) (*executor.Result, error) {
			return &executor.Result{ExitCode: 0}, nil
		},
	}
	p := NewProvisioner(db, mock, nil)
	p.ipv6Available = func() bool { return false }
	p.provision(context.Background(), "ws-auto-node")
	status, message := getWebsiteStatus(t, db, "ws-auto-node")
	if status != "active" || installCalls != 1 {
		t.Fatalf("status=%q error=%q installCalls=%d", status, message, installCalls)
	}
}
