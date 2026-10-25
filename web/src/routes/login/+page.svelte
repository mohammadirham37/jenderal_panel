<script lang="ts">
	import { goto } from '$app/navigation';
	import LogoMark from '#lib/components/LogoMark.svelte';
	import ThemeToggle from '#lib/components/ThemeToggle.svelte';
	import { login, fetchLoginCaptcha, isAuthenticated, authError, TotpRequiredError, type CaptchaChallenge } from '#lib/stores/auth.js';
	import { APIRequestError } from '#lib/api.js';
	import { language, translate } from '#lib/stores/language.js';
	import { onMount } from 'svelte';

	let username = $state('');
	let password = $state('');
	let totpCode = $state('');
	let totpRequired = $state(false);
	let totpInput = $state<HTMLInputElement | null>(null);
	let error = $state('');
	let submitting = $state(false);
	let visibleError = $derived(error || $authError);

	// Captcha: required by the server after repeated failed logins from this
	// address or against this account.
	let captcha = $state<CaptchaChallenge | null>(null);
	let captchaAnswer = $state('');
	let captchaLoading = $state(false);

	async function refreshCaptcha() {
		captchaLoading = true;
		try {
			captcha = await fetchLoginCaptcha(username.trim());
		} catch {
			// Leave the form usable; the server still enforces the captcha.
		} finally {
			captchaAnswer = '';
			captchaLoading = false;
		}
	}

	$effect(() => {
		if (totpRequired) totpInput?.focus();
	});

	onMount(() => {
		if ($isAuthenticated) {
			goto('/dashboard');
			return;
		}
		void refreshCaptcha();
	});

	async function handleSubmit(e: Event) {
		e.preventDefault();
		error = '';
		submitting = true;

		try {
			await login(
				username,
				password,
				totpRequired ? totpCode.trim() : '',
				captcha ? { id: captcha.id, answer: captchaAnswer.trim() } : null
			);
			totpRequired = false;
			goto('/dashboard');
		} catch (err) {
			if (err instanceof TotpRequiredError) {
				// Credentials accepted; the server waits for the 6-digit code.
				// The solved captcha stays valid for that second request.
				totpRequired = true;
				error = '';
			} else {
				const code = err instanceof APIRequestError ? err.code : '';
				error =
					code === 'CAPTCHA_REQUIRED'
						? translate($language, 'lg.captchaRequired')
						: code === 'CAPTCHA_INVALID'
							? translate($language, 'lg.captchaInvalid')
							: err instanceof Error
								? err.message
								: translate($language, 'lg.loginFailed');
				// Every failed attempt uses up the challenge; fetch the next
				// one (or learn that none is needed).
				if (totpRequired && code.startsWith('CAPTCHA_')) {
					// The code step has no captcha field: start over at the
					// credentials with a fresh challenge.
					totpRequired = false;
					totpCode = '';
					await refreshCaptcha();
				} else if (!totpRequired) {
					await refreshCaptcha();
				}
			}
		} finally {
			submitting = false;
		}
	}

	function backToCredentials() {
		totpRequired = false;
		totpCode = '';
		error = '';
	}

	// Accept pasted codes like "471 503" or "471-503" by stripping everything
	// that is not a digit; validation happens server-side.
	function sanitizeTotpCode() {
		totpCode = totpCode.replace(/\D/g, '').slice(0, 6);
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

				{#if totpRequired}
					<p class="text-xs font-semibold uppercase tracking-[0.2em] text-blue-400">{translate($language, 'lg.totpTitle')}</p>
					<h1 class="mt-3 text-3xl font-semibold tracking-tight text-white">{translate($language, 'lg.totpHeading')}</h1>
					<p class="mt-2 text-sm leading-6 text-gray-400">{translate($language, 'lg.totpDesc')}</p>
				{:else}
					<p class="text-xs font-semibold uppercase tracking-[0.2em] text-blue-400">{translate($language, 'lg.secureAccess')}</p>
					<h1 class="mt-3 text-3xl font-semibold tracking-tight text-white">{translate($language, 'lg.welcomeBack')}</h1>
					<p class="mt-2 text-sm leading-6 text-gray-400">{translate($language, 'lg.signInToContinue')}</p>
				{/if}

				{#if visibleError}
					<div id="login-error" role="alert" class="mt-6 rounded-xl border border-red-700/80 bg-red-900/40 p-3.5 text-sm text-red-300">
						{visibleError}
					</div>
				{/if}

				{#if totpRequired}
					<form onsubmit={handleSubmit} aria-describedby={visibleError ? 'login-error' : undefined} class="mt-8 space-y-5">
						<div>
							<label for="totp-code" class="mb-2 block text-sm font-medium text-gray-300">
								{translate($language, 'lg.totpCode')}
							</label>
							<input
								id="totp-code"
								type="text"
								bind:value={totpCode}
								bind:this={totpInput}
								oninput={sanitizeTotpCode}
								inputmode="numeric"
								maxlength={6}
								autocomplete="one-time-code"
								class="w-full rounded-xl border border-gray-700 bg-gray-950/70 px-4 py-3 text-center font-mono text-lg tracking-[0.4em] text-white outline-none transition placeholder:text-gray-400 hover:border-gray-600 focus:border-blue-500 focus:ring-4 focus:ring-blue-500/10"
								placeholder={translate($language, 'lg.totpCodePlaceholder')}
							/>
						</div>

						<button
							type="submit"
							disabled={submitting || totpCode.trim().length !== 6}
							class="group flex w-full cursor-pointer items-center justify-center gap-2 rounded-xl bg-blue-600 px-4 py-3 font-semibold text-white shadow-lg shadow-blue-950/40 transition hover:bg-blue-700 hover:shadow-blue-900/40 disabled:cursor-not-allowed disabled:bg-blue-800 disabled:text-blue-200"
						>
							{submitting ? translate($language, 'lg.verifying') : translate($language, 'lg.verify')}
						</button>
					</form>
					<button
						type="button"
						onclick={backToCredentials}
						class="mt-4 w-full cursor-pointer text-center text-xs text-gray-400 transition hover:text-gray-200"
					>
						{translate($language, 'lg.useAnotherAccount')}
					</button>
				{:else}
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

					{#if captcha}
						<div>
							<label for="captcha-answer" class="mb-2 block text-sm font-medium text-gray-300">
								{translate($language, 'lg.captcha')}
							</label>
							<div class="flex items-center gap-2">
								<img
									src={captcha.image}
									alt={translate($language, 'lg.captchaAlt')}
									width="168"
									height="56"
									class="h-14 w-[168px] shrink-0 rounded-lg border border-gray-700 bg-white"
								/>
								<button
									type="button"
									onclick={refreshCaptcha}
									disabled={captchaLoading}
									title={translate($language, 'lg.captchaRefresh')}
									aria-label={translate($language, 'lg.captchaRefresh')}
									class="cursor-pointer rounded-lg border border-gray-700 p-2.5 text-gray-400 transition hover:border-gray-500 hover:text-gray-200 disabled:opacity-50"
								>
									<svg class="h-4 w-4 {captchaLoading ? 'animate-spin' : ''}" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="2" aria-hidden="true">
										<path stroke-linecap="round" stroke-linejoin="round" d="M16.023 9.348h4.992v-.001M2.985 19.644v-4.992m0 0h4.992m-4.993 0 3.181 3.183a8.25 8.25 0 0 0 13.803-3.7M4.031 9.865a8.25 8.25 0 0 1 13.803-3.7l3.181 3.182m0-4.991v4.99" />
									</svg>
								</button>
							</div>
							<input
								id="captcha-answer"
								type="text"
								bind:value={captchaAnswer}
								required
								maxlength={5}
								autocomplete="off"
								autocapitalize="characters"
								spellcheck="false"
								class="mt-2 w-full rounded-xl border border-gray-700 bg-gray-950/70 px-4 py-3 font-mono uppercase tracking-[0.3em] text-white outline-none transition placeholder:normal-case placeholder:tracking-normal placeholder:text-gray-400 hover:border-gray-600 focus:border-blue-500 focus:ring-4 focus:ring-blue-500/10"
								placeholder={translate($language, 'lg.captchaPlaceholder')}
							/>
							<p class="mt-1.5 text-xs text-gray-500">{translate($language, 'lg.captchaHint')}</p>
						</div>
					{/if}

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
				{/if}

				<p class="mt-8 text-center text-xs text-gray-400">{translate($language, 'lg.protectedAccess')}</p>
			</div>
		</section>
	</main>
</div>
