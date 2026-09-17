import { writable, get } from 'svelte/store';
import { api, APIRequestError, setCSRFToken } from '$lib/api';
import type { User, Role, Permission, LoginResponse, UserWithRoles } from '$lib/types';

export const user = writable<User | null>(null);
export const roles = writable<Role[]>([]);
export const permissions = writable<Permission[]>([]);
export const isAuthenticated = writable(false);
export const authError = writable('');

const authRequestOptions = { timeoutMs: 15_000 };

/** Thrown when the account has 2FA enabled and login needs a TOTP code. */
export class TotpRequiredError extends Error {
	constructor() {
		super('totp required');
		this.name = 'TotpRequiredError';
	}
}

export async function login(username: string, password: string, totpCode = ''): Promise<void> {
	authError.set('');
	const data = await api.post<LoginResponse>('/api/v1/auth/login', { username, password, totp_code: totpCode }, authRequestOptions);
	if (data.requires_totp) {
		// The server created no session; the caller must collect the code.
		throw new TotpRequiredError();
	}
	setCSRFToken(data.csrf_token);
	user.set(data.user);
	roles.set(data.roles || []);
	permissions.set(data.permissions || []);
	isAuthenticated.set(true);
}

export async function logout(): Promise<void> {
	try {
		await api.post('/api/v1/auth/logout', undefined, authRequestOptions);
	} catch (error) {
		setTimeoutError(error);
	}
	user.set(null);
	roles.set([]);
	permissions.set([]);
	isAuthenticated.set(false);
	setCSRFToken('');
}

export async function checkAuth(): Promise<boolean> {
	try {
		const data = await api.get<UserWithRoles>('/api/v1/auth/me', authRequestOptions);
		authError.set('');
		user.set(data.user);
		roles.set(data.roles || []);
		permissions.set(data.permissions || []);
		isAuthenticated.set(true);
		return true;
	} catch (error) {
		setTimeoutError(error);
		user.set(null);
		roles.set([]);
		permissions.set([]);
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
