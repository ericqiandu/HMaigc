import { describe, expect, it } from "vitest";

import { buildInputImageUsagePricing, readInputImageUsagePricing } from "@/pages/admin/model-pricing/media-input-usage-pricing";

describe("media input usage pricing", () => {
    it("keeps the adjustment disabled when every field is empty", () => {
        expect(
            buildInputImageUsagePricing({
                includedQuantity: undefined,
                supplierUnitCostMicros: undefined,
                userUnitPriceMicrocredits: undefined,
            }),
        ).toBeNull();
    });

    it("builds paired supplier and user input-image tiers", () => {
        expect(
            buildInputImageUsagePricing({
                includedQuantity: 1,
                supplierUnitCostMicros: 20_000,
                userUnitPriceMicrocredits: 4_000,
            }),
        ).toEqual({
            supplierTier: {
                specification: "input_image",
                usageMetric: "input_image",
                includedQuantity: 1,
                supplierCostMicros: 20_000,
            },
            userTier: {
                resolution: "",
                inputVariant: "",
                usageMetric: "input_image",
                includedQuantity: 1,
                unitPriceMicrocredits: 4_000,
            },
        });
    });

    it("rejects partially configured adjustments", () => {
        expect(() =>
            buildInputImageUsagePricing({
                includedQuantity: 1,
                supplierUnitCostMicros: 20_000,
                userUnitPriceMicrocredits: undefined,
            }),
        ).toThrow("参考图附加价必须同时配置");
    });

    it("rejects invalid quantities and prices", () => {
        expect(() =>
            buildInputImageUsagePricing({
                includedQuantity: -1,
                supplierUnitCostMicros: 20_000,
                userUnitPriceMicrocredits: 4_000,
            }),
        ).toThrow("免费参考图数量");
        expect(() =>
            buildInputImageUsagePricing({
                includedQuantity: 1,
                supplierUnitCostMicros: 0,
                userUnitPriceMicrocredits: 4_000,
            }),
        ).toThrow("参考图供应商成本");
    });

    it("reads a matching persisted pair and exposes incomplete facts for repair", () => {
        const supplierTiers = [
            {
                specification: "input_image",
                usageMetric: "input_image",
                includedQuantity: 1,
                supplierCostMicros: 20_000,
            },
        ];
        const userTiers = [
            {
                resolution: "",
                inputVariant: "" as const,
                usageMetric: "input_image",
                includedQuantity: 1,
                unitPriceMicrocredits: 4_000,
            },
        ];

        expect(readInputImageUsagePricing(supplierTiers, userTiers)).toEqual({
            state: "complete",
            includedQuantity: 1,
            supplierUnitCostMicros: 20_000,
            userUnitPriceMicrocredits: 4_000,
        });
        expect(readInputImageUsagePricing(supplierTiers, [])).toEqual({
            state: "incomplete",
            includedQuantity: 1,
            supplierUnitCostMicros: 20_000,
            userUnitPriceMicrocredits: undefined,
        });
    });
});
