<script lang="ts">
	import { onMount } from 'svelte';
	import { api } from '$lib/api';
	import { language, translate } from '$lib/stores/language';
	import TaskProgress from '$lib/components/TaskProgress.svelte';

	interface FilesystemStat {
		source: string;
		fstype: string;
		size_bytes: number;
		used_bytes: number;
		avail_bytes: number;
		use_percent: number;
		mounted_on: string;
	}

	interface DirUsage {
		path: string;
		label: string;
		bytes: number;
		missing: boolean;
	}

	interface SiteUsage {
		id: string;
		domain: string;
		path: string;
		bytes: number;
	}

	interface BreakdownEntry {
		path: string;
		bytes: number;
	}

	interface SiteBreakdown {
		website_id: string;
		domain: string;
		home: string;
		total_bytes: number;
		entries: BreakdownEntry[];
	}

	interface DockerUsage {
		type: string;
		size: string;
	}

	interface Overview {
		filesystems: FilesystemStat[];
		dirs: DirUsage[];
		websites: SiteUsage[];
		root_dirs: DirUsage[];
		journal_bytes: number;
		kernel: string;
		old_kernels: string[];
		has_docker: boolean;
		docker: DockerUsage[];
	}

	interface CleanupAction {
		name: string;
		commands: string[][];
	}

	interface CleanupOptions {
		journal_vacuum_mb: number;
		apt_clean: boolean;
		apt_autoremove: boolean;
		tmp_clean_days: number;
		docker_prune: boolean;
	}

	let overview = $state<Overview | null>(null);
	let loading = $state(true);
	let error = $state('');

	let options = $state<CleanupOptions>({
		journal_vacuum_mb: 0,
		apt_clean: false,
		apt_autoremove: false,
		tmp_clean_days: 0,
		docker_prune: false
	});
	let plan = $state<CleanupAction[]>([]);
	let starting = $state(false);
	let startError = $state('');
	let taskId = $state('');

	function formatBytes(bytes: number): string {
		if (!bytes) return '0 B';
		const units = ['B', 'KB', 'MB', 'GB', 'TB'];
		const i = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1);
		return `${(bytes / Math.pow(1024, i)).toFixed(i === 0 ? 0 : 1)} ${units[i]}`;
	}

	function labelKey(label: string): string {
		return `dku.label.${label}`;
	}

	function shareOf(bytes: number, max: number): number {
		if (max <= 0) return 0;
		return Math.max(2, Math.round((bytes / max) * 100));
	}

	// Usage severity: red past 90%, yellow past 75%, blue otherwise.
	function severityClass(percent: number): string {
		if (percent >= 90) return 'bg-red-500 stroke-red-500';
		if (percent >= 75) return 'bg-yellow-500 stroke-yellow-500';
		return 'bg-blue-500 stroke-blue-500';
	}

	// Root filesystem first (it is the one users care about), the rest by size.
	const sortedFilesystems = $derived.by(() => {
		if (!overview) return [];
		return [...overview.filesystems].sort((a, b) => {
			if (a.mounted_on === '/') return -1;
			if (b.mounted_on === '/') return 1;
			return b.size_bytes - a.size_bytes;
		});
	});

	const primaryFs = $derived(sortedFilesystems.find((fs) => fs.mounted_on === '/') ?? sortedFilesystems[0]);

	const RING_RADIUS = 52;
	const RING_CIRCUMFERENCE = 2 * Math.PI * RING_RADIUS;
	const ringOffset = $derived(
		RING_CIRCUMFERENCE * (1 - Math.min(primaryFs?.use_percent ?? 0, 100) / 100)
	);

	interface DirRow {
		key: string;
		label: string;
		path: string;
		bytes: number;
		missing: boolean;
	}

	// Labeled locations plus the journal, so the bars compare against one max.
	const dirRows = $derived.by<DirRow[]>(() => {
		if (!overview) return [];
		const rows: DirRow[] = overview.dirs.map((d) => ({
			key: d.path,
			label: translate($language, labelKey(d.label)),
			path: d.path,
			bytes: d.missing ? 0 : d.bytes,
			missing: d.missing
		}));
		if (overview.journal_bytes > 0) {
			rows.push({
				key: '/var/log/journal',
				label: translate($language, 'dku.journal'),
				path: '/var/log/journal',
				bytes: overview.journal_bytes,
				missing: false
			});
		}
		return rows;
	});

	const maxDirBytes = $derived(Math.max(1, ...dirRows.map((r) => r.bytes)));
	const maxRootBytes = $derived(Math.max(1, ...(overview?.root_dirs.map((d) => d.bytes) ?? [1])));

	// Per-website folder breakdown (expandable rows in "Websites by Size").
	// One row is expanded at a time; the report is fetched on demand.
	let expandedSiteId = $state<string | null>(null);
	let expandedBreakdown = $state<SiteBreakdown | null>(null);
	let expandedState = $state<'loading' | 'error' | 'ready'>('loading');

	function folderName(path: string, home: string): string {
		if (path.startsWith(home + '/')) return path.slice(home.length + 1);
		return path;
	}

	async function fetchBreakdown(siteId: string) {
		expandedState = 'loading';
		try {
			expandedBreakdown = await api.get<SiteBreakdown>(`/api/v1/websites/${siteId}/disk-usage`);
			expandedState = 'ready';
		} catch {
			expandedBreakdown = null;
			expandedState = 'error';
		}
	}

	async function toggleSite(site: SiteUsage) {
		if (expandedSiteId === site.id) {
			expandedSiteId = null;
			return;
		}
		expandedSiteId = site.id;
		await fetchBreakdown(site.id);
	}

	async function retryBreakdown(site: SiteUsage) {
		if (expandedSiteId !== site.id) return;
		await fetchBreakdown(site.id);
	}

	async function load() {
		loading = true;
		error = '';
		try {
			overview = await api.get<Overview>('/api/v1/disk/usage');
		} catch (err) {
			error = err instanceof Error ? err.message : translate($language, 'dku.loadFailed');
		} finally {
			loading = false;
		}
	}

	async function preview() {
		startError = '';
		try {
			plan = (await api.post<CleanupAction[]>('/api/v1/disk/plan', options)) || [];
		} catch (err) {
			plan = [];
			startError = err instanceof Error ? err.message : '';
		}
	}

	async function runCleanup() {
		startError = '';
		if (plan.length === 0) {
			startError = translate($language, 'dku.cleanup.empty');
			return;
		}
		starting = true;
		try {
			const data = await api.post<{ task_id: string }>('/api/v1/disk/cleanup', options);
			taskId = data.task_id;
		} catch (err) {
			startError = err instanceof Error ? err.message : '';
		} finally {
			starting = false;
		}
	}

	// Re-fetch the preview whenever the selection changes.
	$effect(() => {
		JSON.stringify(options);
		preview();
	});

	onMount(() => {
		load();
	});
</script>

{#snippet usageBarRow(label: string, sub: string, bytes: number, max: number, missing: boolean)}
	<div class="flex items-center gap-3 py-2">
		<div class="w-44 shrink-0 sm:w-56">
			<p class="truncate text-sm font-medium text-white">{label}</p>
			<p class="truncate font-mono text-[11px] text-gray-500" title={sub}>{sub}</p>
		</div>
		<div class="h-2.5 flex-1 overflow-hidden rounded-full bg-gray-700">
			{#if !missing}
				<div class="h-full rounded-full bg-blue-500" style="width: {shareOf(bytes, max)}%"></div>
			{/if}
		</div>
		<span class="w-20 shrink-0 text-right text-sm tabular-nums text-gray-300">
			{missing ? translate($language, 'dku.dir.missing') : formatBytes(bytes)}
		</span>
	</div>
{/snippet}

<div class="space-y-6">
	<div class="flex flex-wrap items-center justify-between gap-2">
		<div>
			<h2 class="text-2xl font-bold text-white">{translate($language, 'dku.title')}</h2>
			<p class="text-sm text-gray-500">{translate($language, 'dku.subtitle')}</p>
		</div>
		<button
			onclick={load}
			disabled={loading}
			class="cursor-pointer rounded-lg bg-gray-700 px-4 py-2 text-sm font-medium text-gray-200 transition-colors hover:bg-gray-600 disabled:opacity-50"
		>
			{translate($language, 'dku.refresh')}
		</button>
	</div>

	{#if taskId}
		<div class="rounded-lg border border-gray-700 bg-gray-800 p-5">
			<h3 class="mb-3 text-lg font-semibold text-white">{translate($language, 'dku.cleanup.plan')}</h3>
			<TaskProgress bind:taskId storageKey="jenderal_disk_cleanup" onComplete={load} />
		</div>
	{/if}

	{#if loading}
		<div class="rounded-lg border border-gray-700 bg-gray-800 p-8 text-center text-gray-400">
			{translate($language, 'dku.loading')}
		</div>
	{:else if error}
		<div class="rounded-lg border border-red-700 bg-red-900/50 p-4 text-red-300">{error}</div>
	{:else if overview}
		{#if primaryFs}
			<!-- Storage overview: ring gauge for the primary filesystem plus quick facts -->
			<div class="rounded-xl border border-gray-700 bg-gray-800 p-5 sm:p-6">
				<div class="flex flex-col items-center gap-6 sm:flex-row sm:gap-8">
					<div class="relative shrink-0">
						<svg viewBox="0 0 120 120" class="h-32 w-32 -rotate-90">
							<circle cx="60" cy="60" r={RING_RADIUS} fill="none" stroke-width="12" class="stroke-gray-700" />
							<circle
								cx="60" cy="60" r={RING_RADIUS} fill="none" stroke-width="12" stroke-linecap="round"
								class={severityClass(primaryFs.use_percent).split(' ')[1]}
								style="stroke-dasharray: {RING_CIRCUMFERENCE}; stroke-dashoffset: {ringOffset};"
							/>
						</svg>
						<div class="absolute inset-0 flex items-center justify-center">
							<span class="text-2xl font-bold tabular-nums text-white">{primaryFs.use_percent}%</span>
						</div>
					</div>
					<div class="min-w-0 flex-1 text-center sm:text-left">
						<h3 class="text-xs font-semibold uppercase tracking-wider text-gray-400">
							{translate($language, 'dku.summary.primary')}
						</h3>
						<p class="mt-1 truncate font-mono text-lg font-semibold text-white" title={primaryFs.source}>
							{primaryFs.source}
							{#if primaryFs.fstype}<span class="font-sans text-sm font-normal text-gray-400">· {primaryFs.fstype}</span>{/if}
						</p>
						<p class="mt-0.5 text-sm text-gray-400">
							{translate($language, 'dku.fs.mount')} <span class="font-mono">{primaryFs.mounted_on}</span>
						</p>
						<p class="mt-3 text-sm text-gray-300">
							<span class="font-semibold text-white">{formatBytes(primaryFs.used_bytes)}</span>
							{translate($language, 'dku.summary.usedOf')}
							{formatBytes(primaryFs.size_bytes)}
							<span class="text-gray-500">·</span>
							<span class="text-green-400">{formatBytes(primaryFs.avail_bytes)} {translate($language, 'dku.summary.free')}</span>
						</p>
					</div>
					<div class="grid w-full max-w-xs grid-cols-2 gap-2 sm:w-auto sm:max-w-none sm:shrink-0">
						<div class="rounded-lg border border-gray-700 bg-gray-900 px-3 py-2">
							<p class="text-[11px] uppercase tracking-wider text-gray-500">{translate($language, 'dku.filesystems')}</p>
							<p class="text-sm font-semibold tabular-nums text-white">{overview.filesystems.length}</p>
						</div>
						{#if overview.journal_bytes > 0}
							<div class="rounded-lg border border-gray-700 bg-gray-900 px-3 py-2">
								<p class="truncate text-[11px] uppercase tracking-wider text-gray-500">{translate($language, 'dku.journal')}</p>
								<p class="text-sm font-semibold text-white">{formatBytes(overview.journal_bytes)}</p>
							</div>
						{/if}
						{#if overview.websites.length > 0}
							<div class="rounded-lg border border-gray-700 bg-gray-900 px-3 py-2">
								<p class="truncate text-[11px] uppercase tracking-wider text-gray-500">{translate($language, 'dku.websites.site')}</p>
								<p class="text-sm font-semibold tabular-nums text-white">{overview.websites.length}</p>
							</div>
						{/if}
						{#if overview.old_kernels.length > 0}
							<div class="rounded-lg border border-yellow-700/50 bg-gray-900 px-3 py-2">
								<p class="truncate text-[11px] uppercase tracking-wider text-gray-500">{translate($language, 'dku.oldKernels')}</p>
								<p class="text-sm font-semibold tabular-nums text-yellow-400">{overview.old_kernels.length}</p>
							</div>
						{/if}
					</div>
				</div>
			</div>
		{/if}

		<!-- Filesystems -->
		<div class="rounded-lg border border-gray-700 bg-gray-800 p-5">
			<h3 class="mb-3 text-lg font-semibold text-white">{translate($language, 'dku.filesystems')}</h3>
			{#if sortedFilesystems.length === 0}
				<p class="py-4 text-center text-sm text-gray-400">{translate($language, 'dku.fs.empty')}</p>
			{:else}
				<div class="overflow-x-auto">
					<table class="w-full">
						<thead>
							<tr class="border-b border-gray-700">
								<th class="px-4 py-3 text-left text-xs font-medium uppercase tracking-wider text-gray-400">{translate($language, 'dku.fs.source')}</th>
								<th class="px-4 py-3 text-left text-xs font-medium uppercase tracking-wider text-gray-400">{translate($language, 'dku.fs.type')}</th>
								<th class="px-4 py-3 text-left text-xs font-medium uppercase tracking-wider text-gray-400">{translate($language, 'dku.fs.mount')}</th>
								<th class="px-4 py-3 text-right text-xs font-medium uppercase tracking-wider text-gray-400">{translate($language, 'dku.fs.size')}</th>
								<th class="px-4 py-3 text-right text-xs font-medium uppercase tracking-wider text-gray-400">{translate($language, 'dku.fs.used')}</th>
								<th class="px-4 py-3 text-right text-xs font-medium uppercase tracking-wider text-gray-400">{translate($language, 'dku.fs.avail')}</th>
								<th class="w-44 px-4 py-3 text-left text-xs font-medium uppercase tracking-wider text-gray-400">{translate($language, 'dku.fs.use')}</th>
							</tr>
						</thead>
						<tbody class="divide-y divide-gray-700">
							{#each sortedFilesystems as fs (fs.source + fs.mounted_on)}
								<tr class="hover:bg-gray-700/40">
									<td class="px-4 py-3 text-sm font-mono text-white">{fs.source}</td>
									<td class="px-4 py-3">
										<span class="rounded border border-gray-600 bg-gray-900 px-1.5 py-0.5 text-[11px] uppercase text-gray-400">{fs.fstype || '—'}</span>
									</td>
									<td class="px-4 py-3 text-sm font-mono {fs.mounted_on === '/' ? 'font-semibold text-blue-400' : 'text-gray-400'}">{fs.mounted_on}</td>
									<td class="px-4 py-3 text-right text-sm tabular-nums text-gray-400">{formatBytes(fs.size_bytes)}</td>
									<td class="px-4 py-3 text-right text-sm tabular-nums text-gray-400">{formatBytes(fs.used_bytes)}</td>
									<td class="px-4 py-3 text-right text-sm tabular-nums text-gray-400">{formatBytes(fs.avail_bytes)}</td>
									<td class="px-4 py-3">
										<div class="flex items-center gap-2">
											<div class="h-2 flex-1 overflow-hidden rounded-full bg-gray-700">
												<div
													class="h-full rounded-full {severityClass(fs.use_percent).split(' ')[0]}"
													style="width: {Math.min(fs.use_percent, 100)}%"
												></div>
											</div>
											<span class="w-10 text-right text-xs tabular-nums text-gray-400">{fs.use_percent}%</span>
										</div>
									</td>
								</tr>
							{/each}
						</tbody>
					</table>
				</div>
			{/if}
		</div>

		<!-- Space by location -->
		<div class="rounded-lg border border-gray-700 bg-gray-800 p-5">
			<h3 class="mb-1 text-lg font-semibold text-white">{translate($language, 'dku.dirs')}</h3>
			<p class="mb-4 text-xs text-gray-500">{translate($language, 'dku.dirs.desc')}</p>
			<div class="divide-y divide-gray-700/60">
				{#each dirRows as row (row.key)}
					{@render usageBarRow(row.label, row.path, row.bytes, maxDirBytes, row.missing)}
				{/each}
			</div>
		</div>

		{#if overview.websites.length > 0}
			<div class="rounded-lg border border-gray-700 bg-gray-800 p-5">
				<h3 class="mb-1 text-lg font-semibold text-white">{translate($language, 'dku.websites.title')}</h3>
				<p class="mb-4 text-xs text-gray-500">{translate($language, 'dku.websites.desc')} {translate($language, 'dku.websites.analyzeHint')}</p>
				<div class="overflow-x-auto">
					<table class="w-full">
						<thead>
							<tr class="border-b border-gray-700">
								<th class="px-4 py-3 text-left text-xs font-medium uppercase tracking-wider text-gray-400">{translate($language, 'dku.websites.site')}</th>
								<th class="px-4 py-3 text-left text-xs font-medium uppercase tracking-wider text-gray-400">{translate($language, 'dku.websites.path')}</th>
								<th class="px-4 py-3 text-right text-xs font-medium uppercase tracking-wider text-gray-400">{translate($language, 'dku.websites.size')}</th>
								<th class="w-48 px-4 py-3 text-left text-xs font-medium uppercase tracking-wider text-gray-400">{translate($language, 'dku.fs.use')}</th>
							</tr>
						</thead>
						<tbody class="divide-y divide-gray-700">
							{#each overview.websites as site (site.id)}
								{@const maxSiteBytes = overview.websites[0]?.bytes || 1}
								<tr
									class="cursor-pointer hover:bg-gray-700/40 {expandedSiteId === site.id ? 'bg-gray-700/40' : ''}"
									onclick={() => toggleSite(site)}
									aria-expanded={expandedSiteId === site.id}
								>
									<td class="px-4 py-3 text-sm">
										<span class="inline-flex items-center gap-2 text-white">
											<svg
												class="h-3.5 w-3.5 text-gray-500 transition-transform {expandedSiteId === site.id ? 'rotate-90' : ''}"
												fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="2"
											>
												<path stroke-linecap="round" stroke-linejoin="round" d="M9 5l7 7-7 7" />
											</svg>
											{site.domain}
										</span>
									</td>
									<td class="px-4 py-3 text-sm font-mono text-gray-400">{site.path}</td>
									<td class="px-4 py-3 text-right text-sm tabular-nums text-gray-400">{formatBytes(site.bytes)}</td>
									<td class="px-4 py-3">
										<div class="h-2 overflow-hidden rounded-full bg-gray-700">
											<div class="h-full rounded-full bg-blue-500" style="width: {shareOf(site.bytes, maxSiteBytes)}%"></div>
										</div>
									</td>
								</tr>
								{#if expandedSiteId === site.id}
									<tr>
										<td colspan="4" class="bg-gray-900/40 px-4 pb-4">
											<div class="rounded-lg border border-gray-700 bg-gray-900 p-3">
												{#if expandedState === 'loading'}
													<div class="py-2 text-sm text-gray-400">{translate($language, 'dku.breakdown.loading')}</div>
												{:else if expandedState === 'error'}
													<div class="flex items-center justify-between gap-2 py-2">
														<span class="text-sm text-red-400">{translate($language, 'dku.breakdown.loadFailed')}</span>
														<button
															onclick={() => retryBreakdown(site)}
															class="cursor-pointer rounded bg-gray-700 px-2.5 py-1 text-xs text-gray-200 transition-colors hover:bg-gray-600"
														>
															{translate($language, 'dku.breakdown.retry')}
														</button>
													</div>
												{:else if expandedBreakdown}
													<div class="space-y-1.5">
														{#each expandedBreakdown.entries as entry (entry.path)}
															{@const maxEntryBytes = expandedBreakdown.entries[0]?.bytes || 1}
															<div class="flex items-center gap-3">
																<code class="w-40 shrink-0 truncate font-mono text-xs text-gray-300" title={entry.path}>{folderName(entry.path, expandedBreakdown.home)}</code>
																<div class="h-1.5 flex-1 overflow-hidden rounded-full bg-gray-700">
																	<div class="h-full rounded-full bg-blue-500" style="width: {shareOf(entry.bytes, maxEntryBytes)}%"></div>
																</div>
																<span class="w-20 shrink-0 text-right text-xs text-gray-400">{formatBytes(entry.bytes)}</span>
															</div>
														{/each}
														<div class="mt-2 flex items-center justify-between border-t border-gray-700 pt-2 text-xs">
															<span class="text-gray-500">{translate($language, 'dku.breakdown.total')}</span>
															<span class="text-gray-300">{formatBytes(expandedBreakdown.total_bytes)}</span>
														</div>
													</div>
												{/if}
											</div>
										</td>
									</tr>
								{/if}
							{/each}
						</tbody>
					</table>
				</div>
			</div>
		{/if}

		{#if overview.root_dirs.length > 0}
			<div class="rounded-lg border border-gray-700 bg-gray-800 p-5">
				<h3 class="mb-1 text-lg font-semibold text-white">{translate($language, 'dku.root.title')}</h3>
				<p class="mb-4 text-xs text-gray-500">{translate($language, 'dku.root.desc')}</p>
				<div class="divide-y divide-gray-700/60">
					{#each overview.root_dirs as dir (dir.path)}
						{@render usageBarRow(dir.path, '', dir.bytes, maxRootBytes, false)}
					{/each}
				</div>
			</div>
		{/if}

		{#if overview.old_kernels.length > 0}
			<div class="rounded-lg border border-gray-700 bg-gray-800 p-5">
				<h3 class="mb-1 text-lg font-semibold text-white">{translate($language, 'dku.oldKernels')}</h3>
				<p class="mb-3 text-xs text-gray-500">{translate($language, 'dku.oldKernels.desc')}</p>
				<p class="mb-2 text-xs text-gray-400">{translate($language, 'dku.kernel.running')}: <code class="font-mono text-gray-300">{overview.kernel}</code></p>
				<div class="flex flex-wrap gap-2">
					{#each overview.old_kernels as pkg (pkg)}
						<code class="rounded border border-gray-700 bg-gray-900 px-2 py-1 font-mono text-xs text-gray-400">{pkg}</code>
					{/each}
				</div>
			</div>
		{/if}

		{#if overview.docker.length > 0}
			<div class="rounded-lg border border-gray-700 bg-gray-800 p-5">
				<h3 class="mb-3 text-lg font-semibold text-white">{translate($language, 'dku.docker')}</h3>
				<div class="overflow-x-auto">
					<table class="w-full">
						<thead>
							<tr class="border-b border-gray-700">
								<th class="px-4 py-3 text-left text-xs font-medium uppercase tracking-wider text-gray-400">{translate($language, 'dku.docker.type')}</th>
								<th class="px-4 py-3 text-right text-xs font-medium uppercase tracking-wider text-gray-400">{translate($language, 'dku.docker.size')}</th>
							</tr>
						</thead>
						<tbody class="divide-y divide-gray-700">
							{#each overview.docker as row (row.type)}
								<tr class="hover:bg-gray-700/40">
									<td class="px-4 py-3 text-sm text-white">{row.type}</td>
									<td class="px-4 py-3 text-right text-sm tabular-nums text-gray-400">{row.size}</td>
								</tr>
							{/each}
						</tbody>
					</table>
				</div>
			</div>
		{/if}

		<!-- Cleanup -->
		<div class="rounded-lg border border-gray-700 bg-gray-800 p-5">
			<h3 class="mb-1 text-lg font-semibold text-white">{translate($language, 'dku.cleanup.title')}</h3>
			<p class="mb-4 text-xs text-gray-500">{translate($language, 'dku.cleanup.desc')}</p>

			{#if startError}
				<div class="mb-3 rounded-lg border border-red-700 bg-red-900/50 p-3 text-sm text-red-300">{startError}</div>
			{/if}

			<div class="grid gap-2 md:grid-cols-2">
				<label class="flex cursor-pointer items-start gap-3 rounded-xl border p-3.5 transition-colors {options.apt_clean ? 'border-blue-500/60 bg-blue-500/5' : 'border-gray-700 hover:border-gray-600'}">
					<input type="checkbox" bind:checked={options.apt_clean} class="mt-0.5 h-4 w-4 accent-blue-500" />
					<span>
						<span class="block text-sm font-medium text-white">{translate($language, 'dku.cleanup.aptClean')}</span>
						<span class="mt-0.5 block text-xs text-gray-500">{translate($language, 'dku.cleanup.aptClean.desc')}</span>
					</span>
				</label>
				<label class="flex cursor-pointer items-start gap-3 rounded-xl border p-3.5 transition-colors {options.apt_autoremove ? 'border-blue-500/60 bg-blue-500/5' : 'border-gray-700 hover:border-gray-600'}">
					<input type="checkbox" bind:checked={options.apt_autoremove} class="mt-0.5 h-4 w-4 accent-blue-500" />
					<span>
						<span class="block text-sm font-medium text-white">{translate($language, 'dku.cleanup.aptAutoremove')}</span>
						<span class="mt-0.5 block text-xs text-gray-500">{translate($language, 'dku.cleanup.aptAutoremove.desc')}</span>
					</span>
				</label>
				<label class="flex cursor-pointer items-start gap-3 rounded-xl border p-3.5 transition-colors {options.docker_prune ? 'border-blue-500/60 bg-blue-500/5' : 'border-gray-700 hover:border-gray-600'}">
					<input type="checkbox" bind:checked={options.docker_prune} class="mt-0.5 h-4 w-4 accent-blue-500" />
					<span>
						<span class="block text-sm font-medium text-white">{translate($language, 'dku.cleanup.dockerPrune')}</span>
						<span class="mt-0.5 block text-xs text-gray-500">{translate($language, 'dku.cleanup.dockerPrune.desc')}</span>
					</span>
				</label>
				<div class="flex flex-wrap items-end gap-3 rounded-xl border border-gray-700 p-3.5">
					<div>
						<label class="mb-1 block text-[11px] font-medium uppercase tracking-wider text-gray-400" for="dku-journal">
							{translate($language, 'dku.cleanup.journalVacuum')}
						</label>
						<input
							id="dku-journal"
							type="number"
							min="0"
							max="1024"
							step="50"
							bind:value={options.journal_vacuum_mb}
							class="w-32 rounded-lg border border-gray-600 bg-gray-700 px-3 py-2 text-sm text-white focus:outline-none focus:ring-2 focus:ring-blue-500"
						/>
					</div>
					<div>
						<label class="mb-1 block text-[11px] font-medium uppercase tracking-wider text-gray-400" for="dku-tmp">
							{translate($language, 'dku.cleanup.tmpClean')}
						</label>
						<input
							id="dku-tmp"
							type="number"
							min="0"
							max="365"
							bind:value={options.tmp_clean_days}
							class="w-32 rounded-lg border border-gray-600 bg-gray-700 px-3 py-2 text-sm text-white focus:outline-none focus:ring-2 focus:ring-blue-500"
						/>
					</div>
				</div>
			</div>

			<div class="mt-4 flex flex-wrap items-center justify-between gap-3">
				<span class="text-sm text-gray-400">
					{#if plan.length > 0}
						<span class="font-semibold text-white">{plan.length}</span>
						{translate($language, 'dku.cleanup.actions')}
					{/if}
				</span>
				<button
					onclick={runCleanup}
					disabled={starting || plan.length === 0}
					class="rounded-lg bg-red-600 px-4 py-2 text-sm font-medium text-white transition-colors hover:bg-red-700 disabled:cursor-not-allowed disabled:opacity-50"
				>
					{starting ? translate($language, 'dku.cleanup.running') : translate($language, 'dku.cleanup.run')}
				</button>
			</div>

			{#if plan.length > 0}
				<details class="mt-3 rounded-lg border border-gray-700 bg-gray-900">
					<summary class="cursor-pointer px-3 py-2 text-[11px] font-semibold uppercase tracking-wider text-gray-400">
						{translate($language, 'dku.cleanup.details')}
					</summary>
					<div class="border-t border-gray-700 px-3 py-2">
						<p class="mb-2 text-xs text-yellow-500/90">{translate($language, 'dku.cleanup.danger')}</p>
						{#each plan as action (action.name)}
							{#each action.commands as cmd, ci (action.name + '-' + ci)}
								<code class="block px-2 py-1 font-mono text-xs text-gray-300">
									{cmd.map((c) => (c.includes(' ') ? `"${c}"` : c)).join(' ')}
								</code>
							{/each}
						{/each}
					</div>
				</details>
			{/if}
		</div>
	{/if}
</div>
