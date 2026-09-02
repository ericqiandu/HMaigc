import "./setup-happy-dom";

import { afterEach, describe, expect, mock, test } from "bun:test";
import { App, ConfigProvider } from "antd";
import { act, createElement } from "react";
import { createRoot, type Root } from "react-dom/client";
import { createMemoryRouter, RouterProvider } from "react-router";

import type { AgentDefaultModelSetting, ModelPricing, ModelPricingOperationsSetting } from "../src/services/api/auth";
import type { ChannelModel } from "../src/services/api/wallet";

const channelId = "channel-kuaizi";
const operationsSetting: ModelPricingOperationsSetting = {
    configured: true,
    currency: "CNY",
    creditRevenueMicros: 10_000,
    targetMarginBasisPoints: 3_000,
};
const agentSetting: AgentDefaultModelSetting = {
    configured: false,
    channelModelId: "",
    channelId: "",
    modelKey: "",
    displayName: "",
};

const seedance25 = videoModel({
    id: "seedance-25",
    modelKey: "doubao-seedance-2-5-260628",
    displayName: "Seedance 2.5",
    priceStrategy: "video_resolution",
    billingMode: "per_second",
    resolutions: ["480p", "720p", "1080p"],
    priceTiers: [
        priceTier("480p", "standard", 67_000_000),
        priceTier("480p", "standard_audio", 67_000_000),
        priceTier("720p", "standard", 151_000_000),
        priceTier("720p", "standard_audio", 151_000_000),
        priceTier("1080p", "standard", 374_000_000),
        priceTier("1080p", "standard_audio", 374_000_000),
    ],
});

const seedanceFast = videoModel({
    id: "seedance-fast",
    modelKey: "doubao-seedance-2-0-fast-260128",
    displayName: "Seedance 2.0 Fast",
    priceStrategy: "flat",
    billingMode: "fixed_request",
    resolutions: ["480p", "720p"],
    priceTiers: [],
});

const deepSeekVision: ChannelModel = {
    id: "deepseek-vision",
    channelId,
    modelKey: "deepseek-vl2",
    displayName: "DeepSeek Vision",
    marketingCopy: "",
    promotionBadge: "",
    estimatedDurationSeconds: 5,
    brandKey: "deepseek",
    accessPolicy: "authenticated",
    capability: "vision",
    billingMode: "token_usage",
    priceStrategy: "token",
    unitPriceMicrocredits: 0,
    priceTiers: [],
    priceConfigured: true,
    enabled: true,
    priceVersion: 1,
    providerCapabilities: {
        resolutions: [],
        qualities: [],
        inputVariants: [],
        referenceVideoResolutions: [],
        generatedAudioResolutions: [],
        supportsTokenUsageBilling: true,
    },
    createdAt: "2026-08-25T00:00:00Z",
    updatedAt: "2026-08-25T00:00:00Z",
};

const seedance25Pricing = pricingFixture(seedance25, [
    ["480p::standard", 670_000],
    ["480p::standard_audio", 670_000],
    ["720p::standard", 1_510_000],
    ["720p::standard_audio", 1_510_000],
    ["1080p::standard", 3_740_000],
    ["1080p::standard_audio", 3_740_000],
]);

const deepSeekVisionPricing: ModelPricing = {
    id: "pricing-deepseek-vision",
    channelId,
    model: deepSeekVision.modelKey,
    capability: "vision",
    currency: "CNY",
    inputPerMillionMicros: 1_000_000,
    outputPerMillionMicros: 2_000_000,
    cachedPerMillionMicros: 100_000,
    expectedInputTokens: 2_000,
    expectedOutputTokens: 500,
    expectedCachedTokens: 0,
    maxOutputTokens: 8_192,
    perRequestMicros: 0,
    perMediaMicros: 0,
    perVideoSecondMicros: 0,
    tiers: [],
    createdAt: "2026-08-25T00:00:00Z",
    updatedAt: "2026-08-25T00:00:00Z",
};

const authModule = await import("../src/services/api/auth");
mock.module("@/services/api/auth", () => ({
    ...authModule,
    getAdminReferences: async () => ({ users: [], channels: [{ id: channelId, name: "筷子科技", interfaceType: "ai-open-platform-video-volcengine", models: [seedance25.modelKey, seedanceFast.modelKey, deepSeekVision.modelKey] }] }),
    listAdminModelPricings: async () => ({ pricings: [seedance25Pricing, deepSeekVisionPricing] }),
    getAdminModelPricingOperationsSetting: async () => ({ setting: operationsSetting }),
    getAdminAgentDefaultModelSetting: async () => ({ setting: agentSetting }),
    getAdminAgentVisionModelSetting: async () => ({ setting: agentSetting }),
}));

const walletModule = await import("../src/services/api/wallet");
mock.module("@/services/api/wallet", () => ({
    ...walletModule,
    listAdminChannelModels: async () => ({ models: [seedance25, seedanceFast, deepSeekVision] }),
}));

const [{ default: ModelPricingPage }, { AdminProvider }] = await Promise.all([import("../src/pages/admin/model-pricing/model-pricing-page"), import("../src/pages/admin/admin-context")]);

let root: Root | null = null;

afterEach(async () => {
    if (root) await act(async () => root?.unmount());
    root = null;
    document.body.replaceChildren();
});

describe("admin model pricing form state", () => {
    test("opening another model clears the previous model's tier values", async () => {
        await renderPricingPage();

        await clickButton("配置 Seedance 2.5");
        expect(activeDrawer().textContent).toContain("1080p · 普通生成（无声）");
        await clickDrawerClose();

        await clickButton("配置 Seedance 2.0 Fast");
        await clickSegment("按分辨率");

        const drawer = activeDrawer();
        expect(drawer.textContent).not.toContain("1080p");
        expect(Array.from(drawer.querySelectorAll<HTMLInputElement>(".model-pricing-resolution-row input"), (input) => input.value)).toEqual(Array<string>(12).fill(""));
    });

    test("vision pricing uses token fields instead of per-image pricing", async () => {
        await renderPricingPage();

        await clickButton("配置 DeepSeek Vision");

        const drawerText = activeDrawer().textContent ?? "";
        expect(drawerText).toContain("输入成本 / 百万 Token");
        expect(drawerText).toContain("输出成本 / 百万 Token");
        expect(drawerText).toContain("最大输出 Token");
        expect(drawerText).not.toContain("供应商成本 / 张");
    });
});

async function renderPricingPage() {
    const host = document.createElement("div");
    host.className = "admin-workspace";
    document.body.append(host);
    root = createRoot(host);
    const router = createMemoryRouter([{ path: "/", element: createElement(ModelPricingPage) }], { initialEntries: ["/"] });
    await act(async () => {
        root?.render(createElement(ConfigProvider, null, createElement(App, null, createElement(AdminProvider, null, createElement(RouterProvider, { router })))));
    });
    await flushUpdates();
}

async function clickButton(label: string) {
    const button = document.querySelector<HTMLButtonElement>(`button[aria-label="${label}"]`);
    expect(button).not.toBeNull();
    await act(async () => button?.click());
    await flushUpdates();
}

async function clickDrawerClose() {
    const button = activeDrawer().querySelector<HTMLButtonElement>(".ant-drawer-close");
    expect(button).not.toBeNull();
    await act(async () => button?.click());
    await flushUpdates();
}

async function clickSegment(label: string) {
    const segment = Array.from(activeDrawer().querySelectorAll<HTMLElement>(".ant-segmented-item-label")).find((candidate) => candidate.textContent === label);
    expect(segment).not.toBeUndefined();
    await act(async () => segment?.click());
    await flushUpdates();
}

function activeDrawer() {
    const drawer = document.querySelector<HTMLElement>(".model-pricing-drawer");
    if (!drawer) throw new Error("未找到已打开的模型定价抽屉");
    return drawer;
}

async function flushUpdates() {
    await act(async () => {
        await Promise.resolve();
        await new Promise((resolve) => setTimeout(resolve, 0));
    });
}

function videoModel(input: { id: string; modelKey: string; displayName: string; priceStrategy: ChannelModel["priceStrategy"]; billingMode: ChannelModel["billingMode"]; resolutions: string[]; priceTiers: ChannelModel["priceTiers"] }): ChannelModel {
    return {
        id: input.id,
        channelId,
        modelKey: input.modelKey,
        displayName: input.displayName,
        marketingCopy: "",
        promotionBadge: "",
        estimatedDurationSeconds: 30,
        brandKey: "seedance",
        accessPolicy: "authenticated",
        capability: "video",
        billingMode: input.billingMode,
        priceStrategy: input.priceStrategy,
        unitPriceMicrocredits: input.priceStrategy === "flat" ? 1_000_000 : 0,
        priceTiers: input.priceTiers,
        priceConfigured: true,
        enabled: true,
        priceVersion: 1,
        providerCapabilities: {
            resolutions: input.resolutions,
            qualities: [],
            inputVariants: ["standard", "standard_audio", "reference_video"],
            referenceVideoResolutions: input.resolutions,
            generatedAudioResolutions: [],
        },
        createdAt: "2026-08-25T00:00:00Z",
        updatedAt: "2026-08-25T00:00:00Z",
    };
}

function priceTier(resolution: string, inputVariant: "standard" | "standard_audio" | "reference_video", unitPriceMicrocredits: number): ChannelModel["priceTiers"][number] {
    return { id: `${resolution}-${inputVariant}`, resolution, inputVariant, usageMetric: "", includedQuantity: 0, unitPriceMicrocredits, priceVersion: 1 };
}

function pricingFixture(model: ChannelModel, tiers: Array<[string, number]>): ModelPricing {
    return {
        id: `pricing-${model.id}`,
        channelId,
        model: model.modelKey,
        capability: "video",
        currency: "CNY",
        inputPerMillionMicros: 0,
        outputPerMillionMicros: 0,
        cachedPerMillionMicros: 0,
        expectedInputTokens: 0,
        expectedOutputTokens: 0,
        expectedCachedTokens: 0,
        maxOutputTokens: 0,
        perRequestMicros: 0,
        perMediaMicros: 0,
        perVideoSecondMicros: 0,
        tiers: tiers.map(([specification, supplierCostMicros]) => ({
            id: specification,
            modelPricingId: `pricing-${model.id}`,
            specification,
            usageMetric: "",
            includedQuantity: 0,
            supplierCostMicros,
            createdAt: "2026-08-25T00:00:00Z",
            updatedAt: "2026-08-25T00:00:00Z",
        })),
        createdAt: "2026-08-25T00:00:00Z",
        updatedAt: "2026-08-25T00:00:00Z",
    };
}
