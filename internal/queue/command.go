package queue

import (
	"strings"

	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

// validateWorkerCommand refuses commands that would break out of the single
// ExecStart= line they are written into, or out of the heredoc the unit file
// is written with. Ordinary artisan commands with flags are unaffected.
func validateWorkerCommand(command string) error {
	command = strings.TrimSpace(command)
	if command == "" {
		return model.NewValidationError("command is required")
	}
	if strings.ContainsAny(command, "\n\r\x00") {
		return model.NewValidationError("command must be a single line without control characters")
	}
	if len(command) > 1024 {
		return model.NewValidationError("command is too long (max 1024 characters)")
	}
	return nil
}
