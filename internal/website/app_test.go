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

func TestBuildPythonAppUnit(t *testing.T) {
	w := model.Website{
		ID: "site-2", Domain: "api.example.com", AppType: "python",
		PHPVersion: "8.2", DocumentRoot: "/home/web_example_com/public",
		WebUser: "web_example_com", AppStartCommand: "gunicorn -w 2 app:app",
	}
	unit, err := buildPythonAppUnit(w, w.DocumentRoot)
	if err != nil {
		t.Fatalf("buildPythonAppUnit: %v", err)
	}
	for _, want := range []string{
		`Environment="PATH=/home/web_example_com/venv/bin:/usr/local/bin:/usr/bin:/bin"`,
		"WorkingDirectory=/home/web_example_com/public",
		"exec gunicorn -w 2 app:app",
		"Restart=always",
	} {
		if !strings.Contains(unit, want) {
			t.Errorf("python unit missing %q:\n%s", want, unit)
		}
	}
}

func TestBuildAppBuildArgvPython(t *testing.T) {
	args, err := buildAppBuildArgv("python", "web_example_com", "/home/web_example_com/public", "")
	if err != nil {
		t.Fatalf("buildAppBuildArgv: %v", err)
	}
	script := args[2]
	for _, want := range []string{
		"python3 -m venv '/home/web_example_com/venv'",
		"pip install --no-input -r requirements.txt",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("python build script missing %q: %s", want, script)
		}
	}

	// A user build command runs after dependency installation.
	args, err = buildAppBuildArgv("python", "web_example_com", "/home/web_example_com/public", "python manage.py collectstatic --noinput")
	if err != nil {
		t.Fatalf("buildAppBuildArgv with build: %v", err)
	}
	if !strings.Contains(args[2], "python manage.py collectstatic --noinput") {
		t.Errorf("user build command missing: %s", args[2])
	}
}
