import adapter from '@sveltejs/adapter-static';
import { vitePreprocess } from '@sveltejs/vite-plugin-svelte';

// Single source of SvelteKit config: the Vite plugin, svelte-check, and
// `svelte-kit sync` all read this file. Do not pass options to sveltekit()
// in vite.config.ts — Kit then ignores this file and warns on every build.
/** @type {import('@sveltejs/kit').Config} */
export default {
	preprocess: vitePreprocess(),
	compilerOptions: {
		// Runes mode for our own components; dependencies keep their own setting.
		runes: ({ filename }) => (filename.split(/[/\\]/).includes('node_modules') ? undefined : true)
	},
	kit: {
		adapter: adapter({
			pages: 'build',
			assets: 'build',
			fallback: 'index.html',
			precompress: false,
			strict: false
		})
	}
};
