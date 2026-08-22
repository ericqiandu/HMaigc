import { expect, test } from "bun:test";

import { agentCanvasCommittedRevision } from "../src/lib/canvas/canvas-agent-runtime-event";
import { requireCanvasCollaborationRevision, requireEditableCanvasCollaboration } from "../src/lib/canvas/canvas-collaboration-preflight";
import type { AgentRuntimeEvent } from "../src/services/api/agent-runtime";
import type { CanvasCollaborationState } from "../src/services/api/canvas-collaboration";

test("Agent canvas.commit 使用 committedRevision 触发当前画布刷新", () => {
    const event = agentEvent({ canvasId: "canvas-1", committedRevision: 8 });

    expect(agentCanvasCommittedRevision(event, "canvas-1")).toBe(8);
    expect(agentCanvasCommittedRevision(event, "canvas-2")).toBeUndefined();
});

test("后续运行事件携带旧工具结果时不重复刷新画布", () => {
    const event = agentEvent({ canvasId: "canvas-1", committedRevision: 8 });
    event.kind = "run.completed";

    expect(agentCanvasCommittedRevision(event, "canvas-1")).toBeUndefined();
});

test("Agent 启动前权限尚未加载时读取权威协作状态", async () => {
    const state = collaborationState(true);
    let loadCount = 0;

    const loaded = await requireEditableCanvasCollaboration(null, false, async () => {
        loadCount += 1;
        return state;
    });

    expect(loaded).toBe(state);
    expect(loadCount).toBe(1);
});

test("Agent 启动前权限已加载但远程基线缺失时仍重建基线", async () => {
    const state = collaborationState(true);
    let loadCount = 0;

    const loaded = await requireEditableCanvasCollaboration(state.access, false, async () => {
        loadCount += 1;
        return state;
    });

    expect(loaded).toBe(state);
    expect(loadCount).toBe(1);
});

test("Agent 启动前已确认只读且已有基线时显式拒绝", async () => {
    let loadCount = 0;

    await expect(requireEditableCanvasCollaboration(collaborationState(false).access, true, async () => {
        loadCount += 1;
        return collaborationState(true);
    })).rejects.toThrow("当前用户没有画布编辑权限");
    expect(loadCount).toBe(0);
});

test("Agent 提交后协作查询未达到已确认版本时显式失败", () => {
    const staleState = collaborationState(true);
    staleState.project.revision = 7;

    expect(() => requireCanvasCollaborationRevision(staleState, 8)).toThrow("仅返回版本 7");
    expect(requireCanvasCollaborationRevision(staleState, 7)).toBe(staleState);
});

function agentEvent(output: Record<string, unknown>): AgentRuntimeEvent {
    return {
        protocolVersion: 1,
        threadId: "thread-1",
        runId: "run-1",
        sequence: 3,
        kind: "item.completed",
        itemId: "tool-result-1",
        payload: {
            toolCallId: "canvas-commit-1",
            toolName: "canvas.commit",
            actionVersion: 1,
            succeeded: true,
            output,
        },
        createdAt: "2026-08-20T00:00:00Z",
    };
}

function collaborationState(canEdit: boolean): CanvasCollaborationState {
    return {
        project: {
            id: "canvas-1",
            title: "测试画布",
            nodes: [],
            connections: [],
            chatSessions: [],
            activeChatId: null,
            backgroundMode: "dots",
            viewport: { x: 0, y: 0, k: 1 },
            createdAt: "2026-08-20T00:00:00Z",
            updatedAt: "2026-08-20T00:00:00Z",
            revision: 1,
        },
        access: {
            level: canEdit ? "manager" : "viewer",
            canEdit,
            canManage: canEdit,
            teamSubscriptionActive: true,
        },
        collaborators: [],
        teamMembers: [],
    };
}
