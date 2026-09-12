<script lang="ts">
	import TerminalConsole from '$lib/components/TerminalConsole.svelte';
	import { language, translate } from '$lib/stores/language';

	const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
	const endpoint = `${protocol}//${window.location.host}/ws/terminal`;
</script>

<div class="space-y-6">
	<h2 class="text-2xl font-bold text-white">{translate($language, 'trm.title')}</h2>
	<TerminalConsole {endpoint} />
</div>
