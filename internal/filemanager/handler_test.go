package filemanager

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	_ "github.com/mattn/go-sqlite3"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
)

func TestBrowseUsesWebsiteRouteID(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`
		CREATE TABLE websites (id TEXT PRIMARY KEY, document_root TEXT NOT NULL, web_user TEXT NOT NULL);
		INSERT INTO websites (id, document_root, web_user) VALUES ('site-1', '/home/web_example_com/public', 'web_example_com');
	`); err != nil {
		t.Fatal(err)
	}

	mock := &executor.MockExecutor{
		RunFunc: func(context.Context, string, ...string) (*executor.Result, error) {
			return &executor.Result{ExitCode: 0}, nil
		},
		RunSudoFunc: func(context.Context, string, ...string) (*executor.Result, error) {
			return &executor.Result{ExitCode: 0, Stdout: "total 0\n"}, nil
		},
	}
	handler := NewHandler(NewService(mock, nil), db, nil)
	router := chi.NewRouter()
	router.Get("/websites/{id}/files", handler.Browse)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/websites/site-1/files?path=/", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("Browse status = %d, want 200; body = %s", recorder.Code, recorder.Body.String())
	}
}
