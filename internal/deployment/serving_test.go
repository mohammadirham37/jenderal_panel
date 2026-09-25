package deployment

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/mohammadirham37/jenderal_panel/internal/audit"
)

// recordingRestorer captures RestoreServingAccess calls from the deploy flow.
type recordingRestorer struct {
	mu  sync.Mutex
	ids []string
	err error
}

func (r *recordingRestorer) RestoreServingAccess(ctx context.Context, websiteID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ids = append(r.ids, websiteID)
	return r.err
}

func deployServingTestService(t *testing.T, restorer *recordingRestorer) (*Service, string) {
	t.Helper()
	db := setupTestDB(t)
	auditSvc := audit.NewService(db)
	exec := newMockExecutor()
	svc := NewService(db, exec, auditSvc)
	if restorer != nil {
		svc.SetServingAccessRestorer(restorer)
	}
	websiteID := insertWebsite(t, db, "app.example.com", "web_app_example_com", "/home/web_app_example_com/app/public")
	return svc, websiteID
}

// A deploy recreates the project directory and chowns it, losing the nginx
// group and serving ACLs from provisioning; the deploy must re-apply them so
// the site does not answer "File not found" afterwards.
func TestDeployRestoresServingAccess(t *testing.T) {
	restorer := &recordingRestorer{}
	svc, websiteID := deployServingTestService(t, restorer)

	d, err := svc.Deploy(context.Background(), websiteID, "https://github.com/example/repo.git", "main")
	if err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	svc.deploy(context.Background(), d.ID)

	restorer.mu.Lock()
	defer restorer.mu.Unlock()
	if len(restorer.ids) != 1 || restorer.ids[0] != websiteID {
		t.Fatalf("RestoreServingAccess calls = %v, want [%s]", restorer.ids, websiteID)
	}

	got, err := svc.GetDeployment(context.Background(), d.ID)
	if err != nil {
		t.Fatalf("GetDeployment: %v", err)
	}
	if got.Status != "success" {
		t.Fatalf("deployment status = %s, want success (log: %s)", got.Status, got.Log)
	}
}

// Serving access repair is best effort: if it fails, the deployment itself
// still succeeds, but the log must name the failure.
func TestDeployServingRestoreFailureIsLoggedNotFatal(t *testing.T) {
	restorer := &recordingRestorer{err: errors.New("setfacl exploded")}
	svc, websiteID := deployServingTestService(t, restorer)

	d, err := svc.Deploy(context.Background(), websiteID, "https://github.com/example/repo.git", "main")
	if err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	svc.deploy(context.Background(), d.ID)

	restorer.mu.Lock()
	calls := len(restorer.ids)
	restorer.mu.Unlock()
	if calls != 1 {
		t.Fatalf("RestoreServingAccess calls = %d, want 1", calls)
	}

	got, err := svc.GetDeployment(context.Background(), d.ID)
	if err != nil {
		t.Fatalf("GetDeployment: %v", err)
	}
	if got.Status != "success" {
		t.Fatalf("deployment status = %s, want success", got.Status)
	}
	if !strings.Contains(got.Log, "setfacl exploded") {
		t.Fatalf("deployment log does not mention restore failure: %s", got.Log)
	}
}
