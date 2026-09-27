import { test } from 'node:test';
import assert from 'node:assert/strict';
import { buildErDiagram } from '../../src/lib/erdiagram.js';

test('renders entity boxes with PK/FK markers and relations', () => {
	const text = buildErDiagram({
		engine: 'mysql',
		tables: [
			{
				name: 'users',
				columns: [
					{ name: 'id', type: 'bigint unsigned', nullable: false, key: 'PRI', default: '' },
					{ name: 'name', type: 'varchar(190)', nullable: true, key: '', default: 'NULL' }
				]
			},
			{
				name: 'posts',
				columns: [
					{ name: 'id', type: 'bigint', nullable: false, key: 'PRI', default: '' },
					{ name: 'user_id', type: 'bigint', nullable: true, key: 'MUL', default: '', fk_table: 'users', fk_column: 'id' }
				]
			}
		],
		relations: [{ from_table: 'posts', from_column: 'user_id', to_table: 'users', to_column: 'id' }]
	});

	assert.match(text, /^erDiagram\n/);
	assert.match(text, /users \{\n {8}bigint_unsigned id PK\n {8}varchar_190 name\n {4}\}/);
	assert.match(text, /posts \{[\s\S]*bigint user_id FK[\s\S]*\}/);
	assert.match(text, /posts }o--\|\| users : "user_id"/);
});

test('marks unique keys as UK', () => {
	const text = buildErDiagram({
		engine: 'postgresql',
		tables: [{ name: 't', columns: [{ name: 'email', type: 'varchar', nullable: false, key: 'UNI', default: '' }] }],
		relations: []
	});
	assert.match(text, /varchar email UK/);
});

test('sanitizes schema-qualified PostgreSQL table names', () => {
	const text = buildErDiagram({
		engine: 'postgresql',
		tables: [{ name: 'analytics.page_events', columns: [{ name: 'id', type: 'integer', nullable: false, key: 'PRI', default: '' }] }],
		relations: []
	});
	assert.match(text, /analytics_page_events \{/);
});

test('handles no-FK databases by emitting entities only', () => {
	const text = buildErDiagram({
		engine: 'mysql',
		tables: [{ name: 'settings', columns: [{ name: 'k', type: 'varchar', nullable: false, key: 'PRI', default: '' }] }],
		relations: []
	});
	assert.match(text, /settings \{/);
	assert.doesNotMatch(text, /}o--/);
});

test('empty database yields a bare diagram', () => {
	assert.equal(buildErDiagram({ engine: 'mysql', tables: [], relations: [] }), 'erDiagram\n');
});
