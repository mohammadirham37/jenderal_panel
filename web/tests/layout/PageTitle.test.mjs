import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { readdirSync, statSync } from 'node:fs';
import { join } from 'node:path';
import { formatPageTitle, pageTitleKey } from '../../src/lib/page-title.js';

const routes = [
	{ href: '/websites', labelKey: 'nav.websites' },
	{ href: '/databases', labelKey: 'nav.databases' },
	{ href: '/db-config', labelKey: 'nav.db_config' }
];

test('pageTitleKey matches exact routes and nested detail pages', () => {
	assert.equal(pageTitleKey('/websites', routes), 'nav.websites');
	assert.equal(pageTitleKey('/websites/01HXYZ', routes), 'nav.websites');
	assert.equal(pageTitleKey('/databases/manage/7', routes), 'nav.databases');
	assert.equal(pageTitleKey('/db-config', routes), 'nav.db_config');
	// A shared prefix without a slash is a different route.
	assert.equal(pageTitleKey('/websites-old', routes), '');
	assert.equal(pageTitleKey('/login', routes), '');
});

test('formatPageTitle falls back to the host alone', () => {
	assert.equal(formatPageTitle('Websites', 'srv1'), 'Websites · srv1');
	assert.equal(formatPageTitle('', 'srv1'), 'srv1');
});

test('only the root layout sets the document title', () => {
	const root = fileURLToPath(new URL('../../src/routes', import.meta.url));
	const offenders = [];
	const walk = (dir) => {
		for (const name of readdirSync(dir)) {
			const path = join(dir, name);
			if (statSync(path).isDirectory()) walk(path);
			else if (name.endsWith('.svelte') && path !== join(root, '+layout.svelte') && /<title>/.test(readFileSync(path, 'utf8'))) offenders.push(path);
		}
	};
	walk(root);
	assert.deepEqual(offenders, []);
});
