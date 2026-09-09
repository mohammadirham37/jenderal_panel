package website

import (
	"context"
	"fmt"
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

	var presets []CommandPreset

	framework := strings.ToLower(w.Framework)

	// PHP frameworks get composer commands.
	if framework == "laravel" || framework == "codeigniter" || framework == "codeigniter4" || framework == "php" || w.AppType == "php" {
		presets = append(presets,
			CommandPreset{Label: "composer install", Command: "composer install", Category: "composer", Danger: false},
			CommandPreset{Label: "composer update", Command: "composer update", Category: "composer", Danger: false},
			CommandPreset{Label: "composer dump-autoload", Command: "composer dump-autoload", Category: "composer", Danger: false},
		)
	}

	// Laravel artisan commands.
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

	return presets, nil
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

	// Run from home directory (not document_root) so artisan/composer/npm
	// work from project root, not the public/ subdirectory.
	homeDir := "/home/" + w.WebUser
	shellCmd := "cd " + homeDir + " && " + strings.Join(args, " ")

	taskID := tr.Run(
		command+" ("+w.Domain+")",
		"sudo", "-u", w.WebUser, "bash", "-c", shellCmd,
	)

	return taskID, nil
}
