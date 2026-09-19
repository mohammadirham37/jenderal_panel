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

	interface DockerUsage {
		type: string;
		size: string;
	}

	interface Overview {
		filesystems: FilesystemStat[];
		dirs: DirUsage[];
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
