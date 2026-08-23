import "./setup-happy-dom";

import { afterEach, beforeAll, expect, test } from "bun:test";
import { act, createElement } from "react";
import type { Root } from "react-dom/client";

import type { CanvasStoryboardPipelineProgress } from "../src/lib/canvas/canvas-storyboard-progress";
import { CanvasNodeType, type CanvasNodeData, type StoryboardRow } from "../src/types/canvas";

let CanvasScriptNodeContent: typeof import("../src/components/canvas/canvas-script-node").CanvasScriptNodeContent;
let CanvasScriptEditor: typeof import("../src/components/canvas/canvas-script-node").CanvasScriptEditor;
let createRoot: (container: Element | DocumentFragment) => Root;
let root: Root | null = null;

beforeAll(async () => {
    ({ createRoot } = await import("react-dom/client"));
    ({ CanvasScriptNodeContent, CanvasScriptEditor } = await import("../src/components/canvas/canvas-script-node"));
});

afterEach(async () => {
    if (root) await act(async () => root?.unmount());
    root = null;
    document.body.replaceChildren();
});

test("纯视频生产计划不展示未声明的分镜图阶段和图片操作", async () => {
    await mount(scriptNode([row({ deliverables: ["video_clip"], videoNodeId: "video-1" })]));

    expect(Boolean(exactText("分镜图"))).toBe(false);
    expect(Boolean(button("创建 1 个图片节点"))).toBe(false);
    expect(Boolean(button("生成未完成的图片"))).toBe(false);
    expect(document.querySelector(".lucide-grid-3-x-3")).toBeNull();
    expect(Boolean(exactText("镜头视频"))).toBe(true);
    expect(Boolean(button("视频节点已创建"))).toBe(true);
});

test("纯图片生产计划只展示声明的分镜图阶段", async () => {
    await mount(scriptNode([row({ deliverables: ["storyboard_image"], imageNodeId: "image-1" })]));

    expect(Boolean(exactText("分镜图"))).toBe(true);
    expect(Boolean(exactText("镜头视频"))).toBe(false);
    expect(Boolean(exactText("合并成片"))).toBe(false);
});

test("纯视频生产计划的全屏编辑器不提供图片生成操作", async () => {
    await mountEditor(scriptNode([row({ deliverables: ["video_clip"], videoNodeId: "video-1" })]));

    expect(Boolean(button("生成分镜图"))).toBe(false);
    expect(Boolean(button("生成视频"))).toBe(true);
});

test("未绑定生产交付合同的手工脚本节点继续展示完整媒体编辑能力", async () => {
    await mount(scriptNode([row()]));

    expect(Boolean(exactText("分镜图"))).toBe(true);
    expect(Boolean(exactText("镜头视频"))).toBe(true);
});

async function mount(node: CanvasNodeData) {
    const container = document.createElement("div");
    document.body.append(container);
    root = createRoot(container);
    await act(async () => {
        root?.render(createElement(CanvasScriptNodeContent, {
            node,
            pipeline,
            scale: 1,
            mentionReferences: [],
            onOpen: noop,
            onCreateImageNodes: noop,
            onCreateVideoNodes: noop,
            onGenerateImages: noop,
            onGenerateVideos: noop,
            onMergeVideos: noop,
            onCreateActionBoards: noop,
            onRetryBatch: noop,
            onRetryBatchItem: noop,
            onStopBatch: noop,
            onCancelBatchItem: noop,
            onAddRow: noop,
            onRemoveRow: noop,
            onUpdateRow: noop,
            onPromptChange: noop,
            onGenerateScript: noop,
            onShotDurationChange: noop,
            onShotCountChange: noop,
            onComposerHeightChange: noop,
            onConnectStart: noop,
            onScrollTopChange: noop,
        }));
    });
}

async function mountEditor(node: CanvasNodeData) {
    const container = document.createElement("div");
    document.body.append(container);
    root = createRoot(container);
    await act(async () => {
        root?.render(createElement(CanvasScriptEditor, {
            node,
            open: true,
            onClose: noop,
            onUpdateRows: noop,
            onVisibleColumnsChange: noop,
            onGenerateImages: noop,
            onGenerateVideos: noop,
        }));
    });
}

type DeliveryAwareStoryboardRow = StoryboardRow & { deliverables: Array<"storyboard_image" | "video_clip"> };

function row(patch: Partial<DeliveryAwareStoryboardRow> = {}): StoryboardRow {
    return {
        id: "shot-1",
        shotNumber: 1,
        durationSeconds: 5,
        plotDescription: "蓝紫色光带汇聚",
        dialogue: "",
        characters: [],
        shotSize: "",
        emotion: "",
        lightingAndAtmosphere: "",
        audioEffects: "",
        camera: "",
        motion: "",
        timeBeats: "",
        imageGenerationPrompt: "",
        videoMotionPrompt: "缓慢推进",
        negativePrompt: "",
        referenceNodeIds: [],
        status: "success",
        ...patch,
    };
}

function scriptNode(rows: StoryboardRow[]): CanvasNodeData {
    return {
        id: "script-1",
        type: CanvasNodeType.Script,
        title: "纯视频计划",
        position: { x: 0, y: 0 },
        width: 920,
        height: 360,
        metadata: {
            status: "success",
            composerContent: "蓝紫色光带汇聚",
            storyboard: { rows, visibleColumns: ["shotNumber", "durationSeconds", "plotDescription", "dialogue"], referenceNodeIds: [] },
        },
    };
}

const stage = { total: 1, created: 1, success: 1, failed: 0, loading: 0, incomplete: 0, nodeIds: ["node-1"] };
const pipeline: CanvasStoryboardPipelineProgress = {
    rows: [],
    images: { ...stage, created: 0, success: 0, incomplete: 1, nodeIds: [] },
    videos: stage,
    final: { ...stage, total: 1, created: 0, success: 0, incomplete: 1, nodeIds: [] },
    successfulVideoNodeIds: ["video-1"],
    finalNodeIds: [],
};

function exactText(value: string) {
    return Array.from(document.querySelectorAll<HTMLElement>("*")).find((element) => element.children.length === 0 && element.textContent === value) || null;
}

function button(name: string) {
    return Array.from(document.querySelectorAll<HTMLButtonElement>("button")).find((element) => element.textContent?.trim() === name) || null;
}

function noop() {}
