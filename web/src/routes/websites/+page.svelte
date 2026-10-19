<script lang="ts">
	import { onMount, onDestroy } from 'svelte';
	import { api } from '$lib/api';
	import TaskProgress from '$lib/components/TaskProgress.svelte';
	import { availablePHPVersions, normalizeWebsiteSelection, selectedCombination } from '$lib/website-form.js';
	import { countByGroup, filterWebsites, formatBytes, frameworkLabel, sortKeys, statusGroup, usesPHP } from '$lib/website-list.js';
import { toast } from '$lib/stores/toast';
import { language, translate } from '$lib/stores/language';
import type { ServerInfo } from '$lib/types';

	interface Website {
		id: string;
		domain: string;
		app_type: string;
		php_version: string;
		status: string;
		error_message?: string;
		framework: string;
		framework_version: string;
		frontend_stack: string;
		inertia_adapter: string;
		project_variant: string;
		setup_mode: string;
		provision_stage: string;
		provision_log: string;
		bandwidth_bytes: number;
		created_at: string;
		owner_email?: string;
	}
	interface RuntimeOption { version: string; installed: boolean; running: boolean }
	interface DependencyOption { name: string; version: string; installed: boolean; manage_url: string }
	interface ProfileOption {
		template: string; framework_version: string; frontend_stack: string; inertia_adapter: string;
		project_variant: string; setup_mode: string; enabled: boolean; reason: string; minimum_php: string;
		document_root: string; prerequisites: string[]; php_compatibility: { version: string; enabled: boolean; reason: string }[];
	}
	interface WebsiteSelection {
		node_version: string;
		template: string; php_version: string; framework_version: string; frontend_stack: string;
		inertia_adapter: string; project_variant: string; setup_mode: string;
		proxy_scheme: string; proxy_host: string; proxy_port: string;
	}
	interface WebsiteOptions {
		node_versions: string[];
		php_versions: RuntimeOption[]; dependencies: DependencyOption[]; profiles: ProfileOption[];
		inertia_adapters: string[]; defaults: WebsiteSelection;
	}

	let websites = $state<Website[]>([]);
	let loading = $state(true);
	let error = $state('');
	let options = $state<WebsiteOptions | null>(null);
	let optionsError = $state('');
	let repairTaskId = $state('');
	let repairing = $state(false);
	let repairBusy = $derived(repairing || !!repairTaskId);
	let refreshing = $state(false);

	// Toolbar: search, status filter, sort, and grid/table view. Sort and view
	// survive reloads so a long list keeps the operator's preferred layout.
	const viewStorageKey = 'jenderal.websites.view';
	const sortStorageKey = 'jenderal.websites.sort';
	let search = $state('');
	let searchInput = $state<HTMLInputElement | null>(null);
	let statusFilter = $state('all');
	let sortBy = $state(readPreference(sortStorageKey, sortKeys, 'newest'));
	let viewMode = $state(readPreference(viewStorageKey, ['grid', 'table'], 'grid'));
	const statusFilters = [
		{ value: 'all', label: 'wl.filterAll', tone: 'text-white', ring: 'ring-blue-500/60' },
		{ value: 'active', label: 'wl.filterActive', tone: 'text-green-300', ring: 'ring-green-500/60' },
		{ value: 'pending', label: 'wl.filterInProgress', tone: 'text-yellow-300', ring: 'ring-yellow-500/60' },
		{ value: 'failed', label: 'wl.filterFailed', tone: 'text-red-300', ring: 'ring-red-500/60' },
		{ value: 'suspended', label: 'wl.filterSuspended', tone: 'text-gray-300', ring: 'ring-gray-400/60' }
	] as const;

	let filteredWebsites = $derived(filterWebsites(websites, { query: search, status: statusFilter, sort: sortBy }) as Website[]);
	const counts = $derived(countByGroup(websites));
	const hasFilters = $derived(search.trim() !== '' || statusFilter !== 'all');

	function readPreference(key: string, allowed: readonly string[], fallback: string): string {
		try {
			const value = localStorage.getItem(key);
			return value && allowed.includes(value) ? value : fallback;
		} catch {
			return fallback;
		}
	}

	function savePreference(key: string, value: string) {
		try {
			localStorage.setItem(key, value);
		} catch {
			// Private mode or storage disabled: the preference just won't persist.
		}
	}

	function setView(mode: 'grid' | 'table') {
		viewMode = mode;
		savePreference(viewStorageKey, mode);
	}

	function setSort(value: string) {
		sortBy = value;
		savePreference(sortStorageKey, value);
	}

	function toggleStatusFilter(value: string) {
		statusFilter = statusFilter === value && value !== 'all' ? 'all' : value;
	}

	function clearFilters() {
		search = '';
		statusFilter = 'all';
	}

	function statusLabel(status: string): string {
		const key = `wl.status.${status}`;
		const label = translate($language, key);
		return label === key ? status : label;
	}

	function siteUrl(website: Website): string {
		return `http://${website.domain}`;
	}

	async function copyDomain(domain: string) {
		try {
			await navigator.clipboard.writeText(domain);
			toast.success(translate($language, 'wl.toastDomainCopied').replace('{domain}', domain));
		} catch (err) {
			toast.error(err instanceof Error ? err.message : translate($language, 'wl.dnsHelp.copyFailed'));
		}
	}

	async function refresh() {
		if (refreshing) return;
		refreshing = true;
		try {
			websites = (await api.get<Website[]>('/api/v1/websites')) || [];
			error = '';
			if (shouldPoll(websites)) startPolling();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : translate($language, 'wl.errorLoad'));
		} finally {
			refreshing = false;
		}
	}

	function handleKeydown(e: KeyboardEvent) {
		if (e.key === 'Escape') {
			closeConfirm();
			return;
		}
		// "/" jumps to search, like most list UIs, unless the user is typing.
		const target = e.target as HTMLElement | null;
		const typing = target && (target.isContentEditable || ['INPUT', 'TEXTAREA', 'SELECT'].includes(target.tagName));
		if (e.key === '/' && !typing && !confirmModal && !dnsHelpOpen && searchInput) {
			e.preventDefault();
			searchInput.focus();
		}
	}

	async function repairLaravel(id: string) {
		if (repairBusy) return;
		repairing = true;
		try {
			const result = await api.post<{task_id: string}>(`/api/v1/websites/${id}/repair-laravel`, {confirm: true});
			repairTaskId = result.task_id;
		} catch (err) { toast.error(err instanceof Error ? err.message : translate($language, 'wl.toastRepairFailed')); repairing = false; }
	}

	// Create form
	let showCreateForm = $state(false);
	let createDomain = $state('');
	let selection = $state<WebsiteSelection>({ template: 'php', php_version: '', framework_version: '', frontend_stack: '', inertia_adapter: '', project_variant: 'empty', setup_mode: 'config-only', node_version: '', proxy_scheme: 'http', proxy_host: '', proxy_port: '' });
	let creating = $state(false);
	let installedPHP = $derived(options ? availablePHPVersions(options) as RuntimeOption[] : []);
	let combination = $derived(options ? selectedCombination(options, selection) : null);
	let laravelVersions = $derived(options ? [...new Set(options.profiles.filter((item) => item.template === 'laravel').map((item) => item.framework_version))] : []);
	let laravelOctaneVersions = $derived(options ? [...new Set(options.profiles.filter((item) => item.template === 'laravel-octane').map((item) => item.framework_version))] : []);

	// Every state-changing row action is confirmed through one modal.
	type ConfirmAction = 'suspend' | 'delete' | 'enable' | 'repair';
	let confirmModal = $state<{ website: Website; action: ConfirmAction } | null>(null);
	let confirmBusy = $state(false);
	const confirmCopy: Record<ConfirmAction, { title: string; body: string; yes: string; button: string }> = {
		suspend: { title: 'wl.titleSuspend', body: 'wl.suspendConfirm', yes: 'wl.yesSuspend', button: 'bg-yellow-600 hover:bg-yellow-700' },
		delete: { title: 'wl.titleDelete', body: 'wl.deleteConfirm', yes: 'wl.yesDelete', button: 'bg-red-600 hover:bg-red-700' },
		enable: { title: 'wl.titleEnable', body: 'wl.enableConfirm', yes: 'wl.yesEnable', button: 'bg-green-600 hover:bg-green-700' },
		repair: { title: 'wl.titleRepair', body: 'wl.repairConfirm', yes: 'wl.confirmRepair', button: 'bg-blue-600 hover:bg-blue-700' }
	};

	// DNS tutorial modal on the add-website form; the server's public IP is
	// fetched once so beginners can copy it straight into their A record.
	let dnsHelpOpen = $state(false);
	let serverPublicIP = $state('');
	let serverIPLoading = $state(false);
	let serverIPLoaded = $state(false);

	async function openDnsHelp() {
		dnsHelpOpen = true;
		if (serverIPLoaded || serverIPLoading) return;
		serverIPLoading = true;
		try {
			const info = await api.get<ServerInfo>('/api/v1/server/info');
			serverPublicIP = info.ip || '';
		} catch {
			// The IP is context, not a requirement — the tutorial stands alone.
		} finally {
			serverIPLoading = false;
			serverIPLoaded = true;
		}
	}

	async function copyServerIP() {
		try {
			await navigator.clipboard.writeText(serverPublicIP);
			toast.success(translate($language, 'wl.dnsHelp.copied'));
		} catch (err) {
			toast.error(err instanceof Error ? err.message : translate($language, 'wl.dnsHelp.copyFailed'));
		}
	}

	// Polling
	let pollTimer: ReturnType<typeof setInterval> | null = null;

	function normalizeSelection() {
		if (options) selection = normalizeWebsiteSelection(selection, options) as WebsiteSelection;
	}

	async function loadOptions() {
		optionsError = '';
		try {
			options = await api.get<WebsiteOptions>('/api/v1/websites/options');
			selection = normalizeWebsiteSelection(options.defaults || {}, options) as WebsiteSelection;
		} catch (err) {
			optionsError = err instanceof Error ? err.message : translate($language, 'wl.errorLoadOptions');
		}
	}

	function isPending(status: string): boolean {
		return statusGroup(status) === 'pending';
	}

	function statusBadgeClass(status: string): string {
		switch (status) {
			case 'active':
				return 'bg-green-900/50 text-green-300';
			case 'pending':
			case 'installing':
			case 'configuring':
			case 'validating':
				return 'bg-yellow-900/50 text-yellow-300';
			case 'failed':
				return 'bg-red-900/50 text-red-300';
			default:
				return 'bg-gray-700/60 text-gray-300';
		}
	}

	function statusDotClass(status: string): string {
		switch (status) {
			case 'active':
				return 'bg-green-400';
			case 'pending':
			case 'installing':
			case 'configuring':
			case 'validating':
				return 'bg-yellow-400 animate-pulse';
			case 'failed':
				return 'bg-red-400';
			default:
				return 'bg-gray-400';
		}
	}

	function shouldPoll(sites: Website[]): boolean {
		return sites.some((w) => isPending(w.status));
	}

	function startPolling() {
		stopPolling();
		pollTimer = setInterval(async () => {
			try {
				websites = (await api.get<Website[]>('/api/v1/websites')) || [];
				if (!shouldPoll(websites)) {
					stopPolling();
				}
			} catch {
				// Silently ignore polling errors
			}
		}, 3000);
	}

	function stopPolling() {
		if (pollTimer) {
			clearInterval(pollTimer);
			pollTimer = null;
		}
	}

	async function loadWebsites() {
		loading = true;
		error = '';
		try {
			websites = (await api.get<Website[]>('/api/v1/websites')) || [];
			if (shouldPoll(websites)) {
				startPolling();
			}
		} catch (err) {
			error = err instanceof Error ? err.message : translate($language, 'wl.errorLoad');
		} finally {
			loading = false;
		}
	}

	async function createWebsite() {
		creating = true;
		try {
			const body: Record<string, unknown> = { domain: createDomain, ...selection };
			// proxy_port must travel as a number and only for reverse-proxy
			// sites: the backend decodes it as int, and an empty string (sent
			// for every other template) fails JSON decoding outright.
			if (selection.template === 'reverse-proxy') {
				body.proxy_port = Number(selection.proxy_port) || 0;
			} else {
				delete body.proxy_scheme;
				delete body.proxy_host;
				delete body.proxy_port;
			}
			await api.post('/api/v1/websites', body);
			toast.success(translate($language, 'wl.toastCreateStarted').replace('{domain}', createDomain));
			showCreateForm = false;
			createDomain = '';
			if (options) selection = normalizeWebsiteSelection(options.defaults || {}, options) as WebsiteSelection;
			await loadWebsites();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : translate($language, 'wl.errorCreate'));
		} finally {
			creating = false;
		}
	}

	async function runConfirmed() {
		if (!confirmModal || confirmBusy) return;
		const { website, action } = confirmModal;
		if (action === 'enable' || action === 'repair') {
			// These report their own success/failure; close straight away.
			confirmModal = null;
			if (action === 'enable') await enableWebsite(website.id);
			else await repairLaravel(website.id);
			return;
		}
		confirmBusy = true;
		try {
			if (action === 'suspend') {
				await api.post(`/api/v1/websites/${website.id}/suspend`);
				toast.success(translate($language, 'wl.toastSuspended'));
			} else {
				await api.del(`/api/v1/websites/${website.id}`);
				toast.success(translate($language, 'wl.toastDeleted'));
			}
			await loadWebsites();
		} catch (err) {
			toast.error(
				err instanceof Error
					? err.message
					: translate($language, action === 'suspend' ? 'wl.errorSuspend' : 'wl.errorDelete')
			);
		} finally {
			// Close on failure too so the toast is not hidden behind the backdrop.
			confirmBusy = false;
			confirmModal = null;
		}
	}

	function closeConfirm() {
		if (confirmBusy) return;
		confirmModal = null;
	}

	async function enableWebsite(id: string) {
		try {
			await api.post(`/api/v1/websites/${id}/enable`);
			toast.success(translate($language, 'wl.toastEnabled'));
			await loadWebsites();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : translate($language, 'wl.errorEnable'));
		}
	}

	async function retryWebsite(id: string) {
		try {
			await api.post(`/api/v1/websites/${id}/retry`);
			toast.success(translate($language, 'wl.toastRetry'));
			await loadWebsites();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : translate($language, 'wl.errorRetry'));
		}
	}

	onMount(() => { void Promise.all([loadWebsites(), loadOptions()]); });

	onDestroy(() => {
		stopPolling();
	});
</script>

<svelte:window onkeydown={handleKeydown} />

{#snippet statusBadge(status: string)}
	<span class="inline-flex shrink-0 items-center gap-1.5 rounded-full px-2 py-0.5 text-xs font-medium {statusBadgeClass(status)}">
		<span class="h-1.5 w-1.5 rounded-full {statusDotClass(status)}"></span>
		{statusLabel(status)}
	</span>
{/snippet}

{#snippet iconButton(label: string, onclick: () => void, path: string, tone = 'text-gray-400 hover:bg-gray-700 hover:text-white')}
	<button type="button" {onclick} title={label} aria-label={label} class="cursor-pointer rounded-md p-1.5 transition {tone}">
		<svg class="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="1.8" aria-hidden="true">
			<path stroke-linecap="round" stroke-linejoin="round" d={path} />
		</svg>
	</button>
{/snippet}

{#snippet rowActions(website: Website, compact: boolean)}
	<div class="flex flex-wrap items-center gap-1.5 {compact ? 'justify-end' : ''}">
		<a
			href="/websites/{website.id}"
			class="rounded-md bg-blue-600/90 font-medium text-white transition hover:bg-blue-600 {compact ? 'px-2.5 py-1 text-xs' : 'px-3 py-1.5 text-sm'}"
		>
			{translate($language, 'wl.manage')}
		</a>
		{#if website.status === 'failed'}
			<button onclick={() => retryWebsite(website.id)} class="cursor-pointer rounded-md border border-yellow-600/50 bg-yellow-600/20 font-medium text-yellow-300 transition hover:bg-yellow-600/30 {compact ? 'px-2.5 py-1 text-xs' : 'px-3 py-1.5 text-sm'}">
				{translate($language, 'wl.retry')}
			</button>
		{/if}
		{#if website.status === 'suspended' || website.status === 'disabled'}
			<button onclick={() => (confirmModal = { website, action: 'enable' })} class="cursor-pointer rounded-md border border-green-600/50 bg-green-600/20 font-medium text-green-300 transition hover:bg-green-600/30 {compact ? 'px-2.5 py-1 text-xs' : 'px-3 py-1.5 text-sm'}">
				{translate($language, 'wl.enable')}
			</button>
		{/if}
		{#if website.status === 'active' && website.framework === 'laravel' && website.setup_mode === 'auto-install'}
			<button
				disabled={repairBusy}
				onclick={() => (confirmModal = { website, action: 'repair' })}
				class="cursor-pointer rounded-md border border-gray-600 bg-gray-700 font-medium text-gray-200 transition hover:bg-gray-600 disabled:opacity-50 {compact ? 'px-2.5 py-1 text-xs' : 'px-3 py-1.5 text-sm'}"
			>
				{translate($language, 'wl.repair')}
			</button>
		{/if}
		<span class="flex items-center {compact ? '' : 'ml-auto'}">
			{#if website.status === 'active'}
				{@render iconButton(translate($language, 'wl.suspend'), () => (confirmModal = { website, action: 'suspend' }), 'M15.75 5.25v13.5m-7.5-13.5v13.5', 'text-gray-400 hover:bg-yellow-500/10 hover:text-yellow-300')}
			{/if}
			{@render iconButton(translate($language, 'wl.delete'), () => (confirmModal = { website, action: 'delete' }), 'm14.74 9-.346 9m-4.788 0L9.26 9m9.968-3.21c.342.052.682.107 1.022.166m-1.022-.165L18.16 19.673a2.25 2.25 0 0 1-2.244 2.077H8.084a2.25 2.25 0 0 1-2.244-2.077L4.772 5.79m14.456 0a48.108 48.108 0 0 0-3.478-.397m-12 .562c.34-.059.68-.114 1.022-.165m0 0a48.11 48.11 0 0 1 3.478-.397m7.5 0v-.916c0-1.18-.91-2.164-2.09-2.201a51.964 51.964 0 0 0-3.32 0c-1.18.037-2.09 1.022-2.09 2.201v.916m7.5 0a48.667 48.667 0 0 0-7.5 0', 'text-gray-400 hover:bg-red-500/10 hover:text-red-400')}
		</span>
	</div>
{/snippet}

{#snippet domainLinks(website: Website)}
	<span class="flex shrink-0 items-center">
		{#if website.status === 'active'}
			<a href={siteUrl(website)} target="_blank" rel="noopener noreferrer" title={translate($language, 'wl.visitSite')} aria-label={translate($language, 'wl.visitSite')} class="rounded-md p-1 text-gray-500 transition hover:bg-gray-700 hover:text-white">
				<svg class="h-3.5 w-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="2" aria-hidden="true">
					<path stroke-linecap="round" stroke-linejoin="round" d="M13.5 6H5.25A2.25 2.25 0 0 0 3 8.25v10.5A2.25 2.25 0 0 0 5.25 21h10.5A2.25 2.25 0 0 0 18 18.75V10.5m-10.5 6L21 3m0 0h-5.25M21 3v5.25" />
				</svg>
			</a>
		{/if}
		<button type="button" onclick={() => copyDomain(website.domain)} title={translate($language, 'wl.copyDomain')} aria-label={translate($language, 'wl.copyDomain')} class="cursor-pointer rounded-md p-1 text-gray-500 transition hover:bg-gray-700 hover:text-white">
			<svg class="h-3.5 w-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="2" aria-hidden="true">
				<path stroke-linecap="round" stroke-linejoin="round" d="M15.75 17.25v3.375c0 .621-.504 1.125-1.125 1.125h-9.75a1.125 1.125 0 0 1-1.125-1.125V7.875c0-.621.504-1.125 1.125-1.125H6.75a9.06 9.06 0 0 1 1.5.124m7.5 10.376h3.375c.621 0 1.125-.504 1.125-1.125V11.25c0-4.46-3.243-8.161-7.5-8.876a9.06 9.06 0 0 0-1.5-.124H9.375c-.621 0-1.125.504-1.125 1.125v3.5m7.5 10.375H9.375a1.125 1.125 0 0 1-1.125-1.125v-9.25m12 6.625v-1.875a3.375 3.375 0 0 0-3.375-3.375h-1.5a1.125 1.125 0 0 1-1.125-1.125v-1.5a3.375 3.375 0 0 0-3.375-3.375H9.75" />
			</svg>
		</button>
	</span>
{/snippet}

<div class="space-y-5">
	<div class="flex flex-wrap items-start justify-between gap-3">
		<div>
			<h2 class="text-2xl font-bold text-white">{translate($language, 'wl.title')}</h2>
			<p class="mt-0.5 text-sm text-gray-400">{translate($language, 'wl.subtitleEmpty')}</p>
		</div>
		<div class="flex items-center gap-2">
			{#if websites.length > 0}
				<button
					type="button"
					onclick={refresh}
					disabled={refreshing}
					title={translate($language, 'wl.refresh')}
					aria-label={translate($language, 'wl.refresh')}
					class="cursor-pointer rounded-lg border border-gray-600 bg-gray-800 p-2 text-gray-300 transition hover:bg-gray-700 hover:text-white disabled:opacity-50"
				>
					<svg class="h-4 w-4 {refreshing ? 'animate-spin' : ''}" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="2" aria-hidden="true">
						<path stroke-linecap="round" stroke-linejoin="round" d="M16.023 9.348h4.992v-.001M2.985 19.644v-4.992m0 0h4.992m-4.993 0 3.181 3.183a8.25 8.25 0 0 0 13.803-3.7M4.031 9.865a8.25 8.25 0 0 1 13.803-3.7l3.181 3.182m0-4.991v4.99" />
					</svg>
				</button>
			{/if}
			<button
				onclick={() => (showCreateForm = !showCreateForm)}
				class="inline-flex cursor-pointer items-center gap-1.5 rounded-lg bg-blue-600 px-4 py-2 text-sm font-semibold text-white transition hover:bg-blue-700"
			>
				<svg class="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="2" aria-hidden="true">
					<path stroke-linecap="round" stroke-linejoin="round" d={showCreateForm ? 'M6 18 18 6M6 6l12 12' : 'M12 4.5v15m7.5-7.5h-15'} />
				</svg>
				{showCreateForm ? translate($language, 'wl.cancel') : translate($language, 'wl.createWebsite')}
			</button>
		</div>
	</div>

	<TaskProgress bind:taskId={repairTaskId} storageKey="website-laravel-repair-task" onComplete={(task) => { repairing = false; repairTaskId = ''; if (task.status === 'completed') toast.success(translate($language, 'wl.toastRepairCompleted')); }} onMissing={() => { repairing = false; }} />

	<!-- Create Form -->
	{#if showCreateForm}
		<div class="rounded-xl border border-gray-700 bg-gray-800 p-5">
			<div class="mb-4 flex items-center justify-between">
				<div>
					<h3 class="text-lg font-semibold text-white">{translate($language, 'wl.newWebsite')}</h3>
					<p class="text-xs text-gray-400">{translate($language, 'wl.newWebsiteHint')}</p>
				</div>
			</div>
			{#if optionsError}
				<div class="mb-4 rounded-lg border border-red-700 bg-red-900/30 px-3 py-2.5 text-sm text-red-300">
					{optionsError} <button class="cursor-pointer underline" onclick={loadOptions}>{translate($language, 'wl.retry')}</button>
				</div>
			{/if}
			<div class="space-y-5">
				<div>
					<div class="mb-1 flex items-center justify-between gap-2">
						<label for="create-domain" class="text-[11px] font-medium uppercase tracking-wider text-gray-400">{translate($language, 'wl.labelDomain')}</label>
						<button
							type="button"
							onclick={openDnsHelp}
							class="cursor-pointer text-[11px] text-blue-400 transition hover:text-blue-300"
						>
							{translate($language, 'wl.dnsHelp.button')}
						</button>
					</div>
					<input
						id="create-domain"
						type="text"
						bind:value={createDomain}
						placeholder="example.com"
						spellcheck="false"
						autocomplete="off"
						class="w-full rounded-lg border border-gray-600 bg-gray-900 px-3 py-2 text-sm text-gray-200 placeholder-gray-500 focus:border-blue-500 focus:outline-none focus:ring-2 focus:ring-blue-500/40"
					/>
				</div>

				<fieldset class="space-y-3">
					<legend class="text-[11px] font-medium uppercase tracking-wider text-gray-400">{translate($language, 'wl.labelApplication')}</legend>
					<div class="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
						<div>
							<label for="create-app-type" class="mb-1 block text-xs text-gray-400">{translate($language, 'wl.labelTemplate')}</label>
							<select
								id="create-app-type"
								value={selection.template}
								onchange={(event) => { selection.template = event.currentTarget.value; normalizeSelection(); }}
								class="w-full rounded-lg border border-gray-600 bg-gray-900 px-3 py-2 text-sm text-gray-200 focus:border-blue-500 focus:outline-none focus:ring-2 focus:ring-blue-500/40"
							>
								<option value="static">{translate($language, 'wl.optStatic')}</option>
								<option value="reverse-proxy">{translate($language, 'wl.optReverseProxy')}</option>
								<option value="php">{translate($language, 'wl.optPhp')}</option>
								<option value="wordpress">{translate($language, 'wl.optWordpress')}</option>
								<option value="codeigniter3">{translate($language, 'wl.optCi3')}</option>
								<option value="codeigniter4">{translate($language, 'wl.optCi4')}</option>
								<option value="laravel">{translate($language, 'wl.optLaravel')}</option>
								<option value="laravel-octane">{translate($language, 'wl.optLaravelOctane')}</option>
								<option value="go-build">{translate($language, 'wl.optGoBuild')}</option>
								<option value="go-binary">{translate($language, 'wl.optGoBinary')}</option>
								<option value="python">{translate($language, 'wl.optPython')}</option>
								<option value="deno">{translate($language, 'wl.optDeno')}</option>
								<option value="bun">{translate($language, 'wl.optBun')}</option>
							</select>
						</div>
						{#if selection.template === 'wordpress'}
							<p class="sm:col-span-2 lg:col-span-3 text-xs text-gray-500">{translate($language, 'wl.wordpressHint')}</p>
						{/if}
						{#if selection.template === 'go-build' || selection.template === 'go-binary'}
							<p class="sm:col-span-2 lg:col-span-3 text-xs text-gray-500">{translate($language, 'wl.goTemplateHint')}</p>
						{/if}
						{#if selection.template !== 'static' && selection.template !== 'reverse-proxy'}
							<div>
								<label for="create-php-version" class="mb-1 block text-xs text-gray-400">{translate($language, 'wl.labelPhpVersion')}</label>
								<select
									id="create-php-version"
									bind:value={selection.php_version}
									class="w-full rounded-lg border border-gray-600 bg-gray-900 px-3 py-2 text-sm text-gray-200 focus:border-blue-500 focus:outline-none focus:ring-2 focus:ring-blue-500/40"
								>
									{#each installedPHP as runtime}
										<option value={runtime.version}>{runtime.version}{runtime.running ? '' : ' (' + translate($language, 'wl.fpmStopped') + ')'}</option>
									{/each}
								</select>
								{#if installedPHP.length === 0}
									<p class="mt-1 text-xs text-yellow-300">{translate($language, 'wl.noPhpInstalled')} <a class="underline" href="/php">{translate($language, 'wl.installPhp')}</a></p>
								{:else if installedPHP.find((runtime) => runtime.version === selection.php_version)?.running === false}
									<p class="mt-1 text-xs text-yellow-300">{translate($language, 'wl.fpmRestartNote').replace('{version}', selection.php_version)}</p>
								{/if}
							</div>
						{/if}
						{#if selection.template === 'laravel' || selection.template === 'laravel-octane'}
							<div>
								<label for="framework-version" class="mb-1 block text-xs text-gray-400">{translate($language, 'wl.labelLaravelVersion')}</label>
								<select id="framework-version" value={selection.framework_version} onchange={(event) => { selection.framework_version = event.currentTarget.value; normalizeSelection(); }} class="w-full rounded-lg border border-gray-600 bg-gray-900 px-3 py-2 text-sm text-gray-200 focus:border-blue-500 focus:outline-none focus:ring-2 focus:ring-blue-500/40">
									{#if selection.template === 'laravel-octane'}
										{#each laravelOctaneVersions as version}<option value={version}>Laravel {version}</option>{/each}
									{:else}
										{#each laravelVersions as version}<option value={version}>Laravel {version}</option>{/each}
									{/if}
								</select>
							</div>
							{#if selection.template === 'laravel'}
							<div>
								<label for="frontend-stack" class="mb-1 block text-xs text-gray-400">{translate($language, 'wl.labelFrontend')}</label>
								<select id="frontend-stack" value={selection.frontend_stack} onchange={(event) => { selection.frontend_stack = event.currentTarget.value; normalizeSelection(); }} class="w-full rounded-lg border border-gray-600 bg-gray-900 px-3 py-2 text-sm text-gray-200 focus:border-blue-500 focus:outline-none focus:ring-2 focus:ring-blue-500/40">
									<option value="blade">Blade</option><option value="inertia">Inertia</option><option value="livewire">Livewire</option>
								</select>
							</div>
							{#if selection.frontend_stack === 'inertia'}
								<div>
									<label for="inertia-adapter" class="mb-1 block text-xs text-gray-400">{translate($language, 'wl.labelInertiaAdapter')}</label>
									<select id="inertia-adapter" value={selection.inertia_adapter} onchange={(event) => { selection.inertia_adapter = event.currentTarget.value; normalizeSelection(); }} class="w-full rounded-lg border border-gray-600 bg-gray-900 px-3 py-2 text-sm text-gray-200 focus:border-blue-500 focus:outline-none focus:ring-2 focus:ring-blue-500/40">
										{#each options?.inertia_adapters || [] as adapter}<option value={adapter}>{adapter[0].toUpperCase() + adapter.slice(1)}</option>{/each}
									</select>
								</div>
							{/if}
							{#if selection.frontend_stack !== 'blade'}
								<div>
									<label for="project-variant" class="mb-1 block text-xs text-gray-400">{translate($language, 'wl.labelProject')}</label>
									<select id="project-variant" value={selection.project_variant} onchange={(event) => { selection.project_variant = event.currentTarget.value; normalizeSelection(); }} class="w-full rounded-lg border border-gray-600 bg-gray-900 px-3 py-2 text-sm text-gray-200 focus:border-blue-500 focus:outline-none focus:ring-2 focus:ring-blue-500/40">
										<option value="empty">{translate($language, 'wl.optEmptyProject')}</option><option value="starter-kit">{translate($language, 'wl.optStarterKit')}</option>
									</select>
								</div>
							{/if}
							{/if}
						{/if}

						{#if selection.template === 'reverse-proxy'}
							<div class="sm:col-span-2 grid grid-cols-1 gap-3 sm:grid-cols-6">
								<p class="sm:col-span-6 text-xs text-gray-500">{translate($language, 'wl.proxyHint')}</p>
								<div class="sm:col-span-2">
									<label for="create-proxy-scheme" class="mb-1 block text-xs text-gray-400">{translate($language, 'wl.labelProxyScheme')}</label>
									<select
										id="create-proxy-scheme"
										value={selection.proxy_scheme}
										onchange={(event) => { selection.proxy_scheme = event.currentTarget.value; normalizeSelection(); }}
										class="w-full rounded-lg border border-gray-600 bg-gray-900 px-3 py-2 text-sm text-gray-200 focus:border-blue-500 focus:outline-none focus:ring-2 focus:ring-blue-500/40"
									>
										<option value="http">HTTP</option>
										<option value="https">HTTPS</option>
									</select>
								</div>
								<div class="sm:col-span-2">
									<label for="create-proxy-host" class="mb-1 block text-xs text-gray-400">{translate($language, 'wl.labelProxyHost')}</label>
									<input
										id="create-proxy-host"
										type="text"
										value={selection.proxy_host}
										onchange={(event) => { selection.proxy_host = event.currentTarget.value; normalizeSelection(); }}
										placeholder="127.0.0.1"
										class="w-full rounded-lg border border-gray-600 bg-gray-900 px-3 py-2 text-sm text-gray-200 placeholder:text-gray-600 focus:border-blue-500 focus:outline-none focus:ring-2 focus:ring-blue-500/40"
									/>
								</div>
								<div class="sm:col-span-2">
									<label for="create-proxy-port" class="mb-1 block text-xs text-gray-400">{translate($language, 'wl.labelProxyPort')}</label>
									<input
										id="create-proxy-port"
										type="number"
										min="1"
										max="65535"
										value={selection.proxy_port}
										onchange={(event) => { selection.proxy_port = event.currentTarget.value; normalizeSelection(); }}
										placeholder="3000"
										class="w-full rounded-lg border border-gray-600 bg-gray-900 px-3 py-2 text-sm text-gray-200 placeholder:text-gray-600 focus:border-blue-500 focus:outline-none focus:ring-2 focus:ring-blue-500/40"
									/>
								</div>
							</div>
						{/if}
					</div>
				</fieldset>

				<fieldset class="space-y-3">
					<legend class="text-[11px] font-medium uppercase tracking-wider text-gray-400">{translate($language, 'wl.labelEnvironment')}</legend>
					<div class="grid gap-3 sm:grid-cols-2">
						<div>
							<label for="setup-mode" class="mb-1 block text-xs text-gray-400">{translate($language, 'wl.labelSetup')}</label>
							<select id="setup-mode" value={selection.setup_mode} onchange={(event) => { selection.setup_mode = event.currentTarget.value; normalizeSelection(); }} class="w-full rounded-lg border border-gray-600 bg-gray-900 px-3 py-2 text-sm text-gray-200 focus:border-blue-500 focus:outline-none focus:ring-2 focus:ring-blue-500/40">
								<option value="config-only">{translate($language, 'wl.optConfigOnly')}</option><option value="auto-install">{translate($language, 'wl.optAutoInstall')}</option>
							</select>
						</div>
						<div>
							<label for="website-node-version" class="mb-1 block text-xs text-gray-400">{translate($language, 'wl.labelNode')}</label>
							<select id="website-node-version" bind:value={selection.node_version} class="w-full rounded-lg border border-gray-600 bg-gray-900 px-3 py-2 text-sm text-gray-200 focus:border-blue-500 focus:outline-none focus:ring-2 focus:ring-blue-500/40">
								<option value="" disabled={selection.setup_mode === 'auto-install' && combination?.prerequisites?.includes('node')}>{translate($language, 'wl.optNone')}</option>
								{#each options?.node_versions || [] as version}<option value={version}>Node.js {version}{version === '24' ? ' (' + translate($language, 'wl.recommended') + ')' : ''}</option>{/each}
							</select>
							<p class="mt-1 text-xs text-gray-500">{selection.setup_mode === 'config-only' ? translate($language, 'wl.nodeHintConfigOnly') : translate($language, 'wl.nodeHintAutoInstall')}</p>
						</div>
					</div>
				</fieldset>

				{#if combination}
					<div class="rounded-lg border border-gray-700 bg-gray-900/60 p-3 text-sm text-gray-300">
						<p>{translate($language, 'wl.documentRoot')} <code class="text-gray-200">{combination.document_root || '-'}</code></p>
						{#if !combination.enabled}<p class="mt-2 text-yellow-300">{combination.reason}</p>{/if}
						{#if combination.missing_dependencies?.length}
							<div class="mt-2 flex flex-wrap gap-3">
								{#each combination.missing_dependencies as dependency}
									<a class="text-blue-400 underline" href={dependency.manage_url}>{translate($language, 'wl.installDependency').replace('{name}', dependency.name)}</a>
								{/each}
							</div>
						{/if}
					</div>
				{/if}

				<div class="flex items-center justify-end gap-2 border-t border-gray-700 pt-4">
					<button
						type="button"
						onclick={() => (showCreateForm = false)}
						class="cursor-pointer rounded-lg border border-gray-600 bg-gray-700 px-4 py-2 text-sm font-medium text-gray-200 transition hover:bg-gray-600"
					>
						{translate($language, 'wl.cancel')}
					</button>
					<button
						onclick={createWebsite}
						disabled={creating || !createDomain.trim() || !options || !combination?.enabled || (selection.template !== 'static' && selection.template !== 'reverse-proxy' && !selection.php_version) || (selection.template === 'reverse-proxy' && (!selection.proxy_host.trim() || !selection.proxy_port.trim()))}
						class="cursor-pointer rounded-lg bg-blue-600 px-4 py-2 text-sm font-semibold text-white transition hover:bg-blue-700 disabled:opacity-50"
					>
						{creating ? translate($language, 'wl.creating') : translate($language, 'wl.createWebsite')}
					</button>
				</div>
			</div>
		</div>
	{/if}

	{#if !loading && !error && websites.length > 0}
		<!-- Status overview doubles as the status filter -->
		<div class="grid grid-cols-2 gap-2 sm:grid-cols-5">
			{#each statusFilters as filter}
				<button
					type="button"
					onclick={() => toggleStatusFilter(filter.value)}
					aria-pressed={statusFilter === filter.value}
					class="cursor-pointer rounded-xl border bg-gray-800 px-3 py-2.5 text-left transition hover:border-gray-500 {statusFilter === filter.value
						? `border-transparent ring-2 ${filter.ring}`
						: 'border-gray-700'} {filter.value === 'all' ? 'col-span-2 sm:col-span-1' : ''}"
				>
					<p class="text-[11px] font-medium uppercase tracking-wider text-gray-400">{translate($language, filter.label)}</p>
					<p class="mt-0.5 text-xl font-bold tabular-nums {counts[filter.value] > 0 || filter.value === 'all' ? filter.tone : 'text-gray-600'}">{counts[filter.value]}</p>
				</button>
			{/each}
		</div>

		<!-- Toolbar -->
		<div class="flex flex-col gap-2 sm:flex-row sm:items-center">
			<div class="relative flex-1 sm:max-w-sm">
				<svg class="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-gray-500" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="2" aria-hidden="true">
					<path stroke-linecap="round" stroke-linejoin="round" d="m21 21-5.197-5.197m0 0A7.5 7.5 0 1 0 5.196 5.196a7.5 7.5 0 0 0 10.607 10.607Z" />
				</svg>
				<input
					bind:this={searchInput}
					type="search"
					bind:value={search}
					onkeydown={(e) => { if (e.key === 'Escape' && search) { e.stopPropagation(); search = ''; } }}
					placeholder={translate($language, 'wl.searchPlaceholder')}
					aria-label={translate($language, 'wl.searchAria')}
					class="w-full rounded-lg border border-gray-600 bg-gray-800 py-2 pl-9 pr-9 text-sm text-gray-200 placeholder-gray-500 focus:border-blue-500 focus:outline-none focus:ring-2 focus:ring-blue-500/40"
				/>
				{#if !search}
					<kbd class="pointer-events-none absolute right-2.5 top-1/2 hidden -translate-y-1/2 rounded border border-gray-600 px-1.5 font-mono text-[10px] text-gray-500 sm:block">/</kbd>
				{/if}
			</div>
			<div class="flex items-center gap-2 sm:ml-auto">
				<label for="website-sort" class="sr-only">{translate($language, 'wl.sortLabel')}</label>
				<select
					id="website-sort"
					value={sortBy}
					onchange={(e) => setSort(e.currentTarget.value)}
					class="flex-1 rounded-lg border border-gray-600 bg-gray-800 px-3 py-2 text-sm text-gray-200 focus:border-blue-500 focus:outline-none focus:ring-2 focus:ring-blue-500/40 sm:flex-none"
				>
					{#each sortKeys as key}
						<option value={key}>{translate($language, `wl.sort.${key}`)}</option>
					{/each}
				</select>
				<div class="flex rounded-lg border border-gray-600 bg-gray-800 p-0.5" role="group" aria-label={translate($language, 'wl.viewLabel')}>
					{#each [{ mode: 'grid', label: 'wl.viewGrid', path: 'M3.75 6A2.25 2.25 0 0 1 6 3.75h2.25A2.25 2.25 0 0 1 10.5 6v2.25a2.25 2.25 0 0 1-2.25 2.25H6a2.25 2.25 0 0 1-2.25-2.25V6ZM3.75 15.75A2.25 2.25 0 0 1 6 13.5h2.25a2.25 2.25 0 0 1 2.25 2.25V18a2.25 2.25 0 0 1-2.25 2.25H6A2.25 2.25 0 0 1 3.75 18v-2.25ZM13.5 6a2.25 2.25 0 0 1 2.25-2.25H18A2.25 2.25 0 0 1 20.25 6v2.25A2.25 2.25 0 0 1 18 10.5h-2.25a2.25 2.25 0 0 1-2.25-2.25V6ZM13.5 15.75a2.25 2.25 0 0 1 2.25-2.25H18a2.25 2.25 0 0 1 2.25 2.25V18A2.25 2.25 0 0 1 18 20.25h-2.25A2.25 2.25 0 0 1 13.5 18v-2.25Z' }, { mode: 'table', label: 'wl.viewTable', path: 'M3.75 6.75h16.5M3.75 12h16.5m-16.5 5.25h16.5' }] as const as option}
						<button
							type="button"
							onclick={() => setView(option.mode)}
							aria-pressed={viewMode === option.mode}
							title={translate($language, option.label)}
							aria-label={translate($language, option.label)}
							class="cursor-pointer rounded-md p-1.5 transition {viewMode === option.mode ? 'bg-gray-700 text-white' : 'text-gray-400 hover:text-white'}"
						>
							<svg class="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="1.8" aria-hidden="true">
								<path stroke-linecap="round" stroke-linejoin="round" d={option.path} />
							</svg>
						</button>
					{/each}
				</div>
			</div>
		</div>
		{#if hasFilters && filteredWebsites.length > 0}
			<p class="-mt-2 text-xs text-gray-500">
				{translate($language, 'wl.resultCount').replace('{shown}', String(filteredWebsites.length)).replace('{total}', String(websites.length))}
				<button type="button" onclick={clearFilters} class="ml-1 cursor-pointer text-blue-400 hover:underline">{translate($language, 'wl.clearFilters')}</button>
			</p>
		{/if}
	{/if}

	<!-- Website list -->
	{#if loading}
		<div class="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
			{#each Array(3) as _}
				<div class="animate-pulse rounded-xl border border-gray-700 bg-gray-800 p-5">
					<div class="mb-3 h-4 w-1/2 rounded bg-gray-700/60"></div>
					<div class="mb-2 h-3 w-1/3 rounded bg-gray-700/40"></div>
					<div class="h-3 w-1/4 rounded bg-gray-700/40"></div>
					<div class="mt-4 h-8 w-full rounded bg-gray-700/30"></div>
				</div>
			{/each}
		</div>
	{:else if error}
		<div class="flex flex-wrap items-center justify-between gap-2 rounded-lg border border-red-700 bg-red-900/30 p-4 text-sm text-red-300">
			<span>{error}</span>
			<button type="button" onclick={loadWebsites} class="cursor-pointer underline">{translate($language, 'wl.retry')}</button>
		</div>
	{:else if websites.length === 0}
		<div class="flex flex-col items-center gap-3 rounded-xl border border-dashed border-gray-600 bg-gray-800/50 px-6 py-12 text-center">
			<span class="flex h-12 w-12 items-center justify-center rounded-xl bg-blue-500/10 text-blue-400">
				<svg class="h-6 w-6" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="1.5" aria-hidden="true">
					<path stroke-linecap="round" stroke-linejoin="round" d="M12 21a9.004 9.004 0 0 0 8.716-6.747M12 21a9.004 9.004 0 0 1-8.716-6.747M12 21c2.485 0 4.5-4.03 4.5-9S14.485 3 12 3m0 18c-2.485 0-4.5-4.03-4.5-9S9.515 3 12 3m0 0a8.997 8.997 0 0 1 7.843 4.582M12 3a8.997 8.997 0 0 0-7.843 4.582m15.686 0A11.953 11.953 0 0 1 12 10.5c-2.998 0-5.74-1.1-7.843-2.918m15.686 0A8.959 8.959 0 0 1 21 12c0 .778-.099 1.533-.284 2.253m-18.432 0A8.959 8.959 0 0 1 3 12c0-1.605.42-3.113 1.157-4.418" />
				</svg>
			</span>
			<div>
				<p class="font-medium text-gray-200">{translate($language, 'wl.emptyTitle')}</p>
				<p class="mt-1 text-sm text-gray-400">{translate($language, 'wl.emptyDesc')}</p>
			</div>
			{#if !showCreateForm}
				<button
					onclick={() => (showCreateForm = true)}
					class="cursor-pointer rounded-lg bg-blue-600 px-4 py-2 text-sm font-semibold text-white transition hover:bg-blue-700"
				>
					{translate($language, 'wl.createWebsite')}
				</button>
			{/if}
		</div>
	{:else if filteredWebsites.length === 0}
		<div class="rounded-xl border border-dashed border-gray-600 bg-gray-800/50 px-6 py-10 text-center">
			<p class="text-sm text-gray-400">{translate($language, 'wl.emptyFiltered')}</p>
			<button onclick={clearFilters} class="mt-2 cursor-pointer text-sm text-blue-400 hover:underline">
				{translate($language, 'wl.clearFilters')}
			</button>
		</div>
	{:else if viewMode === 'table'}
		<div class="overflow-x-auto rounded-xl border border-gray-700 bg-gray-800">
			<table class="w-full min-w-[720px] text-sm">
				<thead>
					<tr class="border-b border-gray-700 text-left text-[11px] uppercase tracking-wider text-gray-400">
						<th class="px-4 py-2.5 font-medium">{translate($language, 'wl.labelDomain')}</th>
						<th class="px-3 py-2.5 font-medium">{translate($language, 'wl.labelStatus')}</th>
						<th class="px-3 py-2.5 font-medium">{translate($language, 'wl.labelStack')}</th>
						<th class="px-3 py-2.5 font-medium">PHP</th>
						<th class="px-3 py-2.5 text-right font-medium" title={translate($language, 'wl.bandwidthMonthHint')}>{translate($language, 'wl.labelBandwidth')}</th>
						<th class="px-3 py-2.5 font-medium">{translate($language, 'wl.labelCreated')}</th>
						<th class="px-4 py-2.5"><span class="sr-only">{translate($language, 'wl.labelActions')}</span></th>
					</tr>
				</thead>
				<tbody class="divide-y divide-gray-700/60">
					{#each filteredWebsites as website (website.id)}
						<tr class="transition hover:bg-gray-700/30">
							<td class="max-w-[260px] px-4 py-2.5">
								<div class="flex items-center gap-1">
									<a href="/websites/{website.id}" class="truncate font-medium text-blue-400 hover:text-blue-300 hover:underline" title={website.domain}>{website.domain}</a>
									{@render domainLinks(website)}
								</div>
								{#if isPending(website.status) && website.provision_stage}
									<p class="truncate text-xs text-yellow-300">{website.provision_stage}</p>
								{:else if website.status === 'failed' && website.error_message}
									<p class="truncate text-xs text-red-400" title={website.error_message}>{website.error_message}</p>
								{/if}
							</td>
							<td class="px-3 py-2.5">{@render statusBadge(website.status)}</td>
							<td class="px-3 py-2.5 capitalize text-gray-300">{frameworkLabel(website)}</td>
							<td class="px-3 py-2.5 text-gray-300">{usesPHP(website) ? website.php_version : '—'}</td>
							<td class="px-3 py-2.5 text-right tabular-nums text-gray-300">{formatBytes(website.bandwidth_bytes)}</td>
							<td class="px-3 py-2.5 text-gray-400" title={website.owner_email || translate($language, 'wl.ownerLegacy')}>{new Date(website.created_at).toLocaleDateString()}</td>
							<td class="px-4 py-2.5">{@render rowActions(website, true)}</td>
						</tr>
					{/each}
				</tbody>
			</table>
		</div>
	{:else}
		<div class="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
			{#each filteredWebsites as website (website.id)}
				<div class="flex flex-col rounded-xl border bg-gray-800 p-4 transition hover:border-gray-500 {website.status === 'failed'
					? 'border-red-800/70'
					: isPending(website.status)
						? 'border-yellow-700/50'
						: 'border-gray-700'}">
					<div class="flex items-start gap-3">
						<span class="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg bg-gray-700/60 text-sm font-bold uppercase text-gray-200" aria-hidden="true">
							{website.domain.charAt(0)}
						</span>
						<div class="min-w-0 flex-1">
							<div class="flex items-center gap-1">
								<a href="/websites/{website.id}" class="truncate text-base font-semibold text-blue-400 hover:text-blue-300 hover:underline" title={website.domain}>
									{website.domain}
								</a>
								{@render domainLinks(website)}
							</div>
							<p class="truncate text-xs capitalize text-gray-400">
								{frameworkLabel(website)}{usesPHP(website) ? ` · PHP ${website.php_version}` : ''}
							</p>
						</div>
						{@render statusBadge(website.status)}
					</div>

					<dl class="mt-3 grid grid-cols-3 gap-2 rounded-lg bg-gray-900/40 px-3 py-2 text-xs">
						<div class="min-w-0">
							<dt class="text-gray-500" title={translate($language, 'wl.bandwidthMonthHint')}>{translate($language, 'wl.labelBandwidth')}</dt>
							<dd class="font-medium tabular-nums text-gray-300">{formatBytes(website.bandwidth_bytes)}</dd>
						</div>
						<div class="min-w-0">
							<dt class="text-gray-500">{translate($language, 'wl.labelCreated')}</dt>
							<dd class="font-medium text-gray-300">{new Date(website.created_at).toLocaleDateString()}</dd>
						</div>
						<div class="min-w-0">
							<dt class="text-gray-500">{translate($language, 'wl.labelOwner')}</dt>
							<dd class="truncate font-medium text-gray-300" title={website.owner_email}>{website.owner_email || translate($language, 'wl.ownerLegacy')}</dd>
						</div>
					</dl>

					{#if isPending(website.status) && website.provision_stage}
						<p class="mt-2 flex items-center gap-1.5 text-xs text-yellow-300">
							<span class="h-3 w-3 shrink-0 animate-spin rounded-full border border-yellow-400 border-t-transparent"></span>
							{website.provision_stage}
						</p>
					{/if}
					{#if website.status === 'failed' && website.error_message}
						<p class="mt-2 break-words text-xs text-red-400">{website.error_message}</p>
					{/if}
					{#if website.status === 'failed' && website.provision_log}
						<details class="mt-2">
							<summary class="cursor-pointer text-xs text-gray-400 hover:text-gray-300">{translate($language, 'wl.provisionLog')}</summary>
							<pre class="mt-1 max-h-40 overflow-auto whitespace-pre-wrap rounded-lg bg-gray-950 p-2 text-xs text-gray-300">{website.provision_log}</pre>
						</details>
					{/if}

					<div class="mt-auto border-t border-gray-700/60 pt-3 {isPending(website.status) || website.status === 'failed' ? 'mt-3' : 'mt-4'}">
						{@render rowActions(website, false)}
					</div>
				</div>
			{/each}
		</div>
	{/if}
</div>

{#if confirmModal}
	{@const copy = confirmCopy[confirmModal.action]}
	<!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
	<div
		class="fixed inset-0 z-50 flex items-center justify-center overflow-y-auto bg-black/60 p-4 backdrop-blur-sm"
		role="dialog"
		aria-modal="true"
		tabindex="-1"
		aria-label={translate($language, copy.title)}
		onclick={(e) => {
			if (e.target === e.currentTarget) closeConfirm();
		}}
	>
		<div class="w-full max-w-md rounded-2xl border border-gray-700 bg-gray-800 p-5 shadow-2xl">
			<div class="mb-3 flex items-start justify-between gap-3">
				<h3 class="text-base font-semibold text-white">{translate($language, copy.title)}</h3>
				<button
					type="button"
					onclick={closeConfirm}
					class="cursor-pointer rounded-lg p-1.5 text-gray-400 transition hover:bg-gray-700 hover:text-white"
					aria-label={translate($language, 'wl.cancel')}
				>
					<svg class="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="2" aria-hidden="true">
						<path stroke-linecap="round" stroke-linejoin="round" d="M6 18L18 6M6 6l12 12" />
					</svg>
				</button>
			</div>
			<p class="text-sm text-gray-300">{translate($language, copy.body).replace('{domain}', confirmModal.website.domain)}</p>
			{#if confirmModal.action === 'delete' || confirmModal.action === 'repair'}
				<pre class="mt-3 overflow-x-auto rounded-lg border border-gray-700 bg-gray-950 px-3 py-2 font-mono text-xs break-all whitespace-pre-wrap text-gray-200">{confirmModal.website.domain}</pre>
			{/if}
			<div class="mt-4 flex items-center justify-end gap-2 border-t border-gray-700 pt-4">
				<button
					type="button"
					onclick={closeConfirm}
					disabled={confirmBusy}
					class="cursor-pointer rounded-lg border border-gray-600 bg-gray-700 px-4 py-2 text-sm font-medium text-gray-200 transition hover:bg-gray-600 disabled:opacity-40"
				>
					{translate($language, 'wl.cancel')}
				</button>
				<button
					type="button"
					onclick={runConfirmed}
					disabled={confirmBusy || (confirmModal.action === 'repair' && repairBusy)}
					class="flex cursor-pointer items-center gap-2 rounded-lg px-4 py-2 text-sm font-semibold text-white transition disabled:opacity-40 {copy.button}"
				>
					{#if confirmBusy}
						<svg class="h-4 w-4 animate-spin" viewBox="0 0 24 24" fill="none" aria-hidden="true">
							<circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4" />
							<path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 0 1 8-8v4a4 4 0 0 0-4 4H4z" />
						</svg>
					{/if}
					{translate($language, copy.yes)}
				</button>
			</div>
		</div>
	</div>
{/if}

{#if dnsHelpOpen}
	<!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
	<div
		class="fixed inset-0 z-50 flex items-center justify-center overflow-y-auto bg-black/60 p-4 backdrop-blur-sm"
		role="dialog"
		aria-modal="true"
		tabindex="-1"
		aria-label={translate($language, 'wl.dnsHelp.title')}
		onclick={(e) => {
			if (e.target === e.currentTarget) dnsHelpOpen = false;
		}}
	>
		<div class="w-full max-w-2xl rounded-2xl border border-gray-700 bg-gray-800 p-5 shadow-2xl">
			<div class="mb-3 flex items-start justify-between gap-3">
				<h3 class="text-base font-semibold text-white">{translate($language, 'wl.dnsHelp.title')}</h3>
				<button
					type="button"
					onclick={() => (dnsHelpOpen = false)}
					class="cursor-pointer rounded-lg p-1.5 text-gray-400 transition hover:bg-gray-700 hover:text-white"
					aria-label={translate($language, 'wl.cancel')}
				>
					<svg class="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="2" aria-hidden="true">
						<path stroke-linecap="round" stroke-linejoin="round" d="M6 18L18 6M6 6l12 12" />
					</svg>
				</button>
			</div>

			<p class="text-sm leading-6 text-gray-300">{translate($language, 'wl.dnsHelp.intro')}</p>

			<div class="mt-3 rounded-lg border border-gray-700 bg-gray-900 p-3">
				<div class="flex flex-wrap items-center justify-between gap-2">
					<p class="text-xs font-medium uppercase tracking-wider text-gray-400">{translate($language, 'wl.dnsHelp.ipTitle')}</p>
					{#if serverPublicIP}
						<button
							type="button"
							onclick={copyServerIP}
							class="cursor-pointer rounded-md border border-gray-600 px-2 py-0.5 text-[10px] text-gray-300 transition hover:bg-gray-700"
						>
							{translate($language, 'wl.dnsHelp.copy')}
						</button>
					{/if}
				</div>
				{#if serverIPLoading}
					<p class="mt-1 text-sm text-gray-500">{translate($language, 'wl.dnsHelp.ipLoading')}</p>
				{:else if serverPublicIP}
					<p class="mt-1 font-mono text-lg font-semibold text-green-400">{serverPublicIP}</p>
				{:else}
					<p class="mt-1 text-sm text-gray-500">{translate($language, 'wl.dnsHelp.ipUnavailable')}</p>
				{/if}
			</div>

			<div class="mt-4">
				<h4 class="text-sm font-semibold text-white">{translate($language, 'wl.dnsHelp.whatTitle')}</h4>
				<p class="mt-1 text-sm leading-6 text-gray-300">{translate($language, 'wl.dnsHelp.whatText')}</p>
			</div>

			<div class="mt-4">
				<h4 class="text-sm font-semibold text-white">{translate($language, 'wl.dnsHelp.stepsTitle')}</h4>
				<ol class="mt-2 list-decimal space-y-2 pl-5 text-sm leading-6 text-gray-300">
					<li>{translate($language, 'wl.dnsHelp.step1')}</li>
					<li>{translate($language, 'wl.dnsHelp.step2')}</li>
					<li>{translate($language, 'wl.dnsHelp.step3')}</li>
					<li>{translate($language, 'wl.dnsHelp.step4')}</li>
					<li>{translate($language, 'wl.dnsHelp.step5')}</li>
					<li>{translate($language, 'wl.dnsHelp.step6')}</li>
				</ol>
			</div>

			<div class="mt-4 rounded-lg border border-yellow-700/50 bg-yellow-900/20 p-3">
				<h4 class="text-sm font-semibold text-yellow-300">{translate($language, 'wl.dnsHelp.tipsTitle')}</h4>
				<ul class="mt-1 list-disc space-y-1 pl-5 text-sm leading-6 text-yellow-200/90">
					<li>{translate($language, 'wl.dnsHelp.tipCloudflare')}</li>
					<li>{translate($language, 'wl.dnsHelp.tipReplace')}</li>
				</ul>
			</div>

			<div class="mt-4 flex items-center justify-end border-t border-gray-700 pt-4">
				<button
					type="button"
					onclick={() => (dnsHelpOpen = false)}
					class="cursor-pointer rounded-lg border border-gray-600 bg-gray-700 px-4 py-2 text-sm font-medium text-gray-200 transition hover:bg-gray-600"
				>
					{translate($language, 'wl.cancel')}
				</button>
			</div>
		</div>
	</div>
{/if}
