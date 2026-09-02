import type { AgentToolName } from "@/services/api/agent-capabilities";

import { parseLocalAgentEvent, parseLocalAgentStartResponse, parseLocalAgentThread, parseLocalAgentThreadList, type LocalAgentEvent, type LocalAgentStartResponse, type LocalAgentThread } from "./local-agent-contracts";
import { validateLocalAgentBaseUrl, validateLocalAgentToken } from "./local-agent-session";

export type LocalAgentAttachment = { kind: "image" | "file"; name: string; mimeType: string; url: string };
export type LocalAgentStartTurnInput = { requestId: string; canvasId: string; threadId?: string; message: string; attachments: LocalAgentAttachment[]; ephemeral?: true };
export type LocalAgentToolResultInput = {
    requestId: string;
    threadId: string;
    turnId: string;
    toolName: AgentToolName;
    succeeded: boolean;
    output: Record<string, unknown>;
    errorCode?: string;
    errorMessage?: string;
};
export type LocalAgentHealth = { version: string; protocolVersion: 1; ready: true };

type FetchPort = (input: RequestInfo | URL, init?: RequestInit) => Promise<Response>;

export type LocalAgentClientPort = Pick<LocalAgentHttpClient, "health" | "streamEvents" | "startTurn" | "cancelTurn" | "deliverToolResult" | "listThreads" | "readThread" | "archiveThread">;

export class LocalAgentRequestError extends Error {
    constructor(
        readonly status: number,
        readonly code: string,
        message: string,
    ) {
        super(message);
        this.name = "LocalAgentRequestError";
    }
}

export class LocalAgentHttpClient {
    readonly #baseUrl: string;
    readonly #token: string;
    readonly #fetch: FetchPort;

    constructor(options: { baseUrl: string; token: string; fetch?: FetchPort }) {
        this.#baseUrl = validateLocalAgentBaseUrl(options.baseUrl);
        this.#token = validateLocalAgentToken(options.token);
        this.#fetch = options.fetch ?? fetch.bind(globalThis);
    }

    async health(signal?: AbortSignal): Promise<LocalAgentHealth> {
        const response = await this.#fetch(`${this.#baseUrl}/health`, { signal });
        const payload = await readJson(response);
        if (!response.ok) throw parseRequestError(response.status, payload);
        if (!isRecord(payload) || typeof payload.version !== "string" || payload.protocolVersion !== 1 || payload.ready !== true) {
            throw new Error("本机 Agent 健康检查响应无效");
        }
        return { version: payload.version, protocolVersion: 1, ready: true };
    }

    async streamEvents(signal: AbortSignal, onEvent: (event: LocalAgentEvent) => void): Promise<void> {
        const response = await this.#fetch(`${this.#baseUrl}/events`, { headers: this.#headers(), signal });
        if (!response.ok) throw parseRequestError(response.status, await readJson(response));
        if (!response.body || !response.headers.get("content-type")?.startsWith("text/event-stream")) {
            throw new Error("本机 Agent 事件流响应无效");
        }
        const reader = response.body.getReader();
        const decoder = new TextDecoder();
        let buffer = "";
        while (true) {
            const chunk = await reader.read();
            buffer += decoder.decode(chunk.value, { stream: !chunk.done });
            let boundary = buffer.indexOf("\n\n");
            while (boundary >= 0) {
                const frame = buffer.slice(0, boundary);
                buffer = buffer.slice(boundary + 2);
                const payload = frame
                    .split("\n")
                    .filter((line) => line.startsWith("data:"))
                    .map((line) => line.slice(5).trimStart())
                    .join("\n");
                if (payload) onEvent(parseLocalAgentEvent(JSON.parse(payload) as unknown));
                boundary = buffer.indexOf("\n\n");
            }
            if (chunk.done) break;
        }
        if (buffer.trim()) throw new Error("本机 Agent 事件流在不完整帧中断开");
    }

    async startTurn(input: LocalAgentStartTurnInput): Promise<LocalAgentStartResponse> {
        const path = input.threadId ? `/agent/codex/threads/${encodeURIComponent(input.threadId)}/resume` : "/agent/codex/turns";
        return parseLocalAgentStartResponse(await this.#json(path, { method: "POST", body: JSON.stringify(input) }));
    }

    async listThreads(canvasId: string): Promise<LocalAgentThread[]> {
        return parseLocalAgentThreadList(await this.#json(`/agent/codex/threads?canvasId=${encodeURIComponent(canvasId)}`, { method: "GET" }));
    }

    async readThread(canvasId: string, threadId: string): Promise<LocalAgentThread> {
        return parseLocalAgentThread(await this.#json(`/agent/codex/threads/${encodeURIComponent(threadId)}?canvasId=${encodeURIComponent(canvasId)}`, { method: "GET" }));
    }

    async archiveThread(canvasId: string, threadId: string): Promise<void> {
        await this.#void(`/agent/codex/threads/${encodeURIComponent(threadId)}/archive`, { method: "POST", body: JSON.stringify({ canvasId }) });
    }

    async cancelTurn(turnId: string): Promise<void> {
        await this.#void(`/agent/codex/turns/${encodeURIComponent(turnId)}/cancel`, { method: "POST" });
    }

    async deliverToolResult(input: LocalAgentToolResultInput): Promise<void> {
        await this.#void(`/tools/${encodeURIComponent(input.requestId)}/results`, { method: "POST", body: JSON.stringify(input) });
    }

    async #json(path: string, init: RequestInit): Promise<unknown> {
        const response = await this.#fetch(`${this.#baseUrl}${path}`, { ...init, headers: this.#headers(init.headers) });
        const payload = await readJson(response);
        if (!response.ok) throw parseRequestError(response.status, payload);
        return payload;
    }

    async #void(path: string, init: RequestInit): Promise<void> {
        const response = await this.#fetch(`${this.#baseUrl}${path}`, { ...init, headers: this.#headers(init.headers) });
        if (!response.ok) throw parseRequestError(response.status, await readJson(response));
    }

    #headers(existing?: HeadersInit): Headers {
        const headers = new Headers(existing);
        headers.set("X-HMaigc-Agent-Token", this.#token);
        headers.set("Content-Type", "application/json");
        return headers;
    }
}

async function readJson(response: Response): Promise<unknown> {
    try {
        return await response.json();
    } catch (cause) {
        throw new Error(`本机 Agent 返回了无法解析的响应（HTTP ${response.status}）`, { cause });
    }
}

function parseRequestError(status: number, value: unknown): LocalAgentRequestError {
    if (isRecord(value) && isRecord(value.error) && typeof value.error.code === "string" && typeof value.error.message === "string") {
        return new LocalAgentRequestError(status, value.error.code, value.error.message);
    }
    return new LocalAgentRequestError(status, "local_agent_request_failed", `本机 Agent 请求失败（HTTP ${status}）`);
}

function isRecord(value: unknown): value is Record<string, unknown> {
    return typeof value === "object" && value !== null && !Array.isArray(value);
}
