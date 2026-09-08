import { existsSync, readFileSync } from 'node:fs';
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { compile } from 'svelte/compiler';
import { render } from 'svelte/server';

// Kept as .mjs so Svelte's browser-focused typecheck does not include this Node test.

async function loadComponent() {
	const componentUrl = new URL('../../src/lib/components/LogoMark.svelte', import.meta.url);
	assert.equal(existsSync(componentUrl), true, 'LogoMark component should exist');
	const source = readFileSync(componentUrl, 'utf8');
	const { js } = compile(source, { filename: 'LogoMark.svelte', generate: 'server' });
	const serverRuntime = import.meta.resolve('svelte/internal/server');
	const code = js.code.replace("'svelte/internal/server'", JSON.stringify(serverRuntime));
	const url = `data:text/javascript;base64,${Buffer.from(code).toString('base64')}`;
	return (await import(url)).default;
}

test('renders the Jenderal Panel logo with accessible text', async () => {
	const LogoMark = await loadComponent();
	const { body } = render(LogoMark);

	assert.match(body, /src="\/jenderal-panel-logo\.png"/);
	assert.match(body, /alt="Jenderal Panel"/);
});

test('supports the larger login presentation', async () => {
	const LogoMark = await loadComponent();
	const { body } = render(LogoMark, { props: { size: 'lg' } });

	assert.match(body, /h-20 w-20/);
});
