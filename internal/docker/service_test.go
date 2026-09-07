package docker

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
)

// ---------- Status tests ----------

func TestStatus_Installed(t *testing.T) {
	mock := &executor.MockExecutor{
		RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return &executor.Result{ExitCode: 0, Duration: time.Millisecond}, nil
		},
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			joined := name + " " + strings.Join(args, " ")
			switch {
			case strings.Contains(joined, "docker --version"):
				return &executor.Result{
					Stdout:   "Docker version 24.0.7, build afdd53b\n",
					ExitCode: 0,
					Duration: time.Millisecond,
				}, nil
			case strings.Contains(joined, "systemctl is-active docker"):
				return &executor.Result{
					Stdout:   "active\n",
					ExitCode: 0,
					Duration: time.Millisecond,
				}, nil
			case strings.Contains(joined, "docker ps -a -q"):
				return &executor.Result{
					Stdout:   "abc123\ndef456\nghi789\n",
					ExitCode: 0,
					Duration: time.Millisecond,
				}, nil
			case strings.Contains(joined, "docker images -q"):
				return &executor.Result{
					Stdout:   "img001\nimg002\n",
					ExitCode: 0,
					Duration: time.Millisecond,
				}, nil
			default:
				return &executor.Result{ExitCode: 0, Duration: time.Millisecond}, nil
			}
		},
	}

	svc := NewService(mock, nil)
	status, err := svc.Status(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !status.Installed {
		t.Fatal("expected Installed=true")
	}
	if !status.Running {
		t.Fatal("expected Running=true")
	}
	if status.Version != "24.0.7" {
		t.Fatalf("expected version '24.0.7', got %q", status.Version)
	}
	if status.Containers != 3 {
		t.Fatalf("expected 3 containers, got %d", status.Containers)
	}
	if status.Images != 2 {
		t.Fatalf("expected 2 images, got %d", status.Images)
	}
}

func TestStatus_NotInstalled(t *testing.T) {
	mock := &executor.MockExecutor{
		RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return &executor.Result{ExitCode: 127, Duration: time.Millisecond}, nil
		},
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return &executor.Result{
				Stdout:   "",
				Stderr:   "docker: command not found",
				ExitCode: 127,
				Duration: time.Millisecond,
			}, nil
		},
	}

	svc := NewService(mock, nil)
	status, err := svc.Status(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status.Installed {
		t.Fatal("expected Installed=false")
	}
	if status.Running {
		t.Fatal("expected Running=false")
	}
	if status.Version != "" {
		t.Fatalf("expected empty version, got %q", status.Version)
	}
}

// ---------- Container tests ----------

func TestListContainers(t *testing.T) {
	mock := &executor.MockExecutor{
		RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return &executor.Result{ExitCode: 0, Duration: time.Millisecond}, nil
		},
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			output := "abc123\tweb-app\tnginx:latest\tUp 2 hours\trunning\t0.0.0.0:80->80/tcp\t2024-01-15 10:30:00 +0000 UTC\n" +
				"def456\tdb-server\tpostgres:16\tUp 5 hours\trunning\t0.0.0.0:5432->5432/tcp\t2024-01-14 08:00:00 +0000 UTC\n"
			return &executor.Result{
				Stdout:   output,
				ExitCode: 0,
				Duration: time.Millisecond,
			}, nil
		},
	}

	svc := NewService(mock, nil)
	containers, err := svc.ListContainers(context.Background(), false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(containers) != 2 {
		t.Fatalf("expected 2 containers, got %d", len(containers))
	}

	if containers[0].ID != "abc123" {
		t.Fatalf("expected first container ID 'abc123', got %q", containers[0].ID)
	}
	if containers[0].Name != "web-app" {
		t.Fatalf("expected first container name 'web-app', got %q", containers[0].Name)
	}
	if containers[0].Image != "nginx:latest" {
		t.Fatalf("expected first container image 'nginx:latest', got %q", containers[0].Image)
	}
	if containers[0].State != "running" {
		t.Fatalf("expected first container state 'running', got %q", containers[0].State)
	}

	if containers[1].ID != "def456" {
		t.Fatalf("expected second container ID 'def456', got %q", containers[1].ID)
	}
	if containers[1].Name != "db-server" {
		t.Fatalf("expected second container name 'db-server', got %q", containers[1].Name)
	}
	if containers[1].Image != "postgres:16" {
		t.Fatalf("expected second container image 'postgres:16', got %q", containers[1].Image)
	}
}

func TestListContainers_AllFlag(t *testing.T) {
	var capturedArgs []string
	mock := &executor.MockExecutor{
		RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return &executor.Result{ExitCode: 0, Duration: time.Millisecond}, nil
		},
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			capturedArgs = args
			return &executor.Result{
				Stdout:   "",
				ExitCode: 0,
				Duration: time.Millisecond,
			}, nil
		},
	}

	svc := NewService(mock, nil)
	_, _ = svc.ListContainers(context.Background(), true)

	found := false
	for _, arg := range capturedArgs {
		if arg == "-a" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected -a flag when all=true, got args: %v", capturedArgs)
	}
}

// ---------- Image tests ----------

func TestListImages(t *testing.T) {
	mock := &executor.MockExecutor{
		RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return &executor.Result{ExitCode: 0, Duration: time.Millisecond}, nil
		},
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			output := "sha256:abc123\tnginx\tlatest\t187MB\t2024-01-10 12:00:00 +0000 UTC\n" +
				"sha256:def456\tpostgres\t16\t412MB\t2024-01-08 09:00:00 +0000 UTC\n"
			return &executor.Result{
				Stdout:   output,
				ExitCode: 0,
				Duration: time.Millisecond,
			}, nil
		},
	}

	svc := NewService(mock, nil)
	images, err := svc.ListImages(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(images) != 2 {
		t.Fatalf("expected 2 images, got %d", len(images))
	}

	if images[0].ID != "sha256:abc123" {
		t.Fatalf("expected first image ID 'sha256:abc123', got %q", images[0].ID)
	}
	if images[0].Repository != "nginx" {
		t.Fatalf("expected first image repository 'nginx', got %q", images[0].Repository)
	}
	if images[0].Tag != "latest" {
		t.Fatalf("expected first image tag 'latest', got %q", images[0].Tag)
	}
	if images[0].Size != "187MB" {
		t.Fatalf("expected first image size '187MB', got %q", images[0].Size)
	}

	if images[1].Repository != "postgres" {
		t.Fatalf("expected second image repository 'postgres', got %q", images[1].Repository)
	}
	if images[1].Tag != "16" {
		t.Fatalf("expected second image tag '16', got %q", images[1].Tag)
	}
}

// ---------- Volume tests ----------

func TestListVolumes(t *testing.T) {
	mock := &executor.MockExecutor{
		RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return &executor.Result{ExitCode: 0, Duration: time.Millisecond}, nil
		},
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			output := "my-data\tlocal\t/var/lib/docker/volumes/my-data/_data\n"
			return &executor.Result{
				Stdout:   output,
				ExitCode: 0,
				Duration: time.Millisecond,
			}, nil
		},
	}

	svc := NewService(mock, nil)
	volumes, err := svc.ListVolumes(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(volumes) != 1 {
		t.Fatalf("expected 1 volume, got %d", len(volumes))
	}
	if volumes[0].Name != "my-data" {
		t.Fatalf("expected volume name 'my-data', got %q", volumes[0].Name)
	}
	if volumes[0].Driver != "local" {
		t.Fatalf("expected driver 'local', got %q", volumes[0].Driver)
	}
}

// ---------- Network tests ----------

func TestListNetworks(t *testing.T) {
	mock := &executor.MockExecutor{
		RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return &executor.Result{ExitCode: 0, Duration: time.Millisecond}, nil
		},
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			output := "net001\tbridge\tbridge\tlocal\n" +
				"net002\tmy-net\toverlay\tswarm\n"
			return &executor.Result{
				Stdout:   output,
				ExitCode: 0,
				Duration: time.Millisecond,
			}, nil
		},
	}

	svc := NewService(mock, nil)
	networks, err := svc.ListNetworks(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(networks) != 2 {
		t.Fatalf("expected 2 networks, got %d", len(networks))
	}
	if networks[0].Name != "bridge" {
		t.Fatalf("expected first network name 'bridge', got %q", networks[0].Name)
	}
	if networks[1].Scope != "swarm" {
		t.Fatalf("expected second network scope 'swarm', got %q", networks[1].Scope)
	}
}

// ---------- Parsing helper tests ----------

func TestParseDockerVersion(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Docker version 24.0.7, build afdd53b\n", "24.0.7"},
		{"Docker version 25.0.0, build abc123", "25.0.0"},
		{"Docker version 20.10.21", "20.10.21"},
		{"unexpected output", "unexpected output"},
	}

	for _, tt := range tests {
		got := parseDockerVersion(tt.input)
		if got != tt.want {
			t.Errorf("parseDockerVersion(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestCountNonEmptyLines(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"", 0},
		{"\n\n\n", 0},
		{"a\nb\nc\n", 3},
		{"a\n\nb\n", 2},
	}

	for _, tt := range tests {
		got := countNonEmptyLines(tt.input)
		if got != tt.want {
			t.Errorf("countNonEmptyLines(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

// ---------- Error handling tests ----------

func TestStartContainer_Error(t *testing.T) {
	mock := &executor.MockExecutor{
		RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return &executor.Result{ExitCode: 0, Duration: time.Millisecond}, nil
		},
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return &executor.Result{
				Stderr:   "Error: No such container: badid",
				ExitCode: 1,
				Duration: time.Millisecond,
			}, nil
		},
	}

	svc := NewService(mock, nil)
	err := svc.StartContainer(context.Background(), "badid")
	if err == nil {
		t.Fatal("expected error for bad container id")
	}
	if !strings.Contains(err.Error(), "failed to start container") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestContainerLogs(t *testing.T) {
	mock := &executor.MockExecutor{
		RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return &executor.Result{ExitCode: 0, Duration: time.Millisecond}, nil
		},
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			// Verify --tail argument is passed
			joined := strings.Join(args, " ")
			if !strings.Contains(joined, "--tail 50") {
				return &executor.Result{
					Stderr:   fmt.Sprintf("unexpected args: %s", joined),
					ExitCode: 1,
					Duration: time.Millisecond,
				}, nil
			}
			return &executor.Result{
				Stdout:   "log line 1\nlog line 2\n",
				ExitCode: 0,
				Duration: time.Millisecond,
			}, nil
		},
	}

	svc := NewService(mock, nil)
	logs, err := svc.ContainerLogs(context.Background(), "abc123", 50)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(logs, "log line 1") {
		t.Fatalf("expected log output, got %q", logs)
	}
}
