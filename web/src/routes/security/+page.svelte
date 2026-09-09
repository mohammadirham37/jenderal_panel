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
	import {
		buildOnAccessRequest,
		buildSafeSchedule,
		formatSignatureAge,
		normalizeMalwareStatus
	} from '$lib/malware.js';
	import {
		buildTrafficProfile,
		enforcementWarning,
		observationProgress,
		summarizeTraffic
	} from '$lib/traffic-guard.js';

	type Tab = 'overview' | 'fail2ban' | 'malware' | 'traffic' | 'events';
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
	interface MalwareStatus {
		installed: boolean; healthy: boolean; state: string; version: string; engine: string;
		signature_version: string; signature_updated_at: string | null; signature_fresh: boolean;
		updater_running: boolean; daemon_installed: boolean; daemon_running: boolean;
		daemon_supported: boolean; message: string;
		on_access: { available: boolean; enabled: boolean; prevention_supported: boolean; prevention_enabled: boolean; message: string };
	}
	interface MalwareScan {
		id: string; mode: string; status: string; engine: string; files_scanned: number;
		findings_count: number; error?: string; started_at?: string; ended_at?: string;
	}
	interface QuarantineItem {
		id: string; website_id: string; original_path: string; sha256: string; signature: string;
		status: string; size_bytes: number; detected_at: string;
	}
	interface MalwareSchedule { id?: string; enabled: boolean; local_time: string; mode: string; website_ids: string[] }
	interface Website { id: string; domain: string; status: string }
	interface TrafficProfile {
		website_id: string; mode: 'observe' | 'balanced' | 'strict' | 'custom';
		proxy_mode: 'direct' | 'cloudflare' | 'custom'; proxy_header: string; proxy_cidrs: string[];
		requests_per_second: number; burst: number; connections: number;
		observe_started_at: string; created_at: string; updated_at: string;
	}
	interface TrafficBucket {
		bucket_at: string; requests: number; status_4xx: number; status_5xx: number;
		status_429: number; bytes: number; peak_rps: number;
		top_ips: Record<string, number>; top_paths: Record<string, number>;
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
	let malware = $state<MalwareStatus>(normalizeMalwareStatus(null) as MalwareStatus);
	let malwareScans = $state<MalwareScan[]>([]);
	let quarantine = $state<QuarantineItem[]>([]);
	let malwareSchedules = $state<MalwareSchedule[]>([]);
	let websites = $state<Website[]>([]);
	let selectedWebsite = $state('');
	let malwareMode = $state<'simple' | 'advanced'>('simple');
	let scheduleEnabled = $state(true);
	let scheduleTime = $state('02:00');
	let onAccessEnabled = $state(false);
	let preventionEnabled = $state(false);
	let preventionConfirmed = $state(false);
	let trafficProfiles = $state<TrafficProfile[]>([]);
	let trafficBuckets = $state<TrafficBucket[]>([]);
	let selectedTrafficWebsite = $state('');
	let trafficMode = $state<TrafficProfile['mode']>('observe');
	let trafficProxyMode = $state<TrafficProfile['proxy_mode']>('direct');
	let trafficProxyHeader = $state('X-Forwarded-For');
	let trafficProxyCIDRs = $state('');
	let trafficRPS = $state(10);
	let trafficBurst = $state(20);
	let trafficConnections = $state(20);
	let trafficConfirmed = $state(false);
	let trafficSummary = $derived(summarizeTraffic(trafficBuckets));
	let trafficPeak = $derived(Math.max(1, ...trafficBuckets.map((bucket) => bucket.requests)));
	let topTrafficPaths = $derived.by(() => {
		const totals = new Map<string, number>();
		for (const bucket of trafficBuckets) for (const [path, count] of Object.entries(bucket.top_paths || {})) totals.set(path, (totals.get(path) || 0) + count);
		return [...totals.entries()].sort((a, b) => b[1] - a[1]).slice(0, 10);
	});
	let selectedTrafficProfile = $derived(trafficProfiles.find((profile) => profile.website_id === selectedTrafficWebsite));
	let trafficObservation = $derived(observationProgress(selectedTrafficProfile?.observe_started_at));
	let trafficCondition = $derived.by(() => {
		const severities = events.filter((event) => event.category === 'traffic' && (event.status === 'open' || event.status === 'acknowledged')).map((event) => event.severity);
		if (severities.includes('critical')) return 'Critical';
		if (severities.includes('high')) return 'High';
		if (severities.includes('medium')) return 'Warning';
		return 'Normal';
	});

	async function loadData() {
		loading = true;
		error = '';
		try {
			const [overviewData, statusData, banData, eventData, malwareData, scanData, quarantineData, scheduleData, websiteData, trafficData] = await Promise.all([
				api.get<Overview>('/api/v1/security/overview'),
				api.get<Fail2banStatus>('/api/v1/security/fail2ban'),
				api.get<Ban[]>('/api/v1/security/fail2ban/bans'),
				api.get<SecurityEvent[]>('/api/v1/security/events?per_page=100'),
				api.get<MalwareStatus>('/api/v1/security/malware/status'),
				api.get<MalwareScan[]>('/api/v1/security/malware/scans'),
				api.get<QuarantineItem[]>('/api/v1/security/malware/quarantine'),
				api.get<MalwareSchedule[]>('/api/v1/security/malware/schedules'),
				api.get<Website[]>('/api/v1/websites'),
				api.get<TrafficProfile[]>('/api/v1/security/traffic/profiles')
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
			malware = normalizeMalwareStatus(malwareData) as MalwareStatus;
			malwareScans = scanData || [];
			quarantine = quarantineData || [];
			malwareSchedules = scheduleData || [];
			websites = (websiteData || []).filter((website) => website.status === 'active' || website.status === 'suspended');
			trafficProfiles = trafficData || [];
			if (!selectedWebsite && websites.length > 0) selectedWebsite = websites[0].id;
			if (!selectedTrafficWebsite && websites.length > 0) selectedTrafficWebsite = websites[0].id;
			syncTrafficForm();
			await loadTrafficBuckets();
			if (malwareSchedules.length > 0) {
				scheduleEnabled = malwareSchedules[0].enabled;
				scheduleTime = malwareSchedules[0].local_time;
			}
			onAccessEnabled = malware.on_access.enabled;
			preventionEnabled = malware.on_access.prevention_enabled;
			if (!banJail && fail2ban.jails.length > 0) banJail = fail2ban.jails[0].name;
		} catch (err) {
			error = err instanceof Error ? err.message : 'Unable to load Security Center.';
		} finally {
			loading = false;
		}
	}

	function syncTrafficForm() {
		const profile = trafficProfiles.find((item) => item.website_id === selectedTrafficWebsite);
		if (!profile) return;
		trafficMode = profile.mode;
		trafficProxyMode = profile.proxy_mode;
		trafficProxyHeader = profile.proxy_header || 'X-Forwarded-For';
		trafficProxyCIDRs = (profile.proxy_cidrs || []).join('\n');
		trafficRPS = profile.requests_per_second;
		trafficBurst = profile.burst;
		trafficConnections = profile.connections;
		trafficConfirmed = false;
	}

	async function loadTrafficBuckets() {
		if (!selectedTrafficWebsite) { trafficBuckets = []; return; }
		try { trafficBuckets = await api.get<TrafficBucket[]>(`/api/v1/security/traffic/websites/${selectedTrafficWebsite}/buckets`) || []; }
		catch (err) { actionError = err instanceof Error ? err.message : 'Unable to load Traffic Guard evidence.'; trafficBuckets = []; }
	}

	async function chooseTrafficWebsite(id: string) {
		selectedTrafficWebsite = id;
		syncTrafficForm();
		await loadTrafficBuckets();
	}

	async function applyTrafficGuard() {
		if (!selectedTrafficWebsite) { actionError = 'Select a website first.'; return; }
		busy = 'traffic-apply'; actionError = ''; actionMessage = '';
		try {
			const payload = buildTrafficProfile({
				mode: trafficMode, proxy_mode: trafficProxyMode, proxy_header: trafficProxyHeader,
				proxy_cidrs: trafficProxyCIDRs.split(/[\s,]+/).filter(Boolean),
				requests_per_second: Number(trafficRPS), burst: Number(trafficBurst), connections: Number(trafficConnections)
			});
			if (trafficMode !== 'observe' && !trafficConfirmed) throw new Error('Confirm HTTP enforcement before applying this profile.');
			const result = await api.put<{ task_id: string }>(`/api/v1/security/traffic/websites/${selectedTrafficWebsite}`, { ...payload, confirm: trafficMode !== 'observe' && trafficConfirmed });
			currentTaskId = result.task_id; actionMessage = 'Traffic Guard configuration is being validated and applied.';
		} catch (err) { actionError = err instanceof Error ? err.message : 'Unable to apply Traffic Guard.'; }
		finally { busy = ''; }
	}

	async function resetTrafficObserve() {
		if (!selectedTrafficWebsite) return;
		busy = 'traffic-observe'; actionError = '';
		try {
			const result = await api.post<{ task_id: string }>(`/api/v1/security/traffic/websites/${selectedTrafficWebsite}/observe`, {});
			currentTaskId = result.task_id; actionMessage = 'Traffic Guard is returning to Observe Mode.';
		} catch (err) { actionError = err instanceof Error ? err.message : 'Unable to restore Observe Mode.'; }
		finally { busy = ''; }
	}

	async function refreshCloudflareCIDRs() {
		busy = 'traffic-cloudflare'; actionError = '';
		try {
			const result = await api.post<{ task_id: string }>('/api/v1/security/traffic/cloudflare/refresh', {});
			currentTaskId = result.task_id; actionMessage = 'Official Cloudflare CIDR refresh started.';
		} catch (err) { actionError = err instanceof Error ? err.message : 'Unable to refresh Cloudflare CIDRs.'; }
		finally { busy = ''; }
	}

	async function installMalware(mode: 'low_memory' | 'daemon') {
		busy = `malware-install:${mode}`; actionError = ''; actionMessage = '';
		try {
			const result = await api.post<{ task_id: string }>('/api/v1/security/malware/install', { mode });
			currentTaskId = result.task_id;
			actionMessage = 'ClamAV installation started. Progress remains available after refresh.';
		} catch (err) { actionError = err instanceof Error ? err.message : 'Unable to install ClamAV.'; }
		finally { busy = ''; }
	}

	async function updateSignatures() {
		busy = 'malware-signatures'; actionError = ''; actionMessage = '';
		try {
			const result = await api.post<{ task_id: string }>('/api/v1/security/malware/signatures/update', {});
			currentTaskId = result.task_id; actionMessage = 'ClamAV signature update started.';
		} catch (err) { actionError = err instanceof Error ? err.message : 'Unable to update signatures.'; }
		finally { busy = ''; }
	}

	async function startMalwareScan(mode: 'quick' | 'website' | 'full_websites') {
		if (mode === 'website' && !selectedWebsite) { actionError = 'Select a website first.'; return; }
		busy = `malware-scan:${mode}`; actionError = ''; actionMessage = '';
		try {
			const result = await api.post<{ task_id: string }>('/api/v1/security/malware/scans', {
				mode, website_ids: mode === 'website' ? [selectedWebsite] : []
			});
			currentTaskId = result.task_id; actionMessage = 'Malware scan started with safe resource limits.';
		} catch (err) { actionError = err instanceof Error ? err.message : 'Unable to start malware scan.'; }
		finally { busy = ''; }
	}

	async function saveMalwareSchedule() {
		busy = 'malware-schedule'; actionError = '';
		try {
			const safe = buildSafeSchedule({ time: scheduleTime });
			await api.put('/api/v1/security/malware/schedules', {
				...safe, id: malwareSchedules[0]?.id || '', enabled: scheduleEnabled
			});
			actionMessage = scheduleEnabled ? `Daily Quick Scan saved for ${scheduleTime}.` : 'Daily malware scan disabled.';
			await loadData();
		} catch (err) { actionError = err instanceof Error ? err.message : 'Unable to save scan schedule.'; }
		finally { busy = ''; }
	}

	async function configureOnAccess() {
		busy = 'malware-on-access'; actionError = '';
		try {
			const request = buildOnAccessRequest({ enabled: onAccessEnabled, prevention: preventionEnabled, confirmed: preventionConfirmed });
			const result = await api.put<{ task_id: string }>('/api/v1/security/malware/on-access', request);
			currentTaskId = result.task_id; actionMessage = 'On-access configuration is being validated and applied.';
		} catch (err) { actionError = err instanceof Error ? err.message : 'Unable to configure on-access scanning.'; }
		finally { busy = ''; }
	}

	async function restoreQuarantine(item: QuarantineItem) {
		if (!confirm(`Restore ${item.original_path}? The destination must still be empty and the sample must scan clean.`)) return;
		busy = `restore:${item.id}`; actionError = '';
		try {
			const result = await api.post<{ task_id: string }>(`/api/v1/security/malware/quarantine/${item.id}/restore`, {});
			currentTaskId = result.task_id; actionMessage = 'Restore validation started.';
		} catch (err) { actionError = err instanceof Error ? err.message : 'Unable to restore sample.'; }
		finally { busy = ''; }
	}

	async function markFalsePositive(item: QuarantineItem) {
		if (!confirm('Allowlist only this exact website, path, and SHA-256 hash?')) return;
		busy = `false-positive:${item.id}`; actionError = '';
		try { await api.post(`/api/v1/security/malware/quarantine/${item.id}/false-positive`, {}); actionMessage = 'Exact sample marked as false positive.'; await loadData(); }
		catch (err) { actionError = err instanceof Error ? err.message : 'Unable to mark false positive.'; }
		finally { busy = ''; }
	}

	async function deleteQuarantine(item: QuarantineItem) {
		if (!confirm(`Permanently delete quarantined sample ${item.id}? This cannot be undone.`)) return;
		busy = `delete:${item.id}`; actionError = '';
		try { await api.del(`/api/v1/security/malware/quarantine/${item.id}`); actionMessage = 'Quarantined sample permanently deleted.'; await loadData(); }
		catch (err) { actionError = err instanceof Error ? err.message : 'Unable to delete quarantined sample.'; }
		finally { busy = ''; }
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
		{#each ['overview', 'fail2ban', 'malware', 'traffic', 'events'] as tab}
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
	{:else if activeTab === 'malware'}
		<div class="space-y-4">
			<section class="rounded-xl border border-gray-700 bg-gray-800 p-5">
				<div class="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
					<div>
						<div class="flex flex-wrap items-center gap-2">
							<h3 class="text-lg font-semibold text-white">Malware Scanner</h3>
							<span class="rounded-full px-2 py-0.5 text-xs {malware.healthy ? 'bg-green-900 text-green-300' : malware.installed ? 'bg-yellow-900 text-yellow-300' : 'bg-gray-700 text-gray-300'}">{malware.state.replaceAll('_', ' ')}</span>
						</div>
						<p class="mt-1 max-w-2xl text-sm text-gray-400">{malware.message || 'Scan panel-managed website roots with ClamAV and isolate suspicious files outside Nginx document roots.'}</p>
					</div>
					{#if !malware.installed}
						<div class="flex flex-wrap gap-2">
							<button onclick={() => installMalware('low_memory')} disabled={busy !== '' || !!currentTaskId} class="rounded-lg bg-blue-600 px-4 py-2 text-sm font-medium text-white hover:bg-blue-700 disabled:opacity-50">Install low-memory</button>
							<button onclick={() => installMalware('daemon')} disabled={!malware.daemon_supported || busy !== '' || !!currentTaskId} title={malware.daemon_supported ? 'Resident scanner for repeated scans' : 'Requires at least 2 GiB RAM'} class="rounded-lg border border-gray-600 px-4 py-2 text-sm text-gray-300 hover:bg-gray-900 disabled:opacity-50">Install daemon</button>
						</div>
					{:else}
						<button onclick={updateSignatures} disabled={busy !== '' || !!currentTaskId} class="rounded-lg border border-gray-600 px-4 py-2 text-sm text-gray-300 hover:bg-gray-900 disabled:opacity-50">Update signatures</button>
					{/if}
				</div>
				{#if malware.installed}
					<div class="mt-5 grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
						<div class="rounded-lg border border-gray-700 bg-gray-900/60 p-3"><p class="text-xs uppercase text-gray-500">Engine</p><p class="mt-1 font-medium text-white">{malware.engine || 'clamscan'}</p><p class="text-xs text-gray-400">{malware.version || 'version unknown'}</p></div>
						<div class="rounded-lg border border-gray-700 bg-gray-900/60 p-3"><p class="text-xs uppercase text-gray-500">Signatures</p><p class="mt-1 font-medium text-white">{malware.signature_version || 'unknown'}</p><p class="text-xs {malware.signature_fresh ? 'text-green-300' : 'text-yellow-300'}">{formatSignatureAge(malware.signature_updated_at)}</p></div>
						<div class="rounded-lg border border-gray-700 bg-gray-900/60 p-3"><p class="text-xs uppercase text-gray-500">Updater</p><p class="mt-1 font-medium text-white">{malware.updater_running ? 'Running' : 'Needs attention'}</p></div>
						<div class="rounded-lg border border-gray-700 bg-gray-900/60 p-3"><p class="text-xs uppercase text-gray-500">Quarantine</p><p class="mt-1 font-medium text-white">{quarantine.filter((item) => item.status === 'quarantined' || item.status === 'false_positive').length} retained</p><p class="text-xs text-gray-400">Never auto-deleted</p></div>
					</div>
				{/if}
			</section>

			{#if malware.installed}
				<section class="rounded-xl border border-gray-700 bg-gray-800 p-5">
					<div class="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
						<div><h3 class="font-semibold text-white">Run a scan</h3><p class="mt-1 text-sm text-gray-400">One scan at a time, low CPU/I/O priority, one-hour limit, and files up to 100 MiB.</p></div>
						<div class="flex gap-1 rounded-lg border border-gray-700 bg-gray-900 p-1"><button onclick={() => (malwareMode = 'simple')} class="rounded px-3 py-1.5 text-sm {malwareMode === 'simple' ? 'bg-blue-600 text-white' : 'text-gray-400'}">Simple</button><button onclick={() => (malwareMode = 'advanced')} class="rounded px-3 py-1.5 text-sm {malwareMode === 'advanced' ? 'bg-blue-600 text-white' : 'text-gray-400'}">Advanced</button></div>
					</div>
					<div class="mt-5 flex flex-wrap gap-3">
						<button onclick={() => startMalwareScan('quick')} disabled={busy !== '' || !!currentTaskId} class="rounded-lg bg-blue-600 px-4 py-2 text-sm font-medium text-white disabled:opacity-50">Quick scan</button>
						<button onclick={() => startMalwareScan('full_websites')} disabled={busy !== '' || !!currentTaskId} class="rounded-lg border border-gray-600 px-4 py-2 text-sm text-gray-300 hover:bg-gray-900 disabled:opacity-50">Full website scan</button>
					</div>
					<div class="mt-4 flex flex-col gap-2 sm:flex-row">
						<select bind:value={selectedWebsite} class="min-w-64 rounded-lg border border-gray-600 bg-gray-900 px-3 py-2 text-sm text-white"><option value="">Select website</option>{#each websites as website}<option value={website.id}>{website.domain}</option>{/each}</select>
						<button onclick={() => startMalwareScan('website')} disabled={!selectedWebsite || busy !== '' || !!currentTaskId} class="rounded-lg border border-blue-600 px-4 py-2 text-sm text-blue-300 disabled:opacity-50">Scan selected website</button>
					</div>
					{#if malwareMode === 'advanced'}
						<div class="mt-5 grid gap-3 rounded-lg border border-gray-700 bg-gray-900/50 p-4 text-sm text-gray-300 sm:grid-cols-3"><span>Maximum files: 100,000</span><span>Maximum file size: 100 MiB</span><span>Archive depth: 16</span></div>
					{/if}
				</section>

				<div class="grid gap-4 lg:grid-cols-2">
					<section class="rounded-xl border border-gray-700 bg-gray-800 p-5">
						<h3 class="font-semibold text-white">Daily schedule</h3><p class="mt-1 text-sm text-gray-400">The Safe preset scans only files changed since the last successful Quick Scan.</p>
						<div class="mt-4 flex flex-wrap items-end gap-3"><label class="flex items-center gap-2 rounded-lg border border-gray-700 bg-gray-900/50 px-3 py-2 text-sm text-gray-300"><input type="checkbox" bind:checked={scheduleEnabled} /> Enabled</label><label><span class="block text-xs text-gray-400">Server local time</span><input type="time" bind:value={scheduleTime} class="mt-1 rounded-lg border border-gray-600 bg-gray-900 px-3 py-2 text-sm text-white" /></label><button onclick={saveMalwareSchedule} disabled={busy !== ''} class="rounded-lg bg-blue-600 px-4 py-2 text-sm text-white disabled:opacity-50">Save schedule</button></div>
					</section>
					<section class="rounded-xl border border-gray-700 bg-gray-800 p-5">
						<h3 class="font-semibold text-white">On-access protection <span class="ml-1 text-xs font-normal text-yellow-300">Advanced</span></h3><p class="mt-1 text-sm text-gray-400">Off by default. Requires the daemon, clamonacc, and supported Linux fanotify features.</p>
						<p class="mt-2 text-xs text-gray-500">{malware.on_access.message}</p>
						<div class="mt-4 space-y-3"><label class="flex items-center gap-2 text-sm text-gray-300"><input type="checkbox" bind:checked={onAccessEnabled} disabled={!malware.on_access.available && !malware.on_access.enabled} /> Enable notify-only monitoring</label><label class="flex items-center gap-2 text-sm text-gray-300"><input type="checkbox" bind:checked={preventionEnabled} disabled={!onAccessEnabled || !malware.on_access.prevention_supported} /> Block access to detected files</label>{#if preventionEnabled}<label class="flex items-start gap-2 rounded-lg border border-yellow-700 bg-yellow-900/25 p-3 text-sm text-yellow-300"><input class="mt-1" type="checkbox" bind:checked={preventionConfirmed} /><span>I understand prevention can materially affect busy website directories and may block access.</span></label>{/if}</div>
						<button onclick={configureOnAccess} disabled={busy !== '' || (!malware.on_access.available && !malware.on_access.enabled)} class="mt-4 rounded-lg border border-blue-600 px-4 py-2 text-sm text-blue-300 disabled:opacity-50">Validate & Apply</button>
					</section>
				</div>

				<section class="rounded-xl border border-gray-700 bg-gray-800 p-5">
					<div class="flex items-center justify-between"><div><h3 class="font-semibold text-white">Scan history</h3><p class="mt-1 text-sm text-gray-400">Recent persistent scan results.</p></div><span class="text-sm text-gray-400">{malwareScans.length} shown</span></div>
					{#if malwareScans.length === 0}<p class="mt-4 text-sm text-gray-400">No malware scan has run yet.</p>{:else}<div class="mt-4 overflow-x-auto"><table class="w-full text-left text-sm"><thead class="text-xs uppercase text-gray-400"><tr><th class="pb-2">Mode</th><th class="pb-2">Status</th><th class="pb-2">Files</th><th class="pb-2">Findings</th><th class="pb-2">Started</th></tr></thead><tbody class="divide-y divide-gray-700">{#each malwareScans as scan}<tr><td class="py-3 text-white">{scan.mode.replaceAll('_', ' ')}</td><td class="py-3"><span class="rounded-full px-2 py-0.5 text-xs {scan.status === 'completed' ? 'bg-green-900 text-green-300' : scan.status === 'failed' ? 'bg-red-900 text-red-300' : 'bg-yellow-900 text-yellow-300'}">{scan.status}</span>{#if scan.error}<p class="mt-1 max-w-lg text-xs text-red-300">{scan.error}</p>{/if}</td><td class="py-3 text-gray-300">{scan.files_scanned}</td><td class="py-3 text-gray-300">{scan.findings_count}</td><td class="py-3 text-gray-400">{scan.started_at ? new Date(scan.started_at).toLocaleString() : '—'}</td></tr>{/each}</tbody></table></div>{/if}
				</section>

				<section class="rounded-xl border border-gray-700 bg-gray-800 p-5">
					<div class="flex items-center justify-between"><div><h3 class="font-semibold text-white">Quarantine</h3><p class="mt-1 text-sm text-gray-400">Review comes first; permanent deletion is never the default action.</p></div><span class="text-sm text-gray-400">{quarantine.length} items</span></div>
					{#if quarantine.length === 0}<p class="mt-4 text-sm text-gray-400">No quarantined files.</p>{:else}<div class="mt-4 space-y-3">{#each quarantine as item}<article class="rounded-lg border border-gray-700 bg-gray-900/60 p-4"><div class="flex flex-col gap-3 lg:flex-row lg:items-start lg:justify-between"><div class="min-w-0"><div class="flex flex-wrap items-center gap-2"><span class="rounded-full bg-red-900 px-2 py-0.5 text-xs text-red-300">{item.signature}</span><span class="text-xs text-gray-400">{item.status.replaceAll('_', ' ')}</span></div><p class="mt-2 break-all font-mono text-sm text-white">{item.original_path}</p><p class="mt-1 break-all font-mono text-xs text-gray-500">SHA-256 {item.sha256}</p><p class="mt-1 text-xs text-gray-400">Detected {new Date(item.detected_at).toLocaleString()} · {item.size_bytes} bytes</p></div><div class="flex flex-wrap gap-2">{#if item.status === 'quarantined' || item.status === 'false_positive'}<a download href={`/api/v1/security/malware/quarantine/${item.id}/download`} class="rounded border border-gray-600 px-3 py-1.5 text-xs text-gray-300">Download</a><button onclick={() => restoreQuarantine(item)} disabled={busy !== ''} class="rounded border border-green-700 px-3 py-1.5 text-xs text-green-300 disabled:opacity-50">Restore</button>{#if item.status === 'quarantined'}<button onclick={() => markFalsePositive(item)} disabled={busy !== ''} class="rounded border border-gray-600 px-3 py-1.5 text-xs text-gray-300 disabled:opacity-50">False positive</button>{/if}<button onclick={() => deleteQuarantine(item)} disabled={busy !== ''} class="rounded bg-red-700 px-3 py-1.5 text-xs text-white disabled:opacity-50">Delete permanently</button>{/if}</div></div></article>{/each}</div>{/if}
				</section>
			{/if}
		</div>
	{:else if activeTab === 'traffic'}
		<div class="space-y-4">
			<section class="rounded-xl border border-gray-700 bg-gray-800 p-5">
				<div class="flex flex-col gap-4 lg:flex-row lg:items-start lg:justify-between">
					<div>
						<div class="flex flex-wrap items-center gap-2"><h3 class="text-lg font-semibold text-white">Traffic Guard</h3><span class="rounded-full bg-blue-900 px-2 py-0.5 text-xs text-blue-300">HTTP layer</span><span class="rounded-full px-2 py-0.5 text-xs {trafficCondition === 'Critical' ? 'bg-red-900 text-red-300' : trafficCondition === 'High' || trafficCondition === 'Warning' ? 'bg-yellow-900 text-yellow-300' : 'bg-green-900 text-green-300'}">{trafficCondition}</span></div>
						<p class="mt-1 max-w-3xl text-sm text-gray-400">Observe traffic per website, preserve the real client IP behind an explicitly trusted proxy, then optionally apply bounded Nginx limits.</p>
					</div>
					<select value={selectedTrafficWebsite} onchange={(event) => chooseTrafficWebsite(event.currentTarget.value)} class="min-w-64 rounded-lg border border-gray-600 bg-gray-900 px-3 py-2 text-sm text-white">
						<option value="">Select website</option>{#each websites as website}<option value={website.id}>{website.domain}</option>{/each}
					</select>
				</div>
				<div class="mt-5 rounded-lg border border-blue-800 bg-blue-950/40 p-4">
					<div class="flex items-center justify-between text-sm"><span class="font-medium text-blue-200">24-hour observation</span><span class="text-blue-300">{trafficObservation}%</span></div>
					<div class="mt-2 h-2 overflow-hidden rounded-full bg-gray-700"><div class="h-full rounded-full bg-blue-500 transition-all" style={`width: ${trafficObservation}%`}></div></div>
					<p class="mt-2 text-xs text-gray-400">Enforcement remains unavailable until a complete 24-hour observation period has been recorded. Observe Mode never rejects a request.</p>
				</div>
			</section>

			<div class="grid gap-3 sm:grid-cols-2 lg:grid-cols-5">
				<div class="rounded-xl border border-gray-700 bg-gray-800 p-4"><p class="text-xs uppercase text-gray-500">Requests / 24h</p><p class="mt-1 text-xl font-semibold text-white">{trafficSummary.requests.toLocaleString()}</p></div>
				<div class="rounded-xl border border-gray-700 bg-gray-800 p-4"><p class="text-xs uppercase text-gray-500">Peak RPS</p><p class="mt-1 text-xl font-semibold text-white">{trafficSummary.peakRPS}</p></div>
				<div class="rounded-xl border border-gray-700 bg-gray-800 p-4"><p class="text-xs uppercase text-gray-500">4xx</p><p class="mt-1 text-xl font-semibold text-yellow-300">{trafficSummary.status4xx}</p></div>
				<div class="rounded-xl border border-gray-700 bg-gray-800 p-4"><p class="text-xs uppercase text-gray-500">5xx</p><p class="mt-1 text-xl font-semibold text-red-300">{trafficSummary.status5xx}</p></div>
				<div class="rounded-xl border border-gray-700 bg-gray-800 p-4"><p class="text-xs uppercase text-gray-500">HTTP 429</p><p class="mt-1 text-xl font-semibold text-blue-300">{trafficSummary.status429}</p></div>
			</div>

			<section class="rounded-xl border border-gray-700 bg-gray-800 p-5">
				<div class="flex items-center justify-between"><div><h3 class="font-semibold text-white">Request activity</h3><p class="mt-1 text-sm text-gray-400">Latest collected minute buckets; collection continues after panel restarts.</p></div><span class="text-xs text-gray-500">Last {Math.min(30, trafficBuckets.length)} minutes shown</span></div>
				{#if trafficBuckets.length === 0}<p class="mt-6 text-sm text-gray-400">No completed access-log buckets are available yet.</p>{:else}
					<div class="mt-5 flex h-32 items-end gap-1 overflow-hidden" aria-label="Request chart">
						{#each trafficBuckets.slice(-30) as bucket}<div title={`${new Date(bucket.bucket_at).toLocaleTimeString()}: ${bucket.requests} requests`} class="min-w-1 flex-1 rounded-t bg-blue-500/80" style={`height: ${Math.max(3, (bucket.requests / trafficPeak) * 100)}%`}></div>{/each}
					</div>
				{/if}
			</section>

			<div class="grid gap-4 lg:grid-cols-2">
				<section class="rounded-xl border border-gray-700 bg-gray-800 p-5"><h3 class="font-semibold text-white">Top client IPs</h3>{#if trafficSummary.topIPs.length === 0}<p class="mt-4 text-sm text-gray-400">No client evidence yet.</p>{:else}<div class="mt-4 space-y-2">{#each trafficSummary.topIPs.slice(0, 10) as [ip, count]}<div class="flex items-center justify-between rounded border border-gray-700 bg-gray-900/50 px-3 py-2"><code class="text-sm text-gray-200">{ip}</code><span class="text-xs text-gray-400">{count} requests</span></div>{/each}</div>{/if}</section>
				<section class="rounded-xl border border-gray-700 bg-gray-800 p-5"><h3 class="font-semibold text-white">Top paths</h3>{#if topTrafficPaths.length === 0}<p class="mt-4 text-sm text-gray-400">No path evidence yet.</p>{:else}<div class="mt-4 space-y-2">{#each topTrafficPaths as [path, count]}<div class="flex items-center justify-between gap-3 rounded border border-gray-700 bg-gray-900/50 px-3 py-2"><code class="truncate text-sm text-gray-200">{path}</code><span class="shrink-0 text-xs text-gray-400">{count}</span></div>{/each}</div>{/if}</section>
			</div>

			<section class="rounded-xl border border-gray-700 bg-gray-800 p-5">
				<div><h3 class="font-semibold text-white">Protection profile</h3><p class="mt-1 text-sm text-gray-400">A candidate is written atomically, tested with <code>nginx -t</code>, reloaded, health-checked, and rolled back on failure.</p></div>
				<div class="mt-5 grid gap-4 md:grid-cols-2">
					<label><span class="text-sm text-gray-300">Mode</span><select bind:value={trafficMode} class="mt-1 w-full rounded-lg border border-gray-600 bg-gray-900 px-3 py-2 text-sm text-white"><option value="observe">Observe (recommended first)</option><option value="balanced" disabled={trafficObservation < 100}>Balanced</option><option value="strict" disabled={trafficObservation < 100}>Strict</option><option value="custom" disabled={trafficObservation < 100}>Custom</option></select></label>
					<label><span class="text-sm text-gray-300">Traffic source</span><select bind:value={trafficProxyMode} class="mt-1 w-full rounded-lg border border-gray-600 bg-gray-900 px-3 py-2 text-sm text-white"><option value="direct">Direct to VPS</option><option value="cloudflare">Cloudflare proxy</option><option value="custom">Custom trusted proxy</option></select></label>
					{#if trafficProxyMode === 'cloudflare'}<div class="rounded-lg border border-gray-700 bg-gray-900/50 p-3 text-sm text-gray-300 md:col-span-2"><p>Uses only <code>CF-Connecting-IP</code> from official Cloudflare CIDRs.</p><button onclick={refreshCloudflareCIDRs} disabled={busy !== '' || !!currentTaskId} class="mt-3 rounded border border-blue-600 px-3 py-1.5 text-xs text-blue-300 disabled:opacity-50">Refresh official CIDRs</button></div>{/if}
					{#if trafficProxyMode === 'custom'}
						<label><span class="text-sm text-gray-300">Forwarded IP header</span><select bind:value={trafficProxyHeader} class="mt-1 w-full rounded-lg border border-gray-600 bg-gray-900 px-3 py-2 text-sm text-white"><option>X-Forwarded-For</option><option>X-Real-IP</option><option>CF-Connecting-IP</option></select></label>
						<label><span class="text-sm text-gray-300">Exact trusted proxy CIDRs</span><textarea bind:value={trafficProxyCIDRs} rows="3" placeholder="203.0.113.0/24" class="mt-1 w-full rounded-lg border border-gray-600 bg-gray-900 px-3 py-2 font-mono text-sm text-white"></textarea></label>
					{/if}
					{#if trafficMode === 'custom'}<label><span class="text-sm text-gray-300">Requests per second</span><input type="number" min="1" max="1000" bind:value={trafficRPS} class="mt-1 w-full rounded-lg border border-gray-600 bg-gray-900 px-3 py-2 text-white" /></label><label><span class="text-sm text-gray-300">Burst</span><input type="number" min="1" max="5000" bind:value={trafficBurst} class="mt-1 w-full rounded-lg border border-gray-600 bg-gray-900 px-3 py-2 text-white" /></label><label><span class="text-sm text-gray-300">Connections per IP</span><input type="number" min="1" max="1000" bind:value={trafficConnections} class="mt-1 w-full rounded-lg border border-gray-600 bg-gray-900 px-3 py-2 text-white" /></label>{/if}
				</div>
				<div class="mt-5 rounded-lg border {trafficMode === 'observe' ? 'border-blue-800 bg-blue-950/30 text-blue-200' : 'border-yellow-700 bg-yellow-950/30 text-yellow-200'} p-4 text-sm">{enforcementWarning(trafficMode)}</div>
				{#if trafficMode !== 'observe'}<label class="mt-4 flex items-start gap-2 rounded-lg border border-yellow-700 bg-yellow-900/20 p-3 text-sm text-yellow-200"><input class="mt-1" type="checkbox" bind:checked={trafficConfirmed} /><span>I confirm that this origin-level profile may return HTTP 429 to excess requests and that upstream volumetric protection is separate.</span></label>{/if}
				<div class="mt-5 flex flex-wrap gap-3"><button onclick={applyTrafficGuard} disabled={!selectedTrafficWebsite || busy !== '' || !!currentTaskId || (trafficMode !== 'observe' && (!trafficConfirmed || trafficObservation < 100))} class="rounded-lg bg-blue-600 px-4 py-2 text-sm font-medium text-white disabled:opacity-50">Validate & Apply</button><button onclick={resetTrafficObserve} disabled={!selectedTrafficWebsite || selectedTrafficProfile?.mode === 'observe' || busy !== '' || !!currentTaskId} class="rounded-lg border border-green-700 px-4 py-2 text-sm text-green-300 disabled:opacity-50">Return to Observe</button></div>
			</section>
		</div>
	{:else}
		<section class="rounded-xl border border-gray-700 bg-gray-800 p-5">
			<div class="flex items-center justify-between"><div><h3 class="font-semibold text-white">Security events</h3><p class="mt-1 text-sm text-gray-400">Repeated evidence is grouped to keep this list actionable.</p></div><span class="text-sm text-gray-400">{events.length} shown</span></div>
			{#if events.length === 0}<div class="mt-6 rounded-lg border border-gray-700 bg-gray-900/50 p-6 text-center text-sm text-gray-400">No security events have been observed.</div>{:else}<div class="mt-4 space-y-3">{#each events as event}<article class="rounded-lg border border-gray-700 bg-gray-900/60 p-4"><div class="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between"><div><div class="flex flex-wrap items-center gap-2"><span class="rounded-full px-2 py-0.5 text-xs uppercase {event.severity === 'critical' ? 'bg-red-900 text-red-300' : event.severity === 'high' ? 'bg-yellow-900 text-yellow-300' : 'bg-gray-700 text-gray-300'}">{event.severity}</span><span class="text-sm font-medium text-white">{event.category}</span><span class="text-xs text-gray-500">×{event.occurrence_count}</span></div><p class="mt-2 text-sm text-gray-300">{event.component} · {event.resource || 'server'}</p><p class="mt-1 text-sm text-gray-400">{event.recommended_action || 'Review the related component and evidence.'}</p><p class="mt-2 text-xs text-gray-500">Last observed {new Date(event.last_seen).toLocaleString()}</p></div><div class="flex flex-wrap gap-2">{#if event.status === 'open'}<button onclick={() => transitionEvent(event, 'acknowledged')} disabled={busy !== ''} class="rounded border border-gray-600 px-2 py-1 text-xs text-gray-300">Acknowledge</button>{/if}{#if event.status === 'open' || event.status === 'acknowledged'}<button onclick={() => transitionEvent(event, 'resolved')} disabled={busy !== ''} class="rounded border border-green-700 px-2 py-1 text-xs text-green-300">Resolve</button><button onclick={() => transitionEvent(event, 'false_positive')} disabled={busy !== ''} class="rounded border border-gray-600 px-2 py-1 text-xs text-gray-400">False positive</button>{:else}<span class="text-xs capitalize text-gray-400">{event.status.replaceAll('_', ' ')}</span>{/if}</div></div></article>{/each}</div>{/if}
		</section>
	{/if}
</div>
