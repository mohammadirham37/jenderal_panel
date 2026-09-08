import { readFileSync } from 'node:fs';
import { test } from 'node:test';
import assert from 'node:assert/strict';

// Kept as .mjs so Svelte's browser-focused typecheck does not include this Node test.

const login = readFileSync(new URL('../../src/routes/login/+page.svelte', import.meta.url), 'utf8');
const layout = readFileSync(new URL('../../src/routes/+layout.svelte', import.meta.url), 'utf8');

test('places the logo on the login page', () => {
	assert.match(login, /import LogoMark from '\$lib\/components\/LogoMark\.svelte'/);
	assert.match(login, /<LogoMark size="lg"/);
	assert.match(login, /class="login-shell/);
	assert.match(login, /<h1[^>]*>Welcome back<\/h1>/);
	assert.match(login, /role="alert"/);
});

test('keeps the logo visible in both sidebar states', () => {
	assert.match(layout, /import LogoMark from '\$lib\/components\/LogoMark\.svelte'/);
	assert.match(layout, /<LogoMark size=\{sidebarExpanded \? 'md' : 'sm'\}/);
	assert.match(layout, /class="app-shell/);
	assert.match(layout, /class="sidebar-surface/);
	assert.match(layout, /class=\{sidebarExpanded \? '' : 'sr-only'\}/);
});

test('uses off-canvas navigation on mobile', () => {
	assert.match(layout, /let mobileSidebarOpen = \$state\(false\)/);
	assert.match(layout, /mobileSidebarOpen \? 'translate-x-0' : '-translate-x-full'/);
	assert.match(layout, /aria-label="Open navigation"/);
	assert.match(layout, /aria-label="Close navigation"/);
});

test('manages mobile drawer focus and background interaction', () => {
	assert.match(layout, /aria-expanded=\{mobileSidebarOpen\}/);
	assert.match(layout, /aria-controls="primary-sidebar"/);
	assert.match(layout, /inert=\{isMobile && mobileSidebarOpen \? true : undefined\}/);
	assert.match(layout, /event\.key === 'Escape'/);
	assert.match(layout, /mobileMenuButton\?\.focus\(\)/);
});
