import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { compile, compileModule } from 'svelte/compiler';
import ts from 'typescript';

// These module hooks run in Node's loader thread, separately from the test DOM.
const appStateURL = new URL('./route-state.svelte.js', import.meta.url).href;

export function resolve(specifier, context, nextResolve) {
	if (specifier === '$app/state') return { url: appStateURL, shortCircuit: true };
	if (specifier.startsWith('$lib/')) {
		let path = specifier.slice(5);
		if (!/\.(js|ts|svelte)$/.test(path)) path += '.ts';
		return { url: new URL(`../../src/lib/${path}`, import.meta.url).href, shortCircuit: true };
	}
	return nextResolve(specifier, { ...context, conditions: [...context.conditions, 'browser'] });
}

export function load(url, context, nextLoad) {
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
