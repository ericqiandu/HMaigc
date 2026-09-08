import { describe, expect, test } from "bun:test";

import { parseProductRolloutFlags } from "../src/services/api/product-rollouts";

const flags = {
    canvasToolRegistryV2: true,
    canvasMediaToolsV2: false,
    canvasProductionNodesV2: false,
    creativeWorkspaceV2: false,
    skillLibraryV2: false,
};

describe("product rollout contract", () => {
    test("accepts only the complete five-flag response", () => {
        expect(parseProductRolloutFlags(flags)).toEqual(flags);
    });

    test("fails explicitly for missing, unknown, or non-boolean fields", () => {
        const { canvasToolRegistryV2: _omitted, ...missingFlag } = flags;
        expect(() => parseProductRolloutFlags(missingFlag)).toThrow("产品能力配置缺少字段");
        expect(() => parseProductRolloutFlags({ ...flags, fallback: true })).toThrow("包含未知字段: fallback");
        expect(() => parseProductRolloutFlags({ ...flags, canvasToolRegistryV2: 1 })).toThrow("canvasToolRegistryV2 必须是布尔值");
    });
});
