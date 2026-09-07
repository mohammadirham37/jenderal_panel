<script lang="ts">
	import { goto } from '$app/navigation';
	import { login, isAuthenticated } from '$lib/stores/auth';
	import { onMount } from 'svelte';

	let username = $state('');
	let password = $state('');
	let error = $state('');
	let submitting = $state(false);

	onMount(() => {
		if ($isAuthenticated) {
			goto('/dashboard');
		}
	});

	async function handleSubmit(e: Event) {
		e.preventDefault();
		error = '';
		submitting = true;

		try {
			await login(username, password);
			goto('/dashboard');
		} catch (err) {
			error = err instanceof Error ? err.message : 'Login failed';
		} finally {
			submitting = false;
		}
	}
</script>

<div class="flex min-h-screen items-center justify-center bg-gray-900 p-4">
	<div class="w-full max-w-sm">
		<div class="bg-gray-800 rounded-xl shadow-2xl border border-gray-700 p-8">
			<div class="text-center mb-8">
				<h1 class="text-2xl font-bold text-white">Jenderal Panel</h1>
				<p class="text-gray-400 text-sm mt-1">Sign in to your account</p>
			</div>

			{#if error}
				<div class="mb-4 p-3 bg-red-900/50 border border-red-700 rounded-lg text-red-300 text-sm">
					{error}
				</div>
			{/if}

			<form onsubmit={handleSubmit} class="space-y-4">
				<div>
					<label for="username" class="block text-sm font-medium text-gray-300 mb-1">
						Username
					</label>
					<input
						id="username"
						type="text"
						bind:value={username}
						required
						autocomplete="username"
						class="w-full px-3 py-2 bg-gray-700 border border-gray-600 rounded-lg text-white placeholder-gray-400 focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-transparent"
						placeholder="Enter your username"
					/>
				</div>

				<div>
					<label for="password" class="block text-sm font-medium text-gray-300 mb-1">
						Password
					</label>
					<input
						id="password"
						type="password"
						bind:value={password}
						required
						autocomplete="current-password"
						class="w-full px-3 py-2 bg-gray-700 border border-gray-600 rounded-lg text-white placeholder-gray-400 focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-transparent"
						placeholder="Enter your password"
					/>
				</div>

				<button
					type="submit"
					disabled={submitting}
					class="w-full py-2.5 px-4 bg-blue-600 hover:bg-blue-700 disabled:bg-blue-800 disabled:cursor-not-allowed text-white font-medium rounded-lg transition-colors cursor-pointer"
				>
					{submitting ? 'Signing in...' : 'Sign In'}
				</button>
			</form>
		</div>
	</div>
</div>
