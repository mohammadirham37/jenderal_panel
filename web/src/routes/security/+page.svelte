<script lang="ts">
	import { onMount } from 'svelte';
	import { api } from '$lib/api';
	import TaskProgress from '$lib/components/TaskProgress.svelte';
	import {
		buildSafeFail2banSettings,
		conditionTone,
		formatBanExpiry,
		normalizeOverview,
		validateBan
	} from '$lib/security.js';

	type Tab = 'overview' | 'fail2ban' | 'events';
	interface ComponentStatus {
		name: string;
		state: string;
		version?: string;
		message?: string;
		installed: boolean;
		enabled: boolean;
		healthy: boolean;
	}
	interface Overview {
		condition: string;
		reasons: string[];
		components: ComponentStatus[];
		open_events: number;
		setup_complete: boolean;
		active_tasks: unknown[];
	}
	interface Jail {
		name: string;
		currently_failed: number;
		total_failed: number;
		currently_banned: number;
		total_banned: number;
		banned_ips: string[];
		filter_available: boolean;
		source_available: boolean;
	}
	interface Fail2banStatus {
		installed: boolean;
		running: boolean;
		enabled: boolean;
		healthy: boolean;
		state: string;
		version?: string;
		message?: string;
		ssh_port?: number;
		jails: Jail[];
		settings?: Fail2banSettings;
	}
	interface Ban {
		id?: string;
		jail: string;
		ip: string;
		reason?: string;
		expires_at?: string;
		manual: boolean;
	}
	interface SecurityEvent {
		id: string;
		category: string;
		severity: string;
		component: string;
		resource: string;
		recommended_action: string;
		status: string;
		occurrence_count: number;
		last_seen: string;
	}
	interface Fail2banSettings {
		sshd_enabled: boolean;
		max_retry: number;
		find_time_seconds: number;
		ban_time_seconds: number;
		ignore_ips: string[];
	}

	let activeTab = $state<Tab>('overview');
	let editMode = $state<'simple' | 'advanced'>('simple');
	let loading = $state(true);
	let error = $state('');
	let actionMessage = $state('');
	let actionError = $state('');
	let busy = $state('');
	let currentTaskId = $state('');
	let overview = $state<Overview>(normalizeOverview({}) as Overview);
	let fail2ban = $state<Fail2banStatus>({
		installed: false,
		running: false,
		enabled: false,
		healthy: false,
		state: 'not_installed',
		jails: []
	});
	let bans = $state<Ban[]>([]);
	let events = $state<SecurityEvent[]>([]);
	let settings = $state<Fail2banSettings>(buildSafeFail2banSettings([]));
	let managementNetworks = $state('');
	let banJail = $state('');
	let banIP = $state('');
	let banDuration = $state(300);

	async function loadData() {
		loading = true;
		error = '';
		try {
			const [overviewData, statusData, banData, eventData] = await Promise.all([
				api.get<Overview>('/api/v1/security/overview'),
				api.get<Fail2banStatus>('/api/v1/security/fail2ban'),
				api.get<Ban[]>('/api/v1/security/fail2ban/bans'),
				api.get<SecurityEvent[]>('/api/v1/security/events?per_page=100')
			]);
			overview = normalizeOverview(overviewData) as Overview;
			fail2ban = { ...statusData, jails: statusData?.jails || [] };
			if (statusData?.settings) {
				settings = { ...statusData.settings, ignore_ips: statusData.settings.ignore_ips || [] };
				managementNetworks = settings.ignore_ips
					.filter((value) => value !== '127.0.0.1/8' && value !== '::1/128')
					.join('\n');
			}
			bans = banData || [];
			events = eventData || [];
			if (!banJail && fail2ban.jails.length > 0) banJail = fail2ban.jails[0].name;
		} catch (err) {
			error = err instanceof Error ? err.message : 'Unable to load Security Center.';
		} finally {
			loading = false;
		}
	}

	async function installFail2ban() {
		busy = 'install';
		actionError = '';
		actionMessage = '';
		try {
			const result = await api.post<{ task_id: string }>('/api/v1/security/fail2ban/install', {});
			currentTaskId = result.task_id;
			actionMessage = 'Fail2ban installation started. Progress is saved if this page is refreshed.';
		} catch (err) {
			actionError = err instanceof Error ? err.message : 'Unable to start installation.';
		} finally {
			busy = '';
		}
	}

	async function applySettings() {
		busy = 'apply';
		actionError = '';
		actionMessage = '';
		try {
			const networks = managementNetworks.split(/[\s,]+/).map((value) => value.trim()).filter(Boolean);
			settings.ignore_ips = networks;
			const result = await api.put<{ task_id: string }>('/api/v1/security/fail2ban/settings', {
				...settings,
				max_retry: Number(settings.max_retry),
				find_time_seconds: Number(settings.find_time_seconds),
				ban_time_seconds: Number(settings.ban_time_seconds)
			});
			currentTaskId = result.task_id;
			actionMessage = 'Fail2ban settings are being validated and applied.';
		} catch (err) {
			actionError = err instanceof Error ? err.message : 'Unable to apply Fail2ban settings.';
		} finally {
			busy = '';
		}
	}

	async function serviceAction(action: 'start' | 'stop' | 'restart') {
		busy = action;
		actionError = '';
		try {
			await api.post(`/api/v1/security/fail2ban/${action}`, {});
			actionMessage = `Fail2ban ${action} request completed.`;
			await loadData();
		} catch (err) {
			actionError = err instanceof Error ? err.message : `Unable to ${action} Fail2ban.`;
		} finally {
			busy = '';
		}
	}

	async function createBan() {
		busy = 'ban';
		actionError = '';
		try {
			const request = validateBan({ jail: banJail, ip: banIP, duration_seconds: Number(banDuration) });
			await api.post('/api/v1/security/fail2ban/bans', request);
			actionMessage = `${banIP} was temporarily banned.`;
			banIP = '';
			await loadData();
		} catch (err) {
			actionError = err instanceof Error ? err.message : 'Unable to create temporary ban.';
		} finally {
			busy = '';
		}
	}

	async function removeBan(ban: Ban) {
		if (!confirm(`Unban ${ban.ip} from ${ban.jail}?`)) return;
		busy = `unban:${ban.jail}:${ban.ip}`;
		actionError = '';
		try {
			await api.del(`/api/v1/security/fail2ban/bans/${encodeURIComponent(ban.ip)}?jail=${encodeURIComponent(ban.jail)}`);
			actionMessage = `${ban.ip} was unbanned.`;
			await loadData();
		} catch (err) {
			actionError = err instanceof Error ? err.message : 'Unable to remove ban.';
		} finally {
			busy = '';
		}
	}

	async function transitionEvent(event: SecurityEvent, status: 'acknowledged' | 'resolved' | 'false_positive') {
		busy = `event:${event.id}`;
		actionError = '';
		try {
			await api.post(`/api/v1/security/events/${event.id}/transition`, { status });
			actionMessage = `Security event marked ${status.replace('_', ' ')}.`;
			await loadData();
		} catch (err) {
			actionError = err instanceof Error ? err.message : 'Unable to update event.';
		} finally {
			busy = '';
		}
	}

	function taskComplete(task: { status?: string; error?: string }) {
		if (task.status === 'completed') {
			actionMessage = 'Security operation completed successfully.';
			void loadData();
		} else {
			actionError = task.error || 'Security operation failed. Review the task output and retry.';
		}
	}

	onMount(() => void loadData());
</script>

<svelte:head><title>Security Center · Jenderal Panel</title></svelte:head>

<div class="space-y-6">
	<div class="flex flex-col gap-3 sm:flex-row sm:items-end sm:justify-between">
		<div>
			<p class="text-xs font-semibold uppercase tracking-[0.18em] text-blue-400">Server protection</p>
			<h2 class="mt-1 text-2xl font-bold text-white">Security Center</h2>
			<p class="mt-1 max-w-2xl text-sm text-gray-400">Detect risks, manage temporary protections, and keep recovery steps visible.</p>
		</div>
		<button onclick={loadData} disabled={loading} class="rounded-lg border border-gray-600 px-3 py-2 text-sm text-gray-300 hover:bg-gray-800 disabled:opacity-50">Refresh status</button>
	</div>

	<div class="flex gap-1 overflow-x-auto rounded-xl border border-gray-700 bg-gray-900 p-1">
		{#each ['overview', 'fail2ban', 'events'] as tab}
			<button onclick={() => (activeTab = tab as Tab)} class="min-w-28 rounded-lg px-4 py-2 text-sm font-medium capitalize transition {activeTab === tab ? 'bg-blue-600 text-white' : 'text-gray-400 hover:bg-gray-800 hover:text-white'}">{tab}</button>
		{/each}
	</div>

	{#if actionMessage}<div class="rounded-lg border border-green-700 bg-green-900/50 p-3 text-sm text-green-300">{actionMessage}</div>{/if}
	{#if actionError}<div class="rounded-lg border border-red-700 bg-red-900/50 p-3 text-sm text-red-300">{actionError}</div>{/if}

	<TaskProgress bind:taskId={currentTaskId} storageKey="jenderal_security_fail2ban_task" onComplete={taskComplete} />

	{#if loading}
		<div class="rounded-xl border border-gray-700 bg-gray-800 p-8 text-center text-sm text-gray-400">Checking security components…</div>
	{:else if error}
		<div class="rounded-xl border border-red-700 bg-red-900/40 p-6">
			<p class="font-medium text-red-300">Security status could not be loaded.</p>
			<p class="mt-1 text-sm text-red-300">{error}</p>
			<button onclick={loadData} class="mt-4 rounded-lg bg-red-600 px-3 py-2 text-sm text-white hover:bg-red-700">Retry</button>
		</div>
	{:else if activeTab === 'overview'}
		{@const tone = conditionTone(overview.condition)}
		<div class="grid gap-4 lg:grid-cols-[1.1fr_1.9fr]">
			<section class="rounded-xl border p-5 {tone === 'critical' ? 'border-red-700 bg-red-900/30' : tone === 'warning' ? 'border-yellow-700 bg-yellow-900/25' : 'border-green-700 bg-green-900/20'}">
				<p class="text-xs font-semibold uppercase tracking-wider text-gray-400">Current condition</p>
				<h3 class="mt-2 text-2xl font-semibold text-white">{overview.condition === 'needs_attention' ? 'Needs Attention' : overview.condition === 'good' ? 'Good' : overview.condition === 'critical' ? 'Critical' : 'Unknown'}</h3>
				<ul class="mt-4 space-y-2 text-sm text-gray-300">
					{#each overview.reasons as reason}<li class="flex gap-2"><span aria-hidden="true">•</span><span>{reason}</span></li>{/each}
				</ul>
				<p class="mt-4 text-xs text-gray-400">This status summarizes observed signals; it is not a security guarantee.</p>
			</section>
			<section class="rounded-xl border border-gray-700 bg-gray-800 p-5">
				<div class="flex items-center justify-between"><h3 class="font-semibold text-white">Components</h3><span class="text-sm text-gray-400">{overview.open_events} active events</span></div>
				{#if overview.components.length === 0}<p class="mt-4 text-sm text-gray-400">No security component is configured yet. Open Fail2ban to begin with the Safe preset.</p>{:else}
					<div class="mt-4 grid gap-3 sm:grid-cols-2">
						{#each overview.components as component}
							<div class="rounded-lg border border-gray-700 bg-gray-900/60 p-4">
								<div class="flex items-center justify-between gap-2"><span class="font-medium capitalize text-white">{component.name}</span><span class="rounded-full px-2 py-0.5 text-xs {component.healthy ? 'bg-green-900 text-green-300' : component.installed ? 'bg-yellow-900 text-yellow-300' : 'bg-gray-700 text-gray-300'}">{component.state.replaceAll('_', ' ')}</span></div>
								<p class="mt-2 text-sm text-gray-400">{component.message || (component.installed ? 'Status checked.' : 'Optional component is not installed.')}</p>
							</div>
						{/each}
					</div>
				{/if}
			</section>
		</div>
	{:else if activeTab === 'fail2ban'}
		<div class="space-y-4">
			<section class="rounded-xl border border-gray-700 bg-gray-800 p-5">
				<div class="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
					<div><div class="flex items-center gap-2"><h3 class="text-lg font-semibold text-white">Fail2ban</h3><span class="rounded-full px-2 py-0.5 text-xs {fail2ban.healthy ? 'bg-green-900 text-green-300' : 'bg-gray-700 text-gray-300'}">{fail2ban.state.replaceAll('_', ' ')}</span></div><p class="mt-1 text-sm text-gray-400">{fail2ban.message || 'Temporary intrusion bans for SSH and supported Nginx logs.'}</p></div>
					{#if !fail2ban.installed}<button onclick={installFail2ban} disabled={busy !== '' || !!currentTaskId} class="rounded-lg bg-blue-600 px-4 py-2 text-sm font-medium text-white hover:bg-blue-700 disabled:opacity-50">{busy === 'install' ? 'Starting…' : 'Install Fail2ban'}</button>{:else}<div class="flex flex-wrap gap-2"><button onclick={() => serviceAction(fail2ban.running ? 'restart' : 'start')} disabled={busy !== ''} class="rounded-lg bg-blue-600 px-3 py-2 text-sm text-white disabled:opacity-50">{fail2ban.running ? 'Restart' : 'Start'}</button>{#if fail2ban.running}<button onclick={() => serviceAction('stop')} disabled={busy !== ''} class="rounded-lg border border-red-700 px-3 py-2 text-sm text-red-300 disabled:opacity-50">Stop</button>{/if}</div>{/if}
				</div>
				{#if fail2ban.installed}<div class="mt-4 grid gap-2 text-sm text-gray-400 sm:grid-cols-3"><span>Version: {fail2ban.version || 'unknown'}</span><span>SSH port: {fail2ban.ssh_port || 'unknown'}</span><span>Service: {fail2ban.enabled ? 'enabled at boot' : 'not enabled'}</span></div>{/if}
			</section>

			{#if fail2ban.installed}
				<section class="rounded-xl border border-gray-700 bg-gray-800 p-5">
					<div class="flex items-center justify-between"><div><h3 class="font-semibold text-white">Configuration</h3><p class="mt-1 text-sm text-gray-400">Simple mode uses conservative temporary-ban defaults.</p></div><div class="rounded-lg border border-gray-700 bg-gray-900 p-1"><button onclick={() => (editMode = 'simple')} class="rounded px-3 py-1.5 text-sm {editMode === 'simple' ? 'bg-blue-600 text-white' : 'text-gray-400'}">Simple</button><button onclick={() => (editMode = 'advanced')} class="rounded px-3 py-1.5 text-sm {editMode === 'advanced' ? 'bg-blue-600 text-white' : 'text-gray-400'}">Advanced</button></div></div>
					<div class="mt-5 grid gap-4 sm:grid-cols-2">
						<label class="sm:col-span-2"><span class="text-sm text-gray-300">Management IPs or CIDRs</span><textarea bind:value={managementNetworks} rows="3" placeholder="203.0.113.8/32" class="mt-1 w-full rounded-lg border border-gray-600 bg-gray-900 px-3 py-2 font-mono text-sm text-white"></textarea><span class="mt-1 block text-xs text-gray-400">Separate entries with spaces, commas, or new lines. Loopback is always allowed by the backend.</span></label>
						{#if settings.sshd_enabled && managementNetworks.trim() === ''}<div class="sm:col-span-2 rounded-lg border border-yellow-700 bg-yellow-900/30 p-3 text-sm text-yellow-300"><strong>Lockout warning:</strong> add the IP/CIDR used to manage this VPS and keep provider console access open before enabling SSH protection.</div>{/if}
						<label class="flex items-center gap-3 rounded-lg border border-gray-700 bg-gray-900/50 p-3 sm:col-span-2"><input type="checkbox" bind:checked={settings.sshd_enabled} class="h-4 w-4" /><span><span class="block text-sm font-medium text-white">Protect SSH</span><span class="text-xs text-gray-400">Only temporary bans; SSH configuration is not changed.</span></span></label>
						{#if editMode === 'advanced'}
							<label><span class="text-sm text-gray-300">Maximum retries</span><input type="number" min="1" max="20" bind:value={settings.max_retry} class="mt-1 w-full rounded-lg border border-gray-600 bg-gray-900 px-3 py-2 text-white" /></label>
							<label><span class="text-sm text-gray-300">Observation window (seconds)</span><input type="number" min="60" max="86400" bind:value={settings.find_time_seconds} class="mt-1 w-full rounded-lg border border-gray-600 bg-gray-900 px-3 py-2 text-white" /></label>
							<label><span class="text-sm text-gray-300">Temporary ban (seconds)</span><input type="number" min="60" max="604800" bind:value={settings.ban_time_seconds} class="mt-1 w-full rounded-lg border border-gray-600 bg-gray-900 px-3 py-2 text-white" /></label>
						{:else}<div class="sm:col-span-2 grid gap-2 rounded-lg border border-gray-700 bg-gray-900/50 p-4 text-sm text-gray-300 sm:grid-cols-3"><span>5 failed attempts</span><span>10-minute window</span><span>15-minute ban</span></div>{/if}
					</div>
					<button onclick={applySettings} disabled={busy !== '' || !!currentTaskId} class="mt-5 rounded-lg bg-blue-600 px-4 py-2 text-sm font-medium text-white hover:bg-blue-700 disabled:opacity-50">Validate & Apply</button>
				</section>

				<section class="rounded-xl border border-gray-700 bg-gray-800 p-5"><h3 class="font-semibold text-white">Active jails</h3>{#if fail2ban.jails.length === 0}<p class="mt-3 text-sm text-gray-400">No supported jail is active. Apply a validated configuration or retry after checking the task output.</p>{:else}<div class="mt-4 grid gap-3 md:grid-cols-2">{#each fail2ban.jails as jail}<div class="rounded-lg border border-gray-700 bg-gray-900/60 p-4"><div class="flex justify-between"><span class="font-medium text-white">{jail.name}</span><span class="text-xs text-gray-400">{jail.currently_banned} banned</span></div><div class="mt-3 grid grid-cols-2 gap-2 text-xs text-gray-400"><span>Failed now: {jail.currently_failed}</span><span>Failed total: {jail.total_failed}</span><span>Filter: {jail.filter_available ? 'ready' : 'missing'}</span><span>Log source: {jail.source_available ? 'ready' : 'missing'}</span></div></div>{/each}</div>{/if}</section>

				<section class="rounded-xl border border-gray-700 bg-gray-800 p-5"><h3 class="font-semibold text-white">Temporary manual ban</h3><div class="mt-4 grid gap-3 sm:grid-cols-4"><select bind:value={banJail} class="rounded-lg border border-gray-600 bg-gray-900 px-3 py-2 text-sm text-white"><option value="">Select jail</option>{#each fail2ban.jails as jail}<option value={jail.name}>{jail.name}</option>{/each}</select><input bind:value={banIP} placeholder="203.0.113.7" class="rounded-lg border border-gray-600 bg-gray-900 px-3 py-2 font-mono text-sm text-white sm:col-span-2" /><input type="number" min="60" max="604800" bind:value={banDuration} class="rounded-lg border border-gray-600 bg-gray-900 px-3 py-2 text-sm text-white" /></div><button onclick={createBan} disabled={busy !== '' || fail2ban.jails.length === 0} class="mt-3 rounded-lg bg-red-600 px-4 py-2 text-sm text-white disabled:opacity-50">Ban temporarily</button>
					{#if bans.length === 0}<p class="mt-5 text-sm text-gray-400">No active bans.</p>{:else}<div class="mt-5 overflow-x-auto"><table class="w-full text-left text-sm"><thead class="text-xs uppercase text-gray-400"><tr><th class="pb-2">Address</th><th class="pb-2">Jail</th><th class="pb-2">Expiry</th><th class="pb-2 text-right">Action</th></tr></thead><tbody class="divide-y divide-gray-700">{#each bans as ban}<tr><td class="py-3 font-mono text-white">{ban.ip}</td><td class="py-3 text-gray-300">{ban.jail}</td><td class="py-3 text-gray-400">{formatBanExpiry(ban.expires_at)}</td><td class="py-3 text-right"><button onclick={() => removeBan(ban)} disabled={busy !== ''} class="text-red-400 hover:text-red-300 disabled:opacity-50">Unban</button></td></tr>{/each}</tbody></table></div>{/if}
				</section>
			{/if}
		</div>
	{:else}
		<section class="rounded-xl border border-gray-700 bg-gray-800 p-5">
			<div class="flex items-center justify-between"><div><h3 class="font-semibold text-white">Security events</h3><p class="mt-1 text-sm text-gray-400">Repeated evidence is grouped to keep this list actionable.</p></div><span class="text-sm text-gray-400">{events.length} shown</span></div>
			{#if events.length === 0}<div class="mt-6 rounded-lg border border-gray-700 bg-gray-900/50 p-6 text-center text-sm text-gray-400">No security events have been observed.</div>{:else}<div class="mt-4 space-y-3">{#each events as event}<article class="rounded-lg border border-gray-700 bg-gray-900/60 p-4"><div class="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between"><div><div class="flex flex-wrap items-center gap-2"><span class="rounded-full px-2 py-0.5 text-xs uppercase {event.severity === 'critical' ? 'bg-red-900 text-red-300' : event.severity === 'high' ? 'bg-yellow-900 text-yellow-300' : 'bg-gray-700 text-gray-300'}">{event.severity}</span><span class="text-sm font-medium text-white">{event.category}</span><span class="text-xs text-gray-500">×{event.occurrence_count}</span></div><p class="mt-2 text-sm text-gray-300">{event.component} · {event.resource || 'server'}</p><p class="mt-1 text-sm text-gray-400">{event.recommended_action || 'Review the related component and evidence.'}</p><p class="mt-2 text-xs text-gray-500">Last observed {new Date(event.last_seen).toLocaleString()}</p></div><div class="flex flex-wrap gap-2">{#if event.status === 'open'}<button onclick={() => transitionEvent(event, 'acknowledged')} disabled={busy !== ''} class="rounded border border-gray-600 px-2 py-1 text-xs text-gray-300">Acknowledge</button>{/if}{#if event.status === 'open' || event.status === 'acknowledged'}<button onclick={() => transitionEvent(event, 'resolved')} disabled={busy !== ''} class="rounded border border-green-700 px-2 py-1 text-xs text-green-300">Resolve</button><button onclick={() => transitionEvent(event, 'false_positive')} disabled={busy !== ''} class="rounded border border-gray-600 px-2 py-1 text-xs text-gray-400">False positive</button>{:else}<span class="text-xs capitalize text-gray-400">{event.status.replaceAll('_', ' ')}</span>{/if}</div></div></article>{/each}</div>{/if}
		</section>
	{/if}
</div>
