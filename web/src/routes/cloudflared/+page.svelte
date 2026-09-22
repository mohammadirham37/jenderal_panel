<script lang="ts">
	import { onMount } from 'svelte';
	import { api } from '$lib/api';
	import { language, translate } from '$lib/stores/language';
	import TaskProgress from '$lib/components/TaskProgress.svelte';

	interface TunnelStatus {
		installed: boolean;
		version: string;
		pinned_version: string;
		token_installed: boolean;
		service_state: string;
		apt_service_running: boolean;
	}

	let status = $state<TunnelStatus | null>(null);
	let loading = $state(true);
	let error = $state('');

	let token = $state('');
	let showToken = $state(false);
	let connecting = $state(false);
	let connectError = $state('');
	let installTaskId = $state('');
	let connectTaskId = $state('');

	let logs = $state('');
	let logsLoading = $state(false);
	let actionError = $state('');

	const t = (key: string) => translate($language, key);

	async function load() {
		error = '';
		try {
			status = await api.get<TunnelStatus>('/api/v1/cloudflared');
		} catch (e) {
			error = e instanceof Error ? e.message : t('cft.loadFailed');
		} finally {
			loading = false;
		}
	}

	async function loadLogs() {
		logsLoading = true;
		try {
			const res = await api.get<{ lines: number; output: string }>('/api/v1/cloudflared/logs?lines=200');
			logs = res.output ?? '';
		} catch {
			logs = '';
		} finally {
			logsLoading = false;
		}
	}

	async function install() {
		actionError = '';
		try {
			const res = await api.post<{ task_id: string }>('/api/v1/cloudflared/install', {});
			installTaskId = res.task_id;
		} catch (e) {
			actionError = e instanceof Error ? e.message : t('cft.loadFailed');
		}
	}

	async function connect() {
		connectError = '';
		actionError = '';
		if (!token.trim()) {
			connectError = t('cft.token.required');
			return;
		}
		connecting = true;
		try {
			const res = await api.post<{ task_id: string }>('/api/v1/cloudflared/connect', { token: token.trim() });
			connectTaskId = res.task_id;
			token = '';
		} catch (e) {
			connectError = e instanceof Error ? e.message : t('cft.loadFailed');
		} finally {
			connecting = false;
		}
	}

	async function disconnect() {
		if (!confirm(t('cft.disconnectConfirm'))) return;
		actionError = '';
		try {
			await api.post('/api/v1/cloudflared/disconnect', {});
			await load();
		} catch (e) {
			actionError = e instanceof Error ? e.message : t('cft.loadFailed');
		}
	}

	async function restart() {
		actionError = '';
		try {
			await api.post('/api/v1/cloudflared/restart', {});
			await load();
		} catch (e) {
			actionError = e instanceof Error ? e.message : t('cft.loadFailed');
		}
	}

	function stateBadgeClass(state: string): string {
		if (state === 'active') return 'bg-green-500/15 text-green-300 border-green-500/30';
		if (state === 'failed') return 'bg-red-500/15 text-red-300 border-red-500/30';
		return 'bg-gray-500/15 text-gray-300 border-gray-500/30';
	}

	function stateDotClass(state: string): string {
		if (state === 'active') return 'bg-green-400';
		if (state === 'failed') return 'bg-red-400';
		return 'bg-gray-500';
	}

	onMount(() => {
		load();
		loadLogs();
	});
</script>

<div class="mx-auto max-w-7xl px-4 py-8 space-y-6">
	<header class="flex items-start gap-4">
		<div class="rounded-xl bg-blue-500/10 p-2.5 text-blue-400">
			<svg class="h-7 w-7" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="1.5">
				<path stroke-linecap="round" stroke-linejoin="round" d="M2.25 15a4.5 4.5 0 004.5 4.5H18a3.75 3.75 0 001.332-7.257 3 3 0 00-3.758-3.848 5.25 5.25 0 00-10.233 2.33A4.502 4.502 0 002.25 15z" />
			</svg>
		</div>
		<div>
			<h1 class="text-2xl font-semibold text-gray-100">{t('cft.title')}</h1>
			<p class="mt-1 text-sm text-gray-400">{t('cft.subtitle')}</p>
		</div>
	</header>

	{#if error}
		<div class="rounded-xl border border-red-500/30 bg-red-500/10 px-4 py-3 text-sm text-red-300">{error}</div>
	{/if}
	{#if actionError}
		<div class="rounded-xl border border-red-500/30 bg-red-500/10 px-4 py-3 text-sm text-red-300">{actionError}</div>
	{/if}

	{#if loading}
		<div class="h-24 animate-pulse rounded-2xl border border-white/5 bg-gray-800/60"></div>
		<div class="grid grid-cols-1 gap-4 md:grid-cols-3">
			<div class="h-36 animate-pulse rounded-2xl border border-white/5 bg-gray-800/60"></div>
			<div class="h-36 animate-pulse rounded-2xl border border-white/5 bg-gray-800/60"></div>
			<div class="h-36 animate-pulse rounded-2xl border border-white/5 bg-gray-800/60"></div>
		</div>
	{:else if status}
		{#if status.apt_service_running}
			<div class="rounded-xl border border-yellow-500/30 bg-yellow-500/10 px-4 py-3 text-sm text-yellow-200">
				{t('cft.conflict')}
			</div>
		{/if}

		<!-- Connection band: what a tunnel is — an outbound link to the edge -->
		<section class="rounded-2xl border border-white/5 bg-gray-800/60 p-6" data-testid="connection-band">
			<div class="flex items-center">
				<div class="flex shrink-0 items-center gap-3">
					<div class="relative flex">
						<span class="h-2.5 w-2.5 rounded-full {stateDotClass(status.service_state)} {status.service_state === 'active' ? 'animate-ping opacity-60 motion-reduce:animate-none' : ''}"></span>
						<span class="absolute h-2.5 w-2.5 rounded-full {stateDotClass(status.service_state)}"></span>
					</div>
					<div class="rounded-xl bg-blue-500/10 p-2 text-blue-400">
						<svg class="h-5 w-5" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="1.5">
							<path stroke-linecap="round" stroke-linejoin="round" d="M21.75 17.25v-.228a4.5 4.5 0 00-.12-1.03l-2.268-9.64a3.375 3.375 0 00-3.285-2.602H7.923a3.375 3.375 0 00-3.285 2.602l-2.268 9.64a4.5 4.5 0 00-.12 1.03v.228m19.5 0a3 3 0 01-3 3H5.25a3 3 0 01-3-3m19.5 0a3 3 0 00-3-3H5.25a3 3 0 00-3 3m16.5 0h.008v.008h-.008v-.008zm-3 0h.008v.008h-.008v-.008z" />
						</svg>
					</div>
					<div>
						<p class="text-sm font-semibold text-gray-100">{t('cft.hero.server')}</p>
						<p class="text-xs text-gray-500">{t('cft.state.' + status.service_state)}</p>
					</div>
				</div>

				<div class="relative mx-4 h-px flex-1 bg-gray-700">
					<span class="absolute left-1/2 top-1/2 -translate-x-1/2 -translate-y-1/2 rounded-full border border-gray-700 bg-gray-900 px-3 py-1 text-[11px] font-semibold uppercase tracking-[0.14em] text-gray-400 whitespace-nowrap">
						{t('cft.hero.outbound')}
					</span>
				</div>

				<div class="flex shrink-0 items-center gap-3">
					<div class="rounded-xl bg-blue-500/10 p-2 text-blue-400">
						<svg class="h-5 w-5" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="1.5">
							<path stroke-linecap="round" stroke-linejoin="round" d="M2.25 15a4.5 4.5 0 004.5 4.5H18a3.75 3.75 0 001.332-7.257 3 3 0 00-3.758-3.848 5.25 5.25 0 00-10.233 2.33A4.502 4.502 0 002.25 15z" />
						</svg>
					</div>
					<p class="text-sm font-semibold text-gray-100">{t('cft.hero.edge')}</p>
					<span class="h-2.5 w-2.5 rounded-full {stateDotClass(status.service_state)}"></span>
				</div>
			</div>
		</section>

		<!-- Status tiles -->
		<div class="grid grid-cols-1 gap-4 md:grid-cols-3">
			<!-- Binary -->
			<section class="flex flex-col rounded-2xl border border-white/5 bg-gray-800/60 p-5">
				<div class="flex items-center justify-between">
					<p class="text-xs font-semibold uppercase tracking-[0.14em] text-gray-400">{t('cft.status.binary')}</p>
					<div class="rounded-xl bg-blue-500/10 p-1.5 text-blue-400">
						<svg class="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="1.5">
							<path stroke-linecap="round" stroke-linejoin="round" d="M8.25 3v1.5M4.5 8.25H3m18 0h-1.5M4.5 12H3m18 0h-1.5m-15 3.75H3m18 0h-1.5M8.25 19.5V21M12 3v1.5m0 15V21m3.75-18v1.5m0 15V21m-9-1.5h10.5a2.25 2.25 0 002.25-2.25V6.75a2.25 2.25 0 00-2.25-2.25H6.75A2.25 2.25 0 004.5 6.75v10.5a2.25 2.25 0 002.25 2.25zm.75-12h9v9h-9v-9z" />
						</svg>
					</div>
				</div>
				<p class="mt-3 font-mono text-lg font-semibold text-gray-100">
					{#if status.installed}{status.version || '?'}{:else}{t('cft.status.notInstalled')}{/if}
				</p>
				<p class="mt-1 text-xs text-gray-500">
					{t('cft.status.pinned')}: <span class="font-mono">{status.pinned_version}</span>
					{#if status.installed && status.version !== status.pinned_version}
						<span class="ml-1 rounded border border-yellow-500/30 bg-yellow-500/10 px-1.5 py-0.5 text-yellow-200">{t('cft.status.updateAvailable')}</span>
					{/if}
				</p>
				<div class="mt-auto pt-4">
					<button
						class="w-full rounded-xl bg-blue-600 px-4 py-2 text-sm font-medium text-white hover:bg-blue-500 disabled:opacity-50"
						disabled={!!installTaskId}
						onclick={install}
					>
						{t('cft.install.button').replace('{version}', status.pinned_version)}
					</button>
					{#if installTaskId}
						<div class="mt-3">
							<TaskProgress bind:taskId={installTaskId} storageKey="jenderal_cft_install" onComplete={() => { load(); loadLogs(); }} />
						</div>
					{/if}
				</div>
			</section>

			<!-- Service -->
			<section class="flex flex-col rounded-2xl border border-white/5 bg-gray-800/60 p-5">
				<div class="flex items-center justify-between">
					<p class="text-xs font-semibold uppercase tracking-[0.14em] text-gray-400">{t('cft.status.service')}</p>
					<div class="rounded-xl bg-blue-500/10 p-1.5 text-blue-400">
						<svg class="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="1.5">
							<path stroke-linecap="round" stroke-linejoin="round" d="M3.75 13.5l10.5-11.25L12 10.5h8.25L9.75 21.75 12 13.5H3.75z" />
						</svg>
					</div>
				</div>
				<p class="mt-3">
					<span class="inline-flex items-center rounded-full border px-2.5 py-1 text-xs font-semibold {stateBadgeClass(status.service_state)}">
						{t('cft.state.' + status.service_state)}
					</span>
				</p>
				<div class="mt-auto flex gap-2 pt-4">
					<button
						class="flex-1 rounded-xl border border-gray-700 px-3 py-2 text-sm text-gray-200 hover:bg-white/5 disabled:opacity-50"
						disabled={!status.token_installed}
						onclick={restart}
					>
						{t('cft.restart')}
					</button>
					<button
						class="flex-1 rounded-xl border border-red-500/40 px-3 py-2 text-sm text-red-300 hover:bg-red-500/10 disabled:opacity-50"
						disabled={!status.token_installed}
						onclick={disconnect}
					>
						{t('cft.disconnect')}
					</button>
				</div>
			</section>

			<!-- Token -->
			<section class="flex flex-col rounded-2xl border border-white/5 bg-gray-800/60 p-5">
				<div class="flex items-center justify-between">
					<p class="text-xs font-semibold uppercase tracking-[0.14em] text-gray-400">{t('cft.status.token')}</p>
					<div class="rounded-xl bg-blue-500/10 p-1.5 text-blue-400">
						<svg class="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="1.5">
							<path stroke-linecap="round" stroke-linejoin="round" d="M15.75 5.25a3 3 0 013 3m3 0a6 6 0 01-7.029 5.912c-.563-.097-1.159.026-1.563.43L10.5 17.25H8.25v2.25H6v2.25H2.25v-2.818c0-.597.237-1.17.659-1.591l6.499-6.499c.404-.404.527-1 .43-1.563A6 6 0 1121.75 8.25z" />
						</svg>
					</div>
				</div>
				<p class="mt-3 text-lg font-semibold {status.token_installed ? 'text-green-300' : 'text-gray-400'}">
					{status.token_installed ? t('cft.status.tokenSet') : t('cft.status.tokenMissing')}
				</p>
				<p class="mt-1 text-xs text-gray-500">{t('cft.token.desc')}</p>
			</section>
		</div>

		<!-- Connect + guide -->
		<div class="grid grid-cols-1 gap-4 xl:grid-cols-2">
			<section class="rounded-2xl border border-white/5 bg-gray-800/60 p-5">
				<h2 class="text-base font-semibold text-gray-100">{t('cft.token.title')}</h2>
				<p class="mt-1 text-sm text-gray-400">{t('cft.token.desc')}</p>
				{#if status.token_installed}
					<p class="mt-2 text-xs text-yellow-200/80">{t('cft.token.replace')}</p>
				{/if}
				<div class="mt-4 flex gap-2">
					<input
						type={showToken ? 'text' : 'password'}
						bind:value={token}
						placeholder={t('cft.token.placeholder')}
						autocomplete="off"
						spellcheck="false"
						class="w-full rounded-xl border border-gray-700 bg-gray-950 px-3 py-2 text-sm text-gray-100 placeholder:text-gray-600 focus:border-blue-500 focus:outline-none"
					/>
					<button
						class="shrink-0 rounded-xl border border-gray-700 px-3 py-2 text-sm text-gray-300 hover:bg-white/5"
						onclick={() => (showToken = !showToken)}
					>
						{showToken ? t('cft.token.hide') : t('cft.token.show')}
					</button>
				</div>
				{#if connectError}
					<p class="mt-2 text-sm text-red-300">{connectError}</p>
				{/if}
				<button
					class="mt-4 rounded-xl bg-blue-600 px-4 py-2 text-sm font-medium text-white hover:bg-blue-500 disabled:opacity-50"
					disabled={connecting}
					onclick={connect}
				>
					{t('cft.token.connect')}
				</button>
				{#if connectTaskId}
					<div class="mt-4">
						<TaskProgress bind:taskId={connectTaskId} storageKey="jenderal_cft_connect" onComplete={() => { load(); loadLogs(); }} />
					</div>
				{/if}
			</section>

			<section class="rounded-2xl border border-white/5 bg-gray-800/60 p-5">
				<h2 class="text-base font-semibold text-gray-100">{t('cft.guide.title')}</h2>
				<ol class="mt-4 space-y-4">
					{#each [1, 2, 3, 4] as step (step)}
						<li class="flex gap-3">
							<span class="flex h-6 w-6 shrink-0 items-center justify-center rounded-full bg-blue-500/15 text-xs font-semibold text-blue-300">
								{step}
							</span>
							<p class="text-sm leading-relaxed text-gray-300">{t('cft.guide.step' + step)}</p>
						</li>
					{/each}
				</ol>
				<p class="mt-4 border-t border-white/5 pt-3 text-xs text-gray-500">{t('cft.guide.note')}</p>
			</section>
		</div>

		<!-- Logs -->
		<section class="rounded-2xl border border-white/5 bg-gray-800/60 p-5">
			<div class="flex items-center justify-between">
				<h2 class="text-base font-semibold text-gray-100">{t('cft.logs.title')}</h2>
				<button
					class="rounded-xl border border-gray-700 px-3 py-1.5 text-sm text-gray-300 hover:bg-white/5 disabled:opacity-50"
					disabled={logsLoading}
					onclick={loadLogs}
				>
					{t('cft.logs.refresh')}
				</button>
			</div>
			<pre class="mt-3 max-h-[28rem] overflow-auto rounded-xl bg-gray-950 p-4 text-xs leading-relaxed text-gray-300">{logs || t('cft.logs.empty')}</pre>
		</section>
	{/if}
</div>
