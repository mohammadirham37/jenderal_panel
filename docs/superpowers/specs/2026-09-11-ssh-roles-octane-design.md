# Design: Panel Users as SSH Accounts, Role Scoping, Laravel Octane (FrankenPHP)

Status: draft for review
Date: 2026-09-11

Three related features requested by the maintainer:

1. Panel users also exist as SSH accounts, with SSH port management and SSH
   public-key (RSA) login from the panel.
2. Two roles with row-level ownership: `admin` keeps full power; `user` may
   only manage the websites and databases they created.
3. Laravel deployment using Octane with FrankenPHP and a Caddyfile.

## Decisions taken without confirmation (defaults, easy to change)

The review questions below were defaulted to the recommended options. Flag
any of them and the affected section changes:

| # | Question | Default taken |
|---|----------|---------------|
| 1 | SSH account ↔ website relationship | One Linux account per panel user, granted group/ACL access to owned sites |
| 2 | SSH auth method | Public key only (password login disabled for panel accounts) |
| 3 | How Octane serves traffic | Behind the existing nginx (TLS termination + proxy to loopback port) |
| 4 | Octane scope | New template choice **and** an enable/disable conversion for existing Laravel sites |

---

## Phase 1 — Roles and ownership scoping (foundation)

Ownership must exist before SSH accounts can be mapped to "the sites a user
owns", so this phase lands first.

### Data model

- Migration: add `created_by TEXT` (nullable) to `websites` and
  `managed_databases`. `db_users` inherit scope through their parent
  database.
- Backfill: existing rows keep `NULL`; `NULL` is interpreted as
  "admin-owned legacy resource". Admins can transfer ownership from the
  website detail page later.

### Enforcement

- New helper in `internal/model` or a small `internal/scope` package:
  `CanManageWebsite(user, website) bool` and the database equivalent.
  Admin role → always true; user role → `created_by == user.ID`.
- Website service: `List` gains a scope filter; `Get`-backed mutators
  (update, delete, commands, files, env, logs, ssl, config, terminal,
  deploy) call the guard after loading the row. `dbmanager` mirrors this
  for managed databases and db users.
- RBAC: keep module permissions (`websites.*`, `databases.*`) for the user
  role, but strip server-wide permissions (server.*, nginx.*, firewall.*,
  ssh.*, users.* …). Because the current seed is `INSERT OR IGNORE` and
  never revokes, add `RBAC.SyncRolePermissions(ctx, "user", perms)` run at
  startup so the user role's set is declarative and converges on upgrade.
- User accounts are created by admins on the Users page. The user role
  cannot create accounts and cannot list other users.
- Audit log stays admin-only (no user scoping in phase 1).

### Frontend

- Website/database lists already render from API responses; scoping is
  server-side, so most pages need no change. Permission-safe fetching
  (the dashboard `getSafe` pattern) stays the rule for optional cards.
- Website detail page: show owner (admin view) + ownership transfer
  control (admin only).
- Users page: role selector, SSH-access toggle, and per-user SSH key
  management (see phase 2).

---

## Phase 2 — Panel users as SSH accounts

### System account provisioning (`internal/sshaccount`)

- Creating a panel user with **SSH access enabled** (checkbox, default on
  for the `user` role, off for `admin` unless requested) provisions a
  Linux account:
  - username = panel username, validated `^[a-z_][a-z0-9_-]{0,31}$`,
    checked against `/etc/passwd` collisions and the `web_` prefix;
  - home `/home/<username>`, shell `/bin/bash`, password locked
    (`usermod -p '*'`) because auth is key-only;
  - for each website the user owns: add the account to the site's
    `web_<domain>` group and grant read/write via POSIX ACLs
    (`setfacl -R -m u:<paneluser>:rwX -d -m u:<paneluser>:rwX` on the site
    project directories) so SFTP/SSH file edits work without loosening
    group permissions for everyone else. Fallback to `chmod -R g+rwX` if
    `setfacl` is unavailable.
- Panel username becomes immutable once SSH is enabled (system account
  rename is not worth the edge cases).
- Deactivating a panel user locks + sets `nologin` on the shell account
  (files preserved); deleting the panel user deletes the system account
  with `userdel -r` after an explicit confirmation naming the home
  directory.
- Grant/revoke of website ownership re-runs the ACL/group sync.

### SSH keys (`user_ssh_keys` table)

- Columns: `id, user_id, name, public_key, fingerprint, algo, bits,
  created_at`.
- Add flow: paste an OpenSSH public key (ssh-rsa / ed25519 / ecdsa).
  Validation runs `ssh-keygen -lf <tmpfile>` via the executor — the
  fingerprint, algorithm, and bit size returned by ssh-keygen are stored,
  never trusted from client input. Private keys are never accepted or
  stored.
- Enforcement of "source of truth = database": adding/removing a key
  rewrites `/home/<username>/.ssh/authorized_keys` (0700 dir, 0600 file,
  owned by the account) from the DB list in one atomic move. This makes
  drift impossible and removal effective immediately.
- UI: admin sees a Keys section per user on the Users page; a non-admin
  user manages their own keys under a "SSH Keys" card (profile area).

### SSH port management (admin, server settings)

- New card in Server settings backed by `internal/sshserver`:
  - **Read**: current effective port from `sshd -T` (authoritative,
    no restart needed).
  - **Change** (two-phase, lockout-safe, executed by taskrunner):
    1. Write `Port <new>` to `/etc/ssh/sshd_config.d/99-jenderal.conf`
       while keeping the old port accepted;
    2. `sshd -t` — abort and roll back the drop-in on any parse error;
    3. `ufw allow <new>/tcp` **before** restarting ssh (reuse the
       firewall module's primitives); the old allow rule stays;
    4. restart `ssh.service` via the ServiceManager allow-list;
    5. UI asks the operator to confirm they can log in on the new port,
       and only then offers "Finalize", which removes the old `Port`
       line and deletes the old `ufw` rule.
- Validation: 1–65535, must differ from the panel's own listen port and
  from ports of managed sites; refuse values already claimed in the
  firewall module's rule list without explicit override.
- Every step is audit-logged (`ssh_port_change_start`,
  `ssh_port_change_finalize`, rollback events).

---

## Phase 3 — Laravel Octane with FrankenPHP + Caddyfile

### Runtime model

- **nginx stays in front** (TLS via the existing SSL module, domains
  module unchanged): the generated nginx profile `laravel-octane`
  terminates TLS and `proxy_pass`es to the site's Octane worker on
  `127.0.0.1:<port>` (websocket upgrade headers included; static assets
  under `public/build`/`public` served directly by nginx when present).
  A standalone Caddy taking 80/443 was rejected: it would fight nginx for
  the ports and bypass the SSL/domains modules.
- **FrankenPHP provides the app server and its embedded Caddy.** The
  Caddyfile requirement is satisfied per-site: Octane's FrankenPHP server
  runs from a generated Caddyfile that binds only
  `127.0.0.1:<port>` (never public), so the Caddy layer is real but
  loopback-only.
- FrankenPHP is installed server-wide (admin action, taskrunner) as the
  official static binary, pinned version + sha256 checksum stored in
  panel settings code (supply-chain safety). Verify with
  `frankenphp -v` after install.

### Data model & provisioning

- `websites` gains `octane_enabled INTEGER` and `octane_port INTEGER`.
- Port allocation: pick the lowest free port in 8100–8199 at provision
  time (checked against other sites and listening sockets); stored, so
  nginx/Caddyfile/systemd always agree.
- New template `laravel-octane` in `ResolveProfile`:
  - RelativeProjectRoot `app`, RelativeDocumentRoot `app/public`
    (same layout as Laravel);
  - composer create-project + `php artisan octane:install --server=frankenphp`;
  - panel writes the site's Caddyfile (frankenphp section:
    `http://127.0.0.1:<port>`, octane workers/threads from site settings);
  - systemd unit `jenderal-octane-<webuser>.service`
    (`ExecStart=sudo -u <webuser> php artisan octane:start --server=frankenphp
    --host=127.0.0.1 --port=<port>`, `Restart=always`) registered in the
    ServiceManager allow-list via the existing `jenderal-*` glob approach;
  - nginx vhost rendered from the new `laravel-octane` profile template.
- **Conversion of existing Laravel sites** (enable/disable Octane card on
  the detail page): runs the same install steps against the existing
  project, switches the nginx vhost profile, and keeps the old PHP-FPM
  vhost available as instant rollback while enabled=false.

### Day-2 operations

- Detail page Octane card: running/stopped (systemd), port, workers,
  Start/Stop/Restart, `octane:reload` for zero-downtime deploys.
- Command presets gain `php artisan octane:reload` (plus the existing
  `git pull` works in the project root) so the common deploy loop is
  `git pull` → `octane:reload`.
- Logs tab gains the Octane server log
  (`storage/logs/octane-server-*.log`).
- Note in docs: PHP version selection applies to CLI/artisan (system PHP);
  the HTTP request path runs on FrankenPHP's embedded PHP build — the
  provisioning step prints both versions so mismatches are visible.

---

## Testing

- Pure unit tests (no live server): username validation, ssh-key parse/
  fingerprint via command contract (mock executor), port validation,
  ownership guard truth table, octane port allocator, Caddyfile/nginx
  template rendering, command presets.
- All repo gates per testing policy (`go test`, `npm run check/test`);
  SSH port change, sshd/ufw interplay, and Octane start/stop are
  maintainer-verified manually on Ubuntu 24.04 — the panel must never
  test those against a live server automatically.
- Rollback safety notes: ssh port change is two-phase; Octane conversion
  keeps the PHP-FPM vhost as rollback; ACL changes are additive.

## Suggested implementation order

1. Phase 1 (ownership + RBAC sync) — small, unblocks everything.
2. Phase 2 (SSH accounts, keys, port) — largest security surface.
3. Phase 3 (Octane stack) — independent, can proceed in parallel after 1.
