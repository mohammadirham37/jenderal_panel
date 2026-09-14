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

// A virtual filesystem for hasProjectFile/findDirWithFile probes: the keys
// are "test -e/-f" paths that answer "exists".
func setupProbeExecutor(existing map[string]bool) *executor.MockExecutor {
	return &executor.MockExecutor{
		RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return &executor.Result{ExitCode: 0}, nil
		},
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			if name == "test" && len(args) == 2 && (args[0] == "-e" || args[0] == "-f") {
				if existing[args[1]] {
					return &executor.Result{ExitCode: 0}, nil
				}
				return &executor.Result{ExitCode: 1}, nil
			}
			if name == "cat" && len(args) == 1 && strings.HasSuffix(args[0], "/.nvm/alias/default") {
				return &executor.Result{ExitCode: 0, Stdout: "22\n"}, nil
			}
			return &executor.Result{ExitCode: 0}, nil
		},
	}
}

func TestBuildInitialSetupScriptComposesConditionalChain(t *testing.T) {
	existing := map[string]bool{
		"/home/web_example/app/composer.json": true,
		"/home/web_example/app/artisan":       true,
		"/home/web_example/app/package.json":  true,
		"/home/web_example/app/.env.example":  true,
		"/home/web_example/app/.env":          true,
	}
	svc := NewService(nil, setupProbeExecutor(existing), nil)
	w := model.Website{WebUser: "web_example", DocumentRoot: "/home/web_example/app/public"}

	script, err := svc.buildInitialSetupScript(context.Background(), w)
	if err != nil {
		t.Fatalf("buildInitialSetupScript() error = %v", err)
	}
	if !strings.HasPrefix(script, "cd /home/web_example/app && ") {
		t.Errorf("chain must run in the composer project root, got %q", script)
	}
	for _, want := range []string{
		"composer install --no-interaction",
		"php artisan key:generate --force",
		"nvm-exec npm install",
		"nvm-exec npm run build",
		"NODE_VERSION=22",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("chain missing %q:\n%s", want, script)
		}
	}
	// .env already exists: the copy step must be skipped.
	if strings.Contains(script, "cp .env.example") {
		t.Errorf("chain must not overwrite an existing .env:\n%s", script)
	}
}

func TestBuildInitialSetupScriptIncludesEnvCopyWhenMissing(t *testing.T) {
	existing := map[string]bool{
		"/home/web_example/app/composer.json": true,
		"/home/web_example/app/.env.example":  true,
	}
	svc := NewService(nil, setupProbeExecutor(existing), nil)
	w := model.Website{WebUser: "web_example", DocumentRoot: "/home/web_example/app/public"}

	script, err := svc.buildInitialSetupScript(context.Background(), w)
	if err != nil {
		t.Fatalf("buildInitialSetupScript() error = %v", err)
	}
	if !strings.Contains(script, "cp .env.example .env") {
		t.Errorf("chain must bootstrap .env when it is missing:\n%s", script)
	}
	if strings.Contains(script, "npm") {
		t.Errorf("no package.json: npm steps must be skipped:\n%s", script)
	}
}

func TestInitialSetupPresetOnlyWhileComposerNotInstalled(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	probeSvc := NewService(db, setupProbeExecutor(nil), nil)
	created, err := probeSvc.Create(context.Background(), CreateRequest{
		Domain: "setup.example.com", Template: "laravel", PHPVersion: "8.3", SetupMode: "config-only",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	appRoot := "/home/" + created.WebUser + "/app"

	existing := map[string]bool{appRoot + "/composer.json": true}
	svc := NewService(db, setupProbeExecutor(existing), nil)

	presets, err := svc.GetCommandPresets(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("GetCommandPresets() error = %v", err)
	}
	found := false
	for _, p := range presets {
		if p.Label == InitialSetupCommand {
			found = true
		}
	}
	if !found {
		t.Errorf("initial setup preset must be offered before composer install, got %+v", presets)
	}

	// Once vendor/autoload.php exists the one-click chain is hidden and the
	// individual presets take over.
	existing[appRoot+"/vendor/autoload.php"] = true
	presets, err = svc.GetCommandPresets(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("GetCommandPresets() after install error = %v", err)
	}
	for _, p := range presets {
		if p.Label == InitialSetupCommand {
			t.Errorf("initial setup preset must disappear once composer install ran, got %+v", presets)
		}
	}
}
