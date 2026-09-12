package website

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/mohammadirham37/jenderal_panel/internal/executor"
)

func TestHandlerOptionsReturnsResponseEnvelope(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	mock := &executor.MockExecutor{RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
		return &executor.Result{ExitCode: 1}, nil
	}}
	handler := NewHandler(NewService(db, mock, nil), nil)
	recorder := httptest.NewRecorder()
	handler.Options(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/websites/options", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Data WebsiteOptions `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Data.PHPVersions) != 4 || len(response.Data.Profiles) == 0 {
		t.Fatalf("response data = %#v", response.Data)
	}
}

func TestHandlerCreateReturnsBadRequestForMissingRuntime(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	mock := &executor.MockExecutor{RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
		return &executor.Result{ExitCode: 1}, nil
	}}
	handler := NewHandler(NewService(db, mock, nil), nil)
	body := bytes.NewBufferString(`{"domain":"runtime.example.com","template":"php","php_version":"8.4"}`)
	recorder := httptest.NewRecorder()
	handler.Create(recorder, httptest.NewRequest(http.MethodPost, "/api/v1/websites", body))

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", recorder.Code, recorder.Body.String())
	}
}

// The router registers POST /websites/{id}/octane/{action} and the handler
// reads the action from the URL param. This pins the contract between them:
// a valid action must reach the service (failing later on the unknown
// website, not on action validation), and a bogus action must be rejected
// by the service's validation.
func TestHandlerOctaneActionReceivesRouteParam(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	mock := &executor.MockExecutor{RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
		return &executor.Result{ExitCode: 1}, nil
	}}
	handler := NewHandler(NewService(db, mock, nil), nil)

	router := chi.NewRouter()
	router.Post("/api/v1/websites/{id}/octane/{action}", handler.OctaneAction)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/v1/websites/nope/octane/start", nil))
	if strings.Contains(recorder.Body.String(), "unknown Octane action") {
		t.Fatalf("action param did not reach the service: %d %s", recorder.Code, recorder.Body.String())
	}
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d for unknown website, want 404; body = %s", recorder.Code, recorder.Body.String())
	}

	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/v1/websites/nope/octane/bogus", nil))
	if !strings.Contains(recorder.Body.String(), "unknown Octane action") {
		t.Fatalf("status = %d for bogus action, want validation error; body = %s", recorder.Code, recorder.Body.String())
	}
}

// AppAction reads its action the same way; pin that contract too.
func TestHandlerAppActionReceivesRouteParam(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	mock := &executor.MockExecutor{RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
		return &executor.Result{ExitCode: 1}, nil
	}}
	handler := NewHandler(NewService(db, mock, nil), nil)

	router := chi.NewRouter()
	router.Post("/api/v1/websites/{id}/app/{action}", handler.AppAction)
	router.Post("/api/v1/websites/{id}/app/build", handler.AppBuild)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/v1/websites/nope/app/start", nil))
	if strings.Contains(recorder.Body.String(), "unknown app action") {
		t.Fatalf("action param did not reach the service: %d %s", recorder.Code, recorder.Body.String())
	}
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d for unknown website, want 404; body = %s", recorder.Code, recorder.Body.String())
	}

	// The static /app/build route must still win over the {action} param.
	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/v1/websites/nope/app/build", nil))
	if strings.Contains(recorder.Body.String(), "unknown app action") {
		t.Fatalf("static build route was captured by the action param: %d %s", recorder.Code, recorder.Body.String())
	}
}
