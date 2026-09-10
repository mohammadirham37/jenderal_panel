<script lang="ts">
	import { onMount, onDestroy } from 'svelte';
	import { api } from '$lib/api';
	import { connectMetrics, disconnectMetrics, currentMetrics } from '$lib/stores/metrics';
	import { language, translate } from '$lib/stores/language';
	import type { DashboardData, ServerInfo, ServerMetrics, ServiceStatus } from '$lib/types';

	interface SSLCertLite {
		id: string;
		domain: string;
		issuer: string;
		status: string;
		expires_at: string;
	}

	interface AlertEventLite {
		id: string;
		metric: string;
		value: number;
		message: string;
		resolved: boolean;
		created_at: string;
	}

	interface WebsiteLite {
		id: string;
		domain: string;
		status: string;
	}

	interface DetailRow {
		label: string;
		value: string;
	}

	let serverInfo = $state<ServerInfo | null>(null);
	let metricsSnapshot = $state<ServerMetrics | null>(null);
	let services = $state<ServiceStatus[]>([]);
	let history = $state<ServerMetrics[]>([]);
	let websites = $state<WebsiteLite[] | null>(null);
	let databases = $state<unknown[] | null>(null);
	let backups = $state<unknown[] | null>(null);
	let certificates = $state<SSLCertLite[] | null>(null);
	let alertEvents = $state<AlertEventLite[] | null>(null);
	let loading = $state(true);
	let refreshing = $state(false);
	let error = $state('');
	let lastUpdated = $state<Date | null>(null);
	let now = $state(Date.now());

	let clockTimer: ReturnType<typeof setInterval> | undefined;

	const GAUGE_CIRC = 188.5;

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

	function gaugeTone(value: number | null, warn: number, crit: number): string {
		if (value === null) return 'text-gray-500';
		if (value >= crit) return 'text-red-500';
		if (value >= warn) return 'text-yellow-400';
		return 'text-blue-400';
	}

	function barTone(percent: number): string {
		if (percent >= 90) return 'bg-red-500';
		if (percent >= 70) return 'bg-yellow-400';
		return 'bg-blue-400';
	}

	function sparkLine(values: number[], w: number, h: number, pad = 2): string {
		if (values.length < 2) return '';
		const max = Math.max(...values);
		const min = Math.min(...values);
		const span = max - min || 1;
		const stepX = (w - pad * 2) / (values.length - 1);
		return values
			.map((v, i) => {
				const x = pad + i * stepX;
				const y = pad + (1 - (v - min) / span) * (h - pad * 2);
				return `${i === 0 ? 'M' : 'L'}${x.toFixed(1)},${y.toFixed(1)}`;
			})
			.join(' ');
	}

	function sparkArea(values: number[], w: number, h: number, pad = 2): string {
		const line = sparkLine(values, w, h, pad);
		if (!line) return '';
		return `${line} L${w - pad},${h} L${pad},${h} Z`;
	}

	function chartLine(values: number[], w: number, h: number, max: number): string {
		if (values.length < 2) return '';
		const stepX = w / (values.length - 1);
		return values
			.map((v, i) => {
				const x = i * stepX;
				const y = h - (Math.min(v, max) / max) * h;
				return `${i === 0 ? 'M' : 'L'}${x.toFixed(1)},${y.toFixed(1)}`;
			})
			.join(' ');
	}

	function chartArea(values: number[], w: number, h: number, max: number): string {
		const line = chartLine(values, w, h, max);
		if (!line) return '';
		return `${line} L${w},${h} L0,${h} Z`;
	}

	function formatServiceUptime(ns: number): string {
		const seconds = Math.floor(ns / 1e9);
		if (seconds <= 0) return '';
		const d = Math.floor(seconds / 86400);
		const h = Math.floor((seconds % 86400) / 3600);
		const min = Math.floor((seconds % 3600) / 60);
		if (d > 0) return `${d}d ${h}h`;
		if (h > 0) return `${h}h ${min}m`;
		return `${min}m`;
	}

	function partitionPercent(usePercent: string): number {
		return parseInt(usePercent, 10) || 0;
	}

	function daysLeft(expiresAt: string): number {
		return Math.ceil((new Date(expiresAt).getTime() - now) / 86400000);
	}

	function timeAgo(ts: string): string {
		const seconds = Math.max(0, Math.floor((now - new Date(ts).getTime()) / 1000));
		if (seconds < 60) return `${seconds}s`;
		const minutes = Math.floor(seconds / 60);
		if (minutes < 60) return `${minutes}m`;
		const hours = Math.floor(minutes / 60);
		if (hours < 24) return `${hours}h`;
		return `${Math.floor(hours / 24)}d`;
	}

	async function getSafe<T>(path: string): Promise<T | null> {
		try {
			return await api.get<T>(path);
		} catch {
			// Endpoint unreachable or permission missing — related sections stay hidden.
			return null;
		}
	}

	function pushHistory(point: ServerMetrics) {
		const last = history[history.length - 1];
		if (last && last.timestamp === point.timestamp) return;
		lastUpdated = new Date();
		history = history.length >= 60 ? [...history.slice(-59), point] : [...history, point];
	}

	async function loadDashboard() {
		refreshing = true;
		error = '';
		try {
			const data = await api.get<DashboardData>('/api/v1/dashboard');
			serverInfo = data.server;
			metricsSnapshot = data.metrics;
		} catch (err) {
			error = err instanceof Error ? err.message : 'Failed to load dashboard';
			loading = false;
			refreshing = false;
			return;
		}
		loading = false;
		refreshing = false;

		const [recent, svc, sites, dbs, bks, certs, alerts] = await Promise.all([
			getSafe<ServerMetrics[]>('/api/v1/dashboard/metrics'),
			getSafe<ServiceStatus[]>('/api/v1/services'),
			getSafe<WebsiteLite[]>('/api/v1/websites'),
			getSafe<unknown[]>('/api/v1/databases'),
			getSafe<unknown[]>('/api/v1/backups'),
			getSafe<SSLCertLite[]>('/api/v1/ssl'),
			getSafe<AlertEventLite[]>('/api/v1/alert-history?limit=50')
		]);

		history = (recent ?? []).slice().reverse().slice(-60);
		if (history.length > 0) lastUpdated = new Date();
		services = svc ?? [];
		websites = sites;
		databases = dbs;
		backups = bks;
		certificates = certs;
		alertEvents = alerts;
	}

	onMount(async () => {
		await loadDashboard();
		connectMetrics();
		clockTimer = setInterval(() => (now = Date.now()), 5000);
	});

	onDestroy(() => {
		disconnectMetrics();
		if (clockTimer) clearInterval(clockTimer);
	});

	// Live metrics from the websocket win over the initial snapshot.
	let m = $derived($currentMetrics ?? metricsSnapshot);

	$effect(() => {
		const live = $currentMetrics;
		if (live) pushHistory(live);
	});

	let cpuValue = $derived(m ? Math.min(Math.round(m.cpu), 100) : null);
	let ramValue = $derived(m ? pct(m.ram_used, m.ram_total) : null);
	let diskValue = $derived(m ? pct(m.disk_used, m.disk_total) : null);

	let cpuSeries = $derived(history.map((h) => Math.min(Math.round(h.cpu), 100)));
	let ramSeries = $derived(history.map((h) => pct(h.ram_used, h.ram_total)));
	let diskSeries = $derived(history.map((h) => pct(h.disk_used, h.disk_total)));
	let rxSeries = $derived(history.map((h) => h.net_rx));
	let txSeries = $derived(history.map((h) => h.net_tx));

	let runningServices = $derived(services.filter((s) => s.running).length);
	let stoppedServices = $derived(services.length - runningServices);
	let activeWebsites = $derived(websites ? websites.filter((w) => w.status === 'active').length : 0);
	let unresolvedAlerts = $derived(alertEvents ? alertEvents.filter((a) => !a.resolved).length : 0);
	let latestAlerts = $derived(alertEvents ? alertEvents.slice(0, 5) : []);
	let sslAttention = $derived(
		certificates
			? certificates
					.filter((c) => c.status !== 'active' || daysLeft(c.expires_at) <= 30)
					.sort((a, b) => new Date(a.expires_at).getTime() - new Date(b.expires_at).getTime())
					.slice(0, 5)
			: []
	);

	let liveOk = $derived(lastUpdated !== null && now - lastUpdated.getTime() < 20000);

	function sslChip(cert: SSLCertLite): { label: string; cls: string } {
		const left = daysLeft(cert.expires_at);
		if (cert.status === 'active') {
			if (left < 0)
				return {
					label: `${translate($language, 'dash.ssl.expired')} ${Math.abs(left)} ${translate($language, 'dash.ssl.days')}`,
					cls: 'bg-red-900/40 text-red-300'
				};
			return {
				label: `${translate($language, 'dash.ssl.expires_in')} ${left} ${translate($language, 'dash.ssl.days')}`,
				cls: left <= 7 ? 'bg-red-900/40 text-red-300' : 'bg-yellow-900/40 text-yellow-300'
			};
		}
		return {
			label: cert.status,
			cls:
				cert.status === 'failed' || cert.status === 'expired'
					? 'bg-red-900/40 text-red-300'
					: 'bg-yellow-900/40 text-yellow-300'
		};
	}
</script>

<div class="space-y-6">
	{#snippet gaugeCard(label: string, value: number | null, series: number[], warn: number, crit: number, rows: DetailRow[])}
		{@const tone = gaugeTone(value, warn, crit)}
		<div
			class="rounded-2xl border border-white/5 bg-gray-800/60 p-5 transition-colors hover:border-blue-400/20"
		>
			<div class="flex items-start justify-between gap-3">
				<div class="min-w-0">
					<p class="text-xs font-semibold uppercase tracking-[0.14em] text-gray-400">{label}</p>
					<dl class="mt-2.5 space-y-1.5">
						{#each rows as row}
							<div class="flex items-baseline justify-between gap-3 text-[11px]">
								<dt class="shrink-0 text-gray-500">{row.label}</dt>
								<dd class="truncate font-medium text-gray-300">{row.value}</dd>
							</div>
						{/each}
					</dl>
				</div>
				<div class="relative shrink-0">
					<svg viewBox="0 0 72 72" class="h-[72px] w-[72px] -rotate-90 {tone}">
						<circle cx="36" cy="36" r="30" fill="none" stroke-width="7" class="stroke-gray-700/70">
						</circle>
						{#if value !== null}
							<circle
								cx="36"
								cy="36"
								r="30"
								fill="none"
								stroke-width="7"
								stroke-linecap="round"
								class="stroke-current transition-all duration-700"
								stroke-dasharray={GAUGE_CIRC}
								stroke-dashoffset={GAUGE_CIRC * (1 - value / 100)}
							></circle>
						{/if}
					</svg>
					<div class="absolute inset-0 flex items-center justify-center">
						<span class="text-lg font-bold text-white">
							{value !== null ? `${value}%` : '–'}
						</span>
					</div>
				</div>
			</div>
			{#if series.length >= 2}
				<svg
					viewBox="0 0 120 30"
					preserveAspectRatio="none"
					class="mt-4 h-7 w-full {tone}"
					aria-hidden="true"
				>
					<path d={sparkArea(series, 120, 30)} class="fill-current opacity-10"></path>
					<path
						d={sparkLine(series, 120, 30)}
						class="fill-none stroke-current"
						stroke-width="1.5"
						vector-effect="non-scaling-stroke"
						stroke-linejoin="round"
						stroke-linecap="round"
					></path>
				</svg>
			{:else}
				<div class="mt-4 h-7"></div>
			{/if}
		</div>
	{/snippet}

	{#snippet statTile(label: string, count: number | null, sub: string, href: string, iconPath: string)}
		<a
			{href}
			class="group rounded-2xl border border-white/5 bg-gray-800/60 p-4 transition-all
			hover:-translate-y-0.5 hover:border-blue-400/25 hover:bg-blue-500/5"
		>
			<div class="flex items-center justify-between">
				<div class="rounded-xl bg-blue-500/10 p-2 text-blue-400 transition group-hover:bg-blue-500/15">
					<svg
						class="h-5 w-5"
						fill="none"
						stroke="currentColor"
						viewBox="0 0 24 24"
						stroke-width="1.5"
						aria-hidden="true"
					>
						<path stroke-linecap="round" stroke-linejoin="round" d={iconPath} />
					</svg>
				</div>
				<svg
					class="h-4 w-4 text-gray-600 transition group-hover:translate-x-0.5 group-hover:text-blue-400"
					fill="none"
					stroke="currentColor"
					viewBox="0 0 24 24"
					stroke-width="2"
					aria-hidden="true"
				>
					<path stroke-linecap="round" stroke-linejoin="round" d="M13.5 4.5L21 12l-7.5 7.5M21 12H3" />
				</svg>
			</div>
			<p class="mt-3 text-2xl font-bold text-white">{count ?? '–'}</p>
			<p class="text-xs text-gray-400">{label}</p>
			{#if sub}
				<p class="mt-0.5 truncate text-[11px] text-blue-400/80">{sub}</p>
			{/if}
		</a>
	{/snippet}

	<!-- Header -->
	<div class="flex flex-wrap items-end justify-between gap-4">
		<div>
			<h2 class="text-2xl font-bold text-white">{translate($language, 'dash.title')}</h2>
			<div class="mt-1 flex flex-wrap items-center gap-2 text-sm text-gray-400">
				{#if serverInfo}
					<span class="font-medium text-gray-300">{serverInfo.hostname}</span>
					<span class="text-gray-600">·</span>
					<span>{serverInfo.os}</span>
				{/if}
				{#if lastUpdated}
					<span
						class="inline-flex items-center gap-1.5 rounded-full px-2 py-0.5 text-[11px] font-semibold
						{liveOk ? 'bg-green-900/40 text-green-400' : 'bg-yellow-900/40 text-yellow-400'}"
					>
						<span class="relative flex h-1.5 w-1.5">
							{#if liveOk}
								<span
									class="absolute inline-flex h-full w-full animate-ping rounded-full bg-green-400 opacity-60"
								></span>
							{/if}
							<span
								class="relative inline-flex h-1.5 w-1.5 rounded-full {liveOk
									? 'bg-green-400'
									: 'bg-yellow-400'}"
							></span>
						</span>
						{liveOk ? translate($language, 'dash.live') : translate($language, 'dash.reconnecting')}
					</span>
				{/if}
			</div>
		</div>
		<div class="flex items-center gap-3">
			{#if lastUpdated}
				<span class="text-xs text-gray-500" title={lastUpdated.toLocaleString()}>
					{translate($language, 'dash.updated')}
					{lastUpdated.toLocaleTimeString()}
				</span>
			{/if}
			<button
				type="button"
				onclick={loadDashboard}
				disabled={refreshing}
				class="inline-flex cursor-pointer items-center gap-2 rounded-xl border border-white/8 bg-white/[0.035]
				px-3.5 py-2 text-xs font-medium text-gray-300 transition
				hover:border-blue-400/20 hover:bg-blue-500/8 hover:text-white disabled:opacity-50"
			>
				<svg
					class="h-3.5 w-3.5 {refreshing ? 'animate-spin' : ''}"
					fill="none"
					stroke="currentColor"
					viewBox="0 0 24 24"
					stroke-width="2"
					aria-hidden="true"
				>
					<path
						stroke-linecap="round"
						stroke-linejoin="round"
						d="M16.023 9.348h4.992v-.001M2.985 19.644v-4.992m0 0h4.992m-4.992 0l3.181 3.183a8.25 8.25 0 0013.803-3.7M4.031 9.865a8.25 8.25 0 0113.803-3.7l3.181 3.182M21.015 4.353v4.992"
					/>
				</svg>
				{translate($language, 'dash.refresh')}
			</button>
		</div>
	</div>

	{#if loading}
		<!-- Skeleton -->
		<div class="space-y-4">
			<div class="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-4">
				{#each Array(4) as _}
					<div class="h-40 animate-pulse rounded-2xl border border-white/5 bg-gray-800/60"></div>
				{/each}
			</div>
			<div class="grid grid-cols-1 gap-4 xl:grid-cols-3">
				<div class="h-72 animate-pulse rounded-2xl border border-white/5 bg-gray-800/60 xl:col-span-2">
				</div>
				<div class="h-72 animate-pulse rounded-2xl border border-white/5 bg-gray-800/60"></div>
			</div>
		</div>
	{:else if error}
		<div class="rounded-2xl border border-red-700 bg-red-900/30 p-8 text-center">
			<svg
				class="mx-auto h-10 w-10 text-red-400"
				fill="none"
				stroke="currentColor"
				viewBox="0 0 24 24"
				stroke-width="1.5"
				aria-hidden="true"
			>
				<path
					stroke-linecap="round"
					stroke-linejoin="round"
					d="M12 9v3.75m-9.303 3.376c-.866 1.5.217 3.374 1.948 3.374h14.71c1.73 0 2.813-1.874 1.948-3.374L13.949 3.378c-.866-1.5-3.032-1.5-3.898 0L2.697 16.126zM12 15.75h.007v.008H12v-.008z"
				/>
			</svg>
			<p class="mt-3 text-sm text-red-300">{error}</p>
			<button
				type="button"
				onclick={loadDashboard}
				class="mt-4 cursor-pointer rounded-xl border border-white/10 bg-white/5 px-4 py-2 text-xs font-medium text-gray-200 transition hover:bg-white/10"
			>
				{translate($language, 'dash.retry')}
			</button>
		</div>
	{:else}
		<!-- Resource gauge cards -->
		<div class="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-4">
			{@render gaugeCard(
				translate($language, 'dash.cpu'),
				cpuValue,
				cpuSeries,
				50,
				80,
				[
					{ label: `${translate($language, 'dash.load')} 1m`, value: m ? m.load_1.toFixed(2) : '–' },
					{ label: `${translate($language, 'dash.load')} 5m`, value: m ? m.load_5.toFixed(2) : '–' },
					{ label: `${translate($language, 'dash.load')} 15m`, value: m ? m.load_15.toFixed(2) : '–' },
					{
						label: translate($language, 'dash.cores'),
						value: serverInfo ? String(serverInfo.cpu_cores) : '–'
					}
				]
			)}

			{@render gaugeCard(
				translate($language, 'dash.memory'),
				ramValue,
				ramSeries,
				50,
				80,
				[
					{
						label: translate($language, 'dash.used'),
						value: m ? `${formatBytes(m.ram_used)} / ${formatBytes(m.ram_total)}` : '–'
					},
					{
						label: translate($language, 'dash.swap'),
						value: m
							? m.swap_total > 0
								? `${formatBytes(m.swap_used)} / ${formatBytes(m.swap_total)}`
								: '—'
							: '–'
					}
				]
			)}

			{@render gaugeCard(
				translate($language, 'dash.disk'),
				diskValue,
				diskSeries,
				70,
				90,
				[
					{
						label: translate($language, 'dash.used'),
						value: m ? `${formatBytes(m.disk_used)} / ${formatBytes(m.disk_total)}` : '–'
					}
				]
			)}

			<!-- Network card -->
			<div
				class="rounded-2xl border border-white/5 bg-gray-800/60 p-5 transition-colors hover:border-blue-400/20"
			>
				<p class="text-xs font-semibold uppercase tracking-[0.14em] text-gray-400">
					{translate($language, 'dash.network')}
				</p>
				<div class="mt-3 space-y-3">
					<div>
						<div class="flex items-center justify-between gap-3 text-xs">
							<span class="flex items-center gap-1.5 text-gray-400">
								<svg
									class="h-3.5 w-3.5 text-blue-400"
									fill="none"
									stroke="currentColor"
									viewBox="0 0 24 24"
									stroke-width="2"
									aria-hidden="true"
								>
									<path stroke-linecap="round" stroke-linejoin="round" d="M12 4.5v15m0 0l6.75-6.75M12 19.5l-6.75-6.75" />
								</svg>
								{translate($language, 'dash.download')}
							</span>
							<span class="font-semibold text-gray-200">
								{m ? `${formatBytes(m.net_rx)}/s` : '–'}
							</span>
						</div>
						{#if rxSeries.length >= 2}
							<svg viewBox="0 0 120 22" preserveAspectRatio="none" class="mt-1.5 h-5 w-full text-blue-400" aria-hidden="true">
								<path d={sparkArea(rxSeries, 120, 22)} class="fill-current opacity-10"></path>
								<path
									d={sparkLine(rxSeries, 120, 22)}
									class="fill-none stroke-current"
									stroke-width="1.5"
									vector-effect="non-scaling-stroke"
									stroke-linejoin="round"
									stroke-linecap="round"
								></path>
							</svg>
						{/if}
					</div>
					<div>
						<div class="flex items-center justify-between gap-3 text-xs">
							<span class="flex items-center gap-1.5 text-gray-400">
								<svg
									class="h-3.5 w-3.5 text-purple-400"
									fill="none"
									stroke="currentColor"
									viewBox="0 0 24 24"
									stroke-width="2"
									aria-hidden="true"
								>
									<path stroke-linecap="round" stroke-linejoin="round" d="M12 19.5v-15m0 0l-6.75 6.75M12 4.5l6.75 6.75" />
								</svg>
								{translate($language, 'dash.upload')}
							</span>
							<span class="font-semibold text-gray-200">
								{m ? `${formatBytes(m.net_tx)}/s` : '–'}
							</span>
						</div>
						{#if txSeries.length >= 2}
							<svg viewBox="0 0 120 22" preserveAspectRatio="none" class="mt-1.5 h-5 w-full text-purple-400" aria-hidden="true">
								<path d={sparkArea(txSeries, 120, 22)} class="fill-current opacity-10"></path>
								<path
									d={sparkLine(txSeries, 120, 22)}
									class="fill-none stroke-current"
									stroke-width="1.5"
									vector-effect="non-scaling-stroke"
									stroke-linejoin="round"
									stroke-linecap="round"
								></path>
							</svg>
						{/if}
					</div>
				</div>
			</div>
		</div>

		<!-- History chart + system info -->
		<div class="grid grid-cols-1 gap-4 xl:grid-cols-3">
			<div class="rounded-2xl border border-white/5 bg-gray-800/60 p-5 xl:col-span-2">
				<div class="flex flex-wrap items-center justify-between gap-2">
					<div>
						<h3 class="text-sm font-semibold text-white">{translate($language, 'dash.history')}</h3>
						<p class="text-[11px] text-gray-500">{translate($language, 'dash.history.sub')}</p>
					</div>
					<div class="flex items-center gap-4 text-[11px] text-gray-400">
						<span class="flex items-center gap-1.5">
							<span class="h-1.5 w-4 rounded-full bg-blue-400"></span>
							CPU
						</span>
						<span class="flex items-center gap-1.5">
							<span class="h-1.5 w-4 rounded-full bg-purple-400"></span>
							{translate($language, 'dash.memory')}
						</span>
					</div>
				</div>
				{#if cpuSeries.length < 2}
					<div class="flex h-44 items-center justify-center text-xs text-gray-500">
						{translate($language, 'dash.no_history')}
					</div>
				{:else}
					<div class="mt-4 flex gap-2">
						<div class="flex h-44 flex-col justify-between py-0.5 text-right text-[10px] text-gray-500">
							<span>100</span>
							<span>75</span>
							<span>50</span>
							<span>25</span>
							<span>0</span>
						</div>
						<div class="min-w-0 flex-1">
							<svg viewBox="0 0 600 176" preserveAspectRatio="none" class="h-44 w-full" aria-hidden="true">
								{#each [44, 88, 132] as y}
									<line
										x1="0"
										x2="600"
										y1={y}
										y2={y}
										class="stroke-gray-500/20"
										stroke-dasharray="4 4"
										vector-effect="non-scaling-stroke"
									></line>
								{/each}
								<path d={chartArea(ramSeries, 600, 176, 100)} class="fill-purple-400/10"></path>
								<path d={chartArea(cpuSeries, 600, 176, 100)} class="fill-blue-400/10"></path>
								<path
									d={chartLine(ramSeries, 600, 176, 100)}
									class="fill-none stroke-purple-400"
									stroke-width="1.5"
									vector-effect="non-scaling-stroke"
									stroke-linejoin="round"
								></path>
								<path
									d={chartLine(cpuSeries, 600, 176, 100)}
									class="fill-none stroke-blue-400"
									stroke-width="1.5"
									vector-effect="non-scaling-stroke"
									stroke-linejoin="round"
								></path>
							</svg>
							<div class="mt-1 flex justify-between text-[10px] text-gray-500">
								<span>-5 min</span>
								<span>-2.5 min</span>
								<span>now</span>
							</div>
						</div>
					</div>
				{/if}
			</div>

			<div class="rounded-2xl border border-white/5 bg-gray-800/60 p-5">
				<h3 class="text-sm font-semibold text-white">{translate($language, 'dash.system')}</h3>
				{#if serverInfo}
					<div
						class="mt-4 flex items-center gap-3 rounded-xl border border-blue-400/10 bg-blue-500/8 px-4 py-3"
					>
						<svg
							class="h-7 w-7 shrink-0 text-blue-400"
							fill="none"
							stroke="currentColor"
							viewBox="0 0 24 24"
							stroke-width="1.5"
							aria-hidden="true"
						>
							<path
								stroke-linecap="round"
								stroke-linejoin="round"
								d="M12 6v6h4.5m4.5 0a9 9 0 11-18 0 9 9 0 0118 0z"
							/>
						</svg>
						<div class="min-w-0">
							<p class="text-[10px] font-semibold uppercase tracking-[0.16em] text-gray-400">
								{translate($language, 'dash.uptime')}
							</p>
							<p class="truncate text-sm font-semibold text-white">{serverInfo.uptime}</p>
						</div>
					</div>
					<dl class="mt-4 space-y-2.5 text-sm">
						<div class="flex items-center justify-between gap-3">
							<dt class="shrink-0 text-xs text-gray-500">IP</dt>
							<dd class="truncate font-medium text-gray-200">{serverInfo.ip}</dd>
						</div>
						<div class="flex items-center justify-between gap-3">
							<dt class="shrink-0 text-xs text-gray-500">CPU</dt>
							<dd class="truncate text-right font-medium text-gray-200" title={serverInfo.cpu_model}>
								{serverInfo.cpu_cores}
								{translate($language, 'dash.cores')}
							</dd>
						</div>
						<div class="flex items-center justify-between gap-3">
							<dt class="shrink-0 text-xs text-gray-500">Kernel</dt>
							<dd class="truncate font-medium text-gray-200">{serverInfo.kernel}</dd>
						</div>
						<div class="flex items-center justify-between gap-3">
							<dt class="shrink-0 text-xs text-gray-500">Timezone</dt>
							<dd class="truncate font-medium text-gray-200">{serverInfo.timezone}</dd>
						</div>
					</dl>
					{#if serverInfo.disk_partitions?.length}
						<div class="mt-4 border-t border-white/5 pt-3">
							<p class="text-[10px] font-semibold uppercase tracking-[0.16em] text-gray-400">
								{translate($language, 'dash.partitions')}
							</p>
							<div class="mt-2 space-y-2">
								{#each serverInfo.disk_partitions.slice(0, 4) as p}
									{@const percent = partitionPercent(p.use_percent)}
									<div>
										<div class="flex items-center justify-between gap-3 text-[11px]">
											<span class="truncate text-gray-400" title={p.device}>{p.mount}</span>
											<span class="shrink-0 text-gray-300">{p.used} / {p.size}</span>
										</div>
										<div class="mt-1 h-1 rounded-full bg-gray-700/70">
											<div
												class="h-1 rounded-full transition-all duration-500 {barTone(percent)}"
												style="width: {Math.min(percent, 100)}%"
											></div>
										</div>
									</div>
								{/each}
							</div>
						</div>
					{/if}
				{/if}
			</div>
		</div>

		<!-- Quick tiles -->
		<div class="grid grid-cols-2 gap-4 sm:grid-cols-3 xl:grid-cols-5">
			{@render statTile(
				translate($language, 'dash.websites'),
				websites ? websites.length : null,
				websites
					? `${activeWebsites} ${translate($language, 'dash.active')}`
					: '',
				'/websites',
				'M12 21a9.004 9.004 0 008.716-6.747M12 21a9.004 9.004 0 01-8.716-6.747M12 21c2.485 0 4.5-4.03 4.5-9S14.485 3 12 3m0 18c-2.485 0-4.5-4.03-4.5-9S9.515 3 12 3m0 0a8.997 8.997 0 017.843 4.582M12 3a8.997 8.997 0 00-7.843 4.582m15.686 0A11.953 11.953 0 0112 10.5c-2.998 0-5.74-1.1-7.843-2.918m15.686 0A8.959 8.959 0 0121 12c0 .778-.099 1.533-.284 2.253m0 0A17.919 17.919 0 0112 16.5c-3.162 0-6.133-.815-8.716-2.247m0 0A9.015 9.015 0 013 12c0-1.605.42-3.113 1.157-4.418'
			)}
			{@render statTile(
				translate($language, 'dash.databases'),
				databases ? databases.length : null,
				'',
				'/databases',
				'M20.25 6.375c0 2.278-3.694 4.125-8.25 4.125S3.75 8.653 3.75 6.375m16.5 0c0-2.278-3.694-4.125-8.25-4.125S3.75 4.097 3.75 6.375m16.5 0v11.25c0 2.278-3.694 4.125-8.25 4.125s-8.25-1.847-8.25-4.125V6.375m16.5 0v3.75c0 2.278-3.694 4.125-8.25 4.125s-8.25-1.847-8.25-4.125v-3.75m16.5 3.75v3.75c0 2.278-3.694 4.125-8.25 4.125s-8.25-1.847-8.25-4.125v-3.75'
			)}
			{@render statTile(
				translate($language, 'dash.backups'),
				backups ? backups.length : null,
				'',
				'/backups',
				'M20.25 7.5l-.625 10.632a2.25 2.25 0 01-2.247 2.118H6.622a2.25 2.25 0 01-2.247-2.118L3.75 7.5M10 11.25h4M3.375 7.5h17.25c.621 0 1.125-.504 1.125-1.125v-1.5c0-.621-.504-1.125-1.125-1.125H2.25c-.621 0-1.125.504-1.125 1.125v1.5c0 .621.504 1.125 1.125 1.125z'
			)}
			{@render statTile(
				translate($language, 'dash.certificates'),
				certificates ? certificates.length : null,
				certificates
					? sslAttention.length > 0
						? `${sslAttention.length} ${translate($language, 'dash.ssl.need_attention')}`
						: translate($language, 'dash.ssl_ok')
					: '',
				'/security',
				'M9 12.75L11.25 15 15 9.75m-3-7.036A11.959 11.959 0 013.598 6 11.99 11.99 0 003 9.749c0 5.592 3.824 10.29 9 11.623 5.176-1.332 9-6.03 9-11.622 0-1.31-.21-2.571-.598-3.751h-.152c-3.196 0-6.1-1.248-8.25-3.285z'
			)}
			{@render statTile(
				translate($language, 'dash.alerts'),
				alertEvents ? alertEvents.length : null,
				alertEvents
					? unresolvedAlerts > 0
						? `${unresolvedAlerts} ${translate($language, 'dash.alerts.unresolved')}`
						: translate($language, 'dash.alerts.none')
					: '',
				'/alerts',
				'M14.857 17.082a23.848 23.848 0 005.454-1.31A8.967 8.967 0 0118 9.75v-.7V9A6 6 0 006 9v.75a8.967 8.967 0 01-2.312 6.022c1.733.64 3.56 1.085 5.455 1.31m5.714 0a24.255 24.255 0 01-5.714 0m5.714 0a3 3 0 11-5.714 0'
			)}
		</div>

		<!-- Services + SSL + Alerts -->
		<div class="grid grid-cols-1 gap-4 xl:grid-cols-3">
			<div class="rounded-2xl border border-white/5 bg-gray-800/60 p-5 xl:col-span-2">
				<div class="flex flex-wrap items-center justify-between gap-2">
					<div class="flex items-center gap-3">
						<h3 class="text-sm font-semibold text-white">{translate($language, 'dash.services')}</h3>
						{#if services.length > 0}
							<span
								class="rounded-full px-2 py-0.5 text-[11px] font-semibold
								{stoppedServices > 0
									? 'bg-red-900/40 text-red-300'
									: 'bg-green-900/40 text-green-400'}"
							>
								{runningServices}
								{translate($language, 'dash.services.running')}
								{#if stoppedServices > 0}
									· {stoppedServices}
									{translate($language, 'dash.services.stopped')}
								{/if}
							</span>
						{/if}
					</div>
					<a
						href="/services"
						class="inline-flex items-center gap-1 text-xs font-medium text-blue-400 transition hover:text-blue-300"
					>
						{translate($language, 'dash.manage')}
						<svg class="h-3 w-3" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="2" aria-hidden="true">
							<path stroke-linecap="round" stroke-linejoin="round" d="M13.5 4.5L21 12l-7.5 7.5M21 12H3" />
						</svg>
					</a>
				</div>
				{#if services.length === 0}
					<div class="mt-4 flex h-24 items-center justify-center text-xs text-gray-500">—</div>
				{:else}
					<div class="mt-4 grid grid-cols-1 gap-2 sm:grid-cols-2">
						{#each services as svc}
							<div
								class="flex items-center justify-between gap-3 rounded-xl border border-white/5 bg-white/[0.02] px-3.5 py-2.5"
							>
								<div class="flex min-w-0 items-center gap-2.5">
									<span
										class="h-2 w-2 shrink-0 rounded-full {svc.running
											? 'bg-green-400 shadow-[0_0_6px_rgba(74,222,128,0.55)]'
											: 'bg-red-500'}"
									></span>
									<div class="min-w-0">
										<p class="truncate text-sm text-gray-200">{svc.name}</p>
										<p class="text-[10px] text-gray-500">
											{#if svc.running}
												{formatServiceUptime(svc.uptime) || ''}
												{svc.pid ? `· PID ${svc.pid}` : ''}
											{:else if !svc.installed}
												{translate($language, 'dash.not_installed')}
											{/if}
										</p>
									</div>
								</div>
								<span
									class="inline-flex shrink-0 items-center gap-1.5 rounded-full px-2 py-0.5 text-[10px] font-semibold
									{svc.running ? 'bg-green-900/40 text-green-400' : 'bg-red-900/40 text-red-300'}"
								>
									{svc.running
										? translate($language, 'dash.status.running')
										: translate($language, 'dash.status.stopped')}
								</span>
							</div>
						{/each}
					</div>
				{/if}
			</div>

			<div class="space-y-4">
				<!-- SSL attention -->
				<div class="rounded-2xl border border-white/5 bg-gray-800/60 p-5">
					<div class="flex items-center justify-between gap-3">
						<h3 class="text-sm font-semibold text-white">
							{sslAttention.length > 0
								? translate($language, 'dash.ssl_attention')
								: translate($language, 'dash.certificates')}
						</h3>
						<a
							href="/security"
							class="inline-flex items-center gap-1 text-xs font-medium text-blue-400 transition hover:text-blue-300"
						>
							{translate($language, 'dash.manage')}
							<svg class="h-3 w-3" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="2" aria-hidden="true">
								<path stroke-linecap="round" stroke-linejoin="round" d="M13.5 4.5L21 12l-7.5 7.5M21 12H3" />
							</svg>
						</a>
					</div>
					{#if certificates === null}
						<div class="mt-4 flex h-20 items-center justify-center text-xs text-gray-500">—</div>
					{:else if sslAttention.length === 0}
						<div class="mt-4 flex items-center gap-3 rounded-xl border border-green-900/40 bg-green-900/20 px-4 py-3">
							<svg
								class="h-6 w-6 shrink-0 text-green-400"
								fill="none"
								stroke="currentColor"
								viewBox="0 0 24 24"
								stroke-width="1.5"
								aria-hidden="true"
							>
								<path
									stroke-linecap="round"
									stroke-linejoin="round"
									d="M9 12.75L11.25 15 15 9.75m-3-7.036A11.959 11.959 0 013.598 6 11.99 11.99 0 003 9.749c0 5.592 3.824 10.29 9 11.623 5.176-1.332 9-6.03 9-11.622 0-1.31-.21-2.571-.598-3.751h-.152c-3.196 0-6.1-1.248-8.25-3.285z"
								/>
							</svg>
							<p class="text-xs text-gray-300">{translate($language, 'dash.ssl_ok')}</p>
						</div>
					{:else}
						<ul class="mt-4 space-y-2">
							{#each sslAttention as cert}
								{@const chip = sslChip(cert)}
								<li
									class="flex items-center justify-between gap-3 rounded-xl border border-white/5 bg-white/[0.02] px-3.5 py-2.5"
								>
									<div class="min-w-0">
										<p class="truncate text-sm text-gray-200" title={cert.domain}>{cert.domain}</p>
										<p class="text-[10px] text-gray-500">{cert.issuer || cert.status}</p>
									</div>
									<span class="shrink-0 rounded-full px-2 py-0.5 text-[10px] font-semibold {chip.cls}">
										{chip.label}
									</span>
								</li>
							{/each}
						</ul>
					{/if}
				</div>

				<!-- Recent alerts -->
				<div class="rounded-2xl border border-white/5 bg-gray-800/60 p-5">
					<div class="flex items-center justify-between gap-3">
						<div class="flex items-center gap-2">
							<h3 class="text-sm font-semibold text-white">
								{translate($language, 'dash.recent_alerts')}
							</h3>
							{#if alertEvents && unresolvedAlerts > 0}
								<span class="rounded-full bg-red-900/40 px-2 py-0.5 text-[10px] font-semibold text-red-300">
									{unresolvedAlerts}
								</span>
							{/if}
						</div>
						<a
							href="/alerts"
							class="inline-flex items-center gap-1 text-xs font-medium text-blue-400 transition hover:text-blue-300"
						>
							{translate($language, 'dash.manage')}
							<svg class="h-3 w-3" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="2" aria-hidden="true">
								<path stroke-linecap="round" stroke-linejoin="round" d="M13.5 4.5L21 12l-7.5 7.5M21 12H3" />
							</svg>
						</a>
					</div>
					{#if alertEvents === null}
						<div class="mt-4 flex h-16 items-center justify-center text-xs text-gray-500">—</div>
					{:else if latestAlerts.length === 0}
						<div class="mt-4 flex items-center gap-3 rounded-xl border border-green-900/40 bg-green-900/20 px-4 py-3">
							<svg
								class="h-6 w-6 shrink-0 text-green-400"
								fill="none"
								stroke="currentColor"
								viewBox="0 0 24 24"
								stroke-width="1.5"
								aria-hidden="true"
							>
								<path stroke-linecap="round" stroke-linejoin="round" d="M9 12.75L11.25 15 15 9.75M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
							</svg>
							<p class="text-xs text-gray-300">{translate($language, 'dash.alerts.none')}</p>
						</div>
					{:else}
						<ul class="mt-4 space-y-2">
							{#each latestAlerts as ev}
								<li
									class="flex items-start gap-2.5 rounded-xl border border-white/5 bg-white/[0.02] px-3.5 py-2.5"
								>
									<span
										class="mt-1.5 h-2 w-2 shrink-0 rounded-full {ev.resolved ? 'bg-green-400' : 'bg-red-500'}"
									></span>
									<div class="min-w-0 flex-1">
										<p class="truncate text-xs text-gray-200" title={ev.message}>
											{ev.message || `${ev.metric} = ${ev.value}`}
										</p>
										<p class="mt-0.5 text-[10px] text-gray-500">
											{ev.metric}
											· {timeAgo(ev.created_at)}
											{#if ev.resolved}
												· {translate($language, 'dash.alerts.resolved')}
											{/if}
										</p>
									</div>
								</li>
							{/each}
						</ul>
					{/if}
				</div>
			</div>
		</div>
	{/if}
</div>
