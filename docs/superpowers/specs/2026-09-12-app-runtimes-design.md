# Design: App Runtimes — deploy Node.js, Go, Python, Deno/Bun lewat panel

Status: draft for review
Date: 2026-09-12

## Latar belakang

Panel sudah punya semua bahan untuk menjalankan aplikasi *long-running*
(server) di belakang nginx, terbukti dari fitur Laravel Octane:

- Port loopback per situs 8100–8199 dengan allocator (octane.go).
- Vhost nginx reverse-proxy dengan websocket map (profil `laravel-octane`).
- Unit systemd per situs (`jenderal-octane-*`), pola yang sama dipakai queue
  workers (`jenderal-queue-*`).
- Runtime Node.js per-user via NVM + `nvm-exec` (tanpa login shell).
- Deploy: git (deploy key + webhook), upload zip, dan preset `git pull`.

Yang belum ada: konsep **"App Service" generik** — aplikasi non-PHP yang
menjalankan server proses panjang, dengan build command, start command, env
vars, dan manajemen siklus (start/stop/restart/log) di satu tempat.

## Penilaian kerealistisan per teknologi

| Teknologi | Realistis? | Alasan | Cara jalan di panel |
|---|---|---|---|
| **Node.js** (Express, Fastify, NestJS, Hono) | ✅ Sangat | Infra NVM per-user sudah ada | `nvm-exec npm run start` sebagai service systemd |
| **Next.js / Nuxt** (SSR) | ✅ Sangat | Sama di atas + build step | build (`npm run build`) saat deploy, start (`next start` / `node .output/server/index.mjs`) sebagai service |
| **Go** (Gin, Fiber, Echo, chi) | ✅ Ya, dua jalur | Binary statis = paling mudah di-supervise; build di server butuh toolchain Go terkelola | (a) upload binary Linux hasil `go build`, (b) toolchain Go ter-pinel di server untuk `go build` |
| **Deno / Bun** | ✅ Ya | Runtime single-binary; pola instal ter-pin + checksum sama dengan frankenphp/NVM | Instal runtime ter-pinel, `deno task start` / `bun run start` |
| **Python** (FastAPI, Flask, Django) | ✅ Ya | `python3` + venv per situs + pip; gunicorn/uvicorn sebagai server | venv di home situs, `pip install -r requirements.txt`, start via uvicorn/gunicorn |
| Java / Kotlin (Spring Boot) | ⛔ Tunda | JVM + Maven/Gradle berat (RAM, toolchain), jarang di VPS kecil | — |
| .NET, Ruby, Elixir | ⛔ Tunda | Niche untuk pengguna target panel | — |
| Docker-based apps | ⛔ Tunda | Modul Docker ada, tapi "containerize anything" adalah fitur terpisah yang jauh lebih besar | — |

**Kesimpulan:** Node.js dan Go adalah dua yang paling realistis dan bernilai;
Python menyusul; Deno/Bun bonus murah. Java/.NET tidak untuk v1.

## Arsitektur inti: "App Service" generik

Satu implementasi melayani semua runtime — menggeneralisasi pola Octane:

- `websites` kolom baru: `app_port` (allocator umum, generalisasi 8100–8199),
  `app_start_command`, `app_build_command`, `app_runtime`
  (`node` / `go` / `python` / `deno` / `bun`), `app_env` (reuse pola .env).
- Unit systemd `jenderal-app-<websiteID>.service`: User, WorkingDirectory,
  ExecStart via wrapper runtime (nvm-exec / binary go / venv bin / deno),
  `Restart=always`, EnvironmentFile dari panel.
- Nginx: profil `reverse-proxy` generik (generalisasi profil laravel-octane):
  proxy ke `127.0.0.1:<app_port>` + websocket map + **opsi SPA fallback**
  (`try_files $uri /index.html`) untuk aplikasi frontend SSR/SPA.
- Deploy mengalir lewat fitur yang sudah ada: git webhook → build command →
  restart service; upload-deploy untuk binary/zip.
- Log: `journalctl -u jenderal-app-<id>` (pola queue workers), tab log
  per situs.
- Env vars: editor .env generik (pola Laravel .env yang sudah ada) per app.

## Fase implementasi

1. **Fase 1 — Core + Node.js**: generalisasi port allocator + profil proxy +
   unit manager; template `node` (start command bebas), `next`, `nuxt`;
   deploy git/upload + webhook build+restart; env editor. (Nilai tertinggi,
   infra NVM tinggal dipakai.)
2. **Fase 2 — Go**: modul toolchain Go ter-pinel + checksum (pola
   frankenphp); template `go-build` (go build saat deploy) dan
   `go-binary` (upload binary, chmod + systemd).
3. **Fase 3 — Python**: venv per situs + pip; template `fastapi`
   (uvicorn), `flask`/`django` (gunicorn).
4. **Fase 4 — Deno/Bun**: instal runtime ter-pinel per server + template
   start command.

## Yang tidak untuk v1

- Multi-container / docker-compose apps (modul Docker terpisah).
- Java/.NET/Ruby (lihat tabel).
- Zero-downtime blue-green (restart cepat sudah cukup; socket-activation
  bisa jadi peningkatan nanti).
- Database terkelola per app (modul database sudah ada untuk ini).

## Testing

- Pure: allocator port, builder unit systemd + argumen runtime (nvm-exec /
  venv), validasi start/build command (whitelist biner), template nginx.
- MockExecutor: alur build→restart, status unit.
- Live: node/go/python app nyata di Ubuntu 24.04 oleh maintainer (kebijakan
  testing).
