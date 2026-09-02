import "./setup-happy-dom";

import { afterEach, describe, expect, mock, test } from "bun:test";
import { App, ConfigProvider } from "antd";
import { act, createElement } from "react";
import { createRoot, type Root } from "react-dom/client";
import { createMemoryRouter, RouterProvider } from "react-router";

import type { AgentDefaultModelSetting, ModelPricingOperationsSetting } from "../src/services/api/auth";
import type { ChannelModel } from "../src/services/api/wallet";

const operationsSetting: ModelPricingOperationsSetting = {
    configured: false,
    currency: "",
    creditRevenueMicros: 0,
    targetMarginBasisPoints: 0,
};

const agentSetting: AgentDefaultModelSetting = {
    configured: false,
    channelModelId: "",
    channelId: "",
    modelKey: "",
    displayName: "",
};

const agentVisionSetting: AgentDefaultModelSetting = {
    configured: true,
    channelModelId: "model-vision-1",
    channelId: "channel-kuaizi",
    modelKey: "deepseek-v4-flash-vision-exp",
    displayName: "DeepSeek Vision",
};
let rejectVisionSetting = false;

const modelFixture: ChannelModel = {
    id: "model-image-2",
    channelId: "channel-kuaizi",
    modelKey: "gpt-image-2",
    displayName: "GPT Image 2",
    marketingCopy: "",
    promotionBadge: "",
    estimatedDurationSeconds: 30,
    brandKey: "openai",
    accessPolicy: "authenticated",
    capability: "image",
    billingMode: "fixed_request",
    priceStrategy: "image_resolution",
    unitPriceMicrocredits: 100,
    priceTiers: [],
    priceConfigured: false,
    enabled: true,
    priceVersion: 1,
    createdAt: "2026-08-14T00:00:00Z",
    updatedAt: "2026-08-14T00:00:00Z",
};

const authModule = await import("../src/services/api/auth");
mock.module("@/services/api/auth", () => ({
    ...authModule,
    getAdminReferences: async () => ({ users: [], channels: [{ id: "channel-kuaizi", name: "筷子科技", interfaceType: "chat-completion", models: ["gpt-image-2"] }] }),
    listAdminModelPricings: async () => ({ pricings: [] }),
    getAdminModelPricingOperationsSetting: async () => ({ setting: operationsSetting }),
    getAdminAgentDefaultModelSetting: async () => ({ setting: agentSetting }),
    getAdminAgentVisionModelSetting: async () => {
        if (rejectVisionSetting) throw new Error("视觉配置服务暂时不可用");
        return { setting: agentVisionSetting };
    },
}));

const walletModule = await import("../src/services/api/wallet");
mock.module("@/services/api/wallet", () => ({
    ...walletModule,
    listAdminChannelModels: async () => ({ models: [modelFixture] }),
}));

const [{ default: ModelPricingPage }, { AdminProvider }] = await Promise.all([import("../src/pages/admin/model-pricing/model-pricing-page"), import("../src/pages/admin/admin-context")]);

let root: Root | null = null;

afterEach(async () => {
    if (root) await act(async () => root?.unmount());
    root = null;
    document.body.replaceChildren();
    rejectVisionSetting = false;
});

describe("admin model pricing layout", () => {
    test("uses the shared Pro data-page sequence instead of parallel pricing cards", async () => {
        await renderPricingPage();

        const dataLayout = document.querySelector(".admin-data-layout");
        expect(Array.from(dataLayout?.children ?? [], (node) => Array.from(node.classList).filter((className) => className.startsWith("admin-") || className.startsWith("model-pricing-")))).toEqual([
            ["admin-content-section", "model-pricing-agent-section"],
            ["admin-metric-band"],
            ["model-pricing-notice"],
            ["admin-content-section", "model-pricing-table-section"],
        ]);
        expect(dataLayout?.querySelector(".admin-metric-band-list")?.textContent).toContain("全部模型1");
        expect(dataLayout?.querySelector(".model-pricing-table-section .model-pricing-toolbar")).not.toBeNull();
        expect(dataLayout?.querySelector(".model-pricing-table-section .model-pricing-table")?.textContent).toContain("GPT Image 2");
        expect(dataLayout?.querySelector(".model-pricing-agent-section")?.textContent).toContain("文本决策模型");
        expect(dataLayout?.querySelector(".model-pricing-agent-section")?.textContent).toContain("视觉理解模型");
        expect(dataLayout?.querySelector('button[aria-label="保存文本 Agent 模型"]')).not.toBeNull();
        expect(dataLayout?.querySelector('button[aria-label="保存视觉理解模型"]')).not.toBeNull();
        expect(dataLayout?.querySelector('[aria-label="全站 Agent 默认视觉理解模型"]')).not.toBeNull();
        expect(document.querySelector(".model-pricing-metrics")).toBeNull();
    });

    test("keeps the text setting available when only the vision setting fails to load", async () => {
        rejectVisionSetting = true;
        await renderPricingPage();

        const section = document.querySelector(".model-pricing-agent-section");
        const textSelect = section?.querySelector<HTMLElement>('[aria-label="全站 Agent 默认模型"]');
        const visionSelect = section?.querySelector<HTMLElement>('[aria-label="全站 Agent 默认视觉理解模型"]');
        expect(textSelect).not.toBeNull();
        expect(visionSelect).not.toBeNull();
        expect(textSelect?.closest(".ant-select")?.classList.contains("ant-select-disabled")).toBe(false);
        expect(visionSelect?.closest(".ant-select")?.classList.contains("ant-select-disabled")).toBe(true);
        expect(section?.textContent).toContain("视觉配置服务暂时不可用");
        expect(section?.textContent).toContain("配置状态未知，读取成功前不可保存");
        expect(section?.textContent).not.toContain("模型商业定价加载失败");
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
    await act(async () => {
        await Promise.resolve();
        await new Promise((resolve) => setTimeout(resolve, 0));
    });
}
