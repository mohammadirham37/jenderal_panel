# Design: Reverse Proxy — tipe website dengan upstream bebas

Status: draft for review
Date: 2026-09-25

## Latar belakang

Panel sudah punya profil nginx `app-proxy`, tetapi upstream-nya **hardcoded** ke
`http://127.0.0.1:<AppPort>` milik situs itu sendiri (templates.go:359-370) — hanya
berguna untuk app service yang dikelola panel. Belum ada cara mem-proxy domain ke
target arbitrer: app di port lokal lain, service di host lain dalam LAN, atau host
eksternal.

Fitur ini menambahkan **tipe website `reverse-proxy`**: dibuat lewat alur website
normal sehingga mewarisi domain (addon domain), SSL (lego), ForceHTTPS, ownership,
dan ScopeMiddleware secara gratis; yang baru hanyalah konfigurasi upstream.

## Keputusan (disetujui via sesi brainstorming; user memilih opsi rekomendasi)

- Bentuk: **tipe website**, bukan modul terpisah — domain/SSL/ownership sudah ada
  di modul website.
- Upstream: **bebas** — `scheme` (`http`/`https`) + `host` (hostname/IP) + `port`;
  `127.0.0.1:port` untuk lokal.

## Backend

### Model & migration (`038_website_reverse_proxy.sql`)

Kolom baru di `websites`: `proxy_scheme TEXT DEFAULT ''`, `proxy_host TEXT DEFAULT ''`,
`proxy_port INTEGER DEFAULT 0`. Field JSON di `model.Website`:
`ProxyScheme/ProxyHost/ProxyPort`.

### Profil & template

- `profiles.go`: template baru `reverse-proxy` di `ResolveProfile` →
  `AppType = "reverse-proxy"`, `NginxProfile = "reverse-proxy"`; masuk
  `ValidNginxProfiles`. `websiteProfileOptions()` menambahkan opsi dengan
  `SetupMode = SetupConfigOnly` (tidak ada provisioning file).
- `templates.go`: `VhostData` dapat `ProxyScheme/ProxyHost/ProxyPort`; case
  `reverse-proxy` di `directivesForProfile` merender upstream
  `<scheme>://<host>:<port>` memakai blok proxy + websocket map yang sudah
  generik (identik pola `app-proxy`, tanpa `try_files` statis — semua location
  diteruskan ke upstream).
- Provisioner: tipe ini tanpa docroot/PHP pool — mengikuti jalur situs statis
  dengan docroot placeholder (folder kosong milik web user) agar alur existing
  (permission, addon domain) tidak perlu diubah.

### Validasi (masuk config nginx → ketat, murni Go, unit-testable)

- `scheme`: `http` | `https`.
- `host`: regex hostname (RFC label) atau IPv4/IPv6 literal — tanpa spasi, `/`,
  `:`, `%`, `;` (mencegah injeksi config nginx).
- `port`: 1–65535.
- Create/Update menolak `reverse-proxy` tanpa triple upstream lengkap
  (`NewValidationError`).

### API

- `Create` menerima `ProxyScheme/ProxyHost/ProxyPort` saat template
  `reverse-proxy`.
- `PUT /websites/{id}/proxy` (izin `websites.update`, kena ScopeMiddleware):
  ubah upstream → validasi → persist → regenerate vhost (reuse alur
  `SetNginxProfile`) → reload nginx. Response: website terbaru.
- `GET /websites` dan detail sudah mengembalikan field baru via model.

## Frontend

- Form create (halaman `/websites`): pilihan tipe **Reverse Proxy** dengan tiga
  input (Scheme select, Host, Port) muncul saat tipe dipilih; kirim field baru.
- Daftar website: badge tipe `reverse-proxy`.
- Detail website (`/websites/[id]`): kartu **Upstream** di tab Config berisi
  scheme/host/port saat ini + form edit (panggil `PUT /proxy`), info bahwa SSL
  edge/Cloudflare atau SSL panel tetap bisa dipakai seperti situs lain.
- i18n: keys baru di domain `ws`/`wssections` yang berlaku (prefix ada),
  EN + ID.

## Testing

- Unit: validasi upstream (valid host/IP/port/scheme; invalid karakter, port 0,
  port 70000, host kosong); rendering `directivesForProfile reverse-proxy`
  (proxy_pass target benar, websocket map muncul, tidak ada `try_files` statis);
  `ResolveProfile reverse-proxy`; golden vhost.
- MockExecutor: create → vhost tertulis dengan upstream; `PUT /proxy` → vhost
  teregenterasi.
- Live (maintainer): buat situs reverse proxy ke `127.0.0.1:3000` (proses dari
  fitur Supervisor), akses domain, tes websocket.

## Yang tidak untuk v1 (YAGNI)

- Load balancing multi-upstream, health check aktif.
- Rewrite path prefix → strip prefix.
- Buffering/caching tuning per-situs.
- Upstream dengan autentikasi (header inject) — bisa menyusul lewat Config tab.
