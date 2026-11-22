# Ubuntu 24.04 → 26.04 release upgrade — design

Date: 2026-11-22
Status: approved in conversation, pending spec review

## Goal

Let an admin upgrade a panel server from Ubuntu 24.04 (noble) to 26.04
(resolute) from the panel, safely: nothing the panel manages (PHP versions
used by sites, PostgreSQL clusters, MySQL users, edited configs) may be lost,
and a failed upgrade must leave a readable log and a repair path.

Out of scope: other upgrade paths (22.04 → 24.04, non-LTS), automatic
reboot, VPS snapshots (the panel cannot take them; the admin confirms one
exists).

## Decisions

- Use Canonical's `do-release-upgrade` (the supported path), not a manual
  sources rewrite + `apt full-upgrade`.
- On resolute the panel uses packages.sury.org/php too, so PHP 8.1–8.5 stay
  available and sites keep their PHP version across the upgrade.
- No automatic reboot: when the upgrade finishes the panel shows
  "finished, reboot required" with a Reboot button.

## What the stock upgrader would do (verified in resolute.tar.gz)

Read from the resolute dist-upgrader (`DistUpgradeController.py`,
`DistUpgradeCache.py`, `DistUpgrade.cfg`, `DistUpgradeViewNonInteractive.py`):

1. `DistUpgradeViewNonInteractive.askYesNoQuestion` always returns True, and
   `confirmChanges` always returns True. Every prompt is answered "yes".
2. `DistUpgrade.cfg` sets `RemoveObsoletes=yes` and `PurgeObsoletes=yes`.
   Packages that were downloadable before the sources rewrite and are not
   after it are removed **and purged**. A disabled sury or pgdg source would
   purge php8.x-* (pool configs included) and postgresql-NN (whose purge can
   drop clusters).
3. Sources whose URI is not in `mirrors.cfg` are disabled unless third-party
   sources are allowed (`[Sources] AllowThirdParty` or
   `RELEASE_UPGRADER_ALLOW_THIRD_PARTY`). `mirrors.tencentyun.com` is not
   listed; when no `main` source survives, the non-interactive view accepts
   "continue using default sources" and switches to archive.ubuntu.com.
4. Suites are mapped only for `noble` and `noble-<pocket>` with pockets from
   `[Sources] Pockets` (security, updates, proposed, backports). `noble-pgdg`
   is "unknown dist" and gets disabled.
5. Overrides are read from `/etc/update-manager/release-upgrades.d/*.cfg`
   after the bundled config.
6. Legacy `.list` files are migrated to deb822 `.sources` before rewriting.

## Design

### 1. Upgrader configuration override

For the duration of the upgrade the panel writes
`/etc/update-manager/release-upgrades.d/jenderal-panel.cfg`:

```ini
[Distro]
RemoveObsoletes=no
PurgeObsoletes=no

[Sources]
AllowThirdParty=yes
Pockets=security,updates,proposed,backports,pgdg
```

- `RemoveObsoletes=no` / `PurgeObsoletes=no`: nothing is removed just
  because it is no longer downloadable.
- `AllowThirdParty=yes`: provider mirrors (Tencent etc.) and sury are kept
  and moved to resolute instead of being disabled.
- `pgdg` as a pocket maps `noble-pgdg` → `resolute-pgdg`.

The file is removed when the upgrade ends (success or failure).

### 2. Preflight — `GET /api/v1/server/release-upgrade`

Returns `{ current, target, checks: [{id, status: ok|warn|block, message}],
state }`. Any `block` disables the start button. Checks:

| id | block when |
|----|-----------|
| `release` | `VERSION_CODENAME` is not `noble` |
| `available` | `do-release-upgrade -c` does not offer resolute |
| `pending_updates` | `apt-get -s dist-upgrade` would upgrade anything (the upgrader refuses otherwise) |
| `reboot_required` | `/var/run/reboot-required` exists |
| `dpkg` | `dpkg --audit` reports anything, or apt/dpkg is locked |
| `disk` | `/` has < 5 GB free or `/boot` < 300 MB free |
| `sources` | a panel repository (sury, pgdg) has no resolute equivalent (its mapped suite's `InRelease`/`Release` does not answer 200). Any other source without one is only a warning: the upgrader disables it and, with `RemoveObsoletes=no`, keeps its packages |
| `php` | a PHP version used by any site is not published by sury for resolute |
| `panel_task` | a panel apt/PHP/DB task or a panel update is running |

Warnings (never block):

- `mysql_native_password`: MySQL users using that plugin (MySQL 8.4 in
  resolute disables it by default). The post-upgrade step re-enables it.
- `foreign_sources`: enabled third-party sources that are not panel-managed
  (sury, pgdg); they are moved to resolute as well.

Source parsing covers `/etc/apt/sources.list`, `sources.list.d/*.list` and
`*.sources` (deb822), read via `RunSudo cat`. Suite mapping mirrors the
upgrader: `noble` → `resolute`, `noble-<pocket>` → `resolute-<pocket>` for
the pockets above (including pgdg).

### 3. Start — `POST /api/v1/server/release-upgrade`

- Permission: new `server.release_upgrade` (seeded, admin role).
- Body: `{ snapshot_confirmed: true, confirm: "26.04" }`; anything else is a
  validation error.
- Preflight runs again server-side; any `block` → validation error.
- Audit log entry.
- Runs `systemd-run --unit=jenderal-release-upgrade --collect
  --property=Type=exec /bin/bash -c <script>` so the upgrade survives a
  panel restart or closed browser. Starting while the unit is active is
  rejected.

Script (fixed text, no user input):

1. `mkdir -p /var/lib/jenderal/release-upgrade`; write `state` = `running`.
2. Record `dpkg-query -W` of installed `php*`, `postgresql*` and
   `mysql-server*` packages to `packages-before.txt`.
3. Write the override cfg (section 1); set `Prompt=lts` in
   `/etc/update-manager/release-upgrades`.
4. Write `/etc/apt/apt.conf.d/99jenderal-release-upgrade` with
   `Dpkg::Options {"--force-confdef";"--force-confold";}` so edited configs
   (nginx, php.ini, pools) are kept.
5. `DEBIAN_FRONTEND=noninteractive do-release-upgrade -f
   DistUpgradeViewNonInteractive` with output to
   `/var/log/jenderal/release-upgrade.log`.
6. Post-upgrade repair (only if the codename is now resolute):
   - Normalize panel repos: drop the deb822 copies the upgrader made of
     `sury-php` and `pgdg`, re-run the PHP repository setup script and the
     pgdg setup, `apt-get update`.
   - Reinstall any package in `packages-before.txt` that is no longer
     installed, and `apt-get install --only-upgrade` the ones still
     installed so sury/pgdg packages move from their noble builds to the
     resolute builds (php8.5 from Ubuntu's build to sury's).
   - The upgrader leaves the panel's lists as `*.list.migrate` /
     `*.list.disabled`; these are removed and never count as an existing
     entry.
   - If MySQL users use `mysql_native_password`, add
     `mysql_native_password=ON` in a `conf.d` drop-in and restart MySQL.
   - `systemctl enable --now` PHP-FPM for every version used by a site;
     `nginx -t` then reload.
7. Always (trap): remove the override cfg and the apt.conf.d file; write
   `state` = `reboot_required` (success) or `failed` with the exit code.

### 3a. PostgreSQL majors older than resolute's (18)

- Preflight check `postgresql` (warn) lists clusters older than 18 from
  `pg_lsclusters -h` and whether migrating fits on disk: free space next
  to the data directories must be at least twice the data size (copy +
  backup) plus 1 GiB.
- Without migration the post-upgrade repair adds pgdg whenever a recorded
  `postgresql-NN` other than 18 is installed (resolute's archive does not
  carry it), then upgrades it to the pgdg build.
- The confirm modal offers "also migrate to PostgreSQL 18"
  (`migrate_postgresql`); Start rejects it when no cluster needs it or the
  disk check failed. For each online older cluster the script installs
  the `postgresql-18-*` counterparts of its extension packages (skips the
  cluster with a warning if one is missing), writes a `pg_dumpall` backup
  to `/var/backups/jenderal`, runs `pg_upgradecluster -v 18 -m upgrade`
  (copy mode, the new cluster takes the old port) and keeps the old
  cluster stopped with `start.conf` = manual. On failure the new cluster is
  dropped and the old one started again; the upgrade itself still counts
  as finished, with a warning.

### 4. Status — `GET /api/v1/server/release-upgrade/status`

Reads `state`, `systemctl is-active jenderal-release-upgrade` and the last
~400 lines of the log. A `running` state with an inactive unit (crash) is
reported as `failed`. The script records the boot ID it finished in; once
the server has booted again, `reboot_required` is reported as `idle`.

### 5. Repair — `POST /api/v1/server/release-upgrade/repair`

Only in `failed` state: runs `dpkg --configure -a` then `apt-get -f install
-y` (confold) as a panel task with live output.

### 5a. Cleanup after the reboot — `GET/POST /api/v1/server/release-upgrade/cleanup`

Only on resolute with state `idle` (upgrade finished and rebooted):

- `postgresql`: a cluster older than 18 that is `down` while an online
  18 cluster with the same name exists (what the migration left behind).
  Confirmed by typing its ID (`16/main`); runs `pg_dropcluster`, then
  `apt-get remove` of that version's packages (`postgresql-16`,
  `postgresql-16-*`, `postgresql-client-16`, ...) unless another cluster
  still uses the version. The pg_dumpall backup is kept.
- `autoremove`: lists `apt-get -s autoremove` removals (old kernels,
  noble libraries) and runs `apt-get autoremove -y`.

Both run as panel tasks with live output.

### 6. PHP on resolute

- `phpRepositorySetupScript`: resolute joins jammy/noble (sury) instead of
  removing sources and exiting.
- `VersionsForCodename("resolute")` → 8.1–8.5 (noble/jammy unchanged).
- The PHP security baseline also applies on resolute.
- Existing 26.04 servers move php8.5 from the Ubuntu archive to the sury
  build on the next PHP install or update.

### 7. UI

New "Upgrade Ubuntu" card on `/update` below the apt section, shown only on
noble or while an upgrade state exists:

- Preflight checklist (ok / warn / block with messages).
- Start button → confirm modal: snapshot checkbox + type `26.04`.
- Live log (polls status every 3 s), state badge.
- `reboot_required` → Reboot button (existing `/server/reboot`).
- `failed` → Repair button + log.
- i18n EN + ID.

## Error handling

- The upgrader restores sources itself when it fails before installing
  packages; `state` = `failed`, log kept.
- Mid-install failure: `failed` + Repair.
- The panel binary is not a package; it keeps running across the upgrade.

## Testing

- Unit tests: source parsing (.list + deb822, disabled entries, suite
  mapping), each preflight check with `MockExecutor`, start validation
  (confirmation, blocks, already running), status mapping (including
  crashed unit), script `bash -n`, override cfg content, PHP resolute
  version set and repository script on resolute.
- Real upgrade: tested by the maintainer on a snapshotted 24.04 VPS.
