/**
 * Database server config helpers for the db-config page: a form mode that
 * edits known directives inside the raw file, and a manual mode over the
 * whole file. MySQL uses my.cnf INI sections, PostgreSQL uses flat
 * `key = value` lines with commented template defaults.
 *
 * @type {Record<string, Record<string, {pattern: RegExp, placeholder: string}>>}
 */
export const dbConfigFields = {
	mysql: {
		port: { pattern: /^[1-9]\d*$/, placeholder: '3306' },
		'bind-address': { pattern: /^[0-9a-zA-Z.]+$/, placeholder: '127.0.0.1' },
		max_connections: { pattern: /^[1-9]\d*$/, placeholder: '151' },
		innodb_buffer_pool_size: { pattern: /^\d+[KMG]?$/i, placeholder: '128M' },
		max_allowed_packet: { pattern: /^\d+[KMG]?$/i, placeholder: '64M' },
		tmp_table_size: { pattern: /^\d+[KMG]?$/i, placeholder: '32M' },
		max_heap_table_size: { pattern: /^\d+[KMG]?$/i, placeholder: '32M' },
		wait_timeout: { pattern: /^[1-9]\d*$/, placeholder: '600' },
		slow_query_log: { pattern: /^(on|off|0|1)$/i, placeholder: 'OFF' },
		long_query_time: { pattern: /^\d+(\.\d+)?$/, placeholder: '2' },
		'character-set-server': { pattern: /^[A-Za-z0-9_-]+$/, placeholder: 'utf8mb4' },
		'collation-server': { pattern: /^[A-Za-z0-9_-]+$/, placeholder: 'utf8mb4_general_ci' }
	},
	postgresql: {
		port: { pattern: /^[1-9]\d*$/, placeholder: '5432' },
		listen_addresses: { pattern: /^[0-9a-zA-Z*,.\s]+$/, placeholder: 'localhost' },
		max_connections: { pattern: /^[1-9]\d*$/, placeholder: '100' },
		shared_buffers: { pattern: /^\d+[KMGT]B?$/i, placeholder: '128MB' },
		effective_cache_size: { pattern: /^\d+[KMGT]B?$/i, placeholder: '4GB' },
		work_mem: { pattern: /^\d+[KMGT]B?$/i, placeholder: '4MB' },
		maintenance_work_mem: { pattern: /^\d+[KMGT]B?$/i, placeholder: '64MB' },
		wal_buffers: { pattern: /^-1|\d+[KMGT]B?$/i, placeholder: '-1' },
		min_wal_size: { pattern: /^\d+[KMGT]B?$/i, placeholder: '80MB' },
		max_wal_size: { pattern: /^\d+[KMGT]B?$/i, placeholder: '1GB' },
		checkpoint_completion_target: { pattern: /^(0(\.\d+)?|1(\.0+)?)$/, placeholder: '0.9' },
		random_page_cost: { pattern: /^\d+(\.\d+)?$/, placeholder: '4.0' },
		log_min_duration_statement: { pattern: /^-1|\d+$/, placeholder: '-1' },
		synchronous_commit: { pattern: /^(on|off|local|remote_write|remote_apply)$/i, placeholder: 'on' }
	}
};

/**
 * Parse an `key = value` or bare `key` entry (a bare flag is ON).
 * @param {string} line
 * @returns {{key: string, value: string} | null}
 */
function parseMySQLEntry(line) {
	const assignment = line.match(/^([A-Za-z0-9_-]+)\s*=\s*(.*)$/);
	if (assignment) return { key: assignment[1], value: assignment[2].trim() };
	const flag = line.match(/^([A-Za-z0-9_-]+)$/);
	if (flag) return { key: flag[1], value: 'ON' };
	return null;
}

/**
 * @param {string} content
 * @param {Record<string, {pattern: RegExp}>} fields
 * @returns {{values: Record<string,string>, errors: string[]}}
 */
function parseMySQL(content, fields) {
	let inMysqld = false;
	/** @type {Record<string,string>} */
	const values = {};
	/** @type {string[]} */
	const errors = [];
	for (const line of content.split('\n')) {
		const trimmed = line.trim();
		if (trimmed.startsWith('[')) {
			inMysqld = trimmed.toLowerCase() === '[mysqld]';
			continue;
		}
		if (!inMysqld || trimmed === '' || trimmed.startsWith('#')) continue;
		const entry = parseMySQLEntry(trimmed);
		if (!entry || !fields[entry.key]) continue;
		values[entry.key] = entry.value;
		if (!fields[entry.key].pattern.test(entry.value)) errors.push(entry.key);
	}
	return { values, errors };
}

/**
 * @param {string} content
 * @param {Record<string, {pattern: RegExp}>} fields
 * @returns {{values: Record<string,string>, errors: string[]}}
 */
function parsePostgreSQL(content, fields) {
	/** @type {Record<string,string>} */
	const values = {};
	/** @type {string[]} */
	const errors = [];
	for (const line of content.split('\n')) {
		const trimmed = line.trim();
		if (trimmed === '' || trimmed.startsWith('#')) continue;
		const match = trimmed.match(/^([A-Za-z0-9_.]+)\s*=\s*(.*)$/);
		if (!match || !fields[match[1]]) continue;
		let value = match[2];
		const inlineComment = value.indexOf(' #');
		if (inlineComment !== -1) value = value.slice(0, inlineComment);
		value = value.trim().replace(/^(['"])(.*)\1$/, '$2').trim();
		values[match[1]] = value;
		if (!fields[match[1]].pattern.test(value)) errors.push(match[1]);
	}
	return { values, errors };
}

/**
 * Read the active (non-commented) values of the known directives.
 * @param {string} content
 * @param {string} engine
 * @returns {{values: Record<string,string>, errors: string[]}}
 */
export function parseDbConfig(content, engine) {
	const fields = dbConfigFields[engine];
	if (!fields) throw new Error(`Unsupported engine: ${engine}`);
	return engine === 'mysql' ? parseMySQL(content, fields) : parsePostgreSQL(content, fields);
}

/**
 * Update one directive in the config content. An empty value removes the
 * active directive (reverting to the engine default). MySQL edits live in the
 * [mysqld] section; PostgreSQL inserts next to the commented template default
 * when one exists, otherwise at the end of the file.
 * @param {string} content
 * @param {string} engine
 * @param {string} key
 * @param {string} value
 */
export function applyDbConfig(content, engine, key, value) {
	const fields = dbConfigFields[engine];
	if (!fields || !fields[key]) throw new Error(`Unsupported ${engine} directive: ${key}`);
	const normalized = String(value ?? '').trim();
	if (normalized !== '' && !fields[key].pattern.test(normalized)) {
		throw new Error(`Invalid value for ${key}: ${normalized}`);
	}
	return engine === 'mysql'
		? applyMySQL(content, key, normalized)
		: applyPostgreSQL(content, key, normalized);
}

/**
 * @param {string} content
 * @param {string} key
 * @param {string} value
 */
function applyMySQL(content, key, value) {
	const lines = content.split('\n');
	let inMysqld = false;
	let mysqldSeen = false;
	let changed = false;
	for (let index = 0; index < lines.length; index++) {
		const trimmed = lines[index].trim();
		if (trimmed.startsWith('[')) {
			inMysqld = trimmed.toLowerCase() === '[mysqld]';
			if (inMysqld) mysqldSeen = true;
			continue;
		}
		if (!inMysqld || trimmed === '' || trimmed.startsWith('#')) continue;
		const entry = parseMySQLEntry(trimmed);
		if (!entry || entry.key !== key) continue;
		if (value === '') {
			lines.splice(index, 1);
			index--;
		} else {
			lines[index] = `${key} = ${value}`;
		}
		changed = true;
	}
	if (!changed && value !== '') {
		if (mysqldSeen) {
			const header = lines.findIndex((l) => l.trim().toLowerCase() === '[mysqld]');
			lines.splice(header + 1, 0, `${key} = ${value}`);
		} else {
			const leadingNewline = content === '' || content.endsWith('\n') ? '' : '\n';
			return `${content}${leadingNewline}[mysqld]\n${key} = ${value}\n`;
		}
	}
	return lines.join('\n');
}

/**
 * @param {string} content
 * @param {string} key
 * @param {string} value
 */
function applyPostgreSQL(content, key, value) {
	const lines = content.split('\n');
	const active = new RegExp(`^\\s*${key}\\s*=`);
	let changed = false;
	for (let index = 0; index < lines.length; index++) {
		if (!active.test(lines[index])) continue;
		if (value === '') {
			lines.splice(index, 1);
			index--;
		} else {
			lines[index] = `${key} = ${value}`;
		}
		changed = true;
	}
	if (!changed && value !== '') {
		const commentedDefault = new RegExp(`^\\s*#\\s*${key}\\s*=`);
		const template = lines.findIndex((line) => commentedDefault.test(line));
		if (template !== -1) lines.splice(template + 1, 0, `${key} = ${value}`);
		else lines.push(`${key} = ${value}`);
	}
	return lines.join('\n');
}

/**
 * Produce the full config content from form values, touching only the
 * directives whose value changed since the content was loaded.
 * @param {string} content
 * @param {string} engine
 * @param {Record<string,string>} baseline values as parsed from content
 * @param {Record<string,string>} values current form values
 */
export function buildConfigFromForm(content, engine, baseline, values) {
	let next = content;
	for (const key of Object.keys(dbConfigFields[engine])) {
		if ((baseline[key] ?? '') !== (values[key] ?? '')) {
			next = applyDbConfig(next, engine, key, values[key] ?? '');
		}
	}
	return next;
}
