package dependency

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
	"github.com/mohammadirham37/jenderal_panel/internal/taskrunner"
)

func TestListReturnsComposerDependencyStatus(t *testing.T) {
	mock := &executor.MockExecutor{
		RunFunc: func(context.Context, string, ...string) (*executor.Result, error) {
			return &executor.Result{ExitCode: 0, Stdout: "Composer version 2.10.3 2026-08-27\n"}, nil
		},
	}
	handler := NewHandler(NewService(mock), nil, taskrunner.New())
	recorder := httptest.NewRecorder()

	handler.List(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/services/dependencies", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var body struct {
		Data []Status `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Data) != 1 || body.Data[0].Version != "2.10.3" || !body.Data[0].Installed {
		t.Fatalf("data = %+v, want Composer 2.10.3", body.Data)
	}
}

func TestInstallReturnsPersistentTaskID(t *testing.T) {
	handler := NewHandler(NewService(nil), nil, taskrunner.New())
	recorder := httptest.NewRecorder()

	handler.InstallComposer(recorder, httptest.NewRequest(http.MethodPost, "/api/v1/services/composer/install", nil))

	if recorder.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var body struct {
		Data struct {
			TaskID string `json:"task_id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Data.TaskID == "" {
		t.Fatal("task_id is empty")
	}
}
