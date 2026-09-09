import { readFileSync } from 'node:fs';
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { compile } from 'svelte/compiler';
import { render } from 'svelte/server';
import { websiteSectionLinks } from '../../src/lib/website-sections.js';

const componentUrl = new URL('../../src/lib/components/WebsiteSectionNav.svelte', import.meta.url);

async function loadComponent() {
	const source = readFileSync(componentUrl, 'utf8');
	const { js } = compile(source, { filename: 'WebsiteSectionNav.svelte', generate: 'server' });
	const serverRuntime = import.meta.resolve('svelte/internal/server');
	const helperUrl = new URL('../../src/lib/website-sections.js', import.meta.url).href;
	const code = js.code
		.replace("'svelte/internal/server'", JSON.stringify(serverRuntime))
		.replace("'$lib/website-sections.js'", JSON.stringify(helperUrl));
	const url = `data:text/javascript;base64,${Buffer.from(code).toString('base64')}`;
	return (await import(url)).default;
}

test('builds encoded links for every website section', () => {
	assert.deepEqual(
		websiteSectionLinks('site/a').map((link) => link.href),
		[
			'/websites/site%2Fa',
			'/websites/site%2Fa/deployments',
			'/websites/site%2Fa/ssl',
			'/websites/site%2Fa/cron',
			'/websites/site%2Fa/queue-workers'
		]
	);
});

test('compiles the website section nav and derives links from its helper', () => {
	const source = readFileSync(componentUrl, 'utf8');
	assert.doesNotThrow(() => compile(source, { filename: 'WebsiteSectionNav.svelte', generate: 'server' }));
	assert.match(source, /websiteSectionLinks\(websiteId\)/);
});

test('marks only the exact current section as the current page', async () => {
	const WebsiteSectionNav = await loadComponent();
	const { body } = render(WebsiteSectionNav, {
		props: { websiteId: 'site/a', currentPath: '/websites/site%2Fa/ssl' }
	});

	assert.match(body, /href="\/websites\/site%2Fa\/ssl"[^>]*aria-current="page"/);
	assert.equal((body.match(/aria-current="page"/g) || []).length, 1);
});
