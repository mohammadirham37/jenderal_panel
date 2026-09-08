const simpleDirectives = [
	'memory_limit',
	'upload_max_filesize',
	'post_max_size',
	'max_execution_time',
	'max_input_time',
	'max_input_vars',
	'display_errors'
];

/** @type {Record<string, string>} */
const simpleDefaults = {
	memory_limit: '128M',
	upload_max_filesize: '2M',
	post_max_size: '8M',
	max_execution_time: '30',
	max_input_time: '60',
	max_input_vars: '1000',
	display_errors: 'Off'
};

/**
 * Read the commonly edited directives from php.ini.
 *
 * @param {string} content
 * @returns {Record<string, string>}
 */
export function parseSimplePHPConfig(content) {
	const values = { ...simpleDefaults };
	const supported = new Set(simpleDirectives);

	for (const line of content.split(/\r?\n/)) {
		const match = line.match(/^\s*([A-Za-z0-9_.]+)\s*=\s*(.*?)\s*$/);
		if (match && supported.has(match[1])) {
			values[match[1]] = match[2];
		}
	}

	return values;
}

/**
 * Update only the simple-mode directives and leave the rest of php.ini intact.
 * Missing directives are appended so the saved values are explicit.
 *
 * @param {string} content
 * @param {Record<string, string>} values
 * @returns {string}
 */
export function updateSimplePHPConfig(content, values) {
	let updated = content;
	const missing = [];

	for (const directive of simpleDirectives) {
		const pattern = new RegExp(`^\\s*${directive}\\s*=.*$`, 'gm');
		const replacement = `${directive} = ${values[directive]}`;
		if (pattern.test(updated)) {
			updated = updated.replace(pattern, replacement);
		} else {
			missing.push(replacement);
		}
	}

	if (missing.length > 0) {
		if (updated.length > 0 && !updated.endsWith('\n')) updated += '\n';
		updated += missing.join('\n') + '\n';
	}

	return updated;
}

/**
 * Move between the Simple and Advanced config views without losing an
 * unsaved draft. Selecting the active mode is intentionally a no-op.
 *
 * @param {{ mode: 'simple' | 'advanced', content: string, simpleConfig: Record<string, string> }} state
 * @param {'simple' | 'advanced'} targetMode
 * @returns {{ mode: 'simple' | 'advanced', content: string, simpleConfig: Record<string, string> }}
 */
export function switchPHPConfigMode(state, targetMode) {
	if (state.mode === targetMode) return state;

	if (targetMode === 'advanced') {
		return {
			...state,
			mode: targetMode,
			content: updateSimplePHPConfig(state.content, state.simpleConfig)
		};
	}

	return {
		...state,
		mode: targetMode,
		simpleConfig: parseSimplePHPConfig(state.content)
	};
}
