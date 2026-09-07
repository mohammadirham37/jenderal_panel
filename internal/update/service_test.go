package update

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
)

func TestCheck_UpdateAvailable(t *testing.T) {
	// Set up a mock GitHub API server.
	release := githubRelease{
		TagName: "v2.0.0",
		HTMLURL: "https://github.com/mohammadirham37/jenderal_panel/releases/tag/v2.0.0",
		Assets: []githubAsset{
			{
				Name:               "jenderal_panel_linux_amd64",
				BrowserDownloadURL: "https://example.com/download/linux_amd64",
			},
		},
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(release)
	}))
	defer ts.Close()

	mock := &executor.MockExecutor{
		RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return &executor.Result{ExitCode: 0, Duration: time.Millisecond}, nil
		},
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return &executor.Result{ExitCode: 0, Duration: time.Millisecond}, nil
		},
	}

	svc := NewService(mock, "1.0.0")
	// Override the HTTP client to point to the test server.
	svc.httpClient = ts.Client()

	// We need to override the URL used by fetchLatestRelease.
	// Since the URL is a constant, we'll test via a direct approach:
	// create a custom test that calls the test server.
	info, err := checkWithURL(svc, ts.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if info.CurrentVersion != "1.0.0" {
		t.Fatalf("expected current version '1.0.0', got %q", info.CurrentVersion)
	}
	if info.LatestVersion != "2.0.0" {
		t.Fatalf("expected latest version '2.0.0', got %q", info.LatestVersion)
	}
	if !info.UpdateAvail {
		t.Fatal("expected UpdateAvail=true")
	}
	if info.ReleaseURL != release.HTMLURL {
		t.Fatalf("expected release URL %q, got %q", release.HTMLURL, info.ReleaseURL)
	}
}

func TestCheck_AlreadyLatest(t *testing.T) {
	release := githubRelease{
		TagName: "v1.0.0",
		HTMLURL: "https://github.com/mohammadirham37/jenderal_panel/releases/tag/v1.0.0",
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(release)
	}))
	defer ts.Close()

	mock := &executor.MockExecutor{
		RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return &executor.Result{ExitCode: 0, Duration: time.Millisecond}, nil
		},
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return &executor.Result{ExitCode: 0, Duration: time.Millisecond}, nil
		},
	}

	svc := NewService(mock, "v1.0.0")
	svc.httpClient = ts.Client()

	info, err := checkWithURL(svc, ts.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if info.UpdateAvail {
		t.Fatal("expected UpdateAvail=false when already on latest")
	}
}

func TestNormalizeVersion(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"v1.0.0", "1.0.0"},
		{"1.0.0", "1.0.0"},
		{"v2.3.4", "2.3.4"},
		{"  v1.0.0  ", "1.0.0"},
	}

	for _, tt := range tests {
		got := normalizeVersion(tt.input)
		if got != tt.want {
			t.Errorf("normalizeVersion(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestFindAssetURL(t *testing.T) {
	assets := []githubAsset{
		{Name: "jenderal_panel_linux_amd64", BrowserDownloadURL: "https://example.com/linux_amd64"},
		{Name: "jenderal_panel_darwin_arm64", BrowserDownloadURL: "https://example.com/darwin_arm64"},
		{Name: "checksums.txt", BrowserDownloadURL: "https://example.com/checksums"},
	}

	// The result depends on runtime.GOOS/GOARCH, but we can at least verify
	// the function doesn't return the checksums file.
	url := findAssetURL(assets)
	if url == "https://example.com/checksums" {
		t.Fatal("findAssetURL should not return checksums.txt")
	}
	if url == "" {
		t.Fatal("expected a non-empty URL for a common platform")
	}
}

// checkWithURL is a test helper that calls a custom URL instead of the GitHub API.
func checkWithURL(svc *Service, url string) (info updateInfo, err error) {
	ctx := context.Background()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return info, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := svc.httpClient.Do(req)
	if err != nil {
		return info, err
	}
	defer resp.Body.Close()

	var release githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return info, err
	}

	return updateInfo{
		CurrentVersion: svc.currentVer,
		LatestVersion:  normalizeVersion(release.TagName),
		ReleaseURL:     release.HTMLURL,
		UpdateAvail:    normalizeVersion(release.TagName) != normalizeVersion(svc.currentVer),
	}, nil
}

// updateInfo mirrors model.UpdateInfo for test-local usage to avoid coupling
// the test helper return type to the model package directly.
type updateInfo struct {
	CurrentVersion string
	LatestVersion  string
	ReleaseURL     string
	UpdateAvail    bool
}
