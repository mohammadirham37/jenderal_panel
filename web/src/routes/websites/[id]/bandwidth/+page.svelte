<script lang="ts">
	import { page } from '$app/state';
	import { api } from '$lib/api';
	import WebsiteSectionNav from '$lib/components/WebsiteSectionNav.svelte';
	import WebsiteBandwidthSection from '$lib/components/WebsiteBandwidthSection.svelte';
	import { websiteOperationAPI } from '$lib/website-operations.js';
	import { language, translate } from '$lib/stores/language';

	interface Website {
		id: string;
		domain: string;
		app_type: string;
		status: string;
	}

	let websiteID = $derived(page.params.id ?? '');
	let website = $state<Website | null>(null);
	let loadingWebsite = $state(true);
	let websiteError = $state('');

	$effect(() => {
		const id = websiteID;
		if (!id) return;
		loadingWebsite = true;
		websiteError = '';
		(async () => {
			try {
				website = await api.get<Website>(websiteOperationAPI(id).website);
			} catch (err) {
				websiteError = err instanceof Error ? err.message : translate($language, 'wsbw.error.load_website');
			} finally {
				loadingWebsite = false;
			}
		})();
	});
</script>

<div class="space-y-5">
	{#if loadingWebsite}
		<p class="text-sm text-gray-400">{translate($language, 'wsbw.loading')}</p>
	{:else if websiteError}
		<div class="rounded-xl border border-red-700 bg-red-900/30 p-4 text-sm text-red-300">{websiteError}</div>
		<a href="/websites" class="inline-block text-xs text-gray-500 transition hover:text-gray-300">{translate($language, 'wssub.back_to_websites')}</a>
	{:else if website}
		<div class="flex flex-wrap items-start justify-between gap-3">
			<div class="min-w-0">
				<h2 class="truncate text-2xl font-bold text-white">{website.domain}</h2>
				<p class="mt-0.5 text-sm text-gray-400">{translate($language, 'wsbw.subtitle')}</p>
			</div>
			<span class="inline-flex shrink-0 items-center gap-1.5 rounded-full px-2.5 py-0.5 text-xs font-medium {website.status === 'active' ? 'bg-green-900/40 text-green-400' : 'bg-gray-700 text-gray-300'}">
				{website.status}
			</span>
		</div>

		<WebsiteSectionNav websiteId={website.id} currentPath={page.url.pathname} />

		<WebsiteBandwidthSection websiteId={website.id} />
	{/if}
</div>
