import { isAgentToolName, type AgentToolName } from "@/services/api/agent-capabilities";
import { parseAgentExpectedDelivery, type AgentExpectedDelivery } from "@/services/api/agent-runtime";
import { array, exactObject, object, text } from "@/services/api/strict-contract";

export type LocalAgentStartResponse = { threadId: string; turnId: string };
export type LocalAgentToolCallEvent = {
    protocolVersion: 1;
    kind: "tool_call";
    requestId: string;
    threadId: string;
    turnId: string;
    toolName: AgentToolName;
    arguments: Record<string, unknown>;
    expectedDelivery: AgentExpectedDelivery;
    createdAt: string;
};
export type LocalAgentFinalDecisionEvent = {
    kind: "final_decision";
    threadId: string;
    turnId: string;
    message: string;
    expectedDelivery: AgentExpectedDelivery;
};
export type LocalAgentLifecycleEvent =
    | { kind: "connected" }
    | { kind: "thread_started"; threadId: string; sdkThreadId: string }
    | { kind: "turn_started"; threadId: string; turnId: string }
    | { kind: "item_started" | "item_updated" | "item_completed" | "turn_completed"; threadId: string; turnId: string; event: Record<string, unknown> }
    | { kind: "turn_failed"; threadId: string; turnId: string; message: string; event?: Record<string, unknown> }
    | { kind: "turn_cancelled"; threadId: string; turnId: string };
export type LocalAgentEvent = LocalAgentToolCallEvent | LocalAgentFinalDecisionEvent | LocalAgentLifecycleEvent;
export type LocalAgentThreadTurn = {
    turnId: string;
    status: "running" | "completed" | "failed" | "cancelled";
    message: string;
    createdAt: string;
    completedAt?: string;
    errorMessage?: string;
};
export type LocalAgentThread = {
    threadId: string;
    canvasId: string;
    model: string;
    createdAt: string;
    updatedAt: string;
    archivedAt?: string;
    turns: LocalAgentThreadTurn[];
};

export function parseLocalAgentStartResponse(value: unknown): LocalAgentStartResponse {
    const source = exactObject(value, "本机 Agent 启动响应", ["threadId", "turnId"]);
    return { threadId: text(source.threadId, "threadId"), turnId: text(source.turnId, "turnId") };
}

export function parseLocalAgentEvent(value: unknown): LocalAgentEvent {
    const header = object(value, "本机 Agent 事件");
    switch (header.kind) {
        case "connected":
            exactObject(value, "connected 事件", ["kind"]);
            return { kind: "connected" };
        case "thread_started": {
            const source = exactObject(value, "thread_started 事件", ["kind", "threadId", "sdkThreadId"]);
            return { kind: "thread_started", threadId: text(source.threadId, "threadId"), sdkThreadId: text(source.sdkThreadId, "sdkThreadId") };
        }
        case "turn_started":
        case "turn_cancelled": {
            const source = exactObject(value, `${String(header.kind)} 事件`, ["kind", "threadId", "turnId"]);
            return { kind: header.kind, threadId: text(source.threadId, "threadId"), turnId: text(source.turnId, "turnId") };
        }
        case "item_started":
        case "item_updated":
        case "item_completed":
        case "turn_completed": {
            const source = exactObject(value, `${header.kind} 事件`, ["kind", "threadId", "turnId", "event"]);
            return {
                kind: header.kind,
                threadId: text(source.threadId, "threadId"),
                turnId: text(source.turnId, "turnId"),
                event: object(source.event, "event"),
            };
        }
        case "turn_failed": {
            const source = exactObject(value, "turn_failed 事件", ["kind", "threadId", "turnId", "message", "event"]);
            const result: Extract<LocalAgentLifecycleEvent, { kind: "turn_failed" }> = {
                kind: "turn_failed",
                threadId: text(source.threadId, "threadId"),
                turnId: text(source.turnId, "turnId"),
                message: text(source.message, "message"),
            };
            if (source.event !== undefined) result.event = object(source.event, "event");
            return result;
        }
        case "tool_call": {
            const source = exactObject(value, "tool_call 事件", ["protocolVersion", "kind", "requestId", "threadId", "turnId", "toolName", "arguments", "expectedDelivery", "createdAt"]);
            if (source.protocolVersion !== 1) throw new Error(`不受支持的本机 Agent 协议版本: ${String(source.protocolVersion)}`);
            if (!isAgentToolName(source.toolName)) throw new Error(`不受支持的 Agent 工具: ${String(source.toolName)}`);
            return {
                protocolVersion: 1,
                kind: "tool_call",
                requestId: text(source.requestId, "requestId"),
                threadId: text(source.threadId, "threadId"),
                turnId: text(source.turnId, "turnId"),
                toolName: source.toolName,
                arguments: object(source.arguments, "arguments"),
                expectedDelivery: parseAgentExpectedDelivery(source.expectedDelivery),
                createdAt: parseInstant(source.createdAt, "createdAt"),
            };
        }
        case "final_decision": {
            const source = exactObject(value, "final_decision 事件", ["kind", "threadId", "turnId", "message", "expectedDelivery"]);
            return {
                kind: "final_decision",
                threadId: text(source.threadId, "threadId"),
                turnId: text(source.turnId, "turnId"),
                message: text(source.message, "message"),
                expectedDelivery: parseAgentExpectedDelivery(source.expectedDelivery),
            };
        }
        default:
            throw new Error(`不受支持的本机 Agent 事件: ${String(header.kind)}`);
    }
}

export function parseLocalAgentThread(value: unknown): LocalAgentThread {
    const source = exactObject(value, "本机 Agent 线程", ["threadId", "canvasId", "model", "createdAt", "updatedAt", "archivedAt", "turns"]);
    const result: LocalAgentThread = {
        threadId: text(source.threadId, "threadId"),
        canvasId: text(source.canvasId, "canvasId"),
        model: text(source.model, "model"),
        createdAt: parseInstant(source.createdAt, "createdAt"),
        updatedAt: parseInstant(source.updatedAt, "updatedAt"),
        turns: array(source.turns, "turns").map(parseLocalAgentThreadTurn),
    };
    if (source.archivedAt !== undefined) result.archivedAt = parseInstant(source.archivedAt, "archivedAt");
    return result;
}

export function parseLocalAgentThreadList(value: unknown): LocalAgentThread[] {
    const source = exactObject(value, "本机 Agent 线程列表", ["threads"]);
    return array(source.threads, "threads").map(parseLocalAgentThread);
}

function parseLocalAgentThreadTurn(value: unknown): LocalAgentThreadTurn {
    const source = exactObject(value, "本机 Agent turn", ["turnId", "status", "message", "attachments", "events", "createdAt", "completedAt", "errorMessage"]);
    if (source.status !== "running" && source.status !== "completed" && source.status !== "failed" && source.status !== "cancelled") {
        throw new Error(`不受支持的本机 Agent turn 状态: ${String(source.status)}`);
    }
    array(source.attachments, "attachments");
    array(source.events, "events");
    const result: LocalAgentThreadTurn = {
        turnId: text(source.turnId, "turnId"),
        status: source.status,
        message: text(source.message, "message"),
        createdAt: parseInstant(source.createdAt, "createdAt"),
    };
    if (source.completedAt !== undefined) result.completedAt = parseInstant(source.completedAt, "completedAt");
    if (source.errorMessage !== undefined) result.errorMessage = text(source.errorMessage, "errorMessage");
    return result;
}

function parseInstant(value: unknown, label: string): string {
    const source = text(value, label);
    if (Number.isNaN(Date.parse(source)) || !source.endsWith("Z")) throw new Error(`${label} 必须是 UTC ISO-8601 时间`);
    return source;
}
