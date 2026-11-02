import test from 'node:test';
import assert from 'node:assert/strict';
import {
	formatBytes,
	formatServerTime,
	formatUptime,
	diskPercent,
	uptimeSeconds,
	cleanUptimeText,
	isValidHostname,
	loadPercent,
	parsePercent,
	usageTone,
	utcOffsetLabel
} from '../../src/lib/server-info.js';

test('isValidHostname follows RFC 1123 like the backend', () => {
	for (const ok of ['web01', 'srv-1.example.com', 'A1', ' web01 ']) assert.equal(isValidHostname(ok), true, ok);
	for (const bad of ['', '-h', 'bad_host', 'a..b', 'host.', 'x'.repeat(64), 'spa ce', 'end-']) assert.equal(isValidHostname(bad), false, bad);
});

test('parsePercent and usageTone', () => {
	assert.equal(parsePercent('45%'), 45);
	assert.equal(parsePercent('120%'), 100);
	assert.equal(parsePercent('-'), null);
	assert.equal(usageTone(50), 'green');
	assert.equal(usageTone(80), 'yellow');
	assert.equal(usageTone(95), 'red');
	assert.equal(usageTone(null), 'gray');
});

test('formatBytes, formatUptime, loadPercent', () => {
	assert.equal(formatBytes(0), '0 B');
	assert.equal(formatBytes(1536), '1.5 KB');
	assert.equal(formatUptime(0), null);
	assert.equal(formatUptime(59), '0m');
	assert.equal(formatUptime(3 * 86400 + 3600 + 120), '3d 1h 2m');
	assert.equal(formatUptime(7200), '2h 0m');
	assert.equal(formatUptime(3 * 86400 + 3600 + 120, 'id'), '3 hari 1 jam 2 mnt');
	// Metrics carry a Go time.Duration (nanoseconds), not seconds.
	assert.equal(uptimeSeconds(3 * 86400 * 1e9), 3 * 86400);
	assert.equal(uptimeSeconds(0), 0);
	assert.equal(cleanUptimeText('up 2 weeks, 3 days'), '2 weeks, 3 days');
	assert.equal(loadPercent(2, 4), 50);
	assert.equal(loadPercent(1, 0), null);
});

test('server time helpers tolerate unknown zones', () => {
	const date = new Date('2026-01-01T00:00:00Z');
	assert.equal(utcOffsetLabel(date, 'Asia/Jakarta'), 'UTC+07:00');
	assert.equal(utcOffsetLabel(date, 'UTC'), 'UTC+00:00');
	assert.equal(utcOffsetLabel(date, 'Not/AZone'), '');
	assert.equal(formatServerTime(date, 'Not/AZone'), null);
	assert.match(formatServerTime(date, 'Asia/Jakarta', 'en-GB') || '', /07:00:00/);
});

test('diskPercent matches df (root reserve excluded)', () => {
	// 100 GB disk, 60 GB used, 35 GB available to users (5 GB reserved).
	const GB = 1e9;
	assert.equal(diskPercent(60 * GB, 100 * GB, 35 * GB), 64); // df: 60/95 = 63.2% -> 64%
	assert.equal(diskPercent(60 * GB, 100 * GB), 60); // history without avail
	assert.equal(diskPercent(95 * GB, 100 * GB, 0.0001), 100);
	assert.equal(diskPercent(NaN, 100), null);
});
