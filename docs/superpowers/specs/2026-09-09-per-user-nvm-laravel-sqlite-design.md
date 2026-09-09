# Per-Website NVM and Laravel SQLite Bootstrap Design

**Date:** 2026-09-09

## Goal

Replace panel-managed global Node.js with an isolated NVM runtime for each website user, expose the selected runtime during website creation and management, and make automatically installed Laravel applications immediately usable with their default SQLite and database-backed session configuration.

This design supersedes the global Node.js prerequisite and `/usr/bin/node` or `/usr/bin/npm` execution described by earlier website-template plans. It does not automatically remove a system-wide Node.js installation that may be used outside the panel.

## Scope

This design covers:

- NVM installation under each website user's home directory;
- Node.js 20, 22, and 24, with Node.js 24 as the recommended default;
- optional Node.js selection in the website creation form;
- automatic Node.js selection for framework variants that require an asset build;
- per-website runtime visibility and actions on the Node.js page;
- NVM-aware npm commands and systemd services;
- compatibility behavior for existing websites and existing global Node.js packages;
- Laravel SQLite creation, permissions, and initial migration;
- persistent task progress, safe retries, and automated tests.

It does not install a separate Node.js version for every application within one website, automatically uninstall system packages, or manage production database credentials.

## Runtime Ownership and Data Model

Each website owns one optional Node.js major version. The `websites` table gains a `node_version` field whose valid application values are empty, `20`, `22`, and `24`.

An empty value means that the website does not use a panel-managed Node.js runtime. Node.js applications belonging to a website inherit the website runtime; they do not select an independent version. This prevents an application's persisted metadata, generated systemd unit, and actual runtime from disagreeing.

The compatibility migration assigns `24` to existing website rows but does not download NVM or Node.js. Existing sites therefore have an intended runtime without causing network activity, service interruption, or filesystem changes during a panel update. Installation occurs only after an explicit Install, Retry, Recreate, or Change Version action.

New website records always persist the normalized form selection explicitly. Static HTML, Native PHP, and CodeIgniter default to no Node.js. Users may still select Node.js when their deployment requires it. Laravel configurations that require a frontend build default to Node.js 24 and require one of the supported versions before submission.

Runtime installation state is detected from the website user's NVM directory and executable output rather than inferred solely from the database. API responses distinguish the selected version from the version actually installed.

## NVM Installation and Version Selection

The panel pins NVM to `v0.40.7`. Installation is performed for the website's Linux user under `/home/<web-user>/.nvm`; NVM, Node.js, npm, and their caches remain owned by that user.

The installation flow is:

1. Validate the website, Linux username, home directory, and requested version against panel-owned records and fixed allowlists.
2. Download the pinned NVM source into a panel-owned staging location with bounded network timeouts.
3. Validate the downloaded source against the pinned release identity before promotion.
4. Promote a valid NVM installation into the user's `.nvm` directory without deleting a working installation.
5. Execute NVM as the website user to install the selected Node.js major and set its default alias.
6. Verify `node --version` has the requested major and verify `npm --version` succeeds in the same NVM environment.
7. Only after successful verification, persist a changed `node_version` and regenerate or restart affected Node.js services.

A shared internal runtime component owns the version catalog, path construction, detection, installation, and command environment. Website provisioning and Node.js application management must consume this component rather than duplicating shell setup.

NVM profile modifications are not required for panel operations. Commands use an explicit `NVM_DIR` and NVM entry point so behavior does not depend on interactive shell startup files.

## Website Creation Form

The creation form includes a Node.js selector with:

- **Tidak menggunakan Node.js**;
- Node.js 20;
- Node.js 22;
- Node.js 24 (recommended).

The default and validation depend on the chosen template:

- Static HTML, Native PHP, CodeIgniter 3, and CodeIgniter 4 default to no Node.js.
- Laravel configuration-only and Laravel Blade without an asset build may use no Node.js.
- Laravel automatic installation that builds a starter kit, Inertia application, Livewire frontend assets, or another npm-backed variant requires a Node.js selection and defaults to 24.
- Inertia continues to expose React, Vue, and Svelte where the framework-version catalog supports them.

Changing a parent template or setup-mode field recalculates the suggested Node.js value but does not silently discard a still-valid explicit user choice. The backend repeats all compatibility checks and returns a clear validation error if a required runtime is absent.

Automatic website provisioning installs the selected NVM runtime after the Linux user exists and before any npm step. npm commands execute as that user with the project working directory and explicit NVM environment. Global `/usr/bin/node` and `/usr/bin/npm` are never considered sufficient prerequisites.

## Node.js Management Page

The `/nodejs` page becomes a per-website runtime manager. Its website rows display:

- domain and Linux user;
- selected Node.js major;
- detected installed Node.js and npm versions;
- NVM state;
- current task or failure state;
- Install, Retry/Reinstall, and Change Version actions as appropriate.

Long-running operations use the existing persistent task system. Progress and logs remain available after navigation or full refresh. A successful runtime change refreshes the data and then restarts only the affected website's Node.js application services.

Node.js application creation inherits the website runtime. If the selected runtime has not been installed, the API rejects service creation with an actionable message directing the user to install or retry that website runtime.

The global Node.js installer and global version cards are removed. If the panel detects a legacy system-wide Node.js package, it shows a separate compatibility notice with an explicit **Uninstall Global Node.js** action and confirmation dialog. Panel update and website provisioning never invoke this removal automatically.

## NVM-Aware Application Services

Generated systemd units no longer hard-code `/usr/bin/node`. A service receives fixed `NVM_DIR` and `NODE_VERSION` values and starts Node through the website user's NVM `nvm-exec` entry point. The unit retains the website user, working directory, restart policy, and existing resource/security restrictions.

Before writing or restarting a unit, the backend verifies that the requested version is installed and that the NVM executable resolves under the expected website home. Paths are derived from persisted website data, not arbitrary request values.

Changing a website runtime regenerates its panel-owned Node.js units only after the new version passes verification. If any regeneration or restart fails, the task reports the affected service and preserves enough state for a safe retry. It must not replace unrelated, non-panel systemd units.

## Laravel SQLite Bootstrap

Laravel automatic installation must produce a runnable default application instead of leaving a configuration that references a missing SQLite file.

After the framework has been promoted and `.env` exists, the installer:

1. Ensures the Laravel database connection is SQLite when no external database has been configured.
2. Creates `<project-root>/database/database.sqlite` as the website user.
3. Ensures `database`, `storage`, and `bootstrap/cache` have the required website-user ownership and writable permissions without making them world-writable.
4. Generates the application key when it is absent.
5. Runs the selected PHP binary with `artisan migrate --force --no-interaction` as the website user.
6. Verifies the expected public entry point and completes Nginx activation only after bootstrap succeeds.

This initializes tables required by Laravel defaults such as database-backed sessions, cache, and jobs. A migration error makes provisioning fail and retains detailed command output in the task log. Retry is idempotent: an existing SQLite file is preserved, successful migrations are not reapplied destructively, and only missing setup steps are completed.

Configuration-only mode does not create a database, install packages, build assets, generate keys, or run migrations.

## Failure Handling and Security

- Node.js versions, NVM release, website users, framework choices, and paths come from backend allowlists or persisted panel records.
- Commands use fixed executables and positional arguments. Request values are never interpolated into an unrestricted shell command.
- Runtime and framework commands run as the website user; root access is limited to account, ownership, Nginx, PHP-FPM, and systemd operations that require it.
- Downloads use HTTPS and bounded timeouts, and the pinned NVM source is validated before activation.
- A failed NVM download or Node.js installation does not overwrite a previously working runtime.
- A failed version change does not update the website's selected version or restart applications against an unverified runtime.
- A failed Laravel migration leaves the website in a retryable failed state and does not report successful provisioning.
- Concise errors are shown in the UI while full command output remains in persistent task logs.
- Runtime detection and retry tolerate missing home directories, partially installed NVM trees, stopped services, and a global Node.js installation without confusing those states.

## Compatibility and Migration

The database migration is additive and idempotent. Existing website rows, document roots, Nginx configurations, SSL state, PHP settings, and project files remain unchanged.

Assigning Node.js 24 to an existing website is metadata compatibility only. Startup and panel update perform no automatic NVM installation. A legacy global Node.js package remains installed until the administrator explicitly removes it, so applications not yet migrated to NVM are not silently broken.

Existing Node.js application records are normalized to their owning website's runtime when their panel-managed units are next explicitly recreated, restarted after a runtime change, or edited. Merely viewing the page or updating the panel does not rewrite running services.

## Testing and Acceptance Criteria

Backend tests must prove:

- the migration preserves existing rows, assigns their compatibility version, and remains idempotent;
- new websites can explicitly persist no Node.js or a supported version;
- unsupported versions and invalid template/runtime combinations are rejected before provisioning;
- runtime detection distinguishes selected, installed, partially installed, and mismatched versions;
- install and change-version operations execute as the website user, validate the pinned NVM source, verify Node.js/npm, and update metadata only after success;
- npm provisioning commands use the selected per-user NVM runtime and never `/usr/bin/npm`;
- generated systemd units use the website's NVM runtime and cannot reference an arbitrary user path;
- a failed runtime change preserves the previous valid runtime and records retryable progress;
- global Node.js is not removed by panel update or website provisioning;
- Laravel automatic installation creates SQLite with safe ownership, runs the initial migration, and supports idempotent retry;
- configuration-only mode performs none of the automatic installation or database steps.

Frontend tests must prove:

- the website form exposes none, 20, 22, and 24;
- Node.js defaults and required validation follow the selected template, variant, and setup mode;
- the Native PHP label remains unchanged;
- the Node.js page reports per-website selected and detected state;
- install, reinstall, and change-version tasks retain progress after refresh;
- the global installer is absent and legacy global removal requires confirmation;
- successful operations refresh the displayed runtime data.

Before merge, the full Go test suite, frontend unit tests, Svelte type checks, production frontend build, Go build, `go vet`, migration checks, and focused systemd/NVM rendering tests must pass. The implementation diff must be reviewed before commit and push to `main`.
