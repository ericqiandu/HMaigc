import "./setup-happy-dom";

import { afterEach, describe, expect, test } from "bun:test";
import { act, createElement } from "react";
import { createRoot, type Root } from "react-dom/client";

import { invalidateTaskBillingQuotes, useTaskBillingQuote } from "../src/hooks/use-task-billing-quote";
import type { TaskBillingQuote, TaskBillingQuoteRequest } from "../src/services/api/task-center";

type Deferred<T> = {
    promise: Promise<T>;
    resolve: (value: T) => void;
    reject: (reason: Error) => void;
};

function deferred<T>(): Deferred<T> {
    let resolve!: (value: T) => void;
    let reject!: (reason: Error) => void;
    const promise = new Promise<T>((resolvePromise, rejectPromise) => {
        resolve = resolvePromise;
        reject = rejectPromise;
    });
    return { promise, resolve, reject };
}

const imageQuoteRequest = (model: string): TaskBillingQuoteRequest => ({
    projectId: "canvas-project",
    type: "canvas_image",
    operation: "generate",
    batchCount: 1,
    input: {
        mode: "image",
        referenceVideoCount: 0,
        config: {
            channelId: "channel",
            model,
            size: "1024x1024",
            quality: "low",
            videoSeconds: "",
            vquality: "",
            videoSuperResolutionEnabled: false,
            videoSuperResolutionResolution: "",
            videoSuperResolutionVersion: "",
            videoSuperResolutionFps: 0,
        },
    },
});

const quote = (amountMicrocredits: number, fingerprint: string): TaskBillingQuote => ({
    amountMicrocredits,
    perTaskAmountMicrocredits: amountMicrocredits,
    taskCount: 1,
    priceVersion: 1,
    billingMode: "fixed_request",
    pricingResolution: "1K",
    pricingInputVariant: "",
    quantity: 1,
    enhancementAmountMicrocredits: 0,
    quoteFingerprint: fingerprint,
});

type QuoteCall = {
    request: TaskBillingQuoteRequest;
    signal: AbortSignal;
    result: Deferred<TaskBillingQuote>;
};

let root: Root | null = null;

afterEach(async () => {
    if (root) await act(async () => root?.unmount());
    root = null;
    document.body.replaceChildren();
});

function QuoteProbe({ request, load }: { request: TaskBillingQuoteRequest | null; load: (request: TaskBillingQuoteRequest, signal: AbortSignal) => Promise<TaskBillingQuote> }) {
    const state = useTaskBillingQuote(request, load);
    return createElement("output", {
        "data-status": state.status,
        "data-amount": state.quote?.amountMicrocredits ?? "",
        "data-error": state.error ?? "",
    });
}

async function renderProbe(request: TaskBillingQuoteRequest | null, load: (request: TaskBillingQuoteRequest, signal: AbortSignal) => Promise<TaskBillingQuote>) {
    let host = document.querySelector<HTMLDivElement>("#quote-host");
    if (!host) {
        host = document.createElement("div");
        host.id = "quote-host";
        document.body.append(host);
        root = createRoot(host);
    }
    await act(async () => root?.render(createElement(QuoteProbe, { request, load })));
}

async function waitForQuoteDebounce() {
    await act(async () => new Promise((resolve) => setTimeout(resolve, 280)));
}

function output() {
    const element = document.querySelector<HTMLOutputElement>("output");
    if (!element) throw new Error("quote probe output is missing");
    return element;
}

describe("useTaskBillingQuote", () => {
    test("debounces rapid pricing input changes into the latest request", async () => {
        const calls: QuoteCall[] = [];
        const load = (request: TaskBillingQuoteRequest, signal: AbortSignal) => {
            const result = deferred<TaskBillingQuote>();
            calls.push({ request, signal, result });
            return result.promise;
        };

        await renderProbe(imageQuoteRequest("model-a"), load);
        await act(async () => new Promise((resolve) => setTimeout(resolve, 80)));
        await renderProbe(imageQuoteRequest("model-b"), load);
        await waitForQuoteDebounce();

        expect(calls).toHaveLength(1);
        expect(calls[0]?.request.input.config.model).toBe("model-b");
        calls[0]?.result.resolve(quote(200_000, "b"));
        await act(async () => calls[0]?.result.promise);
    });

    test("aborts the previous request and ignores its late response", async () => {
        const calls: QuoteCall[] = [];
        const load = (request: TaskBillingQuoteRequest, signal: AbortSignal) => {
            const result = deferred<TaskBillingQuote>();
            calls.push({ request, signal, result });
            return result.promise;
        };

        await renderProbe(imageQuoteRequest("model-a"), load);
        await waitForQuoteDebounce();
        expect(calls).toHaveLength(1);

        await renderProbe(imageQuoteRequest("model-b"), load);
        expect(calls[0]?.signal.aborted).toBe(true);
        expect(output().dataset.amount).toBe("");
        await waitForQuoteDebounce();
        expect(calls).toHaveLength(2);

        calls[1]?.result.resolve(quote(200_000, "b"));
        await act(async () => calls[1]?.result.promise);
        expect(output().dataset.amount).toBe("200000");

        calls[0]?.result.resolve(quote(100_000, "a"));
        await act(async () => calls[0]?.result.promise);
        expect(output().dataset.amount).toBe("200000");
    });

    test("clears the previous quote immediately and keeps it cleared when refresh fails", async () => {
        const calls: QuoteCall[] = [];
        const load = (request: TaskBillingQuoteRequest, signal: AbortSignal) => {
            const result = deferred<TaskBillingQuote>();
            calls.push({ request, signal, result });
            return result.promise;
        };

        await renderProbe(imageQuoteRequest("model-a"), load);
        await waitForQuoteDebounce();
        calls[0]?.result.resolve(quote(100_000, "a"));
        await act(async () => calls[0]?.result.promise);
        expect(output().dataset.amount).toBe("100000");

        await renderProbe(imageQuoteRequest("model-b"), load);
        expect(output().dataset.amount).toBe("");
        await waitForQuoteDebounce();
        calls[1]?.result.reject(new Error("报价服务不可用"));
        await act(async () => calls[1]?.result.promise.catch(() => undefined));

        expect(output().dataset.status).toBe("error");
        expect(output().dataset.amount).toBe("");
        expect(output().dataset.error).toBe("报价服务不可用");
    });

    test("invalidates a confirmed quote and refetches unchanged parameters", async () => {
        const calls: QuoteCall[] = [];
        const load = (request: TaskBillingQuoteRequest, signal: AbortSignal) => {
            const result = deferred<TaskBillingQuote>();
            calls.push({ request, signal, result });
            return result.promise;
        };

        await renderProbe(imageQuoteRequest("model-a"), load);
        await waitForQuoteDebounce();
        calls[0]?.result.resolve(quote(100_000, "a"));
        await act(async () => calls[0]?.result.promise);
        expect(output().dataset.amount).toBe("100000");

        await act(async () => invalidateTaskBillingQuotes("other-quote"));
        expect(output().dataset.amount).toBe("100000");
        expect(calls).toHaveLength(1);

        await act(async () => invalidateTaskBillingQuotes("a"));
        expect(output().dataset.amount).toBe("");
        await waitForQuoteDebounce();
        expect(calls).toHaveLength(2);

        calls[1]?.result.resolve(quote(200_000, "b"));
        await act(async () => calls[1]?.result.promise);
        expect(output().dataset.amount).toBe("200000");
    });
});
