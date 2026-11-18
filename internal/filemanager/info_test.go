package filemanager

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
)

func infoMock(t *testing.T, outputs map[string]*executor.Result) (*executor.MockExecutor, *[]string) {
	t.Helper()
	var calls []string
	return &executor.MockExecutor{
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			// runAsWebsiteUser: sudo -u <user> -- <cmd> ...
			if name != "-u" || len(args) < 3 || args[1] != "--" {
				t.Fatalf("expected a command run as the website user, got %s %v", name, args)
			}
			cmd := strings.Join(args[2:], " ")
			calls = append(calls, cmd)
			for prefix, res := range outputs {
				if strings.HasPrefix(cmd, prefix) {
					return res, nil
				}
			}
			return &executor.Result{ExitCode: 1}, nil
		},
	}, &calls
}

func TestInfoForFolderCountsContents(t *testing.T) {
	base := t.TempDir()
	if err := os.Mkdir(filepath.Join(base, "app"), 0o755); err != nil {
		t.Fatal(err)
	}
	mock, calls := infoMock(t, map[string]*executor.Result{
		"stat ":        {Stdout: "directory\t4096\t755\tdrwxr-xr-x\tweb_x\twww-data\t1700000000\t1700000100\t1700000200\n"},
		"du -s -B1 -x": {Stdout: "8192\t/x\n"},
		"du -s -b -x":  {Stdout: "5000\t/x\n"},
		"find ":        {Stdout: "ffdfl"},
	})
	info, err := NewService(mock, nil).Info(context.Background(), base, "app")
	if err != nil {
		t.Fatal(err)
	}
	if info.Type != "directory" || info.Size != 5000 || info.DiskUsage != 8192 || info.Files != 3 || info.Dirs != 1 || info.Others != 1 {
		t.Errorf("unexpected info %+v", info)
	}
	if info.Mode != "0755" || info.Owner != "web_x" || info.Group != "www-data" || info.Path != "/app" || info.Name != "app" {
		t.Errorf("unexpected metadata %+v", info)
	}
	if info.Modified != "2023-11-14T22:13:20Z" || info.Partial {
		t.Errorf("modified=%q partial=%v", info.Modified, info.Partial)
	}
	if len(*calls) != 4 {
		t.Errorf("calls %v", *calls)
	}
}

func TestInfoForFileSkipsFolderWalk(t *testing.T) {
	base := t.TempDir()
	if err := os.WriteFile(filepath.Join(base, "a.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	mock, calls := infoMock(t, map[string]*executor.Result{
		"stat ":     {Stdout: "regular file\t2\t644\t-rw-r--r--\tweb_x\tweb_x\t1\t1\t1\n"},
		"du -B1 --": {Stdout: "4096\t/x\n"},
	})
	info, err := NewService(mock, nil).Info(context.Background(), base, "a.txt")
	if err != nil {
		t.Fatal(err)
	}
	if info.Type != "file" || info.Size != 2 || info.DiskUsage != 4096 || info.Mode != "0644" {
		t.Errorf("unexpected info %+v", info)
	}
	for _, c := range *calls {
		if strings.HasPrefix(c, "find ") {
			t.Error("a file must not be walked")
		}
	}
}

func TestInfoRejectsTraversal(t *testing.T) {
	mock, _ := infoMock(t, nil)
	if _, err := NewService(mock, nil).Info(context.Background(), t.TempDir(), "../../etc"); err == nil {
		t.Error("path traversal must be refused")
	}
}

func TestDirSizesKeepsDirectChildren(t *testing.T) {
	base := t.TempDir()
	if err := os.Mkdir(filepath.Join(base, "app"), 0o755); err != nil {
		t.Fatal(err)
	}
	dir, _ := filepath.EvalSymlinks(filepath.Join(base, "app"))
	out := "100\t" + dir + "/vendor\n20\t" + dir + "/my folder\n130\t" + dir + "\n"
	mock, calls := infoMock(t, map[string]*executor.Result{
		"du -b -x --max-depth=1": {Stdout: out, ExitCode: 1, Stderr: "du: cannot read directory"},
	})
	sizes, err := NewService(mock, nil).DirSizes(context.Background(), base, "app")
	if err != nil {
		t.Fatal(err)
	}
	if len(sizes) != 2 || sizes["vendor"] != 100 || sizes["my folder"] != 20 {
		t.Errorf("unexpected sizes %v (calls %v)", sizes, *calls)
	}
}
