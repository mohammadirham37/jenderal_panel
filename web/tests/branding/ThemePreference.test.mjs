import { readFileSync } from 'node:fs';
import { test } from 'node:test';
import assert from 'node:assert/strict';

const themeModuleUrl = new URL('../../src/lib/theme.js', import.meta.url);

async function loadThemeModule() {
	try {
		return await import(themeModuleUrl);
	} catch {
		return {};
	}
}

test('uses the device color scheme until the user chooses a theme', async () => {
	const { resolveTheme } = await loadThemeModule();
	assert.equal(typeof resolveTheme, 'function');
	assert.equal(resolveTheme(null, true), 'dark');
	assert.equal(resolveTheme(null, false), 'light');
});

test('a saved manual theme overrides the device color scheme', async () => {
	const { resolveTheme } = await loadThemeModule();
	assert.equal(typeof resolveTheme, 'function');
	assert.equal(resolveTheme('light', true), 'light');
	assert.equal(resolveTheme('dark', false), 'dark');
	assert.equal(resolveTheme('invalid', true), 'dark');
});

test('theme toggle is available before and after login', () => {
	const login = readFileSync(new URL('../../src/routes/login/+page.svelte', import.meta.url), 'utf8');
	const layout = readFileSync(new URL('../../src/routes/+layout.svelte', import.meta.url), 'utf8');

	assert.match(login, /import ThemeToggle from '\$lib\/components\/ThemeToggle\.svelte'/);
	assert.match(login, /<ThemeToggle\s*\/>/);
	assert.match(layout, /import ThemeToggle from '\$lib\/components\/ThemeToggle\.svelte'/);
	assert.match(layout, /<ThemeToggle\s*\/>/);
});

test('applies a saved or system theme before the application renders', () => {
	const appHtml = readFileSync(new URL('../../src/app.html', import.meta.url), 'utf8');
	const themeScript = appHtml.indexOf('jenderal-theme');
	const renderedApp = appHtml.indexOf('%sveltekit.body%');

	assert.ok(themeScript >= 0, 'app shell should read the saved theme');
	assert.ok(themeScript < renderedApp, 'theme should be applied before the Svelte body renders');
	assert.match(appHtml, /matchMedia\('\(prefers-color-scheme: dark\)'\)/);
});
