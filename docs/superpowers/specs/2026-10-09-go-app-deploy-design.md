# Design: Deploy aplikasi Go semudah Laravel (one-click build on deploy)

Status: draft for review
Date: 2026-10-09
Melengkapi: `2026-09-12-app-runtimes-design.md` (fase 2 Go, setengah terimplementasi)

## Latar belakang

Fondasi aplikasi Go di panel sudah ada sejak spec app-runtimes: template
`go-build`/`go-binary` di form tambah site, installer toolchain Go ter-pinel
(`internal/goruntime`, 1.27.1), profil nginx `app-proxy` (proxy ke
`127.0.0.1:<AppPort>` 8200–8299), unit systemd `jenderal-app-<id>.service`,
dan tab **App** (start/stop/restart/build). Yang ada saat ini:

- Worker deployment (`internal/deployment/service.go`) adalah clone/pull
  murni — tidak memicu build atau restart apa pun.
- Template go tidak mengisi `AppBuildCommand`/`AppStartCommand`; user harus
  mengetiknya sendiri di tab App.
- `buildBinaryAppUnit` (`internal/website/app.go`) tidak meng-inject
  `Environment=PORT=...` — aplikasi user harus meng-hardcode port alokasi
  panel di kodenya.
- `goEnsureInstalled` hanya memeriksa; build gagal dengan
  `GO_TOOLCHAIN_MISSING` bila Go belum diinstall manual.
- Repo site go di-clone ke docroot `<home>/public`; binary hasil build di
  docroot dapat diunduh via browser (`try_files` melayani file statis,
  hanya dotfile yang di-deny).

Tujuan fitur ini: alur **buat site go-build → pasang deploy key → Deploy
(tombol/webhook/zip) → panel otomatis build → restart → live**, tanpa satu
pun langkah manual di antaranya — setara dengan pengalaman deploy Laravel.

Keputusan yang sudah disetujui maintainer (sesi 2026-10-09):

1. **Full otomatis** — build-on-deploy selalu aktif untuk site go (tanpa
   toggle per site).
2. **Pendekat A: post-deploy hook** di worker deployment — log build masuk
   ke catatan deployment yang sama; bukan pipeline "release" terpisah.
3. **Non-regresi mutlak** untuk fitur yang sudah stabil (PHP Laravel,
   CodeIgniter, WordPress, static) — lihat bagian "Prinsip non-regresi".

## Prinsip non-regresi (kebutuhan maintainer, tidak dapat dinegosiasikan)

- Semua perilaku baru di-gate ketat pada `AppType == "go"`. Worker
  deployment site non-go mengeksekusi cabang `if` yang tidak pernah terpenuhi;
  urutan langkah clone/pull/preserve/chown/ACL mereka identik dengan hari ini.
- Tidak ada perubahan pada: template vhost PHP/Laravel/CodeIgniter, PHP-FPM
  pool rendering, command presets composer/artisan/npm, alur preserve
  `.env`+`storage` Laravel, maupun endpoint deployment yang ada.
- Guard test: unit test yang menegaskan deploy site `laravel` dan
  `codeigniter4` **tidak memanggil** hook build sama sekali.
- Acceptance manual di VPS uji: satu deploy Laravel + satu CodeIgniter4
  sebelum dan sesudah perubahan — hasil harus identik.

## 1. Layout situs Go (paritas konvensi Laravel)

Status quo untuk site non-Laravel: `projectRoot = docRoot` sehingga repo go
mendarat di `<home>/public` bersama binary-nya. Desain baru meniru konvensi
Laravel ("sites live at `<home>/app`"):

- Repo: **`<home>/app`**. Docroot: **`<home>/app/public`**
  (`RelativeDocumentRoot: "app/public"` untuk kedua template go).
- Bila repo punya direktori `public/` (css/js), nginx otomatis melayaninya
  via `try_files` — perilaku app-proxy yang sudah ada, tidak berubah.
- Binary hasil build ada di root repo — **di luar docroot** — sehingga tidak
  bisa diunduh via HTTP; `.git` tetap terlindung deny dotfile yang ada.
- `.env` opsional di root repo, dibaca unit via `EnvironmentFile`.

Penentuan `projectRoot` di `internal/deployment/service.go` (hari ini
baris ~246–254) mendapat kondisi tambahan `appType == "go"` →
`homeDir + "/app"`. Kondisi lift Laravel yang ada tidak disentuh.

**Fallback situs lama:** bila `<home>/public/.git` sudah ada (site go
dibuat versi sekarang), worker tetap memakai docroot sebagai project root.
Deteksi via satu probe `test -d`; helper dibagikan supaya deployment, unit
builder, dan build argv sepakat tentang lokasi:

```go
// internal/siteops
func AppProjectRoot(ctx, exec, homeDir, docroot string) (root string, legacy bool)
```

Deployment dan `internal/website` sama-sama sudah mengimpor `siteops`.

## 2. Provisioning & toolchain

- Template `go-build` mengisi default saat Create:
  `AppBuildCommand = "go build -o app ."`, `AppStartCommand = "./app"`
  (keduanya tetap bisa diedit di tab App). `go-binary`: start command
  default `"./app"` (binary hasil build lokal di-rename `app`, atau user
  mengubah start command), build command kosong.
- **Alokasi port saat provisioning** (baru; hari ini baru terjadi di
  `SaveAppService`): provisioning site go mengalokasikan `AppPort`
  (range 8200–8299 yang sudah ada), merender vhost `app-proxy` **dengan**
  port (status quo merender tanpa proxy bila port 0), dan menulis unit —
  tetapi **tidak menstart** unit sebelum ada binary (menghindari
  crash-loop `Restart=always` di site kosong). Unit di-enable; deploy hook
  yang menstart setelah build pertama sehat.
- Provisioning `go-build` meng-install toolchain Go bila belum ada —
  reuse `goruntime.Install()` sebagai tahapan provision (paritas dengan
  PHP yang di-install saat provisioning). `go-binary` tidak butuh
  toolchain.
- `goEnsureInstalled` berubah dari "laporkan `GO_TOOLCHAIN_MISSING`"
  menjadi **"install bila belum ada"** (probe `/usr/local/go/bin/go
  version`; bila gagal, jalankan installer via task runner, log ke output
  task pemanggil). Dipakai oleh provisioning, build manual (tab App), dan
  deploy hook. Halaman install manual `/goruntime` tetap tersedia.

## 3. Post-deploy hook (inti fitur)

Titik sisip: akhir `deploy()` di `internal/deployment/service.go`, setelah
`RestoreServingAccess` sukses dan **sebelum** deployment ditandai success
(sharing `SyncOwnedWebsites` tetap berjalan setelah penandaan success,
sesuai urutan kode hari ini — tidak diubah). Struktur:

```
bila AppType != "go" → selesai seperti hari ini (tanpa perubahan apa pun)
bila AppType == "go":
  1. resolve project root (helper AppProjectRoot, lihat bagian 1)
  2. go-build: validasi go.mod ada di root
     → tidak ada: deployment failed, pesan "bukan proyek Go / go.mod
       tidak ditemukan", binary lama tetap berjalan
  3. pastikan toolchain (install bila belum ada; progres masuk log
     deployment)
  4. build sebagai web_user:
     sudo -u <u> bash -c 'cd <root> && PATH=/usr/local/go/bin:$PATH go build -o app .'
     (dua perbaikan bug laten: script build hari ini tidak memasukkan
     /usr/local/go/bin ke PATH, dan pemanggil `RunSudo("-u", args...)`
     dengan `args=["/bin/bash","-c",script]` menghasilkan argv
     `sudo -u /bin/bash -c …` — sudo menganggap "/bin/bash" sebagai nama
     user; argv build harus penuh `-u <webuser> -- /bin/bash -c …`)
     — `go build -o` build di temp lalu rename → atomik; proses lama
       tetap memakai inode lama selama build
  5. build gagal → deployment failed dengan output compiler di log;
     binary lama tetap melayani (zero-downtime failure)
  6. restart unit (reuse `AppAction("restart")`; unit baru yang belum
     pernah jalan → `start`)
  7. health check: TCP dial 127.0.0.1:<AppPort> hingga 30 detik
     (poll 1 detik) → sukses: log "app healthy on port <N>",
     deployment success
  8. health gagal → deployment failed; log mengarahkan ke log unit
     (tab App / jenderal-app-<id>) — binary baru sudah aktif di sini,
     crash-loop diawasi systemd seperti biasa
```

- `go-binary`: hook hanya langkah 1, 6, 7 (restart + health, tanpa build).
- Build command yang dieksekusi adalah `AppBuildCommand` milik site —
  sama persis dengan tombol Build di tab App (reuse `buildAppBuildArgv`
  + `RunAppBuildTask` dibekukan jadi fungsi yang bisa dipanggil hook
  dengan writer log deployment).
- Webhook `WebhookDeploy` dan upload zip otomatis mendapat perilaku yang
  sama karena melewati worker deployment yang sama.
- Deployment tetap satu baris riwayat dengan status
  success/failed/cancelled yang sudah ada — tahapan hanya terlihat sebagai
  bagian log. **Tidak ada migrasi DB di fitur ini.**
- Timeout deployment 15 menit yang sudah ada cukup (build Fiber kecil
  ±1–2 menit; instal toolchain pertama ±1–2 menit lagi).

## 4. Unit systemd

`buildBinaryAppUnit` cabang go berubah:

```ini
[Service]
User=<web_user>
WorkingDirectory=<project root>          # <home>/app, bukan docroot
Environment=PORT=<AppPort>
EnvironmentFile=-<project root>/.env     # opsional, boleh tidak ada
ExecStart=<project root>/app
Restart=always
RestartSec=5
```

Konsekuensi untuk kode user (didokumentasikan di UI):

```go
app.Listen(":" + os.Getenv("PORT"))   // Fiber; tidak ada port hardcode
```

Catatan format `.env`: format systemd `EnvironmentFile` adalah `KEY=val`
satu per baris (tanpa `export`, tanpa kutip shell) — dinyatakan eksplisit
di hint UI karena berbeda dari `.env` Laravel.

## 5. API & frontend

Tidak ada endpoint baru. Perubahan UI:

- **Tab App** (`web/src/lib/components/WebsiteAppSection.svelte`): tampilkan
  `PORT` yang dialokasikan secara mencolok + snippet Fiber siap-salin
  (`app.Listen(":" + os.Getenv("PORT"))`) + hint format `.env`. Status
  unit, edit build/start command, tombol Build — sudah ada, tetap.
- **Tab Deployment**: badge "Build otomatis aktif" untuk site go
  (`app_type == "go"`); riwayat deployment sudah menampilkan status + log.
- **Form tambah site** (`web/src/routes/websites/+page.svelte`): saat
  template `go-build` dipilih, tampilkan hint singkat alur satu-klik
  (deploy → build otomatis → live) + pengingat deploy key.
- Semua string baru: kunci i18n EN **dan** ID di
  `web/src/lib/i18n/domains/wl.ts` (mengikuti pola kunci `wl.*`).

## 6. Testing & acceptance

Backend (`go test ./...`, sesuai kebijakan tidak menjalankan panel):

- `internal/website/profiles_test.go`: default build/start command
  go-build & go-binary; `RelativeDocumentRoot == "app/public"`.
- `internal/website/app_test.go` (unit render): `Environment=PORT=`,
  `EnvironmentFile=-`, `WorkingDirectory` = root repo — berlaku sama untuk
  `go` dan `go-binary` (keduanya menerima PORT agar konsisten).
- `internal/website/appargs_test.go`: build argv go mem-prepend
  `/usr/local/go/bin` ke PATH.
- `internal/deployment/service_test.go`: (a) site `laravel` dan
  `codeigniter4` → hook tidak dipanggil (guard non-regresi); (b) site go →
  hook terpanggil setelah restore akses; (c) build gagal → deployment
  failed, restart tidak dipanggil; (d) `AppProjectRoot`: go →
  `<home>/app`; `.git` di `<home>/public` → legacy docroot.
- `goEnsureInstalled`: mock executor — probe sukses → tidak install;
  probe gagal → installer terpanggil.

Frontend: `npm run check` (0 error), `npm run build`, `npm test` — statis
saja.

Acceptance manual maintainer di VPS Ubuntu (24.04/26.04):

1. **Regresi**: deploy Laravel + CodeIgniter4 seperti biasa → tidak ada
   perbedaan perilaku apa pun.
2. Site go lama (bila ada) masih jalan via fallback legacy path.
3. **Skenario inti**: buat site `go-build` → pasang deploy key → push repo
   Fiber minimal (`app.Listen(":"+os.Getenv("PORT"))`) → Deploy →
   deployment success, log menampilkan clone/build/restart/healthy →
   domain melayani app (HTTP + HTTPS setelah SSL).
4. Build rusak (syntax error) → deployment failed, app lama tetap melayani.
5. Webhook memicu hal yang sama.
6. `go-binary`: upload binary bernama `app` → jalan tanpa build.

## Batasan & yang sengaja di luar scope (v1)

- **Hanya runtime `go`** yang diaktifkan. Mekanisme hook ditulis generik
  per app_type, tetapi node/python/deno/bun belum diaktifkan — menyusul
  terpisah supaya tidak menambah permukaan risiko regresi.
- Tanpa swap sementara untuk build (pola self-update tidak direplikasi);
  build Fiber kecil lolos di 1GB. Bila terbukti perlu, tindak lanjut
  terpisah.
- Tanpa rollback otomatis multi-versi (binary lama hanya "tetap berjalan
  saat build gagal"; tidak ada simpanan N rilis terakhir).
- Tanpa zero-downtime handoff (restart systemd = jeda ~detik; paritas
  dengan queue workers).
- Masalah diketahui (pra-eksis, tidak ditangani di sini): range port app
  8200–8299 bertumpukan dengan range admin-port Octane (octane port+100);
  `ss`-check allocator yang saat ini mencegah tabrakan aktif.

## File yang disentuh (ringkasan)

| File | Perubahan |
|---|---|
| `internal/deployment/service.go` | gate + panggil hook; lift projectRoot go (dengan fallback legacy) |
| `internal/siteops/` (func baru) | `AppProjectRoot(ctx, exec, homeDir, docroot)` |
| `internal/website/profiles.go` | default command + `RelativeDocumentRoot` kedua template go |
| `internal/website/provisioner.go` | tahap: alokasi AppPort + render vhost + tulis unit (tanpa start) + install toolchain untuk go-build |
| `internal/website/app.go` | `buildBinaryAppUnit` (PORT/EnvironmentFile/WorkingDirectory); `goEnsureInstalled` jadi install-if-missing; `RunAppBuildTask` refactor jadi bisa dipanggil hook dengan writer sendiri |
| `internal/website/appargs.go` | prepend PATH `/usr/local/go/bin` |
| `internal/goruntime/service.go` | tidak berubah (dipakai ulang) |
| `web/src/lib/components/WebsiteAppSection.svelte` | PORT + snippet + hint .env |
| `web/src/routes/websites/[id]/+page.svelte` | badge build-otomatis tab Deployment |
| `web/src/routes/websites/+page.svelte` | hint template go-build |
| `web/src/lib/i18n/domains/wl.ts` | kunci EN+ID baru |

Tidak ada migrasi database.
