import { readFileSync } from 'node:fs';
import { registerHooks } from 'node:module';
import { fileURLToPath } from 'node:url';
import { compile, compileModule } from 'svelte/compiler';
import ts from 'typescript';
import { Window } from 'happy-dom';

// Compile the real components and use Svelte's browser runtime. Only SvelteKit's
// route state is supplied by the harness; page logic and API calls stay intact.
const appStateURL = new URL('./route-state.svelte.js', import.meta.url).href;
registerHooks({
	resolve(specifier, context, nextResolve) {
		if (specifier === '$app/state') return { url: appStateURL, shortCircuit: true };
		if (specifier.startsWith('$lib/')) {
			let path = specifier.slice(5);
			if (!/\.(js|ts|svelte)$/.test(path)) path += '.ts';
			return { url: new URL(`../../src/lib/${path}`, import.meta.url).href, shortCircuit: true };
		}
		return nextResolve(specifier, { ...context, conditions: [...context.conditions, 'browser'] });
	},
	load(url, context, nextLoad) {
		if (url === appStateURL) {
			return {
				format: 'module', shortCircuit: true,
				source: compileModule(`export const page = $state({ params: { id: '' }, url: { pathname: '' } });`, {
					filename: fileURLToPath(url), generate: 'client'
				}).js.code
			};
		}
		if (url.endsWith('.svelte')) {
			return {
				format: 'module', shortCircuit: true,
				source: compile(readFileSync(new URL(url), 'utf8'), {
					filename: fileURLToPath(url), generate: 'client'
				}).js.code
			};
		}
		if (url.endsWith('.ts')) {
			return {
				format: 'module', shortCircuit: true,
				source: ts.transpileModule(readFileSync(new URL(url), 'utf8'), {
					compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.ESNext }
				}).outputText
			};
		}
		return nextLoad(url, context);
	}
});

const window = new Window({ url: 'http://localhost' });
for (const name of ['window', 'document', 'Node', 'Element', 'HTMLElement', 'HTMLInputElement', 'HTMLSelectElement', 'HTMLTextAreaElement', 'Text', 'Comment', 'Event', 'MouseEvent']) {
	globalThis[name] = name === 'window' ? window : window[name];
}

export const { page } = await import('$app/state');
export const { mount, unmount, flushSync } = await import('svelte');

export async function settle() {
	// Let fetch/JSON promises and scheduled Svelte effects drain, without sleeps.
	await new Promise(setImmediate);
	flushSync();
}
