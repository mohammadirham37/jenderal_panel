import test from 'node:test';
import assert from 'node:assert/strict';
import { countByGroup, filterWebsites, formatBytes, statusGroup, usesPHP } from '../../src/lib/website-list.js';

const sites = [
	{ id: '1', domain: 'blog.example.com', app_type: 'wordpress', framework: 'wordpress', framework_version: '', php_version: '8.3', status: 'active', created_at: '2026-01-01T00:00:00Z', bandwidth_bytes: 500, owner_email: 'ana@example.com' },
	{ id: '2', domain: 'api.example.com', app_type: 'laravel', framework: 'laravel', framework_version: '12', php_version: '8.4', status: 'failed', created_at: '2026-02-01T00:00:00Z', bandwidth_bytes: 10 },
	{ id: '3', domain: 'shop.test', app_type: 'static', framework: 'none', framework_version: '', php_version: '8.3', status: 'suspended', created_at: '2026-03-01T00:00:00Z', bandwidth_bytes: 9000 },
	{ id: '4', domain: 'new.test', app_type: 'php', framework: 'none', framework_version: '', php_version: '8.2', status: 'installing', created_at: '2026-04-01T00:00:00Z', bandwidth_bytes: 0 },
	{ id: '5', domain: 'alpha.test', app_type: 'php', framework: 'none', framework_version: '', php_version: '8.2', status: 'disabled', created_at: '2026-05-01T00:00:00Z', bandwidth_bytes: 1 }
];

const ids = (list) => list.map((w) => w.id);

test('statusGroup folds raw statuses into filter groups', () => {
	assert.equal(statusGroup('validating'), 'pending');
	assert.equal(statusGroup('disabled'), 'suspended');
	assert.equal(statusGroup('weird'), 'other');
});

test('countByGroup counts every filter group', () => {
	assert.deepEqual(countByGroup(sites), { all: 5, active: 1, pending: 1, failed: 1, suspended: 2 });
});

test('filterWebsites keeps failed and in-progress sites on top under every sort', () => {
	assert.deepEqual(ids(filterWebsites(sites, { sort: 'newest' })), ['4', '2', '5', '3', '1']);
	assert.deepEqual(ids(filterWebsites(sites, { sort: 'name' })), ['2', '4', '5', '1', '3']);
	assert.deepEqual(ids(filterWebsites(sites, { sort: 'bandwidth' })), ['2', '4', '3', '1', '5']);
});

test('filterWebsites searches domain, stack, owner and PHP version with all terms', () => {
	assert.deepEqual(ids(filterWebsites(sites, { query: 'laravel' })), ['2']);
	assert.deepEqual(ids(filterWebsites(sites, { query: 'ANA@' })), ['1']);
	assert.deepEqual(ids(filterWebsites(sites, { query: 'php 8.2' })), ['4', '5']);
	assert.deepEqual(ids(filterWebsites(sites, { query: 'example wordpress' })), ['1']);
	// Static sites carry a stale php_version that must not match.
	assert.deepEqual(ids(filterWebsites(sites, { query: 'shop php' })), []);
});

test('filterWebsites applies the status group filter', () => {
	assert.deepEqual(ids(filterWebsites(sites, { status: 'suspended', sort: 'name' })), ['5', '3']);
});

test('usesPHP and formatBytes', () => {
	assert.equal(usesPHP(sites[2]), false);
	assert.equal(usesPHP(sites[0]), true);
	assert.equal(formatBytes(0), '0 B');
	assert.equal(formatBytes(1536), '1.5 KB');
});

test('usesPHP ignores php_version stored on non-PHP app types', () => {
	for (const app_type of ['go', 'python', 'deno', 'bun', 'node']) {
		assert.equal(usesPHP({ app_type, php_version: '8.2' }), false, app_type);
	}
	assert.equal(usesPHP({ app_type: 'laravel', php_version: '8.3' }), true);
});

test('appTypeUsesPHP drives the PHP UI on the website detail page', async () => {
	const { appTypeUsesPHP } = await import('../../src/lib/website-list.js');
	for (const t of ['go', 'python', 'deno', 'bun', 'node', 'static', 'reverse-proxy', undefined]) assert.equal(appTypeUsesPHP(t), false, String(t));
	for (const t of ['php', 'wordpress', 'laravel', 'codeigniter']) assert.equal(appTypeUsesPHP(t), true, t);

	const { readFileSync } = await import('node:fs');
	const page = readFileSync(new URL('../../src/routes/websites/[id]/+page.svelte', import.meta.url), 'utf8');
	assert.match(page, /\(t !== 'PHP Settings' \|\| appTypeUsesPHP\(website\?\.app_type\)\)/);
	assert.match(page, /\{#if appTypeUsesPHP\(website\.app_type\)\}\s*<span>\{translate\(\$language, 'wd\.php_version'\)\}/);
});

test('Go sites get the Build & Run card on the Deployment tab', async () => {
	const { readFileSync } = await import('node:fs');
	const page = readFileSync(new URL('../../src/routes/websites/[id]/+page.svelte', import.meta.url), 'utf8');
	assert.match(page, /\{#if website\.app_type === 'go'\}\s*<WebsiteGoBuildSection /);
	const card = readFileSync(new URL('../../src/lib/components/WebsiteGoBuildSection.svelte', import.meta.url), 'utf8');
	assert.match(card, /appAPI\('auto-build'\)/);
	assert.match(card, /appAPI\('build'\)/);
});
