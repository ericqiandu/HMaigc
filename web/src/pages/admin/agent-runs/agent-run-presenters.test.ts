import assert from "node:assert/strict";
import { describe, test } from "node:test";

import type { AdminAgentRun } from "@/services/api/admin-agent-runs";

import { describeAgentRunControl, describeAgentRunFacts, formatAgentRunInactiveDuration, formatAgentRunTimestamp, getAgentRunActivityLabel, getAgentRunStatusLabel } from "./agent-run-presenters";

const baseRun: AdminAgentRun = {
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
    linkedVisionTaskStatus: "none",
    linkedMediaTaskStatus: "none",
    billingState: "running",
    providerRequestState: "submitted",
    controlDisposition: "cancel_request_required",
    controlBlockedReason: "",
};

describe("Agent 运行事实 presenter", () => {
    test("直接展示服务端权威活动分类，不在前端按时间重新判定", () => {
        assert.equal(getAgentRunActivityLabel(baseRun.activityClassification), "可能卡住");
        assert.equal(formatAgentRunInactiveDuration(baseRun.inactiveSeconds), "12 分 1 秒未更新");
    });

    test("覆盖所有运行态的人类可读文案", () => {
        assert.equal(getAgentRunStatusLabel("queued"), "排队中");
        assert.equal(getAgentRunStatusLabel("running"), "运行中");
        assert.equal(getAgentRunStatusLabel("waiting_input"), "等待用户回答");
        assert.equal(getAgentRunStatusLabel("waiting_approval"), "等待用户批准");
        assert.equal(getAgentRunStatusLabel("waiting_tool"), "等待工具");
        assert.equal(getAgentRunStatusLabel("cancelled"), "已终止");
        assert.equal(getAgentRunStatusLabel("failed"), "失败");
        assert.equal(getAgentRunStatusLabel("succeeded"), "已完成");
    });

    test("供应商已收到请求时明确说明只发出取消并进入账务核对", () => {
        assert.deepEqual(describeAgentRunControl(baseRun), {
            title: "终止运行并请求取消供应商任务",
            description: "供应商请求已经提交。终止后会停止 Agent 继续推进，同时请求取消关联任务；已提交的供应商任务可能继续执行，账务将进入核对。",
            canInterrupt: true,
            danger: true,
        });
    });

    test("关联任务尚未提交供应商时不误报供应商影响", () => {
        assert.deepEqual(
            describeAgentRunControl({
                ...baseRun,
                linkedModelTaskStatus: "queued",
                billingState: "reserved",
                providerRequestState: "not_submitted",
            }),
            {
                title: "终止 Agent 运行并取消关联任务",
                description: "关联任务尚未提交给供应商。终止后会取消任务、停止 Agent 继续推进，并退回尚未消费的预留积分。",
                canInterrupt: true,
                danger: true,
            },
        );
    });

    test("未决账务和已结束运行不可终止", () => {
        assert.deepEqual(
            describeAgentRunControl({
                ...baseRun,
                controlDisposition: "blocked_by_unresolved_billing",
                controlBlockedReason: "billing_unresolved",
            }),
            {
                title: "暂不可终止",
                description: "存在无法与活动任务安全对应的未决账务，请先完成账务核对。",
                canInterrupt: false,
                danger: false,
            },
        );
        assert.equal(describeAgentRunControl({ ...baseRun, status: "cancelled", controlDisposition: "already_terminal" }).canInterrupt, false);
    });

    test("运行详情只展示后端返回的任务、账务、供应商和工具事实", () => {
        const runWithVision = {
            ...baseRun,
            linkedVisionTaskStatus: "running",
            pendingToolName: "production.render",
        };
        assert.deepEqual(describeAgentRunFacts(runWithVision), [
            { label: "文本模型任务", value: "运行中" },
            { label: "图片理解任务", value: "运行中" },
            { label: "媒体任务", value: "无关联任务" },
            { label: "账务", value: "计费中" },
            { label: "供应商请求", value: "已提交" },
            { label: "等待工具", value: "production.render" },
        ]);
    });

    test("时间格式固定使用北京时间且缺失事实显式显示", () => {
        assert.equal(formatAgentRunTimestamp("2026-08-24T02:03:04Z"), "2026-08-24 10:03:04");
        assert.equal(formatAgentRunTimestamp(""), "未提供");
    });
});
