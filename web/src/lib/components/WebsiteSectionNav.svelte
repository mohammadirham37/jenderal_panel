<script lang="ts">
	import { websiteSectionLinks } from '$lib/website-sections.js';
	import { language, translate } from '$lib/stores/language';

	let {
		websiteId,
		currentPath
	}: {
		websiteId: string;
		currentPath: string;
	} = $props();

	const links = $derived(websiteSectionLinks(websiteId));

	// Display labels from website-sections.js mapped to dictionary keys.
	const labelKeys: Record<string, string> = {
		Overview: 'wsnav.overview',
		Deployments: 'wsnav.deployments',
		SSL: 'wsnav.ssl',
		'Cron Jobs': 'wsnav.cron',
		'Queue Workers': 'wsnav.queue'
	};
</script>

<nav aria-label={translate($language, 'wsnav.aria')} class="flex flex-wrap gap-2 border-b border-gray-700 pb-3">
	{#each links as link}
		<a
			href={link.href}
			aria-current={currentPath === link.href ? 'page' : undefined}
			class="rounded-lg px-3 py-2 text-sm transition-colors {currentPath === link.href
				? 'bg-blue-500/15 text-blue-100'
				: 'text-gray-400 hover:bg-white/5 hover:text-gray-100'}"
		>
			{translate($language, labelKeys[link.label] ?? link.label)}
		</a>
	{/each}
</nav>
