import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import vm from 'node:vm';
import ts from 'typescript';
import { nodeRuntimeState, summarizeNodeRuntimes } from '../../src/lib/node-runtimes.js';

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
		$state: (value) => value, $derived: (value) => value, onMount: () => {},
		summarizeNodeRuntimes, nodeRuntimeState, hasPermission: () => true, $permissions: []
	});
	vm.runInContext(javascript + `
	globalThis.handlers = {
		load, removeGlobal, createApp, appAction, updateRuntime, updateAll,
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

test('summarizeNodeRuntimes and nodeRuntimeState classify website runtimes', async () => {
	const { summarizeNodeRuntimes, nodeRuntimeState } = await import('../../src/lib/node-runtimes.js');
	const runtimes = [
		{ website_id: 'a', domain: 'a.test', selected_version: '22', installed: true, installed_version: 'v22.20.0', vulnerable: true },
		{ website_id: 'b', domain: 'b.test', selected_version: '24', installed: true, installed_version: 'v24.22.1' },
		{ website_id: 'c', domain: 'c.test', selected_version: '20', installed: true, installed_version: 'v20.19.0', end_of_life: true },
		{ website_id: 'd', domain: 'd.test', selected_version: '24', installed: false, installed_version: '' },
		{ website_id: 'e', domain: 'e.test', selected_version: '', installed: false, installed_version: '' }
	];
	const s = summarizeNodeRuntimes(runtimes);
	assert.deepEqual(s.selected.map((r) => r.website_id), ['a', 'b', 'c', 'd']);
	assert.deepEqual(s.vulnerable.map((r) => r.website_id), ['a']);
	assert.deepEqual(s.endOfLife.map((r) => r.website_id), ['c']);
	assert.deepEqual(s.notInstalled.map((r) => r.website_id), ['d']);
	assert.deepEqual(runtimes.map(nodeRuntimeState), ['vulnerable', 'ok', 'eol', 'missing', 'none']);
});

test('update buttons post to the runtime update endpoints', async () => {
	const {handlers, calls} = harness();
	await handlers.updateRuntime('site-1');
	assert.deepEqual(calls.at(-1), {method: 'POST', path: '/api/v1/nodejs/runtimes/site-1/update', payload: undefined});
	assert.equal(handlers.state().currentTaskId, 'task-1');
	handlers.lock(false);
	await handlers.updateAll();
	assert.deepEqual(calls.at(-1), {method: 'POST', path: '/api/v1/nodejs/runtimes/update', payload: undefined});
});
