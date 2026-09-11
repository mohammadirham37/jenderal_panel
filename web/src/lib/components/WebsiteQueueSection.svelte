<script lang="ts">
	import { onDestroy } from 'svelte';
	import { api } from '$lib/api';

	interface QueueWorker {
		id: string;
		website_id: string;
		command: string;
		num_workers: number;
		auto_restart: boolean;
		status: string;
		created_at: string;
	}

	interface Props {
		websiteID: string;
		/** Shown in the create form heading; purely cosmetic. */
		domain?: string;
	}

	let { websiteID, domain = '' }: Props = $props();

	let workers = $state<QueueWorker[]>([]);
	let loading = $state(false);
	let error = $state('');
	let actionMsg = $state('');
	let actionError = $state('');

	let showCreateForm = $state(false);
	let createCommand = $state('php artisan queue:work --sleep=3 --tries=3');
	let createNumWorkers = $state(1);
	let creating = $state(false);

	let deleteConfirmId = $state<string | null>(null);
	let busyWorkerId = $state<string | null>(null);

	// Monitoring panels: one worker at a time shows status or logs.
	let panelWorkerId = $state<string | null>(null);
	let panelMode = $state<'status' | 'logs'>('status');
	let panelContent = $state('');
	let panelLoading = $state(false);
	let panelError = $state('');

	let refreshTimer: ReturnType<typeof setInterval> | null = null;

	function workerAPI(workerId: string): string {
		return `/api/v1/websites/${encodeURIComponent(websiteID)}/queue-workers/${encodeURIComponent(workerId)}`;
	}

	function statusBadgeClass(status: string): string {
		switch (status) {
			case 'running': return 'bg-green-900/50 text-green-400';
			case 'stopped': return 'bg-gray-700 text-gray-400';
			case 'failed': return 'bg-red-900/50 text-red-400';
			case 'starting':
			case 'restarting': return 'bg-yellow-900/50 text-yellow-400 animate-pulse';
			default: return 'bg-gray-700 text-gray-400';
		}
	}

	async function loadWorkers() {
		if (!websiteID) return;
		loading = true;
		error = '';
		try {
			workers = (await api.get<QueueWorker[]>(
				`/api/v1/websites/${encodeURIComponent(websiteID)}/queue-workers`
			)) || [];
		} catch (err) {
			error = err instanceof Error ? err.message : 'Failed to load queue workers';
		} finally {
			loading = false;
		}
	}

	async function createWorker() {
		if (creating || !createCommand.trim() || createNumWorkers < 1) return;
		creating = true;
		actionMsg = '';
		actionError = '';
		try {
			await api.post(
				`/api/v1/websites/${encodeURIComponent(websiteID)}/queue-workers`,
				{ command: createCommand.trim(), num_workers: createNumWorkers }
			);
			actionMsg = 'Queue worker created and started.';
			showCreateForm = false;
			createCommand = 'php artisan queue:work --sleep=3 --tries=3';
			createNumWorkers = 1;
			await loadWorkers();
		} catch (err) {
			actionError = err instanceof Error ? err.message : 'Failed to create queue worker';
		} finally {
			creating = false;
		}
	}

	async function runWorkerAction(worker: QueueWorker, action: 'start' | 'stop' | 'restart', success: string) {
		if (busyWorkerId) return;
		busyWorkerId = worker.id;
		actionMsg = '';
		actionError = '';
		try {
			await api.post(`${workerAPI(worker.id)}/${action}`, {});
			actionMsg = success;
			await loadWorkers();
		} catch (err) {
			actionError = err instanceof Error ? err.message : `Failed to ${action} worker`;
		} finally {
			busyWorkerId = null;
		}
	}

	async function deleteWorker(worker: QueueWorker) {
		if (busyWorkerId) return;
		busyWorkerId = worker.id;
		deleteConfirmId = null;
		actionMsg = '';
		actionError = '';
		try {
			await api.del(workerAPI(worker.id));
			actionMsg = 'Queue worker deleted.';
			if (panelWorkerId === worker.id) closePanel();
			await loadWorkers();
		} catch (err) {
			actionError = err instanceof Error ? err.message : 'Failed to delete queue worker';
		} finally {
			busyWorkerId = null;
		}
	}

	async function openPanel(worker: QueueWorker, mode: 'status' | 'logs') {
		if (panelWorkerId === worker.id && panelMode === mode) {
			closePanel();
			return;
		}
		panelWorkerId = worker.id;
		panelMode = mode;
		panelError = '';
		await loadPanel(worker.id, mode);
	}

	async function loadPanel(workerId: string, mode: 'status' | 'logs') {
		panelLoading = true;
		panelError = '';
		try {
			const url = workerAPI(workerId) + (mode === 'status' ? '/status' : '/logs?lines=100');
			const data = await api.get<Record<string, string>>(url);
			panelContent = (mode === 'status' ? data.status_output : data.logs) || '(empty)';
		} catch (err) {
			panelError = err instanceof Error ? err.message : 'Failed to load output';
			panelContent = '';
		} finally {
			panelLoading = false;
		}
	}

	function closePanel() {
		panelWorkerId = null;
		panelMode = 'status';
		panelContent = '';
		panelError = '';
	}

	// Light polling keeps the monitoring view fresh while mounted.
	$effect(() => {
		if (!websiteID) return;
		loadWorkers();
		refreshTimer = setInterval(() => {
			if (workers.length > 0) loadWorkers();
		}, 20000);
		return () => {
			if (refreshTimer) clearInterval(refreshTimer);
		};
	});

	onDestroy(() => {
		if (refreshTimer) clearInterval(refreshTimer);
	});
</script>

<div class="space-y-4">
	<div class="flex flex-wrap items-center justify-between gap-2">
		<p class="text-xs text-gray-500">
			{domain ? `Queue workers for ${domain}` : 'Queue workers'} · status refreshes automatically
		</p>
		<div class="flex items-center gap-2">
			<button
				type="button"
				onclick={loadWorkers}
				class="cursor-pointer rounded-lg border border-gray-600 bg-gray-700 px-3 py-1.5 text-xs text-gray-200 transition hover:bg-gray-600"
			>
				Refresh
			</button>
			<button
				type="button"
				onclick={() => (showCreateForm = !showCreateForm)}
				class="cursor-pointer rounded-lg bg-blue-600 px-3 py-1.5 text-xs font-semibold text-white transition hover:bg-blue-700"
			>
				{showCreateForm ? 'Close' : 'New worker'}
			</button>
		</div>
	</div>

	{#if actionMsg || actionError}
		<div class="rounded-lg px-4 py-2.5 text-sm {actionError ? 'bg-red-900/50 border border-red-700 text-red-300' : 'bg-green-900/40 border border-green-700 text-green-300'}">
			{actionError || actionMsg}
		</div>
	{/if}

	{#if showCreateForm}
		<div class="rounded-xl border border-gray-700 bg-gray-800 p-5">
			<h3 class="mb-4 text-sm font-semibold text-white">New queue worker</h3>
			<div class="grid gap-3 sm:grid-cols-3">
				<div class="sm:col-span-2">
					<label class="mb-1 block text-[11px] font-medium uppercase tracking-wider text-gray-400" for="qw-command">Command</label>
					<input
						id="qw-command"
						type="text"
						bind:value={createCommand}
						class="w-full rounded-lg border border-gray-600 bg-gray-900 px-2.5 py-2 font-mono text-xs text-gray-200 focus:border-blue-500 focus:outline-none"
					/>
				</div>
				<div>
					<label class="mb-1 block text-[11px] font-medium uppercase tracking-wider text-gray-400" for="qw-workers">Workers</label>
					<input
						id="qw-workers"
						type="number"
						min="1"
						bind:value={createNumWorkers}
						class="w-full rounded-lg border border-gray-600 bg-gray-900 px-2.5 py-2 text-sm text-gray-200 focus:border-blue-500 focus:outline-none"
					/>
				</div>
			</div>
			<p class="mt-2 text-[11px] text-gray-500">
				The worker runs as a systemd service in the site directory, restarted automatically on failure.
			</p>
			<button
				type="button"
				onclick={createWorker}
				disabled={creating || !createCommand.trim()}
				class="mt-3 cursor-pointer rounded-lg bg-blue-600 px-4 py-2 text-sm font-semibold text-white transition hover:bg-blue-700 disabled:opacity-40"
			>
				{creating ? 'Creating…' : 'Create worker'}
			</button>
		</div>
	{/if}

	<div class="rounded-xl border border-gray-700 bg-gray-800">
		{#if loading}
			<div class="space-y-2 p-5">
				{#each Array(3) as _}
					<div class="h-5 animate-pulse rounded bg-gray-700/50"></div>
				{/each}
			</div>
		{:else if error}
			<div class="m-5 rounded-lg border border-red-700 bg-red-900/30 p-3.5 text-sm text-red-300">{error}</div>
		{:else if workers.length === 0}
			<div class="p-10 text-center">
				<p class="text-sm text-gray-400">No queue workers yet.</p>
				<p class="mt-1 text-xs text-gray-500">Create one to process Laravel jobs in the background.</p>
			</div>
		{:else}
			<div class="divide-y divide-gray-700/40">
				{#each workers as w (w.id)}
					<div class="px-5 py-3">
						<div class="flex flex-wrap items-center gap-3">
							<span class="rounded-full px-2.5 py-0.5 text-[11px] font-medium {statusBadgeClass(w.status)}">{w.status}</span>
							<div class="min-w-0 flex-1">
								<p class="truncate font-mono text-sm text-gray-100">{w.command}</p>
								<p class="text-[11px] text-gray-500">
									{w.num_workers} worker{w.num_workers > 1 ? 's' : ''}
									{w.auto_restart ? '· auto-restart' : ''}
									· created {new Date(w.created_at).toLocaleString()}
								</p>
							</div>
							<div class="flex shrink-0 flex-wrap items-center gap-1">
								{#if w.status === 'running'}
									<button type="button" onclick={() => runWorkerAction(w, 'restart', 'Worker restarted.')} disabled={busyWorkerId === w.id}
										class="cursor-pointer rounded-lg px-2.5 py-1 text-[11px] text-gray-300 transition hover:bg-gray-700 disabled:opacity-50">Restart</button>
									<button type="button" onclick={() => runWorkerAction(w, 'stop', 'Worker stopped.')} disabled={busyWorkerId === w.id}
										class="cursor-pointer rounded-lg px-2.5 py-1 text-[11px] text-red-300 transition hover:bg-red-600 hover:text-white disabled:opacity-50">Stop</button>
								{:else}
									<button type="button" onclick={() => runWorkerAction(w, 'start', 'Worker started.')} disabled={busyWorkerId === w.id}
										class="cursor-pointer rounded-lg px-2.5 py-1 text-[11px] text-green-300 transition hover:bg-green-600 hover:text-white disabled:opacity-50">Start</button>
								{/if}
								<button type="button" onclick={() => openPanel(w, 'status')}
									class="cursor-pointer rounded-lg px-2.5 py-1 text-[11px] text-gray-300 transition hover:bg-gray-700">Status</button>
								<button type="button" onclick={() => openPanel(w, 'logs')}
									class="cursor-pointer rounded-lg px-2.5 py-1 text-[11px] text-gray-300 transition hover:bg-gray-700">Logs</button>
								{#if deleteConfirmId === w.id}
									<span class="text-[11px] text-red-400">Delete?</span>
									<button type="button" onclick={() => deleteWorker(w)} disabled={busyWorkerId === w.id}
										class="cursor-pointer rounded-lg bg-red-600 px-2.5 py-1 text-[11px] font-semibold text-white transition hover:bg-red-700 disabled:opacity-50">Yes</button>
									<button type="button" onclick={() => (deleteConfirmId = null)}
										class="cursor-pointer rounded-lg bg-gray-700 px-2.5 py-1 text-[11px] text-gray-200 transition hover:bg-gray-600">No</button>
								{:else}
									<button type="button" onclick={() => (deleteConfirmId = w.id)} title="Delete worker"
										class="cursor-pointer rounded-lg p-1.5 text-gray-400 transition hover:bg-red-600 hover:text-white">
										<svg class="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="1.8" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" d="M6 18L18 6M6 6l12 12" /></svg>
									</button>
								{/if}
							</div>
						</div>
						{#if panelWorkerId === w.id}
							<div class="mt-3 rounded-lg border border-gray-700 bg-gray-950">
								<div class="flex items-center justify-between border-b border-gray-700 px-3 py-1.5">
									<span class="text-[10px] font-semibold uppercase tracking-wider text-gray-500">
										{panelMode === 'status' ? 'systemctl status' : 'journalctl (last 100 lines)'}
									</span>
									<div class="flex items-center gap-2">
										{#if panelMode === 'logs'}
											<button type="button" onclick={() => loadPanel(w.id, 'logs')} class="cursor-pointer text-[10px] text-gray-400 hover:text-gray-200">Reload</button>
										{/if}
										<button type="button" onclick={closePanel} class="cursor-pointer text-[10px] text-gray-400 hover:text-gray-200">Close</button>
									</div>
								</div>
								{#if panelLoading}
									<div class="p-3 text-xs text-gray-500">Loading…</div>
								{:else if panelError}
									<div class="p-3 font-mono text-xs text-red-400">{panelError}</div>
								{:else}
									<pre class="max-h-72 overflow-auto p-3 font-mono text-[11px] leading-relaxed text-green-400">{panelContent}</pre>
								{/if}
							</div>
						{/if}
					</div>
				{/each}
			</div>
		{/if}
	</div>
</div>
