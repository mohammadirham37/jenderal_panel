package siteops

import (
	"context"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
)

// AppProjectRoot resolves the repository root for app-service sites
// (app_type go today). New sites keep their checkout at <home>/app with the
// document root at <home>/app/public, mirroring the Laravel convention.
// Sites created before that layout cloned into the document root itself
// (<home>/public); a .git directory there marks the legacy layout so every
// consumer (deployment clone target, systemd unit working directory, build
// command) keeps agreeing on one location. When the probe cannot run at all,
// the document root is the conservative answer.
func AppProjectRoot(ctx context.Context, exec executor.CommandExecutor, homeDir, docroot string) (string, bool) {
	res, err := exec.Run(ctx, "test", "-d", homeDir+"/public/.git")
	if err != nil || res == nil {
		return docroot, false
	}
	if res.ExitCode == 0 {
		return homeDir + "/public", true
	}
	return homeDir + "/app", false
}
