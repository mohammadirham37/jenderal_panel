package website

import (
	"strings"
	"testing"

	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

func TestBuildCommandScript(t *testing.T) {
	w := model.Website{WebUser: "web_example_com", DocumentRoot: "/home/web_example_com/app/public"}

	composer := buildCommandScript(w, "/home/web_example_com/app", []string{"composer", "install", "--no-interaction"})
	if composer != "cd /home/web_example_com/app && composer install --no-interaction" {
		t.Errorf("system-wide command should run directly, got %q", composer)
	}

	npm := buildCommandScript(w, "/home/web_example_com/app", []string{"npm", "install"})
	if !strings.Contains(npm, "NVM_DIR=/home/web_example_com/.nvm") {
		t.Errorf("npm command should set NVM_DIR, got %q", npm)
	}
	if !strings.Contains(npm, "/home/web_example_com/.nvm/nvm-exec npm install") {
		t.Errorf("npm command should run through nvm-exec, got %q", npm)
	}
	if !strings.HasPrefix(npm, "cd /home/web_example_com/app && ") {
		t.Errorf("npm command should run in the project directory, got %q", npm)
	}
	if !strings.Contains(npm, "Node.js runtime is not installed") {
		t.Errorf("npm command should explain a missing runtime, got %q", npm)
	}
}
