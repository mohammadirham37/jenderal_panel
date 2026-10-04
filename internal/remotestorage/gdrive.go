package remotestorage

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
)

// Google Drive via the Drive API v3 with the least-privileged drive.file
// scope: the panel can only access files it created itself. Authorization is
// the copy-paste code flow against an OAuth client of type "Desktop app"
// (loopback redirects are implicitly allowed, so no public domain is needed).

const (
	loopbackRedirect = "http://localhost:1/"
	gdriveScope      = "https://www.googleapis.com/auth/drive.file"
	gdriveFolderName = "Jenderal Panel Backups"
	gdriveFolderMIME = "application/vnd.google-apps.folder"
)

// GDriveStorage is the native Google Drive backend.
type GDriveStorage struct {
	cfg  Config
	exec executor.CommandExecutor
	http *http.Client

	// Overridable for tests; default to Google endpoints.
	tokenURL   string
	apiBase    string
	uploadBase string
	authURL    string

	mu          sync.Mutex
	cachedToken string
	tokenExpiry time.Time
}

// NewGDrive creates the Google Drive backend.
func NewGDrive(cfg Config, exec executor.CommandExecutor, hc *http.Client) *GDriveStorage {
	if hc == nil {
		hc = http.DefaultClient
	}
	return &GDriveStorage{
		cfg: cfg, exec: exec, http: hc,
		tokenURL:   "https://oauth2.googleapis.com/token",
		apiBase:    "https://www.googleapis.com/drive/v3",
		uploadBase: "https://www.googleapis.com/upload/drive/v3",
		authURL:    "https://accounts.google.com/o/oauth2/v2/auth",
	}
}

// AuthorizeURL builds the Google consent URL the admin opens in a browser.
func (g *GDriveStorage) AuthorizeURL() string {
	q := url.Values{}
	q.Set("client_id", g.cfg.GDriveClientID)
	q.Set("redirect_uri", loopbackRedirect)
	q.Set("response_type", "code")
	q.Set("scope", gdriveScope)
	q.Set("access_type", "offline")
	q.Set("prompt", "consent")
	return g.authURL + "?" + q.Encode()
}

// ExchangeCode swaps a pasted authorization code for a refresh token.
func (g *GDriveStorage) ExchangeCode(ctx context.Context, code string) (string, error) {
	form := url.Values{}
	form.Set("code", code)
	form.Set("client_id", g.cfg.GDriveClientID)
	form.Set("client_secret", g.cfg.GDriveClientSecret)
	form.Set("redirect_uri", loopbackRedirect)
	form.Set("grant_type", "authorization_code")

	var out struct {
		AccessToken  string `json:"access_token"`
		ExpiresIn    int    `json:"expires_in"`
		RefreshToken string `json:"refresh_token"`
	}
	if err := g.postForm(ctx, g.tokenURL, form, &out); err != nil {
		return "", fmt.Errorf("token exchange: %w", err)
	}
	if out.RefreshToken == "" {
		return "", fmt.Errorf("token exchange returned no refresh token (re-authorize with prompt=consent)")
	}
	return out.RefreshToken, nil
}

func (g *GDriveStorage) postForm(ctx context.Context, endpoint string, form url.Values, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := g.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("google returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return json.Unmarshal(body, out)
}

// accessToken returns a valid access token, refreshing it when missing or
// near expiry.
func (g *GDriveStorage) accessToken(ctx context.Context) (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.cachedToken != "" && time.Now().Before(g.tokenExpiry) {
		return g.cachedToken, nil
	}
	if g.cfg.GDriveRefreshToken == "" {
		return "", fmt.Errorf("google drive is not connected yet")
	}
	form := url.Values{}
	form.Set("client_id", g.cfg.GDriveClientID)
	form.Set("client_secret", g.cfg.GDriveClientSecret)
	form.Set("refresh_token", g.cfg.GDriveRefreshToken)
	form.Set("grant_type", "refresh_token")

	var out struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := g.postForm(ctx, g.tokenURL, form, &out); err != nil {
		return "", fmt.Errorf("refresh access token: %w", err)
	}
	if out.AccessToken == "" {
		return "", fmt.Errorf("token refresh returned no access token")
	}
	g.cachedToken = out.AccessToken
	g.tokenExpiry = time.Now().Add(time.Duration(out.ExpiresIn-60) * time.Second)
	return g.cachedToken, nil
}

// authedRequest performs an authenticated Google API request; on 401 the
// cached token is dropped and the request is retried once.
func (g *GDriveStorage) authedRequest(ctx context.Context, method, rawURL string) (*http.Response, error) {
	for attempt := 0; attempt < 2; attempt++ {
		token, err := g.accessToken(ctx)
		if err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, method, rawURL, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := g.http.Do(req)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode == http.StatusUnauthorized && attempt == 0 {
			resp.Body.Close()
			g.mu.Lock()
			g.cachedToken = ""
			g.mu.Unlock()
			continue
		}
		return resp, nil
	}
	return nil, fmt.Errorf("google api unauthorized")
}

// EnsureFolder returns the configured folder id, creating (but not
// persisting) the default folder when none is configured.
func (g *GDriveStorage) EnsureFolder(ctx context.Context) (string, error) {
	if g.cfg.GDriveFolderID != "" {
		return g.cfg.GDriveFolderID, nil
	}
	meta := map[string]any{"name": gdriveFolderName, "mimeType": gdriveFolderMIME}
	body, _ := json.Marshal(meta)
	token, err := g.accessToken(ctx)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.apiBase+"/files", strings.NewReader(string(body)))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := g.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("create folder: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("create folder: google returned %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	var out struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(respBody, &out); err != nil || out.ID == "" {
		return "", fmt.Errorf("create folder: unexpected response %q", strings.TrimSpace(string(respBody)))
	}
	return out.ID, nil
}

// Upload creates a resumable session and streams the file into it. The file
// is read as root (sudo cat) because backup files are root-owned.
func (g *GDriveStorage) Upload(ctx context.Context, localPath string, size int64, name string) (string, error) {
	folderID, err := g.EnsureFolder(ctx)
	if err != nil {
		return "", err
	}
	meta := map[string]any{"name": filepath.Base(name)}
	if folderID != "" {
		meta["parents"] = []string{folderID}
	}
	metaBody, _ := json.Marshal(meta)

	token, err := g.accessToken(ctx)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.uploadBase+"/files?uploadType=resumable", strings.NewReader(string(metaBody)))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json; charset=UTF-8")
	req.Header.Set("X-Upload-Content-Length", fmt.Sprintf("%d", size))
	req.Header.Set("X-Upload-Content-Type", "application/gzip")

	resp, err := g.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("start resumable session: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("start resumable session: google returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	location := resp.Header.Get("Location")
	if location == "" {
		return "", fmt.Errorf("start resumable session: no Location header")
	}

	pr, pw := io.Pipe()
	go func() {
		_, err := g.exec.RunSudoStream(ctx, pw, "cat", localPath)
		_ = pw.CloseWithError(err)
	}()

	putReq, err := http.NewRequestWithContext(ctx, http.MethodPut, location, pr)
	if err != nil {
		return "", err
	}
	if size > 0 {
		putReq.ContentLength = size
	}
	putResp, err := g.http.Do(putReq)
	if err != nil {
		return "", fmt.Errorf("upload bytes: %w", err)
	}
	defer putResp.Body.Close()
	putBody, _ := io.ReadAll(io.LimitReader(putResp.Body, 1<<16))
	if putResp.StatusCode/100 != 2 {
		return "", fmt.Errorf("upload bytes: google returned %d: %s", putResp.StatusCode, strings.TrimSpace(string(putBody)))
	}
	var file struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(putBody, &file); err != nil || file.ID == "" {
		return "", fmt.Errorf("upload bytes: unexpected response %q", strings.TrimSpace(string(putBody)))
	}
	return "gdrive://" + file.ID, nil
}

// Download streams the file (alt=media) into w.
func (g *GDriveStorage) Download(ctx context.Context, name string, w io.Writer) error {
	resp, err := g.authedRequest(ctx, http.MethodGet, g.apiBase+"/files/"+url.PathEscape(name)+"?alt=media")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("download: google returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if _, err := io.Copy(w, resp.Body); err != nil {
		return fmt.Errorf("download stream: %w", err)
	}
	return nil
}

// Delete removes the file.
func (g *GDriveStorage) Delete(ctx context.Context, name string) error {
	resp, err := g.authedRequest(ctx, http.MethodDelete, g.apiBase+"/files/"+url.PathEscape(name))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 && resp.StatusCode != http.StatusNotFound {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("delete: google returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

// Test verifies the token works and returns the account email.
func (g *GDriveStorage) Test(ctx context.Context) (string, error) {
	resp, err := g.authedRequest(ctx, http.MethodGet, g.apiBase+"/about?fields=user")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("about: google returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var out struct {
		User struct {
			EmailAddress string `json:"emailAddress"`
		} `json:"user"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", fmt.Errorf("about: unexpected response %q", strings.TrimSpace(string(body)))
	}
	if out.User.EmailAddress == "" {
		return "", fmt.Errorf("about: no account email in response")
	}
	return out.User.EmailAddress, nil
}
