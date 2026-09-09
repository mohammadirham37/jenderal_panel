# Per-website Node.js and Laravel repair

## Runtime selection

Each website has an optional Node.js major version: 20, 22 or 24. Node.js 24 is the default for automatic templates that build frontend assets. Static and Native PHP websites can select no Node.js.

Node.js runs from the website user's NVM installation under `/home/<web-user>/.nvm`. Applications belonging to the same website share that runtime. Configuration-only setup records the chosen version; use the Node.js page to install it when needed.

An upgraded website may show selected version 24 with no runtime installed. This is expected: upgrading panel metadata does not download Node.js or restart existing applications. Use the per-website Install action to migrate the runtime explicitly.

The panel uses a separate Node.js 24 NVM environment for its own frontend builds. Removing the old global apt package is an explicit action and must wait until panel-managed applications no longer depend on it. Check scripts and applications managed outside the panel before confirming removal.

## Laravel SQLite

Automatic Laravel setup creates the default SQLite database and runs initial migrations as the website user. It preserves an existing database and application key when retried. Configuration-only setup leaves database management to the deployment owner.

For an existing automatically installed Laravel website that reports a missing SQLite file or session table, use **Repair Laravel** in Websites. Review the confirmation because pending SQLite migrations will run. The operation leaves the website's Nginx and SSL configuration intact and reports its result through task progress.

If PHP reports a missing `pdo_sqlite` extension, install SQLite support for the selected PHP version using the PHP management page, then retry repair. Explicit external database settings are preserved and are not migrated by this repair action.

## Ubuntu 24.04 acceptance scenarios

These checks require a disposable VPS; local unit tests do not prove systemd, apt or network access on that VPS.

1. Create two automatic frontend websites with Node.js 22 and 24. Confirm the Node.js page detects their different versions and that both asset builds finish.
2. Create a configuration-only Native PHP website with no Node.js. Confirm it serves its landing page and has no `.nvm` installation.
3. Refresh during a runtime installation. Confirm task progress remains visible and completion refreshes the installed version.
4. Change one website's runtime. Confirm only that website's Node applications switch versions; a failed download leaves its prior runtime usable.
5. Create a fresh Laravel application with SQLite. Open the HTTPS domain and exercise a session-backed page. Confirm there is no missing-file or missing-session-table exception.
6. Repair an older auto-installed Laravel project with a missing SQLite file. Repeat repair and confirm existing SQLite data and APP_KEY remain unchanged.
7. Repair a project with an explicitly configured external database. Confirm its connection settings and external database are untouched and the task explains that database setup remains manual.
8. Remove legacy global Node only after all panel apps are migrated and unrelated applications are checked. Run a subsequent panel update and confirm the panel-owned NVM runtime builds the frontend successfully.
