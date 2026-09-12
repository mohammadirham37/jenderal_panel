<script lang="ts">
	import { onMount, onDestroy } from 'svelte';
	import { api } from '$lib/api';
	import TaskProgress from '$lib/components/TaskProgress.svelte';
	import { availablePHPVersions, normalizeWebsiteSelection, selectedCombination } from '$lib/website-form.js';
import { toast } from '$lib/stores/toast';
import { language, translate } from '$lib/stores/language';

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
		created_at: string;
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
	let repairConfirmId = $state<string | null>(null);
	let repairBusy = $derived(repairing || !!repairTaskId);

	// Toolbar: search + status filter
	let search = $state('');
	let statusFilter = $state('all');
	const statusFilters = [
		{ value: 'all', label: 'wl.filterAll' },
		{ value: 'active', label: 'wl.filterActive' },
		{ value: 'pending', label: 'wl.filterInProgress' },
		{ value: 'failed', label: 'wl.filterFailed' },
		{ value: 'suspended', label: 'wl.filterSuspended' }
	];

	const pendingStatuses = ['pending', 'installing', 'configuring', 'validating'];

	let filteredWebsites = $derived.by(() => {
		const query = search.trim().toLowerCase();
		return websites.filter((w) => {
			if (query && !w.domain.toLowerCase().includes(query)) return false;
			if (statusFilter === 'all') return true;
			if (statusFilter === 'pending') return pendingStatuses.includes(w.status);
			if (statusFilter === 'suspended') return w.status === 'suspended' || w.status === 'disabled';
			return w.status === statusFilter;
		});
	});

	const counts = $derived.by(() => ({
		all: websites.length,
		active: websites.filter((w) => w.status === 'active').length,
		pending: websites.filter((w) => pendingStatuses.includes(w.status)).length,
		failed: websites.filter((w) => w.status === 'failed').length,
		suspended: websites.filter((w) => w.status === 'suspended' || w.status === 'disabled').length
	}));

	async function repairLaravel(id: string) {
		if (repairBusy) return;
		repairing = true; repairConfirmId = null;
		try {
			const result = await api.post<{task_id: string}>(`/api/v1/websites/${id}/repair-laravel`, {confirm: true});
			repairTaskId = result.task_id;
		} catch (err) { toast.error(err instanceof Error ? err.message : translate($language, 'wl.toastRepairFailed')); repairing = false; }
	}

	// Create form
	let showCreateForm = $state(false);
	let createDomain = $state('');
	let selection = $state<WebsiteSelection>({ template: 'php', php_version: '', framework_version: '', frontend_stack: '', inertia_adapter: '', project_variant: 'empty', setup_mode: 'config-only', node_version: '' });
	let creating = $state(false);
	let installedPHP = $derived(options ? availablePHPVersions(options) as RuntimeOption[] : []);
	let combination = $derived(options ? selectedCombination(options, selection) : null);
	let laravelVersions = $derived(options ? [...new Set(options.profiles.filter((item) => item.template === 'laravel').map((item) => item.framework_version))] : []);
	let laravelOctaneVersions = $derived(options ? [...new Set(options.profiles.filter((item) => item.template === 'laravel-octane').map((item) => item.framework_version))] : []);

	// Delete confirm
	let deleteConfirmId = $state<string | null>(null);

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
		return pendingStatuses.includes(status);
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

	function frameworkLabel(website: Website): string {
		const base = website.framework && website.framework !== 'none' ? website.framework : website.app_type;
		return website.framework_version ? `${base} ${website.framework_version}` : base;
	}

	function shouldPoll(sites: Website[]): boolean {
		return sites.some((w) => pendingStatuses.includes(w.status));
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
			const body: Record<string, string> = { domain: createDomain, ...selection };
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

	async function suspendWebsite(id: string) {
		try {
			await api.post(`/api/v1/websites/${id}/suspend`);
			toast.success(translate($language, 'wl.toastSuspended'));
			await loadWebsites();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : translate($language, 'wl.errorSuspend'));
		}
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

	async function deleteWebsite(id: string) {
		deleteConfirmId = null;
		try {
			await api.del(`/api/v1/websites/${id}`);
			toast.success(translate($language, 'wl.toastDeleted'));
			await loadWebsites();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : translate($language, 'wl.errorDelete'));
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

<div class="space-y-5">
	<div class="flex flex-wrap items-start justify-between gap-3">
		<div>
			<h2 class="text-2xl font-bold text-white">{translate($language, 'wl.title')}</h2>
			<p class="mt-0.5 text-sm text-gray-400">
				{websites.length === 0
					? translate($language, 'wl.subtitleEmpty')
					: translate($language, 'wl.summaryCounts')
							.replace('{active}', String(counts.active))
							.replace('{pending}', String(counts.pending))
							.replace('{failed}', String(counts.failed))
							.replace('{suspended}', String(counts.suspended))}
			</p>
		</div>
		<button
			onclick={() => (showCreateForm = !showCreateForm)}
			class="inline-flex cursor-pointer items-center gap-1.5 rounded-lg bg-blue-600 px-4 py-2 text-sm font-semibold text-white transition hover:bg-blue-700"
		>
			<svg class="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="2" aria-hidden="true">
				<path stroke-linecap="round" stroke-linejoin="round" d="M12 4.5v15m7.5-7.5h-15" />
			</svg>
			{showCreateForm ? translate($language, 'wl.cancel') : translate($language, 'wl.createWebsite')}
		</button>
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
					<label for="create-domain" class="mb-1 block text-[11px] font-medium uppercase tracking-wider text-gray-400">{translate($language, 'wl.labelDomain')}</label>
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
								<option value="php">{translate($language, 'wl.optPhp')}</option>
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
						{#if selection.template !== 'static'}
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
						disabled={creating || !createDomain.trim() || !options || !combination?.enabled || (selection.template !== 'static' && !selection.php_version)}
						class="cursor-pointer rounded-lg bg-blue-600 px-4 py-2 text-sm font-semibold text-white transition hover:bg-blue-700 disabled:opacity-50"
					>
						{creating ? translate($language, 'wl.creating') : translate($language, 'wl.createWebsite')}
					</button>
				</div>
			</div>
		</div>
	{/if}

	<!-- Toolbar -->
	{#if !loading && websites.length > 3}
		<div class="flex flex-col gap-2 sm:flex-row sm:items-center">
			<div class="relative sm:w-72">
				<svg class="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-gray-500" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="2" aria-hidden="true">
					<path stroke-linecap="round" stroke-linejoin="round" d="m21 21-5.197-5.197m0 0A7.5 7.5 0 1 0 5.196 5.196a7.5 7.5 0 0 0 10.607 10.607Z" />
				</svg>
				<input
					type="search"
					bind:value={search}
					placeholder={translate($language, 'wl.searchPlaceholder')}
					aria-label={translate($language, 'wl.searchAria')}
					class="w-full rounded-lg border border-gray-600 bg-gray-800 py-2 pl-9 pr-3 text-sm text-gray-200 placeholder-gray-500 focus:border-blue-500 focus:outline-none focus:ring-2 focus:ring-blue-500/40"
				/>
			</div>
			<div class="flex flex-wrap gap-1.5">
				{#each statusFilters as filter}
					<button
						onclick={() => (statusFilter = filter.value)}
						class="cursor-pointer rounded-full px-3 py-1 text-xs font-medium transition {statusFilter === filter.value
							? 'bg-blue-600 text-white'
							: 'border border-gray-600 bg-gray-800 text-gray-300 hover:bg-gray-700'}"
					>
						{translate($language, filter.label)}
						{#if counts[filter.value as keyof typeof counts] !== undefined}
							<span class="ml-1 opacity-70">{counts[filter.value as keyof typeof counts]}</span>
						{/if}
					</button>
				{/each}
			</div>
		</div>
	{/if}

	<!-- Website Cards -->
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
		<div class="rounded-lg border border-red-700 bg-red-900/30 p-4 text-sm text-red-300">{error}</div>
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
			<button
				onclick={() => (showCreateForm = true)}
				class="cursor-pointer rounded-lg bg-blue-600 px-4 py-2 text-sm font-semibold text-white transition hover:bg-blue-700"
			>
				{translate($language, 'wl.createWebsite')}
			</button>
		</div>
	{:else if filteredWebsites.length === 0}
		<div class="rounded-xl border border-dashed border-gray-600 bg-gray-800/50 px-6 py-10 text-center">
			<p class="text-sm text-gray-400">{translate($language, 'wl.emptyFiltered')}</p>
			<button
				onclick={() => { search = ''; statusFilter = 'all'; }}
				class="mt-2 cursor-pointer text-sm text-blue-400 hover:underline"
			>
				{translate($language, 'wl.clearFilters')}
			</button>
		</div>
	{:else}
		<div class="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
			{#each filteredWebsites as website (website.id)}
				<div class="flex flex-col rounded-xl border border-gray-700 bg-gray-800 p-4 transition hover:border-gray-600 {isPending(website.status) ? 'ring-1 ring-yellow-500/20' : ''}">
					<div class="flex items-start justify-between gap-2">
						<a href="/websites/{website.id}" class="min-w-0 flex-1 truncate text-base font-semibold text-blue-400 hover:text-blue-300 hover:underline" title={website.domain}>
							{website.domain}
						</a>
						<span class="inline-flex shrink-0 items-center gap-1.5 rounded-full px-2 py-0.5 text-xs font-medium {statusBadgeClass(website.status)}">
							<span class="h-1.5 w-1.5 rounded-full {statusDotClass(website.status)}"></span>
							{website.status}
						</span>
					</div>

					<dl class="mt-3 grid grid-cols-2 gap-x-3 gap-y-1.5 text-xs">
						<div class="min-w-0">
							<dt class="text-gray-500">{translate($language, 'wl.labelStack')}</dt>
							<dd class="truncate font-medium capitalize text-gray-300">{frameworkLabel(website)}</dd>
						</div>
						<div>
							<dt class="text-gray-500">PHP</dt>
							<dd class="truncate font-medium text-gray-300">{website.app_type === 'static' ? '—' : website.php_version || '—'}</dd>
						</div>
						<div class="col-span-2">
							<dt class="text-gray-500">{translate($language, 'wl.labelCreated')}</dt>
							<dd class="font-medium text-gray-300">{new Date(website.created_at).toLocaleDateString()}</dd>
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

					<div class="mt-auto pt-4">
						{#if repairConfirmId === website.id}
							<div class="rounded-lg border border-yellow-600/50 bg-yellow-900/20 p-2.5">
								<p class="text-xs text-yellow-300">{translate($language, 'wl.repairConfirm')}</p>
								<div class="mt-2 flex gap-2">
									<button class="cursor-pointer rounded-md bg-blue-600 px-2.5 py-1 text-xs font-medium text-white disabled:opacity-50" onclick={() => repairLaravel(website.id)} disabled={repairBusy}>{translate($language, 'wl.confirmRepair')}</button>
									<button class="cursor-pointer text-xs text-gray-400 hover:text-gray-200" onclick={() => repairConfirmId = null}>{translate($language, 'wl.cancel')}</button>
								</div>
							</div>
						{:else if deleteConfirmId === website.id}
							<div class="rounded-lg border border-red-600/50 bg-red-900/20 p-2.5">
								<p class="text-xs text-red-300">{translate($language, 'wl.deleteConfirm')}</p>
								<div class="mt-2 flex gap-2">
									<button class="cursor-pointer rounded-md bg-red-600 px-2.5 py-1 text-xs font-medium text-white transition hover:bg-red-700" onclick={() => deleteWebsite(website.id)}>{translate($language, 'wl.yesDelete')}</button>
									<button class="cursor-pointer text-xs text-gray-400 hover:text-gray-200" onclick={() => (deleteConfirmId = null)}>{translate($language, 'wl.cancel')}</button>
								</div>
							</div>
						{:else}
							<div class="flex flex-wrap items-center gap-2">
								<a
									href="/websites/{website.id}"
									class="rounded-md bg-blue-600/90 px-2.5 py-1.5 text-xs font-medium text-white transition hover:bg-blue-600"
								>
									{translate($language, 'wl.manage')}
								</a>
								{#if website.status === 'active' && website.framework === 'laravel' && website.setup_mode === 'auto-install'}
									<button class="cursor-pointer rounded-md border border-gray-600 bg-gray-700 px-2.5 py-1.5 text-xs text-gray-200 transition hover:bg-gray-600 disabled:opacity-50" disabled={repairBusy} onclick={() => repairConfirmId = website.id}>{translate($language, 'wl.repair')}</button>
								{/if}
								{#if website.status === 'failed'}
									<button onclick={() => retryWebsite(website.id)} class="cursor-pointer rounded-md border border-yellow-600/50 bg-yellow-600/20 px-2.5 py-1.5 text-xs font-medium text-yellow-300 transition hover:bg-yellow-600/30">
										{translate($language, 'wl.retry')}
									</button>
								{/if}
								{#if website.status === 'active'}
									<button onclick={() => suspendWebsite(website.id)} class="cursor-pointer rounded-md border border-gray-600 bg-gray-700 px-2.5 py-1.5 text-xs text-gray-200 transition hover:bg-gray-600">
										{translate($language, 'wl.suspend')}
									</button>
								{/if}
								{#if website.status === 'suspended' || website.status === 'disabled'}
									<button onclick={() => enableWebsite(website.id)} class="cursor-pointer rounded-md border border-green-600/50 bg-green-600/20 px-2.5 py-1.5 text-xs font-medium text-green-300 transition hover:bg-green-600/30">
										{translate($language, 'wl.enable')}
									</button>
								{/if}
								<button onclick={() => (deleteConfirmId = website.id)} class="ml-auto cursor-pointer rounded-md px-2.5 py-1.5 text-xs text-red-400 transition hover:bg-red-500/10" title={translate($language, 'wl.titleDelete')}>
									{translate($language, 'wl.delete')}
								</button>
							</div>
						{/if}
					</div>
				</div>
			{/each}
		</div>
	{/if}
</div>
