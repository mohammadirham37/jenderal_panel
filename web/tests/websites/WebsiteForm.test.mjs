import test from 'node:test';
import assert from 'node:assert/strict';
import {
	availablePHPVersions,
	normalizeWebsiteSelection,
	selectedCombination
} from '../../src/lib/website-form.js';

const options = {
	php_versions: [
		{ version: '8.1', installed: false },
		{ version: '8.2', installed: true },
		{ version: '8.3', installed: true }
	],
	dependencies: [
		{ name: 'composer', installed: false, version: '', manage_url: '/services' },
		{ name: 'node', installed: false, version: '', manage_url: '/nodejs' }
	],
	defaults: { template: 'php', php_version: '8.2', framework_version: '12', frontend_stack: 'blade', project_variant: 'empty', setup_mode: 'config-only' },
	inertia_adapters: ['react', 'vue', 'svelte'],
	profiles: [
		{ template: 'php', framework_version: '', frontend_stack: '', inertia_adapter: '', project_variant: 'empty', setup_mode: 'config-only', enabled: true, document_root: '/home/<user>/public', prerequisites: [], php_compatibility: [{ version: '8.2', enabled: true }] },
		{ template: 'laravel', framework_version: '13', frontend_stack: 'inertia', inertia_adapter: 'svelte', project_variant: 'starter-kit', setup_mode: 'automatic', enabled: true, document_root: '/home/<user>/app/public', prerequisites: ['composer', 'node'], php_compatibility: [{ version: '8.2', enabled: false, reason: 'Laravel 13 requires PHP 8.3 or newer' }, { version: '8.3', enabled: true, reason: '' }] }
	]
};

test('only installed PHP versions are available', () => {
	assert.deepEqual(availablePHPVersions(options).map((item) => item.version), ['8.2', '8.3']);
});

test('non-Laravel templates clear Laravel-only selections', () => {
	const normalized = normalizeWebsiteSelection({ template: 'php', php_version: '8.2', framework_version: '13', frontend_stack: 'inertia', inertia_adapter: 'svelte', project_variant: 'starter-kit', setup_mode: 'automatic' }, options);
	assert.equal(normalized.framework_version, '');
	assert.equal(normalized.frontend_stack, '');
	assert.equal(normalized.inertia_adapter, '');
	assert.equal(normalized.project_variant, 'empty');
});

test('Inertia adapters come from backend options', () => {
	assert.deepEqual(options.inertia_adapters, ['react', 'vue', 'svelte']);
});

test('backend PHP incompatibility reason wins', () => {
	const result = selectedCombination(options, { template: 'laravel', php_version: '8.2', framework_version: '13', frontend_stack: 'inertia', inertia_adapter: 'svelte', project_variant: 'starter-kit', setup_mode: 'automatic' });
	assert.equal(result.enabled, false);
	assert.equal(result.reason, 'Laravel 13 requires PHP 8.3 or newer');
});

test('missing dependencies retain their management links', () => {
	const result = selectedCombination(options, { template: 'laravel', php_version: '8.3', framework_version: '13', frontend_stack: 'inertia', inertia_adapter: 'svelte', project_variant: 'starter-kit', setup_mode: 'automatic' });
	assert.equal(result.enabled, false);
	assert.deepEqual(result.missing_dependencies.map((item) => item.manage_url), ['/services', '/nodejs']);
});

test('Laravel Inertia Svelte normalization preserves six profile fields', () => {
	const result = normalizeWebsiteSelection({ template: 'laravel', php_version: '8.3', framework_version: '13', frontend_stack: 'inertia', inertia_adapter: 'svelte', project_variant: 'starter-kit', setup_mode: 'automatic' }, options);
	assert.deepEqual(Object.fromEntries(['template', 'framework_version', 'frontend_stack', 'inertia_adapter', 'project_variant', 'setup_mode'].map((key) => [key, result[key]])), {
		template: 'laravel', framework_version: '13', frontend_stack: 'inertia', inertia_adapter: 'svelte', project_variant: 'starter-kit', setup_mode: 'automatic'
	});
});
