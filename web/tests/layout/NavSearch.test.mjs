import { test } from 'node:test';
import assert from 'node:assert/strict';
import { filterNavGroups } from '../../src/lib/nav-search.js';

const labels = {
	'g.web': 'Web',
	'g.infra': 'Infrastruktur',
	'n.websites': 'Website',
	'n.php': 'PHP',
	'n.databases': 'Basis Data',
	'n.nginx': 'Nginx'
};
const label = (key) => labels[key] ?? key;
const groups = [
	{ key: 'g.web', items: [
		{ href: '/websites', labelKey: 'n.websites', keywords: ['ssl', 'domain'] },
		{ href: '/php', labelKey: 'n.php' }
	] },
	{ key: 'g.infra', items: [
		{ href: '/databases', labelKey: 'n.databases', keywords: ['mysql', 'postgresql'] },
		{ href: '/nginx', labelKey: 'n.nginx' }
	] }
];
const hrefs = (result) => result.flatMap((g) => g.items.map((i) => i.href));

test('empty query keeps the whole menu', () => {
	assert.equal(filterNavGroups(groups, '   ', label), groups);
});

test('matches translated labels, paths, keywords, and group names case-insensitively', () => {
	assert.deepEqual(hrefs(filterNavGroups(groups, 'basis', label)), ['/databases']);
	assert.deepEqual(hrefs(filterNavGroups(groups, 'MySQL', label)), ['/databases']);
	assert.deepEqual(hrefs(filterNavGroups(groups, 'ssl', label)), ['/websites']);
	assert.deepEqual(hrefs(filterNavGroups(groups, 'infra', label)), ['/databases', '/nginx']);
	assert.deepEqual(hrefs(filterNavGroups(groups, '/php', label)), ['/php']);
});

test('every term must match and empty groups are dropped', () => {
	const result = filterNavGroups(groups, 'infra ngi', label);
	assert.deepEqual(hrefs(result), ['/nginx']);
	assert.equal(result.length, 1);
	assert.deepEqual(filterNavGroups(groups, 'nothing-here', label), []);
});
