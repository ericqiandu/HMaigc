export type CanvasPersistenceSnapshot =
    | { status: "idle" | "saving" | "saved"; error: null }
    | { status: "failed"; error: Error };

export type CanvasPersistenceScheduler<T> = {
    schedule: (name: string, value: T) => void;
    flush: () => Promise<void>;
    retry: () => Promise<void>;
    getSnapshot: () => CanvasPersistenceSnapshot;
    dispose: () => void;
};

type PendingPersistence<T> = {
    name: string;
    value: T;
};

type FailedPersistence<T> = PendingPersistence<T> & {
    payload: string | null;
    error: Error;
};

type CanvasPersistenceSchedulerOptions<T> = {
    delayMs: number;
    serialize: (value: T) => string;
    write: (name: string, payload: string) => Promise<void>;
};

export function createCanvasPersistenceScheduler<T>(options: CanvasPersistenceSchedulerOptions<T>): CanvasPersistenceScheduler<T> {
    if (!Number.isFinite(options.delayMs) || options.delayMs < 0) throw new Error("画布持久化延迟必须是非负有限数字");

    let timer: ReturnType<typeof setTimeout> | null = null;
    let pending: PendingPersistence<T> | null = null;
    let failed: FailedPersistence<T> | null = null;
    let activeWrite: Promise<void> | null = null;
    let disposed = false;
    let snapshot: CanvasPersistenceSnapshot = { status: "idle", error: null };

    const clearScheduledTimer = () => {
        if (!timer) return;
        clearTimeout(timer);
        timer = null;
    };

    const writePendingValues = async () => {
        if (failed) throw failed.error;

        while (pending && !disposed) {
            const current = pending;
            pending = null;
            snapshot = { status: "saving", error: null };

            let payload: string;
            try {
                payload = options.serialize(current.value);
            } catch (error) {
                const persistenceError = toPersistenceError(error);
                failed = { ...current, payload: null, error: persistenceError };
                snapshot = { status: "failed", error: persistenceError };
                throw persistenceError;
            }

            try {
                await options.write(current.name, payload);
            } catch (error) {
                const persistenceError = toPersistenceError(error);
                failed = { ...current, payload, error: persistenceError };
                snapshot = { status: "failed", error: persistenceError };
                throw persistenceError;
            }

            snapshot = { status: "saved", error: null };
        }
    };

    const flush = async (): Promise<void> => {
        clearScheduledTimer();
        if (disposed) return;
        if (activeWrite) {
            await activeWrite;
            if (pending) await flush();
            return;
        }

        const operation = writePendingValues();
        activeWrite = operation;
        try {
            await operation;
        } finally {
            if (activeWrite === operation) activeWrite = null;
        }
        if (pending) await flush();
    };

    const retry = async (): Promise<void> => {
        clearScheduledTimer();
        if (disposed) return;
        if (activeWrite) {
            try {
                await activeWrite;
            } catch {
                // The retained failure below is the authoritative retry input.
            }
        }

        const retained = failed;
        if (!retained) {
            await flush();
            return;
        }

        snapshot = { status: "saving", error: null };
        let payload = retained.payload;
        if (payload === null) {
            try {
                payload = options.serialize(retained.value);
            } catch (error) {
                const persistenceError = toPersistenceError(error);
                failed = { ...retained, payload: null, error: persistenceError };
                snapshot = { status: "failed", error: persistenceError };
                throw persistenceError;
            }
        }

        try {
            await options.write(retained.name, payload);
        } catch (error) {
            const persistenceError = toPersistenceError(error);
            failed = { ...retained, payload, error: persistenceError };
            snapshot = { status: "failed", error: persistenceError };
            throw persistenceError;
        }

        failed = null;
        snapshot = { status: "saved", error: null };
        await flush();
    };

    return {
        schedule: (name, value) => {
            if (disposed) throw new Error("画布持久化调度器已释放");
            pending = { name, value };
            clearScheduledTimer();
            timer = setTimeout(() => {
                timer = null;
                void flush().catch((error: unknown) => {
                    console.error("[canvas-persistence] scheduled write failed", toPersistenceError(error));
                });
            }, options.delayMs);
        },
        flush,
        retry,
        getSnapshot: () => snapshot,
        dispose: () => {
            disposed = true;
            pending = null;
            clearScheduledTimer();
        },
    };
}

function toPersistenceError(error: unknown) {
    return error instanceof Error ? error : new Error(String(error));
}
