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
	if !s.isWordPressSite(ctx, w) {
		return wpContext{}, model.Website{}, model.NewValidationError("the WordPress toolkit is only available for WordPress sites")
	}
	wpCtx := s.wpContextFor(w)
	if err := s.ensureWpCli(ctx, wpCtx); err != nil {
		return wpContext{}, model.Website{}, err
	}
	return wpCtx, w, nil
}

// isWordPressSite reports whether the toolkit applies to w: sites created
// from the WordPress template, plus plain PHP sites where WordPress was
// uploaded or migrated by hand (wp-load.php in the document root).
func (s *Service) isWordPressSite(ctx context.Context, w model.Website) bool {
	if w.AppType == "wordpress" {
		return true
	}
	if w.PHPVersion == "" || w.AppType == "laravel" || w.DocumentRoot == "" {
		return false
	}
	return s.hasProjectFile(ctx, w.DocumentRoot, "wp-load.php")
}

// DetectWordPress reports whether the WordPress toolkit applies to a site.
func (s *Service) DetectWordPress(ctx context.Context, websiteID string) (bool, error) {
	w, err := s.Get(ctx, websiteID)
	if err != nil {
		return false, err
	}
	return s.isWordPressSite(ctx, w), nil
}

// wpCliURL is the official wp-cli phar, also used by the WordPress installer.
const wpCliURL = "https://raw.githubusercontent.com/wp-cli/builds/gh-pages/phar/wp-cli.phar"

// ensureWpCli downloads wp-cli into the site home when it is missing. Sites
// installed by the panel already have it; hand-uploaded WordPress sites
// fetch it on first use.
func (s *Service) ensureWpCli(ctx context.Context, wpCtx wpContext) error {
	if res, err := s.exec.RunSudo(ctx, "test", "-f", wpCtx.phar); err == nil && res.ExitCode == 0 {
		return nil
	}
	runCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	for _, args := range [][]string{
		{"mkdir", "-p", filepath.Dir(wpCtx.phar)},
		{"/usr/bin/curl", "-fsSL", "-o", wpCtx.phar, wpCliURL},
	} {
		res, err := s.exec.RunSudo(runCtx, "-u", append([]string{wpCtx.webUser, "--"}, args...)...)
		if err != nil {
			return fmt.Errorf("install wp-cli: %w", err)
		}
		if res.ExitCode != 0 {
			return model.NewDomainError("WP_CLI_ERROR", "failed to install wp-cli: "+strings.TrimSpace(res.Stderr), nil)
		}
	}
	return nil
}

// hasWpConfig reports whether wp-config.php exists in the document root or
// its parent, the two places WordPress looks for it.
func (s *Service) hasWpConfig(ctx context.Context, docRoot string) bool {
	return s.hasProjectFile(ctx, docRoot, "wp-config.php") ||
		s.hasProjectFile(ctx, filepath.Dir(docRoot), "wp-config.php")
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
	if !s.hasWpConfig(ctx, wpCtx.path) {
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
	"shuffle-salts":  {"config", "shuffle-salts"},
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

	tr := s.taskRunner()
	if tr == nil {
		return "", fmt.Errorf("task runner not available")
	}

	full := append([]string{wpCtx.php, wpCtx.phar, "--path=" + wpCtx.path}, wpArgs...)
	taskArgs := append([]string{"-u", w.WebUser, "--"}, full...)
	return tr.Run("WP "+action+" — "+w.Domain, "sudo", taskArgs...), nil
}
