package fail2ban

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mohammadirham37/jenderal_panel/internal/audit"
	"github.com/mohammadirham37/jenderal_panel/internal/executor"
	"github.com/mohammadirham37/jenderal_panel/internal/taskrunner"
)

func TestInstallReturnsPersistentSecurityTask(t *testing.T) {
	tasks := taskrunner.New()
	exec := &executor.MockExecutor{
		RunSudoFunc: func(context.Context, string, ...string) (*executor.Result, error) {
			return &executor.Result{}, nil
		},
	}
	handler := NewHandler(NewService(exec, newFakeManagedFiles(map[string]string{}), nil, nil), tasks, nil, nil)
	w := httptest.NewRecorder()
	handler.Install(w, httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(`{}`)))
	if w.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var body struct {
		Data struct {
			TaskID string `json:"task_id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	task, ok := tasks.Get(body.Data.TaskID)
	if !ok || task.Module != "security" {
		t.Fatalf("task = %#v", task)
	}
}

func TestAcceptedInstallWritesSecurityAuditWithTaskID(t *testing.T) {
	db := migratedFail2banDB(t)
	tasks := taskrunner.New()
	exec := &executor.MockExecutor{RunSudoFunc: func(context.Context, string, ...string) (*executor.Result, error) {
		return &executor.Result{}, nil
	}}
	handler := NewHandler(NewService(exec, newFakeManagedFiles(map[string]string{}), db, nil), tasks, audit.NewService(db), nil)
	w := httptest.NewRecorder()
	handler.Install(w, httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(`{}`)))
	if w.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var detail string
	if err := db.QueryRow(`SELECT detail FROM audit_logs WHERE module = 'security' AND action = 'install_fail2ban'`).Scan(&detail); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(detail, "task:") {
		t.Fatalf("audit detail = %q", detail)
	}
}

func TestManualBanRejectsPermanentDuration(t *testing.T) {
	calls := 0
	exec := &executor.MockExecutor{RunSudoFunc: func(context.Context, string, ...string) (*executor.Result, error) {
		calls++
		return &executor.Result{}, nil
	}}
	handler := NewHandler(NewService(exec, newFakeManagedFiles(map[string]string{}), nil, nil), taskrunner.New(), nil, nil)
	w := httptest.NewRecorder()
	handler.Ban(w, httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(
		`{"jail":"sshd","ip":"203.0.113.7","duration_seconds":0}`)))
	if w.Code != http.StatusBadRequest || calls != 0 {
		t.Fatalf("status=%d calls=%d body=%s", w.Code, calls, w.Body.String())
	}
}

func TestApplyRunsWithExplicitSecurityTimeout(t *testing.T) {
	tasks := taskrunner.New()
	exec := applyExecutor(func(name string, args ...string) *executor.Result {
		if name == "mktemp" {
			return &executor.Result{Stdout: "/tmp/jenderal-fail2ban.handler\n"}
		}
		if name == "fail2ban-client" && len(args) == 1 && args[0] == "status" {
			return &executor.Result{Stdout: "Jail list: sshd"}
		}
		return &executor.Result{}
	})
	handler := NewHandler(NewService(exec, newFakeManagedFiles(map[string]string{}), nil, nil), tasks, nil, nil)
	w := httptest.NewRecorder()
	requestBody, _ := json.Marshal(SafeSettings())
	handler.Apply(w, httptest.NewRequest(http.MethodPut, "/", bytes.NewReader(requestBody)))
	if w.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var body struct {
		Data map[string]string `json:"data"`
	}
	_ = json.NewDecoder(w.Body).Decode(&body)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if task, ok := tasks.Get(body.Data["task_id"]); ok && task.Status != "running" {
			if task.Status != "completed" || task.Module != "security" {
				t.Fatalf("task = %#v", task)
			}
			for _, phase := range []string{"validation", "promotion", "reload", "health confirmation"} {
				if !strings.Contains(task.Output, phase) {
					t.Fatalf("task output missing %q: %s", phase, task.Output)
				}
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("apply task did not complete")
}
