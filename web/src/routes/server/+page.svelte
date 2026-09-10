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

	// Searchable timezone combobox state
	let tzQuery = $state('');
	let tzOpen = $state(false);
	let tzHighlight = $state(0);
	let tzEditRoot: HTMLElement | undefined = $state();

	const fallbackTimezones = [
		'Asia/Jakarta',
		'Asia/Makassar',
		'Asia/Jayapura',
		'Asia/Singapore',
		'Asia/Kuala_Lumpur',
		'Asia/Bangkok',
		'Asia/Manila',
		'Asia/Tokyo',
		'Asia/Seoul',
		'Asia/Shanghai',
		'Asia/Kolkata',
		'Asia/Dubai',
		'Australia/Perth',
		'Australia/Sydney',
		'Europe/London',
		'Europe/Paris',
		'Europe/Berlin',
		'Europe/Moscow',
		'America/New_York',
		'America/Chicago',
		'America/Denver',
		'America/Los_Angeles',
		'UTC'
	];

	// Full IANA list when the browser provides it; keeps users from typos.
	const timezoneOptions: string[] = (() => {
		try {
			const supported = (Intl as unknown as { supportedValuesOf?: (kind: 'timeZone') => string[] }).supportedValuesOf?.('timeZone');
			if (supported && supported.length > 0) return supported;
		} catch { /* older runtime */ }
		return fallbackTimezones;
	})();

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
		tzOpen = false;
		try {
			await api.post('/api/v1/server/timezone', { timezone: newTimezone });
			editingTimezone = false;
			actionMsg = 'Timezone updated successfully.';
			await loadInfo();
		} catch (err) {
			actionMsg = err instanceof Error ? err.message : 'Failed to update timezone';
		}
	}

	// Keep the rendered list light; searching narrows to the wanted entry.
	let filteredTimezones = $derived.by(() => {
		const q = tzQuery.trim().toLowerCase();
		const list = q ? timezoneOptions.filter((tz) => tz.toLowerCase().includes(q)) : timezoneOptions;
		return list.slice(0, 100);
	});

	function openTzList() {
		tzOpen = true;
		const idx = filteredTimezones.indexOf(newTimezone);
		tzHighlight = idx >= 0 ? idx : 0;
	}

	function selectTz(tz: string) {
		newTimezone = tz;
		tzQuery = '';
		tzOpen = false;
	}

	function handleTzKeydown(e: KeyboardEvent) {
		if (e.key === 'ArrowDown') {
			e.preventDefault();
			if (!tzOpen) openTzList();
			else tzHighlight = Math.min(tzHighlight + 1, filteredTimezones.length - 1);
		} else if (e.key === 'ArrowUp' && tzOpen) {
			e.preventDefault();
			tzHighlight = Math.max(tzHighlight - 1, 0);
		} else if (e.key === 'Enter' && tzOpen) {
			e.preventDefault();
			const tz = filteredTimezones[tzHighlight];
			if (tz) selectTz(tz);
		} else if (e.key === 'Escape') {
			tzOpen = false;
		}
	}

	function handleWindowClick(e: MouseEvent) {
		if (tzOpen && tzEditRoot && !tzEditRoot.contains(e.target as Node)) {
			tzOpen = false;
		}
	}

	onMount(loadInfo);
</script>

<svelte:window onclick={handleWindowClick} />

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

			<!-- CPU Model -->
			{#if info.cpu_model}
				<div class="p-4">
					<div class="text-xs text-gray-400 uppercase tracking-wider">CPU Model</div>
					<div class="text-white mt-1">{info.cpu_model}</div>
				</div>
			{/if}

			<!-- CPU Cores -->
			{#if info.cpu_cores}
				<div class="p-4">
					<div class="text-xs text-gray-400 uppercase tracking-wider">CPU Cores</div>
					<div class="text-white mt-1">{info.cpu_cores}</div>
				</div>
			{/if}

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
						<div class="flex items-start gap-2 mt-1" bind:this={tzEditRoot}>
							<div class="relative">
								<div class="relative">
									<svg
										class="pointer-events-none absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-gray-400"
										fill="none"
										stroke="currentColor"
										viewBox="0 0 24 24"
										stroke-width="2"
										aria-hidden="true"
									>
										<path stroke-linecap="round" stroke-linejoin="round" d="M21 21l-4.35-4.35M17 10.5a6.5 6.5 0 11-13 0 6.5 6.5 0 0113 0z" />
									</svg>
									<input
										type="text"
										bind:value={tzQuery}
										onfocus={openTzList}
										oninput={() => {
											tzOpen = true;
											tzHighlight = 0;
										}}
										onkeydown={handleTzKeydown}
										placeholder="Search timezone..."
										aria-label="Search timezone"
										autocomplete="off"
										class="w-64 pl-8 pr-3 py-1 bg-gray-700 border border-gray-600 rounded text-white text-sm placeholder:text-gray-400 focus:outline-none focus:ring-2 focus:ring-blue-500"
									/>
								</div>
								{#if tzOpen}
									<ul
										class="absolute z-20 mt-1 max-h-60 w-72 overflow-y-auto rounded-lg border border-gray-600 bg-gray-800 py-1 shadow-lg"
										role="listbox"
										aria-label="Timezone options"
									>
										{#each filteredTimezones as tz, i}
											<li>
												<button
													type="button"
													onclick={() => selectTz(tz)}
													role="option"
													aria-selected={tz === newTimezone}
													class="flex w-full items-center justify-between gap-2 px-3 py-1.5 text-left text-sm cursor-pointer
													{i === tzHighlight ? 'bg-blue-600/70 text-white' : 'text-gray-200 hover:bg-gray-700'}
													{tz === newTimezone ? 'font-medium' : ''}"
												>
													<span>{tz}</span>
													{#if tz === newTimezone}
														<svg class="h-3.5 w-3.5 shrink-0 text-blue-300" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="2.5" aria-hidden="true">
															<path stroke-linecap="round" stroke-linejoin="round" d="M4.5 12.75l6 6 9-13.5" />
														</svg>
													{/if}
												</button>
											</li>
										{:else}
											<li class="px-3 py-2 text-sm text-gray-400">No matching timezone</li>
										{/each}
									</ul>
								{/if}
								<p class="mt-1 text-xs text-gray-400">
									Current: <span class="text-gray-300">{newTimezone}</span>
								</p>
							</div>
							<button
								onclick={saveTimezone}
								class="px-2 py-1 bg-blue-600 hover:bg-blue-700 text-white text-xs rounded cursor-pointer"
							>
								Save
							</button>
							<button
								onclick={() => {
									editingTimezone = false;
									tzOpen = false;
									tzQuery = '';
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

		<!-- Disk Partitions -->
		{#if info.disk_partitions && info.disk_partitions.length > 0}
			<div class="bg-gray-800 rounded-lg border border-gray-700 overflow-hidden">
				<div class="px-4 py-3 border-b border-gray-700">
					<h3 class="text-sm font-semibold text-white">Disk Partitions</h3>
				</div>
				<div class="overflow-x-auto">
					<table class="w-full">
						<thead>
							<tr class="border-b border-gray-700 bg-gray-800/80">
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Device</th>
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Mount</th>
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Size</th>
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Used</th>
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Available</th>
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Use%</th>
							</tr>
						</thead>
						<tbody class="divide-y divide-gray-700">
							{#each info.disk_partitions as partition}
								<tr class="hover:bg-gray-750">
									<td class="px-4 py-2 text-sm text-white font-mono">{partition.device}</td>
									<td class="px-4 py-2 text-sm text-gray-300 font-mono">{partition.mount}</td>
									<td class="px-4 py-2 text-sm text-gray-300">{partition.size}</td>
									<td class="px-4 py-2 text-sm text-gray-300">{partition.used}</td>
									<td class="px-4 py-2 text-sm text-gray-300">{partition.available}</td>
									<td class="px-4 py-2 text-sm text-gray-300">{partition.use_percent}</td>
								</tr>
							{/each}
						</tbody>
					</table>
				</div>
			</div>
		{/if}

		<!-- Network Interfaces -->
		{#if info.network_interfaces && info.network_interfaces.length > 0}
			<div class="bg-gray-800 rounded-lg border border-gray-700 overflow-hidden">
				<div class="px-4 py-3 border-b border-gray-700">
					<h3 class="text-sm font-semibold text-white">Network Interfaces</h3>
				</div>
				<div class="overflow-x-auto">
					<table class="w-full">
						<thead>
							<tr class="border-b border-gray-700 bg-gray-800/80">
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Name</th>
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">IP Address</th>
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">MAC Address</th>
							</tr>
						</thead>
						<tbody class="divide-y divide-gray-700">
							{#each info.network_interfaces as iface}
								<tr class="hover:bg-gray-750">
									<td class="px-4 py-2 text-sm text-white font-mono">{iface.name}</td>
									<td class="px-4 py-2 text-sm text-gray-300 font-mono">{iface.ip || '-'}</td>
									<td class="px-4 py-2 text-sm text-gray-300 font-mono">{iface.mac || '-'}</td>
								</tr>
							{/each}
						</tbody>
					</table>
				</div>
			</div>
		{/if}
	{/if}
</div>
