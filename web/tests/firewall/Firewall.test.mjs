import { test } from 'node:test';
import assert from 'node:assert/strict';

let firewallActionTone;
let normalizeFirewallStatus;
let parseFirewallPort;
try {
	({ firewallActionTone, normalizeFirewallStatus, parseFirewallPort } = await import('../../src/lib/firewall.js'));
} catch {
	// The first TDD run intentionally reaches the assertions before the helper exists.
}

test('normalizes the direct firewall status response', () => {
	assert.equal(typeof normalizeFirewallStatus, 'function', 'expected a firewall response normalizer');
	assert.deepEqual(normalizeFirewallStatus({ active: true, default: 'deny', rules: [] }), {
		active: true,
		defaultPolicy: 'deny',
		rules: []
	});
});

test('converts only complete valid firewall ports', () => {
	assert.equal(parseFirewallPort('443'), 443);
	assert.throws(() => parseFirewallPort('443x'), /valid port/i);
	assert.throws(() => parseFirewallPort('0'), /between 1 and 65535/i);
});

test('classifies compound ufw actions', () => {
	assert.equal(firewallActionTone('ALLOW IN'), 'allow');
	assert.equal(firewallActionTone('DENY IN'), 'deny');
	assert.equal(firewallActionTone('REJECT IN'), 'deny');
	assert.equal(firewallActionTone('LIMIT IN'), 'limit');
});
