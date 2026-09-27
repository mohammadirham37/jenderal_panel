import { readFileSync } from 'node:fs';
import { test } from 'node:test';
import assert from 'node:assert/strict';

let websiteOperationAPI;
try {
	({ websiteOperationAPI } = await import('../../src/lib/website-operations.js'));
} catch {}

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
		queueWorkers: '/api/v1/websites/site%2F1/queue-workers',
		bandwidth: '/api/v1/websites/site%2F1/bandwidth'
	});
});

const legacyOperationRoutes = [
	{
		name: 'Deployments',
		url: new URL('../../src/routes/deployments/+page.ts', import.meta.url)
	},
	{
		name: 'SSL',
		url: new URL('../../src/routes/ssl/+page.ts', import.meta.url)
	},
	{
		name: 'Cron',
		url: new URL('../../src/routes/cron/+page.ts', import.meta.url)
	},
	{
		name: 'Queue workers',
		url: new URL('../../src/routes/queue-workers/+page.ts', import.meta.url)
	}
];

for (const legacyOperationRoute of legacyOperationRoutes) {
	test(`${legacyOperationRoute.name} legacy route redirects to websites`, () => {
		// Assert on the file source: node --test cannot import .ts route
		// modules directly (Unknown file extension), and the redirect call is
		// the only thing the import-based version checked anyway.
		const source = readFileSync(legacyOperationRoute.url, 'utf8');
		assert.match(source, /redirect\(307,\s*['"]\/websites['"]\)/);
	});
}
