import { writable } from 'svelte/store';
import type { ServerMetrics } from '$lib/types';

export const currentMetrics = writable<ServerMetrics | null>(null);
export const metricsHistory = writable<ServerMetrics[]>([]);

let ws: WebSocket | null = null;
let reconnectTimer: ReturnType<typeof setTimeout> | null = null;

export function connectMetrics(): void {
	if (ws && (ws.readyState === WebSocket.OPEN || ws.readyState === WebSocket.CONNECTING)) {
		return;
	}

	const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
	const url = `${protocol}//${window.location.host}/ws/metrics`;

	ws = new WebSocket(url);

	ws.onmessage = (event) => {
		try {
			const metrics: ServerMetrics = JSON.parse(event.data);
			currentMetrics.set(metrics);
			metricsHistory.update((history) => {
				const updated = [...history, metrics];
				// Keep last 60 entries
				if (updated.length > 60) {
					return updated.slice(updated.length - 60);
				}
				return updated;
			});
		} catch {
			// Ignore malformed messages
		}
	};

	ws.onclose = () => {
		ws = null;
		// Auto-reconnect after 3 seconds
		if (reconnectTimer) clearTimeout(reconnectTimer);
		reconnectTimer = setTimeout(() => {
			connectMetrics();
		}, 3000);
	};

	ws.onerror = () => {
		ws?.close();
	};
}

export function disconnectMetrics(): void {
	if (reconnectTimer) {
		clearTimeout(reconnectTimer);
		reconnectTimer = null;
	}
	if (ws) {
		ws.onclose = null; // Prevent auto-reconnect
		ws.close();
		ws = null;
	}
}
