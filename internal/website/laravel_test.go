package website

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
)

func TestLaravelEnvironment(t *testing.T) {
	const root = "/home/web_example_com/app"
	tests := []struct {
		name, input               string
		fresh                     bool
		path                      string
		key, unchanged, wantError bool
	}{
		{name: "fresh mysql skeleton", input: "APP_KEY=\nDB_CONNECTION=mysql\nDB_DATABASE=laravel\n", fresh: true, path: root + "/database/database.sqlite", key: true},
		{name: "missing database", input: "APP_KEY=base64:keep\nDB_CONNECTION=sqlite\n", path: root + "/database/database.sqlite"},
		{name: "missing key", input: "DB_CONNECTION=sqlite\n", path: root + "/database/database.sqlite", key: true},
		{name: "commented empty key", input: "APP_KEY= # create a key\nDB_CONNECTION='sqlite' # user's choice\n", path: root + "/database/database.sqlite", key: true},
		{name: "relative database", input: "APP_KEY='base64:keep'\nDB_CONNECTION=sqlite\nDB_DATABASE=database/custom.sqlite\n", path: root + "/database/custom.sqlite"},
		{name: "external database", input: "APP_KEY=base64:keep\nDB_CONNECTION=mysql\nDB_DATABASE=production\n", unchanged: true},
		{name: "external URL", input: "DB_CONNECTION=sqlite\nDB_URL='postgres://example/db'\n", unchanged: true},
		{name: "outside project", input: "DB_CONNECTION=sqlite\nDB_DATABASE=/tmp/other.sqlite\n", wantError: true},
		{name: "unresolved variable", input: "DB_CONNECTION=${DATABASE_DRIVER}\n", wantError: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, path, key, err := laravelEnvironment(tt.input, root, tt.fresh)
			if (err != nil) != tt.wantError {
				t.Fatalf("error=%v", err)
			}
			if err != nil {
				return
			}
			if path != tt.path || key != tt.key {
				t.Fatalf("path=%q key=%v", path, key)
			}
			if tt.unchanged && got != tt.input {
				t.Fatal("external database settings modified")
			}
			if !tt.key && !tt.unchanged && !strings.Contains(got, "base64:keep") {
				t.Fatal("existing app key lost")
			}
			if path != "" {
				again, p, k, e := laravelEnvironment(got, root, false)
				if e != nil || again != got || p != path || k != key {
					t.Fatal("bootstrap environment is not idempotent")
				}
			}
		})
	}
}

func TestLaravelBootstrapCreatesDatabaseAndPreservesDataOnRetry(t *testing.T) {
	php, err := exec.LookPath("php")
	if err != nil {
		t.Skip("PHP CLI required for SQLite bootstrap fixture")
	}
	if err := exec.Command(php, "-r", `exit(extension_loaded('pdo_sqlite') ? 0 : 1);`).Run(); err != nil {
		t.Skip("pdo_sqlite required")
	}
	root := t.TempDir()
	const canonical = "/home/web_example_com/app"
	if err := os.MkdirAll(filepath.Join(root, "public"), 0750); err != nil {
		t.Fatal(err)
	}
	initial := "APP_KEY=\nDB_CONNECTION=mysql\nDB_DATABASE=laravel\n"
	for name, content := range map[string]string{".env": initial, "public/index.php": "<?php echo 'ready';", "artisan": `<?php
 $root=__DIR__;
 if ($argv[1]==='key:generate') { $file=$root.'/.env'; file_put_contents($file,str_replace('APP_KEY=', 'APP_KEY=base64:preserved-fixture-key',file_get_contents($file))); }
 if ($argv[1]==='migrate') {
   if (file_exists($root.'/fail-migration')) {fwrite(STDERR,'migration fixture failure'); exit(1);}
   $env=parse_ini_file($root.'/.env'); $db=new PDO('sqlite:'.$env['DB_DATABASE']);
   $db->exec('CREATE TABLE IF NOT EXISTS sessions (id TEXT PRIMARY KEY)');
   $db->exec("INSERT OR IGNORE INTO sessions VALUES ('keep-session')");
 }
 `} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	var commands []string
	invoke := func(ctx context.Context, input, name string, args ...string) (*executor.Result, error) {
		if name != "-u" || len(args) < 3 || args[0] != "web_example_com" || args[1] != "--" {
			t.Fatalf("not tenant execution: %s %q", name, args)
		}
		argv := append([]string{}, args[2:]...)
		if argv[0] == "/usr/bin/env" {
			argv = argv[2:]
		}
		for n, arg := range argv {
			argv[n] = strings.ReplaceAll(arg, canonical, root)
		}
		if argv[0] == "/usr/bin/php8.3" {
			argv[0] = php
		}
		commands = append(commands, strings.Join(argv, " "))
		cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
		cmd.Stdin = strings.NewReader(strings.ReplaceAll(input, canonical, root))
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		e := cmd.Run()
		code := 0
		if e != nil {
			if x, ok := e.(*exec.ExitError); ok {
				code = x.ExitCode()
			} else {
				return nil, e
			}
		}
		return &executor.Result{Stdout: strings.ReplaceAll(stdout.String(), root, canonical), Stderr: stderr.String(), ExitCode: code}, nil
	}
	mock := &executor.MockExecutor{RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
		return invoke(ctx, "", name, args...)
	}, RunSudoWithInputFunc: invoke}
	row := automaticRow("laravel", "12", "blade", "", "empty")
	installer := NewInstaller(mock)
	progress := func(stage, output string) error {
		if output != "" {
			t.Log(stage, output)
		}
		return nil
	}
	if err := installer.bootstrapLaravel(context.Background(), row, canonical, true, progress); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(root, "database/database.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	envBefore, _ := os.ReadFile(filepath.Join(root, ".env"))
	commands = nil
	if err := installer.bootstrapLaravel(context.Background(), row, canonical, false, progress); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(filepath.Join(root, "database/database.sqlite"))
	envAfter, _ := os.ReadFile(filepath.Join(root, ".env"))
	if !bytes.Equal(before, after) || !bytes.Equal(envBefore, envAfter) {
		t.Fatal("Retry changed existing database or app key")
	}
	if strings.Contains(strings.Join(commands, "\n"), "key:generate") {
		t.Fatal("Retry regenerated existing app key")
	}
	if err := os.WriteFile(filepath.Join(root, "fail-migration"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := installer.bootstrapLaravel(context.Background(), row, canonical, false, progress); err == nil || !strings.Contains(err.Error(), "migrate Laravel SQLite failed") {
		t.Fatalf("migration failure not propagated: %v", err)
	}
	external := "APP_KEY=base64:keep\nDB_CONNECTION=mysql\nDB_DATABASE=production\n"
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte(external), 0600); err != nil {
		t.Fatal(err)
	}
	commands = nil
	if err := installer.bootstrapLaravel(context.Background(), row, canonical, false, progress); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(commands, "\n"), "/artisan") {
		t.Fatal("external database repair invoked Artisan")
	}
}
