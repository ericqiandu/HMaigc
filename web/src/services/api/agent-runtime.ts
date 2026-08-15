import { localForageStorage } from "@/lib/localforage-storage";

export type AgentRunStatus = "queued" | "running" | "waiting_approval" | "waiting_tool" | "succeeded" | "failed" | "cancelled";
export type AgentRuntimeEventKind = "run.created" | "run.status_changed" | "model.delta" | "tool.call" | "approval.required" | "approval.decided" | "tool.started" | "tool.result" | "checkpoint.saved" | "run.completed" | "run.failed";
export type AgentToolName = "canvas.read_state" | "canvas.read_selection" | "canvas.apply_ops" | "generation.submit" | "generation.wait";

export type AgentToolCall = { toolCallId: string; toolName: AgentToolName; actionVersion: number; arguments: Record<string, unknown> };
export type AgentDeliveryVerification = { status: "satisfied" | "repairable" | "failed"; rationale: string; missingCriteria?: Array<{ fact: string; artifact?: string }> };
export type AgentRuntimeState = {
    stateVersion: number;
    stepNumber: number;
    maxSteps: number;
    status: AgentRunStatus;
    expectedDelivery?: { kind: "answer" | "canvas_change" | "generated_asset" | "mixed"; requiredArtifacts?: string[]; targetCanvasId?: string; completionCriteria: Array<{ fact: string; artifact?: string }> };
    verification?: AgentDeliveryVerification;
    pendingToolCall?: AgentToolCall;
    pendingToolStarted?: boolean;
    lastToolResult?: { toolCallId: string; actionVersion: number; succeeded: boolean; output: Record<string, unknown>; errorCode?: string };
    finalMessage?: string;
    failureCode?: string;
    userMessage: string;
};
export type AgentRuntimeView = {
    run: {
        id: string;
        threadId: string;
        actorUserId: string;
        clientRequestId: string;
        status: AgentRunStatus;
        lastEventSequence: number;
        stateVersion: number;
        stepNumber: number;
        maxSteps: number;
        modelRecordId: string;
        modelKey: string;
        toolSchemaVersion: number;
        createdAt: string;
        updatedAt: string;
        completedAt?: string;
    };
    state: AgentRuntimeState;
};
export type AgentRuntimeEvent = { sequence: number; kind: AgentRuntimeEventKind; payload: AgentRuntimeState; createdAt: string };
export type AgentRuntimeHandle = { threadId: string; activeRunId?: string; lastSequence: number; pendingRun?: { clientRequestId: string; userMessage: string } };
export type AgentRuntimeHandleStorage = {
    load: (canvasId: string) => Promise<AgentRuntimeHandle | null>;
    save: (canvasId: string, handle: AgentRuntimeHandle) => Promise<void>;
    clear: (canvasId: string) => Promise<void>;
};
export type AgentRuntimeClient = {
    createThread: (canvasId: string) => Promise<{ id: string; canvasId: string; status: "active" }>;
    startRun: (threadId: string, input: { clientRequestId: string; userMessage: string; maxSteps: number }) => Promise<AgentRuntimeView>;
    getRun: (runId: string) => Promise<AgentRuntimeView>;
    submitApproval: (runId: string, input: { toolCallId: string; actionVersion: number; decision: "approved" | "rejected" }) => Promise<AgentRuntimeView>;
    submitSelection: (runId: string, input: { toolCallId: string; actionVersion: number; selection: { revision: number; nodeIds: string[] } }) => Promise<AgentRuntimeView>;
    subscribe: (runId: string, afterSequence: number, handlers: { onOpen?: () => void; onEvent: (event: AgentRuntimeEvent) => void; onError: (error?: Error) => void }) => () => void;
};

const runStatuses = new Set<AgentRunStatus>(["queued", "running", "waiting_approval", "waiting_tool", "succeeded", "failed", "cancelled"]);
const eventKinds = new Set<AgentRuntimeEventKind>(["run.created", "run.status_changed", "model.delta", "tool.call", "approval.required", "approval.decided", "tool.started", "tool.result", "checkpoint.saved", "run.completed", "run.failed"]);
const toolNames = new Set<AgentToolName>(["canvas.read_state", "canvas.read_selection", "canvas.apply_ops", "generation.submit", "generation.wait"]);
const deliveryFacts = new Set(["final_message", "canvas_revision", "artifact"]);
const artifactKinds = new Set(["image", "video", "audio", "text", "canvas_revision"]);
const baseURL = String(import.meta.env.VITE_CANVAS_BACKEND_URL || "/api").replace(/\/+$/, "");

export function parseAgentRuntimeView(value: unknown): AgentRuntimeView {
    const root = object(value, "Agent Runtime");
    const run = object(root.run, "Agent run");
    const state = parseState(root.state);
    const parsedRun: AgentRuntimeView["run"] = {
        id: text(run.id, "run.id"),
        threadId: text(run.threadId, "run.threadId"),
        actorUserId: text(run.actorUserId, "run.actorUserId"),
        clientRequestId: text(run.clientRequestId, "run.clientRequestId"),
        status: runStatus(run.status),
        lastEventSequence: integer(run.lastEventSequence, "run.lastEventSequence", true),
        stateVersion: integer(run.stateVersion, "run.stateVersion"),
        stepNumber: integer(run.stepNumber, "run.stepNumber", true),
        maxSteps: integer(run.maxSteps, "run.maxSteps"),
        modelRecordId: text(run.modelRecordId, "run.modelRecordId", true),
        modelKey: text(run.modelKey, "run.modelKey", true),
        toolSchemaVersion: integer(run.toolSchemaVersion, "run.toolSchemaVersion"),
        createdAt: text(run.createdAt, "run.createdAt"),
        updatedAt: text(run.updatedAt, "run.updatedAt"),
    };
    if (run.completedAt !== undefined) parsedRun.completedAt = text(run.completedAt, "run.completedAt");
    if (parsedRun.status !== state.status) throw new Error("Agent run 与 checkpoint 状态冲突");
    if (parsedRun.stateVersion !== state.stateVersion || parsedRun.stepNumber !== state.stepNumber || parsedRun.maxSteps !== state.maxSteps) {
        throw new Error("Agent run 与 checkpoint 版本事实冲突");
    }
    return { run: parsedRun, state };
}

export function parseAgentRuntimeEvent(value: unknown): AgentRuntimeEvent {
    const source = object(value, "Agent event");
    const kind = source.kind;
    if (typeof kind !== "string" || !eventKinds.has(kind as AgentRuntimeEventKind)) throw new Error(`不受支持的 Agent 事件: ${String(kind)}`);
    return { sequence: integer(source.sequence, "event.sequence"), kind: kind as AgentRuntimeEventKind, payload: parseState(source.payload), createdAt: text(source.createdAt, "event.createdAt") };
}

function parseState(value: unknown): AgentRuntimeState {
    const source = object(value, "Agent state");
    const result: AgentRuntimeState = {
        stateVersion: integer(source.stateVersion, "state.stateVersion"),
        stepNumber: integer(source.stepNumber, "state.stepNumber", true),
        maxSteps: integer(source.maxSteps, "state.maxSteps"),
        status: runStatus(source.status),
        userMessage: text(source.userMessage, "state.userMessage"),
    };
    if (source.pendingToolCall !== undefined) result.pendingToolCall = parseToolCall(source.pendingToolCall);
    if (source.pendingToolStarted !== undefined) result.pendingToolStarted = flag(source.pendingToolStarted, "state.pendingToolStarted");
    if (source.finalMessage !== undefined) result.finalMessage = text(source.finalMessage, "state.finalMessage");
    if (source.failureCode !== undefined) result.failureCode = text(source.failureCode, "state.failureCode");
    if (source.expectedDelivery !== undefined) result.expectedDelivery = parseExpectedDelivery(source.expectedDelivery);
    if (source.verification !== undefined) result.verification = parseVerification(source.verification);
    if (source.lastToolResult !== undefined) result.lastToolResult = parseToolResult(source.lastToolResult);
    validateStateFacts(result);
    return result;
}

function validateStateFacts(state: AgentRuntimeState) {
    const waitingForApproval = state.status === "waiting_approval";
    const waitingForTool = state.status === "waiting_tool";
    if (waitingForApproval && (!state.pendingToolCall || state.pendingToolStarted)) throw new Error("Agent 等待审批状态缺少冻结工具事实");
    if (waitingForTool && !state.pendingToolCall) throw new Error("Agent 等待工具状态缺少冻结工具事实");
    if (!waitingForApproval && !waitingForTool && state.pendingToolCall) throw new Error("Agent 非等待状态携带了冻结工具事实");
    if (state.pendingToolStarted && !waitingForTool) throw new Error("Agent 工具执行状态冲突");
    if (state.status === "succeeded" && (!state.finalMessage || state.verification?.status !== "satisfied" || !state.expectedDelivery)) {
        throw new Error("Agent 成功状态缺少已验收交付事实");
    }
    if (state.status === "failed" && !state.failureCode) throw new Error("Agent 失败状态缺少失败代码");
    if (state.status !== "succeeded" && state.verification?.status === "satisfied") throw new Error("Agent 验收状态与运行状态冲突");
}

function parseToolCall(value: unknown): AgentToolCall {
    const source = object(value, "pendingToolCall");
    const toolName = source.toolName;
    if (typeof toolName !== "string" || !toolNames.has(toolName as AgentToolName)) throw new Error(`不受支持的 Agent 工具: ${String(toolName)}`);
    return { toolCallId: text(source.toolCallId, "toolCallId"), toolName: toolName as AgentToolName, actionVersion: integer(source.actionVersion, "actionVersion"), arguments: object(source.arguments, "tool arguments") };
}

function parseExpectedDelivery(value: unknown): NonNullable<AgentRuntimeState["expectedDelivery"]> {
    const source = object(value, "expectedDelivery");
    const kind = source.kind;
    if (kind !== "answer" && kind !== "canvas_change" && kind !== "generated_asset" && kind !== "mixed") throw new Error(`不受支持的交付类型: ${String(kind)}`);
    const criteria = array(source.completionCriteria, "completionCriteria").map((item) => criterion(item));
    const result: NonNullable<AgentRuntimeState["expectedDelivery"]> = { kind, completionCriteria: criteria };
    if (source.requiredArtifacts !== undefined) result.requiredArtifacts = array(source.requiredArtifacts, "requiredArtifacts").map((item) => artifact(item, "requiredArtifact"));
    if (source.targetCanvasId !== undefined) result.targetCanvasId = text(source.targetCanvasId, "targetCanvasId");
    return result;
}

function parseVerification(value: unknown): AgentDeliveryVerification {
    const source = object(value, "verification");
    if (source.status !== "satisfied" && source.status !== "repairable" && source.status !== "failed") throw new Error(`不受支持的验收状态: ${String(source.status)}`);
    const result: AgentDeliveryVerification = { status: source.status, rationale: text(source.rationale, "verification.rationale") };
    if (source.missingCriteria !== undefined) result.missingCriteria = array(source.missingCriteria, "missingCriteria").map((item) => criterion(item));
    return result;
}

function parseToolResult(value: unknown): NonNullable<AgentRuntimeState["lastToolResult"]> {
    const source = object(value, "lastToolResult");
    const result: NonNullable<AgentRuntimeState["lastToolResult"]> = {
        toolCallId: text(source.toolCallId, "toolResult.toolCallId"),
        actionVersion: integer(source.actionVersion, "toolResult.actionVersion"),
        succeeded: flag(source.succeeded, "toolResult.succeeded"),
        output: object(source.output, "toolResult.output"),
    };
    if (source.errorCode !== undefined) result.errorCode = text(source.errorCode, "toolResult.errorCode");
    return result;
}

function criterion(value: unknown) {
    const source = object(value, "delivery criterion");
    const fact = text(source.fact, "criterion.fact");
    if (!deliveryFacts.has(fact)) throw new Error(`不受支持的交付事实: ${fact}`);
    const result: { fact: string; artifact?: string } = { fact };
    if (source.artifact !== undefined) result.artifact = artifact(source.artifact, "criterion.artifact");
    return result;
}

async function request(path: string, init?: RequestInit): Promise<unknown> {
    const response = await fetch(`${baseURL}${path}`, { ...init, credentials: "include", headers: { "Content-Type": "application/json", ...init?.headers } });
    let payload: unknown;
    try {
        payload = await response.json();
    } catch {
        throw new Error(`Agent 服务返回了无法解析的响应（HTTP ${response.status}）`);
    }
    const envelope = object(payload, "Agent response");
    const message = typeof envelope.msg === "string" ? envelope.msg : "Agent 请求失败";
    if (!response.ok || envelope.code !== 0) throw new Error(message);
    return envelope.data;
}

export const agentRuntimeClient: AgentRuntimeClient = {
    createThread: async (canvasId) => {
        const source = object(await request("/agent/threads", { method: "POST", body: JSON.stringify({ canvasId }) }), "Agent thread");
        if (source.status !== "active") throw new Error(`不受支持的 Agent thread 状态: ${String(source.status)}`);
        return { id: text(source.id, "thread.id"), canvasId: text(source.canvasId, "thread.canvasId"), status: "active" };
    },
    startRun: async (threadId, input) => parseAgentRuntimeView(await request(`/agent/threads/${encodeURIComponent(threadId)}/runs`, { method: "POST", body: JSON.stringify(input) })),
    getRun: async (runId) => parseAgentRuntimeView(await request(`/agent/runs/${encodeURIComponent(runId)}`)),
    submitApproval: async (runId, input) => parseAgentRuntimeView(await request(`/agent/runs/${encodeURIComponent(runId)}/approvals`, { method: "POST", body: JSON.stringify(input) })),
    submitSelection: async (runId, input) => parseAgentRuntimeView(await request(`/agent/runs/${encodeURIComponent(runId)}/tool-results`, { method: "POST", body: JSON.stringify(input) })),
    subscribe: (runId, afterSequence, handlers) => {
        const stream = new EventSource(`${baseURL}/agent/runs/${encodeURIComponent(runId)}/events?afterSequence=${afterSequence}`, { withCredentials: true });
        stream.onopen = () => handlers.onOpen?.();
        stream.onerror = () => handlers.onError();
        eventKinds.forEach((kind) =>
            stream.addEventListener(kind, (event) => {
                try {
                    handlers.onEvent(parseAgentRuntimeEvent(JSON.parse((event as MessageEvent<string>).data)));
                } catch (cause) {
                    stream.close();
                    handlers.onError(cause instanceof Error ? cause : new Error("Agent 事件格式无效"));
                }
            }),
        );
        return () => stream.close();
    },
};

const handleKey = (canvasId: string) => `agent-runtime-handle:${canvasId}`;
export const agentRuntimeHandleStorage: AgentRuntimeHandleStorage = {
    load: async (canvasId) => {
        const encoded = await localForageStorage.getItem(handleKey(canvasId));
        if (!encoded) return null;
        const source = object(JSON.parse(encoded), "Agent recovery handle");
        const result: AgentRuntimeHandle = { threadId: text(source.threadId, "handle.threadId"), lastSequence: integer(source.lastSequence, "handle.lastSequence", true) };
        if (source.activeRunId !== undefined) result.activeRunId = text(source.activeRunId, "handle.activeRunId");
        if (source.pendingRun !== undefined) {
            const pending = object(source.pendingRun, "handle.pendingRun");
            result.pendingRun = { clientRequestId: text(pending.clientRequestId, "handle.pendingRun.clientRequestId"), userMessage: text(pending.userMessage, "handle.pendingRun.userMessage") };
        }
        if (result.activeRunId && result.pendingRun) throw new Error("Agent recovery handle 生命周期冲突");
        return result;
    },
    save: async (canvasId, handle) => {
        await localForageStorage.setItem(handleKey(canvasId), JSON.stringify(handle));
    },
    clear: async (canvasId) => {
        await localForageStorage.removeItem(handleKey(canvasId));
    },
};

function object(value: unknown, label: string): Record<string, unknown> {
    if (!value || typeof value !== "object" || Array.isArray(value)) throw new Error(`${label} 必须是对象`);
    return value as Record<string, unknown>;
}
function array(value: unknown, label: string): unknown[] {
    if (!Array.isArray(value)) throw new Error(`${label} 必须是数组`);
    return value;
}
function text(value: unknown, label: string, allowEmpty = false): string {
    if (typeof value !== "string" || (!allowEmpty && !value.trim())) throw new Error(`${label} 必须是字符串`);
    return value;
}
function integer(value: unknown, label: string, allowZero = false): number {
    if (typeof value !== "number" || !Number.isSafeInteger(value) || value < (allowZero ? 0 : 1)) throw new Error(`${label} 必须是${allowZero ? "非负" : "正"}整数`);
    return value;
}
function flag(value: unknown, label: string): boolean {
    if (typeof value !== "boolean") throw new Error(`${label} 必须是布尔值`);
    return value;
}
function runStatus(value: unknown): AgentRunStatus {
    if (typeof value !== "string" || !runStatuses.has(value as AgentRunStatus)) throw new Error(`不受支持的 Agent 状态: ${String(value)}`);
    return value as AgentRunStatus;
}
function artifact(value: unknown, label: string): string {
    const kind = text(value, label);
    if (!artifactKinds.has(kind)) throw new Error(`不受支持的交付资产: ${kind}`);
    return kind;
}
