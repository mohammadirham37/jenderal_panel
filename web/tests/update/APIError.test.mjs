import { readFileSync } from 'node:fs';
import { test } from 'node:test';
import assert from 'node:assert/strict';
import ts from 'typescript';

const source = readFileSync(new URL('../../src/lib/api.ts', import.meta.url), 'utf8');
const authSource = readFileSync(new URL('../../src/lib/stores/auth.ts', import.meta.url), 'utf8');
const javascript = ts.transpileModule(source, {
	compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.ESNext }
}).outputText;
const moduleURL = `data:text/javascript;base64,${Buffer.from(javascript).toString('base64')}`;
const { api, APIRequestError } = await import(moduleURL);

test('API errors preserve HTTP status and application error code', async () => {
	assert.equal(typeof APIRequestError, 'function', 'expected a structured API error');

	const originalFetch = globalThis.fetch;
	globalThis.fetch = async () => new Response(JSON.stringify({
		error: { code: 'NOT_FOUND', message: 'task not found' }
	}), {
		status: 404,
		headers: { 'Content-Type': 'application/json' }
	});

	try {
		await assert.rejects(
			api.get('/api/v1/tasks/stale-task'),
			(error) => error instanceof APIRequestError && error.status === 404 && error.code === 'NOT_FOUND' && error.message === 'task not found'
		);
	} finally {
		globalThis.fetch = originalFetch;
	}
});

test('timed API requests abort and provide SSH restart guidance', async () => {
	const originalFetch = globalThis.fetch;
	const originalDocument = globalThis.document;
	globalThis.document = { cookie: '' };
	globalThis.fetch = async (_path, init) => new Promise((_resolve, reject) => {
		init.signal.addEventListener('abort', () => reject(new DOMException('aborted', 'AbortError')), { once: true });
	});

	try {
		await assert.rejects(
			api.post('/api/v1/auth/login', { username: 'admin', password: 'secret' }, { timeoutMs: 5 }),
			(error) => error instanceof APIRequestError
				&& error.status === 0
				&& error.code === 'REQUEST_TIMEOUT'
				&& error.message.includes('sudo /opt/jenderal/jenderal restart')
		);
	} finally {
		globalThis.fetch = originalFetch;
		if (originalDocument === undefined) {
			delete globalThis.document;
		} else {
			globalThis.document = originalDocument;
		}
	}
});

test('request timeout remains active while the response body is being read', async () => {
	const originalFetch = globalThis.fetch;
	globalThis.fetch = async (_path, init) => ({
		ok: true,
		json: () => new Promise((_resolve, reject) => {
			init.signal.addEventListener('abort', () => reject(new DOMException('aborted', 'AbortError')), { once: true });
		})
	});

	try {
		await assert.rejects(
			Promise.race([
				api.get('/api/v1/auth/me', { timeoutMs: 5 }),
				new Promise((_resolve, reject) => setTimeout(() => reject(new Error('test deadline exceeded')), 100))
			]),
			(error) => error instanceof APIRequestError && error.code === 'REQUEST_TIMEOUT'
		);
	} finally {
		globalThis.fetch = originalFetch;
	}
});

test('all authentication requests use the bounded timeout', () => {
	assert.match(authSource, /const authRequestOptions = \{ timeoutMs: 15_000 \}/);
	assert.match(authSource, /api\.post<LoginResponse>\('\/api\/v1\/auth\/login',[\s\S]*authRequestOptions\)/);
	assert.match(authSource, /api\.post\('\/api\/v1\/auth\/logout', undefined, authRequestOptions\)/);
	assert.match(authSource, /api\.get<UserWithRoles>\('\/api\/v1\/auth\/me', authRequestOptions\)/);
	assert.match(authSource, /export const authError = writable\(''\)/);
	assert.match(authSource, /authError\.set\(error\.message\)/);
});
