# Reverse Proxy + Supervisor — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:subagent-driven-development atau executing-plans. Steps memakai checkbox. Dua fitur independen, dieksekusi berurutan (A lalu B).

**Goal:** (A) tipe website `reverse-proxy` dengan upstream arbitrer; (B) modul supervisor untuk proses kustom berbasis systemd.

**Tech Stack:** Go (chi, executor, taskrunner — tidak dipakai untuk supervisor: pola queue worker sinkron), SQLite migration, SvelteKit 5 + Tailwind tokens, i18n en+id.

**Specs:** `docs/superpowers/specs/2026-09-25-reverse-proxy-design.md`, `docs/superpowers/specs/2026-09-25-supervisor-design.md`

## Global Constraints

- Semua command via executor (`RunSudo`/`RunSudoWithInput`); tidak pernah `sh -c` dengan input user tanpa validasi ketat.
- Nginx config & unit systemd dirender murni (fungsi pure, golden-testable); input user ke config divalidasi regex ketat.
- i18n EN+ID wajib; Tailwind tokens; RBAC seed via `INSERT OR IGNORE` (idempoten).
- Validasi statis saja: `go build/vet/test`, `npm run check/test/build` (kebijakan repo).

---

## Feature A — Reverse Proxy

### Task A1: Model, migration, profil, validasi upstream

**Files:** `internal/model/models.go` (Website: `ProxyScheme/ProxyHost string`, `ProxyPort int` + json tags), `internal/database/migrations/038_website_reverse_proxy.sql` (3× ALTER TABLE ADD COLUMN, gaya 019/020 — cek dulu gaya idempotensi yang dipakai), `internal/website/profiles.go` (case `reverse-proxy` di ResolveProfile; `ValidNginxProfiles` += `"reverse-proxy"`; `NginxProfileFor` branch appType/framework `reverse-proxy` → `"reverse-proxy"`; `websiteProfileOptions` tambah 1 entri ConfigOnly + skip PHP-compat loop untuk template ini), `internal/website/service.go` (CreateRequest 3 field baru; Create: PHPVersion="" untuk reverse-proxy + `ValidateUpstream`; scan kolom baru di semua query SELECT/scan website), `internal/website/upstream.go` BARU (`ValidateUpstream(scheme,host,port)` + `parseUpstream`).

- [ ] Test dulu: `internal/website/upstream_test.go` — host valid (domain, multi-label, IPv4, IPv6), invalid (kosong, spasi, `/`, `:`, `%`, `;`, label panjang), port 0/70000, scheme salah.
- [ ] Implement, `go test ./internal/website/` (test sqlite-dependent yang sudah gagal cgo diabaikan — samakan dengan baseline).
- [ ] Commit: `feat(proxy): reverse proxy profile with upstream validation`

### Task A2: Template vhost

**Files:** `internal/website/templates.go` — `VhostData` += `ProxyScheme string; ProxyHost string; ProxyPort int`; case `directivesForProfile` `"reverse-proxy"`: bila host kosong/port≤0 → fallback statis; else satu `location /` proxy_pass `<scheme>://<host>:<port>` + headers (Host/X-Real-IP/X-Forwarded-*) + websocket map (pola `app-proxy`, TANPA `try_files`/`@app`). `regenerateConfig` (service.go:1272-1287) isi field baru dari `w`.

- [ ] Test golden: render vhost reverse-proxy → berisi `proxy_pass http://10.0.0.5:3000;`, map ws, tanpa `try_files`.
- [ ] Commit: `feat(proxy): nginx vhost rendering for reverse proxy`

### Task A3: Endpoint PUT /websites/{id}/proxy

**Files:** `internal/website/service.go` (`SetProxy(ctx, id, scheme, host, port) (model.Website, error)` — Lock → ValidateUpstream → UPDATE 3 kolom+updated_at → Get → regenerateConfig), `internal/website/handler.go` (`SetProxy` handler pola SetNginxProfile), `internal/api/router.go` (setelah line ~397: `Put("/websites/{id}/proxy", websiteHandler.SetProxy)`).

- [ ] Test handler: body invalid → 400. Commit: `feat(proxy): update upstream endpoint`

### Task A4: Frontend

- [ ] `web/src/lib/website-form.js`: `WebsiteSelection` += `proxy_scheme:'http', proxy_host:'', proxy_port:''`; `normalizeWebsiteSelection` clear field proxy untuk non-reverse-proxy, clear framework_version dsb untuk reverse-proxy.
- [ ] `web/src/routes/websites/+page.svelte`: `<option value="reverse-proxy">` di select template; blok `{#if selection.template === 'reverse-proxy'}` (scheme select http/https, host input, port number) pola blok laravel (line ~373-408); guard tombol create: `!['static','reverse-proxy'].includes(selection.template) && !selection.php_version`; payload create sertakan 3 field.
- [ ] Detail `/websites/[id]` tab Config: kartu Upstream (tampil bila `website.app_type === 'reverse-proxy'`) form scheme/host/port → `api.put('/api/v1/websites/'+id+'/proxy', ...)`.
- [ ] i18n `wl.ts` (form create) + domain config (cari prefix config tab) EN+ID.
- [ ] `npm run check && npm test && npm run build`. Commit: `feat(proxy): reverse proxy website type UI`

## Feature B — Supervisor

### Task B1: Migration, model, service

**Files:** `internal/database/migrations/039_supervisor_processes.sql` (pola 011_queue_workers.sql), `internal/model/models.go` (`SupervisorProcess`), `internal/supervisor/service.go` BARU (pola queue/service.go): `RenderProcessUnit(p)` murni (EnvironmentFile `/etc/jenderal/proc/<id>.env`, `ExecStart=/bin/bash -lc '<escaped>'`, `User=<run_as>`, Restart=always|no), `renderProcEnv(p)`; validasi: name `^[a-zA-Z0-9][a-zA-Z0-9._-]{0,63}$`, command non-kosong tanpa newline, working_dir kosong atau `^/[\w./-]*$`, run_as `^[a-z_][a-z0-9_-]{0,31}$` ≠ root, env baris `^[A-Za-z_][A-Za-z0-9_]*=`; Create (insert ULID → env via RunSudoWithInput tee 0600 → unit heredoc → daemon-reload → enable --now), List (live `systemctl is-active` per unit), Get, Update (rewrite env+unit, restart bila aktif), Delete (stop/disable/rm unit+env/daemon-reload/delete row), Action, Logs (journalctl, cap 500).

- [ ] Test: validator + golden unit (auto-restart on/off, quoting command) + MockExecutor create/update/delete/action. Commit: `feat(supervisor): process supervisor service`

### Task B2: Handler, router, RBAC, wiring

**Files:** `internal/supervisor/handler.go` (pola queue/handler, audit Module "supervisor"), router: `SupervisorSvc *supervisor.Service` + handler konstruksi + blok route `/supervisor*` (`supervisor.view`/`supervisor.manage`); rbac.go append 2 permission; main.go `supervisorSvc := supervisor.NewService(db, exec, auditSvc)` + Dependencies.

- [ ] `go build ./... && go vet ./...`; test handler invalid → 400. Commit: `feat(supervisor): API routes and RBAC`

### Task B3: Frontend

- [ ] i18n `domains/supervisor.ts` prefix `supp.` (en+id) + register index.ts + `nav.supervisor` di core.ts.
- [ ] `web/src/routes/supervisor/+page.svelte` (pola dashboard card + tabel actions): list proses (status badge live, tombol start/stop/restart/log/delete), form create/edit inline, log panel `<pre>`.
- [ ] `+layout.svelte`: nav Operations `{ href: '/supervisor', permission: 'supervisor.view', labelKey: 'nav.supervisor', icon: 'cpu' }` + case ikon cpu.
- [ ] check/test/build. Commit: `feat(supervisor): supervisor page UI`

### Task B4: Final

- [ ] `go vet ./... && go test ./internal/website/ ./internal/supervisor/ ./internal/cloudflared/`; frontend penuh; build binary (replikasi make build). Push ke main.
