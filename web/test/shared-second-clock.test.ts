import { describe, expect, test } from "bun:test";

import { createSharedSecondClock } from "../src/hooks/use-shared-second-clock";

describe("shared second clock", () => {
    test("uses one interval for all subscribers and releases it after the final unsubscribe", () => {
        let now = 1_000;
        let tick: (() => void) | undefined;
        let intervalStarts = 0;
        const clearedHandles: number[] = [];
        const clock = createSharedSecondClock({
            now: () => now,
            setInterval: (callback) => {
                intervalStarts += 1;
                tick = callback;
                return 17;
            },
            clearInterval: (handle) => clearedHandles.push(handle),
        });
        let firstUpdates = 0;
        let secondUpdates = 0;

        const unsubscribeFirst = clock.subscribe(() => {
            firstUpdates += 1;
        });
        const unsubscribeSecond = clock.subscribe(() => {
            secondUpdates += 1;
        });

        expect(intervalStarts).toBe(1);
        now = 2_000;
        tick?.();
        expect(clock.getSnapshot()).toBe(2_000);
        expect([firstUpdates, secondUpdates]).toEqual([1, 1]);

        unsubscribeFirst();
        expect(clearedHandles).toEqual([]);
        unsubscribeSecond();
        expect(clearedHandles).toEqual([17]);
    });
});
