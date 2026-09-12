package website

import (
	"fmt"
	"strings"

	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

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

// buildAppBuildArgv assembles the argv that runs the build command. The go
// toolchain is prepended to PATH via the shell; the build command is the
// site owner's own and runs as their user.
func buildAppBuildArgv(runtime, webUser, docroot, buildCommand string) ([]string, error) {
	buildCommand = strings.TrimSpace(buildCommand)
	if buildCommand == "" {
		return nil, model.NewValidationError("build command is required")
	}
	switch runtime {
	case "go", "go-binary":
		script := "cd " + docroot + " && " + buildCommand
		return []string{"/bin/bash", "-c", script}, nil
	default:
		return nil, fmt.Errorf("unsupported app runtime: %s", runtime)
	}
}
