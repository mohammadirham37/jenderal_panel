<script lang="ts">
	import { onMount, onDestroy } from 'svelte';
	import { page } from '$app/state';
	import { goto, replaceState } from '$app/navigation';
	import { api, getCSRFToken } from '$lib/api';
	import TaskProgress from '$lib/components/TaskProgress.svelte';
	import WebsiteSslSection from '$lib/components/WebsiteSslSection.svelte';
	import WebsiteFilesSection from '$lib/components/WebsiteFilesSection.svelte';
	import TerminalConsole from '$lib/components/TerminalConsole.svelte';
	import WebsiteQueueSection from '$lib/components/WebsiteQueueSection.svelte';
	import WebsiteCronSection from '$lib/components/WebsiteCronSection.svelte';
	import WebsiteWpToolkitSection from '$lib/components/WebsiteWpToolkitSection.svelte';
	import WebsitePhpSettingsSection from '$lib/components/WebsitePhpSettingsSection.svelte';
	import WebsiteAppSection from '$lib/components/WebsiteAppSection.svelte';
	import { permissions, user as authUser } from '$lib/stores/auth';
	import { hasPermission } from '$lib/stores/auth';
	import { language, translate } from '$lib/stores/language';
	import { applyEnvValues, parseEnvFile } from '$lib/env-file.js';
import { toast } from '$lib/stores/toast';

	// ─── Interfaces ───────────────────────────────────────────────────

	interface WebsiteDomain {
		id: string;
		name: string;
		type: string;
	}

	interface Website {
		id: string;
		domain: string;
		app_type: string;
		node_version?: string;
		php_version: string;
		document_root: string;
		web_user: string;
		status: string;
		ssl_enabled: boolean;
		framework: string;
		nginx_profile?: string;
		octane_enabled?: boolean;
		octane_port?: number;
		octane_workers?: number;
		error_message?: string;
		domains?: WebsiteDomain[];
		created_at: string;
		created_by?: string;
		owner_email?: string;
	}

	interface CommandPreset {
		label: string;
		command: string;
		category: string;
		danger: boolean;
	}

	interface DeploymentEntry {
		id: string;
		commit_hash: string;
		branch: string;
		status: string;
		duration_ms: number;
		created_at: string;
		log?: string;
	}

	interface NodeRuntime {
		website_id: string;
		web_user: string;
		selected_version: string;
		installed: boolean;
		installed_version: string;
		npm_version: string;
		nvm_version: string;
		nvm_state: string;
		error_message?: string;
	}

	// ─── Tabs ─────────────────────────────────────────────────────────

	const allTabs = ['Overview', 'Deployment', 'SSL', 'Commands', 'PHP Settings', 'App', 'WP Toolkit', 'Cron Jobs', 'Files', 'Terminal', 'Logs', 'Config', 'Domains', 'Queue'] as const;
	type Tab = typeof allTabs[number];
	// Display labels are translated; the values in `allTabs` stay English (used in URLs and logic).
	const tabKeys: Record<Tab, string> = {
		Overview: 'wd.tab.overview',
		Deployment: 'wd.tab.deployment',
		SSL: 'wd.tab.ssl',
		Commands: 'wd.tab.commands',
		'PHP Settings': 'wd.tab.php_settings',
		App: 'wd.tab.app',
		'WP Toolkit': 'wd.tab.wp_toolkit',
		'Cron Jobs': 'wd.tab.cron',
		Files: 'wd.tab.files',
		Terminal: 'wd.tab.terminal',
		Logs: 'wd.tab.logs',
		Config: 'wd.tab.config',
		Domains: 'wd.tab.domains',
		Queue: 'wd.tab.queue'
	};
	function tabFromURL(): Tab {
		const tab = new URLSearchParams(page.url.search).get('tab');
		return (allTabs as readonly string[]).includes(tab ?? '') ? (tab as Tab) : 'Overview';
	}
	// The queue tab only applies to Laravel sites (Octane/artisan workers).
	let tabs = $derived(allTabs.filter((t) =>
		(t !== 'Queue' || website?.framework === 'laravel') &&
		(t !== 'WP Toolkit' || website?.app_type === 'wordpress') &&
		(t !== 'PHP Settings' || website?.app_type !== 'static') &&
		(t !== 'App' || website?.app_type === 'node' || website?.app_type === 'go' || website?.app_type === 'python' || website?.app_type === 'deno' || website?.app_type === 'bun')
	));
	$effect(() => {
		if (website && activeTab === 'Queue' && website.framework !== 'laravel') {
			activeTab = 'Overview';
		}
		if (website && activeTab === 'WP Toolkit' && website.app_type !== 'wordpress') {
			activeTab = 'Overview';
		}
		if (website && activeTab === 'PHP Settings' && website.app_type === 'static') {
			activeTab = 'Overview';
		}
		if (website && activeTab === 'App' && ['node','go','python','deno','bun'].indexOf(website.app_type) === -1) {
			activeTab = 'Overview';
		}
	});

	let activeTab = $state<Tab>(tabFromURL());

	function setActiveTab(tab: Tab) {
		activeTab = tab;
		// Keep the tab in the URL so a refresh reopens the same tab.
		const url = new URL(page.url);
		url.searchParams.set('tab', tab);
		replaceState(url, page.state);
	}

	// ─── Core State ───────────────────────────────────────────────────

	let website = $state<Website | null>(null);
	let loading = $state(true);
	let error = $state('');

	// ─── Overview ─────────────────────────────────────────────────────

	let deleteConfirm = $state(false);
	const pendingStatuses = ['pending', 'installing', 'configuring', 'validating'];

	// Node.js runtime (moved from the Node.js page)
	let nodeRuntime = $state<NodeRuntime | null>(null);
	let nodeRuntimeChoice = $state('24');
	let nodeRuntimeLoading = $state(false);
	let nodeRuntimeError = $state('');
	let nodeRuntimeTaskId = $state('');
	let nodeRuntimeInitialized = $state(false);

	// Laravel Octane (FrankenPHP)
	interface OctaneStatus {
		enabled: boolean;
		running: boolean;
		port: number;
		admin_port: number;
		workers: number;
		unit: string;
		frankenphp_version: string;
	}
	let octane = $state<OctaneStatus | null>(null);
	let octaneLoading = $state(false);
	let octaneBusy = $state(false);
	let octaneTaskId = $state('');
	let octaneWorkersChoice = $state('4');
	let octaneInitialized = $state(false);
	let canManageServices = $derived(hasPermission($permissions, 'services.manage'));

	async function loadOctaneStatus() {
		if (!website) return;
		octaneLoading = true;
		try {
			octane = await api.get<OctaneStatus>(`/api/v1/websites/${website.id}/octane`);
			if (octane) octaneWorkersChoice = String(octane.workers || 4);
		} catch {
			octane = null;
		} finally {
			octaneLoading = false;
		}
	}

	async function enableOctane() {
		if (!website || octaneBusy) return;
		octaneBusy = true;
		
		try {
			const result = await api.post<{ task_id: string }>(`/api/v1/websites/${website.id}/octane/enable`);
			octaneTaskId = result.task_id || '';
			toast.success(translate($language, 'wd.octane.install_started'));
		} catch (err) {
			toast.error(err instanceof Error ? err.message : translate($language, 'wd.octane.enable_failed'));
		} finally {
			octaneBusy = false;
		}
	}

	async function disableOctane() {
		if (!website || octaneBusy) return;
		if (!confirm(translate($language, 'wd.octane.disable_confirm'))) return;
		octaneBusy = true;
		
		try {
			await api.post(`/api/v1/websites/${website.id}/octane/disable`);
			toast.success(translate($language, 'wd.octane.disabled_success'));
			await loadOctaneStatus();
			await loadWebsite();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : translate($language, 'wd.octane.disable_failed'));
		} finally {
			octaneBusy = false;
		}
	}

	async function octaneAction(action: 'start' | 'stop' | 'restart') {
		if (!website || octaneBusy) return;
		octaneBusy = true;

		try {
			await api.post(`/api/v1/websites/${website.id}/octane/${action}`);
			await loadOctaneStatus();
		} catch (err) {
			const failedKeys = { start: 'wd.octane.start_failed', stop: 'wd.octane.stop_failed', restart: 'wd.octane.restart_failed' } as const;
			toast.error(err instanceof Error ? err.message : translate($language, failedKeys[action]));
		} finally {
			octaneBusy = false;
		}
	}

	async function reloadOctane() {
		if (!website || octaneBusy) return;
		octaneBusy = true;
		
		try {
			const result = await api.post<{ task_id: string }>(`/api/v1/websites/${website.id}/octane/reload`);
			octaneTaskId = result.task_id || '';
		} catch (err) {
			toast.error(err instanceof Error ? err.message : translate($language, 'wd.octane.reload_failed'));
		} finally {
			octaneBusy = false;
		}
	}

	async function saveOctaneWorkers() {
		if (!website || octaneBusy) return;
		octaneBusy = true;
		
		try {
			await api.put(`/api/v1/websites/${website.id}/octane/workers`, { workers: Number(octaneWorkersChoice) });
			toast.success(translate($language, 'wd.octane.workers_saved'));
			await loadOctaneStatus();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : translate($language, 'wd.octane.workers_save_failed'));
		} finally {
			octaneBusy = false;
		}
	}

	let canInstallFrankenphp = $derived(hasPermission($permissions, 'services.manage'));

	async function installFrankenphp() {
		if (octaneTaskId) return;
		
		try {
			const result = await api.post<{ task_id: string }>('/api/v1/frankenphp/install');
			octaneTaskId = result.task_id || '';
		} catch (err) {
			toast.error(err instanceof Error ? err.message : translate($language, 'wd.frankenphp.install_start_failed'));
		}
	}

	// ─── Deployment ───────────────────────────────────────────────────

	let gitProvider = $state<'github' | 'gitlab' | 'bitbucket' | 'custom'>('github');
	let repoUrl = $state('');
	let repoBranch = $state('main');
	let repoVisibility = $state<'public' | 'private'>('public');

	let deployKey = $state('');
	let deployKeyLoading = $state(false);
	let deployKeyError = $state('');
	let deployKeyCopied = $state(false);

	let deploying = $state(false);

	let uploadDeployFile: HTMLInputElement;
	let uploadDeployTaskId = $state('');
	let uploadingDeploy = $state(false);
	let repairingLayout = $state(false);

	let deployments = $state<DeploymentEntry[]>([]);
	let deploymentsLoading = $state(false);
	let deploymentsError = $state('');
	let deploymentsPollTimer: ReturnType<typeof setInterval> | null = null;
	let autoExpandedDeployment = $state('');
	let expandedDeploymentId = $state<string | null>(null);

	let deploymentTabInitialized = $state(false);

	// ─── Commands ─────────────────────────────────────────────────────

	let commandPresets = $state<CommandPreset[]>([]);
	let commandsLoading = $state(false);
	let commandsError = $state('');
	let commandTaskId = $state('');
	let commandsInitialized = $state(false);
	let pendingCommand = $state<CommandPreset | null>(null);
	let commandConfirmBusy = $state(false);

	// ─── Laravel .env ──────────────────────────────────────────────────

	let envLoading = $state(false);
	let envExists = $state(false);
	let envRaw = $state('');
	let envMode = $state<'values' | 'raw'>('values');
	let envValues = $state<{ key: string; value: string }[]>([]);
	let envSaving = $state(false);
	let envError = $state('');
	let envMsg = $state('');
	let envInitialized = $state(false);

	// ─── Terminal ─────────────────────────────────────────────────────
	// Rendered by the shared TerminalConsole component; the console
	// connects while its tab is active and disconnects on leave.
	// Ownership (admin only): show the owner and allow transferring the site.
	let canManageUsers = $derived(hasPermission($permissions, 'users.manage'));
	let panelUsers = $state<{ id: string; email: string }[]>([]);
	let transferTarget = $state('');
	let transferring = $state(false);
	let transferMsg = $state('');
	let transferError = $state('');

	async function loadPanelUsers() {
		if (!canManageUsers || panelUsers.length > 0) return;
		try {
			panelUsers = await api.get<{ id: string; email: string }[]>('/users');
		} catch {
			panelUsers = [];
		}
	}

	async function transferOwnership() {
		if (!website || !transferTarget || transferring) return;
		transferring = true;
		transferMsg = '';
		transferError = '';
		try {
			const updated = await api.post<{ owner_email?: string }>(`/websites/${website.id}/owner`, {
				user_id: transferTarget
			});
			website = { ...website, created_by: transferTarget, owner_email: updated.owner_email };
			transferMsg = translate($language, 'wd.ownership.transferred');
			transferTarget = '';
		} catch (err) {
			transferError = err instanceof Error ? err.message : translate($language, 'wd.ownership.transfer_failed');
		} finally {
			transferring = false;
		}
	}

	// ─── Health check ─────────────────────────────────────────────
	interface HealthCheck {
		website_id: string;
		url: string;
		expected_status: number;
		enabled: boolean;
		last_status: number;
		last_latency_ms: number;
		consecutive_failures: number;
		last_checked_at: string;
	}
	let health = $state<HealthCheck | null>(null);
	let healthURL = $state('');
	let healthExpected = $state(200);
	let healthEnabled = $state(false);
	let healthBusy = $state(false);

	async function loadHealth() {
		if (!website) return;
		try {
			const h = await api.get<HealthCheck>(`/api/v1/websites/${website.id}/health`);
			health = h;
			healthURL = h.url || `http://${website.domain}`;
			healthExpected = h.expected_status || 200;
			healthEnabled = h.enabled;
		} catch { /* optional feature */ }
	}

	async function saveHealth() {
		if (!website || healthBusy) return;
		healthBusy = true;
		try {
			health = await api.put<HealthCheck>(`/api/v1/websites/${website.id}/health`, {
				url: healthURL,
				expected_status: healthExpected,
				enabled: healthEnabled
			});
			toast.success(translate($language, 'wd.health.saved'));
		} catch (err) {
			toast.error(err instanceof Error ? err.message : translate($language, 'wd.health.save_failed'));
		} finally {
			healthBusy = false;
		}
	}

	async function checkHealthNow() {
		if (!website || healthBusy) return;
		healthBusy = true;
		try {
			health = await api.post<HealthCheck>(`/api/v1/websites/${website.id}/health/check`, {});
		} catch (err) {
			toast.error(err instanceof Error ? err.message : translate($language, 'wd.health.check_failed'));
		} finally {
			healthBusy = false;
		}
	}

	let terminalEndpoint = $derived.by(() => {
		if (!website) return '';
		const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
		const homeDir = `/home/${website.web_user}`;
		return `${protocol}//${location.host}/ws/terminal?web_user=${encodeURIComponent(website.web_user)}&workdir=${encodeURIComponent(homeDir)}`;
	});

	// ─── Files ────────────────────────────────────────────────────────
	// The Files tab renders WebsiteFilesSection, which owns its own state.

	// ─── Logs ─────────────────────────────────────────────────────────

	let logTab = $state<'access' | 'error' | 'octane'>('access');
	let accessLogs = $state('');
	let errorLogs = $state('');
	let octaneLogs = $state('');
	let logsLoading = $state(false);
	let logsError = $state('');
	let logsInitialized = $state(false);

	// ─── Config ───────────────────────────────────────────────────────

	let configContent = $state('');
	let configLoading = $state(false);
	let configError = $state('');
	let configSaveMsg = $state('');
	let configInitialized = $state(false);

	// Nginx template engine
	let profileChoice = $state('');
	let profileSaving = $state(false);

	// Labels/descriptions are i18n keys — translate them at render time.
	const profileOptions: { value: string; label: string; description: string }[] = [
		{ value: '', label: 'wd.profile.auto.label', description: 'wd.profile.auto.desc' },
		{ value: 'php', label: 'wd.profile.php.label', description: 'wd.profile.php.desc' },
		{ value: 'laravel', label: 'wd.profile.laravel.label', description: 'wd.profile.laravel.desc' },
		{ value: 'codeigniter3', label: 'wd.profile.ci3.label', description: 'wd.profile.ci3.desc' },
		{ value: 'codeigniter4', label: 'wd.profile.ci4.label', description: 'wd.profile.ci4.desc' },
		{ value: 'static', label: 'wd.profile.static.label', description: 'wd.profile.static.desc' }
	];

	let effectiveProfileLabel = $derived.by(() => {
		if (!website) return '—';
		const effective = website.nginx_profile || deriveAutoProfile(website);
		const opt = profileOptions.find((o) => o.value === effective);
		return opt ? translate($language, opt.label) : effective;
	});

	function deriveAutoProfile(w: Website): string {
		if (w.app_type === 'static') return 'static';
		if (w.framework === 'laravel' || w.app_type === 'laravel') return 'laravel';
		return 'php';
	}

	let selectedProfileDescription = $derived.by(() => {
		const opt = profileOptions.find((o) => o.value === profileChoice);
		return opt ? translate($language, opt.description) : '';
	});

	async function applyNginxProfile() {
		if (!website || profileSaving) return;
		profileSaving = true;
		configError = '';
		configSaveMsg = '';
		try {
			await api.put(`/api/v1/websites/${website.id}/nginx-profile`, { profile: profileChoice });
			const opt = profileOptions.find((o) => o.value === profileChoice);
			configSaveMsg = profileChoice
				? translate($language, 'wd.profile.applied').replace('{name}', opt ? translate($language, opt.label) : '')
				: translate($language, 'wd.profile.reset_auto');
			await Promise.all([loadWebsite(), loadConfig()]);
		} catch (err) {
			configError = err instanceof Error ? err.message : translate($language, 'wd.profile.apply_failed');
		} finally {
			profileSaving = false;
		}
	}

	// ─── Domains ──────────────────────────────────────────────────────

	let addDomainName = $state('');
	let addDomainType = $state('alias');
	let addingDomain = $state(false);

	// ─── Helpers ──────────────────────────────────────────────────────

	function statusBadgeClass(status: string): string {
		switch (status) {
			case 'active': return 'bg-green-900 text-green-300';
			case 'pending': case 'installing': case 'configuring': case 'validating':
				return 'bg-yellow-900 text-yellow-300 animate-pulse';
			case 'failed': return 'bg-red-900 text-red-300';
			case 'suspended': case 'disabled': return 'bg-gray-700 text-gray-400';
			default: return 'bg-gray-700 text-gray-400';
		}
	}

	function domainTypeBadgeClass(type: string): string {
		switch (type) {
			case 'primary': return 'bg-blue-900 text-blue-300';
			case 'alias': return 'bg-purple-900 text-purple-300';
			case 'subdomain': return 'bg-cyan-900 text-cyan-300';
			default: return 'bg-gray-700 text-gray-400';
		}
	}

	function deployStatusBadgeClass(status: string): string {
		switch (status) {
			case 'success': case 'completed': return 'bg-green-900 text-green-300';
			case 'running': return 'bg-yellow-900 text-yellow-300 animate-pulse';
			case 'failed': return 'bg-red-900 text-red-300';
			default: return 'bg-gray-700 text-gray-400';
		}
	}

	function formatSize(bytes: number): string {
		if (bytes === 0) return '-';
		const units = ['B', 'KB', 'MB', 'GB'];
		let i = 0;
		let size = bytes;
		while (size >= 1024 && i < units.length - 1) { size /= 1024; i++; }
		return `${size.toFixed(i === 0 ? 0 : 1)} ${units[i]}`;
	}

	function formatDuration(seconds: number): string {
		if (seconds < 60) return `${seconds}s`;
		const m = Math.floor(seconds / 60);
		const s = seconds % 60;
		return `${m}m ${s}s`;
	}

	function formatDate(dateStr: string): string {
		return new Date(dateStr).toLocaleString();
	}

	const providerInstructions: Record<string, string> = {
		github: 'wd.deploy.instructions.github',
		gitlab: 'wd.deploy.instructions.gitlab',
		bitbucket: 'wd.deploy.instructions.bitbucket',
		custom: 'wd.deploy.instructions.custom'
	};

	// ─── API Calls ────────────────────────────────────────────────────

	async function loadWebsite() {
		loading = true;
		error = '';
		try {
			website = await api.get<Website>(`/api/v1/websites/${page.params.id}`);
		} catch (err) {
			error = err instanceof Error ? err.message : translate($language, 'wd.load_failed');
		} finally {
			loading = false;
		}
	}

	async function loadNodeRuntime() {
		if (!website) return;
		nodeRuntimeLoading = true;
		try {
			const runtimes = await api.get<NodeRuntime[]>('/api/v1/nodejs/runtimes') || [];
			nodeRuntime = runtimes.find((r) => r.website_id === website?.id) || null;
			if (nodeRuntime) nodeRuntimeChoice = nodeRuntime.selected_version || '24';
			nodeRuntimeError = '';
		} catch (err) {
			nodeRuntime = null;
			nodeRuntimeError = err instanceof Error ? err.message : translate($language, 'wd.node.load_failed');
		} finally {
			nodeRuntimeLoading = false;
		}
	}

	async function installNodeRuntime() {
		if (!website || nodeRuntimeTaskId) return;
		
		try {
			const result = await api.post<{ task_id: string }>(`/api/v1/nodejs/runtimes/${website.id}`, {
				version: nodeRuntimeChoice
			});
			nodeRuntimeTaskId = result.task_id || '';
		} catch (err) {
			nodeRuntimeError = err instanceof Error ? err.message : translate($language, 'wd.node.install_failed');
		}
	}

	// Overview actions
	async function suspendWebsite() {
		if (!website) return;
		
		try {
			await api.post(`/api/v1/websites/${website.id}/suspend`);
			toast.success(translate($language, 'wd.suspend_success'));
			await loadWebsite();
		} catch (err) { toast.error(err instanceof Error ? err.message : translate($language, 'wd.suspend_failed')); }
	}

	async function enableWebsite() {
		if (!website) return;
		
		try {
			await api.post(`/api/v1/websites/${website.id}/enable`);
			toast.success(translate($language, 'wd.enable_success'));
			await loadWebsite();
		} catch (err) { toast.error(err instanceof Error ? err.message : translate($language, 'wd.enable_failed')); }
	}

	async function retryWebsite() {
		if (!website) return;
		
		try {
			await api.post(`/api/v1/websites/${website.id}/retry`);
			toast.success(translate($language, 'wd.retry_success'));
			await loadWebsite();
		} catch (err) { toast.error(err instanceof Error ? err.message : translate($language, 'wd.retry_failed')); }
	}

	async function deleteWebsite() {
		if (!website) return;
		
		try {
			await api.del(`/api/v1/websites/${website.id}`);
			goto('/websites');
		} catch (err) {
			toast.error(err instanceof Error ? err.message : translate($language, 'wd.delete_failed'));
			deleteConfirm = false;
		}
	}

	// Deployment
	async function checkDeployKey() {
		if (!website) return;
		deployKeyLoading = true;
		deployKeyError = '';
		try {
			const data = await api.get<{ public_key: string }>(`/api/v1/websites/${website.id}/deploy-key`);
			deployKey = data.public_key || '';
		} catch {
			deployKey = '';
		} finally {
			deployKeyLoading = false;
		}
	}

	async function generateDeployKey() {
		if (!website) return;
		deployKeyLoading = true;
		deployKeyError = '';
		try {
			const data = await api.post<{ public_key: string }>(`/api/v1/websites/${website.id}/deploy-key`);
			deployKey = data.public_key || '';
		} catch (err) {
			deployKeyError = err instanceof Error ? err.message : translate($language, 'wd.deploykey.generate_failed');
		} finally {
			deployKeyLoading = false;
		}
	}

	async function deleteDeployKey() {
		if (!website) return;
		deployKeyLoading = true;
		deployKeyError = '';
		try {
			await api.del(`/api/v1/websites/${website.id}/deploy-key`);
			deployKey = '';
		} catch (err) {
			deployKeyError = err instanceof Error ? err.message : translate($language, 'wd.deploykey.delete_failed');
		} finally {
			deployKeyLoading = false;
		}
	}

	async function copyDeployKey() {
		try {
			await navigator.clipboard.writeText(deployKey);
			deployKeyCopied = true;
			setTimeout(() => { deployKeyCopied = false; }, 2000);
		} catch { /* clipboard not available */ }
	}

	async function deployNow() {
		if (!website || !repoUrl.trim()) return;
		deploying = true;
		
		try {
			// The API returns the created Deployment (202 Accepted), not a task.
			const d = await api.post<DeploymentEntry>(`/api/v1/websites/${website.id}/deploy`, {
				repo: repoUrl,
				branch: repoBranch
			});
			toast.success(d?.id ? translate($language, 'wd.deploy.started_id').replace('{id}', d.id.slice(-6).toLowerCase()) : translate($language, 'wd.deploy.started'));
			await loadDeployments();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : translate($language, 'wd.deploy.start_failed'));
		} finally {
			deploying = false;
		}
	}

	async function repairLayout() {
		if (!website) return;
		if (!confirm(translate($language, 'wd.deploy.repair_confirm'))) return;
		repairingLayout = true;
		
		try {
			const data = await api.post<{ output: string }>(`/api/v1/websites/${website.id}/repair-layout`, {});
			toast.success(data.output || translate($language, 'wd.deploy.repair_done'));
		} catch (err) {
			toast.error(err instanceof Error ? err.message : translate($language, 'wd.deploy.repair_failed'));
		} finally {
			repairingLayout = false;
		}
	}

	async function uploadDeploy(event: Event) {
		if (!website) return;
		const input = event.target as HTMLInputElement;
		const file = input.files?.[0];
		if (!file) return;
		uploadingDeploy = true;
		
		try {
			const formData = new FormData();
			formData.append('file', file);
			const res = await fetch(`/api/v1/websites/${website.id}/upload-deploy`, {
				method: 'POST',
				headers: { 'X-CSRF-Token': getCSRFToken() },
				credentials: 'include',
				body: formData
			});
			if (!res.ok) throw new Error(translate($language, 'wd.deploy.upload_failed_status').replace('{status}', res.statusText));
			const json = await res.json();
			uploadDeployTaskId = json.data?.task_id || '';
			toast.success(translate($language, 'wd.deploy.upload_started'));
			input.value = '';
		} catch (err) {
			toast.error(err instanceof Error ? err.message : translate($language, 'wd.deploy.upload_failed'));
		} finally {
			uploadingDeploy = false;
		}
	}

	function hasActiveDeployments(deps: DeploymentEntry[]): boolean {
		return deps.some((d) => d.status === 'pending' || d.status === 'running');
	}

	function startDeploymentsPolling() {
		if (deploymentsPollTimer) return;
		deploymentsPollTimer = setInterval(() => {
			if (!website) {
				stopDeploymentsPolling();
				return;
			}
			void loadDeployments();
		}, 5000);
	}

	function stopDeploymentsPolling() {
		if (deploymentsPollTimer) {
			clearInterval(deploymentsPollTimer);
			deploymentsPollTimer = null;
		}
	}

	async function loadDeployments() {
		if (!website) return;
		deploymentsLoading = true;
		try {
			deployments = await api.get<DeploymentEntry[]>(`/api/v1/websites/${website.id}/deployments`) || [];
			deploymentsError = '';
			if (hasActiveDeployments(deployments)) {
				startDeploymentsPolling();
			} else {
				stopDeploymentsPolling();
			}
			// Surface the error log of the newest failed deployment automatically.
			const failed = deployments.find((d) => d.status === 'failed');
			if (failed && failed.id !== autoExpandedDeployment) {
				autoExpandedDeployment = failed.id;
				expandedDeploymentId = failed.id;
			}
		} catch (err) {
			deploymentsError = err instanceof Error ? err.message : translate($language, 'wd.deploy.load_failed');
		} finally {
			deploymentsLoading = false;
		}
	}

	// Commands
	async function loadCommandPresets() {
		if (!website) return;
		commandsLoading = true;
		commandsError = '';
		try {
			commandPresets = await api.get<CommandPreset[]>(`/api/v1/websites/${website.id}/command-presets`) || [];
		} catch (err) {
			commandsError = err instanceof Error ? err.message : translate($language, 'wd.cmd.load_failed');
		} finally {
			commandsLoading = false;
		}
	}

	async function runCommand(preset: CommandPreset) {
		if (!website) return;
		
		try {
			const data = await api.post<{ task_id: string }>(`/api/v1/websites/${website.id}/run-command`, {
				command: preset.label
			});
			commandTaskId = data.task_id || '';
		} catch (err) {
			toast.error(err instanceof Error ? err.message : translate($language, 'wd.cmd.run_failed'));
		}
	}

	async function confirmRunCommand() {
		if (!pendingCommand || commandConfirmBusy) return;
		const preset = pendingCommand;
		commandConfirmBusy = true;
		await runCommand(preset);
		commandConfirmBusy = false;
		pendingCommand = null;
	}

	function groupedCommands(): Record<string, CommandPreset[]> {
		const groups: Record<string, CommandPreset[]> = {};
		for (const preset of commandPresets) {
			const cat = preset.category || 'other';
			if (!groups[cat]) groups[cat] = [];
			groups[cat].push(preset);
		}
		return groups;
	}

	// ─── Laravel .env ──────────────────────────────────────────────────

	async function loadEnv() {
		if (!website) return;
		envLoading = true;
		envError = '';
		try {
			const data = await api.get<{ exists: boolean; content: string }>(`/api/v1/websites/${website.id}/env`);
			envExists = data.exists;
			envRaw = data.content || '';
			if (envExists) syncEnvValues();
		} catch (err) {
			envError = err instanceof Error ? err.message : translate($language, 'wd.env.load_failed');
		} finally {
			envLoading = false;
		}
	}

	function syncEnvValues() {
		envValues = parseEnvFile(envRaw).map((entry) => ({ key: entry.key, value: entry.value }));
	}

	function switchEnvMode(mode: 'values' | 'raw') {
		if (mode === envMode) return;
		if (mode === 'raw') {
			// Fold edited values back into the raw content before switching.
			envRaw = applyEnvValues(envRaw, Object.fromEntries(envValues.map((row) => [row.key, row.value])));
		} else {
			syncEnvValues();
		}
		envMode = mode;
	}

	async function createEnvFile() {
		if (!website) return;
		envError = ''; envMsg = '';
		try {
			const data = await api.post<{ task_id: string }>(`/api/v1/websites/${website.id}/run-command`, {
				command: 'cp .env.example .env'
			});
			commandTaskId = data.task_id || '';
			envMsg = translate($language, 'wd.env.copying');
			await pollEnvAfterCreate();
		} catch (err) {
			envError = err instanceof Error ? err.message : translate($language, 'wd.env.copy_failed');
		}
	}

	async function pollEnvAfterCreate() {
		// The copy runs in the background and is quick; poll until the file
		// appears so the editor can open right away.
		for (let i = 0; i < 15; i++) {
			await new Promise((resolve) => setTimeout(resolve, 1000));
			if (!website) return;
			try {
				const data = await api.get<{ exists: boolean; content: string }>(`/api/v1/websites/${website.id}/env`);
				if (data.exists) {
					envExists = true;
					envRaw = data.content || '';
					syncEnvValues();
					envMsg = translate($language, 'wd.env.created');
					return;
				}
			} catch {
				// Keep polling; the task may still be running.
			}
		}
		envError = translate($language, 'wd.env.not_created');
	}

	async function saveEnv() {
		if (!website) return;
		envSaving = true;
		envError = ''; envMsg = '';
		try {
			const content = envMode === 'values'
				? applyEnvValues(envRaw, Object.fromEntries(envValues.map((row) => [row.key, row.value])))
				: envRaw;
			await api.put(`/api/v1/websites/${website.id}/env`, { content });
			envRaw = content;
			syncEnvValues();
			envMsg = translate($language, 'wd.env.saved');
		} catch (err) {
			envError = err instanceof Error ? err.message : translate($language, 'wd.env.save_failed');
		} finally {
			envSaving = false;
		}
	}

	// Files: handled by WebsiteFilesSection.

	// Logs
	async function loadLogs(type: 'access' | 'error' | 'octane') {
		if (!website) return;
		logsLoading = true;
		logsError = '';
		try {
			const data = await api.get<{ content: string }>(`/api/v1/websites/${website.id}/logs/${type}?lines=100`);
			if (type === 'access') accessLogs = data.content || '';
			else if (type === 'octane') octaneLogs = data.content || '';
			else errorLogs = data.content || '';
		} catch (err) {
			logsError = err instanceof Error ? err.message : translate($language, 'wd.logs.load_failed');
		} finally {
			logsLoading = false;
		}
	}

	// Config
	async function loadConfig() {
		if (!website) return;
		configLoading = true;
		configError = '';
		try {
			const data = await api.get<{ content: string }>(`/api/v1/websites/${website.id}/config`);
			configContent = data.content || '';
		} catch (err) {
			configError = err instanceof Error ? err.message : translate($language, 'wd.config.load_failed');
		} finally {
			configLoading = false;
		}
	}

	async function saveConfig() {
		if (!website) return;
		configSaveMsg = ''; configError = '';
		try {
			await api.put(`/api/v1/websites/${website.id}/config`, { content: configContent });
			configSaveMsg = translate($language, 'wd.config.saved');
		} catch (err) {
			configError = err instanceof Error ? err.message : translate($language, 'wd.config.save_failed');
		}
	}

	// Domains
	async function addDomain() {
		if (!website || !addDomainName.trim()) return;
		addingDomain = true;
		
		try {
			await api.post(`/api/v1/websites/${website.id}/domains`, { name: addDomainName, type: addDomainType });
			toast.success(translate($language, 'wd.domains.added').replace('{name}', addDomainName));
			addDomainName = '';
			addDomainType = 'alias';
			await loadWebsite();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : translate($language, 'wd.domains.add_failed'));
		} finally {
			addingDomain = false;
		}
	}

	async function removeDomain(domainId: string) {
		if (!website) return;
		
		try {
			await api.del(`/api/v1/websites/${website.id}/domains/${domainId}`);
			toast.success(translate($language, 'wd.domains.removed'));
			await loadWebsite();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : translate($language, 'wd.domains.remove_failed'));
		}
	}

	// Copy utility
	let copiedField = $state('');
	async function copyText(text: string, field: string) {
		try {
			await navigator.clipboard.writeText(text);
			copiedField = field;
			setTimeout(() => { copiedField = ''; }, 2000);
		} catch { /* clipboard not available */ }
	}

	// ─── Tab change effects ───────────────────────────────────────────

	$effect(() => {
		if (activeTab === 'Deployment' && !deploymentTabInitialized && website) {
			deploymentTabInitialized = true;
			checkDeployKey();
			loadDeployments();
		}
	});

	$effect(() => {
		if (activeTab === 'Commands' && !commandsInitialized && website) {
			commandsInitialized = true;
			loadCommandPresets();
			if (website.framework === 'laravel' && !envInitialized) {
				envInitialized = true;
				loadEnv();
			}
		}
	});

	$effect(() => {
		if (activeTab === 'Overview' && website && !nodeRuntimeInitialized) {
			nodeRuntimeInitialized = true;
			loadNodeRuntime();
			loadHealth();
		}
	});

	$effect(() => {
		if (activeTab === 'Overview' && website && !octaneInitialized) {
			octaneInitialized = true;
			loadOctaneStatus();
		}
	});

	$effect(() => {
		if (activeTab === 'Overview' && website) {
			loadPanelUsers();
		}
	});

	$effect(() => {
		if (activeTab === 'Logs' && !logsInitialized && website) {
			logsInitialized = true;
			loadLogs(logTab);
		}
	});

	$effect(() => {
		if (activeTab === 'Config' && !configInitialized && website) {
			configInitialized = true;
			profileChoice = website.nginx_profile || '';
			loadConfig();
		}
	});

	// ─── Lifecycle ────────────────────────────────────────────────────

	onMount(loadWebsite);

	onDestroy(() => {
		stopDeploymentsPolling();
	});
</script>

<svelte:window
	onkeydown={(e) => {
		if (e.key === 'Escape' && pendingCommand && !commandConfirmBusy) pendingCommand = null;
	}}
/>

<div class="space-y-6">
	<!-- Back link -->
	<a href="/websites" class="inline-flex items-center gap-1 text-sm text-gray-400 hover:text-white transition-colors">
		<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="2">
			<path stroke-linecap="round" stroke-linejoin="round" d="M15 19l-7-7 7-7" />
		</svg>
		{translate($language, 'wd.back')}
	</a>



	{#if loading}
		<div class="text-gray-400">{translate($language, 'wd.loading')}</div>
	{:else if error}
		<div class="p-4 bg-red-900/50 border border-red-700 rounded-lg text-red-300">{error}</div>
	{:else if website}
		<!-- Header -->
		<div class="bg-gray-800 rounded-lg border border-gray-700 p-5">
			<div class="flex flex-wrap items-center gap-3 mb-4">
				<h2 class="text-2xl font-bold text-white">{website.domain}</h2>
				<span class="inline-block px-2.5 py-0.5 rounded text-xs font-medium {statusBadgeClass(website.status)}">
					{website.status}
				</span>
			</div>
			<div class="flex flex-wrap gap-4 text-sm text-gray-400">
				<span>{translate($language, 'wd.app_type')} <span class="text-gray-200 capitalize">{website.app_type}</span></span>
				{#if website.app_type !== 'static'}
					<span>{translate($language, 'wd.php_version')} <span class="text-gray-200">{website.php_version}</span></span>
				{/if}
				<span>{translate($language, 'wd.ssl_label')} <span class={website.ssl_enabled ? 'text-green-400' : 'text-gray-300'}>{website.ssl_enabled ? translate($language, 'wd.enabled') : translate($language, 'wd.ssl_not_configured')}</span></span>
				<a href="/ssl" class="text-blue-400 hover:text-blue-300 transition-colors">{translate($language, 'wd.manage_ssl')}</a>
			</div>
		</div>

		<!-- Tab Navigation -->
		<div class="flex flex-wrap gap-1 border-b border-gray-700 pb-0">
			{#each tabs as tab}
				<button
					onclick={() => setActiveTab(tab)}
					class="px-4 py-2.5 text-sm font-medium rounded-t-lg transition-colors cursor-pointer
						{activeTab === tab
							? 'bg-gray-800 text-white border border-gray-700 border-b-gray-800 -mb-px'
							: 'text-gray-400 hover:text-gray-200 hover:bg-gray-800/50'}"
				>
					{translate($language, tabKeys[tab])}
				</button>
			{/each}
		</div>

		<!-- Tab Content -->
		<div class="mt-0">

			<!-- ============================================================ -->
			<!-- OVERVIEW TAB                                                  -->
			<!-- ============================================================ -->
			{#if activeTab === 'Overview'}
				<div class="space-y-4">
					{#if canManageUsers}
						<div class="p-4 bg-gray-800 border border-gray-700 rounded-lg">
							<div class="flex flex-wrap items-center justify-between gap-2">
								<h3 class="text-sm font-semibold text-white">{translate($language, 'wd.ownership.title')}</h3>
								<span class="text-xs text-gray-500">
									{translate($language, 'wd.ownership.owner')} <span class="text-gray-300">{website.owner_email || website.created_by || translate($language, 'wd.ownership.legacy')}</span>
								</span>
							</div>
							{#if website.created_by}
								<p class="mt-1 text-xs text-gray-500">
									{website.created_by === $authUser?.id ? translate($language, 'wd.ownership.yours') : translate($language, 'wd.ownership.other')}
								</p>
							{/if}
							<div class="mt-3 flex flex-wrap items-center gap-2">
								<select
									bind:value={transferTarget}
									class="rounded-lg border border-gray-600 bg-gray-900 px-2.5 py-1.5 text-xs text-gray-200 focus:border-blue-500 focus:outline-none"
									aria-label={translate($language, 'wd.ownership.new_owner')}
								>
									<option value="">{translate($language, 'wd.ownership.transfer_to')}</option>
									{#each panelUsers as u (u.id)}
										<option value={u.id}>{u.email}</option>
									{/each}
								</select>
								<button
									type="button"
									onclick={transferOwnership}
									disabled={!transferTarget || transferring}
									class="rounded-lg bg-blue-600 px-3 py-1.5 text-xs font-semibold text-white transition hover:bg-blue-700 disabled:cursor-not-allowed disabled:opacity-40"
								>
									{transferring ? translate($language, 'wd.ownership.transferring') : translate($language, 'wd.ownership.transfer')}
								</button>
							</div>
							{#if transferMsg}<p class="mt-2 text-xs text-green-400">{transferMsg}</p>{/if}
							{#if transferError}<p class="mt-2 text-xs text-red-400">{transferError}</p>{/if}
						</div>
					{/if}

					{#if website.app_type !== 'static'}
						<div class="bg-gray-800 rounded-lg border border-gray-700 p-5">
							<div class="flex flex-wrap items-center justify-between gap-2">
								<h3 class="text-sm font-semibold text-white">{translate($language, 'wd.health.title')}</h3>
								{#if health && health.last_checked_at}
									<span class="text-xs {health.consecutive_failures > 0 ? 'text-red-400' : 'text-green-400'}">
										{health.last_status || '—'} · {health.last_latency_ms}ms · {translate($language, 'wd.health.checked').replace('{date}', formatDate(health.last_checked_at))}
										{#if health.consecutive_failures > 0}· {translate($language, 'wd.health.consecutive_failures').replace('{count}', String(health.consecutive_failures))}{/if}
									</span>
								{/if}
							</div>
							<div class="mt-3 flex flex-wrap items-center gap-2">
								<input
									type="text"
									bind:value={healthURL}
									placeholder={`http://${website.domain}`}
									aria-label={translate($language, 'wd.health.url_label')}
									class="flex-1 min-w-48 rounded-lg border border-gray-600 bg-gray-900 px-2.5 py-1.5 font-mono text-xs text-gray-200 focus:border-blue-500 focus:outline-none"
								/>
								<input
									type="number"
									bind:value={healthExpected}
									min="100"
									max="599"
									aria-label={translate($language, 'wd.health.expected_label')}
									class="w-20 rounded-lg border border-gray-600 bg-gray-900 px-2.5 py-1.5 font-mono text-xs text-gray-200 focus:border-blue-500 focus:outline-none"
								/>
								<button
									type="button"
									onclick={saveHealth}
									disabled={healthBusy}
									class="rounded-lg bg-blue-600 px-3 py-1.5 text-xs font-semibold text-white transition hover:bg-blue-700 disabled:opacity-50"
								>
									{translate($language, 'wd.save')}
								</button>
								<button
									type="button"
									onclick={checkHealthNow}
									disabled={healthBusy}
									class="rounded-lg border border-gray-600 bg-gray-700 px-3 py-1.5 text-xs font-semibold text-gray-200 transition hover:bg-gray-600 disabled:opacity-50"
								>
									{healthBusy ? translate($language, 'wd.health.checking') : translate($language, 'wd.health.check_now')}
								</button>
							</div>
							<p class="mt-2 text-[11px] text-gray-500">
								{translate($language, 'wd.health.hint')}
							</p>
						</div>
					{/if}

					{#if website.status === 'failed' && website.error_message}
						<div class="p-3 bg-red-900/50 border border-red-700 rounded-lg text-red-300 text-sm">
							{website.error_message}
						</div>
					{/if}

					{#if pendingStatuses.includes(website.status)}
						<div class="p-3 bg-yellow-900/30 border border-yellow-700 rounded-lg text-yellow-300 text-sm">
							{translate($language, 'wd.provisioning')} <span class="font-medium">{website.status}</span>
						</div>
					{/if}

					<!-- Info Cards -->
					<div class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
						<div class="bg-gray-800 rounded-lg border border-gray-700 p-4">
							<div class="text-xs text-gray-400 uppercase tracking-wider mb-1">{translate($language, 'wd.docroot')}</div>
							<div class="text-sm text-gray-200 font-mono">{website.document_root}</div>
						</div>
						<div class="bg-gray-800 rounded-lg border border-gray-700 p-4">
							<div class="text-xs text-gray-400 uppercase tracking-wider mb-1">{translate($language, 'wd.web_user')}</div>
							<div class="text-sm text-gray-200 font-mono">{website.web_user}</div>
						</div>
						<div class="bg-gray-800 rounded-lg border border-gray-700 p-4">
							<div class="text-xs text-gray-400 uppercase tracking-wider mb-1">{translate($language, 'wd.created')}</div>
							<div class="text-sm text-gray-200">{formatDate(website.created_at)}</div>
						</div>
					</div>

					<!-- Node.js Runtime (moved from the Node.js page) -->
					<div class="bg-gray-800 rounded-lg border border-gray-700 p-5">
						<h3 class="text-lg font-semibold text-white mb-3">{translate($language, 'wd.node.title')}</h3>
						{#if nodeRuntimeError}
							<div class="mb-3 p-3 bg-red-900/50 border border-red-700 rounded-lg text-red-300 text-sm">
								{nodeRuntimeError}
								<button onclick={() => (nodeRuntimeError = '')} class="ml-2 text-red-400 hover:text-red-200 cursor-pointer">{translate($language, 'wd.dismiss')}</button>
							</div>
						{/if}
						{#if nodeRuntimeLoading}
							<div class="text-gray-400 text-sm">{translate($language, 'wd.node.loading')}</div>
						{:else}
							<div class="flex flex-wrap items-center justify-between gap-4">
								<div>
									{#if nodeRuntime}
										<p class="text-sm text-gray-300">
											{nodeRuntime.installed ? translate($language, 'wd.node.installed').replace('{version}', nodeRuntime.installed_version).replace('{npm}', nodeRuntime.npm_version) : translate($language, 'wd.node.not_installed')}
											· NVM {nodeRuntime.nvm_version || nodeRuntime.nvm_state}
										</p>
										<p class="text-xs text-gray-400">{translate($language, 'wd.node.selected').replace('{version}', nodeRuntime.selected_version || translate($language, 'wd.none'))}</p>
										{#if nodeRuntime.error_message}<p class="text-sm text-red-400">{nodeRuntime.error_message}</p>{/if}
									{:else}
										<p class="text-sm text-gray-400">{translate($language, 'wd.node.none_configured')}</p>
									{/if}
								</div>
								<div class="flex gap-2">
									<select
										bind:value={nodeRuntimeChoice}
										aria-label={translate($language, 'wd.node.version_label')}
										disabled={!!nodeRuntimeTaskId}
										class="rounded border border-gray-600 bg-gray-900 px-3 py-2 text-gray-200"
									>
										{#each ['20', '22', '24'] as version}<option value={version}>Node.js {version}</option>{/each}
									</select>
									<button
										onclick={installNodeRuntime}
										disabled={!!nodeRuntimeTaskId}
										class="rounded bg-blue-600 px-4 py-2 text-white text-sm disabled:opacity-50"
									>
										{nodeRuntime?.installed ? translate($language, 'wd.node.install_update') : translate($language, 'wd.install')}
									</button>
								</div>
							</div>
							<TaskProgress bind:taskId={nodeRuntimeTaskId} storageKey="nodejs-task-{website.id}" onComplete={() => { nodeRuntimeTaskId = ''; void loadNodeRuntime(); }} />
						{/if}
					</div>

					<!-- Laravel Octane (FrankenPHP) -->
					{#if website.framework === 'laravel' || website.app_type === 'laravel'}
						<div class="bg-gray-800 rounded-lg border border-gray-700 p-5">
							<div class="flex flex-wrap items-center justify-between gap-2 mb-3">
								<h3 class="text-lg font-semibold text-white">Laravel Octane (FrankenPHP)</h3>
								{#if octane?.enabled}
									<span class="inline-block px-2.5 py-0.5 rounded text-xs font-medium {octane.running ? 'bg-green-900 text-green-300' : 'bg-gray-700 text-gray-400'}">
										{octane.running ? translate($language, 'wd.running') : translate($language, 'wd.stopped')}
									</span>
								{:else}
									<span class="inline-block px-2.5 py-0.5 rounded text-xs font-medium bg-gray-700 text-gray-400">{translate($language, 'wd.disabled')}</span>
								{/if}
							</div>

							{#if octaneLoading}
								<div class="text-gray-400 text-sm">{translate($language, 'wd.octane.loading')}</div>
							{:else if octane?.enabled}
								<div class="flex flex-wrap gap-4 text-sm text-gray-400 mb-4">
									<span>{translate($language, 'wd.port')} <span class="text-gray-200 font-mono">127.0.0.1:{octane.port}</span></span>
									<span>{translate($language, 'wd.octane.unit')} <span class="text-gray-200 font-mono">{octane.unit}</span></span>
									{#if octane.frankenphp_version}
										<span>FrankenPHP: <span class="text-gray-200">v{octane.frankenphp_version}</span></span>
									{/if}
								</div>
								<div class="flex flex-wrap items-center gap-2">
									<button
										onclick={() => octaneAction('start')}
										disabled={octaneBusy || octane.running}
										class="px-3 py-1.5 bg-green-600 hover:bg-green-700 disabled:opacity-50 text-white text-sm rounded transition-colors cursor-pointer"
									>{translate($language, 'wd.start')}</button>
									<button
										onclick={() => octaneAction('stop')}
										disabled={octaneBusy || !octane.running}
										class="px-3 py-1.5 bg-yellow-600 hover:bg-yellow-700 disabled:opacity-50 text-white text-sm rounded transition-colors cursor-pointer"
									>{translate($language, 'wd.stop')}</button>
									<button
										onclick={() => octaneAction('restart')}
										disabled={octaneBusy}
										class="px-3 py-1.5 bg-blue-600 hover:bg-blue-700 disabled:opacity-50 text-white text-sm rounded transition-colors cursor-pointer"
									>{translate($language, 'wd.restart')}</button>
									<button
										onclick={reloadOctane}
										disabled={octaneBusy || !octane.running}
										class="px-3 py-1.5 bg-gray-700 hover:bg-gray-600 disabled:opacity-50 text-gray-200 text-sm rounded transition-colors cursor-pointer"
										title={translate($language, 'wd.octane.reload_title')}
									>octane:reload</button>
									<button
										onclick={disableOctane}
										disabled={octaneBusy}
										class="px-3 py-1.5 bg-red-600 hover:bg-red-700 disabled:opacity-50 text-white text-sm rounded transition-colors cursor-pointer"
									>{translate($language, 'wd.octane.disable')}</button>
									<select
										bind:value={octaneWorkersChoice}
										aria-label={translate($language, 'wd.octane.workers_label')}
										class="rounded border border-gray-600 bg-gray-900 px-2 py-1.5 text-sm text-gray-200"
									>
										{#each [1, 2, 4, 6, 8, 12, 16] as count}<option value={String(count)}>{translate($language, 'wd.octane.workers_count').replace('{count}', String(count))}</option>{/each}
									</select>
									<button
										onclick={saveOctaneWorkers}
										disabled={octaneBusy || octaneWorkersChoice === String(octane.workers || 4)}
										class="px-3 py-1.5 bg-gray-700 hover:bg-gray-600 disabled:opacity-50 text-gray-200 text-sm rounded transition-colors cursor-pointer"
									>{translate($language, 'wd.octane.save_workers')}</button>
								</div>
							{:else}
								<p class="text-sm text-gray-400 mb-3">
									{translate($language, 'wd.octane.desc')}<code class="text-gray-300 font-mono">laravel/octane</code>{translate($language, 'wd.octane.desc_tail')}
								</p>
								{#if octane && !octane.frankenphp_version}
									<div class="mb-3 p-3 bg-yellow-900/30 border border-yellow-700 rounded-lg text-yellow-300 text-sm">
										{translate($language, 'wd.frankenphp.not_installed')}
										{#if canInstallFrankenphp}
											<button onclick={installFrankenphp} class="ml-2 underline cursor-pointer hover:text-yellow-200">{translate($language, 'wd.frankenphp.install')} {octane ? '' : ''}</button>
										{:else}
											{translate($language, 'wd.frankenphp.ask_admin')}
										{/if}
									</div>
								{/if}
								<button
									onclick={enableOctane}
									disabled={octaneBusy || website.status !== 'active' || (!!octane && !octane.frankenphp_version)}
									class="px-4 py-2 bg-green-600 hover:bg-green-700 disabled:opacity-50 text-white text-sm font-medium rounded transition-colors cursor-pointer"
								>
									{octaneBusy ? translate($language, 'wd.working') : translate($language, 'wd.octane.enable')}
								</button>
							{/if}
							<TaskProgress
								bind:taskId={octaneTaskId}
								storageKey="octane-task-{website.id}"
								onComplete={() => { octaneTaskId = ''; void loadOctaneStatus(); void loadWebsite(); }}
							/>
						</div>
					{/if}

					<!-- Actions -->
					<div class="bg-gray-800 rounded-lg border border-gray-700 p-5">
						<h3 class="text-lg font-semibold text-white mb-3">{translate($language, 'wd.actions')}</h3>
						<div class="flex flex-wrap gap-2">
							{#if website.status === 'active'}
								<button
									onclick={suspendWebsite}
									class="px-3 py-1.5 bg-yellow-600 hover:bg-yellow-700 text-white text-sm rounded transition-colors cursor-pointer"
								>
									{translate($language, 'wd.suspend')}
								</button>
							{/if}
							{#if website.status === 'suspended' || website.status === 'disabled'}
								<button
									onclick={enableWebsite}
									class="px-3 py-1.5 bg-green-600 hover:bg-green-700 text-white text-sm rounded transition-colors cursor-pointer"
								>
									{translate($language, 'wd.enable')}
								</button>
							{/if}
							{#if website.status === 'failed'}
								<button
									onclick={retryWebsite}
									class="px-3 py-1.5 bg-yellow-600 hover:bg-yellow-700 text-white text-sm rounded transition-colors cursor-pointer"
								>
									{translate($language, 'wd.retry')}
								</button>
							{/if}

							{#if deleteConfirm}
								<div class="flex items-center gap-2 p-2 bg-red-900/30 border border-red-700 rounded-lg">
									<span class="text-sm text-red-300">{translate($language, 'wd.delete_confirm')}</span>
									<button
										onclick={deleteWebsite}
										class="px-3 py-1.5 bg-red-600 hover:bg-red-700 text-white text-sm rounded transition-colors cursor-pointer"
									>
										{translate($language, 'wd.delete_yes')}
									</button>
									<button
										onclick={() => (deleteConfirm = false)}
										class="px-3 py-1.5 bg-gray-600 hover:bg-gray-500 text-white text-sm rounded transition-colors cursor-pointer"
									>
										{translate($language, 'wd.cancel')}
									</button>
								</div>
							{:else}
								<button
									onclick={() => (deleteConfirm = true)}
									class="px-3 py-1.5 bg-red-600 hover:bg-red-700 text-white text-sm rounded transition-colors cursor-pointer"
								>
									{translate($language, 'wd.delete')}
								</button>
							{/if}
						</div>
					</div>
				</div>

			<!-- ============================================================ -->
			<!-- DEPLOYMENT TAB                                                -->
			<!-- ============================================================ -->
			{:else if activeTab === 'Deployment'}
				<div class="space-y-6">
					<!-- Git Repository -->
					<div class="bg-gray-800 rounded-lg border border-gray-700 p-5">
						<h3 class="text-lg font-semibold text-white mb-4">{translate($language, 'wd.deploy.repo_title')}</h3>
						<div class="space-y-4">
							<!-- Provider -->
							<div>
								<label for="git-provider" class="block text-sm text-gray-400 mb-1">{translate($language, 'wd.deploy.provider')}</label>
								<select
									id="git-provider"
									bind:value={gitProvider}
									class="w-full max-w-xs px-3 py-2 bg-gray-900 border border-gray-600 rounded text-gray-200 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
								>
									<option value="github">GitHub</option>
									<option value="gitlab">GitLab</option>
									<option value="bitbucket">Bitbucket</option>
									<option value="custom">{translate($language, 'wd.custom')}</option>
								</select>
							</div>

							<!-- Repo URL -->
							<div>
								<label for="repo-url" class="block text-sm text-gray-400 mb-1">{translate($language, 'wd.deploy.repo_url')}</label>
								<input
									id="repo-url"
									type="text"
									bind:value={repoUrl}
									placeholder="git@github.com:user/repo.git"
									class="w-full px-3 py-2 bg-gray-900 border border-gray-600 rounded text-gray-200 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
								/>
							</div>

							<!-- Branch -->
							<div>
								<label for="repo-branch" class="block text-sm text-gray-400 mb-1">{translate($language, 'wd.branch')}</label>
								<input
									id="repo-branch"
									type="text"
									bind:value={repoBranch}
									placeholder="main"
									class="w-full max-w-xs px-3 py-2 bg-gray-900 border border-gray-600 rounded text-gray-200 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
								/>
							</div>

							<!-- Visibility -->
							<div>
								<span class="block text-sm text-gray-400 mb-2">{translate($language, 'wd.deploy.visibility')}</span>
								<div class="flex gap-4">
									<label class="flex items-center gap-2 cursor-pointer">
										<input type="radio" bind:group={repoVisibility} value="public" class="accent-blue-500" />
										<span class="text-sm text-gray-200">{translate($language, 'wd.public')}</span>
									</label>
									<label class="flex items-center gap-2 cursor-pointer">
										<input type="radio" bind:group={repoVisibility} value="private" class="accent-blue-500" />
										<span class="text-sm text-gray-200">{translate($language, 'wd.private')}</span>
									</label>
								</div>
							</div>

							<!-- Deploy Key (visible when private) -->
							{#if repoVisibility === 'private'}
								<div class="p-4 bg-gray-900 rounded-lg border border-gray-700 space-y-3">
									<h4 class="text-sm font-medium text-gray-300">{translate($language, 'wd.deploykey.title')}</h4>

									{#if deployKeyError}
										<div class="text-red-400 text-sm">{deployKeyError}</div>
									{/if}

									{#if deployKey}
										<div class="space-y-2">
											<textarea
												readonly
												value={deployKey}
												rows={4}
												class="w-full bg-gray-950 border border-gray-700 rounded p-3 text-gray-300 text-xs font-mono resize-none focus:outline-none"
											></textarea>
											<div class="flex items-center gap-2">
												<button
													onclick={copyDeployKey}
													class="px-3 py-1.5 bg-blue-600 hover:bg-blue-700 text-white text-xs rounded transition-colors cursor-pointer"
												>
													{deployKeyCopied ? translate($language, 'wd.copied') : translate($language, 'wd.copy')}
												</button>
												<button
													onclick={deleteDeployKey}
													disabled={deployKeyLoading}
													class="px-3 py-1.5 bg-red-600 hover:bg-red-700 disabled:opacity-50 text-white text-xs rounded transition-colors cursor-pointer"
												>
													{translate($language, 'wd.deploykey.delete')}
												</button>
											</div>
											<p class="text-xs text-gray-400 mt-2">
												{translate($language, providerInstructions[gitProvider])}
											</p>
										</div>
									{:else}
										<button
											onclick={generateDeployKey}
											disabled={deployKeyLoading}
											class="px-3 py-1.5 bg-blue-600 hover:bg-blue-700 disabled:opacity-50 text-white text-sm rounded transition-colors cursor-pointer"
										>
											{deployKeyLoading ? translate($language, 'wd.deploykey.generating') : translate($language, 'wd.deploykey.generate')}
										</button>
									{/if}
								</div>
							{/if}

							<!-- Deploy Now -->
							<div class="flex items-center gap-3 pt-2">
								<button
									onclick={deployNow}
									disabled={deploying || !repoUrl.trim()}
									class="px-4 py-2 bg-green-600 hover:bg-green-700 disabled:opacity-50 text-white text-sm font-medium rounded transition-colors cursor-pointer"
								>
									{deploying ? translate($language, 'wd.deploy.deploying') : translate($language, 'wd.deploy.now')}
								</button>
								{#if deploying}
									<span class="text-sm text-gray-400">{translate($language, 'wd.deploy.queued')}</span>
								{/if}
							</div>
						</div>

						{#if website.framework === 'laravel'}
							<div class="pt-4 border-t border-gray-700">
								<button
									onclick={repairLayout}
									disabled={repairingLayout}
									class="px-4 py-2 bg-yellow-600 hover:bg-yellow-700 disabled:opacity-50 text-white text-sm font-medium rounded transition-colors cursor-pointer"
								>
									{repairingLayout ? translate($language, 'wd.deploy.fixing') : translate($language, 'wd.deploy.fix')}
								</button>
								<p class="text-xs text-gray-500 mt-2">{translate($language, 'wd.deploy.fix_hint')}</p>
							</div>
						{/if}
					</div>

					<!-- Upload Files -->
					<div class="bg-gray-800 rounded-lg border border-gray-700 p-5">
						<h3 class="text-lg font-semibold text-white mb-4">{translate($language, 'wd.upload_title')}</h3>
						<p class="text-sm text-gray-400 mb-3">{translate($language, 'wd.upload_hint')}</p>
						<div class="flex items-center gap-3">
							<input
								type="file"
								accept=".zip,.tar.gz,.tgz"
								bind:this={uploadDeployFile}
								onchange={uploadDeploy}
								class="hidden"
							/>
							<button
								onclick={() => uploadDeployFile.click()}
								disabled={uploadingDeploy}
								class="px-4 py-2 bg-blue-600 hover:bg-blue-700 disabled:opacity-50 text-white text-sm font-medium rounded transition-colors cursor-pointer"
							>
								{uploadingDeploy ? translate($language, 'wd.uploading') : translate($language, 'wd.upload_extract')}
							</button>
						</div>
						<TaskProgress bind:taskId={uploadDeployTaskId} storageKey="upload-deploy-task-{website.id}" />
					</div>

					<!-- Manual (SSH) -->
					<div class="bg-gray-800 rounded-lg border border-gray-700 p-5">
						<h3 class="text-lg font-semibold text-white mb-4">{translate($language, 'wd.manual_ssh')}</h3>
						<div class="space-y-3">
							<div class="flex items-center gap-2">
								<span class="text-sm text-gray-400">SSH:</span>
								<code class="text-sm text-gray-200 font-mono bg-gray-900 px-2 py-1 rounded">ssh {website?.web_user}@{typeof window !== 'undefined' ? window.location.hostname : 'localhost'}</code>
								<button
									onclick={() => copyText(`ssh ${website?.web_user}@${window.location.hostname}`, 'ssh')}
									class="px-2 py-1 bg-gray-700 hover:bg-gray-600 text-gray-300 text-xs rounded transition-colors cursor-pointer"
								>
									{copiedField === 'ssh' ? translate($language, 'wd.copied') : translate($language, 'wd.copy')}
								</button>
							</div>
							<div class="flex items-center gap-2">
								<span class="text-sm text-gray-400">{translate($language, 'wd.docroot_label')}</span>
								<code class="text-sm text-gray-200 font-mono bg-gray-900 px-2 py-1 rounded">{website.document_root}</code>
								<button
									onclick={() => copyText(website?.document_root ?? '', 'docroot')}
									class="px-2 py-1 bg-gray-700 hover:bg-gray-600 text-gray-300 text-xs rounded transition-colors cursor-pointer"
								>
									{copiedField === 'docroot' ? translate($language, 'wd.copied') : translate($language, 'wd.copy')}
								</button>
							</div>
							<button
								onclick={() => setActiveTab('Terminal')}
								class="px-4 py-2 bg-gray-700 hover:bg-gray-600 text-white text-sm rounded transition-colors cursor-pointer"
							>
								{translate($language, 'wd.open_terminal')}
							</button>
						</div>
					</div>

					<!-- Deployment History -->
					<div class="bg-gray-800 rounded-lg border border-gray-700 p-5">
						<div class="flex items-center justify-between mb-4">
							<h3 class="text-lg font-semibold text-white">{translate($language, 'wd.deploy.history')}</h3>
							<button
								onclick={loadDeployments}
								disabled={deploymentsLoading}
								class="px-3 py-1.5 bg-gray-700 hover:bg-gray-600 disabled:opacity-50 text-gray-300 text-xs rounded transition-colors cursor-pointer"
							>
								{deploymentsLoading ? translate($language, 'wd.loading_generic') : translate($language, 'wd.refresh')}
							</button>
						</div>

						{#if deploymentsLoading && deployments.length === 0}
							<div class="text-gray-400 text-sm">{translate($language, 'wd.deploy.loading')}</div>
						{:else if deploymentsError}
							<div class="p-3 bg-red-900/50 border border-red-700 rounded-lg text-red-300 text-sm">{deploymentsError}</div>
						{:else if deployments.length === 0}
							<div class="text-gray-400 text-sm">{translate($language, 'wd.deploy.empty')}</div>
						{:else}
							<div class="overflow-x-auto">
								<table class="w-full">
									<thead>
										<tr class="border-b border-gray-700">
											<th class="text-left px-4 py-2 text-xs text-gray-400 uppercase tracking-wider font-medium">{translate($language, 'wd.th.commit')}</th>
											<th class="text-left px-4 py-2 text-xs text-gray-400 uppercase tracking-wider font-medium">{translate($language, 'wd.branch')}</th>
											<th class="text-left px-4 py-2 text-xs text-gray-400 uppercase tracking-wider font-medium">{translate($language, 'wd.th.status')}</th>
											<th class="text-left px-4 py-2 text-xs text-gray-400 uppercase tracking-wider font-medium">{translate($language, 'wd.th.duration')}</th>
											<th class="text-left px-4 py-2 text-xs text-gray-400 uppercase tracking-wider font-medium">{translate($language, 'wd.th.date')}</th>
										</tr>
									</thead>
									<tbody class="divide-y divide-gray-700">
										{#each deployments as dep}
											<tr
												class="hover:bg-gray-750 cursor-pointer"
												onclick={() => { expandedDeploymentId = expandedDeploymentId === dep.id ? null : dep.id; }}
											>
												<td class="px-4 py-2 text-sm text-gray-200 font-mono">{dep.commit_hash ? dep.commit_hash.slice(0, 8) : '-'}</td>
												<td class="px-4 py-2 text-sm text-gray-200">{dep.branch || '-'}</td>
												<td class="px-4 py-2">
													<span class="inline-block px-2 py-0.5 rounded text-xs font-medium {deployStatusBadgeClass(dep.status)}">{dep.status}</span>
													{#if dep.status === 'failed'}
														<span class="ml-2 text-xs text-red-400">{translate($language, 'wd.deploy.view_error')}</span>
													{/if}
												</td>
												<td class="px-4 py-2 text-sm text-gray-400">{dep.duration_ms ? formatDuration(dep.duration_ms) : '-'}</td>
												<td class="px-4 py-2 text-sm text-gray-400">{formatDate(dep.created_at)}</td>
											</tr>
											{#if expandedDeploymentId === dep.id}
												<tr>
													<td colspan="5" class="px-4 py-2">
														<pre class="text-xs bg-gray-950 rounded p-3 max-h-96 overflow-y-auto whitespace-pre-wrap font-mono {dep.status === 'failed' ? 'text-red-400' : 'text-gray-400'}">{dep.log || (dep.status === 'failed' ? translate($language, 'wd.deploy.failed_no_output') : translate($language, 'wd.deploy.no_output'))}</pre>
													</td>
												</tr>
											{/if}
										{/each}
									</tbody>
								</table>
							</div>
						{/if}
					</div>
				</div>

			<!-- ============================================================ -->
			<!-- SSL TAB                                                        -->
			<!-- ============================================================ -->
			{:else if activeTab === 'SSL'}
				<WebsiteSslSection {website} />

			<!-- ============================================================ -->
			<!-- COMMANDS TAB                                                   -->
			<!-- ============================================================ -->
			{:else if activeTab === 'Commands'}
				<div class="space-y-6">
					{#if commandsLoading}
						<div class="text-gray-400 text-sm">{translate($language, 'wd.cmd.loading')}</div>
					{:else if commandsError}
						<div class="p-3 bg-red-900/50 border border-red-700 rounded-lg text-red-300 text-sm">{commandsError}</div>
					{:else if commandPresets.length === 0}
						<div class="bg-gray-800 rounded-lg border border-gray-700 p-5">
							<p class="text-gray-400 text-sm">{translate($language, 'wd.cmd.empty')}</p>
						</div>
					{:else}
						<div class="grid grid-cols-1 md:grid-cols-2 gap-6 items-start">
							{#each Object.entries(groupedCommands()) as [category, presets]}
								<div class="bg-gray-800 rounded-lg border border-gray-700 p-5">
									<h3 class="text-lg font-semibold text-white mb-3 capitalize">{category}</h3>
									<div class="flex flex-wrap gap-2">
										{#each presets as preset}
											<button
												type="button"
												onclick={() => (pendingCommand = preset)}
												class="px-3 py-2 text-sm rounded transition-colors cursor-pointer
													{preset.danger
														? 'bg-red-600 hover:bg-red-700 text-white'
														: 'bg-gray-700 hover:bg-gray-600 text-gray-200'}"
											>
												{preset.label}
											</button>
										{/each}
									</div>
								</div>
							{/each}

							{#if website.framework === 'laravel'}
								<div class="bg-gray-800 rounded-lg border border-gray-700 p-5">
									<div class="flex items-center justify-between mb-4">
										<h3 class="text-lg font-semibold text-white">Laravel .env</h3>
										{#if envExists}
											<div class="inline-flex rounded-lg border border-gray-600 p-1 bg-gray-900" role="group" aria-label={translate($language, 'wd.env.mode_label')}>
												<button
													type="button"
													onclick={() => switchEnvMode('values')}
													aria-pressed={envMode === 'values'}
													class="px-3 py-1.5 rounded text-sm transition-colors cursor-pointer {envMode === 'values' ? 'bg-blue-600 text-white' : 'text-gray-300 hover:bg-gray-700'}"
												>{translate($language, 'wd.env.edit_values')}</button>
												<button
													type="button"
													onclick={() => switchEnvMode('raw')}
													aria-pressed={envMode === 'raw'}
													class="px-3 py-1.5 rounded text-sm transition-colors cursor-pointer {envMode === 'raw' ? 'bg-blue-600 text-white' : 'text-gray-300 hover:bg-gray-700'}"
												>{translate($language, 'wd.env.manual')}</button>
											</div>
										{/if}
									</div>

									{#if envError}
										<div class="mb-3 p-3 bg-red-900/50 border border-red-700 rounded-lg text-red-300 text-sm">
											{envError}
											<button onclick={() => (envError = '')} class="ml-2 text-red-400 hover:text-red-200 cursor-pointer">{translate($language, 'wd.dismiss')}</button>
										</div>
									{/if}

									{#if envMsg}
										<div class="mb-3 p-3 bg-green-900/50 border border-green-700 rounded-lg text-green-300 text-sm">
											{envMsg}
											<button onclick={() => (envMsg = '')} class="ml-2 text-green-400 hover:text-green-200 cursor-pointer">{translate($language, 'wd.dismiss')}</button>
										</div>
									{/if}

									{#if envLoading}
										<div class="text-gray-400 text-sm">{translate($language, 'wd.env.loading')}</div>
									{:else if !envExists}
										<p class="text-sm text-gray-400 mb-3">{translate($language, 'wd.env.not_found_a')} <code class="text-gray-300 font-mono">.env</code>{translate($language, 'wd.env.not_found_b')}<code class="text-gray-300 font-mono">.env.example</code>{translate($language, 'wd.env.not_found_c')}</p>
										<button
											type="button"
											onclick={createEnvFile}
											class="px-4 py-2 bg-green-600 hover:bg-green-700 text-white text-sm font-medium rounded transition-colors cursor-pointer"
										>cp .env.example .env</button>
									{:else if envMode === 'values'}
										<div class="max-h-96 overflow-y-auto border border-gray-700 rounded">
											<table class="w-full">
												<thead>
												<tr class="border-b border-gray-700">
													<th class="text-left px-3 py-2 text-xs text-gray-400 uppercase tracking-wider font-medium">{translate($language, 'wd.env.key')}</th>
													<th class="text-left px-3 py-2 text-xs text-gray-400 uppercase tracking-wider font-medium">{translate($language, 'wd.env.value')}</th>
												</tr>
												</thead>
												<tbody class="divide-y divide-gray-700">
													{#each envValues as row}
														<tr>
															<td class="px-3 py-1.5 text-xs text-gray-300 font-mono align-middle">{row.key}</td>
															<td class="px-3 py-1.5">
																<input
																	bind:value={row.value}
																	aria-label={row.key}
																	class="w-full px-2 py-1 bg-gray-950 border border-gray-700 rounded text-xs font-mono text-gray-200 focus:outline-none focus:ring-2 focus:ring-blue-500"
																/>
															</td>
														</tr>
													{/each}
												</tbody>
											</table>
										</div>
										<button
											onclick={saveEnv}
											disabled={envSaving}
											class="mt-3 px-4 py-2 bg-blue-600 hover:bg-blue-700 disabled:opacity-50 text-white text-sm font-medium rounded transition-colors cursor-pointer"
										>
											{envSaving ? translate($language, 'wd.saving') : translate($language, 'wd.env.save')}
										</button>
									{:else}
										<textarea
											bind:value={envRaw}
											rows={16}
											spellcheck="false"
											aria-label={translate($language, 'wd.env.contents_label')}
											class="w-full bg-gray-950 border border-gray-700 rounded p-3 text-xs font-mono text-gray-200 resize-y focus:outline-none focus:ring-2 focus:ring-blue-500"
										></textarea>
										<button
											onclick={saveEnv}
											disabled={envSaving}
											class="mt-3 px-4 py-2 bg-blue-600 hover:bg-blue-700 disabled:opacity-50 text-white text-sm font-medium rounded transition-colors cursor-pointer"
										>
										{envSaving ? translate($language, 'wd.saving') : translate($language, 'wd.env.save')}
									</button>
										{/if}
							</div>
						{/if}
						</div>
					{/if}

					<TaskProgress
						bind:taskId={commandTaskId}
						storageKey="cmd-task-{website.id}"
						onComplete={() => {
							// Give the Completed badge a moment, then reload so every
							// section reflects the post-command state.
							setTimeout(() => window.location.reload(), 800);
						}}
					/>
				</div>

			<!-- ============================================================ -->
			<!-- FILES TAB                                                     -->
			<!-- ============================================================ -->
			{:else if activeTab === 'Files'}
				<WebsiteFilesSection {website} />

			<!-- ============================================================ -->
			<!-- TERMINAL TAB                                                  -->
			<!-- ============================================================ -->
			{:else if activeTab === 'Queue'}
				<WebsiteQueueSection websiteID={website.id} domain={website.domain} />

			{:else if activeTab === 'Cron Jobs'}
				<WebsiteCronSection websiteID={website.id} domain={website.domain} />

			{:else if activeTab === 'PHP Settings'}
				<WebsitePhpSettingsSection websiteID={website.id} appType={website.app_type} />

			{:else if activeTab === 'App'}
				<WebsiteAppSection websiteID={website.id} domain={website.domain} nodeVersion={website.node_version} runtime={website.app_type} />

			{:else if activeTab === 'WP Toolkit'}
				<WebsiteWpToolkitSection websiteID={website.id} domain={website.domain} />

			{:else if activeTab === 'Terminal'}
				<TerminalConsole
					endpoint={terminalEndpoint}
					active={activeTab === 'Terminal'}
					title={website.web_user}
					heightClass="h-[26rem]"
				/>

			<!-- ============================================================ -->
			<!-- LOGS TAB                                                      -->
			<!-- ============================================================ -->
			{:else if activeTab === 'Logs'}
				<div class="bg-gray-800 rounded-lg border border-gray-700 p-5">
					<h3 class="text-lg font-semibold text-white mb-3">{translate($language, 'wd.logs.title')}</h3>

					<div class="flex gap-2 mb-4">
						<button
							onclick={() => { logTab = 'access'; loadLogs('access'); }}
							class="px-3 py-1.5 text-sm rounded transition-colors cursor-pointer {logTab === 'access'
								? 'bg-blue-600 text-white'
								: 'bg-gray-700 text-gray-300 hover:bg-gray-600'}"
						>
							{translate($language, 'wd.logs.access')}
						</button>
						<button
							onclick={() => { logTab = 'error'; loadLogs('error'); }}
							class="px-3 py-1.5 text-sm rounded transition-colors cursor-pointer {logTab === 'error'
								? 'bg-blue-600 text-white'
								: 'bg-gray-700 text-gray-300 hover:bg-gray-600'}"
						>
							{translate($language, 'wd.logs.error')}
						</button>
						{#if website.octane_enabled}
							<button
								onclick={() => { logTab = 'octane'; loadLogs('octane'); }}
								class="px-3 py-1.5 text-sm rounded transition-colors cursor-pointer {logTab === 'octane'
									? 'bg-blue-600 text-white'
									: 'bg-gray-700 text-gray-300 hover:bg-gray-600'}"
							>
								Octane
							</button>
						{/if}
					</div>

					{#if logsError}
						<div class="mb-2 text-red-400 text-sm">{logsError}</div>
					{/if}

					<div class="flex items-center justify-end mb-2">
						<button
							onclick={() => loadLogs(logTab)}
							disabled={logsLoading}
							class="px-2.5 py-1 bg-gray-700 hover:bg-gray-600 disabled:opacity-50 text-gray-300 text-xs rounded transition-colors cursor-pointer"
						>
							{logsLoading ? translate($language, 'wd.loading_generic') : translate($language, 'wd.refresh')}
							</button>
					</div>

					<textarea
						readonly
						value={logTab === 'access' ? accessLogs : logTab === 'octane' ? octaneLogs : errorLogs}
						class="w-full h-64 bg-gray-950 border border-gray-700 rounded p-3 text-gray-300 text-xs font-mono resize-y focus:outline-none"
					></textarea>
				</div>

			<!-- ============================================================ -->
			<!-- CONFIG TAB                                                    -->
			<!-- ============================================================ -->
			{:else if activeTab === 'Config'}
				<div class="space-y-4">
					<!-- Template Engine -->
					<div class="rounded-lg border border-gray-700 bg-gray-800 p-5">
						<div class="mb-1 flex items-center gap-2.5">
							<h3 class="text-lg font-semibold text-white">{translate($language, 'wd.config.template_title')}</h3>
							<span class="rounded-full bg-blue-900/50 px-2.5 py-0.5 text-[11px] font-semibold text-blue-300">
								{effectiveProfileLabel}
							</span>
						</div>
						<p class="mb-4 text-sm text-gray-400">
							{translate($language, 'wd.config.template_hint')}
						</p>

						<div class="flex flex-wrap items-start gap-3">
							<div class="min-w-56 flex-1">
								<label for="nginx-profile" class="mb-1.5 block text-xs font-medium uppercase tracking-wider text-gray-400">{translate($language, 'wd.config.template')}</label>
								<select
									id="nginx-profile"
									bind:value={profileChoice}
									disabled={profileSaving}
									class="w-full rounded-lg border border-gray-600 bg-gray-900 px-3 py-2 text-sm text-gray-200 focus:border-blue-500 focus:outline-none focus:ring-2 focus:ring-blue-500/30"
								>
									{#each profileOptions as opt}
										<option value={opt.value}>{translate($language, opt.label)}</option>
									{/each}
								</select>
								{#if selectedProfileDescription}
									<p class="mt-2 text-xs leading-5 text-gray-500">{selectedProfileDescription}</p>
								{/if}
							</div>
							<button
								onclick={applyNginxProfile}
								disabled={profileSaving || (website.nginx_profile || '') === profileChoice}
								class="mt-6 cursor-pointer rounded-lg bg-blue-600 px-4 py-2 text-sm font-semibold text-white transition hover:bg-blue-700 disabled:cursor-not-allowed disabled:opacity-40"
							>
								{profileSaving ? translate($language, 'wd.config.applying') : translate($language, 'wd.config.apply')}
							</button>
						</div>
					</div>

					<!-- Manual editor -->
					<div class="rounded-lg border border-gray-700 bg-gray-800 p-5">
						<h3 class="text-lg font-semibold text-white mb-3">{translate($language, 'wd.config.nginx_title')}</h3>

						{#if configError}
							<div class="mb-2 text-red-400 text-sm">{configError}</div>
						{/if}
						{#if configSaveMsg}
							<div class="mb-2 text-green-400 text-sm">
								{configSaveMsg}
								<button onclick={() => (configSaveMsg = '')} class="ml-2 hover:underline cursor-pointer">{translate($language, 'wd.dismiss')}</button>
							</div>
						{/if}

						{#if configLoading}
							<div class="text-gray-400 text-sm">{translate($language, 'wd.config.loading')}</div>
						{:else}
							<textarea
								bind:value={configContent}
								rows={20}
								class="w-full bg-gray-950 border border-gray-700 rounded p-3 text-gray-300 text-sm font-mono resize-y focus:outline-none focus:ring-2 focus:ring-blue-500"
							></textarea>
							<div class="mt-2 flex gap-2">
								<button
									onclick={saveConfig}
									class="px-4 py-2 bg-blue-600 hover:bg-blue-700 text-white text-sm font-medium rounded transition-colors cursor-pointer"
								>
									{translate($language, 'wd.config.save')}
								</button>
								<button
									onclick={loadConfig}
									class="px-4 py-2 bg-gray-700 hover:bg-gray-600 text-gray-300 text-sm rounded transition-colors cursor-pointer"
								>
									{translate($language, 'wd.reload')}
								</button>
							</div>
						{/if}
					</div>
				</div>

			<!-- ============================================================ -->
			<!-- DOMAINS TAB                                                   -->
			<!-- ============================================================ -->
			{:else if activeTab === 'Domains'}
				<div class="bg-gray-800 rounded-lg border border-gray-700 p-5">
					<h3 class="text-lg font-semibold text-white mb-3">{translate($language, 'wd.domains.title')}</h3>

					{#if website.domains && website.domains.length > 0}
						<div class="space-y-2 mb-4">
							{#each website.domains as domain}
								<div class="flex items-center justify-between p-3 bg-gray-900 rounded-lg">
									<div class="flex items-center gap-3">
										<span class="text-sm text-gray-200">{domain.name}</span>
										<span class="inline-block px-2 py-0.5 rounded text-xs font-medium {domainTypeBadgeClass(domain.type)}">
											{domain.type}
										</span>
									</div>
										{#if domain.type !== 'primary'}
											<button
												onclick={() => removeDomain(domain.id)}
												class="px-2.5 py-1 bg-red-600 hover:bg-red-700 text-white text-xs rounded transition-colors cursor-pointer"
											>
												{translate($language, 'wd.remove')}
											</button>
										{/if}
									</div>
								{/each}
							</div>
						{:else}
							<p class="text-sm text-gray-400 mb-4">{translate($language, 'wd.domains.empty')}</p>
						{/if}

						<!-- Add Domain Form -->
						<div class="flex flex-wrap items-end gap-3">
							<div>
								<label for="add-domain-name" class="block text-sm text-gray-400 mb-1">{translate($language, 'wd.domains.name')}</label>
							<input
								id="add-domain-name"
								type="text"
								bind:value={addDomainName}
								placeholder="sub.example.com"
								class="px-3 py-2 bg-gray-900 border border-gray-600 rounded text-gray-200 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
							/>
						</div>
						<div>
							<label for="add-domain-type" class="block text-sm text-gray-400 mb-1">{translate($language, 'wd.type')}</label>
							<select
								id="add-domain-type"
								bind:value={addDomainType}
								class="px-3 py-2 bg-gray-900 border border-gray-600 rounded text-gray-200 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
							>
								<option value="alias">{translate($language, 'wd.domains.alias')}</option>
								<option value="subdomain">{translate($language, 'wd.domains.subdomain')}</option>
							</select>
						</div>
						<button
							onclick={addDomain}
							disabled={addingDomain || !addDomainName.trim()}
							class="px-4 py-2 bg-blue-600 hover:bg-blue-700 disabled:opacity-50 text-white text-sm font-medium rounded transition-colors cursor-pointer"
						>
							{addingDomain ? translate($language, 'wd.adding') : translate($language, 'wd.domains.add')}
						</button>
					</div>
				</div>
			{/if}
		</div>
	{/if}
</div>

<!-- Command confirmation modal -->
{#if pendingCommand}
	<div
		class="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4 backdrop-blur-sm"
		role="dialog"
		aria-modal="true"
		aria-label={translate($language, 'wd.cmd.confirm_label')}
	>
		<div class="w-full max-w-md rounded-2xl border border-gray-700 bg-gray-800 p-5 shadow-2xl">
			<div class="flex items-start gap-3">
				<span
					class="flex h-10 w-10 shrink-0 items-center justify-center rounded-xl {pendingCommand.danger
						? 'bg-red-500/10 text-red-400'
						: 'bg-blue-500/10 text-blue-400'}"
				>
					<svg class="h-5 w-5" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="1.5" aria-hidden="true">
						{#if pendingCommand.danger}
							<path stroke-linecap="round" stroke-linejoin="round" d="M12 9v3.75m-9.303 3.376c-.866 1.5.217 3.374 1.948 3.374h14.71c1.73 0 2.813-1.874 1.948-3.374L13.949 3.378c-.866-1.5-3.032-1.5-3.898 0L2.697 16.126zM12 15.75h.007v.008H12v-.008z" />
						{:else}
							<path stroke-linecap="round" stroke-linejoin="round" d="M6.75 7.5l3 2.25-3 2.25m4.5 0h3m-9 8.25h13.5A2.25 2.25 0 0021 18V6a2.25 2.25 0 00-2.25-2.25H5.25A2.25 2.25 0 003 6v12a2.25 2.25 0 002.25 2.25z" />
						{/if}
					</svg>
				</span>
				<div class="min-w-0">
					<h4 class="text-base font-semibold text-white">
						{pendingCommand.danger ? translate($language, 'wd.cmd.danger_title') : translate($language, 'wd.cmd.title')}
					</h4>
					<p class="mt-1 text-sm text-gray-400">
						{#if pendingCommand.danger}
							{translate($language, 'wd.cmd.danger_body')}
						{:else}
							{translate($language, 'wd.cmd.runs_as')} <span class="font-mono text-gray-300">{website?.web_user}</span>.
						{/if}
						{translate($language, 'wd.cmd.auto_reload')}
					</p>
				</div>
			</div>

			<pre class="mt-3 overflow-x-auto rounded-lg border border-gray-700 bg-gray-950 p-3 font-mono text-xs text-gray-200">{pendingCommand.command}</pre>

			<div class="mt-4 flex justify-end gap-2">
				<button
					type="button"
					onclick={() => (pendingCommand = null)}
					disabled={commandConfirmBusy}
					class="cursor-pointer rounded-lg bg-gray-700 px-3.5 py-2 text-sm font-medium text-gray-200 transition hover:bg-gray-600 disabled:opacity-50"
				>
					{translate($language, 'wd.cancel')}
				</button>
				<button
					type="button"
					onclick={confirmRunCommand}
					disabled={commandConfirmBusy}
					class="cursor-pointer rounded-lg px-3.5 py-2 text-sm font-semibold text-white transition disabled:opacity-50 {pendingCommand.danger
						? 'bg-red-600 hover:bg-red-700'
						: 'bg-blue-600 hover:bg-blue-700'}"
				>
					{commandConfirmBusy ? translate($language, 'wd.cmd.starting') : pendingCommand.danger ? translate($language, 'wd.cmd.yes_run') : translate($language, 'wd.cmd.run')}
				</button>
			</div>
		</div>
	</div>
{/if}
