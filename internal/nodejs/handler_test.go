package nodejs

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
	"github.com/mohammadirham37/jenderal_panel/internal/noderuntime"
	"github.com/mohammadirham37/jenderal_panel/internal/taskrunner"
)

func TestInstallRejectsUnsupportedVersionBeforeStartingRootTask(t *testing.T) {
	handler := NewHandler(NewService(nil, nil, nil), nil, nil)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/nodejs/install", strings.NewReader(`{"version":"20; touch /tmp/injected"}`))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	handler.Install(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", recorder.Code)
	}
}

func TestRuntimeHandlersExposePersistentTaskContract(t *testing.T) {
	db := setupTestDB(t)
	insertTestWebsite(t, db, "site-handler", "handler.example.com", "web_handler", "/home/web_handler/public")
	svc := NewService(db, newMockExec(), nil)
	svc.runtime = installedRuntime("22")
	runner := taskrunner.New()
	handler := NewHandler(svc, nil, runner)
	router := chi.NewRouter()
	router.Get("/api/v1/nodejs/runtimes", handler.ListRuntimes)
	router.Post("/api/v1/nodejs/runtimes/{websiteID}", handler.ChangeRuntime)

	listRecorder := httptest.NewRecorder()
	router.ServeHTTP(listRecorder, httptest.NewRequest(http.MethodGet, "/api/v1/nodejs/runtimes", nil))
	if listRecorder.Code != http.StatusOK || !strings.Contains(listRecorder.Body.String(), `"selected_version":"24"`) || !strings.Contains(listRecorder.Body.String(), `"installed_version":"v24.2.1"`) {
		t.Fatalf("list response status=%d body=%s", listRecorder.Code, listRecorder.Body.String())
	}

	postRecorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/nodejs/runtimes/site-handler", strings.NewReader(`{"version":"22"}`))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(postRecorder, request)
	if postRecorder.Code != http.StatusAccepted {
		t.Fatalf("change response status=%d body=%s", postRecorder.Code, postRecorder.Body.String())
	}
	var response struct {
		Data map[string]string `json:"data"`
	}
	if err := json.Unmarshal(postRecorder.Body.Bytes(), &response); err != nil || response.Data["task_id"] == "" {
		t.Fatalf("change response=%q err=%v", postRecorder.Body.String(), err)
	}
	taskID := response.Data["task_id"]
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		task, _ := runner.Get(taskID)
		if task != nil && task.Status != "running" {
			if task.Status != "completed" {
				t.Fatalf("runtime task=%#v", task)
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("runtime task did not complete")
}

func TestListRuntimesReturnsEmptyArrayForFreshPanel(t *testing.T) {
	db := setupTestDB(t)
	handler := NewHandler(NewService(db, newMockExec(), nil), nil, taskrunner.New())
	recorder := httptest.NewRecorder()
	handler.ListRuntimes(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/nodejs/runtimes", nil))
	if recorder.Code != http.StatusOK || strings.TrimSpace(recorder.Body.String()) != `{"data":[]}` {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestGlobalRemovalHandlerRequiresConfirmationAndQueuesRemoval(t *testing.T) {
	db := setupTestDB(t)
	var aptCalls atomic.Int32
	mock := newMockExec()
	mock.RunFunc = func(context.Context, string, ...string) (*executor.Result, error) {
		return mockResult("install ok installed\t20.19.0-1\n", "", 0), nil
	}
	mock.RunSudoFunc = func(_ context.Context, name string, args ...string) (*executor.Result, error) {
		if name == "apt-get" {
			aptCalls.Add(1)
			if strings.Contains(strings.Join(args, " "), "autoremove") {
				t.Fatal("global removal used autoremove")
			}
		}
		return mockResult("", "", 0), nil
	}
	svc := NewService(db, mock, nil)
	svc.runtime = fakeRuntime{
		detect:       func(context.Context, string, string) (noderuntime.Status, error) { return noderuntime.Status{}, nil },
		install:      func(context.Context, string, string, func(string)) error { return nil },
		installPanel: func(context.Context, func(string)) error { return nil },
	}
	runner := taskrunner.New()
	handler := NewHandler(svc, nil, runner)
	router := chi.NewRouter()
	router.Delete("/api/v1/nodejs/global", handler.RemoveGlobal)

	denied := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodDelete, "/api/v1/nodejs/global", strings.NewReader(`{"confirm":false}`))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(denied, request)
	if denied.Code != http.StatusBadRequest || aptCalls.Load() != 0 {
		t.Fatalf("unconfirmed status=%d aptCalls=%d body=%s", denied.Code, aptCalls.Load(), denied.Body.String())
	}

	accepted := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodDelete, "/api/v1/nodejs/global", strings.NewReader(`{"confirm":true}`))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(accepted, request)
	if accepted.Code != http.StatusAccepted {
		t.Fatalf("confirmed status=%d body=%s", accepted.Code, accepted.Body.String())
	}
	deadline := time.Now().Add(time.Second)
	for aptCalls.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if aptCalls.Load() != 1 {
		t.Fatalf("aptCalls=%d, want 1", aptCalls.Load())
	}
}
