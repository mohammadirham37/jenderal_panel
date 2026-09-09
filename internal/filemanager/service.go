package filemanager

import (
	"context"
	"errors"
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

func rejectWebsiteRoot(basePath, subPath string) error {
	target, err := validatePath(basePath, subPath)
	if err != nil {
		return err
	}
	if filepath.Clean(target) == filepath.Clean(basePath) {
		return model.NewValidationError("the website root cannot be modified")
	}
	return nil
}

func (s *Service) runAsWebsiteUser(ctx context.Context, basePath, name string, args ...string) (*executor.Result, error) {
	webUser := filepath.Base(filepath.Clean(basePath))
	sudoArgs := make([]string, 0, len(args)+3)
	sudoArgs = append(sudoArgs, webUser, "--", name)
	sudoArgs = append(sudoArgs, args...)
	return s.exec.RunSudo(ctx, "-u", sudoArgs...)
}

func (s *Service) runAsWebsiteUserWithInput(ctx context.Context, basePath, input, name string, args ...string) (*executor.Result, error) {
	webUser := filepath.Base(filepath.Clean(basePath))
	sudoArgs := make([]string, 0, len(args)+3)
	sudoArgs = append(sudoArgs, webUser, "--", name)
	sudoArgs = append(sudoArgs, args...)
	return s.exec.RunSudoWithInput(ctx, input, "-u", sudoArgs...)
}

func (s *Service) rejectWebsiteSymlinksAsOwner(ctx context.Context, basePath, target string, allowMissing bool) error {
	relativeTarget, err := filepath.Rel(filepath.Clean(basePath), target)
	if err != nil {
		return model.NewValidationError("path is not accessible")
	}
	cursor := filepath.Clean(basePath)
	for _, component := range strings.Split(relativeTarget, string(filepath.Separator)) {
		if component == "." || component == "" {
			continue
		}
		cursor = filepath.Join(cursor, component)
		linkResult, runErr := s.runAsWebsiteUser(ctx, basePath, "test", "-L", cursor)
		if runErr != nil {
			return runErr
		}
		if linkResult.ExitCode == 0 {
			return model.NewValidationError("symbolic links cannot be used in file manager paths")
		}
		existsResult, runErr := s.runAsWebsiteUser(ctx, basePath, "test", "-e", cursor)
		if runErr != nil {
			return runErr
		}
		if existsResult.ExitCode != 0 {
			if allowMissing {
				break
			}
			return model.NewValidationError("path is not accessible")
		}
	}
	return nil
}

func (s *Service) resolveWebsitePath(ctx context.Context, basePath, subPath string, allowMissing bool) (string, error) {
	resolved, err := resolvePath(basePath, subPath, allowMissing)
	if err == nil {
		return resolved, nil
	}

	var domainErr *model.DomainError
	if !errors.As(err, &domainErr) || (domainErr.Message != "path is not accessible" && domainErr.Message != "website home is not accessible") {
		return "", err
	}

	// A website may legitimately use mode 0700. Resolve in that case as its
	// owner, while keeping all subsequent operations under the same user.
	target, validationErr := validatePath(basePath, subPath)
	if validationErr != nil {
		return "", validationErr
	}
	if symlinkErr := s.rejectWebsiteSymlinksAsOwner(ctx, basePath, target, allowMissing); symlinkErr != nil {
		return "", symlinkErr
	}
	resolve := func(flag, path string) (string, error) {
		result, runErr := s.runAsWebsiteUser(ctx, basePath, "realpath", flag, "--", path)
		if runErr != nil {
			return "", runErr
		}
		if result.ExitCode != 0 {
			return "", model.NewValidationError("path is not accessible")
		}
		return strings.TrimSpace(result.Stdout), nil
	}

	resolvedBase, resolveErr := resolve("-e", filepath.Clean(basePath))
	if resolveErr != nil {
		return "", resolveErr
	}
	targetFlag := "-e"
	if allowMissing {
		targetFlag = "-m"
	}
	resolvedTarget, resolveErr := resolve(targetFlag, target)
	if resolveErr != nil {
		return "", resolveErr
	}
	rel, resolveErr := filepath.Rel(resolvedBase, resolvedTarget)
	if resolveErr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", model.NewValidationError("path escapes website home through a symlink")
	}
	return resolvedTarget, nil
}

func (s *Service) rejectResolvedWebsiteRoot(ctx context.Context, basePath, target string) error {
	resolvedBase, err := s.resolveWebsitePath(ctx, basePath, "", false)
	if err != nil {
		return err
	}
	if filepath.Clean(target) == filepath.Clean(resolvedBase) {
		return model.NewValidationError("the website root cannot be modified")
	}
	return nil
}

// Browse lists the contents of a directory within the website's base path.
func (s *Service) Browse(ctx context.Context, basePath, subPath string) ([]model.FileEntry, error) {
	dir, err := s.resolveWebsitePath(ctx, basePath, subPath, false)
	if err != nil {
		return nil, err
	}

	result, err := s.runAsWebsiteUser(ctx, basePath, "ls", "-la", "--", dir)
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
	full, err := s.resolveWebsitePath(ctx, basePath, filePath, false)
	if err != nil {
		return "", err
	}

	result, err := s.runAsWebsiteUser(ctx, basePath, "cat", "--", full)
	if err != nil {
		return "", fmt.Errorf("file read: %w", err)
	}
	if result.ExitCode != 0 {
		return "", model.NewDomainError("FILE_ERROR", "failed to read file: "+result.Stderr, nil)
	}

	return result.Stdout, nil
}

// WriteFile writes content as the website's Linux user so a path race cannot
// turn a file-manager request into a privileged filesystem operation.
func (s *Service) WriteFile(ctx context.Context, basePath, filePath, content string) error {
	if err := rejectWebsiteRoot(basePath, filePath); err != nil {
		return err
	}
	full, err := s.resolveWebsitePath(ctx, basePath, filePath, true)
	if err != nil {
		return err
	}
	if err := s.rejectResolvedWebsiteRoot(ctx, basePath, full); err != nil {
		return err
	}

	existsResult, err := s.runAsWebsiteUser(ctx, basePath, "stat", "--", full)
	if err != nil {
		return fmt.Errorf("check existing file: %w", err)
	}
	existed := existsResult.ExitCode == 0

	result, err := s.runAsWebsiteUserWithInput(ctx, basePath, content, "tee", "--", full)
	if err != nil {
		return fmt.Errorf("file write: %w", err)
	}
	if result.ExitCode != 0 {
		return model.NewDomainError("FILE_ERROR", "failed to write file: "+result.Stderr, nil)
	}
	if !existed {
		result, err = s.runAsWebsiteUser(ctx, basePath, "chmod", "0644", "--", full)
		if err != nil {
			return fmt.Errorf("set new file mode: %w", err)
		}
		if result.ExitCode != 0 {
			return model.NewDomainError("FILE_ERROR", "failed to set new file mode: "+result.Stderr, nil)
		}
	}

	return nil
}

// DeleteFile removes a file within the website's base path.
func (s *Service) DeleteFile(ctx context.Context, basePath, filePath string) error {
	if err := rejectWebsiteRoot(basePath, filePath); err != nil {
		return err
	}
	full, err := s.resolveWebsitePath(ctx, basePath, filePath, false)
	if err != nil {
		return err
	}
	if err := s.rejectResolvedWebsiteRoot(ctx, basePath, full); err != nil {
		return err
	}

	result, err := s.runAsWebsiteUser(ctx, basePath, "rm", "-rf", "--", full)
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
	if err := rejectWebsiteRoot(basePath, oldPath); err != nil {
		return err
	}
	if err := rejectWebsiteRoot(basePath, newPath); err != nil {
		return err
	}
	fullOld, err := s.resolveWebsitePath(ctx, basePath, oldPath, false)
	if err != nil {
		return err
	}
	fullNew, err := s.resolveWebsitePath(ctx, basePath, newPath, true)
	if err != nil {
		return err
	}
	if err := s.rejectResolvedWebsiteRoot(ctx, basePath, fullOld); err != nil {
		return err
	}
	if err := s.rejectResolvedWebsiteRoot(ctx, basePath, fullNew); err != nil {
		return err
	}
	result, err := s.runAsWebsiteUser(ctx, basePath, "mv", "--", fullOld, fullNew)
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
	if err := rejectWebsiteRoot(basePath, dirPath); err != nil {
		return err
	}
	full, err := s.resolveWebsitePath(ctx, basePath, dirPath, true)
	if err != nil {
		return err
	}
	if err := s.rejectResolvedWebsiteRoot(ctx, basePath, full); err != nil {
		return err
	}
	result, err := s.runAsWebsiteUser(ctx, basePath, "mkdir", "-p", "--", full)
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
	if err := rejectWebsiteRoot(basePath, filePath); err != nil {
		return err
	}
	full, err := s.resolveWebsitePath(ctx, basePath, filePath, false)
	if err != nil {
		return err
	}
	if err := s.rejectResolvedWebsiteRoot(ctx, basePath, full); err != nil {
		return err
	}
	result, err := s.runAsWebsiteUser(ctx, basePath, "chmod", "--", mode, full)
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
