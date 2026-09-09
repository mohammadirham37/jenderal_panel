package website

import (
	"context"
	"strings"
	"testing"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
)

func TestInstallationPlanUsesPinnedAllowlistedCommands(t *testing.T) {
	tests := []struct {
		name string
		row  websiteRow
		want []string
	}{
		{"laravel 11", automaticRow("laravel", "11", "blade", "", "empty"), []string{"/usr/bin/php8.3", "/usr/local/bin/composer", "create-project", "laravel/laravel:^11.0", "--no-scripts"}},
		{"codeigniter 4", automaticRow("codeigniter4", "4", "", "", "empty"), []string{"/usr/bin/php8.3", "/usr/local/bin/composer", "create-project", "codeigniter4/appstarter", "--no-scripts"}},
		{"codeigniter 3", automaticRow("codeigniter3", "3", "", "", "empty"), []string{"https://github.com/bcit-ci/CodeIgniter.git", "3.1.13", "bcb17eb8ba53a85de154439d0ab8ff1bed047bc9"}},
		{"laravel 13 svelte", automaticRow("laravel", "13", "inertia", "svelte", "starter-kit"), []string{"https://github.com/laravel/svelte-starter-kit.git", "593365653c38308fbea55fc90dfb22c554702b81", "npm", "install", "--no-audit", "--no-fund", "run", "build"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			steps, err := installationPlan(tt.row)
			if err != nil {
				t.Fatal(err)
			}
			var flattened []string
			for _, step := range steps {
				command := append([]string{step.Command}, step.Args...)
				if len(command) < 4 || command[0] != "-u" || command[1] != tt.row.WebUser || command[2] != "--" {
					t.Fatalf("command does not run as website user: %q", command)
				}
				if !strings.Contains(strings.Join(command, " "), "HOME=/home/"+tt.row.WebUser) {
					t.Fatalf("command does not set website HOME: %q", command)
				}
				flattened = append(flattened, command...)
				flattened = append(flattened, step.ExpectedOutput)
			}
			joined := strings.Join(flattened, " ")
			for _, want := range tt.want {
				if !strings.Contains(joined, want) {
					t.Errorf("plan missing %q: %s", want, joined)
				}
			}
			if strings.Contains(joined, " php ") || strings.Contains(joined, " composer ") {
				t.Errorf("plan uses ambient PHP or Composer: %s", joined)
			}
		})
	}
}

func TestInstallationPlanRejectsStoredPackageInjection(t *testing.T) {
	row := automaticRow("laravel", "13; touch /tmp/owned", "blade", "", "empty")
	if _, err := installationPlan(row); err == nil {
		t.Fatal("installationPlan accepted a non-allowlisted version")
	}
}

func TestInstallerPreservesExistingFinalProject(t *testing.T) {
	row := automaticRow("laravel", "12", "blade", "", "empty")
	var destructive bool
	mock := &executor.MockExecutor{RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
		joined := strings.Join(append([]string{name}, args...), " ")
		if strings.Contains(joined, "test -e /home/web_example_com/app") || strings.Contains(joined, "test -f /home/web_example_com/app/public/index.php") {
			return &executor.Result{ExitCode: 0}, nil
		}
		if strings.Contains(joined, "rm -rf") || strings.Contains(joined, "create-project") {
			destructive = true
		}
		return &executor.Result{ExitCode: 0}, nil
	}}
	if err := NewInstaller(mock).Install(context.Background(), row, func(string, string) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if destructive {
		t.Fatal("existing valid final project was modified")
	}
}

func TestInstallerClearsOnlyValidatedWebsiteStagingDirectory(t *testing.T) {
	row := automaticRow("laravel", "12", "blade", "", "empty")
	staging := "/home/web_example_com/.jenderal-install-01TESTWEBSITE"
	var removed []string
	mock := &executor.MockExecutor{RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
		joined := strings.Join(append([]string{name}, args...), " ")
		if strings.Contains(joined, "/usr/bin/test -e /home/web_example_com/app") || strings.Contains(joined, "/usr/bin/test -L /home/web_example_com/app") {
			return &executor.Result{ExitCode: 1}, nil
		}
		if strings.Contains(joined, "/usr/bin/test -e "+staging) {
			return &executor.Result{ExitCode: 0}, nil
		}
		if strings.Contains(joined, "/usr/bin/rm") {
			removed = append([]string{name}, args...)
			return &executor.Result{ExitCode: 0}, nil
		}
		if strings.Contains(joined, "create-project") {
			return &executor.Result{ExitCode: 1, Stderr: "stop after cleanup"}, nil
		}
		return &executor.Result{ExitCode: 1}, nil
	}}
	err := NewInstaller(mock).Install(context.Background(), row, func(string, string) error { return nil })
	if err == nil {
		t.Fatal("Install() error = nil")
	}
	want := []string{"-u", "web_example_com", "--", "/usr/bin/rm", "-rf", "--", staging}
	if strings.Join(removed, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("cleanup command = %q, want %q", removed, want)
	}
}

func TestInstallerPublishesStageBeforeStartingLongCommand(t *testing.T) {
	row := automaticRow("laravel", "12", "blade", "", "empty")
	stagePublished := false
	mock := &executor.MockExecutor{RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
		joined := strings.Join(append([]string{name}, args...), " ")
		if strings.Contains(joined, "/usr/bin/test") {
			return &executor.Result{ExitCode: 1}, nil
		}
		if strings.Contains(joined, "create-project") {
			if !stagePublished {
				t.Fatal("long-running install command started before its stage was published")
			}
			return &executor.Result{ExitCode: 1, Stderr: "download failed"}, nil
		}
		return &executor.Result{ExitCode: 0}, nil
	}}
	err := NewInstaller(mock).Install(context.Background(), row, func(stage, output string) error {
		if stage == "installing framework" && strings.Contains(output, "install Laravel") {
			stagePublished = true
		}
		return nil
	})
	if err == nil || !stagePublished {
		t.Fatalf("Install() error=%v stagePublished=%v", err, stagePublished)
	}
	if strings.Contains(err.Error(), "download failed") || !strings.Contains(err.Error(), "see provisioning log") {
		t.Fatalf("Install() returned unsafe or unhelpful error: %v", err)
	}
}

func automaticRow(template, version, frontend, adapter, variant string) websiteRow {
	framework := "none"
	appType := "php"
	docRoot := "/home/web_example_com/public"
	if template == "laravel" {
		framework, appType, docRoot = "laravel", "laravel", "/home/web_example_com/app/public"
	}
	if strings.HasPrefix(template, "codeigniter") {
		framework = "codeigniter"
		if template == "codeigniter4" {
			docRoot = "/home/web_example_com/app/public"
		}
	}
	return websiteRow{ID: "01TESTWEBSITE", Domain: "example.com", WebUser: "web_example_com", PHPVersion: "8.3", AppType: appType,
		DocumentRoot: docRoot, Framework: framework, FrameworkVersion: version, FrontendStack: frontend,
		InertiaAdapter: adapter, ProjectVariant: variant, SetupMode: SetupAutomatic}
}
