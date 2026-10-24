<script lang="ts">
	import { onMount, tick } from 'svelte';
	import { api } from '#lib/api.js';
	import {
		parseSimplePHPConfig,
		phpConfigIsDirty,
		phpConfigPresets,
		phpConfigWarnings,
		switchPHPConfigMode,
		updateSimplePHPConfig
	} from '#lib/php-config.js';
	import { countExtensions, groupPHPVersions, listExtensions, listsAgeHours, summarizePatches, websitesByPHPVersion } from '#lib/php-versions.js';
	import Modal from '#lib/components/Modal.svelte';
	import { hasPermission, permissions } from '#lib/stores/auth.js';
	import TaskProgress from '#lib/components/TaskProgress.svelte';
	import { toast } from '#lib/stores/toast.js';
	import { language, translate } from '#lib/stores/language.js';

	interface PhpVersion {
		version: string;
		installed: boolean;
		running: boolean;
	}
	interface ExtensionStatus {
		enabled: string[];
		disabled: string[];
		available: string[];
	}
	type ExtensionFilter = 'all' | 'enabled' | 'disabled' | 'available';
	type ManageTab = 'config' | 'extensions';

	let phpVersions = $state<PhpVersion[]>([]);
	let loading = $state(true);
	let refreshing = $state(false);
	let error = $state('');
	let actionInProgress = $state<string | null>(null);
	let currentTaskId = $state('');
	let configSaving = $state(false);
	let operationInProgress = $derived(actionInProgress !== null || !!currentTaskId || configSaving);

	let groups = $derived(groupPHPVersions(phpVersions));

	// Websites per PHP version. Needs websites.view; without it the usage hints
	// are simply hidden rather than shown as zero.
	let usage = $state<Record<string, string[]> | null>(null);
	let totalPHPSites = $derived(usage ? Object.values(usage).reduce((sum, domains) => sum + domains.length, 0) : 0);

	// Manage panel: one selected version with php.ini and extension tabs.
	let selectedVersion = $state<string | null>(null);
	let activeTab = $state<ManageTab>('config');
	let managePanel = $state<HTMLElement | null>(null);

	// php.ini editor
	let configContent = $state('');
	let savedContent = $state('');
	let configLoading = $state(false);
	let configError = $state('');
	let configMode = $state<'simple' | 'advanced'>('simple');
	let simpleConfig = $state<Record<string, string>>(parseSimplePHPConfig(''));
	let configDirty = $derived(!configLoading && phpConfigIsDirty({ mode: configMode, content: configContent, simpleConfig }, savedContent));
	let configWarnings = $derived(configMode === 'simple' ? phpConfigWarnings(simpleConfig) : []);
	const presets = [
		{ key: 'default', label: 'phpv.preset.default' },
		{ key: 'wordpress', label: 'phpv.preset.wordpress' },
		{ key: 'laravel', label: 'phpv.preset.laravel' },
		{ key: 'uploads', label: 'phpv.preset.uploads' }
	] as const;

	// Extensions
	let extensions = $state<ExtensionStatus | null>(null);
	let extLoading = $state(false);
	let extBusy = $state('');
	let extSearch = $state('');
	let extFilter = $state<ExtensionFilter>('all');
	let extCounts = $derived(countExtensions(extensions));
	let extList = $derived(listExtensions(extensions, { query: extSearch, state: extFilter }));
	const extFilters: { value: ExtensionFilter; label: string }[] = [
		{ value: 'all', label: 'phpv.ext.all' },
		{ value: 'enabled', label: 'phpv.ext.enabled' },
		{ value: 'disabled', label: 'phpv.ext.disabled' },
		{ value: 'available', label: 'phpv.ext.available' }
	];

	// Uninstall confirmation modal
	let uninstallVersion = $state<string | null>(null);

	// Patch updates (apt candidates for installed versions)
	interface PackageUpdate { name: string; installed: string; candidate: string }
	interface PatchStatus {
		version: string; current: string; latest: string; update_available: boolean; packages: PackageUpdate[];
		security_baseline: string; vulnerable: boolean; end_of_life: boolean; advisory_url?: string;
	}
	let patchStatuses = $state<PatchStatus[]>([]);
	let listsUpdatedAt = $state<string | null>(null);
	let patchesLoaded = $state(false);
	let upgradeTarget = $state<'all' | string | null>(null);
	let patchSummary = $derived(summarizePatches(patchStatuses));
	let listsAge = $derived(listsAgeHours(listsUpdatedAt));
	let canManage = $derived(hasPermission($permissions, 'php.manage'));
	let upgradeVersions = $derived(
		upgradeTarget === 'all' ? patchSummary.updatable : patchStatuses.filter((p) => p.version === upgradeTarget)
	);

	function patchFor(version: string): PatchStatus | undefined {
		return patchStatuses.find((p) => p.version === version);
	}

	async function loadPatches() {
		try {
			const overview = await api.get<{ versions: PatchStatus[]; lists_updated_at: string | null }>('/api/v1/php/updates');
			patchStatuses = overview.versions || [];
			listsUpdatedAt = overview.lists_updated_at;
		} catch {
			// The PHP list still works without patch data; keep the badges hidden.
			patchStatuses = [];
		} finally {
			patchesLoaded = true;
		}
	}

	async function checkUpdates() {
		currentTaskId = '';
		actionInProgress = 'check-updates';
		try {
			const result = await api.post<{ task_id: string }>('/api/v1/php/updates/refresh');
			currentTaskId = result.task_id;
			toast.success(translate($language, 'phpv.patch.checkStarted'));
		} catch (err) {
			toast.error(err instanceof Error ? err.message : translate($language, 'phpv.patch.checkFailed'));
			actionInProgress = null;
		}
	}

	async function runUpgrade() {
		const target = upgradeTarget;
		if (!target) return;
		upgradeTarget = null;
		currentTaskId = '';
		actionInProgress = `upgrade-${target}`;
		try {
			const path = target === 'all' ? '/api/v1/php/updates/upgrade' : `/api/v1/php/${target}/upgrade`;
			const result = await api.post<{ task_id: string }>(path);
			currentTaskId = result.task_id;
			toast.success(translate($language, 'phpv.patch.upgradeStarted'));
		} catch (err) {
			toast.error(err instanceof Error ? err.message : translate($language, 'phpv.patch.upgradeFailed'));
			actionInProgress = null;
		}
	}

	async function loadPhp() {
		error = '';
		try {
			// The API returns every version manageable on this OS (installed
			// or not), so the list follows the server's per-release matrix.
			phpVersions = (await api.get<PhpVersion[]>('/api/v1/php')) || [];
		} catch (err) {
			error = err instanceof Error ? err.message : translate($language, 'phpv.loadFailed');
		} finally {
			loading = false;
		}
	}

	async function loadUsage() {
		try {
			usage = websitesByPHPVersion((await api.get<Parameters<typeof websitesByPHPVersion>[0]>('/api/v1/websites')) || []);
		} catch {
			usage = null;
		}
	}

	async function refresh() {
		if (refreshing) return;
		refreshing = true;
		await Promise.all([loadPhp(), loadUsage(), loadPatches()]);
		refreshing = false;
	}

	function sitesFor(version: string): string[] {
		return usage?.[version] ?? [];
	}

	async function installPhp(version: string) {
		currentTaskId = '';
		actionInProgress = `install-${version}`;
		try {
			const result = await api.post<{ task_id: string }>(`/api/v1/php/${version}/install`);
			currentTaskId = result.task_id;
			toast.success(translate($language, 'phpv.toast.installStarted').replace('{version}', version));
		} catch (err) {
			toast.error(err instanceof Error ? err.message : translate($language, 'phpv.toast.installFailed').replace('{version}', version));
			actionInProgress = null;
		}
	}

	async function reinstallPhp(version: string) {
		currentTaskId = '';
		actionInProgress = `reinstall-${version}`;
		try {
			const result = await api.post<{ task_id: string }>(`/api/v1/php/${version}/reinstall`);
			currentTaskId = result.task_id;
			toast.success(translate($language, 'phpv.toast.reinstallStarted').replace('{version}', version));
		} catch (err) {
			toast.error(err instanceof Error ? err.message : translate($language, 'phpv.toast.reinstallFailed').replace('{version}', version));
			actionInProgress = null;
		}
	}

	function onTaskComplete() {
		actionInProgress = null;
		loadPhp();
		void loadPatches();
		if (selectedVersion && extensions) void loadExtensions(selectedVersion);
	}

	async function uninstallPhp(version: string) {
		uninstallVersion = null;
		actionInProgress = `uninstall-${version}`;
		try {
			await api.post(`/api/v1/php/${version}/uninstall`);
			toast.success(translate($language, 'phpv.toast.uninstalled').replace('{version}', version));
			if (selectedVersion === version) closeManage(true);
			await loadPhp();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : translate($language, 'phpv.toast.uninstallFailed').replace('{version}', version));
		} finally {
			actionInProgress = null;
		}
	}

	async function restartPhp(version: string) {
		actionInProgress = `restart-${version}`;
		try {
			await api.post(`/api/v1/php/${version}/restart`);
			toast.success(translate($language, 'phpv.toast.restarted').replace('{version}', version));
			await loadPhp();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : translate($language, 'phpv.toast.restartFailed').replace('{version}', version));
		} finally {
			actionInProgress = null;
		}
	}

	// ─── Manage panel ───────────────────────────────────────────────

	function confirmDiscard(): boolean {
		return !configDirty || window.confirm(translate($language, 'phpv.config.discardConfirm'));
	}

	async function openManage(version: string, tab: ManageTab) {
		if (configSaving) return;
		if (selectedVersion !== version) {
			if (!confirmDiscard()) return;
			selectedVersion = version;
			extensions = null;
			extSearch = '';
			extFilter = 'all';
			void loadConfig(version);
		}
		activeTab = tab;
		if (tab === 'extensions' && !extensions && !extLoading) void loadExtensions(version);
		await tick();
		managePanel?.scrollIntoView({ behavior: 'smooth', block: 'start' });
	}

	function closeManage(force = false) {
		if (configSaving || (!force && !confirmDiscard())) return;
		selectedVersion = null;
		extensions = null;
		configContent = '';
		savedContent = '';
		configError = '';
	}

	function selectTab(tab: ManageTab) {
		activeTab = tab;
		if (tab === 'extensions' && selectedVersion && !extensions && !extLoading) void loadExtensions(selectedVersion);
	}

	async function loadConfig(version: string) {
		configMode = 'simple';
		configLoading = true;
		configError = '';
		try {
			const data = await api.get<{ content: string }>(`/api/v1/php/${version}/config`);
			configContent = data.content || '';
			savedContent = configContent;
			simpleConfig = parseSimplePHPConfig(configContent);
		} catch (err) {
			configError = err instanceof Error ? err.message : translate($language, 'phpv.config.loadFailed');
		} finally {
			configLoading = false;
		}
	}

	async function persistConfig(content: string) {
		if (!selectedVersion || configSaving) return;
		configError = '';
		configSaving = true;
		try {
			await api.put(`/api/v1/php/${selectedVersion}/config`, { content });
			configContent = content;
			savedContent = content;
			simpleConfig = parseSimplePHPConfig(content);
			toast.success(translate($language, 'phpv.config.saved'));
		} catch (err) {
			configError = err instanceof Error ? err.message : translate($language, 'phpv.config.saveFailed');
		} finally {
			configSaving = false;
		}
	}

	function saveSimpleConfig(event: SubmitEvent) {
		event.preventDefault();
		void persistConfig(updateSimplePHPConfig(configContent, simpleConfig));
	}

	function resetConfig() {
		configContent = savedContent;
		simpleConfig = parseSimplePHPConfig(savedContent);
	}

	function applyPreset(key: keyof typeof phpConfigPresets) {
		simpleConfig = { ...simpleConfig, ...phpConfigPresets[key] };
	}

	function switchConfigMode(targetMode: 'simple' | 'advanced') {
		if (configSaving) return;
		const next = switchPHPConfigMode({ mode: configMode, content: configContent, simpleConfig }, targetMode);
		configMode = next.mode;
		configContent = next.content;
		simpleConfig = next.simpleConfig;
	}

	// ─── Extensions ─────────────────────────────────────────────────

	async function loadExtensions(version: string) {
		extLoading = true;
		try {
			extensions = await api.get<ExtensionStatus>(`/api/v1/php/${version}/extensions`);
		} catch (err) {
			toast.error(err instanceof Error ? err.message : translate($language, 'phpv.ext.errLoad'));
		} finally {
			extLoading = false;
		}
	}

	async function extensionAction(ext: string, action: 'enable' | 'disable' | 'install') {
		if (!selectedVersion || extBusy || (action === 'install' && currentTaskId)) return;
		const version = selectedVersion;
		extBusy = `${action}-${ext}`;
		try {
			const result = await api.post<{ task_id?: string }>(`/api/v1/php/${version}/extensions/${ext}/${action}`);
			toast.success(translate($language, `phpv.ext.${action}Toast`).replace('{ext}', ext));
			if (action === 'install') currentTaskId = result?.task_id || '';
			else await loadExtensions(version);
		} catch (err) {
			toast.error(err instanceof Error ? err.message : translate($language, 'phpv.ext.errAction'));
		} finally {
			extBusy = '';
		}
	}

	function onWindowKeydown(e: KeyboardEvent) {
		if (e.key === 'Escape' && uninstallVersion) uninstallVersion = null;
	}

	function onBeforeUnload(e: BeforeUnloadEvent) {
		if (configDirty) e.preventDefault();
	}

	onMount(() => {
		void Promise.all([loadPhp(), loadUsage(), loadPatches()]);
	});

	const inputClass =
		'w-full rounded-lg border border-gray-600 bg-gray-900 px-3 py-2 text-sm text-gray-200 placeholder-gray-500 focus:border-blue-500 focus:outline-none focus:ring-2 focus:ring-blue-500/40';
</script>

<svelte:window onkeydown={onWindowKeydown} onbeforeunload={onBeforeUnload} />

{#snippet usageLine(version: string)}
	{#if usage}
		{@const sites = sitesFor(version)}
		<p class="truncate text-xs {sites.length ? 'text-gray-300' : 'text-gray-500'}" title={sites.join(', ')}>
			{#if sites.length === 0}
				{translate($language, 'phpv.usage.none')}
			{:else}
				{translate($language, sites.length === 1 ? 'phpv.usage.one' : 'phpv.usage.many').replace('{count}', String(sites.length))}
				<span class="text-gray-500">· {sites.slice(0, 2).join(', ')}{sites.length > 2 ? ` +${sites.length - 2}` : ''}</span>
			{/if}
		</p>
	{/if}
{/snippet}

{#snippet patchLine(version: string)}
	{@const patch = patchFor(version)}
	{#if patch?.current}
		<p class="mt-0.5 flex flex-wrap items-center gap-1.5 text-xs">
			<span class="font-mono text-gray-300">{patch.current}</span>
			{#if patch.vulnerable}
				<span class="rounded-full bg-red-900/60 px-2 py-0.5 text-[10px] font-semibold text-red-200" title={translate($language, 'phpv.patch.vulnTitle')}>{translate($language, 'phpv.patch.vulnerable')}</span>
			{/if}
			{#if patch.update_available}
				<span class="rounded-full bg-yellow-900/50 px-2 py-0.5 text-[10px] font-semibold text-yellow-200">→ {patch.latest !== patch.current ? patch.latest : translate($language, 'phpv.patch.newBuild')}</span>
			{:else if !patch.vulnerable && !patch.end_of_life}
				<span class="text-[10px] text-green-400">✓ {translate($language, 'phpv.patch.latest')}</span>
			{/if}
			{#if patch.end_of_life}
				<span class="rounded-full bg-gray-700 px-2 py-0.5 text-[10px] font-semibold text-gray-200" title={translate($language, 'phpv.patch.eolHint')}>{translate($language, 'phpv.patch.eol')}</span>
			{/if}
		</p>
	{/if}
{/snippet}

<div class="space-y-6">
	<div class="flex flex-wrap items-start justify-between gap-3">
		<div>
			<h2 class="text-2xl font-bold text-white">{translate($language, 'phpv.title')}</h2>
			<p class="mt-0.5 text-sm text-gray-400">{translate($language, 'phpv.subtitle')}</p>
		</div>
		<button
			type="button"
			onclick={refresh}
			disabled={refreshing || loading}
			title={translate($language, 'phpv.refresh')}
			aria-label={translate($language, 'phpv.refresh')}
			class="cursor-pointer rounded-lg border border-gray-600 bg-gray-800 p-2 text-gray-300 transition hover:bg-gray-700 hover:text-white disabled:opacity-50"
		>
			<svg class="h-4 w-4 {refreshing ? 'animate-spin' : ''}" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="2" aria-hidden="true">
				<path stroke-linecap="round" stroke-linejoin="round" d="M16.023 9.348h4.992v-.001M2.985 19.644v-4.992m0 0h4.992m-4.993 0 3.181 3.183a8.25 8.25 0 0 0 13.803-3.7M4.031 9.865a8.25 8.25 0 0 1 13.803-3.7l3.181 3.182m0-4.991v4.99" />
			</svg>
		</button>
	</div>

	<!-- Install/reinstall progress sits at the top so it is visible right after clicking -->
	<TaskProgress bind:taskId={currentTaskId} storageKey="jenderal_php_task" onComplete={onTaskComplete} onMissing={() => { actionInProgress = null; }} />

	{#if loading}
		<div class="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
			{#each Array(3) as _}
				<div class="animate-pulse rounded-xl border border-gray-700 bg-gray-800 p-5">
					<div class="mb-3 h-6 w-1/3 rounded bg-gray-700/60"></div>
					<div class="mb-2 h-3 w-1/2 rounded bg-gray-700/40"></div>
					<div class="mt-4 h-8 w-full rounded bg-gray-700/30"></div>
				</div>
			{/each}
		</div>
	{:else if error}
		<div class="flex flex-wrap items-center justify-between gap-2 rounded-lg border border-red-700 bg-red-900/30 p-4 text-sm text-red-300">
			<span>{error}</span>
			<button type="button" onclick={refresh} class="cursor-pointer underline">{translate($language, 'phpv.retry')}</button>
		</div>
	{:else}
		<!-- Summary -->
		<div class="grid grid-cols-2 gap-2 sm:grid-cols-4">
			<div class="rounded-xl border border-gray-700 bg-gray-800 px-3 py-2.5">
				<p class="text-[11px] font-medium uppercase tracking-wider text-gray-400">{translate($language, 'phpv.stat.installed')}</p>
				<p class="mt-0.5 text-xl font-bold tabular-nums text-white">{groups.installed.length}<span class="text-sm font-medium text-gray-500"> / {phpVersions.length}</span></p>
			</div>
			<div class="rounded-xl border border-gray-700 bg-gray-800 px-3 py-2.5">
				<p class="text-[11px] font-medium uppercase tracking-wider text-gray-400">{translate($language, 'phpv.stat.running')}</p>
				<p class="mt-0.5 text-xl font-bold tabular-nums {groups.running ? 'text-green-300' : 'text-gray-600'}">{groups.running}</p>
			</div>
			<div class="rounded-xl border border-gray-700 bg-gray-800 px-3 py-2.5">
				<p class="text-[11px] font-medium uppercase tracking-wider text-gray-400">{translate($language, 'phpv.stat.stopped')}</p>
				<p class="mt-0.5 text-xl font-bold tabular-nums {groups.stopped ? 'text-red-300' : 'text-gray-600'}">{groups.stopped}</p>
			</div>
			<div class="rounded-xl border border-gray-700 bg-gray-800 px-3 py-2.5">
				<p class="text-[11px] font-medium uppercase tracking-wider text-gray-400">{translate($language, 'phpv.stat.sites')}</p>
				<p class="mt-0.5 text-xl font-bold tabular-nums {totalPHPSites ? 'text-blue-300' : 'text-gray-600'}">{usage ? totalPHPSites : '—'}</p>
			</div>
		</div>

		{#if patchSummary.vulnerable.length > 0}
			<div class="rounded-xl border border-red-700/70 bg-red-950/40 p-4">
				<div class="flex flex-wrap items-start justify-between gap-3">
					<div class="flex min-w-0 gap-3">
						<svg class="mt-0.5 h-5 w-5 shrink-0 text-red-400" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="2" aria-hidden="true">
							<path stroke-linecap="round" stroke-linejoin="round" d="M12 9v3.75m-9.303 3.376c-.866 1.5.217 3.374 1.948 3.374h14.71c1.73 0 2.813-1.874 1.948-3.374L13.949 3.378c-.866-1.5-3.032-1.5-3.898 0L2.697 16.126ZM12 15.75h.007v.008H12v-.008Z" />
						</svg>
						<div class="min-w-0">
							<p class="font-semibold text-red-200">{translate($language, 'phpv.patch.vulnTitle')}</p>
							<p class="mt-1 text-sm text-red-200/80">{translate($language, 'phpv.patch.vulnBody')}</p>
							<ul class="mt-2 space-y-0.5 font-mono text-xs text-red-100">
								{#each patchSummary.vulnerable as p (p.version)}
									<li>PHP {p.current} → {translate($language, 'phpv.patch.needs')} ≥ {p.security_baseline}</li>
								{/each}
							</ul>
							{#if patchSummary.stuck.length > 0}
								<p class="mt-2 text-xs text-yellow-200">{translate($language, 'phpv.patch.stuckHint')}</p>
							{/if}
							{#if patchSummary.vulnerable[0]?.advisory_url}
								<a href={patchSummary.vulnerable[0].advisory_url} target="_blank" rel="noopener noreferrer" class="mt-2 inline-block text-xs font-medium text-red-300 underline hover:text-red-100">{translate($language, 'phpv.patch.advisory')} ↗</a>
							{/if}
						</div>
					</div>
					{#if canManage}
						<div class="flex shrink-0 gap-2">
							{#if patchSummary.stuck.length > 0}
								<button type="button" onclick={checkUpdates} disabled={operationInProgress} class="cursor-pointer rounded-lg border border-red-500/50 px-3 py-2 text-sm font-medium text-red-100 transition hover:bg-red-500/15 disabled:opacity-50">
									{translate($language, 'phpv.patch.check')}
								</button>
							{/if}
							{#if patchSummary.updatable.length > 0}
								<button type="button" onclick={() => (upgradeTarget = 'all')} disabled={operationInProgress} class="cursor-pointer rounded-lg bg-red-600 px-4 py-2 text-sm font-semibold text-white transition hover:bg-red-700 disabled:opacity-50">
									{translate($language, 'phpv.patch.updateNow')}
								</button>
							{/if}
						</div>
					{/if}
				</div>
			</div>
		{/if}

		{#if patchesLoaded && patchStatuses.length > 0}
			<div class="flex flex-wrap items-center justify-between gap-3 rounded-xl border border-gray-700 bg-gray-800 px-4 py-3">
				<div class="flex min-w-0 items-center gap-3">
					<span class="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg {patchSummary.updatable.length ? 'bg-yellow-500/15 text-yellow-300' : 'bg-green-500/15 text-green-300'}" aria-hidden="true">
						<svg class="h-5 w-5" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="1.8">
							<path stroke-linecap="round" stroke-linejoin="round" d={patchSummary.updatable.length ? 'M3 16.5v2.25A2.25 2.25 0 0 0 5.25 21h13.5A2.25 2.25 0 0 0 21 18.75V16.5M16.5 12 12 16.5m0 0L7.5 12m4.5 4.5V3' : 'M9 12.75 11.25 15 15 9.75M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0Z'} />
						</svg>
					</span>
					<div class="min-w-0">
						<p class="text-sm font-medium text-white">
							{patchSummary.updatable.length
								? translate($language, 'phpv.patch.available').replace('{count}', String(patchSummary.updatable.length))
								: translate($language, 'phpv.patch.upToDate')}
						</p>
						<p class="text-xs text-gray-500">
							{listsAge === null
								? translate($language, 'phpv.patch.neverChecked')
								: listsAge < 1
									? translate($language, 'phpv.patch.checkedRecently')
									: translate($language, 'phpv.patch.checkedHours').replace('{hours}', String(listsAge))}
							{#if listsAge !== null && listsAge >= 24}<span class="text-yellow-300"> · {translate($language, 'phpv.patch.stale')}</span>{/if}
						</p>
					</div>
				</div>
				{#if canManage}
					<div class="flex items-center gap-2">
						<button type="button" onclick={checkUpdates} disabled={operationInProgress} class="cursor-pointer rounded-lg border border-gray-600 bg-gray-700 px-3 py-1.5 text-sm font-medium text-gray-200 transition hover:bg-gray-600 disabled:opacity-50">
							{actionInProgress === 'check-updates' ? translate($language, 'phpv.patch.checking') : translate($language, 'phpv.patch.check')}
						</button>
						{#if patchSummary.updatable.length > 0}
							<button type="button" onclick={() => (upgradeTarget = 'all')} disabled={operationInProgress} class="cursor-pointer rounded-lg bg-blue-600 px-3 py-1.5 text-sm font-semibold text-white transition hover:bg-blue-700 disabled:opacity-50">
								{translate($language, 'phpv.patch.updateAll')}
							</button>
						{/if}
					</div>
				{/if}
			</div>
		{/if}

		{#if groups.stopped > 0}
			<div class="rounded-lg border border-yellow-700/50 bg-yellow-900/20 px-4 py-3 text-sm text-yellow-200">
				{translate($language, 'phpv.stoppedBanner')}
			</div>
		{/if}

		<!-- Installed versions -->
		<section class="space-y-3">
			<h3 class="text-sm font-semibold uppercase tracking-wider text-gray-400">{translate($language, 'phpv.section.installed')}</h3>
			{#if groups.installed.length === 0}
				<div class="flex flex-col items-center gap-2 rounded-xl border border-dashed border-gray-600 bg-gray-800/50 px-6 py-10 text-center">
					<p class="font-medium text-gray-200">{translate($language, 'phpv.emptyTitle')}</p>
					<p class="text-sm text-gray-400">{translate($language, 'phpv.emptyDesc')}</p>
					{#if groups.available.length > 0}
						<button
							onclick={() => installPhp(groups.available[0].version)}
							disabled={operationInProgress}
							class="mt-2 cursor-pointer rounded-lg bg-blue-600 px-4 py-2 text-sm font-semibold text-white transition hover:bg-blue-700 disabled:opacity-50"
						>
							{translate($language, 'phpv.installVersion').replace('{version}', groups.available[0].version)}
						</button>
					{/if}
				</div>
			{:else}
				<div class="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
					{#each groups.installed as php (php.version)}
						{@const selected = selectedVersion === php.version}
						<div class="flex flex-col rounded-xl border bg-gray-800 p-4 transition {selected ? 'border-blue-500 ring-1 ring-blue-500/40' : php.running ? 'border-gray-700 hover:border-gray-500' : 'border-red-800/70'}">
							<div class="flex items-start gap-3">
								<span class="flex h-11 w-11 shrink-0 items-center justify-center rounded-lg bg-indigo-500/15 font-mono text-sm font-bold text-indigo-300" aria-hidden="true">
									{php.version}
								</span>
								<div class="min-w-0 flex-1">
									<div class="flex flex-wrap items-center gap-2">
										<h4 class="text-lg font-bold text-white">PHP {php.version}</h4>
										{#if php.version === groups.newest}
											<span class="rounded-full bg-blue-500/15 px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wide text-blue-300">{translate($language, 'phpv.newest')}</span>
										{/if}
									</div>
									{@render usageLine(php.version)}
									{@render patchLine(php.version)}
								</div>
								<span class="inline-flex shrink-0 items-center gap-1.5 rounded-full px-2 py-0.5 text-xs font-medium {php.running ? 'bg-green-900/50 text-green-300' : 'bg-red-900/50 text-red-300'}">
									<span class="h-1.5 w-1.5 rounded-full {php.running ? 'bg-green-400' : 'bg-red-400'}"></span>
									{php.running ? translate($language, 'phpv.running') : translate($language, 'phpv.stopped')}
								</span>
							</div>

							{#if !php.running}
								<p class="mt-3 text-xs text-red-300/90">{translate($language, 'phpv.stoppedHint')}</p>
							{/if}

							<div class="mt-auto flex flex-wrap items-center gap-1.5 border-t border-gray-700/60 pt-3 {php.running ? 'mt-4' : 'mt-3'}">
								<button
									onclick={() => openManage(php.version, 'config')}
									disabled={configSaving}
									class="cursor-pointer rounded-md px-3 py-1.5 text-sm font-medium transition disabled:opacity-50 {selected && activeTab === 'config' ? 'bg-blue-600 text-white' : 'bg-gray-700 text-gray-200 hover:bg-gray-600'}"
								>
									{translate($language, 'phpv.config')}
								</button>
								<button
									onclick={() => openManage(php.version, 'extensions')}
									disabled={configSaving}
									class="cursor-pointer rounded-md px-3 py-1.5 text-sm font-medium transition disabled:opacity-50 {selected && activeTab === 'extensions' ? 'bg-blue-600 text-white' : 'bg-gray-700 text-gray-200 hover:bg-gray-600'}"
								>
									{translate($language, 'phpv.ext.open')}
								</button>
								{#if canManage && patchFor(php.version)?.update_available}
									<button
										onclick={() => (upgradeTarget = php.version)}
										disabled={operationInProgress}
										class="cursor-pointer rounded-md px-3 py-1.5 text-sm font-semibold text-white transition disabled:opacity-50 {patchFor(php.version)?.vulnerable ? 'bg-red-600 hover:bg-red-700' : 'bg-green-600 hover:bg-green-700'}"
									>
										{actionInProgress === `upgrade-${php.version}` ? translate($language, 'phpv.patch.updating') : translate($language, 'phpv.patch.update')}
									</button>
								{/if}
								<span class="ml-auto flex items-center gap-1">
									<button
										onclick={() => restartPhp(php.version)}
										disabled={operationInProgress}
										title={php.running ? translate($language, 'phpv.restart') : translate($language, 'phpv.start')}
										aria-label={php.running ? translate($language, 'phpv.restart') : translate($language, 'phpv.start')}
										class="cursor-pointer rounded-md p-1.5 text-gray-400 transition hover:bg-yellow-500/10 hover:text-yellow-300 disabled:opacity-40"
									>
										<svg class="h-4 w-4 {actionInProgress === `restart-${php.version}` ? 'animate-spin' : ''}" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="1.8" aria-hidden="true">
											<path stroke-linecap="round" stroke-linejoin="round" d="M16.023 9.348h4.992v-.001M2.985 19.644v-4.992m0 0h4.992m-4.993 0 3.181 3.183a8.25 8.25 0 0 0 13.803-3.7M4.031 9.865a8.25 8.25 0 0 1 13.803-3.7l3.181 3.182m0-4.991v4.99" />
										</svg>
									</button>
									{#if !php.running}
										<button
											onclick={() => reinstallPhp(php.version)}
											disabled={operationInProgress}
											title={translate($language, 'phpv.reinstallHint')}
											class="cursor-pointer rounded-md border border-purple-600/50 bg-purple-600/20 px-2.5 py-1 text-xs font-medium text-purple-200 transition hover:bg-purple-600/30 disabled:opacity-50"
										>
											{actionInProgress === `reinstall-${php.version}` ? translate($language, 'phpv.reinstalling') : translate($language, 'phpv.reinstall')}
										</button>
									{/if}
									<button
										onclick={() => (uninstallVersion = php.version)}
										disabled={operationInProgress}
										title={translate($language, 'phpv.uninstall')}
										aria-label={translate($language, 'phpv.uninstall')}
										class="cursor-pointer rounded-md p-1.5 text-gray-400 transition hover:bg-red-500/10 hover:text-red-400 disabled:opacity-40"
									>
										<svg class="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="1.8" aria-hidden="true">
											<path stroke-linecap="round" stroke-linejoin="round" d="m14.74 9-.346 9m-4.788 0L9.26 9m9.968-3.21c.342.052.682.107 1.022.166m-1.022-.165L18.16 19.673a2.25 2.25 0 0 1-2.244 2.077H8.084a2.25 2.25 0 0 1-2.244-2.077L4.772 5.79m14.456 0a48.108 48.108 0 0 0-3.478-.397m-12 .562c.34-.059.68-.114 1.022-.165m0 0a48.11 48.11 0 0 1 3.478-.397m7.5 0v-.916c0-1.18-.91-2.164-2.09-2.201a51.964 51.964 0 0 0-3.32 0c-1.18.037-2.09 1.022-2.09 2.201v.916m7.5 0a48.667 48.667 0 0 0-7.5 0" />
										</svg>
									</button>
								</span>
							</div>
						</div>
					{/each}
				</div>
			{/if}
		</section>

		<!-- Manage panel -->
		{#if selectedVersion}
			<section bind:this={managePanel} class="scroll-mt-4 rounded-xl border border-gray-700 bg-gray-800">
				<div class="flex flex-wrap items-center justify-between gap-3 border-b border-gray-700 px-5 pt-4">
					<div class="flex items-end gap-5">
						<h3 class="pb-3 text-lg font-semibold text-white">PHP {selectedVersion}</h3>
						<div class="flex gap-1" role="tablist">
							{#each [{ tab: 'config', label: 'phpv.tab.config' }, { tab: 'extensions', label: 'phpv.tab.extensions' }] as const as item}
								<button
									type="button"
									role="tab"
									aria-selected={activeTab === item.tab}
									onclick={() => selectTab(item.tab)}
									class="-mb-px cursor-pointer border-b-2 px-3 pb-3 text-sm font-medium transition {activeTab === item.tab ? 'border-blue-500 text-white' : 'border-transparent text-gray-400 hover:text-gray-200'}"
								>
									{translate($language, item.label)}
									{#if item.tab === 'config' && configDirty}
										<span class="ml-1 inline-block h-1.5 w-1.5 rounded-full bg-yellow-400 align-middle" title={translate($language, 'phpv.config.unsaved')}></span>
									{/if}
								</button>
							{/each}
						</div>
					</div>
					<button
						type="button"
						onclick={() => closeManage()}
						disabled={configSaving}
						aria-label={translate($language, 'phpv.config.close')}
						class="mb-3 cursor-pointer rounded-lg p-1.5 text-gray-400 transition hover:bg-gray-700 hover:text-white disabled:opacity-50"
					>
						<svg class="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="2" aria-hidden="true">
							<path stroke-linecap="round" stroke-linejoin="round" d="M6 18L18 6M6 6l12 12" />
						</svg>
					</button>
				</div>

				<div class="p-5">
					{#if activeTab === 'config'}
						{#if configError}
							<div class="mb-3 rounded-lg border border-red-700 bg-red-900/30 px-3 py-2 text-sm text-red-300">{configError}</div>
						{/if}
						{#if configLoading}
							<div class="text-sm text-gray-400">{translate($language, 'phpv.config.loading')}</div>
						{:else}
							<div class="mb-4 flex flex-wrap items-center justify-between gap-3">
								<p class="text-sm text-gray-400">{translate($language, configMode === 'simple' ? 'phpv.config.hint' : 'phpv.config.advancedHint')}</p>
								<div class="inline-flex rounded-lg border border-gray-600 p-0.5" role="group" aria-label={translate($language, 'phpv.config.modeAria')}>
									{#each ['simple', 'advanced'] as const as mode}
										<button
											type="button"
											onclick={() => switchConfigMode(mode)}
											disabled={configSaving}
											aria-pressed={configMode === mode}
											class="cursor-pointer rounded-md px-3 py-1 text-sm transition disabled:opacity-50 {configMode === mode ? 'bg-blue-600 text-white' : 'text-gray-300 hover:bg-gray-700'}"
										>
											{translate($language, `phpv.config.${mode}`)}
										</button>
									{/each}
								</div>
							</div>

							<form
								onsubmit={(e) => {
									if (configMode === 'simple') saveSimpleConfig(e);
									else { e.preventDefault(); void persistConfig(configContent); }
								}}
							>
								<fieldset disabled={configSaving} class="space-y-4">
									{#if configMode === 'simple'}
										<div class="flex flex-wrap items-center gap-2 rounded-lg border border-gray-700 bg-gray-900/40 px-3 py-2">
											<span class="text-xs font-medium text-gray-400">{translate($language, 'phpv.preset.label')}</span>
											{#each presets as preset}
												<button
													type="button"
													onclick={() => applyPreset(preset.key)}
													class="cursor-pointer rounded-full border border-gray-600 bg-gray-800 px-3 py-1 text-xs font-medium text-gray-200 transition hover:border-blue-500 hover:text-white"
												>
													{translate($language, preset.label)}
												</button>
											{/each}
											<span class="text-[11px] text-gray-500">{translate($language, 'phpv.preset.hint')}</span>
										</div>

										<div class="grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-3">
											<label class="space-y-1 text-sm text-gray-300">
												<span>{translate($language, 'phpv.config.memoryLimit')}</span>
												<input bind:value={simpleConfig.memory_limit} required pattern="-1|[0-9]+[KMGkmg]?" placeholder="128M" class={inputClass} />
												<span class="block text-xs text-gray-500">{translate($language, 'phpv.config.memoryLimitHint')}</span>
											</label>
											<label class="space-y-1 text-sm text-gray-300">
												<span>{translate($language, 'phpv.config.uploadMaxFilesize')}</span>
												<input bind:value={simpleConfig.upload_max_filesize} required pattern="[0-9]+[KMGkmg]?" placeholder="2M" class={inputClass} />
												<span class="block text-xs text-gray-500">{translate($language, 'phpv.config.uploadMaxFilesizeHint')}</span>
											</label>
											<label class="space-y-1 text-sm text-gray-300">
												<span>{translate($language, 'phpv.config.postMaxSize')}</span>
												<input bind:value={simpleConfig.post_max_size} required pattern="[0-9]+[KMGkmg]?" placeholder="8M" class={inputClass} />
												<span class="block text-xs text-gray-500">{translate($language, 'phpv.config.postMaxSizeHint')}</span>
											</label>
											<label class="space-y-1 text-sm text-gray-300">
												<span>{translate($language, 'phpv.config.maxExecutionTime')}</span>
												<input type="number" min="0" step="1" bind:value={simpleConfig.max_execution_time} required class={inputClass} />
												<span class="block text-xs text-gray-500">{translate($language, 'phpv.config.maxExecutionTimeHint')}</span>
											</label>
											<label class="space-y-1 text-sm text-gray-300">
												<span>{translate($language, 'phpv.config.maxInputTime')}</span>
												<input type="number" min="-1" step="1" bind:value={simpleConfig.max_input_time} required class={inputClass} />
												<span class="block text-xs text-gray-500">{translate($language, 'phpv.config.maxInputTimeHint')}</span>
											</label>
											<label class="space-y-1 text-sm text-gray-300">
												<span>{translate($language, 'phpv.config.maxInputVars')}</span>
												<input type="number" min="0" step="1" bind:value={simpleConfig.max_input_vars} required class={inputClass} />
												<span class="block text-xs text-gray-500">{translate($language, 'phpv.config.maxInputVarsHint')}</span>
											</label>
											<label class="space-y-1 text-sm text-gray-300">
												<span>{translate($language, 'phpv.config.displayErrors')}</span>
												<select bind:value={simpleConfig.display_errors} class={inputClass}>
													<option value="Off">{translate($language, 'phpv.config.displayErrorsOff')}</option>
													<option value="On">{translate($language, 'phpv.config.displayErrorsOn')}</option>
												</select>
												<span class="block text-xs text-gray-500">{translate($language, 'phpv.config.displayErrorsHint')}</span>
											</label>
										</div>

										{#if configWarnings.length}
											<ul class="space-y-1 rounded-lg border border-yellow-700/50 bg-yellow-900/20 px-4 py-3 text-sm text-yellow-200">
												{#each configWarnings as warning}
													<li>{translate($language, warning)}</li>
												{/each}
											</ul>
										{/if}
									{:else}
										<textarea
											bind:value={configContent}
											rows={22}
											spellcheck="false"
											class="w-full resize-y rounded-lg border border-gray-700 bg-gray-950 p-3 font-mono text-sm text-gray-300 focus:outline-none focus:ring-2 focus:ring-blue-500 disabled:opacity-50"
										></textarea>
									{/if}

									<div class="flex flex-wrap items-center justify-end gap-2 border-t border-gray-700 pt-4">
										{#if configDirty}
											<span class="mr-auto inline-flex items-center gap-1.5 text-xs text-yellow-300">
												<span class="h-1.5 w-1.5 rounded-full bg-yellow-400"></span>
												{translate($language, 'phpv.config.unsaved')}
											</span>
										{/if}
										<button
											type="button"
											onclick={resetConfig}
											disabled={!configDirty}
											class="cursor-pointer rounded-lg border border-gray-600 bg-gray-700 px-4 py-2 text-sm font-medium text-gray-200 transition hover:bg-gray-600 disabled:cursor-default disabled:opacity-40"
										>
											{translate($language, 'phpv.config.reset')}
										</button>
										<button
											type="submit"
											disabled={configSaving || !configDirty}
											class="cursor-pointer rounded-lg bg-blue-600 px-4 py-2 text-sm font-semibold text-white transition hover:bg-blue-700 disabled:cursor-default disabled:opacity-40"
										>
											{configSaving ? translate($language, 'phpv.config.saving') : translate($language, 'phpv.config.save')}
										</button>
									</div>
								</fieldset>
							</form>
						{/if}
					{:else}
						<div class="mb-4 flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
							<div class="flex flex-wrap gap-1.5">
								{#each extFilters as filter}
									<button
										type="button"
										onclick={() => (extFilter = filter.value)}
										aria-pressed={extFilter === filter.value}
										class="cursor-pointer rounded-full px-3 py-1 text-xs font-medium transition {extFilter === filter.value ? 'bg-blue-600 text-white' : 'border border-gray-600 bg-gray-800 text-gray-300 hover:bg-gray-700'}"
									>
										{translate($language, filter.label)}
										<span class="ml-1 opacity-70">{extCounts[filter.value]}</span>
									</button>
								{/each}
							</div>
							<input type="search" bind:value={extSearch} placeholder={translate($language, 'phpv.ext.search')} aria-label={translate($language, 'phpv.ext.search')} class="{inputClass} sm:w-60" />
						</div>
						<p class="mb-4 text-xs text-gray-500">{translate($language, 'phpv.ext.reloadNote')}</p>

						{#if extLoading && !extensions}
							<div class="text-sm text-gray-400">{translate($language, 'phpv.ext.loading')}</div>
						{:else if extList.length === 0}
							<div class="rounded-lg border border-dashed border-gray-600 px-4 py-8 text-center text-sm text-gray-400">{translate($language, 'phpv.ext.none')}</div>
						{:else}
							<ul class="grid gap-2 sm:grid-cols-2 xl:grid-cols-3">
								{#each extList as ext (ext.name)}
									{@const busy = extBusy.endsWith(`-${ext.name}`)}
									<li class="flex items-center justify-between gap-2 rounded-lg border border-gray-700 bg-gray-900/50 px-3 py-2">
										<div class="flex min-w-0 items-center gap-2">
											<span class="h-2 w-2 shrink-0 rounded-full {ext.state === 'enabled' ? 'bg-green-400' : ext.state === 'disabled' ? 'bg-gray-500' : 'bg-blue-400/60'}" title={translate($language, `phpv.ext.${ext.state}`)}></span>
											<code class="truncate text-sm {ext.state === 'enabled' ? 'text-gray-100' : 'text-gray-400'}">{ext.name}</code>
										</div>
										{#if ext.state === 'available'}
											<button
												onclick={() => extensionAction(ext.name, 'install')}
												disabled={!!extBusy || !!currentTaskId}
												class="shrink-0 cursor-pointer rounded-md border border-blue-600/50 bg-blue-600/15 px-2.5 py-1 text-xs font-medium text-blue-200 transition hover:bg-blue-600/30 disabled:opacity-50"
											>
												{busy ? translate($language, 'phpv.ext.installing') : translate($language, 'phpv.ext.install')}
											</button>
										{:else}
											<!-- Toggle switch: on = enabled -->
											<button
												type="button"
												role="switch"
												aria-checked={ext.state === 'enabled'}
												aria-label={translate($language, ext.state === 'enabled' ? 'phpv.ext.disable' : 'phpv.ext.enable') + ' ' + ext.name}
												title={translate($language, ext.state === 'enabled' ? 'phpv.ext.disable' : 'phpv.ext.enable')}
												onclick={() => extensionAction(ext.name, ext.state === 'enabled' ? 'disable' : 'enable')}
												disabled={!!extBusy}
												class="relative inline-flex h-5 w-9 shrink-0 cursor-pointer items-center rounded-full transition disabled:opacity-50 {ext.state === 'enabled' ? 'bg-green-600' : 'bg-gray-600'} {busy ? 'animate-pulse' : ''}"
											>
												<span class="inline-block h-4 w-4 rounded-full bg-white shadow transition {ext.state === 'enabled' ? 'translate-x-4.5' : 'translate-x-0.5'}"></span>
											</button>
										{/if}
									</li>
								{/each}
							</ul>
						{/if}
					{/if}
				</div>
			</section>
		{/if}

		<!-- Versions available to install -->
		{#if groups.available.length > 0}
			<section class="space-y-3">
				<div>
					<h3 class="text-sm font-semibold uppercase tracking-wider text-gray-400">{translate($language, 'phpv.section.available')}</h3>
					<p class="mt-0.5 text-xs text-gray-500">{translate($language, 'phpv.section.availableHint')}</p>
				</div>
				<div class="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-4 xl:grid-cols-6">
					{#each groups.available as php (php.version)}
						<div class="flex flex-col items-center gap-2 rounded-xl border border-dashed border-gray-600 bg-gray-800/40 p-3 text-center">
							<p class="font-mono text-lg font-bold text-gray-200">PHP {php.version}</p>
							{#if php.version === groups.newest}
								<span class="-mt-1 rounded-full bg-blue-500/15 px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wide text-blue-300">{translate($language, 'phpv.newest')}</span>
							{/if}
							<button
								onclick={() => installPhp(php.version)}
								disabled={operationInProgress}
								class="w-full cursor-pointer rounded-md bg-blue-600 px-3 py-1.5 text-sm font-medium text-white transition hover:bg-blue-700 disabled:opacity-50"
							>
								{actionInProgress === `install-${php.version}` ? translate($language, 'phpv.installing') : translate($language, 'phpv.install')}
							</button>
						</div>
					{/each}
				</div>
			</section>
		{/if}
	{/if}
</div>

{#if upgradeTarget && upgradeVersions.length > 0}
	<Modal
		title={upgradeTarget === 'all' ? translate($language, 'phpv.patch.modalTitleAll') : translate($language, 'phpv.patch.modalTitle').replace('{version}', upgradeTarget)}
		onclose={() => (upgradeTarget = null)}
		size="lg"
		closeLabel={translate($language, 'phpv.no')}
	>
		<div class="space-y-4">
			{#each upgradeVersions as p (p.version)}
				<div class="rounded-lg border {p.vulnerable ? 'border-red-700/60' : 'border-gray-700'} p-3">
					<div class="flex flex-wrap items-center justify-between gap-2">
						<p class="font-semibold text-white">PHP {p.version}</p>
						<p class="font-mono text-sm">
							<span class="text-gray-400">{p.current}</span>
							<span class="text-gray-500">→</span>
							<span class="text-green-300">{p.latest}</span>
						</p>
					</div>
					{#if p.vulnerable}
						<p class="mt-1 text-xs text-red-300">{translate($language, 'phpv.patch.fixesSecurity')}</p>
					{/if}
					{#if sitesFor(p.version).length > 0}
						<p class="mt-1 text-xs text-gray-400">{translate($language, 'phpv.patch.sitesRestart').replace('{count}', String(sitesFor(p.version).length))}</p>
					{/if}
					<details class="mt-2">
						<summary class="cursor-pointer text-xs text-gray-400 hover:text-gray-200">{translate($language, 'phpv.patch.packages').replace('{count}', String(p.packages.length))}</summary>
						<ul class="mt-1 max-h-40 space-y-0.5 overflow-y-auto font-mono text-[11px] text-gray-400">
							{#each p.packages as pkg (pkg.name)}
								<li><span class="text-gray-200">{pkg.name}</span> {pkg.installed} → {pkg.candidate}</li>
							{/each}
						</ul>
					</details>
				</div>
			{/each}
			<p class="text-xs text-gray-500">{translate($language, 'phpv.patch.modalNote')}</p>
		</div>
		{#snippet footer()}
			<button type="button" onclick={() => (upgradeTarget = null)} class="cursor-pointer rounded-lg border border-gray-600 bg-gray-700 px-4 py-2 text-sm font-medium text-gray-200 transition hover:bg-gray-600">
				{translate($language, 'phpv.no')}
			</button>
			<button type="button" onclick={runUpgrade} disabled={operationInProgress} class="cursor-pointer rounded-lg bg-blue-600 px-4 py-2 text-sm font-semibold text-white transition hover:bg-blue-700 disabled:opacity-50">
				{translate($language, 'phpv.patch.confirmUpdate')}
			</button>
		{/snippet}
	</Modal>
{/if}

{#if uninstallVersion}
	{@const sites = sitesFor(uninstallVersion)}
	<!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
	<div
		class="fixed inset-0 z-50 flex items-center justify-center overflow-y-auto bg-black/60 p-4 backdrop-blur-sm"
		role="dialog"
		aria-modal="true"
		tabindex="-1"
		aria-label={translate($language, 'phpv.uninstallTitle').replace('{version}', uninstallVersion)}
		onclick={(e) => { if (e.target === e.currentTarget) uninstallVersion = null; }}
	>
		<div class="w-full max-w-md rounded-2xl border border-gray-700 bg-gray-800 p-5 shadow-2xl">
			<h3 class="mb-3 text-base font-semibold text-white">{translate($language, 'phpv.uninstallTitle').replace('{version}', uninstallVersion)}</h3>
			<p class="text-sm text-gray-300">{translate($language, 'phpv.removeConfirm').replace('{version}', uninstallVersion)}</p>
			{#if sites.length > 0}
				<div class="mt-3 rounded-lg border border-red-700/60 bg-red-900/25 p-3">
					<p class="text-sm font-medium text-red-300">
						{translate($language, 'phpv.uninstallInUse').replace('{count}', String(sites.length))}
					</p>
					<ul class="mt-2 max-h-32 space-y-0.5 overflow-y-auto font-mono text-xs text-red-200/90">
						{#each sites as domain}<li>{domain}</li>{/each}
					</ul>
				</div>
			{/if}
			<div class="mt-4 flex items-center justify-end gap-2 border-t border-gray-700 pt-4">
				<button
					type="button"
					onclick={() => (uninstallVersion = null)}
					class="cursor-pointer rounded-lg border border-gray-600 bg-gray-700 px-4 py-2 text-sm font-medium text-gray-200 transition hover:bg-gray-600"
				>
					{translate($language, 'phpv.no')}
				</button>
				<button
					type="button"
					onclick={() => uninstallVersion && uninstallPhp(uninstallVersion)}
					class="cursor-pointer rounded-lg bg-red-600 px-4 py-2 text-sm font-semibold text-white transition hover:bg-red-700"
				>
					{translate($language, sites.length > 0 ? 'phpv.uninstallAnyway' : 'phpv.uninstall')}
				</button>
			</div>
		</div>
	</div>
{/if}
