<script lang="ts">
	import { onMount } from 'svelte';
	import { api } from '$lib/api';
	import type { ServerInfo } from '$lib/types';

	let info = $state<ServerInfo | null>(null);
	let loading = $state(true);
	let error = $state('');
	let actionMsg = $state('');

	let newHostname = $state('');
	let newTimezone = $state('');
	let editingHostname = $state(false);
	let editingTimezone = $state(false);

	async function loadInfo() {
		try {
			info = await api.get<ServerInfo>('/api/v1/server/info');
			newHostname = info.hostname;
			newTimezone = info.timezone;
		} catch (err) {
			error = err instanceof Error ? err.message : 'Failed to load server info';
		} finally {
			loading = false;
		}
	}

	async function handleReboot() {
		if (!confirm('Are you sure you want to reboot the server? This will disconnect all sessions.')) {
			return;
		}
		try {
			await api.post('/api/v1/server/reboot');
			actionMsg = 'Reboot initiated. Server will restart shortly.';
		} catch (err) {
			actionMsg = err instanceof Error ? err.message : 'Failed to initiate reboot';
		}
	}

	async function saveHostname() {
		try {
			await api.post('/api/v1/server/hostname', { hostname: newHostname });
			editingHostname = false;
			actionMsg = 'Hostname updated successfully.';
			await loadInfo();
		} catch (err) {
			actionMsg = err instanceof Error ? err.message : 'Failed to update hostname';
		}
	}

	async function saveTimezone() {
		try {
			await api.post('/api/v1/server/timezone', { timezone: newTimezone });
			editingTimezone = false;
			actionMsg = 'Timezone updated successfully.';
			await loadInfo();
		} catch (err) {
			actionMsg = err instanceof Error ? err.message : 'Failed to update timezone';
		}
	}

	onMount(loadInfo);
</script>

<div class="space-y-6">
	<div class="flex items-center justify-between">
		<h2 class="text-2xl font-bold text-white">Server</h2>
		<button
			onclick={handleReboot}
			class="px-4 py-2 bg-red-600 hover:bg-red-700 text-white text-sm font-medium rounded-lg transition-colors cursor-pointer"
		>
			Reboot Server
		</button>
	</div>

	{#if actionMsg}
		<div class="p-3 bg-blue-900/50 border border-blue-700 rounded-lg text-blue-300 text-sm">
			{actionMsg}
			<button onclick={() => (actionMsg = '')} class="ml-2 text-blue-400 hover:text-blue-200 cursor-pointer">
				Dismiss
			</button>
		</div>
	{/if}

	{#if loading}
		<div class="text-gray-400">Loading server info...</div>
	{:else if error}
		<div class="p-4 bg-red-900/50 border border-red-700 rounded-lg text-red-300">{error}</div>
	{:else if info}
		<div class="bg-gray-800 rounded-lg border border-gray-700 divide-y divide-gray-700">
			<!-- Hostname -->
			<div class="flex items-center justify-between p-4">
				<div>
					<div class="text-xs text-gray-400 uppercase tracking-wider">Hostname</div>
					{#if editingHostname}
						<div class="flex items-center gap-2 mt-1">
							<input
								type="text"
								bind:value={newHostname}
								class="px-2 py-1 bg-gray-700 border border-gray-600 rounded text-white text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
							/>
							<button
								onclick={saveHostname}
								class="px-2 py-1 bg-blue-600 hover:bg-blue-700 text-white text-xs rounded cursor-pointer"
							>
								Save
							</button>
							<button
								onclick={() => {
									editingHostname = false;
									newHostname = info!.hostname;
								}}
								class="px-2 py-1 bg-gray-600 hover:bg-gray-500 text-white text-xs rounded cursor-pointer"
							>
								Cancel
							</button>
						</div>
					{:else}
						<div class="text-white mt-1">{info.hostname}</div>
					{/if}
				</div>
				{#if !editingHostname}
					<button
						onclick={() => (editingHostname = true)}
						class="px-3 py-1.5 bg-gray-700 hover:bg-gray-600 text-gray-300 text-sm rounded cursor-pointer"
					>
						Edit
					</button>
				{/if}
			</div>

			<!-- IP -->
			<div class="p-4">
				<div class="text-xs text-gray-400 uppercase tracking-wider">IP Address</div>
				<div class="text-white mt-1">{info.ip}</div>
			</div>

			<!-- OS -->
			<div class="p-4">
				<div class="text-xs text-gray-400 uppercase tracking-wider">Operating System</div>
				<div class="text-white mt-1">{info.os}</div>
			</div>

			<!-- Kernel -->
			<div class="p-4">
				<div class="text-xs text-gray-400 uppercase tracking-wider">Kernel</div>
				<div class="text-white mt-1">{info.kernel}</div>
			</div>

			<!-- CPU -->
			<div class="p-4">
				<div class="text-xs text-gray-400 uppercase tracking-wider">CPU</div>
				<div class="text-white mt-1">{info.cpu}</div>
			</div>

			<!-- RAM -->
			<div class="p-4">
				<div class="text-xs text-gray-400 uppercase tracking-wider">Memory</div>
				<div class="text-white mt-1">{info.ram}</div>
			</div>

			<!-- Disk -->
			<div class="p-4">
				<div class="text-xs text-gray-400 uppercase tracking-wider">Disk</div>
				<div class="text-white mt-1">{info.disk}</div>
			</div>

			<!-- Uptime -->
			<div class="p-4">
				<div class="text-xs text-gray-400 uppercase tracking-wider">Uptime</div>
				<div class="text-white mt-1">{info.uptime}</div>
			</div>

			<!-- Timezone -->
			<div class="flex items-center justify-between p-4">
				<div>
					<div class="text-xs text-gray-400 uppercase tracking-wider">Timezone</div>
					{#if editingTimezone}
						<div class="flex items-center gap-2 mt-1">
							<input
								type="text"
								bind:value={newTimezone}
								placeholder="e.g. Asia/Jakarta"
								class="px-2 py-1 bg-gray-700 border border-gray-600 rounded text-white text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
							/>
							<button
								onclick={saveTimezone}
								class="px-2 py-1 bg-blue-600 hover:bg-blue-700 text-white text-xs rounded cursor-pointer"
							>
								Save
							</button>
							<button
								onclick={() => {
									editingTimezone = false;
									newTimezone = info!.timezone;
								}}
								class="px-2 py-1 bg-gray-600 hover:bg-gray-500 text-white text-xs rounded cursor-pointer"
							>
								Cancel
							</button>
						</div>
					{:else}
						<div class="text-white mt-1">{info.timezone}</div>
					{/if}
				</div>
				{#if !editingTimezone}
					<button
						onclick={() => (editingTimezone = true)}
						class="px-3 py-1.5 bg-gray-700 hover:bg-gray-600 text-gray-300 text-sm rounded cursor-pointer"
					>
						Edit
					</button>
				{/if}
			</div>
		</div>
	{/if}
</div>
