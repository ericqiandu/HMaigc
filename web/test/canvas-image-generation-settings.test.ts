import { describe, expect, test } from "bun:test";

import { buildImageDimensions, imageModelMetadataPatch, imageModelSupportsBatch } from "../src/components/canvas/canvas-image-generation-settings";

const ratios = ["1:1", "1:2", "2:1", "9:16", "16:9", "3:4", "4:3", "3:2", "2:3", "5:4", "4:5", "21:9", "9:21"] as const;

describe("GPT Image 2 画布尺寸", () => {
    test("全部比例和清晰度都满足 16 倍数及上游像素边界", () => {
        for (const ratio of ratios) {
            for (const resolution of ["1K", "2K", "4K"] as const) {
                const [width, height] = buildImageDimensions(ratio, resolution).split("x").map(Number);
                expect(width % 16).toBe(0);
                expect(height % 16).toBe(0);
                expect(Math.max(width, height)).toBeLessThanOrEqual(3840);
                expect(width * height).toBeGreaterThanOrEqual(655_360);
                expect(width * height).toBeLessThanOrEqual(8_294_400);
            }
        }
    });

    test("GPT Image 2 不展示上游不支持的批量张数入口", () => {
        expect(imageModelSupportsBatch("kz_gpt_image2")).toBe(false);
        expect(imageModelSupportsBatch("kuaizi-channel::kz_gpt_image2")).toBe(false);
        expect(imageModelSupportsBatch("gpt-image-1")).toBe(true);
        expect(imageModelMetadataPatch("kuaizi-channel::kz_gpt_image2")).toEqual({ model: "kuaizi-channel::kz_gpt_image2", count: 1 });
        expect(imageModelMetadataPatch("gpt-image-1")).toEqual({ model: "gpt-image-1" });
    });
});
