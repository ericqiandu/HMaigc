import { beforeAll, describe, expect, test } from "bun:test";
import { Window } from "happy-dom";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";

import { defaultConfig, encodeChannelModel, type AiConfig, type ModelChannel } from "../src/stores/use-config-store";

let CanvasAgentModelMenu: (typeof import("../src/components/canvas/canvas-agent-model-menu"))["CanvasAgentModelMenu"];
let CanvasAgentSelectionSummary: (typeof import("../src/components/canvas/canvas-agent-selection-summary"))["CanvasAgentSelectionSummary"];
let removeLastCanvasAgentSelection: (typeof import("../src/components/canvas/canvas-agent-selection-summary"))["removeLastCanvasAgentSelection"];

beforeAll(async () => {
    const browserWindow = new Window({ url: "http://localhost/canvas/test" });
    const globals: Record<string, unknown> = {
        window: browserWindow,
        document: browserWindow.document,
        navigator: browserWindow.navigator,
        HTMLElement: browserWindow.HTMLElement,
        Element: browserWindow.Element,
        Node: browserWindow.Node,
        ShadowRoot: browserWindow.ShadowRoot,
        SVGElement: browserWindow.SVGElement,
        getComputedStyle: browserWindow.getComputedStyle.bind(browserWindow),
    };
    for (const [name, value] of Object.entries(globals)) {
        Object.defineProperty(globalThis, name, { configurable: true, writable: true, value });
    }
    ({ CanvasAgentModelMenu } = await import("../src/components/canvas/canvas-agent-model-menu"));
    ({ CanvasAgentSelectionSummary, removeLastCanvasAgentSelection } = await import("../src/components/canvas/canvas-agent-selection-summary"));
});

const textChannels: ModelChannel[] = [
    {
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
    },
    {
        id: "kuaizi-chat-deepseek",
        name: "筷子 DeepSeek Agent",
        baseUrl: "/api/ai/system/kuaizi-chat-deepseek",
        apiKey: "system",
        apiFormat: "openai",
        interfaceType: "chat-completion",
        models: ["deepseek-v4-pro"],
        scope: "system",
        enabled: true,
        modelCosts: [
            {
                model: "deepseek-v4-pro",
                displayName: "DeepSeek V4 Pro",
                marketingCopy: "纯文本 Agent 模型，不支持图片输入",
                promotionBadge: "",
                estimatedDurationSeconds: 0,
                brandKey: "deepseek",
                accessPolicy: "authenticated",
                accessible: true,
                capability: "text",
                billingMode: "fixed_request",
                priceStrategy: "flat",
                unitPriceMicrocredits: 8,
                priceTiers: [],
            },
        ],
    },
];

const gptValue = encodeChannelModel("kuaizi-chat-gpt", "gpt-5.5");
const deepseekValue = encodeChannelModel("kuaizi-chat-deepseek", "deepseek-v4-pro");
const config: AiConfig = { ...defaultConfig, channels: textChannels, models: [gptValue, deepseekValue], textModels: [gptValue, deepseekValue] };

describe("canvas Agent text model selection", () => {
    test("renders the dynamic GPT and DeepSeek choices only when Agent selection is enabled", () => {
        const markup = renderToStaticMarkup(
            createElement(CanvasAgentModelMenu, {
                config,
                value: { image: "", video: "" },
                agentModel: gptValue,
                showAgentModels: true,
                onChange: () => undefined,
                onAgentModelChange: () => undefined,
            }),
        );
        expect(markup).toContain("Agent");
        expect(markup).toContain("GPT 5.5");
        expect(markup).toContain("DeepSeek V4 Pro");
        expect(markup).toContain("支持图片理解与 Agent 工具调用");
        expect(markup).toContain("纯文本 Agent 模型，不支持图片输入");
    });

    test("shows and removes the Agent model independently from generation models", () => {
        const markup = renderToStaticMarkup(
            createElement(CanvasAgentSelectionSummary, {
                config,
                models: { image: "", video: "" },
                agentModel: deepseekValue,
                selectedSkills: [],
                onModelsChange: () => undefined,
                onAgentModelChange: () => undefined,
                onSkillsChange: () => undefined,
            }),
        );
        expect(markup).toContain("DeepSeek V4 Pro");
        expect(markup).toContain("移除 Agent 模型 DeepSeek V4 Pro");
        expect(removeLastCanvasAgentSelection({ models: { image: "", video: "" }, agentModel: deepseekValue, selectedSkills: [] })).toEqual({
            models: { image: "", video: "" },
            agentModel: "",
            selectedSkills: [],
        });
    });
});
