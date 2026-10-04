package remotestorage

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
)

func s3TestConfig(endpoint string) Config {
	return Config{Type: "s3", Endpoint: endpoint, Bucket: "panel-backups",
		Region: "ap-southeast-1", AccessKey: "AKIDEXAMPLE", SecretKey: "secret",
		Prefix: "vps-1", UseTLS: strings.HasPrefix(endpoint, "https")}
}

func fakeExec(catData string) *executor.MockExecutor {
	return &executor.MockExecutor{
		RunSudoStreamFunc: func(ctx context.Context, w io.Writer, name string, args ...string) (int, error) {
			if name == "cat" {
				n, _ := io.WriteString(w, catData)
				return n, nil
			}
			return 0, nil
		},
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return &executor.Result{ExitCode: 0}, nil
		},
	}
}

func TestS3UploadDownloadDeleteRoundTrip(t *testing.T) {
	var gotMethod, gotAuth string
	var gotBody strings.Builder
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotAuth = r.Method, r.Header.Get("Authorization")
		io.Copy(&gotBody, r.Body)
		switch r.Method {
		case http.MethodPut:
			w.WriteHeader(http.StatusOK)
		case http.MethodGet:
			w.WriteHeader(200)
			io.WriteString(w, "backup-bytes")
		case http.MethodDelete:
			w.WriteHeader(204)
		}
	}))
	defer srv.Close()

	st, err := New(s3TestConfig(srv.URL), fakeExec("backup-bytes"), srv.Client())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ref, err := st.Upload(context.Background(), "/data/f.tar.gz", 12, "website/f.tar.gz")
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}
	if !strings.HasSuffix(ref, "/panel-backups/vps-1/website/f.tar.gz") {
		t.Errorf("ref = %q", ref)
	}
	if !strings.Contains(gotAuth, "AWS4-HMAC-SHA256 Credential=AKIDEXAMPLE/") {
		t.Errorf("upload not SigV4-signed: %q", gotAuth)
	}
	if gotBody.String() != "backup-bytes" {
		t.Errorf("uploaded body = %q", gotBody.String())
	}

	// Download by key parsed from the ref.
	backend, name, ok := ParseRemoteRef(ref)
	if !ok || backend != "s3" {
		t.Fatalf("ParseRemoteRef: %q %q %v", backend, name, ok)
	}
	var buf strings.Builder
	if err := st.Download(context.Background(), name, &buf); err != nil {
		t.Fatalf("Download: %v", err)
	}
	if buf.String() != "backup-bytes" {
		t.Errorf("downloaded = %q", buf.String())
	}

	if err := st.Delete(context.Background(), name); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if gotMethod != http.MethodDelete {
		t.Errorf("delete method = %s", gotMethod)
	}
}

func TestS3TestListsBucket(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && strings.Contains(r.URL.RawQuery, "list-type=2") {
			w.WriteHeader(200)
			io.WriteString(w, `<ListBucketResult></ListBucketResult>`)
			return
		}
		w.WriteHeader(403)
	}))
	defer srv.Close()

	st, _ := New(s3TestConfig(srv.URL), fakeExec(""), srv.Client())
	info, err := st.Test(context.Background())
	if err != nil {
		t.Fatalf("Test: %v", err)
	}
	if !strings.Contains(info, "panel-backups") {
		t.Errorf("Test info = %q", info)
	}
}

func TestS3UploadPropagatesHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(403)
		io.WriteString(w, "AccessDenied")
	}))
	defer srv.Close()

	st, _ := New(s3TestConfig(srv.URL), fakeExec("x"), srv.Client())
	if _, err := st.Upload(context.Background(), "/x", 1, "x"); err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("expected 403 error, got %v", err)
	}
}

func TestS3SigningHelpers(t *testing.T) {
	cfg := s3TestConfig("https://s3.wasabisys.com")
	if got := uriEncodePath("website/example.com/file 1.tar.gz"); got != "website/example.com/file%201.tar.gz" {
		t.Errorf("uriEncodePath = %q", got)
	}
	if got := canonicalURI(cfg.s3ObjectURL(cfg.s3ObjectKey("database/app db.sql"))); got != "/panel-backups/vps-1/database/app%20db.sql" {
		t.Errorf("canonicalURI = %q", got)
	}
}
