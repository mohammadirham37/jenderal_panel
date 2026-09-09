package website

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

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
