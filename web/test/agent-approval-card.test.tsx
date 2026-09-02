import "./setup-happy-dom";

import { App } from "antd";
import { afterEach, beforeAll, expect, test } from "bun:test";
import { act, createElement } from "react";
import type { Root } from "react-dom/client";

import type { AgentPendingApproval, AgentToolCall } from "@/services/api/agent-runtime";

import { AgentApprovalCard, type AgentApprovalDecision } from "../src/components/canvas/agent-approval-card";

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

test("画布变更审批卡展示冻结操作且只提供一组批准与拒绝操作", async () => {
    const decisions: AgentApprovalDecision[] = [];
    await mount(
        canvasCall(),
        approval({
            toolCallId: "canvas-change-1",
            toolName: "canvas.apply_ops",
            effect: { kind: "canvas_mutation", summary: "新增镜头并移除旧连线", targetIds: ["node-1", "edge-1"] },
        }),
        async (decision) => {
            decisions.push(decision);
        },
    );

    const card = document.querySelector<HTMLElement>('[aria-label="Agent 执行审批"]');
    expect(card?.textContent).toContain("canvas.apply_ops");
    expect(card?.textContent).toContain("新增节点 · node-1");
    expect(card?.textContent).toContain("删除连线 · edge-1");
    expect(card?.textContent).toContain("画布版本 12");
    expect(card?.querySelectorAll("button")).toHaveLength(2);

    await act(async () => button("批准执行").click());
    expect(decisions).toEqual([
        {
            toolCallId: "canvas-change-1",
            actionVersion: 3,
            proposalHash: "a".repeat(64),
            decision: "approved",
        },
    ]);
});

test("媒体审批卡展示模型参数、冻结积分与到期时间", async () => {
    await mount(mediaCall(), mediaApproval(), async () => undefined);

    const card = document.querySelector<HTMLElement>('[aria-label="Agent 执行审批"]');
    expect(card?.textContent).toContain("video-model-pro");
    expect(card?.textContent).toContain("15 秒");
    expect(card?.textContent).toContain("720p");
    expect(card?.textContent).toContain("16:9");
    expect(card?.textContent).toContain("2.265 积分");
    expect(card?.textContent).toContain("价格版本9");
    expect(card?.textContent).toContain("2099/09/01 16:30");
});

test("视觉理解审批展示资源数量、细节等级和冻结费用，但不展示内部提示词", async () => {
    await mount(visionCall(), visionApproval(), async () => undefined);

    const card = document.querySelector<HTMLElement>('[aria-label="Agent 执行审批"]');
    expect(card?.textContent).toContain("理解 2 张图片");
    expect(card?.textContent).toContain("低细节");
    expect(card?.textContent).toContain("0.001 积分");
    expect(card?.textContent).not.toContain("不应展示的内部视觉提示词");
    expect(buttons("批准执行")).toHaveLength(1);
});

test("提交中的审批只锁定当前 proposal，不会复用到下一份 proposal", async () => {
    let resolveFirst: (() => void) | undefined;
    const firstDecision = new Promise<void>((resolve) => {
        resolveFirst = resolve;
    });
    const secondApproval = mediaApproval({ toolCallId: "media-2", proposalHash: "c".repeat(64) });
    const secondCall = mediaCall({ toolCallId: "media-2", actionVersion: 4 });

    await mountMany([
        { call: mediaCall(), approval: mediaApproval(), onDecision: async () => firstDecision },
        { call: secondCall, approval: secondApproval, onDecision: async () => undefined },
    ]);

    const approveButtons = buttons("批准执行");
    expect(approveButtons).toHaveLength(2);
    await act(async () => approveButtons[0]?.click());
    expect(approveButtons[0]?.disabled).toBe(true);
    expect(approveButtons[1]?.disabled).toBe(false);

    await act(async () => resolveFirst?.());
});

test("过期审批必须由 Agent 创建新提案且不允许继续操作", async () => {
    await mount(mediaCall(), mediaApproval({ expiresAt: "2026-09-01T07:59:59Z" }), async () => undefined, new Date("2026-09-01T08:00:00Z"));

    const alert = document.querySelector<HTMLElement>('[role="alert"]');
    expect(alert?.textContent).toContain("审批已过期");
    expect(alert?.textContent).toContain("由 Agent 创建新提案");
    expect(buttons("批准执行")).toHaveLength(0);
    expect(buttons("拒绝执行")).toHaveLength(0);
});

test("提案身份与当前工具调用不一致时显式失败", async () => {
    await mount(mediaCall(), mediaApproval({ actionVersion: 5 }), async () => undefined);

    const alert = document.querySelector<HTMLElement>('[role="alert"]');
    expect(alert?.textContent).toContain("审批事实与当前工具调用不一致");
    expect(alert?.textContent).toContain("由 Agent 创建新提案");
    expect(document.querySelectorAll("button")).toHaveLength(0);
});

async function mount(call: AgentToolCall, pendingApproval: AgentPendingApproval, onDecision: (decision: AgentApprovalDecision) => Promise<void>, now = new Date("2026-09-01T08:00:00Z")) {
    await mountMany([{ call, approval: pendingApproval, onDecision }], now);
}

async function mountMany(cards: Array<{ call: AgentToolCall; approval: AgentPendingApproval; onDecision: (decision: AgentApprovalDecision) => Promise<void> }>, now = new Date("2026-09-01T08:00:00Z")) {
    const container = document.createElement("div");
    document.body.append(container);
    root = createRoot(container);
    await act(async () => {
        root?.render(createElement(App, null, createElement("div", { className: "agent-approval-card-test-list" }, ...cards.map((card) => createElement(AgentApprovalCard, { key: card.approval.proposalHash, ...card, busy: false, muted: "#777", now })))));
    });
}

function canvasCall(): AgentToolCall {
    return {
        toolCallId: "canvas-change-1",
        toolName: "canvas.apply_ops",
        actionVersion: 3,
        arguments: {
            canvasId: "canvas-1",
            baseRevision: 12,
            clientMutationId: "mutation-1",
            operations: [
                {
                    operationId: "operation-add-1",
                    type: "add_node",
                    node: { id: "node-1", type: "image", title: "镜头一", position: { x: 10, y: 20 }, width: 320, height: 180 },
                },
                { operationId: "operation-delete-edge-1", type: "delete_connections", connectionIds: ["edge-1"] },
            ],
        },
        expectedDelivery: { kind: "canvas_change", targetCanvasId: "canvas-1", completionCriteria: [{ fact: "canvas_revision" }] },
    };
}

function mediaCall(overrides: Partial<Pick<AgentToolCall, "toolCallId" | "actionVersion">> = {}): AgentToolCall {
    return {
        toolCallId: overrides.toolCallId ?? "media-1",
        toolName: "media.generate",
        actionVersion: overrides.actionVersion ?? 4,
        arguments: {
            mediaKind: "video",
            modelRecordId: "video-record-1",
            modelKey: "video-model-pro",
            parameters: { prompt: "雨夜追逐", durationSeconds: 15, resolution: "720p", aspectRatio: "16:9", generateAudio: true },
            sourceResourceIds: ["resource-image-1"],
            targetCanvasNodeId: "video-node-1",
            clientRequestId: "generate-video-1",
        },
        expectedDelivery: { kind: "generated_asset", requiredArtifacts: ["video"], completionCriteria: [{ fact: "resource", artifact: "video" }] },
    };
}

function visionCall(): AgentToolCall {
    return {
        toolCallId: "vision-1",
        toolName: "vision.analyze",
        actionVersion: 1,
        arguments: {
            modelRecordId: "vision-record-1",
            modelKey: "deepseek-v4-flash-vision-exp",
            sourceResourceIds: ["resource-image-1", "resource-image-2"],
            prompt: "不应展示的内部视觉提示词",
            detail: "low",
            clientRequestId: "vision-request-1",
        },
        expectedDelivery: { kind: "answer", completionCriteria: [{ fact: "final_message" }] },
    };
}

function approval(overrides: Partial<AgentPendingApproval>): AgentPendingApproval {
    return {
        toolCallId: "canvas-change-1",
        toolName: "canvas.apply_ops",
        actionVersion: 3,
        proposalHash: "a".repeat(64),
        expiresAt: "2099-09-01T08:30:00Z",
        effect: { kind: "canvas_mutation", summary: "更新画布", targetIds: ["canvas-1"] },
        ...overrides,
    };
}

function mediaApproval(overrides: Partial<AgentPendingApproval> = {}): AgentPendingApproval {
    return approval({
        toolCallId: "media-1",
        toolName: "media.generate",
        actionVersion: 4,
        proposalHash: "b".repeat(64),
        expiresAt: "2099-09-01T08:30:00Z",
        effect: { kind: "media_generation", summary: "生成 15 秒视频", targetIds: ["video-node-1"] },
        quote: { modelRecordId: "video-record-1", modelKey: "video-model-pro", priceVersion: 9, amountMicrocredits: 2_265_000 },
        ...overrides,
    });
}

function visionApproval(): AgentPendingApproval {
    return approval({
        toolCallId: "vision-1",
        toolName: "vision.analyze",
        actionVersion: 1,
        proposalHash: "d".repeat(64),
        effect: { kind: "vision_analysis", summary: "理解 2 张图片", targetIds: ["resource-image-1", "resource-image-2"] },
        quote: { modelRecordId: "vision-record-1", modelKey: "deepseek-v4-flash-vision-exp", priceVersion: 4, amountMicrocredits: 1_000 },
    });
}

function button(label: string): HTMLButtonElement {
    const result = buttons(label)[0];
    if (!result) throw new Error(`未找到按钮：${label}`);
    return result;
}

function buttons(label: string): HTMLButtonElement[] {
    return Array.from(document.querySelectorAll<HTMLButtonElement>("button")).filter((candidate) => candidate.textContent?.trim() === label);
}
