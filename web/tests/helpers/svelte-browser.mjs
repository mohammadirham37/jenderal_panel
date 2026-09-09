import { register } from 'node:module';
import { Window } from 'happy-dom';

// Compile the real components and use Svelte's browser runtime. Only SvelteKit's
// route state is supplied by the harness; page logic and API calls stay intact.
register('./svelte-browser-hooks.mjs', import.meta.url);

const window = new Window({ url: 'http://localhost' });
Object.defineProperty(globalThis, 'navigator', { configurable: true, value: window.navigator });
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
