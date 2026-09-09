# Website-Scoped Operations Design

**Date:** 2026-09-10

## Goal

Move Deployment, SSL, Cron Jobs, and Queue Workers out of the global navigation and into each website's detail area. Every list and creation flow in those screens must be scoped by the website ID in the URL so operators cannot accidentally view or create resources for another website.

## Scope

The website detail area will expose these routes:

- `/websites/{id}` — website overview, configuration, domains, logs, and the existing File Manager.
- `/websites/{id}/deployments` — deployment form, current progress, and history for the website.
- `/websites/{id}/ssl` — certificate installation and certificate management for domains belonging to the website.
- `/websites/{id}/cron` — cron job creation and management for the website.
- `/websites/{id}/queue-workers` — queue worker creation and lifecycle controls for the website.

All five screens will display the same website-section navigation. The base detail page continues to own File Manager; this change does not create a separate File Manager route.

The following global UI routes will no longer expose management screens and will redirect to `/websites`:

- `/deployments`
- `/ssl`
- `/cron`
- `/queue-workers`

Their sidebar entries will be removed. Existing RBAC permission names remain unchanged.

## User Experience

Each website operation page has a consistent header with a back link to Websites, the website domain, status, and tabs for Overview, Deployments, SSL, Cron Jobs, and Queue Workers. The active tab is visually identified and includes an accessible current-page state.

Forms do not contain a website selector. The website is determined exclusively by `{id}` in the route. Loading, empty, success, and failure states remain local to the active operation page.

If the website does not exist or cannot be loaded, the operation page shows the API error and does not call its resource list or mutation endpoints. Direct visits to the four former global routes redirect to the Websites list instead of showing an unscoped view.

## API Design

New scoped routes will be added under the existing `/api/v1/websites/{id}` resource:

| Method | Route | Purpose |
| --- | --- | --- |
| `GET` | `/websites/{id}/ssl` | List certificates owned by the website |
| `POST` | `/websites/{id}/ssl/issue` | Issue a certificate for one of the website's domains |
| `POST` | `/websites/{id}/ssl/custom` | Install custom certificate material for one of the website's domains |
| `GET` | `/websites/{id}/cron-jobs` | List cron jobs owned by the website |
| `POST` | `/websites/{id}/cron-jobs` | Create a cron job owned by the website |
| `GET` | `/websites/{id}/queue-workers` | List queue workers owned by the website |
| `POST` | `/websites/{id}/queue-workers` | Create a queue worker owned by the website |

Deployments already use scoped list and create routes and require no API shape change.

For scoped create operations, handlers take `website_id` from the URL and do not accept ownership from the request body. SSL create bodies contain the domain and certificate material when applicable. Cron and Queue Worker create bodies retain their operational fields but omit `website_id`.

Existing global APIs remain available for backward compatibility. Existing resource-ID actions—such as renew certificate, toggle cron state, or restart a worker—also remain unchanged. The panel UI will use only scoped list/create routes.

Scoped list methods query by `website_id` at the database layer rather than loading every server resource and filtering in the browser. A missing website returns `404`; malformed requests retain the existing validation response conventions.

## Domain and Ownership Rules

All four resource types already store `website_id`, so no database migration is required.

- SSL issuance and custom installation continue to validate that the requested domain belongs to the website.
- Cron jobs continue to execute as the website's system user and rebuild only that website user's crontab.
- Queue workers continue to execute using the website account and document root resolved by the backend.
- Deployments continue to use the existing website-scoped service contract.

The route ID is authoritative. A request body cannot override it. Existing backend path, domain, command, and permission validation stays in place.

## Frontend Structure

A small reusable website-section navigation component will own tab labels, URLs, and active-state rendering. It will not fetch data or perform mutations.

The four existing global pages will be adapted into nested website pages. Each page will:

1. Load the website identified by the route.
2. Load only that website's resources through its scoped endpoint.
3. Submit create operations to the same website-scoped endpoint.
4. Reuse existing action endpoints for individual resources.
5. Preserve the current polling behavior for pending SSL certificates and deployments.

The existing website detail page receives the shared navigation but keeps its current responsibilities. No unrelated visual redesign or state-management framework is introduced.

## Error Handling and Compatibility

- Website load failure stops dependent API calls and presents an actionable error.
- Scoped list failures leave the page usable for retry and never fall back to a global list.
- Create failures retain form input so the operator can correct and retry it.
- Polling is stopped when the page is destroyed or when no pending resource remains.
- Legacy UI routes redirect, while legacy APIs remain intact for external callers and staged upgrades.

## Testing

Backend tests will prove:

- Scoped SSL, Cron, and Queue lists return resources for the requested website only.
- Scoped creates use the URL website ID even when ownership is absent from the body.
- Requests for an unknown website return `404`.
- Existing global APIs and resource actions remain registered.

Frontend tests will prove:

- The sidebar no longer includes the four global menu entries.
- Website-section navigation generates the five expected routes and active state.
- Nested pages call scoped list/create endpoints and do not render a website selector.
- Legacy UI pages redirect to `/websites`.
- Deployment and SSL polling remain bounded to the active website.

Full Go tests, frontend tests, Svelte diagnostics, and production build must pass before merge.

## Non-Goals

- Changing database schemas or migrating existing records.
- Introducing per-user access control within a single website beyond existing RBAC.
- Redesigning Deployment, SSL, Cron, or Queue functionality unrelated to website scoping.
- Moving File Manager, domains, logs, or Nginx configuration to additional routes.
- Removing legacy APIs in this release.

## Acceptance Criteria

1. Deployment, SSL, Cron Jobs, and Queue Workers are absent from the global sidebar.
2. An operator reaches each feature from the selected website's detail navigation.
3. Lists and create operations are scoped by the route website ID at the backend.
4. No page requires selecting a website after entering a website detail area.
5. Data belonging to another website is not returned by scoped list endpoints.
6. The four former global UI routes redirect to `/websites`.
7. Existing resource actions and legacy API consumers remain compatible.
8. No schema migration is required and all verification gates pass.
