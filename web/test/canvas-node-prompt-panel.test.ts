import "./setup-happy-dom";

import { afterEach, describe, expect, test } from "bun:test";
import { act, createElement, createRef, useState } from "react";
import { createRoot, type Root } from "react-dom/client";

import { buildNodeConfig } from "@/components/canvas/canvas-node-prompt-panel";
import { CanvasResourceMentionTextarea, type CanvasResourceMentionTextareaHandle } from "@/components/canvas/canvas-resource-mention-textarea";
import { canvasResourceMentionQueryAt, insertCanvasResourceMention, reconcileCanvasResourceMentions, type CanvasResourceReference } from "@/lib/canvas/canvas-resource-references";
import { defaultConfig, encodeChannelModel, type AiConfig } from "@/stores/use-config-store";
import { CanvasNodeType, type CanvasNodeData } from "@/types/canvas";

let root: Root | null = null;

afterEach(async () => {
    if (root) await act(async () => root?.unmount());
    root = null;
    document.body.replaceChildren();
});

describe("buildNodeConfig", () => {
    test("uses the Agent-frozen channel, model and video parameters instead of global defaults", () => {
        const frozenModel = "doubao-seedance-2-0-mini-260615";
        const config: AiConfig = {
            ...defaultConfig,
            model: "default-channel::doubao-seedance-2-0-260128",
            videoModel: "default-channel::doubao-seedance-2-0-260128",
            models: ["default-channel::doubao-seedance-2-0-260128", `seedance-channel::${frozenModel}`],
            videoModels: ["default-channel::doubao-seedance-2-0-260128", `seedance-channel::${frozenModel}`],
            videoSeconds: "6",
            vquality: "1080p",
            size: "16:9",
            videoGenerateAudio: "true",
            channels: [
                {
                    id: "default-channel",
                    name: "Default",
                    baseUrl: "/api/system/channels/default-channel",
                    apiKey: "",
                    apiFormat: "openai",
                    interfaceType: "ai-open-platform-video-volcengine",
                    models: ["doubao-seedance-2-0-260128"],
                    scope: "system",
                    enabled: true,
                    modelCosts: [],
                },
                {
                    id: "seedance-channel",
                    name: "Seedance",
                    baseUrl: "/api/system/channels/seedance-channel",
                    apiKey: "",
                    apiFormat: "openai",
                    interfaceType: "ai-open-platform-video-volcengine",
                    models: [frozenModel],
                    scope: "system",
                    enabled: true,
                    modelCosts: [
                        {
                            model: frozenModel,
                            displayName: "Seedance 2.0 Mini",
                            marketingCopy: "",
                            promotionBadge: "",
                            estimatedDurationSeconds: 30,
                            brandKey: "seedance",
                            accessPolicy: "authenticated",
                            accessible: true,
                            capability: "video",
                            watermarkCapability: "controlled",
                            billingMode: "per_second",
                            priceStrategy: "video_resolution",
                            unitPriceMicrocredits: 0,
                            priceTiers: [{ resolution: "720p", inputVariant: "standard", unitPriceMicrocredits: 200_000 }],
                            providerCapabilities: {
                                providerFamily: "seedance",
                                modelKey: frozenModel,
                                displayName: "Seedance 2.0 Mini",
                                upstreamMode: "video",
                                capability: "video",
                                resolutions: ["480p", "720p"],
                                resolutionPixels: {},
                                inputVariants: ["standard"],
                                referenceVideoResolutions: [],
                                generatedAudioResolutions: [],
                                ratios: ["adaptive", "16:9", "9:16"],
                                qualities: [],
                                outputCounts: [1],
                                durations: Array.from({ length: 12 }, (_, index) => index + 4),
                                durationMin: 4,
                                durationMax: 15,
                                supportsSmartDuration: false,
                                supportsTextToVideo: true,
                                supportsImageToVideo: true,
                                supportsReferenceVideo: false,
                                supportsNativeAudio: false,
                                supportsDialogue: false,
                                supportsVoiceReference: false,
                                supportsLipSync: false,
                                supportsIndependentAudio: false,
                                supportsGeneratedAudio: false,
                                watermarkCapability: "controlled",
                                supportsAudioOnly: false,
                                requiresAdaptiveFrames: false,
                                supportsTokenUsageBilling: false,
                                generationModes: ["text", "image"],
                                adaptiveRatioModes: ["text", "image"],
                                requiredAdaptiveRatioModes: [],
                                maxImages: 1,
                                maxImagesWithVideo: 1,
                                maxVideos: 0,
                                maxAudios: 0,
                                maxVideoDurationSeconds: 0,
                                maxAudioDurationSeconds: 0,
                                tools: [],
                            },
                        },
                    ],
                },
            ],
        };
        const node: CanvasNodeData = {
            id: "agent-video",
            type: CanvasNodeType.Video,
            title: "Agent video",
            position: { x: 0, y: 0 },
            width: 420,
            height: 236,
            metadata: {
                channelId: "seedance-channel",
                model: frozenModel,
                size: "adaptive",
                seconds: "5",
                vquality: "720p",
                generateAudio: "false",
                videoEditOperation: "text_to_video",
            },
        };

        const result = buildNodeConfig(config, node, "video");

        expect(result.model).toBe(encodeChannelModel("seedance-channel", frozenModel));
        expect(result.size).toBe("adaptive");
        expect(result.videoSeconds).toBe("5");
        expect(result.vquality).toBe("720p");
        expect(result.videoGenerateAudio).toBe("false");
    });

    test("does not replace a frozen model with the global default after catalog removal", () => {
        const node: CanvasNodeData = {
            id: "historical-agent-video",
            type: CanvasNodeType.Video,
            title: "Historical Agent video",
            position: { x: 0, y: 0 },
            width: 420,
            height: 236,
            metadata: { channelId: "retired-channel", model: "retired-video-model", seconds: "5", size: "9:16", vquality: "720p", generateAudio: "false" },
        };

        const result = buildNodeConfig({ ...defaultConfig, videoModel: "current-channel::current-video-model" }, node, "video");

        expect(result.model).toBe("retired-channel::retired-video-model");
    });
});

describe("canvas resource mention editing", () => {
    const firstReference = reference("character-xiaoming", "小明");
    const secondReference = reference("character-xiaoliang", "小亮");

    test("replaces the pending @ at the current caret instead of appending at the end", () => {
        const first = insertCanvasResourceMention("这是@，这是@", { start: 3, end: 3 }, firstReference);
        const second = insertCanvasResourceMention(first.value, { start: first.value.length, end: first.value.length }, secondReference);

        expect(second.value).toBe("这是@[node:character-xiaoming]，这是@[node:character-xiaoliang] ");
    });

    test("opens the mention query directly after Chinese text", () => {
        expect(canvasResourceMentionQueryAt("这是小明@", 5)).toEqual({ start: 4, query: "" });
    });

    test("removes dangling node mentions after their edge-derived references disappear", () => {
        const prompt = "让 @[node:character-xiaoming] 看向 @[node:character-xiaoliang]";

        expect(reconcileCanvasResourceMentions(prompt, [secondReference])).toBe("让 看向 @[node:character-xiaoliang]");
    });

    test("keeps the last valid caret when an external reference control takes focus", async () => {
        const host = document.createElement("div");
        document.body.append(host);
        root = createRoot(host);
        const editorHandleRef = createRef<CanvasResourceMentionTextareaHandle>();
        let latestValue = "这是@，这是@";

        function Harness() {
            const [value, setValue] = useState(latestValue);
            return createElement(CanvasResourceMentionTextarea, {
                editorHandleRef,
                value,
                references: [firstReference],
                onChange: (next) => {
                    latestValue = next;
                    setValue(next);
                },
            });
        }

        await act(async () => root?.render(createElement(Harness)));
        const editor = host.querySelector<HTMLElement>("[contenteditable='true']");
        expect(editor).not.toBeNull();
        const textNode = editor?.firstChild;
        expect(textNode?.nodeType).toBe(Node.TEXT_NODE);

        await act(async () => {
            if (!editor || !textNode) return;
            editor.focus();
            const range = document.createRange();
            range.setStart(textNode, 3);
            range.collapse(true);
            window.getSelection()?.removeAllRanges();
            window.getSelection()?.addRange(range);
            editor.dispatchEvent(new Event("pointerup", { bubbles: true }));
        });

        window.getSelection()?.removeAllRanges();
        await act(async () => editorHandleRef.current?.insertReference(firstReference));

        expect(latestValue).toBe("这是@[node:character-xiaoming]，这是@");
    });
});

function reference(nodeId: string, label: string) {
    return {
        id: nodeId,
        nodeId,
        kind: "character",
        label,
        title: label,
        active: true,
        sourceType: CanvasNodeType.Image,
    } satisfies CanvasResourceReference;
}
