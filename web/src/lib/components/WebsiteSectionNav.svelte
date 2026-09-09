<script lang="ts">
	import { websiteSectionLinks } from '$lib/website-sections.js';

	let {
		websiteId,
		currentPath
	}: {
		websiteId: string;
		currentPath: string;
	} = $props();

	const links = $derived(websiteSectionLinks(websiteId));
</script>

<nav aria-label="Website sections" class="flex flex-wrap gap-2 border-b border-gray-700 pb-3">
	{#each links as link}
		<a
			href={link.href}
			aria-current={currentPath === link.href ? 'page' : undefined}
			class="rounded-lg px-3 py-2 text-sm transition-colors {currentPath === link.href
				? 'bg-blue-500/15 text-blue-100'
				: 'text-gray-400 hover:bg-white/5 hover:text-gray-100'}"
		>
			{link.label}
		</a>
	{/each}
</nav>
