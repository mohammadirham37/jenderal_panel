# Ubuntu 24.04 → 26.04 Release Upgrade Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let an admin run a safe noble → resolute upgrade from the `/update` page, per `docs/superpowers/specs/2026-11-22-ubuntu-release-upgrade-design.md`.

**Architecture:** A new `internal/releaseupgrade` package owns preflight checks, the upgrade script, state/status reading and repair. The upgrade runs in a transient systemd unit (`jenderal-release-upgrade`) so it survives panel restarts; state lives in `/var/lib/jenderal/release-upgrade/state` and the log in `/var/log/jenderal/release-upgrade.log`. PHP and PostgreSQL repository setup scripts are exported from their packages and reused for the post-upgrade repair.

**Tech Stack:** Go (chi, MockExecutor, httptest), bash, SvelteKit 3 / Svelte 5 runes.

---

## File structure

| File | Responsibility |
|------|----------------|
| `internal/php/service.go` (modify) | resolute uses sury; export `RepositorySetupScript()`; resolute versions 8.1–8.5 |
| `internal/php/updates.go` (modify) | baseline applies on resolute |
| `internal/dbmanager/postgresql.go` (modify) | extract and export `PGDGRepositorySetupScript()` |
| `internal/releaseupgrade/sources.go` | parse apt sources (.list + deb822), map suites noble → resolute |
| `internal/releaseupgrade/preflight.go` | the preflight checks |
| `internal/releaseupgrade/script.go` | override cfg, apt conf, upgrade script text |
| `internal/releaseupgrade/service.go` | `Service`: Preflight, Start, Status, Repair |
| `internal/releaseupgrade/handler.go` | HTTP handlers |
| `internal/api/router.go`, `cmd/jenderal/main.go` (modify) | wiring |
| `internal/auth/rbac.go` (modify) | `server.release_upgrade` permission |
| `web/src/lib/components/ReleaseUpgradeCard.svelte` | UI card |
| `web/src/routes/update/+page.svelte` (modify) | mount the card |
| `web/src/lib/i18n/domains/relupg.ts` + registry | EN/ID strings |

## Task 1: PHP on resolute uses sury

- [ ] Test (`internal/php/service_test.go`): `VersionsForCodename("resolute")` = `[8.1 8.2 8.3 8.4 8.5]`; noble unchanged `[8.1 8.2 8.3 8.4]`.
- [ ] Test: `runRepositoryScript(t, phpRepositorySetupScript, "resolute", {ondrej-php.list})` writes `sury-php.list` with `packages.sury.org/php/ resolute main` and removes the PPA. Replace `TestRepositoryScriptSkipsPPAOnResolute` (no longer true).
- [ ] Implement: case `jammy|noble|resolute) ;;`; drop the resolute exit branch; `VersionsForCodename` returns `resoluteVersions = []string{"8.1","8.2","8.3","8.4","8.5"}`; export `func RepositorySetupScript() string { return phpRepositorySetupScript }`.
- [ ] `updates.go`: `applyBaseline := true` (remove the resolute exception) and adjust its test if one asserts the old behaviour.
- [ ] `go test ./internal/php/` → PASS. Commit `feat(php): use packages.sury.org on Ubuntu 26.04 too`.

## Task 2: Export the PGDG repository setup

- [ ] Test (`internal/dbmanager`): `PGDGRepositorySetupScript()` contains the fingerprint, `%s-pgdg`/`$codename-pgdg` list line, passes `bash -n`, and does not contain `apt-get install`. Existing pgvector tests keep passing.
- [ ] Implement: move the key + list lines out of `pgvectorInstallScript` into `PGDGRepositorySetupScript()`; `pgvectorInstallScript` = `"set -eu\n" + PGDGRepositorySetupScript() + apt-get update + install`.
- [ ] `go test ./internal/dbmanager/` → PASS. Commit.

## Task 3: Apt source parsing (`sources.go`)

```go
type AptSource struct {
    File   string
    URI    string   // first URI
    Suites []string
    Types  []string // deb, deb-src
}
func ParseListFile(file, content string) []AptSource       // one-line format, skips comments/disabled, strips [options]
func ParseDeb822File(file, content string) []AptSource     // stanzas; Enabled: no skipped
func MapSuite(suite string) (string, bool)                 // noble→resolute, noble-{security,updates,proposed,backports,pgdg}→resolute-*; resolute* unchanged; else false
```

- [ ] Tests: list with `[signed-by=...]` options, comments, `deb-src`; deb822 with multiple stanzas, `Enabled: no`, multi-suite; MapSuite table incl. `noble-pgdg`, `jammy` (false), `resolute-updates` (kept).
- [ ] Implement, `go test ./internal/releaseupgrade/` → PASS. Commit.

## Task 4: Upgrade script (`script.go`)

- [ ] Constants: `OverrideConfig` (spec section 1 text), `AptConfig` (`Dpkg::Options {"--force-confdef";"--force-confold";};`), paths (`StateDir`, `LogPath`, `UnitName`).
- [ ] `func UpgradeScript() string` assembling spec section 3 steps 1–7 with `php.RepositorySetupScript()` and `dbmanager.PGDGRepositorySetupScript()` embedded as functions (`setup_php_repo() ( ... )`, `setup_pgdg_repo() ( ... )`), a `trap finish EXIT` writing `reboot_required`/`failed`, and the post-step reading site PHP versions from `$STATE_DIR/php-versions` (written by Go before start) and MySQL native-password flag `$STATE_DIR/mysql-native` (written by Go).
- [ ] Tests: `bash -n`; contains `do-release-upgrade -f DistUpgradeViewNonInteractive`; override cfg written before the upgrade line and removed in `finish`; `RemoveObsoletes=no`, `PurgeObsoletes=no`, `AllowThirdParty=yes`, `pgdg` pocket; post-repair only runs when codename is resolute.
- [ ] Commit.

## Task 5: Preflight (`preflight.go`)

```go
type Check struct{ ID, Status, Message string } // Status: ok|warn|block
type Preflight struct {
    Current string  `json:"current"`
    Target  string  `json:"target"`
    Checks  []Check `json:"checks"`
    Blocked bool    `json:"blocked"`
}
```

Checks per spec table, each a method on Service using `exec` (RunSudo), `db`, and an injectable `http *http.Client` + `suryPackagesURL`:
`release` (cat /etc/os-release), `available` (`do-release-upgrade -c`, exit 0 + mentions 26.04), `pending_updates` (`apt-get -s dist-upgrade`, count `^Inst `), `reboot_required` (`test -e /var/run/reboot-required`), `dpkg` (`dpkg --audit` empty + `fuser /var/lib/dpkg/lock-frontend` exit≠0), `disk` (`df -B1 --output=avail / /boot`), `sources` (read files via `cat`, map, HEAD `InRelease` then `Release`), `php` (DB distinct versions vs sury resolute Packages.gz `phpX.Y-fpm`), `mysql_native_password` (warn; `mysql -N -e "SELECT COUNT(*) FROM mysql.user WHERE plugin='mysql_native_password'"`, skip when mysql absent), `foreign_sources` (warn), `panel_task` (unit active).

- [ ] Tests with MockExecutor + httptest: happy path all ok; each block condition flips `Blocked`; unknown source host returning 404 blocks with its URI in the message; sury list missing a used version blocks `php`.
- [ ] Commit.

## Task 6: Service Start / Status / Repair (`service.go`)

- [ ] `Start(ctx, StartRequest{SnapshotConfirmed bool, Confirm string})`: validate (`Confirm == "26.04"`, snapshot), rerun preflight → block error, reject if unit active, write `php-versions`/`mysql-native`/`state=running` via `RunSudoWithInput tee`, then `RunSudo("systemd-run", "--unit="+UnitName, "--collect", "--property=Type=exec", "/bin/bash", "-c", UpgradeScript())`.
- [ ] `Status(ctx)`: read state file, `systemctl is-active`, `tail -n 400` log; `running` + inactive unit → `failed`.
- [ ] `Repair()`: only when state `failed`; taskrunner `RunMultiple` with `dpkg --configure -a` and `apt-get -f install -y -o Dpkg::Options::=--force-confdef -o Dpkg::Options::=--force-confold`; returns task ID.
- [ ] Tests for validation errors, already running, the systemd-run argv, status mapping, repair state gate.
- [ ] Commit.

## Task 7: Handler, routes, RBAC, wiring

- [ ] `handler.go`: `Preflight` GET, `Start` POST (audit `release_upgrade_started`), `Status` GET, `Repair` POST (audit).
- [ ] Router: `/server/release-upgrade` (GET preflight, `server.release_upgrade`), `/server/release-upgrade` POST, `/server/release-upgrade/status` GET, `/server/release-upgrade/repair` POST, all `server.release_upgrade`.
- [ ] `rbac.go`: add `{"server.release_upgrade", "server"}` (admin only; not in user role).
- [ ] `main.go`: construct `releaseupgrade.NewService(exec, db, tasks)`; add to Dependencies.
- [ ] `go build ./... && go test ./...` → PASS. Commit.

## Task 8: Frontend card

- [ ] `relupg.ts` i18n (EN + ID), registered like the other domains.
- [ ] `ReleaseUpgradeCard.svelte`: loads preflight + status; hides when current ≠ noble and state is idle; checklist with ok/warn/block icons; Start → Modal (snapshot checkbox, type 26.04); while running polls status every 3 s and shows log `<pre>`; `reboot_required` → Reboot button (`POST /api/v1/server/reboot`, confirm); `failed` → Repair button with `TaskProgress`.
- [ ] Mount in `/update` page below the apt section, gated by `hasPermission($permissions, 'server.release_upgrade')`.
- [ ] `npm run check`, `npm test`, `npm run build` → PASS. Commit and push.

## Self-review notes

Spec coverage: §1 → Task 4; §2 → Tasks 3, 5; §3 → Tasks 4, 6; §4 → Task 6; §5 → Task 6; §6 → Task 1; §7 → Task 8; RBAC/audit → Task 7; pgdg normalization → Tasks 2, 4.
