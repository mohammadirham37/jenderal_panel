<script lang="ts">
	import { onMount } from 'svelte';
	import { api } from '$lib/api';

	interface NotificationChannel {
		id: string;
		type: string;
		config: Record<string, unknown>;
		enabled: boolean;
		created_at: string;
	}
	type ChannelConfig = Record<string, string | number>;

	let channels = $state<NotificationChannel[]>([]);
	let loading = $state(true);
	let error = $state('');
	let actionMsg = $state('');
	let actionError = $state('');

	// Create form
	let newType = $state('email');
	let newConfig = $state<ChannelConfig>({});
	let creating = $state(false);

	// Edit state
	let editingChannel = $state<NotificationChannel | null>(null);
	let editConfig = $state<ChannelConfig>({});

	const typeOptions = ['email', 'telegram', 'discord', 'webhook'];

	function defaultConfig(type: string): ChannelConfig {
		if (type === 'email') return { smtp_host: '', smtp_port: 587, username: '', password: '', from: '', to: '', encryption: 'starttls' };
		if (type === 'telegram') return { bot_token: '', chat_id: '' };
		if (type === 'discord') return { webhook_url: '' };
		return { url: '', authorization: '' };
	}

	function typeBadgeClass(type: string): string {
		switch (type) {
			case 'email': return 'bg-blue-900/50 text-blue-400';
			case 'telegram': return 'bg-cyan-900/50 text-cyan-400';
			case 'discord': return 'bg-purple-900/50 text-purple-400';
			case 'webhook': return 'bg-yellow-900/50 text-yellow-400';
			default: return 'bg-gray-700 text-gray-300';
		}
	}

	function configPreview(config: Record<string, unknown>): string {
		const entries = Object.entries(config).filter(([key]) => !['password', 'bot_token', 'authorization'].includes(key));
		if (entries.length === 0) return '-';
		return entries.slice(0, 2).map(([k, v]) => `${k}: ${typeof v === 'string' ? v.substring(0, 20) : v}`).join(', ') + (entries.length > 2 ? '...' : '');
	}

	async function loadChannels() {
		loading = true;
		error = '';
		try {
			channels = (await api.get<NotificationChannel[]>('/api/v1/notification-channels')) || [];
		} catch (err) {
			error = err instanceof Error ? err.message : 'Failed to load channels';
		} finally {
			loading = false;
		}
	}

	async function addChannel() {
		actionMsg = '';
		actionError = '';
		creating = true;
		try {
			const config = { ...newConfig, ...(newType === 'email' ? { smtp_port: Number(newConfig.smtp_port) } : {}) };
			await api.post('/api/v1/notification-channels', {
				type: newType,
				config
			});
			actionMsg = 'Channel created.';
			newType = 'email';
			newConfig = defaultConfig('email');
			await loadChannels();
		} catch (err) {
			actionError = err instanceof Error ? err.message : 'Failed to create channel';
		} finally {
			creating = false;
		}
	}

	async function toggleChannel(ch: NotificationChannel) {
		actionMsg = '';
		actionError = '';
		try {
			await api.put(`/api/v1/notification-channels/${ch.id}`, { enabled: !ch.enabled });
			actionMsg = `Channel ${ch.enabled ? 'disabled' : 'enabled'}.`;
			await loadChannels();
		} catch (err) {
			actionError = err instanceof Error ? err.message : 'Failed to toggle channel';
		}
	}

	async function testChannel(ch: NotificationChannel) {
		actionMsg = '';
		actionError = '';
		try {
			await api.post(`/api/v1/notification-channels/${ch.id}/test`);
			actionMsg = 'Test notification sent.';
		} catch (err) {
			actionError = err instanceof Error ? err.message : 'Failed to send test notification';
		}
	}

	function startEdit(ch: NotificationChannel) {
		editingChannel = ch;
		editConfig = Object.fromEntries(Object.entries(ch.config).map(([key, value]) => [key, typeof value === 'number' || typeof value === 'string' ? value : '']));
	}

	async function saveEdit() {
		if (!editingChannel) return;
		actionMsg = '';
		actionError = '';
		try {
			await api.put(`/api/v1/notification-channels/${editingChannel.id}`, {
				config: { ...editConfig, ...(editingChannel.type === 'email' ? { smtp_port: Number(editConfig.smtp_port) } : {}) },
				enabled: editingChannel.enabled
			});
			actionMsg = 'Channel updated.';
			editingChannel = null;
			await loadChannels();
		} catch (err) {
			actionError = err instanceof Error ? err.message : 'Failed to update channel';
		}
	}

	async function deleteChannel(id: string) {
		actionMsg = '';
		actionError = '';
		try {
			await api.del(`/api/v1/notification-channels/${id}`);
			actionMsg = 'Channel deleted.';
			await loadChannels();
		} catch (err) {
			actionError = err instanceof Error ? err.message : 'Failed to delete channel';
		}
	}

	// Update placeholder when type changes
	$effect(() => {
		if (!editingChannel) {
			newConfig = defaultConfig(newType);
		}
	});

	onMount(loadChannels);
</script>

<div class="space-y-6">
	<h2 class="text-2xl font-bold text-white">Notifications</h2>

	{#if actionMsg}
		<div class="p-3 bg-green-900/50 border border-green-700 rounded-lg text-green-300 text-sm">
			{actionMsg}
			<button onclick={() => (actionMsg = '')} class="ml-2 text-green-400 hover:text-green-200 cursor-pointer">Dismiss</button>
		</div>
	{/if}

	{#if actionError}
		<div class="p-3 bg-red-900/50 border border-red-700 rounded-lg text-red-300 text-sm">
			{actionError}
			<button onclick={() => (actionError = '')} class="ml-2 text-red-400 hover:text-red-200 cursor-pointer">Dismiss</button>
		</div>
	{/if}

	{#if loading}
		<div class="text-gray-400">Loading notification channels...</div>
	{:else if error}
		<div class="p-4 bg-red-900/50 border border-red-700 rounded-lg text-red-300">{error}</div>
	{:else}
		<!-- Channels -->
		<div class="bg-gray-800 rounded-lg border border-gray-700 p-5">
			<h3 class="text-lg font-semibold text-white mb-3">Channels</h3>

			{#if channels.length === 0}
				<div class="text-gray-400 text-sm mb-4">No notification channels configured.</div>
			{:else}
				<div class="overflow-x-auto mb-4">
					<table class="w-full">
						<thead>
							<tr class="border-b border-gray-700">
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Type</th>
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Config</th>
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Enabled</th>
								<th class="text-right px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Actions</th>
							</tr>
						</thead>
						<tbody class="divide-y divide-gray-700">
							{#each channels as ch}
								<tr class="hover:bg-gray-750">
									<td class="px-4 py-3">
										<span class="inline-flex px-2 py-0.5 rounded text-xs font-medium {typeBadgeClass(ch.type)}">
											{ch.type}
										</span>
									</td>
									<td class="px-4 py-3 text-sm text-gray-300 font-mono max-w-xs truncate">{configPreview(ch.config)}</td>
									<td class="px-4 py-3">
										<button
											onclick={() => toggleChannel(ch)}
											aria-label="Toggle channel"
											class="relative inline-flex h-5 w-9 items-center rounded-full transition-colors cursor-pointer {ch.enabled ? 'bg-blue-600' : 'bg-gray-600'}"
										>
											<span class="inline-block h-3.5 w-3.5 rounded-full bg-white transition-transform {ch.enabled ? 'translate-x-4.5' : 'translate-x-0.5'}"></span>
										</button>
									</td>
									<td class="px-4 py-3 text-right">
										<div class="flex justify-end gap-2">
											<button onclick={() => testChannel(ch)} class="px-2.5 py-1 bg-cyan-600 hover:bg-cyan-700 text-white text-xs rounded transition-colors cursor-pointer">Test</button>
											<button onclick={() => startEdit(ch)} class="px-2.5 py-1 bg-gray-600 hover:bg-gray-500 text-white text-xs rounded transition-colors cursor-pointer">Edit</button>
											<button onclick={() => deleteChannel(ch.id)} class="px-2.5 py-1 bg-red-600 hover:bg-red-700 text-white text-xs rounded transition-colors cursor-pointer">Delete</button>
										</div>
									</td>
								</tr>
							{/each}
						</tbody>
					</table>
				</div>
			{/if}

			<!-- Edit Modal -->
			{#if editingChannel}
				<div class="mb-4 p-4 bg-gray-900 border border-gray-600 rounded-lg">
					<h4 class="text-sm font-medium text-white mb-2">Edit Channel: {editingChannel.type}</h4>
					<div class="grid gap-3 sm:grid-cols-2">
						{#if editingChannel.type === 'email'}
							<label class="text-sm text-gray-300">SMTP Host<input bind:value={editConfig.smtp_host} class="mt-1 w-full rounded border border-gray-600 bg-gray-800 px-3 py-2 text-white" /></label>
							<label class="text-sm text-gray-300">SMTP Port<input type="number" bind:value={editConfig.smtp_port} class="mt-1 w-full rounded border border-gray-600 bg-gray-800 px-3 py-2 text-white" /></label>
							<label class="text-sm text-gray-300">Username<input bind:value={editConfig.username} class="mt-1 w-full rounded border border-gray-600 bg-gray-800 px-3 py-2 text-white" /></label>
							<label class="text-sm text-gray-300">Password<input type="password" bind:value={editConfig.password} class="mt-1 w-full rounded border border-gray-600 bg-gray-800 px-3 py-2 text-white" /></label>
							<label class="text-sm text-gray-300">From<input type="email" bind:value={editConfig.from} class="mt-1 w-full rounded border border-gray-600 bg-gray-800 px-3 py-2 text-white" /></label>
							<label class="text-sm text-gray-300">Recipient<input type="email" bind:value={editConfig.to} class="mt-1 w-full rounded border border-gray-600 bg-gray-800 px-3 py-2 text-white" /></label>
							<label class="text-sm text-gray-300">Encryption<select bind:value={editConfig.encryption} class="mt-1 w-full rounded border border-gray-600 bg-gray-800 px-3 py-2 text-white"><option value="starttls">STARTTLS</option><option value="tls">SSL/TLS</option><option value="none">None</option></select></label>
						{:else if editingChannel.type === 'telegram'}
							<label class="text-sm text-gray-300">Bot Token<input type="password" bind:value={editConfig.bot_token} class="mt-1 w-full rounded border border-gray-600 bg-gray-800 px-3 py-2 text-white" /></label>
							<label class="text-sm text-gray-300">Chat ID<input bind:value={editConfig.chat_id} class="mt-1 w-full rounded border border-gray-600 bg-gray-800 px-3 py-2 text-white" /></label>
						{:else if editingChannel.type === 'discord'}
							<label class="text-sm text-gray-300 sm:col-span-2">Webhook URL<input type="url" bind:value={editConfig.webhook_url} class="mt-1 w-full rounded border border-gray-600 bg-gray-800 px-3 py-2 text-white" /></label>
						{:else}
							<label class="text-sm text-gray-300">Webhook URL<input type="url" bind:value={editConfig.url} class="mt-1 w-full rounded border border-gray-600 bg-gray-800 px-3 py-2 text-white" /></label>
							<label class="text-sm text-gray-300">Authorization Header<input type="password" bind:value={editConfig.authorization} class="mt-1 w-full rounded border border-gray-600 bg-gray-800 px-3 py-2 text-white" /></label>
						{/if}
					</div>
					<div class="mt-2 flex gap-2">
						<button onclick={saveEdit} class="px-3 py-1.5 bg-blue-600 hover:bg-blue-700 text-white text-sm rounded transition-colors cursor-pointer">Save</button>
						<button onclick={() => (editingChannel = null)} class="px-3 py-1.5 bg-gray-600 hover:bg-gray-500 text-white text-sm rounded transition-colors cursor-pointer">Cancel</button>
					</div>
				</div>
			{/if}
		</div>

		<!-- Add Channel -->
		<div class="bg-gray-800 rounded-lg border border-gray-700 p-5">
			<h3 class="text-lg font-semibold text-white mb-3">Add Channel</h3>

			<div class="space-y-3">
				<div>
					<label for="notif-type" class="block text-xs text-gray-400 uppercase tracking-wider mb-1">Type</label>
					<select id="notif-type" bind:value={newType} class="w-full sm:w-48 px-3 py-2 bg-gray-700 border border-gray-600 rounded text-white text-sm focus:outline-none focus:ring-2 focus:ring-blue-500">
						{#each typeOptions as t}
							<option value={t}>{t}</option>
						{/each}
					</select>
				</div>
				<div class="grid gap-3 sm:grid-cols-2">
					{#if newType === 'email'}
						<label class="text-sm text-gray-300">SMTP Host<input aria-label="smtp_host" bind:value={newConfig.smtp_host} class="mt-1 w-full rounded border border-gray-600 bg-gray-700 px-3 py-2 text-white" placeholder="smtp.example.com" /></label>
						<label class="text-sm text-gray-300">SMTP Port<input aria-label="smtp_port" type="number" min="1" max="65535" bind:value={newConfig.smtp_port} class="mt-1 w-full rounded border border-gray-600 bg-gray-700 px-3 py-2 text-white" /></label>
						<label class="text-sm text-gray-300">Username<input aria-label="username" bind:value={newConfig.username} class="mt-1 w-full rounded border border-gray-600 bg-gray-700 px-3 py-2 text-white" /></label>
						<label class="text-sm text-gray-300">Password<input aria-label="password" type="password" bind:value={newConfig.password} class="mt-1 w-full rounded border border-gray-600 bg-gray-700 px-3 py-2 text-white" /></label>
						<label class="text-sm text-gray-300">From<input aria-label="from" type="email" bind:value={newConfig.from} class="mt-1 w-full rounded border border-gray-600 bg-gray-700 px-3 py-2 text-white" placeholder="panel@example.com" /></label>
						<label class="text-sm text-gray-300">Recipient<input aria-label="to" type="email" bind:value={newConfig.to} class="mt-1 w-full rounded border border-gray-600 bg-gray-700 px-3 py-2 text-white" placeholder="admin@example.com" /></label>
						<label class="text-sm text-gray-300">Encryption<select aria-label="encryption" bind:value={newConfig.encryption} class="mt-1 w-full rounded border border-gray-600 bg-gray-700 px-3 py-2 text-white"><option value="starttls">STARTTLS</option><option value="tls">SSL/TLS</option><option value="none">None</option></select></label>
					{:else if newType === 'telegram'}
						<label class="text-sm text-gray-300">Bot Token<input aria-label="bot_token" type="password" bind:value={newConfig.bot_token} class="mt-1 w-full rounded border border-gray-600 bg-gray-700 px-3 py-2 text-white" /></label>
						<label class="text-sm text-gray-300">Chat ID<input aria-label="chat_id" bind:value={newConfig.chat_id} class="mt-1 w-full rounded border border-gray-600 bg-gray-700 px-3 py-2 text-white" /></label>
					{:else if newType === 'discord'}
						<label class="text-sm text-gray-300 sm:col-span-2">Webhook URL<input aria-label="webhook_url" type="url" bind:value={newConfig.webhook_url} class="mt-1 w-full rounded border border-gray-600 bg-gray-700 px-3 py-2 text-white" /></label>
					{:else}
						<label class="text-sm text-gray-300">Webhook URL<input aria-label="url" type="url" bind:value={newConfig.url} class="mt-1 w-full rounded border border-gray-600 bg-gray-700 px-3 py-2 text-white" /></label>
						<label class="text-sm text-gray-300">Authorization Header<input aria-label="authorization" type="password" bind:value={newConfig.authorization} class="mt-1 w-full rounded border border-gray-600 bg-gray-700 px-3 py-2 text-white" placeholder="Bearer ..." /></label>
					{/if}
				</div>
				<button
					onclick={addChannel}
					disabled={creating}
					class="px-4 py-2 bg-blue-600 hover:bg-blue-700 disabled:opacity-50 text-white text-sm font-medium rounded transition-colors cursor-pointer"
				>
					{creating ? 'Adding...' : 'Add'}
				</button>
			</div>
		</div>
	{/if}
</div>
