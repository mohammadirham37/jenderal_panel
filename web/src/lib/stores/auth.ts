import { writable, get } from 'svelte/store';
import { api, setCSRFToken } from '$lib/api';
import type { User, Permission, LoginResponse, UserWithRoles } from '$lib/types';

export const user = writable<User | null>(null);
export const permissions = writable<Permission[]>([]);
export const isAuthenticated = writable(false);

export async function login(username: string, password: string): Promise<void> {
	const data = await api.post<LoginResponse>('/api/v1/auth/login', { username, password });
	setCSRFToken(data.csrf_token);
	user.set(data.user);
	permissions.set(data.permissions || []);
	isAuthenticated.set(true);
}

export async function logout(): Promise<void> {
	try {
		await api.post('/api/v1/auth/logout');
	} catch {
		// Ignore errors on logout
	}
	user.set(null);
	permissions.set([]);
	isAuthenticated.set(false);
	setCSRFToken('');
}

export async function checkAuth(): Promise<boolean> {
	try {
		const data = await api.get<UserWithRoles>('/api/v1/auth/me');
		user.set(data.user);
		permissions.set(data.permissions || []);
		isAuthenticated.set(true);
		return true;
	} catch {
		user.set(null);
		permissions.set([]);
		isAuthenticated.set(false);
		return false;
	}
}

export function hasPermission(userPerms: Permission[], required: string): boolean {
	return userPerms.some((p) => p.name === required);
}
