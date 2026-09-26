<script lang="ts">
	import { onMount } from 'svelte';
	import { api } from '$lib/api';
	import TaskProgress from '$lib/components/TaskProgress.svelte';
	import { cacheBustedURL, classifyUpdateRecovery, waitForUpdatedPanel } from '$lib/update-readiness.js';
	import { language, translate } from '$lib/stores/language';
	import { permissions, hasPermission } from '$lib/stores/auth';
	import { toast } from '$lib/stores/toast';

	interface UpdateInfo {
		current_version: string;
		latest_version: string;
		update_available: boolean;
		release_url?: string;
	}

	interface CommitInfo {
		sha: string;
		message: string;
		author: string;
		date: string;
		url: string;
	}

	let info = $state<UpdateInfo | null>(null);
	let loading = $state(true);
	let error = $state('');
	let updating = $state(false);
	let updateMsg = $state('');
	let updateError = $state('');
	let confirmUpdate = $state(false);
	let currentTaskId = $state('');
	let expectedUpdateVersion = $state('');
	let reloadTimedOut = $state(false);
	let reloadWatcherActive = false;
	let reloadWatcherGeneration = 0;
	let updateInProgress = $derived(updating || !!currentTaskId);
	const updateTargetStorageKey = 'jenderal_update_target';
	const updateTaskStorageKey = 'jenderal_update_task';

	async function checkUpdate() {
		loading = true;
		error = '';
		try {
			info = await api.getNoStore<UpdateInfo>(`/api/v1/update/check?_=${Date.now()}`);
			const recovery = classifyUpdateRecovery(info.current_version, expectedUpdateVersion, currentTaskId);
			if (recovery === 'complete' || recovery === 'stale') {
				localStorage.removeItem(updateTargetStorageKey);
				if (recovery === 'complete') localStorage.removeItem(updateTaskStorageKey);
				expectedUpdateVersion = '';
				if (recovery === 'complete') currentTaskId = '';
				updating = false;
			}
		} catch (err) {
			error = err instanceof Error ? err.message : translate($language, 'upd.failedToCheck');
		} finally {
			loading = false;
		}
	}

	async function performUpdate() {
		confirmUpdate = false;
		updating = true;
		updateMsg = '';
		updateError = '';
		reloadTimedOut = false;
		currentTaskId = '';
		try {
			expectedUpdateVersion = info?.latest_version || '';
			if (expectedUpdateVersion) localStorage.setItem(updateTargetStorageKey, expectedUpdateVersion);
			const result = await api.post<{ task_id: string }>('/api/v1/update/perform');
			currentTaskId = result.task_id;
			updateMsg = translate($language, 'upd.started');
		} catch (err) {
			updateError = err instanceof Error ? err.message : translate($language, 'upd.failedToStart');
			updating = false;
			localStorage.removeItem(updateTargetStorageKey);
		}
	}

	async function resumeUpdateReload(maxAttempts = 900) {
		if (reloadWatcherActive) return;
		const target = expectedUpdateVersion || localStorage.getItem(updateTargetStorageKey) || '';
		if (!target) return;
		reloadWatcherActive = true;
		updating = true;
		const generation = ++reloadWatcherGeneration;
		const ready = await waitForUpdatedPanel({
			expectedVersion: target,
			check: () => api.getNoStore<Pick<UpdateInfo, 'current_version'>>(`/api/v1/update/current?_=${Date.now()}`),
			maxAttempts,
			shouldContinue: () => generation === reloadWatcherGeneration
		});
		if (generation !== reloadWatcherGeneration) return;
		reloadWatcherActive = false;
		updating = false;
		if (ready) {
			localStorage.removeItem(updateTargetStorageKey);
			localStorage.removeItem(updateTaskStorageKey);
			currentTaskId = '';
			window.location.replace(cacheBustedURL(window.location.href));
			return;
		}
		reloadTimedOut = true;
		updateMsg = translate($language, 'upd.recoveryTimedOut');
	}

	async function onTaskComplete(task: { status?: string; error?: string }) {
		updating = false;
		if (task?.status === 'failed') {
			reloadWatcherGeneration += 1;
			reloadWatcherActive = false;
			localStorage.removeItem(updateTargetStorageKey);
			updateError = task.error || translate($language, 'upd.failed');
			return;
		}

		updateMsg = translate($language, 'upd.completedWaiting');
		const target = expectedUpdateVersion || localStorage.getItem(updateTargetStorageKey) || info?.latest_version || '';
		if (!target) {
			reloadTimedOut = true;
			updateMsg = translate($language, 'upd.completedUnconfirmed');
			return;
		}

		await resumeUpdateReload(60);
	}

	function onTaskMissing() {
		reloadWatcherGeneration += 1;
		reloadWatcherActive = false;
		updating = false;
		expectedUpdateVersion = '';
		updateMsg = '';
		reloadTimedOut = false;
		localStorage.removeItem(updateTargetStorageKey);
	}

	function reloadPanel() {
		window.location.replace(cacheBustedURL(window.location.href));
	}

	// ─── Changelog modal ─────────────────────────────────────────────

	let showChangelog = $state(false);
	let changelogLoading = $state(false);
	let changelogError = $state('');
	let changelog = $state<CommitInfo[]>([]);

	async function openChangelog() {
		showChangelog = true;
		changelogLoading = true;
		changelogError = '';
		changelog = [];
		try {
			changelog = (await api.get<CommitInfo[]>('/api/v1/update/changelog?limit=15')) || [];
		} catch (err) {
			changelogError = err instanceof Error ? err.message : translate($language, 'upd.changelog_failed');
		} finally {
			changelogLoading = false;
		}
	}

	function commitDate(iso: string): string {
		try {
			return new Date(iso).toLocaleDateString('en-US', { year: 'numeric', month: 'short', day: 'numeric' });
		} catch {
			return iso;
		}
	}

	// ─── Ubuntu package updates (apt) ────────────────────────────────

	let aptUpdateTaskId = $state('');
	let aptUpgradeTaskId = $state('');
	let aptBusy = $state(false);
	let confirmAptUpgrade = $state(false);
	const canManageOS = $derived(hasPermission($permissions, 'update.perform'));
	const aptInProgress = $derived(!!aptUpdateTaskId || !!aptUpgradeTaskId);

	async function startApt(kind: 'update' | 'upgrade') {
		if (aptBusy || aptInProgress) {
			toast.error(translate($language, 'upd.os.running'));
			return;
		}
		confirmAptUpgrade = false;
		aptBusy = true;
		try {
			const result = await api.post<{ task_id: string }>(`/api/v1/server/apt/${kind}`);
			if (kind === 'update') {
				aptUpdateTaskId = result.task_id;
				toast.success(translate($language, 'upd.os.update_started'));
			} else {
				aptUpgradeTaskId = result.task_id;
				toast.success(translate($language, 'upd.os.upgrade_started'));
			}
		} catch (err) {
			toast.error(err instanceof Error ? err.message : translate($language, 'upd.os.start_failed'));
		} finally {
			aptBusy = false;
		}
	}

	function onAptComplete(_task: { status?: string }) {
		// TaskProgress renders the outcome (including failures) itself; here we
		// only release the buttons.
		aptUpdateTaskId = '';
		aptUpgradeTaskId = '';
	}

	function onAptMissing() {
		aptUpdateTaskId = '';
		aptUpgradeTaskId = '';
	}

	onMount(() => {
		expectedUpdateVersion = localStorage.getItem(updateTargetStorageKey) || '';
		currentTaskId = localStorage.getItem(updateTaskStorageKey) || '';
		void checkUpdate();
	});
</script>

<svelte:window
	onkeydown={(e) => {
		if (e.key === 'Escape') showChangelog = false;
	}}
/>

<div class="space-y-6">
	<div class="flex items-center justify-between">
		<h2 class="text-2xl font-bold text-white">{translate($language, 'upd.title')}</h2>
		<div class="flex items-center gap-2">
			<button
				onclick={openChangelog}
				class="inline-flex items-center gap-1.5 px-4 py-2 bg-gray-700 hover:bg-gray-600 text-gray-300 hover:text-white text-sm rounded-lg transition-colors cursor-pointer"
			>
				<svg class="h-3.5 w-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="2" aria-hidden="true">
					<path stroke-linecap="round" stroke-linejoin="round" d="M9 12h3.75M9 15h3.75M9 18h3.75m3 .75H18a2.25 2.25 0 002.25-2.25V6.108c0-1.135-.845-2.098-1.976-2.192a48.424 48.424 0 00-1.123-.08m-5.801 0c-.065.21-.1.433-.1.664 0 .414.336.75.75.75h4.5a.75.75 0 00.75-.75 2.25 2.25 0 00-.1-.664m-5.8 0A2.251 2.251 0 0113.5 2.25H15c1.012 0 1.867.668 2.15 1.586m-5.8 0c-.376.023-.75.05-1.124.08C9.095 4.01 8.25 4.973 8.25 6.108V8.25m0 0H4.875c-.621 0-1.125.504-1.125 1.125v11.25c0 .621.504 1.125 1.125 1.125h9.75c.621 0 1.125-.504 1.125-1.125V9.375c0-.621-.504-1.125-1.125-1.125H8.25z" />
				</svg>
				{translate($language, 'upd.changelog')}
			</button>
			<button
				onclick={checkUpdate}
				disabled={loading || updateInProgress}
				class="px-4 py-2 bg-gray-700 hover:bg-gray-600 disabled:opacity-50 text-gray-300 text-sm rounded-lg transition-colors cursor-pointer"
			>
				{loading ? translate($language, 'upd.checking') : translate($language, 'upd.checkAgain')}
			</button>
		</div>
	</div>

	{#if updateMsg}
		<div class="p-3 bg-green-900/50 border border-green-700 rounded-lg text-green-300 text-sm">
			{updateMsg}
		</div>
	{/if}

	{#if updateError}
		<div class="p-3 bg-red-900/50 border border-red-700 rounded-lg text-red-300 text-sm">
			{updateError}
			<button onclick={() => (updateError = '')} class="ml-2 text-red-400 hover:text-red-200 cursor-pointer">{translate($language, 'upd.dismiss')}</button>
		</div>
	{/if}

	{#if reloadTimedOut}
		<button
			onclick={reloadPanel}
			class="px-4 py-2 bg-blue-600 hover:bg-blue-700 text-white text-sm font-medium rounded-lg transition-colors cursor-pointer"
		>
			{translate($language, 'upd.reloadPanel')}
		</button>
	{/if}

	{#if loading}
		<div class="text-gray-400">{translate($language, 'upd.checkingForUpdates')}</div>
	{:else if error}
		<div class="p-4 bg-red-900/50 border border-red-700 rounded-lg text-red-300">{error}</div>
	{:else if info}
		<div class="bg-gray-800 rounded-lg border border-gray-700 p-5">
			<div class="grid grid-cols-1 sm:grid-cols-2 gap-4 mb-4">
				<div>
					<span class="block text-xs text-gray-400 uppercase tracking-wider mb-1">{translate($language, 'upd.currentVersion')}</span>
					<span class="text-lg font-mono text-white">{info.current_version}</span>
				</div>
				<div>
					<span class="block text-xs text-gray-400 uppercase tracking-wider mb-1">{translate($language, 'upd.latestCommit')}</span>
					<span class="text-lg font-mono text-white">{info.latest_version}</span>
					{#if info.release_url}
						<a href={info.release_url} target="_blank" rel="noopener" class="ml-2 text-xs text-blue-400 hover:text-blue-300">{translate($language, 'upd.viewOnGitHub')}</a>
					{/if}
				</div>
			</div>

			<div class="flex items-center gap-3 mb-4">
				{#if info.update_available}
					<span class="inline-flex items-center gap-1.5 px-2.5 py-0.5 rounded-full text-xs font-medium bg-yellow-900/50 text-yellow-400">
						<span class="w-1.5 h-1.5 rounded-full bg-yellow-400"></span>
						{translate($language, 'upd.updateAvailable')}
					</span>
				{:else}
					<span class="inline-flex items-center gap-1.5 px-2.5 py-0.5 rounded-full text-xs font-medium bg-green-900/50 text-green-400">
						<span class="w-1.5 h-1.5 rounded-full bg-green-400"></span>
						{translate($language, 'upd.upToDate')}
					</span>
				{/if}
			</div>

			{#if info.update_available && !updateInProgress}
				{#if confirmUpdate}
					<div class="p-4 bg-yellow-950 border border-yellow-700 rounded-lg">
						<p class="text-yellow-300 text-sm font-medium mb-1">{translate($language, 'upd.confirmUpdate')}</p>
						<p class="text-yellow-400 text-xs mb-3">
							{translate($language, 'upd.confirmUpdateDesc')}
						</p>
						<div class="flex gap-2">
							<button
								onclick={performUpdate}
								class="px-4 py-2 bg-yellow-600 hover:bg-yellow-700 text-white text-sm font-medium rounded transition-colors cursor-pointer"
							>
								{translate($language, 'upd.confirmUpdate')}
							</button>
							<button
								onclick={() => (confirmUpdate = false)}
								class="px-4 py-2 bg-gray-600 hover:bg-gray-500 text-white text-sm rounded transition-colors cursor-pointer"
							>
								{translate($language, 'upd.cancel')}
							</button>
						</div>
					</div>
				{:else}
					<button
						onclick={() => (confirmUpdate = true)}
						class="px-4 py-2 bg-blue-600 hover:bg-blue-700 text-white text-sm font-medium rounded-lg transition-colors cursor-pointer"
					>
						{translate($language, 'upd.updateNow')}
					</button>
				{/if}
			{/if}
		</div>
	{/if}

<!-- Changelog modal -->
{#if showChangelog}
	<div class="fixed inset-0 z-50 flex items-center justify-center p-4" role="dialog" aria-modal="true" aria-label={translate($language, 'upd.changelog')}>
		<button
			type="button"
			class="absolute inset-0 cursor-default bg-black/60 backdrop-blur-sm"
			onclick={() => (showChangelog = false)}
			aria-label={translate($language, 'upd.cancel')}
			tabindex="-1"
		></button>
		<div class="relative z-10 flex max-h-[80vh] w-full max-w-2xl flex-col overflow-hidden rounded-2xl border border-white/10 bg-gray-800 shadow-2xl">
			<div class="flex items-start justify-between gap-3 border-b border-white/5 px-5 py-4">
				<div>
					<h3 class="text-base font-semibold text-white">{translate($language, 'upd.changelog')}</h3>
					<p class="mt-0.5 text-xs text-gray-500">{translate($language, 'upd.changelog_desc')}</p>
				</div>
				<button
					type="button"
					onclick={() => (showChangelog = false)}
					class="cursor-pointer rounded-lg p-1.5 text-gray-400 transition hover:bg-gray-700 hover:text-white"
					aria-label={translate($language, 'upd.cancel')}
				>
					<svg class="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="2" aria-hidden="true">
						<path stroke-linecap="round" d="M6 18L18 6M6 6l12 12" />
					</svg>
				</button>
			</div>

			<div class="flex-1 overflow-y-auto divide-y divide-gray-700/40">
				{#if changelogLoading}
					<div class="divide-y divide-gray-700/40">
						{#each Array(5) as _}
							<div class="px-5 py-3.5">
								<div class="h-3.5 w-3/4 animate-pulse rounded bg-gray-700/50"></div>
								<div class="mt-2 h-3 w-1/3 animate-pulse rounded bg-gray-700/40"></div>
							</div>
						{/each}
					</div>
				{:else if changelogError}
					<div class="px-5 py-6 text-sm text-red-300">{changelogError}</div>
				{:else if changelog.length === 0}
					<div class="px-5 py-6 text-sm text-gray-400">{translate($language, 'upd.changelog_empty')}</div>
				{:else}
					{#each changelog as c (c.sha)}
						<div class="px-5 py-3.5 transition hover:bg-gray-750">
							<p class="text-sm leading-snug text-gray-100">{c.message}</p>
							<p class="mt-1.5 flex flex-wrap items-center gap-2 text-[11px] text-gray-500">
								<a href={c.url} target="_blank" rel="noopener" class="font-mono text-blue-400 hover:text-blue-300">{c.sha}</a>
								<span class="text-gray-600">·</span>
								<span>{c.author}</span>
								<span class="text-gray-600">·</span>
								<span>{commitDate(c.date)}</span>
							</p>
						</div>
					{/each}
				{/if}
			</div>
		</div>
	</div>
{/if}

	<TaskProgress bind:taskId={currentTaskId} storageKey="jenderal_update_task" onComplete={onTaskComplete} onMissing={onTaskMissing} />

	{#if canManageOS}
		<div class="bg-gray-800 rounded-lg border border-gray-700 p-5">
			<div class="flex flex-wrap items-center justify-between gap-2">
				<h3 class="text-sm font-semibold text-white">{translate($language, 'upd.os.title')}</h3>
				<span class="text-xs text-gray-500">{aptInProgress ? translate($language, 'upd.os.running') : ''}</span>
			</div>
			<p class="mt-1 text-xs text-gray-500">{translate($language, 'upd.os.desc')}</p>

			{#if confirmAptUpgrade}
				<div class="mt-3 p-4 bg-yellow-950 border border-yellow-700 rounded-lg">
					<p class="text-yellow-300 text-sm font-medium mb-1">{translate($language, 'upd.os.confirm_upgrade')}</p>
					<p class="text-yellow-400 text-xs mb-3">{translate($language, 'upd.os.confirm_upgrade_desc')}</p>
					<div class="flex gap-2">
						<button
							type="button"
							onclick={() => startApt('upgrade')}
							disabled={aptBusy}
							class="px-4 py-2 bg-yellow-600 hover:bg-yellow-700 text-white text-sm font-medium rounded transition-colors cursor-pointer disabled:opacity-50"
						>
							{translate($language, 'upd.os.upgrade')}
						</button>
						<button
							type="button"
							onclick={() => (confirmAptUpgrade = false)}
							class="px-4 py-2 bg-gray-600 hover:bg-gray-500 text-white text-sm rounded transition-colors cursor-pointer"
						>
							{translate($language, 'upd.cancel')}
						</button>
					</div>
				</div>
			{:else}
				<div class="mt-3 flex flex-wrap gap-2">
					<button
						type="button"
						onclick={() => startApt('update')}
						disabled={aptBusy || aptInProgress}
						title={translate($language, 'upd.os.update_hint')}
						class="px-4 py-2 bg-blue-600 hover:bg-blue-700 text-white text-sm font-medium rounded-lg transition-colors cursor-pointer disabled:opacity-50"
					>
						{translate($language, 'upd.os.update')}
					</button>
					<button
						type="button"
						onclick={() => (confirmAptUpgrade = true)}
						disabled={aptBusy || aptInProgress}
						title={translate($language, 'upd.os.upgrade_hint')}
						class="px-4 py-2 bg-gray-700 hover:bg-gray-600 text-gray-200 text-sm font-medium rounded-lg transition-colors cursor-pointer disabled:opacity-50"
					>
						{translate($language, 'upd.os.upgrade')}
					</button>
				</div>
			{/if}

			<TaskProgress bind:taskId={aptUpdateTaskId} storageKey="jenderal_apt_update_task" onComplete={onAptComplete} onMissing={onAptMissing} />
			<TaskProgress bind:taskId={aptUpgradeTaskId} storageKey="jenderal_apt_upgrade_task" onComplete={onAptComplete} onMissing={onAptMissing} />
		</div>
	{/if}
</div>
