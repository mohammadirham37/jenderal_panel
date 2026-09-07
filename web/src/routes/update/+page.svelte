<script lang="ts">
	import { onMount } from 'svelte';
	import { api } from '$lib/api';

	interface UpdateInfo {
		current_version: string;
		latest_version: string;
		update_available: boolean;
		release_notes?: string;
	}

	let info = $state<UpdateInfo | null>(null);
	let loading = $state(true);
	let error = $state('');
	let updating = $state(false);
	let updateMsg = $state('');
	let updateError = $state('');
	let confirmUpdate = $state(false);

	async function checkUpdate() {
		loading = true;
		error = '';
		try {
			info = await api.get<UpdateInfo>('/api/v1/update/check');
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
		try {
			await api.post('/api/v1/update/apply');
			updateMsg = 'Update applied successfully. The panel may restart shortly.';
			await checkUpdate();
		} catch (err) {
			updateError = err instanceof Error ? err.message : 'Failed to apply update';
		} finally {
			updating = false;
		}
	}

	onMount(checkUpdate);
</script>

<div class="space-y-6">
	<div class="flex items-center justify-between">
		<h2 class="text-2xl font-bold text-white">Update</h2>
		<button
			onclick={checkUpdate}
			disabled={loading}
			class="px-4 py-2 bg-gray-700 hover:bg-gray-600 disabled:opacity-50 text-gray-300 text-sm rounded-lg transition-colors cursor-pointer"
		>
			{loading ? 'Checking...' : 'Check Again'}
		</button>
	</div>

	{#if updateMsg}
		<div class="p-3 bg-green-900/50 border border-green-700 rounded-lg text-green-300 text-sm">
			{updateMsg}
			<button onclick={() => (updateMsg = '')} class="ml-2 text-green-400 hover:text-green-200 cursor-pointer">Dismiss</button>
		</div>
	{/if}

	{#if updateError}
		<div class="p-3 bg-red-900/50 border border-red-700 rounded-lg text-red-300 text-sm">
			{updateError}
			<button onclick={() => (updateError = '')} class="ml-2 text-red-400 hover:text-red-200 cursor-pointer">Dismiss</button>
		</div>
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
					<span class="block text-xs text-gray-400 uppercase tracking-wider mb-1">Latest Version</span>
					<span class="text-lg font-mono text-white">{info.latest_version}</span>
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

			{#if info.release_notes}
				<div class="mb-4">
					<span class="block text-xs text-gray-400 uppercase tracking-wider mb-1">Release Notes</span>
					<div class="p-3 bg-gray-900 rounded text-sm text-gray-300 whitespace-pre-wrap font-mono">{info.release_notes}</div>
				</div>
			{/if}

			{#if info.update_available}
				{#if confirmUpdate}
					<div class="p-4 bg-yellow-950 border border-yellow-700 rounded-lg">
						<p class="text-yellow-300 text-sm font-medium mb-1">Confirm Update</p>
						<p class="text-yellow-400 text-xs mb-3">This will update the panel from {info.current_version} to {info.latest_version}. The panel may restart during the update.</p>
						<div class="flex gap-2">
							<button
								onclick={performUpdate}
								disabled={updating}
								class="px-4 py-2 bg-yellow-600 hover:bg-yellow-700 disabled:opacity-50 text-white text-sm font-medium rounded transition-colors cursor-pointer"
							>
								{updating ? 'Updating...' : 'Confirm Update'}
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
						disabled={updating}
						class="px-4 py-2 bg-blue-600 hover:bg-blue-700 disabled:opacity-50 text-white text-sm font-medium rounded-lg transition-colors cursor-pointer"
					>
						Update Now
					</button>
				{/if}
			{/if}
		</div>
	{/if}
</div>
