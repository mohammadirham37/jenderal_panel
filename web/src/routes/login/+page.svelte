<script lang="ts">
	import { goto } from '$app/navigation';
	import LogoMark from '$lib/components/LogoMark.svelte';
	import ThemeToggle from '$lib/components/ThemeToggle.svelte';
	import { login, isAuthenticated, authError } from '$lib/stores/auth';
	import { language, translate } from '$lib/stores/language';
	import { onMount } from 'svelte';

	let username = $state('');
	let password = $state('');
	let error = $state('');
	let submitting = $state(false);
	let visibleError = $derived(error || $authError);

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
			error = err instanceof Error ? err.message : translate($language, 'lg.loginFailed');
		} finally {
			submitting = false;
		}
	}
</script>

<div class="login-shell flex min-h-screen items-center px-4 py-8 sm:px-6 lg:px-8">
	<div class="absolute right-4 top-4 z-20 sm:right-6 sm:top-6">
		<ThemeToggle />
	</div>
	<main
		class="login-card relative z-10 mx-auto grid w-full max-w-6xl overflow-hidden rounded-[2rem] border border-white/10 lg:min-h-[680px] lg:grid-cols-[1.08fr_0.92fr]"
	>
		<section class="login-brand-panel hidden flex-col justify-between border-r border-white/8 p-12 lg:flex">
			<div class="flex items-center gap-4">
				<div class="rounded-2xl border border-blue-400/20 bg-blue-500/10 p-2.5 shadow-lg shadow-blue-950/30">
					<LogoMark size="lg" decorative />
				</div>
				<div>
					<p class="text-xl font-semibold tracking-tight text-white">Jenderal Panel</p>
					<p class="mt-1 text-xs font-medium uppercase tracking-[0.2em] text-blue-300">
						{translate($language, 'lg.tagline')}
					</p>
				</div>
			</div>

			<div class="max-w-lg">
				<div class="mb-6 inline-flex items-center gap-2 rounded-full border border-blue-400/15 bg-blue-500/8 px-3 py-1.5 text-xs font-medium text-blue-200">
					<span class="h-1.5 w-1.5 rounded-full bg-blue-400 shadow-[0_0_12px_rgba(45,212,191,0.8)]"></span>
					{translate($language, 'lg.badge')}
				</div>
				<h2 class="text-4xl font-semibold leading-tight tracking-[-0.035em] text-white xl:text-5xl">
					{translate($language, 'lg.heroTitle')}
				</h2>
				<p class="mt-5 max-w-md text-base leading-7 text-gray-400">
					{translate($language, 'lg.heroSubtitle')}
				</p>

				<div class="mt-10 grid grid-cols-3 gap-3">
					<div class="rounded-2xl border border-white/8 bg-white/[0.025] p-4">
						<p class="text-sm font-medium text-gray-200">{translate($language, 'lg.featureSecure')}</p>
						<p class="mt-1 text-xs text-gray-400">{translate($language, 'lg.featureSecureDesc')}</p>
					</div>
					<div class="rounded-2xl border border-white/8 bg-white/[0.025] p-4">
						<p class="text-sm font-medium text-gray-200">{translate($language, 'lg.featureLive')}</p>
						<p class="mt-1 text-xs text-gray-400">{translate($language, 'lg.featureLiveDesc')}</p>
					</div>
					<div class="rounded-2xl border border-white/8 bg-white/[0.025] p-4">
						<p class="text-sm font-medium text-gray-200">{translate($language, 'lg.featureLean')}</p>
						<p class="mt-1 text-xs text-gray-400">{translate($language, 'lg.featureLeanDesc')}</p>
					</div>
				</div>
			</div>

			<p class="text-xs text-gray-400">{translate($language, 'lg.footer')}</p>
		</section>

		<section class="flex items-center justify-center p-6 sm:p-10 lg:p-12">
			<div class="w-full max-w-sm">
				<div class="mb-10 lg:hidden">
					<div class="mb-5 flex items-center gap-3">
						<div class="rounded-2xl border border-blue-400/20 bg-blue-500/10 p-2">
							<LogoMark size="lg" decorative />
						</div>
						<div>
							<p class="text-xl font-semibold text-white">Jenderal Panel</p>
							<p class="text-xs uppercase tracking-[0.18em] text-blue-300">{translate($language, 'lg.mobileTagline')}</p>
						</div>
					</div>
				</div>

				<p class="text-xs font-semibold uppercase tracking-[0.2em] text-blue-400">{translate($language, 'lg.secureAccess')}</p>
				<h1 class="mt-3 text-3xl font-semibold tracking-tight text-white">{translate($language, 'lg.welcomeBack')}</h1>
				<p class="mt-2 text-sm leading-6 text-gray-400">{translate($language, 'lg.signInToContinue')}</p>

				{#if visibleError}
					<div id="login-error" role="alert" class="mt-6 rounded-xl border border-red-700/80 bg-red-900/40 p-3.5 text-sm text-red-300">
						{visibleError}
					</div>
				{/if}

				<form onsubmit={handleSubmit} aria-describedby={visibleError ? 'login-error' : undefined} class="mt-8 space-y-5">
					<div>
						<label for="username" class="mb-2 block text-sm font-medium text-gray-300">
							{translate($language, 'lg.username')}
						</label>
						<input
							id="username"
							type="text"
							bind:value={username}
							required
							autocomplete="username"
							class="w-full rounded-xl border border-gray-700 bg-gray-950/70 px-4 py-3 text-white outline-none transition placeholder:text-gray-400 hover:border-gray-600 focus:border-blue-500 focus:ring-4 focus:ring-blue-500/10"
							placeholder={translate($language, 'lg.usernamePlaceholder')}
						/>
					</div>

					<div>
						<label for="password" class="mb-2 block text-sm font-medium text-gray-300">
							{translate($language, 'lg.password')}
						</label>
						<input
							id="password"
							type="password"
							bind:value={password}
							required
							autocomplete="current-password"
							class="w-full rounded-xl border border-gray-700 bg-gray-950/70 px-4 py-3 text-white outline-none transition placeholder:text-gray-400 hover:border-gray-600 focus:border-blue-500 focus:ring-4 focus:ring-blue-500/10"
							placeholder={translate($language, 'lg.passwordPlaceholder')}
						/>
					</div>

					<button
						type="submit"
						disabled={submitting}
						class="group flex w-full cursor-pointer items-center justify-center gap-2 rounded-xl bg-blue-600 px-4 py-3 font-semibold text-white shadow-lg shadow-blue-950/40 transition hover:bg-blue-700 hover:shadow-blue-900/40 disabled:cursor-not-allowed disabled:bg-blue-800 disabled:text-blue-200"
					>
						{submitting ? translate($language, 'lg.signingIn') : translate($language, 'lg.signIn')}
						{#if !submitting}
							<svg class="h-4 w-4 transition-transform group-hover:translate-x-0.5" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true">
								<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 5l7 7-7 7" />
							</svg>
						{/if}
					</button>
				</form>

				<p class="mt-8 text-center text-xs text-gray-400">{translate($language, 'lg.protectedAccess')}</p>
			</div>
		</section>
	</main>
</div>
