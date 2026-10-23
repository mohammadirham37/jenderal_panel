package website

import (
	"context"
	"strings"
	"testing"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

// Root writes into a website home follow a symlink the site owner can plant,
// and a following chown then hands them the target. These files belong to
// the web user, so they are written as that account instead.
func TestPHPSettingsWriteAsWebsiteUser(t *testing.T) {
	var sudo [][]string
	mock := &executor.MockExecutor{
		RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return &executor.Result{}, nil
		},
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			sudo = append(sudo, append([]string{name}, args...))
			return &executor.Result{}, nil
		},
		RunSudoWithInputFunc: func(ctx context.Context, input, name string, args ...string) (*executor.Result, error) {
			sudo = append(sudo, append([]string{name}, args...))
			return &executor.Result{}, nil
		},
	}
	svc := NewService(nil, mock, nil)
	w := model.Website{WebUser: "web_shop", DocumentRoot: "/home/web_shop/public", PHPVersion: "8.3", Domain: "shop.test"}
	if err := svc.writeUserIni(context.Background(), w, map[string]string{"memory_limit": "256M"}); err != nil {
		t.Fatalf("writeUserIni: %v", err)
	}

	wrote := false
	for _, call := range sudo {
		line := strings.Join(call, " ")
		if strings.Contains(line, "tee") {
			if !strings.HasPrefix(line, "-u web_shop --") {
				t.Errorf("the .user.ini write runs as root: %s", line)
			}
			wrote = true
		}
		if call[0] == "chown" {
			t.Errorf("a root chown on a path inside the website home can be redirected: %s", line)
		}
	}
	if !wrote {
		t.Fatal("no write was issued")
	}
}
