# Design: Cloudflare Tunnel — expose server tanpa IP publik via cloudflared

Status: approved (design dirumuskan lewat sesi brainstorming, disetujui user)
Date: 2026-09-21

## Latar belakang

Server on-premise / VPS di belakang CGNAT tidak punya IP publik, sehingga
website yang di-host dan panel itu sendiri tidak bisa diakses dari luar.
Cloudflare Tunnel (cloudflared) menyelesaikan ini lewat koneksi **outbound**
saja — tidak perlu port forwarding, tidak perlu IP publik.

Mode yang dipilih: **token paste (remotely-managed tunnel)**. User membuat
tunnel + public hostname di dashboard Cloudflare Zero Trust, menyalin
*connector token*, dan menempelkannya di panel. Panel yang meng-install
`cloudflared`, menjalankannya sebagai unit systemd, dan menampilkan status
+ log. Konfigurasi ingress (hostname → service lokal) tetap di dashboard
Cloudflare — panel tidak menyimpan kredensial API Cloudflare dan tidak
memanggil API Cloudflare sama sekali.

Keputusan lain yang sudah disetujui:

- **Satu tunnel aktif per server** (token bisa diganti/dihapus kapan saja).
- Tidak ada quick tunnel (`trycloudflare.com`), tidak ada multiple tunnel,
  tidak ada alur `cloudflared tunnel login` (cert.pem).

## Arsitektur

Modul baru `internal/cloudflared/` (service.go + handler.go + service_test.go),
mengikuti checklist modul existing (service → handler → router Dependencies →
routes+RBAC → main.go wiring → rbac.go Seed → frontend page). **Tidak ada
migration DB** — seluruh state turunan filesystem + systemd:

- binary terinstall? → `/usr/local/bin/cloudflared` (`cloudflared version`)
- token terpasang? → ada tidaknya `/etc/cloudflared/jenderal-cloudflared.env`
- tunnel jalan? → `systemctl is-active jenderal-cloudflared`

### 1. Installer binary ter-pin (pola `internal/frankenphp`)

- `Version = "2026.9.1"` (rilis stabil terbaru saat desain ini ditulis).
- Unduhan: `https://github.com/cloudflare/cloudflared/releases/download/2026.9.1/cloudflared-linux-<arch>`
- `checksums` per `uname -m` (sha256 resmi artefak rilis):
  - `x86_64`: `03f1f25d1cc93b9ad6c60569d44060bc4f17ed97075760ed8cfca4b12dcd68cc`
  - `aarch64`: `3d97437c71848bd8df68041e12436b484a661d95073ea1937f01a845ce88faa3`
- Script install root tanpa input user: arch case → URL + checksum pin →
  `curl -fsSL --retry 3` ke mktemp → `sha256sum -c` → `install -m 0755` ke
  `/usr/local/bin/cloudflared` → print versi.
- `Install(ctx)` meng-audit lalu mengembalikan task taskrunner; tombol install
  yang sama dipakai untuk update saat versi terinstall ≠ versi pin.

### 2. Koneksi via token

- Validasi sanity token di service (murni Go, unit-testable): token connector
  adalah base64 dari JSON berisi minimal field `"a"` (account tag) dan `"s"`
  (secret). Gagal decode / field kurang → `NewValidationError` **sebelum** ada
  file yang ditulis. Validasi keberadaan tunnel sesungguhnya terjadi saat
  cloudflared connect dan terlihat di status service + log.
- Token disimpan di `/etc/cloudflared/jenderal-cloudflared.env` berisi
  `TUNNEL_TOKEN=<token>`, direktori `0750` root, file `0600` root, ditulis
  via `RunSudo` + `tee` (`RunSudoWithInput`). Token tidak pernah masuk DB,
  response API, atau audit log. cloudflared menghormati env `TUNNEL_TOKEN`
  (didokumentasikan resmi); bila implementasi menemukan versi pin yang tidak
  menghormatinya, fallback: `--token-file /etc/cloudflared/jenderal-cloudflared.token`.
- Unit `/etc/systemd/system/jenderal-cloudflared.service` dirender murni
  (`RenderUnit()`, golden-testable) lalu ditulis dengan pola Octane
  (`cat > ... << 'UNITEOF'` via RunSudo) + `daemon-reload` + `enable --now`:

  ```ini
  [Unit]
  Description=Jenderal Cloudflare Tunnel
  After=network-online.target
  Wants=network-online.target

  [Service]
  Type=simple
  EnvironmentFile=/etc/cloudflared/jenderal-cloudflared.env
  ExecStart=/usr/local/bin/cloudflared tunnel --no-update run
  Restart=always
  RestartSec=5
  NoNewPrivileges=true

  [Install]
  WantedBy=multi-user.target
  ```

  `--no-update` supaya autoupdate cloudflared tidak menimpa binary ter-pin —
  update hanya lewat tombol panel (posisi flag diverifikasi terhadap
  `cloudflared tunnel run --help` saat implementasi). Hardening systemd
  (`ProtectSystem`, dsb.) sengaja minimal dan diverifikasi live oleh
  maintainer — jangan sampai hardening memblokir connector.

  Nama unit `jenderal-*` sudah ada di glob `services.allowed`
  (`configs/jenderal.yaml.example`) sehingga start/stop/restart otomatis bisa
  juga dari halaman Services existing tanpa perubahan config.

- `Connect(ctx, token)`: validasi → tulis env file → tulis unit (idempoten) →
  `enable --now` (restart bila unit sudah aktif). Dijalankan sebagai task
  taskrunner (multi-langkah, output per langkah terlihat).
- `Disconnect(ctx)`: stop + disable + `rm` unit + hapus env file + reload.
  Binary dibiarkan terinstall. Bukan task (cepat), cukup sinkron dengan audit.
- `Restart(ctx)`: `systemctl restart` via executor langsung.

### 3. Status & deteksi konflik

`Status(ctx)` mengembalikan:

- `installed`, `version`, `pinned_version` (indikator "update tersedia" bila beda)
- `token_installed` (boolean saja — token tidak pernah dikirim balik)
- `service`: active state (`active`/`inactive`/`failed`/`unknown`)
- `apt_service_running`: true bila ada unit `cloudflared.service` bawaan repo
  apt Cloudflare yang aktif → frontend menampilkan peringatan konflik (dua
  connector untuk akun yang sama bisa rebutan koneksi), tidak memblokir.

Log: `journalctl -u jenderal-cloudflared -n <n> --no-pager` (default 200
baris, dibaca langsung, bukan task).

## API (RBAC `tunnel.view` + `tunnel.manage`, admin-only — server-wide)

Semua di bawah `/api/v1`, response via `httputil.JSON` / `HandleError`:

| Method | Path | Izin | Catatan |
|---|---|---|---|
| GET | `/cloudflared` | `tunnel.view` | Status lengkap |
| POST | `/cloudflared/install` | `tunnel.manage` | Install/update binary → `{task_id}` |
| POST | `/cloudflared/connect` | `tunnel.manage` | Body `{token}` → `{task_id}` |
| POST | `/cloudflared/disconnect` | `tunnel.manage` | Sinkron |
| POST | `/cloudflared/restart` | `tunnel.manage` | Sinkron |
| GET | `/cloudflared/logs?lines=200` | `tunnel.view` | `{lines: N, output: "..."}` |

RBAC: dua permission baru di `internal/auth/rbac.go` Seed —
`{"tunnel.view", "tunnel"}` dan `{"tunnel.manage", "tunnel"}`; tidak masuk
`userRolePermissions` (bukan resource per-user). Wiring main.go: `NewService`
+ `SetTaskRunner` + field baru di `api.Dependencies`.

## Frontend

Halaman `/cloudflared` (nav baru, hanya tampil untuk admin/permission
`tunnel.view`), satu kolom:

1. **Kartu status**: badge service (running/stopped/failed), versi terinstall
   vs pin + tombol Install/Update (`bind:taskId` + `TaskProgress` +
   `storageKey="jenderal_cft_install"`), indikator token terpasang, peringatan
   konflik apt service bila ada.
2. **Kartu koneksi**: textarea token (masked, toggle show/hide, paste-only) +
   tombol Connect (`TaskProgress`, `storageKey="jenderal_cft_connect"`); bila
   aktif → tombol Disconnect (dengan konfirmasi) + Restart.
3. **Kartu log**: tail journalctl + tombol refresh.
4. **Kartu panduan** (ringkas): langkah di dashboard Cloudflare — Zero Trust →
   Networks → Tunnels → Create tunnel → copy token → paste di panel →
   tambah Public Hostname (contoh: `app.domain.com` → `HTTP://localhost:80`
   untuk website nginx; `panel.domain.com` → `HTTPS://localhost:8443` +
   *No TLS Verify* untuk panel sendiri). Termasuk catatan: panel kini bisa
   diakses tanpa IP publik lewat hostname tunnel tsb.

i18n: domain baru `web/src/lib/i18n/domains/tunnel.ts` prefix `cft.`, en + id
(kunci identik, di-enforce TypeScript), didaftarkan di `i18n/index.ts`.
Semua HTTP via `$lib/api.ts`. Status polling page memakai `api.get`.

## Error handling

- Token invalid → `NewValidationError` (400) sebelum efek apa pun.
- Connect saat binary belum ada → error jelas "install dulu"
  (`CLOUDFLARED_MISSING`, pola `EnsureInstalled` frankenphp) — atau opsi
  auto-install di dalam task connect; **pilihan: error saja**, urutan UI
  sudah memandu install dulu.
- `cloudflared: command ok tapi token ditolak Cloudflare` → task connect
  tetap sukses menulis + start, tetapi status service `failed`/`inactive`
  dan log menunjukkan error auth; frontend menampilkan log + saran cek token.
- Arch tidak didukung → pesan jelas di output task install.

## Testing (kebijakan: statis, tanpa menjalankan panel)

- `go test`: validasi token (valid, bukan base64, base64 bukan JSON, JSON
  tanpa field), golden test `RenderUnit()`, install script (arch x86_64,
  aarch64, unsupported), parsing `cloudflared version`, state status dengan
  MockExecutor (mengikuti pola test modul lain).
- Frontend: `npm run check` 0 error, `npm test`, `npm run build`.
- Live test Ubuntu 24.04 oleh maintainer (install → connect token nyata →
  public hostname → akses dari luar; disconnect; konflik apt service).

## Yang tidak untuk v1 (YAGNI)

- Manajemen ingress/public hostname dari panel (butuh API token Cloudflare +
  integrasi API; dilakukan di dashboard CF).
- Quick tunnel `trycloudflare` (tanpa akun, tidak untuk produksi).
- Multiple tunnel per server.
- Alur `cloudflared tunnel login` / cert.pem / `cloudflared tunnel info`.
- Metrics lokal cloudflared (`--metrics`); status cukup dari service + log.
