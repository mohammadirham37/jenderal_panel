package backup

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
	"github.com/mohammadirham37/jenderal_panel/internal/remotestorage"
)

func remoteSvc(t *testing.T) (*Service, *fakeStorage) {
	t.Helper()
	db := setupTestDB(t)
	seedRemoteConfig(t, db, "s3", false)
	svc := NewService(db, mockExecutor(), nil, "/tmp/test-backups")
	svc.SetRemoteStore(remotestorage.NewConfigStore(db))
	fake := &fakeStorage{}
	storageOverride = func(remotestorage.Config) (remotestorage.Storage, error) { return fake, nil }
	t.Cleanup(func() { storageOverride = nil })
	return svc, fake
}

func TestRemoteConfigViewMasksSecrets(t *testing.T) {
	svc, _ := remoteSvc(t)
	view, err := svc.RemoteConfigView(context.Background())
	if err != nil {
		t.Fatalf("RemoteConfigView: %v", err)
	}
	out, _ := json.Marshal(view)
	if strings.Contains(string(out), `"secret_key":"sk"`) || strings.Contains(string(out), `"secret_key": "sk"`) {
		t.Errorf("view leaks secret: %s", out)
	}
	if !view.SecretSet {
		t.Error("SecretSet must be true")
	}
	if view.Type != "s3" || view.Bucket != "bkt" {
		t.Errorf("view = %+v", view)
	}
}

func TestSaveRemoteConfigPreservesSecret(t *testing.T) {
	svc, _ := remoteSvc(t)
	err := svc.SaveRemoteConfig(context.Background(), RemoteConfigRequest{
		Type: "s3", Endpoint: "https://s3.new", Bucket: "bkt2", Region: "us-east-1", AccessKey: "ak2",
	})
	if err != nil {
		t.Fatalf("SaveRemoteConfig: %v", err)
	}
	cfg, _ := svc.remoteConfig(context.Background())
	if cfg.SecretKey != "sk" {
		t.Errorf("secret must survive a save without it, got %q", cfg.SecretKey)
	}
	if cfg.Bucket != "bkt2" {
		t.Errorf("bucket = %q", cfg.Bucket)
	}
}

func TestSaveRemoteConfigRejectsUnknownType(t *testing.T) {
	svc, _ := remoteSvc(t)
	if err := svc.SaveRemoteConfig(context.Background(), RemoteConfigRequest{Type: "ftp"}); err == nil {
		t.Fatal("unknown type must be rejected")
	}
}

func TestTestRemoteConnection(t *testing.T) {
	svc, _ := remoteSvc(t)
	info, err := svc.TestRemoteConnection(context.Background())
	if err != nil || info != "fake ok" {
		t.Fatalf("TestRemoteConnection: %q %v", info, err)
	}
}

func TestGDriveAuthorizeRequiresClientID(t *testing.T) {
	svc, _ := remoteSvc(t)
	if _, err := svc.GDriveAuthorizeURL(context.Background()); err == nil {
		t.Fatal("expected error when no gdrive client id is configured")
	}
}

// fakeGDriveExchange simulates a connected gdrive backend for the exchange
// flow (ExchangeCode + EnsureFolder + Test).
type fakeGDriveExchange struct {
	fake         *fakeStorage
	refreshToken string
	folderID     string
	email        string
}

func (f *fakeGDriveExchange) Upload(ctx context.Context, localPath string, size int64, name string) (string, error) {
	return f.fake.Upload(ctx, localPath, size, name)
}
func (f *fakeGDriveExchange) Download(ctx context.Context, name string, w io.Writer) error {
	return f.fake.Download(ctx, name, w)
}
func (f *fakeGDriveExchange) Delete(ctx context.Context, name string) error {
	return f.fake.Delete(ctx, name)
}
func (f *fakeGDriveExchange) Test(ctx context.Context) (string, error) { return f.email, nil }
func (f *fakeGDriveExchange) ExchangeCode(ctx context.Context, code string) (string, error) {
	return f.refreshToken, nil
}
func (f *fakeGDriveExchange) EnsureFolder(ctx context.Context) (string, error) { return f.folderID, nil }
func (f *fakeGDriveExchange) AuthorizeURL() string                              { return "https://accounts.google.com/fake" }

func TestGDriveExchangeStoresTokenAndFolder(t *testing.T) {
	db := setupTestDB(t)
	svc := NewService(db, mockExecutor(), nil, "/tmp/test-backups")
	svc.SetRemoteStore(remotestorage.NewConfigStore(db))

	// The exchange flow builds its gdrive client through the newGDriveForConfig
	// seam — override it with a fake that records the stored token/folder.
	fake := &fakeStorage{}
	newGDriveForConfig = func(cfg remotestorage.Config, exec executor.CommandExecutor, hc *http.Client) gdriveClient {
		return &fakeGDriveExchange{fake: fake, refreshToken: "rt-1", folderID: "fld-1", email: "me@gmail.com"}
	}
	defer func() { newGDriveForConfig = defaultNewGDrive }()

	if _, err := db.Exec(`INSERT INTO settings (key, value, updated_at) VALUES ('backup_remote_gdrive_client_id','cid','now')`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO settings (key, value, updated_at) VALUES ('backup_remote_gdrive_client_secret','csec','now')`); err != nil {
		t.Fatalf("seed: %v", err)
	}

	email, err := svc.GDriveExchange(context.Background(), "code-1")
	if err != nil {
		t.Fatalf("GDriveExchange: %v", err)
	}
	if email != "me@gmail.com" {
		t.Errorf("email = %q", email)
	}
	cfg, _ := svc.remoteConfig(context.Background())
	if cfg.GDriveRefreshToken != "rt-1" || cfg.GDriveFolderID != "fld-1" || cfg.Type != "gdrive" {
		t.Errorf("config after exchange: %+v", cfg)
	}
}
