import "./setup-happy-dom";

import { afterEach, beforeAll, expect, test } from "bun:test";
import { act, createElement } from "react";
import type { Root } from "react-dom/client";

import type { AgentRuntimeEvent, AgentRuntimeView, AgentThreadHistoryRun, AgentThreadHistoryTurn, AgentTimelineItem } from "@/services/api/agent-runtime";

import { AgentRuntimeActivity } from "../src/components/canvas/agent-runtime-activity";

let createRoot: (container: Element | DocumentFragment) => Root;
let root: Root | null = null;

beforeAll(async () => {
    ({ createRoot } = await import("react-dom/client"));
});

afterEach(async () => {
    if (root) await act(async () => root?.unmount());
    root = null;
    document.body.replaceChildren();
});

test("读取画布与加载 Skill 只显示真实活动，不出现确认操作", async () => {
    await mount([toolItem("read-item", 1, "canvas.read", true, { canvasId: "canvas-1", revision: 8 }), toolItem("skill-item", 2, "skills.load", true, { skillDir: "skills/storyboard", version: 3 })]);

    const activity = document.querySelector<HTMLElement>('[aria-label="Agent 工具活动"]');
    expect(activity?.textContent).toContain("canvas.read");
    expect(activity?.textContent).toContain("已读取画布");
    expect(activity?.textContent).toContain("skills.load");
    expect(activity?.textContent).toContain("已加载 Skill");
    expect(activity?.querySelectorAll("button")).toHaveLength(0);
});

test("项目写入显示项目变更而不是媒体生成", async () => {
    await mount([
        toolItem("project-item", 1, "project.apply_ops", true, {
            projectId: "project-1",
            unitId: "unit-1",
            baseRevision: 6,
            committedRevision: 7,
            clientMutationId: "project-mutation-1",
            proposalHash: "a".repeat(64),
            appliedOperationIds: ["create-character-candidates"],
        }),
    ]);

    const activity = document.querySelector<HTMLElement>('[data-agent-activity-id="project-item"]');
    expect(activity?.textContent).toContain("项目变更已提交");
    expect(activity?.textContent).not.toContain("媒体已生成");
});

test("刷新后从持久化事件恢复生成媒体，且后续画布写入失败不会隐藏资产", async () => {
    await mount([
        toolItem("media-item", 1, "media.generate", true, {
            taskId: "task-1",
            billingOrderId: "billing-1",
            mediaKind: "image",
            clientRequestId: "request-1",
            resources: [{ resourceId: "resource-1", kind: "image", url: "/api/resources/resource-1/file" }],
        }),
        toolItem("canvas-item", 2, "canvas.apply_ops", false, {}, "canvas_revision_conflict"),
    ]);

    const activity = document.querySelector<HTMLElement>('[aria-label="Agent 工具活动"]');
    expect(activity?.textContent).toContain("媒体已生成");
    expect(activity?.textContent).toContain("画布写入失败");
    expect(activity?.textContent).toContain("画布版本已经更新，请同步最新内容后重试");
    expect(activity?.textContent).not.toContain("canvas_revision_conflict");
    expect(activity?.querySelector<HTMLImageElement>('img[src="/api/resources/resource-1/file"]')).not.toBeNull();
});

test("实时事件覆盖同一持久化执行项而不产生重复活动", async () => {
    await mount(
        [toolItem("media-item", 1, "media.generate", true, { taskId: "old-task", mediaKind: "video", resources: [{ resourceId: "old-resource", kind: "video", url: "/api/resources/old-resource/file" }] })],
        [completedEvent("media-item", 8, "media.generate", { taskId: "new-task", mediaKind: "video", resources: [{ resourceId: "new-resource", kind: "video", url: "/api/resources/new-resource/file" }] })],
    );

    const activities = document.querySelectorAll('[data-agent-activity-id="media-item"]');
    expect(activities).toHaveLength(1);
    expect(activities[0]?.textContent).not.toContain("new-task");
    expect(activities[0]?.textContent).not.toContain("old-task");
    expect(activities[0]?.querySelector<HTMLVideoElement>('video[src="/api/resources/new-resource/file"]')).not.toBeNull();
    expect(activities[0]?.querySelector<HTMLVideoElement>('video[src="/api/resources/old-resource/file"]')).toBeNull();
});

test("终态权威结果收口本机流中迟到的工具开始事件", async () => {
    const pending = visionView("waiting_tool", false);
    const terminal: AgentRuntimeView = {
        ...pending,
        run: { ...pending.run, status: "succeeded", lastEventSequence: 1, stateVersion: 4, completedAt: "2026-09-01T00:00:08Z" },
        state: {
            ...pending.state,
            status: "succeeded",
            stateVersion: 4,
            lastToolResult: { toolCallId: "read-call", actionVersion: 1, succeeded: true, output: { revision: 12 } },
            pendingToolCall: undefined,
            pendingToolStarted: undefined,
        },
    };
    await mount([], [startedEvent("read-item", 2, "canvas.read", "read-call")], terminal);

    const activity = document.querySelector<HTMLElement>('[data-agent-activity-id="read-item"]');
    expect(activity?.textContent).toContain("已读取画布");
    expect(activity?.textContent).not.toContain("工具正在执行");
});

test("取消后的待结算媒体工具不再显示为执行中", async () => {
    const pending = cancelledMediaView();
    await mount([], [startedEvent("media-item", 2, "media.generate", pending.state.pendingToolCall!.toolCallId)], pending);

    const activity = document.querySelector<HTMLElement>('[data-agent-activity-id="media-item"]');
    expect(activity?.dataset.status).toBe("interrupted");
    expect(activity?.textContent).toContain("已停止，等待结算");
    expect(activity?.textContent).not.toContain("工具正在执行");
    expect(activity?.querySelector(".animate-spin")).toBeNull();
});

test("取消态权威事实会清除历史中断项的旧协议错误", async () => {
    const pending = cancelledMediaView();
    const interruptedItem: AgentTimelineItem = {
        id: "media-item",
        runId: "run-1",
        kind: "tool_call",
        status: "interrupted",
        ordinal: 1,
        sourceEventSequence: 5,
        content: { toolCallId: "media-call-1", actionVersion: 1, interrupted: true },
        startedAt: "2026-09-01T00:00:00Z",
        completedAt: "2026-09-01T00:00:04Z",
        createdAt: "2026-09-01T00:00:00Z",
        updatedAt: "2026-09-01T00:00:04Z",
    };
    await mount([interruptedItem], [], pending);

    const activity = document.querySelector<HTMLElement>('[data-agent-activity-id="media-item"]');
    expect(activity?.dataset.status).toBe("interrupted");
    expect(activity?.textContent).toContain("已停止，等待结算");
    expect(activity?.textContent).not.toContain("工具执行事实缺少有效的 toolName 或 toolCallId");
});

test("媒体工具完成但缺少真实资源时显式暴露协议错误", async () => {
    await mount([toolItem("media-item", 1, "media.generate", true, { taskId: "task-without-resource", mediaKind: "video", resources: [] })]);

    const activity = document.querySelector<HTMLElement>('[data-agent-activity-id="media-item"]');
    expect(activity?.dataset.status).toBe("invalid");
    expect(activity?.textContent).toContain("媒体生成完成事实缺少有效资源");
    expect(activity?.querySelector("img, video, audio")).toBeNull();
});

test("视觉理解按服务端 Run 事实展示审批与执行阶段，不读取提示词推断状态", async () => {
    await mount([], [], visionView("waiting_approval", false));
    expect(document.body.textContent).toContain("等待费用确认");

    await unmount();
    await mount([], [], visionView("waiting_tool", false));
    expect(document.body.textContent).toContain("准备理解图片");

    await unmount();
    await mount([], [], visionView("waiting_tool", true));
    expect(document.body.textContent).toContain("正在理解 2 张图片");
    expect(document.body.textContent).not.toContain("不应展示的内部视觉提示词");
});

test("视觉理解完成只展示结果与用量，不向聊天面板暴露审计身份", async () => {
    await mount([
        toolItem("vision-item", 1, "vision.analyze", true, {
            taskId: "task-secret-1",
            billingOrderId: "billing-secret-1",
            modelRecordId: "vision-record-1",
            modelKey: "deepseek-v4-flash-vision-exp",
            clientRequestId: "vision-request-1",
            sourceResourceIds: ["resource-1", "resource-2"],
            detail: "low",
            analysis: "画面中有两个人物站在雨夜街道。",
            usage: { inputTokens: 384, cachedTokens: 16, outputTokens: 42 },
        }),
    ]);

    const activity = document.querySelector<HTMLElement>('[data-agent-activity-id="vision-item"]');
    expect(activity?.textContent).toContain("图片理解完成");
    expect(activity?.textContent).toContain("输入 384 · 缓存 16 · 输出 42 Token");
    expect(activity?.textContent).not.toContain("task-secret-1");
    expect(activity?.textContent).not.toContain("billing-secret-1");
    expect(activity?.textContent).not.toContain("vision-request-1");
});

test("视觉失败区分已退款与待核对账务且隐藏原始错误码", async () => {
    await mount([toolItem("vision-refunded", 1, "vision.analyze", false, {}, "vision_analysis_failed"), toolItem("vision-uncertain", 2, "vision.analyze", false, {}, "vision_settlement_uncertain")]);

    const refunded = document.querySelector<HTMLElement>('[data-agent-activity-id="vision-refunded"]');
    const uncertain = document.querySelector<HTMLElement>('[data-agent-activity-id="vision-uncertain"]');
    expect(refunded?.textContent).toContain("已退款失败");
    expect(refunded?.textContent).not.toContain("vision_analysis_failed");
    expect(uncertain?.textContent).toContain("账务待核对");
    expect(uncertain?.textContent).not.toContain("vision_settlement_uncertain");
});

async function mount(items: AgentTimelineItem[], events: AgentRuntimeEvent[] = [], view?: AgentRuntimeView) {
    const container = document.createElement("div");
    document.body.append(container);
    root = createRoot(container);
    await act(async () => {
        root?.render(createElement(AgentRuntimeActivity, { runId: "run-1", turns: [{ run: historyRun(), items }], events, view, muted: "#777" }));
    });
}

async function unmount() {
    if (root) await act(async () => root?.unmount());
    root = null;
    document.body.replaceChildren();
}

function historyRun(): AgentThreadHistoryRun {
    return {
        id: "run-1",
        threadId: "thread-1",
        status: "succeeded",
        lastEventSequence: 8,
        stateVersion: 4,
        stepNumber: 2,
        maxSteps: 16,
        modelKey: "agent-model",
        toolSchemaVersion: 10,
        runtimeVersion: 5,
        policyVersion: 5,
        createdAt: "2026-09-01T00:00:00Z",
        updatedAt: "2026-09-01T00:00:08Z",
        completedAt: "2026-09-01T00:00:08Z",
    };
}

function visionView(status: "waiting_approval" | "waiting_tool", started: boolean): AgentRuntimeView {
    const pendingToolCall: NonNullable<AgentRuntimeView["state"]["pendingToolCall"]> = {
        toolCallId: "vision-call-1",
        toolName: "vision.analyze",
        actionVersion: 1,
        arguments: {
            modelRecordId: "vision-record-1",
            modelKey: "deepseek-v4-flash-vision-exp",
            sourceResourceIds: ["resource-1", "resource-2"],
            prompt: "不应展示的内部视觉提示词",
            detail: "low",
            clientRequestId: "vision-request-1",
        },
        expectedDelivery: { kind: "answer", completionCriteria: [{ fact: "final_message" }] },
    };
    return {
        run: {
            id: "run-1",
            threadId: "thread-1",
            reasoningHost: "managed",
            actorUserId: "user-1",
            clientRequestId: "request-1",
            status,
            lastEventSequence: 4,
            stateVersion: 2,
            stepNumber: 1,
            maxSteps: 8,
            modelRecordId: "agent-model-record",
            modelKey: "agent-model",
            toolSchemaVersion: 10,
            runtimeVersion: 5,
            policyVersion: 5,
            createdAt: "2026-09-01T00:00:00Z",
            updatedAt: "2026-09-01T00:00:01Z",
        },
        state: {
            stateVersion: 2,
            stepNumber: 1,
            maxSteps: 8,
            status,
            pendingToolCall,
            ...(started ? { pendingToolStarted: true } : {}),
            clarificationHistory: [],
            userMessage: "分析参考图",
            configuration: {
                generationModels: { vision: { channelId: "channel-deepseek", modelRecordId: "vision-record-1", model: "deepseek-v4-flash-vision-exp", priceVersion: 4 } },
                skills: [],
                attachments: [],
                executionMode: "guided",
            },
        },
        ...(status === "waiting_approval"
            ? {
                  pendingApproval: {
                      toolCallId: "vision-call-1",
                      toolName: "vision.analyze" as const,
                      actionVersion: 1,
                      proposalHash: "a".repeat(64),
                      expiresAt: "2099-09-01T00:05:00Z",
                      effect: { kind: "vision_analysis" as const, summary: "理解 2 张图片", targetIds: ["resource-1", "resource-2"] },
                      quote: { modelRecordId: "vision-record-1", modelKey: "deepseek-v4-flash-vision-exp", priceVersion: 4, amountMicrocredits: 800 },
                  },
              }
            : {}),
    };
}

function cancelledMediaView(): AgentRuntimeView {
    const pendingToolCall: NonNullable<AgentRuntimeView["state"]["pendingToolCall"]> = {
        toolCallId: "media-call-1",
        toolName: "media.generate",
        actionVersion: 1,
        arguments: {
            mediaKind: "image",
            modelRecordId: "image-model-record",
            modelKey: "gpt-image-2",
            parameters: { prompt: "纯白纸船漂浮在平静蓝色水面上" },
            sourceResourceIds: [],
            targetCanvasNodeId: "image-node-1",
            clientRequestId: "generate-image-1",
        },
        expectedDelivery: { kind: "generated_asset", requiredArtifacts: ["image"], completionCriteria: [{ fact: "canvas_bound_resource", artifact: "image" }] },
    };
    return {
        run: {
            id: "run-1",
            threadId: "thread-1",
            reasoningHost: "managed",
            actorUserId: "user-1",
            clientRequestId: "request-1",
            status: "cancelled",
            lastEventSequence: 5,
            stateVersion: 3,
            stepNumber: 1,
            maxSteps: 8,
            modelRecordId: "agent-model-record",
            modelKey: "agent-model",
            toolSchemaVersion: 10,
            runtimeVersion: 5,
            policyVersion: 5,
            createdAt: "2026-09-01T00:00:00Z",
            updatedAt: "2026-09-01T00:00:04Z",
            completedAt: "2026-09-01T00:00:04Z",
        },
        state: {
            stateVersion: 3,
            stepNumber: 1,
            maxSteps: 8,
            status: "cancelled",
            pendingToolCall,
            pendingToolStarted: true,
            clarificationHistory: [],
            userMessage: "生成测试图",
            configuration: {
                generationModels: { image: { channelId: "channel-image", model: "gpt-image-2" } },
                skills: [],
                attachments: [],
                executionMode: "guided",
            },
        },
    };
}

function toolItem(id: string, ordinal: number, toolName: string, succeeded: boolean, output: Record<string, unknown>, errorCode?: string): AgentTimelineItem {
    return {
        id,
        runId: "run-1",
        kind: "tool_call",
        status: succeeded ? "completed" : "failed",
        ordinal,
        sourceEventSequence: ordinal,
        content: { toolCallId: `${id}-call`, toolName, actionVersion: 1, succeeded, output, ...(errorCode ? { errorCode } : {}) },
        startedAt: "2026-09-01T00:00:00Z",
        completedAt: "2026-09-01T00:00:01Z",
        createdAt: "2026-09-01T00:00:00Z",
        updatedAt: "2026-09-01T00:00:01Z",
    };
}

function completedEvent(itemId: string, sequence: number, toolName: string, output: Record<string, unknown>): AgentRuntimeEvent {
    return {
        protocolVersion: 5,
        threadId: "thread-1",
        runId: "run-1",
        sequence,
        createdAt: "2026-09-01T00:00:08Z",
        kind: "item.completed",
        itemId,
        itemKind: "tool_call",
        payload: { toolCallId: `${itemId}-call`, toolName, actionVersion: 1, succeeded: true, output },
    };
}

function startedEvent(itemId: string, sequence: number, toolName: string, toolCallId: string): AgentRuntimeEvent {
    return {
        protocolVersion: 5,
        threadId: "thread-1",
        runId: "run-1",
        sequence,
        createdAt: "2026-09-01T00:00:01Z",
        kind: "item.started",
        itemId,
        itemKind: "tool_call",
        payload: { toolCallId, toolName, actionVersion: 1 },
    };
}
