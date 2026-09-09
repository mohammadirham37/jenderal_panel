# Notification Delivery Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make notification channel CRUD, toggles, tests, and real delivery work for Email, Telegram, Discord, and Webhook.

**Architecture:** Handler DTOs expose config as a JSON object while the service keeps JSON text in SQLite. Partial updates merge with stored records. Channel senders validate type-specific schemas, share an injectable HTTP client, and use an injectable SMTP transport for plain, STARTTLS, and implicit TLS connections.

**Tech Stack:** Go standard library (`net/http`, `net/smtp`, `crypto/tls`), SQLite, chi, Svelte 5.

**Spec:** `docs/superpowers/specs/2026-09-09-nginx-firewall-alert-notification-design.md`

## Global Constraints

- New channels default to enabled when `enabled` is omitted.
- Partial toggle requests preserve channel type and config.
- Credentials and tokens never appear in application logs or transport errors.
- Existing JSON-text rows remain compatible.
- Target runtime is Ubuntu 24.04.

---

### Task 1: JSON-object API and partial updates

**Files:**
- Create: `internal/notification/dto.go`
- Modify: `internal/notification/handler.go`
- Modify: `internal/notification/service.go`
- Modify: `internal/notification/service_test.go`
- Create: `internal/notification/handler_test.go`
- Modify: `internal/api/router.go`

**Interfaces:**
- Produces: `ChannelResponse` with `Config map[string]any`.
- Produces: `CreateChannelRequest { Type string; Config json.RawMessage; Enabled *bool }`.
- Produces: `UpdateChannelRequest { Type *string; Config json.RawMessage; Enabled *bool }`.

- [ ] **Step 1: Write failing request/response tests**

Test object-shaped config output, enabled-by-default creation, toggle-only
updates preserving stored fields, malformed config rejection, and the plural
`/notifications/channels` alias reaching the same handler.

- [ ] **Step 2: Run focused tests and confirm failure**

Run: `go test ./internal/notification ./internal/api -run 'Test.*(Channel|NotificationAlias)' -v`

Expected: FAIL because config is currently a string and updates require full records.

- [ ] **Step 3: Implement DTO conversion and merging**

Marshal `json.RawMessage` to compact JSON text before calling the storage
service and unmarshal stored text into `map[string]any` responses. For updates,
load the stored channel, apply only present fields, validate the merged record,
and update it. Register plural list/create/update/delete/test aliases with the
same RBAC permissions as canonical routes.

- [ ] **Step 4: Run service and handler tests**

Run: `go test ./internal/notification ./internal/api -v`

Expected: PASS.

- [ ] **Step 5: Commit API repairs**

```bash
git add internal/notification/dto.go internal/notification/handler.go internal/notification/handler_test.go internal/notification/service.go internal/notification/service_test.go internal/api/router.go
git commit -m "fix: align notification channel api contracts"
```

### Task 2: Validate channel configurations and HTTP delivery

**Files:**
- Modify: `internal/notification/channels.go`
- Create: `internal/notification/channels_test.go`

**Interfaces:**
- Produces: `ValidateConfig(channelType, config string) error`.
- Produces: Webhook config fields `url` and optional `authorization`.

- [ ] **Step 1: Write failing validation and HTTP tests**

Use `httptest.Server` to assert Webhook sends `{ "text": message }` plus an
optional Authorization header, Discord sends `content`, Telegram sends
`chat_id` and `text`, non-2xx responses fail, and every missing required field
returns a validation error without echoing secrets.

- [ ] **Step 2: Run focused tests and confirm failure**

Run: `go test ./internal/notification -run 'Test(SendWebhook|SendDiscord|SendTelegram|ValidateConfig)' -v`

Expected: at least the authorization and centralized validation cases fail.

- [ ] **Step 3: Implement validated HTTP senders**

Extend `WebhookConfig` with `Authorization string`, build requests with
`http.NewRequestWithContext`, set JSON content type, and add the header only
when non-empty. Route all create/update validation through `ValidateConfig` so
Test and real delivery use the same schema.

- [ ] **Step 4: Run notification tests**

Run: `go test ./internal/notification -v`

Expected: PASS.

- [ ] **Step 5: Commit HTTP channel behavior**

```bash
git add internal/notification/channels.go internal/notification/channels_test.go internal/notification/service.go
git commit -m "fix: validate notification http channels"
```

### Task 3: Real SMTP delivery

**Files:**
- Create: `internal/notification/smtp.go`
- Create: `internal/notification/smtp_test.go`
- Modify: `internal/notification/channels.go`

**Interfaces:**
- Produces: `EmailConfig` fields `smtp_host`, `smtp_port`, `username`, `password`, `from`, `to`, and `encryption`.
- Produces: `SendEmail(ctx context.Context, config, message string) error`.
- Produces: internal `smtpDialer` seam for local fake SMTP tests.

- [ ] **Step 1: Write failing SMTP tests**

Test defaults (`smtp_port=587`, `encryption=starttls`), RFC-style From/To/Subject
headers, optional AUTH when credentials are empty, AUTH when both credentials
exist, STARTTLS upgrade, implicit TLS dialing, invalid encryption rejection,
and errors that omit username/password.

- [ ] **Step 2: Run focused tests and confirm failure**

Run: `go test ./internal/notification -run 'TestSendEmail' -v`

Expected: FAIL because Email currently only logs a stub message.

- [ ] **Step 3: Implement SMTP transport**

Use `smtp.NewClient` for plain/STARTTLS and `tls.Dialer.DialContext` followed by
`smtp.NewClient` for implicit TLS. Set `tls.Config{ServerName: host, MinVersion:
tls.VersionTLS12}`. Authenticate with `smtp.PlainAuth` only when username and
password are both present. Send a UTF-8 text message through `MAIL`, `RCPT`,
`DATA`, and `QUIT`, closing safely on every failure.

- [ ] **Step 4: Run all notification tests**

Run: `go test ./internal/notification -v`

Expected: PASS with no credentials in captured logs/errors.

- [ ] **Step 5: Commit SMTP delivery**

```bash
git add internal/notification/smtp.go internal/notification/smtp_test.go internal/notification/channels.go
git commit -m "feat: send email notifications over smtp"
```

### Task 4: Notification channel UI

**Files:**
- Modify: `web/src/routes/notifications/+page.svelte`
- Create: `web/tests/notifications/Notifications.test.mjs`
- Modify: `web/package.json`

**Interfaces:**
- Consumes: canonical `/api/v1/notification-channels` object-config API.

- [ ] **Step 1: Write a failing source-level UI test**

Assert the page uses canonical paths; includes every Email key and encryption
choice; includes Telegram, Discord, and Webhook schemas; masks password/token
inputs; and keeps toggle-only PUT requests.

- [ ] **Step 2: Run the source test and confirm failure**

Run: `cd web && node --test tests/notifications/Notifications.test.mjs`

Expected: FAIL because the existing UI exposes a raw JSON textarea with an incomplete Email placeholder.

- [ ] **Step 3: Implement type-aware forms**

Replace raw JSON creation with fields for the selected channel type. Keep an
advanced JSON textarea in edit mode only if it round-trips the same object.
Mask `password` and `bot_token`, show safe config previews that redact those
keys, and use canonical paths for list/create/update/delete/test.

- [ ] **Step 4: Register and verify frontend tests**

Add `tests/notifications/*.test.mjs` to `web/package.json`.

Run: `cd web && npm test && npm run check && npm run build`

Expected: all commands exit 0.

- [ ] **Step 5: Commit the Notification UI**

```bash
git add web/src/routes/notifications/+page.svelte web/tests/notifications/Notifications.test.mjs web/package.json
git commit -m "feat: add working notification channel forms"
```

### Task 5: Integrated regression verification

**Files:**
- None; this task only verifies the completed implementation.

**Interfaces:**
- Consumes: all completed Nginx, Firewall, Alert, and Notification tasks.

- [ ] **Step 1: Run formatting and static checks**

Run: `gofmt -w cmd/jenderal/main.go internal/alert/*.go internal/firewall/*.go internal/notification/*.go`

Run: `go vet ./...`

Expected: both commands exit 0.

- [ ] **Step 2: Run complete backend tests**

Run: `go test ./...`

Expected: PASS.

- [ ] **Step 3: Run complete frontend verification**

Run: `cd web && npm test && npm run check && npm run build`

Expected: all commands exit 0.

- [ ] **Step 4: Review the final diff**

Run: `git diff --check && git status --short && git log --oneline --decorate -12`

Expected: no whitespace errors; only intended changes remain.

- [ ] **Step 5: Push the verified main branch**

```bash
git push origin main
```

Expected: remote `main` advances to the final verified commit.
