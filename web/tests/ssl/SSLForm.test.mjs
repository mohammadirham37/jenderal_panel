import { test } from 'node:test';
import assert from 'node:assert/strict';

let domainsForWebsite;
let buildSSLInstallRequest;
try {
	({ domainsForWebsite, buildSSLInstallRequest } = await import('../../src/lib/ssl-form.js'));
} catch {}

test('offers only domains registered to the selected website', () => {
	assert.equal(typeof domainsForWebsite, 'function');
	const websites = [
		{
			id: 'ws-1',
			domain: 'example.com',
			domains: [{ name: 'example.com' }, { name: 'www.example.com' }, { name: 'www.example.com' }]
		},
		{ id: 'ws-2', domain: 'other.test', domains: [{ name: 'other.test' }] }
	];
	assert.deepEqual(domainsForWebsite(websites, 'ws-1'), ['example.com', 'www.example.com']);
});

test('uses the existing lets encrypt issue endpoint', () => {
	assert.deepEqual(
		buildSSLInstallRequest('letsencrypt', {
			websiteId: 'ws-1',
			domain: ' example.com ',
			certificatePEM: '',
			privateKeyPEM: ''
		}),
		{ path: '/api/v1/ssl/issue', body: { website_id: 'ws-1', domain: 'example.com' } }
	);
});

test('sends pasted material only to the custom endpoint', () => {
	assert.deepEqual(
		buildSSLInstallRequest('custom', {
			websiteId: 'ws-1',
			domain: 'example.com',
			certificatePEM: 'CERT',
			privateKeyPEM: 'KEY'
		}),
		{
			path: '/api/v1/ssl/custom',
			body: {
				website_id: 'ws-1',
				domain: 'example.com',
				certificate_pem: 'CERT',
				private_key_pem: 'KEY'
			}
		}
	);
});
