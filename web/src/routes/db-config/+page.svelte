<script lang="ts">
	import { api } from '$lib/api';
	import { dbConfigFields, dbConfigOptions, parseDbConfig, buildConfigFromForm } from '$lib/db-config.js';
	import { toast } from '$lib/stores/toast';
	import { language, translate } from '$lib/stores/language';

	interface EngineConfig {
		engine: string;
		path: string;
		content: string;
		available: boolean;
	}

	const engines: Array<'mysql' | 'postgresql'> = ['mysql', 'postgresql'];

	let engine = $state<'mysql' | 'postgresql'>('mysql');
	let mode = $state<'form' | 'manual'>('form');
	let configs = $state<Record<string, EngineConfig | undefined>>({});
	let formValues = $state<Record<string, Record<string, string>>>({});
	let baselines = $state<Record<string, Record<string, string>>>({});
	let parseWarnings = $state<Record<string, string[]>>({});
	let manualDraft = $state('');
	let loading = $state(false);
	let saving = $state(false);
	let error = $state('');
	let loadedEngine = $state('');

	let current = $derived(loadedEngine === engine ? configs[engine] : undefined);
	let values = $derived(loadedEngine === engine ? (formValues[engine] ?? {}) : {});
	let baseline = $derived(loadedEngine === engine ? (baselines[engine] ?? {}) : {});
	let currentWarnings = $derived(loadedEngine === engine ? (parseWarnings[engine] ?? []) : []);

	async function loadEngine(target: 'mysql' | 'postgresql') {
		loading = true;
		error = '';
		try {
			const cfg = await api.get<EngineConfig>(`/api/v1/db-config/${target}`);
			const parsed = parseDbConfig(cfg.content ?? '', target);
			configs = { ...configs, [target]: cfg };
			formValues = { ...formValues, [target]: parsed.values };
			baselines = { ...baselines, [target]: parsed.values };
			parseWarnings = { ...parseWarnings, [target]: parsed.errors };
			if (target === engine) manualDraft = cfg.content ?? '';
			loadedEngine = target;
		} catch (err) {
			error = err instanceof Error ? err.message : translate($language, 'dbc.error.load');
		} finally {
			loading = false;
		}
	}

	function switchEngine(target: 'mysql' | 'postgresql') {
		if (target === engine) return;
		engine = target;
		mode = 'form';
		if (loadedEngine !== target) loadEngine(target);
		else manualDraft = configs[target]?.content ?? '';
	}

	function switchMode(next: 'form' | 'manual') {
		if (next === mode) return;
		if (next === 'manual') {
			// Carry current form values into the raw editor.
			manualDraft = buildConfigFromForm(current?.content ?? '', engine, baseline, values);
		} else {
			// Read manual edits back into the form.
			const parsed = parseDbConfig(manualDraft, engine);
			formValues = { ...formValues, [engine]: parsed.values };
			parseWarnings = { ...parseWarnings, [engine]: parsed.errors };
		}
		mode = next;
	}

	async function applyConfig() {
		if (saving) return;
		saving = true;
		try {
			const content =
				mode === 'manual'
					? manualDraft
					: buildConfigFromForm(current?.content ?? '', engine, baseline, values);
			await api.put(`/api/v1/db-config/${engine}`, { content });
			toast.success(translate($language, 'dbc.toast.applied'));
			await loadEngine(engine);
		} catch (err) {
			toast.error(err instanceof Error ? err.message : translate($language, 'dbc.error.apply'));
		} finally {
			saving = false;
		}
	}

	$effect(() => {
		void loadEngine(engine);
	});
</script>

<div class="space-y-6">
	<div class="flex flex-wrap items-center justify-between gap-3">
		<div>
			<h2 class="text-2xl font-bold text-white">{translate($language, 'dbc.title')}</h2>
			<p class="mt-1 text-sm text-gray-400">{translate($language, 'dbc.subtitle')}</p>
		</div>
		<div class="flex gap-1 rounded-lg border border-gray-700 bg-gray-800 p-1">
			{#each engines as eng (eng)}
				<button
					type="button"
					onclick={() => switchEngine(eng)}
					class="cursor-pointer rounded-md px-3 py-1.5 text-sm font-medium transition {engine === eng
						? 'bg-blue-600 text-white'
						: 'text-gray-300 hover:bg-gray-700'}"
				>
					{translate($language, eng === 'mysql' ? 'dbc.engine.mysql' : 'dbc.engine.postgresql')}
				</button>
			{/each}
		</div>
	</div>

	{#if loading}
		<div class="space-y-2 rounded-xl border border-gray-700 bg-gray-800 p-5">
			{#each Array(4) as _}
				<div class="h-8 animate-pulse rounded bg-gray-700/50"></div>
			{/each}
		</div>
	{:else if error}
		<div class="rounded-lg border border-red-700 bg-red-900/30 p-3.5 text-sm text-red-300">{error}</div>
	{:else if !current?.available}
		<div class="rounded-xl border border-gray-700 bg-gray-800 p-8 text-center text-gray-400">
			{translate($language, 'dbc.not_available')}
		</div>
	{:else}
		<div class="flex flex-wrap items-center justify-between gap-2">
			<div class="flex gap-1 rounded-lg border border-gray-700 bg-gray-800 p-1">
				<button
					type="button"
					onclick={() => switchMode('form')}
					class="cursor-pointer rounded-md px-3 py-1.5 text-xs font-medium transition {mode === 'form'
						? 'bg-gray-600 text-white'
						: 'text-gray-300 hover:bg-gray-700'}"
				>
					{translate($language, 'dbc.mode.form')}
				</button>
				<button
					type="button"
					onclick={() => switchMode('manual')}
					class="cursor-pointer rounded-md px-3 py-1.5 text-xs font-medium transition {mode === 'manual'
						? 'bg-gray-600 text-white'
						: 'text-gray-300 hover:bg-gray-700'}"
				>
					{translate($language, 'dbc.mode.manual')}
				</button>
			</div>
			<span class="font-mono text-[11px] text-gray-500">{current.path}</span>
		</div>

		{#if currentWarnings.length > 0}
			<div class="rounded-lg border border-yellow-700/60 bg-yellow-900/20 p-3 text-xs text-yellow-300">
				{translate($language, 'dbc.invalid_values')}
				{currentWarnings.join(', ')}
			</div>
		{/if}

		<div class="rounded-xl border border-gray-700 bg-gray-800 p-5">
			{#if mode === 'form'}
				<div class="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
					{#each Object.entries(dbConfigFields[engine]) as [key, def] (key)}
						<div>
							<label class="mb-1 block font-mono text-[11px] font-medium text-gray-300" for={key}>
								{key}
							</label>
							{#if dbConfigOptions(def, formValues[engine][key] ?? '') !== null}
								<select
									id={key}
									bind:value={formValues[engine][key]}
									class="w-full cursor-pointer rounded-lg border border-gray-600 bg-gray-900 px-2.5 py-2 font-mono text-xs text-gray-200 focus:border-blue-500 focus:outline-none"
								>
									<option value="">{translate($language, 'dbc.select.default')}</option>
									{#each dbConfigOptions(def, formValues[engine][key] ?? '') as opt (opt)}
										<option value={opt}>{opt}</option>
									{/each}
								</select>
							{:else}
								<input
									id={key}
									type="text"
									bind:value={formValues[engine][key]}
									placeholder={def.placeholder}
									class="w-full rounded-lg border border-gray-600 bg-gray-900 px-2.5 py-2 font-mono text-xs text-gray-200 focus:border-blue-500 focus:outline-none"
								/>
							{/if}
						</div>
					{/each}
				</div>
			{:else}
				<textarea
					bind:value={manualDraft}
					rows="18"
					spellcheck="false"
					class="w-full rounded-lg border border-gray-600 bg-gray-950 p-4 font-mono text-[12px] leading-5 text-gray-200 focus:border-blue-500 focus:outline-none"
				></textarea>
			{/if}

			<div class="mt-4 flex flex-wrap items-center gap-3">
				<button
					type="button"
					onclick={applyConfig}
					disabled={saving}
					class="cursor-pointer rounded-lg bg-blue-600 px-4 py-2 text-sm font-semibold text-white transition hover:bg-blue-700 disabled:opacity-50"
				>
					{saving ? translate($language, 'dbc.applying') : translate($language, 'dbc.apply')}
				</button>
				<p class="text-[11px] text-gray-500">{translate($language, 'dbc.restart_note')}</p>
			</div>
		</div>
	{/if}
</div>
