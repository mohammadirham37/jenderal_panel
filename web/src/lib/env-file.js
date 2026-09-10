/**
 * Parse a raw .env file into its KEY=VALUE entries. Lines that are comments,
 * blank, or not simple assignments are ignored (they round-trip unchanged).
 *
 * @param {string} raw
 * @returns {{ key: string, value: string, line: number }[]}
 */
export function parseEnvFile(raw) {
	/** @type {{ key: string, value: string, line: number }[]} */
	const entries = [];
	raw.split('\n').forEach((line, index) => {
		const match = line.match(/^\s*([A-Za-z_][A-Za-z0-9_]*)=(.*)$/);
		if (match) entries.push({ key: match[1], value: match[2], line: index });
	});
	return entries;
}

/**
 * Replace the values of the given keys in a raw .env file. Only the first
 * occurrence of each key is updated; all other lines are preserved as-is.
 *
 * @param {string} raw
 * @param {Record<string, string>} values keyed by variable name
 * @returns {string}
 */
export function applyEnvValues(raw, values) {
	const lines = raw.split('\n');
	for (let i = 0; i < lines.length; i++) {
		const match = lines[i].match(/^\s*([A-Za-z_][A-Za-z0-9_]*)=(.*)$/);
		if (!match) continue;
		if (Object.prototype.hasOwnProperty.call(values, match[1])) {
			const separator = lines[i].indexOf('=') + 1;
			lines[i] = lines[i].slice(0, separator) + values[match[1]];
		}
	}
	return lines.join('\n');
}
