<script lang="ts">
	import { onMount } from 'svelte';
	import { apiRaw } from '$lib/api';
	import type { AuditEntry } from '$lib/types';

	let entries = $state<AuditEntry[]>([]);
	let loading = $state(true);
	let error = $state('');

	let currentPage = $state(1);
	let perPage = $state(20);
	let total = $state(0);

	let totalPages = $derived(Math.max(1, Math.ceil(total / perPage)));

	async function loadLogs(page: number) {
		loading = true;
		error = '';

		try {
			const res = await apiRaw<AuditEntry[]>(
				'GET',
				`/api/v1/audit-logs?page=${page}&per_page=${perPage}`
			);
			entries = res.data || [];
			if (res.meta) {
				currentPage = res.meta.page;
				total = res.meta.total;
			}
		} catch (err) {
			error = err instanceof Error ? err.message : 'Failed to load audit logs';
		} finally {
			loading = false;
		}
	}

	function goToPage(page: number) {
		if (page >= 1 && page <= totalPages) {
			loadLogs(page);
		}
	}

	onMount(() => loadLogs(1));
</script>

<div class="space-y-6">
	<h2 class="text-2xl font-bold text-white">Audit Logs</h2>

	{#if loading}
		<div class="text-gray-400">Loading audit logs...</div>
	{:else if error}
		<div class="p-4 bg-red-900/50 border border-red-700 rounded-lg text-red-300">{error}</div>
	{:else if entries.length === 0}
		<div class="bg-gray-800 rounded-lg border border-gray-700 p-8 text-center text-gray-400">
			No audit log entries found.
		</div>
	{:else}
		<div class="bg-gray-800 rounded-lg border border-gray-700 overflow-hidden">
			<div class="overflow-x-auto">
				<table class="w-full">
					<thead>
						<tr class="border-b border-gray-700 bg-gray-800/80">
							<th
								class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium"
								>Time</th
							>
							<th
								class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium"
								>Action</th
							>
							<th
								class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium"
								>Module</th
							>
							<th
								class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium"
								>Target</th
							>
							<th
								class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium"
								>Detail</th
							>
							<th
								class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium"
								>IP</th
							>
						</tr>
					</thead>
					<tbody class="divide-y divide-gray-700">
						{#each entries as entry}
							<tr class="hover:bg-gray-750">
								<td class="px-4 py-3 text-sm text-gray-400 whitespace-nowrap">
									{new Date(entry.created_at).toLocaleString()}
								</td>
								<td class="px-4 py-3">
									<span
										class="inline-flex px-2 py-0.5 rounded text-xs font-medium bg-gray-700 text-gray-300"
									>
										{entry.action}
									</span>
								</td>
								<td class="px-4 py-3 text-sm text-gray-300">{entry.module}</td>
								<td class="px-4 py-3 text-sm text-gray-300 font-mono text-xs"
									>{entry.target}</td
								>
								<td class="px-4 py-3 text-sm text-gray-400">{entry.detail}</td>
								<td class="px-4 py-3 text-sm text-gray-400 font-mono text-xs"
									>{entry.ip_address}</td
								>
							</tr>
						{/each}
					</tbody>
				</table>
			</div>
		</div>

		<!-- Pagination -->
		<div class="flex items-center justify-between">
			<div class="text-sm text-gray-400">
				Showing page {currentPage} of {totalPages} ({total} total entries)
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
