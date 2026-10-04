# Backup Remote Storage — Design

Date: 2026-10-04
Status: Approved design (brainstorming outcome)

## Background

An off-site copy already exists: after every non-safety backup completes, the
archive is uploaded to a configured remote destination — S3-compatible storage
via a hand-rolled SigV4 client (`internal/backup/s3.go`) or any rclone remote
(including Google Drive via `rclone copyto`). Configuration lives in `settings`
rows (`backup_remote_*`) and is edited through a form on the Settings page.
`backups.remote_path` records the remote location but is informational only.

Gaps this design closes:

1. The local file is always kept — backups keep consuming local disk.
2. Download/restore/delete/prune only see the local file.
3. Google Drive requires rclone installed and configured on the server.
4. The config UI lives on the Settings page, invisible from the Backups page.
5. `GET /api/v1/settings` returns the S3 secret key in plaintext to any user
   with `settings.view`.

## Goals

1. **Disk-saving mode** (global toggle): after a successful upload, delete the
   local file and mark the backup remote-only. Applies to every non-safety
   backup — manual and scheduled alike. Failed upload → file stays local with
   a retry action.
2. **Native Google Drive backend** via the Drive API v3 (OAuth refresh token,
   copy-paste authorization code — no rclone dependency).
3. **Storage configuration on the Backups page** (moved from Settings) with a
   connection test.
4. **Download and restore from remote** for remote-only backups (download
   streams straight to the browser; restore uses a temporary local file).
5. **Delete-everywhere semantics**: manual delete and retention prune remove
   the local file and the remote object.

## Non-goals

- Per-schedule destination targeting (`backup_schedules.storage` stays unused).
- Encryption at rest for secrets (plaintext in SQLite, same as today; see
  Security notes).
- Multiple simultaneous remote destinations.
- Streaming restore (restore-from-remote stages a temp file first).

## Architecture

### New package: `internal/remotestorage`

A pure storage-client package. It depends on `executor.CommandExecutor` (for
sudo file reads and the rclone backend) and on `net/http`; the backup package
depends only on the interface.

```go
type Storage interface {
    // Upload streams the local file (opened via RunSudoStream) to the
    // destination and returns a stable remote reference.
    Upload(ctx context.Context, localPath string, size int64, name string) (string, error)
    // Download streams the remote object into w (never fully buffered).
    Download(ctx context.Context, name string, w io.Writer) error
    // Delete removes the remote object.
    Delete(ctx context.Context, name string) error
    // Test verifies credentials and returns a human-readable identifier
    // (e.g. bucket name or Drive account email).
    Test(ctx context.Context) (string, error)
}

func New(cfg Config, exec executor.CommandExecutor, httpClient *http.Client) (Storage, error)
```

The remote reference (`remote_path` in the `backups` row) is scheme-tagged so
deletes/downloads keep working even if the panel later inspects them:

- S3: `https://<endpoint-host>/<bucket>/<prefix>/<name>` (unchanged format)
- Google Drive: `gdrive://<fileId>`
- rclone: `<remote>:<path>/<name>` (unchanged format)

`New` dispatches on `cfg.Type`; unknown/empty type returns an error, which the
backup service treats as "no remote".

### Config

Stored in the existing `settings` table. Existing keys are unchanged, so
current S3/rclone configurations keep working:

| Key | Purpose |
|---|---|
| `backup_remote_type` | `""` \| `s3` \| `gdrive` \| `rclone` |
| `backup_remote_s3_*` | endpoint, bucket, region, access_key, secret_key, prefix (existing) |
| `backup_remote_rclone_*` | remote, path (existing) |
| `backup_remote_gdrive_client_id` | OAuth client ID |
| `backup_remote_gdrive_client_secret` | OAuth client secret |
| `backup_remote_gdrive_refresh_token` | stored after code exchange |
| `backup_remote_gdrive_folder_id` | target Drive folder (created on first connect if empty) |
| `backup_remote_delete_local` | `"1"` enables disk-saving mode |

A typed `Config` struct lives in `remotestorage`; `internal/backup` gains
read/save helpers (replacing `GetRemoteConfig`/`SaveRemoteConfig` in `s3.go`,
which move to the new package and gain the gdrive + delete-local fields).

### Google Drive backend

- **Scope**: `https://www.googleapis.com/auth/drive.file` — least privilege;
  the panel can only see and delete files it created itself.
- **OAuth flow (copy-paste)**: user creates an OAuth client of type
  *Desktop app* in Google Cloud Console (and enables the Drive API for the
  project). The panel builds the consent URL with a fixed loopback redirect
  (`http://localhost:1/`, implicitly allowed for Desktop clients). After
  authorizing, the browser lands on a non-loading localhost URL whose address
  bar contains `?code=...`; the user pastes that code into the panel.
- **Token exchange / refresh**: `POST https://oauth2.googleapis.com/token`
  (`authorization_code` once, then `refresh_token`). The refresh token is
  persisted in settings; access tokens are held in memory with expiry and
  refreshed on demand (one retry after a 401).
- **Upload**: Drive API v3 *resumable* session
  (`uploadType=resumable`, metadata `{name, parents:[folderId]}`, then a
  single `PUT` of the streamed bytes with known `Content-Length`).
- **Folder**: if `backup_remote_gdrive_folder_id` is empty at connect time,
  the panel creates a folder named `Jenderal Panel Backups` (via
  `files.create` with `mimeType=text/vnd.google-apps.folder`) and stores its
  ID.
- **Download**: `GET /drive/v3/files/<id>?alt=media`, streamed.
- **Delete**: `DELETE /drive/v3/files/<id>`.
- **Test**: `GET /drive/v3/about?fields=user` → returns the account email.

### S3 backend

Port of the current SigV4 code from `internal/backup/s3.go`:
`UNSIGNED-PAYLOAD` streaming `PUT` for upload, plus new `GET` (streamed) for
download, `DELETE` for delete, and a 1-key list request for `Test`.

### rclone backend

Commands through the executor: `rclone copyto` (upload), `rclone cat`
(streamed download), `rclone deletefile` (delete), `rclone lsjson --max-depth 1`
(test). Needed so existing rclone configurations get the full new feature set
(remote-only download, delete-everywhere, connection test).

## Backup service changes (`internal/backup`)

- **`executeBackup`** (unchanged upload point, `service.go:237-250`): on
  successful upload, set `remote_path`. If disk-saving mode is enabled, delete
  the local file (`RunSudo rm -f`) and set `remote_only=1`. Upload failure
  stays best-effort: warning line in the task output, backup remains local.
- **`RetryUpload(ctx, caller, id)`** — new: for a completed backup whose local
  file exists and `remote_path` is empty. Runs as a task (2h timeout, same as
  backup tasks) and applies the same post-upload handling as `executeBackup`.
- **Delete-everywhere**: a shared `destroyBackup` helper used by both
  `DeleteBackup` and the prune paths (`pruneExpired`, `retentionExpired`):
  1. If `remote_path` is set and the currently configured backend matches the
     scheme, delete the remote object first. Failure aborts the deletion with
     a clear error (no orphaned rows). Scheme matching: `gdrive://…` → the
     gdrive backend, `http(s)://…` → the S3 backend, anything else shaped
     `<remote>:<path>` → the rclone backend.
  2. Delete the local file if present.
  3. Delete the row.
  If `remote_path` is set but no backend is configured (or the scheme does not
  match the configured type), local deletion proceeds and the response/log
  carries a warning that the remote copy was left in place. Prune skips rows
  whose remote deletion failed and retries them on the next hourly tick.
- **Download** (`StreamBackupFile` / `ResolveBackupDownload`): when
  `remote_only` is set, stream from the remote backend into the
  `http.ResponseWriter` with `Content-Length` from `size_bytes` and the usual
  filename. Local behavior unchanged.
- **Restore**: when `remote_only` is set, first download the archive to
  `<localDir>/tmp/<ulid>-<basename>` (directory created via `RunSudo`; free
  space checked against `size_bytes` beforehand via the existing `df` helper
  used by `Stats`), run the existing path-based restore logic, then remove the
  temp file (`defer`, also on failure). The temp staging disk cost is shown in
  the UI when the backup is remote-only.
- **Safety backups** are never uploaded and always stay local (unchanged).
- `model.Backup` gains `RemoteOnly bool` (`json:"remote_only"`,
  DB column `remote_only`).

## API

New endpoints (registered in `internal/api/router.go`, wired through
`Dependencies`):

| Method & path | Permission | Purpose |
|---|---|---|
| `GET /api/v1/backup-remote/config` | `backups.remote` | Config with secrets replaced by `*_set` booleans |
| `PUT /api/v1/backup-remote/config` | `backups.remote` | Save config; empty secret fields preserve stored values |
| `POST /api/v1/backup-remote/test` | `backups.remote` | Run `Test`, return the identifier |
| `GET /api/v1/backup-remote/gdrive/authorize` | `backups.remote` | `{url}` consent URL |
| `POST /api/v1/backup-remote/gdrive/exchange` | `backups.remote` | `{code}` → store refresh token, return connected account |
| `POST /api/v1/backups/{id}/upload` | `backups.create` | Retry upload (202 + `task_id`) |

- New RBAC permission **`backups.remote`** (module `backups`), granted to
  admin only — not to the `user` role. Seeded in `internal/auth/rbac.go`
  (74 permissions total after this change).
- `GET /api/v1/settings` no longer needs to expose `backup_remote_s3_secret_key`:
  after the Settings-page form is removed, the raw secret is only ever written
  through the new PUT, which never echoes it back.

## Migration

`043_backup_remote_only.sql`:

```sql
ALTER TABLE backups ADD COLUMN remote_only INTEGER NOT NULL DEFAULT 0;
```

Settings keys need no migration.

## Frontend

- **`web/src/routes/backups/+page.svelte`** — third tab **Storage**:
  - Backend selector: Nonaktif / S3 / Google Drive / rclone, with per-type
    forms (loaded via the config GET, saved via PUT).
  - Google Drive wizard: instructions (create *Desktop* OAuth client, enable
    Drive API) → paste client ID/secret → "Buka halaman izin Google" (link
    from the authorize endpoint) → paste the code → connected state shows the
    account email (from Test).
  - Toggle: "Hapus file lokal setelah upload sukses".
  - "Uji koneksi" button + status line.
  - Config loading uses the permission-safe `getSafe` pattern: a 403 (regular
    user) collapses the tab to a notice instead of breaking the page.
  - Backup rows: location badge — Lokal / Lokal+Remote / Remote — derived from
    `remote_only` + `remote_path`; Download works for remote-only rows;
    "Upload ke remote" retry button appears for completed local rows with no
    `remote_path` when a backend is configured; Restore on remote-only rows
    shows the temporary-disk note.
- **`web/src/routes/settings/+page.svelte`** — remove the existing remote
  backup storage section (moved to the Backups page; same settings keys).
- i18n: new keys in `web/src/lib/i18n/domains/bksrv.ts`, English and Bahasa
  Indonesia.

## Testing

- `internal/remotestorage` (unit, `httptest` server): SigV4 signing + upload/
  download/delete round trips, GDrive code exchange, token refresh, resumable
  upload, streamed download, delete, about/test; error paths (non-2xx).
- `internal/backup` (unit, fake `Storage` + existing executor doubles):
  disk-saving post-upload flow, retry upload, delete-everywhere success and
  remote-failure abort, prune retry behavior, remote-only download/restore
  path selection, temp-file cleanup on failure.
- Static gates: `go test ./... -race`, `make lint`, `cd web && npm run check`,
  `npm run build`, `npm test`. No browser/app testing (maintainer tests
  manually on Ubuntu).

## Security notes

- Secrets (S3 secret key, GDrive client secret + refresh token) remain
  plaintext rows in SQLite — unchanged from the status quo, a documented
  limitation. The config GET now masks them, which is an improvement over the
  current `GET /api/v1/settings` exposure.
- Drive scope is `drive.file`: panel-scoped visibility, cannot enumerate or
  delete unrelated Drive content.
- All privileged file operations continue to go through `RunSudo` with
  positional arguments (no `sh -c`).

## Follow-ups (documentation)

- AGENTS.md states "63 RBAC permissions"; the real current count is 73 (74
  after this feature). Correct the count while touching `rbac.go`.
