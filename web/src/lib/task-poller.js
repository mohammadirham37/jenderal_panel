/**
 * Poll a background task until it completes or the caller stops polling.
 * Request failures are treated as temporary so a page can recover while the
 * panel or network is briefly unavailable.
 *
 * @template {{ status: string }} T
 * @param {{
 *   taskId: string,
 *   fetchTask: (taskId: string) => Promise<T>,
 *   onTask: (task: T) => void,
 *   onComplete: (task: T) => void,
 *   onError?: (error: unknown) => void,
 *   onMissing?: (error: unknown) => void,
 *   intervalMs?: number,
 *   schedule?: (callback: () => void, intervalMs: number) => number,
 *   cancel?: (timer: number) => void
 * }} options
 */
export function createTaskPoller(options) {
	const {
		taskId,
		fetchTask,
		onTask,
		onComplete,
		onError = () => {},
		onMissing = () => {},
		intervalMs = 2000,
		schedule = setInterval,
		cancel = clearInterval
	} = options;
	let stopped = false;
	let inFlight = false;
	/** @type {number | undefined} */
	let timer;

	async function poll() {
		if (stopped || inFlight) return;
		inFlight = true;
		try {
			const task = await fetchTask(taskId);
			if (stopped) return;
			onTask(task);
			if (task.status === 'completed' || task.status === 'failed') {
				stop();
				onComplete(task);
			}
		} catch (error) {
			if (!stopped && error && typeof error === 'object' && 'status' in error && error.status === 404) {
				stop();
				onMissing(error);
			} else if (!stopped) {
				onError(error);
			}
		} finally {
			inFlight = false;
		}
	}

	function stop() {
		if (stopped) return;
		stopped = true;
		if (timer !== undefined) cancel(timer);
	}

	timer = schedule(() => void poll(), intervalMs);
	void poll();

	return stop;
}

/**
 * Clear a task that the backend no longer knows about, then let the owning
 * page release any operation-specific state it keeps alongside the task ID.
 *
 * @param {{
 *   storageKey?: string,
 *   storage?: { removeItem: (key: string) => void },
 *   clearTask: () => void,
 *   onMissing?: () => void
 * }} options
 */
export function clearMissingTask(options) {
	const {
		storageKey = '',
		storage = globalThis.localStorage,
		clearTask,
		onMissing = () => {}
	} = options;
	clearTask();
	if (storageKey) storage?.removeItem(storageKey);
	onMissing();
}
