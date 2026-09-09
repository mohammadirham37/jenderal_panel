import { existsSync, readFileSync } from 'node:fs';
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { compile } from 'svelte/compiler';

let websiteOperationAPI;
try {
	({ websiteOperationAPI } = await import('../../src/lib/website-operations.js'));
} catch {}

const operationPages = [
	{
		name: 'Deployment',
		url: new URL('../../src/routes/websites/[id]/deployments/+page.svelte', import.meta.url),
		selectorID: 'deploy-website'
	},
	{
		name: 'SSL',
		url: new URL('../../src/routes/websites/[id]/ssl/+page.svelte', import.meta.url),
		selectorID: 'ssl-website'
	}
];

function collectAttributeValues(node, name, values = []) {
	if (!node || typeof node !== 'object') return values;
	if (node.type === 'Attribute' && node.name === name) {
		for (const value of node.value || []) {
			if (value.type === 'Text') values.push(value.data);
		}
	}
	for (const value of Object.values(node)) {
		if (Array.isArray(value)) {
			for (const item of value) collectAttributeValues(item, name, values);
		} else if (value && typeof value === 'object') {
			collectAttributeValues(value, name, values);
		}
	}
	return values;
}

function collectRouteReloadEffects(node, effects = []) {
	if (!node || typeof node !== 'object') return effects;
	if (
		node.type === 'CallExpression' &&
		node.callee?.type === 'Identifier' &&
		node.callee.name === '$effect'
	) {
		effects.push(node);
	}
	for (const value of Object.values(node)) {
		if (Array.isArray(value)) {
			for (const item of value) collectRouteReloadEffects(item, effects);
		} else if (value && typeof value === 'object') {
			collectRouteReloadEffects(value, effects);
		}
	}
	return effects;
}

test('builds encoded scoped endpoints for every website operation module', () => {
	assert.equal(typeof websiteOperationAPI, 'function');
	assert.deepEqual(websiteOperationAPI('site/1'), {
		website: '/api/v1/websites/site%2F1',
		deploy: '/api/v1/websites/site%2F1/deploy',
		deployments: '/api/v1/websites/site%2F1/deployments',
		ssl: '/api/v1/websites/site%2F1/ssl',
		sslIssue: '/api/v1/websites/site%2F1/ssl/issue',
		sslCustom: '/api/v1/websites/site%2F1/ssl/custom',
		cronJobs: '/api/v1/websites/site%2F1/cron-jobs',
		queueWorkers: '/api/v1/websites/site%2F1/queue-workers'
	});
});

for (const operationPage of operationPages) {
	test(`${operationPage.name} nested page compiles without a website selector`, () => {
		assert.equal(existsSync(operationPage.url), true, `${operationPage.name} page must exist`);
		const source = readFileSync(operationPage.url, 'utf8');
		const result = compile(source, { filename: operationPage.url.pathname, generate: 'server' });
		const ids = collectAttributeValues(result.ast, 'id');
		assert.equal(ids.includes(operationPage.selectorID), false);
	});

	test(`${operationPage.name} reloads after a same-route website ID change`, () => {
		const source = readFileSync(operationPage.url, 'utf8');
		const result = compile(source, { filename: operationPage.url.pathname, generate: 'server' });
		const effects = collectRouteReloadEffects(result.ast);
		const reloadEffect = effects.find((effect) => {
			const effectSource = source.slice(effect.start, effect.end);
			return effectSource.includes('websiteID') && effectSource.includes('loadWebsite(websiteID)');
		});

		assert.ok(reloadEffect, 'route changes must reload through a websiteID-tracked $effect');
		assert.doesNotMatch(source, /\bonMount\s*\(/);
	});
}
