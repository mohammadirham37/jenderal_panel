<script lang="ts">
	import { onMount } from 'svelte';
	import { api } from '$lib/api';
	import { toast } from '$lib/stores/toast';
	import { language, translate } from '$lib/stores/language';

	interface RemoteConfig {
		type: string;
		endpoint: string;
		bucket: string;
		region: string;
		access_key: string;
		prefix: string;
		use_tls: boolean;
		s3_secret_set: boolean;
		gdrive_client_id: string;
		gdrive_client_secret_set: boolean;
		gdrive_connected: boolean;
		gdrive_folder_id: string;
		rclone_remote: string;
		rclone_path: string;
		delete_local_after_upload: boolean;
	}

	let cfg = $state<RemoteConfig | null>(null);
	let loadError = $state('');
	let forbidden = $state(false);
	let saving = $state(false);
	let testing = $state(false);
	let testInfo = $state('');
	let testError = $state('');

	// S3 secret + Drive client secret are write-only; blank = keep stored.
	let s3Secret = $state('');
	let gdClientSecret = $state('');

	// Google Drive wizard
	let authUrl = $state('');
	let authCode = $state('');
	let connecting = $state(false);

	async function load() {
		loadError = '';
		forbidden = false;
		try {
			cfg = await api.get<RemoteConfig>('/api/v1/backup-remote/config');
		} catch (err) {
			if (err instanceof Error && /403|forbidden/i.test(err.message)) {
				forbidden = true;
			} else {
				loadError = err instanceof Error ? err.message : translate($language, 'bk.storage.saveFailed');
			}
		}
	}

	async function save() {
		if (!cfg || saving) return;
		saving = true;
		try {
			await api.put('/api/v1/backup-remote/config', {
				type: cfg.type,
				endpoint: cfg.endpoint,
				bucket: cfg.bucket,
				region: cfg.region || 'us-east-1',
				access_key: cfg.access_key,
				s3_secret_key: s3Secret,
				prefix: cfg.prefix,
				use_tls: cfg.use_tls,
				gdrive_client_id: cfg.gdrive_client_id,
				gdrive_client_secret: gdClientSecret,
				rclone_remote: cfg.rclone_remote,
				rclone_path: cfg.rclone_path,
				delete_local_after_upload: cfg.delete_local_after_upload
			});
			s3Secret = '';
			gdClientSecret = '';
			toast.success(translate($language, 'bk.storage.saved'));
			await load();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : translate($language, 'bk.storage.saveFailed'));
		} finally {
			saving = false;
		}
	}

	async function testConnection() {
		if (testing) return;
		testing = true;
		testInfo = '';
		testError = '';
		try {
			const res = await api.post<{ info: string }>('/api/v1/backup-remote/test', {});
			testInfo = res?.info ?? '';
		} catch (err) {
			testError = err instanceof Error ? err.message : translate($language, 'bk.storage.testFailed');
		} finally {
			testing = false;
		}
	}

	async function startAuthorize() {
		try {
			const res = await api.get<{ url: string }>('/api/v1/backup-remote/gdrive/authorize');
			authUrl = res?.url ?? '';
			window.open(authUrl, '_blank', 'noopener');
		} catch (err) {
			toast.error(err instanceof Error ? err.message : translate($language, 'bk.storage.authorizeFailed'));
		}
	}

	async function connectGDrive() {
		if (!authCode.trim() || connecting) return;
		connecting = true;
		try {
			const res = await api.post<{ info: string }>('/api/v1/backup-remote/gdrive/exchange', { code: authCode.trim() });
			toast.success(translate($language, 'bk.storage.connected').replace('{account}', res?.info ?? ''));
			authUrl = '';
			authCode = '';
			await load();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : translate($language, 'bk.storage.connectFailed'));
		} finally {
			connecting = false;
		}
	}

	const inputCls =
		'w-full rounded-lg border border-gray-600 bg-gray-900 px-3 py-2 text-sm text-gray-200 focus:border-blue-500 focus:outline-none';
	const labelCls = 'mb-1 block text-[11px] font-medium uppercase tracking-wider text-gray-400';

	onMount(load);
</script>

<div class="rounded-xl border border-gray-700 bg-gray-800 p-5">
	{#if forbidden}
		<p class="text-sm text-gray-400">{translate($language, 'bk.storage.adminOnly')}</p>
	{:else if loadError}
		<div class="rounded-lg border border-red-700 bg-red-900/30 p-3.5 text-sm text-red-300">{loadError}</div>
	{:else if cfg}
		<div class="mb-4 flex flex-wrap items-center justify-between gap-2">
			<h3 class="text-lg font-semibold text-white">{translate($language, 'bk.storage.title')}</h3>
			{#if cfg.type}
				<span class="rounded-md bg-green-900/50 px-2 py-0.5 text-[10px] font-semibold text-green-300">{cfg.type}</span>
			{:else}
				<span class="rounded-md bg-gray-700 px-2 py-0.5 text-[10px] font-semibold text-gray-400">{translate($language, 'bk.storage.off')}</span>
			{/if}
		</div>

		<div class="grid gap-3 sm:grid-cols-2">
			<div>
				<label class={labelCls} for="rs-type">{translate($language, 'bk.storage.backend')}</label>
				<select id="rs-type" bind:value={cfg.type} class={inputCls}>
					<option value="">{translate($language, 'bk.storage.off')}</option>
					<option value="s3">S3</option>
					<option value="gdrive">Google Drive</option>
					<option value="rclone">rclone</option>
				</select>
			</div>
			<div class="flex items-end">
				<label class="flex cursor-pointer items-center gap-2 text-sm text-gray-300">
					<input type="checkbox" bind:checked={cfg.delete_local_after_upload} class="h-4 w-4 rounded border-gray-600 bg-gray-900" />
					{translate($language, 'bk.storage.deleteLocal')}
				</label>
			</div>
		</div>
		<p class="mt-1 text-xs text-gray-500">{translate($language, 'bk.storage.deleteLocalHint')}</p>

		{#if cfg.type === 's3'}
			<div class="mt-4 grid gap-3 sm:grid-cols-2">
				<div class="sm:col-span-2">
					<label class={labelCls} for="rs-endpoint">{translate($language, 'bk.storage.endpoint')}</label>
					<input id="rs-endpoint" bind:value={cfg.endpoint} placeholder="https://s3.wasabisys.com" class={inputCls} />
				</div>
				<div>
					<label class={labelCls} for="rs-bucket">{translate($language, 'bk.storage.bucket')}</label>
					<input id="rs-bucket" bind:value={cfg.bucket} class={inputCls} />
				</div>
				<div>
					<label class={labelCls} for="rs-region">{translate($language, 'bk.storage.region')}</label>
					<input id="rs-region" bind:value={cfg.region} placeholder="us-east-1" class={inputCls} />
				</div>
				<div>
					<label class={labelCls} for="rs-ak">{translate($language, 'bk.storage.accessKey')}</label>
					<input id="rs-ak" bind:value={cfg.access_key} class={inputCls} />
				</div>
				<div>
					<label class={labelCls} for="rs-sk">{translate($language, 'bk.storage.secretKey')}</label>
					<input id="rs-sk" type="password" bind:value={s3Secret} placeholder={cfg.s3_secret_set ? '••••••••' : ''} class={inputCls} />
					<p class="mt-1 text-[10px] text-gray-500">{translate($language, 'bk.storage.secretKeep')}</p>
				</div>
				<div>
					<label class={labelCls} for="rs-prefix">{translate($language, 'bk.storage.prefix')}</label>
					<input id="rs-prefix" bind:value={cfg.prefix} placeholder="vps-1/daily" class={inputCls} />
				</div>
				<div class="flex items-end">
					<label class="flex cursor-pointer items-center gap-2 text-sm text-gray-300">
						<input type="checkbox" bind:checked={cfg.use_tls} class="h-4 w-4 rounded border-gray-600 bg-gray-900" />
						{translate($language, 'bk.storage.useTLS')}
					</label>
				</div>
			</div>
		{:else if cfg.type === 'gdrive'}
			<div class="mt-4 space-y-3">
				<ol class="list-decimal space-y-1 pl-5 text-xs text-gray-400">
					<li>{translate($language, 'bk.storage.gdStep1')}</li>
					<li>{translate($language, 'bk.storage.gdStep2')}</li>
					<li>{translate($language, 'bk.storage.gdStep3')}</li>
					<li>{translate($language, 'bk.storage.gdStep4')}</li>
				</ol>
				<div class="grid gap-3 sm:grid-cols-2">
					<div>
						<label class={labelCls} for="rs-gd-cid">{translate($language, 'bk.storage.gdClientID')}</label>
						<input id="rs-gd-cid" bind:value={cfg.gdrive_client_id} class={inputCls} />
					</div>
					<div>
						<label class={labelCls} for="rs-gd-cs">{translate($language, 'bk.storage.gdClientSecret')}</label>
						<input id="rs-gd-cs" type="password" bind:value={gdClientSecret} placeholder={cfg.gdrive_client_secret_set ? '••••••••' : ''} class={inputCls} />
						<p class="mt-1 text-[10px] text-gray-500">{translate($language, 'bk.storage.secretKeep')}</p>
					</div>
				</div>
				<div class="flex flex-wrap items-center gap-2">
					<button type="button" onclick={startAuthorize}
						class="cursor-pointer rounded-lg bg-blue-600 px-4 py-2 text-sm font-semibold text-white transition hover:bg-blue-700">
						{translate($language, 'bk.storage.gdAuthorize')}
					</button>
					<input bind:value={authCode} placeholder={translate($language, 'bk.storage.gdCodePlaceholder')} class="w-72 rounded-lg border border-gray-600 bg-gray-900 px-3 py-2 text-sm text-gray-200" />
					<button type="button" onclick={connectGDrive} disabled={connecting || !authCode.trim()}
						class="cursor-pointer rounded-lg bg-green-600 px-4 py-2 text-sm font-semibold text-white transition hover:bg-green-700 disabled:opacity-40">
						{connecting ? translate($language, 'bk.storage.connecting') : translate($language, 'bk.storage.gdConnect')}
					</button>
					{#if cfg.gdrive_connected}
						<span class="rounded-md bg-green-900/50 px-2 py-0.5 text-[10px] font-semibold text-green-300">{translate($language, 'bk.storage.gdConnected')}</span>
					{/if}
				</div>
				{#if authUrl}
					<p class="break-all text-xs text-gray-500">
						{translate($language, 'bk.storage.gdUrlFallback')}:
						<a href={authUrl} target="_blank" rel="noopener" class="text-blue-400 underline">{authUrl}</a>
					</p>
				{/if}
			</div>
		{:else if cfg.type === 'rclone'}
			<div class="mt-4 grid gap-3 sm:grid-cols-2">
				<div>
					<label class={labelCls} for="rs-rc-remote">{translate($language, 'bk.storage.rcRemote')}</label>
					<input id="rs-rc-remote" bind:value={cfg.rclone_remote} placeholder="gdrive" class={inputCls} />
				</div>
				<div>
					<label class={labelCls} for="rs-rc-path">{translate($language, 'bk.storage.rcPath')}</label>
					<input id="rs-rc-path" bind:value={cfg.rclone_path} placeholder="backups" class={inputCls} />
				</div>
			</div>
			<p class="mt-1 text-xs text-gray-500">{translate($language, 'bk.storage.rcHint')}</p>
		{/if}

		<div class="mt-4 flex flex-wrap items-center gap-2">
			<button type="button" onclick={save} disabled={saving}
				class="cursor-pointer rounded-lg bg-blue-600 px-4 py-2 text-sm font-semibold text-white transition hover:bg-blue-700 disabled:opacity-40">
				{saving ? translate($language, 'bk.storage.saving') : translate($language, 'bk.storage.save')}
			</button>
			<button type="button" onclick={testConnection} disabled={testing || !cfg.type}
				class="cursor-pointer rounded-lg border border-gray-600 bg-gray-700 px-4 py-2 text-sm font-medium text-gray-200 transition hover:bg-gray-600 disabled:opacity-40">
				{testing ? translate($language, 'bk.storage.testing') : translate($language, 'bk.storage.test')}
			</button>
			{#if testInfo}<span class="text-xs text-green-400">{testInfo}</span>{/if}
			{#if testError}<span class="text-xs text-red-400">{testError}</span>{/if}
		</div>
	{/if}
</div>
