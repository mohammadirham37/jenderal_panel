package website

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"github.com/mohammadirham37/jenderal_panel/internal/model"
	"github.com/mohammadirham37/jenderal_panel/internal/taskrunner"
)

// CommandPreset represents a predefined command that can be run against a
// website, grouped by category.
type CommandPreset struct {
	Label    string `json:"label"`
	Command  string `json:"command"`
	Category string `json:"category"`
	Danger   bool   `json:"danger"`
}

// allowedCommands maps human-readable command labels to the actual argument
// slices that will be executed. Only commands present in this map may be run.
var allowedCommands = map[string][]string{
	"cp .env.example .env":         {"cp", ".env.example", ".env"},
	"git pull":                     {"git", "pull"},
	"composer install":             {"composer", "install", "--no-interaction"},
	"composer update":              {"composer", "update", "--no-interaction"},
	"composer dump-autoload":       {"composer", "dump-autoload"},
	"php artisan key:generate":     {"php", "artisan", "key:generate"},
	"php artisan migrate":          {"php", "artisan", "migrate", "--force"},
	"php artisan migrate:fresh":    {"php", "artisan", "migrate:fresh", "--force"},
	"php artisan migrate:rollback": {"php", "artisan", "migrate:rollback"},
	"php artisan db:seed":          {"php", "artisan", "db:seed", "--force"},
	"php artisan config:cache":     {"php", "artisan", "config:cache"},
	"php artisan route:cache":      {"php", "artisan", "route:cache"},
	"php artisan view:cache":       {"php", "artisan", "view:cache"},
	"php artisan cache:clear":      {"php", "artisan", "cache:clear"},
	"php artisan optimize":         {"php", "artisan", "optimize"},
	"php artisan storage:link":     {"php", "artisan", "storage:link"},
	"php artisan queue:restart":    {"php", "artisan", "queue:restart"},
	"php artisan octane:reload":    {"php", "artisan", "octane:reload"},
	"php spark migrate":            {"php", "spark", "migrate"},
	"php spark migrate:rollback":   {"php", "spark", "migrate:rollback"},
	"php spark db:seed":            {"php", "spark", "db:seed"},
	"php spark cache:clear":        {"php", "spark", "cache:clear"},
	"npm install":                  {"npm", "install"},
	"npm run build":                {"npm", "run", "build"},
	"npm run dev":                  {"npm", "run", "dev"},
	"yarn install":                 {"yarn", "install"},
	"yarn build":                   {"yarn", "build"},
	"pnpm install":                 {"pnpm", "install"},
	"pnpm build":                   {"pnpm", "build"},
}

// serviceTaskRunners maps Service instances to their task runners. This avoids
// modifying the existing Service struct definition.
var serviceTaskRunners sync.Map // map[*Service]*taskrunner.Runner

// SetTaskRunner assigns a task runner to the service, enabling background
// command execution. Call this after construction to avoid circular
// dependencies.
func (s *Service) SetTaskRunner(tr *taskrunner.Runner) {
	serviceTaskRunners.Store(s, tr)
}

// taskRunner returns the task runner associated with this service, or nil.
func (s *Service) taskRunner() *taskrunner.Runner {
	v, ok := serviceTaskRunners.Load(s)
	if !ok {
		return nil
	}
	return v.(*taskrunner.Runner)
}

// GetCommandPresets returns the list of predefined commands available for a
// website based on its framework.
func (s *Service) GetCommandPresets(ctx context.Context, websiteID string) ([]CommandPreset, error) {
	w, err := s.Get(ctx, websiteID)
	if err != nil {
		return nil, err
	}
	return commandPresetsFor(w), nil
}

// commandPresetsFor builds the preset list offered for a website.
func commandPresetsFor(w model.Website) []CommandPreset {
	var presets []CommandPreset

	// git pull applies to any website that is a git checkout.
	presets = append(presets,
		CommandPreset{Label: "git pull", Command: "git pull", Category: "git", Danger: false},
	)

	framework := strings.ToLower(w.Framework)

	// PHP frameworks get composer commands.
	if framework == "laravel" || framework == "codeigniter" || framework == "codeigniter4" || framework == "php" || w.AppType == "php" {
		presets = append(presets,
			CommandPreset{Label: "composer install", Command: "composer install", Category: "composer", Danger: false},
			CommandPreset{Label: "composer update", Command: "composer update", Category: "composer", Danger: false},
			CommandPreset{Label: "composer dump-autoload", Command: "composer dump-autoload", Category: "composer", Danger: false},
		)
	}

	// Laravel artisan commands. The .env creation is offered by the dedicated
	// Laravel .env card on the same tab, so it is not listed as a preset here.
	if framework == "laravel" {
		presets = append(presets,
			CommandPreset{Label: "php artisan key:generate", Command: "php artisan key:generate", Category: "artisan", Danger: false},
			CommandPreset{Label: "php artisan migrate", Command: "php artisan migrate", Category: "artisan", Danger: false},
			CommandPreset{Label: "php artisan migrate:fresh", Command: "php artisan migrate:fresh", Category: "artisan", Danger: true},
			CommandPreset{Label: "php artisan migrate:rollback", Command: "php artisan migrate:rollback", Category: "artisan", Danger: false},
			CommandPreset{Label: "php artisan db:seed", Command: "php artisan db:seed", Category: "artisan", Danger: false},
			CommandPreset{Label: "php artisan config:cache", Command: "php artisan config:cache", Category: "artisan", Danger: false},
			CommandPreset{Label: "php artisan route:cache", Command: "php artisan route:cache", Category: "artisan", Danger: false},
			CommandPreset{Label: "php artisan view:cache", Command: "php artisan view:cache", Category: "artisan", Danger: false},
			CommandPreset{Label: "php artisan cache:clear", Command: "php artisan cache:clear", Category: "artisan", Danger: false},
			CommandPreset{Label: "php artisan optimize", Command: "php artisan optimize", Category: "artisan", Danger: false},
			CommandPreset{Label: "php artisan storage:link", Command: "php artisan storage:link", Category: "artisan", Danger: false},
			CommandPreset{Label: "php artisan queue:restart", Command: "php artisan queue:restart", Category: "artisan", Danger: false},
		)
		if w.OctaneEnabled {
			// Zero-downtime deploy step for Octane sites: the common loop
			// is git pull followed by octane:reload.
			presets = append(presets,
				CommandPreset{Label: "php artisan octane:reload", Command: "php artisan octane:reload", Category: "octane", Danger: false},
			)
		}
	}

	// CodeIgniter spark commands.
	if framework == "codeigniter" || framework == "codeigniter4" {
		presets = append(presets,
			CommandPreset{Label: "php spark migrate", Command: "php spark migrate", Category: "spark", Danger: false},
			CommandPreset{Label: "php spark migrate:rollback", Command: "php spark migrate:rollback", Category: "spark", Danger: false},
			CommandPreset{Label: "php spark db:seed", Command: "php spark db:seed", Category: "spark", Danger: false},
			CommandPreset{Label: "php spark cache:clear", Command: "php spark cache:clear", Category: "spark", Danger: false},
		)
	}

	// Node/frontend commands are always available.
	presets = append(presets,
		CommandPreset{Label: "npm install", Command: "npm install", Category: "npm", Danger: false},
		CommandPreset{Label: "npm run build", Command: "npm run build", Category: "npm", Danger: false},
		CommandPreset{Label: "npm run dev", Command: "npm run dev", Category: "npm", Danger: false},
		CommandPreset{Label: "yarn install", Command: "yarn install", Category: "yarn", Danger: false},
		CommandPreset{Label: "yarn build", Command: "yarn build", Category: "yarn", Danger: false},
		CommandPreset{Label: "pnpm install", Command: "pnpm install", Category: "pnpm", Danger: false},
		CommandPreset{Label: "pnpm build", Command: "pnpm build", Category: "pnpm", Danger: false},
	)

	return presets
}

// projectMarkerFor returns the file that identifies the directory a command
// should run in, or "" to fall back to the document root.
func projectMarkerFor(command string) string {
	switch {
	case strings.HasPrefix(command, "composer"):
		return "composer.json"
	case strings.HasPrefix(command, "php artisan"):
		return "artisan"
	case strings.HasPrefix(command, "npm"), strings.HasPrefix(command, "yarn"), strings.HasPrefix(command, "pnpm"):
		return "package.json"
	case strings.HasPrefix(command, "cp .env.example"):
		return ".env.example"
	case strings.HasPrefix(command, "git"):
		// .git is a directory, so findDirWithFile probes with test -e.
		return ".git"
	default:
		return ""
	}
}

// projectDirCandidates returns the directories that may hold a website's
// project files, most specific first: <home>/app for Laravel automatic
// installs (document root is app/public), the document root, then the home
// directory.
func (s *Service) projectDirCandidates(w model.Website) []string {
	homeDir := "/home/" + w.WebUser
	candidates := []string{w.DocumentRoot}
	if filepath.Clean(w.DocumentRoot) == filepath.Join(homeDir, "app", "public") {
		candidates = append([]string{filepath.Join(homeDir, "app")}, candidates...)
	}
	candidates = append(candidates, homeDir)
	return candidates
}

// findDirWithFile returns the first project candidate directory containing
// the given entry (file or directory — .git is a directory), or "" when no
// candidate has it.
func (s *Service) findDirWithFile(ctx context.Context, w model.Website, name string) string {
	for _, dir := range s.projectDirCandidates(w) {
		res, err := s.exec.RunSudo(ctx, "test", "-e", filepath.Join(dir, name))
		if err == nil && res.ExitCode == 0 {
			return dir
		}
	}
	return ""
}

// usesNvmRuntime reports whether the command's binary is provided by the
// website's NVM runtime rather than installed system-wide.
func usesNvmRuntime(binary string) bool {
	switch binary {
	case "npm", "yarn", "pnpm", "node":
		return true
	default:
		return false
	}
}

// buildCommandScript assembles the shell command the task runner executes as
// the website user. Node.js package managers live inside the website's NVM
// runtime (~/.nvm), which a non-interactive shell does not load, so they run
// through nvm-exec with an explicit NODE_VERSION (the site's default alias).
// When the project pins a version via .nvmrc it is left to nvm-exec. Missing
// runtimes produce an actionable error instead of nvm's terse output.
func buildCommandScript(w model.Website, workDir string, args []string, nodeVersion string) string {
	joined := strings.Join(args, " ")
	home := "/home/" + w.WebUser
	if !usesNvmRuntime(args[0]) {
		return "cd " + workDir + " && " + joined
	}
	nvmDir := home + "/.nvm"
	var env string
	if nodeVersion != "" {
		env = " NODE_VERSION=" + nodeVersion
	}
	return fmt.Sprintf(
		"cd %[1]s && if [ -x %[2]s/nvm-exec ]; then NVM_DIR=%[2]s%[4]s %[2]s/nvm-exec %[3]s; else echo \"Node.js runtime is not installed for this website — install it from the website detail page first\"; exit 127; fi",
		workDir, nvmDir, joined, env,
	)
}

// nvmDefaultVersion reads the website's NVM default alias (e.g. "22" or
// "v22.11.0"). Returns "" when no alias is set.
func (s *Service) nvmDefaultVersion(ctx context.Context, w model.Website) string {
	alias := "/home/" + w.WebUser + "/.nvm/alias/default"
	res, err := s.exec.RunSudo(ctx, "cat", alias)
	if err != nil || res == nil || res.ExitCode != 0 {
		return ""
	}
	return strings.TrimSpace(res.Stdout)
}

// hasProjectFile reports whether name exists in workDir.
func (s *Service) hasProjectFile(ctx context.Context, workDir, name string) bool {
	res, err := s.exec.RunSudo(ctx, "test", "-f", workDir+"/"+name)
	return err == nil && res != nil && res.ExitCode == 0
}

// RunCommand executes a predefined command against a website in the
// background. The command must be present in the allowedCommands map. Returns
// the background task ID.
func (s *Service) RunCommand(ctx context.Context, websiteID string, command string) (string, error) {
	args, ok := allowedCommands[command]
	if !ok {
		return "", model.NewValidationError("command not allowed: " + command)
	}

	w, err := s.Get(ctx, websiteID)
	if err != nil {
		return "", err
	}

	tr := s.taskRunner()
	if tr == nil {
		return "", fmt.Errorf("task runner not available")
	}

	// Run in the directory that actually holds the project files.
	workDir := w.DocumentRoot
	if marker := projectMarkerFor(command); marker != "" {
		if dir := s.findDirWithFile(ctx, w, marker); dir != "" {
			workDir = dir
		}
	}

	// Node commands need the website's NVM runtime; pin the default alias
	// unless the project pins its own version via .nvmrc.
	nodeVersion := ""
	if usesNvmRuntime(args[0]) && !s.hasProjectFile(ctx, workDir, ".nvmrc") {
		nodeVersion = s.nvmDefaultVersion(ctx, w)
	}

	shellCmd := buildCommandScript(w, workDir, args, nodeVersion)

	taskID := tr.Run(
		command+" ("+w.Domain+")",
		"sudo", "-u", w.WebUser, "bash", "-c", shellCmd,
	)

	return taskID, nil
}
