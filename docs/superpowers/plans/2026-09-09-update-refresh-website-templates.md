# Update Refresh and Website Templates Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make update completion perform a cache-safe full reload and add installed-runtime-aware website templates with optional safe framework installation.

**Architecture:** Keep version readiness, developer dependency management, website profile validation, Nginx rendering, framework installation, and form state in separate tested units. Persist normalized profile metadata so HTTP, TLS, retry, and future deployment operations all resolve the same document root and routing profile.

**Tech Stack:** Go 1.25, Chi, SQLite migrations, Svelte 5/SvelteKit, Node test runner, Nginx, PHP-FPM, Composer, npm.

**Spec:** `docs/superpowers/specs/2026-09-09-update-refresh-website-templates-design.md`

## Global Constraints

- Ubuntu 24.04 remains the deployment target.
- Website provisioning must never silently install PHP, Composer, or Node.js.
- All framework commands run as the validated website user with an explicit timeout.
- Existing website rows and custom document roots remain unchanged.
- HTTP and HTTPS use the same persisted Nginx profile.
- Inertia always offers React, Vue, and Svelte, but unsupported automatic-install combinations are disabled with a reason.
- No application database credentials or migrations are managed by this feature.
- All production edits follow red-green-refactor TDD.

---

### Task 1: Cache-Safe Update Readiness and Full Document Reload

**Files:**
- Create: `web/src/lib/update-readiness.js`
- Create: `web/tests/update/UpdateReadiness.test.mjs`
- Modify: `web/src/lib/api.ts`
- Modify: `web/src/routes/update/+page.svelte`
- Create: `internal/update/handler_test.go`
- Modify: `internal/update/handler.go`

**Interfaces:**
- Produces: `waitForUpdatedPanel({ expectedVersion, check, delay, maxAttempts }) -> Promise<boolean>`.
- Produces: `cacheBustedURL(href, token) -> string`.
- Produces: `api.getNoStore<T>(path) -> Promise<T>`.
- HTTP contract: `GET /api/v1/update/check` returns `Cache-Control: no-store, no-cache, must-revalidate`.

- [ ] **Step 1: Write failing frontend tests for readiness and reload URL behavior**

  Test literal sequences in `UpdateReadiness.test.mjs`: an unavailable request followed by old version and then `{current_version:'bbbbbbbb', latest_version:'bbbbbbbb'}` resolves `true`; exhausting `maxAttempts: 3` resolves `false`; `cacheBustedURL('https://panel.test/update?foo=1', '123')` preserves `foo=1` and adds `_panel_reload=123`.

- [ ] **Step 2: Run the focused frontend test and verify RED**

  Run: `cd web && node --test tests/update/UpdateReadiness.test.mjs`

  Expected: FAIL because `src/lib/update-readiness.js` does not exist.

- [ ] **Step 3: Implement the pure readiness helper**

  Use dependency-injected scheduling so the test has no real delay:

  ```js
  export async function waitForUpdatedPanel({
    expectedVersion,
    check,
    delay = (milliseconds) => new Promise((resolve) => setTimeout(resolve, milliseconds)),
    maxAttempts = 30
  }) {
    for (let attempt = 0; attempt < maxAttempts; attempt += 1) {
      try {
        const info = await check();
        if (info.current_version === expectedVersion) return true;
      } catch {
        // A disconnect is expected while systemd replaces the process.
      }
      if (attempt + 1 < maxAttempts) await delay(2000);
    }
    return false;
  }

  export function cacheBustedURL(href, token = Date.now().toString()) {
    const url = new URL(href);
    url.searchParams.set('_panel_reload', token);
    return url.toString();
  }
  ```

  The test injects `delay: async () => {}` so it completes without wall-clock waiting.

- [ ] **Step 4: Add no-store API support and wire the Update page**

  Extend the internal request helper to accept `RequestInit`, and expose:

  ```ts
  getNoStore<T>(path: string): Promise<T> {
    return request<T>('GET', path, undefined, { cache: 'no-store' });
  }
  ```

  Capture `info.latest_version` when update starts. On task completion call `waitForUpdatedPanel`, checking `/api/v1/update/check?_=${Date.now()}` with `api.getNoStore`. On success call `window.location.replace(cacheBustedURL(window.location.href))`. On timeout set a `reloadReady=false` state and render a manual **Reload Panel** button using the same `replace` call. Remove the fixed five-second `location.reload()`.

- [ ] **Step 5: Write and verify a failing backend cache-header test**

  Instantiate the package-local update handler with a service backed by `githubCommitClient`, call `Check` through `httptest`, and assert the exact `Cache-Control` header. Run `go test ./internal/update -run TestCheckResponseDisablesCaching -count=1`; expect failure because the header is absent.

- [ ] **Step 6: Set the cache header and run focused tests GREEN**

  Add before writing the JSON response:

  ```go
  w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
  ```

  Run: `go test ./internal/update -count=1 && cd web && node --test tests/update/UpdateReadiness.test.mjs tests/update/UpdateProgress.test.mjs`

- [ ] **Step 7: Commit the update-refresh slice**

  ```bash
  git add internal/update web/src/lib/api.ts web/src/lib/update-readiness.js web/src/routes/update/+page.svelte web/tests/update
  git commit -m "fix(update): reload after new version is ready"
  ```

---

### Task 2: Composer Dependency Management on Services

**Files:**
- Create: `internal/dependency/service.go`
- Create: `internal/dependency/service_test.go`
- Create: `internal/dependency/handler.go`
- Create: `internal/dependency/handler_test.go`
- Modify: `internal/api/router.go`
- Modify: `cmd/jenderal/main.go`
- Create: `web/src/lib/developer-dependencies.js`
- Modify: `web/src/routes/services/+page.svelte`
- Modify: `web/tests/update/UpdateProgress.test.mjs`
- Create: `web/tests/services/DeveloperDependencies.test.mjs`
- Modify: `web/package.json`

**Interfaces:**
- Produces: `dependency.Status{Name, Version string; Installed bool}`.
- Produces: `(*dependency.Service).ComposerStatus(context.Context) (Status, error)`.
- Produces: `(*dependency.Service).ComposerInstallCommands() [][]string` and `ComposerUpdateCommands() [][]string`.
- Produces: `composerActionPath(status) -> '/api/v1/services/composer/install' | '/api/v1/services/composer/update'`.
- HTTP contracts: `GET /api/v1/services/dependencies`, `POST /api/v1/services/composer/install`, and `POST /api/v1/services/composer/update`.
- API dependencies gain `DependencySvc *dependency.Service`; handler receives the existing task runner and audit service.

- [ ] **Step 1: Write failing service tests for absent and installed Composer**

  Use `executor.MockExecutor` at the external command boundary. Literal cases: `composer --version --no-ansi` returning command-not-found means `{installed:false}`; returning `Composer version 2.10.3 ...` means `{installed:true, version:"2.10.3"}`. The production break caught is treating an unavailable binary as a request failure or parsing the whole banner as the version.

- [ ] **Step 2: Run dependency service tests RED**

  Run: `go test ./internal/dependency -count=1`

  Expected: FAIL because the package does not exist.

- [ ] **Step 3: Implement status detection and fixed installation command plans**

  The installation plan is one fixed `bash -c` script with no user input. It must:

  ```bash
  set -euo pipefail
  work_dir="$(mktemp -d)"
  trap 'rm -rf "$work_dir"' EXIT
  curl --fail --silent --show-error --location --connect-timeout 10 --max-time 60 \
    https://composer.github.io/installer.sig -o "$work_dir/expected"
  curl --fail --silent --show-error --location --connect-timeout 10 --max-time 60 \
    https://getcomposer.org/installer -o "$work_dir/composer-setup.php"
  actual="$(php -r "echo hash_file('sha384', '$work_dir/composer-setup.php');")"
  test "$actual" = "$(tr -d '\r\n' < "$work_dir/expected")"
  php "$work_dir/composer-setup.php" --2 --install-dir="$work_dir" --filename=composer
  install -o root -g root -m 0755 "$work_dir/composer" /usr/local/bin/composer.new
  mv -f /usr/local/bin/composer.new /usr/local/bin/composer
  composer --version --no-ansi
  ```

  Update uses `composer self-update --2 --no-interaction --no-ansi` followed by the version check. The Go tests assert fixed URLs, verification-before-installer ordering, atomic `.new` promotion, and absence of request-derived interpolation.

- [ ] **Step 4: Add handler tests and endpoints**

  Tests assert JSON status, `202` task IDs, `services.manage` route permission, and audit details. Implement handlers that invoke `tasks.RunMultiple` with the service command plan; do not execute Composer synchronously in the request.

- [ ] **Step 5: Wire the dependency service**

  Construct one `dependency.Service` from the shared executor in `cmd/jenderal/main.go`, add it to `api.Dependencies`, create the handler in `internal/api/router.go`, and register static Composer routes before the `{name}` systemd routes.

- [ ] **Step 6: Write failing frontend dependency-action tests**

  Add `services` with storage key `jenderal_composer_task` to the existing TaskProgress mount contract in `UpdateProgress.test.mjs`. In `DeveloperDependencies.test.mjs`, feed literal installed/not-installed status objects to `composerActionPath` and assert the exact update/install endpoint. Update the npm test glob to include `tests/services/*.test.mjs`.

- [ ] **Step 7: Implement the Services developer-dependencies card and verify GREEN**

  Add a separate card above the systemd table with Install/Update actions, task persistence, success/error messages, and a refresh after completion. Keep Node.js as a link to `/nodejs` rather than duplicating its installer.

  Run: `go test ./internal/dependency ./internal/api -count=1 && cd web && node --test tests/services/*.test.mjs tests/update/UpdateProgress.test.mjs`

- [ ] **Step 8: Commit the Composer slice**

  ```bash
  git add internal/dependency internal/api/router.go cmd/jenderal/main.go web/src/lib/developer-dependencies.js web/src/routes/services web/tests/services web/tests/update/UpdateProgress.test.mjs web/package.json
  git commit -m "feat(services): manage Composer dependency"
  ```

---

### Task 3: Persisted Website Profiles and Runtime-Aware Options

**Files:**
- Create: `internal/database/migrations/019_website_profiles.sql`
- Modify: `internal/database/migrations.go`
- Create: `internal/database/migrations_test.go`
- Create: `internal/website/profiles.go`
- Create: `internal/website/profiles_test.go`
- Modify: `internal/model/models.go`
- Modify: `internal/website/service.go`
- Modify: `internal/website/service_test.go`
- Modify: `internal/website/provisioner_test.go`
- Modify: `internal/website/handler.go`
- Create: `internal/website/handler_test.go`
- Modify: `internal/api/router.go`

**Interfaces:**
- Adds website fields: `Framework`, `FrameworkVersion`, `FrontendStack`, `InertiaAdapter`, `ProjectVariant`, `SetupMode`, `ProvisionStage`, and `ProvisionLog`.
- Extends `website.CreateRequest` with `Template`, `FrameworkVersion`, `FrontendStack`, `InertiaAdapter`, `ProjectVariant`, and `SetupMode`; `Framework` and `AppType` are derived server-side.
- Produces: `ResolveProfile(CreateRequest) (Profile, error)`.
- Produces: `(*website.Service).Options(context.Context) (WebsiteOptions, error)`.
- HTTP contract: `GET /api/v1/websites/options`.

- [ ] **Step 1: Write failing migration-runner and preservation tests**

  First prove `Migrate(db)` can be called twice even when a migration contains `ALTER TABLE`: the current runner reapplies every file and will fail. Add a legacy-database test that creates an old-format website row, runs migrations, and asserts its original app type/document root plus the new defaults.

  Implement a `schema_migrations(filename TEXT PRIMARY KEY, applied_at TEXT NOT NULL)` ledger. For an existing database without the ledger, the first upgraded run may safely re-run migrations 001–018 because they contain idempotent `CREATE ... IF NOT EXISTS`, then records each successful filename. Each migration is applied and recorded in one transaction. The second run skips recorded files.

  Migration 019 uses eight `ALTER TABLE websites ADD COLUMN` statements with safe defaults: framework `none`, project variant `empty`, setup mode `config-only`, provision stage empty, and all other new values nullable/empty.

- [ ] **Step 2: Define profile tables through failing literal tests**

  Table tests must cover document roots and at least these validations:

  ```go
  {Template: "laravel", FrameworkVersion: "13", PHPVersion: "8.2"} // reject: PHP 8.3+
  {Template: "laravel", FrameworkVersion: "12", PHPVersion: "8.2", FrontendStack: "inertia", InertiaAdapter: "svelte"} // valid config-only
  {Template: "codeigniter4", PHPVersion: "8.3"} // /home/<user>/app/public
  {Template: "php", PHPVersion: "8.3"} // /home/<user>/public
  {Template: "static", PHPVersion: ""} // valid without PHP
  ```

  Add this explicit automatic-install support matrix with a reason string for every disabled combination:

  | Laravel major | Empty Blade | Starter React | Starter Vue | Starter Svelte | Starter Livewire |
  | --- | --- | --- | --- | --- | --- |
  | 8–11 | enabled | disabled | disabled | disabled | disabled |
  | 12 | enabled | `laravel/react-starter-kit` v1.0.1 | `laravel/vue-starter-kit` v1.0.2 | disabled | `laravel/livewire-starter-kit` v1.0.1 |
  | 13 | enabled | commit `87cce8705d712629ebddd70ccfbb06592ecbaac2` | commit `12c8185609b10802b96d740dc5c4fcac0d66ade3` | commit `593365653c38308fbea55fc90dfb22c554702b81` | commit `78fed019a1848eb42ff15911ef3c5a22042755fc` |

  Empty Inertia and empty Livewire are config-only in this release. PHP, CodeIgniter 3, and CodeIgniter 4 support automatic installation. All combinations stay visible, but only the table entries above enable Laravel automatic installation.

- [ ] **Step 3: Run profile tests RED**

  Run: `go test ./internal/website -run 'TestResolveProfile|TestWebsiteProfileMigration' -count=1`

  Expected: FAIL because the profile resolver and migration do not exist.

- [ ] **Step 4: Implement the allowlist-only profile catalog**

  Use typed strings and literal maps; never accept a package name or version constraint from the request. `ResolveProfile` derives `AppType`, `NginxProfile`, PHP minimum, `ProjectRoot`, and relative document root from allowlisted inputs. It returns validation errors with the exact disabled reason used by the options response.

- [ ] **Step 5: Extend persistence and scanners**

  Add all metadata to insert/select/scan paths in `Create`, `Get`, `List`, retry loading, Nginx regeneration, and test schemas. Use `sql.NullString` for old rows. Derive the canonical document root from `DomainToUser` plus the profile; do not accept a document root from create input.

- [ ] **Step 6: Write and verify failing runtime-option tests**

  Configure the command mock so `/etc/php/8.2` and `/etc/php/8.4` exist, Composer reports `2.10.3`, and Node reports `v20.19.0`. Assert `Options` returns only PHP 8.2 and 8.4 as selectable and contains those dependency versions. Add a Create test where PHP directory lookup fails and assert no website row was inserted.

- [ ] **Step 7: Implement Options and authoritative Create validation**

  Inspect PHP with fixed supported versions, Composer with `composer --version --no-ansi`, and Node with `node --version`. Config-only requires only installed selected PHP; auto-install additionally checks the profile's Composer/Node flags. Return navigation targets (`/php`, `/services`, `/nodejs`) in disabled dependency entries.

- [ ] **Step 8: Add the options handler and route**

  Register `GET /websites/options` with `websites.view` before the website ID routes. Handler tests assert the response envelope and confirm missing-runtime validation returns HTTP 400 rather than 202.

- [ ] **Step 9: Run the website/database suites GREEN and commit**

  Run: `go test ./internal/database ./internal/website ./internal/api -count=1`

  ```bash
  git add internal/database/migrations.go internal/database/migrations_test.go internal/database/migrations/019_website_profiles.sql internal/model/models.go internal/website internal/api/router.go
  git commit -m "feat(websites): add runtime-aware template profiles"
  ```

---

### Task 4: Profile-Specific HTTP and HTTPS Nginx Rendering

**Files:**
- Modify: `internal/website/templates.go`
- Modify: `internal/website/templates_test.go`
- Modify: `internal/website/service.go`
- Modify: `internal/website/service_test.go`
- Modify: `internal/website/provisioner.go`
- Modify: `internal/ssl/nginx_config.go`
- Modify: `internal/ssl/nginx_config_test.go`

**Interfaces:**
- `website.VhostData` gains `Profile string`.
- Supported profiles: `static`, `php`, `codeigniter3`, `codeigniter4`, `laravel`.
- `RenderVhost` and `RenderTLSVhost` select routing from `Profile`, falling back to legacy `AppType` only for old rows.

- [ ] **Step 1: Write failing table tests for every Nginx profile**

  Assert literal behavior for both HTTP and TLS:

  - static: `try_files $uri $uri/ =404`, no FastCGI;
  - PHP: PHP files use the tenant socket and `SCRIPT_FILENAME`, index includes HTML;
  - CodeIgniter 3: `/index.php?$query_string` and `error_page 404 /index.php`;
  - CodeIgniter 4: front controller plus canonical `app/public` root;
  - Laravel: `index index.php`, only `location ~ ^/index\.php(/|$)`, `$realpath_root$fastcgi_script_name`, security headers, and hidden-file denial excluding `.well-known`.

  Mutating the profile to `static` must make each dynamic-profile test fail.

- [ ] **Step 2: Run focused renderer tests RED**

  Run: `go test ./internal/website -run 'TestRender.*Profile' -count=1`

- [ ] **Step 3: Implement shared profile rendering**

  Keep common server directives and ACME behavior shared, while profile-specific helpers return `index`, `location /`, and PHP location blocks. Do not restore `snippets/fastcgi-params.conf`; continue using packaged `fastcgi_params` with an explicit script filename.

- [ ] **Step 4: Write failing SSL preservation test**

  Insert a Laravel-profile website, activate a test certificate, and assert both generated HTTP and TLS configs retain the Laravel-only index PHP location and `/app/public` root. This catches dropping profile metadata in `loadSiteForDomain`.

- [ ] **Step 5: Thread profile metadata through provisioning, regeneration, and SSL**

  Select/scan `framework` and derive `Profile` in `websiteRow`, `model.Website`, and `ssl.siteRecord`. Build all `VhostData` instances with that profile.

- [ ] **Step 6: Run website and SSL suites GREEN and commit**

  Run: `go test ./internal/website ./internal/ssl -count=1`

  ```bash
  git add internal/website internal/ssl
  git commit -m "feat(nginx): render framework-specific website profiles"
  ```

---

### Task 5: Safe Framework Installation and Retry Progress

**Files:**
- Create: `internal/website/installer.go`
- Create: `internal/website/installer_test.go`
- Modify: `internal/website/provisioner.go`
- Modify: `internal/website/provisioner_test.go`
- Modify: `internal/website/service.go`
- Modify: `internal/model/models.go`

**Interfaces:**
- Produces: `type Installer struct { exec executor.CommandExecutor }`.
- Produces: `(*Installer).Install(context.Context, websiteRow, func(stage, output string) error) error`.
- Produces: `installationPlan(websiteRow) ([]installStep, error)` where each step has fixed `Name`, `Timeout`, `Command`, and `Args`.
- Provisioning exposes `provision_stage` and bounded `provision_log` on the website JSON.

- [ ] **Step 1: Write failing literal command-plan tests**

  Tests must prove:

  - every command begins with `sudo -u <validated-web-user> --` at the executor boundary;
  - selected PHP uses `/usr/bin/php8.3 /usr/local/bin/composer`, never ambient `php`;
  - Laravel 11 uses `create-project laravel/laravel:^11.0`;
  - CodeIgniter 4 uses `codeigniter4/appstarter`;
  - CodeIgniter 3 checks out tag `3.1.13` and verifies commit `bcb17eb8ba53a85de154439d0ab8ff1bed047bc9`;
  - frontend plans include `npm install --no-audit --no-fund` and `npm run build` only when the catalog marks Node required;
  - no request-derived package name or shell interpolation appears.

  Composer project creation always adds `--no-interaction --prefer-dist --no-scripts`; the installer copies `.env.example` to `.env` and runs `artisan key:generate` explicitly, but never invokes a starter kit's database migration hook. Laravel 12 starter kits use the exact package versions in Task 3. Laravel 13 starter kits clone their official repository, detach at the exact catalogued commit, verify `git rev-parse HEAD`, then run Composer and npm inside staging.

- [ ] **Step 2: Run installer tests RED**

  Run: `go test ./internal/website -run 'TestInstallationPlan|TestInstaller' -count=1`

- [ ] **Step 3: Implement staging and bounded execution**

  Use `/home/<web-user>/.jenderal-install-<website-id>` as the only staging root. Before starting, reject an existing non-empty final project root. Wrap each external command in `context.WithTimeout` using 15 minutes for Composer/git and 10 minutes for npm. Validate `public/index.php` before promotion.

  Automatic mode creates the account, log directory, and temp directory before installation but must not pre-create the final project/document root or placeholder landing page. Promotion uses website-user-owned filesystem operations for content and privileged `chown -h`/permission repair only at canonical directory boundaries. Never run recursive removal on a path computed from raw stored input.

- [ ] **Step 4: Add failure and retry tests**

  Simulate a Composer non-zero exit and assert: website becomes `failed`; final project root is not promoted; stage is `installing framework`; detailed output is appended to `provision_log`. Then simulate retry with a valid staged app and assert only the exact panel staging directory is cleared. A pre-existing valid final app must be preserved and provisioning should continue to Nginx configuration.

- [ ] **Step 5: Integrate installer stages into provisioning**

  For config-only, create the canonical document root and non-destructively write the landing page. For auto-install, call Installer after account/directory creation and before writing Nginx. Update stages in order: `checking dependencies`, `installing framework`, `building assets`, `writing configuration`, `validating nginx`, `active`. Cap stored log size to 256 KiB by retaining the newest output.

- [ ] **Step 6: Ensure Laravel/CodeIgniter writable permissions**

  Laravel grants the website user write access to `storage` and `bootstrap/cache`; CodeIgniter 4 grants it to `writable`. Extend `ensureServingPermissions` so it accepts only the two derived canonical layouts: `/home/<user>/public` and `/home/<user>/app/public`. For the app layout, assign `www-data` group without following final symlinks on home, app, and app/public; apply `0710` to home and app plus `0750` to app/public as the website user. Add a symlink/trailing-path regression test equivalent to the existing managed-root safety test. Other/custom roots remain untouched.

- [ ] **Step 7: Run provisioning tests GREEN and commit**

  Run: `go test ./internal/website -count=1`

  ```bash
  git add internal/website internal/model/models.go
  git commit -m "feat(websites): install framework projects safely"
  ```

---

### Task 6: Progressive Website Creation Form

**Files:**
- Create: `web/src/lib/website-form.js`
- Create: `web/tests/websites/WebsiteForm.test.mjs`
- Modify: `web/src/routes/websites/+page.svelte`
- Modify: `web/package.json`

**Interfaces:**
- Produces: `normalizeWebsiteSelection(selection, options) -> selection`.
- Produces: `availablePHPVersions(options) -> PhpVersion[]`.
- Produces: `selectedCombination(options, selection) -> {enabled, reason, document_root, prerequisites}`.

- [ ] **Step 1: Write failing form-state tests**

  Use a complete literal options fixture. Assert:

  - only `{version:'8.2', installed:true}` appears, while uninstalled 8.1 does not;
  - changing template from Laravel to PHP clears framework version, frontend, adapter, and starter-kit values;
  - selecting Inertia exposes the literal adapters `react`, `vue`, `svelte`;
  - Laravel 13 with PHP 8.2 returns the backend-provided incompatibility reason;
  - missing Composer points to `/services`; missing Node points to `/nodejs`;
  - the normalized Laravel Inertia/Svelte payload contains all six persisted metadata fields.

- [ ] **Step 2: Run form tests RED**

  Run: `cd web && node --test tests/websites/WebsiteForm.test.mjs`

  Expected: FAIL because the helper does not exist.

- [ ] **Step 3: Implement pure selection helpers GREEN**

  Keep compatibility decisions in the backend response; the helper filters and resets state but does not recreate the PHP/framework matrix. Run the focused test until green.

- [ ] **Step 4: Replace the hardcoded website form**

  Load `/api/v1/websites/options` together with the website list. Render the progressive fields from the response, show stopped-PHP warnings, disable invalid automatic combinations, and provide prerequisite links. Submission sends:

  ```json
  {
    "domain": "example.com",
    "template": "laravel",
    "php_version": "8.3",
    "framework_version": "13",
    "frontend_stack": "inertia",
    "inertia_adapter": "svelte",
    "project_variant": "starter-kit",
    "setup_mode": "auto-install"
  }
  ```

  Show the returned `provision_stage` in pending rows and a collapsible provisioning log for failed rows. Reset the form from backend defaults after successful creation instead of resetting PHP to hardcoded 8.3.

- [ ] **Step 5: Add test glob, run frontend tests and type checks**

  Add `tests/websites/*.test.mjs` to `npm test`.

  Run: `cd web && npm test && npm run check`

- [ ] **Step 6: Commit the form slice**

  ```bash
  git add web/src/lib/website-form.js web/src/routes/websites web/tests/websites web/package.json
  git commit -m "feat(websites): add framework template creation form"
  ```

---

### Task 7: End-to-End Regression Gate and Main Push

**Files:**
- Modify only files required to fix failures caused by Tasks 1–6.

**Interfaces:**
- Consumes every prior task's public API and persisted schema.
- Produces a clean, verified `main` commit history synchronized with `origin/main`.

- [ ] **Step 1: Run fresh backend verification**

  ```bash
  go test -count=1 ./...
  go vet ./...
  ```

  Expected: all packages pass with no vet findings.

- [ ] **Step 2: Run fresh frontend verification**

  ```bash
  cd web
  npm test
  npm run check
  npm run build
  ```

  Expected: all tests pass, Svelte reports zero errors, and the production build completes.

- [ ] **Step 3: Build the complete panel and check the diff**

  ```bash
  cd ..
  make build
  git diff --check
  git status --short
  ```

  Expected: build succeeds, diff check is empty, and status contains only intended tracked work if any final fix remains.

- [ ] **Step 4: Review the complete change against the spec**

  Verify each acceptance item in the linked design, with particular attention to raw-input command construction, symlink handling, non-empty destination refusal, TLS profile preservation, and cache-safe update readiness. Apply review fixes with new failing regression tests.

- [ ] **Step 5: Fetch and verify remote divergence**

  ```bash
  git fetch origin main
  git rev-list --left-right --count main...origin/main
  ```

  Expected before push: local commits only and zero remote-only commits. If remote-only commits exist, stop the push and reconcile without rewriting remote history.

- [ ] **Step 6: Push and verify exact SHA synchronization**

  ```bash
  git push origin main
  git rev-parse HEAD
  git rev-parse origin/main
  git status --short
  ```

  Expected: local and remote SHA are identical and the working tree is clean.
