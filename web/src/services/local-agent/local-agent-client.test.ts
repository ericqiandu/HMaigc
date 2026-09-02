import assert from "node:assert/strict";
import { describe, it } from "node:test";

import { LocalAgentHttpClient, LocalAgentRequestError } from "./local-agent-client";

const token = "ab".repeat(32);
const expectedDelivery = {
    kind: "canvas_change",
    targetCanvasId: "canvas-1",
    completionCriteria: [{ fact: "canvas_revision" }],
} as const;

describe("LocalAgentHttpClient", () => {
    it("keeps the token in the header and never in the URL", async () => {
        const calls: Array<{ url: string; init?: RequestInit }> = [];
        const client = new LocalAgentHttpClient({
            baseUrl: "http://127.0.0.1:17371",
            token,
            fetch: async (input, init) => {
                calls.push({ url: String(input), init });
                return Response.json({ threadId: "thread-1", turnId: "turn-1" }, { status: 202 });
            },
        });

        const result = await client.startTurn({ requestId: "request-1", canvasId: "canvas-1", message: "读取画布", attachments: [] });
        assert.deepEqual(result, { threadId: "thread-1", turnId: "turn-1" });
        assert.equal(calls.length, 1);
        assert.equal(calls[0]?.url, "http://127.0.0.1:17371/agent/codex/turns");
        assert.equal(calls[0]?.url.includes(token), false);
        assert.equal(new Headers(calls[0]?.init?.headers).get("x-hmaigc-agent-token"), token);
    });

    it("resumes an existing Codex thread through the dedicated endpoint", async () => {
        const calls: Array<{ url: string; init?: RequestInit }> = [];
        const client = new LocalAgentHttpClient({
            baseUrl: "http://127.0.0.1:17371",
            token,
            fetch: async (input, init) => {
                calls.push({ url: String(input), init });
                return Response.json({ threadId: "thread-1", turnId: "turn-2" }, { status: 202 });
            },
        });

        const result = await client.startTurn({
            requestId: "repair-1",
            canvasId: "canvas-1",
            threadId: "thread-1",
            message: "继续修复交付",
            attachments: [],
            ephemeral: true,
        });

        assert.deepEqual(result, { threadId: "thread-1", turnId: "turn-2" });
        assert.equal(calls[0]?.url, "http://127.0.0.1:17371/agent/codex/threads/thread-1/resume");
        assert.deepEqual(JSON.parse(String(calls[0]?.init?.body)), {
            requestId: "repair-1",
            canvasId: "canvas-1",
            threadId: "thread-1",
            message: "继续修复交付",
            attachments: [],
            ephemeral: true,
        });
    });

    it("parses fragmented SSE events without EventSource query authentication", async () => {
        const encoder = new TextEncoder();
        const stream = new ReadableStream<Uint8Array>({
            start(controller) {
                controller.enqueue(encoder.encode('data: {"kind":"connec'));
                controller.enqueue(
                    encoder.encode(
                        'ted"}\n\n: heartbeat\n\ndata: ' +
                            JSON.stringify({
                                protocolVersion: 1,
                                kind: "tool_call",
                                requestId: "tool-1",
                                threadId: "thread-1",
                                turnId: "turn-1",
                                toolName: "canvas.read",
                                arguments: { canvasId: "canvas-1", selectedNodeIds: [], includeViewport: true },
                                expectedDelivery,
                                createdAt: "2026-09-01T00:00:00.000Z",
                            }) +
                            "\n\n",
                    ),
                );
                controller.close();
            },
        });
        let observedUrl = "";
        let observedHeaders = new Headers();
        const client = new LocalAgentHttpClient({
            baseUrl: "http://127.0.0.1:17371",
            token,
            fetch: async (input, init) => {
                observedUrl = String(input);
                observedHeaders = new Headers(init?.headers);
                return new Response(stream, { status: 200, headers: { "content-type": "text/event-stream" } });
            },
        });
        const events: string[] = [];
        await client.streamEvents(new AbortController().signal, (event) => {
            events.push(event.kind);
        });
        assert.deepEqual(events, ["connected", "tool_call"]);
        assert.equal(observedUrl, "http://127.0.0.1:17371/events");
        assert.equal(observedUrl.includes(token), false);
        assert.equal(observedHeaders.get("x-hmaigc-agent-token"), token);
    });

    it("reports non-2xx once and does not retry writes", async () => {
        let attempts = 0;
        const client = new LocalAgentHttpClient({
            baseUrl: "http://127.0.0.1:17371",
            token,
            fetch: async () => {
                attempts += 1;
                return Response.json({ error: { code: "request_replay_conflict", message: "请求身份冲突" } }, { status: 409 });
            },
        });
        const error = await client
            .deliverToolResult({
                requestId: "tool-1",
                threadId: "thread-1",
                turnId: "turn-1",
                toolName: "canvas.read",
                succeeded: false,
                output: {},
                errorCode: "agent_external_decision_conflict",
                errorMessage: "决策版本冲突",
            })
            .catch((cause: unknown) => cause);
        assert.ok(error instanceof LocalAgentRequestError);
        assert.equal(error.code, "request_replay_conflict");
        assert.equal(attempts, 1);
    });

    it("lists canvas-scoped local Codex history without exposing the token", async () => {
        let observedUrl = "";
        const client = new LocalAgentHttpClient({
            baseUrl: "http://127.0.0.1:17371",
            token,
            fetch: async (input) => {
                observedUrl = String(input);
                return Response.json({
                    threads: [
                        {
                            threadId: "thread-1",
                            canvasId: "canvas/a",
                            model: "gpt-5",
                            createdAt: "2026-09-01T00:00:00.000Z",
                            updatedAt: "2026-09-01T00:01:00.000Z",
                            turns: [{ turnId: "turn-1", status: "completed", message: "读取画布", attachments: [], events: [], createdAt: "2026-09-01T00:00:00.000Z", completedAt: "2026-09-01T00:01:00.000Z" }],
                        },
                    ],
                });
            },
        });
        const threads = await client.listThreads("canvas/a");
        assert.equal(threads[0]?.turns[0]?.message, "读取画布");
        assert.equal(observedUrl, "http://127.0.0.1:17371/agent/codex/threads?canvasId=canvas%2Fa");
        assert.equal(observedUrl.includes(token), false);
    });
});
