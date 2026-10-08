<script lang="ts">
	import { onDestroy, tick } from 'svelte';
	import { decodeTerminalEvent } from '$lib/terminal-message.js';
	import { language, translate } from '$lib/stores/language';

	interface Props {
		/** Full WebSocket URL of the /ws/terminal endpoint (with query params). */
		endpoint: string;
		/** While true the shell session is kept alive; false disconnects. */
		active?: boolean;
		title?: string;
		heightClass?: string;
	}

	let { endpoint, active = true, title = 'Terminal', heightClass = 'h-96' }: Props = $props();

	let output = $state('');
	let command = $state('');
	let cwd = $state('');
	let connected = $state(false);
	let connecting = $state(false);
	// True while a command is executing: the input box then feeds the
	// command's prompts (confirmations, passwords) instead of starting a
	// new command, and Ctrl+C interrupts it.
	let running = $state(false);
	let ws: WebSocket | null = null;
	let outputEl: HTMLTextAreaElement;

	// Command history navigation.
	let history: string[] = $state([]);
	let historyIndex = $state(-1);
	let draft = $state('');

	function prompt(): string {
		return `${cwd || '~'} $ `;
	}

	function shortenPath(path: string): string {
		if (path.length <= 32) return path;
		return '…' + path.slice(-31);
	}

	function connect() {
		if (!endpoint) return;
		connecting = true;
		output = '';
		cwd = '';
		history = [];
		historyIndex = -1;
		draft = '';
		ws = new WebSocket(endpoint);

		ws.onopen = () => {
			connected = true;
			connecting = false;
			output += translate($language, 'common.terminal.connectedBanner') + '\n';
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
				running = false;
				if (ev.output) output += ev.output;
				if (!output.endsWith('\n')) output += '\n';
				scrollToBottom();
				return;
			}
			// Streaming chunks append verbatim; the final message of a command
			// reports its exit code and the shell's working directory.
			if (ev.output) output += ev.output;
			if (!ev.partial) {
				running = false;
				if (ev.output && !ev.output.endsWith('\n')) output += '\n';
				if (ev.exitCode !== null && ev.exitCode !== 0) output += translate($language, 'common.terminal.exitCode').replace('{code}', String(ev.exitCode)) + '\n';
				if (ev.cwd) cwd = ev.cwd;
				output += prompt();
			}
			scrollToBottom();
		};

		ws.onclose = () => {
			connected = false;
			connecting = false;
			running = false;
			output += translate($language, 'common.terminal.disconnectedBanner') + '\n';
			scrollToBottom();
		};

		ws.onerror = () => {
			connected = false;
			connecting = false;
			running = false;
			output += translate($language, 'common.terminal.errorBanner') + '\n';
			scrollToBottom();
		};
	}

	function disconnect() {
		if (ws) {
			ws.close();
			ws = null;
		}
		connected = false;
		connecting = false;
	}

	// The session lives exactly as long as the console is active.
	$effect(() => {
		if (active) connect();
		else disconnect();
	});

	onDestroy(disconnect);

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
		ws.send(JSON.stringify({ command: cmd }));
		command = '';
		running = true;
		scrollToBottom();
	}

	// Feeds the running command's prompt (e.g. answering a production
	// migrate confirmation with "yes").
	function sendInput() {
		const text = command;
		if (!ws || !connected || !running || !text.trim()) return;
		output += `${text}\n`;
		ws.send(JSON.stringify({ input: text }));
		command = '';
		scrollToBottom();
	}

	// Interrupts the running command the way Ctrl+C would; the shell itself
	// survives and reports the command's exit code.
	function sendInterrupt() {
		if (!ws || !connected || !running) return;
		output += '^C\n';
		ws.send(JSON.stringify({ signal: 'SIGINT' }));
		scrollToBottom();
	}

	function handleKeydown(e: KeyboardEvent) {
		if (e.key === 'Enter') {
			e.preventDefault();
			if (running) sendInput();
			else sendCommand();
			return;
		}
		if (e.ctrlKey && (e.key === 'c' || e.key === 'C') && running) {
			e.preventDefault();
			sendInterrupt();
			return;
		}
		if (running) return;
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

	function clearOutput() {
		output = connected ? prompt() : '';
	}
</script>

<div class="overflow-hidden rounded-lg border border-gray-700 bg-gray-800">
	<!-- Header -->
	<div class="flex flex-wrap items-center justify-between gap-2 border-b border-gray-700 bg-gray-900/70 px-4 py-2.5">
		<div class="flex min-w-0 items-center gap-3">
			<span class="flex shrink-0 items-center gap-1.5" aria-hidden="true">
				<span class="h-2.5 w-2.5 rounded-full bg-red-500/70"></span>
				<span class="h-2.5 w-2.5 rounded-full bg-yellow-500/70"></span>
				<span class="h-2.5 w-2.5 rounded-full bg-green-500/70"></span>
			</span>
			<h3 class="shrink-0 text-sm font-semibold text-white">{title}</h3>
			{#if cwd}
				<span class="max-w-64 truncate rounded-md bg-blue-500/10 px-2 py-0.5 font-mono text-[11px] text-blue-300" title={cwd}>
					{shortenPath(cwd)}
				</span>
			{/if}
		</div>
		<div class="flex shrink-0 items-center gap-2">
			<span class="inline-flex items-center gap-1.5 rounded-full px-2.5 py-0.5 text-xs font-medium {connected ? 'bg-green-900/50 text-green-400' : 'bg-red-900/50 text-red-400'}">
				<span class="h-1.5 w-1.5 rounded-full {connected ? 'bg-green-400' : 'bg-red-400'}"></span>
				{connecting ? translate($language, 'common.connecting') : connected ? translate($language, 'common.connected') : translate($language, 'common.disconnected')}
			</span>
			{#if connected}
				<button
					type="button"
					onclick={clearOutput}
					class="cursor-pointer rounded px-2.5 py-1 text-xs text-gray-400 transition-colors hover:bg-gray-700 hover:text-gray-200"
				>
					{translate($language, 'common.clear')}
				</button>
			{:else if !connecting}
				<button
					type="button"
					onclick={connect}
					class="cursor-pointer rounded bg-blue-600 px-3 py-1 text-xs text-white transition-colors hover:bg-blue-700"
				>
					{translate($language, 'common.reconnect')}
				</button>
			{/if}
		</div>
	</div>

	<!-- Output -->
	<textarea
		bind:this={outputEl}
		readonly
		value={output}
		class="w-full {heightClass} resize-y border-none bg-gray-950 p-4 font-mono text-sm leading-relaxed text-green-400 focus:outline-none"
		spellcheck="false"
	></textarea>

	<!-- Input -->
	<div class="flex border-t border-gray-700">
		<span
			class="flex max-w-56 items-center whitespace-nowrap bg-gray-900 px-3 font-mono text-sm {running ? 'text-yellow-400' : 'text-green-400'}"
			title={cwd || '~'}
		>
			{running ? '…' : `${shortenPath(cwd || '~')} $`}
		</span>
		<input
			type="text"
			bind:value={command}
			onkeydown={handleKeydown}
			onpaste={handlePaste}
			disabled={!connected}
			placeholder={running
				? translate($language, 'common.terminal.awaitingInput')
				: connected
					? translate($language, 'common.typeCommand')
					: translate($language, 'common.notConnected')}
			autocomplete="off"
			spellcheck="false"
			class="flex-1 bg-gray-900 px-3 py-3 font-mono text-sm text-white focus:outline-none disabled:cursor-not-allowed disabled:opacity-50"
		/>
		<button
			type="button"
			onclick={sendInterrupt}
			disabled={!running}
			title="Ctrl+C"
			class="cursor-pointer bg-red-600/80 px-4 py-3 text-sm font-medium text-white transition-colors hover:bg-red-600 disabled:cursor-not-allowed disabled:opacity-30"
		>
			Ctrl+C
		</button>
		<button
			type="button"
			onclick={() => (running ? sendInput() : sendCommand())}
			disabled={!connected || !command.trim()}
			class="cursor-pointer bg-blue-600 px-4 py-3 text-sm font-medium text-white transition-colors hover:bg-blue-700 disabled:opacity-50"
		>
			{translate($language, 'common.send')}
		</button>
	</div>

	<!-- Hints -->
	<div class="border-t border-gray-700/50 bg-gray-900 px-3 py-1.5 text-[10px] text-gray-500">
		{running
			? translate($language, 'common.terminal.runningHints')
			: translate($language, 'common.terminal.hints')}
	</div>
</div>
