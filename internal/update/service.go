package update

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
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
	updateMu   sync.Mutex
	updateTask string
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
		CurrentVersion: displayVersion(s.currentVer),
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

	latestRevision := strings.TrimSpace(commit.SHA)
	if len(latestRevision) < 8 {
		return info, fmt.Errorf("GitHub API returned an invalid commit SHA")
	}
	info.LatestVersion = displayVersion(latestRevision)
	info.ReleaseURL = commit.HTMLURL
	info.UpdateAvail = !strings.EqualFold(strings.TrimSpace(s.currentVer), latestRevision)

	return info, nil
}

func displayVersion(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 8 && isHexRevision(value) {
		return value[:8]
	}
	return value
}

func isHexRevision(value string) bool {
	for _, char := range value {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') && (char < 'A' || char > 'F') {
			return false
		}
	}
	return value != ""
}

// Update pulls latest source, rebuilds, and replaces binary.
// Returns task ID for progress tracking.
func (s *Service) Update(ctx context.Context) (string, error) {
	execPath, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("get executable path: %w", err)
	}
	replacementPath := execPath + ".new"
	backupPath := execPath + ".bak"
	restartScriptPath := execPath + ".restart-update.sh"
	rollbackPath := execPath + ".rollback"

	s.updateMu.Lock()
	defer s.updateMu.Unlock()
	if s.updateTask != "" {
		if task, ok := s.tasks.Get(s.updateTask); ok && task.Status == "running" {
			return "", model.NewValidationError("a panel update is already running")
		}
		s.updateTask = ""
	}
	if _, err := os.Stat(restartScriptPath); err == nil {
		return "", model.NewValidationError("a panel update restart is still in progress")
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("check update restart state: %w", err)
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
rm -f %s
trap 'rm -f %s' EXIT
revision="$(git rev-parse --verify HEAD^{commit})"
case "$revision" in
    ''|*[!0-9a-f]*) echo "Invalid source revision: $revision" >&2; exit 1 ;;
esac
CGO_ENABLED=1 go build -o %s -ldflags "-X main.version=$revision" ./cmd/jenderal 2>&1

echo ">>> Step 6: Installing branded Nginx welcome page..."
mkdir -p /var/www/html
install -m 0644 %s/internal/landing/landing.css /var/www/html/jenderal-landing.css
install -m 0644 %s/internal/landing/nginx-welcome.html /var/www/html/index.nginx-debian.html

echo ">>> Step 7: Preparing replacement binary..."
chown jenderal:jenderal %s
chmod +x %s

echo ">>> Step 8: Backing up current binary..."
cp -f %s %s

echo ">>> Step 9: Replacing binary atomically..."
mv -f %s %s
trap - EXIT

echo ">>> Step 10: Scheduling verified service restart..."
cat > %s <<'JENDERAL_RESTART_SCRIPT'
#!/bin/bash
set -e
restart_script=%s
binary=%s
backup_binary=%s
rollback_binary=%s

cleanup() {
    rm -f "$restart_script" "$rollback_binary"
}
trap cleanup EXIT

if systemctl restart jenderal; then
    sleep 5
    if systemctl is-active --quiet jenderal; then
        exit 0
    fi
fi

echo "New Jenderal binary failed to stay active; restoring backup." >&2
systemctl stop jenderal || true
cp -f "$backup_binary" "$rollback_binary"
chown jenderal:jenderal "$rollback_binary"
chmod +x "$rollback_binary"
mv -f "$rollback_binary" "$binary"
systemctl start jenderal
sleep 5
systemctl is-active --quiet jenderal
JENDERAL_RESTART_SCRIPT
chmod 700 %s
if ! systemd-run --quiet --collect --property=Type=exec --unit="jenderal-update-restart-$$" --on-active=5s /bin/bash %s; then
    cp -f %s %s
    chown jenderal:jenderal %s
    chmod +x %s
    mv -f %s %s
    rm -f %s
    exit 1
fi

echo ">>> Update complete! Service restart scheduled."
`,
		sourceDir, sourceDir, sourceDir, repoURL, sourceDir,
		sourceDir,
		sourceDir,
		replacementPath, replacementPath, replacementPath,
		sourceDir, sourceDir,
		replacementPath, replacementPath,
		execPath, backupPath,
		replacementPath, execPath,
		restartScriptPath,
		restartScriptPath, execPath, backupPath, rollbackPath,
		restartScriptPath, restartScriptPath,
		backupPath, rollbackPath,
		rollbackPath, rollbackPath,
		rollbackPath, execPath,
		restartScriptPath,
	)

	taskID := s.tasks.Run("Update Jenderal Panel", "bash", "-c", script)
	s.updateTask = taskID

	return taskID, nil
}
