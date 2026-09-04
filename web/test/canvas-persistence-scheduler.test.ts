import { describe, expect, test } from "bun:test";

import { createCanvasPersistenceScheduler } from "../src/stores/canvas/canvas-persistence-scheduler";

describe("canvas persistence scheduler", () => {
    test("coalesces scheduled values and defers serialization until flush", async () => {
        const writes: Array<{ name: string; payload: string }> = [];
        let serializeCount = 0;
        const scheduler = createCanvasPersistenceScheduler({
            delayMs: 10_000,
            serialize: (value: { revision: number }) => {
                serializeCount += 1;
                return JSON.stringify(value);
            },
            write: async (name, payload) => {
                writes.push({ name, payload });
            },
        });

        scheduler.schedule("canvas", { revision: 1 });
        scheduler.schedule("canvas", { revision: 2 });
        expect(serializeCount).toBe(0);
        expect(writes).toEqual([]);
        expect(scheduler.getSnapshot()).toEqual({ status: "idle", error: null });

        await scheduler.flush();

        expect(serializeCount).toBe(1);
        expect(writes).toEqual([{ name: "canvas", payload: '{"revision":2}' }]);
        expect(scheduler.getSnapshot()).toEqual({ status: "saved", error: null });
    });

    test("reports saving while a write is pending and saved after it resolves", async () => {
        let releaseWrite: (() => void) | null = null;
        const scheduler = createCanvasPersistenceScheduler({
            delayMs: 10_000,
            serialize: JSON.stringify,
            write: () =>
                new Promise<void>((resolve) => {
                    releaseWrite = resolve;
                }),
        });

        scheduler.schedule("canvas", { revision: 1 });
        const flushPromise = scheduler.flush();
        expect(scheduler.getSnapshot()).toEqual({ status: "saving", error: null });
        releaseWrite?.();
        await flushPromise;

        expect(scheduler.getSnapshot()).toEqual({ status: "saved", error: null });
    });

    test("retains a rejected payload and retries that exact payload", async () => {
        const failure = new Error("IndexedDB write rejected");
        const writes: string[] = [];
        let shouldFail = true;
        const scheduler = createCanvasPersistenceScheduler({
            delayMs: 10_000,
            serialize: (value: { revision: number }) => JSON.stringify(value),
            write: async (_name, payload) => {
                writes.push(payload);
                if (shouldFail) throw failure;
            },
        });

        scheduler.schedule("canvas", { revision: 3 });
        await expect(scheduler.flush()).rejects.toThrow("IndexedDB write rejected");
        expect(scheduler.getSnapshot()).toEqual({ status: "failed", error: failure });

        shouldFail = false;
        await scheduler.retry();

        expect(writes).toEqual(['{"revision":3}', '{"revision":3}']);
        expect(scheduler.getSnapshot()).toEqual({ status: "saved", error: null });
    });

    test("does not let a newer schedule replace a retained failed payload", async () => {
        const writes: string[] = [];
        let attempts = 0;
        const scheduler = createCanvasPersistenceScheduler({
            delayMs: 10_000,
            serialize: (value: { revision: number }) => JSON.stringify(value),
            write: async (_name, payload) => {
                writes.push(payload);
                attempts += 1;
                if (attempts === 1) throw new Error("first write failed");
            },
        });

        scheduler.schedule("canvas", { revision: 4 });
        await expect(scheduler.flush()).rejects.toThrow("first write failed");
        scheduler.schedule("canvas", { revision: 5 });

        await scheduler.retry();

        expect(writes).toEqual(['{"revision":4}', '{"revision":4}', '{"revision":5}']);
        expect(scheduler.getSnapshot()).toEqual({ status: "saved", error: null });
    });

    test("dispose cancels a pending timer without serializing or writing", async () => {
        let serializeCount = 0;
        const writes: string[] = [];
        const scheduler = createCanvasPersistenceScheduler({
            delayMs: 5,
            serialize: (value: { revision: number }) => {
                serializeCount += 1;
                return JSON.stringify(value);
            },
            write: async (_name, payload) => {
                writes.push(payload);
            },
        });

        scheduler.schedule("canvas", { revision: 6 });
        scheduler.dispose();
        await new Promise((resolve) => setTimeout(resolve, 15));

        expect(serializeCount).toBe(0);
        expect(writes).toEqual([]);
    });
});
