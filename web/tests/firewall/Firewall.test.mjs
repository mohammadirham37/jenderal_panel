import { test } from 'node:test';
import assert from 'node:assert/strict';

let firewallActionTone;
let normalizeFirewallStatus;
let parseFirewallPort;
let publicSMBRules;
try {
	({ firewallActionTone, normalizeFirewallStatus, parseFirewallPort, publicSMBRules } = await import('../../src/lib/firewall.js'));
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

test('flags SMB ports opened to anyone, including ranges and IPv6 rows', () => {
	const rules = [
		{ to: '445/tcp', action: 'ALLOW IN', from: 'Anywhere' },
		{ to: '445/tcp (v6)', action: 'ALLOW IN', from: 'Anywhere (v6)' },
		{ to: '137:139/udp', action: 'ALLOW IN', from: 'Anywhere' },
		{ to: '80,443/tcp', action: 'ALLOW IN', from: 'Anywhere' },
		{ to: '445', action: 'ALLOW IN', from: '10.0.0.5' },
		{ to: '445', action: 'DENY IN', from: 'Anywhere' },
		{ to: '22/tcp', action: 'LIMIT IN', from: 'Anywhere' }
	];
	assert.deepEqual(publicSMBRules(rules).map((r) => r.to), ['445/tcp', '445/tcp (v6)', '137:139/udp']);
});
