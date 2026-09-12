package website

import (
	"strings"
	"testing"

	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

func TestBuildAppUnit(t *testing.T) {
	w := model.Website{
		ID: "site-1", Domain: "app.example.com", AppType: "node",
		PHPVersion: "8.2", NodeVersion: "22",
		DocumentRoot: "/home/web_example_com/public", WebUser: "web_example_com",
		AppStartCommand: "npm run start", AppPort: 8200,
	}
	unit, err := buildAppUnit(w, w.DocumentRoot)
	if err != nil {
		t.Fatalf("buildAppUnit: %v", err)
	}
	for _, want := range []string{
		"User=web_example_com",
		"WorkingDirectory=/home/web_example_com/public",
		"NODE_VERSION=22",
		"nvm-exec npm run start",
		"Restart=always",
	} {
		if !strings.Contains(unit, want) {
			t.Errorf("unit missing %q:\n%s", want, unit)
		}
	}
}

func TestBuildAppUnitRejectsEmptyCommand(t *testing.T) {
	w := model.Website{WebUser: "web_example_com", NodeVersion: "22"}
	if _, err := buildAppUnit(w, "/home/web_example_com/public"); err == nil {
		t.Fatal("empty start command must be rejected")
	}
}

func TestAppProxyNginxProfile(t *testing.T) {
	data := VhostData{Domain: "app.example.com", Profile: "app-proxy", AppPort: 8200}
	d := directivesFor(data)

	if !strings.Contains(d.Header, "$jenderal_app_app_example_com") {
		t.Errorf("ws map variable missing: %q", d.Header)
	}
	if !strings.Contains(d.Location, "proxy_pass http://127.0.0.1:8200;") {
		t.Errorf("proxy target wrong: %q", d.Location)
	}
	if !strings.Contains(d.Location, "try_files $uri $uri/ @app;") {
		t.Errorf("static-first try_files missing: %q", d.Location)
	}
	if !strings.Contains(d.Location, "Upgrade $http_upgrade") {
		t.Errorf("websocket upgrade header missing: %q", d.Location)
	}
}
