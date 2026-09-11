# Design: Backup/Restore Improvements

Status: draft for review
Date: 2026-09-11

## Current state and problems found

The backup module (`internal/backup`) supports manual and scheduled backups
(website / database / config / full) to local disk with day-based retention.
Reviewing the code surfaced these issues:

1. **PostgreSQL restore is broken**: backups are written with `pg_dump`
   (plain SQL) but restored with `pg_restore`, which cannot read plain SQL.
2. **Command injection surface**: database dump/restore interpolate the
   user-typed target into `bash -c "mysqldump %s > %s"`.
3. **No execution visibility**: backups run in a bare goroutine — no task ID,
   no progress, violating the panel convention of using the taskrunner for
   long operations.
4. **No download**: backup files cannot be retrieved from the panel.
5. **Restore side effects**: website restore extracts without re-applying
   ownership; full backups cannot be restored at all.
6. **UX gaps**: targets are typed by hand (typo-prone), no size summary,
   no filters, no duration, retention is day-based only.

## Decisions taken without confirmation (defaults)

| # | Question | Default taken |
|---|----------|---------------|
| 1 | Storage scope | Local disk now; storage abstraction prepared so S3/rsync can be added later without schema changes |
| 2 | Restore safety | Auto safety-backup of the target before every restore |
| 3 | Role users | Backup opened to the user role, scoped by ownership (consistent with phase 1) |
| 4 | Retention | Day-based **and** keep-N, plus a manual "Prune now" |

---

## Phase A — Correctness and safety (must-fix)

- **No more interpolated shell.** Database dump/restore commands become
  direct argv with redirect handled as
  `sh -c 'exec mysqldump "$1" > "$2"' dump <target> <path>` — the shell is
  used only for the redirect and both user values travel as positional
  parameters, never inside the command string. DB restore pipes the dump on
  stdin via `RunSudoWithInput` (same as the databases-page restore).
- **Fix PostgreSQL restore**: restore plain-SQL dumps with
  `psql --set ON_ERROR_STOP=on --dbname <db>` (pg_restore stays reserved for
  future custom-format dumps).
- **Shared dump/restore core**: extract the working dump/restore logic from
  `internal/dbmanager` (export/restore feature) into a small
  `internal/dbdump` package used by both the databases page and the backup
  module, so the two never drift again.
- **Website restore ownership**: after `tar -xzf`, re-apply
  `chown -R <webuser>:<webuser>` on the site directory.
- **Safety backup before restore**: every website/database restore first
  creates a backup of the current state, recorded with
  `kind = 'safety'` (3-day retention, pruned like everything else). The
  restore confirmation dialog names the safety backup created.
- **Full backups restorable by component**: the full archive keeps
  per-component files plus a `manifest.json`; restore asks which component
  (website(s) / databases / config) to restore.

## Phase B — Execution and progress

- `runBackup` moves to the **taskrunner** (`tasks.Run`); the backups table
  gains `task_id`, `started_at`, `finished_at`, and `kind`
  (`manual` / `scheduled` / `safety`). The backups page shows live progress
  via the existing `TaskProgress` component (`bind:taskId` + `storageKey`).
- Duration and started/finished times shown per row.

## Phase C — Usability

- **Download**: `GET /backups/{id}/download` streams the archive as an
  attachment (ownership-scoped, audit-logged).
- **Target pickers**: the create form selects a website (by domain) or a
  database (by name) from dropdowns populated from their list endpoints —
  no more free-typed targets; role users only see their own resources.
- **Overview cards**: total backups, total size, disk free in the backup
  directory (`df`), last successful backup; filter chips by type and status.
- **Row actions**: Download / Restore (typed confirmation, shows the safety
  backup it will create) / Delete (typed confirmation).
- **Ownership**: `backups.created_by`; non-admin callers list only their own
  backups and may only target resources they own (`backups.view` and
  `backups.manage` granted to the user role).

## Phase D — Retention and schedules

- Per-schedule retention: `retention_days` (existing) **plus**
  `retention_keep` (keep-N newest, 0 = off); whichever prunes first wins.
- **Prune now** button (per schedule and a global one) running the same
  retention pass as the scheduler.
- Schedules display the next estimated run (schedule preset + last_run) and
  `last_run_status` (success/failed) so failures are visible on the page.
- Schedule presets stay hourly/daily/weekly/monthly (the scheduler ticks
  hourly; documented drift ≤ 1 interval).

## Storage abstraction (foundation only)

- The `storage` column stays; reads/writes go through a narrow
  `Destination` interface (local driver implemented now) so an S3- or
  rsync-based driver can slot in later without schema or UI rework. No
  remote UI in this round.

## Schema changes (migration 028)

- `backups`: `+ created_by, task_id, kind, started_at, finished_at`
- `backup_schedules`: `+ created_by, retention_keep, last_run_status`

## Testing

- Pure unit tests: path generation, retention math (days vs keep-N),
  interval parsing, dump/restore command builders, manifest read/write,
  ownership guard truth table.
- MockExecutor tests: runBackup status transitions, safety-backup ordering,
  prune behavior.
- Live restore flows (tar ownership, psql ON_ERROR_STOP, safety backups on
  real data) remain maintainer-verified on Ubuntu 24.04 per the testing
  policy.

## Rollout order

A (correctness/safety) → B (taskrunner) → C (UX/download/ownership) →
D (retention). Each phase is independently shippable.
