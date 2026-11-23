package website

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

// maxEnvSize bounds the accepted .env content size.
const maxEnvSize = 256 << 10

// GetEnvFile reads the website's Laravel .env file. The second return value
// reports whether the file exists.
func (s *Service) GetEnvFile(ctx context.Context, websiteID string) (string, bool, error) {
	w, err := s.Get(ctx, websiteID)
	if err != nil {
		return "", false, err
	}

	dir := s.findDirWithFile(ctx, w, ".env")
	if dir == "" {
		return "", false, nil
	}

	res, err := s.exec.RunSudo(ctx, "cat", filepath.Join(dir, ".env"))
	if err != nil {
		return "", false, fmt.Errorf("read .env: %w", err)
	}
	if res.ExitCode != 0 {
		return "", false, fmt.Errorf("read .env: %s", strings.TrimSpace(res.Stderr))
	}

	return res.Stdout, true, nil
}

// UpdateEnvFile writes content to the website's .env file. The target
// directory is the one holding the current .env, else the one holding
// .env.example (the file will be created there).
func (s *Service) UpdateEnvFile(ctx context.Context, websiteID, content string) error {
	if len(content) > maxEnvSize {
		return model.NewValidationError(".env content too large (max 256 KB)")
	}

	w, err := s.Get(ctx, websiteID)
	if err != nil {
		return err
	}

	dir := s.findDirWithFile(ctx, w, ".env")
	if dir == "" {
		dir = s.findDirWithFile(ctx, w, ".env.example")
	}
	if dir == "" {
		return model.NewValidationError("no project directory containing .env or .env.example was found")
	}

	envPath := filepath.Join(dir, ".env")
	write := envWriteArgs(w.WebUser, envPath)
	res, err := s.exec.RunSudoWithInput(ctx, content, write[0], write[1:]...)
	if err != nil {
		return fmt.Errorf("write .env: %w", err)
	}
	if res.ExitCode != 0 && s.adoptEnvFile(ctx, w.WebUser, envPath) {
		res, err = s.exec.RunSudoWithInput(ctx, content, write[0], write[1:]...)
		if err != nil {
			return fmt.Errorf("write .env: %w", err)
		}
	}
	if res.ExitCode != 0 {
		return model.NewDomainError("ENV_WRITE_FAILED", "could not write "+envPath+": "+strings.TrimSpace(res.Stderr), nil)
	}
	return nil
}

// envWriteArgs writes stdin to path as the site account, mode 600. It used
// to be "install ... /dev/stdin <path>" as root, which the Rust coreutils of
// Ubuntu 26.04 reject (not a regular file). Writing as the site account also
// means a symlink planted at .env cannot redirect a root write. The path is
// a positional parameter, never part of the shell text.
func envWriteArgs(webUser, path string) []string {
	return []string{"-u", webUser, "--", "/bin/sh", "-c", `umask 077 && cat > "$1" && chmod 600 "$1"`, "sh", path}
}

// adoptEnvFile gives an existing regular .env (not a symlink) that the site
// account cannot write, e.g. one edited as root, back to that account. It
// reports whether it changed anything worth a retry.
func (s *Service) adoptEnvFile(ctx context.Context, webUser, path string) bool {
	if res, err := s.exec.RunSudo(ctx, "test", "-L", path); err != nil || res == nil || res.ExitCode == 0 {
		return false
	}
	if res, err := s.exec.RunSudo(ctx, "test", "-f", path); err != nil || res == nil || res.ExitCode != 0 {
		return false
	}
	res, err := s.exec.RunSudo(ctx, "chown", "--no-dereference", webUser+":"+webUser, path)
	return err == nil && res != nil && res.ExitCode == 0
}
