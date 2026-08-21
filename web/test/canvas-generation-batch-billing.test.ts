import { describe, expect, test } from "bun:test";

import { currentGenerationTargets, quoteGenerationBatch } from "../src/lib/billing/canvas-generation-batch-billing";
import type { TaskBillingQuote, TaskBillingQuoteRequest } from "../src/services/api/task-center";

const imageRequest: TaskBillingQuoteRequest = {
    projectId: "canvas-project",
    type: "canvas_image",
    operation: "image",
    batchCount: 1,
    input: {
        mode: "image",
        referenceVideoCount: 0,
        config: {
            channelId: "kuaizi",
            model: "gpt-image-2",
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
};

function quote(request: TaskBillingQuoteRequest, fingerprint: string): TaskBillingQuote {
    return {
        amountMicrocredits: request.batchCount * 1_000_000,
        perTaskAmountMicrocredits: 1_000_000,
        taskCount: request.batchCount,
        priceVersion: 3,
        billingMode: "fixed_request",
        pricingResolution: "1K",
        pricingInputVariant: "none",
        quantity: 1,
        enhancementAmountMicrocredits: 0,
        quoteFingerprint: fingerprint,
    };
}

describe("canvas generation batch billing", () => {
    test("quotes the latest canvas node facts instead of stale pre-update objects", () => {
        const stale = [{ id: "video-1", operation: "text_to_video" }];
        const current = [{ id: "video-1", operation: "image_to_video" }];

        expect(currentGenerationTargets(stale, current)).toEqual(current);
    });

    test("fails closed when a requested target no longer exists on the canvas", () => {
        expect(() => currentGenerationTargets(
            [{ id: "deleted-image", operation: "image" }],
            [],
        )).toThrow("生成目标 deleted-image 已不存在");
    });

    test("groups identical task facts into one exact backend quote and maps the confirmation to every target", async () => {
        const requests: TaskBillingQuoteRequest[] = [];
        const result = await quoteGenerationBatch([
            { targetId: "image-1", request: imageRequest },
            { targetId: "image-2", request: imageRequest },
        ], async (request) => {
            requests.push(request);
            return quote(request, "image-price-v3");
        });

        expect(requests).toHaveLength(1);
        expect(requests[0].batchCount).toBe(2);
        expect(result.amountMicrocredits).toBe(2_000_000);
        expect(result.confirmations).toEqual([
            { targetId: "image-1", priceVersion: 3, quoteFingerprint: "image-price-v3" },
            { targetId: "image-2", priceVersion: 3, quoteFingerprint: "image-price-v3" },
        ]);
    });

    test("splits identical quote facts at the backend batch limit without losing confirmations", async () => {
        const requests: TaskBillingQuoteRequest[] = [];
        const targets = Array.from({ length: 16 }, (_, index) => ({
            targetId: `image-${index + 1}`,
            request: imageRequest,
        }));

        const result = await quoteGenerationBatch(targets, async (request) => {
            requests.push(request);
            return quote(request, `image-price-v3-${requests.length}`);
        });

        expect(requests.map((request) => request.batchCount)).toEqual([15, 1]);
        expect(result.amountMicrocredits).toBe(16_000_000);
        expect(result.confirmations).toHaveLength(16);
        expect(result.confirmations.map((item) => item.targetId)).toEqual(targets.map((item) => item.targetId));
    });

    test("keeps different duration tiers as separate quote facts and sums backend amounts", async () => {
        const video6 = { ...imageRequest, type: "canvas_video" as const, operation: "image_to_video", input: { ...imageRequest.input, mode: "video" as const, config: { ...imageRequest.input.config, videoSeconds: "6", vquality: "720p" } } };
        const video10 = { ...video6, input: { ...video6.input, config: { ...video6.input.config, videoSeconds: "10" } } };
        const requests: TaskBillingQuoteRequest[] = [];

        const result = await quoteGenerationBatch([
            { targetId: "video-6", request: video6 },
            { targetId: "video-10", request: video10 },
        ], async (request) => {
            requests.push(request);
            const amount = request.input.config.videoSeconds === "6" ? 3_000_000 : 5_000_000;
            return { ...quote(request, `video-${request.input.config.videoSeconds}`), amountMicrocredits: amount, perTaskAmountMicrocredits: amount };
        });

        expect(requests).toHaveLength(2);
        expect(result.amountMicrocredits).toBe(8_000_000);
        expect(result.confirmations.map((item) => item.quoteFingerprint)).toEqual(["video-6", "video-10"]);
    });

    test("fails closed when any required quote cannot be loaded", async () => {
        await expect(quoteGenerationBatch([{ targetId: "image-1", request: imageRequest }], async () => {
            throw new Error("价格档位未配置");
        })).rejects.toThrow("价格档位未配置");
    });
});
