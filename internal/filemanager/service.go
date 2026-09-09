package filemanager

import (
	"context"
	"fmt"
	"os"
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

	rel, err := filepath.Rel(basePath, full)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", model.NewValidationError("path escapes base directory")
	}

	return full, nil
}

// resolvePath resolves symlinks in both the website home and requested path,
// then verifies that the canonical target remains inside the website home.
// For a target that does not exist yet, its nearest existing ancestor is
// resolved before the missing suffix is appended.
func resolvePath(basePath, subPath string, allowMissing bool) (string, error) {
	target, err := validatePath(basePath, subPath)
	if err != nil {
		return "", err
	}
	basePath = filepath.Clean(basePath)
	relativeTarget, err := filepath.Rel(basePath, target)
	if err != nil {
		return "", model.NewValidationError("path is not accessible")
	}
	cursor := basePath
	for _, component := range strings.Split(relativeTarget, string(filepath.Separator)) {
		if component == "." || component == "" {
			continue
		}
		cursor = filepath.Join(cursor, component)
		info, statErr := os.Lstat(cursor)
		if statErr != nil {
			if allowMissing && os.IsNotExist(statErr) {
				break
			}
			return "", model.NewValidationError("path is not accessible")
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", model.NewValidationError("symbolic links cannot be used in file manager paths")
		}
	}

	resolvedBase, err := filepath.EvalSymlinks(basePath)
	if err != nil {
		return "", model.NewValidationError("website home is not accessible")
	}

	resolvedTarget, err := filepath.EvalSymlinks(target)
	if err != nil {
		if !allowMissing || !os.IsNotExist(err) {
			return "", model.NewValidationError("path is not accessible")
		}

		cursor := target
		var missing []string
		for {
			parent := filepath.Dir(cursor)
			if parent == cursor {
				return "", model.NewValidationError("path is not accessible")
			}
			missing = append([]string{filepath.Base(cursor)}, missing...)
			cursor = parent
			resolvedParent, resolveErr := filepath.EvalSymlinks(cursor)
			if resolveErr == nil {
				resolvedTarget = filepath.Join(append([]string{resolvedParent}, missing...)...)
				break
			}
			if !os.IsNotExist(resolveErr) {
				return "", model.NewValidationError("path is not accessible")
			}
		}
	}

	rel, err := filepath.Rel(resolvedBase, resolvedTarget)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", model.NewValidationError("path escapes website home through a symlink")
	}
	return resolvedTarget, nil
}

func rejectWebsiteRoot(basePath, target string) error {
	resolvedBase, err := filepath.EvalSymlinks(filepath.Clean(basePath))
	if err != nil {
		return model.NewValidationError("website home is not accessible")
	}
	if filepath.Clean(target) == filepath.Clean(resolvedBase) {
		return model.NewValidationError("the website root cannot be modified")
	}
	return nil
}

// Browse lists the contents of a directory within the website's base path.
func (s *Service) Browse(ctx context.Context, basePath, subPath string) ([]model.FileEntry, error) {
	dir, err := resolvePath(basePath, subPath, false)
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
	full, err := resolvePath(basePath, filePath, false)
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
	full, err := resolvePath(basePath, filePath, true)
	if err != nil {
		return err
	}
	if err := rejectWebsiteRoot(basePath, full); err != nil {
		return err
	}

	tmpFile, err := os.CreateTemp("", "jenderal_write_*.tmp")
	if err != nil {
		return fmt.Errorf("create file write temp: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)
	if _, err := tmpFile.WriteString(content); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("write file temp: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("close file temp: %w", err)
	}

	// --remove-destination prevents a last-moment destination symlink from
	// being followed. All user-controlled values remain separate arguments.
	result, err := s.exec.RunSudo(ctx, "cp", "--remove-destination", "--", tmpPath, full)
	if err != nil {
		return fmt.Errorf("file write copy: %w", err)
	}
	if result.ExitCode != 0 {
		return model.NewDomainError("FILE_ERROR", "failed to copy file: "+result.Stderr, nil)
	}

	webUser := filepath.Base(filepath.Clean(basePath))
	result, err = s.exec.RunSudo(ctx, "chown", "--", webUser+":"+webUser, full)
	if err != nil {
		return fmt.Errorf("file write ownership: %w", err)
	}
	if result.ExitCode != 0 {
		return model.NewDomainError("FILE_ERROR", "failed to set file ownership: "+result.Stderr, nil)
	}

	return nil
}

// DeleteFile removes a file within the website's base path.
func (s *Service) DeleteFile(ctx context.Context, basePath, filePath string) error {
	full, err := resolvePath(basePath, filePath, false)
	if err != nil {
		return err
	}
	if err := rejectWebsiteRoot(basePath, full); err != nil {
		return err
	}

	result, err := s.exec.RunSudo(ctx, "rm", "-rf", "--", full)
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
	fullOld, err := resolvePath(basePath, oldPath, false)
	if err != nil {
		return err
	}
	fullNew, err := resolvePath(basePath, newPath, true)
	if err != nil {
		return err
	}
	if err := rejectWebsiteRoot(basePath, fullOld); err != nil {
		return err
	}
	if err := rejectWebsiteRoot(basePath, fullNew); err != nil {
		return err
	}

	result, err := s.exec.RunSudo(ctx, "mv", "--", fullOld, fullNew)
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
	full, err := resolvePath(basePath, dirPath, true)
	if err != nil {
		return err
	}
	if err := rejectWebsiteRoot(basePath, full); err != nil {
		return err
	}

	result, err := s.exec.RunSudo(ctx, "mkdir", "-p", "--", full)
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
	full, err := resolvePath(basePath, filePath, false)
	if err != nil {
		return err
	}
	if err := rejectWebsiteRoot(basePath, full); err != nil {
		return err
	}

	result, err := s.exec.RunSudo(ctx, "chmod", "--", mode, full)
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
