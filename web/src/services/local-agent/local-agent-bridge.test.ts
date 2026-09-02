import assert from "node:assert/strict";
import { describe, it } from "node:test";

import { parseAgentCapabilityArguments } from "@/services/api/agent-capabilities";
import type { AgentExpectedDelivery, AgentLocalRuntimeClient, AgentRuntimeStartConfiguration, AgentRuntimeView } from "@/services/api/agent-runtime";
import { AgentRuntimeRequestError } from "@/services/api/agent-runtime";

import { LocalAgentBridge } from "./local-agent-bridge";
import type { LocalAgentHttpClient, LocalAgentStartTurnInput, LocalAgentToolResultInput } from "./local-agent-client";
import type { LocalAgentEvent, LocalAgentToolCallEvent } from "./local-agent-contracts";

const configuration: AgentRuntimeStartConfiguration = { generationModels: {}, skillDirs: [], attachments: [], executionMode: "guided" };
const delivery: AgentExpectedDelivery = { kind: "canvas_change", targetCanvasId: "canvas-1", completionCriteria: [{ fact: "canvas_revision" }] };
const toolEvent: LocalAgentToolCallEvent = {
    protocolVersion: 1,
    kind: "tool_call",
    requestId: "tool-request-1",
    threadId: "thread-1",
    turnId: "turn-1",
    toolName: "canvas.read",
    arguments: { canvasId: "canvas-1", selectedNodeIds: [], includeViewport: true },
    expectedDelivery: delivery,
    createdAt: "2026-09-01T00:00:00.000Z",
};

class FakeLocalClient {
    readonly calls: string[] = [];
    readonly deliveries: LocalAgentToolResultInput[] = [];
    readonly starts: LocalAgentStartTurnInput[] = [];
    cancelFailure: Error | undefined;
    deliveryFailuresRemaining = 0;

    async startTurn(input: LocalAgentStartTurnInput) {
        this.calls.push("local.start");
        this.starts.push(input);
        return { threadId: "thread-1", turnId: "turn-1" };
    }

    async deliverToolResult(input: LocalAgentToolResultInput) {
        this.calls.push("local.deliver");
        if (this.deliveryFailuresRemaining > 0) {
            this.deliveryFailuresRemaining -= 1;
            throw new Error("local delivery transport failed");
        }
        this.deliveries.push(input);
    }

    async cancelTurn() {
        this.calls.push("local.cancel");
        if (this.cancelFailure) throw this.cancelFailure;
    }
}

function runtimeView(
    options: {
        status?: AgentRuntimeView["state"]["status"];
        stateVersion?: number;
        lastToolResult?: AgentRuntimeView["state"]["lastToolResult"];
        pendingApproval?: boolean;
        pendingTool?: boolean;
        failureCode?: string;
        decisionFeedback?: AgentRuntimeView["state"]["decisionFeedback"];
        expectedDelivery?: AgentRuntimeView["state"]["expectedDelivery"];
        verification?: AgentRuntimeView["state"]["verification"];
    } = {},
): AgentRuntimeView {
    const status = options.status ?? "running";
    const state: AgentRuntimeView["state"] = {
        stateVersion: options.stateVersion ?? 2,
        stepNumber: 0,
        maxSteps: 8,
        status,
        clarificationHistory: [],
        userMessage: "读取画布",
        configuration: { generationModels: {}, skills: [], attachments: [], executionMode: "guided" },
    };
    if (options.lastToolResult) state.lastToolResult = options.lastToolResult;
    if (options.failureCode) state.failureCode = options.failureCode;
    if (options.decisionFeedback) state.decisionFeedback = options.decisionFeedback;
    if (options.expectedDelivery) state.expectedDelivery = options.expectedDelivery;
    if (options.verification) state.verification = options.verification;
    const view: AgentRuntimeView = {
        run: {
            id: "run-1",
            threadId: "audit-thread-1",
            reasoningHost: "local_codex",
            actorUserId: "user-1",
            clientRequestId: "turn-request-1",
            status,
            lastEventSequence: 1,
            stateVersion: state.stateVersion,
            stepNumber: state.stepNumber,
            maxSteps: state.maxSteps,
            modelRecordId: "",
            modelKey: "",
            toolSchemaVersion: 8,
            runtimeVersion: 5,
            policyVersion: 5,
            createdAt: "2026-09-01T00:00:00.000Z",
            updatedAt: "2026-09-01T00:00:00.000Z",
        },
        state,
    };
    if (options.pendingApproval || options.pendingTool) {
        const call = { toolCallId: toolEvent.requestId, toolName: toolEvent.toolName, actionVersion: 1, arguments: parseAgentCapabilityArguments(toolEvent.toolName, toolEvent.arguments), expectedDelivery: delivery } as const;
        state.pendingToolCall = call;
        if (options.pendingTool) state.pendingToolStarted = true;
        if (!options.pendingApproval) return view;
        view.pendingApproval = {
            toolCallId: call.toolCallId,
            toolName: call.toolName,
            actionVersion: 1,
            proposalHash: "proposal-hash",
            expiresAt: "2026-09-01T01:00:00.000Z",
            effect: { kind: "canvas_mutation", summary: "更新画布", targetIds: ["canvas-1"] },
        };
    }
    return view;
}

function makeBridge(options: {
    local?: FakeLocalClient;
    startRun?: AgentLocalRuntimeClient["startRun"];
    startView?: AgentRuntimeView;
    startFailure?: Error;
    submitDecision: AgentLocalRuntimeClient["submitDecision"];
    interrupt?: (runId: string, input: { expectedStateVersion: number }) => Promise<AgentRuntimeView>;
    before?: () => Promise<void>;
    onToolResult?: ConstructorParameters<typeof LocalAgentBridge>[0]["onToolResult"];
}) {
    const local = options.local ?? new FakeLocalClient();
    const runtime: AgentLocalRuntimeClient = {
        startRun:
            options.startRun ??
            (async () => {
                local.calls.push("backend.start");
                if (options.startFailure) throw options.startFailure;
                return options.startView ?? runtimeView();
            }),
        submitDecision: options.submitDecision,
        interrupt: options.interrupt ?? (async () => runtimeView({ status: "cancelled", stateVersion: 3 })),
    };
    const bridge = new LocalAgentBridge({
        canvasId: "canvas-1",
        localClient: local as Pick<LocalAgentHttpClient, "startTurn" | "deliverToolResult" | "cancelTurn">,
        runtimeClient: runtime,
        configuration,
        maxSteps: 8,
        beforeToolProposal: options.before ?? (async () => {}),
        onToolResult: options.onToolResult,
    });
    return { bridge, local };
}

describe("LocalAgentBridge", () => {
    it("reports both activation and cancellation failures instead of hiding cleanup failure", async () => {
        const local = new FakeLocalClient();
        local.cancelFailure = new Error("cancel transport failed");
        const { bridge } = makeBridge({
            local,
            startFailure: new Error("backend activation failed"),
            submitDecision: async () => runtimeView(),
        });

        await assert.rejects(
            bridge.start({ requestId: "turn-request-1", message: "读取画布", attachments: [] }),
            (cause: unknown) => cause instanceof AggregateError && cause.message.includes("backend activation failed") && cause.message.includes("cancel transport failed"),
        );
        assert.deepEqual(local.calls, ["local.start", "backend.start", "local.cancel"]);
        assert.equal(bridge.state.kind, "failed");
        assert.match(bridge.state.kind === "failed" ? bridge.state.message : "", /cancel transport failed/);
    });

    it("keeps the turn active when an explicit disconnect cannot cancel Codex", async () => {
        const local = new FakeLocalClient();
        const { bridge } = makeBridge({ local, submitDecision: async () => runtimeView() });
        await bridge.start({ requestId: "turn-request-1", message: "读取画布", attachments: [] });
        local.cancelFailure = new Error("cancel transport failed");

        await assert.rejects(bridge.disconnect(), /cancel transport failed/);
        assert.equal(bridge.hasActiveTurn, true);
        assert.deepEqual(bridge.state, {
            kind: "failed",
            code: "local_agent_cancel_failed",
            message: "本机 Codex turn 取消失败：cancel transport failed",
        });
    });

    it("still terminates the audit Run during disposal when local cancellation fails", async () => {
        const local = new FakeLocalClient();
        const interrupts: string[] = [];
        const { bridge } = makeBridge({
            local,
            submitDecision: async () => runtimeView(),
            interrupt: async (runId) => {
                interrupts.push(runId);
                return runtimeView({ status: "cancelled", stateVersion: 3 });
            },
        });
        await bridge.start({ requestId: "turn-request-1", message: "读取画布", attachments: [] });
        local.cancelFailure = new Error("cancel transport failed");

        await assert.rejects(bridge.dispose(), /存在未完成的终止操作/);

        assert.deepEqual(interrupts, ["run-1"]);
        assert.equal(bridge.hasActiveTurn, false);
        assert.equal(bridge.state.kind, "failed");
    });

    it("cleans up both sides when disposal races with audit Run activation", async () => {
        const local = new FakeLocalClient();
        const interrupts: string[] = [];
        let resolveActivation: ((view: AgentRuntimeView) => void) | undefined;
        const { bridge } = makeBridge({
            local,
            startRun: async () => {
                local.calls.push("backend.start");
                return new Promise<AgentRuntimeView>((resolve) => {
                    resolveActivation = resolve;
                });
            },
            submitDecision: async () => runtimeView(),
            interrupt: async (runId) => {
                interrupts.push(runId);
                return runtimeView({ status: "cancelled", stateVersion: 3 });
            },
        });

        const start = bridge.start({ requestId: "turn-request-1", message: "读取画布", attachments: [] });
        await Promise.resolve();
        await bridge.dispose();
        resolveActivation?.(runtimeView());

        await assert.rejects(start, /工作区已卸载/);
        assert.deepEqual(local.calls, ["local.start", "backend.start", "local.cancel"]);
        assert.deepEqual(interrupts, ["run-1"]);
        assert.equal(bridge.hasActiveTurn, false);
    });

    it("cancels both the local Codex turn and canonical audit run on disconnect", async () => {
        const interrupts: Array<{ runId: string; expectedStateVersion: number }> = [];
        const { bridge, local } = makeBridge({
            submitDecision: async () => runtimeView(),
            interrupt: async (runId, input) => {
                interrupts.push({ runId, expectedStateVersion: input.expectedStateVersion });
                return runtimeView({ status: "cancelled", stateVersion: 3 });
            },
        });
        await bridge.start({ requestId: "turn-request-1", message: "读取画布", attachments: [] });

        await bridge.disconnect("用户已断开本机 Agent");

        assert.deepEqual(local.calls, ["local.start", "backend.start", "local.cancel"]);
        assert.deepEqual(interrupts, [{ runId: "run-1", expectedStateVersion: 2 }]);
        assert.equal(bridge.hasActiveTurn, false);
        assert.deepEqual(bridge.state, {
            kind: "failed",
            code: "local_agent_disconnected",
            message: "用户已断开本机 Agent",
        });
    });

    it("accepts the late terminal acknowledgement after an explicit disconnect", async () => {
        const { bridge } = makeBridge({ submitDecision: async () => runtimeView() });
        await bridge.start({ requestId: "turn-request-1", message: "读取画布", attachments: [] });
        await bridge.disconnect("用户已断开本机 Agent");

        await bridge.handleEvent({ kind: "turn_cancelled", threadId: "thread-1", turnId: "turn-1" });

        assert.equal(bridge.hasActiveTurn, false);
        assert.equal(bridge.state.kind, "failed");
    });

    it("ignores a late terminal acknowledgement after the next turn has started", async () => {
        const local = new FakeLocalClient();
        let turnNumber = 0;
        local.startTurn = async () => {
            turnNumber += 1;
            local.calls.push("local.start");
            return { threadId: "thread-1", turnId: `turn-${turnNumber}` };
        };
        const { bridge } = makeBridge({ local, submitDecision: async () => runtimeView() });
        await bridge.start({ requestId: "turn-request-1", message: "读取画布", attachments: [] });
        await bridge.disconnect("用户已断开本机 Agent");
        await bridge.start({ requestId: "turn-request-2", message: "继续读取画布", attachments: [] });

        await bridge.handleEvent({ kind: "turn_cancelled", threadId: "thread-1", turnId: "turn-1" });

        assert.equal(bridge.hasActiveTurn, true);
        assert.deepEqual(bridge.state, { kind: "reasoning", threadId: "thread-1", turnId: "turn-2", runId: "run-1" });
    });

    it("buffers local lifecycle events until the canonical audit run becomes active", async () => {
        const local = new FakeLocalClient();
        let bridge: LocalAgentBridge;
        let earlyEvent: Promise<void> | undefined;
        local.startTurn = async () => {
            local.calls.push("local.start");
            earlyEvent = bridge.handleEvent(toolEvent);
            return { threadId: "thread-1", turnId: "turn-1" };
        };
        let decisions = 0;
        ({ bridge } = makeBridge({
            local,
            submitDecision: async () => {
                decisions += 1;
                return runtimeView({
                    stateVersion: 4,
                    lastToolResult: { toolCallId: "tool-request-1", actionVersion: 1, succeeded: true, output: { revision: 7 } },
                });
            },
        }));

        await bridge.start({ requestId: "turn-request-1", message: "读取画布", attachments: [] });
        await earlyEvent;
        assert.equal(decisions, 1);
        assert.deepEqual(local.deliveries[0]?.output, { revision: 7 });
    });

    it("starts Codex before creating the canonical local audit run", async () => {
        const { bridge, local } = makeBridge({ submitDecision: async () => runtimeView() });
        await bridge.start({ requestId: "turn-request-1", message: "读取画布", attachments: [] });
        assert.deepEqual(local.calls, ["local.start", "backend.start"]);
        assert.deepEqual(bridge.state, { kind: "reasoning", threadId: "thread-1", turnId: "turn-1", runId: "run-1" });
    });

    it("interrupts the canonical audit run when the local Codex turn fails", async () => {
        const interrupts: Array<{ runId: string; expectedStateVersion: number }> = [];
        const { bridge } = makeBridge({
            submitDecision: async () => runtimeView(),
            interrupt: async (runId, input) => {
                interrupts.push({ runId, expectedStateVersion: input.expectedStateVersion });
                return runtimeView({ status: "cancelled", stateVersion: 3 });
            },
        });
        await bridge.start({ requestId: "turn-request-1", message: "读取画布", attachments: [] });

        await bridge.handleEvent({
            kind: "turn_failed",
            threadId: "thread-1",
            turnId: "turn-1",
            message: "Codex 子进程异常退出",
        });

        assert.deepEqual(interrupts, [{ runId: "run-1", expectedStateVersion: 2 }]);
        assert.equal(bridge.hasActiveTurn, false);
        assert.deepEqual(bridge.state, {
            kind: "failed",
            code: "local_codex_turn_failed",
            message: "Codex 子进程异常退出",
        });
    });

    it("retries only the audit interruption after a terminal local failure", async () => {
        const local = new FakeLocalClient();
        let interruptAttempts = 0;
        const { bridge } = makeBridge({
            local,
            submitDecision: async () => runtimeView(),
            interrupt: async () => {
                interruptAttempts += 1;
                if (interruptAttempts === 1) throw new Error("backend interrupt unavailable");
                return runtimeView({ status: "cancelled", stateVersion: 3 });
            },
        });
        await bridge.start({ requestId: "turn-request-1", message: "读取画布", attachments: [] });

        await assert.rejects(
            bridge.handleEvent({
                kind: "turn_failed",
                threadId: "thread-1",
                turnId: "turn-1",
                message: "Codex 子进程异常退出",
            }),
            /backend interrupt unavailable/,
        );
        local.cancelFailure = new Error("turn not active");

        await bridge.disconnect("重试清理审计 Run");

        assert.equal(interruptAttempts, 2);
        assert.deepEqual(local.calls, ["local.start", "backend.start"]);
        assert.equal(bridge.hasActiveTurn, false);
    });

    it("interrupts the canonical audit run when local Codex is cancelled", async () => {
        const interrupts: string[] = [];
        const { bridge } = makeBridge({
            submitDecision: async () => runtimeView(),
            interrupt: async (runId) => {
                interrupts.push(runId);
                return runtimeView({ status: "cancelled", stateVersion: 3 });
            },
        });
        await bridge.start({ requestId: "turn-request-1", message: "读取画布", attachments: [] });

        await bridge.handleEvent({ kind: "turn_cancelled", threadId: "thread-1", turnId: "turn-1" });

        assert.deepEqual(interrupts, ["run-1"]);
        assert.equal(bridge.hasActiveTurn, false);
        assert.equal(bridge.state.kind, "failed");
    });

    it("interrupts the canonical audit run when Codex completes without a final decision", async () => {
        const interrupts: string[] = [];
        const { bridge } = makeBridge({
            submitDecision: async () => runtimeView(),
            interrupt: async (runId) => {
                interrupts.push(runId);
                return runtimeView({ status: "cancelled", stateVersion: 3 });
            },
        });
        await bridge.start({ requestId: "turn-request-1", message: "读取画布", attachments: [] });

        await bridge.handleEvent({ kind: "turn_completed", threadId: "thread-1", turnId: "turn-1", event: { type: "turn.completed" } });

        assert.deepEqual(interrupts, ["run-1"]);
        assert.equal(bridge.hasActiveTurn, false);
        assert.deepEqual(bridge.state, {
            kind: "failed",
            code: "local_codex_final_decision_missing",
            message: "本机 Codex turn 未提交结构化最终交付决策",
        });
    });

    it("delivers an L0 authoritative result back to Codex exactly once", async () => {
        let prepared = 0;
        const { bridge, local } = makeBridge({
            before: async () => {
                prepared += 1;
            },
            submitDecision: async () =>
                runtimeView({
                    stateVersion: 4,
                    lastToolResult: { toolCallId: "tool-request-1", actionVersion: 1, succeeded: true, output: { revision: 8 } },
                }),
        });
        await bridge.start({ requestId: "turn-request-1", message: "读取画布", attachments: [] });
        await bridge.handleEvent(toolEvent);
        await bridge.acceptRuntimeView(
            runtimeView({
                stateVersion: 4,
                lastToolResult: { toolCallId: "tool-request-1", actionVersion: 1, succeeded: true, output: { revision: 8 } },
            }),
        );
        assert.equal(prepared, 1);
        assert.equal(local.deliveries.length, 1);
        assert.deepEqual(local.deliveries[0], { requestId: "tool-request-1", threadId: "thread-1", turnId: "turn-1", toolName: "canvas.read", succeeded: true, output: { revision: 8 } });
    });

    it("retries the unchanged authoritative result after a transient local delivery failure", async () => {
        const local = new FakeLocalClient();
        local.deliveryFailuresRemaining = 1;
        const authoritative = runtimeView({
            stateVersion: 4,
            lastToolResult: { toolCallId: "tool-request-1", actionVersion: 1, succeeded: true, output: { revision: 8 } },
        });
        const { bridge } = makeBridge({
            local,
            submitDecision: async () => authoritative,
        });
        await bridge.start({ requestId: "turn-request-1", message: "读取画布", attachments: [] });

        await assert.rejects(bridge.handleEvent(toolEvent), /local delivery transport failed/);
        assert.equal(local.deliveries.length, 0);
        assert.deepEqual(bridge.state, {
            kind: "failed",
            code: "local_agent_result_delivery_failed",
            message: "工具结果回传本机 Agent 失败：local delivery transport failed",
        });

        await bridge.acceptRuntimeView(authoritative);
        assert.equal(local.deliveries.length, 1);
        assert.equal(local.deliveries[0]?.succeeded, true);
        assert.deepEqual(local.deliveries[0]?.output, { revision: 8 });
        assert.equal(bridge.state.kind, "reasoning");
    });

    it("returns canvas synchronization failure to Codex without creating a backend tool decision", async () => {
        let decisions = 0;
        const { bridge, local } = makeBridge({
            before: async () => {
                throw new Error("canvas flush failed");
            },
            submitDecision: async () => {
                decisions += 1;
                return runtimeView();
            },
        });
        await bridge.start({ requestId: "turn-request-1", message: "读取画布", attachments: [] });
        await bridge.handleEvent(toolEvent);

        assert.equal(decisions, 0);
        assert.equal(local.deliveries.length, 1);
        assert.equal(local.deliveries[0]?.succeeded, false);
        assert.equal(local.deliveries[0]?.errorCode, "local_agent_canvas_sync_failed");
        assert.match(local.deliveries[0]?.errorMessage ?? "", /canvas flush failed/);
        assert.equal(bridge.state.kind, "reasoning");
    });

    it("returns structurally invalid tool arguments to Codex before syncing or submitting", async () => {
        let prepared = 0;
        let decisions = 0;
        const { bridge, local } = makeBridge({
            before: async () => {
                prepared += 1;
            },
            submitDecision: async () => {
                decisions += 1;
                return runtimeView();
            },
        });
        await bridge.start({ requestId: "turn-request-1", message: "读取画布", attachments: [] });
        await bridge.handleEvent({ ...toolEvent, arguments: {} });

        assert.equal(prepared, 0);
        assert.equal(decisions, 0);
        assert.equal(local.deliveries.length, 1);
        assert.equal(local.deliveries[0]?.errorCode, "local_agent_tool_arguments_invalid");
        assert.equal(bridge.state.kind, "reasoning");
    });

    it("waits for approval and delivers the resolved tool result once", async () => {
        const results: Array<{ toolName: string; succeeded: boolean; output: Record<string, unknown> }> = [];
        const { bridge, local } = makeBridge({
            submitDecision: async () => runtimeView({ status: "waiting_approval", stateVersion: 3, pendingApproval: true }),
            onToolResult: (result) => results.push(result),
        });
        await bridge.start({ requestId: "turn-request-1", message: "更新画布", attachments: [] });
        await bridge.handleEvent(toolEvent);
        assert.equal(local.deliveries.length, 0);
        assert.equal(bridge.state.kind, "waiting_approval");

        const approved = runtimeView({
            stateVersion: 5,
            lastToolResult: { toolCallId: "tool-request-1", actionVersion: 1, succeeded: true, output: { revision: 9 } },
        });
        await bridge.acceptRuntimeView(approved);
        await bridge.acceptRuntimeView(approved);
        assert.equal(local.deliveries.length, 1);
        assert.deepEqual(local.deliveries[0]?.output, { revision: 9 });
        assert.deepEqual(results, [{ toolName: "canvas.read", succeeded: true, output: { revision: 9 } }]);
    });

    it("keeps an approved asynchronous tool pending until an authoritative result arrives", async () => {
        const { bridge, local } = makeBridge({ submitDecision: async () => runtimeView({ status: "waiting_approval", stateVersion: 3, pendingApproval: true }) });
        await bridge.start({ requestId: "turn-request-1", message: "更新画布", attachments: [] });
        await bridge.handleEvent(toolEvent);
        await bridge.acceptRuntimeView(runtimeView({ status: "waiting_tool", stateVersion: 4, pendingTool: true }));
        assert.equal(local.deliveries.length, 0);
        assert.equal(bridge.state.kind, "delivering_result");

        await bridge.acceptRuntimeView(
            runtimeView({
                stateVersion: 5,
                lastToolResult: { toolCallId: "tool-request-1", actionVersion: 1, succeeded: true, output: { revision: 10 } },
            }),
        );
        assert.equal(local.deliveries.length, 1);
        assert.deepEqual(local.deliveries[0]?.output, { revision: 10 });
    });

    it("returns rejection facts to Codex instead of treating approval rejection as success", async () => {
        const { bridge, local } = makeBridge({ submitDecision: async () => runtimeView({ status: "waiting_approval", stateVersion: 3, pendingApproval: true }) });
        await bridge.start({ requestId: "turn-request-1", message: "更新画布", attachments: [] });
        await bridge.handleEvent(toolEvent);
        await bridge.acceptRuntimeView(
            runtimeView({
                stateVersion: 4,
                lastToolResult: { toolCallId: "tool-request-1", actionVersion: 1, succeeded: false, output: {}, errorCode: "agent_tool_rejected" },
            }),
        );
        assert.equal(local.deliveries.length, 1);
        assert.equal(local.deliveries[0]?.succeeded, false);
        assert.equal(local.deliveries[0]?.errorCode, "agent_tool_rejected");
    });

    it("does not retry a conflicting external decision", async () => {
        let decisions = 0;
        const { bridge, local } = makeBridge({
            submitDecision: async () => {
                decisions += 1;
                throw new AgentRuntimeRequestError("状态冲突", 409, "agent_external_decision_conflict", 4);
            },
        });
        await bridge.start({ requestId: "turn-request-1", message: "读取画布", attachments: [] });
        await bridge.handleEvent(toolEvent);
        assert.equal(decisions, 1);
        assert.equal(local.deliveries.length, 1);
        assert.equal(local.deliveries[0]?.succeeded, false);
        assert.equal(local.deliveries[0]?.errorCode, "agent_external_decision_conflict");
        assert.equal(bridge.state.kind, "failed");
    });

    it("submits the structured final decision without inferring delivery", async () => {
        const decisions: Array<Parameters<AgentLocalRuntimeClient["submitDecision"]>[1]> = [];
        const { bridge } = makeBridge({
            submitDecision: async (_runId, input) => {
                decisions.push(input);
                return runtimeView({ status: "succeeded", stateVersion: 3 });
            },
        });
        await bridge.start({ requestId: "turn-request-1", message: "说明结果", attachments: [] });
        const event: LocalAgentEvent = { kind: "final_decision", threadId: "thread-1", turnId: "turn-1", message: "已完成", expectedDelivery: delivery };
        await bridge.handleEvent(event);
        assert.deepEqual(decisions[0]?.decision, { kind: "final", final: { message: "已完成", expectedDelivery: delivery } });
        assert.equal(bridge.state.kind, "reasoning");
        assert.equal(bridge.hasActiveTurn, true);
        await bridge.handleEvent({ kind: "turn_completed", threadId: "thread-1", turnId: "turn-1", event: { type: "turn.completed" } });
        assert.equal(bridge.state.kind, "idle");
        assert.equal(bridge.hasActiveTurn, false);
    });

    it("continues a repairable final decision in the same Codex thread without interrupting the audit run", async () => {
        const local = new FakeLocalClient();
        let localTurn = 0;
        local.startTurn = async (input) => {
            local.calls.push("local.start");
            local.starts.push(input);
            localTurn += 1;
            return { threadId: "thread-1", turnId: `turn-${localTurn}` };
        };
        const interrupts: string[] = [];
        const decisions: Array<Parameters<AgentLocalRuntimeClient["submitDecision"]>[1]> = [];
        const { bridge } = makeBridge({
            local,
            submitDecision: async (_runId, input) => {
                decisions.push(input);
                return decisions.length === 1
                    ? runtimeView({
                          stateVersion: 3,
                          expectedDelivery: delivery,
                          verification: {
                              status: "repairable",
                              rationale: "当前画布交付证据缺少最新修订",
                              missingCriteria: [{ fact: "canvas_revision" }],
                          },
                      })
                    : runtimeView({ status: "succeeded", stateVersion: 4, expectedDelivery: delivery, verification: { status: "satisfied", rationale: "已满足" } });
            },
            interrupt: async (runId) => {
                interrupts.push(runId);
                return runtimeView({ status: "cancelled", stateVersion: 5 });
            },
        });

        await bridge.start({ requestId: "turn-request-1", message: "生成完整视频", attachments: [] });
        await bridge.handleEvent({ kind: "final_decision", threadId: "thread-1", turnId: "turn-1", message: "已完成", expectedDelivery: delivery });
        await bridge.handleEvent({ kind: "turn_completed", threadId: "thread-1", turnId: "turn-1", event: { type: "turn.completed" } });

        assert.equal(interrupts.length, 0);
        assert.equal(local.starts.length, 2);
        assert.equal(local.starts[1]?.threadId, "thread-1");
        assert.equal(local.starts[1]?.ephemeral, true);
        assert.match(local.starts[1]?.message ?? "", /canvas_revision/);
        assert.deepEqual(bridge.state, { kind: "reasoning", threadId: "thread-1", turnId: "turn-2", runId: "run-1" });

        await bridge.handleEvent({ kind: "final_decision", threadId: "thread-1", turnId: "turn-2", message: "已校验完成", expectedDelivery: delivery });
        await bridge.handleEvent({ kind: "turn_completed", threadId: "thread-1", turnId: "turn-2", event: { type: "turn.completed" } });
        assert.equal(bridge.state.kind, "idle");
        assert.equal(bridge.hasActiveTurn, false);
    });

    it("reports cleanup failures when a repair continuation violates thread identity", async () => {
        const local = new FakeLocalClient();
        let localTurn = 0;
        local.startTurn = async (input) => {
            local.calls.push("local.start");
            local.starts.push(input);
            localTurn += 1;
            return localTurn === 1 ? { threadId: "thread-1", turnId: "turn-1" } : { threadId: "thread-other", turnId: "turn-2" };
        };
        const { bridge } = makeBridge({
            local,
            submitDecision: async () =>
                runtimeView({
                    stateVersion: 3,
                    expectedDelivery: delivery,
                    verification: { status: "repairable", rationale: "缺少当前修订", missingCriteria: [{ fact: "canvas_revision" }] },
                }),
            interrupt: async () => runtimeView({ status: "cancelled", stateVersion: 4 }),
        });
        await bridge.start({ requestId: "turn-request-1", message: "生成完整视频", attachments: [] });
        await bridge.handleEvent({ kind: "final_decision", threadId: "thread-1", turnId: "turn-1", message: "已完成", expectedDelivery: delivery });
        local.cancelFailure = new Error("mismatched turn could not be cancelled");

        await assert.rejects(
            bridge.handleEvent({ kind: "turn_completed", threadId: "thread-1", turnId: "turn-1", event: { type: "turn.completed" } }),
            (cause: unknown) => cause instanceof AggregateError && cause.message.includes("线程归属冲突") && cause.message.includes("could not be cancelled"),
        );
        assert.equal(bridge.hasActiveTurn, true);
        assert.equal(bridge.state.kind, "failed");
    });
});
