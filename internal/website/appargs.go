package website

import (
	"fmt"
	"strings"

	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

// validateCommandLine refuses values that would break out of the single
// line they are written into: a systemd unit directive (ExecStart=) or a
// shell script line. Everything else stays allowed, so ordinary commands
// with flags and quotes keep working.
func validateCommandLine(kind, value string) error {
	if strings.ContainsAny(value, "\n\r\x00") {
		return model.NewValidationError(kind + " must be a single line without control characters")
	}
	if len(value) > 1024 {
		return model.NewValidationError(kind + " is too long (max 1024 characters)")
	}
	return nil
}

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
	if err := validateCommandLine("start command", startCommand); err != nil {
		return nil, err
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
	if err := validateCommandLine("build command", buildCommand); err != nil {
		return nil, err
	}
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
