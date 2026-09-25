# Design: Supervisor — proses kustom panel-native

Status: draft for review
Date: 2026-09-25

## Latar belakang

Panel sudah menjalankan tiga jenis proses panjang berbasis systemd (app service
per situs, queue worker, Octane), tetapi semuanya terikat pada website. Belum ada
cara mendaftarkan **proses bebas** — misal bot, worker non-Laravel, daemon kecil,
atau app Node/Go yang berdiri sendiri — dan mengawasinya dari panel.

Keputusan (sesi brainstorming; user memilih opsi rekomendasi): **panel-native**
berbasis systemd, mengikuti pola queue worker yang sudah terbukti — **bukan** paket
supervisord (menghindari daemon kedua yang harus dirawat).

## Backend (`internal/supervisor/`)

### Model & migration (`039_supervisor_processes.sql`)

Tabel `supervisor_processes`: `id TEXT PK`, `name TEXT UNIQUE`, `command TEXT`,
`working_dir TEXT DEFAULT ''`, `run_as TEXT DEFAULT ''` (user sistem; kosong =
root tidak diizinkan → wajib user non-root), `env TEXT DEFAULT ''` (format KEY=VALUE
per baris, disimpan terpisah dari unit), `auto_restart INTEGER DEFAULT 1`,
`status TEXT`, `created_by TEXT`, `created_at/updated_at`.

### Unit systemd

`jenderal-proc-<id>.service` (glob `jenderal-*` sudah ada di `services.allowed`
→ ikut terlihat di halaman Services). Render murni `RenderProcessUnit(...)`
(golden-testable), ditulis via pola heredoc Octane, `EnvironmentFile`
`/etc/jenderal/proc/<id>.env` (0600 root, ditulis via `RunSudoWithInput tee`),
`Restart=always`/`RestartSec=5` bila auto_restart, `Type=simple`,
`After=network.target`.

### Service (pola `internal/queue/service.go`)

- `List`, `Get`, `Create` (validasi nama `[a-zA-Z0-9-_.]{1,64}` unik, command
  non-kosong, working_dir path absolut bila diisi, run_as user non-root valid,
  env baris `KEY=VALUE` regex ketat), `Update` (tulis ulang unit+env, restart
  bila aktif), `Delete` (stop+disable+rm unit+env+row), `Action`
  (start/stop/restart), `Logs` (journalctl -u, tail).
- Unit name pattern di-validate sebelum interpolarasi shell (anti-injection,
  pola `octaneUnitNamePattern`).
- Audit log tiap aksi (`proc_create/update/delete/start/stop/restart`).

### API & RBAC

- `GET /supervisor` (list+status), `POST /supervisor` (create),
  `PUT /supervisor/{id}`, `DELETE /supervisor/{id}`,
  `POST /supervisor/{id}/{action}` (start|stop|restart),
  `GET /supervisor/{id}/logs`.
- Izin baru di rbac.go Seed: `supervisor.view`, `supervisor.manage` —
  server-wide → admin-only (tidak masuk `userRolePermissions`).
- Wiring: `Dependencies.SupervisorSvc`, main.go, handler pola `queue/handler.go`.

## Frontend

- Halaman baru `/supervisor` (grup nav Operations, setelah Terminal; permission
  `supervisor.view`), domain i18n baru `domains/supervisor.ts` prefix `supp.`
  (EN + ID), didaftarkan di `i18n/index.ts`.
- UI: daftar proses (nama, command, user, status badge, auto-restart) dengan
  tombol start/stop/restart per baris; form tambah/edit (drawer/modal pola
  queue-workers); log viewer per proses; delete dengan konfirmasi dua langkah.
- Status unit juga otomatis muncul di halaman Services (tanpa kerja tambahan).

## Testing

- Unit: validasi nama/command/env/run_as; `RenderProcessUnit` golden
  (auto-restart on/off, env file path, User=); parsing status.
- MockExecutor: create → unit+env tertulis + daemon-reload + enable; delete
  → stop/disable/rm; action → systemctl argumen benar.
- Frontend: `npm run check` 0 error, `npm test`, build.
- Live (maintainer): daftarkan proses `ping`/app kecil, restart server
  (systemd) dan pastikan auto-restart, log tampil.

## Yang tidak untuk v1 (YAGNI)

- Multi-instance / scaling count per proses.
- Cron/schedule (modul cron sudah ada).
- Dependency antar proses, priority, nice/ionice.
- Notifikasi status proses (modul notification bisa menyusul).
