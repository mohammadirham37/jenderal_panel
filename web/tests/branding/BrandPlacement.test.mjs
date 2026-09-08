import { readFileSync } from 'node:fs';
import { test } from 'node:test';
import assert from 'node:assert/strict';

// Kept as .mjs so Svelte's browser-focused typecheck does not include this Node test.

const login = readFileSync(new URL('../../src/routes/login/+page.svelte', import.meta.url), 'utf8');
const layout = readFileSync(new URL('../../src/routes/+layout.svelte', import.meta.url), 'utf8');

test('places the logo on the login page', () => {
	assert.match(login, /import LogoMark from '\$lib\/components\/LogoMark\.svelte'/);
	assert.match(login, /<LogoMark size="lg"/);
});

test('keeps the logo visible in both sidebar states', () => {
	assert.match(layout, /import LogoMark from '\$lib\/components\/LogoMark\.svelte'/);
	assert.match(layout, /<LogoMark size=\{sidebarOpen \? 'md' : 'sm'\}/);
});
