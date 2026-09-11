package website

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

// wpToolboxTimeout bounds one wp-cli invocation.
const wpToolboxTimeout = 10 * time.Minute

// WpStatus is the API view of a WordPress installation's health.
type WpStatus struct {
	Installed    bool   `json:"installed"`
	CoreVersion  string `json:"core_version,omitempty"`
	CoreUpdate   bool   `json:"core_update"`
	PluginUpdate bool   `json:"plugin_update"`
	ThemeUpdate  bool   `json:"theme_update"`
}

// wpContext resolves the paths and runtime details wp-cli needs for a site.
type wpContext struct {
	phar    string // wp-cli.phar location inside the site home
	path    string // document root handed to wp-cli --path
	php     string // full PHP CLI binary for the site's version
	webUser string
	domain  string
}

func (s *Service) wpContextFor(w model.Website) wpContext {
	home := filepath.Join("/home", w.WebUser)
	return wpContext{
		phar:    filepath.Join(home, ".wp-cli", "wp-cli.phar"),
		path:    w.DocumentRoot,
		php:     "/usr/bin/php" + w.PHPVersion,
		webUser: w.WebUser,
		domain:  w.Domain,
	}
}

// wpContextForID loads the website and resolves its wp-cli context.
func (s *Service) wpContextForID(ctx context.Context, websiteID string) (wpContext, model.Website, error) {
	w, err := s.Get(ctx, websiteID)
	if err != nil {
		return wpContext{}, model.Website{}, err
	}
	if w.AppType != "wordpress" {
		return wpContext{}, model.Website{}, model.NewValidationError("the WordPress toolkit is only available for WordPress sites")
	}
	return s.wpContextFor(w), w, nil
}

// runWp executes one wp-cli invocation as the web user and returns the
// captured output. The phar and path travel as argv; nothing is interpolated
// through a shell.
func (s *Service) runWp(ctx context.Context, wpCtx wpContext, webUser string, args ...string) (string, error) {
	full := append([]string{wpCtx.php, wpCtx.phar, "--path=" + wpCtx.path}, args...)
	runCtx, cancel := context.WithTimeout(ctx, wpToolboxTimeout)
	defer cancel()
	result, err := s.exec.RunSudo(runCtx, "-u", append([]string{webUser, "--"}, full...)...)
	if err != nil {
		return "", err
	}
	if result.ExitCode != 0 {
		return "", fmt.Errorf("wp %s failed (exit %d): %s",
			strings.Join(args, " "), result.ExitCode, strings.TrimSpace(result.Stderr))
	}
	return result.Stdout, nil
}

// WpStatus reports whether WordPress is installed plus core, plugin, and
// theme update availability.
func (s *Service) WpStatus(ctx context.Context, websiteID string) (WpStatus, error) {
	status := WpStatus{}

	wpCtx, w, err := s.wpContextForID(ctx, websiteID)
	if err != nil {
		return status, err
	}

	// A missing wp-config means the automatic install has not completed.
	if !s.hasProjectFile(ctx, wpCtx.path, "wp-config.php") {
		return status, nil
	}

	versionOut, err := s.runWp(ctx, wpCtx, w.WebUser, "core", "version")
	if err != nil {
		return status, nil // config present but wp-cli cannot run yet
	}
	status.Installed = true
	status.CoreVersion = strings.TrimSpace(versionOut)

	if coreUpdates, err := s.runWp(ctx, wpCtx, w.WebUser, "core", "check-update", "--format=count"); err == nil {
		trimmed := strings.TrimSpace(coreUpdates)
		status.CoreUpdate = trimmed != "0" && trimmed != ""
	}
	if pluginUpdates, err := s.runWp(ctx, wpCtx, w.WebUser, "plugin", "list", "--update=available", "--format=count"); err == nil {
		trimmed := strings.TrimSpace(pluginUpdates)
		status.PluginUpdate = trimmed != "0" && trimmed != ""
	}
	if themeUpdates, err := s.runWp(ctx, wpCtx, w.WebUser, "theme", "list", "--update=available", "--format=count"); err == nil {
		trimmed := strings.TrimSpace(themeUpdates)
		status.ThemeUpdate = trimmed != "0" && trimmed != ""
	}
	return status, nil
}

// wpActions maps toolkit actions to their wp-cli arguments.
var wpActions = map[string][]string{
	"core-update":    {"core", "update"},
	"core-update-db": {"core", "update-db"},
	"plugin-update":  {"plugin", "update", "--all"},
	"theme-update":   {"theme", "update", "--all"},
	"cache-flush":    {"cache", "flush"},
}

// WpRunTask executes a wp-cli maintenance action as a visible background
// task and returns the task ID.
func (s *Service) WpRunTask(ctx context.Context, websiteID, action string) (string, error) {
	wpArgs, ok := wpActions[action]
	if !ok {
		return "", model.NewValidationError("unknown WordPress action: " + action)
	}

	wpCtx, w, err := s.wpContextForID(ctx, websiteID)
	if err != nil {
		return "", err
	}

	if s.tasks == nil {
		return "", fmt.Errorf("task runner not available")
	}

	full := append([]string{wpCtx.php, wpCtx.phar, "--path=" + wpCtx.path}, wpArgs...)
	taskArgs := append([]string{"-u", w.WebUser, "--"}, full...)
	return s.tasks.Run("WP "+action+" — "+w.Domain, "sudo", taskArgs...), nil
}
