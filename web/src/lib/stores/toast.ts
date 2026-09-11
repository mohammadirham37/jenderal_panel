import { writable } from 'svelte/store';

export type ToastType = 'success' | 'error' | 'info';

export interface Toast {
	id: number;
	type: ToastType;
	message: string;
}

export const toasts = writable<Toast[]>([]);

// Errors stay up longer so they can be read; the rest auto-dismiss quickly.
const DURATION: Record<ToastType, number> = { success: 4000, info: 4000, error: 8000 };
const MAX_TOASTS = 5;

let nextId = 0;

export function dismissToast(id: number) {
	toasts.update((all) => all.filter((t) => t.id !== id));
}

function push(type: ToastType, message: string) {
	const id = ++nextId;
	toasts.update((all) => {
		const next = [...all, { id, type, message }];
		return next.length > MAX_TOASTS ? next.slice(-MAX_TOASTS) : next;
	});
	setTimeout(() => dismissToast(id), DURATION[type]);
}

export const toast = {
	success: (message: string) => push('success', message),
	error: (message: string) => push('error', message),
	info: (message: string) => push('info', message)
};
