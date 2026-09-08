<script lang="ts">
	import { onMount, onDestroy, tick } from 'svelte';
	import { decodeTerminalMessage } from '$lib/terminal-message.js';

	let output = $state('');
	let command = $state('');
	let connected = $state(false);
	let connecting = $state(true);
	let ws: WebSocket | null = null;
	let outputEl: HTMLTextAreaElement;

	function connect() {
		connecting = true;
		const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
		ws = new WebSocket(`${protocol}//${window.location.host}/ws/terminal`);

		ws.onopen = () => {
			connected = true;
			connecting = false;
			output += '--- Connected to terminal ---\n';
			scrollToBottom();
		};

		ws.onmessage = (event) => {
			const message = decodeTerminalMessage(event.data);
			output += message;
			if (!message.endsWith('\n')) {
				output += '\n';
			}
			scrollToBottom();
		};

		ws.onclose = () => {
			connected = false;
			connecting = false;
			output += '--- Disconnected ---\n';
			scrollToBottom();
		};

		ws.onerror = () => {
			connected = false;
			connecting = false;
			output += '--- Connection error ---\n';
			scrollToBottom();
		};
	}

	async function scrollToBottom() {
		await tick();
		if (outputEl) {
			outputEl.scrollTop = outputEl.scrollHeight;
		}
	}

	function sendCommand() {
		if (!ws || !connected || !command.trim()) return;
		output += `$ ${command}\n`;
		ws.send(command);
		command = '';
		scrollToBottom();
	}

	function handleKeydown(e: KeyboardEvent) {
		if (e.key === 'Enter') {
			e.preventDefault();
			sendCommand();
		}
	}

	function reconnect() {
		if (ws) {
			ws.close();
		}
		output = '';
		connect();
	}

	onMount(connect);

	onDestroy(() => {
		if (ws) {
			ws.close();
		}
	});
</script>

<div class="space-y-6">
	<div class="flex items-center justify-between">
		<h2 class="text-2xl font-bold text-white">Terminal</h2>
		<div class="flex items-center gap-3">
			<span class="inline-flex items-center gap-1.5 px-2.5 py-0.5 rounded-full text-xs font-medium {connected ? 'bg-green-900/50 text-green-400' : 'bg-red-900/50 text-red-400'}">
				<span class="w-1.5 h-1.5 rounded-full {connected ? 'bg-green-400' : 'bg-red-400'}"></span>
				{connecting ? 'Connecting...' : connected ? 'Connected' : 'Disconnected'}
			</span>
			{#if !connected && !connecting}
				<button
					onclick={reconnect}
					class="px-3 py-1.5 bg-blue-600 hover:bg-blue-700 text-white text-sm rounded transition-colors cursor-pointer"
				>
					Reconnect
				</button>
			{/if}
		</div>
	</div>

	<div class="bg-gray-800 rounded-lg border border-gray-700 overflow-hidden">
		<!-- Terminal Output -->
		<textarea
			bind:this={outputEl}
			readonly
			value={output}
			class="w-full h-96 bg-gray-950 p-4 text-green-400 text-sm font-mono resize-y focus:outline-none border-none"
		></textarea>

		<!-- Command Input -->
		<div class="flex border-t border-gray-700">
			<span class="flex items-center px-3 text-green-400 text-sm font-mono bg-gray-900">$</span>
			<input
				type="text"
				bind:value={command}
				onkeydown={handleKeydown}
				disabled={!connected}
				placeholder={connected ? 'Type a command...' : 'Not connected'}
				class="flex-1 px-3 py-3 bg-gray-900 text-white text-sm font-mono focus:outline-none disabled:opacity-50 disabled:cursor-not-allowed"
			/>
			<button
				onclick={sendCommand}
				disabled={!connected || !command.trim()}
				class="px-4 py-3 bg-blue-600 hover:bg-blue-700 disabled:opacity-50 text-white text-sm font-medium transition-colors cursor-pointer"
			>
				Send
			</button>
		</div>
	</div>
</div>
