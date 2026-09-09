<script lang="ts">
	import { onMount } from 'svelte';
	import { api } from '$lib/api';
	import TaskProgress from '$lib/components/TaskProgress.svelte';
	import { cacheBustedURL, waitForUpdatedPanel } from '$lib/update-readiness.js';

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

	async function checkUpdate() {
		loading = true;
		error = '';
		try {
			info = await api.getNoStore<UpdateInfo>(`/api/v1/update/check?_=${Date.now()}`);
		} catch (err) {
			error = err instanceof Error ? err.message : 'Failed to check for updates';
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
			updateMsg = 'Update started. See progress below. Panel will restart after completion.';
		} catch (err) {
			updateError = err instanceof Error ? err.message : 'Failed to start update';
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
			localStorage.removeItem('jenderal_update_task');
			currentTaskId = '';
			window.location.replace(cacheBustedURL(window.location.href));
			return;
		}
		reloadTimedOut = true;
		updateMsg = 'Update recovery timed out. The panel may still be restarting.';
	}

	async function onTaskComplete(task: { status?: string; error?: string }) {
		updating = false;
		if (task?.status === 'failed') {
			reloadWatcherGeneration += 1;
			reloadWatcherActive = false;
			localStorage.removeItem(updateTargetStorageKey);
			updateError = task.error || 'Panel update failed.';
			return;
		}

		updateMsg = 'Update completed. Waiting for the updated panel to restart...';
		const target = expectedUpdateVersion || localStorage.getItem(updateTargetStorageKey) || info?.latest_version || '';
		if (!target) {
			reloadTimedOut = true;
			updateMsg = 'Update completed, but the target version could not be confirmed.';
			return;
		}

		await resumeUpdateReload(60);
	}

	function reloadPanel() {
		window.location.replace(cacheBustedURL(window.location.href));
	}

	onMount(() => {
		expectedUpdateVersion = localStorage.getItem(updateTargetStorageKey) || '';
		void checkUpdate();
		if (expectedUpdateVersion) void resumeUpdateReload();
	});
</script>

<div class="space-y-6">
	<div class="flex items-center justify-between">
		<h2 class="text-2xl font-bold text-white">Update Panel</h2>
		<button
			onclick={checkUpdate}
			disabled={loading || updateInProgress}
			class="px-4 py-2 bg-gray-700 hover:bg-gray-600 disabled:opacity-50 text-gray-300 text-sm rounded-lg transition-colors cursor-pointer"
		>
			{loading ? 'Checking...' : 'Check Again'}
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
			<button onclick={() => (updateError = '')} class="ml-2 text-red-400 hover:text-red-200 cursor-pointer">Dismiss</button>
		</div>
	{/if}

	{#if reloadTimedOut}
		<button
			onclick={reloadPanel}
			class="px-4 py-2 bg-blue-600 hover:bg-blue-700 text-white text-sm font-medium rounded-lg transition-colors cursor-pointer"
		>
			Reload Panel
		</button>
	{/if}

	{#if loading}
		<div class="text-gray-400">Checking for updates...</div>
	{:else if error}
		<div class="p-4 bg-red-900/50 border border-red-700 rounded-lg text-red-300">{error}</div>
	{:else if info}
		<div class="bg-gray-800 rounded-lg border border-gray-700 p-5">
			<div class="grid grid-cols-1 sm:grid-cols-2 gap-4 mb-4">
				<div>
					<span class="block text-xs text-gray-400 uppercase tracking-wider mb-1">Current Version</span>
					<span class="text-lg font-mono text-white">{info.current_version}</span>
				</div>
				<div>
					<span class="block text-xs text-gray-400 uppercase tracking-wider mb-1">Latest Commit</span>
					<span class="text-lg font-mono text-white">{info.latest_version}</span>
					{#if info.release_url}
						<a href={info.release_url} target="_blank" rel="noopener" class="ml-2 text-xs text-blue-400 hover:text-blue-300">View on GitHub</a>
					{/if}
				</div>
			</div>

			<div class="flex items-center gap-3 mb-4">
				{#if info.update_available}
					<span class="inline-flex items-center gap-1.5 px-2.5 py-0.5 rounded-full text-xs font-medium bg-yellow-900/50 text-yellow-400">
						<span class="w-1.5 h-1.5 rounded-full bg-yellow-400"></span>
						Update Available
					</span>
				{:else}
					<span class="inline-flex items-center gap-1.5 px-2.5 py-0.5 rounded-full text-xs font-medium bg-green-900/50 text-green-400">
						<span class="w-1.5 h-1.5 rounded-full bg-green-400"></span>
						Up to Date
					</span>
				{/if}
			</div>

			{#if info.update_available && !updateInProgress}
				{#if confirmUpdate}
					<div class="p-4 bg-yellow-950 border border-yellow-700 rounded-lg">
						<p class="text-yellow-300 text-sm font-medium mb-1">Confirm Update</p>
						<p class="text-yellow-400 text-xs mb-3">
							This will pull latest source, rebuild, and restart the panel.
							The process takes 2-5 minutes. Panel will be briefly unavailable during restart.
						</p>
						<div class="flex gap-2">
							<button
								onclick={performUpdate}
								class="px-4 py-2 bg-yellow-600 hover:bg-yellow-700 text-white text-sm font-medium rounded transition-colors cursor-pointer"
							>
								Confirm Update
							</button>
							<button
								onclick={() => (confirmUpdate = false)}
								class="px-4 py-2 bg-gray-600 hover:bg-gray-500 text-white text-sm rounded transition-colors cursor-pointer"
							>
								Cancel
							</button>
						</div>
					</div>
				{:else}
					<button
						onclick={() => (confirmUpdate = true)}
						class="px-4 py-2 bg-blue-600 hover:bg-blue-700 text-white text-sm font-medium rounded-lg transition-colors cursor-pointer"
					>
						Update Now
					</button>
				{/if}
			{/if}
		</div>
	{/if}

	<TaskProgress bind:taskId={currentTaskId} storageKey="jenderal_update_task" onComplete={onTaskComplete} />
</div>
