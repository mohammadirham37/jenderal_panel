<script lang="ts">
	import { onMount } from 'svelte';
	import { api } from '$lib/api';
	import type { Setting, SSHKey } from '$lib/types';
	import { user as authUser } from '$lib/stores/auth';
	import { language, translate } from '$lib/stores/language';
import { toast } from '$lib/stores/toast';
import TaskProgress from '$lib/components/TaskProgress.svelte';

	let settings = $state<Setting[]>([]);
	let loading = $state(true);
	let error = $state('');
	let saving = $state(false);

	// Track edited values
	let editedValues = $state<Record<string, string>>({});

	// 2FA
	let totpEnabled = $state(false);
	let totpLoading = $state(true);
	let totpSetupUrl = $state('');
	let totpCode = $state('');
	let totpMsg = $state('');
	let totpError = $state('');
	let showTotpSetup = $state(false);
	let disableTotpConfirm = $state(false);

	// API Tokens
	interface ApiToken {
		id: string;
		name: string;
		created_at: string;
		last_used: string | null;
	}

	let tokens = $state<ApiToken[]>([]);
	let tokensLoading = $state(true);
	let newTokenName = $state('');
	let createdToken = $state('');
	let tokenMsg = $state('');
	let tokenError = $state('');

	// SSH Keys (self-service; the account is the panel username)
	let sshKeys = $state<SSHKey[]>([]);
	let sshKeysLoading = $state(true);
	let sshKeyLoaded = $state(false);
	let newSSHKeyName = $state('');
	let newSSHKeyMaterial = $state('');
	let sshKeyMsg = $state('');
	let sshKeyError = $state('');

	async function loadSSHKeys() {
		sshKeysLoading = true;
		try {
			sshKeys = (await api.get<SSHKey[]>('/api/v1/profile/ssh-keys')) || [];
			sshKeyLoaded = true;
			sshKeyError = '';
		} catch (err) {
			sshKeyError = err instanceof Error ? err.message : translate($language, 'set.ssh.loadFailed');
		} finally {
			sshKeysLoading = false;
		}
	}

	async function addSSHKey() {
		if (!newSSHKeyMaterial.trim()) return;
		sshKeyMsg = '';
		sshKeyError = '';
		try {
			await api.post('/api/v1/profile/ssh-keys', {
				name: newSSHKeyName,
				public_key: newSSHKeyMaterial
			});
			newSSHKeyName = '';
			newSSHKeyMaterial = '';
			sshKeyMsg = translate($language, 'set.ssh.addedMsg');
			await loadSSHKeys();
		} catch (err) {
			sshKeyError = err instanceof Error ? err.message : translate($language, 'set.ssh.addFailed');
		}
	}

	async function deleteSSHKey(key: SSHKey) {
		if (!confirm(translate($language, 'set.ssh.removeConfirm').replace('{name}', key.name || key.fingerprint))) return;
		sshKeyMsg = '';
		sshKeyError = '';
		try {
			await api.del(`/api/v1/profile/ssh-keys/${key.id}`);
			sshKeyMsg = translate($language, 'set.ssh.removedMsg');
			await loadSSHKeys();
		} catch (err) {
			sshKeyError = err instanceof Error ? err.message : translate($language, 'set.ssh.deleteFailed');
		}
	}

	async function loadSettings() {
		try {
			settings = (await api.get<Setting[]>('/api/v1/settings')) || [];
			// Initialize edited values
			const vals: Record<string, string> = {};
			for (const s of settings) {
				vals[s.key] = s.value;
			}
			editedValues = vals;
		} catch (err) {
			error = err instanceof Error ? err.message : translate($language, 'set.loadFailed');
		} finally {
			loading = false;
		}
		loadRemote();
	}

	async function saveSettings() {
		saving = true;

		try {
			// Build diff: only send changed values
			const payload: Record<string, string> = {};
			for (const s of settings) {
				if (editedValues[s.key] !== s.value) {
					payload[s.key] = editedValues[s.key];
				}
			}

			if (Object.keys(payload).length === 0) {
				toast.success(translate($language, 'set.noChanges'));
				saving = false;
				return;
			}

			await api.put('/api/v1/settings', payload);
			toast.success(translate($language, 'set.saved'));
			await loadSettings();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : translate($language, 'set.saveFailed'));
		} finally {
			saving = false;
		}
	}

	// ─── Remote backup storage (S3) ───────────────────────────────
	const remoteKeys = [
		'backup_remote_type', 'backup_remote_s3_endpoint', 'backup_remote_s3_bucket',
		'backup_remote_s3_region', 'backup_remote_s3_access_key', 'backup_remote_s3_secret_key',
		'backup_remote_s3_prefix', 'backup_remote_rclone_remote', 'backup_remote_rclone_path'
	];
	let remote = $state<Record<string, string>>({
		backup_remote_type: '',
		backup_remote_s3_endpoint: '',
		backup_remote_s3_bucket: '',
		backup_remote_s3_region: 'us-east-1',
		backup_remote_s3_access_key: '',
		backup_remote_s3_secret_key: '',
		backup_remote_s3_prefix: '',
		backup_remote_rclone_remote: '',
		backup_remote_rclone_path: ''
	});
	let savingRemote = $state(false);

	function loadRemote() {
		const next = { ...remote };
		for (const s of settings) {
			if ((remoteKeys as readonly string[]).includes(s.key)) {
				next[s.key] = s.value;
			}
		}
		remote = next;
	}

	async function saveRemote() {
		if (savingRemote) return;
		savingRemote = true;
		try {
			const payload: Record<string, string> = {};
			for (const key of remoteKeys) payload[key] = remote[key] ?? '';
			await api.put('/api/v1/settings', payload);
			toast.success(translate($language, 'set.remote.saved'));
			await loadSettings();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : translate($language, 'set.remote.saveFailed'));
		} finally {
			savingRemote = false;
		}
	}

	// ─── Panel domain (HTTPS untuk panel) ─────────────────────────
	interface PanelDomainStatus {
		domain: string;
		email: string;
		enabled: boolean;
		vhost_present: boolean;
		cert_expiry?: string;
	}
	let pdStatus = $state<PanelDomainStatus | null>(null);
	let pdDomain = $state('');
	let pdEmail = $state('');
	let pdBusy = $state(false);
	let pdTaskId = $state('');
	let pdConfirmDisable = $state(false);

	async function loadPanelDomain() {
		try {
			pdStatus = await api.get<PanelDomainStatus>('/api/v1/panel-domain');
			if (pdStatus.domain) pdDomain = pdStatus.domain;
			if (pdStatus.email) pdEmail = pdStatus.email;
		} catch (err) {
			toast.error(err instanceof Error ? err.message : translate($language, 'set.domain.loadFailed'));
		}
	}

	async function setupPanelDomain() {
		if (pdBusy || !pdDomain.trim() || !pdEmail.trim()) return;
		pdBusy = true;
		try {
			const res = await api.post<{ task_id: string }>('/api/v1/panel-domain/setup', {
				domain: pdDomain.trim(),
				email: pdEmail.trim()
			});
			pdTaskId = res.task_id;
			toast.success(translate($language, 'set.domain.setupStarted'));
			await loadPanelDomain();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : translate($language, 'set.domain.setupFailed'));
		} finally {
			pdBusy = false;
		}
	}

	async function renewPanelDomain() {
		if (pdBusy) return;
		pdBusy = true;
		try {
			const res = await api.post<{ task_id: string }>('/api/v1/panel-domain/renew', {});
			pdTaskId = res.task_id;
			toast.success(translate($language, 'set.domain.renewStarted'));
		} catch (err) {
			toast.error(err instanceof Error ? err.message : translate($language, 'set.domain.renewFailed'));
		} finally {
			pdBusy = false;
		}
	}

	async function disablePanelDomain() {
		if (pdBusy || !pdConfirmDisable) { pdConfirmDisable = true; return; }
		pdBusy = true;
		try {
			await api.post('/api/v1/panel-domain/disable', { domain: pdStatus?.domain || pdDomain });
			toast.success(translate($language, 'set.domain.removed'));
			pdConfirmDisable = false;
			await loadPanelDomain();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : translate($language, 'set.domain.removeFailed'));
		} finally {
			pdBusy = false;
		}
	}

	// 2FA functions
	async function loadTotpStatus() {
		totpLoading = true;
		try {
			const data = await api.get<{ enabled: boolean }>('/api/v1/settings/2fa/status');
			totpEnabled = data.enabled;
		} catch {
			// If endpoint doesn't exist, assume not enabled
			totpEnabled = false;
		} finally {
			totpLoading = false;
		}
	}

	async function setupTotp() {
		totpMsg = '';
		totpError = '';
		try {
			const data = await api.post<{ qr_url: string }>('/api/v1/settings/2fa/setup');
			totpSetupUrl = data.qr_url;
			showTotpSetup = true;
		} catch (err) {
			totpError = err instanceof Error ? err.message : translate($language, 'set.totp.setupFailed');
		}
	}

	async function enableTotp() {
		totpMsg = '';
		totpError = '';
		if (!totpCode.trim()) {
			totpError = translate($language, 'set.totp.enterCode');
			return;
		}
		try {
			await api.post('/api/v1/settings/2fa/enable', { code: totpCode });
			totpMsg = translate($language, 'set.totp.enabledMsg');
			totpEnabled = true;
			showTotpSetup = false;
			totpCode = '';
			totpSetupUrl = '';
		} catch (err) {
			totpError = err instanceof Error ? err.message : translate($language, 'set.totp.enableFailed');
		}
	}

	async function disableTotp() {
		disableTotpConfirm = false;
		totpMsg = '';
		totpError = '';
		try {
			await api.post('/api/v1/settings/2fa/disable');
			totpMsg = translate($language, 'set.totp.disabledMsg');
			totpEnabled = false;
		} catch (err) {
			totpError = err instanceof Error ? err.message : translate($language, 'set.totp.disableFailed');
		}
	}

	// API Token functions
	async function loadTokens() {
		tokensLoading = true;
		try {
			tokens = (await api.get<ApiToken[]>('/api/v1/settings/tokens')) || [];
		} catch {
			tokens = [];
		} finally {
			tokensLoading = false;
		}
	}

	async function createToken() {
		tokenMsg = '';
		tokenError = '';
		createdToken = '';
		if (!newTokenName.trim()) {
			tokenError = translate($language, 'set.tokens.nameRequired');
			return;
		}
		try {
			const data = await api.post<{ token: string }>('/api/v1/settings/tokens', { name: newTokenName });
			createdToken = data.token;
			tokenMsg = translate($language, 'set.tokens.createdMsg');
			newTokenName = '';
			await loadTokens();
		} catch (err) {
			tokenError = err instanceof Error ? err.message : translate($language, 'set.tokens.createFailed');
		}
	}

	async function deleteToken(id: string) {
		tokenMsg = '';
		tokenError = '';
		createdToken = '';
		try {
			await api.del(`/api/v1/settings/tokens/${id}`);
			tokenMsg = translate($language, 'set.tokens.deletedMsg');
			await loadTokens();
		} catch (err) {
			tokenError = err instanceof Error ? err.message : translate($language, 'set.tokens.deleteFailed');
		}
	}

	onMount(() => {
		loadSettings();
		loadTotpStatus();
		loadTokens();
		loadSSHKeys();
	});
</script>

<div class="space-y-6">
	<div class="flex items-center justify-between">
		<h2 class="text-2xl font-bold text-white">{translate($language, 'set.title')}</h2>
		<button
			onclick={saveSettings}
			disabled={saving}
			class="px-4 py-2 bg-blue-600 hover:bg-blue-700 disabled:opacity-50 text-white text-sm font-medium rounded-lg transition-colors cursor-pointer"
		>
			{saving ? translate($language, 'set.saving') : translate($language, 'set.saveChanges')}
		</button>
	</div>

	<!-- Remote backup storage (S3) -->
	<div class="bg-gray-800 rounded-lg border border-gray-700 p-5 mb-6">
		<h3 class="text-lg font-semibold text-white mb-1">{translate($language, 'set.remote.title')}</h3>
		<p class="text-xs text-gray-500 mb-4">
			{translate($language, 'set.remote.desc')}
		</p>
		<div class="grid gap-3 md:grid-cols-2 lg:grid-cols-3">
			<div>
				<label class="block text-[11px] font-medium uppercase tracking-wider text-gray-400 mb-1" for="remote-type">{translate($language, 'set.type')}</label>
				<select id="remote-type" bind:value={remote.backup_remote_type}
					class="w-full bg-gray-900 border border-gray-700 rounded px-3 py-2 text-sm text-gray-200 focus:outline-none focus:ring-2 focus:ring-blue-500">
					<option value="">{translate($language, 'set.remote.off')}</option>
					<option value="s3">s3</option>
					<option value="rclone">rclone</option>
				</select>
			</div>
			{#if remote.backup_remote_type === 'rclone'}
				<div>
					<label class="block text-[11px] font-medium uppercase tracking-wider text-gray-400 mb-1" for="remote-rclone-remote">{translate($language, 'set.remote.rcloneRemote')}</label>
					<input id="remote-rclone-remote" type="text" bind:value={remote.backup_remote_rclone_remote} placeholder="gdrive"
						class="w-full bg-gray-900 border border-gray-700 rounded px-3 py-2 text-sm text-gray-200 focus:outline-none focus:ring-2 focus:ring-blue-500" />
				</div>
				<div>
					<label class="block text-[11px] font-medium uppercase tracking-wider text-gray-400 mb-1" for="remote-rclone-path">{translate($language, 'set.remote.rclonePath')}</label>
					<input id="remote-rclone-path" type="text" bind:value={remote.backup_remote_rclone_path} placeholder="backups/vps-1"
						class="w-full bg-gray-900 border border-gray-700 rounded px-3 py-2 text-sm text-gray-200 focus:outline-none focus:ring-2 focus:ring-blue-500" />
				</div>
			{:else}
				<div>
					<label class="block text-[11px] font-medium uppercase tracking-wider text-gray-400 mb-1" for="remote-endpoint">{translate($language, 'set.remote.endpoint')}</label>
				<input id="remote-endpoint" type="text" bind:value={remote.backup_remote_s3_endpoint} placeholder="s3.wasabisys.com"
					class="w-full bg-gray-900 border border-gray-700 rounded px-3 py-2 text-sm text-gray-200 focus:outline-none focus:ring-2 focus:ring-blue-500" />
			</div>
			<div>
				<label class="block text-[11px] font-medium uppercase tracking-wider text-gray-400 mb-1" for="remote-bucket">{translate($language, 'set.remote.bucket')}</label>
				<input id="remote-bucket" type="text" bind:value={remote.backup_remote_s3_bucket}
					class="w-full bg-gray-900 border border-gray-700 rounded px-3 py-2 text-sm text-gray-200 focus:outline-none focus:ring-2 focus:ring-blue-500" />
			</div>
			<div>
				<label class="block text-[11px] font-medium uppercase tracking-wider text-gray-400 mb-1" for="remote-region">{translate($language, 'set.remote.region')}</label>
				<input id="remote-region" type="text" bind:value={remote.backup_remote_s3_region} placeholder="us-east-1"
					class="w-full bg-gray-900 border border-gray-700 rounded px-3 py-2 text-sm text-gray-200 focus:outline-none focus:ring-2 focus:ring-blue-500" />
			</div>
			<div>
				<label class="block text-[11px] font-medium uppercase tracking-wider text-gray-400 mb-1" for="remote-access">{translate($language, 'set.remote.accessKey')}</label>
				<input id="remote-access" type="text" bind:value={remote.backup_remote_s3_access_key}
					class="w-full bg-gray-900 border border-gray-700 rounded px-3 py-2 text-sm text-gray-200 focus:outline-none focus:ring-2 focus:ring-blue-500" />
			</div>
			<div>
				<label class="block text-[11px] font-medium uppercase tracking-wider text-gray-400 mb-1" for="remote-secret">{translate($language, 'set.remote.secretKey')}</label>
				<input id="remote-secret" type="password" bind:value={remote.backup_remote_s3_secret_key}
					class="w-full bg-gray-900 border border-gray-700 rounded px-3 py-2 text-sm text-gray-200 focus:outline-none focus:ring-2 focus:ring-blue-500" />
			</div>
			{/if}
			<div>
				<label class="block text-[11px] font-medium uppercase tracking-wider text-gray-400 mb-1" for="remote-prefix">{translate($language, 'set.remote.prefix')}</label>
				<input id="remote-prefix" type="text" bind:value={remote.backup_remote_s3_prefix} placeholder="vps-1/backups"
					class="w-full bg-gray-900 border border-gray-700 rounded px-3 py-2 text-sm text-gray-200 focus:outline-none focus:ring-2 focus:ring-blue-500" />
			</div>
		</div>
		<button
			type="button"
			onclick={saveRemote}
			disabled={savingRemote}
			class="mt-4 px-4 py-2 bg-blue-600 hover:bg-blue-700 disabled:opacity-50 text-white text-sm font-medium rounded-lg transition-colors cursor-pointer"
		>
			{savingRemote ? translate($language, 'set.saving') : translate($language, 'set.remote.save')}
		</button>
	</div>

	<!-- Language Selector -->
	<div class="bg-gray-800 rounded-lg border border-gray-700 p-5 mb-6">
		<h3 class="text-lg font-semibold text-white mb-3">{translate($language, 'settings.language')}</h3>
		<select
			bind:value={$language}
			class="bg-gray-900 border border-gray-700 text-gray-300 rounded px-3 py-2 focus:outline-none focus:ring-2 focus:ring-blue-500"
		>
			<option value="en">{translate($language, 'settings.language.en')}</option>
			<option value="id">{translate($language, 'settings.language.id')}</option>
		</select>
	</div>
	<!-- Panel domain (HTTPS untuk panel) -->
	<div class="bg-gray-800 rounded-lg border border-gray-700 p-5 mb-6">
		<div class="flex flex-wrap items-center justify-between gap-2 mb-1">
			<h3 class="text-lg font-semibold text-white">{translate($language, 'set.domain.title')}</h3>
			{#if pdStatus?.enabled}
				<span class="rounded-full bg-green-900/50 px-2.5 py-0.5 text-[11px] font-medium text-green-400">{translate($language, 'set.domain.enabled')}</span>
			{/if}
		</div>
		<p class="text-xs text-gray-500 mb-4">
			{translate($language, 'set.domain.descBefore')}<code class="text-gray-400">https://panel.domain-anda.com</code>{translate($language, 'set.domain.descAfter')}
		</p>
		{#if pdTaskId}
			<div class="rounded-lg border border-gray-700 bg-gray-900 p-3 mb-3">
				<p class="mb-2 text-[11px] font-semibold uppercase tracking-wider text-gray-400">{translate($language, 'set.domain.setupProgress')}</p>
				<TaskProgress bind:taskId={pdTaskId} storageKey="panel-domain-task" onComplete={loadPanelDomain} />
			</div>
		{/if}
		<div class="grid gap-3 md:grid-cols-2">
			<div>
				<label class="block text-[11px] font-medium uppercase tracking-wider text-gray-400 mb-1" for="pd-domain">{translate($language, 'set.domain.title')}</label>
				<input id="pd-domain" type="text" bind:value={pdDomain} placeholder="panel.example.com"
					class="w-full bg-gray-900 border border-gray-700 rounded px-3 py-2 text-sm text-gray-200 focus:outline-none focus:ring-2 focus:ring-blue-500" />
			</div>
			<div>
				<label class="block text-[11px] font-medium uppercase tracking-wider text-gray-400 mb-1" for="pd-email">{translate($language, 'set.domain.emailLabel')}</label>
				<input id="pd-email" type="email" bind:value={pdEmail} placeholder="admin@example.com"
					class="w-full bg-gray-900 border border-gray-700 rounded px-3 py-2 text-sm text-gray-200 focus:outline-none focus:ring-2 focus:ring-blue-500" />
			</div>
		</div>
		{#if pdStatus?.cert_expiry}
			<p class="mt-3 text-xs text-gray-500">{translate($language, 'set.domain.certExpires')} {new Date(pdStatus.cert_expiry).toLocaleString()}</p>
		{/if}
		<div class="mt-4 flex flex-wrap items-center gap-2">
			<button type="button" onclick={setupPanelDomain} disabled={pdBusy || !pdDomain.trim() || !pdEmail.trim()}
				class="px-4 py-2 bg-blue-600 hover:bg-blue-700 disabled:opacity-50 text-white text-sm font-medium rounded-lg transition-colors cursor-pointer">
				{pdBusy ? translate($language, 'set.domain.working') : (pdStatus?.enabled ? translate($language, 'set.domain.rerunSetup') : translate($language, 'set.domain.setupButton'))}
			</button>
			{#if pdStatus?.enabled}
				<button type="button" onclick={renewPanelDomain} disabled={pdBusy}
					class="px-3 py-2 bg-gray-700 hover:bg-gray-600 disabled:opacity-50 text-gray-200 text-xs rounded-lg transition-colors cursor-pointer">{translate($language, 'set.domain.renewCert')}</button>
				<button type="button" onclick={disablePanelDomain} disabled={pdBusy}
					class="px-3 py-2 bg-red-600 hover:bg-red-700 disabled:opacity-50 text-white text-xs rounded-lg transition-colors cursor-pointer">{pdConfirmDisable ? translate($language, 'set.domain.confirmRemove') : translate($language, 'set.domain.remove')}</button>
			{/if}
		</div>
		{#if pdConfirmDisable}
			<p class="mt-2 text-[11px] text-red-400">{translate($language, 'set.domain.removeWarning')}</p>
		{/if}
	</div>



	{#if loading}
		<div class="text-gray-400">{translate($language, 'set.loading')}</div>
	{:else if error}
		<div class="p-4 bg-red-900/50 border border-red-700 rounded-lg text-red-300">{error}</div>
	{:else if settings.length === 0}
		<div class="bg-gray-800 rounded-lg border border-gray-700 p-8 text-center text-gray-400">
			{translate($language, 'set.empty')}
		</div>
	{:else}
		<div class="bg-gray-800 rounded-lg border border-gray-700 divide-y divide-gray-700">
			{#each settings as setting}
				<div class="flex items-center gap-4 p-4">
					<div class="w-48 shrink-0">
						<label for="setting-{setting.key}" class="text-sm font-medium text-gray-300 font-mono">
							{setting.key}
						</label>
					</div>
					<div class="flex-1">
						<input
							id="setting-{setting.key}"
							type="text"
							bind:value={editedValues[setting.key]}
							class="w-full px-3 py-2 bg-gray-700 border border-gray-600 rounded-lg text-white text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
						/>
					</div>
					<div class="text-xs text-gray-500 w-40 shrink-0 text-right">
						{new Date(setting.updated_at).toLocaleString()}
					</div>
				</div>
			{/each}
		</div>
	{/if}

	<!-- Two-Factor Authentication -->
	<div class="bg-gray-800 rounded-lg border border-gray-700 p-5">
		<h3 class="text-lg font-semibold text-white mb-3">{translate($language, 'set.totp.title')}</h3>

		{#if totpMsg}
			<div class="mb-3 p-3 bg-green-900/50 border border-green-700 rounded-lg text-green-300 text-sm">
				{totpMsg}
				<button onclick={() => (totpMsg = '')} class="ml-2 text-green-400 hover:text-green-200 cursor-pointer">{translate($language, 'set.dismiss')}</button>
			</div>
		{/if}

		{#if totpError}
			<div class="mb-3 p-3 bg-red-900/50 border border-red-700 rounded-lg text-red-300 text-sm">
				{totpError}
				<button onclick={() => (totpError = '')} class="ml-2 text-red-400 hover:text-red-200 cursor-pointer">{translate($language, 'set.dismiss')}</button>
			</div>
		{/if}

		{#if totpLoading}
			<div class="text-gray-400 text-sm">{translate($language, 'set.totp.loading')}</div>
		{:else if totpEnabled}
			<div class="flex items-center gap-3 mb-3">
				<span class="inline-flex items-center gap-1.5 px-2.5 py-0.5 rounded-full text-xs font-medium bg-green-900/50 text-green-400">
					<span class="w-1.5 h-1.5 rounded-full bg-green-400"></span>
					{translate($language, 'set.totp.enabledBadge')}
				</span>
			</div>
			{#if disableTotpConfirm}
				<div class="p-4 bg-red-950 border border-red-700 rounded-lg">
					<p class="text-red-300 text-sm font-medium mb-1">{translate($language, 'set.totp.disableConfirmTitle')}</p>
					<p class="text-red-400 text-xs mb-3">{translate($language, 'set.totp.disableConfirmBody')}</p>
					<div class="flex gap-2">
						<button onclick={disableTotp} class="px-3 py-1.5 bg-red-600 hover:bg-red-700 text-white text-sm rounded transition-colors cursor-pointer">{translate($language, 'set.totp.confirmDisable')}</button>
						<button onclick={() => (disableTotpConfirm = false)} class="px-3 py-1.5 bg-gray-600 hover:bg-gray-500 text-white text-sm rounded transition-colors cursor-pointer">{translate($language, 'set.totp.cancel')}</button>
					</div>
				</div>
			{:else}
				<button onclick={() => (disableTotpConfirm = true)} class="px-4 py-2 bg-red-600 hover:bg-red-700 text-white text-sm font-medium rounded transition-colors cursor-pointer">
					{translate($language, 'set.totp.disable2fa')}
				</button>
			{/if}
		{:else}
			{#if showTotpSetup}
				<div class="space-y-3">
					<p class="text-sm text-gray-300">{translate($language, 'set.totp.scanQr')}</p>
					<div class="p-3 bg-gray-900 rounded-lg">
						<p class="text-xs text-gray-400 font-mono break-all">{totpSetupUrl}</p>
					</div>
					<div class="flex items-end gap-3">
						<div>
							<label for="totp-code" class="block text-xs text-gray-400 uppercase tracking-wider mb-1">{translate($language, 'set.totp.codeLabel')}</label>
							<input
								id="totp-code"
								type="text"
								bind:value={totpCode}
								placeholder="000000"
								maxlength={6}
								class="w-32 px-3 py-2 bg-gray-700 border border-gray-600 rounded text-white text-sm font-mono text-center focus:outline-none focus:ring-2 focus:ring-blue-500"
							/>
						</div>
						<button onclick={enableTotp} class="px-4 py-2 bg-blue-600 hover:bg-blue-700 text-white text-sm font-medium rounded transition-colors cursor-pointer">
							{translate($language, 'set.totp.enable2fa')}
						</button>
						<button onclick={() => { showTotpSetup = false; totpSetupUrl = ''; totpCode = ''; }} class="px-4 py-2 bg-gray-600 hover:bg-gray-500 text-white text-sm rounded transition-colors cursor-pointer">
							{translate($language, 'set.totp.cancel')}
						</button>
					</div>
				</div>
			{:else}
				<p class="text-sm text-gray-400 mb-3">{translate($language, 'set.totp.desc')}</p>
				<button onclick={setupTotp} class="px-4 py-2 bg-blue-600 hover:bg-blue-700 text-white text-sm font-medium rounded transition-colors cursor-pointer">
					{translate($language, 'set.totp.setup2fa')}
				</button>
			{/if}
		{/if}
	</div>

	<!-- SSH Keys -->
	{#if $authUser?.ssh_enabled}
	<div class="bg-gray-800 rounded-lg border border-gray-700 p-5">
		<h3 class="text-lg font-semibold text-white mb-3">{translate($language, 'set.ssh.title')}</h3>
		<p class="text-sm text-gray-400 mb-4">
			{translate($language, 'set.ssh.descBefore')}<code class="text-gray-300 font-mono">{$authUser.username}</code>{translate($language, 'set.ssh.descAfter')}
		</p>

		{#if sshKeyMsg}
			<div class="mb-3 p-3 bg-green-900/50 border border-green-700 rounded-lg text-green-300 text-sm">
				{sshKeyMsg}
				<button onclick={() => (sshKeyMsg = '')} class="ml-2 text-green-400 hover:text-green-200 cursor-pointer">{translate($language, 'set.dismiss')}</button>
			</div>
		{/if}

		{#if sshKeyError}
			<div class="mb-3 p-3 bg-red-900/50 border border-red-700 rounded-lg text-red-300 text-sm">
				{sshKeyError}
				<button onclick={() => (sshKeyError = '')} class="ml-2 text-red-400 hover:text-red-200 cursor-pointer">{translate($language, 'set.dismiss')}</button>
			</div>
		{/if}

		{#if sshKeysLoading}
			<div class="text-gray-400 text-sm">{translate($language, 'set.ssh.loading')}</div>
		{:else}
			{#if sshKeys.length > 0}
				<div class="overflow-x-auto mb-4">
					<table class="w-full">
						<thead>
							<tr class="border-b border-gray-700">
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">{translate($language, 'set.name')}</th>
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">{translate($language, 'set.ssh.fingerprint')}</th>
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">{translate($language, 'set.type')}</th>
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">{translate($language, 'set.ssh.added')}</th>
								<th class="text-right px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">{translate($language, 'set.actions')}</th>
							</tr>
						</thead>
						<tbody class="divide-y divide-gray-700">
							{#each sshKeys as key (key.id)}
								<tr class="hover:bg-gray-750">
									<td class="px-4 py-3 text-sm text-white">{key.name}</td>
									<td class="px-4 py-3 text-sm text-gray-400 font-mono">{key.fingerprint}</td>
									<td class="px-4 py-3 text-sm text-gray-400">{key.algo} {key.bits}bit</td>
									<td class="px-4 py-3 text-sm text-gray-400">{new Date(key.created_at).toLocaleDateString()}</td>
									<td class="px-4 py-3 text-right">
										<button onclick={() => deleteSSHKey(key)} class="px-2.5 py-1 bg-red-600 hover:bg-red-700 text-white text-xs rounded transition-colors cursor-pointer">{translate($language, 'set.delete')}</button>
									</td>
								</tr>
							{/each}
						</tbody>
					</table>
				</div>
			{:else}
				<div class="text-gray-400 text-sm mb-4">{translate($language, 'set.ssh.empty')}</div>
			{/if}

			<!-- Add Key Form -->
			<div class="flex flex-wrap items-end gap-3 pt-3 border-t border-gray-700">
				<div>
					<label for="ssh-key-name" class="block text-xs text-gray-400 uppercase tracking-wider mb-1">{translate($language, 'set.ssh.label')}</label>
					<input
						id="ssh-key-name"
						type="text"
						bind:value={newSSHKeyName}
						placeholder={translate($language, 'set.ssh.labelPlaceholder')}
						class="px-3 py-2 bg-gray-700 border border-gray-600 rounded text-white text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
					/>
				</div>
				<div class="flex-1 min-w-72">
					<label for="ssh-key-material" class="block text-xs text-gray-400 uppercase tracking-wider mb-1">{translate($language, 'set.ssh.publicKeyLabel')}</label>
					<input
						id="ssh-key-material"
						type="text"
						bind:value={newSSHKeyMaterial}
						placeholder="ssh-ed25519 AAAA... you@host"
						spellcheck="false"
						class="w-full px-3 py-2 bg-gray-700 border border-gray-600 rounded text-white text-sm font-mono focus:outline-none focus:ring-2 focus:ring-blue-500"
					/>
				</div>
				<button onclick={addSSHKey} disabled={!newSSHKeyMaterial.trim()} class="px-4 py-2 bg-blue-600 hover:bg-blue-700 disabled:opacity-50 text-white text-sm font-medium rounded transition-colors cursor-pointer">
					{translate($language, 'set.ssh.addKey')}
				</button>
			</div>
		{/if}
	</div>
	{/if}

	<!-- API Tokens -->
	<div class="bg-gray-800 rounded-lg border border-gray-700 p-5">
		<h3 class="text-lg font-semibold text-white mb-3">{translate($language, 'set.tokens.title')}</h3>

		{#if tokenMsg}
			<div class="mb-3 p-3 bg-green-900/50 border border-green-700 rounded-lg text-green-300 text-sm">
				{tokenMsg}
				<button onclick={() => (tokenMsg = '')} class="ml-2 text-green-400 hover:text-green-200 cursor-pointer">{translate($language, 'set.dismiss')}</button>
			</div>
		{/if}

		{#if tokenError}
			<div class="mb-3 p-3 bg-red-900/50 border border-red-700 rounded-lg text-red-300 text-sm">
				{tokenError}
				<button onclick={() => (tokenError = '')} class="ml-2 text-red-400 hover:text-red-200 cursor-pointer">{translate($language, 'set.dismiss')}</button>
			</div>
		{/if}

		{#if createdToken}
			<div class="mb-4 p-4 bg-yellow-950 border border-yellow-700 rounded-lg">
				<p class="text-yellow-300 text-sm font-medium mb-2">{translate($language, 'set.tokens.createdLabel')}</p>
				<code class="block p-2 bg-gray-900 rounded text-sm text-yellow-200 font-mono break-all select-all">{createdToken}</code>
			</div>
		{/if}

		{#if tokensLoading}
			<div class="text-gray-400 text-sm">{translate($language, 'set.tokens.loading')}</div>
		{:else}
			{#if tokens.length > 0}
				<div class="overflow-x-auto mb-4">
					<table class="w-full">
						<thead>
							<tr class="border-b border-gray-700">
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">{translate($language, 'set.name')}</th>
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">{translate($language, 'set.tokens.created')}</th>
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">{translate($language, 'set.tokens.lastUsed')}</th>
								<th class="text-right px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">{translate($language, 'set.actions')}</th>
							</tr>
						</thead>
						<tbody class="divide-y divide-gray-700">
							{#each tokens as token}
								<tr class="hover:bg-gray-750">
									<td class="px-4 py-3 text-sm text-white">{token.name}</td>
									<td class="px-4 py-3 text-sm text-gray-400">{new Date(token.created_at).toLocaleString()}</td>
									<td class="px-4 py-3 text-sm text-gray-400">{token.last_used ? new Date(token.last_used).toLocaleString() : translate($language, 'set.tokens.never')}</td>
									<td class="px-4 py-3 text-right">
										<button onclick={() => deleteToken(token.id)} class="px-2.5 py-1 bg-red-600 hover:bg-red-700 text-white text-xs rounded transition-colors cursor-pointer">{translate($language, 'set.delete')}</button>
									</td>
								</tr>
							{/each}
						</tbody>
					</table>
				</div>
			{:else}
				<div class="text-gray-400 text-sm mb-4">{translate($language, 'set.tokens.empty')}</div>
			{/if}

			<!-- Create Token Form -->
			<div class="flex items-end gap-3 pt-3 border-t border-gray-700">
				<div>
					<label for="token-name" class="block text-xs text-gray-400 uppercase tracking-wider mb-1">{translate($language, 'set.tokens.nameLabel')}</label>
					<input
						id="token-name"
						type="text"
						bind:value={newTokenName}
						placeholder={translate($language, 'set.tokens.namePlaceholder')}
						class="px-3 py-2 bg-gray-700 border border-gray-600 rounded text-white text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
					/>
				</div>
				<button onclick={createToken} class="px-4 py-2 bg-blue-600 hover:bg-blue-700 text-white text-sm font-medium rounded transition-colors cursor-pointer">
					{translate($language, 'set.tokens.create')}
				</button>
			</div>
		{/if}
	</div>
</div>
