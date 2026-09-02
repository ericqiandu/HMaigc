import { describe, expect, test } from "bun:test";

import { normalizedPricingTierKey, specificationsForModel } from "../src/pages/admin/model-pricing/pricing-specifications";

describe("Image 2 pricing specifications", () => {
    test("publishes the complete resolution and quality matrix", () => {
        const specifications = specificationsForModel({
            modelKey: "kz_gpt_image2",
            priceStrategy: "image_resolution",
            providerCapabilities: {
                resolutions: ["1K", "2K", "4K"],
                qualities: ["low", "medium", "high"],
                inputVariants: [],
                referenceVideoResolutions: [],
                generatedAudioResolutions: [],
            },
        });

        expect(specifications).toHaveLength(9);
        expect(specifications.map((specification) => specification.key)).toEqual([
            "1K::low",
            "1K::medium",
            "1K::high",
            "2K::low",
            "2K::medium",
            "2K::high",
            "4K::low",
            "4K::medium",
            "4K::high",
        ]);
        expect(specifications.map((specification) => specification.label)).toContain("2K · 高画质");
        expect(normalizedPricingTierKey("2K", "HIGH")).toBe("2k::high");
    });
});
