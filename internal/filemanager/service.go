package filemanager

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/mohammadirham37/jenderal_panel/internal/audit"
	"github.com/mohammadirham37/jenderal_panel/internal/executor"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

// Service manages file operations within website document roots.
type Service struct {
	exec  executor.CommandExecutor
	audit *audit.Service
}

// NewService creates a new file manager Service.
func NewService(exec executor.CommandExecutor, auditSvc *audit.Service) *Service {
	return &Service{exec: exec, audit: auditSvc}
}

// validatePath ensures subPath stays within basePath. It returns the cleaned
// absolute path or an error if the path escapes the base directory.
func validatePath(basePath, subPath string) (string, error) {
	basePath = filepath.Clean(basePath)
	if subPath == "" {
		return basePath, nil
	}

	// Reject any path containing .. components before cleaning.
	if strings.Contains(subPath, "..") {
		return "", model.NewValidationError("path must not contain '..'")
	}

	full := filepath.Join(basePath, subPath)
	full = filepath.Clean(full)

	if !strings.HasPrefix(full, basePath) {
		return "", model.NewValidationError("path escapes base directory")
	}

	return full, nil
}

// Browse lists the contents of a directory within the website's base path.
func (s *Service) Browse(ctx context.Context, basePath, subPath string) ([]model.FileEntry, error) {
	dir, err := validatePath(basePath, subPath)
	if err != nil {
		return nil, err
	}

	result, err := s.exec.RunSudo(ctx, "ls", "-la", dir)
	if err != nil {
		return nil, fmt.Errorf("file browse: %w", err)
	}
	if result.ExitCode != 0 {
		return nil, model.NewDomainError("FILE_ERROR", "failed to list directory: "+result.Stderr, nil)
	}

	return parseLsOutput(result.Stdout, dir), nil
}

// ReadFile reads the contents of a file within the website's base path.
func (s *Service) ReadFile(ctx context.Context, basePath, filePath string) (string, error) {
	full, err := validatePath(basePath, filePath)
	if err != nil {
		return "", err
	}

	result, err := s.exec.RunSudo(ctx, "cat", full)
	if err != nil {
		return "", fmt.Errorf("file read: %w", err)
	}
	if result.ExitCode != 0 {
		return "", model.NewDomainError("FILE_ERROR", "failed to read file: "+result.Stderr, nil)
	}

	return result.Stdout, nil
}

// WriteFile writes content to a file within the website's base path using a
// temporary file and sudo cp to avoid permission issues.
func (s *Service) WriteFile(ctx context.Context, basePath, filePath, content string) error {
	full, err := validatePath(basePath, filePath)
	if err != nil {
		return err
	}

	// Write to a temp file first, then sudo cp to the target.
	tmpFile := "/tmp/jenderal_write_" + filepath.Base(full)

	// Use tee to write content via stdin-like approach.
	result, err := s.exec.Run(ctx, "sh", "-c", fmt.Sprintf("cat > %s", tmpFile))
	if err != nil {
		// Fallback: use printf to write content to temp file.
		result, err = s.exec.Run(ctx, "sh", "-c", fmt.Sprintf("printf '%%s' %q > %s", content, tmpFile))
		if err != nil {
			return fmt.Errorf("file write temp: %w", err)
		}
		if result.ExitCode != 0 {
			return model.NewDomainError("FILE_ERROR", "failed to write temp file: "+result.Stderr, nil)
		}
	} else {
		// First attempt didn't work as expected; use printf directly.
		result, err = s.exec.Run(ctx, "sh", "-c", fmt.Sprintf("printf '%%s' %q > %s", content, tmpFile))
		if err != nil {
			return fmt.Errorf("file write temp: %w", err)
		}
		if result.ExitCode != 0 {
			return model.NewDomainError("FILE_ERROR", "failed to write temp file: "+result.Stderr, nil)
		}
	}

	// Copy the temp file to the target location with sudo.
	result, err = s.exec.RunSudo(ctx, "cp", tmpFile, full)
	if err != nil {
		return fmt.Errorf("file write copy: %w", err)
	}
	if result.ExitCode != 0 {
		return model.NewDomainError("FILE_ERROR", "failed to copy file: "+result.Stderr, nil)
	}

	// Clean up the temp file.
	_, _ = s.exec.Run(ctx, "rm", "-f", tmpFile)

	return nil
}

// DeleteFile removes a file within the website's base path.
func (s *Service) DeleteFile(ctx context.Context, basePath, filePath string) error {
	full, err := validatePath(basePath, filePath)
	if err != nil {
		return err
	}

	result, err := s.exec.RunSudo(ctx, "rm", "-rf", full)
	if err != nil {
		return fmt.Errorf("file delete: %w", err)
	}
	if result.ExitCode != 0 {
		return model.NewDomainError("FILE_ERROR", "failed to delete file: "+result.Stderr, nil)
	}

	return nil
}

// Rename renames or moves a file/directory within the website's base path.
func (s *Service) Rename(ctx context.Context, basePath, oldPath, newPath string) error {
	fullOld, err := validatePath(basePath, oldPath)
	if err != nil {
		return err
	}
	fullNew, err := validatePath(basePath, newPath)
	if err != nil {
		return err
	}

	result, err := s.exec.RunSudo(ctx, "mv", fullOld, fullNew)
	if err != nil {
		return fmt.Errorf("file rename: %w", err)
	}
	if result.ExitCode != 0 {
		return model.NewDomainError("FILE_ERROR", "failed to rename: "+result.Stderr, nil)
	}

	return nil
}

// CreateDir creates a directory (including parents) within the website's base path.
func (s *Service) CreateDir(ctx context.Context, basePath, dirPath string) error {
	full, err := validatePath(basePath, dirPath)
	if err != nil {
		return err
	}

	result, err := s.exec.RunSudo(ctx, "mkdir", "-p", full)
	if err != nil {
		return fmt.Errorf("create dir: %w", err)
	}
	if result.ExitCode != 0 {
		return model.NewDomainError("FILE_ERROR", "failed to create directory: "+result.Stderr, nil)
	}

	return nil
}

// Chmod changes the permissions of a file within the website's base path.
func (s *Service) Chmod(ctx context.Context, basePath, filePath, mode string) error {
	full, err := validatePath(basePath, filePath)
	if err != nil {
		return err
	}

	result, err := s.exec.RunSudo(ctx, "chmod", mode, full)
	if err != nil {
		return fmt.Errorf("chmod: %w", err)
	}
	if result.ExitCode != 0 {
		return model.NewDomainError("FILE_ERROR", "failed to chmod: "+result.Stderr, nil)
	}

	return nil
}

// parseLsOutput parses the output of `ls -la` into FileEntry models.
// Expected format per line:
//
//	-rw-r--r--  1 user group  1234 Jan 15 10:30 filename
func parseLsOutput(output, dir string) []model.FileEntry {
	var entries []model.FileEntry
	lines := strings.Split(output, "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Skip the "total NNN" line.
		if strings.HasPrefix(line, "total ") {
			continue
		}
		// Skip . and .. entries.
		if strings.HasSuffix(line, " .") || strings.HasSuffix(line, " ..") {
			continue
		}

		entry := parseLsLine(line, dir)
		if entry != nil {
			entries = append(entries, *entry)
		}
	}

	return entries
}

// parseLsLine parses a single ls -la output line into a FileEntry.
func parseLsLine(line, dir string) *model.FileEntry {
	// ls -la output has at least 9 fields:
	// permissions links owner group size month day time/year name
	fields := strings.Fields(line)
	if len(fields) < 9 {
		return nil
	}

	permissions := fields[0]
	owner := fields[2]
	// group is fields[3]

	size, _ := strconv.ParseInt(fields[4], 10, 64)

	// Date is month day time/year (fields 5-7).
	modTime := strings.Join(fields[5:8], " ")

	// Name is everything after the date fields. This handles filenames
	// with spaces. For symlinks, ls shows "name -> target"; we keep only
	// the name part.
	name := strings.Join(fields[8:], " ")
	if idx := strings.Index(name, " -> "); idx != -1 {
		name = name[:idx]
	}

	isDir := len(permissions) > 0 && permissions[0] == 'd'
	path := filepath.Join(dir, name)

	return &model.FileEntry{
		Name:        name,
		Path:        path,
		IsDir:       isDir,
		Size:        size,
		Permissions: permissions,
		Owner:       owner,
		ModTime:     modTime,
	}
}
