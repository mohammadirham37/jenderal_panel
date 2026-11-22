package releaseupgrade

import (
	"context"
	"fmt"
	"path"
	"strconv"
	"strings"
)

// ToPostgresMajor is the PostgreSQL major version resolute ships. Older
// majors are not in its archive: without migration the panel adds
// apt.postgresql.org (pgdg) after the upgrade so they keep receiving
// updates; with migration their clusters are moved to this version.
const ToPostgresMajor = "18"

// PostgresMigration describes the optional migration of clusters older
// than ToPostgresMajor. Possible is false when there is not enough free
// disk for the copy and the backup (Reason says why).
type PostgresMigration struct {
	Clusters  []string `json:"clusters"` // e.g. "16/main"
	Target    string   `json:"target"`
	DataBytes int64    `json:"data_bytes"`
	FreeBytes int64    `json:"free_bytes"`
	Possible  bool     `json:"possible"`
	Reason    string   `json:"reason,omitempty"`
}

// pgCluster is one line of pg_lsclusters -h.
type pgCluster struct {
	Version string
	Name    string
	DataDir string
}

// parseClusters reads pg_lsclusters -h output (Ver Cluster Port Status
// Owner Datadir Logfile).
func parseClusters(out string) []pgCluster {
	var clusters []pgCluster
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 6 {
			continue
		}
		clusters = append(clusters, pgCluster{Version: f[0], Name: f[1], DataDir: f[5]})
	}
	return clusters
}

// pgMajor returns the major number of a cluster version ("16" -> 16,
// "9.6" -> 9), or -1 when it is not a version.
func pgMajor(v string) int {
	major, _, _ := strings.Cut(v, ".")
	n, err := strconv.Atoi(major)
	if err != nil || n < 0 {
		return -1
	}
	return n
}

// migrationHeadroom is added to the copy and the backup when checking free
// space.
const migrationHeadroom = 1 << 30

// checkPostgreSQL reports clusters older than resolute's PostgreSQL and
// whether migrating them fits on disk: pg_upgradecluster -m upgrade copies
// the data directory and the panel writes a pg_dumpall backup first, so
// twice the data size plus headroom must be free.
func (s *Service) checkPostgreSQL(ctx context.Context, p *Preflight) {
	res, err := s.exec.Run(ctx, "pg_lsclusters", "-h")
	if err != nil || res == nil || res.ExitCode != 0 {
		return // PostgreSQL is not installed
	}
	target, _ := strconv.Atoi(ToPostgresMajor)
	var old []pgCluster
	for _, c := range parseClusters(res.Stdout) {
		if m := pgMajor(c.Version); m >= 0 && m < target {
			old = append(old, c)
		}
	}
	if len(old) == 0 {
		p.add("postgresql", CheckOK, "No PostgreSQL cluster needs migrating.")
		return
	}

	mig := &PostgresMigration{Target: ToPostgresMajor}
	var versions []string
	for _, c := range old {
		mig.Clusters = append(mig.Clusters, c.Version+"/"+c.Name)
		if !contains(versions, c.Version) {
			versions = append(versions, c.Version)
		}
	}
	p.PostgreSQL = mig
	p.add("postgresql", CheckWarn, fmt.Sprintf(
		"PostgreSQL %s (%s) is not part of Ubuntu %s. The panel adds apt.postgresql.org so it keeps receiving updates, or tick the option to migrate it to PostgreSQL %s.",
		strings.Join(versions, ", "), strings.Join(mig.Clusters, ", "), ToVersion, ToPostgresMajor))

	for _, c := range old {
		size, ok := s.dirSize(ctx, c.DataDir)
		if !ok {
			mig.Reason = "Could not measure " + c.DataDir + "."
			return
		}
		mig.DataBytes += size
	}
	free, ok := s.freeBytes(ctx, path.Dir(old[0].DataDir))
	if !ok {
		mig.Reason = "Could not read the free disk space for PostgreSQL."
		return
	}
	mig.FreeBytes = free
	need := 2*mig.DataBytes + migrationHeadroom
	if free < need {
		mig.Reason = fmt.Sprintf("Migrating needs %s free (a copy of %s of data plus a backup); %s is free.",
			humanBytes(need), humanBytes(mig.DataBytes), humanBytes(free))
		return
	}
	mig.Possible = true
}

func (s *Service) dirSize(ctx context.Context, dir string) (int64, bool) {
	if !path.IsAbs(dir) || path.Clean(dir) != dir {
		return 0, false
	}
	res, err := s.exec.RunSudo(ctx, "du", "-sb", dir)
	if err != nil || res == nil || res.ExitCode != 0 {
		return 0, false
	}
	f := strings.Fields(res.Stdout)
	if len(f) == 0 {
		return 0, false
	}
	n, err := strconv.ParseInt(f[0], 10, 64)
	return n, err == nil
}

func (s *Service) freeBytes(ctx context.Context, dir string) (int64, bool) {
	res, err := s.exec.Run(ctx, "df", "-B1", "--output=avail", dir)
	if err != nil || res == nil || res.ExitCode != 0 {
		return 0, false
	}
	for _, line := range strings.Split(res.Stdout, "\n") {
		if n, err := strconv.ParseInt(strings.TrimSpace(line), 10, 64); err == nil {
			return n, true
		}
	}
	return 0, false
}
