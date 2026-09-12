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
	import { buildSafeSetupRequest, normalizeSetupReview } from '$lib/security-setup.js';
import { toast } from '$lib/stores/toast';
	import { language, translate } from '$lib/stores/language';

	type Tab = 'overview' | 'setup' | 'fail2ban' | 'malware' | 'traffic' | 'events';
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
		posture?: { checked_at: string; components: Record<string, string>; findings: Array<{ code: string; component: string; severity: string; summary: string; remediation: string }> };
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
	interface SetupReview { mutations: string[]; warnings: string[]; hash: string }
	interface SetupState { id: string; status: string; safe_error: string; task_id: string; completed_steps: string[] }
	interface SetupAssessment { website_ids: string[]; latest?: SetupState }

	let activeTab = $state<Tab>('overview');
	let editMode = $state<'simple' | 'advanced'>('simple');
	let loading = $state(true);
	let error = $state('');
	let actionMessage = $state('');
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
	let setupAssessment = $state<SetupAssessment>({ website_ids: [] });
	let setupManagementCIDRs = $state('');
	let setupFail2ban = $state(true);
	let setupMalwareMode = $state('low_memory');
	let setupSchedule = $state(true);
	let setupScheduleTime = $state('02:00');
	let setupTrafficWebsites = $state<string[]>([]);
	let setupReview = $state<SetupReview | null>(null);
	let setupConfirmed = $state(false);
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
			const [overviewData, statusData, banData, eventData, malwareData, scanData, quarantineData, scheduleData, websiteData, trafficData, setupData] = await Promise.all([
				api.get<Overview>('/api/v1/security/overview'),
				api.get<Fail2banStatus>('/api/v1/security/fail2ban'),
				api.get<Ban[]>('/api/v1/security/fail2ban/bans'),
				api.get<SecurityEvent[]>('/api/v1/security/events?per_page=100'),
				api.get<MalwareStatus>('/api/v1/security/malware/status'),
				api.get<MalwareScan[]>('/api/v1/security/malware/scans'),
				api.get<QuarantineItem[]>('/api/v1/security/malware/quarantine'),
				api.get<MalwareSchedule[]>('/api/v1/security/malware/schedules'),
				api.get<Website[]>('/api/v1/websites'),
				api.get<TrafficProfile[]>('/api/v1/security/traffic/profiles'),
				api.get<SetupAssessment>('/api/v1/security/setup')
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
			setupAssessment = setupData || { website_ids: [] };
			if (setupTrafficWebsites.length === 0) setupTrafficWebsites = [...(setupAssessment.website_ids || [])];
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
			error = err instanceof Error ? err.message : translate($language, 'sec.err.load');
		} finally {
			loading = false;
		}
	}

	function toggleSetupWebsite(id: string) {
		setupReview = null; setupConfirmed = false;
		setupTrafficWebsites = setupTrafficWebsites.includes(id) ? setupTrafficWebsites.filter((value) => value !== id) : [...setupTrafficWebsites, id];
	}

	function setupRequest() {
		return buildSafeSetupRequest({
			management_cidrs: setupManagementCIDRs.split(/[\s,]+/).filter(Boolean), enable_fail2ban: setupFail2ban,
			malware_mode: setupMalwareMode, schedule_malware: setupSchedule, schedule_time: setupScheduleTime,
			traffic_website_ids: setupTrafficWebsites
		});
	}

	async function reviewSecuritySetup() {
		busy = 'setup-review'; setupConfirmed = false;
		try { setupReview = normalizeSetupReview(await api.post<SetupReview>('/api/v1/security/setup/review', setupRequest())); actionMessage = translate($language, 'sec.setup.reviewGenerated'); }
		catch (err) { toast.error(err instanceof Error ? err.message : translate($language, 'sec.setup.errReview')); }
		finally { busy = ''; }
	}

	async function applySecuritySetup() {
		if (!setupReview || !setupConfirmed) { toast.error(translate($language, 'sec.setup.errConfirmFirst')); return; }
		busy = 'setup-apply'; 
		try {
			const result = await api.post<{ run_id: string; task_id: string }>('/api/v1/security/setup/apply', { request: setupRequest(), review: setupReview, confirm: true });
			currentTaskId = result.task_id; actionMessage = translate($language, 'sec.setup.started');
		} catch (err) { toast.error(err instanceof Error ? err.message : translate($language, 'sec.setup.errApply')); }
		finally { busy = ''; }
	}

	async function resumeSecuritySetup() {
		if (!setupAssessment.latest) return;
		busy = 'setup-resume'; 
		try { const result = await api.post<{ task_id: string }>('/api/v1/security/setup/resume', { run_id: setupAssessment.latest.id }); currentTaskId = result.task_id; actionMessage = translate($language, 'sec.setup.resumed'); }
		catch (err) { toast.error(err instanceof Error ? err.message : translate($language, 'sec.setup.errResume')); }
		finally { busy = ''; }
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
		catch (err) { toast.error(err instanceof Error ? err.message : translate($language, 'tg.errEvidence')); trafficBuckets = []; }
	}

	async function chooseTrafficWebsite(id: string) {
		selectedTrafficWebsite = id;
		syncTrafficForm();
		await loadTrafficBuckets();
	}

	async function applyTrafficGuard() {
		if (!selectedTrafficWebsite) { toast.error(translate($language, 'sec.selectWebsiteFirst')); return; }
		busy = 'traffic-apply'; actionMessage = '';
		try {
			const payload = buildTrafficProfile({
				mode: trafficMode, proxy_mode: trafficProxyMode, proxy_header: trafficProxyHeader,
				proxy_cidrs: trafficProxyCIDRs.split(/[\s,]+/).filter(Boolean),
				requests_per_second: Number(trafficRPS), burst: Number(trafficBurst), connections: Number(trafficConnections)
			});
			if (trafficMode !== 'observe' && !trafficConfirmed) throw new Error(translate($language, 'tg.errConfirmEnforcement'));
			const result = await api.put<{ task_id: string }>(`/api/v1/security/traffic/websites/${selectedTrafficWebsite}`, { ...payload, confirm: trafficMode !== 'observe' && trafficConfirmed });
			currentTaskId = result.task_id; actionMessage = translate($language, 'tg.applied');
		} catch (err) { toast.error(err instanceof Error ? err.message : translate($language, 'tg.errApply')); }
		finally { busy = ''; }
	}

	async function resetTrafficObserve() {
		if (!selectedTrafficWebsite) return;
		busy = 'traffic-observe'; 
		try {
			const result = await api.post<{ task_id: string }>(`/api/v1/security/traffic/websites/${selectedTrafficWebsite}/observe`, {});
			currentTaskId = result.task_id; actionMessage = translate($language, 'tg.observeReset');
		} catch (err) { toast.error(err instanceof Error ? err.message : translate($language, 'tg.errObserveReset')); }
		finally { busy = ''; }
	}

	async function refreshCloudflareCIDRs() {
		busy = 'traffic-cloudflare'; 
		try {
			const result = await api.post<{ task_id: string }>('/api/v1/security/traffic/cloudflare/refresh', {});
			currentTaskId = result.task_id; actionMessage = translate($language, 'tg.cloudflareRefreshStarted');
		} catch (err) { toast.error(err instanceof Error ? err.message : translate($language, 'tg.errCloudflareRefresh')); }
		finally { busy = ''; }
	}

	async function installMalware(mode: 'low_memory' | 'daemon') {
		busy = `malware-install:${mode}`; actionMessage = '';
		try {
			const result = await api.post<{ task_id: string }>('/api/v1/security/malware/install', { mode });
			currentTaskId = result.task_id;
			actionMessage = translate($language, 'mal.installStarted');
		} catch (err) { toast.error(err instanceof Error ? err.message : translate($language, 'mal.errInstall')); }
		finally { busy = ''; }
	}

	async function updateSignatures() {
		busy = 'malware-signatures'; actionMessage = '';
		try {
			const result = await api.post<{ task_id: string }>('/api/v1/security/malware/signatures/update', {});
			currentTaskId = result.task_id; actionMessage = translate($language, 'mal.sigUpdateStarted');
		} catch (err) { toast.error(err instanceof Error ? err.message : translate($language, 'mal.errSigUpdate')); }
		finally { busy = ''; }
	}

	async function startMalwareScan(mode: 'quick' | 'website' | 'full_websites') {
		if (mode === 'website' && !selectedWebsite) { toast.error(translate($language, 'sec.selectWebsiteFirst')); return; }
		busy = `malware-scan:${mode}`; actionMessage = '';
		try {
			const result = await api.post<{ task_id: string }>('/api/v1/security/malware/scans', {
				mode, website_ids: mode === 'website' ? [selectedWebsite] : []
			});
			currentTaskId = result.task_id; actionMessage = translate($language, 'mal.scanStarted');
		} catch (err) { toast.error(err instanceof Error ? err.message : translate($language, 'mal.errScan')); }
		finally { busy = ''; }
	}

	async function saveMalwareSchedule() {
		busy = 'malware-schedule'; 
		try {
			const safe = buildSafeSchedule({ time: scheduleTime });
			await api.put('/api/v1/security/malware/schedules', {
				...safe, id: malwareSchedules[0]?.id || '', enabled: scheduleEnabled
			});
			actionMessage = scheduleEnabled ? translate($language, 'mal.scheduleSaved').replace('{time}', scheduleTime) : translate($language, 'mal.scheduleDisabled');
			await loadData();
		} catch (err) { toast.error(err instanceof Error ? err.message : translate($language, 'mal.errSchedule')); }
		finally { busy = ''; }
	}

	async function configureOnAccess() {
		busy = 'malware-on-access'; 
		try {
			const request = buildOnAccessRequest({ enabled: onAccessEnabled, prevention: preventionEnabled, confirmed: preventionConfirmed });
			const result = await api.put<{ task_id: string }>('/api/v1/security/malware/on-access', request);
			currentTaskId = result.task_id; actionMessage = translate($language, 'mal.onAccessApplied');
		} catch (err) { toast.error(err instanceof Error ? err.message : translate($language, 'mal.errOnAccess')); }
		finally { busy = ''; }
	}

	async function restoreQuarantine(item: QuarantineItem) {
		if (!confirm(translate($language, 'mal.confirmRestore').replace('{path}', item.original_path))) return;
		busy = `restore:${item.id}`; 
		try {
			const result = await api.post<{ task_id: string }>(`/api/v1/security/malware/quarantine/${item.id}/restore`, {});
			currentTaskId = result.task_id; actionMessage = translate($language, 'mal.restoreStarted');
		} catch (err) { toast.error(err instanceof Error ? err.message : translate($language, 'mal.errRestore')); }
		finally { busy = ''; }
	}

	async function markFalsePositive(item: QuarantineItem) {
		if (!confirm(translate($language, 'mal.confirmFalsePositive'))) return;
		busy = `false-positive:${item.id}`; 
		try { await api.post(`/api/v1/security/malware/quarantine/${item.id}/false-positive`, {}); actionMessage = translate($language, 'mal.falsePositiveMarked'); await loadData(); }
		catch (err) { toast.error(err instanceof Error ? err.message : translate($language, 'mal.errFalsePositive')); }
		finally { busy = ''; }
	}

	async function deleteQuarantine(item: QuarantineItem) {
		if (!confirm(translate($language, 'mal.confirmDelete').replace('{id}', item.id))) return;
		busy = `delete:${item.id}`; 
		try { await api.del(`/api/v1/security/malware/quarantine/${item.id}`); actionMessage = translate($language, 'mal.deleted'); await loadData(); }
		catch (err) { toast.error(err instanceof Error ? err.message : translate($language, 'mal.errDelete')); }
		finally { busy = ''; }
	}

	async function installFail2ban() {
		busy = 'install';
		actionMessage = '';
		try {
			const result = await api.post<{ task_id: string }>('/api/v1/security/fail2ban/install', {});
			currentTaskId = result.task_id;
			actionMessage = translate($language, 'f2b.installStarted');
		} catch (err) {
			toast.error(err instanceof Error ? err.message : translate($language, 'f2b.errInstallStart'));
		} finally {
			busy = '';
		}
	}

	async function applySettings() {
		busy = 'apply';
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
			actionMessage = translate($language, 'f2b.settingsApplied');
		} catch (err) {
			toast.error(err instanceof Error ? err.message : translate($language, 'f2b.errSettings'));
		} finally {
			busy = '';
		}
	}

	async function serviceAction(action: 'start' | 'stop' | 'restart') {
		busy = action;
		try {
			await api.post(`/api/v1/security/fail2ban/${action}`, {});
			actionMessage = translate($language, `f2b.actionDone.${action}`);
			await loadData();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : translate($language, `f2b.errAction.${action}`));
		} finally {
			busy = '';
		}
	}

	async function createBan() {
		busy = 'ban';
		try {
			const request = validateBan({ jail: banJail, ip: banIP, duration_seconds: Number(banDuration) });
			await api.post('/api/v1/security/fail2ban/bans', request);
			actionMessage = translate($language, 'f2b.banCreated').replace('{ip}', banIP);
			banIP = '';
			await loadData();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : translate($language, 'f2b.errBan'));
		} finally {
			busy = '';
		}
	}

	async function removeBan(ban: Ban) {
		if (!confirm(translate($language, 'f2b.confirmUnban').replace('{ip}', ban.ip).replace('{jail}', ban.jail))) return;
		busy = `unban:${ban.jail}:${ban.ip}`;
		try {
			await api.del(`/api/v1/security/fail2ban/bans/${encodeURIComponent(ban.ip)}?jail=${encodeURIComponent(ban.jail)}`);
			actionMessage = translate($language, 'f2b.unbanned').replace('{ip}', ban.ip);
			await loadData();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : translate($language, 'f2b.errUnban'));
		} finally {
			busy = '';
		}
	}

	async function transitionEvent(event: SecurityEvent, status: 'acknowledged' | 'resolved' | 'false_positive') {
		busy = `event:${event.id}`;
		try {
			await api.post(`/api/v1/security/events/${event.id}/transition`, { status });
			actionMessage = translate($language, `sec.eventMarked.${status}`);
			await loadData();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : translate($language, 'sec.errEventUpdate'));
		} finally {
			busy = '';
		}
	}

	function taskComplete(task: { status?: string; error?: string }) {
		if (task.status === 'completed') {
			actionMessage = translate($language, 'sec.taskCompleted');
			void loadData();
		} else {
			toast.error(task.error || translate($language, 'sec.taskFailed'));
			void loadData();
		}
	}

	onMount(() => void loadData());
</script>

<svelte:head><title>{translate($language, 'sec.title')} · Jenderal Panel</title></svelte:head>

<div class="space-y-6">
	<div class="flex flex-col gap-3 sm:flex-row sm:items-end sm:justify-between">
		<div>
			<p class="text-xs font-semibold uppercase tracking-[0.18em] text-blue-400">{translate($language, 'sec.kicker')}</p>
			<h2 class="mt-1 text-2xl font-bold text-white">{translate($language, 'sec.title')}</h2>
			<p class="mt-1 max-w-2xl text-sm text-gray-400">{translate($language, 'sec.subtitle')}</p>
		</div>
		<button onclick={loadData} disabled={loading} class="rounded-lg border border-gray-600 px-3 py-2 text-sm text-gray-300 hover:bg-gray-800 disabled:opacity-50">{translate($language, 'sec.refreshStatus')}</button>
	</div>

	<div class="flex gap-1 overflow-x-auto rounded-xl border border-gray-700 bg-gray-900 p-1">
		{#each ['overview', 'setup', 'fail2ban', 'malware', 'traffic', 'events'] as tab}
			<button onclick={() => (activeTab = tab as Tab)} class="min-w-28 rounded-lg px-4 py-2 text-sm font-medium capitalize transition {activeTab === tab ? 'bg-blue-600 text-white' : 'text-gray-400 hover:bg-gray-800 hover:text-white'}">{translate($language, `sec.tab.${tab}`)}</button>
		{/each}
	</div>

	{#if actionMessage}<div class="rounded-lg border border-green-700 bg-green-900/50 p-3 text-sm text-green-300">{actionMessage}</div>{/if}

	<TaskProgress bind:taskId={currentTaskId} storageKey="jenderal_security_fail2ban_task" onComplete={taskComplete} />

	{#if loading}
		<div class="rounded-xl border border-gray-700 bg-gray-800 p-8 text-center text-sm text-gray-400">{translate($language, 'sec.checking')}</div>
	{:else if error}
		<div class="rounded-xl border border-red-700 bg-red-900/40 p-6">
			<p class="font-medium text-red-300">{translate($language, 'sec.errLoadStatus')}</p>
			<p class="mt-1 text-sm text-red-300">{error}</p>
			<button onclick={loadData} class="mt-4 rounded-lg bg-red-600 px-3 py-2 text-sm text-white hover:bg-red-700">{translate($language, 'sec.retry')}</button>
		</div>
	{:else if activeTab === 'setup'}
		<div class="space-y-4">
			<section class="rounded-xl border border-gray-700 bg-gray-800 p-5">
				<div class="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between"><div><h3 class="text-lg font-semibold text-white">{translate($language, 'sec.setup.title')}</h3><p class="mt-1 max-w-3xl text-sm text-gray-400">{translate($language, 'sec.setup.desc')}</p></div>{#if setupAssessment.latest}<span class="rounded-full px-2 py-1 text-xs {setupAssessment.latest.status === 'completed' ? 'bg-green-900 text-green-300' : setupAssessment.latest.status === 'failed' ? 'bg-red-900 text-red-300' : 'bg-yellow-900 text-yellow-300'}">{translate($language, 'sec.setup.latestStatus').replace('{status}', setupAssessment.latest.status)}</span>{/if}</div>
				{#if setupAssessment.latest && setupAssessment.latest.status !== 'completed'}<div class="mt-4 rounded-lg border border-red-700 bg-red-900/25 p-3"><p class="text-sm text-red-300">{setupAssessment.latest.safe_error || translate($language, 'sec.setup.resumeError')}</p><p class="mt-1 text-xs text-gray-400">{translate($language, 'sec.setup.completedSteps').replace('{steps}', setupAssessment.latest.completed_steps.join(', ') || translate($language, 'sec.none'))}</p><button onclick={resumeSecuritySetup} disabled={busy !== '' || !!currentTaskId} class="mt-3 rounded bg-red-600 px-3 py-1.5 text-sm text-white disabled:opacity-50">{translate($language, 'sec.setup.resume')}</button></div>{/if}
			</section>

			<div class="grid gap-4 lg:grid-cols-2">
				<section class="rounded-xl border border-gray-700 bg-gray-800 p-5"><p class="text-xs font-semibold uppercase text-blue-400">{translate($language, 'sec.setup.step1')}</p><h3 class="mt-1 font-semibold text-white">{translate($language, 'sec.setup.step1Title')}</h3><p class="mt-2 text-sm text-gray-400">{translate($language, 'sec.setup.step1Desc')}</p><p class="mt-3 text-xs text-gray-500">{overview.posture?.checked_at ? translate($language, 'sec.setup.lastCheck').replace('{time}', new Date(overview.posture.checked_at).toLocaleString()) : translate($language, 'sec.notAvailable')}</p></section>
				<section class="rounded-xl border border-gray-700 bg-gray-800 p-5"><p class="text-xs font-semibold uppercase text-blue-400">{translate($language, 'sec.setup.step2')}</p><label class="mt-2 block"><span class="text-sm text-gray-300">{translate($language, 'sec.setup.mgmtLabel')}</span><textarea bind:value={setupManagementCIDRs} oninput={() => (setupReview = null)} rows="3" placeholder="203.0.113.10/32" class="mt-1 w-full rounded-lg border border-gray-600 bg-gray-900 px-3 py-2 font-mono text-sm text-white"></textarea></label><p class="mt-2 text-xs text-yellow-300">{translate($language, 'sec.setup.keepConsole')}</p></section>
				<section class="rounded-xl border border-gray-700 bg-gray-800 p-5"><p class="text-xs font-semibold uppercase text-blue-400">{translate($language, 'sec.setup.step3')}</p><label class="mt-3 flex items-start gap-3"><input type="checkbox" bind:checked={setupFail2ban} onchange={() => (setupReview = null)} /><span><span class="block text-sm font-medium text-white">{translate($language, 'sec.setup.sshPreset')}</span><span class="text-xs text-gray-400">{translate($language, 'sec.setup.sshPresetHint')}</span></span></label></section>
				<section class="rounded-xl border border-gray-700 bg-gray-800 p-5"><p class="text-xs font-semibold uppercase text-blue-400">{translate($language, 'sec.setup.step4')}</p><label class="mt-2 block"><span class="text-sm text-gray-300">{translate($language, 'sec.setup.clamavRuntime')}</span><select bind:value={setupMalwareMode} onchange={() => (setupReview = null)} class="mt-1 w-full rounded-lg border border-gray-600 bg-gray-900 px-3 py-2 text-sm text-white"><option value="low_memory">{translate($language, 'sec.setup.lowMemory')}</option><option value="daemon">{translate($language, 'sec.setup.daemonMode')}</option><option value="">{translate($language, 'sec.setup.skipMalware')}</option></select></label><div class="mt-3 flex items-center gap-3"><label class="flex items-center gap-2 text-sm text-gray-300"><input type="checkbox" bind:checked={setupSchedule} disabled={!setupMalwareMode} /> {translate($language, 'sec.setup.dailyQuickScan')}</label><input type="time" bind:value={setupScheduleTime} disabled={!setupSchedule || !setupMalwareMode} class="rounded border border-gray-600 bg-gray-900 px-2 py-1 text-sm text-white" /></div></section>
				<section class="rounded-xl border border-gray-700 bg-gray-800 p-5"><p class="text-xs font-semibold uppercase text-blue-400">{translate($language, 'sec.setup.step5')}</p><p class="mt-1 text-sm text-gray-400">{translate($language, 'sec.setup.step5Desc')}</p><div class="mt-3 max-h-36 space-y-2 overflow-auto">{#each websites as website}<label class="flex items-center gap-2 text-sm text-gray-300"><input type="checkbox" checked={setupTrafficWebsites.includes(website.id)} onchange={() => toggleSetupWebsite(website.id)} /> {website.domain}</label>{/each}</div></section>
				<section class="rounded-xl border border-gray-700 bg-gray-800 p-5"><p class="text-xs font-semibold uppercase text-blue-400">{translate($language, 'sec.setup.step6')}</p><p class="mt-2 text-sm text-gray-400">{translate($language, 'sec.setup.step6Desc')}</p><p class="mt-2 text-xs text-yellow-300">{translate($language, 'sec.setup.step6Warn')}</p></section>
				<section class="rounded-xl border border-gray-700 bg-gray-800 p-5"><p class="text-xs font-semibold uppercase text-blue-400">{translate($language, 'sec.setup.step7')}</p><p class="mt-2 text-sm text-gray-400">{translate($language, 'sec.setup.step7Desc')}</p><a href="/notifications" class="mt-3 inline-block text-sm text-blue-300 hover:text-blue-200">{translate($language, 'sec.setup.openNotifications')}</a></section>
				<section class="rounded-xl border border-gray-700 bg-gray-800 p-5"><p class="text-xs font-semibold uppercase text-blue-400">{translate($language, 'sec.setup.step8')}</p><button onclick={reviewSecuritySetup} disabled={busy !== '' || !!currentTaskId} class="mt-3 rounded-lg border border-blue-600 px-4 py-2 text-sm text-blue-300 disabled:opacity-50">{translate($language, 'sec.setup.generateReview')}</button>{#if setupReview}<div class="mt-4 space-y-3"><div><p class="text-xs uppercase text-gray-500">{translate($language, 'sec.setup.mutations')}</p><ol class="mt-1 list-inside list-decimal text-sm text-gray-300">{#each setupReview.mutations as mutation}<li>{mutation}</li>{/each}</ol></div><div><p class="text-xs uppercase text-yellow-500">{translate($language, 'sec.setup.warnings')}</p><ul class="mt-1 list-inside list-disc text-sm text-yellow-300">{#each setupReview.warnings as warning}<li>{warning}</li>{/each}</ul></div><label class="flex items-start gap-2 rounded border border-yellow-700 bg-yellow-900/20 p-2 text-xs text-yellow-200"><input class="mt-0.5" type="checkbox" bind:checked={setupConfirmed} /> {translate($language, 'sec.setup.confirmChanges')}</label><button onclick={applySecuritySetup} disabled={!setupConfirmed || busy !== '' || !!currentTaskId} class="rounded-lg bg-blue-600 px-4 py-2 text-sm text-white disabled:opacity-50">{translate($language, 'sec.setup.apply')}</button></div>{/if}</section>
			</div>
		</div>
	{:else if activeTab === 'overview'}
		{@const tone = conditionTone(overview.condition)}
		<div class="grid gap-4 lg:grid-cols-[1.1fr_1.9fr]">
			<section class="rounded-xl border p-5 {tone === 'critical' ? 'border-red-700 bg-red-900/30' : tone === 'warning' ? 'border-yellow-700 bg-yellow-900/25' : 'border-green-700 bg-green-900/20'}">
				<p class="text-xs font-semibold uppercase tracking-wider text-gray-400">{translate($language, 'sec.overview.condition')}</p>
				<h3 class="mt-2 text-2xl font-semibold text-white">{overview.condition === 'needs_attention' ? translate($language, 'sec.cond.needs_attention') : overview.condition === 'good' ? translate($language, 'sec.cond.good') : overview.condition === 'critical' ? translate($language, 'sec.cond.critical') : translate($language, 'sec.cond.unknown')}</h3>
				<ul class="mt-4 space-y-2 text-sm text-gray-300">
					{#each overview.reasons as reason}<li class="flex gap-2"><span aria-hidden="true">•</span><span>{reason}</span></li>{/each}
				</ul>
				<p class="mt-4 text-xs text-gray-400">{translate($language, 'sec.overview.disclaimer')}</p>
			</section>
			<section class="rounded-xl border border-gray-700 bg-gray-800 p-5">
				<div class="flex items-center justify-between"><h3 class="font-semibold text-white">{translate($language, 'sec.overview.components')}</h3><span class="text-sm text-gray-400">{translate($language, 'sec.overview.activeEvents').replace('{count}', String(overview.open_events))}</span></div>
				{#if overview.components.length === 0}<p class="mt-4 text-sm text-gray-400">{translate($language, 'sec.overview.noComponents')}</p>{:else}
					<div class="mt-4 grid gap-3 sm:grid-cols-2">
						{#each overview.components as component}
							<div class="rounded-lg border border-gray-700 bg-gray-900/60 p-4">
								<div class="flex items-center justify-between gap-2"><span class="font-medium capitalize text-white">{component.name}</span><span class="rounded-full px-2 py-0.5 text-xs {component.healthy ? 'bg-green-900 text-green-300' : component.installed ? 'bg-yellow-900 text-yellow-300' : 'bg-gray-700 text-gray-300'}">{component.state.replaceAll('_', ' ')}</span></div>
								<p class="mt-2 text-sm text-gray-400">{component.message || (component.installed ? translate($language, 'sec.overview.statusChecked') : translate($language, 'sec.overview.optionalMissing'))}</p>
							</div>
						{/each}
					</div>
				{/if}
			</section>
		</div>
		<section class="rounded-xl border border-gray-700 bg-gray-800 p-5">
			<div class="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between"><div><h3 class="font-semibold text-white">{translate($language, 'sec.overview.postureTitle')}</h3><p class="mt-1 text-sm text-gray-400">{translate($language, 'sec.overview.postureDesc')}</p></div><span class="text-xs text-gray-500">{overview.posture?.checked_at ? new Date(overview.posture.checked_at).toLocaleString() : translate($language, 'sec.overview.notChecked')}</span></div>
			{#if !overview.posture?.findings?.length}<p class="mt-4 text-sm text-green-300">{translate($language, 'sec.overview.noFindings')}</p>{:else}<div class="mt-4 grid gap-3 md:grid-cols-2">{#each overview.posture.findings as finding}<article class="rounded-lg border border-gray-700 bg-gray-900/50 p-4"><div class="flex items-center justify-between gap-2"><span class="font-medium capitalize text-white">{finding.component.replaceAll('_', ' ')}</span><span class="rounded-full px-2 py-0.5 text-xs uppercase {finding.severity === 'critical' ? 'bg-red-900 text-red-300' : finding.severity === 'medium' || finding.severity === 'high' ? 'bg-yellow-900 text-yellow-300' : 'bg-gray-700 text-gray-300'}">{finding.severity}</span></div><p class="mt-2 text-sm text-gray-200">{finding.summary}</p><p class="mt-1 text-xs text-gray-400">{finding.remediation}</p></article>{/each}</div>{/if}
		</section>
	{:else if activeTab === 'fail2ban'}
		<div class="space-y-4">
			<section class="rounded-xl border border-gray-700 bg-gray-800 p-5">
				<div class="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
					<div><div class="flex items-center gap-2"><h3 class="text-lg font-semibold text-white">Fail2ban</h3><span class="rounded-full px-2 py-0.5 text-xs {fail2ban.healthy ? 'bg-green-900 text-green-300' : 'bg-gray-700 text-gray-300'}">{fail2ban.state.replaceAll('_', ' ')}</span></div><p class="mt-1 text-sm text-gray-400">{fail2ban.message || translate($language, 'f2b.desc')}</p></div>
					{#if !fail2ban.installed}<button onclick={installFail2ban} disabled={busy !== '' || !!currentTaskId} class="rounded-lg bg-blue-600 px-4 py-2 text-sm font-medium text-white hover:bg-blue-700 disabled:opacity-50">{busy === 'install' ? translate($language, 'f2b.starting') : translate($language, 'f2b.install')}</button>{:else}<div class="flex flex-wrap gap-2"><button onclick={() => serviceAction(fail2ban.running ? 'restart' : 'start')} disabled={busy !== ''} class="rounded-lg bg-blue-600 px-3 py-2 text-sm text-white disabled:opacity-50">{fail2ban.running ? translate($language, 'f2b.restart') : translate($language, 'f2b.start')}</button>{#if fail2ban.running}<button onclick={() => serviceAction('stop')} disabled={busy !== ''} class="rounded-lg border border-red-700 px-3 py-2 text-sm text-red-300 disabled:opacity-50">{translate($language, 'f2b.stop')}</button>{/if}</div>{/if}
				</div>
				{#if fail2ban.installed}<div class="mt-4 grid gap-2 text-sm text-gray-400 sm:grid-cols-3"><span>{translate($language, 'f2b.version').replace('{value}', fail2ban.version || translate($language, 'sec.unknownValue'))}</span><span>{translate($language, 'f2b.sshPort').replace('{value}', String(fail2ban.ssh_port || translate($language, 'sec.unknownValue')))}</span><span>{translate($language, 'f2b.service').replace('{value}', fail2ban.enabled ? translate($language, 'f2b.serviceEnabled') : translate($language, 'f2b.serviceNotEnabled'))}</span></div>{/if}
			</section>

			{#if fail2ban.installed}
				<section class="rounded-xl border border-gray-700 bg-gray-800 p-5">
					<div class="flex items-center justify-between"><div><h3 class="font-semibold text-white">{translate($language, 'f2b.config')}</h3><p class="mt-1 text-sm text-gray-400">{translate($language, 'f2b.configDesc')}</p></div><div class="rounded-lg border border-gray-700 bg-gray-900 p-1"><button onclick={() => (editMode = 'simple')} class="rounded px-3 py-1.5 text-sm {editMode === 'simple' ? 'bg-blue-600 text-white' : 'text-gray-400'}">{translate($language, 'sec.simple')}</button><button onclick={() => (editMode = 'advanced')} class="rounded px-3 py-1.5 text-sm {editMode === 'advanced' ? 'bg-blue-600 text-white' : 'text-gray-400'}">{translate($language, 'sec.advanced')}</button></div></div>
					<div class="mt-5 grid gap-4 sm:grid-cols-2">
						<label class="sm:col-span-2"><span class="text-sm text-gray-300">{translate($language, 'f2b.mgmtLabel')}</span><textarea bind:value={managementNetworks} rows="3" placeholder="203.0.113.8/32" class="mt-1 w-full rounded-lg border border-gray-600 bg-gray-900 px-3 py-2 font-mono text-sm text-white"></textarea><span class="mt-1 block text-xs text-gray-400">{translate($language, 'f2b.mgmtHint')}</span></label>
						{#if settings.sshd_enabled && managementNetworks.trim() === ''}<div class="sm:col-span-2 rounded-lg border border-yellow-700 bg-yellow-900/30 p-3 text-sm text-yellow-300"><strong>{translate($language, 'f2b.lockoutWarning')}</strong> {translate($language, 'f2b.lockoutDesc')}</div>{/if}
						<label class="flex items-center gap-3 rounded-lg border border-gray-700 bg-gray-900/50 p-3 sm:col-span-2"><input type="checkbox" bind:checked={settings.sshd_enabled} class="h-4 w-4" /><span><span class="block text-sm font-medium text-white">{translate($language, 'f2b.protectSsh')}</span><span class="text-xs text-gray-400">{translate($language, 'f2b.protectSshHint')}</span></span></label>
						{#if editMode === 'advanced'}
							<label><span class="text-sm text-gray-300">{translate($language, 'f2b.maxRetry')}</span><input type="number" min="1" max="20" bind:value={settings.max_retry} class="mt-1 w-full rounded-lg border border-gray-600 bg-gray-900 px-3 py-2 text-white" /></label>
							<label><span class="text-sm text-gray-300">{translate($language, 'f2b.findTime')}</span><input type="number" min="60" max="86400" bind:value={settings.find_time_seconds} class="mt-1 w-full rounded-lg border border-gray-600 bg-gray-900 px-3 py-2 text-white" /></label>
							<label><span class="text-sm text-gray-300">{translate($language, 'f2b.banTime')}</span><input type="number" min="60" max="604800" bind:value={settings.ban_time_seconds} class="mt-1 w-full rounded-lg border border-gray-600 bg-gray-900 px-3 py-2 text-white" /></label>
						{:else}<div class="sm:col-span-2 grid gap-2 rounded-lg border border-gray-700 bg-gray-900/50 p-4 text-sm text-gray-300 sm:grid-cols-3"><span>{translate($language, 'f2b.presetRetry')}</span><span>{translate($language, 'f2b.presetWindow')}</span><span>{translate($language, 'f2b.presetBan')}</span></div>{/if}
					</div>
					<button onclick={applySettings} disabled={busy !== '' || !!currentTaskId} class="mt-5 rounded-lg bg-blue-600 px-4 py-2 text-sm font-medium text-white hover:bg-blue-700 disabled:opacity-50">{translate($language, 'sec.validateApply')}</button>
				</section>

				<section class="rounded-xl border border-gray-700 bg-gray-800 p-5"><h3 class="font-semibold text-white">{translate($language, 'f2b.activeJails')}</h3>{#if fail2ban.jails.length === 0}<p class="mt-3 text-sm text-gray-400">{translate($language, 'f2b.noJails')}</p>{:else}<div class="mt-4 grid gap-3 md:grid-cols-2">{#each fail2ban.jails as jail}<div class="rounded-lg border border-gray-700 bg-gray-900/60 p-4"><div class="flex justify-between"><span class="font-medium text-white">{jail.name}</span><span class="text-xs text-gray-400">{translate($language, 'f2b.bannedCount').replace('{count}', String(jail.currently_banned))}</span></div><div class="mt-3 grid grid-cols-2 gap-2 text-xs text-gray-400"><span>{translate($language, 'f2b.failedNow').replace('{value}', String(jail.currently_failed))}</span><span>{translate($language, 'f2b.failedTotal').replace('{value}', String(jail.total_failed))}</span><span>{translate($language, 'f2b.filter').replace('{value}', jail.filter_available ? translate($language, 'f2b.ready') : translate($language, 'f2b.missing'))}</span><span>{translate($language, 'f2b.logSource').replace('{value}', jail.source_available ? translate($language, 'f2b.ready') : translate($language, 'f2b.missing'))}</span></div></div>{/each}</div>{/if}</section>

				<section class="rounded-xl border border-gray-700 bg-gray-800 p-5"><h3 class="font-semibold text-white">{translate($language, 'f2b.manualBan')}</h3><div class="mt-4 grid gap-3 sm:grid-cols-4"><select bind:value={banJail} class="rounded-lg border border-gray-600 bg-gray-900 px-3 py-2 text-sm text-white"><option value="">{translate($language, 'f2b.selectJail')}</option>{#each fail2ban.jails as jail}<option value={jail.name}>{jail.name}</option>{/each}</select><input bind:value={banIP} placeholder="203.0.113.7" class="rounded-lg border border-gray-600 bg-gray-900 px-3 py-2 font-mono text-sm text-white sm:col-span-2" /><input type="number" min="60" max="604800" bind:value={banDuration} class="rounded-lg border border-gray-600 bg-gray-900 px-3 py-2 text-sm text-white" /></div><button onclick={createBan} disabled={busy !== '' || fail2ban.jails.length === 0} class="mt-3 rounded-lg bg-red-600 px-4 py-2 text-sm text-white disabled:opacity-50">{translate($language, 'f2b.banTemp')}</button>
					{#if bans.length === 0}<p class="mt-5 text-sm text-gray-400">{translate($language, 'f2b.noBans')}</p>{:else}<div class="mt-5 overflow-x-auto"><table class="w-full text-left text-sm"><thead class="text-xs uppercase text-gray-400"><tr><th class="pb-2">{translate($language, 'f2b.thAddress')}</th><th class="pb-2">{translate($language, 'f2b.thJail')}</th><th class="pb-2">{translate($language, 'f2b.thExpiry')}</th><th class="pb-2 text-right">{translate($language, 'f2b.thAction')}</th></tr></thead><tbody class="divide-y divide-gray-700">{#each bans as ban}<tr><td class="py-3 font-mono text-white">{ban.ip}</td><td class="py-3 text-gray-300">{ban.jail}</td><td class="py-3 text-gray-400">{formatBanExpiry(ban.expires_at)}</td><td class="py-3 text-right"><button onclick={() => removeBan(ban)} disabled={busy !== ''} class="text-red-400 hover:text-red-300 disabled:opacity-50">{translate($language, 'f2b.unban')}</button></td></tr>{/each}</tbody></table></div>{/if}
				</section>
			{/if}
		</div>
	{:else if activeTab === 'malware'}
		<div class="space-y-4">
			<section class="rounded-xl border border-gray-700 bg-gray-800 p-5">
				<div class="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
					<div>
						<div class="flex flex-wrap items-center gap-2">
							<h3 class="text-lg font-semibold text-white">{translate($language, 'mal.title')}</h3>
							<span class="rounded-full px-2 py-0.5 text-xs {malware.healthy ? 'bg-green-900 text-green-300' : malware.installed ? 'bg-yellow-900 text-yellow-300' : 'bg-gray-700 text-gray-300'}">{malware.state.replaceAll('_', ' ')}</span>
						</div>
						<p class="mt-1 max-w-2xl text-sm text-gray-400">{malware.message || translate($language, 'mal.desc')}</p>
					</div>
					{#if !malware.installed}
						<div class="flex flex-wrap gap-2">
							<button onclick={() => installMalware('low_memory')} disabled={busy !== '' || !!currentTaskId} class="rounded-lg bg-blue-600 px-4 py-2 text-sm font-medium text-white hover:bg-blue-700 disabled:opacity-50">{translate($language, 'mal.installLowMemory')}</button>
							<button onclick={() => installMalware('daemon')} disabled={!malware.daemon_supported || busy !== '' || !!currentTaskId} title={malware.daemon_supported ? translate($language, 'mal.daemonTooltip') : translate($language, 'mal.daemonReq')} class="rounded-lg border border-gray-600 px-4 py-2 text-sm text-gray-300 hover:bg-gray-900 disabled:opacity-50">{translate($language, 'mal.installDaemon')}</button>
						</div>
					{:else}
						<button onclick={updateSignatures} disabled={busy !== '' || !!currentTaskId} class="rounded-lg border border-gray-600 px-4 py-2 text-sm text-gray-300 hover:bg-gray-900 disabled:opacity-50">{translate($language, 'mal.updateSignatures')}</button>
					{/if}
				</div>
				{#if malware.installed}
					<div class="mt-5 grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
						<div class="rounded-lg border border-gray-700 bg-gray-900/60 p-3"><p class="text-xs uppercase text-gray-500">{translate($language, 'mal.engine')}</p><p class="mt-1 font-medium text-white">{malware.engine || 'clamscan'}</p><p class="text-xs text-gray-400">{malware.version || translate($language, 'mal.versionUnknown')}</p></div>
						<div class="rounded-lg border border-gray-700 bg-gray-900/60 p-3"><p class="text-xs uppercase text-gray-500">{translate($language, 'mal.signatures')}</p><p class="mt-1 font-medium text-white">{malware.signature_version || translate($language, 'sec.unknownValue')}</p><p class="text-xs {malware.signature_fresh ? 'text-green-300' : 'text-yellow-300'}">{formatSignatureAge(malware.signature_updated_at)}</p></div>
						<div class="rounded-lg border border-gray-700 bg-gray-900/60 p-3"><p class="text-xs uppercase text-gray-500">{translate($language, 'mal.updater')}</p><p class="mt-1 font-medium text-white">{malware.updater_running ? translate($language, 'mal.running') : translate($language, 'mal.needsAttention')}</p></div>
						<div class="rounded-lg border border-gray-700 bg-gray-900/60 p-3"><p class="text-xs uppercase text-gray-500">{translate($language, 'mal.quarantine')}</p><p class="mt-1 font-medium text-white">{translate($language, 'mal.retained').replace('{count}', String(quarantine.filter((item) => item.status === 'quarantined' || item.status === 'false_positive').length))}</p><p class="text-xs text-gray-400">{translate($language, 'mal.neverAutoDeleted')}</p></div>
					</div>
				{/if}
			</section>

			{#if malware.installed}
				<section class="rounded-xl border border-gray-700 bg-gray-800 p-5">
					<div class="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
						<div><h3 class="font-semibold text-white">{translate($language, 'mal.runScan')}</h3><p class="mt-1 text-sm text-gray-400">{translate($language, 'mal.scanDesc')}</p></div>
						<div class="flex gap-1 rounded-lg border border-gray-700 bg-gray-900 p-1"><button onclick={() => (malwareMode = 'simple')} class="rounded px-3 py-1.5 text-sm {malwareMode === 'simple' ? 'bg-blue-600 text-white' : 'text-gray-400'}">{translate($language, 'sec.simple')}</button><button onclick={() => (malwareMode = 'advanced')} class="rounded px-3 py-1.5 text-sm {malwareMode === 'advanced' ? 'bg-blue-600 text-white' : 'text-gray-400'}">{translate($language, 'sec.advanced')}</button></div>
					</div>
					<div class="mt-5 flex flex-wrap gap-3">
						<button onclick={() => startMalwareScan('quick')} disabled={busy !== '' || !!currentTaskId} class="rounded-lg bg-blue-600 px-4 py-2 text-sm font-medium text-white disabled:opacity-50">{translate($language, 'mal.quickScan')}</button>
						<button onclick={() => startMalwareScan('full_websites')} disabled={busy !== '' || !!currentTaskId} class="rounded-lg border border-gray-600 px-4 py-2 text-sm text-gray-300 hover:bg-gray-900 disabled:opacity-50">{translate($language, 'mal.fullScan')}</button>
					</div>
					<div class="mt-4 flex flex-col gap-2 sm:flex-row">
						<select bind:value={selectedWebsite} class="min-w-64 rounded-lg border border-gray-600 bg-gray-900 px-3 py-2 text-sm text-white"><option value="">{translate($language, 'sec.selectWebsite')}</option>{#each websites as website}<option value={website.id}>{website.domain}</option>{/each}</select>
						<button onclick={() => startMalwareScan('website')} disabled={!selectedWebsite || busy !== '' || !!currentTaskId} class="rounded-lg border border-blue-600 px-4 py-2 text-sm text-blue-300 disabled:opacity-50">{translate($language, 'mal.scanSelected')}</button>
					</div>
					{#if malwareMode === 'advanced'}
						<div class="mt-5 grid gap-3 rounded-lg border border-gray-700 bg-gray-900/50 p-4 text-sm text-gray-300 sm:grid-cols-3"><span>{translate($language, 'mal.maxFiles')}</span><span>{translate($language, 'mal.maxFileSize')}</span><span>{translate($language, 'mal.archiveDepth')}</span></div>
					{/if}
				</section>

				<div class="grid gap-4 lg:grid-cols-2">
					<section class="rounded-xl border border-gray-700 bg-gray-800 p-5">
						<h3 class="font-semibold text-white">{translate($language, 'mal.dailySchedule')}</h3><p class="mt-1 text-sm text-gray-400">{translate($language, 'mal.scheduleDesc')}</p>
						<div class="mt-4 flex flex-wrap items-end gap-3"><label class="flex items-center gap-2 rounded-lg border border-gray-700 bg-gray-900/50 px-3 py-2 text-sm text-gray-300"><input type="checkbox" bind:checked={scheduleEnabled} /> {translate($language, 'mal.enabled')}</label><label><span class="block text-xs text-gray-400">{translate($language, 'mal.serverTime')}</span><input type="time" bind:value={scheduleTime} class="mt-1 rounded-lg border border-gray-600 bg-gray-900 px-3 py-2 text-sm text-white" /></label><button onclick={saveMalwareSchedule} disabled={busy !== ''} class="rounded-lg bg-blue-600 px-4 py-2 text-sm text-white disabled:opacity-50">{translate($language, 'mal.saveSchedule')}</button></div>
					</section>
					<section class="rounded-xl border border-gray-700 bg-gray-800 p-5">
						<h3 class="font-semibold text-white">{translate($language, 'mal.onAccessTitle')} <span class="ml-1 text-xs font-normal text-yellow-300">{translate($language, 'sec.advanced')}</span></h3><p class="mt-1 text-sm text-gray-400">{translate($language, 'mal.onAccessDesc')}</p>
						<p class="mt-2 text-xs text-gray-500">{malware.on_access.message}</p>
						<div class="mt-4 space-y-3"><label class="flex items-center gap-2 text-sm text-gray-300"><input type="checkbox" bind:checked={onAccessEnabled} disabled={!malware.on_access.available && !malware.on_access.enabled} /> {translate($language, 'mal.enableNotifyOnly')}</label><label class="flex items-center gap-2 text-sm text-gray-300"><input type="checkbox" bind:checked={preventionEnabled} disabled={!onAccessEnabled || !malware.on_access.prevention_supported} /> {translate($language, 'mal.blockAccess')}</label>{#if preventionEnabled}<label class="flex items-start gap-2 rounded-lg border border-yellow-700 bg-yellow-900/25 p-3 text-sm text-yellow-300"><input class="mt-1" type="checkbox" bind:checked={preventionConfirmed} /><span>{translate($language, 'mal.preventionConfirm')}</span></label>{/if}</div>
						<button onclick={configureOnAccess} disabled={busy !== '' || (!malware.on_access.available && !malware.on_access.enabled)} class="mt-4 rounded-lg border border-blue-600 px-4 py-2 text-sm text-blue-300 disabled:opacity-50">{translate($language, 'sec.validateApply')}</button>
					</section>
				</div>

				<section class="rounded-xl border border-gray-700 bg-gray-800 p-5">
					<div class="flex items-center justify-between"><div><h3 class="font-semibold text-white">{translate($language, 'mal.scanHistory')}</h3><p class="mt-1 text-sm text-gray-400">{translate($language, 'mal.historyDesc')}</p></div><span class="text-sm text-gray-400">{translate($language, 'sec.shownCount').replace('{count}', String(malwareScans.length))}</span></div>
					{#if malwareScans.length === 0}<p class="mt-4 text-sm text-gray-400">{translate($language, 'mal.noScans')}</p>{:else}<div class="mt-4 overflow-x-auto"><table class="w-full text-left text-sm"><thead class="text-xs uppercase text-gray-400"><tr><th class="pb-2">{translate($language, 'mal.thMode')}</th><th class="pb-2">{translate($language, 'mal.thStatus')}</th><th class="pb-2">{translate($language, 'mal.thFiles')}</th><th class="pb-2">{translate($language, 'mal.thFindings')}</th><th class="pb-2">{translate($language, 'mal.thStarted')}</th></tr></thead><tbody class="divide-y divide-gray-700">{#each malwareScans as scan}<tr><td class="py-3 text-white">{scan.mode.replaceAll('_', ' ')}</td><td class="py-3"><span class="rounded-full px-2 py-0.5 text-xs {scan.status === 'completed' ? 'bg-green-900 text-green-300' : scan.status === 'failed' ? 'bg-red-900 text-red-300' : 'bg-yellow-900 text-yellow-300'}">{scan.status}</span>{#if scan.error}<p class="mt-1 max-w-lg text-xs text-red-300">{scan.error}</p>{/if}</td><td class="py-3 text-gray-300">{scan.files_scanned}</td><td class="py-3 text-gray-300">{scan.findings_count}</td><td class="py-3 text-gray-400">{scan.started_at ? new Date(scan.started_at).toLocaleString() : '—'}</td></tr>{/each}</tbody></table></div>{/if}
				</section>

				<section class="rounded-xl border border-gray-700 bg-gray-800 p-5">
					<div class="flex items-center justify-between"><div><h3 class="font-semibold text-white">{translate($language, 'mal.quarantine')}</h3><p class="mt-1 text-sm text-gray-400">{translate($language, 'mal.quarantineDesc')}</p></div><span class="text-sm text-gray-400">{translate($language, 'mal.itemCount').replace('{count}', String(quarantine.length))}</span></div>
					{#if quarantine.length === 0}<p class="mt-4 text-sm text-gray-400">{translate($language, 'mal.noQuarantine')}</p>{:else}<div class="mt-4 space-y-3">{#each quarantine as item}<article class="rounded-lg border border-gray-700 bg-gray-900/60 p-4"><div class="flex flex-col gap-3 lg:flex-row lg:items-start lg:justify-between"><div class="min-w-0"><div class="flex flex-wrap items-center gap-2"><span class="rounded-full bg-red-900 px-2 py-0.5 text-xs text-red-300">{item.signature}</span><span class="text-xs text-gray-400">{item.status.replaceAll('_', ' ')}</span></div><p class="mt-2 break-all font-mono text-sm text-white">{item.original_path}</p><p class="mt-1 break-all font-mono text-xs text-gray-500">SHA-256 {item.sha256}</p><p class="mt-1 text-xs text-gray-400">{translate($language, 'mal.detectedLine').replace('{time}', new Date(item.detected_at).toLocaleString()).replace('{size}', String(item.size_bytes))}</p></div><div class="flex flex-wrap gap-2">{#if item.status === 'quarantined' || item.status === 'false_positive'}<a download href={`/api/v1/security/malware/quarantine/${item.id}/download`} class="rounded border border-gray-600 px-3 py-1.5 text-xs text-gray-300">{translate($language, 'mal.download')}</a><button onclick={() => restoreQuarantine(item)} disabled={busy !== ''} class="rounded border border-green-700 px-3 py-1.5 text-xs text-green-300 disabled:opacity-50">{translate($language, 'mal.restore')}</button>{#if item.status === 'quarantined'}<button onclick={() => markFalsePositive(item)} disabled={busy !== ''} class="rounded border border-gray-600 px-3 py-1.5 text-xs text-gray-300 disabled:opacity-50">{translate($language, 'sec.falsePositive')}</button>{/if}<button onclick={() => deleteQuarantine(item)} disabled={busy !== ''} class="rounded bg-red-700 px-3 py-1.5 text-xs text-white disabled:opacity-50">{translate($language, 'mal.deletePermanently')}</button>{/if}</div></div></article>{/each}</div>{/if}
				</section>
			{/if}
		</div>
	{:else if activeTab === 'traffic'}
		<div class="space-y-4">
			<section class="rounded-xl border border-gray-700 bg-gray-800 p-5">
				<div class="flex flex-col gap-4 lg:flex-row lg:items-start lg:justify-between">
					<div>
						<div class="flex flex-wrap items-center gap-2"><h3 class="text-lg font-semibold text-white">{translate($language, 'tg.title')}</h3><span class="rounded-full bg-blue-900 px-2 py-0.5 text-xs text-blue-300">{translate($language, 'tg.httpLayer')}</span><span class="rounded-full px-2 py-0.5 text-xs {trafficCondition === 'Critical' ? 'bg-red-900 text-red-300' : trafficCondition === 'High' || trafficCondition === 'Warning' ? 'bg-yellow-900 text-yellow-300' : 'bg-green-900 text-green-300'}">{translate($language, `tg.cond.${trafficCondition.toLowerCase()}`)}</span></div>
						<p class="mt-1 max-w-3xl text-sm text-gray-400">{translate($language, 'tg.desc')}</p>
					</div>
					<select value={selectedTrafficWebsite} onchange={(event) => chooseTrafficWebsite(event.currentTarget.value)} class="min-w-64 rounded-lg border border-gray-600 bg-gray-900 px-3 py-2 text-sm text-white">
						<option value="">{translate($language, 'sec.selectWebsite')}</option>{#each websites as website}<option value={website.id}>{website.domain}</option>{/each}
					</select>
				</div>
				<div class="mt-5 rounded-lg border border-blue-800 bg-blue-950/40 p-4">
					<div class="flex items-center justify-between text-sm"><span class="font-medium text-blue-200">{translate($language, 'tg.observation')}</span><span class="text-blue-300">{trafficObservation}%</span></div>
					<div class="mt-2 h-2 overflow-hidden rounded-full bg-gray-700"><div class="h-full rounded-full bg-blue-500 transition-all" style={`width: ${trafficObservation}%`}></div></div>
					<p class="mt-2 text-xs text-gray-400">{translate($language, 'tg.observationNote')}</p>
				</div>
			</section>

			<div class="grid gap-3 sm:grid-cols-2 lg:grid-cols-5">
				<div class="rounded-xl border border-gray-700 bg-gray-800 p-4"><p class="text-xs uppercase text-gray-500">{translate($language, 'tg.requests24h')}</p><p class="mt-1 text-xl font-semibold text-white">{trafficSummary.requests.toLocaleString()}</p></div>
				<div class="rounded-xl border border-gray-700 bg-gray-800 p-4"><p class="text-xs uppercase text-gray-500">{translate($language, 'tg.peakRps')}</p><p class="mt-1 text-xl font-semibold text-white">{trafficSummary.peakRPS}</p></div>
				<div class="rounded-xl border border-gray-700 bg-gray-800 p-4"><p class="text-xs uppercase text-gray-500">4xx</p><p class="mt-1 text-xl font-semibold text-yellow-300">{trafficSummary.status4xx}</p></div>
				<div class="rounded-xl border border-gray-700 bg-gray-800 p-4"><p class="text-xs uppercase text-gray-500">5xx</p><p class="mt-1 text-xl font-semibold text-red-300">{trafficSummary.status5xx}</p></div>
				<div class="rounded-xl border border-gray-700 bg-gray-800 p-4"><p class="text-xs uppercase text-gray-500">HTTP 429</p><p class="mt-1 text-xl font-semibold text-blue-300">{trafficSummary.status429}</p></div>
			</div>

			<section class="rounded-xl border border-gray-700 bg-gray-800 p-5">
				<div class="flex items-center justify-between"><div><h3 class="font-semibold text-white">{translate($language, 'tg.activity')}</h3><p class="mt-1 text-sm text-gray-400">{translate($language, 'tg.activityDesc')}</p></div><span class="text-xs text-gray-500">{translate($language, 'tg.minutesShown').replace('{count}', String(Math.min(30, trafficBuckets.length)))}</span></div>
				{#if trafficBuckets.length === 0}<p class="mt-6 text-sm text-gray-400">{translate($language, 'tg.noBuckets')}</p>{:else}
					<div class="mt-5 flex h-32 items-end gap-1 overflow-hidden" aria-label={translate($language, 'tg.chartAria')}>
						{#each trafficBuckets.slice(-30) as bucket}<div title={translate($language, 'tg.bucketTitle').replace('{time}', new Date(bucket.bucket_at).toLocaleTimeString()).replace('{count}', String(bucket.requests))} class="min-w-1 flex-1 rounded-t bg-blue-500/80" style={`height: ${Math.max(3, (bucket.requests / trafficPeak) * 100)}%`}></div>{/each}
					</div>
				{/if}
			</section>

			<div class="grid gap-4 lg:grid-cols-2">
				<section class="rounded-xl border border-gray-700 bg-gray-800 p-5"><h3 class="font-semibold text-white">{translate($language, 'tg.topIps')}</h3>{#if trafficSummary.topIPs.length === 0}<p class="mt-4 text-sm text-gray-400">{translate($language, 'tg.noIpEvidence')}</p>{:else}<div class="mt-4 space-y-2">{#each trafficSummary.topIPs.slice(0, 10) as [ip, count]}<div class="flex items-center justify-between rounded border border-gray-700 bg-gray-900/50 px-3 py-2"><code class="text-sm text-gray-200">{ip}</code><span class="text-xs text-gray-400">{translate($language, 'tg.requestCount').replace('{count}', String(count))}</span></div>{/each}</div>{/if}</section>
				<section class="rounded-xl border border-gray-700 bg-gray-800 p-5"><h3 class="font-semibold text-white">{translate($language, 'tg.topPaths')}</h3>{#if topTrafficPaths.length === 0}<p class="mt-4 text-sm text-gray-400">{translate($language, 'tg.noPathEvidence')}</p>{:else}<div class="mt-4 space-y-2">{#each topTrafficPaths as [path, count]}<div class="flex items-center justify-between gap-3 rounded border border-gray-700 bg-gray-900/50 px-3 py-2"><code class="truncate text-sm text-gray-200">{path}</code><span class="shrink-0 text-xs text-gray-400">{count}</span></div>{/each}</div>{/if}</section>
			</div>

			<section class="rounded-xl border border-gray-700 bg-gray-800 p-5">
				<div><h3 class="font-semibold text-white">{translate($language, 'tg.profile')}</h3><p class="mt-1 text-sm text-gray-400">{translate($language, 'tg.profileDesc1')}<code>nginx -t</code>{translate($language, 'tg.profileDesc2')}</p></div>
				<div class="mt-5 grid gap-4 md:grid-cols-2">
					<label><span class="text-sm text-gray-300">{translate($language, 'tg.mode')}</span><select bind:value={trafficMode} class="mt-1 w-full rounded-lg border border-gray-600 bg-gray-900 px-3 py-2 text-sm text-white"><option value="observe">{translate($language, 'tg.modeObserve')}</option><option value="balanced" disabled={trafficObservation < 100}>{translate($language, 'tg.modeBalanced')}</option><option value="strict" disabled={trafficObservation < 100}>{translate($language, 'tg.modeStrict')}</option><option value="custom" disabled={trafficObservation < 100}>{translate($language, 'tg.modeCustom')}</option></select></label>
					<label><span class="text-sm text-gray-300">{translate($language, 'tg.source')}</span><select bind:value={trafficProxyMode} class="mt-1 w-full rounded-lg border border-gray-600 bg-gray-900 px-3 py-2 text-sm text-white"><option value="direct">{translate($language, 'tg.srcDirect')}</option><option value="cloudflare">{translate($language, 'tg.srcCloudflare')}</option><option value="custom">{translate($language, 'tg.srcCustom')}</option></select></label>
					{#if trafficProxyMode === 'cloudflare'}<div class="rounded-lg border border-gray-700 bg-gray-900/50 p-3 text-sm text-gray-300 md:col-span-2"><p>{translate($language, 'tg.cloudflareDesc1')}<code>CF-Connecting-IP</code>{translate($language, 'tg.cloudflareDesc2')}</p><button onclick={refreshCloudflareCIDRs} disabled={busy !== '' || !!currentTaskId} class="mt-3 rounded border border-blue-600 px-3 py-1.5 text-xs text-blue-300 disabled:opacity-50">{translate($language, 'tg.refreshCidrs')}</button></div>{/if}
					{#if trafficProxyMode === 'custom'}
						<label><span class="text-sm text-gray-300">{translate($language, 'tg.headerLabel')}</span><select bind:value={trafficProxyHeader} class="mt-1 w-full rounded-lg border border-gray-600 bg-gray-900 px-3 py-2 text-sm text-white"><option>X-Forwarded-For</option><option>X-Real-IP</option><option>CF-Connecting-IP</option></select></label>
						<label><span class="text-sm text-gray-300">{translate($language, 'tg.cidrLabel')}</span><textarea bind:value={trafficProxyCIDRs} rows="3" placeholder="203.0.113.0/24" class="mt-1 w-full rounded-lg border border-gray-600 bg-gray-900 px-3 py-2 font-mono text-sm text-white"></textarea></label>
					{/if}
					{#if trafficMode === 'custom'}<label><span class="text-sm text-gray-300">{translate($language, 'tg.rps')}</span><input type="number" min="1" max="1000" bind:value={trafficRPS} class="mt-1 w-full rounded-lg border border-gray-600 bg-gray-900 px-3 py-2 text-white" /></label><label><span class="text-sm text-gray-300">{translate($language, 'tg.burst')}</span><input type="number" min="1" max="5000" bind:value={trafficBurst} class="mt-1 w-full rounded-lg border border-gray-600 bg-gray-900 px-3 py-2 text-white" /></label><label><span class="text-sm text-gray-300">{translate($language, 'tg.connPerIp')}</span><input type="number" min="1" max="1000" bind:value={trafficConnections} class="mt-1 w-full rounded-lg border border-gray-600 bg-gray-900 px-3 py-2 text-white" /></label>{/if}
				</div>
				<div class="mt-5 rounded-lg border {trafficMode === 'observe' ? 'border-blue-800 bg-blue-950/30 text-blue-200' : 'border-yellow-700 bg-yellow-950/30 text-yellow-200'} p-4 text-sm">{enforcementWarning(trafficMode)}</div>
				{#if trafficMode !== 'observe'}<label class="mt-4 flex items-start gap-2 rounded-lg border border-yellow-700 bg-yellow-900/20 p-3 text-sm text-yellow-200"><input class="mt-1" type="checkbox" bind:checked={trafficConfirmed} /><span>{translate($language, 'tg.confirmEnforce')}</span></label>{/if}
				<div class="mt-5 flex flex-wrap gap-3"><button onclick={applyTrafficGuard} disabled={!selectedTrafficWebsite || busy !== '' || !!currentTaskId || (trafficMode !== 'observe' && (!trafficConfirmed || trafficObservation < 100))} class="rounded-lg bg-blue-600 px-4 py-2 text-sm font-medium text-white disabled:opacity-50">{translate($language, 'sec.validateApply')}</button><button onclick={resetTrafficObserve} disabled={!selectedTrafficWebsite || selectedTrafficProfile?.mode === 'observe' || busy !== '' || !!currentTaskId} class="rounded-lg border border-green-700 px-4 py-2 text-sm text-green-300 disabled:opacity-50">{translate($language, 'tg.returnObserve')}</button></div>
			</section>
		</div>
	{:else}
		<section class="rounded-xl border border-gray-700 bg-gray-800 p-5">
			<div class="flex items-center justify-between"><div><h3 class="font-semibold text-white">{translate($language, 'sec.events.title')}</h3><p class="mt-1 text-sm text-gray-400">{translate($language, 'sec.events.desc')}</p></div><span class="text-sm text-gray-400">{translate($language, 'sec.shownCount').replace('{count}', String(events.length))}</span></div>
			{#if events.length === 0}<div class="mt-6 rounded-lg border border-gray-700 bg-gray-900/50 p-6 text-center text-sm text-gray-400">{translate($language, 'sec.events.none')}</div>{:else}<div class="mt-4 space-y-3">{#each events as event}<article class="rounded-lg border border-gray-700 bg-gray-900/60 p-4"><div class="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between"><div><div class="flex flex-wrap items-center gap-2"><span class="rounded-full px-2 py-0.5 text-xs uppercase {event.severity === 'critical' ? 'bg-red-900 text-red-300' : event.severity === 'high' ? 'bg-yellow-900 text-yellow-300' : 'bg-gray-700 text-gray-300'}">{event.severity}</span><span class="text-sm font-medium text-white">{event.category}</span><span class="text-xs text-gray-500">×{event.occurrence_count}</span></div><p class="mt-2 text-sm text-gray-300">{event.component} · {event.resource || 'server'}</p><p class="mt-1 text-sm text-gray-400">{event.recommended_action || translate($language, 'sec.events.fallbackAction')}</p><p class="mt-2 text-xs text-gray-500">{translate($language, 'sec.events.lastSeen').replace('{time}', new Date(event.last_seen).toLocaleString())}</p></div><div class="flex flex-wrap gap-2">{#if event.status === 'open'}<button onclick={() => transitionEvent(event, 'acknowledged')} disabled={busy !== ''} class="rounded border border-gray-600 px-2 py-1 text-xs text-gray-300">{translate($language, 'sec.events.acknowledge')}</button>{/if}{#if event.status === 'open' || event.status === 'acknowledged'}<button onclick={() => transitionEvent(event, 'resolved')} disabled={busy !== ''} class="rounded border border-green-700 px-2 py-1 text-xs text-green-300">{translate($language, 'sec.events.resolve')}</button><button onclick={() => transitionEvent(event, 'false_positive')} disabled={busy !== ''} class="rounded border border-gray-600 px-2 py-1 text-xs text-gray-400">{translate($language, 'sec.falsePositive')}</button>{:else}<span class="text-xs capitalize text-gray-400">{event.status.replaceAll('_', ' ')}</span>{/if}</div></div></article>{/each}</div>{/if}
		</section>
	{/if}
</div>
