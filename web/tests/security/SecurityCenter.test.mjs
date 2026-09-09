import { test } from 'node:test';
import assert from 'node:assert/strict';

let buildSafeFail2banSettings;
let validateBan;
let normalizeOverview;
let conditionTone;
let formatBanExpiry;
try {
	({ buildSafeFail2banSettings, validateBan, normalizeOverview, conditionTone, formatBanExpiry } =
		await import('../../src/lib/security.js'));
} catch {
	// The first TDD run intentionally reaches the assertions before the helper exists.
}

test('safe preset cannot produce a permanent ban', () => {
	assert.deepEqual(buildSafeFail2banSettings(['203.0.113.8/32']), {
		sshd_enabled: true,
		max_retry: 5,
		find_time_seconds: 600,
		ban_time_seconds: 900,
		ignore_ips: ['203.0.113.8/32']
	});
	assert.throws(
		() => validateBan({ jail: 'sshd', ip: '203.0.113.7', duration_seconds: 0 }),
		/temporary/i
	);
});

test('overview condition always includes human-readable reasons', () => {
	assert.deepEqual(normalizeOverview({ condition: 'needs_attention', reasons: [] }).reasons, [
		'Review component status below.'
	]);
});

test('condition tone and expiry formatting are bounded and predictable', () => {
	assert.equal(conditionTone('critical'), 'critical');
	assert.equal(conditionTone('needs_attention'), 'warning');
	assert.equal(conditionTone('unknown'), 'neutral');
	assert.equal(formatBanExpiry(null), 'Managed by Fail2ban');
	assert.match(formatBanExpiry('2026-09-09T03:15:00Z', new Date('2026-09-09T03:00:00Z')), /15 min/);
});

test('manual ban validation requires a known jail, IP, and bounded duration', () => {
	assert.throws(() => validateBan({ jail: '', ip: '203.0.113.7', duration_seconds: 300 }), /jail/i);
	assert.throws(() => validateBan({ jail: 'sshd', ip: '', duration_seconds: 300 }), /IP/i);
	assert.throws(() => validateBan({ jail: 'sshd', ip: '203.0.113.7', duration_seconds: 700000 }), /seven days/i);
	assert.deepEqual(validateBan({ jail: 'sshd', ip: '203.0.113.7', duration_seconds: 300 }), {
		jail: 'sshd',
		ip: '203.0.113.7',
		duration_seconds: 300
	});
});
