import type { ApiResponse, ApiError } from './types';

let csrfToken = '';

export class APIRequestError extends Error {
	constructor(message: string, public readonly status: number, public readonly code = '') {
		super(message);
		this.name = 'APIRequestError';
	}
}

type APIRequestOptions = {
	timeoutMs?: number;
};

async function responseError(res: Response): Promise<APIRequestError> {
	try {
		const errorData: ApiError = await res.json();
		return new APIRequestError(errorData.error?.message || `HTTP ${res.status}`, res.status, errorData.error?.code);
	} catch {
		return new APIRequestError(`HTTP ${res.status}: ${res.statusText}`, res.status);
	}
}

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

async function request<T>(
	method: string,
	path: string,
	body?: unknown,
	init: RequestInit = {},
	options: APIRequestOptions = {}
): Promise<T> {
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

	const controller = options.timeoutMs ? new AbortController() : undefined;
	let timedOut = false;
	const timeout = controller
		? setTimeout(() => {
			timedOut = true;
			controller.abort();
		}, options.timeoutMs)
		: undefined;

	try {
		const res = await fetch(path, {
			...init,
			method,
			headers,
			credentials: 'include',
			body: body ? JSON.stringify(body) : undefined,
			signal: controller?.signal ?? init.signal
		});

		if (!res.ok) {
			throw await responseError(res);
		}

		const data: ApiResponse<T> = await res.json();
		return data.data;
	} catch (error) {
		if (timedOut) {
			const seconds = Math.ceil((options.timeoutMs || 0) / 1000);
			throw new APIRequestError(
				`Server did not respond within ${seconds} seconds. Restart it over SSH: sudo /opt/jenderal/jenderal restart`,
				0,
				'REQUEST_TIMEOUT'
			);
		}
		throw error;
	} finally {
		if (timeout !== undefined) clearTimeout(timeout);
	}
}

export const api = {
	get<T>(path: string, options?: APIRequestOptions): Promise<T> {
		return request<T>('GET', path, undefined, {}, options);
	},
	getNoStore<T>(path: string): Promise<T> {
		return request<T>('GET', path, undefined, { cache: 'no-store' });
	},
	post<T>(path: string, body?: unknown, options?: APIRequestOptions): Promise<T> {
		return request<T>('POST', path, body, {}, options);
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
		throw await responseError(res);
	}

	return await res.json();
}
