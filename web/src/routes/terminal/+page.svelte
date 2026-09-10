<script lang="ts">
	import { onDestroy, onMount, tick } from 'svelte';
	import { decodeTerminalEvent } from '$lib/terminal-message.js';

	let output = $state('');
	let command = $state('');
	let cwd = $state('');
	let connected = $state(false);
	let connecting = $state(true);
	let ws: WebSocket | null = null;
	let outputEl: HTMLTextAreaElement;

	// Command history navigation.
	let history: string[] = $state([]);
	let historyIndex = $state(-1);
	let draft = $state('');

	function prompt(): string {
		return `${cwd || '~'} $ `;
	}

	function shortenCwd(path: string): string {
		if (path.length <= 28) return path;
		return '…' + path.slice(-27);
	}

	function connect() {
		connecting = true;
		const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
		ws = new WebSocket(`${protocol}//${window.location.host}/ws/terminal`);

		ws.onopen = () => {
			connected = true;
			connecting = false;
			output += '--- Connected to persistent shell ---\n';
			scrollToBottom();
		};

		ws.onmessage = (event) => {
			const ev = decodeTerminalEvent(typeof event.data === 'string' ? event.data : '');
			if (!ev) {
				// Legacy plain-text server message.
				output += `${event.data}\n`;
				scrollToBottom();
				return;
			}
			if (ev.type === 'error') {
				if (ev.output) output += ev.output;
				if (!output.endsWith('\n')) output += '\n';
				scrollToBottom();
				return;
			}
			// Streaming chunks append verbatim; the final message of a command
			// reports its exit code and the shell's working directory.
			if (ev.output) output += ev.output;
			if (!ev.partial) {
				if (ev.output && !ev.output.endsWith('\n')) output += '\n';
				if (ev.exitCode !== null && ev.exitCode !== 0) output += `[exit ${ev.exitCode}]\n`;
				if (ev.cwd) cwd = ev.cwd;
				output += prompt();
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
		const cmd = command;
		if (!ws || !connected || !cmd.trim()) return;
		output += `${cmd}\n`;
		if (history.length === 0 || history[history.length - 1] !== cmd) {
			history = [...history, cmd];
		}
		historyIndex = -1;
		draft = '';
		ws.send(cmd);
		command = '';
		scrollToBottom();
	}

	function handleKeydown(e: KeyboardEvent) {
		if (e.key === 'Enter') {
			e.preventDefault();
			sendCommand();
			return;
		}
		if (e.key === 'ArrowUp') {
			e.preventDefault();
			if (history.length === 0) return;
			if (historyIndex === -1) draft = command;
			historyIndex = Math.min(historyIndex + 1, history.length - 1);
			command = history[history.length - 1 - historyIndex];
			return;
		}
		if (e.key === 'ArrowDown') {
			e.preventDefault();
			if (historyIndex === -1) return;
			historyIndex -= 1;
			command = historyIndex < 0 ? draft : history[history.length - 1 - historyIndex];
			return;
		}
		if (e.ctrlKey && (e.key === 'l' || e.key === 'L')) {
			e.preventDefault();
			output = prompt();
			scrollToBottom();
		}
	}

	function handlePaste(e: ClipboardEvent) {
		const text = e.clipboardData?.getData('text') ?? '';
		if (!text.includes('\n')) return;
		// Multi-line paste: send the whole block as one command instead of
		// fighting the single-line input.
		e.preventDefault();
		command = text.replace(/\n+$/, '');
		sendCommand();
	}

	function reconnect() {
		if (ws) {
			ws.close();
		}
		output = '';
		cwd = '';
		history = [];
		historyIndex = -1;
		draft = '';
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
			<span
				class="flex items-center px-3 text-green-400 text-sm font-mono bg-gray-900 whitespace-nowrap max-w-56 truncate"
				title={cwd || '~'}
			>
				{shortenCwd(cwd || '~')} $
			</span>
			<input
				type="text"
				bind:value={command}
				onkeydown={handleKeydown}
				onpaste={handlePaste}
				disabled={!connected}
				placeholder={connected ? 'Type a command…' : 'Not connected'}
				autocomplete="off"
				spellcheck="false"
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
		<div class="px-3 py-1.5 bg-gray-900 border-t border-gray-700/50 text-[10px] text-gray-500">
			Shell state (cd, export) persists while connected · ↑/↓ history · Ctrl+L clear · paste multi-line to run it as one command
		</div>
	</div>
</div>
