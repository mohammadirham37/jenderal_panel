package website

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
	"github.com/mohammadirham37/jenderal_panel/internal/taskrunner"
)

func noopExecutor() *executor.MockExecutor {
	return &executor.MockExecutor{RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
		return &executor.Result{ExitCode: 0}, nil
	}}
}

func TestNormalizeBenchmarkOptions(t *testing.T) {
	opts := normalizeBenchmarkOptions(BenchmarkOptions{})
	if opts.DurationSeconds != 10 || opts.Concurrency != 10 || opts.Path != "/" {
		t.Fatalf("defaults = %#v", opts)
	}
	opts = normalizeBenchmarkOptions(BenchmarkOptions{DurationSeconds: 9999, Concurrency: 9999, Path: "app"})
	if opts.DurationSeconds != benchmarkMaxDurationSeconds || opts.Concurrency != benchmarkMaxConcurrency {
		t.Fatalf("clamped = %#v", opts)
	}
	if opts.Path != "/app" {
		t.Fatalf("path = %q, want leading slash", opts.Path)
	}
}

func TestRunBenchmarkAgainstLocalServer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	req, err := http.NewRequest(http.MethodGet, server.URL+"/", nil)
	if err != nil {
		t.Fatal(err)
	}
	client := server.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	res := runBenchmark(ctx, client, req, 4, 300*time.Millisecond, func(string) {})

	if res.TotalRequests == 0 {
		t.Fatal("no requests were recorded")
	}
	if res.OKRequests != res.TotalRequests {
		t.Fatalf("ok = %d, total = %d, want all ok", res.OKRequests, res.TotalRequests)
	}
	if res.RequestsPerSecond <= 0 {
		t.Fatalf("rps = %v, want > 0", res.RequestsPerSecond)
	}
	if res.StatusCounts["200 OK"] != res.TotalRequests {
		t.Fatalf("status counts = %#v", res.StatusCounts)
	}
	if res.LatencyMS.P50 <= 0 || res.LatencyMS.Max < res.LatencyMS.Min {
		t.Fatalf("latency stats = %#v", res.LatencyMS)
	}
}

func TestBenchmarkWebsiteRunsAndWritesResultJSON(t *testing.T) {
	// A stand-in upstream: the benchmark client always dials 127.0.0.1:80 for
	// non-SSL sites, so a plain local HTTP server receives the load.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	db := setupTestDB(t)
	defer db.Close()
	svc := NewService(db, noopExecutor(), nil)
	svc.SetTaskRunner(taskrunner.New())

	created, err := svc.Create(context.Background(), CreateRequest{
		Domain: "bench.example.com", Template: "php", PHPVersion: "8.3", SetupMode: "config-only",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	taskID, err := svc.BenchmarkWebsite(context.Background(), created.ID, BenchmarkOptions{DurationSeconds: 1, Concurrency: 2})
	if err != nil {
		t.Fatalf("BenchmarkWebsite() error = %v", err)
	}

	deadline := time.Now().Add(15 * time.Second)
	var output string
	for time.Now().Before(deadline) {
		if task, ok := svc.taskRunner().Get(taskID); ok && (task.Status == "completed" || task.Status == "failed") {
			output = task.Output
			if task.Status != "completed" {
				t.Fatalf("benchmark task failed: %s", task.Error)
			}
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if output == "" {
		t.Fatal("task did not finish in time")
	}

	marker := strings.Index(output, "##RESULT_JSON## ")
	if marker == -1 {
		t.Fatalf("output missing result marker: %q", output)
	}
	var res benchmarkResult
	if err := json.Unmarshal([]byte(output[marker+len("##RESULT_JSON## "):]), &res); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if res.TotalRequests == 0 {
		t.Fatal("result has no requests")
	}
	if res.StatusCounts["200 OK"] != res.TotalRequests {
		t.Fatalf("status counts = %#v", res.StatusCounts)
	}
}
