<script lang="ts">
	import { onMount } from 'svelte';
	import { api } from '#lib/api.js';
	import { hasPermission, permissions } from '#lib/stores/auth.js';
	import { toast } from '#lib/stores/toast.js';
	import { language, translate } from '#lib/stores/language.js';

	// Composer/npm dependencies of every website checked against OSV.dev
	// (internal/depaudit).

	type Scan = {
		id: string;
		trigger: string;
		status: 'running' | 'completed' | 'failed';
		error: string;
		sites: number;
		packages: number;
		findings: number;
		started_at: string;
		finished_at: string;
	};
	type Finding = {
		website_id: string;
		domain: string;
		ecosystem: string;
		package: string;
		version: string;
		dev: boolean;
		lockfile: string;
		vuln_id: string;
		aliases: string[];
		severity: string;
		summary: string;
		fixed_version: string;
		url: string;
	};
	type Report = { scan: Scan | null; completed: Scan | null; findings: Finding[] };

	let report = $state<Report | null>(null);
	let loading = $state(true);
	let error = $state('');
	let starting = $state(false);
	let showDev = $state(true);

	const t = (key: string) => translate($language, key);
	const canScan = $derived(hasPermission($permissions, 'security.manage'));
	const running = $derived(report?.scan?.status === 'running');
	const visible = $derived((report?.findings ?? []).filter((f) => showDev || !f.dev));
	const severities = ['critical', 'high', 'medium', 'low', 'unknown'];
	const counts = $derived(
		Object.fromEntries(severities.map((s) => [s, visible.filter((f) => f.severity === s).length]))
	);
	const bySite = $derived.by(() => {
		const groups = new Map<string, Finding[]>();
		for (const f of visible) {
			const list = groups.get(f.domain) ?? [];
			list.push(f);
			groups.set(f.domain, list);
		}
		return [...groups.entries()];
	});

	const tone: Record<string, string> = {
		critical: 'bg-red-900 text-red-300 border-red-700',
		high: 'bg-orange-900 text-orange-300 border-orange-700',
		medium: 'bg-yellow-900 text-yellow-300 border-yellow-700',
		low: 'bg-gray-700 text-gray-300 border-gray-600',
		unknown: 'bg-gray-800 text-gray-400 border-gray-600'
	};

	async function load() {
		try {
			report = await api.get<Report>('/api/v1/security/dependencies');
			error = '';
		} catch (err) {
			error = err instanceof Error ? err.message : t('sec.deps.loadFailed');
		} finally {
			loading = false;
		}
	}

	async function scanNow() {
		starting = true;
		try {
			await api.post('/api/v1/security/dependencies/scan');
			toast.success(t('sec.deps.started'));
			await load();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : t('sec.deps.startFailed'));
		} finally {
			starting = false;
		}
	}

	function updateCommand(f: Finding) {
		return (f.ecosystem === 'Packagist' ? 'composer update ' : 'npm update ') + f.package;
	}

	function when(value?: string) {
		return value ? new Date(value).toLocaleString() : '—';
	}

	onMount(() => {
		void load();
	});

	$effect(() => {
		if (!running) return;
		const timer = setInterval(load, 3000);
		return () => clearInterval(timer);
	});
</script>

<section class="rounded-xl border border-gray-700 bg-gray-800 p-5">
	<div class="flex flex-wrap items-start justify-between gap-3">
		<div class="min-w-0">
			<h3 class="font-semibold text-white">{t('sec.deps.title')}</h3>
			<p class="mt-1 text-sm text-gray-400">{t('sec.deps.desc')}</p>
			<p class="mt-1 text-xs text-gray-500">{t('sec.deps.privacy')}</p>
		</div>
		{#if canScan}
			<button
				type="button"
				onclick={scanNow}
				disabled={starting || running}
				class="cursor-pointer rounded-lg bg-blue-600 px-4 py-2 text-sm font-medium text-white hover:bg-blue-700 disabled:cursor-not-allowed disabled:opacity-50"
			>
				{running ? t('sec.deps.scanning') : t('sec.deps.scanNow')}
			</button>
		{/if}
	</div>

	{#if loading}
		<p class="mt-4 text-sm text-gray-400">{t('sec.deps.loading')}</p>
	{:else if error}
		<p class="mt-4 text-sm text-red-400">{error}</p>
	{:else if report}
		<div class="mt-4 flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-gray-400">
			{#if running}
				<span class="text-blue-300">{t('sec.deps.running')}</span>
			{:else if report.scan?.status === 'failed'}
				<span class="text-red-400">{t('sec.deps.failed').replace('{error}', report.scan.error)}</span>
			{/if}
			{#if report.completed}
				<span>{t('sec.deps.lastScan').replace('{time}', when(report.completed.finished_at))}</span>
				<span>
					{t('sec.deps.scope')
						.replace('{sites}', String(report.completed.sites))
						.replace('{packages}', String(report.completed.packages))}
				</span>
			{/if}
			<label class="ml-auto flex cursor-pointer items-center gap-2">
				<input type="checkbox" bind:checked={showDev} />
				{t('sec.deps.showDev')}
			</label>
		</div>

		{#if !report.completed}
			<p class="mt-4 text-sm text-gray-400">{running ? t('sec.deps.firstScan') : t('sec.deps.never')}</p>
		{:else}
			<div class="mt-4 flex flex-wrap gap-2">
				{#each severities as s (s)}
					{#if counts[s] > 0 || s !== 'unknown'}
						<span class="rounded-full border px-3 py-1 text-xs {tone[s]}">
							{t('sec.deps.sev.' + s)}: {counts[s]}
						</span>
					{/if}
				{/each}
			</div>

			{#if visible.length === 0}
				<p class="mt-4 text-sm text-green-300">{t('sec.deps.none')}</p>
			{:else}
				<div class="mt-4 space-y-4">
					{#each bySite as [domain, findings] (domain)}
						<div class="rounded-lg border border-gray-700">
							<div class="border-b border-gray-700 px-4 py-2 text-sm font-medium text-white">{domain}</div>
							<ul class="divide-y divide-gray-700">
								{#each findings as f (f.package + f.version + f.vuln_id)}
									<li class="px-4 py-3">
										<div class="flex flex-wrap items-center gap-2">
											<span class="rounded border px-2 py-0.5 text-xs uppercase {tone[f.severity] ?? tone.unknown}">
												{t('sec.deps.sev.' + f.severity)}
											</span>
											<span class="font-mono text-sm text-gray-100">{f.package}</span>
											<span class="font-mono text-xs text-gray-400">{f.version}</span>
											{#if f.dev}
												<span class="rounded bg-gray-700 px-1.5 py-0.5 text-[10px] uppercase text-gray-300">dev</span>
											{/if}
											<span class="text-xs text-gray-500">{f.lockfile}</span>
										</div>
										{#if f.summary}
											<p class="mt-1 text-sm text-gray-300">{f.summary}</p>
										{/if}
										<div class="mt-1 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs">
											<a href={f.url} target="_blank" rel="noopener noreferrer" class="text-blue-300 hover:text-blue-200">
												{f.vuln_id}
											</a>
											{#each f.aliases as alias (alias)}
												<span class="text-gray-400">{alias}</span>
											{/each}
											{#if f.fixed_version}
												<span class="text-green-300">{t('sec.deps.fixedIn').replace('{version}', f.fixed_version)}</span>
												<code class="rounded bg-gray-950 px-1.5 py-0.5 font-mono text-gray-300">{updateCommand(f)}</code>
											{:else}
												<span class="text-yellow-300">{t('sec.deps.noFix')}</span>
											{/if}
										</div>
									</li>
								{/each}
							</ul>
						</div>
					{/each}
				</div>
			{/if}
		{/if}
	{/if}
</section>
