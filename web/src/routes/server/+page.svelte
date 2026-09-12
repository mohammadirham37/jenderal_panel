<script lang="ts">
	import { onMount } from 'svelte';
	import { api } from '$lib/api';
	import type { ServerInfo } from '$lib/types';
	import { hasPermission, permissions } from '$lib/stores/auth';
import { toast } from '$lib/stores/toast';
import { language, translate } from '$lib/stores/language';

	let info = $state<ServerInfo | null>(null);
	let loading = $state(true);
	let error = $state('');

	let newHostname = $state('');
	let newTimezone = $state('');
	let editingHostname = $state(false);
	let editingTimezone = $state(false);

	// SSH port management (admin, two-phase)
	interface SSHPortStatus {
		current_port: number;
		panel_port: number;
		pending_change: { old_port: number; new_port: number } | null;
	}
	let canManageSSH = $derived(hasPermission($permissions, 'ssh.view'));
	let canChangeSSH = $derived(hasPermission($permissions, 'ssh.manage'));
	let sshPort = $state<SSHPortStatus | null>(null);
	let sshPortLoading = $state(false);
	let newSSHPort = $state('');
	let sshPortBusy = $state(false);
	let sshPortError = $state('');
	let sshPortMsg = $state('');

	async function loadSSHPort() {
		if (!canManageSSH) return;
		sshPortLoading = true;
		try {
			sshPort = await api.get<SSHPortStatus>('/api/v1/ssh/port');
			sshPortError = '';
		} catch (err) {
			sshPort = null;
			sshPortError = err instanceof Error ? err.message : translate($language, 'srv.errorLoadSSHPort');
		} finally {
			sshPortLoading = false;
		}
	}

	async function beginSSHPortChange() {
		const port = Number(newSSHPort);
		if (!port || sshPortBusy) return;
		if (!confirm(
			translate($language, 'srv.confirmChangePort')
				.replace('{old}', String(sshPort?.current_port))
				.replace('{new}', String(port))
		)) return;
		sshPortBusy = true;
		sshPortMsg = '';
		sshPortError = '';
		try {
			const result = await api.post<{ message: string }>('/api/v1/ssh/port/change', { port });
			sshPortMsg = result.message || translate($language, 'srv.msgPortChangeStarted');
			newSSHPort = '';
			await loadSSHPort();
		} catch (err) {
			sshPortError = err instanceof Error ? err.message : translate($language, 'srv.errorChangePort');
		} finally {
			sshPortBusy = false;
		}
	}

	async function finalizeSSHPortChange() {
		if (!confirm(translate($language, 'srv.confirmFinalize'))) return;
		sshPortBusy = true;
		sshPortMsg = '';
		sshPortError = '';
		try {
			await api.post('/api/v1/ssh/port/change/finalize');
			sshPortMsg = translate($language, 'srv.msgFinalized');
			await loadSSHPort();
		} catch (err) {
			sshPortError = err instanceof Error ? err.message : translate($language, 'srv.errorFinalize');
		} finally {
			sshPortBusy = false;
		}
	}

	async function cancelSSHPortChange() {
		if (!confirm(translate($language, 'srv.confirmCancelChange'))) return;
		sshPortBusy = true;
		sshPortMsg = '';
		sshPortError = '';
		try {
			await api.del('/api/v1/ssh/port/change');
			sshPortMsg = translate($language, 'srv.msgCancelled');
			await loadSSHPort();
		} catch (err) {
			sshPortError = err instanceof Error ? err.message : translate($language, 'srv.errorCancel');
		} finally {
			sshPortBusy = false;
		}
	}

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
			error = err instanceof Error ? err.message : translate($language, 'srv.errorLoadInfo');
		} finally {
			loading = false;
		}
	}

	async function handleReboot() {
		if (!confirm(translate($language, 'srv.confirmReboot'))) {
			return;
		}
		try {
			await api.post('/api/v1/server/reboot');
			toast.success(translate($language, 'srv.msgReboot'));
		} catch (err) {
			toast.error(err instanceof Error ? err.message : translate($language, 'srv.errorReboot'));
		}
	}

	async function saveHostname() {
		try {
			await api.post('/api/v1/server/hostname', { hostname: newHostname });
			editingHostname = false;
			toast.success(translate($language, 'srv.msgHostname'));
			await loadInfo();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : translate($language, 'srv.errorHostname'));
		}
	}

	async function saveTimezone() {
		tzOpen = false;
		try {
			await api.post('/api/v1/server/timezone', { timezone: newTimezone });
			editingTimezone = false;
			toast.success(translate($language, 'srv.msgTimezone'));
			await loadInfo();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : translate($language, 'srv.errorTimezone'));
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

	onMount(() => {
		loadInfo();
		loadSSHPort();
	});
</script>

<svelte:window onclick={handleWindowClick} />

<div class="space-y-6">
	<div class="flex items-center justify-between">
		<h2 class="text-2xl font-bold text-white">{translate($language, 'srv.title')}</h2>
		<button
			onclick={handleReboot}
			class="px-4 py-2 bg-red-600 hover:bg-red-700 text-white text-sm font-medium rounded-lg transition-colors cursor-pointer"
		>
			{translate($language, 'srv.rebootServer')}
		</button>
	</div>


	{#if loading}
		<div class="text-gray-400">{translate($language, 'srv.loading')}</div>
	{:else if error}
		<div class="p-4 bg-red-900/50 border border-red-700 rounded-lg text-red-300">{error}</div>
	{:else if info}
		<div class="bg-gray-800 rounded-lg border border-gray-700 divide-y divide-gray-700">
			<!-- Hostname -->
			<div class="flex items-center justify-between p-4">
				<div>
					<div class="text-xs text-gray-400 uppercase tracking-wider">{translate($language, 'srv.hostname')}</div>
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
								{translate($language, 'srv.save')}
							</button>
							<button
								onclick={() => {
									editingHostname = false;
									newHostname = info!.hostname;
								}}
								class="px-2 py-1 bg-gray-600 hover:bg-gray-500 text-white text-xs rounded cursor-pointer"
							>
								{translate($language, 'srv.cancel')}
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
							{translate($language, 'srv.edit')}
						</button>
				{/if}
			</div>

			<!-- IP -->
			<div class="p-4">
				<div class="text-xs text-gray-400 uppercase tracking-wider">{translate($language, 'srv.ipAddress')}</div>
				<div class="text-white mt-1">{info.ip}</div>
			</div>

			<!-- OS -->
			<div class="p-4">
				<div class="text-xs text-gray-400 uppercase tracking-wider">{translate($language, 'srv.os')}</div>
				<div class="text-white mt-1">{info.os}</div>
			</div>

			<!-- Kernel -->
			<div class="p-4">
				<div class="text-xs text-gray-400 uppercase tracking-wider">{translate($language, 'srv.kernel')}</div>
				<div class="text-white mt-1">{info.kernel}</div>
			</div>

			<!-- CPU -->
			<div class="p-4">
				<div class="text-xs text-gray-400 uppercase tracking-wider">{translate($language, 'srv.cpu')}</div>
				<div class="text-white mt-1">{info.cpu}</div>
			</div>

			<!-- CPU Model -->
			{#if info.cpu_model}
				<div class="p-4">
					<div class="text-xs text-gray-400 uppercase tracking-wider">{translate($language, 'srv.cpuModel')}</div>
					<div class="text-white mt-1">{info.cpu_model}</div>
				</div>
			{/if}

			<!-- CPU Cores -->
			{#if info.cpu_cores}
				<div class="p-4">
					<div class="text-xs text-gray-400 uppercase tracking-wider">{translate($language, 'srv.cpuCores')}</div>
					<div class="text-white mt-1">{info.cpu_cores}</div>
				</div>
			{/if}

			<!-- RAM -->
			<div class="p-4">
				<div class="text-xs text-gray-400 uppercase tracking-wider">{translate($language, 'srv.memory')}</div>
				<div class="text-white mt-1">{info.ram}</div>
			</div>

			<!-- Disk -->
			<div class="p-4">
				<div class="text-xs text-gray-400 uppercase tracking-wider">{translate($language, 'srv.disk')}</div>
				<div class="text-white mt-1">{info.disk}</div>
			</div>

			<!-- Uptime -->
			<div class="p-4">
				<div class="text-xs text-gray-400 uppercase tracking-wider">{translate($language, 'srv.uptime')}</div>
				<div class="text-white mt-1">{info.uptime}</div>
			</div>

			<!-- Timezone -->
			<div class="flex items-center justify-between p-4">
				<div>
					<div class="text-xs text-gray-400 uppercase tracking-wider">{translate($language, 'srv.timezone')}</div>
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
										placeholder={translate($language, 'srv.searchTimezone')}
										aria-label={translate($language, 'srv.searchTimezoneAria')}
										autocomplete="off"
										class="w-64 pl-8 pr-3 py-1 bg-gray-700 border border-gray-600 rounded text-white text-sm placeholder:text-gray-400 focus:outline-none focus:ring-2 focus:ring-blue-500"
									/>
								</div>
								{#if tzOpen}
									<ul
										class="absolute z-20 mt-1 max-h-60 w-72 overflow-y-auto rounded-lg border border-gray-600 bg-gray-800 py-1 shadow-lg"
										role="listbox"
										aria-label={translate($language, 'srv.timezoneOptions')}
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
											<li class="px-3 py-2 text-sm text-gray-400">{translate($language, 'srv.noMatchingTimezone')}</li>
										{/each}
									</ul>
								{/if}
									<p class="mt-1 text-xs text-gray-400">
										{translate($language, 'srv.current')} <span class="text-gray-300">{newTimezone}</span>
									</p>
							</div>
							<button
								onclick={saveTimezone}
								class="px-2 py-1 bg-blue-600 hover:bg-blue-700 text-white text-xs rounded cursor-pointer"
							>
								{translate($language, 'srv.save')}
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
								{translate($language, 'srv.cancel')}
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
							{translate($language, 'srv.edit')}
						</button>
				{/if}
			</div>
		</div>

		<!-- SSH Port Management -->
		{#if canManageSSH}
			<div class="bg-gray-800 rounded-lg border border-gray-700 p-5">
				<h3 class="text-lg font-semibold text-white mb-3">{translate($language, 'srv.sshPort')}</h3>

				{#if sshPortMsg}
					<div class="mb-3 p-3 bg-green-900/50 border border-green-700 rounded-lg text-green-300 text-sm">
						{sshPortMsg}
						<button onclick={() => (sshPortMsg = '')} class="ml-2 text-green-400 hover:text-green-200 cursor-pointer">{translate($language, 'srv.dismiss')}</button>
					</div>
				{/if}

				{#if sshPortError}
					<div class="mb-3 p-3 bg-red-900/50 border border-red-700 rounded-lg text-red-300 text-sm">
						{sshPortError}
						<button onclick={() => (sshPortError = '')} class="ml-2 text-red-400 hover:text-red-200 cursor-pointer">{translate($language, 'srv.dismiss')}</button>
					</div>
				{/if}

				{#if sshPortLoading}
					<div class="text-gray-400 text-sm">{translate($language, 'srv.loadingSSHPort')}</div>
				{:else if sshPort}
					<div class="flex flex-wrap gap-4 text-sm text-gray-400 mb-4">
						<span>{translate($language, 'srv.effectivePort')} <span class="text-white font-mono">{sshPort.current_port}</span></span>
						<span>{translate($language, 'srv.panelPort')} <span class="text-white font-mono">{sshPort.panel_port}</span></span>
					</div>

					{#if sshPort.pending_change}
						<div class="p-3 bg-yellow-900/30 border border-yellow-700 rounded-lg text-sm text-yellow-300">
							<p class="font-medium mb-2">
								{translate($language, 'srv.pendingTitle').replace('{old}', String(sshPort.pending_change.old_port)).replace('{new}', String(sshPort.pending_change.new_port))}
							</p>
							<p class="mb-3 text-yellow-200/80">
								{translate($language, 'srv.pendingVerify').replace('{port}', String(sshPort.pending_change.new_port))}
							</p>
							{#if canChangeSSH}
								<div class="flex gap-2">
									<button
										onclick={finalizeSSHPortChange}
										disabled={sshPortBusy}
										class="px-3 py-1.5 bg-green-600 hover:bg-green-700 disabled:opacity-50 text-white text-xs rounded transition-colors cursor-pointer"
									>
										{translate($language, 'srv.finalize')}
									</button>
									<button
										onclick={cancelSSHPortChange}
										disabled={sshPortBusy}
										class="px-3 py-1.5 bg-gray-700 hover:bg-gray-600 disabled:opacity-50 text-gray-200 text-xs rounded transition-colors cursor-pointer"
									>
										{translate($language, 'srv.cancelChange')}
									</button>
								</div>
							{/if}
						</div>
					{:else if canChangeSSH}
						<div class="flex flex-wrap items-end gap-3">
							<div>
								<label for="new-ssh-port" class="block text-xs text-gray-400 uppercase tracking-wider mb-1">{translate($language, 'srv.newPort')}</label>
								<input
									id="new-ssh-port"
									type="number"
									min="1"
									max="65535"
									bind:value={newSSHPort}
									placeholder="2222"
									class="px-3 py-2 bg-gray-900 border border-gray-600 rounded text-white text-sm font-mono focus:outline-none focus:ring-2 focus:ring-blue-500"
								/>
							</div>
							<button
								onclick={beginSSHPortChange}
								disabled={sshPortBusy || !newSSHPort}
								class="px-4 py-2 bg-blue-600 hover:bg-blue-700 disabled:opacity-50 text-white text-sm font-medium rounded transition-colors cursor-pointer"
							>
								{sshPortBusy ? translate($language, 'srv.working') : translate($language, 'srv.changePort')}
							</button>
						</div>
						<p class="mt-2 text-xs text-gray-500">
							{translate($language, 'srv.twoPhaseHint')}
						</p>
					{/if}
				{/if}
			</div>
		{/if}

		<!-- Disk Partitions -->
		{#if info.disk_partitions && info.disk_partitions.length > 0}
			<div class="bg-gray-800 rounded-lg border border-gray-700 overflow-hidden">
				<div class="px-4 py-3 border-b border-gray-700">
					<h3 class="text-sm font-semibold text-white">{translate($language, 'srv.diskPartitions')}</h3>
				</div>
				<div class="overflow-x-auto">
					<table class="w-full">
						<thead>
							<tr class="border-b border-gray-700 bg-gray-800/80">
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">{translate($language, 'srv.device')}</th>
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">{translate($language, 'srv.mount')}</th>
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">{translate($language, 'srv.size')}</th>
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">{translate($language, 'srv.used')}</th>
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">{translate($language, 'srv.available')}</th>
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">{translate($language, 'srv.usePercent')}</th>
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
					<h3 class="text-sm font-semibold text-white">{translate($language, 'srv.networkInterfaces')}</h3>
				</div>
				<div class="overflow-x-auto">
					<table class="w-full">
						<thead>
							<tr class="border-b border-gray-700 bg-gray-800/80">
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">{translate($language, 'srv.name')}</th>
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">{translate($language, 'srv.ipAddress')}</th>
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">{translate($language, 'srv.macAddress')}</th>
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
