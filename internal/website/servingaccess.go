package website

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
)

// defaultNginxUser is the worker account of Ubuntu's packaged nginx.
const defaultNginxUser = "www-data"

// nginxWorkerUser returns the account nginx workers run as, taken from the
// `user` directive in /etc/nginx/nginx.conf. Third-party nginx builds may run
// as another account than www-data; permissions that only target www-data
// then silently lock nginx out of the document root ("File not found").
func nginxWorkerUser(ctx context.Context, exec executor.CommandExecutor) string {
	result, err := exec.RunSudo(ctx, "cat", "/etc/nginx/nginx.conf")
	if err != nil || result == nil || result.ExitCode != 0 {
		return defaultNginxUser
	}
	return parseNginxUserDirective(result.Stdout)
}

// parseNginxUserDirective extracts the account from the first active
// `user <name>[ <group>];` directive, falling back to www-data.
func parseNginxUserDirective(conf string) string {
	for _, line := range strings.Split(conf, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") || !strings.HasPrefix(line, "user ") {
			continue
		}
		value := strings.TrimSuffix(strings.TrimPrefix(line, "user "), ";")
		fields := strings.Fields(value)
		if len(fields) == 0 || !webUserRegex.MatchString(fields[0]) {
			continue
		}
		return fields[0]
	}
	return defaultNginxUser
}

// RestoreServingAccess re-applies the web server's filesystem access to a
// managed site. Git deploys recreate the project directory and chown it, so
// the group membership and default ACLs established during provisioning are
// lost; without this reconciliation nginx answers every request with
// "File not found". Safe to run repeatedly.
func (p *Provisioner) RestoreServingAccess(ctx context.Context, websiteID string) error {
	w, err := p.loadWebsite(ctx, websiteID)
	if err != nil {
		return fmt.Errorf("load website for serving access repair: %w", err)
	}
	if err := p.ensureServingPermissions(ctx, w); err != nil {
		return err
	}
	return p.grantNginxACLs(ctx, w)
}

// servingReconcileInterval is how often document roots are checked for files
// nginx cannot read.
const servingReconcileInterval = 5 * time.Minute

// StartServingReconciler periodically repairs document roots holding files
// nginx cannot read. Builds and checkouts run outside the panel (SSH, the
// web terminal) create files whose modes come from the parent directory's
// default ACL; a document root recreated by `git clone` inherits the
// project directory's default ACL, whose other entry is ---, so every
// public/build asset ends up 0640 and Laravel answers the asset URLs with
// an HTML page. Checking is a single find per site; the full repair only
// runs when something is unreadable.
func (p *Provisioner) StartServingReconciler(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(servingReconcileInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				p.ReconcileServingAccess(ctx)
			}
		}
	}()
}

// ReconcileServingAccess re-applies serving access to every active site
// whose document root contains something nginx cannot read.
func (p *Provisioner) ReconcileServingAccess(ctx context.Context) {
	rows, err := p.db.QueryContext(ctx,
		`SELECT id, domain, document_root, web_user FROM websites WHERE status IN ('active', 'suspended') ORDER BY created_at ASC`)
	if err != nil {
		slog.Warn("serving access reconcile: list websites", "error", err)
		return
	}
	var websites []websiteRow
	for rows.Next() {
		var w websiteRow
		if err := rows.Scan(&w.ID, &w.Domain, &w.DocumentRoot, &w.WebUser); err != nil {
			rows.Close()
			slog.Warn("serving access reconcile: scan website", "error", err)
			return
		}
		websites = append(websites, w)
	}
	rows.Close()
	for _, w := range websites {
		if ctx.Err() != nil {
			return
		}
		path := p.unservablePath(ctx, w)
		if path == "" {
			continue
		}
		if err := p.ensureServingPermissions(ctx, w); err != nil {
			slog.Warn("serving access reconcile failed", "domain", w.Domain, "path", path, "error", err)
			continue
		}
		slog.Info("serving access repaired", "domain", w.Domain, "unreadable", path)
	}
}

// unservablePath returns the first file or directory under a managed
// document root that nginx cannot read through the other class, or "" when
// everything is servable (or the root does not exist yet). Symlinks are not
// followed; public/storage is covered by grantNginxACLs.
func (p *Provisioner) unservablePath(ctx context.Context, w websiteRow) string {
	root := filepath.Clean(w.DocumentRoot)
	if !webUserRegex.MatchString(w.WebUser) || !pathStrictlyBelow(filepath.Join("/home", w.WebUser), root) {
		return ""
	}
	result, err := p.exec.RunSudo(ctx, "find", root, "-xdev", "(",
		"(", "-type", "d", "!", "-perm", "-o=rx", ")", "-o",
		"(", "-type", "f", "!", "-perm", "-o=r", ")",
		")", "-print", "-quit")
	if err != nil || result == nil {
		return ""
	}
	return strings.TrimSpace(result.Stdout)
}

// grantNginxACLs gives the nginx worker account named-user ACLs on the
// managed directories. Group permissions (www-data) can be lost to later
// chowns or ACL masks, so the worker account gets its own entries: traverse
// only on the boundaries and storage parents (never read — the project and
// .env stay private), read/execute on the document root and Laravel's
// public storage. Missing directories (config-only sites) are skipped so the
// grant can run before the first deploy.
func (p *Provisioner) grantNginxACLs(ctx context.Context, w websiteRow) error {
	if !webUserRegex.MatchString(w.WebUser) || !knownWebUser(w.WebUser, w.Domain) {
		return fmt.Errorf("unsafe stored web user %q", w.WebUser)
	}
	homeDir := filepath.Join("/home", w.WebUser)
	appRoot := filepath.Join(homeDir, "app")
	documentRoot := filepath.Clean(w.DocumentRoot)

	var boundaries, traverse, readable []string
	switch documentRoot {
	case filepath.Join(homeDir, "public"):
		boundaries = []string{homeDir}
		readable = []string{documentRoot}
	case filepath.Join(appRoot, "public"):
		boundaries = []string{homeDir, appRoot}
		// Laravel serves uploaded files through the public/storage symlink;
		// nginx needs traverse on the parents and read on storage/app/public.
		storage := filepath.Join(appRoot, "storage")
		traverse = []string{storage, filepath.Join(storage, "app")}
		readable = []string{documentRoot, filepath.Join(storage, "app", "public")}
	default:
		// Custom document roots are served just as much as the standard
		// layouts — as long as they live inside the managed home. Traverse
		// on every intermediate directory, read on the document root itself,
		// and follow a Laravel public/storage symlink so uploads stay
		// servable.
		if !pathStrictlyBelow(homeDir, documentRoot) {
			return nil
		}
		boundaries, traverse, readable = servingLayout(homeDir, documentRoot)
		if target, ok := p.symlinkTarget(ctx, filepath.Join(documentRoot, "storage")); ok {
			sBoundaries, sTraverse, sReadable := servingLayout(homeDir, target)
			boundaries = append(boundaries, sBoundaries...)
			traverse = append(traverse, sTraverse...)
			readable = append(readable, sReadable...)
		}
		boundaries = dedupePaths(boundaries)
		traverse = dedupePaths(traverse)
		readable = dedupePaths(readable)
	}

	result, err := p.exec.Run(ctx, "setfacl", "--version")
	if err != nil || result == nil || result.ExitCode != 0 {
		// Without ACL tooling, fall back to other-class permissions: the
		// document root is public content anyway, the boundaries stay
		// traverse-only so the project and .env remain unreadable.
		for _, dir := range p.existingDirs(ctx, append(append([]string{}, boundaries...), traverse...)) {
			if _, err := p.exec.RunSudo(ctx, "chmod", "o+x", "--", dir); err != nil {
				return fmt.Errorf("chmod o+x %s: %w", dir, err)
			}
		}
		for _, dir := range p.existingDirs(ctx, readable) {
			if _, err := p.exec.RunSudo(ctx, "chmod", "-R", "o+rX", "--", dir); err != nil {
				return fmt.Errorf("chmod -R o+rX %s: %w", dir, err)
			}
		}
		return nil
	}

	user := nginxWorkerUser(ctx, p.exec)
	for _, dir := range p.existingDirs(ctx, boundaries) {
		if result, err := p.exec.RunSudo(ctx, "setfacl", "-m", "u:"+user+":x", "--", dir); err != nil {
			return fmt.Errorf("setfacl boundary %s: %w", dir, err)
		} else if result.ExitCode != 0 {
			return fmt.Errorf("setfacl boundary %s: %s", dir, strings.TrimSpace(result.Stderr))
		}
	}
	for _, dir := range p.existingDirs(ctx, traverse) {
		if result, err := p.exec.RunSudo(ctx, "setfacl", "-m", "u:"+user+":x", "--", dir); err != nil {
			return fmt.Errorf("setfacl traverse %s: %w", dir, err)
		} else if result.ExitCode != 0 {
			return fmt.Errorf("setfacl traverse %s: %s", dir, strings.TrimSpace(result.Stderr))
		}
	}
	for _, dir := range p.existingDirs(ctx, readable) {
		for _, mode := range []string{"-R", "-R -d"} {
			args := append(strings.Fields(mode), "-m", "u:"+user+":rX", "--", dir)
			if result, err := p.exec.RunSudo(ctx, "setfacl", args...); err != nil {
				return fmt.Errorf("setfacl readable %s: %w", dir, err)
			} else if result.ExitCode != 0 {
				return fmt.Errorf("setfacl readable %s: %s", dir, strings.TrimSpace(result.Stderr))
			}
		}
	}
	return nil
}

// existingDirs filters paths to the ones that currently exist as directories.
func (p *Provisioner) existingDirs(ctx context.Context, paths []string) []string {
	var found []string
	for _, path := range paths {
		result, err := p.exec.RunSudo(ctx, "test", "-d", path)
		if err != nil || result == nil || result.ExitCode != 0 {
			continue
		}
		found = append(found, path)
	}
	return found
}

// pathStrictlyBelow reports whether target lies strictly below root (not
// equal, not outside). Used to keep serve grants inside the managed home.
func pathStrictlyBelow(root, target string) bool {
	rel, err := filepath.Rel(root, target)
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// servingLayout plans the serve grants for a path strictly below homeDir:
// traverse on homeDir and every intermediate directory, read on the final
// component. The .env and project sources stay private because intermediates
// only ever receive traverse (x), never read.
func servingLayout(homeDir, target string) (boundaries, traverse, readable []string) {
	if !pathStrictlyBelow(homeDir, target) {
		return nil, nil, nil
	}
	rel, _ := filepath.Rel(homeDir, target)
	current := homeDir
	boundaries = append(boundaries, homeDir)
	parts := strings.Split(rel, string(filepath.Separator))
	for i, part := range parts {
		current = filepath.Join(current, part)
		if i == len(parts)-1 {
			readable = append(readable, current)
		} else {
			traverse = append(traverse, current)
		}
	}
	return boundaries, traverse, readable
}

// symlinkTarget resolves a symlink to its absolute target. ok is false when
// the path is not a symlink or cannot be resolved.
func (p *Provisioner) symlinkTarget(ctx context.Context, path string) (string, bool) {
	if result, err := p.exec.RunSudo(ctx, "test", "-L", "--", path); err != nil || result == nil || result.ExitCode != 0 {
		return "", false
	}
	result, err := p.exec.RunSudo(ctx, "readlink", "-f", "--", path)
	if err != nil || result == nil || result.ExitCode != 0 {
		return "", false
	}
	target := strings.TrimSpace(result.Stdout)
	if !strings.HasPrefix(target, "/") {
		return "", false
	}
	return target, true
}

// dedupePaths removes duplicate paths while preserving order.
func dedupePaths(paths []string) []string {
	seen := make(map[string]bool, len(paths))
	out := paths[:0]
	for _, path := range paths {
		if seen[path] {
			continue
		}
		seen[path] = true
		out = append(out, path)
	}
	return out
}
