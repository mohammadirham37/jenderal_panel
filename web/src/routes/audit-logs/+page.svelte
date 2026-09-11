<script lang="ts">
	import { onMount } from 'svelte';
	import { apiRaw, api } from '$lib/api';
	import type { AuditEntry } from '$lib/types';

	interface AuditModules {
		modules: string[];
	}

	let entries = $state<AuditEntry[]>([]);
	let loading = $state(true);
	let error = $state('');

	let currentPage = $state(1);
	let perPage = $state(25);
	let total = $state(0);

	// Filters. The default range is "today" so the page opens on what just
	// happened instead of the full history.
	let range = $state<'today' | 'yesterday' | '7d' | '30d' | 'all'>('today');
	let fromDate = $state(toDateInput(new Date()));
	let toDate = $state(toDateInput(new Date()));
	let moduleFilter = $state('');
	let search = $state('');
	let searchInput = $state('');

	let modules = $state<string[]>([]);

	let totalPages = $derived(Math.max(1, Math.ceil(total / perPage)));
	let requestSeq = 0;

	function toDateInput(d: Date): string {
		const y = d.getFullYear();
		const m = String(d.getMonth() + 1).padStart(2, '0');
		const day = String(d.getDate()).padStart(2, '0');
		return `${y}-${m}-${day}`;
	}

	function applyRange(r: 'today' | 'yesterday' | '7d' | '30d' | 'all') {
		range = r;
		const today = new Date();
		if (r === 'today') {
			fromDate = toDateInput(today);
			toDate = toDateInput(today);
		} else if (r === 'yesterday') {
			const y = new Date(today);
			y.setDate(y.getDate() - 1);
			fromDate = toDateInput(y);
			toDate = toDateInput(y);
		} else if (r === '7d') {
			const s = new Date(today);
			s.setDate(s.getDate() - 6);
			fromDate = toDateInput(s);
			toDate = toDateInput(today);
		} else if (r === '30d') {
			const s = new Date(today);
			s.setDate(s.getDate() - 29);
			fromDate = toDateInput(s);
			toDate = toDateInput(today);
		} else {
			fromDate = '';
			toDate = '';
		}
		loadLogs(1);
	}

	function onRangeDateChange() {
		// Manual date edits switch to a custom selection.
		range = fromDate || toDate ? 'all' : 'all';
		loadLogs(1);
	}

	let searchTimer: ReturnType<typeof setTimeout> | null = null;
	function onSearchInput() {
		if (searchTimer) clearTimeout(searchTimer);
		searchTimer = setTimeout(() => {
			search = searchInput.trim();
			loadLogs(1);
		}, 350);
	}

	async function loadLogs(page: number) {
		const seq = ++requestSeq;
		loading = true;
		error = '';

		const params = new URLSearchParams();
		params.set('page', String(page));
		params.set('per_page', String(perPage));
		if (moduleFilter) params.set('module', moduleFilter);
		if (fromDate) params.set('from', fromDate);
		if (toDate) params.set('to', toDate);
		if (search) params.set('search', search);

		try {
			const res = await apiRaw<AuditEntry[]>('GET', `/api/v1/audit-logs?${params.toString()}`);
			if (seq !== requestSeq) return;
			entries = res.data || [];
			if (res.meta) {
				currentPage = res.meta.page;
				total = res.meta.total;
			}
		} catch (err) {
			if (seq !== requestSeq) return;
			error = err instanceof Error ? err.message : 'Failed to load audit logs';
		} finally {
			if (seq === requestSeq) loading = false;
		}
	}

	async function loadModules() {
		try {
			modules = (await api.get<AuditModules['modules']>('/api/v1/audit-logs/modules')) || [];
		} catch {
			modules = [];
		}
	}

	function actionBadgeClass(action: string): string {
		const a = action.toLowerCase();
		if (a.includes('delete') || a.includes('drop') || a.includes('revoke')) return 'bg-red-900/50 text-red-300';
		if (a.includes('create') || a.includes('enable') || a.includes('install') || a.includes('start')) return 'bg-green-900/50 text-green-300';
		if (a.includes('update') || a.includes('restart') || a.includes('reload') || a.includes('renew')) return 'bg-blue-900/50 text-blue-300';
		if (a.includes('failed') || a.includes('error')) return 'bg-red-900/50 text-red-300';
		return 'bg-gray-700 text-gray-300';
	}

	function moduleBadgeClass(module: string): string {
		switch (module) {
			case 'website': return 'bg-blue-900/50 text-blue-300';
			case 'dbmanager': case 'database': return 'bg-teal-900/50 text-teal-300';
			case 'backup': return 'bg-purple-900/50 text-purple-300';
			case 'auth': return 'bg-yellow-900/50 text-yellow-300';
			case 'ssl': return 'bg-emerald-900/50 text-emerald-300';
			default: return 'bg-gray-700 text-gray-300';
		}
	}

	function formatDate(dateStr: string): string {
		const d = new Date(dateStr);
		const today = new Date();
		const sameDay = d.toDateString() === today.toDateString();
		if (sameDay) {
			return d.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit', second: '2-digit' });
		}
		return d.toLocaleString(undefined, {
			month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit'
		});
	}

	function truncate(value: string, max: number): string {
		if (!value) return '—';
		return value.length > max ? value.slice(0, max) + '…' : value;
	}

	function goToPage(page: number) {
		if (page >= 1 && page <= totalPages) {
			loadLogs(page);
		}
	}

	onMount(() => {
		loadModules();
		loadLogs(1);
	});
</script>

<div class="space-y-6">
	<div class="flex flex-wrap items-center justify-between gap-3">
		<h2 class="text-2xl font-bold text-white">Audit Logs</h2>
		<button
			type="button"
			onclick={() => loadLogs(currentPage)}
			class="cursor-pointer rounded-lg border border-gray-600 bg-gray-700 px-3 py-1.5 text-xs text-gray-200 transition hover:bg-gray-600"
		>
			Refresh
		</button>
	</div>

	<!-- Filters -->
	<div class="rounded-xl border border-gray-700 bg-gray-800 p-4 space-y-3">
		<div class="flex flex-wrap items-center gap-2">
			<span class="text-[11px] font-semibold uppercase tracking-wider text-gray-500">Range:</span>
			{#each [{ label: 'Today', value: 'today' }, { label: 'Yesterday', value: 'yesterday' }, { label: 'Last 7 days', value: '7d' }, { label: 'Last 30 days', value: '30d' }, { label: 'All time', value: 'all' }] as chip}
				<button
					type="button"
					onclick={() => applyRange(chip.value as typeof range)}
					class="rounded-full px-3 py-1 text-[11px] font-medium transition {range === chip.value
						? 'bg-blue-500/20 text-blue-200'
						: 'text-gray-400 hover:bg-gray-700'}"
				>
					{chip.label}
				</button>
			{/each}
			<span class="ml-auto text-[11px] text-gray-500">
				{fromDate || 'beginning'} → {toDate || 'now'}
			</span>
		</div>
		<div class="flex flex-wrap items-center gap-2">
			<div>
				<label class="sr-only" for="audit-from">From</label>
				<input id="audit-from" type="date" bind:value={fromDate} onchange={onRangeDateChange}
					class="rounded-lg border border-gray-600 bg-gray-900 px-2.5 py-1.5 text-xs text-gray-200 focus:border-blue-500 focus:outline-none" />
				<span class="mx-1 text-xs text-gray-500">→</span>
				<label class="sr-only" for="audit-to">To</label>
				<input id="audit-to" type="date" bind:value={toDate} onchange={onRangeDateChange}
					class="rounded-lg border border-gray-600 bg-gray-900 px-2.5 py-1.5 text-xs text-gray-200 focus:border-blue-500 focus:outline-none" />
			</div>
			<div>
				<label class="sr-only" for="audit-module">Module</label>
				<select id="audit-module" bind:value={moduleFilter} onchange={() => loadLogs(1)}
					class="rounded-lg border border-gray-600 bg-gray-900 px-2.5 py-1.5 text-xs text-gray-200 focus:border-blue-500 focus:outline-none">
					<option value="">All modules</option>
					{#each modules as m}
						<option value={m}>{m}</option>
					{/each}
				</select>
			</div>
			<div class="relative min-w-52 flex-1 max-w-xs">
				<svg class="pointer-events-none absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-gray-500" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="2" aria-hidden="true">
					<path stroke-linecap="round" stroke-linejoin="round" d="M21 21l-4.35-4.35M17 10.5a6.5 6.5 0 11-13 0 6.5 6.5 0 0113 0z" />
				</svg>
				<input
					type="text"
					bind:value={searchInput}
					oninput={onSearchInput}
					placeholder="Search action, target, detail, user…"
					aria-label="Search audit logs"
					class="w-full rounded-lg border border-gray-600 bg-gray-900 py-1.5 pl-8 pr-3 text-xs text-gray-200 placeholder:text-gray-500 focus:border-blue-500 focus:outline-none"
				/>
			</div>
		</div>
	</div>

	{#if loading}
		<div class="space-y-2 rounded-xl border border-gray-700 bg-gray-800 p-5">
			{#each Array(6) as _}
				<div class="h-5 animate-pulse rounded bg-gray-700/50"></div>
			{/each}
		</div>
	{:else if error}
		<div class="rounded-lg border border-red-700 bg-red-900/30 p-3.5 text-sm text-red-300">{error}</div>
	{:else if entries.length === 0}
		<div class="rounded-xl border border-gray-700 bg-gray-800 p-10 text-center">
			<p class="text-sm text-gray-400">No audit entries for this filter.</p>
			<p class="mt-1 text-xs text-gray-500">Widen the date range or clear the search to see more.</p>
		</div>
	{:else}
		<div class="rounded-xl border border-gray-700 bg-gray-800 overflow-hidden">
			<div class="overflow-x-auto">
				<table class="w-full">
					<thead>
						<tr class="border-b border-gray-700 bg-gray-800/80">
							<th class="text-left px-4 py-3 text-[11px] text-gray-400 uppercase tracking-wider font-semibold">Time</th>
							<th class="text-left px-4 py-3 text-[11px] text-gray-400 uppercase tracking-wider font-semibold">User</th>
							<th class="text-left px-4 py-3 text-[11px] text-gray-400 uppercase tracking-wider font-semibold">Action</th>
							<th class="text-left px-4 py-3 text-[11px] text-gray-400 uppercase tracking-wider font-semibold">Module</th>
							<th class="text-left px-4 py-3 text-[11px] text-gray-400 uppercase tracking-wider font-semibold">Target</th>
							<th class="text-left px-4 py-3 text-[11px] text-gray-400 uppercase tracking-wider font-semibold">Detail</th>
							<th class="text-left px-4 py-3 text-[11px] text-gray-400 uppercase tracking-wider font-semibold">IP</th>
						</tr>
					</thead>
					<tbody class="divide-y divide-gray-700">
						{#each entries as entry (entry.id)}
							<tr class="hover:bg-gray-750">
								<td class="px-4 py-3 text-xs text-gray-400 whitespace-nowrap" title={new Date(entry.created_at).toLocaleString()}>
									{formatDate(entry.created_at)}
								</td>
								<td class="px-4 py-3 text-xs text-gray-300 truncate max-w-40" title={entry.username || entry.user_id}>
									{entry.username || entry.user_id || '—'}
								</td>
								<td class="px-4 py-3">
									<span class="inline-flex px-2 py-0.5 rounded text-[11px] font-medium {actionBadgeClass(entry.action)}">
										{entry.action}
									</span>
								</td>
								<td class="px-4 py-3">
									<span class="inline-flex px-2 py-0.5 rounded text-[11px] font-medium {moduleBadgeClass(entry.module)}">
										{entry.module}
									</span>
								</td>
								<td class="px-4 py-3 text-xs font-mono text-gray-300 max-w-44 truncate" title={entry.target}>
									{entry.target || '—'}
								</td>
								<td class="px-4 py-3 text-xs text-gray-400 max-w-64 truncate" title={entry.detail}>
									{entry.detail || '—'}
								</td>
								<td class="px-4 py-3 text-xs text-gray-400 font-mono">{entry.ip_address || '—'}</td>
							</tr>
						{/each}
					</tbody>
				</table>
			</div>
		</div>

		<!-- Pagination -->
		<div class="flex flex-wrap items-center justify-between gap-2">
			<div class="text-xs text-gray-500">
				{total} entries · page {currentPage} of {totalPages}
			</div>
			<div class="flex items-center gap-2">
				<button
					onclick={() => goToPage(currentPage - 1)}
					disabled={currentPage <= 1}
					class="px-3 py-1.5 bg-gray-700 hover:bg-gray-600 disabled:opacity-50 disabled:cursor-not-allowed text-gray-300 text-sm rounded transition-colors cursor-pointer"
				>
					Previous
				</button>

				{#each Array.from({ length: Math.min(5, totalPages) }, (_, i) => {
					const start = Math.max(1, Math.min(currentPage - 2, totalPages - 4));
					return start + i;
				}).filter((p) => p <= totalPages) as pageNum}
					<button
						onclick={() => goToPage(pageNum)}
						class="px-3 py-1.5 text-sm rounded transition-colors cursor-pointer
						{pageNum === currentPage
							? 'bg-blue-600 text-white'
							: 'bg-gray-700 hover:bg-gray-600 text-gray-300'}"
					>
						{pageNum}
					</button>
				{/each}

				<button
					onclick={() => goToPage(currentPage + 1)}
					disabled={currentPage >= totalPages}
					class="px-3 py-1.5 bg-gray-700 hover:bg-gray-600 disabled:opacity-50 disabled:cursor-not-allowed text-gray-300 text-sm rounded transition-colors cursor-pointer"
				>
					Next
				</button>
			</div>
		</div>
	{/if}
</div>
