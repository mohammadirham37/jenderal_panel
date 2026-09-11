package website

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
	"github.com/mohammadirham37/jenderal_panel/internal/noderuntime"
)

const codeIgniter3Commit = "bcb17eb8ba53a85de154439d0ab8ff1bed047bc9"

var websiteIDRegex = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

type installStep struct {
	Name           string
	Stage          string
	Timeout        time.Duration
	Command        string
	Args           []string
	ExpectedOutput string
}

type Installer struct {
	exec executor.CommandExecutor
}

func NewInstaller(exec executor.CommandExecutor) *Installer {
	return &Installer{exec: exec}
}

func installationPlan(w websiteRow) ([]installStep, error) {
	profile, err := profileForWebsiteRow(w)
	if err != nil {
		return nil, err
	}
	if profile.SetupMode != SetupAutomatic || profile.Framework == "none" {
		return nil, nil
	}
	staging, err := installerStagingRoot(w)
	if err != nil {
		return nil, err
	}
	phpBinary := "/usr/bin/php" + w.PHPVersion
	composer := "/usr/local/bin/composer"
	homeDir := filepath.Join("/home", w.WebUser)
	userStep := func(name, stage string, timeout time.Duration, command string, args ...string) installStep {
		prefix := []string{w.WebUser, "--", "/usr/bin/env", "HOME=" + homeDir, command}
		return installStep{Name: name, Stage: stage, Timeout: timeout, Command: "-u", Args: append(prefix, args...)}
	}
	nodeStep := func(name, stage string, timeout time.Duration, command string, args ...string) (installStep, error) {
		commandArgs, err := noderuntime.ExecArgs(w.WebUser, w.NodeVersion, command, args...)
		if err != nil {
			return installStep{}, err
		}
		return installStep{Name: name, Stage: stage, Timeout: timeout, Command: "-u", Args: commandArgs}, nil
	}

	var steps []installStep
	switch profile.NginxProfile {
	case "codeigniter3":
		steps = append(steps,
			userStep("clone CodeIgniter 3", "installing framework", 15*time.Minute, "/usr/bin/git", "clone", "--no-checkout", "https://github.com/bcit-ci/CodeIgniter.git", staging),
			userStep("checkout CodeIgniter 3", "installing framework", 15*time.Minute, "/usr/bin/git", "-C", staging, "checkout", "--detach", "3.1.13"),
		)
		verify := userStep("verify CodeIgniter 3", "installing framework", time.Minute, "/usr/bin/git", "-C", staging, "rev-parse", "HEAD")
		verify.ExpectedOutput = codeIgniter3Commit
		steps = append(steps, verify)
	case "codeigniter4":
		steps = append(steps, userStep("install CodeIgniter 4", "installing framework", 15*time.Minute,
			phpBinary, composer, "create-project", "codeigniter4/appstarter", staging, "--no-interaction", "--prefer-dist", "--no-scripts"))
	case "laravel", "laravel-octane":
		if profile.ProjectVariant == "starter-kit" && profile.StarterCommit != "" {
			repositoryURL := "https://github.com/" + profile.StarterRepository + ".git"
			steps = append(steps,
				userStep("clone Laravel starter kit", "installing framework", 15*time.Minute, "/usr/bin/git", "clone", "--no-checkout", repositoryURL, staging),
				userStep("checkout Laravel starter kit", "installing framework", 15*time.Minute, "/usr/bin/git", "-C", staging, "checkout", "--detach", profile.StarterCommit),
			)
			verify := userStep("verify Laravel starter kit", "installing framework", time.Minute, "/usr/bin/git", "-C", staging, "rev-parse", "HEAD")
			verify.ExpectedOutput = profile.StarterCommit
			steps = append(steps, verify,
				userStep("install Composer dependencies", "installing framework", 15*time.Minute, phpBinary, composer, "install", "--working-dir="+staging, "--no-interaction", "--prefer-dist", "--no-scripts"))
		} else {
			packageName := "laravel/laravel:^" + profile.FrameworkVersion + ".0"
			if profile.ProjectVariant == "starter-kit" {
				packageName = profile.StarterRepository + ":" + profile.StarterReference
			}
			steps = append(steps, userStep("install Laravel", "installing framework", 15*time.Minute,
				phpBinary, composer, "create-project", packageName, staging, "--no-interaction", "--prefer-dist", "--no-scripts"))
		}
		steps = append(steps,
			userStep("create Laravel environment", "installing framework", time.Minute, "/usr/bin/cp", "-n", staging+"/.env.example", staging+"/.env"),
			userStep("generate Laravel key", "installing framework", time.Minute, phpBinary, staging+"/artisan", "key:generate", "--force", "--no-interaction"),
		)
		if profile.NginxProfile == "laravel-octane" {
			steps = append(steps,
				userStep("install Laravel Octane", "installing framework", 15*time.Minute, phpBinary, composer, "require", "laravel/octane", "--working-dir="+staging, "--no-interaction", "--no-scripts"),
				userStep("configure Octane FrankenPHP server", "installing framework", 5*time.Minute, phpBinary, staging+"/artisan", "octane:install", "--server=frankenphp", "--no-interaction"),
			)
		}
		if profile.RequiresNode {
			installDependencies, err := nodeStep("install frontend dependencies", "building assets", 10*time.Minute, "npm", "--prefix", staging, "install", "--no-audit", "--no-fund")
			if err != nil {
				return nil, err
			}
			buildAssets, err := nodeStep("build frontend assets", "building assets", 10*time.Minute, "npm", "--prefix", staging, "run", "build")
			if err != nil {
				return nil, err
			}
			steps = append(steps, installDependencies, buildAssets)
		}
	case "wordpress":
		wpCLIDir := filepath.Join(homeDir, ".wp-cli")
		wpCLI := filepath.Join(wpCLIDir, "wp-cli.phar")
		wp := func(label string, timeout time.Duration, args ...string) installStep {
			// wp-cli.phar runs through the site's PHP binary inside the
			// staged project.
			return userStep(label, "installing wordpress", timeout,
				phpBinary, append([]string{wpCLI, "--path=" + staging}, args...)...)
		}
		steps = append(steps,
			userStep("prepare wp-cli directory", "installing wordpress", time.Minute, "mkdir", "-p", wpCLIDir),
			userStep("download wp-cli", "installing wordpress", 5*time.Minute, "/usr/bin/curl", "-fsSL", "-o", wpCLI,
				"https://raw.githubusercontent.com/wp-cli/builds/gh-pages/phar/wp-cli.phar"),
			wp("download WordPress core", 15*time.Minute, "core", "download"),
			userStep("download SQLite integration plugin", "installing wordpress", 5*time.Minute, "/usr/bin/curl", "-fsSL",
				"-o", filepath.Join(staging, "sqlite.zip"),
				"https://downloads.wordpress.org/plugin/sqlite-database-integration.latest-stable.zip"),
			userStep("extract SQLite integration plugin", "installing wordpress", 5*time.Minute, phpBinary, "-r",
				`if (!class_exists('ZipArchive')) { fwrite(STDERR, "Missing php-zip: run sudo apt-get install "+$argv[1]+"-zip in Terminal, then retry this operation.\n"); exit(1); } $z = new ZipArchive(); if ($z->open($argv[1]) !== true) { fwrite(STDERR, "Invalid plugin archive\n"); exit(1); } $z->extractTo($argv[2]); $z->close();`,
				"--", filepath.Join(staging, "sqlite.zip"), filepath.Join(staging, "wp-content", "plugins")),
			// The plugin ships db.copy as the wp-config.php replacement: it
			// points WordPress at SQLite and auto-locates the engine folder
			// relative to wp-content, so the staged project stays
			// relocation-safe when promoted to the document root.
			userStep("configure SQLite config", "installing wordpress", time.Minute, "/usr/bin/cp",
				filepath.Join(staging, "wp-content", "plugins", "sqlite-database-integration", "db.copy"),
				filepath.Join(staging, "wp-config.php")),
			wp("activate SQLite integration", 5*time.Minute, "plugin", "activate", "sqlite-database-integration"),
			wp("install WordPress", 10*time.Minute, "core", "install",
				"--url=http://"+w.Domain, "--title="+w.Domain,
				"--admin_user=admin", "--admin_email=webmaster@"+w.Domain, "--skip-email"),
			// wp core install prints the generated admin password once; make
			// sure it is impossible to miss in the provisioning log.
			userStep("note admin password", "installing wordpress", time.Minute, "/bin/sh", "-c",
				"echo '=== WordPress admin user: admin (password printed above) — change it after first login ==='"),
		)
	default:
		return nil, fmt.Errorf("automatic installation is not implemented for profile %q", profile.NginxProfile)
	}
	return steps, nil
}

func (i *Installer) Install(ctx context.Context, w websiteRow, progress func(stage, output string) error) error {
	steps, err := installationPlan(w)
	if err != nil {
		return err
	}
	if len(steps) == 0 {
		return nil
	}
	staging, _ := installerStagingRoot(w)
	finalRoot, publicIndex, err := installerFinalPaths(w)
	if err != nil {
		return err
	}

	exists, err := i.pathExists(ctx, w.WebUser, finalRoot)
	if err != nil {
		return fmt.Errorf("check final project: %w", err)
	}
	if exists {
		valid, checkErr := i.pathIsFile(ctx, w.WebUser, publicIndex)
		if checkErr != nil {
			return fmt.Errorf("validate existing project: %w", checkErr)
		}
		if valid {
			if err := progress("installing framework", "Existing framework project preserved.\n"); err != nil {
				return err
			}
			return i.bootstrapLaravel(ctx, w, finalRoot, false, false, progress)
		}
		return fmt.Errorf("final project path already exists but %s is missing", publicIndex)
	}

	stagingExists, err := i.pathExists(ctx, w.WebUser, staging)
	if err != nil {
		return fmt.Errorf("check staging project: %w", err)
	}
	if stagingExists {
		if err := i.runUserOK(ctx, w.WebUser, "/usr/bin/rm", "-rf", "--", staging); err != nil {
			return fmt.Errorf("clear website staging directory: %w", err)
		}
	}

	for _, step := range steps {
		if err := progress(step.Stage, "\n=== "+step.Name+" ===\n"); err != nil {
			return err
		}
		stepCtx, cancel := context.WithTimeout(ctx, step.Timeout)
		result, runErr := i.exec.RunSudo(stepCtx, step.Command, step.Args...)
		cancel()
		if runErr != nil {
			return fmt.Errorf("%s: %w", step.Name, runErr)
		}
		output := strings.TrimSpace(strings.TrimSpace(result.Stdout) + "\n" + strings.TrimSpace(result.Stderr))
		if output != "" {
			output += "\n"
		}
		if err := progress(step.Stage, output); err != nil {
			return err
		}
		if result.ExitCode != 0 {
			return fmt.Errorf("%s failed with exit status %d; see provisioning log", step.Name, result.ExitCode)
		}
		if step.ExpectedOutput != "" && strings.TrimSpace(result.Stdout) != step.ExpectedOutput {
			return fmt.Errorf("%s: expected %s, got %s", step.Name, step.ExpectedOutput, strings.TrimSpace(result.Stdout))
		}
	}

	stagedIndex := filepath.Join(staging, strings.TrimPrefix(publicIndex, finalRoot+"/"))
	valid, err := i.pathIsFile(ctx, w.WebUser, stagedIndex)
	if err != nil {
		return fmt.Errorf("validate staged project: %w", err)
	}
	if !valid {
		return fmt.Errorf("staged project is missing %s", stagedIndex)
	}
	if err := i.runUserOK(ctx, w.WebUser, "/usr/bin/mv", "--", staging, finalRoot); err != nil {
		return fmt.Errorf("promote staged project: %w", err)
	}
	return i.bootstrapLaravel(ctx, w, finalRoot, true, false, progress)
}

func profileForWebsiteRow(w websiteRow) (Profile, error) {
	// Persisted profile overrides (e.g. laravel-octane) select the template
	// directly; legacy rows derive one from framework and app type.
	template := resolveNginxProfile(w.NginxProfile, w.Framework, w.FrameworkVersion, w.AppType)
	frameworkVersion := w.FrameworkVersion
	if template == "codeigniter3" {
		frameworkVersion = "3"
	}
	return ResolveProfile(CreateRequest{Template: template, PHPVersion: w.PHPVersion, FrameworkVersion: frameworkVersion,
		FrontendStack: w.FrontendStack, InertiaAdapter: w.InertiaAdapter, ProjectVariant: w.ProjectVariant, SetupMode: w.SetupMode, NodeVersion: w.NodeVersion})
}

func installerStagingRoot(w websiteRow) (string, error) {
	if !webUserRegex.MatchString(w.WebUser) || w.WebUser != DomainToUser(w.Domain) || !websiteIDRegex.MatchString(w.ID) {
		return "", fmt.Errorf("unsafe website identity")
	}
	return filepath.Join("/home", w.WebUser, ".jenderal-install-"+w.ID), nil
}

func installerFinalPaths(w websiteRow) (string, string, error) {
	home := filepath.Join("/home", w.WebUser)
	public := filepath.Join(home, "public")
	appPublic := filepath.Join(home, "app", "public")
	switch filepath.Clean(w.DocumentRoot) {
	case public:
		return public, filepath.Join(public, "index.php"), nil
	case appPublic:
		return filepath.Join(home, "app"), filepath.Join(appPublic, "index.php"), nil
	default:
		return "", "", fmt.Errorf("automatic installation requires a canonical document root")
	}
}

func (i *Installer) pathExists(ctx context.Context, user, path string) (bool, error) {
	for _, flag := range []string{"-e", "-L"} {
		result, err := i.exec.RunSudo(ctx, "-u", user, "--", "/usr/bin/test", flag, path)
		if err != nil {
			return false, err
		}
		if result.ExitCode == 0 {
			return true, nil
		}
		if result.ExitCode != 1 {
			return false, fmt.Errorf("test %s returned exit status %d", path, result.ExitCode)
		}
	}
	return false, nil
}

func (i *Installer) pathIsFile(ctx context.Context, user, path string) (bool, error) {
	result, err := i.exec.RunSudo(ctx, "-u", user, "--", "/usr/bin/test", "-f", path)
	if err != nil {
		return false, err
	}
	if result.ExitCode != 0 && result.ExitCode != 1 {
		return false, fmt.Errorf("test %s returned exit status %d", path, result.ExitCode)
	}
	return result.ExitCode == 0, nil
}

func (i *Installer) runUserOK(ctx context.Context, user, command string, args ...string) error {
	commandArgs := append([]string{user, "--", command}, args...)
	result, err := i.exec.RunSudo(ctx, "-u", commandArgs...)
	if err != nil {
		return err
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("exit status %d: %s", result.ExitCode, strings.TrimSpace(result.Stderr))
	}
	return nil
}
