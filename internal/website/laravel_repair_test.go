package website

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
)

func TestLaravelRepairRequiresExplicitConfirmation(t *testing.T) {
	h := NewHandler(nil, nil)
	r := httptest.NewRecorder()
	h.RepairLaravel(r, httptest.NewRequest(http.MethodPost, "/api/v1/websites/example/repair-laravel", strings.NewReader(`{"confirm":false}`)))
	if r.Code != http.StatusBadRequest {
		t.Fatalf("status=%d", r.Code)
	}
}

func TestLaravelRepairPreservesWebsiteAndSSLState(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	insertTestWebsite(t, db, "repair-example", "example.com", "laravel", "8.3", "active")
	_, err := db.Exec(`UPDATE websites SET framework='laravel',framework_version='12',frontend_stack='blade',setup_mode='auto-install',document_root='/home/web_example_com/app/public',ssl_enabled=1 WHERE id='repair-example'`)
	if err != nil {
		t.Fatal(err)
	}
	commands := 0
	mock := &executor.MockExecutor{RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
		commands++
		if name != "-u" || args[0] != "web_example_com" {
			t.Fatalf("repair attempted privileged configuration mutation: %s %q", name, args)
		}
		return &executor.Result{Stdout: "APP_KEY=base64:keep\nDB_CONNECTION=mysql\nDB_DATABASE=production\n"}, nil
	}}
	svc := NewService(db, mock, nil)
	if err := svc.RepairLaravel(context.Background(), "repair-example", func(string) {}); err != nil {
		t.Fatal(err)
	}
	w, err := svc.Get(context.Background(), "repair-example")
	if err != nil {
		t.Fatal(err)
	}
	if w.Status != "active" || !w.SSLEnabled || commands != 1 {
		t.Fatal("repair changed website state or ran external database commands")
	}
	if _, err := db.Exec(`UPDATE websites SET setup_mode='config-only' WHERE id='repair-example'`); err != nil {
		t.Fatal(err)
	}
	if err := svc.RepairLaravel(context.Background(), "repair-example", func(string) {}); err == nil {
		t.Fatal("configuration-only website accepted for automatic repair")
	}
}
