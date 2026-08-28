import { localForageStorage } from "@/lib/localforage-storage";
import { parseClarificationHistory, parsePendingClarification, type AgentClarificationAnswerInput, type AgentCompletedClarification, type AgentPendingClarification } from "./agent-clarification";
import { parseAgentProductionTimelineContent } from "./agent-production";
import { array, exactObject, flag, integer, object, text } from "./strict-contract";

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
    | "run.started"
    | "run.completed"
    | "run.failed"
    | "run.interrupted"
    | "item.started"
    | "item.delta"
    | "item.completed"
    | "item.failed"
    | "approval.requested"
    | "approval.resolved"
    | "state.snapshot";
export type AgentToolName = "skill.load" | "specialist.delegate" | "vision.analyze" | "media.generate" | "canvas.project";
export type AgentArtifactKind = "image" | "video" | "audio" | "text" | "canvas_revision";
export type AgentDeliveryFact = "final_message" | "canvas_revision" | "artifact" | "artifact_revision" | "resource" | "publication";

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
export type AgentRuntimeRunConfiguration = { generationModels: AgentRuntimeGenerationModelSelections; skills: AgentRuntimeSkillSelection[]; attachments: AgentRuntimeFrozenResource[]; executionMode: AgentRuntimeExecutionMode | "historical" };
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
        runtimeVersion: number;
        policyVersion: number;
        createdAt: string;
        updatedAt: string;
        completedAt?: string;
    };
    state: AgentRuntimeState;
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
type AgentUIEventBase = { protocolVersion: 3; threadId: string; runId: string; sequence: number; createdAt: string };
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
    submitApproval: (runId: string, input: { toolCallId: string; actionVersion: number; decision: "approved" | "rejected" }) => Promise<AgentRuntimeView>;
    submitClarificationResponse: (runId: string, requestId: string, input: { expectedStateVersion: number; questionId: string; answer: AgentClarificationAnswerInput; complete: boolean }) => Promise<AgentRuntimeView>;
    subscribe: (runId: string, afterSequence: number, handlers: { onOpen?: () => void; onEvent: (event: AgentRuntimeEvent) => void; onError: (error?: Error) => void }) => () => void;
};

const runStatuses = new Set<AgentRunStatus>(["queued", "running", "waiting_input", "waiting_approval", "waiting_tool", "succeeded", "failed", "cancelled"]);
const terminalRunStatuses = new Set<AgentRunStatus>(["succeeded", "failed", "cancelled"]);
const eventKinds = new Set<AgentRuntimeEventKind>([
    "run.started",
    "run.completed",
    "run.failed",
    "run.interrupted",
    "item.started",
    "item.delta",
    "item.completed",
    "item.failed",
    "approval.requested",
    "approval.resolved",
    "state.snapshot",
]);
const runEventKinds = new Set<AgentRuntimeEventKind>(["run.started", "run.completed", "run.failed", "run.interrupted", "state.snapshot"]);
const timelineItemKinds = new Set<AgentTimelineItemKind>(["user_message", "agent_message", "status", "clarification", "tool_call", "tool_result", "approval", "artifact", "error"]);
const timelineItemStatuses = new Set<AgentTimelineItemStatus>(["in_progress", "completed", "failed", "declined", "interrupted"]);
const toolNames = new Set<AgentToolName>(["skill.load", "specialist.delegate", "vision.analyze", "media.generate", "canvas.project"]);
const deliveryFacts = new Set(["final_message", "canvas_revision", "artifact", "artifact_revision", "resource", "publication"]);
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
        runtimeVersion: integer(run.runtimeVersion, "run.runtimeVersion"),
        policyVersion: integer(run.policyVersion, "run.policyVersion"),
        createdAt: text(run.createdAt, "run.createdAt"),
        updatedAt: text(run.updatedAt, "run.updatedAt"),
    };
    if (run.completedAt !== undefined) parsedRun.completedAt = text(run.completedAt, "run.completedAt");
    if (parsedRun.status !== state.status) throw new Error("Agent run 与 checkpoint 状态冲突");
    if (parsedRun.stateVersion !== state.stateVersion || parsedRun.stepNumber !== state.stepNumber || parsedRun.maxSteps !== state.maxSteps) {
        throw new Error("Agent run 与 checkpoint 版本事实冲突");
    }
    if (
        state.configuration.executionMode === "historical" &&
        (!terminalRunStatuses.has(parsedRun.status) || parsedRun.toolSchemaVersion !== 1 || parsedRun.runtimeVersion !== 1 || parsedRun.policyVersion !== 1 || parsedRun.completedAt === undefined)
    ) {
        throw new Error("historical 执行模式仅允许首代已终结 Agent 运行");
    }
    return { run: parsedRun, state };
}

export function parseAgentRuntimeEvent(value: unknown): AgentRuntimeEvent {
    const source = exactObject(value, "Agent event", ["protocolVersion", "threadId", "runId", "sequence", "kind", "itemId", "itemKind", "payload", "createdAt"]);
    if (source.protocolVersion !== 3) throw new Error(`不受支持的 Agent UI 协议版本: ${String(source.protocolVersion)}`);
    const kind = source.kind;
    if (typeof kind !== "string" || !eventKinds.has(kind as AgentRuntimeEventKind)) throw new Error(`不受支持的 Agent 事件: ${String(kind)}`);
    const base = {
        protocolVersion: 3 as const,
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
        const production = parseAgentProductionTimelineContent(source);
        const expectedKind = production.contentType === "stage_review_resolution" ? "approval" : "artifact";
        if (kind !== expectedKind) throw new Error(`${label} 的 Agent 生产内容与 item kind 不一致`);
        content = production;
    } else {
        content = kind === "artifact"
            ? exactObject(source, label, ["artifactId", "kind", "planKey", "planVersion", "referenceKey", "shotKey", "resourceId", "status"])
            : source;
    }
    rejectTransientMediaLocator(content, label);
    return content;
}

function rejectTransientMediaLocator(value: unknown, label: string): void {
    if (Array.isArray(value)) {
        value.forEach((item, index) => rejectTransientMediaLocator(item, `${label}[${index}]`));
        return;
    }
    if (!value || typeof value !== "object") return;
    for (const [key, nested] of Object.entries(value)) {
        if (key === "url" || key === "signedUrl") throw new Error(`${label} 不允许返回短期媒体地址字段: ${key}`);
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
    return { generationModels: models, skills, attachments, executionMode: runExecutionMode(source.executionMode, "state.configuration.executionMode") };
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

function runExecutionMode(value: unknown, label: string): AgentRuntimeRunConfiguration["executionMode"] {
    if (value === "historical") return value;
    return executionMode(value, label);
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
