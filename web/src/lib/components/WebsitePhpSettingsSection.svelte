<script lang="ts">
	import { onDestroy } from 'svelte';
	import { api } from '$lib/api';
import { toast } from '$lib/stores/toast';
import { language, translate } from '$lib/stores/language';

	interface Props {
		websiteID: string;
		appType?: string;
	}

	let { websiteID, appType = 'php' }: Props = $props();

	interface Field {
		key: string;
		labelKey: string;
		phpName: string;
		placeholder: string;
	}

	// Managed settings and the php.ini names they map to.
	const fields: Field[] = [
		{ key: 'memory_limit', labelKey: 'wsphp.field.memory_limit', phpName: 'memory_limit', placeholder: '256M' },
		{ key: 'upload_max_filesize', labelKey: 'wsphp.field.upload_max_filesize', phpName: 'upload_max_filesize', placeholder: '64M' },
		{ key: 'post_max_size', labelKey: 'wsphp.field.post_max_size', phpName: 'post_max_size', placeholder: '64M' },
		{ key: 'max_execution_time', labelKey: 'wsphp.field.max_execution_time', phpName: 'max_execution_time', placeholder: '60' },
		{ key: 'max_input_time', labelKey: 'wsphp.field.max_input_time', phpName: 'max_input_time', placeholder: '60' },
		{ key: 'max_input_vars', labelKey: 'wsphp.field.max_input_vars', phpName: 'max_input_vars', placeholder: '3000' }
	];

	let values = $state<Record<string, string>>({});
	let loading = $state(false);
	let saving = $state(false);
	let error = $state('');
	let refreshTimer: ReturnType<typeof setTimeout> | null = null;

	async function loadSettings() {
		if (!websiteID) return;
		loading = true;
		error = '';
		try {
			const data = await api.get<Record<string, string>>(
				`/api/v1/websites/${encodeURIComponent(websiteID)}/php-settings`
			);
			const next: Record<string, string> = {};
			for (const f of fields) next[f.key] = data[f.key] ?? '';
			values = next;
		} catch (err) {
			error = err instanceof Error ? err.message : translate($language, 'wsphp.error.load');
		} finally {
			loading = false;
		}
	}

	async function saveSettings() {
		if (saving) return;
		saving = true;
		try {
			await api.put(`/api/v1/websites/${encodeURIComponent(websiteID)}/php-settings`, { ...values });
			toast.success(translate($language, 'wsphp.toast.saved'));
		} catch (err) {
			toast.error(err instanceof Error ? err.message : translate($language, 'wsphp.error.save'));
		} finally {
			saving = false;
		}
	}

	$effect(() => {
		if (websiteID && appType !== 'static') loadSettings();
	});

	onDestroy(() => {
		if (refreshTimer) clearTimeout(refreshTimer);
	});
</script>

<div class="space-y-4">
	<div class="flex flex-wrap items-center justify-between gap-2">
		<p class="text-xs text-gray-500">
			{translate($language, 'wsphp.subtitle.a')} <code class="text-gray-400">.user.ini</code>
			{translate($language, 'wsphp.subtitle.b')}
		</p>
		<button
			type="button"
			onclick={loadSettings}
			class="cursor-pointer rounded-lg border border-gray-600 bg-gray-700 px-3 py-1.5 text-xs text-gray-200 transition hover:bg-gray-600"
		>
			{translate($language, 'wsphp.refresh')}
		</button>
	</div>


	{#if loading}
		<div class="space-y-2 rounded-xl border border-gray-700 bg-gray-800 p-5">
			{#each Array(4) as _}
				<div class="h-8 animate-pulse rounded bg-gray-700/50"></div>
			{/each}
		</div>
	{:else if error}
		<div class="rounded-lg border border-red-700 bg-red-900/30 p-3.5 text-sm text-red-300">{error}</div>
	{:else}
		<div class="rounded-xl border border-gray-700 bg-gray-800 p-5">
			<div class="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
				{#each fields as f (f.key)}
					<div>
						<label class="mb-1 block text-[11px] font-medium uppercase tracking-wider text-gray-400" for={f.key}>
							{translate($language, f.labelKey)}
						</label>
						<input
							id={f.key}
							type="text"
							bind:value={values[f.key]}
							placeholder={f.placeholder}
							class="w-full rounded-lg border border-gray-600 bg-gray-900 px-2.5 py-2 font-mono text-xs text-gray-200 focus:border-blue-500 focus:outline-none"
						/>
					</div>
				{/each}
			</div>
			<div class="mt-4 flex items-center gap-2">
				<button
					type="button"
					onclick={saveSettings}
					disabled={saving}
					class="cursor-pointer rounded-lg bg-blue-600 px-4 py-2 text-sm font-semibold text-white transition hover:bg-blue-700 disabled:opacity-50"
				>
					{saving ? translate($language, 'wsphp.saving') : translate($language, 'wsphp.saveApply')}
				</button>
				{#each fields as f (f.key)}
					{#if (values[f.key] ?? '') !== ''}
						<button
							type="button"
							onclick={() => { values = { ...values, [f.key]: '' }; }}
							class="cursor-pointer rounded-md border border-gray-600 px-2 py-1 text-[10px] text-gray-400 transition hover:bg-gray-700 hover:text-gray-200"
							title={translate($language, 'wsphp.tooltip.clear').replace('{name}', f.phpName)}
						>
							{translate($language, 'wsphp.clear')} {f.phpName}
						</button>
					{/if}
				{/each}
			</div>
			<p class="mt-3 text-[11px] text-gray-500">
				{translate($language, 'wsphp.hint')}
			</p>
		</div>
	{/if}
</div>
