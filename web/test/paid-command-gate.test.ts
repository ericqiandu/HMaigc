import { describe, expect, test } from "bun:test";

import { createPaidCommandGate } from "../src/lib/billing/paid-command-gate";
import type { TaskBillingQuote } from "../src/services/api/task-center";

const quote: TaskBillingQuote = {
    amountMicrocredits: 100,
    perTaskAmountMicrocredits: 100,
    taskCount: 1,
    priceVersion: 7,
    billingMode: "fixed_request",
    pricingResolution: "1024x1024",
    pricingInputVariant: "default",
    quantity: 1,
    enhancementAmountMicrocredits: 0,
    quoteFingerprint: "quote-7",
};

describe("paid command gate", () => {
    test("executes once with exactly the quote the user confirmed", async () => {
        const gate = createPaidCommandGate();
        const confirmed: TaskBillingQuote[] = [];
        const executed: Array<{ priceVersion: number; quoteFingerprint: string }> = [];
        const result = await gate.run({
            commandKey: "node-1:maskEdit",
            loadQuote: async () => quote,
            confirm: async (value) => { confirmed.push(value); return true; },
            execute: async (value) => { executed.push(value); return "done"; },
        });
        expect(result).toEqual({ status: "executed", value: "done" });
        expect(confirmed).toEqual([quote]);
        expect(executed).toEqual([{ priceVersion: 7, quoteFingerprint: "quote-7" }]);
    });

    test("does not execute when the user rejects the quote", async () => {
        const gate = createPaidCommandGate();
        let executions = 0;
        const result = await gate.run({ commandKey: "node-1:angle", loadQuote: async () => quote, confirm: async () => false, execute: async () => { executions += 1; } });
        expect(result).toEqual({ status: "rejected" });
        expect(executions).toBe(0);
    });

    test("propagates stale quote failure and releases the command for retry", async () => {
        const gate = createPaidCommandGate();
        let attempts = 0;
        const run = () => gate.run({ commandKey: "node-1:emotion", loadQuote: async () => quote, confirm: async () => true, execute: async () => { attempts += 1; throw new Error("预计积分已变化"); } });
        await expect(run()).rejects.toThrow("预计积分已变化");
        await expect(run()).rejects.toThrow("预计积分已变化");
        expect(attempts).toBe(2);
    });

    test("rejects a concurrent duplicate before loading another quote", async () => {
        const gate = createPaidCommandGate();
        let markEntered!: () => void;
        const entered = new Promise<void>((resolve) => { markEntered = resolve; });
        let release!: () => void;
        const held = new Promise<void>((resolve) => { release = resolve; });
        let quoteCalls = 0;
        const first = gate.run({ commandKey: "node-1:maskEdit", loadQuote: async () => { quoteCalls += 1; return quote; }, confirm: async () => true, execute: async () => { markEntered(); await held; } });
        await entered;
        await expect(gate.run({ commandKey: "node-1:maskEdit", loadQuote: async () => { quoteCalls += 1; return quote; }, confirm: async () => true, execute: async () => undefined })).rejects.toThrow("同一付费操作正在执行");
        expect(quoteCalls).toBe(1);
        release();
        await first;
    });
});
