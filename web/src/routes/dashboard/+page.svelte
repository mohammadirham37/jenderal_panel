<script lang="ts">
	import { onMount, onDestroy } from 'svelte';
	import { api } from '$lib/api';
	import { connectMetrics, disconnectMetrics, currentMetrics } from '$lib/stores/metrics';
	import type { DashboardData, ServerInfo, ServerMetrics, ServiceStatus } from '$lib/types';

	let serverInfo = $state<ServerInfo | null>(null);
	let metrics = $state<ServerMetrics | null>(null);
	let services = $state<ServiceStatus[]>([]);
	let loading = $state(true);
	let error = $state('');

	function formatBytes(bytes: number): string {
		if (bytes === 0) return '0 B';
		const k = 1024;
		const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
		const i = Math.floor(Math.log(bytes) / Math.log(k));
		return parseFloat((bytes / Math.pow(k, i)).toFixed(1)) + ' ' + sizes[i];
	}

	function pct(used: number, total: number): number {
		if (total === 0) return 0;
		return Math.round((used / total) * 100);
	}

	onMount(async () => {
		try {
			const data = await api.get<DashboardData>('/api/v1/dashboard');
			serverInfo = data.server;
			metrics = data.metrics;
		} catch (err) {
			error = err instanceof Error ? err.message : 'Failed to load dashboard';
		} finally {
			loading = false;
		}

		// Load services
		try {
			const svcData = await api.get<ServiceStatus[]>('/api/v1/services');
			services = svcData || [];
		} catch {
			// Services may not be available
		}

		connectMetrics();
	});

	onDestroy(() => {
		disconnectMetrics();
	});

	// Use live metrics when available
	let liveMetrics = $derived($currentMetrics || metrics);
</script>

<div class="space-y-6">
	<h2 class="text-2xl font-bold text-white">Dashboard</h2>

	{#if loading}
		<div class="text-gray-400">Loading dashboard...</div>
	{:else if error}
		<div class="p-4 bg-red-900/50 border border-red-700 rounded-lg text-red-300">{error}</div>
	{:else}
		<!-- Server Info Cards -->
		{#if serverInfo}
			<div class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4">
				<div class="bg-gray-800 rounded-lg border border-gray-700 p-4">
					<div class="text-xs text-gray-400 uppercase tracking-wider">Hostname</div>
					<div class="text-lg font-semibold text-white mt-1">{serverInfo.hostname}</div>
				</div>
				<div class="bg-gray-800 rounded-lg border border-gray-700 p-4">
					<div class="text-xs text-gray-400 uppercase tracking-wider">IP Address</div>
					<div class="text-lg font-semibold text-white mt-1">{serverInfo.ip}</div>
				</div>
				<div class="bg-gray-800 rounded-lg border border-gray-700 p-4">
					<div class="text-xs text-gray-400 uppercase tracking-wider">OS</div>
					<div class="text-lg font-semibold text-white mt-1">{serverInfo.os}</div>
				</div>
				<div class="bg-gray-800 rounded-lg border border-gray-700 p-4">
					<div class="text-xs text-gray-400 uppercase tracking-wider">Kernel</div>
					<div class="text-lg font-semibold text-white mt-1">{serverInfo.kernel}</div>
				</div>
				<div class="bg-gray-800 rounded-lg border border-gray-700 p-4">
					<div class="text-xs text-gray-400 uppercase tracking-wider">Uptime</div>
					<div class="text-lg font-semibold text-white mt-1">{serverInfo.uptime}</div>
				</div>
				<div class="bg-gray-800 rounded-lg border border-gray-700 p-4">
					<div class="text-xs text-gray-400 uppercase tracking-wider">Timezone</div>
					<div class="text-lg font-semibold text-white mt-1">{serverInfo.timezone}</div>
				</div>
				<div class="bg-gray-800 rounded-lg border border-gray-700 p-4">
					<div class="text-xs text-gray-400 uppercase tracking-wider">CPU</div>
					<div class="text-lg font-semibold text-white mt-1">{serverInfo.cpu}</div>
				</div>
				<div class="bg-gray-800 rounded-lg border border-gray-700 p-4">
					<div class="text-xs text-gray-400 uppercase tracking-wider">Memory</div>
					<div class="text-lg font-semibold text-white mt-1">{serverInfo.ram}</div>
				</div>
			</div>
		{/if}

		<!-- Metrics -->
		{#if liveMetrics}
			<div class="bg-gray-800 rounded-lg border border-gray-700 p-6">
				<h3 class="text-lg font-semibold text-white mb-4">System Metrics</h3>
				<div class="space-y-4">
					<!-- CPU -->
					<div>
						<div class="flex justify-between text-sm mb-1">
							<span class="text-gray-400">CPU Usage</span>
							<span class="text-white font-medium">{liveMetrics.cpu.toFixed(1)}%</span>
						</div>
						<div class="w-full bg-gray-700 rounded-full h-3">
							<div
								class="h-3 rounded-full transition-all duration-500 {liveMetrics.cpu > 80
									? 'bg-red-500'
									: liveMetrics.cpu > 50
										? 'bg-yellow-500'
										: 'bg-green-500'}"
								style="width: {Math.min(liveMetrics.cpu, 100)}%"
							></div>
						</div>
					</div>

					<!-- RAM -->
					<div>
						<div class="flex justify-between text-sm mb-1">
							<span class="text-gray-400"
								>RAM ({formatBytes(liveMetrics.ram_used)} / {formatBytes(
									liveMetrics.ram_total
								)})</span
							>
							<span class="text-white font-medium">{pct(liveMetrics.ram_used, liveMetrics.ram_total)}%</span>
						</div>
						<div class="w-full bg-gray-700 rounded-full h-3">
							<div
								class="h-3 rounded-full transition-all duration-500 {pct(liveMetrics.ram_used, liveMetrics.ram_total) > 80
									? 'bg-red-500'
									: pct(liveMetrics.ram_used, liveMetrics.ram_total) > 50
										? 'bg-yellow-500'
										: 'bg-blue-500'}"
								style="width: {pct(liveMetrics.ram_used, liveMetrics.ram_total)}%"
							></div>
						</div>
					</div>

					<!-- Disk -->
					<div>
						<div class="flex justify-between text-sm mb-1">
							<span class="text-gray-400"
								>Disk ({formatBytes(liveMetrics.disk_used)} / {formatBytes(
									liveMetrics.disk_total
								)})</span
							>
							<span class="text-white font-medium">{pct(liveMetrics.disk_used, liveMetrics.disk_total)}%</span>
						</div>
						<div class="w-full bg-gray-700 rounded-full h-3">
							<div
								class="h-3 rounded-full transition-all duration-500 {pct(liveMetrics.disk_used, liveMetrics.disk_total) > 90
									? 'bg-red-500'
									: pct(liveMetrics.disk_used, liveMetrics.disk_total) > 70
										? 'bg-yellow-500'
										: 'bg-purple-500'}"
								style="width: {pct(liveMetrics.disk_used, liveMetrics.disk_total)}%"
							></div>
						</div>
					</div>

					<!-- Load Average -->
					<div class="flex gap-6 pt-2">
						<div>
							<span class="text-xs text-gray-400">Load 1m</span>
							<div class="text-white font-medium">{liveMetrics.load_1.toFixed(2)}</div>
						</div>
						<div>
							<span class="text-xs text-gray-400">Load 5m</span>
							<div class="text-white font-medium">{liveMetrics.load_5.toFixed(2)}</div>
						</div>
						<div>
							<span class="text-xs text-gray-400">Load 15m</span>
							<div class="text-white font-medium">{liveMetrics.load_15.toFixed(2)}</div>
						</div>
						<div>
							<span class="text-xs text-gray-400">Net RX</span>
							<div class="text-white font-medium">{formatBytes(liveMetrics.net_rx)}/s</div>
						</div>
						<div>
							<span class="text-xs text-gray-400">Net TX</span>
							<div class="text-white font-medium">{formatBytes(liveMetrics.net_tx)}/s</div>
						</div>
					</div>
				</div>
			</div>
		{/if}

		<!-- Services Status -->
		{#if services.length > 0}
			<div class="bg-gray-800 rounded-lg border border-gray-700 p-6">
				<h3 class="text-lg font-semibold text-white mb-4">Services</h3>
				<div class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-3">
					{#each services as svc}
						<div
							class="flex items-center justify-between bg-gray-750 rounded-lg border border-gray-600 px-4 py-3"
						>
							<span class="text-sm text-gray-200">{svc.name}</span>
							<span
								class="inline-flex items-center gap-1.5 px-2 py-0.5 rounded-full text-xs font-medium {svc.running
									? 'bg-green-900/50 text-green-400'
									: 'bg-red-900/50 text-red-400'}"
							>
								<span class="w-1.5 h-1.5 rounded-full {svc.running ? 'bg-green-400' : 'bg-red-400'}"></span>
								{svc.running ? 'Running' : 'Stopped'}
							</span>
						</div>
					{/each}
				</div>
			</div>
		{/if}
	{/if}
</div>
