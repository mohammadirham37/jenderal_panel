/** @type {Record<string, {context: 'main'|'events'|'http', pattern: RegExp, fallback: string}>} */
const fieldDefinitions = {
	worker_processes: { context: 'main', pattern: /^(auto|[1-9]\d*)$/, fallback: 'auto' },
	worker_connections: { context: 'events', pattern: /^[1-9]\d*$/, fallback: '768' },
	client_max_body_size: { context: 'http', pattern: /^\d+[kKmMgG]?$/, fallback: '64M' },
	keepalive_timeout: { context: 'http', pattern: /^\d+$/, fallback: '65' },
	server_tokens: { context: 'http', pattern: /^(on|off)$/, fallback: 'off' },
	gzip: { context: 'http', pattern: /^(on|off)$/, fallback: 'on' }
};

/** @param {string} content */
function sanitize(content) {
	let result = '';
	let quote = '';
	let comment = false;
	let escaped = false;
	for (const char of content) {
		if (comment) {
			if (char === '\n') {
				comment = false;
				result += char;
			} else result += ' ';
			continue;
		}
		if (escaped) {
			escaped = false;
			result += ' ';
			continue;
		}
		if (quote) {
			if (char === '\\') escaped = true;
			else if (char === quote) quote = '';
			result += ' ';
			continue;
		}
		if (char === '"' || char === "'") {
			quote = char;
			result += ' ';
		} else if (char === '#') {
			comment = true;
			result += ' ';
		} else result += char;
	}
	return result;
}

/** @param {string} content */
function structure(content) {
	const clean = sanitize(content);
	const depth = new Array(content.length + 1).fill(0);
	/** @type {Partial<Record<'events'|'http', {open: number, close: number, depth: number}>>} */
	const blocks = {};
	/** @type {{name: string, open: number, depth: number}[]} */
	const stack = [];
	let currentDepth = 0;
	for (let index = 0; index < clean.length; index++) {
		depth[index] = currentDepth;
		if (clean[index] === '{') {
			const prefix = clean.slice(0, index);
			const match = prefix.match(/([A-Za-z_][A-Za-z0-9_]*)\s*$/);
			const name = match?.[1] || '';
			stack.push({ name, open: index, depth: currentDepth });
			currentDepth++;
		} else if (clean[index] === '}') {
			currentDepth = Math.max(0, currentDepth - 1);
			const block = stack.pop();
			if (block && block.depth === 0 && (block.name === 'events' || block.name === 'http') && !blocks[block.name]) {
				blocks[block.name] = { open: block.open, close: index, depth: 1 };
			}
		}
	}
	depth[content.length] = currentDepth;
	return { clean, depth, blocks };
}

/**
 * @param {string} content
 * @param {string} key
 * @param {'main'|'events'|'http'} context
 * @param {ReturnType<typeof structure>} parsed
 */
function locateDirective(content, key, context, parsed) {
	const range = context === 'main'
		? { open: -1, close: content.length, depth: 0 }
		: parsed.blocks[context];
	if (!range) return null;
	const pattern = new RegExp(`(^|[\\n{;])([ \\t]*)${key}\\s+([^;\\n]+);`, 'g');
	let match;
	while ((match = pattern.exec(parsed.clean)) !== null) {
		const start = match.index + match[1].length;
		if (start > range.open && start < range.close && parsed.depth[start] === range.depth) {
			const original = content.slice(start, start + match[0].length - match[1].length);
			const value = original.match(new RegExp(`^[ \\t]*${key}\\s+([^;\\n]+);`))?.[1]?.trim() || '';
			return { start, end: start + original.length, indent: match[2], value };
		}
	}
	return null;
}

/** @param {string} content @returns {{values: Record<string,string>, errors: string[]}} */
export function parseSimpleNginxConfig(content) {
	const parsed = structure(content);
	/** @type {Record<string,string>} */
	const values = {};
	/** @type {string[]} */
	const errors = [];
	for (const [key, definition] of Object.entries(fieldDefinitions)) {
		if (definition.context !== 'main' && !parsed.blocks[definition.context]) {
			if (!errors.includes(`Missing ${definition.context} block.`)) errors.push(`Missing ${definition.context} block.`);
			values[key] = definition.fallback;
			continue;
		}
		const located = locateDirective(content, key, definition.context, parsed);
		values[key] = located?.value || definition.fallback;
		if (located && !definition.pattern.test(located.value)) errors.push(`Invalid ${key} value.`);
	}
	return { values, errors };
}

/** @param {string} content @param {string} key @param {string} value */
export function updateSimpleNginxConfig(content, key, value) {
	const definition = fieldDefinitions[key];
	if (!definition) throw new Error(`Unsupported Nginx directive: ${key}`);
	const normalized = String(value).trim();
	if (!definition.pattern.test(normalized)) throw new Error(`Invalid ${key} value.`);

	const parsed = structure(content);
	const block = definition.context === 'main' ? null : parsed.blocks[definition.context];
	if (definition.context !== 'main' && !block) throw new Error(`Missing ${definition.context} block.`);
	const existing = locateDirective(content, key, definition.context, parsed);
	if (existing) {
		return content.slice(0, existing.start) + `${existing.indent}${key} ${normalized};` + content.slice(existing.end);
	}
	if (definition.context === 'main') return `${key} ${normalized};\n${content}`;
	if (!block) throw new Error(`Missing ${definition.context} block.`);

	const afterBrace = block.open + 1;
	const lineStart = content.lastIndexOf('\n', block.open) + 1;
	const blockIndent = content.slice(lineStart, block.open).match(/^\s*/)?.[0] || '';
	const indent = `${blockIndent}    `;
	return content.slice(0, afterBrace) + `\n${indent}${key} ${normalized};` + content.slice(afterBrace);
}

/**
 * @param {{mode: 'simple'|'manual', draft: string}} state
 * @param {'simple'|'manual'} nextMode
 */
export function switchNginxConfigMode(state, nextMode) {
	if (state.mode === nextMode) return state;
	return { ...state, mode: nextMode };
}
