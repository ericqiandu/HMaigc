import assert from "node:assert/strict";
import { describe, test } from "node:test";

import type { AdminAgentRun, AdminAgentRunPage } from "@/services/api/admin-agent-runs";

import {
    applyAgentRunConflict,
    failAgentRunPageLoad,
    startAgentRunPageLoad,
    succeedAgentRunPageLoad,
    type AgentRunInterruptDraft,
    type AgentRunPageState,
} from "./agent-run-page-state";

const run: AdminAgentRun = {
    runId: "run-123456789",
    threadId: "thread-1",
    actorUserId: "user-1",
    actorDisplayName: "测试用户",
    domainProjectId: "project-1",
    canvasId: "canvas-1",
    status: "running",
    stateVersion: 3,
    stepNumber: 4,
    maxSteps: 24,
    toolSchemaVersion: 2,
    runtimeVersion: 3,
    policyVersion: 5,
    pendingKind: "",
    pendingToolName: "",
    updatedAt: "2026-08-24T02:03:04Z",
    inactiveSeconds: 721,
    activityClassification: "possibly_stalled",
    linkedModelTaskStatus: "running",
    linkedMediaTaskStatus: "none",
    billingState: "running",
    providerRequestState: "submitted",
    controlDisposition: "cancel_request_required",
    controlBlockedReason: "",
    confirmationPhrase: "STOP run-1234",
};

const page: AdminAgentRunPage = { items: [run], total: 1, page: 1, pageSize: 20 };

describe("Agent 任务管理页状态", () => {
    test("首次读取失败时没有伪造空列表", () => {
        const initial: AgentRunPageState = { data: null, loading: true, refreshing: false, error: "" };
        assert.deepEqual(failAgentRunPageLoad(initial, "服务不可用"), {
            data: null,
            loading: false,
            refreshing: false,
            error: "服务不可用",
        });
    });

    test("刷新失败保留上一次成功事实并标记刷新错误", () => {
        const loaded = succeedAgentRunPageLoad({ data: null, loading: true, refreshing: false, error: "" }, page);
        const refreshing = startAgentRunPageLoad(loaded);
        assert.deepEqual(failAgentRunPageLoad(refreshing, "网络中断"), {
            data: page,
            loading: false,
            refreshing: false,
            error: "网络中断",
        });
    });

    test("409 冲突采用服务端最新状态、保留原因并要求重新确认", () => {
        const draft: AgentRunInterruptDraft = {
            run,
            reason: "管理员确认该任务已经卡住",
            confirmation: run.confirmationPhrase ?? "",
            submitting: true,
            error: "",
        };
        const latestRun: AdminAgentRun = {
            ...run,
            stateVersion: 4,
            status: "waiting_approval",
            activityClassification: "awaiting_user",
            confirmationPhrase: "STOP run-1234",
        };
        assert.deepEqual(applyAgentRunConflict(draft, latestRun, "Agent 运行状态已经变化，请按最新状态重试"), {
            run: latestRun,
            reason: draft.reason,
            confirmation: "",
            submitting: false,
            error: "Agent 运行状态已经变化，请按最新状态重试",
        });
    });
});
