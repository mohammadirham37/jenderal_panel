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
	res, err := s.exec.RunSudoWithInput(ctx, content,
		"install", "-o", w.WebUser, "-g", w.WebUser, "-m", "600", "/dev/stdin", envPath)
	if err != nil {
		return fmt.Errorf("write .env: %w", err)
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("write .env: %s", strings.TrimSpace(res.Stderr))
	}

	return nil
}
