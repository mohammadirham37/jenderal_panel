<script lang="ts">
	import { onMount } from 'svelte';
	import { applyTheme, resolveTheme, THEME_STORAGE_KEY } from '$lib/theme.js';

	let theme: 'dark' | 'light' = $state(
		typeof document !== 'undefined' && document.documentElement.dataset.theme === 'light'
			? 'light'
			: 'dark'
	);
	let hasManualTheme = false;

	function getSavedTheme(): string | null {
		try {
			return localStorage.getItem(THEME_STORAGE_KEY);
		} catch {
			return null;
		}
	}

	function setTheme(nextTheme: 'dark' | 'light') {
		theme = nextTheme;
		applyTheme(
			nextTheme,
			document.documentElement,
			document.querySelector<HTMLMetaElement>('meta[name="theme-color"]')
		);
	}

	function toggleTheme() {
		const nextTheme = theme === 'dark' ? 'light' : 'dark';
		hasManualTheme = true;
		try {
			localStorage.setItem(THEME_STORAGE_KEY, nextTheme);
		} catch {
			// The selected theme still applies for this page when storage is unavailable.
		}
		setTheme(nextTheme);
	}

	onMount(() => {
		const systemTheme = window.matchMedia('(prefers-color-scheme: dark)');
		const savedTheme = getSavedTheme();
		hasManualTheme = savedTheme === 'dark' || savedTheme === 'light';
		setTheme(resolveTheme(savedTheme, systemTheme.matches));

		function followSystemTheme(event: MediaQueryListEvent) {
			if (!hasManualTheme) {
				setTheme(event.matches ? 'dark' : 'light');
			}
		}

		systemTheme.addEventListener('change', followSystemTheme);
		return () => systemTheme.removeEventListener('change', followSystemTheme);
	});
</script>

<button
	type="button"
	onclick={toggleTheme}
	class="theme-toggle cursor-pointer rounded-xl border border-white/8 bg-white/[0.035] p-2 text-gray-300 transition hover:border-blue-400/30 hover:bg-blue-500/10 hover:text-white focus:outline-none focus:ring-2 focus:ring-blue-500/50"
	aria-label={theme === 'dark' ? 'Switch to light mode' : 'Switch to dark mode'}
	title={theme === 'dark' ? 'Light mode' : 'Dark mode'}
>
	{#if theme === 'dark'}
		<svg class="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true">
			<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 3v1m0 16v1m9-9h-1M4 12H3m15.36 6.36-.71-.71M6.35 6.35l-.71-.71m12.72 0-.71.71M6.35 17.65l-.71.71M16 12a4 4 0 1 1-8 0 4 4 0 0 1 8 0Z" />
		</svg>
	{:else}
		<svg class="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true">
			<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M21 12.8A9 9 0 1 1 11.2 3 7 7 0 0 0 21 12.8Z" />
		</svg>
	{/if}
</button>
