/**
 * Builds a Mermaid `erDiagram` definition from the dbmanager schema
 * endpoint's payload. Kept as a plain-JS pure function so it is unit-testable
 * with node --test (see web/tests/databases/ErDiagram.test.mjs).
 */

/** @param {unknown} value @returns {string} */
function sanitize(value) {
	const cleaned = String(value ?? '').replace(/[^A-Za-z0-9_]/g, '_').replace(/_+/g, '_').replace(/^_+|_+$/g, '');
	return cleaned || 't';
}

/** @param {{ key?: string, fk_table?: string }} column @returns {string} */
function keyToken(column) {
	if (column.fk_table) return 'FK';
	if (column.key === 'PRI') return 'PK';
	if (column.key === 'UNI') return 'UK';
	return '';
}

/**
 * @param {{ engine: string, tables: { name: string, columns: { name: string, type: string, nullable: boolean, key: string, default: string, fk_table?: string, fk_column?: string }[] }[], relations: { from_table: string, from_column: string, to_table: string, to_column: string }[] }} diagram
 * @returns {string} Mermaid erDiagram source text
 */
export function buildErDiagram(diagram) {
	const tables = diagram?.tables ?? [];
	if (tables.length === 0) {
		return 'erDiagram\n';
	}
	const lines = ['erDiagram'];
	for (const table of tables) {
		lines.push(`    ${sanitize(table.name)} {`);
		for (const column of table.columns ?? []) {
			const parts = [sanitize(column.type), sanitize(column.name)];
			const key = keyToken(column);
			if (key) parts.push(key);
			lines.push(`        ${parts.join(' ')}`);
		}
		lines.push('    }');
	}
	for (const rel of diagram.relations ?? []) {
		lines.push(
			`    ${sanitize(rel.from_table)} }o--|| ${sanitize(rel.to_table)} : "${sanitize(rel.from_column)}"`
		);
	}
	return lines.join('\n') + '\n';
}
