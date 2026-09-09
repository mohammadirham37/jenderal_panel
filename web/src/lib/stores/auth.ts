import { writable, get } from 'svelte/store';
import { api, APIRequestError, setCSRFToken } from '$lib/api';
import type { User, Permission, LoginResponse, UserWithRoles } from '$lib/types';

export const user = writable<User | null>(null);
export const permissions = writable<Permission[]>([]);
export const isAuthenticated = writable(false);
export const authError = writable('');

const authRequestOptions = { timeoutMs: 15_000 };

export async function login(username: string, password: string): Promise<void> {
	authError.set('');
	const data = await api.post<LoginResponse>('/api/v1/auth/login', { username, password }, authRequestOptions);
	setCSRFToken(data.csrf_token);
	user.set(data.user);
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
	permissions.set([]);
	isAuthenticated.set(false);
	setCSRFToken('');
}

export async function checkAuth(): Promise<boolean> {
	try {
		const data = await api.get<UserWithRoles>('/api/v1/auth/me', authRequestOptions);
		authError.set('');
		user.set(data.user);
		permissions.set(data.permissions || []);
		isAuthenticated.set(true);
		return true;
	} catch (error) {
		setTimeoutError(error);
		user.set(null);
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
