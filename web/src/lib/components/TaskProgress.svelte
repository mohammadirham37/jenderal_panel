<script lang="ts">
	import { onMount } from 'svelte';
	import { api } from '$lib/api';

	let { taskId = $bindable(''), storageKey = '', onComplete = () => {} }: { taskId: string; storageKey?: string; onComplete?: () => void } = $props();

	let task = $state<any>(null);
	let polling = $state(false);
	let intervalId: ReturnType<typeof setInterval> | null = null;

	// Save task ID to localStorage when set
	$effect(() => {
		if (storageKey && taskId) {
			localStorage.setItem(storageKey, taskId);
		}
	});

	// Restore task ID from localStorage on mount
	onMount(() => {
		if (storageKey && !taskId) {
			const saved = localStorage.getItem(storageKey);
			if (saved) {
				taskId = saved;
			}
		}
	});

	$effect(() => {
		if (taskId && !polling) {
			polling = true;
			pollTask();
			intervalId = setInterval(pollTask, 2000);
		}
		return () => {
			if (intervalId) clearInterval(intervalId);
		};
	});

	async function pollTask() {
		if (!taskId) return;
		try {
			task = await api.get(`/api/v1/tasks/${taskId}`);
			if (task.status === 'completed' || task.status === 'failed') {
				if (intervalId) clearInterval(intervalId);
				polling = false;
				if (storageKey) localStorage.removeItem(storageKey);
				onComplete();
			}
		} catch {
			if (intervalId) clearInterval(intervalId);
			polling = false;
		}
	}

	function dismiss() {
		task = null;
		taskId = '';
		if (storageKey) localStorage.removeItem(storageKey);
		if (intervalId) clearInterval(intervalId);
		polling = false;
	}
</script>

{#if task}
	<div class="mt-4 rounded-lg border border-gray-700 bg-gray-950 p-4">
		<div class="flex items-center justify-between mb-2">
			<span class="text-sm font-medium text-gray-300">{task.name}</span>
			<div class="flex items-center gap-2">
				{#if task.status === 'running'}
					<span class="inline-flex items-center gap-1 text-xs bg-yellow-900 text-yellow-300 px-2 py-0.5 rounded">
						<span class="w-2 h-2 bg-yellow-400 rounded-full animate-pulse"></span>
						Running
					</span>
				{:else if task.status === 'completed'}
					<span class="text-xs bg-green-900 text-green-300 px-2 py-0.5 rounded">Completed</span>
					<button onclick={dismiss} class="text-xs text-gray-500 hover:text-gray-300 cursor-pointer">Dismiss</button>
				{:else if task.status === 'failed'}
					<span class="text-xs bg-red-900 text-red-300 px-2 py-0.5 rounded">Failed</span>
					<button onclick={dismiss} class="text-xs text-gray-500 hover:text-gray-300 cursor-pointer">Dismiss</button>
				{/if}
			</div>
		</div>
		<pre class="text-xs text-gray-400 bg-gray-900 rounded p-3 max-h-96 overflow-y-auto whitespace-pre-wrap font-mono">{task.output || 'Waiting for output...'}</pre>
		{#if task.error}
			<p class="mt-2 text-sm text-red-400">{task.error}</p>
		{/if}
	</div>
{/if}
