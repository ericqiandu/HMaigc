import "./setup-happy-dom";

import { App } from "antd";
import { afterEach, beforeAll, expect, test } from "bun:test";
import { act, createElement } from "react";
import type { Root } from "react-dom/client";

import { operationControlIdempotencyKey } from "../src/pages/admin/operations/operations-control";
import { presentPublicVerification } from "../src/pages/admin/operations/operations-presenters";
import type { OperationsRecord } from "../src/services/api/operations";

let ActivePanel: typeof import("../src/pages/admin/operations/operation-active-panel").OperationActivePanel;
let createRoot: (container: Element | DocumentFragment) => Root;
let root: Root | null = null;

beforeAll(async () => {
    ({ createRoot } = await import("react-dom/client"));
    ({ OperationActivePanel: ActivePanel } = await import("../src/pages/admin/operations/operation-active-panel"));
});

afterEach(async () => {
    if (root) await act(async () => root?.unmount());
    root = null;
    document.body.replaceChildren();
});

test("active panel exposes only controls allowed by durable status", async () => {
    const cancelled: string[] = [];
    const recovered: string[] = [];
    const mounted = await mountPanel(operation("running"), {
        onCancel: async (record) => {
            cancelled.push(record.id);
        },
        onRecover: async (record) => {
            recovered.push(record.id);
        },
    });

    await clickButton("停止任务");
    expect(cancelled).toEqual(["op-001"]);

    await mounted.rerender(operation("cancelling"));
    expect(document.body.textContent).toContain("已收到停止请求，正在到达安全点");
    expect(findButton("停止任务")).toBeNull();

    await mounted.rerender(operation("recovery_required"));
    expect(document.body.textContent).toContain("任务等待安全恢复");
    await clickButton("恢复任务");
    expect(recovered).toEqual(["op-001"]);
    expect(document.body.textContent).not.toContain("执行成功，请耐心等待");

    await mounted.rerender({ ...operation("recovery_required"), recoveryAction: "require_operator" });
    expect(document.body.textContent).toContain("任务需要人工核查");
    expect(findButton("恢复任务")).toBeNull();
});

test("active panel renders durable stage, heartbeat, service state, and warnings", async () => {
    await mountPanel({
        ...operation("running"),
        stage: "backing_up",
        phase: "创建一致性备份",
        serviceState: "maintenance",
        heartbeatAt: "2026-08-25T08:00:00Z",
        warnings: [{ code: "controller_handoff_failed", message: "候选控制器未通过验活，旧控制器已恢复" }],
    });

    expect(document.body.textContent).toContain("创建一致性备份");
    expect(document.body.textContent).toContain("维护中");
    expect(document.body.textContent).toContain("最近心跳");
    expect(document.body.textContent).toContain("候选控制器未通过验活，旧控制器已恢复");
});

test("public verification is presented independently", () => {
    expect(presentPublicVerification({ status: "not_run", operationId: "", checkedAt: null, errorCode: "", error: "" }).label).toBe("未执行");
    expect(presentPublicVerification({ status: "succeeded", operationId: "op-v", checkedAt: "2026-08-25T00:00:00Z", errorCode: "", error: "" }).tone).toBe("success");
    expect(presentPublicVerification({ status: "failed", operationId: "op-v", checkedAt: "2026-08-25T00:00:00Z", errorCode: "public_timeout", error: "timeout" }).tone).toBe("error");
});

test("confirmation retries reuse the idempotency key created with the pending action", async () => {
    const source = await Bun.file(new URL("../src/pages/admin/operations/operations-page.tsx", import.meta.url)).text();
    const submitAction = source.slice(source.indexOf("const submitAction"), source.indexOf("const operationColumns"));

    expect(source).toContain("idempotencyKey: crypto.randomUUID()");
    expect(submitAction).toContain("pendingAction.idempotencyKey");
    expect(submitAction).not.toContain("crypto.randomUUID()");
});

test("reopening stop or recovery controls reuses one durable command identity", () => {
    expect(operationControlIdempotencyKey("cancel", "op-001")).toBe("control-cancel-op-001");
    expect(operationControlIdempotencyKey("recover", "op-001")).toBe("control-recover-op-001");
});

function operation(status: OperationsRecord["status"]): OperationsRecord {
    return {
        id: "op-001",
        action: "upgrade",
        targetVersion: "v1.0.58",
        currentVersionAtStart: "v1.0.57",
        status,
        stage: status === "recovery_required" ? "restoring_current" : "online_preflight",
        phase: status === "recovery_required" ? "需要管理员恢复" : "在线预检",
        runnerVersion: "v1.0.58",
        runnerDigest: "example.invalid/runner@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
        runnerGeneration: 1,
        heartbeatAt: "2026-08-25T08:00:00Z",
        serviceState: status === "recovery_required" ? "unknown" : "current_online",
        checkpointSequence: 3,
        recoveryAction: status === "recovery_required" ? "restore_current" : "none",
        controllerHandoff: "unchanged",
        warnings: [],
        actorUserId: "admin-1",
        actorDisplayName: "管理员",
        idempotencyKey: "operation-create-0001",
        createdAt: "2026-08-25T07:59:00Z",
        startedAt: "2026-08-25T07:59:01Z",
        updatedAt: "2026-08-25T08:00:00Z",
    };
}

async function mountPanel(
    record: OperationsRecord,
    callbacks: {
        onCancel: (record: OperationsRecord) => Promise<void>;
        onRecover: (record: OperationsRecord) => Promise<void>;
    } = { onCancel: async () => undefined, onRecover: async () => undefined },
) {
    const host = document.createElement("div");
    document.body.append(host);
    root = createRoot(host);
    const render = async (next: OperationsRecord) => {
        await act(async () => {
            root?.render(createElement(App, null, createElement(ActivePanel, { operation: next, submitting: false, onViewLogs: () => undefined, ...callbacks })));
        });
    };
    await render(record);
    return { rerender: render };
}

function findButton(label: string) {
    return [...document.querySelectorAll("button")].find((item) => item.textContent?.replace(/\s+/g, "").includes(label.replace(/\s+/g, ""))) ?? null;
}

async function clickButton(label: string) {
    const match = findButton(label);
    if (!(match instanceof HTMLButtonElement)) throw new Error(`未找到按钮：${label}`);
    await act(async () => match.click());
}
