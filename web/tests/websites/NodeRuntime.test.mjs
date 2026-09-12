import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import vm from 'node:vm';
import ts from 'typescript';

// Execute the page's actual handlers, replacing only Svelte lifecycle/reactivity
// and network boundaries. No DOM or real VPS is required for payload/state tests.
const page = readFileSync(new URL('../../src/routes/nodejs/+page.svelte', import.meta.url), 'utf8');
const script = page.match(/<script lang="ts">([\s\S]*?)<\/script>/)[1].replace(/^\s*import .*;$/gm, '');
const javascript = ts.transpileModule(script, {compilerOptions: {target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.None}}).outputText;

function harness(overrides = {}) {
	const calls = [];
	const runtime = {website_id: 'site-1', domain: 'example.test', web_user: 'web_example_test', selected_version: '22', installed: true, installed_version: 'v22.1.0', npm_version: '10.0.0', nvm_version: '0.40.7', nvm_state: 'ready'};
	const api = {
		get: async (path) => path.endsWith('/runtimes') ? [runtime] : path.endsWith('/apps') ? [] : {installed: true, version: 'v20.0.0', package: 'nodejs'},
		post: async (path, payload) => { calls.push({method: 'POST', path, payload}); return {task_id: 'task-1'}; },
		del: async (path) => { calls.push({method: 'DELETE', path}); },
		...overrides
	};
	const context = vm.createContext({
		Error,
		api, apiRaw: async (method, path, payload) => { calls.push({method, path, payload}); return {data: {task_id: 'remove-1'}}; },
		toast: {success: () => {}, error: () => {}, info: () => {}},
		translate: (lang, key) => key, $language: 'en',
		$state: (value) => value, $derived: (value) => value, onMount: () => {}
	});
	vm.runInContext(javascript + `
	globalThis.handlers = {
		load, removeGlobal, createApp, appAction,
		select: (runtime, version) => { selectedRuntime = runtime; createWebsiteId = runtime.website_id; },
		confirm: (value) => { confirmGlobal = value; },
		lock: (value) => { busy = value; },
		state: () => ({currentTaskId, loading, error, acting})
	};
	`, context);
	return {handlers: context.handlers, calls, runtime};
}

test('global removal requires explicit confirmation and does not run during another task', async () => {
	const {handlers, calls} = harness();
	await handlers.removeGlobal();
	assert.equal(calls.length, 0);
	handlers.confirm(true); handlers.lock(true);
	await handlers.removeGlobal();
	assert.equal(calls.length, 0);
	handlers.lock(false);
	await handlers.removeGlobal();
	assert.deepEqual(JSON.parse(JSON.stringify(calls)), [{method: 'DELETE', path: '/api/v1/nodejs/global', payload: {confirm: true}}]);
	assert.equal(handlers.state().currentTaskId, 'remove-1');
});

test('app creation inherits runtime and sends canonical backend fields', async () => {
	const {handlers, calls, runtime} = harness();
	handlers.select(runtime, '22');
	await handlers.createApp();
	assert.deepEqual(JSON.parse(JSON.stringify(calls)), [{method: 'POST', path: '/api/v1/nodejs/apps', payload: {website_id: 'site-1', package_mgr: 'npm', build_cmd: '', start_cmd: 'server.js', port: 3000}}]);
});

test('missing runtime and busy state block application creation', async () => {
	const {handlers, calls, runtime} = harness();
	handlers.select({...runtime, installed: false}, '22');
	await handlers.createApp();
	handlers.select(runtime, '22'); handlers.lock(true);
	await handlers.createApp();
	assert.equal(calls.length, 0);
});

test('load failure exits loading state and reports actionable output', async () => {
	const {handlers, runtime} = harness({get: async () => {throw new Error('Runtime unavailable');}});
	await handlers.load();
	assert.equal(handlers.state().loading, false);
	assert.equal(handlers.state().error, 'Runtime unavailable');
});
