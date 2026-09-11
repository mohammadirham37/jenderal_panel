package website

import (
	"context"
	"strings"
	"testing"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

func TestBuildCommandScript(t *testing.T) {
	w := model.Website{WebUser: "web_example_com", DocumentRoot: "/home/web_example_com/app/public"}

	composer := buildCommandScript(w, "/home/web_example_com/app", []string{"composer", "install", "--no-interaction"}, "")
	if composer != "cd /home/web_example_com/app && composer install --no-interaction" {
		t.Errorf("system-wide command should run directly, got %q", composer)
	}

	npm := buildCommandScript(w, "/home/web_example_com/app", []string{"npm", "install"}, "22")
	if !strings.Contains(npm, "NVM_DIR=/home/web_example_com/.nvm") {
		t.Errorf("npm command should set NVM_DIR, got %q", npm)
	}
	if !strings.Contains(npm, " NODE_VERSION=22 /home/web_example_com/.nvm/nvm-exec npm install") {
		t.Errorf("npm command should pin NODE_VERSION from the default alias, got %q", npm)
	}
	if !strings.HasPrefix(npm, "cd /home/web_example_com/app && ") {
		t.Errorf("npm command should run in the project directory, got %q", npm)
	}
	if !strings.Contains(npm, "Node.js runtime is not installed") {
		t.Errorf("npm command should explain a missing runtime, got %q", npm)
	}

	// Without a default alias no NODE_VERSION is injected (nvm-exec or
	// .nvmrc decides), and the missing-runtime hint stays.
	loose := buildCommandScript(w, "/home/web_example_com/app", []string{"npm", "install"}, "")
	if strings.Contains(loose, "NODE_VERSION=") {
		t.Errorf("no alias should mean no NODE_VERSION, got %q", loose)
	}
}

func TestGitPullIsAllowedAndTargetsGitRoot(t *testing.T) {
	args, ok := allowedCommands["git pull"]
	if !ok {
		t.Fatal("git pull must be an allowed command")
	}
	if strings.Join(args, " ") != "git pull" {
		t.Fatalf("git pull args = %v, want [git pull]", args)
	}

	if marker := projectMarkerFor("git pull"); marker != ".git" {
		t.Fatalf("projectMarkerFor(git pull) = %q, want .git", marker)
	}
}

func TestGitPullPresetAvailableToEveryWebsite(t *testing.T) {
	sites := []model.Website{
		{Framework: "laravel", AppType: "laravel"},
		{Framework: "codeigniter", AppType: "php"},
		{Framework: "", AppType: "php"},
		{Framework: "", AppType: "static"},
	}
	for _, w := range sites {
		found := false
		for _, preset := range commandPresetsFor(w) {
			if preset.Command == "git pull" {
				found = true
				if preset.Category != "git" {
					t.Errorf("git pull category = %q, want git", preset.Category)
				}
			}
		}
		if !found {
			t.Errorf("framework %q app %q: git pull preset missing", w.Framework, w.AppType)
		}
	}
}

func TestFindDirWithMarkerDetectsGitDirectory(t *testing.T) {
	// .git is a directory, so the marker probe must accept entries that are
	// not regular files.
	var probed []string
	mock := &executor.MockExecutor{
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			probed = append(probed, strings.Join(args, " "))
			if strings.HasSuffix(strings.Join(args, " "), "/home/web_example_com/.git") {
				return &executor.Result{ExitCode: 0}, nil
			}
			return &executor.Result{ExitCode: 1}, nil
		},
	}
	svc := &Service{exec: mock}
	w := model.Website{WebUser: "web_example_com", DocumentRoot: "/home/web_example_com/public"}

	dir := svc.findDirWithFile(context.Background(), w, ".git")
	if dir != "/home/web_example_com" {
		t.Fatalf("findDirWithFile(.git) = %q, want /home/web_example_com", dir)
	}
	if len(probed) != 2 {
		t.Fatalf("probed %d candidates (%v), want 2", len(probed), probed)
	}
}
