import { localForageStorage } from "@/lib/localforage-storage";
import { isAgentToolName, parseAgentCapabilityArguments, parseAgentCapabilityResult, type AgentCapabilityArguments, type AgentToolName } from "./agent-capabilities";
import { parseClarificationHistory, parsePendingClarification, type AgentClarificationAnswerInput, type AgentClarificationRequest, type AgentCompletedClarification, type AgentPendingClarification } from "./agent-clarification";
import { array, exactObject, flag, integer, object, text } from "./strict-contract";

export { parseAgentCapabilityArguments, parseAgentCapabilityResult } from "./agent-capabilities";
export type { AgentCanvasOperation, AgentCanvasPoint, AgentCanvasViewport, AgentCapabilityArguments, AgentToolName, AgentVisionAnalyzeResult } from "./agent-capabilities";

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
export type AgentReasoningHost = "managed" | "local_codex";
export type AgentRuntimeEventKind = "run.started" | "run.completed" | "run.failed" | "run.interrupted" | "item.started" | "item.delta" | "item.completed" | "item.failed" | "approval.requested" | "approval.resolved" | "state.snapshot";
export type AgentArtifactKind = "image" | "video" | "audio" | "text" | "canvas_revision";
export type AgentDeliveryFact = "final_message" | "canvas_revision" | "artifact" | "artifact_revision" | "resource" | "task_backed_resource" | "canvas_bound_resource" | "publication";

export type AgentExpectedDelivery = {
    kind: "answer" | "canvas_change" | "generated_asset" | "mixed";
    requiredArtifacts?: AgentArtifactKind[];
    targetCanvasId?: string;
    completionCriteria: Array<{ fact: AgentDeliveryFact; artifact?: AgentArtifactKind }>;
};
export type AgentToolCall = { toolCallId: string; toolName: AgentToolName; actionVersion: number; arguments: AgentCapabilityArguments; expectedDelivery: AgentExpectedDelivery };
export type AgentModelDecision = { kind: "final"; final: { message: string; expectedDelivery: AgentExpectedDelivery } } | { kind: "tool_call"; toolCall: AgentToolCall } | { kind: "clarification_request"; clarification: AgentClarificationRequest };
export type AgentApprovalEffect = { kind: "canvas_mutation" | "asset_publish" | "media_generation" | "vision_analysis"; summary: string; targetIds: string[] };
export type AgentApprovalCostQuote = { modelRecordId: string; modelKey: string; priceVersion: number; amountMicrocredits: number };
export type AgentPendingApproval = {
    toolCallId: string;
    toolName: AgentToolName;
    actionVersion: number;
    proposalHash: string;
    expiresAt: string;
    effect: AgentApprovalEffect;
    quote?: AgentApprovalCostQuote;
};
export type AgentApprovalSubmission = {
    toolCallId: string;
    actionVersion: number;
    proposalHash: string;
    decision: "approved" | "rejected";
};
export type AgentDeliveryVerification = { status: "satisfied" | "repairable" | "failed"; rationale: string; missingCriteria?: Array<{ fact: string; artifact?: string }> };
export type AgentRuntimeGenerationModelSelection = { channelId: string; model: string };
export type AgentRuntimeGenerationModelSelections = { image?: AgentRuntimeGenerationModelSelection; video?: AgentRuntimeGenerationModelSelection };
export type AgentRuntimeFrozenVisionModelSelection = AgentRuntimeGenerationModelSelection & { modelRecordId: string; priceVersion: number };
export type AgentRuntimeRunGenerationModelSelections = AgentRuntimeGenerationModelSelections & {
    audio?: AgentRuntimeGenerationModelSelection;
    vision?: AgentRuntimeFrozenVisionModelSelection;
};
export type AgentRuntimeResourceReference = { resourceId: string; name: string };
export type AgentRuntimeFrozenResource = AgentRuntimeResourceReference & { mimeType: string; width?: number; height?: number };
export type AgentRuntimeExecutionMode = "guided" | "automatic";
export type AgentRuntimeStartConfiguration = { generationModels: AgentRuntimeGenerationModelSelections; skillDirs: string[]; attachments: AgentRuntimeResourceReference[]; executionMode: AgentRuntimeExecutionMode };
export type AgentRuntimeSkillSelection = { dir: string; name: string; description: string; instructions: string; version: number; checksum: string };
export type AgentRuntimeRunConfiguration = { generationModels: AgentRuntimeRunGenerationModelSelections; skills: AgentRuntimeSkillSelection[]; attachments: AgentRuntimeFrozenResource[]; executionMode: AgentRuntimeExecutionMode | "historical" };
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
        reasoningHost: AgentReasoningHost;
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
        runtimeVersion: number;
        policyVersion: number;
        createdAt: string;
        updatedAt: string;
        completedAt?: string;
    };
    state: AgentRuntimeState;
    pendingApproval?: AgentPendingApproval;
};
export type AgentTimelineItemKind = "user_message" | "agent_message" | "status" | "clarification" | "tool_call" | "tool_result" | "approval" | "artifact" | "error";
export type AgentTimelineItemStatus = "in_progress" | "completed" | "failed" | "declined" | "interrupted";
export type AgentTimelineItemContent = Record<string, unknown>;
export type AgentRunEventPayload = {
    status: AgentRunStatus;
    stateVersion: number;
    failureCode?: string;
    item?: { kind: AgentTimelineItemKind; status: AgentTimelineItemStatus; content: AgentTimelineItemContent };
};
type AgentUIEventBase = { protocolVersion: 5; threadId: string; runId: string; sequence: number; createdAt: string };
export type AgentRuntimeEvent =
    | (AgentUIEventBase & { kind: "run.started" | "state.snapshot"; itemId?: string; payload: AgentRunEventPayload })
    | (AgentUIEventBase & { kind: "run.completed" | "run.failed" | "run.interrupted"; itemId: string; payload: AgentRunEventPayload & { item: NonNullable<AgentRunEventPayload["item"]> } })
    | (AgentUIEventBase & { kind: "item.started" | "item.delta" | "item.completed" | "item.failed" | "approval.requested" | "approval.resolved"; itemId: string; itemKind: AgentTimelineItemKind; payload: AgentTimelineItemContent });
export type AgentThreadHistoryRun = {
    id: string;
    threadId: string;
    status: AgentRunStatus;
    lastEventSequence: number;
    stateVersion: number;
    stepNumber: number;
    maxSteps: number;
    modelKey: string;
    toolSchemaVersion: number;
    runtimeVersion: number;
    policyVersion: number;
    createdAt: string;
    updatedAt: string;
    completedAt?: string;
};
export type AgentTimelineItem = {
    id: string;
    runId: string;
    kind: AgentTimelineItemKind;
    status: AgentTimelineItemStatus;
    ordinal: number;
    sourceEventSequence: number;
    content: AgentTimelineItemContent;
    startedAt: string;
    completedAt?: string;
    createdAt: string;
    updatedAt: string;
};
export type AgentThreadHistoryTurn = { run: AgentThreadHistoryRun; items: AgentTimelineItem[] };
export type AgentThreadHistoryItem = {
    thread: {
        id: string;
        canvasId: string;
        status: "active";
        createdAt: string;
        updatedAt: string;
    };
    activityAt: string;
    turns: AgentThreadHistoryTurn[];
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
    steer: (runId: string, input: { clientRequestId: string; message: string; expectedStateVersion: number }) => Promise<AgentRuntimeView>;
    interrupt: (runId: string, input: { expectedStateVersion: number }) => Promise<AgentRuntimeView>;
    submitApproval: (runId: string, input: { toolCallId: string; actionVersion: number; decision: "approved" | "rejected"; proposalHash: string }) => Promise<AgentRuntimeView>;
    submitClarificationResponse: (runId: string, requestId: string, input: { expectedStateVersion: number; questionId: string; answer: AgentClarificationAnswerInput; complete: boolean }) => Promise<AgentRuntimeView>;
    subscribe: (runId: string, afterSequence: number, handlers: { onOpen?: () => void; onEvent: (event: AgentRuntimeEvent) => void; onError: (error?: Error) => void }) => () => void;
};
export type AgentLocalRunStartInput = {
    canvasId: string;
    externalThreadId: string;
    clientRequestId: string;
    userMessage: string;
    maxSteps: number;
    configuration: AgentRuntimeStartConfiguration;
};
export type AgentLocalDecisionInput = {
    clientRequestId: string;
    expectedStateVersion: number;
    decision: AgentModelDecision;
};
export type AgentLocalRuntimeClient = {
    startRun: (input: AgentLocalRunStartInput) => Promise<AgentRuntimeView>;
    submitDecision: (runId: string, input: AgentLocalDecisionInput) => Promise<AgentRuntimeView>;
    interrupt: (runId: string, input: { expectedStateVersion: number }) => Promise<AgentRuntimeView>;
};

const runStatuses = new Set<AgentRunStatus>(["queued", "running", "waiting_input", "waiting_approval", "waiting_tool", "succeeded", "failed", "cancelled"]);
const terminalRunStatuses = new Set<AgentRunStatus>(["succeeded", "failed", "cancelled"]);
const eventKinds = new Set<AgentRuntimeEventKind>(["run.started", "run.completed", "run.failed", "run.interrupted", "item.started", "item.delta", "item.completed", "item.failed", "approval.requested", "approval.resolved", "state.snapshot"]);
const runEventKinds = new Set<AgentRuntimeEventKind>(["run.started", "run.completed", "run.failed", "run.interrupted", "state.snapshot"]);
const timelineItemKinds = new Set<AgentTimelineItemKind>(["user_message", "agent_message", "status", "clarification", "tool_call", "tool_result", "approval", "artifact", "error"]);
const timelineItemStatuses = new Set<AgentTimelineItemStatus>(["in_progress", "completed", "failed", "declined", "interrupted"]);
const deliveryFacts = new Set(["final_message", "canvas_revision", "artifact", "artifact_revision", "resource", "task_backed_resource", "canvas_bound_resource", "publication"]);
const artifactKinds = new Set(["image", "video", "audio", "text", "canvas_revision"]);
const isoInstantPattern = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:\.\d{1,9})?Z$/;
const baseURL = String(import.meta.env.VITE_CANVAS_BACKEND_URL || "/api").replace(/\/+$/, "");
const currentRuntimeVersion = 5;
const currentPolicyVersion = 5;
const currentToolSchemaVersion = 8;
const currentAgentUIProtocolVersion = 5;
const retiredRuntimeVersion = 3;
const retiredPolicyVersion = 3;
const retiredToolSchemaVersion = 4;

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
    const root = exactObject(value, "Agent Runtime", ["run", "state", "pendingApproval"]);
    const run = object(root.run, "Agent run");
    const state = parseState(root.state);
    const parsedRun: AgentRuntimeView["run"] = {
        id: text(run.id, "run.id"),
        threadId: text(run.threadId, "run.threadId"),
        reasoningHost: parseReasoningHost(run.reasoningHost),
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
        runtimeVersion: integer(run.runtimeVersion, "run.runtimeVersion"),
        policyVersion: integer(run.policyVersion, "run.policyVersion"),
        createdAt: text(run.createdAt, "run.createdAt"),
        updatedAt: text(run.updatedAt, "run.updatedAt"),
    };
    if (run.completedAt !== undefined) parsedRun.completedAt = text(run.completedAt, "run.completedAt");
    if (parsedRun.status !== state.status) throw new Error("Agent run 与 checkpoint 状态冲突");
    if (parsedRun.reasoningHost === "managed" && (!parsedRun.modelRecordId || !parsedRun.modelKey)) {
        throw new Error("managed Agent run 缺少模型来源事实");
    }
    if (parsedRun.reasoningHost === "local_codex" && (parsedRun.modelRecordId || parsedRun.modelKey)) {
        throw new Error("local_codex Agent run 不得携带云端模型来源");
    }
    if (parsedRun.stateVersion !== state.stateVersion || parsedRun.stepNumber !== state.stepNumber || parsedRun.maxSteps !== state.maxSteps) {
        throw new Error("Agent run 与 checkpoint 版本事实冲突");
    }
    const isTerminal = terminalRunStatuses.has(parsedRun.status) && parsedRun.completedAt !== undefined;
    const isCurrentContract = parsedRun.runtimeVersion === currentRuntimeVersion && parsedRun.policyVersion === currentPolicyVersion && parsedRun.toolSchemaVersion === currentToolSchemaVersion;
    const isRetiredReadOnlyContract = isTerminal && parsedRun.runtimeVersion === retiredRuntimeVersion && parsedRun.policyVersion === retiredPolicyVersion && parsedRun.toolSchemaVersion === retiredToolSchemaVersion;
    if (state.configuration.executionMode === "historical" && (!isTerminal || parsedRun.toolSchemaVersion !== 1 || parsedRun.runtimeVersion !== 1 || parsedRun.policyVersion !== 1)) {
        throw new Error("historical 执行模式仅允许首代已终结 Agent 运行");
    }
    if (state.configuration.executionMode !== "historical" && !isCurrentContract && !isRetiredReadOnlyContract) {
        throw new Error(`不受支持的 Agent Runtime 合同: ${parsedRun.runtimeVersion}/${parsedRun.policyVersion}/${parsedRun.toolSchemaVersion}`);
    }
    if (isCurrentContract && parsedRun.reasoningHost === "managed" && !state.configuration.generationModels.vision) {
        throw new Error("当前 Agent Run 缺少服务端冻结的 vision 模型");
    }
    const pendingApproval = root.pendingApproval === undefined ? undefined : parsePendingApproval(root.pendingApproval);
    if (state.status === "waiting_approval") {
        if (
            !pendingApproval ||
            !state.pendingToolCall ||
            pendingApproval.toolCallId !== state.pendingToolCall.toolCallId ||
            pendingApproval.toolName !== state.pendingToolCall.toolName ||
            pendingApproval.actionVersion !== state.pendingToolCall.actionVersion
        ) {
            throw new Error("Agent 等待审批状态缺少完全一致的冻结提案");
        }
        const expectedEffectKind = approvalEffectKindForTool(state.pendingToolCall.toolName);
        if (!expectedEffectKind || pendingApproval.effect.kind !== expectedEffectKind) {
            throw new Error("Agent 审批影响与冻结工具不一致");
        }
        if (state.pendingToolCall.toolName === "vision.analyze") {
            const argumentsValue = state.pendingToolCall.arguments;
            const frozenVision = state.configuration.generationModels.vision;
            if (
                !("detail" in argumentsValue) ||
                !frozenVision ||
                !pendingApproval.quote ||
                argumentsValue.modelRecordId !== frozenVision.modelRecordId ||
                argumentsValue.modelKey !== frozenVision.model ||
                pendingApproval.quote.modelRecordId !== frozenVision.modelRecordId ||
                pendingApproval.quote.modelKey !== frozenVision.model ||
                pendingApproval.quote.priceVersion !== frozenVision.priceVersion
            ) {
                throw new Error("视觉理解审批与 Run 冻结模型或报价不一致");
            }
        }
    } else if (pendingApproval) {
        throw new Error("Agent 非等待审批状态携带了冻结提案");
    }
    return { run: parsedRun, state, ...(pendingApproval ? { pendingApproval } : {}) };
}

function approvalEffectKindForTool(toolName: AgentToolName): AgentApprovalEffect["kind"] | undefined {
    if (toolName === "canvas.apply_ops") return "canvas_mutation";
    if (toolName === "assets.publish") return "asset_publish";
    if (toolName === "media.generate") return "media_generation";
    if (toolName === "vision.analyze") return "vision_analysis";
    return undefined;
}

function parseReasoningHost(value: unknown): AgentReasoningHost {
    if (value === "managed" || value === "local_codex") return value;
    throw new Error(`run.reasoningHost 不受支持: ${String(value)}`);
}

function parsePendingApproval(value: unknown): AgentPendingApproval {
    const source = exactObject(value, "pendingApproval", ["toolCallId", "toolName", "actionVersion", "proposalHash", "expiresAt", "effect", "quote"]);
    if (!isAgentToolName(source.toolName)) throw new Error(`不受支持的审批工具: ${String(source.toolName)}`);
    const proposalHash = text(source.proposalHash, "pendingApproval.proposalHash");
    if (!/^[0-9a-f]{64}$/.test(proposalHash)) throw new Error("pendingApproval.proposalHash 必须是 SHA-256");
    const effectSource = exactObject(source.effect, "pendingApproval.effect", ["kind", "summary", "targetIds"]);
    const kind = effectSource.kind;
    if (kind !== "canvas_mutation" && kind !== "asset_publish" && kind !== "media_generation" && kind !== "vision_analysis") {
        throw new Error(`不受支持的审批影响类型: ${String(kind)}`);
    }
    const targetIds = array(effectSource.targetIds, "pendingApproval.effect.targetIds").map((target, index) => text(target, `pendingApproval.effect.targetIds[${index}]`));
    if (!targetIds.length || new Set(targetIds).size !== targetIds.length) throw new Error("pendingApproval.effect.targetIds 必须非空且唯一");
    const quote = source.quote === undefined ? undefined : parseApprovalCostQuote(source.quote);
    const paidTool = source.toolName === "media.generate" || source.toolName === "vision.analyze";
    if (paidTool && !quote) throw new Error(`${source.toolName} 审批缺少服务端冻结报价`);
    if (!paidTool && quote) throw new Error("非付费工具审批不得携带冻结报价");
    return {
        toolCallId: text(source.toolCallId, "pendingApproval.toolCallId"),
        toolName: source.toolName,
        actionVersion: integer(source.actionVersion, "pendingApproval.actionVersion"),
        proposalHash,
        expiresAt: isoInstant(source.expiresAt, "pendingApproval.expiresAt"),
        effect: { kind, summary: text(effectSource.summary, "pendingApproval.effect.summary"), targetIds },
        ...(quote ? { quote } : {}),
    };
}

function parseApprovalCostQuote(value: unknown): AgentApprovalCostQuote {
    const source = exactObject(value, "pendingApproval.quote", ["modelRecordId", "modelKey", "priceVersion", "amountMicrocredits"]);
    return {
        modelRecordId: text(source.modelRecordId, "pendingApproval.quote.modelRecordId"),
        modelKey: text(source.modelKey, "pendingApproval.quote.modelKey"),
        priceVersion: integer(source.priceVersion, "pendingApproval.quote.priceVersion"),
        amountMicrocredits: integer(source.amountMicrocredits, "pendingApproval.quote.amountMicrocredits"),
    };
}

export function parseAgentRuntimeEvent(value: unknown): AgentRuntimeEvent {
    const source = exactObject(value, "Agent event", ["protocolVersion", "threadId", "runId", "sequence", "kind", "itemId", "itemKind", "payload", "createdAt"]);
    if (source.protocolVersion !== currentAgentUIProtocolVersion) throw new Error(`不受支持的 Agent UI 协议版本: ${String(source.protocolVersion)}`);
    const kind = source.kind;
    if (typeof kind !== "string" || !eventKinds.has(kind as AgentRuntimeEventKind)) throw new Error(`不受支持的 Agent 事件: ${String(kind)}`);
    const base: AgentUIEventBase = {
        protocolVersion: currentAgentUIProtocolVersion,
        threadId: text(source.threadId, "event.threadId"),
        runId: text(source.runId, "event.runId"),
        sequence: integer(source.sequence, "event.sequence"),
        createdAt: isoInstant(source.createdAt, "event.createdAt"),
    };
    if (runEventKinds.has(kind as AgentRuntimeEventKind)) {
        if (source.itemKind !== undefined) throw new Error(`Agent ${kind} 事件不允许 itemKind`);
        const itemId = source.itemId === undefined ? undefined : text(source.itemId, "event.itemId");
        const payload = parseRunEventPayload(source.payload);
        validateRunUIEvent(kind as "run.started" | "run.completed" | "run.failed" | "run.interrupted" | "state.snapshot", itemId, payload);
        if (kind === "run.completed" || kind === "run.failed" || kind === "run.interrupted") {
            return { ...base, kind, itemId: itemId as string, payload: payload as AgentRunEventPayload & { item: NonNullable<AgentRunEventPayload["item"]> } };
        }
        const event: AgentRuntimeEvent = { ...base, kind: kind as "run.started" | "state.snapshot", payload };
        if (itemId !== undefined) event.itemId = itemId;
        return event;
    }
    const itemId = text(source.itemId, "event.itemId");
    const itemKind = timelineItemKind(source.itemKind, "event.itemKind");
    const payload = parseTimelineContent(source.payload, "event.payload", itemKind);
    return {
        ...base,
        kind: kind as "item.started" | "item.delta" | "item.completed" | "item.failed" | "approval.requested" | "approval.resolved",
        itemId,
        itemKind,
        payload,
    };
}

function validateRunUIEvent(kind: "run.started" | "run.completed" | "run.failed" | "run.interrupted" | "state.snapshot", itemId: string | undefined, payload: AgentRunEventPayload) {
    const expectedStatus = kind === "run.completed" ? "succeeded" : kind === "run.failed" ? "failed" : kind === "run.interrupted" ? "cancelled" : undefined;
    if (expectedStatus && payload.status !== expectedStatus) throw new Error(`Agent ${kind} 事件状态必须是 ${expectedStatus}`);
    if (expectedStatus && (!itemId || !payload.item)) throw new Error(`Agent ${kind} 事件缺少终态时间线事实`);
    if (payload.item && !itemId) throw new Error(`Agent ${kind} 事件缺少 itemId`);
}

export function parseAgentThreadHistory(value: unknown): AgentThreadHistoryView {
    const root = object(value, "Agent thread history");
    const items = array(root.items, "history.items");
    if (items.length > 20) throw new Error("Agent 会话历史不能超过 20 项");
    return { items: items.map((item, index) => parseAgentThreadHistoryItem(item, index)) };
}

function parseAgentThreadHistoryItem(value: unknown, index: number): AgentThreadHistoryItem {
    const source = exactObject(value, `history.items[${index}]`, ["thread", "activityAt", "turns"]);
    const threadSource = exactObject(source.thread, `history.items[${index}].thread`, ["id", "canvasId", "status", "createdAt", "updatedAt"]);
    if (threadSource.status !== "active") throw new Error(`不受支持的 Agent thread 状态: ${String(threadSource.status)}`);
    const thread: AgentThreadHistoryItem["thread"] = {
        id: text(threadSource.id, `history.items[${index}].thread.id`),
        canvasId: text(threadSource.canvasId, `history.items[${index}].thread.canvasId`),
        status: "active",
        createdAt: isoInstant(threadSource.createdAt, `history.items[${index}].thread.createdAt`),
        updatedAt: isoInstant(threadSource.updatedAt, `history.items[${index}].thread.updatedAt`),
    };
    const turns = array(source.turns, `history.items[${index}].turns`).map((turn, turnIndex) => parseAgentThreadHistoryTurn(turn, index, turnIndex, thread.id));
    return {
        thread,
        activityAt: isoInstant(source.activityAt, `history.items[${index}].activityAt`),
        turns,
    };
}

function parseAgentThreadHistoryTurn(value: unknown, historyIndex: number, turnIndex: number, threadId: string): AgentThreadHistoryTurn {
    const label = `history.items[${historyIndex}].turns[${turnIndex}]`;
    const source = exactObject(value, label, ["run", "items"]);
    const run = parseAgentThreadHistoryRun(source.run, `${label}.run`);
    if (run.threadId !== threadId) throw new Error("Agent 会话历史的运行归属冲突");
    const items = array(source.items, `${label}.items`).map((item, itemIndex) => parseAgentTimelineItem(item, `${label}.items[${itemIndex}]`, run.id));
    for (let index = 0; index < items.length; index += 1) {
        if (items[index]?.ordinal !== index + 1) throw new Error(`${label}.items 序号必须连续`);
        if ((items[index]?.sourceEventSequence ?? 0) > run.lastEventSequence) throw new Error(`${label}.items 事件序号超过 Run 游标`);
    }
    return { run, items };
}

function parseAgentThreadHistoryRun(value: unknown, label: string): AgentThreadHistoryRun {
    const source = exactObject(value, label, ["id", "threadId", "status", "lastEventSequence", "stateVersion", "stepNumber", "maxSteps", "modelKey", "toolSchemaVersion", "runtimeVersion", "policyVersion", "createdAt", "updatedAt", "completedAt"]);
    const run: AgentThreadHistoryRun = {
        id: text(source.id, `${label}.id`),
        threadId: text(source.threadId, `${label}.threadId`),
        status: runStatus(source.status),
        lastEventSequence: integer(source.lastEventSequence, `${label}.lastEventSequence`, true),
        stateVersion: integer(source.stateVersion, `${label}.stateVersion`),
        stepNumber: integer(source.stepNumber, `${label}.stepNumber`, true),
        maxSteps: integer(source.maxSteps, `${label}.maxSteps`),
        modelKey: text(source.modelKey, `${label}.modelKey`, true),
        toolSchemaVersion: integer(source.toolSchemaVersion, `${label}.toolSchemaVersion`),
        runtimeVersion: integer(source.runtimeVersion, `${label}.runtimeVersion`),
        policyVersion: integer(source.policyVersion, `${label}.policyVersion`),
        createdAt: isoInstant(source.createdAt, `${label}.createdAt`),
        updatedAt: isoInstant(source.updatedAt, `${label}.updatedAt`),
    };
    if (source.completedAt !== undefined) run.completedAt = isoInstant(source.completedAt, `${label}.completedAt`);
    return run;
}

function parseAgentTimelineItem(value: unknown, label: string, runId: string): AgentTimelineItem {
    const source = exactObject(value, label, ["id", "runId", "kind", "status", "ordinal", "sourceEventSequence", "content", "startedAt", "completedAt", "createdAt", "updatedAt"]);
    const itemRunId = text(source.runId, `${label}.runId`);
    if (itemRunId !== runId) throw new Error("Agent 会话历史的时间线归属冲突");
    const kind = timelineItemKind(source.kind, `${label}.kind`);
    const item: AgentTimelineItem = {
        id: text(source.id, `${label}.id`),
        runId: itemRunId,
        kind,
        status: timelineItemStatus(source.status, `${label}.status`),
        ordinal: integer(source.ordinal, `${label}.ordinal`),
        sourceEventSequence: integer(source.sourceEventSequence, `${label}.sourceEventSequence`),
        content: parseTimelineContent(source.content, `${label}.content`, kind),
        startedAt: isoInstant(source.startedAt, `${label}.startedAt`),
        createdAt: isoInstant(source.createdAt, `${label}.createdAt`),
        updatedAt: isoInstant(source.updatedAt, `${label}.updatedAt`),
    };
    if (source.completedAt !== undefined) item.completedAt = isoInstant(source.completedAt, `${label}.completedAt`);
    if (item.status === "in_progress" && item.completedAt) throw new Error(`${label}.completedAt 与进行中状态冲突`);
    if (item.status !== "in_progress" && !item.completedAt) throw new Error(`${label}.completedAt 是终态时间线必填字段`);
    return item;
}

function parseRunEventPayload(value: unknown): AgentRunEventPayload {
    const source = exactObject(value, "event.payload", ["status", "stateVersion", "failureCode", "item"]);
    const payload: AgentRunEventPayload = { status: runStatus(source.status), stateVersion: integer(source.stateVersion, "event.payload.stateVersion") };
    if (source.failureCode !== undefined) payload.failureCode = text(source.failureCode, "event.payload.failureCode");
    if (source.item !== undefined) {
        const item = exactObject(source.item, "event.payload.item", ["kind", "status", "content"]);
        const kind = timelineItemKind(item.kind, "event.payload.item.kind");
        payload.item = {
            kind,
            status: timelineItemStatus(item.status, "event.payload.item.status"),
            content: parseTimelineContent(item.content, "event.payload.item.content", kind),
        };
    }
    return payload;
}

function parseTimelineContent(value: unknown, label: string, kind: AgentTimelineItemKind): AgentTimelineItemContent {
    const source = object(value, label);
    let content: AgentTimelineItemContent;
    if (source.contentType !== undefined) {
        content = parseAgentV5TimelineContent(source, label, kind);
    } else {
        content = kind === "artifact" ? exactObject(source, label, ["artifactId", "kind", "planKey", "planVersion", "referenceKey", "shotKey", "resourceId", "status"]) : kind === "tool_call" ? parseToolTimelineActivity(source, label) : source;
    }
    rejectTransientMediaLocator(content, label);
    return content;
}

function parseToolTimelineActivity(value: unknown, label: string): AgentTimelineItemContent {
    const source = exactObject(value, label, ["toolCallId", "toolName", "actionVersion", "started", "succeeded", "errorCode", "output", "decision"]);
    const content: AgentTimelineItemContent = {
        toolCallId: text(source.toolCallId, `${label}.toolCallId`),
        actionVersion: integer(source.actionVersion, `${label}.actionVersion`),
    };
    if (source.toolName !== undefined) {
        if (!isAgentToolName(source.toolName)) throw new Error(`${label}.toolName 不受支持: ${String(source.toolName)}`);
        content.toolName = source.toolName;
    }
    if (source.started !== undefined) content.started = flag(source.started, `${label}.started`);
    if (source.succeeded !== undefined) content.succeeded = flag(source.succeeded, `${label}.succeeded`);
    if (source.errorCode !== undefined) content.errorCode = text(source.errorCode, `${label}.errorCode`);
    if (source.output !== undefined) {
        const output = object(source.output, `${label}.output`);
        content.output = content.toolName === "vision.analyze" ? { ...parseAgentCapabilityResult("vision.analyze", output) } : output;
    }
    if (source.decision !== undefined) content.decision = enumText(source.decision, `${label}.decision`, ["approved", "rejected"]);

    const transitionFacts = [content.started, content.succeeded, content.decision].filter((fact) => fact !== undefined);
    if (transitionFacts.length > 1) throw new Error(`${label} 同时包含多个工具状态变更事实`);
    if (content.started === false) throw new Error(`${label}.started 只能为 true`);
    if (content.output !== undefined && content.succeeded !== true) throw new Error(`${label}.output 缺少成功事实`);
    if (content.errorCode !== undefined && content.succeeded !== false) throw new Error(`${label}.errorCode 缺少失败事实`);
    return content;
}

function parseAgentV5TimelineContent(source: Record<string, unknown>, label: string, kind: AgentTimelineItemKind): AgentTimelineItemContent {
    if (source.contentType === "stage_review_resolution" || source.contentType === "artifact_review") {
        throw new Error(`${label} 使用了 Agent UI v5 已退役的生产图事件: ${source.contentType}`);
    }
    if (source.contentType === "media_assembly") {
        if (kind !== "tool_call") throw new Error(`${label} 的媒体装配内容与 item kind 不一致`);
        return parseMediaAssemblyTimelineContent(source, label);
    }
    if (source.contentType === "asset_publication") {
        if (kind !== "artifact") throw new Error(`${label} 的资产入库内容与 item kind 不一致`);
        const content = exactObject(source, label, ["contentType", "publicationId", "artifactRevisionId", "resourceId", "assetId", "assetVersionId", "projectAssetLinkId", "representationId", "publicationPurpose", "targetCategory", "targetBindingKey"]);
        return {
            contentType: "asset_publication",
            publicationId: text(content.publicationId, `${label}.publicationId`),
            artifactRevisionId: text(content.artifactRevisionId, `${label}.artifactRevisionId`),
            resourceId: text(content.resourceId, `${label}.resourceId`),
            assetId: text(content.assetId, `${label}.assetId`),
            assetVersionId: text(content.assetVersionId, `${label}.assetVersionId`),
            projectAssetLinkId: text(content.projectAssetLinkId, `${label}.projectAssetLinkId`),
            representationId: text(content.representationId, `${label}.representationId`),
            publicationPurpose: text(content.publicationPurpose, `${label}.publicationPurpose`),
            targetCategory: enumText(content.targetCategory, `${label}.targetCategory`, ["character", "environment", "wardrobe", "prop", "weapon", "style", "other"]),
            targetBindingKey: text(content.targetBindingKey, `${label}.targetBindingKey`),
        };
    }
    if (source.contentType === "asset_publication_failed") {
        if (kind !== "artifact") throw new Error(`${label} 的资产入库失败内容与 item kind 不一致`);
        const content = exactObject(source, label, ["contentType", "publicationId", "artifactRevisionId", "errorCode"]);
        return {
            contentType: "asset_publication_failed",
            publicationId: text(content.publicationId, `${label}.publicationId`),
            artifactRevisionId: text(content.artifactRevisionId, `${label}.artifactRevisionId`),
            errorCode: text(content.errorCode, `${label}.errorCode`),
        };
    }
    throw new Error(`${label} 包含不受支持的 Agent UI v5 内容类型: ${String(source.contentType)}`);
}

function parseMediaAssemblyTimelineContent(source: Record<string, unknown>, label: string): AgentTimelineItemContent {
    const content = exactObject(source, label, ["contentType", "toolCallId", "actionVersion", "taskId", "taskStatus", "stage", "clipCount", "audioMode", "output", "planRevision", "final", "errorCode"]);
    const taskStatus = enumText(content.taskStatus, `${label}.taskStatus`, ["queued", "running", "succeeded", "failed", "cancelled"]);
    const audioMode = enumText(content.audioMode, `${label}.audioMode`, ["none", "native", "independent"]);
    const outputSource = exactObject(content.output, `${label}.output`, ["artifactKey", "container", "videoCodec", "audioCodec", "width", "height", "frameRate"]);
    const output = {
        artifactKey: text(outputSource.artifactKey, `${label}.output.artifactKey`),
        container: enumText(outputSource.container, `${label}.output.container`, ["mp4"]),
        videoCodec: enumText(outputSource.videoCodec, `${label}.output.videoCodec`, ["h264"]),
        audioCodec: enumText(outputSource.audioCodec, `${label}.output.audioCodec`, ["none", "aac"]),
        width: integer(outputSource.width, `${label}.output.width`),
        height: integer(outputSource.height, `${label}.output.height`),
        frameRate: integer(outputSource.frameRate, `${label}.output.frameRate`),
    };
    if ((audioMode === "none") !== (output.audioCodec === "none")) throw new Error(`${label}.output.audioCodec 与声音模式冲突`);
    const result: AgentTimelineItemContent = {
        contentType: "media_assembly",
        toolCallId: text(content.toolCallId, `${label}.toolCallId`),
        actionVersion: integer(content.actionVersion, `${label}.actionVersion`),
        taskId: text(content.taskId, `${label}.taskId`),
        taskStatus,
        stage: text(content.stage, `${label}.stage`),
        clipCount: integer(content.clipCount, `${label}.clipCount`),
        audioMode,
        output,
        planRevision: parseTimelineRevisionRef(content.planRevision, `${label}.planRevision`),
    };
    if (content.final !== undefined) {
        const final = exactObject(content.final, `${label}.final`, ["artifactRevision", "resourceId", "adopted"]);
        result.final = {
            artifactRevision: parseTimelineRevisionRef(final.artifactRevision, `${label}.final.artifactRevision`),
            resourceId: text(final.resourceId, `${label}.final.resourceId`),
            adopted: flag(final.adopted, `${label}.final.adopted`),
        };
    }
    if (content.errorCode !== undefined) result.errorCode = text(content.errorCode, `${label}.errorCode`);
    const hasFinal = result.final !== undefined;
    const hasError = result.errorCode !== undefined;
    if ((taskStatus === "queued" || taskStatus === "running") && (hasFinal || hasError)) throw new Error(`${label} 进行中事实冲突`);
    if (taskStatus === "succeeded" && (!hasFinal || hasError)) throw new Error(`${label} 成功事实不完整`);
    if ((taskStatus === "failed" || taskStatus === "cancelled") && (hasFinal || !hasError)) throw new Error(`${label} 失败事实不完整`);
    return result;
}

function parseTimelineRevisionRef(value: unknown, label: string): Record<string, unknown> {
    const source = exactObject(value, label, ["artifactId", "revisionId"]);
    return { artifactId: text(source.artifactId, `${label}.artifactId`), revisionId: text(source.revisionId, `${label}.revisionId`) };
}

function enumText(value: unknown, label: string, allowed: readonly string[]): string {
    const parsed = text(value, label);
    if (!allowed.includes(parsed)) throw new Error(`${label} 不受支持: ${parsed}`);
    return parsed;
}

function rejectTransientMediaLocator(value: unknown, label: string): void {
    if (Array.isArray(value)) {
        value.forEach((item, index) => rejectTransientMediaLocator(item, `${label}[${index}]`));
        return;
    }
    if (!value || typeof value !== "object") return;
    const resourceId = typeof Reflect.get(value, "resourceId") === "string" ? Reflect.get(value, "resourceId").trim() : "";
    for (const [key, nested] of Object.entries(value)) {
        if (key === "signedUrl") throw new Error(`${label} 不允许返回短期媒体地址字段: ${key}`);
        if (key === "url") {
            const expectedURL = resourceId ? `/api/resources/${resourceId}/file` : "";
            if (nested !== expectedURL) {
                if (typeof nested === "string" && nested.startsWith("/api/resources/")) {
                    throw new Error(`${label}.url 与 resourceId 的资源身份不匹配`);
                }
                throw new Error(`${label} 不允许返回短期媒体地址字段: ${key}`);
            }
            continue;
        }
        rejectTransientMediaLocator(nested, `${label}.${key}`);
    }
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
        clarificationHistory: parseClarificationHistory(source.clarificationHistory, parseAgentExpectedDelivery),
    };
    if (source.pendingToolCall !== undefined) result.pendingToolCall = parseToolCall(source.pendingToolCall);
    if (source.pendingToolStarted !== undefined) result.pendingToolStarted = flag(source.pendingToolStarted, "state.pendingToolStarted");
    if (source.pendingClarification !== undefined) result.pendingClarification = parsePendingClarification(source.pendingClarification, parseAgentExpectedDelivery);
    if (source.finalMessage !== undefined) result.finalMessage = text(source.finalMessage, "state.finalMessage");
    if (source.failureCode !== undefined) result.failureCode = text(source.failureCode, "state.failureCode");
    if (source.expectedDelivery !== undefined) result.expectedDelivery = parseAgentExpectedDelivery(source.expectedDelivery);
    if (source.verification !== undefined) result.verification = parseVerification(source.verification);
    if (source.lastToolResult !== undefined) result.lastToolResult = parseToolResult(source.lastToolResult);
    if (source.decisionFeedback !== undefined) result.decisionFeedback = parseDecisionFeedback(source.decisionFeedback);
    validateStateFacts(result);
    return result;
}

function parseRunConfiguration(value: unknown): AgentRuntimeRunConfiguration {
    const source = exactObject(value, "state.configuration", ["generationModels", "skills", "attachments", "executionMode"]);
    const models = parseRunGenerationModelSelections(source.generationModels, "state.configuration.generationModels");
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
    return { generationModels: models, skills, attachments, executionMode: runExecutionMode(source.executionMode, "state.configuration.executionMode") };
}

function parseStartConfiguration(value: unknown, label: string): AgentRuntimeStartConfiguration {
    const source = exactObject(value, label, ["generationModels", "skillDirs", "attachments", "executionMode"]);
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

function runExecutionMode(value: unknown, label: string): AgentRuntimeRunConfiguration["executionMode"] {
    if (value === "historical") return value;
    return executionMode(value, label);
}

function parseGenerationModelSelections(value: unknown, label: string): AgentRuntimeGenerationModelSelections {
    const source = exactObject(value, label, ["image", "video"]);
    const result: AgentRuntimeGenerationModelSelections = {};
    if (source.image !== undefined) result.image = parseGenerationModelSelection(source.image, `${label}.image`);
    if (source.video !== undefined) result.video = parseGenerationModelSelection(source.video, `${label}.video`);
    return result;
}

function parseGenerationModelSelection(value: unknown, label: string): AgentRuntimeGenerationModelSelection {
    const source = exactObject(value, label, ["channelId", "model"]);
    return { channelId: text(source.channelId, `${label}.channelId`), model: text(source.model, `${label}.model`) };
}

function parseRunGenerationModelSelections(value: unknown, label: string): AgentRuntimeRunGenerationModelSelections {
    const source = exactObject(value, label, ["image", "video", "audio", "vision"]);
    const result: AgentRuntimeRunGenerationModelSelections = {};
    if (source.image !== undefined) result.image = parseGenerationModelSelection(source.image, `${label}.image`);
    if (source.video !== undefined) result.video = parseGenerationModelSelection(source.video, `${label}.video`);
    if (source.audio !== undefined) result.audio = parseGenerationModelSelection(source.audio, `${label}.audio`);
    if (source.vision !== undefined) result.vision = parseFrozenVisionModelSelection(source.vision, `${label}.vision`);
    return result;
}

function parseFrozenVisionModelSelection(value: unknown, label: string): AgentRuntimeFrozenVisionModelSelection {
    const source = exactObject(value, label, ["channelId", "modelRecordId", "model", "priceVersion"]);
    return {
        channelId: text(source.channelId, `${label}.channelId`),
        modelRecordId: text(source.modelRecordId, `${label}.modelRecordId`),
        model: text(source.model, `${label}.model`),
        priceVersion: integer(source.priceVersion, `${label}.priceVersion`),
    };
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
    const source = exactObject(value, "pendingToolCall", ["toolCallId", "toolName", "actionVersion", "arguments", "expectedDelivery"]);
    const toolName = source.toolName;
    if (!isAgentToolName(toolName)) throw new Error(`不受支持的 Agent 工具: ${String(toolName)}`);
    return {
        toolCallId: text(source.toolCallId, "toolCallId"),
        toolName,
        actionVersion: integer(source.actionVersion, "actionVersion"),
        arguments: parseAgentCapabilityArguments(toolName, source.arguments),
        expectedDelivery: parseAgentExpectedDelivery(source.expectedDelivery),
    };
}

export function parseAgentExpectedDelivery(value: unknown): NonNullable<AgentRuntimeState["expectedDelivery"]> {
    const source = exactObject(value, "expectedDelivery", ["kind", "requiredArtifacts", "targetCanvasId", "completionCriteria"]);
    const kind = source.kind;
    if (kind !== "answer" && kind !== "canvas_change" && kind !== "generated_asset" && kind !== "mixed") throw new Error(`不受支持的交付类型: ${String(kind)}`);
    const rawCriteria = array(source.completionCriteria, "completionCriteria");
    if (rawCriteria.length < 1 || rawCriteria.length > 20) throw new Error("completionCriteria 数量必须在 1 到 20 之间");
    const criteria = rawCriteria.map((item) => criterion(item));
    const result: NonNullable<AgentRuntimeState["expectedDelivery"]> = { kind, completionCriteria: criteria };
    if (source.requiredArtifacts !== undefined) {
        const requiredArtifacts = array(source.requiredArtifacts, "requiredArtifacts");
        if (requiredArtifacts.length > 5) throw new Error("requiredArtifacts 数量不能超过 5");
        result.requiredArtifacts = requiredArtifacts.map((item) => artifact(item, "requiredArtifact"));
    }
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
    const source = exactObject(value, "delivery criterion", ["fact", "artifact"]);
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
    steer: async (runId, input) => parseAgentRuntimeView(await request(`/agent/runs/${encodeURIComponent(runId)}/steer`, { method: "POST", body: JSON.stringify(input) })),
    interrupt: async (runId, input) => parseAgentRuntimeView(await request(`/agent/runs/${encodeURIComponent(runId)}/interrupt`, { method: "POST", body: JSON.stringify(input) })),
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
                    const parsed = parseAgentRuntimeEvent(JSON.parse((event as MessageEvent<string>).data));
                    if (parsed.kind !== kind) throw new Error(`Agent SSE 事件名与载荷冲突: ${kind} / ${parsed.kind}`);
                    if (parsed.runId !== runId) throw new Error(`Agent SSE 事件与订阅 Run 归属冲突: ${parsed.runId}`);
                    handlers.onEvent(parsed);
                } catch (cause) {
                    stream.close();
                    handlers.onError(cause instanceof Error ? cause : new Error("Agent 事件格式无效"));
                }
            }),
        );
        return () => stream.close();
    },
};

export const agentLocalRuntimeClient: AgentLocalRuntimeClient = {
    startRun: async (input) => parseAgentRuntimeView(await request("/agent/local/runs", { method: "POST", body: JSON.stringify(input) })),
    submitDecision: async (runId, input) => parseAgentRuntimeView(await request(`/agent/local/runs/${encodeURIComponent(runId)}/decisions`, { method: "POST", body: JSON.stringify(input) })),
    interrupt: async (runId, input) => parseAgentRuntimeView(await request(`/agent/runs/${encodeURIComponent(runId)}/interrupt`, { method: "POST", body: JSON.stringify(input) })),
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
function timelineItemKind(value: unknown, label: string): AgentTimelineItemKind {
    if (typeof value !== "string" || !timelineItemKinds.has(value as AgentTimelineItemKind)) throw new Error(`${label} 是不受支持的 Agent 时间线类型: ${String(value)}`);
    return value as AgentTimelineItemKind;
}
function timelineItemStatus(value: unknown, label: string): AgentTimelineItemStatus {
    if (typeof value !== "string" || !timelineItemStatuses.has(value as AgentTimelineItemStatus)) throw new Error(`${label} 是不受支持的 Agent 时间线状态: ${String(value)}`);
    return value as AgentTimelineItemStatus;
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
