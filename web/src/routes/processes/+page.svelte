<script lang="ts">
	import { onMount, onDestroy } from 'svelte';
	import { api } from '$lib/api';
import { toast } from '$lib/stores/toast';
	import { language, translate } from '$lib/stores/language';

	interface Process {
		pid: number;
		user: string;
		cpu: number;
		ram: number;
		command: string;
	}

	let processes = $state<Process[]>([]);
	let loading = $state(true);
	let error = $state('');

	let sortBy = $state<'cpu' | 'ram'>('cpu');
	let autoRefresh = $state(false);
	let refreshInterval: ReturnType<typeof setInterval> | null = null;

	// Kill confirm dialog
	let killConfirm = $state<{ process: Process; signal: string } | null>(null);

	let sortedProcesses = $derived(
		[...processes].sort((a, b) => {
			if (sortBy === 'cpu') return b.cpu - a.cpu;
			return b.ram - a.ram;
		})
	);

	async function loadProcesses() {
		try {
			processes = (await api.get<Process[]>('/api/v1/processes')) || [];
			error = '';
		} catch (err) {
			error = err instanceof Error ? err.message : translate($language, 'prc.loadFailed');
		} finally {
			loading = false;
		}
	}

	function toggleAutoRefresh() {
		autoRefresh = !autoRefresh;
		if (autoRefresh) {
			refreshInterval = setInterval(loadProcesses, 5000);
		} else {
			if (refreshInterval) {
				clearInterval(refreshInterval);
				refreshInterval = null;
			}
		}
	}

	function showKillDialog(process: Process) {
		killConfirm = { process, signal: 'TERM' };
	}

	async function killProcess() {
		if (!killConfirm) return;
		const { process: proc, signal } = killConfirm;
		killConfirm = null;
		try {
			await api.post(`/api/v1/processes/${proc.pid}/kill`, { signal });
			toast.success(translate($language, 'prc.toast.signalSent').replace('{signal}', signal).replace('{pid}', String(proc.pid)));
			await loadProcesses();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : translate($language, 'prc.toast.killFailed').replace('{pid}', String(proc.pid)));
		}
	}

	function truncateCommand(cmd: string, max: number = 80): string {
		if (cmd.length <= max) return cmd;
		return cmd.slice(0, max) + '...';
	}

	onMount(loadProcesses);

	onDestroy(() => {
		if (refreshInterval) {
			clearInterval(refreshInterval);
		}
	});
</script>

<div class="space-y-6">
	<div class="flex items-center justify-between">
		<h2 class="text-2xl font-bold text-white">{translate($language, 'prc.title')}</h2>
		<div class="flex items-center gap-3">
			<div class="flex items-center gap-2">
				<span class="text-sm text-gray-400">{translate($language, 'prc.sortBy')}</span>
				<button
					onclick={() => (sortBy = 'cpu')}
					class="px-2.5 py-1 text-xs rounded transition-colors cursor-pointer {sortBy === 'cpu'
						? 'bg-blue-600 text-white'
						: 'bg-gray-700 text-gray-300 hover:bg-gray-600'}"
				>
					CPU
				</button>
				<button
					onclick={() => (sortBy = 'ram')}
					class="px-2.5 py-1 text-xs rounded transition-colors cursor-pointer {sortBy === 'ram'
						? 'bg-blue-600 text-white'
						: 'bg-gray-700 text-gray-300 hover:bg-gray-600'}"
				>
					RAM
				</button>
			</div>
			<button
				onclick={toggleAutoRefresh}
				class="px-3 py-1.5 text-sm rounded transition-colors cursor-pointer {autoRefresh
					? 'bg-green-600 hover:bg-green-700 text-white'
					: 'bg-gray-700 hover:bg-gray-600 text-gray-300'}"
			>
				{autoRefresh ? translate($language, 'prc.autoRefreshOn') : translate($language, 'prc.autoRefreshOff')}
			</button>
			<button
				onclick={loadProcesses}
				class="px-3 py-1.5 bg-gray-700 hover:bg-gray-600 text-gray-300 text-sm rounded transition-colors cursor-pointer"
			>
				{translate($language, 'prc.refresh')}
			</button>
		</div>
	</div>



	{#if loading}
		<div class="text-gray-400">{translate($language, 'prc.loading')}</div>
	{:else if error}
		<div class="p-4 bg-red-900/50 border border-red-700 rounded-lg text-red-300">{error}</div>
	{:else}
		<div class="bg-gray-800 rounded-lg border border-gray-700 overflow-hidden">
			<div class="overflow-x-auto">
				<table class="w-full">
					<thead>
						<tr class="border-b border-gray-700 bg-gray-800/80">
							<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">PID</th>
							<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">{translate($language, 'prc.table.user')}</th>
							<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">CPU%</th>
							<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">RAM%</th>
							<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">{translate($language, 'prc.table.command')}</th>
							<th class="text-right px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">{translate($language, 'prc.table.actions')}</th>
						</tr>
					</thead>
					<tbody class="divide-y divide-gray-700">
						{#each sortedProcesses as proc}
							<tr class="hover:bg-gray-750">
								<td class="px-4 py-2 text-sm text-gray-400 font-mono">{proc.pid}</td>
								<td class="px-4 py-2 text-sm text-gray-300">{proc.user}</td>
								<td class="px-4 py-2 text-sm font-mono {proc.cpu > 50 ? 'text-red-400' : proc.cpu > 20 ? 'text-yellow-400' : 'text-gray-300'}">
									{proc.cpu.toFixed(1)}
								</td>
								<td class="px-4 py-2 text-sm font-mono {proc.ram > 50 ? 'text-red-400' : proc.ram > 20 ? 'text-yellow-400' : 'text-gray-300'}">
									{proc.ram.toFixed(1)}
								</td>
								<td class="px-4 py-2 text-sm text-gray-400 font-mono" title={proc.command}>
									{truncateCommand(proc.command)}
								</td>
								<td class="px-4 py-2 text-right">
									<button
										onclick={() => showKillDialog(proc)}
										class="px-2.5 py-1 bg-red-600 hover:bg-red-700 text-white text-xs rounded transition-colors cursor-pointer"
									>
										{translate($language, 'prc.kill')}
									</button>
								</td>
							</tr>
						{/each}
					</tbody>
				</table>
			</div>
		</div>

		{#if killConfirm}
			<div class="p-4 bg-gray-800 border border-gray-600 rounded-lg">
				<p class="text-gray-300 text-sm mb-3">
					{translate($language, 'prc.killConfirm.label')} <span class="font-mono text-white">{killConfirm.process.pid}</span>
					(<span class="text-gray-400">{truncateCommand(killConfirm.process.command, 40)}</span>)?
				</p>
				<div class="flex items-center gap-3 mb-3">
					<label for="kill-signal" class="text-xs text-gray-400 uppercase tracking-wider">{translate($language, 'prc.signal.label')}</label>
					<select
						id="kill-signal"
						bind:value={killConfirm.signal}
						class="px-2 py-1 bg-gray-700 border border-gray-600 rounded text-white text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
					>
						<option value="TERM">{translate($language, 'prc.signal.term')}</option>
						<option value="KILL">{translate($language, 'prc.signal.kill')}</option>
						<option value="HUP">{translate($language, 'prc.signal.hup')}</option>
						<option value="INT">{translate($language, 'prc.signal.int')}</option>
					</select>
				</div>
				<div class="flex gap-2">
					<button
						onclick={killProcess}
						class="px-3 py-1.5 bg-red-600 hover:bg-red-700 text-white text-sm rounded transition-colors cursor-pointer"
					>
						{translate($language, 'prc.confirmKill')}
					</button>
					<button
						onclick={() => (killConfirm = null)}
						class="px-3 py-1.5 bg-gray-600 hover:bg-gray-500 text-white text-sm rounded transition-colors cursor-pointer"
					>
						{translate($language, 'prc.cancel')}
					</button>
				</div>
			</div>
		{/if}

		<div class="text-xs text-gray-500">
			{translate($language, 'prc.footer').replace('{count}', String(sortedProcesses.length)).replace('{sort}', sortBy === 'cpu' ? 'CPU' : 'RAM')}
			{#if autoRefresh}
				&mdash; {translate($language, 'prc.autoRefreshing')}
			{/if}
		</div>
	{/if}
</div>
