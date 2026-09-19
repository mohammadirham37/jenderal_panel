<script lang="ts">
	import { onMount } from 'svelte';
	import { api } from '$lib/api';
	import { language, translate } from '$lib/stores/language';
	import TaskProgress from '$lib/components/TaskProgress.svelte';

	interface FilesystemStat {
		source: string;
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

<div class="space-y-6">
	<div class="flex flex-wrap items-center justify-between gap-2">
		<div>
			<h2 class="text-2xl font-bold text-white">{translate($language, 'dku.title')}</h2>
			<p class="text-sm text-gray-500">{translate($language, 'dku.subtitle')}</p>
		</div>
		<button
			onclick={load}
			disabled={loading}
			class="px-4 py-2 bg-gray-700 hover:bg-gray-600 disabled:opacity-50 text-gray-200 text-sm font-medium rounded-lg transition-colors cursor-pointer"
		>
			{translate($language, 'dku.refresh')}
		</button>
	</div>

	{#if taskId}
		<div class="bg-gray-800 rounded-lg border border-gray-700 p-5">
			<h3 class="text-lg font-semibold text-white mb-3">{translate($language, 'dku.cleanup.plan')}</h3>
			<TaskProgress bind:taskId storageKey="jenderal_disk_cleanup" onComplete={load} />
		</div>
	{/if}

	{#if loading}
		<div class="bg-gray-800 rounded-lg border border-gray-700 p-8 text-center text-gray-400">
			{translate($language, 'dku.loading')}
		</div>
	{:else if error}
		<div class="p-4 bg-red-900/50 border border-red-700 rounded-lg text-red-300">{error}</div>
	{:else if overview}
		<!-- Filesystems -->
		<div class="bg-gray-800 rounded-lg border border-gray-700 p-5">
			<h3 class="text-lg font-semibold text-white mb-3">{translate($language, 'dku.filesystems')}</h3>
			<div class="overflow-x-auto">
				<table class="w-full">
					<thead>
						<tr class="border-b border-gray-700">
							<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">{translate($language, 'dku.fs.source')}</th>
							<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">{translate($language, 'dku.fs.mount')}</th>
							<th class="text-right px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">{translate($language, 'dku.fs.size')}</th>
							<th class="text-right px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">{translate($language, 'dku.fs.used')}</th>
							<th class="text-right px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">{translate($language, 'dku.fs.avail')}</th>
							<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium w-40">{translate($language, 'dku.fs.use')}</th>
						</tr>
					</thead>
					<tbody class="divide-y divide-gray-700">
						{#each overview.filesystems as fs (fs.source + fs.mounted_on)}
							<tr class="hover:bg-gray-750">
								<td class="px-4 py-3 text-sm text-white font-mono">{fs.source}</td>
								<td class="px-4 py-3 text-sm text-gray-400 font-mono">{fs.mounted_on}</td>
								<td class="px-4 py-3 text-sm text-gray-400 text-right">{formatBytes(fs.size_bytes)}</td>
								<td class="px-4 py-3 text-sm text-gray-400 text-right">{formatBytes(fs.used_bytes)}</td>
								<td class="px-4 py-3 text-sm text-gray-400 text-right">{formatBytes(fs.avail_bytes)}</td>
								<td class="px-4 py-3">
									<div class="flex items-center gap-2">
										<div class="flex-1 h-2 bg-gray-700 rounded-full overflow-hidden">
											<div
												class="h-full rounded-full {fs.use_percent >= 90 ? 'bg-red-500' : fs.use_percent >= 75 ? 'bg-yellow-500' : 'bg-blue-500'}"
												style="width: {Math.min(fs.use_percent, 100)}%"
											></div>
										</div>
										<span class="text-xs text-gray-400 w-10 text-right">{fs.use_percent}%</span>
									</div>
								</td>
							</tr>
						{/each}
					</tbody>
				</table>
			</div>
		</div>

		<!-- Space by location -->
		<div class="bg-gray-800 rounded-lg border border-gray-700 p-5">
			<h3 class="text-lg font-semibold text-white mb-1">{translate($language, 'dku.dirs')}</h3>
			<p class="text-xs text-gray-500 mb-4">{translate($language, 'dku.dirs.desc')}</p>
			<div class="overflow-x-auto">
				<table class="w-full">
					<thead>
						<tr class="border-b border-gray-700">
							<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">{translate($language, 'dku.dir.label')}</th>
							<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">{translate($language, 'dku.dir.path')}</th>
							<th class="text-right px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">{translate($language, 'dku.dir.size')}</th>
						</tr>
					</thead>
					<tbody class="divide-y divide-gray-700">
						{#each overview.dirs as dir (dir.path)}
							<tr class="hover:bg-gray-750">
								<td class="px-4 py-3 text-sm text-white">{translate($language, labelKey(dir.label))}</td>
								<td class="px-4 py-3 text-sm text-gray-400 font-mono">{dir.path}</td>
								<td class="px-4 py-3 text-sm text-gray-400 text-right">
									{dir.missing ? translate($language, 'dku.dir.missing') : formatBytes(dir.bytes)}
								</td>
							</tr>
						{/each}
						{#if overview.journal_bytes > 0}
							<tr class="hover:bg-gray-750">
								<td class="px-4 py-3 text-sm text-white">{translate($language, 'dku.journal')}</td>
								<td class="px-4 py-3 text-sm text-gray-400 font-mono">/var/log/journal</td>
								<td class="px-4 py-3 text-sm text-gray-400 text-right">{formatBytes(overview.journal_bytes)}</td>
							</tr>
						{/if}
					</tbody>
				</table>
			</div>
		</div>

		{#if overview.websites.length > 0}
			<div class="bg-gray-800 rounded-lg border border-gray-700 p-5">
				<h3 class="text-lg font-semibold text-white mb-1">{translate($language, 'dku.websites.title')}</h3>
				<p class="text-xs text-gray-500 mb-4">{translate($language, 'dku.websites.desc')} {translate($language, 'dku.websites.analyzeHint')}</p>
				<div class="overflow-x-auto">
					<table class="w-full">
						<thead>
							<tr class="border-b border-gray-700">
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">{translate($language, 'dku.websites.site')}</th>
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">{translate($language, 'dku.websites.path')}</th>
								<th class="text-right px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">{translate($language, 'dku.websites.size')}</th>
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium w-48">{translate($language, 'dku.fs.use')}</th>
							</tr>
						</thead>
					<tbody class="divide-y divide-gray-700">
						{#each overview.websites as site (site.id)}
							{@const maxSiteBytes = overview.websites[0]?.bytes || 1}
							<tr
								class="hover:bg-gray-750 cursor-pointer {expandedSiteId === site.id ? 'bg-gray-750' : ''}"
								onclick={() => toggleSite(site)}
								aria-expanded={expandedSiteId === site.id}
							>
								<td class="px-4 py-3 text-sm">
									<span class="inline-flex items-center gap-2 text-white">
										<svg
											class="w-3.5 h-3.5 text-gray-500 transition-transform {expandedSiteId === site.id ? 'rotate-90' : ''}"
											fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="2"
										>
											<path stroke-linecap="round" stroke-linejoin="round" d="M9 5l7 7-7 7" />
										</svg>
										{site.domain}
									</span>
								</td>
								<td class="px-4 py-3 text-sm text-gray-400 font-mono">{site.path}</td>
								<td class="px-4 py-3 text-sm text-gray-400 text-right">{formatBytes(site.bytes)}</td>
								<td class="px-4 py-3">
									<div class="h-2 bg-gray-700 rounded-full overflow-hidden">
										<div class="h-full bg-blue-500 rounded-full" style="width: {shareOf(site.bytes, maxSiteBytes)}%"></div>
									</div>
								</td>
							</tr>
							{#if expandedSiteId === site.id}
								<tr>
									<td colspan="4" class="px-4 pb-4 bg-gray-900/40">
										<div class="rounded-lg border border-gray-700 bg-gray-900 p-3">
											{#if expandedState === 'loading'}
												<div class="text-gray-400 text-sm py-2">{translate($language, 'dku.breakdown.loading')}</div>
											{:else if expandedState === 'error'}
												<div class="flex items-center justify-between gap-2 py-2">
													<span class="text-red-400 text-sm">{translate($language, 'dku.breakdown.loadFailed')}</span>
													<button
														onclick={() => retryBreakdown(site)}
														class="px-2.5 py-1 bg-gray-700 hover:bg-gray-600 text-gray-200 text-xs rounded transition-colors cursor-pointer"
													>
														{translate($language, 'dku.breakdown.retry')}
													</button>
												</div>
											{:else if expandedBreakdown}
												<div class="space-y-1.5">
													{#each expandedBreakdown.entries as entry (entry.path)}
														{@const maxEntryBytes = expandedBreakdown.entries[0]?.bytes || 1}
														<div class="flex items-center gap-3">
															<code class="w-40 shrink-0 text-xs text-gray-300 font-mono truncate" title={entry.path}>{folderName(entry.path, expandedBreakdown.home)}</code>
															<div class="flex-1 h-1.5 bg-gray-700 rounded-full overflow-hidden">
																<div class="h-full bg-blue-500 rounded-full" style="width: {shareOf(entry.bytes, maxEntryBytes)}%"></div>
															</div>
															<span class="w-20 shrink-0 text-right text-xs text-gray-400">{formatBytes(entry.bytes)}</span>
														</div>
													{/each}
													<div class="flex items-center justify-between pt-2 mt-2 border-t border-gray-700 text-xs">
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
			<div class="bg-gray-800 rounded-lg border border-gray-700 p-5">
				<h3 class="text-lg font-semibold text-white mb-1">{translate($language, 'dku.root.title')}</h3>
				<p class="text-xs text-gray-500 mb-4">{translate($language, 'dku.root.desc')}</p>
				<div class="overflow-x-auto">
					<table class="w-full">
						<thead>
							<tr class="border-b border-gray-700">
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">{translate($language, 'dku.root.path')}</th>
								<th class="text-right px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">{translate($language, 'dku.root.size')}</th>
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium w-48">{translate($language, 'dku.fs.use')}</th>
							</tr>
						</thead>
						<tbody class="divide-y divide-gray-700">
							{#each overview.root_dirs as dir (dir.path)}
								{@const maxRootBytes = overview.root_dirs[0]?.bytes || 1}
								<tr class="hover:bg-gray-750">
									<td class="px-4 py-3 text-sm text-white font-mono">{dir.path}</td>
									<td class="px-4 py-3 text-sm text-gray-400 text-right">{formatBytes(dir.bytes)}</td>
									<td class="px-4 py-3">
										<div class="h-2 bg-gray-700 rounded-full overflow-hidden">
											<div
												class="h-full rounded-full {dir.path === '/var' ? 'bg-yellow-500' : 'bg-blue-500'}"
												style="width: {shareOf(dir.bytes, maxRootBytes)}%"
											></div>
										</div>
									</td>
								</tr>
							{/each}
						</tbody>
					</table>
				</div>
			</div>
		{/if}

		{#if overview.old_kernels.length > 0}
			<div class="bg-gray-800 rounded-lg border border-gray-700 p-5">
				<h3 class="text-lg font-semibold text-white mb-1">{translate($language, 'dku.oldKernels')}</h3>
				<p class="text-xs text-gray-500 mb-3">{translate($language, 'dku.oldKernels.desc')}</p>
				<p class="text-xs text-gray-400 mb-2">{translate($language, 'dku.kernel.running')}: <code class="font-mono text-gray-300">{overview.kernel}</code></p>
				<div class="flex flex-wrap gap-2">
					{#each overview.old_kernels as pkg (pkg)}
						<code class="px-2 py-1 bg-gray-900 border border-gray-700 rounded text-xs text-gray-400 font-mono">{pkg}</code>
					{/each}
				</div>
			</div>
		{/if}

		{#if overview.docker.length > 0}
			<div class="bg-gray-800 rounded-lg border border-gray-700 p-5">
				<h3 class="text-lg font-semibold text-white mb-3">{translate($language, 'dku.docker')}</h3>
				<div class="overflow-x-auto">
					<table class="w-full">
						<thead>
							<tr class="border-b border-gray-700">
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">{translate($language, 'dku.docker.type')}</th>
								<th class="text-right px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">{translate($language, 'dku.docker.size')}</th>
							</tr>
						</thead>
						<tbody class="divide-y divide-gray-700">
							{#each overview.docker as row (row.type)}
								<tr class="hover:bg-gray-750">
									<td class="px-4 py-3 text-sm text-white">{row.type}</td>
									<td class="px-4 py-3 text-sm text-gray-400 text-right">{row.size}</td>
								</tr>
							{/each}
						</tbody>
					</table>
				</div>
			</div>
		{/if}

		<!-- Cleanup -->
		<div class="bg-gray-800 rounded-lg border border-gray-700 p-5">
			<h3 class="text-lg font-semibold text-white mb-1">{translate($language, 'dku.cleanup.title')}</h3>
			<p class="text-xs text-gray-500 mb-4">{translate($language, 'dku.cleanup.desc')}</p>

			{#if startError}
				<div class="mb-3 p-3 bg-red-900/50 border border-red-700 rounded-lg text-red-300 text-sm">{startError}</div>
			{/if}

			<div class="space-y-3">
				<div class="flex flex-wrap items-center gap-3">
					<label class="flex items-center gap-2 text-sm text-gray-300 cursor-pointer">
						<input type="checkbox" bind:checked={options.apt_clean} class="accent-blue-500 w-4 h-4" />
						{translate($language, 'dku.cleanup.aptClean')}
					</label>
					<label class="flex items-center gap-2 text-sm text-gray-300 cursor-pointer">
						<input type="checkbox" bind:checked={options.apt_autoremove} class="accent-blue-500 w-4 h-4" />
						{translate($language, 'dku.cleanup.aptAutoremove')}
					</label>
					<label class="flex items-center gap-2 text-sm text-gray-300 cursor-pointer">
						<input type="checkbox" bind:checked={options.docker_prune} class="accent-blue-500 w-4 h-4" />
						{translate($language, 'dku.cleanup.dockerPrune')}
					</label>
				</div>
				<div class="flex flex-wrap items-end gap-4">
					<div>
						<label class="block text-[11px] font-medium uppercase tracking-wider text-gray-400 mb-1" for="dku-journal">
							{translate($language, 'dku.cleanup.journalVacuum')} ({translate($language, 'dku.cleanup.journalHint')})
						</label>
						<input
							id="dku-journal"
							type="number"
							min="0"
							max="1024"
							bind:value={options.journal_vacuum_mb}
							class="w-32 px-3 py-2 bg-gray-700 border border-gray-600 rounded-lg text-white text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
						/>
					</div>
					<div>
						<label class="block text-[11px] font-medium uppercase tracking-wider text-gray-400 mb-1" for="dku-tmp">
							{translate($language, 'dku.cleanup.tmpClean')} ({translate($language, 'dku.cleanup.tmpHint')})
						</label>
						<input
							id="dku-tmp"
							type="number"
							min="0"
							max="365"
							bind:value={options.tmp_clean_days}
							class="w-32 px-3 py-2 bg-gray-700 border border-gray-600 rounded-lg text-white text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
						/>
					</div>
					<button
						onclick={runCleanup}
						disabled={starting || plan.length === 0}
						class="px-4 py-2 bg-red-600 hover:bg-red-700 disabled:opacity-50 text-white text-sm font-medium rounded-lg transition-colors cursor-pointer"
					>
						{starting ? translate($language, 'dku.cleanup.running') : translate($language, 'dku.cleanup.run')}
					</button>
				</div>
			</div>

			{#if plan.length > 0}
				<div class="mt-4 rounded-lg border border-gray-700 bg-gray-900 p-3">
					<p class="mb-2 text-[11px] font-semibold uppercase tracking-wider text-gray-400">{translate($language, 'dku.cleanup.preview')}</p>
					{#each plan as action (action.name)}
						{#each action.commands as cmd, ci (action.name + '-' + ci)}
							<code class="block px-2 py-1 text-xs text-gray-300 font-mono">
								{cmd.map((c) => (c.includes(' ') ? `"${c}"` : c)).join(' ')}
							</code>
						{/each}
					{/each}
				</div>
			{/if}
		</div>
	{/if}
</div>
