import "./setup-happy-dom";

import { afterEach, beforeAll, expect, test } from "bun:test";
import { act, createElement } from "react";
import type { Root } from "react-dom/client";

import type {
    AgentArtifactRevision,
    AgentProductionClient,
    AgentStageReviewInput,
} from "../src/services/api/agent-production";
import type {
    AgentRuntimeEvent,
    AgentThreadHistoryRun,
    AgentThreadHistoryTurn,
    AgentTimelineItem,
} from "../src/services/api/agent-runtime";

let AgentProductionTimeline: typeof import("../src/components/canvas/agent-production-card").AgentProductionTimeline;
let createRoot: (container: Element | DocumentFragment) => Root;
let root: Root | null = null;

beforeAll(async () => {
    ({ createRoot } = await import("react-dom/client"));
    ({ AgentProductionTimeline } = await import("../src/components/canvas/agent-production-card"));
});

afterEach(async () => {
    if (root) await act(async () => root?.unmount());
    root = null;
    document.body.replaceChildren();
});

test("刷新历史与 SSE 重放按 itemId 合并为一张可审核剧本卡", async () => {
    const reviews: AgentStageReviewInput[] = [];
    let refreshed = 0;
    const client = productionClient({
        getArtifactRevision: async () => scriptRevision(),
        reviewStage: async (_runId, _stageId, input) => {
            reviews.push(input);
            return reviewResult("stage-script", "approved", input.stageVersion + 1);
        },
    });
    const content = scriptReviewContent();

    await mount({
        client,
        turns: [turn([timelineItem("artifact-review-1", content)])],
        events: [artifactEvent("artifact-review-1", content)],
        onRefresh: async () => {
            refreshed += 1;
        },
    });

    expect(document.querySelectorAll('[aria-label="阶段产物审核"]')).toHaveLength(1);
    expect(document.body.textContent).toContain("纸飞机");
    expect(document.body.textContent).toContain("第一场：屋顶，黄昏。");

    await act(async () => namedButton("确认并继续").click());
    await settle();

    expect(reviews).toHaveLength(1);
    expect(reviews[0]).toEqual({
        stageVersion: 3,
        revisionId: "revision-script-1",
        decision: "approved",
        clientRequestId: "review-request-1",
        comment: "",
    });
    expect(refreshed).toBe(1);
});

test("视觉审核只预览精确候选资源且图片入库目标缺失时拒绝提交", async () => {
    const reviews: AgentStageReviewInput[] = [];
    const review = visualReviewRevision();
    const candidate = imageCandidateRevision();
    const client = productionClient({
        getArtifactRevision: async (_runId, artifactId) => (artifactId === review.artifactId ? review : candidate),
        reviewStage: async (_runId, _stageId, input) => {
            reviews.push(input);
            return reviewResult("stage-visual", "approved", input.stageVersion + 1, input.selectedCandidateRevisionId);
        },
    });

    await mount({
        client,
        turns: [turn([timelineItem("visual-review-1", visualReviewContent())])],
        events: [],
        onRefresh: async () => undefined,
    });

    const preview = document.querySelector<HTMLImageElement>('img[alt="候选 1"]');
    expect(preview?.src).toContain("/api/resources/resource-image-1/file?direct=1");

    await act(async () => namedButton("选择候选 1").click());
    await act(async () => namedButton("确认并继续").click());
    await settle();
    expect(document.querySelector('[role="alert"]')?.textContent).toContain("入库用途、分类和绑定键");
    expect(reviews).toHaveLength(0);

    setInput("入库用途", "character-library");
    setSelect("资产分类", "character");
    setInput("资产绑定键", "hero");
    await act(async () => namedButton("确认并继续").click());
    await settle();

    expect(reviews[0]).toEqual({
        stageVersion: 5,
        revisionId: "revision-review-1",
        decision: "approved",
        selectedCandidateRevisionId: "revision-image-1",
        clientRequestId: "review-request-1",
        comment: "",
        publicationIntent: {
            publicationPurpose: "character-library",
            targetCategory: "character",
            targetBindingKey: "hero",
        },
    });
});

test("视觉候选存在修改要求时拒绝确认并明确引导改用要求修改", async () => {
    const reviews: AgentStageReviewInput[] = [];
    const review = visualReviewRevision();
    const candidate = imageCandidateRevision();
    const client = productionClient({
        getArtifactRevision: async (_runId, artifactId) => (artifactId === review.artifactId ? review : candidate),
        reviewStage: async (_runId, _stageId, input) => {
            reviews.push(input);
            return reviewResult("stage-visual", "approved", input.stageVersion + 1, input.selectedCandidateRevisionId);
        },
    });

    await mount({
        client,
        turns: [turn([timelineItem("visual-review-comment-1", visualReviewContent())])],
        events: [],
        onRefresh: async () => undefined,
    });

    await setTextArea("修改要求", "角色服装颜色需要调整");
    await act(async () => namedButton("选择候选 1").click());
    setInput("入库用途", "character-library");
    setSelect("资产分类", "character");
    setInput("资产绑定键", "hero");
    await act(async () => namedButton("确认并继续").click());
    await settle();

    expect(document.querySelector('[role="alert"]')?.textContent).toContain("确认候选前请清空修改要求");
    expect(reviews).toHaveLength(0);
});

test("持久审核决议恢复后卡片只读且不会重复提交", async () => {
    let reviewCalls = 0;
    const client = productionClient({
        getArtifactRevision: async () => scriptRevision(),
        reviewStage: async () => {
            reviewCalls += 1;
            return reviewResult("stage-script", "approved", 4);
        },
    });
    const resolution = {
        contentType: "stage_review_resolution",
        stageId: "stage-script",
        stageVersion: 3,
        revisionId: "revision-script-1",
        decision: "approved",
        clientRequestId: "saved-review-1",
        resultStageVersion: 4,
        resultStatus: "approved",
        resultReviewRevisionId: "revision-script-1",
        resultUpdatedAt: "2026-08-28T00:00:02Z",
    };

    await mount({
        client,
        turns: [turn([timelineItem("artifact-review-1", scriptReviewContent()), timelineItem("resolution-1", resolution, 5, "approval")])],
        events: [],
        onRefresh: async () => undefined,
    });

    expect(document.body.textContent).toContain("已确认");
    expect(optionalNamedButton("确认并继续")).toBeUndefined();
    expect(reviewCalls).toBe(0);
});

test("审核响应携带未请求的候选或发布事实时保持待确认并显式报错", async () => {
    let refreshed = 0;
    const client = productionClient({
        getArtifactRevision: async () => scriptRevision(),
        reviewStage: async (_runId, stageId, input) => reviewResult(stageId, "approved", input.stageVersion + 1, "unexpected-candidate"),
    });

    await mount({
        client,
        turns: [turn([timelineItem("artifact-review-1", scriptReviewContent())])],
        events: [],
        onRefresh: async () => {
            refreshed += 1;
        },
    });

    await act(async () => namedButton("确认并继续").click());
    await settle();

    expect(document.querySelector('[role="alert"]')?.textContent).toContain("候选版本");
    expect(optionalNamedButton("确认并继续")).toBeDefined();
    expect(refreshed).toBe(0);
});

test("装配任务的历史与 SSE 生命周期合并为一张真实结果卡", async () => {
    const running = mediaAssemblyContent("running", "正在执行 FFmpeg 装配");
    const succeeded = {
        ...mediaAssemblyContent("succeeded", "装配完成"),
        final: {
            artifactRevision: { artifactId: "final-video", revisionId: "final-video-r1" },
            resourceId: "final-resource",
            adopted: true,
        },
    };

    await mount({
        client: productionClient({}),
        turns: [turn([timelineItem("assembly-call-1", running, 4, "tool_call")])],
        events: [toolEvent("assembly-call-1", succeeded, 5)],
        onRefresh: async () => undefined,
    });

    expect(document.querySelectorAll('[aria-label="最终视频装配"]')).toHaveLength(1);
    expect(document.body.textContent).toContain("装配完成");
    expect(document.body.textContent).toContain("1 个片段");
    expect(document.body.textContent).toContain("1920×1080 · 24fps · MP4");
    expect(document.body.textContent).not.toContain("100%");
    expect(document.querySelector<HTMLVideoElement>("video")?.src).toContain("/api/resources/final-resource/file?direct=1");
});

async function mount(input: {
    client: AgentProductionClient;
    turns: AgentThreadHistoryTurn[];
    events: AgentRuntimeEvent[];
    onRefresh: () => Promise<void>;
}) {
    const container = document.createElement("div");
    document.body.append(container);
    root = createRoot(container);
    await act(async () => {
        root?.render(
            createElement(AgentProductionTimeline, {
                runId: "run-1",
                turns: input.turns,
                events: input.events,
                client: input.client,
                onRefresh: input.onRefresh,
                createClientRequestId: () => "review-request-1",
            }),
        );
    });
    await settle();
}

function productionClient(overrides: Partial<AgentProductionClient>): AgentProductionClient {
    return {
        getArtifactRevision: async () => scriptRevision(),
        reviewStage: async (_runId, stageId, input) => reviewResult(stageId, input.decision === "stopped" ? "stopped" : input.decision === "revision_requested" ? "running" : "approved", input.stageVersion + 1),
        ...overrides,
    };
}

function scriptRevision(): AgentArtifactRevision {
    return {
        artifactId: "artifact-script",
        revisionId: "revision-script-1",
        artifactKey: "script-main",
        revision: 1,
        kind: "script_bundle",
        schemaVersion: 1,
        payload: {
            title: "纸飞机",
            logline: "一个孩子用纸飞机找回父亲的记忆。",
            script: "第一场：屋顶，黄昏。",
            characters: [{ key: "child", name: "小雨", description: "十岁，安静但坚定" }],
            scenes: [{ key: "roof", name: "老屋屋顶", description: "黄昏逆光" }],
            props: [],
            voiceRoles: [],
        },
        upstreamRevisions: [],
        skillVersions: [],
        lifecycleStatus: "awaiting_review",
        createdAt: "2026-08-28T00:00:00Z",
    };
}

function visualReviewRevision(): AgentArtifactRevision {
    return {
        artifactId: "artifact-review",
        revisionId: "revision-review-1",
        artifactKey: "visual-review",
        revision: 1,
        kind: "visual_consistency_review",
        schemaVersion: 1,
        payload: {
            reviewRunId: "visual-run-1",
            reviewModelRecordId: "model-review-1",
            reviewRequestIdentity: "request-review-1",
            candidateRevisions: [{ artifactId: "artifact-image", revisionId: "revision-image-1" }],
            confirmedReferenceRevisions: [],
            assessments: [],
            rankedCandidateRevisionIds: ["revision-image-1"],
            uncertainties: [],
            conflicts: [],
            retrySuggestions: [],
        },
        upstreamRevisions: [{ artifactId: "artifact-image", revisionId: "revision-image-1" }],
        skillVersions: [],
        lifecycleStatus: "awaiting_review",
        createdAt: "2026-08-28T00:00:00Z",
    };
}

function imageCandidateRevision(): AgentArtifactRevision {
    return {
        artifactId: "artifact-image",
        revisionId: "revision-image-1",
        artifactKey: "hero-image",
        revision: 1,
        kind: "media_candidate",
        schemaVersion: 1,
        payload: {
            candidateKey: "hero-image-1",
            mediaKind: "image",
            providerRequestIdentity: "provider-request-1",
            resourceId: "resource-image-1",
            sourceTaskId: "task-image-1",
        },
        resourceId: "resource-image-1",
        upstreamRevisions: [],
        skillVersions: [],
        lifecycleStatus: "awaiting_review",
        createdAt: "2026-08-28T00:00:00Z",
    };
}

function scriptReviewContent() {
    return {
        contentType: "artifact_review",
        stageId: "stage-script",
        stageVersion: 3,
        artifactId: "artifact-script",
        revisionId: "revision-script-1",
        artifactSchema: "script_bundle.v1",
        summary: "剧本初稿待确认",
    };
}

function visualReviewContent() {
    return {
        contentType: "artifact_review",
        stageId: "stage-visual",
        stageVersion: 5,
        artifactId: "artifact-review",
        revisionId: "revision-review-1",
        artifactSchema: "visual_consistency_review.v1",
        summary: "角色图候选待确认",
    };
}

function timelineItem(id: string, content: Record<string, unknown>, sourceEventSequence = 4, kind: AgentTimelineItem["kind"] = "artifact"): AgentTimelineItem {
    return {
        id,
        runId: "run-1",
        kind,
        status: "completed",
        ordinal: sourceEventSequence,
        sourceEventSequence,
        content,
        startedAt: "2026-08-28T00:00:00Z",
        completedAt: "2026-08-28T00:00:01Z",
        createdAt: "2026-08-28T00:00:00Z",
        updatedAt: "2026-08-28T00:00:01Z",
    };
}

function turn(items: AgentTimelineItem[]): AgentThreadHistoryTurn {
    const run: AgentThreadHistoryRun = {
        id: "run-1",
        threadId: "thread-1",
        status: "running",
        lastEventSequence: 5,
        stateVersion: 2,
        stepNumber: 1,
        maxSteps: 8,
        modelKey: "deepseek",
        toolSchemaVersion: 4,
        runtimeVersion: 3,
        policyVersion: 3,
        createdAt: "2026-08-28T00:00:00Z",
        updatedAt: "2026-08-28T00:00:01Z",
    };
    return { run, items };
}

function artifactEvent(itemId: string, payload: Record<string, unknown>): AgentRuntimeEvent {
    return {
        protocolVersion: 4,
        threadId: "thread-1",
        runId: "run-1",
        sequence: 4,
        kind: "item.completed",
        itemId,
        itemKind: "artifact",
        payload,
        createdAt: "2026-08-28T00:00:01Z",
    };
}

function toolEvent(itemId: string, payload: Record<string, unknown>, sequence: number): AgentRuntimeEvent {
    return {
        protocolVersion: 4,
        threadId: "thread-1",
        runId: "run-1",
        sequence,
        kind: "item.completed",
        itemId,
        itemKind: "tool_call",
        payload,
        createdAt: "2026-08-28T00:00:02Z",
    };
}

function mediaAssemblyContent(taskStatus: "running" | "succeeded", stage: string) {
    return {
        contentType: "media_assembly",
        toolCallId: "assemble-final",
        actionVersion: 1,
        taskId: "assembly-task",
        taskStatus,
        stage,
        clipCount: 1,
        audioMode: "none",
        output: {
            artifactKey: "final-video",
            container: "mp4",
            videoCodec: "h264",
            audioCodec: "none",
            width: 1_920,
            height: 1_080,
            frameRate: 24,
        },
        planRevision: { artifactId: "artifact-assembly", revisionId: "revision-assembly-2" },
    };
}

function reviewResult(stageId: string, status: "approved" | "running" | "stopped", version: number, selectedCandidateRevisionId?: string) {
    return {
        stage: {
            id: stageId,
            stageKey: "stage-key",
            specialistKey: "visual" as const,
            reviewPolicy: "required" as const,
            costPolicy: "none" as const,
            status,
            version,
            reviewRevisionId: status === "running" ? undefined : stageId === "stage-script" ? "revision-script-1" : "revision-review-1",
            updatedAt: "2026-08-28T00:00:03Z",
        },
        artifactRevisionIds: [],
        selectedCandidateRevisionId,
        ...(selectedCandidateRevisionId
            ? {
                  publication: {
                      id: "publication-1",
                      artifactRevisionId: selectedCandidateRevisionId,
                      assetId: "asset-1",
                      assetVersionId: "asset-version-1",
                      projectAssetLinkId: "asset-link-1",
                      representationId: "representation-1",
                      status: "succeeded" as const,
                      replayed: false,
                  },
              }
            : {}),
    };
}

function namedButton(name: string): HTMLButtonElement {
    const button = optionalNamedButton(name);
    if (!button) throw new Error(`未找到按钮：${name}`);
    return button;
}

function optionalNamedButton(name: string): HTMLButtonElement | undefined {
    return Array.from(document.querySelectorAll("button")).find((item) => item.textContent?.trim() === name);
}

function setInput(label: string, value: string) {
    const input = document.querySelector<HTMLInputElement>(`input[aria-label="${label}"]`);
    if (!input) throw new Error(`未找到输入框：${label}`);
    act(() => {
        const setter = Object.getOwnPropertyDescriptor(Object.getPrototypeOf(input), "value")?.set;
        if (!setter) throw new Error("当前测试环境不支持 input value setter");
        setter.call(input, value);
        input.dispatchEvent(new Event("input", { bubbles: true }));
    });
}

function setSelect(label: string, value: string) {
    const select = document.querySelector<HTMLSelectElement>(`select[aria-label="${label}"]`);
    if (!select) throw new Error(`未找到选择框：${label}`);
    act(() => {
        const setter = Object.getOwnPropertyDescriptor(Object.getPrototypeOf(select), "value")?.set;
        if (!setter) throw new Error("当前测试环境不支持 select value setter");
        setter.call(select, value);
        select.dispatchEvent(new Event("change", { bubbles: true }));
    });
}

async function setTextArea(label: string, value: string) {
    const textArea = document.querySelector<HTMLTextAreaElement>(`textarea[aria-label="${label}"]`);
    if (!textArea) throw new Error(`未找到文本域：${label}`);
    await act(async () => {
        const setter = Object.getOwnPropertyDescriptor(Object.getPrototypeOf(textArea), "value")?.set;
        if (!setter) throw new Error("当前测试环境不支持 textarea value setter");
        setter.call(textArea, value);
        textArea.dispatchEvent(new Event("input", { bubbles: true }));
    });
}

async function settle() {
    await act(async () => {
        await new Promise((resolve) => setTimeout(resolve, 0));
    });
}
