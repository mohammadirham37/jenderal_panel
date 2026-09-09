# Nginx, Firewall, Alert, and Notification Reliability Design

**Date:** 2026-09-09

## Goal

Make the Nginx configuration easier to edit and repair the Firewall, Alert, and
Notification pages so every action shown in the UI performs a real backend
operation on Ubuntu 24.04.

## Scope

This change covers:

- Simple and Manual editing modes on `/nginx`.
- Firewall status, enable/disable, rule creation, and rule deletion.
- Alert rule CRUD, sustained-duration evaluation, target-aware service and SSL
  checks, deduplication, recovery, and notification dispatch.
- Notification channel CRUD, enable/disable, test delivery, and real delivery
  through Email, Telegram, Discord, and generic Webhook channels.
- Compatibility aliases for the plural API paths currently used by released
  frontend builds.

It does not introduce a general-purpose Nginx visual editor, arbitrary alert
expressions, notification retry queues, or encrypted secret storage.

## Nginx Configuration

The existing raw Nginx configuration remains the source of truth. The frontend
will offer two modes:

- **Simple:** a form for commonly changed directives.
- **Manual:** the existing full-text editor.

The Simple form exposes:

- `worker_processes` (`auto` or a positive integer), in the main context.
- `worker_connections` (positive integer), in the `events` context.
- `client_max_body_size` (valid Nginx size), in the `http` context.
- `keepalive_timeout` (non-negative seconds), in the `http` context.
- `server_tokens` (`on` or `off`), in the `http` context.
- `gzip` (`on` or `off`), in the `http` context.

A small frontend parser reads the first active occurrence of each managed
directive in its required context. Saving a Simple field replaces only that
directive. If an optional `http` directive is missing, it is inserted into the
existing `http` block. Required structural blocks are not synthesized: a
missing `events` or `http` block is reported and the user can repair it in
Manual mode.

Switching modes preserves the unsaved draft. Both modes save through the
existing Nginx PUT endpoint, retaining server-side `nginx -t`, backup,
rollback, and reload behavior.

## Firewall Contract

The canonical firewall status response remains the backend shape:

```json
{
  "active": true,
  "default": "deny (incoming), allow (outgoing)",
  "rules": []
}
```

The frontend will consume this object directly and treat rule ports as numbers.
The backend will reject invalid UFW commands instead of interpreting failed
command output as an inactive firewall. Parsed actions such as `ALLOW IN` and
`DENY IN` will be normalized for display styling without changing their shown
text.

Existing safeguards around SSH access remain in place.

## Alert Rules

### API and persistence

The canonical endpoints remain `/api/v1/alert-rules` and
`/api/v1/alert-history`. Compatibility aliases will also accept the currently
released plural frontend paths `/api/v1/alerts/rules` and
`/api/v1/alerts/history`.

An additive database migration adds a nullable `target` column to alert rules.
Existing rules remain valid because system metrics do not require a target.

Create requests default `enabled` to true when the field is omitted. Update
requests use pointer fields so small updates such as `{ "enabled": false }`
do not erase the metric, operator, threshold, duration, or target.

### Supported evaluators

- `cpu`, `ram`, `disk`, `load1`, `load5`, and `load15` read the latest metrics.
- `service_down` requires a systemd service target. It triggers when that unit
  is not active.
- `ssl_expiry` requires a website/domain target. It compares the number of days
  remaining on the active certificate to the configured threshold.

The UI changes its target selector according to the selected metric. Services
come from the existing service API and SSL targets come from installed
certificate/website data. Invalid or missing targets return validation errors.

### Lifecycle

The checker continues to run periodically. A rule must remain violated for
`duration_s` before it fires. A zero duration fires on the first evaluation.
Pending duration state is kept in memory and resets safely when the process
restarts.

Before creating an alert, the checker looks for an unresolved alert for the
same rule. This prevents duplicate history and notification spam. When the
condition recovers, the unresolved alert is marked resolved and one recovery
notification is sent. Disabling or deleting a rule prevents future evaluation.

## Notification Channels

The canonical endpoint remains `/api/v1/notification-channels`. A compatibility
alias will also accept `/api/v1/notifications/channels`.

The API exposes channel `config` as a JSON object while the SQLite model keeps
storing it as JSON text. Request/response DTOs perform this conversion and
validate the schema for each channel type. Create defaults `enabled` to true;
partial updates support toggle-only requests.

Supported configurations are:

- **Email:** SMTP host, port, username, password, sender, recipient, and
  encryption mode (`starttls`, `tls`, or `none`). STARTTLS verifies server
  certificates; implicit TLS uses a TLS connection before SMTP authentication.
- **Telegram:** bot token and chat ID.
- **Discord:** webhook URL.
- **Webhook:** URL plus optional authorization header.

Email delivery will be implemented rather than logged as a successful stub.
The Test action uses the exact saved channel implementation used for real
alerts. Password/token inputs are masked in the UI. Secrets remain present in
the authenticated API response only as needed by the current editor; they are
never written to application logs or error messages.

## Error Handling

- Backend validation failures use the existing structured API error format.
- Command failures preserve useful stderr without presenting raw JSON envelopes
  in the UI.
- Pages keep their controls usable after failed requests and show a clear local
  error message.
- Notification transport errors include safe context such as channel type and
  destination, but exclude credentials and tokens.

## Compatibility and Rollout

All database changes are additive. Existing Nginx text, firewall rules, alert
rules, histories, and notification records remain intact. Canonical and legacy
route aliases share the same handlers, preventing old browser bundles from
breaking during an in-place panel update.

## Verification

Automated tests will cover:

- Context-aware Nginx parsing, replacement, insertion, validation, and draft
  preservation.
- Firewall JSON shape, numeric ports, failed UFW commands, and action styling.
- Alert create defaults, partial updates, target validation, duration,
  deduplication, firing, recovery, service checks, and SSL expiry checks.
- Notification JSON conversion, partial updates, config validation, SMTP modes,
  and HTTP channel deliveries using local test servers.
- Frontend builds and existing Go and web test suites.

Final verification will include `go test ./...`, the frontend test command, and
the production frontend build before committing and pushing to `main`.
