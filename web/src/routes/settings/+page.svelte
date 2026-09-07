<script lang="ts">
	import { onMount } from 'svelte';
	import { api } from '$lib/api';
	import type { Setting } from '$lib/types';

	let settings = $state<Setting[]>([]);
	let loading = $state(true);
	let error = $state('');
	let saving = $state(false);
	let actionMsg = $state('');
	let actionError = $state('');

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
			error = err instanceof Error ? err.message : 'Failed to load settings';
		} finally {
			loading = false;
		}
	}

	async function saveSettings() {
		saving = true;
		actionMsg = '';
		actionError = '';

		try {
			// Build diff: only send changed values
			const payload: Record<string, string> = {};
			for (const s of settings) {
				if (editedValues[s.key] !== s.value) {
					payload[s.key] = editedValues[s.key];
				}
			}

			if (Object.keys(payload).length === 0) {
				actionMsg = 'No changes to save.';
				saving = false;
				return;
			}

			await api.put('/api/v1/settings', payload);
			actionMsg = 'Settings saved successfully.';
			await loadSettings();
		} catch (err) {
			actionError = err instanceof Error ? err.message : 'Failed to save settings';
		} finally {
			saving = false;
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
			totpError = err instanceof Error ? err.message : 'Failed to setup 2FA';
		}
	}

	async function enableTotp() {
		totpMsg = '';
		totpError = '';
		if (!totpCode.trim()) {
			totpError = 'Please enter the verification code.';
			return;
		}
		try {
			await api.post('/api/v1/settings/2fa/enable', { code: totpCode });
			totpMsg = '2FA enabled successfully.';
			totpEnabled = true;
			showTotpSetup = false;
			totpCode = '';
			totpSetupUrl = '';
		} catch (err) {
			totpError = err instanceof Error ? err.message : 'Failed to enable 2FA';
		}
	}

	async function disableTotp() {
		disableTotpConfirm = false;
		totpMsg = '';
		totpError = '';
		try {
			await api.post('/api/v1/settings/2fa/disable');
			totpMsg = '2FA disabled.';
			totpEnabled = false;
		} catch (err) {
			totpError = err instanceof Error ? err.message : 'Failed to disable 2FA';
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
			tokenError = 'Token name is required.';
			return;
		}
		try {
			const data = await api.post<{ token: string }>('/api/v1/settings/tokens', { name: newTokenName });
			createdToken = data.token;
			tokenMsg = 'Token created. Copy it now -- it will not be shown again.';
			newTokenName = '';
			await loadTokens();
		} catch (err) {
			tokenError = err instanceof Error ? err.message : 'Failed to create token';
		}
	}

	async function deleteToken(id: string) {
		tokenMsg = '';
		tokenError = '';
		createdToken = '';
		try {
			await api.del(`/api/v1/settings/tokens/${id}`);
			tokenMsg = 'Token deleted.';
			await loadTokens();
		} catch (err) {
			tokenError = err instanceof Error ? err.message : 'Failed to delete token';
		}
	}

	onMount(() => {
		loadSettings();
		loadTotpStatus();
		loadTokens();
	});
</script>

<div class="space-y-6">
	<div class="flex items-center justify-between">
		<h2 class="text-2xl font-bold text-white">Settings</h2>
		<button
			onclick={saveSettings}
			disabled={saving}
			class="px-4 py-2 bg-blue-600 hover:bg-blue-700 disabled:opacity-50 text-white text-sm font-medium rounded-lg transition-colors cursor-pointer"
		>
			{saving ? 'Saving...' : 'Save Changes'}
		</button>
	</div>

	{#if actionMsg}
		<div class="p-3 bg-green-900/50 border border-green-700 rounded-lg text-green-300 text-sm">
			{actionMsg}
			<button onclick={() => (actionMsg = '')} class="ml-2 text-green-400 hover:text-green-200 cursor-pointer">
				Dismiss
			</button>
		</div>
	{/if}

	{#if actionError}
		<div class="p-3 bg-red-900/50 border border-red-700 rounded-lg text-red-300 text-sm">
			{actionError}
			<button onclick={() => (actionError = '')} class="ml-2 text-red-400 hover:text-red-200 cursor-pointer">
				Dismiss
			</button>
		</div>
	{/if}

	{#if loading}
		<div class="text-gray-400">Loading settings...</div>
	{:else if error}
		<div class="p-4 bg-red-900/50 border border-red-700 rounded-lg text-red-300">{error}</div>
	{:else if settings.length === 0}
		<div class="bg-gray-800 rounded-lg border border-gray-700 p-8 text-center text-gray-400">
			No settings found.
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
		<h3 class="text-lg font-semibold text-white mb-3">Two-Factor Authentication</h3>

		{#if totpMsg}
			<div class="mb-3 p-3 bg-green-900/50 border border-green-700 rounded-lg text-green-300 text-sm">
				{totpMsg}
				<button onclick={() => (totpMsg = '')} class="ml-2 text-green-400 hover:text-green-200 cursor-pointer">Dismiss</button>
			</div>
		{/if}

		{#if totpError}
			<div class="mb-3 p-3 bg-red-900/50 border border-red-700 rounded-lg text-red-300 text-sm">
				{totpError}
				<button onclick={() => (totpError = '')} class="ml-2 text-red-400 hover:text-red-200 cursor-pointer">Dismiss</button>
			</div>
		{/if}

		{#if totpLoading}
			<div class="text-gray-400 text-sm">Loading 2FA status...</div>
		{:else if totpEnabled}
			<div class="flex items-center gap-3 mb-3">
				<span class="inline-flex items-center gap-1.5 px-2.5 py-0.5 rounded-full text-xs font-medium bg-green-900/50 text-green-400">
					<span class="w-1.5 h-1.5 rounded-full bg-green-400"></span>
					Enabled
				</span>
			</div>
			{#if disableTotpConfirm}
				<div class="p-4 bg-red-950 border border-red-700 rounded-lg">
					<p class="text-red-300 text-sm font-medium mb-1">Disable Two-Factor Authentication?</p>
					<p class="text-red-400 text-xs mb-3">This will remove the extra security layer from your account.</p>
					<div class="flex gap-2">
						<button onclick={disableTotp} class="px-3 py-1.5 bg-red-600 hover:bg-red-700 text-white text-sm rounded transition-colors cursor-pointer">Confirm Disable</button>
						<button onclick={() => (disableTotpConfirm = false)} class="px-3 py-1.5 bg-gray-600 hover:bg-gray-500 text-white text-sm rounded transition-colors cursor-pointer">Cancel</button>
					</div>
				</div>
			{:else}
				<button onclick={() => (disableTotpConfirm = true)} class="px-4 py-2 bg-red-600 hover:bg-red-700 text-white text-sm font-medium rounded transition-colors cursor-pointer">
					Disable 2FA
				</button>
			{/if}
		{:else}
			{#if showTotpSetup}
				<div class="space-y-3">
					<p class="text-sm text-gray-300">Scan this QR code with your authenticator app:</p>
					<div class="p-3 bg-gray-900 rounded-lg">
						<p class="text-xs text-gray-400 font-mono break-all">{totpSetupUrl}</p>
					</div>
					<div class="flex items-end gap-3">
						<div>
							<label for="totp-code" class="block text-xs text-gray-400 uppercase tracking-wider mb-1">Verification Code</label>
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
							Enable 2FA
						</button>
						<button onclick={() => { showTotpSetup = false; totpSetupUrl = ''; totpCode = ''; }} class="px-4 py-2 bg-gray-600 hover:bg-gray-500 text-white text-sm rounded transition-colors cursor-pointer">
							Cancel
						</button>
					</div>
				</div>
			{:else}
				<p class="text-sm text-gray-400 mb-3">Add an extra layer of security to your account with TOTP-based two-factor authentication.</p>
				<button onclick={setupTotp} class="px-4 py-2 bg-blue-600 hover:bg-blue-700 text-white text-sm font-medium rounded transition-colors cursor-pointer">
					Setup 2FA
				</button>
			{/if}
		{/if}
	</div>

	<!-- API Tokens -->
	<div class="bg-gray-800 rounded-lg border border-gray-700 p-5">
		<h3 class="text-lg font-semibold text-white mb-3">API Tokens</h3>

		{#if tokenMsg}
			<div class="mb-3 p-3 bg-green-900/50 border border-green-700 rounded-lg text-green-300 text-sm">
				{tokenMsg}
				<button onclick={() => (tokenMsg = '')} class="ml-2 text-green-400 hover:text-green-200 cursor-pointer">Dismiss</button>
			</div>
		{/if}

		{#if tokenError}
			<div class="mb-3 p-3 bg-red-900/50 border border-red-700 rounded-lg text-red-300 text-sm">
				{tokenError}
				<button onclick={() => (tokenError = '')} class="ml-2 text-red-400 hover:text-red-200 cursor-pointer">Dismiss</button>
			</div>
		{/if}

		{#if createdToken}
			<div class="mb-4 p-4 bg-yellow-950 border border-yellow-700 rounded-lg">
				<p class="text-yellow-300 text-sm font-medium mb-2">Your new API token (copy it now):</p>
				<code class="block p-2 bg-gray-900 rounded text-sm text-yellow-200 font-mono break-all select-all">{createdToken}</code>
			</div>
		{/if}

		{#if tokensLoading}
			<div class="text-gray-400 text-sm">Loading tokens...</div>
		{:else}
			{#if tokens.length > 0}
				<div class="overflow-x-auto mb-4">
					<table class="w-full">
						<thead>
							<tr class="border-b border-gray-700">
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Name</th>
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Created</th>
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Last Used</th>
								<th class="text-right px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Actions</th>
							</tr>
						</thead>
						<tbody class="divide-y divide-gray-700">
							{#each tokens as token}
								<tr class="hover:bg-gray-750">
									<td class="px-4 py-3 text-sm text-white">{token.name}</td>
									<td class="px-4 py-3 text-sm text-gray-400">{new Date(token.created_at).toLocaleString()}</td>
									<td class="px-4 py-3 text-sm text-gray-400">{token.last_used ? new Date(token.last_used).toLocaleString() : 'Never'}</td>
									<td class="px-4 py-3 text-right">
										<button onclick={() => deleteToken(token.id)} class="px-2.5 py-1 bg-red-600 hover:bg-red-700 text-white text-xs rounded transition-colors cursor-pointer">Delete</button>
									</td>
								</tr>
							{/each}
						</tbody>
					</table>
				</div>
			{:else}
				<div class="text-gray-400 text-sm mb-4">No API tokens created.</div>
			{/if}

			<!-- Create Token Form -->
			<div class="flex items-end gap-3 pt-3 border-t border-gray-700">
				<div>
					<label for="token-name" class="block text-xs text-gray-400 uppercase tracking-wider mb-1">Token Name</label>
					<input
						id="token-name"
						type="text"
						bind:value={newTokenName}
						placeholder="e.g. CI/CD Pipeline"
						class="px-3 py-2 bg-gray-700 border border-gray-600 rounded text-white text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
					/>
				</div>
				<button onclick={createToken} class="px-4 py-2 bg-blue-600 hover:bg-blue-700 text-white text-sm font-medium rounded transition-colors cursor-pointer">
					Create Token
				</button>
			</div>
		{/if}
	</div>
</div>
