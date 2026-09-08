package update

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
	"github.com/mohammadirham37/jenderal_panel/internal/taskrunner"
)

const (
	githubAPIURL = "https://api.github.com/repos/mohammadirham37/jenderal_panel/commits/main"
	repoURL      = "https://github.com/mohammadirham37/jenderal_panel.git"
	sourceDir    = "/opt/jenderal/source"
	httpTimeout  = 30 * time.Second
)

type Service struct {
	exec       executor.CommandExecutor
	currentVer string
	tasks      *taskrunner.Runner
	httpClient *http.Client
}

func NewService(exec executor.CommandExecutor, version string, tasks *taskrunner.Runner) *Service {
	return &Service{
		exec:       exec,
		currentVer: version,
		tasks:      tasks,
		httpClient: &http.Client{Timeout: httpTimeout},
	}
}

// Check returns current version and latest commit from GitHub.
func (s *Service) Check(ctx context.Context) (model.UpdateInfo, error) {
	info := model.UpdateInfo{
		CurrentVersion: s.currentVer,
	}

	// Get latest commit hash from main branch
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, githubAPIURL, nil)
	if err != nil {
		return info, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "jenderal-panel/"+s.currentVer)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return info, fmt.Errorf("check update: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return info, fmt.Errorf("GitHub API returned %d: %s", resp.StatusCode, string(body))
	}

	var commit struct {
		SHA     string `json:"sha"`
		HTMLURL string `json:"html_url"`
		Commit  struct {
			Message string `json:"message"`
		} `json:"commit"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&commit); err != nil {
		return info, err
	}

	info.LatestVersion = commit.SHA[:8]
	info.ReleaseURL = commit.HTMLURL
	info.UpdateAvail = true // Always allow update from source

	// Check if source dir exists and compare
	if _, err := os.Stat(sourceDir + "/.git"); err == nil {
		result, err := s.exec.Run(ctx, "git", "-C", sourceDir, "rev-parse", "--short", "HEAD")
		if err == nil && result.ExitCode == 0 {
			localHash := strings.TrimSpace(result.Stdout)
			if localHash == info.LatestVersion {
				info.UpdateAvail = false
			}
		}
	}

	return info, nil
}

// Update pulls latest source, rebuilds, and replaces binary.
// Returns task ID for progress tracking.
func (s *Service) Update(ctx context.Context) (string, error) {
	execPath, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("get executable path: %w", err)
	}

	// Single bash script for entire update — avoids PATH/env issues between steps
	script := fmt.Sprintf(`#!/bin/bash
set -e
export PATH=/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin:$PATH
export HOME=/root

echo ">>> Step 1: Pulling latest source..."
if [ -d %s/.git ]; then
    cd %s && git pull 2>&1
else
    rm -rf %s
    git clone --depth 1 %s %s 2>&1
fi

echo ">>> Step 2: Installing frontend dependencies..."
cd %s/web
npm install --loglevel=error 2>&1

echo ">>> Step 3: Building frontend..."
npm run build 2>&1

echo ">>> Step 4: Preparing embed..."
cd %s
rm -rf cmd/jenderal/web_build
cp -r web/build cmd/jenderal/web_build

echo ">>> Step 5: Compiling Go binary..."
CGO_ENABLED=1 go build -o /tmp/jenderal-update ./cmd/jenderal 2>&1

echo ">>> Step 6: Backing up current binary..."
cp %s %s.bak

echo ">>> Step 7: Replacing binary..."
cp /tmp/jenderal-update %s
chown jenderal:jenderal %s
chmod +x %s
rm -f /tmp/jenderal-update

echo ">>> Step 8: Restarting service..."
systemctl restart jenderal

echo ">>> Update complete!"
`,
		sourceDir, sourceDir, sourceDir, repoURL, sourceDir,
		sourceDir,
		sourceDir,
		execPath, execPath,
		execPath, execPath, execPath,
	)

	taskID := s.tasks.Run("Update Jenderal Panel", "bash", "-c", script)

	return taskID, nil
}
