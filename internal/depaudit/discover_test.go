package depaudit

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
)

// fakeFS answers the sudo test/stat/cat probes from a map of path -> content.
func fakeFS(files map[string]string, sizes map[string]int64) *executor.MockExecutor {
	return &executor.MockExecutor{
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			// Every probe runs as the site account: sudo -u <user> -- <cmd> ... <path>
			if name != "-u" || len(args) < 4 || args[1] != "--" {
				return &executor.Result{ExitCode: 1}, nil
			}
			path := args[len(args)-1]
			content, ok := files[path]
			switch args[2] {
			case "test":
				if ok {
					return &executor.Result{}, nil
				}
				return &executor.Result{ExitCode: 1}, nil
			case "stat":
				if !ok {
					return &executor.Result{ExitCode: 1}, nil
				}
				size := int64(len(content))
				if s, big := sizes[path]; big {
					size = s
				}
				return &executor.Result{Stdout: strconv.FormatInt(size, 10) + "\n"}, nil
			case "cat":
				if !ok {
					return &executor.Result{ExitCode: 1}, nil
				}
				return &executor.Result{Stdout: content}, nil
			}
			return &executor.Result{ExitCode: 1}, nil
		},
	}
}

const composerFixture = `{"packages":[{"name":"laravel/framework","version":"v11.9.2"}],"packages-dev":[]}`
const npmFixture = `{"lockfileVersion":3,"packages":{"":{},"node_modules/axios":{"version":"1.6.0"}}}`

func TestProjectDirs(t *testing.T) {
	got := projectDirs(Site{WebUser: "web_a", DocumentRoot: "/home/web_a/app/public"})
	if strings.Join(got, ",") != "/home/web_a/app,/home/web_a/app/public,/home/web_a" {
		t.Errorf("laravel dirs = %v", got)
	}
	got = projectDirs(Site{WebUser: "web_b", DocumentRoot: "/home/web_b"})
	if strings.Join(got, ",") != "/home/web_b" {
		t.Errorf("home docroot dirs = %v", got)
	}
}

func TestSiteDependenciesReadsEachLockfileOnce(t *testing.T) {
	exec := fakeFS(map[string]string{
		"/home/web_a/app/composer.lock":     composerFixture,
		"/home/web_a/app/package-lock.json": npmFixture,
		// A second composer.lock further down the candidates is ignored.
		"/home/web_a/composer.lock": `{"packages":[{"name":"old/pkg","version":"1.0.0"}]}`,
	}, nil)
	deps, read, err := siteDependencies(context.Background(), exec, Site{WebUser: "web_a", DocumentRoot: "/home/web_a/app/public"})
	if err != nil {
		t.Fatal(err)
	}
	assertDeps(t, deps, "Packagist:laravel/framework@11.9.2", "npm:axios@1.6.0")
	if strings.Join(read, ",") != "/home/web_a/app/composer.lock,/home/web_a/app/package-lock.json" {
		t.Errorf("read = %v", read)
	}
}

func TestSiteDependenciesSkipsOversizeAndBrokenFiles(t *testing.T) {
	exec := fakeFS(map[string]string{
		"/home/web_b/composer.lock":     composerFixture,
		"/home/web_b/package-lock.json": "{broken",
		"/home/web_b/yarn.lock":         "lodash@^4:\n  version \"4.17.21\"\n",
	}, map[string]int64{"/home/web_b/composer.lock": maxLockfileBytes + 1})
	deps, _, err := siteDependencies(context.Background(), exec, Site{WebUser: "web_b", DocumentRoot: "/home/web_b"})
	if err != nil {
		t.Fatal(err)
	}
	assertDeps(t, deps, "npm:lodash@4.17.21")
}

func TestSiteDependenciesRejectsUnsafeWebUser(t *testing.T) {
	exec := fakeFS(map[string]string{"/home/root/composer.lock": composerFixture}, nil)
	if _, _, err := siteDependencies(context.Background(), exec, Site{WebUser: "../root", DocumentRoot: "/home/root"}); err == nil {
		t.Error("an unsafe web user must be rejected")
	}
}
