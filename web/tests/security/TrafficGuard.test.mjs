import { test } from 'node:test';
import assert from 'node:assert/strict';

let enforcementWarning;
let observationProgress;
let buildTrafficProfile;
let summarizeTraffic;
try {
	({ enforcementWarning, observationProgress, buildTrafficProfile, summarizeTraffic } = await import('../../src/lib/traffic-guard.js'));
} catch {
	// The first TDD run intentionally reaches assertions before the helper exists.
}

test('enforcement summary distinguishes local mitigation from volumetric DDoS protection', () => {
	assert.match(enforcementWarning('balanced'), /HTTP-layer/i);
	assert.match(enforcementWarning('balanced'), /CDN|provider/i);
});

test('observation progress is capped to a 24-hour window', () => {
	const now = new Date('2026-09-09T12:00:00Z');
	assert.equal(observationProgress('2026-09-09T00:00:00Z', now), 50);
	assert.equal(observationProgress('2026-09-08T00:00:00Z', now), 100);
	assert.equal(observationProgress('invalid', now), 0);
});

test('traffic profile helper validates custom settings and proxy trust', () => {
	assert.throws(() => buildTrafficProfile({ mode: 'custom', requests_per_second: 0, burst: 20, connections: 20 }), /request rate/i);
	assert.throws(() => buildTrafficProfile({ mode: 'observe', proxy_mode: 'custom', proxy_header: 'X-Real-IP', proxy_cidrs: [] }), /CIDR/i);
	assert.equal(buildTrafficProfile({ mode: 'balanced', proxy_mode: 'direct' }).requests_per_second, 10);
});

test('traffic summary totals status and top client evidence', () => {
	const summary = summarizeTraffic([
		{ requests: 10, status_4xx: 2, status_5xx: 1, status_429: 1, bytes: 100, peak_rps: 3, top_ips: { '1.1.1.1': 4 } },
		{ requests: 20, status_4xx: 3, status_5xx: 0, status_429: 2, bytes: 200, peak_rps: 5, top_ips: { '1.1.1.1': 2, '2.2.2.2': 5 } }
	]);
	assert.deepEqual(summary, { requests: 30, status4xx: 5, status5xx: 1, status429: 3, bytes: 300, peakRPS: 5, topIPs: [['1.1.1.1', 6], ['2.2.2.2', 5]] });
});
