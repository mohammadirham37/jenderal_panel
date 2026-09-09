# Website-Scoped Operations Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Place Deployment, SSL, Cron Jobs, and Queue Workers inside a selected website's detail area with backend-enforced scoped list and create operations.

**Architecture:** Preserve existing services and resource-ID actions, expose their existing website filters through nested API handlers, and adapt the existing global Svelte pages into nested routes. A shared, presentation-only website navigation component connects the five website screens; legacy UI routes redirect while legacy APIs stay compatible.

**Tech Stack:** Go 1.x, chi, SQLite, Svelte 5/SvelteKit, Node test runner.

**Spec:** `docs/superpowers/specs/2026-09-10-website-scoped-operations-design.md`

## Global Constraints

- No database migration; all resources already store `website_id`.
- URL `{id}` is authoritative for scoped create operations.
- Existing global APIs and resource-ID actions remain available.
- Existing RBAC permission names remain unchanged.
- File Manager remains on the Overview route.
- No unrelated visual redesign or state-management dependency.

---

### Task 1: Validate Scoped Service Lists

**Files:**
- Modify: `internal/cron/service.go`
- Modify: `internal/cron/service_test.go`
- Modify: `internal/queue/service.go`
- Modify: `internal/queue/service_test.go`
- Modify: `internal/ssl/service.go`
- Modify: `internal/ssl/service_test.go`

**Interfaces:**
- Produces: existing `ListByWebsite(ctx, websiteID)` methods that return `model.ErrNotFound` when the website does not exist.
- Consumes: existing website and resource tables.

- [ ] **Step 1: Add failing service tests**

For each service, insert two websites and one resource per website. Assert that `ListByWebsite(ctx, "site-a")` returns only the `site-a` resource. Also assert:

```go
_, err := svc.ListByWebsite(context.Background(), "missing-site")
if !errors.Is(err, model.ErrNotFound) {
    t.Fatalf("error = %v, want model.ErrNotFound", err)
}
```

- [ ] **Step 2: Run focused tests and verify RED**

Run: `go test ./internal/cron ./internal/queue ./internal/ssl`

Expected: isolation cases pass against existing queries; missing-site cases FAIL because existing methods return an empty list.

- [ ] **Step 3: Add website existence validation**

At the start of each `ListByWebsite`, execute `SELECT 1 FROM websites WHERE id = ?`. Map `sql.ErrNoRows` to `model.ErrNotFound`, then run the existing filtered query. Do not load a global list or filter in Go.

- [ ] **Step 4: Verify service tests GREEN**

Run: `go test ./internal/cron ./internal/queue ./internal/ssl`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/cron/service.go internal/cron/service_test.go internal/queue/service.go internal/queue/service_test.go internal/ssl/service.go internal/ssl/service_test.go
git commit -m "fix(operations): validate scoped website lists"
```

### Task 2: Expose Website-Scoped API Handlers

**Files:**
- Modify: `internal/cron/handler.go`
- Create: `internal/cron/handler_test.go`
- Modify: `internal/queue/handler.go`
- Create: `internal/queue/handler_test.go`
- Modify: `internal/ssl/handler.go`
- Modify: `internal/ssl/handler_test.go`
- Modify: `internal/api/router.go`
- Create: `internal/api/website_operation_routes_test.go`

**Interfaces:**
- Produces: `ListForWebsite` and `CreateForWebsite` on Cron and Queue handlers.
- Produces: `ListForWebsite`, `IssueForWebsite`, and `InstallCustomForWebsite` on SSL handler.
- Produces: scoped routes from the design spec.

- [ ] **Step 1: Add failing handler tests for URL ownership**

Mount each handler on a chi route containing `{id}`. POST bodies omit `website_id`; after a successful request, decode the result and assert its `website_id` equals the URL literal. For list handlers, seed two websites and assert only the route website's resource is returned. SSL custom tests must use generated test certificate material and continue to assert that material is absent from responses and audit logs.

- [ ] **Step 2: Run handler packages and verify RED**

Run: `go test ./internal/cron ./internal/queue ./internal/ssl`

Expected: build/test failure because the scoped handler methods do not exist.

- [ ] **Step 3: Implement scoped handlers**

Cron and Queue scoped create handlers decode their existing request type and overwrite ownership before calling the service:

```go
req.WebsiteID = chi.URLParam(r, "id")
created, err := h.svc.Create(r.Context(), req)
```

SSL scoped issue/custom handlers decode bodies without trusting a body website ID and call `Issue`/`InstallCustom` with `chi.URLParam(r, "id")`. Extract private response/audit helpers only where required to keep global and scoped behavior identical.

- [ ] **Step 4: Add route registration tests and routes**

Register the seven scoped routes under the existing permissions:

```text
GET  /websites/{id}/ssl
POST /websites/{id}/ssl/issue
POST /websites/{id}/ssl/custom
GET  /websites/{id}/cron-jobs
POST /websites/{id}/cron-jobs
GET  /websites/{id}/queue-workers
POST /websites/{id}/queue-workers
```

The route test walks chi routes and asserts these method/path pairs plus representative legacy pairs remain registered.

- [ ] **Step 5: Verify backend GREEN**

Run: `go test ./internal/cron ./internal/queue ./internal/ssl ./internal/api`

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/cron internal/queue internal/ssl/handler.go internal/ssl/handler_test.go internal/api/router.go internal/api/website_operation_routes_test.go
git commit -m "feat(api): scope operations by website"
```

### Task 3: Add Website Section Navigation

**Files:**
- Create: `web/src/lib/website-sections.js`
- Create: `web/src/lib/components/WebsiteSectionNav.svelte`
- Create: `web/tests/websites/WebsiteSections.test.mjs`
- Modify: `web/src/routes/websites/[id]/+page.svelte`
- Modify: `web/src/routes/+layout.svelte`

**Interfaces:**
- Produces: `websiteSectionLinks(websiteID)` returning Overview, Deployments, SSL, Cron Jobs, and Queue Workers links.
- Produces: `<WebsiteSectionNav websiteId currentPath />`.

- [ ] **Step 1: Add failing navigation tests**

Assert the helper returns these literal hrefs for `site/a` using URL encoding:

```js
[
  '/websites/site%2Fa',
  '/websites/site%2Fa/deployments',
  '/websites/site%2Fa/ssl',
  '/websites/site%2Fa/cron',
  '/websites/site%2Fa/queue-workers'
]
```

Compile the component with `svelte/compiler` and assert compilation succeeds. Rendered links must derive from the helper, and the matching link receives `aria-current="page"`.

- [ ] **Step 2: Run focused test and verify RED**

Run: `cd web && node --test tests/websites/WebsiteSections.test.mjs`

Expected: FAIL because the helper/component do not exist.

- [ ] **Step 3: Implement helper and component**

The helper owns the labels and encoded URLs. The component iterates those links and determines the active link from exact pathname equality.

- [ ] **Step 4: Place navigation on Overview and remove global sidebar entries**

Render `WebsiteSectionNav` on `websites/[id]/+page.svelte` after the website header. Remove only `/ssl`, `/deployments`, `/cron`, and `/queue-workers` from `navGroups`; retain translation strings for compatibility.

- [ ] **Step 5: Verify navigation GREEN**

Run: `cd web && node --test tests/websites/WebsiteSections.test.mjs tests/branding/BrandPlacement.test.mjs`

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add web/src/lib/website-sections.js web/src/lib/components/WebsiteSectionNav.svelte web/tests/websites/WebsiteSections.test.mjs web/src/routes/websites/'[id]'/+page.svelte web/src/routes/+layout.svelte
git commit -m "feat(websites): add operation navigation"
```

### Task 4: Move Deployment and SSL Pages

**Files:**
- Move: `web/src/routes/deployments/+page.svelte` to `web/src/routes/websites/[id]/deployments/+page.svelte`
- Move: `web/src/routes/ssl/+page.svelte` to `web/src/routes/websites/[id]/ssl/+page.svelte`
- Create: `web/src/lib/website-operations.js`
- Modify: `web/src/lib/ssl-form.js`
- Modify: `web/tests/ssl/SSLForm.test.mjs`
- Create: `web/tests/websites/WebsiteOperationPages.test.mjs`

**Interfaces:**
- Consumes: route `page.params.id`, `WebsiteSectionNav`, existing deployment endpoints, and new scoped SSL endpoints.
- Produces: nested Deployment and SSL management screens without website selectors.
- Produces: `websiteOperationAPI(websiteID)` with encoded website, deployment, SSL, Cron, and Queue endpoint strings.
- Produces: `buildWebsiteSSLInstallRequest(mode, websiteID, values)` with scoped paths and bodies that omit `website_id`.

- [ ] **Step 1: Add failing SSL request and nested-page tests**

Assert literal scoped requests:

```js
assert.deepEqual(
  buildWebsiteSSLInstallRequest('letsencrypt', 'site/1', { domain: 'example.com' }),
  { path: '/api/v1/websites/site%2F1/ssl/issue', body: { domain: 'example.com' } }
);
```

Assert `websiteOperationAPI('site/1')` returns the literal encoded endpoint strings for all four modules. Compile both wished-for nested pages with `svelte/compiler`; walk their ASTs and assert the former website selector IDs (`deploy-website` and the SSL website selector ID) are absent. The test must fail while the helper and nested files are absent.

- [ ] **Step 2: Run focused tests and verify RED**

Run: `cd web && node --test tests/ssl/*.test.mjs tests/websites/WebsiteOperationPages.test.mjs`

Expected: FAIL because the scoped SSL builder and nested pages are absent.

- [ ] **Step 3: Move and adapt Deployment**

Use `page.params.id` as the only website ID, load `/api/v1/websites/{id}` before deployments, remove the website list/select state and markup, render shared navigation, and retain the existing five-second running-deployment poll with cleanup.

- [ ] **Step 4: Move and adapt SSL**

Load the route website and `/api/v1/websites/{id}/ssl`, derive installable domains from that one website, remove the website selector, submit issue/custom requests through `buildWebsiteSSLInstallRequest`, and retain scoped polling plus existing certificate-ID actions.

- [ ] **Step 5: Verify Deployment and SSL GREEN**

Run: `cd web && node --test tests/ssl/*.test.mjs tests/websites/WebsiteOperationPages.test.mjs`

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add web/src/routes/deployments web/src/routes/ssl web/src/routes/websites/'[id]'/deployments web/src/routes/websites/'[id]'/ssl web/src/lib/website-operations.js web/src/lib/ssl-form.js web/tests/ssl web/tests/websites/WebsiteOperationPages.test.mjs
git commit -m "feat(websites): move deployment and ssl pages"
```

### Task 5: Move Cron and Queue Worker Pages

**Files:**
- Move: `web/src/routes/cron/+page.svelte` to `web/src/routes/websites/[id]/cron/+page.svelte`
- Move: `web/src/routes/queue-workers/+page.svelte` to `web/src/routes/websites/[id]/queue-workers/+page.svelte`
- Modify: `web/tests/websites/WebsiteOperationPages.test.mjs`

**Interfaces:**
- Consumes: route `page.params.id`, `WebsiteSectionNav`, and scoped Cron/Queue list/create APIs.
- Produces: nested Cron and Queue management screens without website selectors.

- [ ] **Step 1: Extend the nested-page test and verify RED**

Require both wished-for nested pages to compile and to use these literal scoped bases:

```text
/api/v1/websites/${websiteID}/cron-jobs
/api/v1/websites/${websiteID}/queue-workers
```

Run: `cd web && node --test tests/websites/WebsiteOperationPages.test.mjs`

Expected: FAIL because the nested pages are absent.

- [ ] **Step 2: Move and adapt Cron**

Load the route website first, list/create via `/api/v1/websites/{id}/cron-jobs`, remove global website list/selector/domain lookup, preserve edit/delete/enable/disable actions, and render shared navigation.

- [ ] **Step 3: Move and adapt Queue Workers**

Load the route website first, list/create via `/api/v1/websites/{id}/queue-workers`, remove global website list/selector/domain lookup, preserve start/stop/restart/delete actions, and render shared navigation.

- [ ] **Step 4: Verify Cron and Queue GREEN**

Run: `cd web && node --test tests/websites/WebsiteOperationPages.test.mjs`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add web/src/routes/cron web/src/routes/queue-workers web/src/routes/websites/'[id]'/cron web/src/routes/websites/'[id]'/queue-workers web/tests/websites/WebsiteOperationPages.test.mjs
git commit -m "feat(websites): move scheduled operations"
```

### Task 6: Redirect Legacy UI Routes and Verify

**Files:**
- Create: `web/src/routes/deployments/+page.ts`
- Create: `web/src/routes/ssl/+page.ts`
- Create: `web/src/routes/cron/+page.ts`
- Create: `web/src/routes/queue-workers/+page.ts`
- Modify: `web/tests/websites/WebsiteOperationPages.test.mjs`

**Interfaces:**
- Produces: four universal load functions that throw `redirect(307, '/websites')`.

- [ ] **Step 1: Add failing redirect behavior tests**

Dynamically import each `+page.ts`, call `load()`, and assert the thrown redirect has status `307` and location `/websites`. These assertions exercise the route load behavior rather than checking source text.

- [ ] **Step 2: Run focused test and verify RED**

Run: `cd web && node --test tests/websites/WebsiteOperationPages.test.mjs`

Expected: FAIL because the legacy load modules do not exist.

- [ ] **Step 3: Implement redirects**

Each file contains:

```ts
import { redirect } from '@sveltejs/kit';

export function load() {
  redirect(307, '/websites');
}
```

- [ ] **Step 4: Run full verification**

Run: `go test ./...`

Run: `cd web && npm test`

Run: `cd web && npm run check`

Run: `cd web && npm run build`

Run: `git diff --check`

Expected: every command exits zero; the known unrelated `uploadInput` Svelte warning may remain.

- [ ] **Step 5: Commit**

```bash
git add web/src/routes/deployments/+page.ts web/src/routes/ssl/+page.ts web/src/routes/cron/+page.ts web/src/routes/queue-workers/+page.ts web/tests/websites/WebsiteOperationPages.test.mjs
git commit -m "fix(routes): redirect global operations to websites"
```

- [ ] **Step 6: Review and integrate**

Request a code review against the pre-plan base, fix every Critical or Important finding, rerun the full verification commands, merge into `main`, and push `origin/main`.
