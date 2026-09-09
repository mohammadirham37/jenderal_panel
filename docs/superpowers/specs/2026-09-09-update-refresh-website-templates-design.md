# Update Refresh and Website Templates Design

**Date:** 2026-09-09

## Goal

Ensure the Update page always displays the newly running panel version after a successful update, and make website creation aware of installed PHP versions while supporting safe Nginx-only or automatic framework setup for PHP, CodeIgniter, and Laravel projects.

## Scope

This design covers:

- a cache-safe full page reload after the updated panel process is ready;
- installed-runtime discovery for website creation;
- Composer management from the Services page;
- persisted website template metadata;
- Nginx and document-root presets for static PHP, CodeIgniter 3, CodeIgniter 4, and Laravel 8 through 13;
- optional automatic framework installation;
- empty-project and supported starter-kit choices;
- React, Vue, and Svelte choices when Inertia is selected;
- validation, progress reporting, failure behavior, and tests.

It does not configure application databases, collect database credentials, run application migrations against a managed database, or silently install PHP, Composer, or Node.js during website provisioning.

## Update Completion and Full Reload

The frontend must not reload at a fixed delay immediately after the update task reports completion. The update task finishes before the scheduled service restart and health verification have necessarily completed, so a fixed five-second reload can still reach the old process or a cached response.

After task completion, the Update page will poll the update-check endpoint with a cache-busting query parameter and `cache: no-store`. Polling succeeds only when the API responds and its `current_version` equals the `latest_version` captured for the update. Once that condition is met, the page performs a full document navigation using `window.location.replace()` to the Update URL with a one-time cache-busting query parameter. If readiness is not confirmed within the bounded timeout, the UI keeps the completed state and exposes a manual **Reload Panel** button rather than looping indefinitely.

The update-check HTTP response will include `Cache-Control: no-store, no-cache, must-revalidate` so browsers and intermediary caches do not reuse version metadata. Normal Update-page startup also requests the check endpoint without cache.

## Website Template Model

Website records gain nullable, backwards-compatible metadata:

- `framework`: `none`, `codeigniter`, or `laravel`;
- `framework_version`: empty, `3`, `4`, or Laravel major `8` through `13`;
- `frontend_stack`: `blade`, `inertia`, or `livewire`;
- `inertia_adapter`: empty, `react`, `vue`, or `svelte`;
- `project_variant`: `empty` or `starter-kit`;
- `setup_mode`: `config-only` or `auto-install`.

Existing `app_type` remains the coarse runtime selector for backwards compatibility:

- static templates use `static`;
- PHP and both CodeIgniter templates use `php`;
- Laravel templates use `laravel`.

Existing website rows are interpreted using their current `app_type` and document root. The migration only adds nullable/defaulted columns and does not rewrite existing document roots or Nginx files.

## Website Options API

A read-only website-options endpoint is the source of truth for the creation form. It returns:

- supported PHP versions with `installed` and `running` state;
- Composer availability and detected version;
- Node.js availability and detected version;
- template definitions and labels;
- valid framework versions;
- frontend and Inertia adapter choices;
- minimum PHP requirements and automatic-install availability;
- a human-readable reason for every disabled combination.

The create form shows only installed PHP versions in its selectable list. A stopped but installed PHP-FPM version remains visible with a warning. If no PHP version is installed, PHP-based templates are disabled and the form links to the PHP page.

The backend repeats every compatibility and dependency check during `POST /websites`; frontend filtering is convenience, not a security boundary.

## Creation Form

The form reveals fields progressively:

1. Domain.
2. Template: Static, PHP, CodeIgniter 3, CodeIgniter 4, or Laravel.
3. Installed PHP version for non-static templates.
4. Laravel major version, when Laravel is selected.
5. Frontend stack: Blade, Inertia, or Livewire for Laravel.
6. Inertia adapter: React, Vue, or Svelte when Inertia is selected.
7. Project variant: Empty or Starter Kit.
8. Setup mode: Configuration Only or Install Automatically.

A summary shows the final document root, prerequisites, and what the panel will execute before submission. Changing a parent field resets incompatible child selections.

## Document Roots and Nginx Profiles

The canonical layouts are:

| Template | Project root | Nginx document root |
| --- | --- | --- |
| Static | `/home/<web-user>/public` | `/home/<web-user>/public` |
| PHP | `/home/<web-user>/public` | `/home/<web-user>/public` |
| CodeIgniter 3 | `/home/<web-user>/public` | `/home/<web-user>/public` |
| CodeIgniter 4 | `/home/<web-user>/app` | `/home/<web-user>/app/public` |
| Laravel 8–13 | `/home/<web-user>/app` | `/home/<web-user>/app/public` |

Static uses exact-file routing and no FastCGI. Plain PHP permits normal PHP scripts with an `index.php` fallback. CodeIgniter 3 uses a front-controller fallback at the public root. CodeIgniter 4 uses its `public/index.php` front controller. Laravel uses only `public/index.php` as the PHP entry point, adds the standard security headers, denies hidden files except ACME challenges, and never exposes the project root.

HTTP and HTTPS rendering consume the same persisted profile so issuing, renewing, or removing a certificate cannot silently change routing behavior.

## Configuration-Only Mode

Configuration-only mode:

- creates the website user, project/document-root directories, logs, and temporary directories;
- writes a safe placeholder landing page only when the destination index does not already exist;
- writes the PHP-FPM pool and selected Nginx profile;
- applies the existing tenant-isolated ownership and Nginx group permissions;
- validates and reloads Nginx;
- does not invoke Composer, Node.js, npm, framework installers, or migrations.

For CodeIgniter 4 and Laravel, the placeholder resides in `app/public` so a later deployment can replace that directory without changing the Nginx configuration.

## Automatic Installation Mode

Automatic installation runs only after preflight confirms all required runtimes are already installed:

- selected PHP binary and PHP-FPM service;
- Composer for CodeIgniter 4 and Laravel;
- Node.js and npm for a Laravel variant that builds frontend assets.

Missing prerequisites fail validation before the website record and system user are created. The error identifies the missing dependency and links the user to PHP, Node.js, or Services. Provisioning never installs these dependencies implicitly.

Framework commands execute as the validated website user with an explicit working directory, bounded timeouts, non-interactive flags, and the selected PHP binary. Root privileges are limited to creating the account, system configuration files, and ownership boundaries. Package-manager output is included in website provisioning progress and retained in the website error message or associated task log on failure.

Install sources:

- CodeIgniter 3 checks out the official stable `3.1.13` tag and verifies that it resolves to commit `bcb17eb8ba53a85de154439d0ab8ff1bed047bc9` before promotion.
- CodeIgniter 4 uses the official Composer app starter.
- Laravel uses a Composer constraint for the selected major version rather than installing whichever major happens to be latest.

The installer stages the project in a temporary directory owned by the website user, validates its expected entry point, and atomically promotes it to the canonical project root. A failed download or build leaves the website in `failed` state and does not expose a partial application. Retry clears only panel-owned staging content and preserves a successfully promoted project.

Laravel writable directories receive the minimum required website-user permissions. The panel generates the application key when appropriate but does not select a production database or execute application database migrations.

## Laravel Variants and Compatibility

All Laravel majors 8 through 13 are selectable for Nginx configuration. PHP compatibility is evaluated from a catalog maintained in the backend; initially the minimums are:

| Laravel | Minimum PHP |
| --- | --- |
| 8 | 7.3 (panel-selectable PHP begins at 8.1) |
| 9 | 8.0 (panel-selectable PHP begins at 8.1) |
| 10 | 8.1 |
| 11 | 8.2 |
| 12 | 8.2 |
| 13 | 8.3 |

The catalog may also disable newer PHP versions for older framework dependencies when Composer cannot resolve a supported set. The backend returns the reason instead of allowing an installation that is known to fail.

An empty Blade project is the base Laravel application and can be installed automatically for Laravel 8 through 13. Empty Livewire and Inertia selections remain available in configuration-only mode; automatic installation is disabled until a maintained, version-pinned upstream skeleton exists for that exact combination.

Starter-kit availability follows pinned upstream combinations. Laravel 12 supports the official React, Vue, and Livewire starter-kit releases. Laravel 13 supports the official React, Vue, Svelte, and Livewire starter-kit repositories pinned to commits recorded in the backend catalog. Older Laravel starter-kit generations and Laravel 12 Svelte remain visible but disabled for automatic installation with an explanation. Configuration-only mode remains available because Nginx routing does not depend on the JavaScript adapter.

Frontend automatic installations run `npm install` and a production asset build as the website user. No long-running development server is started.

## Composer Management in Services

The Services page gains a **Developer Dependencies** section separate from systemd services. Composer displays:

- installed/not-installed status;
- detected version;
- Install action when absent;
- Update action when present;
- persistent task progress using the existing task component.

The backend installs Composer 2 globally at `/usr/local/bin/composer` using Composer's programmatic installer workflow. It downloads the expected installer signature separately, verifies SHA-384 before execution, uses bounded network timeouts, writes through a temporary path, and atomically replaces the destination. Composer update uses the supported self-update mechanism and reports the resulting version.

Node.js is not duplicated in this section. Dependency messages link to the existing Node.js page, which remains the place to install and manage Node.js.

## Failure, Retry, and Existing Websites

Website provisioning status continues to be polled from the Websites page. Framework setup adds explicit stages such as `checking dependencies`, `installing framework`, `building assets`, `writing configuration`, and `validating Nginx` so a long Composer/npm action does not appear frozen.

Validation errors return synchronously and create no website. Runtime failures after record creation set the website to `failed` with a concise public message and retain detailed command output in progress logs. Retry uses persisted template metadata and setup mode.

Existing websites keep their present behavior and configuration. No startup reconciliation rewrites their framework metadata or document root.

## Security

- Domain, web user, framework version, PHP version, stack, and adapter values are selected from backend allowlists.
- Commands are built from argument arrays or fixed scripts; user input is never interpolated into a shell command.
- Framework processes run as the tenant website user.
- Downloads use HTTPS, bounded timeouts, and checksums or Composer's verified package metadata.
- Temporary directories are created under the website home and cannot target arbitrary filesystem paths.
- Automatic installation refuses a non-empty project destination.
- Nginx and PHP-FPM configurations are validated before activation and restored on activation failure.
- The Services Composer endpoint requires the existing privileged service-management permission and writes an audit record.

## Testing and Acceptance Criteria

Backend tests must prove:

- update-check responses are non-cacheable;
- readiness comparison requires the newly reported commit before full reload is requested;
- options include installed PHP versions and accurate dependency status;
- invalid or unavailable runtime/template combinations are rejected before insertion;
- migration preserves existing website rows;
- every profile renders the expected HTTP and HTTPS document root/front-controller behavior;
- SSL regeneration preserves the selected profile;
- installer commands use explicit validated versions, website-user execution, timeouts, and staging paths;
- partial installs are not promoted and retries do not delete a completed project;
- Composer installation verifies the signature before replacing the global executable.

Frontend tests must prove:

- update completion waits for the new version and then performs a cache-busted document replacement;
- timeout exposes manual reload;
- only installed PHP versions are selectable;
- dependent selections reset when a parent choice changes;
- Inertia exposes React, Vue, and Svelte;
- unsupported automatic-install combinations are disabled with their reason;
- missing Composer/Node.js shows the correct navigation link;
- the submitted payload contains the normalized template metadata.

The full Go suite, frontend unit suite, Svelte type checks, production frontend build, Go build, `go vet`, and diff checks must pass before merge and push to `main`.
