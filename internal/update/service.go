package update

import (
	"bytes"
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
	"github.com/mohammadirham37/jenderal_panel/internal/noderuntime"
	"github.com/mohammadirham37/jenderal_panel/internal/taskrunner"
)

const (
	githubAPIURL = "https://api.github.com/repos/mohammadirham37/jenderal_panel/commits/main"
	repoURL      = "https://github.com/mohammadirham37/jenderal_panel.git"
	sourceDir    = "/opt/jenderal/source"
	httpTimeout  = 30 * time.Second

	// restartScriptSuffix marks an installed binary whose self-update has
	// scheduled a verified service restart that has not completed yet.
	restartScriptSuffix = ".restart-update.sh"
)

type Service struct {
	exec       executor.CommandExecutor
	currentVer string
	tasks      *taskrunner.Runner
	httpClient *http.Client
	updateMu   sync.Mutex
	updateTask string
	aptMu      sync.Mutex
	aptRunning bool
	// runSudoStream indirection exists for tests; production always streams
	// through exec.RunSudoStream.
	runSudoStream func(ctx context.Context, w io.Writer, name string, args ...string) (int, error)
}

func NewService(exec executor.CommandExecutor, version string, tasks *taskrunner.Runner) *Service {
	s := &Service{
		exec:       exec,
		currentVer: version,
		tasks:      tasks,
		httpClient: &http.Client{Timeout: httpTimeout},
	}
	if exec != nil {
		s.runSudoStream = exec.RunSudoStream
	}
	return s
}

// Current reports the revision served by this running process without making
// an external request. It is used to detect readiness after self-update.
func (s *Service) Current() model.UpdateInfo {
	return model.UpdateInfo{CurrentVersion: displayVersion(s.currentVer)}
}

// Check returns current version and latest commit from GitHub.
func (s *Service) Check(ctx context.Context) (model.UpdateInfo, error) {
	info := s.Current()

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

// Changelog returns the most recent commits on main from GitHub so the panel
// can show what each update contains. Message is the commit subject line.
func (s *Service) Changelog(ctx context.Context, limit int) ([]model.CommitInfo, error) {
	if limit <= 0 || limit > 50 {
		limit = 15
	}
	url := fmt.Sprintf("%s?sha=main&per_page=%d", githubAPIURL, limit)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "jenderal-panel/"+s.currentVer)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch changelog: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GitHub API returned %d: %s", resp.StatusCode, string(body))
	}

	var commits []struct {
		SHA     string `json:"sha"`
		HTMLURL string `json:"html_url"`
		Commit  struct {
			Message string `json:"message"`
			Author  struct {
				Name string `json:"name"`
				Date string `json:"date"`
			} `json:"author"`
		} `json:"commit"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&commits); err != nil {
		return nil, err
	}

	out := make([]model.CommitInfo, 0, len(commits))
	for _, c := range commits {
		subject := c.Commit.Message
		if idx := strings.IndexByte(subject, '\n'); idx >= 0 {
			subject = subject[:idx]
		}
		out = append(out, model.CommitInfo{
			SHA:     displayVersion(c.SHA),
			Message: strings.TrimSpace(subject),
			Author:  c.Commit.Author.Name,
			Date:    c.Commit.Author.Date,
			URL:     c.HTMLURL,
		})
	}
	return out, nil
}

func displayVersion(value string) string {	value = strings.TrimSpace(value)
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

	s.updateMu.Lock()
	defer s.updateMu.Unlock()
	if s.updateTask != "" {
		if task, ok := s.tasks.Get(s.updateTask); ok && task.Status == "running" {
			return "", model.NewValidationError("a panel update is already running")
		}
		s.updateTask = ""
	}
	if _, err := os.Stat(execPath + restartScriptSuffix); err == nil {
		return "", model.NewValidationError("a panel update restart is still in progress")
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("check update restart state: %w", err)
	}

	taskID := s.tasks.Run("Update Jenderal Panel", "bash", "-c", UpdateScript(execPath))
	s.updateTask = taskID

	return taskID, nil
}

// RestartPending reports whether a prior self-update has scheduled a verified
// service restart for the given binary path that has not completed yet.
func RestartPending(execPath string) bool {
	_, err := os.Stat(execPath + restartScriptSuffix)
	return err == nil
}

// aptOpTimeout bounds one apt run. Package upgrades can legitimately take far
// longer than the executor's default timeout, so the task carries its own
// generous deadline (the executor only applies its default when the context
// has none).
const aptOpTimeout = 2 * time.Hour

// beginApt serializes apt operations — concurrent apt-get runs would fight
// over the dpkg lock. The returned release func marks the slot free again.
func (s *Service) beginApt() (func(), error) {
	s.aptMu.Lock()
	defer s.aptMu.Unlock()
	if s.aptRunning {
		return nil, model.NewValidationError("another package operation is already running")
	}
	s.aptRunning = true
	var once sync.Once
	return func() {
		once.Do(func() {
			s.aptMu.Lock()
			s.aptRunning = false
			s.aptMu.Unlock()
		})
	}, nil
}

// AptUpdate refreshes the Ubuntu package index (apt-get update) as a task.
func (s *Service) AptUpdate() (string, error) {
	release, err := s.beginApt()
	if err != nil {
		return "", err
	}
	taskID := s.tasks.RunFuncWithOptions(taskrunner.Options{Name: "apt-get update", Timeout: aptOpTimeout}, func(ctx context.Context, log func(string)) error {
		defer release()
		return s.runApt(ctx, log, "update")
	})
	return taskID, nil
}

// AptUpgrade installs pending Ubuntu package upgrades (apt-get upgrade) as a
// task. Local configuration files are kept when packages ship changed
// defaults, so a server upgrade never silently rewrites hand-edited configs.
func (s *Service) AptUpgrade() (string, error) {
	release, err := s.beginApt()
	if err != nil {
		return "", err
	}
	taskID := s.tasks.RunFuncWithOptions(taskrunner.Options{Name: "apt-get upgrade", Timeout: aptOpTimeout}, func(ctx context.Context, log func(string)) error {
		defer release()
		return s.runApt(ctx, log, "upgrade")
	})
	return taskID, nil
}

// runApt streams one apt-get operation into the task log. DEBIAN_FRONTEND and
// the confold policy keep non-interactive runs from hanging on prompts, and
// the lock timeout avoids failing instantly when another apt process holds
// the dpkg lock.
func (s *Service) runApt(ctx context.Context, log func(string), action string) error {
	ctx, cancel := context.WithTimeout(ctx, aptOpTimeout)
	defer cancel()
	output := &taskLogWriter{log: log}
	defer output.flush()
	args := []string{"env", "DEBIAN_FRONTEND=noninteractive", "apt-get", action}
	if action == "upgrade" {
		args = append(args,
			"-y",
			"--with-new-pkgs",
			"-o", "Dpkg::Lock::Timeout=120",
			"-o", "Dpkg::Options::=--force-confdef",
			"-o", "Dpkg::Options::=--force-confold",
		)
	}
	exit, err := s.runSudoStream(ctx, output, args[0], args[1:]...)
	if err != nil {
		return fmt.Errorf("apt-get %s: %w", action, err)
	}
	if exit != 0 {
		return fmt.Errorf("apt-get %s exited with code %d", action, exit)
	}
	log("apt-get " + action + " finished.")
	return nil
}

// taskLogWriter forwards command output to the task log line by line so the
// UI shows progress while apt is still running.
type taskLogWriter struct {
	log     func(string)
	pending []byte
}

func (w *taskLogWriter) Write(p []byte) (int, error) {
	w.pending = append(w.pending, p...)
	for {
		idx := bytes.IndexByte(w.pending, '\n')
		if idx < 0 {
			break
		}
		if line := strings.TrimRight(string(w.pending[:idx]), "\r"); line != "" {
			w.log(line)
		}
		w.pending = w.pending[idx+1:]
	}
	return len(p), nil
}

func (w *taskLogWriter) flush() {
	if line := strings.TrimRight(string(w.pending), "\r"); line != "" {
		w.log(line)
	}
	w.pending = nil
}

// UpdateScript renders the one-shot self-update — pull source, rebuild the
// frontend, embed and compile the binary, replace it atomically, and schedule
// a verified service restart with rollback. It is shared by the web update
// task and the `jenderal update` SSH command; every path derives from the
// installed binary location.
func UpdateScript(execPath string) string {
	replacementPath := execPath + ".new"
	backupPath := execPath + ".bak"
	restartScriptPath := execPath + restartScriptSuffix
	rollbackPath := execPath + ".rollback"

	// Single bash script for entire update — avoids PATH/env issues between steps
	return fmt.Sprintf(`#!/bin/bash
set -e
export PATH=/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin:$PATH

echo ">>> Step 1: Pulling latest source..."
if [ -d %s/.git ]; then
    cd %s && git pull 2>&1
else
    rm -rf %s
    git clone --depth 1 %s %s 2>&1
fi

echo ">>> Step 2: Installing frontend dependencies..."
echo ">>> Preparing panel-owned NVM runtime..."
%s
cd %s/web
chown -hR jenderal:jenderal .
%s 2>&1

echo ">>> Step 3: Building frontend..."
%s 2>&1

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
		noderuntime.PanelBootstrapShell(), sourceDir, noderuntime.PanelNPMCommand("install", "--loglevel=error"), noderuntime.PanelNPMCommand("run", "build"),
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
}
