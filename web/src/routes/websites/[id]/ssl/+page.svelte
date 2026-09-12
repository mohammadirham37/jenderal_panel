<script lang="ts">
	import { page } from '$app/state';
	import { api } from '$lib/api';
	import WebsiteSectionNav from '$lib/components/WebsiteSectionNav.svelte';
	import WebsiteSslSection from '$lib/components/WebsiteSslSection.svelte';
	import { websiteOperationAPI } from '$lib/website-operations.js';
	import { language, translate } from '$lib/stores/language';

	interface Website {
		id: string;
		domain: string;
		app_type: string;
		status: string;
		domains?: { name: string; type: string }[];
	}

	let websiteID = $derived(page.params.id ?? '');
	let website = $state<Website | null>(null);
	let loadingWebsite = $state(true);
	let websiteError = $state('');

	async function loadWebsite(requestedWebsiteID: string) {
		loadingWebsite = true;
		websiteError = '';
		website = null;
		if (!requestedWebsiteID) {
			websiteError = translate($language, 'wsslp.error.website_id_required');
			loadingWebsite = false;
			return;
		}
		const scopedAPI = websiteOperationAPI(requestedWebsiteID);
		try {
			website = await api.get<Website>(scopedAPI.website);
		} catch (err) {
			websiteError = err instanceof Error ? err.message : translate($language, 'wsslp.error.load_website');
		} finally {
			loadingWebsite = false;
		}
	}

	$effect(() => {
		void loadWebsite(websiteID);
	});
</script>

<div class="space-y-6">
	{#if loadingWebsite}
		<div class="text-gray-400">{translate($language, 'wsslp.loading_website')}</div>
	{:else if websiteError}
		<div class="p-4 bg-red-900/50 border border-red-700 rounded-lg text-red-300">{websiteError}</div>
	{:else if website}
	<div class="flex items-center justify-between">
		<div>
			<a href="/websites" class="text-sm text-blue-400 hover:text-blue-300">{translate($language, 'wsslp.back_to_websites')}</a>
			<div class="flex flex-wrap items-center gap-3">
				<h2 class="text-2xl font-bold text-white">{translate($language, 'wsslp.title').replace('{domain}', website.domain)}</h2>
				<span aria-label={translate($language, 'wsslp.website_status')} class="inline-block px-2.5 py-0.5 rounded bg-gray-700 text-xs font-medium text-gray-300">{website.status}</span>
			</div>
		</div>
	</div>

	<WebsiteSectionNav websiteId={website.id} currentPath={page.url.pathname} />

	<WebsiteSslSection {website} />
	{/if}
</div>
