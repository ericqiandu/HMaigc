import { parseAgentCapabilityArguments } from "@/services/api/agent-capabilities";
import { AgentRuntimeRequestError, type AgentLocalRuntimeClient, type AgentRuntimeStartConfiguration, type AgentRuntimeView } from "@/services/api/agent-runtime";

import type { LocalAgentHttpClient, LocalAgentStartTurnInput, LocalAgentToolResultInput } from "./local-agent-client";
import type { LocalAgentEvent, LocalAgentFinalDecisionEvent, LocalAgentToolCallEvent } from "./local-agent-contracts";

export type LocalAgentBridgeState =
    | { kind: "idle" }
    | { kind: "reasoning"; threadId: string; turnId: string; runId: string }
    | { kind: "waiting_approval"; threadId: string; turnId: string; runId: string; toolCallId: string }
    | { kind: "delivering_result"; threadId: string; turnId: string; runId: string; requestId: string }
    | { kind: "failed"; code: string; message: string };

type LocalClientPort = Pick<LocalAgentHttpClient, "startTurn" | "deliverToolResult" | "cancelTurn">;
export type LocalAgentAuthoritativeToolResult = {
    toolName: LocalAgentToolCallEvent["toolName"];
    succeeded: boolean;
    output: Record<string, unknown>;
};

export type LocalAgentBridgeOptions = {
    canvasId: string;
    localClient: LocalClientPort;
    runtimeClient: AgentLocalRuntimeClient;
    configuration: AgentRuntimeStartConfiguration;
    maxSteps: number;
    beforeToolProposal: (event: LocalAgentToolCallEvent) => Promise<void>;
    onRuntimeView?: (view: AgentRuntimeView) => void;
    onToolResult?: (result: LocalAgentAuthoritativeToolResult) => void;
    onState?: (state: LocalAgentBridgeState) => void;
};

type ActiveBridgeRun = {
    threadId: string;
    turnId: string;
    runId: string;
    view: AgentRuntimeView;
    pendingTool?: LocalAgentToolCallEvent;
    finalSubmitted: boolean;
    localTerminal: boolean;
    repairInstruction?: string;
};

export class LocalAgentBridge {
    readonly #options: LocalAgentBridgeOptions;
    readonly #deliveredRequests = new Set<string>();
    readonly #reportedToolResults = new Set<string>();
    readonly #activation: Promise<void>;
    readonly #resolveActivation: () => void;
    readonly #rejectActivation: (cause: unknown) => void;
    #active: ActiveBridgeRun | undefined;
    #state: LocalAgentBridgeState = { kind: "idle" };
    #eventQueue: Promise<void> = Promise.resolve();
    #activationSettled = false;
    #closedTurn: Pick<ActiveBridgeRun, "threadId" | "turnId"> | undefined;
    #disposedReason: string | undefined;

    constructor(options: LocalAgentBridgeOptions) {
        this.#options = options;
        let resolveActivation: (() => void) | undefined;
        let rejectActivation: ((cause: unknown) => void) | undefined;
        this.#activation = new Promise<void>((resolve, reject) => {
            resolveActivation = resolve;
            rejectActivation = reject;
        });
        this.#resolveActivation = () => resolveActivation?.();
        this.#rejectActivation = (cause) => rejectActivation?.(cause);
        void this.#activation.catch(() => undefined);
    }

    get state(): LocalAgentBridgeState {
        return this.#state;
    }

    get hasActiveTurn(): boolean {
        return Boolean(this.#active);
    }

    async start(input: Omit<LocalAgentStartTurnInput, "canvasId">): Promise<AgentRuntimeView> {
        if (this.#disposedReason) throw new Error(this.#disposedReason);
        if (this.#active) throw new Error("本机 Agent 已有活动 turn");
        let local: Awaited<ReturnType<LocalClientPort["startTurn"]>>;
        try {
            local = await this.#options.localClient.startTurn({ ...input, canvasId: this.#options.canvasId });
        } catch (cause) {
            this.#settleActivation(cause);
            this.#fail(errorCode(cause), errorMessage(cause, "本机 Codex turn 创建失败"));
            throw cause;
        }
        if (this.#disposedReason) {
            const failure = await this.#cancelAfterActivationFailure(local.turnId, new Error(this.#disposedReason));
            this.#settleActivation(failure);
            this.#fail("local_agent_disposed", errorMessage(failure, this.#disposedReason));
            throw failure;
        }

        let view: AgentRuntimeView;
        try {
            view = await this.#options.runtimeClient.startRun({
                canvasId: this.#options.canvasId,
                externalThreadId: local.threadId,
                clientRequestId: input.requestId,
                userMessage: input.message,
                maxSteps: this.#options.maxSteps,
                configuration: this.#options.configuration,
            });
        } catch (cause) {
            const failure = await this.#cancelAfterActivationFailure(local.turnId, cause);
            this.#settleActivation(failure);
            this.#fail(errorCode(failure), errorMessage(failure, "后端审计 Run 创建失败"));
            throw failure;
        }
        this.#active = { threadId: local.threadId, turnId: local.turnId, runId: view.run.id, view, finalSubmitted: false, localTerminal: false };
        if (this.#disposedReason) {
            try {
                await this.#disposeActive(this.#active, this.#disposedReason);
            } catch (cause) {
                this.#settleActivation(cause);
                throw cause;
            }
            const failure = new Error(this.#disposedReason);
            this.#settleActivation(failure);
            throw failure;
        }
        this.#settleActivation();
        this.#adoptView(view);
        this.#setState({ kind: "reasoning", threadId: local.threadId, turnId: local.turnId, runId: view.run.id });
        return view;
    }

    handleEvent(event: LocalAgentEvent): Promise<void> {
        if (event.kind === "connected") return Promise.resolve();
        const scopedEvent: Exclude<LocalAgentEvent, { kind: "connected" }> = event;
        const operation = this.#eventQueue.then(async () => {
            await this.#activation;
            if (this.#isLateTerminalEvent(scopedEvent)) return;
            await this.#handleEvent(scopedEvent);
        });
        this.#eventQueue = operation.catch(() => {});
        return operation;
    }

    acceptRuntimeView(view: AgentRuntimeView): Promise<void> {
        const operation = this.#eventQueue.then(() => this.#acceptRuntimeView(view));
        this.#eventQueue = operation.catch(() => {});
        return operation;
    }

    async disconnect(message = "本机 Agent 事件连接已断开"): Promise<void> {
        const active = this.#active;
        if (active) {
            if (!active.localTerminal) {
                try {
                    await this.#options.localClient.cancelTurn(active.turnId);
                    active.localTerminal = true;
                } catch (cause) {
                    const cancelMessage = `本机 Codex turn 取消失败：${errorMessage(cause, "未知取消错误")}`;
                    this.#fail("local_agent_cancel_failed", cancelMessage);
                    throw new Error(cancelMessage, { cause });
                }
            }
            await this.#interruptAuditRun(active);
            this.#closedTurn = { threadId: active.threadId, turnId: active.turnId };
        }
        this.#active = undefined;
        this.#settleActivation(new Error(message));
        this.#fail("local_agent_disconnected", message);
    }

    async dispose(message = "本机 Agent 工作区已卸载"): Promise<void> {
        this.#disposedReason ??= message;
        this.#settleActivation(new Error(this.#disposedReason));
        const active = this.#active;
        if (!active) {
            this.#fail("local_agent_disposed", this.#disposedReason);
            return;
        }
        await this.#disposeActive(active, this.#disposedReason);
    }

    async #disposeActive(active: ActiveBridgeRun, message: string): Promise<void> {
        const failures: Error[] = [];
        if (!active.localTerminal) {
            try {
                await this.#options.localClient.cancelTurn(active.turnId);
                active.localTerminal = true;
            } catch (cause) {
                failures.push(new Error(`本机 Codex turn 取消失败：${errorMessage(cause, "未知取消错误")}`, { cause }));
            }
        }
        try {
            await this.#interruptAuditRun(active);
        } catch (cause) {
            failures.push(new Error(`后端审计 Run 终止失败：${errorMessage(cause, "未知终止错误")}`, { cause }));
        }
        this.#closedTurn = { threadId: active.threadId, turnId: active.turnId };
        this.#active = undefined;
        if (failures.length > 0) {
            const failure = new AggregateError(failures, `${message}时存在未完成的终止操作`);
            this.#fail("local_agent_dispose_failed", failure.message);
            throw failure;
        }
        this.#fail("local_agent_disconnected", message);
    }

    async #handleEvent(event: Exclude<LocalAgentEvent, { kind: "connected" }>): Promise<void> {
        const active = this.#requireActiveEvent(event);
        if (event.kind === "tool_call") {
            await this.#handleToolCall(active, event);
            return;
        }
        if (event.kind === "final_decision") {
            await this.#handleFinalDecision(active, event);
            return;
        }
        if (event.kind === "turn_failed") {
            active.localTerminal = true;
            await this.#interruptAuditRun(active);
            this.#closedTurn = { threadId: active.threadId, turnId: active.turnId };
            this.#active = undefined;
            this.#fail("local_codex_turn_failed", event.message);
            return;
        }
        if (event.kind === "turn_cancelled") {
            active.localTerminal = true;
            await this.#interruptAuditRun(active);
            this.#closedTurn = { threadId: active.threadId, turnId: active.turnId };
            this.#active = undefined;
            if (this.#state.kind !== "failed") this.#fail("local_codex_turn_cancelled", "本机 Codex turn 已取消");
            return;
        }
        if (event.kind === "turn_completed") {
            active.localTerminal = true;
            if (!active.finalSubmitted) {
                await this.#interruptAuditRun(active);
                this.#closedTurn = { threadId: active.threadId, turnId: active.turnId };
                this.#active = undefined;
                this.#fail("local_codex_final_decision_missing", "本机 Codex turn 未提交结构化最终交付决策");
            } else if (active.repairInstruction) {
                await this.#continueRepair(active);
            } else if (!isTerminalStatus(active.view.state.status)) {
                await this.#interruptAuditRun(active);
                this.#closedTurn = { threadId: active.threadId, turnId: active.turnId };
                this.#active = undefined;
                if (this.#state.kind !== "failed") this.#fail("agent_delivery_requires_repair", "后端未提供可执行的交付纠偏事实");
            } else if (this.#state.kind !== "failed") {
                this.#closedTurn = { threadId: active.threadId, turnId: active.turnId };
                this.#active = undefined;
                this.#setState({ kind: "idle" });
            } else {
                this.#closedTurn = { threadId: active.threadId, turnId: active.turnId };
                this.#active = undefined;
            }
        }
    }

    async #handleToolCall(active: ActiveBridgeRun, event: LocalAgentToolCallEvent): Promise<void> {
        if (active.pendingTool) throw new Error("本机 Agent 尚有未完成的工具请求");
        let argumentsValue: ReturnType<typeof parseAgentCapabilityArguments>;
        try {
            argumentsValue = parseAgentCapabilityArguments(event.toolName, event.arguments);
        } catch (cause) {
            await this.#deliverFailure(active, event, "local_agent_tool_arguments_invalid", errorMessage(cause, "本机 Agent 工具参数无效"));
            this.#setState({ kind: "reasoning", threadId: active.threadId, turnId: active.turnId, runId: active.runId });
            return;
        }
        try {
            await this.#options.beforeToolProposal(event);
        } catch (cause) {
            await this.#deliverFailure(active, event, "local_agent_canvas_sync_failed", errorMessage(cause, "当前画布同步失败"));
            this.#setState({ kind: "reasoning", threadId: active.threadId, turnId: active.turnId, runId: active.runId });
            return;
        }
        active.pendingTool = event;
        let view: AgentRuntimeView;
        try {
            view = await this.#options.runtimeClient.submitDecision(active.runId, {
                clientRequestId: await stableDecisionRequestId("tool", event.requestId),
                expectedStateVersion: active.view.state.stateVersion,
                decision: {
                    kind: "tool_call",
                    toolCall: {
                        toolCallId: event.requestId,
                        toolName: event.toolName,
                        actionVersion: 1,
                        arguments: argumentsValue,
                        expectedDelivery: event.expectedDelivery,
                    },
                },
            });
        } catch (cause) {
            await this.#deliverFailure(active, event, errorCode(cause), errorMessage(cause, "后端拒绝了本机 Agent 工具决策"));
            this.#fail(errorCode(cause), errorMessage(cause, "本机 Agent 工具决策失败"));
            return;
        }
        await this.#acceptRuntimeView(view);
    }

    async #handleFinalDecision(active: ActiveBridgeRun, event: LocalAgentFinalDecisionEvent): Promise<void> {
        if (active.pendingTool) throw new Error("工具结果交付前不能提交最终决策");
        const view = await this.#options.runtimeClient.submitDecision(active.runId, {
            clientRequestId: await stableDecisionRequestId("final", event.turnId),
            expectedStateVersion: active.view.state.stateVersion,
            decision: { kind: "final", final: { message: event.message, expectedDelivery: event.expectedDelivery } },
        });
        active.finalSubmitted = true;
        active.view = view;
        this.#adoptView(view);
        if (view.state.status === "succeeded") {
            this.#setState({ kind: "reasoning", threadId: active.threadId, turnId: active.turnId, runId: active.runId });
            return;
        }
        const repairInstruction = buildDeliveryRepairInstruction(view);
        if (repairInstruction) {
            active.repairInstruction = repairInstruction;
            this.#setState({ kind: "reasoning", threadId: active.threadId, turnId: active.turnId, runId: active.runId });
            return;
        }
        const reason = view.state.decisionFeedback?.reason ?? view.state.failureCode ?? `最终决策返回非成功状态 ${view.state.status}`;
        this.#fail("agent_delivery_requires_repair", reason);
    }

    async #continueRepair(active: ActiveBridgeRun): Promise<void> {
        const message = active.repairInstruction;
        if (!message) throw new Error("本机 Agent 缺少交付纠偏事实");
        const closedTurn = { threadId: active.threadId, turnId: active.turnId };
        let local: Awaited<ReturnType<LocalClientPort["startTurn"]>>;
        try {
            local = await this.#options.localClient.startTurn({
                requestId: `local-repair:${active.runId}:${active.view.state.stateVersion}`,
                canvasId: this.#options.canvasId,
                threadId: active.threadId,
                message,
                attachments: [],
                ephemeral: true,
            });
        } catch (cause) {
            await this.#interruptAuditRun(active);
            this.#closedTurn = closedTurn;
            this.#active = undefined;
            this.#fail("local_agent_repair_resume_failed", errorMessage(cause, "本机 Codex 交付纠偏续接失败"));
            throw cause;
        }
        if (local.threadId !== active.threadId) {
            const contractFailure = new Error("本机 Codex 交付纠偏续接线程归属冲突");
            const cleanupFailures: Error[] = [];
            active.threadId = local.threadId;
            active.turnId = local.turnId;
            active.localTerminal = false;
            try {
                await this.#options.localClient.cancelTurn(local.turnId);
                active.localTerminal = true;
            } catch (cause) {
                cleanupFailures.push(new Error(`冲突 Codex turn 取消失败：${errorMessage(cause, "未知取消错误")}`, { cause }));
            }
            try {
                await this.#interruptAuditRun(active);
            } catch (cause) {
                cleanupFailures.push(new Error(`后端审计 Run 终止失败：${errorMessage(cause, "未知终止错误")}`, { cause }));
            }
            this.#closedTurn = closedTurn;
            if (active.localTerminal && isTerminalStatus(active.view.state.status)) this.#active = undefined;
            const failure = cleanupFailures.length === 0 ? contractFailure : new AggregateError([contractFailure, ...cleanupFailures], `${contractFailure.message}，且清理未完全成功：${cleanupFailures.map((item) => item.message).join("；")}`);
            this.#fail("local_agent_repair_thread_conflict", failure.message);
            throw failure;
        }
        this.#closedTurn = closedTurn;
        active.turnId = local.turnId;
        active.finalSubmitted = false;
        active.localTerminal = false;
        active.repairInstruction = undefined;
        this.#setState({ kind: "reasoning", threadId: active.threadId, turnId: active.turnId, runId: active.runId });
    }

    async #acceptRuntimeView(view: AgentRuntimeView): Promise<void> {
        const active = this.#active;
        if (!active || view.run.id !== active.runId || view.run.reasoningHost !== "local_codex") {
            throw new Error("本机 Agent Runtime View 归属冲突");
        }
        active.view = view;
        this.#adoptView(view);
        const pending = active.pendingTool;
        if (!pending) return;
        const result = view.state.lastToolResult;
        if (result && result.toolCallId === pending.requestId && result.actionVersion === 1) {
            await this.#deliverResult(active, pending, result);
            return;
        }
        if (view.state.status === "waiting_approval" && view.state.pendingToolCall?.toolCallId === pending.requestId && view.pendingApproval) {
            this.#setState({ kind: "waiting_approval", threadId: active.threadId, turnId: active.turnId, runId: active.runId, toolCallId: pending.requestId });
            return;
        }
        if (view.state.status === "waiting_tool" && view.state.pendingToolCall?.toolCallId === pending.requestId) {
            this.#setState({ kind: "delivering_result", threadId: active.threadId, turnId: active.turnId, runId: active.runId, requestId: pending.requestId });
            return;
        }
        if (view.state.decisionFeedback) {
            await this.#deliverFailure(active, pending, view.state.decisionFeedback.code, view.state.decisionFeedback.reason);
            this.#setState({ kind: "reasoning", threadId: active.threadId, turnId: active.turnId, runId: active.runId });
            return;
        }
        if (view.state.status === "failed" || view.state.status === "cancelled") {
            const code = view.state.failureCode ?? `agent_run_${view.state.status}`;
            await this.#deliverFailure(active, pending, code, `后端 Agent Run 已${view.state.status === "failed" ? "失败" : "取消"}`);
            this.#fail(code, `后端 Agent Run 已${view.state.status === "failed" ? "失败" : "取消"}`);
            return;
        }
        throw new Error(`后端未返回工具结果或审批事实（状态 ${view.state.status}）`);
    }

    async #deliverResult(active: ActiveBridgeRun, event: LocalAgentToolCallEvent, result: NonNullable<AgentRuntimeView["state"]["lastToolResult"]>): Promise<void> {
        this.#reportToolResult(event, result);
        if (result.succeeded) {
            await this.#deliverOnce(active, event, { succeeded: true, output: result.output });
        } else {
            const code = result.errorCode ?? "agent_tool_failed_without_code";
            await this.#deliverOnce(active, event, { succeeded: false, output: result.output, errorCode: code, errorMessage: `后端工具执行失败：${code}` });
        }
        active.pendingTool = undefined;
        this.#setState({ kind: "reasoning", threadId: active.threadId, turnId: active.turnId, runId: active.runId });
    }

    #reportToolResult(event: LocalAgentToolCallEvent, result: NonNullable<AgentRuntimeView["state"]["lastToolResult"]>): void {
        if (this.#reportedToolResults.has(event.requestId)) return;
        this.#options.onToolResult?.({ toolName: event.toolName, succeeded: result.succeeded, output: result.output });
        this.#reportedToolResults.add(event.requestId);
    }

    async #deliverFailure(active: ActiveBridgeRun, event: LocalAgentToolCallEvent, code: string, message: string): Promise<void> {
        await this.#deliverOnce(active, event, { succeeded: false, output: {}, errorCode: code, errorMessage: message });
        active.pendingTool = undefined;
    }

    async #deliverOnce(active: ActiveBridgeRun, event: LocalAgentToolCallEvent, result: Pick<LocalAgentToolResultInput, "succeeded" | "output" | "errorCode" | "errorMessage">): Promise<void> {
        if (this.#deliveredRequests.has(event.requestId)) return;
        this.#setState({ kind: "delivering_result", threadId: active.threadId, turnId: active.turnId, runId: active.runId, requestId: event.requestId });
        try {
            await this.#options.localClient.deliverToolResult({
                requestId: event.requestId,
                threadId: active.threadId,
                turnId: active.turnId,
                toolName: event.toolName,
                ...result,
            });
            this.#deliveredRequests.add(event.requestId);
        } catch (cause) {
            this.#fail("local_agent_result_delivery_failed", `工具结果回传本机 Agent 失败：${errorMessage(cause, "未知传输错误")}`);
            throw cause;
        }
    }

    #requireActiveEvent(event: Exclude<LocalAgentEvent, { kind: "connected" }>): ActiveBridgeRun {
        const active = this.#active;
        if (!active) throw new Error("本机 Agent 事件没有对应的活动 Run");
        if (event.threadId !== active.threadId || ("turnId" in event && event.turnId !== active.turnId)) {
            throw new Error("本机 Agent 事件归属冲突");
        }
        return active;
    }

    #isLateTerminalEvent(event: Exclude<LocalAgentEvent, { kind: "connected" }>): boolean {
        const closed = this.#closedTurn;
        if (!closed || (event.kind !== "turn_completed" && event.kind !== "turn_failed" && event.kind !== "turn_cancelled") || event.threadId !== closed.threadId || event.turnId !== closed.turnId) {
            return false;
        }
        const active = this.#active;
        return !active || event.threadId !== active.threadId || event.turnId !== active.turnId;
    }

    #adoptView(view: AgentRuntimeView): void {
        this.#options.onRuntimeView?.(view);
    }

    async #interruptAuditRun(active: ActiveBridgeRun): Promise<void> {
        if (isTerminalStatus(active.view.state.status)) return;
        let view: AgentRuntimeView;
        try {
            view = await this.#options.runtimeClient.interrupt(active.runId, {
                expectedStateVersion: active.view.state.stateVersion,
            });
        } catch (cause) {
            this.#fail("local_agent_audit_interrupt_failed", `后端审计 Run 终止失败：${errorMessage(cause, "未知终止错误")}`);
            throw cause;
        }
        if (view.run.id !== active.runId || view.run.reasoningHost !== "local_codex" || !isTerminalStatus(view.state.status)) {
            const cause = new Error("本机 Agent 后端审计 Run 终止结果无效");
            this.#fail("local_agent_audit_interrupt_invalid", cause.message);
            throw cause;
        }
        active.view = view;
        this.#adoptView(view);
    }

    #setState(state: LocalAgentBridgeState): void {
        this.#state = state;
        this.#options.onState?.(state);
    }

    #fail(code: string, message: string): void {
        this.#setState({ kind: "failed", code, message });
    }

    #settleActivation(cause?: unknown): void {
        if (this.#activationSettled) return;
        this.#activationSettled = true;
        if (cause === undefined) this.#resolveActivation();
        else this.#rejectActivation(cause);
    }

    async #cancelAfterActivationFailure(turnId: string, activationFailure: unknown): Promise<unknown> {
        try {
            await this.#options.localClient.cancelTurn(turnId);
            return activationFailure;
        } catch (cancelFailure) {
            return new AggregateError([activationFailure, cancelFailure], `${errorMessage(activationFailure, "后端审计 Run 创建失败")}；本机 Codex turn 取消失败：${errorMessage(cancelFailure, "未知取消错误")}`);
        }
    }
}

async function stableDecisionRequestId(namespace: "tool" | "final", identity: string): Promise<string> {
    const digest = await crypto.subtle.digest("SHA-256", new TextEncoder().encode(`${namespace}:${identity}`));
    return `local-${namespace}:${Array.from(new Uint8Array(digest), (byte) => byte.toString(16).padStart(2, "0")).join("")}`;
}

function errorCode(cause: unknown): string {
    return cause instanceof AgentRuntimeRequestError ? cause.code : "local_agent_bridge_failed";
}

function errorMessage(cause: unknown, fallback: string): string {
    return cause instanceof Error && cause.message.trim() ? cause.message : fallback;
}

function isTerminalStatus(status: AgentRuntimeView["state"]["status"]): boolean {
    return status === "succeeded" || status === "failed" || status === "cancelled";
}

function buildDeliveryRepairInstruction(view: AgentRuntimeView): string | undefined {
    if (isTerminalStatus(view.state.status)) return undefined;
    const repairableVerification = view.state.verification?.status === "repairable" ? view.state.verification : undefined;
    if (!repairableVerification && !view.state.decisionFeedback) return undefined;
    const facts = {
        runId: view.run.id,
        stateVersion: view.state.stateVersion,
        expectedDelivery: view.state.expectedDelivery,
        verification: repairableVerification,
        decisionFeedback: view.state.decisionFeedback,
    };
    return [
        "系统交付校验判定本轮尚未完成。请在同一执行链中继续修正，不要把这段内部纠偏事实复述给用户，也不要假设成功。",
        `权威纠偏事实：${JSON.stringify(facts)}`,
        "请基于当前真实画布状态和工具返回继续采取必要动作；完成后重新提交与冻结合同完全一致的 expectedDelivery。若事实仍不足，必须显式失败并说明缺口。",
    ].join("\n");
}
