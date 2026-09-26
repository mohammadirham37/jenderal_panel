<script lang="ts">
	import { page } from '$app/state';
	import { api } from '$lib/api';
	import WebsiteSectionNav from '$lib/components/WebsiteSectionNav.svelte';
	import { websiteOperationAPI } from '$lib/website-operations.js';
	import { language, translate } from '$lib/stores/language';

	interface BandwidthMonth {
		month: string; // 'YYYY-MM' (UTC)
		bytes: number;
		requests: number;
	}

	interface Website {
		id: string;
		domain: string;
		app_type: string;
		status: string;
	}

	let websiteID = $derived(page.params.id ?? '');
	let operationAPI = $derived(websiteOperationAPI(websiteID));
	let website = $state<Website | null>(null);
	let months = $state<BandwidthMonth[]>([]);
	let loadingWebsite = $state(true);
	let loadingBandwidth = $state(true);
	let websiteError = $state('');
	let error = $state('');

	async function loadWebsite(requestedWebsiteID: string) {
		loadingWebsite = true;
		websiteError = '';
		if (!requestedWebsiteID) {
			websiteError = translate($language, 'wsbw.error.website_id_required');
			loadingWebsite = false;
			return;
		}
		try {
			website = await api.get<Website>(websiteOperationAPI(requestedWebsiteID).website);
		} catch (err) {
			websiteError = err instanceof Error ? err.message : translate($language, 'wsbw.error.load_website');
		} finally {
			loadingWebsite = false;
		}
	}

	async function loadBandwidth(requestedWebsiteID: string) {
		loadingBandwidth = true;
		error = '';
		months = [];
		if (!requestedWebsiteID) {
			error = translate($language, 'wsbw.error.website_id_required');
			loadingBandwidth = false;
			return;
		}
		try {
			months = (await api.get<BandwidthMonth[]>(operationAPI.bandwidth)) || [];
		} catch (err) {
			error = err instanceof Error ? err.message : translate($language, 'wsbw.error.load_bandwidth');
		} finally {
			loadingBandwidth = false;
		}
	}

	$effect(() => {
		void loadWebsite(websiteID);
		void loadBandwidth(websiteID);
	});

	function monthLabel(month: string): string {
		try {
			return new Date(`${month}-01T00:00:00Z`).toLocaleDateString('en-US', {
				month: 'short',
				year: 'numeric',
				timeZone: 'UTC'
			});
		} catch {
			return month;
		}
	}

	function formatBytes(bytes: number): string {
		if (!bytes || bytes <= 0) return '0 B';
		const units = ['B', 'KB', 'MB', 'GB', 'TB'];
		let i = 0;
		let size = bytes;
		while (size >= 1024 && i < units.length - 1) { size /= 1024; i++; }
		return `${size.toFixed(i === 0 ? 0 : 1)} ${units[i]}`;
	}

	function formatCount(count: number): string {
		return new Intl.NumberFormat('en-US').format(count || 0);
	}

	let totalBytes = $derived(months.reduce((sum, m) => sum + (m.bytes || 0), 0));
	let totalRequests = $derived(months.reduce((sum, m) => sum + (m.requests || 0), 0));
	let maxBytes = $derived(months.reduce((max, m) => Math.max(max, m.bytes || 0), 0));
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

		{#if loadingBandwidth}
			<div class="space-y-2 rounded-2xl border border-gray-700 bg-gray-800 p-4">
				{#each Array(5) as _}
					<div class="h-6 animate-pulse rounded bg-gray-700/50"></div>
				{/each}
			</div>
		{:else if error}
			<div class="rounded-xl border border-red-700 bg-red-900/30 p-4 text-sm text-red-300">{error}</div>
		{:else if months.length === 0}
			<div class="rounded-2xl border border-gray-700 bg-gray-800 p-8 text-center">
				<p class="text-sm text-gray-400">{translate($language, 'wsbw.empty')}</p>
				<p class="mt-1 text-xs text-gray-500">{translate($language, 'wsbw.empty_hint')}</p>
			</div>
		{:else}
			<!-- Totals -->
			<div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
				<div class="rounded-2xl border border-gray-700 bg-gray-800 p-5">
					<p class="text-xs uppercase tracking-wider text-gray-500">{translate($language, 'wsbw.total_bandwidth')}</p>
					<p class="mt-1 text-2xl font-bold text-white">{formatBytes(totalBytes)}</p>
				</div>
				<div class="rounded-2xl border border-gray-700 bg-gray-800 p-5">
					<p class="text-xs uppercase tracking-wider text-gray-500">{translate($language, 'wsbw.total_requests')}</p>
					<p class="mt-1 text-2xl font-bold text-white">{formatCount(totalRequests)}</p>
				</div>
			</div>

			<!-- Monthly breakdown -->
			<div class="rounded-2xl border border-gray-700 bg-gray-800 p-5">
				<h3 class="text-sm font-semibold text-white">{translate($language, 'wsbw.monthly_title')}</h3>
				<table class="mt-3 w-full text-sm">
					<thead>
						<tr class="border-b border-gray-700 text-left text-xs uppercase tracking-wider text-gray-500">
							<th class="py-2 pr-3 font-semibold">{translate($language, 'wsbw.col_month')}</th>
							<th class="py-2 pr-3 font-semibold text-right">{translate($language, 'wsbw.col_requests')}</th>
							<th class="py-2 pr-3 font-semibold text-right">{translate($language, 'wsbw.col_bandwidth')}</th>
							<th class="hidden py-2 font-semibold sm:table-cell"><span class="sr-only">Share</span></th>
						</tr>
					</thead>
					<tbody class="divide-y divide-gray-700/40">
						{#each [...months].reverse() as m (m.month)}
							<tr>
								<td class="py-2 pr-3 font-medium text-gray-200">{monthLabel(m.month)}</td>
								<td class="py-2 pr-3 text-right tabular-nums text-gray-300">{formatCount(m.requests)}</td>
								<td class="py-2 pr-3 text-right font-medium tabular-nums text-gray-100">{formatBytes(m.bytes)}</td>
								<td class="hidden py-2 sm:table-cell">
									<div class="h-1.5 w-full max-w-40 overflow-hidden rounded-full bg-gray-700/60 sm:ml-auto">
										<div
											class="h-full rounded-full bg-blue-500"
											style="width: {maxBytes > 0 ? Math.max((m.bytes / maxBytes) * 100, 2) : 0}%"
										></div>
									</div>
								</td>
							</tr>
						{/each}
					</tbody>
					<tfoot>
						<tr class="border-t border-gray-700 text-xs text-gray-400">
							<td class="py-2 pr-3 font-semibold">{translate($language, 'wsbw.total')}</td>
							<td class="py-2 pr-3 text-right tabular-nums">{formatCount(totalRequests)}</td>
							<td class="py-2 pr-3 text-right font-semibold tabular-nums">{formatBytes(totalBytes)}</td>
							<td class="hidden sm:table-cell"></td>
						</tr>
					</tfoot>
				</table>
			</div>
		{/if}
	{/if}
</div>
