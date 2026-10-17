import { test } from 'node:test';
import assert from 'node:assert/strict';
import { buildActionPlan, buildProtectionChecklist, blockedSummary } from '../../src/lib/security-plan.js';

const healthy = {
	overview: {
		setup_complete: true,
		components: [{ name: 'fail2ban', enabled: true, healthy: true }],
		posture: { components: { firewall: 'healthy', ssh: 'healthy', security_updates: 'healthy', nginx: 'healthy', apparmor: 'healthy' }, findings: [] }
	},
	fail2ban: { installed: true, running: true, jails: [{ name: 'sshd', currently_banned: 3, total_banned: 10 }, { name: 'recidive', currently_banned: 1, total_banned: 2 }] },
	malware: { installed: true, signature_fresh: true },
	malwareSchedules: [{ enabled: true }],
	quarantine: [],
	events: [],
	waf: { modsecurity: { installed: true, mode: 'blocking' } },
	trafficProfiles: [{ mode: 'balanced' }],
	websites: [{ id: 'w1' }],
	canFirewall: true
};

test('a fully protected server has no actions and a green checklist', () => {
	assert.deepEqual(buildActionPlan(healthy), []);
	const checklist = buildProtectionChecklist(healthy);
	assert.equal(checklist.filter((item) => item.status === 'ok').length, checklist.length);
});

test('blocked Fail2ban addresses never become actions', () => {
	const plan = buildActionPlan({
		...healthy,
		events: Array.from({ length: 8 }, (_, i) => ({
			id: 'e' + i, status: 'open', severity: 'low', component: 'fail2ban', category: 'intrusion_blocked'
		}))
	});
	assert.deepEqual(plan, []);
	assert.deepEqual(blockedSummary(healthy.fail2ban), { current: 4, total: 12 });
});

test('open events are grouped into one review item with all event ids', () => {
	const plan = buildActionPlan({
		...healthy,
		events: [
			{ id: 'a', status: 'open', severity: 'medium', component: 'traffic_guard', category: 'traffic', recommended_action: 'Review top clients.' },
			{ id: 'b', status: 'acknowledged', severity: 'critical', component: 'traffic_guard', category: 'traffic' },
			{ id: 'c', status: 'resolved', severity: 'high', component: 'traffic_guard', category: 'traffic' },
			{ id: 'd', status: 'open', severity: 'medium', component: 'posture', category: 'posture' }
		]
	});
	assert.equal(plan.length, 1);
	assert.equal(plan[0].code, 'events_review');
	assert.equal(plan[0].severity, 'critical');
	assert.equal(plan[0].params.count, 2);
	assert.deepEqual(plan[0].eventIds, ['a', 'b']);
});

test('missing protections become ordered, actionable items', () => {
	const plan = buildActionPlan({
		overview: {
			setup_complete: false,
			components: [],
			posture: {
				components: {},
				findings: [
					{ code: 'security_updates_available', component: 'security_updates', severity: 'medium', evidence: { count: 7 } },
					{ code: 'nginx_invalid', component: 'nginx', severity: 'critical' },
					{ code: 'ufw_inactive', component: 'firewall', severity: 'medium' }
				]
			}
		},
		fail2ban: { installed: false },
		malware: { installed: false },
		quarantine: [{ status: 'quarantined' }, { status: 'restored' }],
		waf: { modsecurity: { installed: false } },
		trafficProfiles: [],
		websites: [{ id: 'w1' }],
		canFirewall: false
	});
	const codes = plan.map((item) => item.code);
	assert.equal(codes[0], 'nginx_invalid');
	assert.deepEqual(codes.slice(1, 3).sort(), ['fail2ban_missing', 'quarantine_review']);
	for (const code of ['security_updates_available', 'ufw_inactive', 'malware_missing', 'setup_incomplete', 'waf_missing', 'traffic_unconfigured']) {
		assert.ok(codes.includes(code), code + ' missing from ' + codes.join(','));
	}
	const updates = plan.find((item) => item.code === 'security_updates_available');
	assert.equal(updates?.params.count, 7);
	assert.match(updates?.command || '', /apt upgrade/);
	// Without firewall.view the item links to the Firewall page instead of the tab.
	const firewall = plan.find((item) => item.code === 'ufw_inactive');
	assert.equal(firewall?.tab, undefined);
	assert.equal(firewall?.href, '/firewall');
	assert.equal(plan.find((item) => item.code === 'quarantine_review')?.params.count, 1);
});
