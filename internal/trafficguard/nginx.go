package trafficguard

import (
	"context"
	"errors"
	"fmt"
	"github.com/mohammadirham37/jenderal_panel/internal/executor"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
	"github.com/oklog/ulid/v2"
	"net/netip"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const trafficBasePath = "/etc/nginx/conf.d/jenderal-traffic-zones.conf"

var trafficWebsiteIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

type ManagedFiles interface {
	Read(context.Context, string) (string, bool, error)
	Write(context.Context, string, string) error
	Remove(context.Context, string) error
}
type sudoFiles struct{ exec executor.CommandExecutor }

func (f *sudoFiles) Read(ctx context.Context, path string) (string, bool, error) {
	r, e := f.exec.RunSudo(ctx, "/usr/bin/test", "-f", path)
	if e != nil {
		return "", false, e
	}
	if r.ExitCode == 1 {
		return "", false, nil
	}
	r, e = f.exec.RunSudo(ctx, "/usr/bin/cat", "--", path)
	if e != nil || r.ExitCode != 0 {
		return "", false, errors.New("read Nginx managed file failed")
	}
	return r.Stdout, true, nil
}
func (f *sudoFiles) Write(ctx context.Context, path, content string) error {
	tmp := filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+"."+ulid.Make().String()+".tmp")
	r, e := f.exec.RunSudoWithInput(ctx, content, "/usr/bin/tee", "--", tmp)
	if e != nil || r.ExitCode != 0 {
		return errors.New("stage Nginx managed file failed")
	}
	defer f.exec.RunSudo(context.Background(), "/usr/bin/rm", "-f", "--", tmp)
	for _, cmd := range [][]string{{"/usr/bin/chmod", "0644", "--", tmp}, {"/usr/bin/mv", "--", tmp, path}} {
		r, e = f.exec.RunSudo(ctx, cmd[0], cmd[1:]...)
		if e != nil || r.ExitCode != 0 {
			return errors.New("promote Nginx managed file failed")
		}
	}
	return nil
}
func (f *sudoFiles) Remove(ctx context.Context, path string) error {
	r, e := f.exec.RunSudo(ctx, "/usr/bin/rm", "-f", "--", path)
	if e != nil || r.ExitCode != 0 {
		return errors.New("remove Nginx managed file failed")
	}
	return nil
}

type NginxManager struct {
	exec  executor.CommandExecutor
	files ManagedFiles
	repo  *Repository
	now   func() time.Time
}

func NewNginxManager(exec executor.CommandExecutor, files ManagedFiles, repo *Repository) *NginxManager {
	if files == nil {
		files = &sudoFiles{exec}
	}
	return &NginxManager{exec: exec, files: files, repo: repo, now: func() time.Time { return time.Now().UTC() }}
}
func baseConfig() string {
	return "# Managed by Jenderal Panel\nlimit_req_zone $binary_remote_addr zone=jenderal_balanced:10m rate=10r/s;\nlimit_req_zone $binary_remote_addr zone=jenderal_strict:10m rate=5r/s;\nlimit_conn_zone $binary_remote_addr zone=jenderal_connections:10m;\n"
}
func (m *NginxManager) EnsureBase(ctx context.Context) error {
	r, err := m.exec.RunSudo(ctx, "/usr/bin/install", "-d", "-m", "0755", "/etc/nginx/jenderal/security/sites")
	if err != nil || r.ExitCode != 0 {
		return errors.New("create Nginx Traffic Guard directory failed")
	}
	return m.applyFile(ctx, trafficBasePath, baseConfig())
}
func (m *NginxManager) ApplyWebsite(ctx context.Context, w model.Website, p WebsiteProfile, confirm bool) error {
	if !trafficWebsiteIDPattern.MatchString(w.ID) {
		return model.NewValidationError("invalid website ID")
	}
	p.WebsiteID = w.ID
	if p.CreatedAt.IsZero() {
		p.CreatedAt = m.now()
	}
	p.UpdatedAt = m.now()
	if p.ObserveStartedAt.IsZero() {
		p.ObserveStartedAt = m.now()
	}
	if err := validateProfile(p, confirm, m.now()); err != nil {
		return err
	}
	var cf []netip.Prefix
	if p.ProxyMode == "cloudflare" {
		snap, err := m.repo.Snapshot(ctx)
		if err != nil {
			return err
		}
		cf, err = SnapshotPrefixes(snap)
		if err != nil {
			return err
		}
	}
	realIP, err := RenderRealIP(p, cf...)
	if err != nil {
		return err
	}
	if err = m.EnsureBase(ctx); err != nil {
		return err
	}
	zonePath, previousZone, zoneExisted := "", "", false
	if p.Mode == "custom" {
		zonePath = "/etc/nginx/conf.d/jenderal-traffic-zone-" + w.ID + ".conf"
		previousZone, zoneExisted, err = m.files.Read(ctx, zonePath)
		if err != nil {
			return err
		}
		zoneConfig := fmt.Sprintf("# Managed by Jenderal Panel\nlimit_req_zone $binary_remote_addr zone=%s:10m rate=%dr/s;\n", customZoneName(w.ID), p.RequestsPerSecond)
		if err = m.applyFile(ctx, zonePath, zoneConfig); err != nil {
			return err
		}
	}
	snippet := renderTrafficSnippet(p, realIP)
	path := "/etc/nginx/jenderal/security/sites/" + w.ID + ".conf"
	if err = m.applyFile(ctx, path, snippet); err != nil {
		if zonePath != "" {
			if rollbackErr := m.restoreFile(context.Background(), zonePath, previousZone, zoneExisted); rollbackErr != nil {
				return fmt.Errorf("%v; custom rate zone rollback failed: %w", err, rollbackErr)
			}
		}
		return err
	}
	return m.repo.SaveProfile(ctx, p)
}
func validateProfile(p WebsiteProfile, confirm bool, now time.Time) error {
	if p.Mode != "observe" && p.Mode != "balanced" && p.Mode != "strict" && p.Mode != "custom" {
		return model.NewValidationError("invalid Traffic Guard mode")
	}
	if p.RequestsPerSecond < 1 || p.RequestsPerSecond > 1000 || p.Burst < 1 || p.Burst > 5000 || p.Connections < 1 || p.Connections > 1000 {
		return model.NewValidationError("Traffic Guard limits are outside safe bounds")
	}
	if p.Mode != "observe" {
		if !confirm {
			return model.NewValidationError("HTTP enforcement requires explicit confirmation")
		}
		if now.Before(p.ObserveStartedAt.Add(24 * time.Hour)) {
			return model.NewValidationError("Traffic Guard requires 24 hours in Observe Mode before enforcement")
		}
	}
	return ValidateProxy(p)
}
func renderTrafficSnippet(p WebsiteProfile, realIP string) string {
	zone := "jenderal_balanced"
	connections := 20
	burst := 20
	if p.Mode == "strict" {
		zone = "jenderal_strict"
		connections = 10
		burst = 10
	}
	if p.Mode == "custom" {
		zone = customZoneName(p.WebsiteID)
		connections = p.Connections
		burst = p.Burst
	}
	var b strings.Builder
	b.WriteString("# Managed by Jenderal Panel - HTTP-layer protection only\n")
	b.WriteString(realIP)
	fmt.Fprintf(&b, "limit_req zone=%s burst=%d nodelay;\nlimit_conn jenderal_connections %d;\nlimit_req_status 429;\nlimit_conn_status 429;\n", zone, burst, connections)
	if p.Mode == "observe" {
		b.WriteString("limit_req_dry_run on;\nlimit_conn_dry_run on;\n")
	}
	return b.String()
}

func customZoneName(websiteID string) string { return "jenderal_custom_" + websiteID }

func (m *NginxManager) restoreFile(ctx context.Context, path, previous string, existed bool) error {
	var err error
	if existed {
		err = m.files.Write(ctx, path, previous)
	} else {
		err = m.files.Remove(ctx, path)
	}
	if err != nil {
		return err
	}
	return m.validate(ctx)
}

func (m *NginxManager) applyFile(ctx context.Context, path, content string) error {
	previous, existed, err := m.files.Read(ctx, path)
	if err != nil {
		return err
	}
	if existed && previous == content {
		return nil
	}
	if err = m.files.Write(ctx, path, content); err != nil {
		return err
	}
	if err = m.validate(ctx); err == nil {
		return nil
	}
	if existed {
		_ = m.files.Write(context.Background(), path, previous)
	} else {
		_ = m.files.Remove(context.Background(), path)
	}
	_, _ = m.exec.RunSudo(context.Background(), "systemctl", "reload", "nginx")
	return err
}

func (m *NginxManager) validate(ctx context.Context) error {
	r, e := m.exec.RunSudo(ctx, "/usr/sbin/nginx", "-t")
	if e != nil || r.ExitCode != 0 {
		return errors.New("Nginx configuration test failed")
	}
	r, e = m.exec.RunSudo(ctx, "systemctl", "reload", "nginx")
	if e != nil || r.ExitCode != 0 {
		return errors.New("Nginx reload failed")
	}
	r, e = m.exec.RunSudo(ctx, "systemctl", "is-active", "--quiet", "nginx")
	if e != nil || r.ExitCode != 0 {
		return errors.New("Nginx health confirmation failed")
	}
	return nil
}
