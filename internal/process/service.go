package process

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

// allowedSignals is the whitelist of signals that can be sent to processes.
var allowedSignals = map[string]bool{
	"TERM": true,
	"KILL": true,
	"HUP":  true,
	"INT":  true,
}

// Service provides process management operations.
type Service struct {
	exec executor.CommandExecutor
}

// NewService creates a new process Service.
func NewService(exec executor.CommandExecutor) *Service {
	return &Service{exec: exec}
}

// List returns a list of running processes sorted by the given field.
// Supported sort values: "cpu" (default) and "ram".
func (s *Service) List(ctx context.Context, sortBy string) ([]model.Process, error) {
	sortFlag := "--sort=-pcpu"
	if sortBy == "ram" {
		sortFlag = "--sort=-rss"
	}

	res, err := s.exec.Run(ctx, "ps", "aux", sortFlag)
	if err != nil {
		return nil, fmt.Errorf("run ps: %w", err)
	}

	return parsePS(res.Stdout), nil
}

// Kill sends a signal to a process.
func (s *Service) Kill(ctx context.Context, pid int, signal string) error {
	if pid <= 1 {
		return model.NewValidationError("cannot signal PID 0 or 1")
	}
	if pid == os.Getpid() {
		return model.NewValidationError("cannot signal own process")
	}
	if signal == "" {
		signal = "TERM"
	}
	if !allowedSignals[signal] {
		return model.NewValidationError(fmt.Sprintf("signal %q is not allowed; use one of TERM, KILL, HUP, INT", signal))
	}

	res, err := s.exec.RunSudo(ctx, "kill", fmt.Sprintf("-%s", signal), strconv.Itoa(pid))
	if err != nil {
		return fmt.Errorf("kill process: %w", err)
	}
	if res.ExitCode != 0 {
		return model.NewDomainError("KILL_FAILED", strings.TrimSpace(res.Stderr), nil)
	}

	return nil
}

// parsePS parses the output of "ps aux" into a slice of Process structs.
// It skips the header line and splits each line into fields. Fields from
// index 10 onward are joined as the command.
func parsePS(output string) []model.Process {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) < 2 {
		return nil
	}

	var processes []model.Process
	for _, line := range lines[1:] { // skip header
		fields := strings.Fields(line)
		if len(fields) < 11 {
			continue
		}

		pid, err := strconv.Atoi(fields[1])
		if err != nil {
			continue
		}

		cpu, _ := strconv.ParseFloat(fields[2], 64)
		ram, _ := strconv.ParseFloat(fields[3], 64)
		vsz, _ := strconv.ParseUint(fields[4], 10, 64)
		rss, _ := strconv.ParseUint(fields[5], 10, 64)

		command := strings.Join(fields[10:], " ")

		processes = append(processes, model.Process{
			PID:     pid,
			User:    fields[0],
			CPU:     cpu,
			RAM:     ram,
			VSZ:     vsz,
			RSS:     rss,
			Command: command,
			Started: fields[8],
		})
	}

	return processes
}
