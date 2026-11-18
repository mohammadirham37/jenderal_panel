package website

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
)

// runClearLogsScript runs the real script against a throwaway log
// directory standing in for /home/<user>/logs.
func runClearLogsScript(t *testing.T, logs, target string) (string, error) {
	t.Helper()
	// The script hardcodes /home/$1/logs; point it at the temp directory.
	script := strings.Replace(clearLogsScript, `dir="/home/$1/logs"`, `dir="`+logs+`"`, 1)
	pathEnv := "PATH=/usr/bin:/bin"
	if exec.Command("dd", "if=/dev/null", "of=/dev/null", "oflag=nofollow", "status=none").Run() != nil {
		// BSD dd (macOS) has no oflag=nofollow; servers use GNU dd.
		shim := t.TempDir()
		ddShim := "#!/bin/bash\nfor a in \"$@\"; do case \"$a\" in of=*) out=\"${a#of=}\";; esac; done\n[ -L \"$out\" ] && exit 1\n: > \"$out\"\n"
		if err := os.WriteFile(filepath.Join(shim, "dd"), []byte(ddShim), 0o755); err != nil {
			t.Fatal(err)
		}
		pathEnv = "PATH=" + shim + ":/usr/bin:/bin"
	}
	cmd := exec.Command("/bin/bash", "-c", script, "clear-logs", "web_x", target)
	cmd.Env = []string{pathEnv}
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func tempLogDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	logs := filepath.Join(dir, "logs")
	if err := os.Mkdir(logs, 0o755); err != nil {
		t.Fatal(err)
	}
	return logs
}

func writeSized(t *testing.T, path string, n int) {
	t.Helper()
	if err := os.WriteFile(path, []byte(strings.Repeat("x", n)), 0o644); err != nil {
		t.Fatal(err)
	}
}

func fileSize(t *testing.T, path string) int64 {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		return -1
	}
	return info.Size()
}

func TestClearLogsScriptClearsOneLogAndItsArchives(t *testing.T) {
	logs := tempLogDir(t)
	writeSized(t, filepath.Join(logs, "access.log"), 100)
	writeSized(t, filepath.Join(logs, "access.log.1"), 50)
	writeSized(t, filepath.Join(logs, "access.log.2.gz"), 25)
	writeSized(t, filepath.Join(logs, "error.log"), 70)

	out, err := runClearLogsScript(t, logs, "access")
	if err != nil {
		t.Fatalf("script: %v\n%s", err, out)
	}
	if out != "175" {
		t.Errorf("freed = %q, want 175", out)
	}
	if fileSize(t, filepath.Join(logs, "access.log")) != 0 {
		t.Error("access.log must be emptied, not removed")
	}
	if fileSize(t, filepath.Join(logs, "access.log.1")) != -1 || fileSize(t, filepath.Join(logs, "access.log.2.gz")) != -1 {
		t.Error("rotated access archives must be removed")
	}
	if fileSize(t, filepath.Join(logs, "error.log")) != 70 {
		t.Error("error.log must be left alone")
	}
}

func TestClearLogsScriptClearsAll(t *testing.T) {
	logs := tempLogDir(t)
	for name, n := range map[string]int{"access.log": 10, "error.log": 20, "php-error.log": 30, "php-slow.log": 40, "error.log.1": 5, "access.log-20261101.gz": 7} {
		writeSized(t, filepath.Join(logs, name), n)
	}
	out, err := runClearLogsScript(t, logs, "all")
	if err != nil {
		t.Fatalf("script: %v\n%s", err, out)
	}
	if out != "112" {
		t.Errorf("freed = %q, want 112", out)
	}
	entries, _ := os.ReadDir(logs)
	if len(entries) != 4 {
		t.Errorf("expected only the 4 active logs to remain, got %d", len(entries))
	}
}

// The website user owns its log directory; a log replaced by a symlink
// must not let the root-run script empty the file it points to.
func TestClearLogsScriptDoesNotFollowSymlinks(t *testing.T) {
	logs := tempLogDir(t)
	victim := filepath.Join(filepath.Dir(logs), "victim")
	writeSized(t, victim, 42)
	if err := os.Symlink(victim, filepath.Join(logs, "access.log")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(victim, filepath.Join(logs, "error.log.1")); err != nil {
		t.Fatal(err)
	}
	if out, err := runClearLogsScript(t, logs, "all"); err != nil {
		t.Fatalf("script: %v\n%s", err, out)
	}
	if fileSize(t, victim) != 42 {
		t.Error("the symlink target was modified")
	}
}

func TestClearLogsScriptRefusesSymlinkedDirectory(t *testing.T) {
	logs := tempLogDir(t)
	elsewhere := filepath.Join(filepath.Dir(logs), "elsewhere")
	if err := os.Mkdir(elsewhere, 0o755); err != nil {
		t.Fatal(err)
	}
	writeSized(t, filepath.Join(elsewhere, "access.log"), 42)
	if err := os.Remove(logs); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(elsewhere, logs); err != nil {
		t.Fatal(err)
	}
	if _, err := runClearLogsScript(t, logs, "all"); err == nil {
		t.Error("a symlinked logs directory must be refused")
	}
	if fileSize(t, filepath.Join(elsewhere, "access.log")) != 42 {
		t.Error("a file outside the logs directory was modified")
	}
}

func TestClearLogsValidatesTargetAndPassesArgs(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	insertTestWebsite(t, db, "logs-1", "logs.example.com", "php", "8.3", "active")
	var got []string
	svc := NewService(db, &executor.MockExecutor{
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			got = append([]string{name}, args...)
			return &executor.Result{Stdout: "2048\n"}, nil
		},
	}, nil)

	if _, err := svc.ClearLogs(context.Background(), "logs-1", "../../etc/passwd"); err == nil {
		t.Fatal("an unknown target must be refused")
	}
	freed, err := svc.ClearLogs(context.Background(), "logs-1", "error")
	if err != nil {
		t.Fatal(err)
	}
	if freed != 2048 {
		t.Errorf("freed = %d", freed)
	}
	user := DomainToUser("logs.example.com")
	if len(got) != 6 || got[0] != "/bin/bash" || got[4] != user || got[5] != "error" {
		t.Errorf("unexpected command %v", got)
	}
}

func TestLogFilesParsesFindOutput(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	insertTestWebsite(t, db, "logs-2", "files.example.com", "php", "8.3", "active")
	svc := NewService(db, &executor.MockExecutor{
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return &executor.Result{Stdout: "error.log\t10\naccess.log\t300\nbroken line\n"}, nil
		},
	}, nil)
	files, err := svc.LogFiles(context.Background(), "logs-2")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 || files[0].Name != "access.log" || files[0].Size != 300 {
		t.Errorf("unexpected files %+v", files)
	}
}
