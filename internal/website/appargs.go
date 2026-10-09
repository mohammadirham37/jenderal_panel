package website

import (
	"fmt"
	"strings"

	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

// shQuote single-quote-escapes a value for safe use inside a shell string.
func shQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

// buildAppStartArgv assembles the argv that runs the app start command for a
// runtime. Relative binaries (./app) resolve against the working directory.
func buildAppStartArgv(runtime, webUser, docroot, startCommand string) ([]string, error) {
	startCommand = strings.TrimSpace(startCommand)
	if startCommand == "" {
		return nil, model.NewValidationError("start command is required")
	}
	switch runtime {
	case "go", "go-binary":
		// Relative executables resolve against the document root.
		if strings.HasPrefix(startCommand, "./") {
			startCommand = docroot + "/" + strings.TrimPrefix(startCommand, "./")
		}
		return []string{startCommand}, nil
	default:
		return nil, fmt.Errorf("unsupported app runtime: %s", runtime)
	}
}

// buildAppBuildArgv assembles the full RunSudo argv that runs the build
// command: ["-u", user, "--", "/bin/bash", "-c", script]. The go toolchain
// is prepended to PATH inside the script; the command is the site owner's
// own and runs as their user. For go/go-binary, dir is the repository root;
// for python it is the document root.
func buildAppBuildArgv(runtime, webUser, dir, buildCommand string) ([]string, error) {
	// The dependency install (venv + requirements.txt) runs even without a
	// user build command.
	buildCommand = strings.TrimSpace(buildCommand)
	switch runtime {
	case "go", "go-binary":
		script := "cd " + dir + " && PATH=/usr/local/go/bin:$PATH " + buildCommand
		return []string{"-u", webUser, "--", "/bin/bash", "-c", script}, nil
	case "python":
		home := "/home/" + webUser
		venvBin := home + "/venv/bin"
		venvPython := venvBin + "/python"
		venvDir := home + "/venv"
		script := "cd " + dir + " && " +
			"[ -x " + shQuote(venvPython) + " ] || " +
			"/usr/bin/python3 -m venv " + shQuote(venvDir) + " && " +
			"[ ! -f requirements.txt ] || " + venvBin + "/pip install --no-input -r requirements.txt"
		if buildCommand != "" {
			script += " && " + buildCommand
		}
		return []string{"-u", webUser, "--", "/bin/bash", "-c", script}, nil
	default:
		return nil, fmt.Errorf("unsupported app runtime: %s", runtime)
	}
}
