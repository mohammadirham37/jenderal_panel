# Nginx Simple Configuration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a safe Simple/Manual Nginx configuration editor while preserving the existing validated save and rollback path.

**Architecture:** A focused browser-side parser maps six managed directives to their Nginx contexts and performs surgical text replacements or insertions. The Svelte page owns mode and form state, while the existing backend PUT endpoint remains the only persistence path.

**Tech Stack:** Svelte 5, TypeScript, JavaScript ES modules, Node test runner, Tailwind CSS.

**Spec:** `docs/superpowers/specs/2026-09-09-nginx-firewall-alert-notification-design.md`

## Global Constraints

- The raw Nginx text is the source of truth.
- Preserve comments and all unmanaged directives.
- Do not synthesize missing `events` or `http` blocks.
- Save through the existing endpoint so `nginx -t`, backup, rollback, and reload remain authoritative.
- Target runtime is Ubuntu 24.04.

---

### Task 1: Context-aware Nginx parser and updater

**Files:**
- Create: `web/src/lib/nginx-config.js`
- Create: `web/tests/nginx/NginxConfig.test.mjs`
- Modify: `web/package.json`

**Interfaces:**
- Produces: `parseSimpleNginxConfig(content: string): { values: Record<string,string>, errors: string[] }`
- Produces: `updateSimpleNginxConfig(content: string, key: string, value: string): string`
- Produces: `switchNginxConfigMode(state, nextMode): state`

- [ ] **Step 1: Write failing parser tests**

```js
import { parseSimpleNginxConfig, updateSimpleNginxConfig, switchNginxConfigMode } from '../../src/lib/nginx-config.js';

const config = `worker_processes auto;
events { worker_connections 768; }
http {
    client_max_body_size 64M;
    keepalive_timeout 65;
    server_tokens off;
    gzip on;
}`;

assert.equal(parseSimpleNginxConfig(config).values.worker_connections, '768');
assert.match(updateSimpleNginxConfig(config, 'gzip', 'off'), /gzip off;/);
assert.match(updateSimpleNginxConfig('events {}\nhttp {}', 'client_max_body_size', '32M'), /http \{\n\s+client_max_body_size 32M;/);
assert.throws(() => updateSimpleNginxConfig('events {}', 'gzip', 'on'), /http block/i);
assert.equal(switchNginxConfigMode({ mode: 'manual', draft: config }, 'simple').draft, config);
```

- [ ] **Step 2: Run the focused test and confirm failure**

Run: `cd web && node --test tests/nginx/NginxConfig.test.mjs`

Expected: FAIL because `src/lib/nginx-config.js` does not exist.

- [ ] **Step 3: Implement the parser and updater**

Implement a brace-aware scanner that ignores `#` comments, records main/events/http ranges, and locates only active directives in the required context. Validate values with these exact rules:

```js
const fields = {
  worker_processes: { context: 'main', pattern: /^(auto|[1-9]\d*)$/ },
  worker_connections: { context: 'events', pattern: /^[1-9]\d*$/ },
  client_max_body_size: { context: 'http', pattern: /^\d+[kKmMgG]?$/ },
  keepalive_timeout: { context: 'http', pattern: /^\d+$/ },
  server_tokens: { context: 'http', pattern: /^(on|off)$/ },
  gzip: { context: 'http', pattern: /^(on|off)$/ }
};
```

Replace a matched directive in place. Insert a missing directive immediately after the opening brace of its required block using the block's indentation. Throw a descriptive error for unsupported keys, invalid values, or missing contexts.

- [ ] **Step 4: Register and run tests**

Add `tests/nginx/*.test.mjs` to the `test` script in `web/package.json`.

Run: `cd web && npm test`

Expected: PASS, including replacement, insertion, comments, invalid values, missing blocks, and draft preservation.

- [ ] **Step 5: Commit the parser**

```bash
git add web/src/lib/nginx-config.js web/tests/nginx/NginxConfig.test.mjs web/package.json
git commit -m "feat: add simple nginx config parser"
```

### Task 2: Simple/Manual Nginx page

**Files:**
- Modify: `web/src/routes/nginx/+page.svelte`

**Interfaces:**
- Consumes: `parseSimpleNginxConfig`, `updateSimpleNginxConfig`, and `switchNginxConfigMode` from Task 1.
- Produces: Simple form state whose save payload remains `{ content: string }` for the existing Nginx API.

- [ ] **Step 1: Extend the source-level UI test**

Add assertions to `web/tests/nginx/NginxConfig.test.mjs` that load the Svelte source and verify it imports the helper, renders `Simple` and `Manual` controls, exposes all six field names, and calls the existing save function.

- [ ] **Step 2: Run the source test and confirm failure**

Run: `cd web && node --test tests/nginx/NginxConfig.test.mjs`

Expected: FAIL because the page has no mode toggle or Simple form.

- [ ] **Step 3: Implement page state and form**

Add `mode: 'simple' | 'manual'`, parsed values, parser errors, and a `setSimpleField(key, value)` handler that updates the raw draft through `updateSimpleNginxConfig`. Render accessible toggle buttons and inputs for the six fields. Keep the Manual textarea bound to the same raw draft.

When switching into Simple mode, parse the current draft and show parser errors above the form. Disable Save when parsing or validation fails. Do not call a new backend endpoint.

- [ ] **Step 4: Verify the frontend**

Run: `cd web && npm test && npm run check && npm run build`

Expected: all commands exit 0.

- [ ] **Step 5: Commit the UI**

```bash
git add web/src/routes/nginx/+page.svelte web/tests/nginx/NginxConfig.test.mjs
git commit -m "feat: add simple nginx configuration mode"
```
