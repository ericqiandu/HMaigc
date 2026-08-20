import { localForageStorage } from "@/lib/localforage-storage";
import { parseClarificationHistory, parsePendingClarification, type AgentClarificationAnswerInput, type AgentCompletedClarification, type AgentPendingClarification } from "./agent-clarification";
import { array, flag, integer, object, text } from "./strict-contract";

export type {
    AgentClarificationAnswer,
    AgentClarificationAnswerInput,
    AgentClarificationOption,
    AgentClarificationQuestion,
    AgentClarificationQuestionType,
    AgentClarificationRequest,
    AgentCompletedClarification,
    AgentPendingClarification,
} from "./agent-clarification";

export type AgentRunStatus = "queued" | "running" | "waiting_input" | "waiting_approval" | "waiting_tool" | "succeeded" | "failed" | "cancelled";
export type AgentRuntimeEventKind =
    | "run.created"
    | "run.status_changed"
    | "model.delta"
    | "model.rejected"
    | "clarification.requested"
    | "clarification.answer_saved"
    | "clarification.responded"
    | "tool.call"
    | "approval.required"
    | "approval.decided"
    | "tool.started"
    | "tool.result"
    | "checkpoint.saved"
    | "run.completed"
    | "run.failed";
export type AgentToolName = "skill.load" | "production.plan" | "production.render" | "canvas.commit";
export type AgentArtifactKind = "image" | "video" | "audio" | "text" | "canvas_revision";
export type AgentDeliveryFact = "final_message" | "canvas_revision" | "artifact";

export type AgentExpectedDelivery = {
    kind: "answer" | "canvas_change" | "generated_asset" | "mixed";
    requiredArtifacts?: AgentArtifactKind[];
    targetCanvasId?: string;
    completionCriteria: Array<{ fact: AgentDeliveryFact; artifact?: AgentArtifactKind }>;
};
export type AgentToolCall = { toolCallId: string; toolName: AgentToolName; actionVersion: number; arguments: Record<string, unknown>; expectedDelivery: AgentExpectedDelivery };
export type AgentDeliveryVerification = { status: "satisfied" | "repairable" | "failed"; rationale: string; missingCriteria?: Array<{ fact: string; artifact?: string }> };
export type AgentRuntimeGenerationModelSelection = { channelId: string; model: string };
export type AgentRuntimeGenerationModelSelections = { image?: AgentRuntimeGenerationModelSelection; video?: AgentRuntimeGenerationModelSelection };
export type AgentRuntimeResourceReference = { resourceId: string; name: string };
export type AgentRuntimeFrozenResource = AgentRuntimeResourceReference & { mimeType: string; width?: number; height?: number };
export type AgentRuntimeExecutionMode = "guided" | "automatic";
export type AgentRuntimeStartConfiguration = { generationModels: AgentRuntimeGenerationModelSelections; skillDirs: string[]; attachments: AgentRuntimeResourceReference[]; executionMode: AgentRuntimeExecutionMode };
export type AgentRuntimeSkillSelection = { dir: string; name: string; description: string; instructions: string; version: number; checksum: string };
export type AgentRuntimeRunConfiguration = { generationModels: AgentRuntimeGenerationModelSelections; skills: AgentRuntimeSkillSelection[]; attachments: AgentRuntimeFrozenResource[]; executionMode: AgentRuntimeExecutionMode };
export type AgentRuntimeState = {
    stateVersion: number;
    stepNumber: number;
    maxSteps: number;
    status: AgentRunStatus;
    expectedDelivery?: AgentExpectedDelivery;
    verification?: AgentDeliveryVerification;
    pendingToolCall?: AgentToolCall;
    pendingToolStarted?: boolean;
    pendingClarification?: AgentPendingClarification;
    clarificationHistory: AgentCompletedClarification[];
    lastToolResult?: { toolCallId: string; actionVersion: number; succeeded: boolean; output: Record<string, unknown>; errorCode?: string };
    decisionFeedback?: { code: "model_decision_invalid" | "delivery_contract_changed" | "required_skill_not_loaded" | "clarification_identity_reused"; reason: string };
    finalMessage?: string;
    failureCode?: string;
    userMessage: string;
    configuration: AgentRuntimeRunConfiguration;
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
export type AgentThreadHistoryItem = {
    thread: {
        id: string;
        canvasId: string;
        status: "active";
        createdAt: string;
        updatedAt: string;
    };
    activityAt: string;
    latestRun: AgentRuntimeView | null;
};
export type AgentThreadHistoryView = { items: AgentThreadHistoryItem[] };
export type AgentRuntimeHandle = { threadId: string; activeRunId?: string; lastSequence: number; pendingRun?: { clientRequestId: string; userMessage: string; configuration: AgentRuntimeStartConfiguration } };
export type AgentRuntimeHandleStorage = {
    load: (canvasId: string) => Promise<AgentRuntimeHandle | null>;
    save: (canvasId: string, handle: AgentRuntimeHandle) => Promise<void>;
    clear: (canvasId: string) => Promise<void>;
};
export type AgentRuntimeClient = {
    listThreads: (canvasId: string, limit?: number) => Promise<AgentThreadHistoryView>;
    createThread: (canvasId: string) => Promise<{ id: string; canvasId: string; status: "active" }>;
    startRun: (threadId: string, input: { clientRequestId: string; userMessage: string; maxSteps: number; configuration: AgentRuntimeStartConfiguration }) => Promise<AgentRuntimeView>;
    getRun: (runId: string) => Promise<AgentRuntimeView>;
    submitApproval: (runId: string, input: { toolCallId: string; actionVersion: number; decision: "approved" | "rejected" }) => Promise<AgentRuntimeView>;
    submitClarificationResponse: (runId: string, requestId: string, input: { expectedStateVersion: number; questionId: string; answer: AgentClarificationAnswerInput; complete: boolean }) => Promise<AgentRuntimeView>;
    subscribe: (runId: string, afterSequence: number, handlers: { onOpen?: () => void; onEvent: (event: AgentRuntimeEvent) => void; onError: (error?: Error) => void }) => () => void;
};

const runStatuses = new Set<AgentRunStatus>(["queued", "running", "waiting_input", "waiting_approval", "waiting_tool", "succeeded", "failed", "cancelled"]);
const eventKinds = new Set<AgentRuntimeEventKind>([
    "run.created",
    "run.status_changed",
    "model.delta",
    "model.rejected",
    "clarification.requested",
    "clarification.answer_saved",
    "clarification.responded",
    "tool.call",
    "approval.required",
    "approval.decided",
    "tool.started",
    "tool.result",
    "checkpoint.saved",
    "run.completed",
    "run.failed",
]);
const toolNames = new Set<AgentToolName>(["skill.load", "production.plan", "production.render", "canvas.commit"]);
const deliveryFacts = new Set(["final_message", "canvas_revision", "artifact"]);
const artifactKinds = new Set(["image", "video", "audio", "text", "canvas_revision"]);
const isoInstantPattern = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:\.\d{1,9})?Z$/;
const baseURL = String(import.meta.env.VITE_CANVAS_BACKEND_URL || "/api").replace(/\/+$/, "");

export class AgentRuntimeRequestError extends Error {
    readonly status: number;
    readonly code: string;
    readonly latestStateVersion?: number;

    constructor(message: string, status: number, code: string, latestStateVersion?: number) {
        super(message);
        this.name = "AgentRuntimeRequestError";
        this.status = status;
        this.code = code;
        this.latestStateVersion = latestStateVersion;
    }
}

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

export function parseAgentThreadHistory(value: unknown): AgentThreadHistoryView {
    const root = object(value, "Agent thread history");
    const items = array(root.items, "history.items");
    if (items.length > 20) throw new Error("Agent 会话历史不能超过 20 项");
    return { items: items.map((item, index) => parseAgentThreadHistoryItem(item, index)) };
}

function parseAgentThreadHistoryItem(value: unknown, index: number): AgentThreadHistoryItem {
    const source = object(value, `history.items[${index}]`);
    const threadSource = object(source.thread, `history.items[${index}].thread`);
    if (threadSource.status !== "active") throw new Error(`不受支持的 Agent thread 状态: ${String(threadSource.status)}`);
    const thread: AgentThreadHistoryItem["thread"] = {
        id: text(threadSource.id, `history.items[${index}].thread.id`),
        canvasId: text(threadSource.canvasId, `history.items[${index}].thread.canvasId`),
        status: "active",
        createdAt: isoInstant(threadSource.createdAt, `history.items[${index}].thread.createdAt`),
        updatedAt: isoInstant(threadSource.updatedAt, `history.items[${index}].thread.updatedAt`),
    };
    if (!("latestRun" in source)) throw new Error(`history.items[${index}].latestRun 为必填字段`);
    const latestRun = source.latestRun === null ? null : parseAgentRuntimeView(source.latestRun);
    if (latestRun && latestRun.run.threadId !== thread.id) throw new Error("Agent 会话历史的最近运行归属冲突");
    return {
        thread,
        activityAt: isoInstant(source.activityAt, `history.items[${index}].activityAt`),
        latestRun,
    };
}

function parseState(value: unknown): AgentRuntimeState {
    const source = object(value, "Agent state");
    const result: AgentRuntimeState = {
        stateVersion: integer(source.stateVersion, "state.stateVersion"),
        stepNumber: integer(source.stepNumber, "state.stepNumber", true),
        maxSteps: integer(source.maxSteps, "state.maxSteps"),
        status: runStatus(source.status),
        userMessage: text(source.userMessage, "state.userMessage"),
        configuration: parseRunConfiguration(source.configuration),
        clarificationHistory: parseClarificationHistory(source.clarificationHistory, parseExpectedDelivery),
    };
    if (source.pendingToolCall !== undefined) result.pendingToolCall = parseToolCall(source.pendingToolCall);
    if (source.pendingToolStarted !== undefined) result.pendingToolStarted = flag(source.pendingToolStarted, "state.pendingToolStarted");
    if (source.pendingClarification !== undefined) result.pendingClarification = parsePendingClarification(source.pendingClarification, parseExpectedDelivery);
    if (source.finalMessage !== undefined) result.finalMessage = text(source.finalMessage, "state.finalMessage");
    if (source.failureCode !== undefined) result.failureCode = text(source.failureCode, "state.failureCode");
    if (source.expectedDelivery !== undefined) result.expectedDelivery = parseExpectedDelivery(source.expectedDelivery);
    if (source.verification !== undefined) result.verification = parseVerification(source.verification);
    if (source.lastToolResult !== undefined) result.lastToolResult = parseToolResult(source.lastToolResult);
    if (source.decisionFeedback !== undefined) result.decisionFeedback = parseDecisionFeedback(source.decisionFeedback);
    validateStateFacts(result);
    return result;
}

function parseRunConfiguration(value: unknown): AgentRuntimeRunConfiguration {
    const source = object(value, "state.configuration");
    const models = parseGenerationModelSelections(source.generationModels, "state.configuration.generationModels");
    const skills = array(source.skills, "state.configuration.skills").map((item, index) => {
        const skill = object(item, `state.configuration.skills[${index}]`);
        const checksum = text(skill.checksum, `state.configuration.skills[${index}].checksum`);
        if (checksum.length !== 64 || Array.from(checksum).some((character) => !"0123456789abcdef".includes(character))) {
            throw new Error(`state.configuration.skills[${index}].checksum 必须是 64 位小写 SHA-256`);
        }
        return {
            dir: text(skill.dir, `state.configuration.skills[${index}].dir`),
            name: text(skill.name, `state.configuration.skills[${index}].name`),
            description: text(skill.description, `state.configuration.skills[${index}].description`, true),
            instructions: text(skill.instructions, `state.configuration.skills[${index}].instructions`),
            version: integer(skill.version, `state.configuration.skills[${index}].version`, true),
            checksum,
        };
    });
    const attachments = array(source.attachments, "state.configuration.attachments").map((item, index) => parseFrozenResource(item, `state.configuration.attachments[${index}]`));
    return { generationModels: models, skills, attachments, executionMode: executionMode(source.executionMode, "state.configuration.executionMode") };
}

function parseStartConfiguration(value: unknown, label: string): AgentRuntimeStartConfiguration {
    const source = object(value, label);
    return {
        generationModels: parseGenerationModelSelections(source.generationModels, `${label}.generationModels`),
        skillDirs: array(source.skillDirs, `${label}.skillDirs`).map((item, index) => text(item, `${label}.skillDirs[${index}]`)),
        attachments: array(source.attachments, `${label}.attachments`).map((item, index) => parseResourceReference(item, `${label}.attachments[${index}]`)),
        executionMode: executionMode(source.executionMode, `${label}.executionMode`),
    };
}

function parseResourceReference(value: unknown, label: string): AgentRuntimeResourceReference {
    const source = object(value, label);
    return { resourceId: text(source.resourceId, `${label}.resourceId`), name: text(source.name, `${label}.name`) };
}

function parseFrozenResource(value: unknown, label: string): AgentRuntimeFrozenResource {
    const source = object(value, label);
    const result: AgentRuntimeFrozenResource = { ...parseResourceReference(source, label), mimeType: text(source.mimeType, `${label}.mimeType`) };
    if (source.width !== undefined) result.width = integer(source.width, `${label}.width`);
    if (source.height !== undefined) result.height = integer(source.height, `${label}.height`);
    return result;
}

function executionMode(value: unknown, label: string): AgentRuntimeExecutionMode {
    if (value !== "guided" && value !== "automatic") throw new Error(`${label} 必须是 guided 或 automatic`);
    return value;
}

function parseGenerationModelSelections(value: unknown, label: string): AgentRuntimeGenerationModelSelections {
    const source = object(value, label);
    const result: AgentRuntimeGenerationModelSelections = {};
    if (source.image !== undefined) result.image = parseGenerationModelSelection(source.image, `${label}.image`);
    if (source.video !== undefined) result.video = parseGenerationModelSelection(source.video, `${label}.video`);
    return result;
}

function parseGenerationModelSelection(value: unknown, label: string): AgentRuntimeGenerationModelSelection {
    const source = object(value, label);
    return { channelId: text(source.channelId, `${label}.channelId`), model: text(source.model, `${label}.model`) };
}

function validateStateFacts(state: AgentRuntimeState) {
    const waitingForInput = state.status === "waiting_input";
    const waitingForApproval = state.status === "waiting_approval";
    const waitingForTool = state.status === "waiting_tool";
    if (waitingForApproval && (!state.pendingToolCall || state.pendingToolStarted)) throw new Error("Agent 等待审批状态缺少冻结工具事实");
    if (waitingForTool && !state.pendingToolCall) throw new Error("Agent 等待工具状态缺少冻结工具事实");
    if (!waitingForApproval && !waitingForTool && state.pendingToolCall) throw new Error("Agent 非等待状态携带了冻结工具事实");
    if (state.pendingToolStarted && !waitingForTool) throw new Error("Agent 工具执行状态冲突");
    if (waitingForInput !== Boolean(state.pendingClarification)) throw new Error("Agent 追问状态与待回答事实冲突");
    if (waitingForInput && state.pendingToolCall) throw new Error("Agent 追问状态不能同时等待工具");
    const requestIds = new Set(state.clarificationHistory.map((item) => item.request.requestId));
    if (requestIds.size !== state.clarificationHistory.length) throw new Error("Agent 追问历史身份重复");
    if (state.pendingClarification && requestIds.has(state.pendingClarification.request.requestId)) throw new Error("Agent 待回答追问身份已被使用");
    for (const item of [...state.clarificationHistory, ...(state.pendingClarification ? [state.pendingClarification] : [])]) {
        if (!state.expectedDelivery || !sameExpectedDelivery(item.request.expectedDelivery, state.expectedDelivery)) throw new Error("Agent 追问交付契约冲突");
    }
    if (state.status === "succeeded" && (!state.finalMessage || state.verification?.status !== "satisfied" || !state.expectedDelivery)) {
        throw new Error("Agent 成功状态缺少已验收交付事实");
    }
    if (state.status === "failed" && !state.failureCode) throw new Error("Agent 失败状态缺少失败代码");
    if (state.status !== "succeeded" && state.verification?.status === "satisfied") throw new Error("Agent 验收状态与运行状态冲突");
}

function sameExpectedDelivery(left: AgentExpectedDelivery, right: AgentExpectedDelivery) {
    return JSON.stringify(left) === JSON.stringify(right);
}

function parseToolCall(value: unknown): AgentToolCall {
    const source = object(value, "pendingToolCall");
    const toolName = source.toolName;
    if (typeof toolName !== "string" || !toolNames.has(toolName as AgentToolName)) throw new Error(`不受支持的 Agent 工具: ${String(toolName)}`);
    return {
        toolCallId: text(source.toolCallId, "toolCallId"),
        toolName: toolName as AgentToolName,
        actionVersion: integer(source.actionVersion, "actionVersion"),
        arguments: object(source.arguments, "tool arguments"),
        expectedDelivery: parseExpectedDelivery(source.expectedDelivery),
    };
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

function parseDecisionFeedback(value: unknown): NonNullable<AgentRuntimeState["decisionFeedback"]> {
    const source = object(value, "decisionFeedback");
    if (source.code !== "model_decision_invalid" && source.code !== "delivery_contract_changed" && source.code !== "required_skill_not_loaded" && source.code !== "clarification_identity_reused") {
        throw new Error(`不受支持的 Agent 决策反馈: ${String(source.code)}`);
    }
    return { code: source.code, reason: text(source.reason, "decisionFeedback.reason") };
}

function criterion(value: unknown): AgentExpectedDelivery["completionCriteria"][number] {
    const source = object(value, "delivery criterion");
    const fact = text(source.fact, "criterion.fact");
    if (!deliveryFacts.has(fact)) throw new Error(`不受支持的交付事实: ${fact}`);
    const result: AgentExpectedDelivery["completionCriteria"][number] = { fact: fact as AgentExpectedDelivery["completionCriteria"][number]["fact"] };
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
    if (!response.ok || envelope.code !== 0) {
        const data = envelope.data && typeof envelope.data === "object" && !Array.isArray(envelope.data) ? (envelope.data as Record<string, unknown>) : {};
        const code = typeof data.errorCode === "string" && data.errorCode.trim() ? data.errorCode : "agent_request_failed";
        const latestStateVersion = typeof data.latestStateVersion === "number" && Number.isSafeInteger(data.latestStateVersion) && data.latestStateVersion > 0 ? data.latestStateVersion : undefined;
        throw new AgentRuntimeRequestError(message, response.status, code, latestStateVersion);
    }
    return envelope.data;
}

export const agentRuntimeClient: AgentRuntimeClient = {
    listThreads: async (canvasId, limit = 20) => {
        const normalizedCanvasID = canvasId.trim();
        if (!normalizedCanvasID) throw new Error("Agent 画布标识不能为空");
        if (!Number.isSafeInteger(limit) || limit < 1 || limit > 20) throw new Error("Agent 会话历史数量必须在 1 到 20 之间");
        return parseAgentThreadHistory(await request(`/agent/threads?canvasId=${encodeURIComponent(normalizedCanvasID)}&limit=${String(limit)}`));
    },
    createThread: async (canvasId) => {
        const source = object(await request("/agent/threads", { method: "POST", body: JSON.stringify({ canvasId }) }), "Agent thread");
        if (source.status !== "active") throw new Error(`不受支持的 Agent thread 状态: ${String(source.status)}`);
        return { id: text(source.id, "thread.id"), canvasId: text(source.canvasId, "thread.canvasId"), status: "active" };
    },
    startRun: async (threadId, input) => parseAgentRuntimeView(await request(`/agent/threads/${encodeURIComponent(threadId)}/runs`, { method: "POST", body: JSON.stringify(input) })),
    getRun: async (runId) => parseAgentRuntimeView(await request(`/agent/runs/${encodeURIComponent(runId)}`)),
    submitApproval: async (runId, input) => parseAgentRuntimeView(await request(`/agent/runs/${encodeURIComponent(runId)}/approvals`, { method: "POST", body: JSON.stringify(input) })),
    submitClarificationResponse: async (runId, requestId, input) =>
        parseAgentRuntimeView(await request(`/agent/runs/${encodeURIComponent(runId)}/clarifications/${encodeURIComponent(requestId)}/responses`, { method: "POST", body: JSON.stringify(input) })),
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
            result.pendingRun = {
                clientRequestId: text(pending.clientRequestId, "handle.pendingRun.clientRequestId"),
                userMessage: text(pending.userMessage, "handle.pendingRun.userMessage"),
                configuration: parseStartConfiguration(pending.configuration, "handle.pendingRun.configuration"),
            };
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

function runStatus(value: unknown): AgentRunStatus {
    if (typeof value !== "string" || !runStatuses.has(value as AgentRunStatus)) throw new Error(`不受支持的 Agent 状态: ${String(value)}`);
    return value as AgentRunStatus;
}
function artifact(value: unknown, label: string): AgentArtifactKind {
    const kind = text(value, label);
    if (!artifactKinds.has(kind)) throw new Error(`不受支持的交付资产: ${kind}`);
    return kind as AgentArtifactKind;
}
function isoInstant(value: unknown, label: string): string {
    const source = text(value, label);
    const match = isoInstantPattern.exec(source);
    const parsed = new Date(source);
    if (
        !match ||
        Number.isNaN(parsed.getTime()) ||
        parsed.getUTCFullYear() !== Number(match[1]) ||
        parsed.getUTCMonth() + 1 !== Number(match[2]) ||
        parsed.getUTCDate() !== Number(match[3]) ||
        parsed.getUTCHours() !== Number(match[4]) ||
        parsed.getUTCMinutes() !== Number(match[5]) ||
        parsed.getUTCSeconds() !== Number(match[6])
    ) {
        throw new Error(`${label} 必须是 UTC ISO-8601 时间`);
    }
    return source;
}
