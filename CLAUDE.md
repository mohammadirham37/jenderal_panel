# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

Jenderal Panel is an open-source VPS control panel for Ubuntu Linux (22.04/24.04). Go backend + SvelteKit frontend deployed as a single binary via `go:embed`. Target: manage websites, databases, services, SSL, Docker, and more through a web GUI.

## Build & Run

```bash
# Full build (frontend + Go binary)
make build

# Dev mode (Go backend only, expects separate frontend dev server)
make dev

# Frontend only
cd web && npm install && npm run build

# Run tests
make test          # go test ./... -v -race
make lint          # go vet + golangci-lint

# Single package test
go test ./internal/auth/ -v -race
go test ./internal/auth/ -run TestSeedAndAdmin -v

# Frontend tests
cd web && npm test
```

The binary embeds the frontend: `web/build/` is copied to `cmd/jenderal/web_build/` before `go build`. The Makefile handles this automatically.

## Architecture

Monolith layered Go application. 40+ internal packages following a consistent pattern:

```
cmd/jenderal/main.go          # CLI entry, DI wiring, all services constructed here
internal/api/router.go         # Chi router, Dependencies struct with 40+ fields, 100+ routes
internal/{domain}/service.go   # Business logic, DB queries, executor calls
internal/{domain}/handler.go   # HTTP handler, decode request → call service → JSON response
internal/{domain}/service_test.go  # Tests with MockExecutor and in-memory SQLite
```

**Dependency flow:** Handler → Service → Executor/Database. Never the reverse.

### Key Interfaces

**CommandExecutor** (`internal/executor/command.go`): All system commands go through this. Returns `(*Result, error)` where Result has Stdout, Stderr, ExitCode. Non-zero exit code is NOT an error. `RunSudo` prepends `/usr/bin/sudo`. MockExecutor available for tests.

**ServiceManager** (`internal/service/systemd.go`): Manages systemd services with an allow-list. Glob patterns supported (e.g. `php*-fpm`).

**TaskRunner** (`internal/taskrunner/runner.go`): Background task execution with live output streaming and SQLite persistence. Used for all install operations (PHP, databases, Docker, updates). Returns task ID immediately; frontend polls `GET /api/v1/tasks/{id}` for progress.

### Auth & RBAC

Server-side sessions in SQLite. Argon2id password hashing. 63 RBAC permissions seeded in `internal/auth/rbac.go` Seed(). Session cookie (`HttpOnly`, `Secure`, `SameSite=Strict`) or Bearer token auth. CSRF via double-submit cookie on mutating requests.

### Database

SQLite with WAL mode. Migrations in `internal/database/migrations/*.sql`, tracked in `schema_migrations` table. Each migration runs in a transaction. Use `CREATE TABLE IF NOT EXISTS` and `INSERT OR IGNORE` for idempotency.

### Response Patterns

All API responses use `internal/httputil/`:
- `httputil.JSON(w, status, data)` → `{"data": ...}`
- `httputil.JSONList(w, data, page, perPage, total)` → `{"data": ..., "meta": {...}}`
- `httputil.HandleError(w, err)` → converts `*model.DomainError` to appropriate HTTP status
- Domain errors: `model.NewDomainError(code, message, err)`, `model.NewValidationError(message)`

### Frontend

SvelteKit 5 (runes syntax: `$state`, `$effect`, `$props`), Tailwind CSS v4, adapter-static. Dark theme default. API client at `web/src/lib/api.ts`. Language store supports English and Bahasa Indonesia. `TaskProgress` component polls task status and shows live terminal output.

### Self-Update

`/update` page triggers `internal/update/service.go` which runs a bash script: git pull → npm build → go build → backup → replace → systemd restart. All via TaskRunner with live output.

**Deployment workflow:** after work is pushed to `main`, the maintainer deploys by clicking Update on the `/update` page in the running panel — never `git pull` or build manually on the server.

## Adding a New Module

1. Create `internal/{name}/service.go` with business logic
2. Create `internal/{name}/handler.go` with HTTP handlers using `httputil`
3. Add to `api.Dependencies` struct in `internal/api/router.go`
4. Wire routes in `NewRouter()` with RBAC middleware
5. Construct service in `cmd/jenderal/main.go` `cmdServe()`
6. Add RBAC permissions in `internal/auth/rbac.go` Seed()
7. Add migration if DB tables needed
8. Add frontend page in `web/src/routes/{name}/+page.svelte`

## Important Conventions

- All privileged operations via `executor.RunSudo()`, never `sh -c` with user input
- Background installs use `taskrunner.RunMultiple()` with `DPkg::Lock::Timeout=120`
- `Start()` methods on background workers MUST use `go func()` (not block main thread)
- `net.Listen` before signal handling in server startup (avoid race condition)
- Svelte pages use `bind:taskId` with `storageKey` prop on TaskProgress for refresh persistence
- PHP install requires ondrej PPA via `internal/php/service.go` phpRepositorySetupScript
- Node.js managed via NVM per-website in `/var/lib/jenderal/.nvm/`

## Testing

- The maintainer tests the running panel manually on Ubuntu 24.04 — do NOT launch the app,
  mock its API, or do browser-based testing. Stop at static validation:
  `npm run check`, `npm run build`, `npm test` (frontend) and `go test ./...` (backend).

## Git

- After completing a task, commit the changed files and push directly to `main` without asking.
- Only stage files related to the task; leave `.commandcode/` changes alone.
