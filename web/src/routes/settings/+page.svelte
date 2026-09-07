<script lang="ts">
	import { onMount } from 'svelte';
	import { api } from '$lib/api';
	import type { Setting } from '$lib/types';

	let settings = $state<Setting[]>([]);
	let loading = $state(true);
	let error = $state('');
	let saving = $state(false);
	let actionMsg = $state('');
	let actionError = $state('');

	// Track edited values
	let editedValues = $state<Record<string, string>>({});

	async function loadSettings() {
		try {
			settings = (await api.get<Setting[]>('/api/v1/settings')) || [];
			// Initialize edited values
			const vals: Record<string, string> = {};
			for (const s of settings) {
				vals[s.key] = s.value;
			}
			editedValues = vals;
		} catch (err) {
			error = err instanceof Error ? err.message : 'Failed to load settings';
		} finally {
			loading = false;
		}
	}

	async function saveSettings() {
		saving = true;
		actionMsg = '';
		actionError = '';

		try {
			// Build diff: only send changed values
			const payload: Record<string, string> = {};
			for (const s of settings) {
				if (editedValues[s.key] !== s.value) {
					payload[s.key] = editedValues[s.key];
				}
			}

			if (Object.keys(payload).length === 0) {
				actionMsg = 'No changes to save.';
				saving = false;
				return;
			}

			await api.put('/api/v1/settings', payload);
			actionMsg = 'Settings saved successfully.';
			await loadSettings();
		} catch (err) {
			actionError = err instanceof Error ? err.message : 'Failed to save settings';
		} finally {
			saving = false;
		}
	}

	onMount(loadSettings);
</script>

<div class="space-y-6">
	<div class="flex items-center justify-between">
		<h2 class="text-2xl font-bold text-white">Settings</h2>
		<button
			onclick={saveSettings}
			disabled={saving}
			class="px-4 py-2 bg-blue-600 hover:bg-blue-700 disabled:opacity-50 text-white text-sm font-medium rounded-lg transition-colors cursor-pointer"
		>
			{saving ? 'Saving...' : 'Save Changes'}
		</button>
	</div>

	{#if actionMsg}
		<div class="p-3 bg-green-900/50 border border-green-700 rounded-lg text-green-300 text-sm">
			{actionMsg}
			<button onclick={() => (actionMsg = '')} class="ml-2 text-green-400 hover:text-green-200 cursor-pointer">
				Dismiss
			</button>
		</div>
	{/if}

	{#if actionError}
		<div class="p-3 bg-red-900/50 border border-red-700 rounded-lg text-red-300 text-sm">
			{actionError}
			<button onclick={() => (actionError = '')} class="ml-2 text-red-400 hover:text-red-200 cursor-pointer">
				Dismiss
			</button>
		</div>
	{/if}

	{#if loading}
		<div class="text-gray-400">Loading settings...</div>
	{:else if error}
		<div class="p-4 bg-red-900/50 border border-red-700 rounded-lg text-red-300">{error}</div>
	{:else if settings.length === 0}
		<div class="bg-gray-800 rounded-lg border border-gray-700 p-8 text-center text-gray-400">
			No settings found.
		</div>
	{:else}
		<div class="bg-gray-800 rounded-lg border border-gray-700 divide-y divide-gray-700">
			{#each settings as setting}
				<div class="flex items-center gap-4 p-4">
					<div class="w-48 shrink-0">
						<label for="setting-{setting.key}" class="text-sm font-medium text-gray-300 font-mono">
							{setting.key}
						</label>
					</div>
					<div class="flex-1">
						<input
							id="setting-{setting.key}"
							type="text"
							bind:value={editedValues[setting.key]}
							class="w-full px-3 py-2 bg-gray-700 border border-gray-600 rounded-lg text-white text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
						/>
					</div>
					<div class="text-xs text-gray-500 w-40 shrink-0 text-right">
						{new Date(setting.updated_at).toLocaleString()}
					</div>
				</div>
			{/each}
		</div>
	{/if}
</div>
