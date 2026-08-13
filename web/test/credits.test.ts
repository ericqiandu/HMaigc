import { describe, expect, test } from "bun:test";

import { requestCreditCost, type ModelCreditCost } from "../src/constant/credits";

const perSecondVideoCost: ModelCreditCost = {
    model: "MiniMax-H3",
    billingMode: "per_second",
    priceStrategy: "video_resolution",
    unitPriceMicrocredits: 0,
    priceTiers: [
        { resolution: "768P", inputVariant: "standard", unitPriceMicrocredits: 500_000 },
        { resolution: "2K", inputVariant: "standard", unitPriceMicrocredits: 800_000 },
        { resolution: "4K", inputVariant: "standard", unitPriceMicrocredits: 1_200_000 },
        { resolution: "720P", inputVariant: "standard", unitPriceMicrocredits: 1_510_000 },
        { resolution: "720P", inputVariant: "reference_video", unitPriceMicrocredits: 1_630_000 },
    ],
};

describe("requestCreditCost", () => {
    test("按秒视频同时计入时长和输出数量", () => {
        expect(requestCreditCost({ channelMode: "remote", modelCosts: [perSecondVideoCost], model: "MiniMax-H3", seconds: 6, count: 2, resolution: "768p" })).toBe(6);
    });

    test("2K 分辨率匹配对应阶梯价格", () => {
        expect(requestCreditCost({ channelMode: "remote", modelCosts: [perSecondVideoCost], model: "MiniMax-H3", seconds: 5, count: 1, resolution: "2k" })).toBe(4);
    });

    test("4K 分辨率匹配对应阶梯价格", () => {
        expect(requestCreditCost({ channelMode: "remote", modelCosts: [perSecondVideoCost], model: "MiniMax-H3", seconds: 5, count: 1, resolution: "4K" })).toBe(6);
    });

    test("参考视频使用独立价格档位", () => {
        expect(requestCreditCost({ channelMode: "remote", modelCosts: [perSecondVideoCost], model: "MiniMax-H3", seconds: 5, count: 1, resolution: "720p", referenceVideoCount: 1 })).toBeCloseTo(8.15);
    });
});
