import { test } from 'node:test';
import assert from 'node:assert/strict';
import { page, mount, unmount, flushSync, settle } from '../helpers/svelte-browser.mjs';

const operations = [
	{ route: 'deployments', endpoint: 'deployments', title: 'Deployments', pending: {
		id: 'deploy-a', website_id: 'site-a', commit_hash: '', branch: 'main', status: 'pending',
		duration: 0, log: '', created_at: '2026-09-10T00:00:00Z'
	} },
	{ route: 'ssl', endpoint: 'ssl', title: 'SSL Certificates', pending: {
		id: 'cert-a', website_id: 'site-a', domain: 'a.example.com', issuer: 'letsencrypt',
		status: 'pending', expires_at: '', auto_renew: true, created_at: '2026-09-10T00:00:00Z'
	} },
	{ route: 'cron', endpoint: 'cron-jobs', title: 'Cron Jobs', stale: {
		id: 'cron-a', website_id: 'site-a', command: 'site-a-only-command', schedule: '* * * * *',
		enabled: true, last_run: '', last_status: '', created_at: '2026-09-10T00:00:00Z'
	} },
	{ route: 'queue-workers', endpoint: 'queue-workers', title: 'Queue Workers', stale: {
		id: 'worker-a', website_id: 'site-a', command: 'site-a-only-command', num_workers: 1,
		status: 'running', created_at: '2026-09-10T00:00:00Z'
	} }
];

function website(id, status = 'active') {
	return { id, domain: `${id === 'site-a' ? 'a' : 'b'}.example.com`, app_type: 'php', status, domains: [] };
}

function response(data) {
	return Response.json({ data });
}

function deferred() {
	let resolve;
	const promise = new Promise((done) => { resolve = done; });
	return { promise, resolve };
}

async function openPage(t, operation, fetchResponse) {
	const requests = [];
	const intervals = new Map();
	let nextInterval = 0;
	t.mock.method(globalThis, 'fetch', (path, options) => {
		requests.push({ path, method: options.method });
		return fetchResponse(path, options);
	});
	t.mock.method(globalThis, 'setInterval', (callback, delay) => {
		const id = ++nextInterval;
		intervals.set(id, { callback, delay });
		return id;
	});
	t.mock.method(globalThis, 'clearInterval', (id) => intervals.delete(id));
	page.params.id = 'site-a';
	page.url.pathname = `/websites/site-a/${operation.route}`;
	const { default: Component } = await import(`../../src/routes/websites/[id]/${operation.route}/+page.svelte`);
	const target = document.createElement('div');
	document.body.append(target);
	const component = mount(Component, { target });
	let destroyed = false;
	async function destroy() {
		if (destroyed) return;
		destroyed = true;
		await unmount(component);
		target.remove();
	}
	t.after(destroy);
	flushSync();
	await settle();
	return { target, requests, intervals, destroy };
}

for (const operation of operations) {
	test(`${operation.title} header shows the loaded website status`, async (t) => {
		const { target } = await openPage(t, operation, async (path) => {
			if (path === '/api/v1/websites/site-a') return response(website('site-a', 'suspended'));
			assert.equal(path, `/api/v1/websites/site-a/${operation.endpoint}`);
			return response([]);
		});
		const heading = target.querySelector('h2');
		assert.equal(heading?.textContent, `${operation.title} · a.example.com`);
		assert.match(heading.parentElement.textContent, /\bsuspended\b/);
	});

	test(`${operation.title} reloads a new route website and ignores the previous list response`, async (t) => {
		const oldList = deferred();
		const { target, intervals, requests } = await openPage(t, operation, async (path) => {
			if (path === '/api/v1/websites/site-a') return response(website('site-a'));
			if (path === `/api/v1/websites/site-a/${operation.endpoint}`) return oldList.promise;
			if (path === '/api/v1/websites/site-b') return response(website('site-b', 'disabled'));
			assert.equal(path, `/api/v1/websites/site-b/${operation.endpoint}`);
			return response([]);
		});
		page.params.id = 'site-b';
		page.url.pathname = `/websites/site-b/${operation.route}`;
		flushSync();
		await settle();
		oldList.resolve(response([operation.pending ?? operation.stale]));
		await settle();
		assert.equal(target.querySelector('h2')?.textContent, `${operation.title} · b.example.com`);
		assert.doesNotMatch(target.textContent, /site-a-only-command/);
		assert.equal(target.querySelector('[aria-current="page"]')?.getAttribute('href'), `/websites/site-b/${operation.route}`);
		assert.equal(intervals.size, 0, 'the previous website must not restart polling');
		assert.deepEqual(requests, [
			{ path: '/api/v1/websites/site-a', method: 'GET' },
			{ path: `/api/v1/websites/site-a/${operation.endpoint}`, method: 'GET' },
			{ path: '/api/v1/websites/site-b', method: 'GET' },
			{ path: `/api/v1/websites/site-b/${operation.endpoint}`, method: 'GET' }
		]);
	});

	if (!operation.pending) continue;

	test(`${operation.title} does not restart polling when a deferred list resolves after destroy`, async (t) => {
		const list = deferred();
		const { intervals, requests, destroy } = await openPage(t, operation, async (path) => {
			if (path === '/api/v1/websites/site-a') return response(website('site-a'));
			assert.equal(path, `/api/v1/websites/site-a/${operation.endpoint}`);
			return list.promise;
		});
		assert.deepEqual(requests, [
			{ path: '/api/v1/websites/site-a', method: 'GET' },
			{ path: `/api/v1/websites/site-a/${operation.endpoint}`, method: 'GET' }
		]);
		await destroy();
		assert.equal(intervals.size, 0);
		list.resolve(response([operation.pending]));
		await settle();
		assert.equal(intervals.size, 0, 'a destroyed page must leave zero intervals');
	});

	test(`${operation.title} clears an active interval on destroy`, async (t) => {
		const { intervals, destroy } = await openPage(t, operation, async (path) => {
			if (path === '/api/v1/websites/site-a') return response(website('site-a'));
			assert.equal(path, `/api/v1/websites/site-a/${operation.endpoint}`);
			return response([operation.pending]);
		});
		assert.equal(intervals.size, 1);
		assert.equal([...intervals.values()][0].delay, 5000);
		await destroy();
		assert.equal(intervals.size, 0);
	});

	test(`${operation.title} stops polling when no pending resource remains`, async (t) => {
		let firstList = true;
		const { intervals } = await openPage(t, operation, async (path) => {
			if (path === '/api/v1/websites/site-a') return response(website('site-a'));
			assert.equal(path, `/api/v1/websites/site-a/${operation.endpoint}`);
			const resources = firstList ? [operation.pending] : [];
			firstList = false;
			return response(resources);
		});
		assert.equal(intervals.size, 1);
		await [...intervals.values()][0].callback();
		await settle();
		assert.equal(intervals.size, 0);
	});
}
