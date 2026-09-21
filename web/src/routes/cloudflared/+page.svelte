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
			status = await api.get<TunnelStatus>('/cloudflared');
		} catch (e) {
			error = e instanceof Error ? e.message : t('cft.loadFailed');
		} finally {
			loading = false;
		}
	}

	async function loadLogs() {
		logsLoading = true;
		try {
			const res = await api.get<{ lines: number; output: string }>('/cloudflared/logs?lines=200');
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
			const res = await api.post<{ task_id: string }>('/cloudflared/install', {});
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
			const res = await api.post<{ task_id: string }>('/cloudflared/connect', { token: token.trim() });
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
			await api.post('/cloudflared/disconnect', {});
			await load();
		} catch (e) {
			actionError = e instanceof Error ? e.message : t('cft.loadFailed');
		}
	}

	async function restart() {
		actionError = '';
		try {
			await api.post('/cloudflared/restart', {});
			await load();
		} catch (e) {
			actionError = e instanceof Error ? e.message : t('cft.loadFailed');
		}
	}

	function stateBadge(state: string): string {
		if (state === 'active') return 'bg-green-500/15 text-green-300 border-green-500/30';
		if (state === 'failed') return 'bg-red-500/15 text-red-300 border-red-500/30';
		return 'bg-gray-500/15 text-gray-300 border-gray-500/30';
	}

	onMount(() => {
		load();
		loadLogs();
	});
</script>

<div class="mx-auto max-w-4xl px-4 py-8 space-y-6">
	<header>
		<h1 class="text-2xl font-semibold text-gray-100">{t('cft.title')}</h1>
		<p class="mt-1 text-sm text-gray-400">{t('cft.subtitle')}</p>
	</header>

	{#if error}
		<div class="rounded-lg border border-red-500/30 bg-red-500/10 px-4 py-3 text-sm text-red-300">{error}</div>
	{/if}
	{#if actionError}
		<div class="rounded-lg border border-red-500/30 bg-red-500/10 px-4 py-3 text-sm text-red-300">{actionError}</div>
	{/if}

	{#if loading}
		<div class="text-sm text-gray-400">{t('cft.loading')}</div>
	{:else if status}
		{#if status.apt_service_running}
			<div class="rounded-lg border border-yellow-500/30 bg-yellow-500/10 px-4 py-3 text-sm text-yellow-200">
				{t('cft.conflict')}
			</div>
		{/if}

		<!-- Status -->
		<section class="rounded-xl border border-gray-800 bg-gray-900 p-5">
			<h2 class="text-base font-medium text-gray-100">{t('cft.status.title')}</h2>
			<dl class="mt-4 grid grid-cols-1 gap-4 sm:grid-cols-2">
				<div>
					<dt class="text-xs uppercase tracking-wide text-gray-500">{t('cft.status.binary')}</dt>
					<dd class="mt-1 text-sm text-gray-200">
						{#if status.installed}
							{t('cft.status.version')}: {status.version || '?'}
							{#if status.version !== status.pinned_version}
								<span class="ml-2 rounded border border-yellow-500/30 bg-yellow-500/10 px-1.5 py-0.5 text-xs text-yellow-200">
									{t('cft.status.updateAvailable')} ({status.pinned_version})
								</span>
							{/if}
						{:else}
							{t('cft.status.notInstalled')} · {t('cft.status.pinned')}: {status.pinned_version}
						{/if}
					</dd>
				</div>
				<div>
					<dt class="text-xs uppercase tracking-wide text-gray-500">{t('cft.status.service')}</dt>
					<dd class="mt-1">
						<span class="rounded border px-2 py-0.5 text-sm {stateBadge(status.service_state)}">
							{t('cft.state.' + status.service_state)}
						</span>
					</dd>
				</div>
				<div>
					<dt class="text-xs uppercase tracking-wide text-gray-500">{t('cft.status.token')}</dt>
					<dd class="mt-1 text-sm text-gray-200">
						{status.token_installed ? t('cft.status.tokenSet') : t('cft.status.tokenMissing')}
					</dd>
				</div>
			</dl>
			<div class="mt-5 flex flex-wrap gap-2">
				{#if !status.installed || status.version !== status.pinned_version}
					<button
						class="rounded-lg bg-blue-600 px-4 py-2 text-sm font-medium text-white hover:bg-blue-500 disabled:opacity-50"
						disabled={!!installTaskId}
						onclick={install}
					>
						{t('cft.install.button').replace('{version}', status.pinned_version)}
					</button>
				{/if}
				{#if status.token_installed}
					<button class="rounded-lg border border-gray-700 px-4 py-2 text-sm text-gray-200 hover:bg-white/5" onclick={restart}>
						{t('cft.restart')}
					</button>
					<button class="rounded-lg border border-red-500/40 px-4 py-2 text-sm text-red-300 hover:bg-red-500/10" onclick={disconnect}>
						{t('cft.disconnect')}
					</button>
				{/if}
			</div>
			{#if installTaskId}
				<div class="mt-4">
					<TaskProgress bind:taskId={installTaskId} storageKey="jenderal_cft_install" onComplete={() => { load(); loadLogs(); }} />
				</div>
			{/if}
		</section>

		<!-- Connect -->
		<section class="rounded-xl border border-gray-800 bg-gray-900 p-5">
			<h2 class="text-base font-medium text-gray-100">{t('cft.token.title')}</h2>
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
					class="w-full rounded-lg border border-gray-700 bg-gray-950 px-3 py-2 text-sm text-gray-100 placeholder:text-gray-600 focus:border-blue-500 focus:outline-none"
				/>
				<button
					class="shrink-0 rounded-lg border border-gray-700 px-3 py-2 text-sm text-gray-300 hover:bg-white/5"
					onclick={() => (showToken = !showToken)}
				>
					{showToken ? t('cft.token.hide') : t('cft.token.show')}
				</button>
			</div>
			{#if connectError}
				<p class="mt-2 text-sm text-red-300">{connectError}</p>
			{/if}
			<button
				class="mt-4 rounded-lg bg-blue-600 px-4 py-2 text-sm font-medium text-white hover:bg-blue-500 disabled:opacity-50"
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

		<!-- Guide -->
		<section class="rounded-xl border border-gray-800 bg-gray-900 p-5">
			<h2 class="text-base font-medium text-gray-100">{t('cft.guide.title')}</h2>
			<ol class="mt-3 list-decimal space-y-2 pl-5 text-sm text-gray-300">
				<li>{t('cft.guide.step1')}</li>
				<li>{t('cft.guide.step2')}</li>
				<li>{t('cft.guide.step3')}</li>
				<li>{t('cft.guide.step4')}</li>
			</ol>
			<p class="mt-3 text-xs text-gray-500">{t('cft.guide.note')}</p>
		</section>

		<!-- Logs -->
		<section class="rounded-xl border border-gray-800 bg-gray-900 p-5">
			<div class="flex items-center justify-between">
				<h2 class="text-base font-medium text-gray-100">{t('cft.logs.title')}</h2>
				<button
					class="rounded-lg border border-gray-700 px-3 py-1.5 text-sm text-gray-300 hover:bg-white/5 disabled:opacity-50"
					disabled={logsLoading}
					onclick={loadLogs}
				>
					{t('cft.logs.refresh')}
				</button>
			</div>
			<pre class="mt-3 max-h-96 overflow-auto rounded-lg bg-gray-950 p-3 text-xs leading-relaxed text-gray-300">{logs || t('cft.logs.empty')}</pre>
		</section>
	{/if}
</div>
