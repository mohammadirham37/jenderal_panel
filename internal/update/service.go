package update

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

const (
	// githubReleasesURL is the GitHub API endpoint for the latest release.
	githubReleasesURL = "https://api.github.com/repos/mohammadirham37/jenderal_panel/releases/latest"

	// httpTimeout is the timeout for HTTP requests to GitHub.
	httpTimeout = 30 * time.Second
)

// githubRelease represents the relevant fields from the GitHub releases API.
type githubRelease struct {
	TagName string        `json:"tag_name"`
	HTMLURL string        `json:"html_url"`
	Assets  []githubAsset `json:"assets"`
}

// githubAsset represents a single asset attached to a release.
type githubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// Service manages self-update operations for the panel.
type Service struct {
	exec       executor.CommandExecutor
	currentVer string
	httpClient *http.Client
}

// NewService creates a new update Service.
func NewService(exec executor.CommandExecutor, version string) *Service {
	return &Service{
		exec:       exec,
		currentVer: version,
		httpClient: &http.Client{Timeout: httpTimeout},
	}
}

// Check queries the GitHub releases API and compares the latest version with
// the currently running version.
func (s *Service) Check(ctx context.Context) (model.UpdateInfo, error) {
	info := model.UpdateInfo{
		CurrentVersion: s.currentVer,
	}

	release, err := s.fetchLatestRelease(ctx)
	if err != nil {
		return info, fmt.Errorf("check update: %w", err)
	}

	info.LatestVersion = normalizeVersion(release.TagName)
	info.ReleaseURL = release.HTMLURL
	info.UpdateAvail = info.LatestVersion != normalizeVersion(s.currentVer)

	return info, nil
}

// Update downloads the latest release binary and replaces the current
// executable, then restarts the jenderal service. Returns an error if the
// current version is already the latest.
func (s *Service) Update(ctx context.Context) error {
	info, err := s.Check(ctx)
	if err != nil {
		return err
	}
	if !info.UpdateAvail {
		return model.NewDomainError("UPDATE_ERROR", "already running the latest version", nil)
	}

	release, err := s.fetchLatestRelease(ctx)
	if err != nil {
		return fmt.Errorf("fetch release: %w", err)
	}

	// Find the appropriate asset for this platform.
	assetURL := findAssetURL(release.Assets)
	if assetURL == "" {
		return model.NewDomainError("UPDATE_ERROR", "no compatible binary found in release assets", nil)
	}

	// Download the new binary.
	tmpPath := "/tmp/jenderal_panel_update"
	if err := s.downloadFile(ctx, assetURL, tmpPath); err != nil {
		return fmt.Errorf("download update: %w", err)
	}

	// Make the downloaded binary executable.
	result, err := s.exec.RunSudo(ctx, "chmod", "+x", tmpPath)
	if err != nil {
		return fmt.Errorf("chmod update binary: %w", err)
	}
	if result.ExitCode != 0 {
		return model.NewDomainError("UPDATE_ERROR", "failed to chmod: "+result.Stderr, nil)
	}

	// Get the current executable path.
	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("get executable path: %w", err)
	}

	// Backup the current binary.
	backupPath := execPath + ".bak"
	result, err = s.exec.RunSudo(ctx, "cp", execPath, backupPath)
	if err != nil {
		return fmt.Errorf("backup current binary: %w", err)
	}
	if result.ExitCode != 0 {
		return model.NewDomainError("UPDATE_ERROR", "failed to backup current binary: "+result.Stderr, nil)
	}

	// Replace the current binary with the new one.
	result, err = s.exec.RunSudo(ctx, "cp", tmpPath, execPath)
	if err != nil {
		return fmt.Errorf("replace binary: %w", err)
	}
	if result.ExitCode != 0 {
		return model.NewDomainError("UPDATE_ERROR", "failed to replace binary: "+result.Stderr, nil)
	}

	// Clean up the temp file.
	_, _ = s.exec.Run(ctx, "rm", "-f", tmpPath)

	// Restart the service.
	result, err = s.exec.RunSudo(ctx, "systemctl", "restart", "jenderal")
	if err != nil {
		return fmt.Errorf("restart service: %w", err)
	}
	if result.ExitCode != 0 {
		return model.NewDomainError("UPDATE_ERROR", "failed to restart service: "+result.Stderr, nil)
	}

	return nil
}

// fetchLatestRelease calls the GitHub releases API and returns the parsed response.
func (s *Service) fetchLatestRelease(ctx context.Context) (*githubRelease, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, githubReleasesURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "jenderal-panel/"+s.currentVer)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GitHub API returned status %d: %s", resp.StatusCode, string(body))
	}

	var release githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, fmt.Errorf("decode release: %w", err)
	}

	return &release, nil
}

// downloadFile downloads a URL to a local file path.
func (s *Service) downloadFile(ctx context.Context, url, destPath string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "jenderal-panel/"+s.currentVer)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download returned status %d", resp.StatusCode)
	}

	out, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	return err
}

// normalizeVersion strips a leading "v" from a version string for comparison.
func normalizeVersion(v string) string {
	return strings.TrimPrefix(strings.TrimSpace(v), "v")
}

// findAssetURL looks for a binary asset matching the current OS and architecture.
func findAssetURL(assets []githubAsset) string {
	goos := runtime.GOOS
	goarch := runtime.GOARCH

	// Try to find an asset matching the platform.
	for _, a := range assets {
		name := strings.ToLower(a.Name)
		if strings.Contains(name, goos) && strings.Contains(name, goarch) {
			return a.BrowserDownloadURL
		}
	}

	// Fallback: look for a linux amd64 binary (most common server platform).
	for _, a := range assets {
		name := strings.ToLower(a.Name)
		if strings.Contains(name, "linux") && (strings.Contains(name, "amd64") || strings.Contains(name, "x86_64")) {
			return a.BrowserDownloadURL
		}
	}

	return ""
}
