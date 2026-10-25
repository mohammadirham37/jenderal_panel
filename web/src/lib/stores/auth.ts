import { writable, get } from 'svelte/store';
import { api, APIRequestError, setCSRFToken } from '#lib/api.js';
import type { User, Role, Permission, LoginResponse, UserWithRoles, ImpersonationInfo } from '#lib/types.js';

export const user = writable<User | null>(null);
export const roles = writable<Role[]>([]);
export const permissions = writable<Permission[]>([]);
export const isAuthenticated = writable(false);
export const authError = writable('');
export const impersonation = writable<ImpersonationInfo | null>(null);

const authRequestOptions = { timeoutMs: 15_000 };

/** Thrown when the account has 2FA enabled and login needs a TOTP code. */
export class TotpRequiredError extends Error {
	constructor() {
		super('totp required');
		this.name = 'TotpRequiredError';
	}
}

export interface CaptchaChallenge {
	id: string;
	image: string;
}

/**
 * Asks whether the login needs a captcha (after repeated failures from this
 * address or against this account) and returns a fresh challenge if so.
 */
export async function fetchLoginCaptcha(username = ''): Promise<CaptchaChallenge | null> {
	const data = await api.get<{ required: boolean; captcha?: CaptchaChallenge }>(
		`/api/v1/auth/captcha?username=${encodeURIComponent(username)}`,
		authRequestOptions
	);
	return data?.required && data.captcha ? data.captcha : null;
}

export async function login(
	username: string,
	password: string,
	totpCode = '',
	captcha: { id: string; answer: string } | null = null
): Promise<void> {
	authError.set('');
	const data = await api.post<LoginResponse>('/api/v1/auth/login',
		{
			username,
			password,
			totp_code: totpCode,
			captcha_id: captcha?.id ?? '',
			captcha_answer: captcha?.answer ?? ''
		},
		authRequestOptions
	);
	if (data.requires_totp) {
		// The server created no session; the caller must collect the code.
		throw new TotpRequiredError();
	}
	applyAuthResponse(data);
}

/** Applies a login/login-as/switch-back response to the auth stores. */
function applyAuthResponse(data: LoginResponse) {
	setCSRFToken(data.csrf_token);
	user.set(data.user);
	roles.set(data.roles || []);
	permissions.set(data.permissions || []);
	impersonation.set(data.impersonation ?? null);
	isAuthenticated.set(true);
}

/**
 * Starts an impersonation session for userId (admin login-as). Cookies and
 * CSRF token are swapped by the server response; the admin session stays
 * alive server-side so stopImpersonation can restore it.
 */
export async function loginAs(userId: string): Promise<void> {
	const data = await api.post<LoginResponse>(`/api/v1/users/${userId}/login-as`);
	applyAuthResponse(data);
}

/** Ends the current impersonation session and restores the admin session. */
export async function stopImpersonation(): Promise<void> {
	const data = await api.post<LoginResponse>('/api/v1/auth/impersonate/stop');
	applyAuthResponse(data);
}

export async function logout(): Promise<void> {
	// Best effort: local session state must clear immediately so the UI can
	// redirect to the login page, and the server-side logout proceeds in the
	// background. A stalled or failed call (busy server, network hiccup) only
	// leaves the session to expire — it must never block the redirect or
	// surface an error banner.
	const serverLogout = api.post('/api/v1/auth/logout', undefined, authRequestOptions).catch(() => {});
	user.set(null);
	roles.set([]);
	permissions.set([]);
	impersonation.set(null);
	isAuthenticated.set(false);
	setCSRFToken('');
	await serverLogout;
}

export async function checkAuth(): Promise<boolean> {
	try {
		const data = await api.get<UserWithRoles>('/api/v1/auth/me', authRequestOptions);
		authError.set('');
		user.set(data.user);
		roles.set(data.roles || []);
		permissions.set(data.permissions || []);
		impersonation.set(data.impersonation ?? null);
		isAuthenticated.set(true);
		return true;
	} catch (error) {
		setTimeoutError(error);
		user.set(null);
		roles.set([]);
		permissions.set([]);
		impersonation.set(null);
		isAuthenticated.set(false);
		return false;
	}
}

function setTimeoutError(error: unknown) {
	if (error instanceof APIRequestError && error.code === 'REQUEST_TIMEOUT') {
		authError.set(error.message);
	} else {
		authError.set('');
	}
}

export function hasPermission(userPerms: Permission[], required: string): boolean {
	return userPerms.some((p) => p.name === required);
}
