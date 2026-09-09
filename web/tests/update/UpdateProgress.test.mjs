import { readFileSync } from 'node:fs';
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { parse } from 'svelte/compiler';

function findTaskProgress(node, insideConditional = false) {
	if (!node || typeof node !== 'object') return null;

	const isConditional = insideConditional || node.type === 'IfBlock';
	if (node.type === 'Component' && node.name === 'TaskProgress') {
		return { node, insideConditional: isConditional };
	}

	for (const [key, value] of Object.entries(node)) {
		if (key === 'metadata' || key === 'parent') continue;
		const children = Array.isArray(value) ? value : [value];
		for (const child of children) {
			const match = findTaskProgress(child, isConditional);
			if (match) return match;
		}
	}

	return null;
}

const pages = [
	['update', 'jenderal_update_task'],
	['php', 'jenderal_php_task'],
	['nodejs', 'jenderal_nodejs_task'],
	['databases', 'jenderal_db_task'],
	['docker', 'jenderal_docker_task'],
	['services', 'jenderal_composer_task']
];

for (const [page, storageKey] of pages) {
	test(`${page} mounts progress before the in-memory task id is restored`, () => {
		const source = readFileSync(new URL(`../../src/routes/${page}/+page.svelte`, import.meta.url), 'utf8');
		const ast = parse(source, { modern: true });
		const progress = findTaskProgress(ast.fragment);

		assert.ok(progress, `expected the ${page} page to render TaskProgress`);
		assert.equal(
			progress.insideConditional,
			false,
			'TaskProgress cannot restore a saved task id when its mount is conditional on that same id'
		);
		assert.ok(
			progress.node.attributes.some(
				(attribute) => attribute.type === 'BindDirective' && attribute.name === 'taskId'
			),
			'The restored task id must be bound back to the page so install controls remain locked'
		);
		assert.ok(
			progress.node.attributes.some(
				(attribute) =>
					attribute.type === 'Attribute' &&
					attribute.name === 'storageKey' &&
					attribute.value?.[0]?.data === storageKey
			),
			`expected ${page} to use storage key ${storageKey}`
		);
	});
}
