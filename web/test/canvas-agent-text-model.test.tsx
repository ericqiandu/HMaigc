import { beforeAll, describe, expect, test } from "bun:test";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";

import { defaultConfig, encodeChannelModel, type AiConfig, type ModelChannel } from "../src/stores/use-config-store";

let CanvasAgentModelMenu: (typeof import("../src/components/canvas/canvas-agent-model-menu"))["CanvasAgentModelMenu"];
let CanvasAgentSelectionSummary: (typeof import("../src/components/canvas/canvas-agent-selection-summary"))["CanvasAgentSelectionSummary"];
let resolveAgentDefaultRequestConfig: (typeof import("../src/components/canvas/canvas-agent-default-model"))["resolveAgentDefaultRequestConfig"];

beforeAll(async () => {
    ({ CanvasAgentModelMenu } = await import("../src/components/canvas/canvas-agent-model-menu"));
    ({ CanvasAgentSelectionSummary } = await import("../src/components/canvas/canvas-agent-selection-summary"));
    ({ resolveAgentDefaultRequestConfig } = await import("../src/components/canvas/canvas-agent-default-model"));
});

const textChannel: ModelChannel = {
    id: "kuaizi-chat-gpt",
    name: "筷子 GPT Agent",
    baseUrl: "/api/ai/system/kuaizi-chat-gpt",
    apiKey: "system",
    apiFormat: "openai",
    interfaceType: "chat-completion",
    models: ["gpt-5.5"],
    scope: "system",
    enabled: true,
    modelCosts: [
        {
            model: "gpt-5.5",
            displayName: "GPT 5.5",
            marketingCopy: "支持图片理解与 Agent 工具调用",
            promotionBadge: "",
            estimatedDurationSeconds: 0,
            brandKey: "openai",
            accessPolicy: "authenticated",
            accessible: true,
            capability: "text",
            billingMode: "fixed_request",
            priceStrategy: "flat",
            unitPriceMicrocredits: 10,
            priceTiers: [],
        },
    ],
};
const agentDefaultModel = encodeChannelModel(textChannel.id, "gpt-5.5");
const config: AiConfig = { ...defaultConfig, channels: [textChannel], models: [agentDefaultModel], textModels: [agentDefaultModel], textModel: "legacy-local-selection" };

describe("canvas Agent default model", () => {
    test("model picker contains only image and video capabilities", () => {
        const markup = renderToStaticMarkup(createElement(CanvasAgentModelMenu, { config, value: { image: "", video: "" }, onChange: () => undefined }));
        expect(markup).not.toContain("Agent");
        expect(markup).toContain("图片");
        expect(markup).toContain("视频");
    });

    test("selection summary never displays an Agent text model", () => {
        const markup = renderToStaticMarkup(
            createElement(CanvasAgentSelectionSummary, {
                config,
                models: { image: "", video: "" },
                selectedSkills: [],
                onModelsChange: () => undefined,
                onSkillsChange: () => undefined,
            }),
        );
        expect(markup).toBe("");
    });

    test("request uses only the backend Agent default and rejects missing configuration", () => {
        const resolved = resolveAgentDefaultRequestConfig(config, agentDefaultModel);
        expect(resolved.model).toBe("gpt-5.5");
        expect(resolved.baseUrl).toBe(textChannel.baseUrl);
        expect(() => resolveAgentDefaultRequestConfig(config, "")).toThrow("管理员尚未配置可用的 Agent 模型");
        expect(() => resolveAgentDefaultRequestConfig(config, "legacy-local-selection")).toThrow("管理员尚未配置可用的 Agent 模型");
    });
});
