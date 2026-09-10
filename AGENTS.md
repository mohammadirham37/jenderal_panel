# AGENTS.md — Jenderal Panel

Open-source VPS control panel for Ubuntu 22.04/24.04. Go backend + SvelteKit 5 frontend shipped as a single binary via `go:embed`. Full details in `CLAUDE.md`; module-specific notes in `docs/`.

## Layout

- `cmd/jenderal/main.go` — CLI entry, DI wiring for all services
- `internal/api/router.go` — Chi router, `Dependencies` struct, 100+ routes with RBAC middleware
- `internal/{domain}/` — one package per module: `service.go` (logic), `handler.go` (HTTP), `service_test.go`
- `internal/database/migrations/*.sql` — SQLite migrations (idempotent, tracked in `schema_migrations`)
- `web/` — SvelteKit 5 + Tailwind v4 frontend (`web/build/` is what gets embedded)

## Commands

```bash
make build                      # frontend + Go binary (embeds web/build)
make test                       # go test ./... -v -race
make lint                       # go vet + golangci-lint
go test ./internal/auth/ -run TestSeedAndAdmin -v   # single test
cd web && npm run check         # svelte-check (must pass with 0 errors)
cd web && npm run build         # frontend production build
cd web && npm test              # frontend node --test suites
```

## Architecture rules

- Dependency flow: Handler → Service → Executor/Database. Never the reverse.
- ALL system commands go through `internal/executor` `CommandExecutor`; privileged ones via `RunSudo()`, never `sh -c` with user input.
- Installs/long operations use `internal/taskrunner` (returns task ID; frontend polls `/api/v1/tasks/{id}`; Svelte pages use `bind:taskId` + `storageKey` on `TaskProgress`).
- systemd services via `internal/service` `ServiceManager` allow-list (globs like `php*-fpm` supported).
- API responses via `internal/httputil`: `JSON` → `{"data":...}`, `JSONList` adds `meta`, errors via `HandleError` + `model.NewDomainError/NewValidationError`.
- New module checklist (service → handler → router Dependencies → routes+RBAC → main.go wiring → rbac.go Seed → migration → frontend page) is in `CLAUDE.md`.
- Auth: SQLite sessions, Argon2id, 63 RBAC permissions seeded in `internal/auth/rbac.go`, CSRF double-submit on mutating requests.

## Frontend rules

- SvelteKit 5 runes mode is enforced (`$state`, `$derived`, `$effect`, `$props`; snippets for reusable markup).
- Tailwind v4 tokens are remapped per theme in `web/src/app.css` — `gray-*`/`blue-*`/`red-*` etc. are custom palettes (dark default; light theme remaps them, including `text-white`). Use these tokens, not raw colors, so both themes work.
- All user-facing strings go through `$lib/stores/language.ts` (`translate($language, key)`) — English AND Bahasa Indonesia keys are required.
- HTTP via `$lib/api.ts` (`api.get/post/...` unwraps `{data}`; `apiRaw` keeps `meta`).
- Adding UI sections that need optional data: fetch with permission-safe fallbacks (a 403 must not break the page — see dashboard `getSafe` pattern).

## Testing policy (important)

The maintainer tests the running panel manually on Ubuntu 24.04. Do NOT launch the app, mock its API, or do browser-based testing. Stop at static validation: `npm run check`, `npm run build`, `npm test`, `go test ./...`. The backend targets Linux (systemd, ufw, nginx) and will not run correctly on macOS.

## Gotchas

- Frontend changes only reach the binary after `make build` (embeds `web/build/`).
- `vite` dev server proxies `/api` and `/ws` to `localhost:8443` (the Go server port).
- Background workers: `Start()` methods must `go func()` (never block main); `net.Listen` before signal handling.
- `ServiceStatus.Uptime` is a Go `time.Duration` → JSON nanoseconds; format `/1e9` on the frontend.
