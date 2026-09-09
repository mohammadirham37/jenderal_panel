import { test } from 'node:test';
import assert from 'node:assert/strict';
import { normalizeSetupReview, buildTrafficRecovery, buildSafeSetupRequest } from '../../src/lib/security-setup.js';

test('safe setup review keeps mutation and warning order', () => {
	const review = normalizeSetupReview({ mutations: ['install fail2ban', 'install clamav'], warnings: ['Confirm console access'] });
	assert.deepEqual(review, { mutations: ['install fail2ban', 'install clamav'], warnings: ['Confirm console access'], hash: '' });
});

test('recovery action always returns Traffic Guard to observe', () => {
	assert.deepEqual(buildTrafficRecovery('01SITE'), { website_id: '01SITE', mode: 'observe', confirm: true });
});

test('safe setup request requires management CIDRs for SSH protection', () => {
	assert.throws(() => buildSafeSetupRequest({ enable_fail2ban: true, management_cidrs: [] }), /management CIDR/i);
	assert.equal(buildSafeSetupRequest({ enable_fail2ban: false, malware_mode: 'low_memory' }).malware_mode, 'low_memory');
});
