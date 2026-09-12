<script lang="ts">
	import { onMount } from 'svelte';
	import { api } from '$lib/api';
	import TaskProgress from '$lib/components/TaskProgress.svelte';
	import { cacheBustedURL, classifyUpdateRecovery, waitForUpdatedPanel } from '$lib/update-readiness.js';
	import { language, translate } from '$lib/stores/language';

	interface UpdateInfo {
		current_version: string;
		latest_version: string;
		update_available: boolean;
		release_url?: string;
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

	onMount(() => {
		expectedUpdateVersion = localStorage.getItem(updateTargetStorageKey) || '';
		currentTaskId = localStorage.getItem(updateTaskStorageKey) || '';
		void checkUpdate();
	});
</script>

<div class="space-y-6">
	<div class="flex items-center justify-between">
		<h2 class="text-2xl font-bold text-white">{translate($language, 'upd.title')}</h2>
		<button
			onclick={checkUpdate}
			disabled={loading || updateInProgress}
			class="px-4 py-2 bg-gray-700 hover:bg-gray-600 disabled:opacity-50 text-gray-300 text-sm rounded-lg transition-colors cursor-pointer"
		>
			{loading ? translate($language, 'upd.checking') : translate($language, 'upd.checkAgain')}
		</button>
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

	<TaskProgress bind:taskId={currentTaskId} storageKey="jenderal_update_task" onComplete={onTaskComplete} onMissing={onTaskMissing} />
</div>
