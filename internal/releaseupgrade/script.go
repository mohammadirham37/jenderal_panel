package releaseupgrade

import (
	"strings"

	"github.com/mohammadirham37/jenderal_panel/internal/dbmanager"
	"github.com/mohammadirham37/jenderal_panel/internal/hardening"
	"github.com/mohammadirham37/jenderal_panel/internal/php"
)

const (
	// UnitName is the transient systemd unit running the upgrade, so it
	// survives a panel restart or a closed browser.
	UnitName = "jenderal-release-upgrade"
	// StateDir holds the state file, inputs written by the panel and the
	// package list recorded before the upgrade. It is world readable so the
	// panel can read the state without sudo, which breaks for a while when
	// the upgrade replaces sudo with sudo-rs.
	StateDir = "/var/lib/jenderal/release-upgrade"
	// ScriptPath is the upgrade script the unit runs.
	ScriptPath = StateDir + "/upgrade.sh"
	// LogPath is the upgrade's combined output.
	LogPath = "/var/log/jenderal/release-upgrade.log"
	// OverridePath is read by do-release-upgrade after its bundled config.
	OverridePath = "/etc/update-manager/release-upgrades.d/jenderal-panel.cfg"
	// AptConfPath keeps locally edited conffiles during the upgrade.
	AptConfPath = "/etc/apt/apt.conf.d/99jenderal-release-upgrade"
)

// States written to StateDir/state.
const (
	StateIdle           = "idle"
	StateRunning        = "running"
	StateRebootRequired = "reboot_required"
	StateFailed         = "failed"
)

// OverrideConfig changes three upgrader defaults for the duration of the
// upgrade (verified against the resolute dist-upgrader):
//
//   - RemoveObsoletes/PurgeObsoletes: the non-interactive frontend answers
//     yes to everything, and by default packages that stop being
//     downloadable are removed and purged — PHP from sury (pool configs
//     included) or PostgreSQL from pgdg (clusters included). Never remove.
//   - AllowThirdParty: keep provider mirrors (e.g. mirrors.tencentyun.com)
//     and third-party repositories, moving them to the new release instead
//     of disabling them.
//   - Pockets + pgdg: map noble-pgdg to resolute-pgdg like Ubuntu's pockets.
const OverrideConfig = `# Written by Jenderal Panel for the release upgrade; removed afterwards.
[Distro]
RemoveObsoletes=no
PurgeObsoletes=no

[Sources]
AllowThirdParty=yes
Pockets=security,updates,proposed,backports,pgdg
`

// AptConfig keeps edited conffiles (nginx, php.ini, pools) when packages
// ship new defaults.
const AptConfig = `// Written by Jenderal Panel for the release upgrade; removed afterwards.
Dpkg::Options {
  "--force-confdef";
  "--force-confold";
};
`

// UpgradeScript is the root script run in the transient unit. It contains
// no user input: the panel writes its inputs (site PHP versions, whether
// MySQL needs mysql_native_password) to files in StateDir beforehand.
func UpgradeScript() string {
	return strings.NewReplacer(
		"@STATE_DIR@", StateDir,
		"@LOG@", LogPath,
		"@OVERRIDE@", OverridePath,
		"@APT_CONF@", AptConfPath,
		"@OVERRIDE_CONFIG@", OverrideConfig,
		"@APT_CONFIG@", AptConfig,
		"@PHP_REPO_SCRIPT@", php.RepositorySetupScript(),
		"@PGDG_REPO_SCRIPT@", "set -eu\n"+dbmanager.PGDGRepositorySetupScript(),
		"@PGDG_PIN_SCRIPT@", dbmanager.PGDGMetaPinScript(),
		"@TO@", ToCodename,
		"@PG_TARGET@", ToPostgresMajor,
		"@NGINX_TOKENS@", hardening.NginxTokensPath,
	).Replace(upgradeScriptTemplate)
}

const upgradeScriptTemplate = `set -uo pipefail
export PATH=/usr/sbin:/usr/bin:/sbin:/bin
export DEBIAN_FRONTEND=noninteractive UCF_FORCE_CONFFOLD=1
STATE_DIR=@STATE_DIR@
LOG=@LOG@
OVERRIDE=@OVERRIDE@
APT_CONF=@APT_CONF@
APT_OPTS="-y -o DPkg::Lock::Timeout=300 -o Dpkg::Options::=--force-confdef -o Dpkg::Options::=--force-confold"

install -d -m 0755 "$STATE_DIR"
chmod 0755 "$STATE_DIR"
install -d -m 0755 "$(dirname "$LOG")" /etc/update-manager/release-upgrades.d
exec >>"$LOG" 2>&1

set_state() {
    printf '%s\n' "$1" > "$STATE_DIR/state.tmp" && mv -f "$STATE_DIR/state.tmp" "$STATE_DIR/state"
}
warn() {
    echo "WARNING: $*"
    printf '%s\n' "$*" >> "$STATE_DIR/warnings"
}
codename() {
    ( . /etc/os-release && printf '%s' "${VERSION_CODENAME:-}" )
}
finish() {
    status=$?
    rm -f "$OVERRIDE" "$APT_CONF"
    if [ "$status" -eq 0 ] && [ "$(codename)" = @TO@ ]; then
        cat /proc/sys/kernel/random/boot_id > "$STATE_DIR/boot_id" 2>/dev/null || true
        set_state reboot_required
        echo ">>> $(date -u '+%F %T') Upgrade finished. Reboot the server to start Ubuntu 26.04's kernel."
    else
        [ "$status" -ne 0 ] || status=1
        printf '%s\n' "$status" > "$STATE_DIR/exit_code"
        set_state failed
        echo ">>> $(date -u '+%F %T') Upgrade failed (exit $status). Details: /var/log/dist-upgrade/main.log"
    fi
}

rm -f "$STATE_DIR/warnings" "$STATE_DIR/exit_code" "$STATE_DIR/boot_id"
set_state running
trap finish EXIT
echo ">>> $(date -u '+%F %T') Ubuntu release upgrade to @TO@ started"

# 1. Remember what the panel manages, to restore anything that goes missing.
dpkg-query -W -f='${Package}\t${db:Status-Status}\n' 'php*' 'postgresql*' 2>/dev/null \
    | awk -F'\t' '$2 == "installed" { print $1 }' > "$STATE_DIR/packages-before.txt" || true
echo ">>> Recorded $(wc -l < "$STATE_DIR/packages-before.txt") PHP/PostgreSQL packages"

# 2. Upgrader settings: never remove obsolete packages, keep third-party
#    sources (moved to @TO@), map noble-pgdg, keep edited conffiles.
cat > "$OVERRIDE" <<'JENDERAL_OVERRIDE'
@OVERRIDE_CONFIG@JENDERAL_OVERRIDE
cat > "$APT_CONF" <<'JENDERAL_APT_CONF'
@APT_CONFIG@JENDERAL_APT_CONF
if [ -f /etc/update-manager/release-upgrades ]; then
    sed 's/^Prompt=.*/Prompt=lts/' /etc/update-manager/release-upgrades > /etc/update-manager/release-upgrades.jenderal \
        && cat /etc/update-manager/release-upgrades.jenderal > /etc/update-manager/release-upgrades
    rm -f /etc/update-manager/release-upgrades.jenderal
else
    printf '[DEFAULT]\nPrompt=lts\n' > /etc/update-manager/release-upgrades
fi

# 3. The upgrade itself (Canonical's supported tool).
echo ">>> Running do-release-upgrade (this takes 20-60 minutes)"
do-release-upgrade -f DistUpgradeViewNonInteractive
rc=$?
if [ "$rc" -ne 0 ]; then
    echo ">>> do-release-upgrade exited with $rc"
    exit "$rc"
fi
if [ "$(codename)" != @TO@ ]; then
    echo ">>> The system still reports '$(codename)'; the upgrade did not complete"
    exit 1
fi

# 4. Repair what the panel manages. Failures here are warnings: the system
#    is already on @TO@.
echo ">>> Repairing panel-managed packages and services"
if grep -qs '^php' "$STATE_DIR/packages-before.txt"; then
    cat > "$STATE_DIR/php-repo.sh" <<'JENDERAL_PHP_REPO'
@PHP_REPO_SCRIPT@JENDERAL_PHP_REPO
    bash "$STATE_DIR/php-repo.sh" || warn "could not configure packages.sury.org/php for @TO@"
fi
# pgdg is needed when it was configured before, or when a PostgreSQL major
# other than @TO@'s (@PG_TARGET@) is installed: @TO@'s archive does not
# carry it, so without pgdg it would never be updated again.
needs_pgdg=0
grep -rqs 'apt.postgresql.org' /etc/apt/sources.list /etc/apt/sources.list.d/ && needs_pgdg=1
grep -qsE '^postgresql-[0-9]+$' "$STATE_DIR/packages-before.txt" \
    && grep -sE '^postgresql-[0-9]+$' "$STATE_DIR/packages-before.txt" | grep -qvx 'postgresql-@PG_TARGET@' && needs_pgdg=1
if [ "$needs_pgdg" = 1 ] \
    && ! grep -rqsE '^[^#]*@TO@-pgdg' /etc/apt/sources.list /etc/apt/sources.list.d/*.list /etc/apt/sources.list.d/*.sources; then
    cat > "$STATE_DIR/pgdg-repo.sh" <<'JENDERAL_PGDG_REPO'
@PGDG_REPO_SCRIPT@JENDERAL_PGDG_REPO
    bash "$STATE_DIR/pgdg-repo.sh" || warn "could not configure the PostgreSQL (pgdg) repository for @TO@"
fi
# pgdg's meta packages follow the newest major (postgresql-19 next to
# @TO@'s 18); keep Ubuntu's. Also covers pgdg entries the upgrader moved.
@PGDG_PIN_SCRIPT@
# Backups the upgrader leaves of the panel's lists; apt warns about them.
rm -f /etc/apt/sources.list.d/pgdg.list.migrate /etc/apt/sources.list.d/pgdg.list.disabled
apt-get update || warn "apt-get update failed after the upgrade"

missing=""
present=""
while read -r pkg; do
    [ -n "$pkg" ] || continue
    if [ "$(dpkg-query -W -f='${db:Status-Status}' "$pkg" 2>/dev/null)" = installed ]; then
        present="$present $pkg"
    else
        if apt-cache policy "$pkg" 2>/dev/null | grep -q 'Candidate: [0-9]'; then
            missing="$missing $pkg"
        else
            warn "$pkg is no longer installed and not available for @TO@"
        fi
    fi
done < "$STATE_DIR/packages-before.txt"
if [ -n "$missing" ]; then
    echo ">>> Reinstalling:$missing"
    # shellcheck disable=SC2086
    apt-get install $APT_OPTS $missing || warn "could not reinstall:$missing"
fi
# With RemoveObsoletes=no, sury/pgdg packages stay on their noble builds
# (and php8.5 on Ubuntu's build) until upgraded from the re-pointed repos.
if [ -n "$present" ]; then
    echo ">>> Upgrading PHP/PostgreSQL packages to their @TO@ builds"
    # shellcheck disable=SC2086
    apt-get install --only-upgrade $APT_OPTS $present || warn "could not upgrade PHP/PostgreSQL packages to their @TO@ builds"
fi

if [ "$(cat "$STATE_DIR/mysql-native" 2>/dev/null)" = 1 ] && [ -d /etc/mysql/mysql.conf.d ]; then
    echo ">>> Enabling mysql_native_password for existing MySQL users (MySQL 8.4 disables it by default)"
    printf '[mysqld]\nmysql_native_password=ON\n' > /etc/mysql/mysql.conf.d/zz-jenderal-native-password.cnf
    systemctl restart mysql || warn "MySQL did not restart with mysql_native_password enabled"
fi

# Optional: move PostgreSQL clusters older than @PG_TARGET@ to @PG_TARGET@.
# pg_upgradecluster -m upgrade copies the data, so the old cluster stays
# intact; it is kept stopped (start.conf manual) as a fallback, and a
# pg_dumpall backup is written first. A failed migration restarts the old
# cluster unchanged.
pg_target=@PG_TARGET@
migrate_cluster() {
    ver=$1 name=$2 port=$3 status=$4
    label="PostgreSQL $ver/$name"
    if pg_lsclusters -h | awk -v v="$pg_target" -v n="$name" '$1 == v && $2 == n { found = 1 } END { exit !found }'; then
        warn "$label was not migrated: a $pg_target/$name cluster already exists"
        return
    fi
    case "$status" in
        online*) ;;
        *) warn "$label was not migrated: it is not running"; return ;;
    esac
    ext=""
    for pkg in $(dpkg-query -W -f='${Package}\t${db:Status-Status}\n' "postgresql-$ver-*" 2>/dev/null | awk -F'\t' '$2 == "installed" { print $1 }'); do
        new_pkg="postgresql-$pg_target-${pkg#postgresql-$ver-}"
        if apt-cache policy "$new_pkg" 2>/dev/null | grep -q 'Candidate: [0-9]'; then
            ext="$ext $new_pkg"
        else
            warn "$label was not migrated: its extension package $pkg has no $new_pkg"
            return
        fi
    done
    if [ -n "$ext" ]; then
        # shellcheck disable=SC2086
        apt-get install $APT_OPTS $ext || { warn "$label was not migrated: could not install$ext"; return; }
    fi
    install -d -m 0700 /var/backups/jenderal
    dump="/var/backups/jenderal/postgresql-$ver-$name-$(date -u +%Y%m%d%H%M%S).sql.gz"
    echo ">>> Backing up $label to $dump"
    if ! runuser -u postgres -- pg_dumpall -p "$port" | gzip > "$dump"; then
        rm -f "$dump"
        warn "$label was not migrated: the backup failed"
        return
    fi
    chmod 0600 "$dump"
    echo ">>> Migrating $label to PostgreSQL $pg_target (pg_upgradecluster)"
    if pg_upgradecluster -v "$pg_target" -m upgrade "$ver" "$name"; then
        pg_ctlcluster "$ver" "$name" stop 2>/dev/null || true
        printf 'manual\n' > "/etc/postgresql/$ver/$name/start.conf"
        echo ">>> $label now runs as PostgreSQL $pg_target/$name on port $port. The old cluster is kept stopped; remove it with: pg_dropcluster $ver $name"
    else
        pg_dropcluster --stop "$pg_target" "$name" 2>/dev/null || true
        pg_ctlcluster "$ver" "$name" start || true
        warn "$label could not be migrated to PostgreSQL $pg_target and was restarted unchanged (backup: $dump)"
    fi
}
if [ "$(cat "$STATE_DIR/pg-migrate" 2>/dev/null)" = 1 ] && command -v pg_lsclusters >/dev/null 2>&1; then
    if [ ! -x "/usr/lib/postgresql/$pg_target/bin/postgres" ]; then
        apt-get install $APT_OPTS "postgresql-$pg_target" || warn "could not install PostgreSQL $pg_target"
    fi
    if [ -x "/usr/lib/postgresql/$pg_target/bin/postgres" ] && pg_lsclusters -h > "$STATE_DIR/clusters-before.txt"; then
        while read -r ver name port status _rest; do
            case "$ver" in
                *[!0-9.]*|'') continue ;;
            esac
            [ "${ver%%.*}" -lt "$pg_target" ] || continue
            migrate_cluster "$ver" "$name" "$port" "$status" < /dev/null
        done < "$STATE_DIR/clusters-before.txt"
    fi
fi

while read -r version; do
    case "$version" in
        [0-9].[0-9]|[0-9].[0-9][0-9]) ;;
        *) continue ;;
    esac
    systemctl enable --now "php$version-fpm" || warn "php$version-fpm is not running"
done < "$STATE_DIR/php-versions"

# glibc changes between releases (2.39 -> 2.43), so text indexes built with
# the old collation may be ordered wrongly. Databases whose recorded
# collation version differs are reindexed and then marked current.
if command -v pg_lsclusters >/dev/null 2>&1; then
    pg_lsclusters -h 2>/dev/null | while read -r ver name port status _rest; do
        case "$status" in online*) ;; *) continue ;; esac
        runuser -u postgres -- psql -p "$port" -XAtqc \
            "SELECT datname FROM pg_database WHERE datallowconn AND datcollversion IS DISTINCT FROM pg_database_collation_actual_version(oid)" \
            < /dev/null 2>/dev/null > "$STATE_DIR/pg-collation-$ver-$name.txt" || continue
        while IFS= read -r db; do
            [ -n "$db" ] || continue
            echo ">>> PostgreSQL $ver/$name: rebuilding indexes of $db for the new glibc collation"
            # Read from stdin so psql quotes the name (:"db") itself.
            if ! printf '%s\n' 'REINDEX DATABASE CONCURRENTLY :"db";' 'ALTER DATABASE :"db" REFRESH COLLATION VERSION;' \
                | runuser -u postgres -- psql -p "$port" -X -q -v ON_ERROR_STOP=1 -v db="$db" -d "$db"; then
                warn "PostgreSQL $ver/$name: could not rebuild the indexes of $db after the glibc update; run REINDEX DATABASE and ALTER DATABASE ... REFRESH COLLATION VERSION"
            fi
        done < "$STATE_DIR/pg-collation-$ver-$name.txt"
    done
fi

if command -v nginx >/dev/null 2>&1; then
    # The upgrade replaces an unmodified nginx.conf with the new release's,
    # which sets "server_tokens build;". With the panel's drop-in that is a
    # duplicate directive and nginx refuses to start, so the new line is
    # commented out (the panel's server_tokens off applied before).
    if [ -f @NGINX_TOKENS@ ] && grep -qE '^[[:space:]]*server_tokens[[:space:]]' /etc/nginx/nginx.conf; then
        sed -E 's/^([[:space:]]*)(server_tokens[[:space:]])/\1# \2/' /etc/nginx/nginx.conf > /etc/nginx/nginx.conf.jenderal \
            && cat /etc/nginx/nginx.conf.jenderal > /etc/nginx/nginx.conf
        rm -f /etc/nginx/nginx.conf.jenderal
        echo ">>> Commented out server_tokens in nginx.conf; @NGINX_TOKENS@ sets it"
    fi
    if nginx -t; then
        if systemctl is-active --quiet nginx; then
            systemctl reload nginx || warn "nginx did not reload"
        else
            systemctl restart nginx || warn "nginx did not start"
        fi
    else
        warn "nginx -t failed after the upgrade; check the nginx configuration"
    fi
fi
exit 0
`
