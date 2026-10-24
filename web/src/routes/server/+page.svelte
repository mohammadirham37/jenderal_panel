<script lang="ts">
	import { onDestroy, onMount } from 'svelte';
	import { api } from '$lib/api';
	import type { ServerInfo } from '$lib/types';
	import { toast } from '$lib/stores/toast';
	import { language, translate } from '$lib/stores/language';
	import { hasPermission, permissions } from '$lib/stores/auth';
	import { connectMetrics, currentMetrics, disconnectMetrics } from '$lib/stores/metrics';
	import SshPortCard from '$lib/components/SshPortCard.svelte';
	import Modal from '$lib/components/Modal.svelte';
	import {
		formatBytes,
		formatServerTime,
		formatUptime,
		uptimeSeconds,
		cleanUptimeText,
		isValidHostname,
		loadPercent,
		parsePercent,
		usageTone,
		utcOffsetLabel
	} from '$lib/server-info.js';

	let info = $state<ServerInfo | null>(null);
	let loading = $state(true);
	let refreshing = $state(false);
	let error = $state('');

	let newHostname = $state('');
	let newTimezone = $state('');
	let editingHostname = $state(false);
	let editingTimezone = $state(false);
	let savingHostname = $state(false);
	let savingTimezone = $state(false);
	let hostnameValid = $derived(isValidHostname(newHostname));

	// Reboot is confirmed by typing the hostname, like other destructive panels.
	let rebootOpen = $state(false);
	let rebootConfirm = $state('');
	let rebooting = $state(false);
	let rebootStarted = $state(false);
	let rebootPrompt = $derived(translate($language, 'srv.rebootTypeHostname').split('{hostname}'));

	// Searchable timezone combobox state
	let tzQuery = $state('');
	let tzOpen = $state(false);
	let tzHighlight = $state(0);
	let tzEditRoot: HTMLElement | undefined = $state();

	let copiedField = $state('');
	let now = $state(new Date());
	let clock: ReturnType<typeof setInterval> | null = null;

	const canReboot = $derived(hasPermission($permissions, 'server.reboot'));
	const canHostname = $derived(hasPermission($permissions, 'server.hostname'));
	const canTimezone = $derived(hasPermission($permissions, 'server.timezone'));

	const fallbackTimezones = [
		'Asia/Jakarta', 'Asia/Makassar', 'Asia/Jayapura', 'Asia/Singapore', 'Asia/Kuala_Lumpur', 'Asia/Bangkok',
		'Asia/Manila', 'Asia/Tokyo', 'Asia/Seoul', 'Asia/Shanghai', 'Asia/Kolkata', 'Asia/Dubai', 'Australia/Perth',
		'Australia/Sydney', 'Europe/London', 'Europe/Paris', 'Europe/Berlin', 'Europe/Moscow', 'America/New_York',
		'America/Chicago', 'America/Denver', 'America/Los_Angeles', 'UTC'
	];
	const quickTimezones = ['Asia/Jakarta', 'Asia/Makassar', 'Asia/Jayapura', 'Asia/Singapore', 'UTC'];

	// Full IANA list when the browser provides it; keeps users from typos.
	const timezoneOptions: string[] = (() => {
		try {
			const supported = (Intl as unknown as { supportedValuesOf?: (kind: 'timeZone') => string[] }).supportedValuesOf?.('timeZone');
			if (supported && supported.length > 0) return supported.includes('UTC') ? supported : [...supported, 'UTC'];
		} catch { /* older runtime */ }
		return fallbackTimezones;
	})();

	// ── Live metrics ─────────────────────────────────────────────
	let m = $derived($currentMetrics);
	let cores = $derived(info?.cpu_cores || 0);
	let cpuPct = $derived(m ? Math.min(Math.max(Math.round(m.cpu), 0), 100) : null);
	let ramPct = $derived(m && m.ram_total > 0 ? Math.round((m.ram_used / m.ram_total) * 100) : null);
	let swapPct = $derived(m && m.swap_total > 0 ? Math.round((m.swap_used / m.swap_total) * 100) : null);
	let diskPct = $derived(m && m.disk_total > 0 ? Math.round((m.disk_used / m.disk_total) * 100) : null);
	let loadPct = $derived(m ? loadPercent(m.load_1, cores) : null);
	// Metrics carry nanoseconds; the boot time follows from the uptime.
	let uptimeSecs = $derived(m ? uptimeSeconds(m.uptime) : 0);
	let liveUptime = $derived(formatUptime(uptimeSecs, $language) ?? (info ? cleanUptimeText(info.uptime) : null));
	let bootedAt = $derived.by(() => {
		if (uptimeSecs <= 0) return '';
		const boot = new Date(Date.now() - uptimeSecs * 1000);
		const locale = $language === 'id' ? 'id-ID' : 'en-GB';
		const options: Intl.DateTimeFormatOptions = { dateStyle: 'medium', timeStyle: 'short' };
		try {
			// Shown in the server's timezone, like the server clock.
			return boot.toLocaleString(locale, { ...options, timeZone: info?.timezone || undefined });
		} catch {
			return boot.toLocaleString(locale, options);
		}
	});
	let serverTime = $derived(info ? formatServerTime(now, info.timezone, $language === 'id' ? 'id-ID' : 'en-GB') : null);
	let serverOffset = $derived(info ? utcOffsetLabel(now, info.timezone) : '');
	let previewOffset = $derived(newTimezone ? utcOffsetLabel(now, newTimezone) : '');

	const toneBar = { green: 'bg-green-500', yellow: 'bg-yellow-500', red: 'bg-red-500', gray: 'bg-gray-600' };
	const toneText = { green: 'text-green-300', yellow: 'text-yellow-300', red: 'text-red-300', gray: 'text-gray-400' };

	async function loadInfo() {
		try {
			info = await api.get<ServerInfo>('/api/v1/server/info');
			error = '';
			if (!editingHostname) newHostname = info.hostname;
			if (!editingTimezone) newTimezone = info.timezone;
		} catch (err) {
			error = err instanceof Error ? err.message : translate($language, 'srv.errorLoadInfo');
		} finally {
			loading = false;
		}
	}

	async function refresh() {
		if (refreshing) return;
		refreshing = true;
		await loadInfo();
		refreshing = false;
	}

	async function handleReboot() {
		if (!info || rebootConfirm.trim() !== info.hostname || rebooting) return;
		rebooting = true;
		try {
			await api.post('/api/v1/server/reboot');
			toast.success(translate($language, 'srv.msgReboot'));
			rebootStarted = true;
			rebootOpen = false;
		} catch (err) {
			toast.error(err instanceof Error ? err.message : translate($language, 'srv.errorReboot'));
		} finally {
			rebooting = false;
		}
	}

	function openReboot() {
		rebootConfirm = '';
		rebootOpen = true;
	}

	async function saveHostname() {
		if (!hostnameValid || savingHostname) return;
		savingHostname = true;
		try {
			await api.post('/api/v1/server/hostname', { hostname: newHostname.trim() });
			editingHostname = false;
			toast.success(translate($language, 'srv.msgHostname'));
			await loadInfo();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : translate($language, 'srv.errorHostname'));
		} finally {
			savingHostname = false;
		}
	}

	function cancelHostname() {
		editingHostname = false;
		newHostname = info?.hostname ?? '';
	}

	async function saveTimezone() {
		if (savingTimezone || !newTimezone || newTimezone === info?.timezone) return;
		tzOpen = false;
		savingTimezone = true;
		try {
			await api.post('/api/v1/server/timezone', { timezone: newTimezone });
			editingTimezone = false;
			toast.success(translate($language, 'srv.msgTimezone'));
			await loadInfo();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : translate($language, 'srv.errorTimezone'));
		} finally {
			savingTimezone = false;
		}
	}

	function cancelTimezone() {
		editingTimezone = false;
		tzOpen = false;
		tzQuery = '';
		newTimezone = info?.timezone ?? '';
	}

	// Keep the rendered list light; searching narrows to the wanted entry.
	let filteredTimezones = $derived.by(() => {
		const q = tzQuery.trim().toLowerCase().replace(/\s+/g, '_');
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

	async function copyText(text: string, field: string) {
		try {
			await navigator.clipboard.writeText(text);
			copiedField = field;
			setTimeout(() => {
				if (copiedField === field) copiedField = '';
			}, 1500);
		} catch {
			toast.error(translate($language, 'srv.copyFailed'));
		}
	}

	onMount(() => {
		void loadInfo();
		connectMetrics();
		clock = setInterval(() => (now = new Date()), 1000);
	});

	onDestroy(() => {
		disconnectMetrics();
		if (clock) clearInterval(clock);
	});
</script>

<svelte:window onclick={handleWindowClick} />

{#snippet copyButton(text: string, field: string)}
	<button
		type="button"
		onclick={() => copyText(text, field)}
		title={translate($language, 'srv.copy')}
		aria-label={translate($language, 'srv.copy')}
		class="cursor-pointer rounded-md p-1 text-gray-500 transition hover:bg-gray-700 hover:text-white"
	>
		{#if copiedField === field}
			<svg class="h-3.5 w-3.5 text-green-400" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="2.5" aria-hidden="true">
				<path stroke-linecap="round" stroke-linejoin="round" d="M4.5 12.75l6 6 9-13.5" />
			</svg>
		{:else}
			<svg class="h-3.5 w-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="2" aria-hidden="true">
				<path stroke-linecap="round" stroke-linejoin="round" d="M15.75 17.25v3.375c0 .621-.504 1.125-1.125 1.125h-9.75a1.125 1.125 0 0 1-1.125-1.125V7.875c0-.621.504-1.125 1.125-1.125H6.75a9.06 9.06 0 0 1 1.5.124m7.5 10.376h3.375c.621 0 1.125-.504 1.125-1.125V11.25c0-4.46-3.243-8.161-7.5-8.876a9.06 9.06 0 0 0-1.5-.124H9.375c-.621 0-1.125.504-1.125 1.125v3.5m7.5 10.375H9.375a1.125 1.125 0 0 1-1.125-1.125v-9.25m12 6.625v-1.875a3.375 3.375 0 0 0-3.375-3.375h-1.5a1.125 1.125 0 0 1-1.125-1.125v-1.5a3.375 3.375 0 0 0-3.375-3.375H9.75" />
			</svg>
		{/if}
	</button>
{/snippet}

{#snippet meter(label: string, percent: number | null, detail: string, tonePercent: number | null = percent)}
	{@const tone = usageTone(tonePercent)}
	<div class="rounded-xl border border-gray-700 bg-gray-800 p-4">
		<div class="flex items-baseline justify-between gap-2">
			<p class="text-[11px] font-medium uppercase tracking-wider text-gray-400">{label}</p>
			<p class="text-xl font-bold tabular-nums {percent === null ? 'text-gray-600' : toneText[tone]}">{percent === null ? '—' : `${percent}%`}</p>
		</div>
		<div class="mt-2 h-1.5 overflow-hidden rounded-full bg-gray-700">
			<div class="h-full rounded-full transition-all duration-500 {toneBar[tone]}" style="width: {Math.min(percent ?? 0, 100)}%"></div>
		</div>
		<p class="mt-2 truncate text-xs text-gray-500" title={detail}>{detail}</p>
	</div>
{/snippet}

<div class="space-y-6">
	<!-- Header -->
	<div class="flex flex-wrap items-start justify-between gap-4">
		<div class="flex min-w-0 items-center gap-4">
			<span class="flex h-12 w-12 shrink-0 items-center justify-center rounded-2xl bg-blue-500/10 text-blue-400" aria-hidden="true">
				<svg class="h-6 w-6" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="1.6">
					<path stroke-linecap="round" stroke-linejoin="round" d="M21.75 17.25v-.228a4.5 4.5 0 0 0-.12-1.03l-2.268-9.64a3.375 3.375 0 0 0-3.285-2.602H7.923a3.375 3.375 0 0 0-3.285 2.602l-2.268 9.64a4.5 4.5 0 0 0-.12 1.03v.228m19.5 0a3 3 0 0 1-3 3H5.25a3 3 0 0 1-3-3m19.5 0a3 3 0 0 0-3-3H5.25a3 3 0 0 0-3 3m16.5 0h.008v.008h-.008v-.008Zm-3 0h.008v.008h-.008v-.008Z" />
				</svg>
			</span>
			<div class="min-w-0">
				<p class="text-xs font-medium uppercase tracking-wider text-gray-500">{translate($language, 'srv.title')}</p>
				<h2 class="truncate text-2xl font-bold text-white">{info?.hostname || translate($language, 'srv.title')}</h2>
				{#if info}
					<div class="mt-1 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-gray-400">
						<span class="inline-flex items-center gap-1.5">
							<span class="h-1.5 w-1.5 rounded-full {m ? 'bg-green-400' : 'bg-gray-500'}"></span>
							{m ? translate($language, 'srv.live') : translate($language, 'srv.connecting')}
						</span>
						<span>{info.os}</span>
						<span class="inline-flex items-center gap-1 font-mono">{info.ip}{@render copyButton(info.ip, 'header-ip')}</span>
						{#if liveUptime}<span title={bootedAt ? `${translate($language, 'srv.bootedAt')}: ${bootedAt}` : undefined}>{translate($language, 'srv.uptime')}: {liveUptime}</span>{/if}
					</div>
				{/if}
			</div>
		</div>
		<div class="flex items-center gap-2">
			<button
				type="button"
				onclick={refresh}
				disabled={refreshing || loading}
				title={translate($language, 'srv.refresh')}
				aria-label={translate($language, 'srv.refresh')}
				class="cursor-pointer rounded-lg border border-gray-600 bg-gray-800 p-2 text-gray-300 transition hover:bg-gray-700 hover:text-white disabled:opacity-50"
			>
				<svg class="h-4 w-4 {refreshing ? 'animate-spin' : ''}" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="2" aria-hidden="true">
					<path stroke-linecap="round" stroke-linejoin="round" d="M16.023 9.348h4.992v-.001M2.985 19.644v-4.992m0 0h4.992m-4.993 0 3.181 3.183a8.25 8.25 0 0 0 13.803-3.7M4.031 9.865a8.25 8.25 0 0 1 13.803-3.7l3.181 3.182m0-4.991v4.99" />
				</svg>
			</button>
			{#if canReboot && info}
				<button
					type="button"
					onclick={openReboot}
					disabled={rebootStarted}
					class="inline-flex cursor-pointer items-center gap-1.5 rounded-lg border border-red-600/50 bg-red-600/15 px-3.5 py-2 text-sm font-semibold text-red-200 transition hover:bg-red-600/30 disabled:opacity-50"
				>
					<svg class="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="2" aria-hidden="true">
						<path stroke-linecap="round" stroke-linejoin="round" d="M5.636 5.636a9 9 0 1 0 12.728 0M12 3v9" />
					</svg>
					{translate($language, 'srv.rebootServer')}
				</button>
			{/if}
		</div>
	</div>

	{#if rebootStarted}
		<div class="rounded-lg border border-yellow-700/50 bg-yellow-900/20 px-4 py-3 text-sm text-yellow-200">
			{translate($language, 'srv.rebootBanner')}
		</div>
	{/if}

	{#if loading}
		<div class="grid grid-cols-2 gap-3 lg:grid-cols-4">
			{#each Array(4) as _}
				<div class="h-24 animate-pulse rounded-xl border border-gray-700 bg-gray-800"></div>
			{/each}
		</div>
		<div class="grid gap-6 lg:grid-cols-2">
			<div class="h-72 animate-pulse rounded-xl border border-gray-700 bg-gray-800"></div>
			<div class="h-72 animate-pulse rounded-xl border border-gray-700 bg-gray-800"></div>
		</div>
	{:else if error}
		<div class="flex flex-wrap items-center justify-between gap-2 rounded-lg border border-red-700 bg-red-900/30 p-4 text-sm text-red-300">
			<span>{error}</span>
			<button type="button" onclick={refresh} class="cursor-pointer underline">{translate($language, 'srv.retry')}</button>
		</div>
	{:else if info}
		<!-- Live resources -->
		<div class="grid grid-cols-2 gap-3 lg:grid-cols-4">
			{@render meter(
				translate($language, 'srv.cpu'),
				cpuPct,
				cores ? translate($language, 'srv.coresCount').replace('{count}', String(cores)) : info.cpu
			)}
			{@render meter(
				translate($language, 'srv.memory'),
				ramPct,
				m ? `${formatBytes(m.ram_used)} / ${formatBytes(m.ram_total)}${swapPct !== null ? ` · swap ${swapPct}%` : ''}` : info.ram
			)}
			{@render meter(
				translate($language, 'srv.disk'),
				diskPct,
				m ? `${formatBytes(m.disk_used)} / ${formatBytes(m.disk_total)}` : info.disk
			)}
			{@render meter(
				translate($language, 'srv.load'),
				loadPct === null ? null : Math.min(loadPct, 999),
				m ? `${m.load_1.toFixed(2)} · ${m.load_5.toFixed(2)} · ${m.load_15.toFixed(2)}` : translate($language, 'srv.connecting'),
				loadPct
			)}
		</div>

		<div class="grid items-start gap-6 lg:grid-cols-2">
			<!-- System information -->
			<section class="rounded-xl border border-gray-700 bg-gray-800">
				<h3 class="border-b border-gray-700 px-5 py-3 text-sm font-semibold text-white">{translate($language, 'srv.systemInfo')}</h3>
				<dl class="divide-y divide-gray-700/60">
					{#each [
						{ key: 'srv.os', value: info.os },
						{ key: 'srv.kernel', value: info.kernel, mono: true },
						{ key: 'srv.cpuModel', value: info.cpu_model || info.cpu },
						{ key: 'srv.cpuCores', value: info.cpu_cores ? String(info.cpu_cores) : '' },
						{ key: 'srv.memory', value: info.ram },
						{ key: 'srv.disk', value: info.disk },
						{ key: 'srv.ipAddress', value: info.ip, mono: true, copy: true },
						{ key: 'srv.uptime', value: liveUptime ?? '' },
						{ key: 'srv.bootedAt', value: bootedAt }
					].filter((row) => row.value) as row}
						<div class="flex items-center justify-between gap-4 px-5 py-2.5">
							<dt class="shrink-0 text-xs text-gray-400">{translate($language, row.key)}</dt>
							<dd class="flex min-w-0 items-center gap-1 text-right text-sm text-gray-100 {row.mono ? 'font-mono' : ''}">
								<span class="truncate" title={row.value}>{row.value}</span>
								{#if row.copy}{@render copyButton(row.value, `info-${row.key}`)}{/if}
							</dd>
						</div>
					{/each}
				</dl>
			</section>

			<!-- Settings -->
			<div class="space-y-6">
				<section class="rounded-xl border border-gray-700 bg-gray-800">
					<h3 class="border-b border-gray-700 px-5 py-3 text-sm font-semibold text-white">{translate($language, 'srv.settings')}</h3>

					<!-- Hostname -->
					<div class="border-b border-gray-700/60 px-5 py-4">
						<div class="flex items-start justify-between gap-3">
							<div class="min-w-0">
								<p class="text-xs text-gray-400">{translate($language, 'srv.hostname')}</p>
								{#if !editingHostname}
									<p class="mt-0.5 truncate font-mono text-sm text-white">{info.hostname}</p>
								{/if}
							</div>
							{#if !editingHostname && canHostname}
								<button
									type="button"
									onclick={() => { newHostname = info?.hostname ?? ''; editingHostname = true; }}
									class="shrink-0 cursor-pointer rounded-md border border-gray-600 bg-gray-700 px-2.5 py-1 text-xs font-medium text-gray-200 transition hover:bg-gray-600"
								>
									{translate($language, 'srv.edit')}
								</button>
							{/if}
						</div>
						{#if editingHostname}
							<form class="mt-2 space-y-2" onsubmit={(e) => { e.preventDefault(); saveHostname(); }}>
								<input
									type="text"
									bind:value={newHostname}
									onkeydown={(e) => { if (e.key === 'Escape') cancelHostname(); }}
									spellcheck="false"
									autocomplete="off"
									aria-invalid={!hostnameValid}
									aria-label={translate($language, 'srv.hostname')}
									class="w-full rounded-lg border bg-gray-900 px-3 py-2 font-mono text-sm text-white focus:outline-none focus:ring-2 {hostnameValid ? 'border-gray-600 focus:border-blue-500 focus:ring-blue-500/40' : 'border-red-600 focus:ring-red-500/40'}"
								/>
								<p class="text-xs {hostnameValid ? 'text-gray-500' : 'text-red-300'}">{translate($language, hostnameValid ? 'srv.hostnameHint' : 'srv.hostnameInvalid')}</p>
								<div class="flex justify-end gap-2">
									<button type="button" onclick={cancelHostname} class="cursor-pointer rounded-lg border border-gray-600 bg-gray-700 px-3 py-1.5 text-xs font-medium text-gray-200 transition hover:bg-gray-600">
										{translate($language, 'srv.cancel')}
									</button>
									<button
										type="submit"
										disabled={!hostnameValid || savingHostname || newHostname.trim() === info.hostname}
										class="cursor-pointer rounded-lg bg-blue-600 px-3 py-1.5 text-xs font-semibold text-white transition hover:bg-blue-700 disabled:cursor-default disabled:opacity-40"
									>
										{savingHostname ? translate($language, 'srv.saving') : translate($language, 'srv.save')}
									</button>
								</div>
							</form>
						{/if}
					</div>

					<!-- Timezone + live server clock -->
					<div class="px-5 py-4">
						<div class="flex items-start justify-between gap-3">
							<div class="min-w-0">
								<p class="text-xs text-gray-400">{translate($language, 'srv.timezone')}</p>
								{#if !editingTimezone}
									<p class="mt-0.5 text-sm text-white">
										{info.timezone}
										{#if serverOffset}<span class="ml-1 text-xs text-gray-500">{serverOffset}</span>{/if}
									</p>
								{/if}
								{#if serverTime}
									<p class="mt-1 font-mono text-xs tabular-nums text-blue-300">
										{translate($language, 'srv.serverTime')}: {serverTime}
									</p>
								{/if}
							</div>
							{#if !editingTimezone && canTimezone}
								<button
									type="button"
									onclick={() => { newTimezone = info?.timezone ?? ''; editingTimezone = true; }}
									class="shrink-0 cursor-pointer rounded-md border border-gray-600 bg-gray-700 px-2.5 py-1 text-xs font-medium text-gray-200 transition hover:bg-gray-600"
								>
									{translate($language, 'srv.edit')}
								</button>
							{/if}
						</div>

						{#if editingTimezone}
							<div class="mt-3 space-y-3" bind:this={tzEditRoot}>
								<div class="flex flex-wrap gap-1.5">
									{#each quickTimezones as tz}
										<button
											type="button"
											onclick={() => selectTz(tz)}
											class="cursor-pointer rounded-full border px-2.5 py-0.5 text-xs transition {newTimezone === tz ? 'border-blue-500 bg-blue-500/15 text-blue-200' : 'border-gray-600 text-gray-300 hover:border-gray-500'}"
										>
											{tz}
										</button>
									{/each}
								</div>
								<div class="relative">
									<svg class="pointer-events-none absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-gray-400" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="2" aria-hidden="true">
										<path stroke-linecap="round" stroke-linejoin="round" d="M21 21l-4.35-4.35M17 10.5a6.5 6.5 0 11-13 0 6.5 6.5 0 0113 0z" />
									</svg>
									<input
										type="text"
										bind:value={tzQuery}
										onfocus={openTzList}
										oninput={() => { tzOpen = true; tzHighlight = 0; }}
										onkeydown={handleTzKeydown}
										placeholder={translate($language, 'srv.searchTimezone')}
										aria-label={translate($language, 'srv.searchTimezoneAria')}
										autocomplete="off"
										class="w-full rounded-lg border border-gray-600 bg-gray-900 py-2 pl-8 pr-3 text-sm text-white placeholder:text-gray-500 focus:border-blue-500 focus:outline-none focus:ring-2 focus:ring-blue-500/40"
									/>
									{#if tzOpen}
										<ul class="absolute z-20 mt-1 max-h-60 w-full overflow-y-auto rounded-lg border border-gray-600 bg-gray-800 py-1 shadow-lg" role="listbox" aria-label={translate($language, 'srv.timezoneOptions')}>
											{#each filteredTimezones as tz, i}
												<li>
													<button
														type="button"
														onclick={() => selectTz(tz)}
														role="option"
														aria-selected={tz === newTimezone}
														class="flex w-full cursor-pointer items-center justify-between gap-2 px-3 py-1.5 text-left text-sm {i === tzHighlight ? 'bg-blue-600/70 text-white' : 'text-gray-200 hover:bg-gray-700'} {tz === newTimezone ? 'font-medium' : ''}"
													>
														<span>{tz}</span>
														<span class="text-[11px] text-gray-400">{utcOffsetLabel(now, tz)}</span>
													</button>
												</li>
											{:else}
												<li class="px-3 py-2 text-sm text-gray-400">{translate($language, 'srv.noMatchingTimezone')}</li>
											{/each}
										</ul>
									{/if}
								</div>
								<p class="text-xs text-gray-400">
									{translate($language, 'srv.selected')} <span class="font-medium text-gray-200">{newTimezone}</span>
									{#if previewOffset}<span class="text-gray-500"> · {previewOffset} · {formatServerTime(now, newTimezone, $language === 'id' ? 'id-ID' : 'en-GB')}</span>{/if}
								</p>
								<p class="text-xs text-gray-500">{translate($language, 'srv.timezoneHint')}</p>
								<div class="flex justify-end gap-2">
									<button type="button" onclick={cancelTimezone} class="cursor-pointer rounded-lg border border-gray-600 bg-gray-700 px-3 py-1.5 text-xs font-medium text-gray-200 transition hover:bg-gray-600">
										{translate($language, 'srv.cancel')}
									</button>
									<button
										type="button"
										onclick={saveTimezone}
										disabled={savingTimezone || !newTimezone || newTimezone === info.timezone}
										class="cursor-pointer rounded-lg bg-blue-600 px-3 py-1.5 text-xs font-semibold text-white transition hover:bg-blue-700 disabled:cursor-default disabled:opacity-40"
									>
										{savingTimezone ? translate($language, 'srv.saving') : translate($language, 'srv.save')}
									</button>
								</div>
							</div>
						{/if}
					</div>
				</section>

				<!-- SSH Port Management -->
				<SshPortCard />
			</div>
		</div>

		<!-- Disk partitions -->
		{#if info.disk_partitions && info.disk_partitions.length > 0}
			<section class="space-y-3">
				<div class="flex items-center justify-between gap-2">
					<h3 class="text-sm font-semibold uppercase tracking-wider text-gray-400">{translate($language, 'srv.diskPartitions')}</h3>
					<a href="/disk" class="text-xs font-medium text-blue-400 hover:underline">{translate($language, 'srv.openDisk')} →</a>
				</div>
				<div class="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
					{#each info.disk_partitions as partition (partition.device + partition.mount)}
						{@const pct = parsePercent(partition.use_percent)}
						{@const tone = usageTone(pct)}
						<div class="rounded-xl border border-gray-700 bg-gray-800 p-4">
							<div class="flex items-start justify-between gap-2">
								<div class="min-w-0">
									<p class="truncate font-mono text-sm font-semibold text-white" title={partition.mount}>{partition.mount}</p>
									<p class="truncate font-mono text-[11px] text-gray-500" title={partition.device}>{partition.device}</p>
								</div>
								<span class="text-lg font-bold tabular-nums {toneText[tone]}">{partition.use_percent}</span>
							</div>
							<div class="mt-3 h-1.5 overflow-hidden rounded-full bg-gray-700">
								<div class="h-full rounded-full {toneBar[tone]}" style="width: {pct ?? 0}%"></div>
							</div>
							<div class="mt-2 flex justify-between text-xs text-gray-400">
								<span>{translate($language, 'srv.used')} <span class="text-gray-200">{partition.used}</span> / {partition.size}</span>
								<span>{translate($language, 'srv.free')} <span class="text-gray-200">{partition.available}</span></span>
							</div>
						</div>
					{/each}
				</div>
			</section>
		{/if}

		<!-- Network interfaces -->
		{#if info.network_interfaces && info.network_interfaces.length > 0}
			<section class="overflow-hidden rounded-xl border border-gray-700 bg-gray-800">
				<h3 class="border-b border-gray-700 px-5 py-3 text-sm font-semibold text-white">{translate($language, 'srv.networkInterfaces')}</h3>
				<div class="overflow-x-auto">
					<table class="w-full text-sm">
						<thead>
							<tr class="border-b border-gray-700 text-left text-[11px] uppercase tracking-wider text-gray-400">
								<th class="px-5 py-2.5 font-medium">{translate($language, 'srv.name')}</th>
								<th class="px-5 py-2.5 font-medium">{translate($language, 'srv.ipAddress')}</th>
								<th class="px-5 py-2.5 font-medium">{translate($language, 'srv.macAddress')}</th>
							</tr>
						</thead>
						<tbody class="divide-y divide-gray-700/60">
							{#each info.network_interfaces as iface (iface.name)}
								<tr class="transition hover:bg-gray-700/30">
									<td class="px-5 py-2.5">
										<span class="inline-flex items-center gap-2 font-mono text-white">
											<span class="h-1.5 w-1.5 rounded-full {iface.ip ? 'bg-green-400' : 'bg-gray-500'}" title={iface.ip ? translate($language, 'srv.hasAddress') : translate($language, 'srv.noAddress')}></span>
											{iface.name}
										</span>
									</td>
									<td class="px-5 py-2.5 font-mono text-gray-300">
										{#if iface.ip}
											<span class="inline-flex items-center gap-1">{iface.ip}{@render copyButton(iface.ip, `ip-${iface.name}`)}</span>
										{:else}—{/if}
									</td>
									<td class="px-5 py-2.5 font-mono text-gray-400">{iface.mac || '—'}</td>
								</tr>
							{/each}
						</tbody>
					</table>
				</div>
			</section>
		{/if}
	{/if}
</div>

{#if rebootOpen && info}
	<Modal title={translate($language, 'srv.rebootServer')} onclose={() => (rebootOpen = false)} busy={rebooting} closeLabel={translate($language, 'srv.cancel')}>
		<form id="reboot-form" class="space-y-3" onsubmit={(e) => { e.preventDefault(); handleReboot(); }}>
			<p class="text-sm leading-6 text-gray-300">{translate($language, 'srv.confirmReboot')}</p>
			<ul class="list-disc space-y-1 pl-5 text-xs text-gray-400">
				<li>{translate($language, 'srv.rebootEffectSites')}</li>
				<li>{translate($language, 'srv.rebootEffectSessions')}</li>
			</ul>
			<label class="block space-y-1 text-sm text-gray-300">
				<span>{rebootPrompt[0]}<code class="rounded bg-gray-900 px-1 font-mono text-white">{info.hostname}</code>{rebootPrompt[1] ?? ''}</span>
				<input
					type="text"
					bind:value={rebootConfirm}
					autocomplete="off"
					spellcheck="false"
					class="w-full rounded-lg border border-gray-600 bg-gray-900 px-3 py-2 font-mono text-sm text-white focus:border-red-500 focus:outline-none focus:ring-2 focus:ring-red-500/40"
				/>
			</label>
		</form>
		{#snippet footer()}
			<button type="button" onclick={() => (rebootOpen = false)} disabled={rebooting} class="cursor-pointer rounded-lg border border-gray-600 bg-gray-700 px-4 py-2 text-sm font-medium text-gray-200 transition hover:bg-gray-600 disabled:opacity-40">
				{translate($language, 'srv.cancel')}
			</button>
			<button
				type="submit"
				form="reboot-form"
				disabled={rebooting || rebootConfirm.trim() !== info?.hostname}
				class="cursor-pointer rounded-lg bg-red-600 px-4 py-2 text-sm font-semibold text-white transition hover:bg-red-700 disabled:cursor-default disabled:opacity-40"
			>
				{rebooting ? translate($language, 'srv.rebooting') : translate($language, 'srv.rebootNow')}
			</button>
		{/snippet}
	</Modal>
{/if}
