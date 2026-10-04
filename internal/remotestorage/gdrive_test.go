package remotestorage

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
)

func gdriveTestConfig() Config {
	return Config{Type: "gdrive", GDriveClientID: "cid", GDriveClientSecret: "csecret",
		GDriveRefreshToken: "rtok", GDriveFolderID: "folder-1"}
}

// newTestGDrive builds a GDriveStorage wired to a fake Google API server.
// The injected executor streams "backup-bytes" for sudo cat (simulating a
// root-owned local backup file).
func newTestGDrive(t *testing.T, handler http.Handler) *GDriveStorage {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	exec := &executor.MockExecutor{
		RunSudoStreamFunc: func(ctx context.Context, w io.Writer, name string, args ...string) (int, error) {
			return w.Write([]byte("backup-bytes"))
		},
	}
	g := NewGDrive(gdriveTestConfig(), exec, srv.Client())
	g.tokenURL = srv.URL + "/token"
	g.apiBase = srv.URL + "/drive/v3"
	g.uploadBase = srv.URL + "/upload/drive/v3"
	g.authURL = srv.URL + "/auth"
	return g
}

func tokenHandler(counter *int32) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(counter, 1)
		w.WriteHeader(200)
		json.NewEncoder(w).Encode(map[string]any{"access_token": "atok", "expires_in": 3600})
	}
}

func TestGDriveExchangeCode(t *testing.T) {
	var gotForm url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		gotForm = r.PostForm
		w.WriteHeader(200)
		json.NewEncoder(w).Encode(map[string]any{
			"access_token": "atok", "expires_in": 3600, "refresh_token": "new-rtok",
		})
	}))
	defer srv.Close()

	g := NewGDrive(Config{Type: "gdrive", GDriveClientID: "cid", GDriveClientSecret: "csecret"}, nil, srv.Client())
	g.tokenURL = srv.URL + "/token"

	rt, err := g.ExchangeCode(context.Background(), "paste-d-code")
	if err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}
	if rt != "new-rtok" {
		t.Errorf("refresh token = %q", rt)
	}
	if gotForm.Get("code") != "paste-d-code" || gotForm.Get("grant_type") != "authorization_code" ||
		gotForm.Get("redirect_uri") != loopbackRedirect || gotForm.Get("client_id") != "cid" {
		t.Errorf("token exchange form = %v", gotForm)
	}
}

func TestGDriveAuthorizeURLContainsScopeAndPrompt(t *testing.T) {
	g := NewGDrive(Config{Type: "gdrive", GDriveClientID: "cid"}, nil, nil)
	u, err := url.Parse(g.AuthorizeURL())
	if err != nil {
		t.Fatalf("parse authorize URL: %v", err)
	}
	q := u.Query()
	if q.Get("client_id") != "cid" || q.Get("scope") != gdriveScope ||
		q.Get("access_type") != "offline" || q.Get("prompt") != "consent" ||
		q.Get("redirect_uri") != loopbackRedirect || q.Get("response_type") != "code" {
		t.Errorf("authorize URL query = %v", q)
	}
}

func TestGDriveUploadResumableAndDownloadDelete(t *testing.T) {
	var sessionPut int32
	var uploaded strings.Builder
	var dlServed, deleted int32

	handler := http.NewServeMux()
	var tokenCount int32
	handler.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) { tokenHandler(&tokenCount)(w, r) })
	handler.HandleFunc("/upload/drive/v3/files", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("uploadType") == "resumable" {
			w.Header().Set("Location", "http://"+r.Host+"/session/1")
			w.WriteHeader(200)
			return
		}
		w.WriteHeader(400)
	})
	handler.HandleFunc("/session/1", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&sessionPut, 1)
		io.Copy(&uploaded, r.Body)
		w.WriteHeader(200)
		json.NewEncoder(w).Encode(map[string]any{"id": "file-9", "name": "f.tar.gz", "size": "12"})
	})
	handler.HandleFunc("/drive/v3/files/file-9", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			atomic.AddInt32(&dlServed, 1)
			io.WriteString(w, "backup-bytes")
			return
		}
		if r.Method == http.MethodDelete {
			atomic.AddInt32(&deleted, 1)
			w.WriteHeader(204)
		}
	})

	g := newTestGDrive(t, handler)

	ref, err := g.Upload(context.Background(), "/data/f.tar.gz", 12, "website/f.tar.gz")
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}
	if ref != "gdrive://file-9" {
		t.Errorf("ref = %q", ref)
	}
	if sessionPut != 1 || uploaded.String() != "backup-bytes" {
		t.Errorf("session PUT=%d body=%q", sessionPut, uploaded.String())
	}

	backend, name, ok := ParseRemoteRef(ref)
	if !ok || backend != "gdrive" || name != "file-9" {
		t.Fatalf("ParseRemoteRef: %v %q %v", backend, name, ok)
	}
	var buf strings.Builder
	if err := g.Download(context.Background(), name, &buf); err != nil {
		t.Fatalf("Download: %v", err)
	}
	if buf.String() != "backup-bytes" || dlServed != 1 {
		t.Errorf("downloaded %q dlServed=%d", buf.String(), dlServed)
	}
	if err := g.Delete(context.Background(), name); err != nil || deleted != 1 {
		t.Fatalf("Delete: %v deleted=%d", err, deleted)
	}
}

func TestGDriveTestReturnsEmail(t *testing.T) {
	handler := http.NewServeMux()
	var tokenCount int32
	handler.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) { tokenHandler(&tokenCount)(w, r) })
	handler.HandleFunc("/drive/v3/about", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"user": map[string]any{"emailAddress": "me@gmail.com"}})
	})
	g := newTestGDrive(t, handler)
	info, err := g.Test(context.Background())
	if err != nil || info != "me@gmail.com" {
		t.Fatalf("Test: %q %v", info, err)
	}
}

func TestGDriveEnsureFolderCreatesWhenEmpty(t *testing.T) {
	var created int32
	handler := http.NewServeMux()
	var tokenCount int32
	handler.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) { tokenHandler(&tokenCount)(w, r) })
	handler.HandleFunc("/drive/v3/files", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			atomic.AddInt32(&created, 1)
			var meta map[string]any
			json.NewDecoder(r.Body).Decode(&meta)
			if meta["mimeType"] != "application/vnd.google-apps.folder" || meta["name"] != "Jenderal Panel Backups" {
				t.Errorf("folder metadata = %v", meta)
			}
			json.NewEncoder(w).Encode(map[string]any{"id": "folder-new"})
			return
		}
		w.WriteHeader(400)
	})
	g := newTestGDrive(t, handler)
	g.cfg.GDriveFolderID = ""

	id, err := g.EnsureFolder(context.Background())
	if err != nil || id != "folder-new" || created != 1 {
		t.Fatalf("EnsureFolder: %q %v created=%d", id, err, created)
	}
	// With a folder already configured nothing is created.
	g.cfg.GDriveFolderID = "folder-1"
	if id, err := g.EnsureFolder(context.Background()); err != nil || id != "folder-1" {
		t.Fatalf("EnsureFolder existing: %q %v", id, err)
	}
}
