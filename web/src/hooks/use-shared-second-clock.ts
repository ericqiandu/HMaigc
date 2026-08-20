import { useSyncExternalStore } from "react";

type ClockScheduler = {
    now: () => number;
    setInterval: (callback: () => void, delay: number) => number;
    clearInterval: (handle: number) => void;
};

type SharedSecondClock = {
    subscribe: (listener: () => void) => () => void;
    getSnapshot: () => number;
};

export function createSharedSecondClock(scheduler: ClockScheduler): SharedSecondClock {
    const listeners = new Set<() => void>();
    let snapshot = scheduler.now();
    let intervalHandle: number | null = null;

    return {
        subscribe(listener) {
            listeners.add(listener);
            if (intervalHandle === null) {
                snapshot = scheduler.now();
                intervalHandle = scheduler.setInterval(() => {
                    snapshot = scheduler.now();
                    for (const notify of listeners) notify();
                }, 1_000);
            }
            return () => {
                listeners.delete(listener);
                if (listeners.size === 0 && intervalHandle !== null) {
                    scheduler.clearInterval(intervalHandle);
                    intervalHandle = null;
                }
            };
        },
        getSnapshot: () => snapshot,
    };
}

const sharedSecondClock = createSharedSecondClock({
    now: () => Date.now(),
    setInterval: (callback, delay) => window.setInterval(callback, delay),
    clearInterval: (handle) => window.clearInterval(handle),
});
const inactiveSubscribe = () => () => undefined;
const inactiveSnapshot = () => 0;

export function useSharedSecondNow(active = true) {
    return useSyncExternalStore(active ? sharedSecondClock.subscribe : inactiveSubscribe, active ? sharedSecondClock.getSnapshot : inactiveSnapshot, active ? sharedSecondClock.getSnapshot : inactiveSnapshot);
}
