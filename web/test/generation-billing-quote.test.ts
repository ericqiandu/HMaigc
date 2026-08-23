import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";

import { buildTaskBillingQuoteRequest, prepareGenerationTaskSubmission, taskBillingQuoteMatches, taskPriceChangedQuoteFromEnvelope } from "../src/lib/billing/task-billing-quote";
import type { CreateTaskInput, TaskBillingQuote } from "../src/services/api/task-center";

const providerConfig = {
    channelId: "kuaizi",
    model: "seedance2.0-mini",
    size: "1024x1024",
    quality: "low",
    videoSeconds: "6",
    vquality: "720p",
    videoGenerateAudio: "true",
    videoSuperResolutionEnabled: "true",
    videoSuperResolutionResolution: "1080p",
    videoSuperResolutionVersion: "v1",
    videoSuperResolutionFps: "30",
};

const currentQuote: TaskBillingQuote = {
    amountMicrocredits: 12_000_000,
    perTaskAmountMicrocredits: 3_000_000,
    taskCount: 4,
    priceVersion: 7,
    billingMode: "per_second",
    pricingResolution: "720P",
    pricingInputVariant: "reference_video",
    quantity: 24,
    enhancementAmountMicrocredits: 0,
    usageAdjustment: {
        metric: "input_image",
        actualQuantity: 3,
        includedQuantity: 1,
        billableQuantity: 2,
        unitPriceMicrocredits: 4_000,
        perTaskAmountMicrocredits: 8_000,
        amountMicrocredits: 32_000,
    },
    quoteFingerprint: "quote-v7",
};

test("generation quote compares frozen facts without using displayed amounts", () => {
    expect(taskBillingQuoteMatches({ priceVersion: 3, quoteFingerprint: "same" }, { priceVersion: 3, quoteFingerprint: "same" })).toBe(true);
    expect(taskBillingQuoteMatches({ priceVersion: 3, quoteFingerprint: "old" }, { priceVersion: 3, quoteFingerprint: "new" })).toBe(false);
});

describe("generation billing quote contract", () => {
    test("recomputes the canvas quote when generated audio changes", () => {
        const source = readFileSync(new URL("../src/hooks/use-canvas-task-billing-quote.ts", import.meta.url), "utf8");
        expect(source).toContain("config.videoGenerateAudio,");
    });

    test("builds an exact video request without a frontend price formula", () => {
        expect(buildTaskBillingQuoteRequest({ projectId: "canvas-project", mode: "video", operation: "extend", batchCount: 4, usage: { referenceImageCount: 0, referenceVideoCount: 2 }, config: providerConfig })).toEqual({
            projectId: "canvas-project",
            type: "canvas_video",
            operation: "extend",
            batchCount: 4,
            input: {
                mode: "video",
                referenceImageCount: 0,
                referenceVideoCount: 2,
                config: {
                    channelId: "kuaizi",
                    model: "seedance2.0-mini",
                    size: "1024x1024",
                    quality: "low",
                    videoSeconds: "6",
                    vquality: "720p",
                    videoGenerateAudio: true,
                    videoSuperResolutionEnabled: true,
                    videoSuperResolutionResolution: "1080p",
                    videoSuperResolutionVersion: "v1",
                    videoSuperResolutionFps: 30,
                },
            },
        });
    });

    test("sends the real reference-image count for image quotes", async () => {
        const input: CreateTaskInput = {
            projectId: "project",
            type: "canvas_image",
            operation: "image",
            prompt: "prompt",
            input: {
                mode: "image",
                config: providerConfig,
                referenceImages: [{ id: "one" }, { id: "two" }, { id: "three" }],
            },
        };
        const requested: unknown[] = [];

        await prepareGenerationTaskSubmission(input, undefined, async (request) => {
            requested.push(request);
            return { ...currentQuote, amountMicrocredits: currentQuote.perTaskAmountMicrocredits, taskCount: 1 };
        });

        expect(requested[0]).toMatchObject({
            batchCount: 1,
            input: { referenceImageCount: 3, referenceVideoCount: 0 },
        });
    });

    test("quotes media task creation and submits the confirmed single-task fingerprint", async () => {
        const input: CreateTaskInput = {
            projectId: "project",
            type: "canvas_video",
            operation: "extend",
            prompt: "prompt",
            input: { mode: "video", config: providerConfig, referenceVideos: [{ id: "reference" }] },
        };
        const requested: unknown[] = [];

        const submission = await prepareGenerationTaskSubmission(input, undefined, async (request) => {
            requested.push(request);
            return { ...currentQuote, amountMicrocredits: currentQuote.perTaskAmountMicrocredits, taskCount: 1 };
        });

        expect(requested).toHaveLength(1);
        expect(requested[0]).toMatchObject({ batchCount: 1, input: { referenceVideoCount: 1 } });
        expect(submission.quotePriceVersion).toBe(7);
        expect(submission.quoteFingerprint).toBe("quote-v7");
    });

    test("submits the displayed confirmation without a duplicate preflight quote", async () => {
        const input: CreateTaskInput = { projectId: "project", type: "canvas_image", operation: "image", prompt: "prompt", input: { mode: "image", config: providerConfig } };
        let quoteCalls = 0;

        const submission = await prepareGenerationTaskSubmission(input, { ...currentQuote, priceVersion: 6, quoteFingerprint: "quote-v6" }, async () => {
            quoteCalls += 1;
            return currentQuote;
        });

        expect(quoteCalls).toBe(0);
        expect(submission.quotePriceVersion).toBe(6);
        expect(submission.quoteFingerprint).toBe("quote-v6");
    });

    test("does not request a media quote for text tasks", async () => {
        let quoteCalls = 0;
        const input: CreateTaskInput = { type: "canvas_text", operation: "text", prompt: "prompt", input: { mode: "text" } };

        const submission = await prepareGenerationTaskSubmission(input, undefined, async () => {
            quoteCalls += 1;
            return currentQuote;
        });

        expect(quoteCalls).toBe(0);
        expect(submission).toEqual(input);
    });

    test("parses the backend price-changed contract for quote invalidation", () => {
        expect(
            taskPriceChangedQuoteFromEnvelope({
                code: 409,
                data: { errorCode: "PRICE_CHANGED", currentQuote },
                msg: "预计积分已变化，请确认新报价后重试",
            }),
        ).toEqual(currentQuote);
        expect(taskPriceChangedQuoteFromEnvelope({ code: 409, data: { errorCode: "PRICE_CHANGED", currentQuote: { priceVersion: 7 } } })).toBeNull();
    });
});
