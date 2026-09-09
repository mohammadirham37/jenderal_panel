import type { ApiResponse, ApiError } from './types';

let csrfToken = '';

export function setCSRFToken(token: string) {
	csrfToken = token;
}

export function getCSRFToken(): string {
	if (csrfToken) return csrfToken;
	// Try reading from cookie
	const match = document.cookie.match(/(?:^|;\s*)csrf_token=([^;]*)/);
	if (match) {
		csrfToken = decodeURIComponent(match[1]);
	}
	return csrfToken;
}

async function request<T>(method: string, path: string, body?: unknown, init: RequestInit = {}): Promise<T> {
	const headers: Record<string, string> = {
		'Content-Type': 'application/json'
	};

	// Add CSRF token on mutating requests
	if (method !== 'GET') {
		const token = getCSRFToken();
		if (token) {
			headers['X-CSRF-Token'] = token;
		}
	}

	const res = await fetch(path, {
		...init,
		method,
		headers,
		credentials: 'include',
		body: body ? JSON.stringify(body) : undefined
	});

	if (!res.ok) {
		let errorData: ApiError;
		try {
			errorData = await res.json();
		} catch {
			throw new Error(`HTTP ${res.status}: ${res.statusText}`);
		}
		throw new Error(errorData.error?.message || `HTTP ${res.status}`);
	}

	const data: ApiResponse<T> = await res.json();
	return data.data;
}

export const api = {
	get<T>(path: string): Promise<T> {
		return request<T>('GET', path);
	},
	getNoStore<T>(path: string): Promise<T> {
		return request<T>('GET', path, undefined, { cache: 'no-store' });
	},
	post<T>(path: string, body?: unknown): Promise<T> {
		return request<T>('POST', path, body);
	},
	put<T>(path: string, body?: unknown): Promise<T> {
		return request<T>('PUT', path, body);
	},
	del<T>(path: string): Promise<T> {
		return request<T>('DELETE', path);
	}
};

// Raw request that returns the full response including meta
export async function apiRaw<T>(method: string, path: string, body?: unknown): Promise<ApiResponse<T>> {
	const headers: Record<string, string> = {
		'Content-Type': 'application/json'
	};

	if (method !== 'GET') {
		const token = getCSRFToken();
		if (token) {
			headers['X-CSRF-Token'] = token;
		}
	}

	const res = await fetch(path, {
		method,
		headers,
		credentials: 'include',
		body: body ? JSON.stringify(body) : undefined
	});

	if (!res.ok) {
		let errorData: ApiError;
		try {
			errorData = await res.json();
		} catch {
			throw new Error(`HTTP ${res.status}: ${res.statusText}`);
		}
		throw new Error(errorData.error?.message || `HTTP ${res.status}`);
	}

	return await res.json();
}
