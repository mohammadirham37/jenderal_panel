package releaseupgrade

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

// Cleanup lists what can be removed once the server runs the new release
// and has been rebooted: PostgreSQL clusters kept stopped as a fallback
// after their migration to ToPostgresMajor, and packages apt no longer
// needs (the old release's kernels and libraries).
type Cleanup struct {
	Clusters   []OldCluster `json:"clusters"`
	Autoremove []string     `json:"autoremove"`
}

// OldCluster is a stopped cluster whose replacement runs on
// ToPostgresMajor under the same name.
type OldCluster struct {
	ID        string `json:"id"` // "16/main"
	Version   string `json:"version"`
	Name      string `json:"name"`
	DataDir   string `json:"data_dir"`
	DataBytes int64  `json:"data_bytes"`
}

// CleanupRequest asks for one cleanup action.
type CleanupRequest struct {
	Action  string `json:"action"`  // "postgresql" or "autoremove"
	Cluster string `json:"cluster"` // for "postgresql": "16/main"
	Confirm string `json:"confirm"` // must repeat Cluster
}

// Cleanup actions.
const (
	CleanupPostgreSQL = "postgresql"
	CleanupAutoremove = "autoremove"
)

var clusterNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)

// Cleanup reports the cleanup candidates. It is empty unless the server
// runs the new release, no upgrade is pending and the reboot is done.
func (s *Service) Cleanup(ctx context.Context) (*Cleanup, error) {
	c := &Cleanup{Clusters: []OldCluster{}, Autoremove: []string{}}
	if s.codename(ctx) != ToCodename {
		return c, nil
	}
	st, err := s.Status(ctx)
	if err != nil {
		return nil, err
	}
	if st.Running || st.State != StateIdle {
		return c, nil
	}
	c.Clusters = s.oldClusters(ctx)
	c.Autoremove = s.autoremovable(ctx)
	return c, nil
}

// oldClusters finds stopped clusters older than ToPostgresMajor whose name
// has an online ToPostgresMajor cluster, i.e. what a migration left behind.
func (s *Service) oldClusters(ctx context.Context) []OldCluster {
	res, err := s.exec.Run(ctx, "pg_lsclusters", "-h")
	if err != nil || res == nil || res.ExitCode != 0 {
		return []OldCluster{}
	}
	target, _ := strconv.Atoi(ToPostgresMajor)
	lines := strings.Split(res.Stdout, "\n")
	migrated := map[string]bool{}
	for _, line := range lines {
		f := strings.Fields(line)
		if len(f) >= 4 && f[0] == ToPostgresMajor && strings.HasPrefix(f[3], "online") {
			migrated[f[1]] = true
		}
	}
	out := []OldCluster{}
	for _, line := range lines {
		f := strings.Fields(line)
		if len(f) < 6 {
			continue
		}
		major := pgMajor(f[0])
		if major < 0 || major >= target || f[3] != "down" || !migrated[f[1]] || !clusterNamePattern.MatchString(f[1]) {
			continue
		}
		c := OldCluster{ID: f[0] + "/" + f[1], Version: f[0], Name: f[1], DataDir: f[5]}
		c.DataBytes, _ = s.dirSize(ctx, c.DataDir)
		out = append(out, c)
	}
	return out
}

// autoremovable lists the packages apt-get autoremove would remove.
func (s *Service) autoremovable(ctx context.Context) []string {
	res, err := s.exec.RunSudo(ctx, "apt-get", "-s", "-o", "Debug::NoLocking=1", "autoremove")
	if err != nil || res == nil || res.ExitCode != 0 {
		return []string{}
	}
	return parseRemovals(res.Stdout)
}

// parseRemovals reads "Remv <package> [<version>]" lines of an apt-get -s run.
func parseRemovals(out string) []string {
	pkgs := []string{}
	for _, line := range strings.Split(out, "\n") {
		if rest, ok := strings.CutPrefix(line, "Remv "); ok {
			if f := strings.Fields(rest); len(f) > 0 {
				pkgs = append(pkgs, f[0])
			}
		}
	}
	sort.Strings(pkgs)
	return pkgs
}

// RunCleanup starts one cleanup action as a task and returns its ID.
func (s *Service) RunCleanup(ctx context.Context, req CleanupRequest) (string, error) {
	if s.runTask == nil {
		return "", fmt.Errorf("task runner unavailable")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	c, err := s.Cleanup(ctx)
	if err != nil {
		return "", err
	}
	switch req.Action {
	case CleanupAutoremove:
		if len(c.Autoremove) == 0 {
			return "", model.NewValidationError("there are no unused packages to remove")
		}
		return s.runTask("Remove packages no longer needed after the Ubuntu upgrade", [][]string{
			{"env", "DEBIAN_FRONTEND=noninteractive", "apt-get", "-o", "DPkg::Lock::Timeout=120", "autoremove", "-y"},
		}), nil
	case CleanupPostgreSQL:
		var cluster *OldCluster
		for i := range c.Clusters {
			if c.Clusters[i].ID == req.Cluster {
				cluster = &c.Clusters[i]
			}
		}
		if cluster == nil {
			return "", model.NewValidationError("only a stopped PostgreSQL cluster that was migrated to " + ToPostgresMajor + " can be removed")
		}
		if strings.TrimSpace(req.Confirm) != cluster.ID {
			return "", model.NewValidationError("type " + cluster.ID + " to confirm")
		}
		commands := [][]string{{"pg_dropcluster", cluster.Version, cluster.Name}}
		if pkgs := s.versionPackagesToRemove(ctx, cluster); len(pkgs) > 0 {
			commands = append(commands, append([]string{
				"env", "DEBIAN_FRONTEND=noninteractive", "apt-get", "-o", "DPkg::Lock::Timeout=120", "remove", "-y",
			}, pkgs...))
		}
		return s.runTask("Remove PostgreSQL "+cluster.ID+" kept after the migration to "+ToPostgresMajor, commands), nil
	}
	return "", model.NewValidationError("unknown cleanup action")
}

// versionPackagesToRemove returns the installed packages of the cluster's
// PostgreSQL version (postgresql-16, postgresql-16-pgvector,
// postgresql-client-16, ...) when no other cluster uses that version.
func (s *Service) versionPackagesToRemove(ctx context.Context, cluster *OldCluster) []string {
	res, err := s.exec.Run(ctx, "pg_lsclusters", "-h")
	if err != nil || res == nil || res.ExitCode != 0 {
		return nil
	}
	for _, c := range parseClusters(res.Stdout) {
		if c.Version == cluster.Version && c.Name != cluster.Name {
			return nil
		}
	}
	list, err := s.exec.Run(ctx, "dpkg-query", "-W", "-f", "${Package}\t${db:Status-Status}\n", "postgresql*")
	if err != nil || list == nil || list.ExitCode != 0 {
		return nil
	}
	return versionPackages(list.Stdout, cluster.Version)
}

// versionPackages picks installed packages that belong to one PostgreSQL
// version from dpkg-query "name<TAB>status" lines.
func versionPackages(out, version string) []string {
	pattern := regexp.MustCompile(`^postgresql-(?:[a-z0-9]+-)*` + regexp.QuoteMeta(version) + `(?:-[a-z0-9][a-z0-9.+-]*)?$`)
	var pkgs []string
	for _, line := range strings.Split(out, "\n") {
		name, status, ok := strings.Cut(line, "\t")
		if ok && status == "installed" && pattern.MatchString(name) {
			pkgs = append(pkgs, name)
		}
	}
	sort.Strings(pkgs)
	return pkgs
}
