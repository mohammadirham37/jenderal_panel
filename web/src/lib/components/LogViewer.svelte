<script lang="ts">
	import { onMount, onDestroy } from 'svelte';
	import { api } from '$lib/api';
	import { language, translate } from '$lib/stores/language';

	let { path, title }: { path: string; title: string } = $props();

	let content = $state('');
	let lines = $state(50);
	let loading = $state(false);
	let error = $state('');
	let streaming = $state(false);
	let ws: WebSocket | null = null;
	let textareaEl: HTMLTextAreaElement | undefined = $state();

	async function fetchLogs() {
		loading = true;
		error = '';
		try {
			const data = await api.get<{ content: string }>(`/api/v1/logs?path=${encodeURIComponent(path)}&lines=${lines}`);
			content = data.content || '';
			scrollToBottom();
		} catch (err) {
			error = err instanceof Error ? err.message : translate($language, 'common.failedToLoadLogs');
		} finally {
			loading = false;
		}
	}

	function scrollToBottom() {
		requestAnimationFrame(() => {
			if (textareaEl) {
				textareaEl.scrollTop = textareaEl.scrollHeight;
			}
		});
	}

	function toggleStream() {
		if (streaming) {
			stopStream();
		} else {
			startStream();
		}
	}

	function startStream() {
		if (ws) return;
		const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
		const url = `${proto}//${location.host}/ws/logs?path=${encodeURIComponent(path)}`;
		ws = new WebSocket(url);
		ws.onopen = () => {
			streaming = true;
		};
		ws.onmessage = (event) => {
			content += event.data + '\n';
			scrollToBottom();
		};
		ws.onerror = () => {
			error = translate($language, 'common.wsError');
			stopStream();
		};
		ws.onclose = () => {
			streaming = false;
			ws = null;
		};
	}

	function stopStream() {
		if (ws) {
			ws.close();
			ws = null;
		}
		streaming = false;
	}

	onMount(fetchLogs);

	onDestroy(() => {
		stopStream();
	});
</script>

<div class="bg-gray-800 rounded-lg border border-gray-700 p-4">
	<div class="flex items-center justify-between mb-3">
		<h4 class="text-sm font-semibold text-white">{title}</h4>
		<div class="flex items-center gap-2">
			<select
				bind:value={lines}
				onchange={fetchLogs}
				class="px-2 py-1 bg-gray-700 border border-gray-600 rounded text-gray-300 text-xs focus:outline-none focus:ring-2 focus:ring-blue-500"
			>
				<option value={50}>{translate($language, 'common.nLines').replace('{n}', '50')}</option>
				<option value={100}>{translate($language, 'common.nLines').replace('{n}', '100')}</option>
				<option value={500}>{translate($language, 'common.nLines').replace('{n}', '500')}</option>
			</select>
			<button
				onclick={fetchLogs}
				disabled={loading}
				class="px-2.5 py-1 bg-gray-700 hover:bg-gray-600 disabled:opacity-50 text-gray-300 text-xs rounded transition-colors cursor-pointer"
			>
				{loading ? translate($language, 'common.loading') : translate($language, 'common.refresh')}
			</button>
			<button
				onclick={toggleStream}
				class="px-2.5 py-1 text-xs rounded transition-colors cursor-pointer {streaming
					? 'bg-red-600 hover:bg-red-700 text-white'
					: 'bg-green-600 hover:bg-green-700 text-white'}"
			>
				{streaming ? translate($language, 'common.stopStream') : translate($language, 'common.stream')}
			</button>
		</div>
	</div>

	{#if error}
		<div class="mb-2 text-red-400 text-xs">{error}</div>
	{/if}

	<textarea
		bind:this={textareaEl}
		readonly
		value={content}
		class="w-full h-64 bg-gray-950 border border-gray-700 rounded p-3 text-gray-300 text-xs font-mono resize-y focus:outline-none"
	></textarea>
</div>
