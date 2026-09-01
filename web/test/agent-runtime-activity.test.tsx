import "./setup-happy-dom";

import { afterEach, beforeAll, expect, test } from "bun:test";
import { act, createElement } from "react";
import type { Root } from "react-dom/client";

import type { AgentRuntimeEvent, AgentThreadHistoryRun, AgentThreadHistoryTurn, AgentTimelineItem } from "@/services/api/agent-runtime";

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
    await mount([
        toolItem("read-item", 1, "canvas.read", true, { canvasId: "canvas-1", revision: 8 }),
        toolItem("skill-item", 2, "skills.load", true, { skillDir: "skills/storyboard", version: 3 }),
    ]);

    const activity = document.querySelector<HTMLElement>('[aria-label="Agent 工具活动"]');
    expect(activity?.textContent).toContain("canvas.read");
    expect(activity?.textContent).toContain("已读取画布");
    expect(activity?.textContent).toContain("skills.load");
    expect(activity?.textContent).toContain("已加载 Skill");
    expect(activity?.querySelectorAll("button")).toHaveLength(0);
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
    expect(activity?.textContent).toContain("canvas_revision_conflict");
    expect(activity?.querySelector<HTMLImageElement>('img[src="/api/resources/resource-1/file"]')).not.toBeNull();
});

test("实时事件覆盖同一持久化执行项而不产生重复活动", async () => {
    await mount(
        [toolItem("media-item", 1, "media.generate", true, { taskId: "old-task", mediaKind: "video", resources: [{ resourceId: "old-resource", kind: "video", url: "/api/resources/old-resource/file" }] })],
        [completedEvent("media-item", 8, "media.generate", { taskId: "new-task", mediaKind: "video", resources: [{ resourceId: "new-resource", kind: "video", url: "/api/resources/new-resource/file" }] })],
    );

    const activities = document.querySelectorAll('[data-agent-activity-id="media-item"]');
    expect(activities).toHaveLength(1);
    expect(activities[0]?.textContent).toContain("new-task");
    expect(activities[0]?.textContent).not.toContain("old-task");
});

test("媒体工具完成但缺少真实资源时显式暴露协议错误", async () => {
    await mount([toolItem("media-item", 1, "media.generate", true, { taskId: "task-without-resource", mediaKind: "video", resources: [] })]);

    const activity = document.querySelector<HTMLElement>('[data-agent-activity-id="media-item"]');
    expect(activity?.dataset.status).toBe("invalid");
    expect(activity?.textContent).toContain("媒体生成完成事实缺少有效资源");
    expect(activity?.querySelector("img, video, audio")).toBeNull();
});

async function mount(items: AgentTimelineItem[], events: AgentRuntimeEvent[] = []) {
    const container = document.createElement("div");
    document.body.append(container);
    root = createRoot(container);
    await act(async () => {
        root?.render(createElement(AgentRuntimeActivity, { runId: "run-1", turns: [{ run: historyRun(), items }], events, muted: "#777" }));
    });
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
        toolSchemaVersion: 6,
        runtimeVersion: 5,
        policyVersion: 5,
        createdAt: "2026-09-01T00:00:00Z",
        updatedAt: "2026-09-01T00:00:08Z",
        completedAt: "2026-09-01T00:00:08Z",
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
