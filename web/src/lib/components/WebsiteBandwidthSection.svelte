<script lang="ts">
	import { api } from '$lib/api';
	import { websiteOperationAPI } from '$lib/website-operations.js';
	import { language, translate } from '$lib/stores/language';

	interface BandwidthMonth {
		month: string; // 'YYYY-MM' (UTC)
		bytes: number;
		requests: number;
	}

	let { websiteId }: { websiteId: string } = $props();

	let months = $state<BandwidthMonth[]>([]);
	let loading = $state(true);
	let error = $state('');

	$effect(() => {
		const id = websiteId;
		if (!id) return;
		loading = true;
		error = '';
		months = [];
		(async () => {
			try {
				months = (await api.get<BandwidthMonth[]>(websiteOperationAPI(id).bandwidth)) || [];
			} catch (err) {
				error = err instanceof Error ? err.message : translate($language, 'wsbw.error.load_bandwidth');
			} finally {
				loading = false;
			}
		})();
	});

	// Locale-aware formatting follows the panel language (en / id).
	let numberLocale = $derived($language === 'id' ? 'id-ID' : 'en-US');

	function monthLabel(month: string, locale: string): string {
		try {
			return new Date(`${month}-01T00:00:00Z`).toLocaleDateString(locale, {
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

	function formatCount(count: number, locale: string): string {
		return new Intl.NumberFormat(locale).format(count || 0);
	}

	let totalBytes = $derived(months.reduce((sum, m) => sum + (m.bytes || 0), 0));
	let totalRequests = $derived(months.reduce((sum, m) => sum + (m.requests || 0), 0));
	let maxBytes = $derived(months.reduce((max, m) => Math.max(max, m.bytes || 0), 0));
	let latestMonth = $derived(months.length > 0 ? months[months.length - 1] : null);
	let avgBytesPerRequest = $derived(totalRequests > 0 ? totalBytes / totalRequests : 0);
	let trackedMonths = $derived(months.length);

	function sharePercent(bytes: number): number {
		if (totalBytes <= 0) return 0;
		return Math.max(Math.round((bytes / totalBytes) * 100), 1);
	}

	function barPercent(bytes: number): number {
		if (maxBytes <= 0) return 0;
		return Math.max((bytes / maxBytes) * 100, 2);
	}
</script>

{#if loading}
	<div class="space-y-4">
		<div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
			<div class="h-[104px] animate-pulse rounded-2xl border border-gray-700/60 bg-gray-800/60"></div>
			<div class="h-[104px] animate-pulse rounded-2xl border border-gray-700/60 bg-gray-800/60"></div>
		</div>
		<div class="h-64 animate-pulse rounded-2xl border border-gray-700/60 bg-gray-800/60"></div>
	</div>
{:else if error}
	<div class="rounded-xl border border-red-700 bg-red-900/30 p-4 text-sm text-red-300">{error}</div>
{:else if months.length === 0}
	<div class="rounded-2xl border border-gray-700 bg-gray-800 p-10 text-center">
		<div class="mx-auto flex h-12 w-12 items-center justify-center rounded-full bg-blue-500/10 text-blue-400">
			<svg class="h-6 w-6" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="1.5" aria-hidden="true">
				<path stroke-linecap="round" stroke-linejoin="round" d="M3 13.125C3 12.504 3.504 12 4.125 12h2.25c.621 0 1.125.504 1.125 1.125v6.75C7.5 20.496 6.996 21 6.375 21h-2.25A1.125 1.125 0 0 1 3 19.875v-6.75ZM9.75 8.625c0-.621.504-1.125 1.125-1.125h2.25c.621 0 1.125.504 1.125 1.125v11.25c0 .621-.504 1.125-1.125 1.125h-2.25a1.125 1.125 0 0 1-1.125-1.125V8.625ZM16.5 4.125c0-.621.504-1.125 1.125-1.125h2.25C20.496 3 21 3.504 21 4.125v15.75c0 .621-.504 1.125-1.125 1.125h-2.25a1.125 1.125 0 0 1-1.125-1.125V4.125Z" />
			</svg>
		</div>
		<p class="mt-3 text-sm font-medium text-gray-300">{translate($language, 'wsbw.empty')}</p>
		<p class="mx-auto mt-1 max-w-md text-xs text-gray-500">{translate($language, 'wsbw.empty_hint')}</p>
	</div>
{:else}
	<!-- Totals -->
	<div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
		<div class="rounded-2xl border border-gray-700 bg-gray-800 p-5">
			<div class="flex items-start justify-between gap-3">
				<div class="min-w-0">
					<p class="text-xs uppercase tracking-wider text-gray-500">{translate($language, 'wsbw.total_bandwidth')}</p>
					<p class="mt-1.5 text-2xl font-bold tabular-nums text-white">{formatBytes(totalBytes)}</p>
					<p class="mt-1 truncate text-[11px] text-gray-500">
						{translate($language, 'wsbw.months_tracked').replace('{n}', String(trackedMonths))}
					</p>
				</div>
				<div class="shrink-0 rounded-xl bg-blue-500/10 p-2.5 text-blue-400">
					<svg class="h-5 w-5" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="1.5" aria-hidden="true">
						<path stroke-linecap="round" stroke-linejoin="round" d="M12 19.5v-15m0 0-6.75 6.75M12 4.5l6.75 6.75" />
					</svg>
				</div>
			</div>
		</div>
		<div class="rounded-2xl border border-gray-700 bg-gray-800 p-5">
			<div class="flex items-start justify-between gap-3">
				<div class="min-w-0">
					<p class="text-xs uppercase tracking-wider text-gray-500">{translate($language, 'wsbw.total_requests')}</p>
					<p class="mt-1.5 text-2xl font-bold tabular-nums text-white">{formatCount(totalRequests, numberLocale)}</p>
					<p class="mt-1 truncate text-[11px] text-gray-500">
						{avgBytesPerRequest > 0
							? translate($language, 'wsbw.avg_per_request').replace('{size}', formatBytes(avgBytesPerRequest))
							: translate($language, 'wsbw.months_tracked').replace('{n}', String(trackedMonths))}
					</p>
				</div>
				<div class="shrink-0 rounded-xl bg-blue-500/10 p-2.5 text-blue-400">
					<svg class="h-5 w-5" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="1.5" aria-hidden="true">
						<path stroke-linecap="round" stroke-linejoin="round" d="M7.5 3v13.5m0 0 3-3m-3 3-3-3M16.5 21V7.5m0 0 3 3m-3-3-3 3" />
					</svg>
				</div>
			</div>
		</div>
	</div>

	<!-- Monthly trend chart -->
	<div class="rounded-2xl border border-gray-700 bg-gray-800 p-5">
		<div class="flex flex-wrap items-center justify-between gap-2">
			<h3 class="text-sm font-semibold text-white">{translate($language, 'wsbw.trend_title')}</h3>
			<p class="text-[11px] text-gray-500">
				{translate($language, 'wsbw.peak_month')}
				<span class="ml-1 font-medium tabular-nums text-gray-400">{formatBytes(maxBytes)}</span>
			</p>
		</div>
		<div class="mt-4 flex items-end gap-2 sm:gap-3">
			{#each months as m, i (m.month)}
				<div class="group flex min-w-0 flex-1 flex-col items-center gap-1.5">
					<span class="max-w-full truncate text-[10px] font-medium tabular-nums text-gray-500 transition group-hover:text-gray-300">
						{m.bytes > 0 ? formatBytes(m.bytes) : '–'}
					</span>
					<div class="flex h-32 w-full items-end justify-center sm:h-40">
						<div
							class="w-3/5 max-w-16 rounded-t-md bg-gradient-to-t transition-all duration-500
							{i === months.length - 1 ? 'from-blue-500 to-blue-400' : 'from-blue-600/50 to-blue-400/50'}
							group-hover:from-blue-500 group-hover:to-blue-400"
							style="height: {m.bytes > 0 ? barPercent(m.bytes) : 0}%"
							title="{monthLabel(m.month, numberLocale)} · {formatBytes(m.bytes)} · {formatCount(m.requests, numberLocale)} {translate($language, 'wsbw.col_requests').toLowerCase()}"
						></div>
					</div>
					<span class="max-w-full truncate text-[10px] text-gray-500" class:text-gray-300={i === months.length - 1}>
						{monthLabel(m.month, numberLocale)}
					</span>
				</div>
			{/each}
		</div>
	</div>

	<!-- Monthly breakdown -->
	<div class="rounded-2xl border border-gray-700 bg-gray-800 p-5">
		<h3 class="text-sm font-semibold text-white">{translate($language, 'wsbw.monthly_title')}</h3>
		<div class="mt-3 overflow-x-auto">
			<table class="w-full min-w-[28rem] text-sm">
				<thead>
					<tr class="border-b border-gray-700 text-left text-xs uppercase tracking-wider text-gray-500">
						<th class="py-2 pr-3 font-semibold">{translate($language, 'wsbw.col_month')}</th>
						<th class="py-2 pr-3 text-right font-semibold">{translate($language, 'wsbw.col_requests')}</th>
						<th class="py-2 pr-3 text-right font-semibold">{translate($language, 'wsbw.col_bandwidth')}</th>
						<th class="hidden py-2 text-right font-semibold sm:table-cell">{translate($language, 'wsbw.col_share')}</th>
					</tr>
				</thead>
				<tbody class="divide-y divide-gray-700/40">
					{#each [...months].reverse() as m, i (m.month)}
						<tr class="transition hover:bg-gray-700/20">
							<td class="py-2.5 pr-3">
								<span class="inline-flex items-center gap-1.5 font-medium text-gray-200">
									{#if i === 0}
										<span
											class="h-1.5 w-1.5 rounded-full bg-blue-400"
											title={translate($language, 'wsbw.latest')}
										></span>
									{/if}
									{monthLabel(m.month, numberLocale)}
								</span>
							</td>
							<td class="py-2.5 pr-3 text-right tabular-nums text-gray-300">{formatCount(m.requests, numberLocale)}</td>
							<td class="py-2.5 pr-3 text-right font-medium tabular-nums text-gray-100">{formatBytes(m.bytes)}</td>
							<td class="hidden py-2.5 sm:table-cell">
								<div class="flex items-center justify-end gap-2">
									<div class="h-1.5 w-24 overflow-hidden rounded-full bg-gray-700/60">
										<div
											class="h-full rounded-full {i === 0 ? 'bg-blue-400' : 'bg-blue-500/60'}"
											style="width: {sharePercent(m.bytes)}%"
										></div>
									</div>
									<span class="w-9 text-right text-[11px] tabular-nums text-gray-500">{sharePercent(m.bytes)}%</span>
								</div>
							</td>
						</tr>
					{/each}
				</tbody>
				<tfoot>
					<tr class="border-t border-gray-700 text-xs text-gray-400">
						<td class="py-2.5 pr-3 font-semibold">{translate($language, 'wsbw.total')}</td>
						<td class="py-2.5 pr-3 text-right tabular-nums">{formatCount(totalRequests, numberLocale)}</td>
						<td class="py-2.5 pr-3 text-right font-semibold tabular-nums">{formatBytes(totalBytes)}</td>
						<td class="hidden py-2.5 text-right sm:table-cell">
							<span class="text-[11px] tabular-nums">{totalBytes > 0 ? '100%' : '–'}</span>
						</td>
					</tr>
				</tfoot>
			</table>
		</div>
	</div>
{/if}
