<script lang="ts">
	import { onMount } from 'svelte';
	import { api } from '$lib/api';
	import { language, translate } from '$lib/stores/language';
	import { permissions, hasPermission } from '$lib/stores/auth';

	// SSH port management card (admin, two-phase): phase one opens the new
	// port while keeping the old one; after the operator verifies the new
	// port works, finalize closes the old one. Self-contained — drop it on
	// any admin page.
	interface SSHPortStatus {
		current_port: number;
		panel_port: number;
		pending_change: { old_port: number; new_port: number } | null;
	}

	let canManageSSH = $derived(hasPermission($permissions, 'ssh.view'));
	let canChangeSSH = $derived(hasPermission($permissions, 'ssh.manage'));
	let sshPort = $state<SSHPortStatus | null>(null);
	let sshPortLoading = $state(false);
	let newSSHPort = $state('');
	let sshPortBusy = $state(false);
	let sshPortError = $state('');
	let sshPortMsg = $state('');

	async function loadSSHPort() {
		if (!canManageSSH) return;
		sshPortLoading = true;
		try {
			sshPort = await api.get<SSHPortStatus>('/api/v1/ssh/port');
			sshPortError = '';
		} catch (err) {
			sshPort = null;
			sshPortError = err instanceof Error ? err.message : translate($language, 'srv.errorLoadSSHPort');
		} finally {
			sshPortLoading = false;
		}
	}

	async function beginSSHPortChange() {
		const port = Number(newSSHPort);
		if (!port || sshPortBusy) return;
		if (!confirm(
			translate($language, 'srv.confirmChangePort')
				.replace('{old}', String(sshPort?.current_port))
				.replace('{new}', String(port))
		)) return;
		sshPortBusy = true;
		sshPortMsg = '';
		sshPortError = '';
		try {
			const result = await api.post<{ message: string }>('/api/v1/ssh/port/change', { port });
			sshPortMsg = result.message || translate($language, 'srv.msgPortChangeStarted');
			newSSHPort = '';
			await loadSSHPort();
		} catch (err) {
			sshPortError = err instanceof Error ? err.message : translate($language, 'srv.errorChangePort');
		} finally {
			sshPortBusy = false;
		}
	}

	async function finalizeSSHPortChange() {
		if (!confirm(translate($language, 'srv.confirmFinalize'))) return;
		sshPortBusy = true;
		sshPortMsg = '';
		sshPortError = '';
		try {
			await api.post('/api/v1/ssh/port/change/finalize');
			sshPortMsg = translate($language, 'srv.msgFinalized');
			await loadSSHPort();
		} catch (err) {
			sshPortError = err instanceof Error ? err.message : translate($language, 'srv.errorFinalize');
		} finally {
			sshPortBusy = false;
		}
	}

	async function cancelSSHPortChange() {
		if (!confirm(translate($language, 'srv.confirmCancelChange'))) return;
		sshPortBusy = true;
		sshPortMsg = '';
		sshPortError = '';
		try {
			await api.del('/api/v1/ssh/port/change');
			sshPortMsg = translate($language, 'srv.msgCancelled');
			await loadSSHPort();
		} catch (err) {
			sshPortError = err instanceof Error ? err.message : translate($language, 'srv.errorCancel');
		} finally {
			sshPortBusy = false;
		}
	}

	onMount(() => {
		loadSSHPort();
	});
</script>

{#if canManageSSH}
	<div class="bg-gray-800 rounded-lg border border-gray-700 p-5">
		<h3 class="text-lg font-semibold text-white mb-3">{translate($language, 'srv.sshPort')}</h3>

		{#if sshPortMsg}
			<div class="mb-3 p-3 bg-green-900/50 border border-green-700 rounded-lg text-green-300 text-sm">
				{sshPortMsg}
				<button onclick={() => (sshPortMsg = '')} class="ml-2 text-green-400 hover:text-green-200 cursor-pointer">{translate($language, 'srv.dismiss')}</button>
			</div>
		{/if}

		{#if sshPortError}
			<div class="mb-3 p-3 bg-red-900/50 border border-red-700 rounded-lg text-red-300 text-sm">
				{sshPortError}
				<button onclick={() => (sshPortError = '')} class="ml-2 text-red-400 hover:text-red-200 cursor-pointer">{translate($language, 'srv.dismiss')}</button>
			</div>
		{/if}

		{#if sshPortLoading}
			<div class="text-gray-400 text-sm">{translate($language, 'srv.loadingSSHPort')}</div>
		{:else if sshPort}
			<div class="flex flex-wrap gap-4 text-sm text-gray-400 mb-4">
				<span>{translate($language, 'srv.effectivePort')} <span class="text-white font-mono">{sshPort.current_port}</span></span>
				<span>{translate($language, 'srv.panelPort')} <span class="text-white font-mono">{sshPort.panel_port}</span></span>
			</div>

			{#if sshPort.pending_change}
				<div class="p-3 bg-yellow-900/30 border border-yellow-700 rounded-lg text-sm text-yellow-300">
					<p class="font-medium mb-2">
						{translate($language, 'srv.pendingTitle').replace('{old}', String(sshPort.pending_change.old_port)).replace('{new}', String(sshPort.pending_change.new_port))}
					</p>
					<p class="mb-3 text-yellow-200/80">
						{translate($language, 'srv.pendingVerify').replace('{port}', String(sshPort.pending_change.new_port))}
					</p>
					{#if canChangeSSH}
						<div class="flex gap-2">
							<button
								onclick={finalizeSSHPortChange}
								disabled={sshPortBusy}
								class="px-3 py-1.5 bg-green-600 hover:bg-green-700 disabled:opacity-50 text-white text-xs rounded transition-colors cursor-pointer"
							>
								{translate($language, 'srv.finalize')}
							</button>
							<button
								onclick={cancelSSHPortChange}
								disabled={sshPortBusy}
								class="px-3 py-1.5 bg-gray-700 hover:bg-gray-600 disabled:opacity-50 text-gray-200 text-xs rounded transition-colors cursor-pointer"
							>
								{translate($language, 'srv.cancelChange')}
							</button>
						</div>
					{/if}
				</div>
			{:else if canChangeSSH}
				<div class="flex flex-wrap items-end gap-3">
					<div>
						<label for="new-ssh-port" class="block text-xs text-gray-400 uppercase tracking-wider mb-1">{translate($language, 'srv.newPort')}</label>
						<input
							id="new-ssh-port"
							type="number"
							min="1"
							max="65535"
							bind:value={newSSHPort}
							placeholder="2222"
							class="px-3 py-2 bg-gray-900 border border-gray-600 rounded text-white text-sm font-mono focus:outline-none focus:ring-2 focus:ring-blue-500"
						/>
					</div>
					<button
						onclick={beginSSHPortChange}
						disabled={sshPortBusy || !newSSHPort}
						class="px-4 py-2 bg-blue-600 hover:bg-blue-700 disabled:opacity-50 text-white text-sm font-medium rounded transition-colors cursor-pointer"
					>
						{sshPortBusy ? translate($language, 'srv.working') : translate($language, 'srv.changePort')}
					</button>
				</div>
				<p class="mt-2 text-xs text-gray-500">
					{translate($language, 'srv.twoPhaseHint')}
				</p>
			{/if}
		{/if}
	</div>
{/if}
