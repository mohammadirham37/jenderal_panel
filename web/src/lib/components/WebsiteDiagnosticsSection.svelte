<script lang="ts">
	import { api } from '$lib/api';
	import { toast } from '$lib/stores/toast';
	import { language, translate } from '$lib/stores/language';

	let {
		websiteId,
		canManage
	}: {
		websiteId: string;
		canManage: boolean;
	} = $props();

	interface DiagnosticCheck {
		id: string;
		status: 'ok' | 'warn' | 'fail';
		detail: string;
		hint?: string;
	}

	interface DiagnosticsReport {
		website_id: string;
		overall: 'ok' | 'warn' | 'fail';
		checks: DiagnosticCheck[];
		error_log?: string;
	}

	let report = $state<DiagnosticsReport | null>(null);
	let running = $state(false);
	let repairing = $state(false);

	// Check IDs come from the backend; labels live in the dictionary.
	const checkLabelKeys: Record<string, string> = {
		vhost: 'wd.diag.check.vhost',
		nginx_config: 'wd.diag.check.nginx_config',
		docroot: 'wd.diag.check.docroot',
		nginx_access: 'wd.diag.check.nginx_access',
		http_response: 'wd.diag.check.http_response',
		php_fpm: 'wd.diag.check.php_fpm',
		laravel_project: 'wd.diag.check.laravel_project'
	};

	const needsPermissionRepair = $derived(
		!!report?.checks.some((c) => c.id === 'nginx_access' && c.status !== 'ok')
	);

	function statusIcon(status: string): string {
		return status === 'ok' ? '✓' : status === 'warn' ? '!' : '✗';
	}

	function statusColor(status: string): string {
		return status === 'ok'
			? 'text-green-400'
			: status === 'warn'
				? 'text-yellow-400'
				: 'text-red-400';
	}

	async function runDiagnose() {
		if (running) return;
		running = true;
		try {
			report = await api.post<DiagnosticsReport>(`/api/v1/websites/${websiteId}/diagnose`, {});
		} catch (err) {
			toast.error(err instanceof Error ? err.message : translate($language, 'wd.diag.failed'));
		} finally {
			running = false;
		}
	}

	async function repairServing() {
		if (repairing) return;
		repairing = true;
		try {
			await api.post(`/api/v1/websites/${websiteId}/repair-serving`, {});
			toast.success(translate($language, 'wd.diag.repair_done'));
			await runDiagnose();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : translate($language, 'wd.diag.repair_failed'));
		} finally {
			repairing = false;
		}
	}
</script>

<div class="bg-gray-800 rounded-lg border border-gray-700 p-5">
	<div class="flex flex-wrap items-center justify-between gap-2">
		<h3 class="text-sm font-semibold text-white">{translate($language, 'wd.diag.title')}</h3>
		<button
			type="button"
			onclick={runDiagnose}
			disabled={running}
			class="rounded-lg border border-gray-600 bg-gray-700 px-3 py-1.5 text-xs font-semibold text-gray-200 transition hover:bg-gray-600 disabled:opacity-50"
		>
			{running ? translate($language, 'wd.diag.running') : translate($language, 'wd.diag.run')}
		</button>
	</div>
	<p class="mt-1 text-xs text-gray-500">{translate($language, 'wd.diag.desc')}</p>

	{#if report}
		<p class="mt-3 text-xs font-semibold {statusColor(report.overall)}">
			{translate($language, report.overall === 'ok'
				? 'wd.diag.overall.ok'
				: report.overall === 'warn'
					? 'wd.diag.overall.warn'
					: 'wd.diag.overall.fail')}
		</p>
		<ul class="mt-2 space-y-2">
			{#each report.checks as check (check.id)}
				<li class="flex flex-wrap items-start gap-2 text-xs">
					<span class="font-bold {statusColor(check.status)}" aria-hidden="true">{statusIcon(check.status)}</span>
					<span class="text-gray-300">{translate($language, checkLabelKeys[check.id] ?? check.id)}</span>
					{#if check.detail}
						<span class="text-gray-500 font-mono break-all">{check.detail}</span>
					{/if}
					{#if check.status !== 'ok' && check.hint}
						<span class="text-gray-400">{translate($language, check.hint)}</span>
					{/if}
				</li>
			{/each}
		</ul>

		{#if needsPermissionRepair && canManage}
			<button
				type="button"
				onclick={repairServing}
				disabled={repairing}
				class="mt-3 rounded-lg bg-blue-600 px-3 py-1.5 text-xs font-semibold text-white transition hover:bg-blue-700 disabled:cursor-not-allowed disabled:opacity-40"
			>
				{repairing ? translate($language, 'wd.diag.repairing') : translate($language, 'wd.diag.repair')}
			</button>
		{/if}

		{#if report.error_log}
			<div class="mt-3">
				<p class="text-[11px] text-gray-500 uppercase tracking-wider">{translate($language, 'wd.diag.errorlog')}</p>
				<pre class="mt-1 max-h-40 overflow-auto rounded-lg bg-gray-900 p-2 text-[11px] text-gray-400 whitespace-pre-wrap">{report.error_log}</pre>
			</div>
		{/if}
	{/if}
</div>
