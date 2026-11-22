<script lang="ts">
	import { onMount } from 'svelte';
	import { api } from '#lib/api.js';
	import Modal from '#lib/components/Modal.svelte';
	import TaskProgress from '#lib/components/TaskProgress.svelte';
	import { language, translate } from '#lib/stores/language.js';
	import { toast } from '#lib/stores/toast.js';

	// Ubuntu 24.04 -> 26.04 release upgrade (internal/releaseupgrade). Shown on
	// noble servers, and on any server while an upgrade state exists so the
	// result stays visible after the reboot.

	type Check = { id: string; status: 'ok' | 'warn' | 'block'; message: string };
	type Preflight = { current: string; target: string; checks: Check[]; blocked: boolean };
	type Status = {
		state: 'idle' | 'running' | 'reboot_required' | 'failed';
		running: boolean;
		codename: string;
		log: string;
		warnings: string[];
	};

	let status = $state<Status | null>(null);
	let preflight = $state<Preflight | null>(null);
	let checking = $state(false);
	let loadError = $state('');
	let showConfirm = $state(false);
	let snapshotConfirmed = $state(false);
	let confirmText = $state('');
	let starting = $state(false);
	let rebooting = $state(false);
	let repairTaskId = $state('');
	let logEl = $state<HTMLPreElement | null>(null);

	const visible = $derived(!!status && (status.codename === 'noble' || status.state !== 'idle'));
	const canConfirm = $derived(snapshotConfirmed && confirmText.trim() === '26.04' && !starting);

	const t = (key: string) => translate($language, key);

	async function loadStatus() {
		try {
			status = await api.get<Status>('/api/v1/server/release-upgrade/status');
			loadError = '';
			// Readiness is shown before a first attempt and after a failed
			// one (to try again).
			const canTry = status.codename === 'noble' && (status.state === 'idle' || status.state === 'failed');
			if (canTry && !preflight && !checking) void loadPreflight();
		} catch (err) {
			loadError = err instanceof Error ? err.message : t('relupg.loadFailed');
		}
	}

	async function loadPreflight() {
		checking = true;
		try {
			preflight = await api.get<Preflight>('/api/v1/server/release-upgrade');
			loadError = '';
		} catch (err) {
			loadError = err instanceof Error ? err.message : t('relupg.loadFailed');
		} finally {
			checking = false;
		}
	}

	async function start() {
		if (!canConfirm) return;
		starting = true;
		try {
			await api.post('/api/v1/server/release-upgrade', { snapshot_confirmed: snapshotConfirmed, confirm: confirmText.trim() });
			toast.success(t('relupg.started'));
			showConfirm = false;
			await loadStatus();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : t('relupg.startFailed'));
			await loadPreflight();
		} finally {
			starting = false;
		}
	}

	async function reboot() {
		if (!confirm(t('relupg.rebootConfirm'))) return;
		rebooting = true;
		try {
			await api.post('/api/v1/server/reboot');
			toast.success(t('relupg.rebooting'));
		} catch (err) {
			rebooting = false;
			toast.error(err instanceof Error ? err.message : t('relupg.rebootFailed'));
		}
	}

	async function repair() {
		try {
			const result = await api.post<{ task_id: string }>('/api/v1/server/release-upgrade/repair');
			repairTaskId = result.task_id;
		} catch (err) {
			toast.error(err instanceof Error ? err.message : t('relupg.repairFailed'));
		}
	}

	function openConfirm() {
		snapshotConfirmed = false;
		confirmText = '';
		showConfirm = true;
	}

	onMount(() => {
		void loadStatus();
	});

	// Poll while the upgrade runs; the unit keeps going if the page closes.
	// A failed state keeps being polled (more slowly) so the card corrects
	// itself if the upgrade turns out to be still running.
	$effect(() => {
		const state = status?.state;
		if (!status?.running && state !== 'running' && state !== 'failed') return;
		const timer = setInterval(loadStatus, state === 'failed' && !status?.running ? 15000 : 3000);
		return () => clearInterval(timer);
	});

	// Keep the newest log lines in view.
	$effect(() => {
		if (status?.log && logEl) logEl.scrollTop = logEl.scrollHeight;
	});

	const checkTone = {
		ok: 'text-green-400',
		warn: 'text-yellow-400',
		block: 'text-red-400'
	};
	const checkIcon = { ok: '✓', warn: '!', block: '✕' };
	const stateTone: Record<string, string> = {
		running: 'bg-blue-900/60 text-blue-300 border-blue-700',
		reboot_required: 'bg-green-900/60 text-green-300 border-green-700',
		failed: 'bg-red-900/60 text-red-300 border-red-700'
	};
</script>

{#snippet readiness()}
	{#if checking && !preflight}
		<p class="mt-3 text-sm text-gray-400">{t('relupg.loading')}</p>
	{:else if preflight}
		<div class="mt-4">
			<div class="flex items-center justify-between">
				<h4 class="text-xs font-semibold uppercase tracking-wide text-gray-400">{t('relupg.checks')}</h4>
				<button
					type="button"
					onclick={loadPreflight}
					disabled={checking}
					class="text-xs text-gray-400 hover:text-white cursor-pointer disabled:opacity-50"
				>
					{checking ? t('relupg.loading') : t('relupg.recheck')}
				</button>
			</div>
			<ul class="mt-2 space-y-1.5">
				{#each preflight.checks as check (check.id)}
					<li class="flex gap-2 text-sm">
						<span class="w-4 shrink-0 font-bold {checkTone[check.status]}" aria-hidden="true">{checkIcon[check.status]}</span>
						<div class="min-w-0">
							<span class="text-gray-200">{t('relupg.check.' + check.id)}</span>
							<p class="text-xs text-gray-500 break-words">{check.message}</p>
						</div>
					</li>
				{/each}
			</ul>
		</div>
		{#if preflight.blocked}
			<p class="mt-3 text-xs text-red-400">{t('relupg.blocked')}</p>
		{/if}
		<button
			type="button"
			onclick={openConfirm}
			disabled={preflight.blocked || checking}
			class="mt-4 px-4 py-2 bg-orange-600 hover:bg-orange-700 text-white text-sm font-medium rounded-lg transition-colors cursor-pointer disabled:opacity-50 disabled:cursor-not-allowed"
		>
			{t('relupg.start')}
		</button>
	{/if}
{/snippet}

{#if visible && status}
	<div class="bg-gray-800 rounded-lg border border-gray-700 p-5">
		<div class="flex flex-wrap items-center justify-between gap-2">
			<h3 class="text-sm font-semibold text-white">{t('relupg.title')}</h3>
			{#if status.state !== 'idle'}
				<span class="px-2 py-0.5 text-xs rounded border {stateTone[status.state] ?? ''}">{t('relupg.state.' + status.state)}</span>
			{/if}
		</div>
		<p class="mt-1 text-xs text-gray-500">{t('relupg.desc')}</p>

		{#if loadError}
			<p class="mt-3 text-sm text-red-400">{loadError}</p>
		{/if}

		{#if status.state === 'idle'}
			{@render readiness()}
		{:else}
			<p class="mt-3 text-sm text-gray-300">
				{#if status.state === 'running'}
					{t('relupg.running.desc')}
				{:else if status.state === 'reboot_required'}
					{t('relupg.done.desc')}
				{:else}
					{t('relupg.failed.desc')}
				{/if}
			</p>

			{#if status.warnings.length > 0}
				<div class="mt-3 p-3 bg-yellow-950 border border-yellow-800 rounded-lg">
					<p class="text-xs font-semibold text-yellow-300">{t('relupg.warnings')}</p>
					<ul class="mt-1 list-disc pl-5 text-xs text-yellow-400 space-y-0.5">
						{#each status.warnings as warning, i (i)}
							<li>{warning}</li>
						{/each}
					</ul>
				</div>
			{/if}

			{#if status.state === 'reboot_required'}
				<button
					type="button"
					onclick={reboot}
					disabled={rebooting}
					class="mt-3 px-4 py-2 bg-green-600 hover:bg-green-700 text-white text-sm font-medium rounded-lg transition-colors cursor-pointer disabled:opacity-50"
				>
					{t('relupg.reboot')}
				</button>
			{:else if status.state === 'failed'}
				<button
					type="button"
					onclick={repair}
					disabled={!!repairTaskId}
					class="mt-3 px-4 py-2 bg-gray-700 hover:bg-gray-600 text-gray-200 text-sm font-medium rounded-lg transition-colors cursor-pointer disabled:opacity-50"
				>
					{t('relupg.repair')}
				</button>
				<TaskProgress bind:taskId={repairTaskId} storageKey="jenderal_release_upgrade_repair_task" />
			{/if}

			{#if status.state === 'failed' && status.codename === 'noble'}
				{@render readiness()}
			{/if}

			{#if status.log}
				<h4 class="mt-4 text-xs font-semibold uppercase tracking-wide text-gray-400">{t('relupg.log')}</h4>
				<pre
					bind:this={logEl}
					class="mt-2 max-h-96 overflow-auto rounded bg-gray-950 p-3 text-xs text-gray-300 whitespace-pre-wrap break-all">{status.log}</pre>
			{/if}
		{/if}
	</div>

	{#if showConfirm}
		<Modal title={t('relupg.confirm.title')} onclose={() => (showConfirm = false)} busy={starting} closeLabel={t('relupg.confirm.cancel')}>
			<p class="text-sm text-gray-300">{t('relupg.confirm.desc')}</p>
			<label class="mt-4 flex items-start gap-2 text-sm text-gray-200 cursor-pointer">
				<input type="checkbox" bind:checked={snapshotConfirmed} class="mt-0.5" />
				<span>{t('relupg.confirm.snapshot')}</span>
			</label>
			<label class="mt-4 block text-sm text-gray-300">
				{t('relupg.confirm.type')}
				<input
					type="text"
					bind:value={confirmText}
					autocomplete="off"
					placeholder="26.04"
					class="mt-1 w-full rounded-lg border border-gray-600 bg-gray-900 px-3 py-2 text-sm text-white"
				/>
			</label>
			{#snippet footer()}
				<button
					type="button"
					onclick={() => (showConfirm = false)}
					disabled={starting}
					class="px-4 py-2 bg-gray-700 hover:bg-gray-600 text-gray-200 text-sm rounded-lg cursor-pointer disabled:opacity-50"
				>
					{t('relupg.confirm.cancel')}
				</button>
				<button
					type="button"
					onclick={start}
					disabled={!canConfirm}
					class="px-4 py-2 bg-orange-600 hover:bg-orange-700 text-white text-sm font-medium rounded-lg cursor-pointer disabled:opacity-50 disabled:cursor-not-allowed"
				>
					{t('relupg.confirm.go')}
				</button>
			{/snippet}
		</Modal>
	{/if}
{/if}
