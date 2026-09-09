# Per-Website NVM and Laravel Bootstrap Implementation Plan

> **For agentic workers:** Use superpowers:subagent-driven-development or superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Install and manage Node per website user and bootstrap working Laravel SQLite applications.

**Architecture:** A shared noderuntime service provides validated tenant paths, installation and detection. Website provisioning and Node services consume it; Laravel bootstrap is an independent installer step that also repairs existing panel-installed projects on Retry.

**Tech Stack:** Go, SQLite, Svelte 5, NVM, Ubuntu 24.04 systemd.

**Spec:** docs/superpowers/specs/2026-09-09-per-user-nvm-laravel-sqlite-design.md

## Global Constraints

- Supported Node majors: `20`, `22`, `24`; recommended default: `24`; empty means none.
- Pin NVM to `v0.40.7` and an independently verified immutable source identity.
- Per-user directory: `/home/<web-user>/.nvm`; execute packages as website user.
- Existing website metadata defaults to `24`; update/startup must not install runtimes or rewrite active services.
- Configuration-only provisioning does not install runtimes or bootstrap a database.
- Global Node removal is an explicit confirmed action, never automatic; prevent removal while panel apps still use it.
- Preserve project files, SQLite data and user external database configuration on Retry.
- Keep Native PHP label and existing PHP/framework compatibility rules.
- Progress survives browser refresh using existing tasks and website progress APIs.

### Task 1: Shared NVM runtime

**Files:** Create `internal/noderuntime/service.go`, `internal/noderuntime/service_test.go`, and focused script files if needed.

**Interfaces:** Consumes `executor.CommandExecutor`. Produces `New(exec executor.CommandExecutor) *Service`, `ValidateVersion(version string) error`, `Home(user string) (string,error)`, `ExecArgs(user,version,command string,args ...string) ([]string,error)`, `(*Service).Detect(ctx context.Context,user,version string) (Status,error)`, `(*Service).Install(ctx context.Context,user,version string,log func(string)) error`. `Status` reports installed state, Node/NPM versions and NVM state. `ExecArgs` returns arguments for `RunSudo(ctx,"-u",args...)` using explicit env and nvm-exec.

- [ ] Write table-driven tests rejecting unsafe usernames/versions and checking isolated paths and fixed positional arguments:
  ```go
  for _, version := range []string{"", "21", "24;id"} {
      if ValidateVersion(version) == nil { t.Fatalf("accepted %q", version) }
  }
  ```
- [ ] Run `go test ./internal/noderuntime` and record initial failures.
- [ ] Implement fixed scripts with argument forwarding (`bash -c SCRIPT -- user version`), HTTPS source verification, bounded commands, user-owned staging, default-alias promotion only after verification, and preservation of prior runtime on failure. Reject symlinked runtime paths before privileged operations. Verify immutable NVM commit from upstream rather than inventing a hash.
- [ ] Exercise installed/missing/mismatched runtime detection, install success and failed verification with the existing executor fake; include a real temporary-shell harness for argument forwarding if practical. Run focused tests and commit.

### Task 2: Persisted runtime selection, provisioning and Node management

**Files:** Add `internal/database/migrations/020_website_node_version.sql`; modify `internal/model/models.go`, `internal/website/{service,provisioner,installer,profiles}.go`, `internal/nodejs/{service,handler}.go`, routing registration, `internal/taskrunner/runner.go`, and their tests. Add focused `internal/nodejs/runtime.go` for runtime operations.

**Interfaces:** Consume Task 1 runtime API. Expose website JSON `node_version`, options `node_versions`; Node runtime list `GET /api/nodejs/runtimes`, install/change `POST /api/nodejs/runtimes/{websiteID}` with `{version}` and response `{task_id}`, legacy status/removal endpoints under `/api/nodejs/global`. Reuse existing auth and task polling. Add callback task API `RunFunc(name string, work func(context.Context, func(string)) error) string` if needed.

- [ ] Add migration and selection tests: existing rows receive 24, repeated migration preserves rows, create explicitly stores empty; Node-required auto profiles default 24 and reject unsupported selections.
- [ ] Add regression tests proving options/create do not probe global Node/npm and configuration-only mode never installs NVM.
- [ ] Implement additive migration:
  ```sql
  ALTER TABLE websites ADD COLUMN node_version TEXT NOT NULL DEFAULT '24';
  ```
  Carry the value through all website selects/scans/inserts and provisioning rows. Install selected NVM only on explicit automatic provisioning. Route frontend npm through `noderuntime.ExecArgs`.
- [ ] Implement runtime endpoints and refresh-restorable callback tasks. Serialize mutations with existing website coordinator, reload website under lock, verify runtime before DB/default/service changes, and roll back version and unit state on activation failure. Keep intended metadata for uninstalled legacy websites until explicit install.
- [ ] Make Node apps inherit website version, validate installed runtime before creating/updating units, and generate NVM-based systemd commands. Reserve NVM_DIR/NODE_VERSION/PATH from app overrides. Preserve legacy units until explicit migration operations; starting legacy app must not silently download packages.
- [ ] Replace global install entrypoints with actionable validation errors. Legacy removal detects an actual apt nodejs package, checks panel-managed apps are migrated, requires explicit confirmation, uses apt removal without autoremove and logs progress/audit.
- [ ] Run `go test ./internal/database ./internal/website ./internal/nodejs ./internal/taskrunner` and commit after success.

### Task 3: Website selector and per-website Node UI

**Files:** Modify `web/src/routes/websites/+page.svelte`, `web/src/routes/nodejs/+page.svelte`, `web/src/lib/website-form.js`, API/types files discovered at these imports; add behavior tests under `web/tests/websites/`.

**Interfaces:** Consume Task 2 endpoint contracts from actual handler definitions; preserve task storage conventions and SSR safety.

- [ ] Add pure form tests for none/20/22/24, template-specific defaults, explicit valid choice preservation and required Node for frontend auto-builds:
  ```js
  assert.equal(normalizeNodeVersion({requires_node: true, setup_mode: 'auto-install'}, undefined), '24');
  assert.equal(normalizeNodeVersion({requires_node: false, setup_mode: 'config-only'}, ''), '');
  ```
  Export the helper from `website-form.js`; adapt call signature only if existing form utilities require it and document the final interface here.
- [ ] Replace global dependency gating with version selection and backend validation. Submit `node_version`; show config-only runtime installation is deferred.
- [ ] Render per-website selected/detected runtime, install/reinstall/change actions, persistent TaskProgress and refresh on success. Keep Node app CRUD with inherited version; show explicit global removal confirmation only when apt global installation is detected. Disable competing actions during active tasks.
- [ ] Run `npm test`, `npm run check`, `npm run build` in `web`; commit.

### Task 4: Laravel SQLite initialization and repair

**Files:** Create `internal/website/laravel.go`, modify `internal/website/installer.go`, and add `internal/website/laravel_test.go` plus installer regressions.

**Interfaces:** `(*Installer).bootstrapLaravel(ctx context.Context,w websiteRow,root string,progress func(string,string) error) error` called after promotion and when Retry encounters a valid existing panel-installed Laravel project. Use selected PHP binary and tenant executor.

- [ ] Test missing SQLite, existing SQLite content preservation, blank/missing app key, existing app key preservation, relative/absolute SQLite paths, explicit external connection, migration failure and config-only exclusion.
- [ ] Implement idempotent environment bootstrap: fresh panel skeleton uses SQLite; preserve existing explicit external database settings and never migrate an external DB automatically. Ensure absolute final SQLite path (not staging), safe writable database/storage/cache directories, SQLite PHP extension preflight, generate key only when empty, clear stale config cache then migrate with `--force --no-interaction` as website user.
- [ ] Run bootstrap both after new project promotion and before successful Retry returns on existing project. Never truncate SQLite or replace an existing app key. Capture migration output in progress and fail provisioning if bootstrap fails.
- [ ] Run `go test ./internal/website` and commit.

### Task 5: Integration verification and delivery

**Files:** Update this plan with actual test evidence and operator notes; only fix task-related findings.

- [ ] Review each task against the spec and resolve all important findings.
- [ ] Run `go test ./...`, `go vet ./...`, production frontend checks/build, Go production build and `git diff --check`. Verify build embed requirements from Makefile first.
- [ ] Perform final independent branch review including website mutation serialization, inherited service runtime, legacy compatibility, Laravel Retry and external DB preservation.
- [ ] Merge the verified branch into main and push to origin/main under the user's existing authorization. Report commit and actual tests, distinguishing local checks from live Ubuntu VPS validation.
