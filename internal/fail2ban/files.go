package fail2ban

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/oklog/ulid/v2"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
)

type ManagedFiles interface {
	Read(context.Context, string) (content string, exists bool, err error)
	WriteAtomic(context.Context, string, string) error
	Remove(context.Context, string) error
}

type sudoManagedFiles struct {
	exec executor.CommandExecutor
}

func newSudoManagedFiles(exec executor.CommandExecutor) ManagedFiles {
	return &sudoManagedFiles{exec: exec}
}

func (f *sudoManagedFiles) Read(ctx context.Context, path string) (string, bool, error) {
	result, err := f.exec.RunSudo(ctx, "test", "-f", path)
	if err != nil {
		return "", false, fmt.Errorf("check managed file: %w", err)
	}
	if result.ExitCode != 0 {
		return "", false, nil
	}
	result, err = f.exec.RunSudo(ctx, "cat", path)
	if err != nil {
		return "", false, fmt.Errorf("read managed file: %w", err)
	}
	if result.ExitCode != 0 {
		return "", false, commandFailure("read managed file", result)
	}
	return result.Stdout, true, nil
}

func (f *sudoManagedFiles) WriteAtomic(ctx context.Context, path, content string) error {
	temporary := filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+"."+ulid.Make().String()+".tmp")
	result, err := f.exec.RunSudoWithInput(ctx, content, "tee", "--", temporary)
	if err != nil {
		return fmt.Errorf("write temporary managed file: %w", err)
	}
	if result.ExitCode != 0 {
		return commandFailure("write temporary managed file", result)
	}
	defer f.exec.RunSudo(context.Background(), "rm", "-f", "--", temporary)
	for _, command := range [][]string{{"chmod", "0644", temporary}, {"mv", "--", temporary, path}} {
		result, err = f.exec.RunSudo(ctx, command[0], command[1:]...)
		if err != nil {
			return fmt.Errorf("install managed file: %w", err)
		}
		if result.ExitCode != 0 {
			return commandFailure("install managed file", result)
		}
	}
	return nil
}

func (f *sudoManagedFiles) Remove(ctx context.Context, path string) error {
	result, err := f.exec.RunSudo(ctx, "rm", "-f", "--", path)
	if err != nil {
		return fmt.Errorf("remove managed file: %w", err)
	}
	if result.ExitCode != 0 {
		return commandFailure("remove managed file", result)
	}
	return nil
}

func commandFailure(action string, result *executor.Result) error {
	detail := strings.TrimSpace(result.Stderr)
	if detail == "" {
		detail = strings.TrimSpace(result.Stdout)
	}
	if detail == "" {
		detail = fmt.Sprintf("exit %d", result.ExitCode)
	}
	return fmt.Errorf("%s: %s", action, detail)
}
