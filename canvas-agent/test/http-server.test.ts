import assert from "node:assert/strict";
import type { AddressInfo } from "node:net";
import test from "node:test";

import type { LocalAgentConfig } from "../src/config.js";
import { LocalAgentEventHub } from "../src/event-hub.js";
import {
  createLocalAgentHttpApp,
  type AttachmentIngestionPort,
  type LocalAgentThreadServicePort,
} from "../src/http-server.js";

const token = "ab".repeat(32);
const config: LocalAgentConfig = {
  url: "http://127.0.0.1:17371",
  token,
  allowedOrigins: ["http://127.0.0.1:3000"],
  allowedAttachmentOrigins: ["https://static.hm.kunagent.com"],
  codex: { model: "gpt-5.6-sol" },
  canvases: { "canvas-1": { workspaceRoot: "E:/workspace/canvas-1" } },
};

class FakeThreadService implements LocalAgentThreadServicePort {
  cancelled: string[] = [];

  async startTurn(): Promise<
    ReturnType<LocalAgentThreadServicePort["startTurn"]> extends Promise<
      infer Result
    >
      ? Result
      : never
  > {
    return {
      threadId: "local-thread-1",
      turnId: "turn-1",
      events: (async function* events() {
        yield {
          kind: "turn_started",
          threadId: "local-thread-1",
          turnId: "turn-1",
        } as const;
        yield {
          kind: "turn_completed",
          threadId: "local-thread-1",
          turnId: "turn-1",
          event: {
            type: "turn.completed",
            usage: {
              input_tokens: 1,
              cached_input_tokens: 0,
              cache_write_input_tokens: 0,
              output_tokens: 1,
              reasoning_output_tokens: 0,
            },
          },
        } as const;
      })(),
    };
  }

  async resumeTurn(): ReturnType<LocalAgentThreadServicePort["resumeTurn"]> {
    return this.startTurn();
  }

  async listThreads(): ReturnType<LocalAgentThreadServicePort["listThreads"]> {
    return [];
  }

  async readThread(): ReturnType<LocalAgentThreadServicePort["readThread"]> {
    throw new Error("E:/private/workspace/secret.txt is unavailable");
  }

  async archiveThread(): Promise<void> {}

  cancelTurn(turnId: string): boolean {
    this.cancelled.push(turnId);
    return true;
  }
}

const attachments: AttachmentIngestionPort = {
  async ingest() {
    return { directory: "E:/managed/turn", attachments: [] };
  },
  async cleanup() {},
};

const protectedHeaders = {
  Origin: "http://127.0.0.1:3000",
  "X-HMaigc-Agent-Token": token,
};

test("loopback HTTP enforces Origin/token/PNA/body limits without leaking paths", async () => {
  const service = new FakeThreadService();
  const app = createLocalAgentHttpApp({
    config,
    threadService: service,
    attachments,
    bodyLimitBytes: 1024,
    version: "0.1.0-test",
  });
  const server = app.listen(0, "127.0.0.1");
  await new Promise<void>((resolve, reject) => {
    server.once("listening", resolve);
    server.once("error", reject);
  });
  const address = server.address() as AddressInfo;
  const base = `http://127.0.0.1:${address.port}`;
  try {
    const health = await fetch(`${base}/health`);
    assert.equal(health.status, 200);
    assert.deepEqual(await health.json(), {
      version: "0.1.0-test",
      protocolVersion: 1,
      ready: true,
    });
    const browserHealth = await fetch(`${base}/health`, {
      headers: { Origin: "http://127.0.0.1:3000" },
    });
    assert.equal(browserHealth.status, 200);
    assert.equal(
      browserHealth.headers.get("access-control-allow-origin"),
      "http://127.0.0.1:3000",
    );
    assert.equal(
      (
        await fetch(`${base}/health`, {
          headers: { Origin: "http://127.0.0.1:3000.evil.test" },
        })
      ).status,
      403,
    );

    assert.equal(
      (await fetch(`${base}/agent/codex/threads?canvasId=canvas-1`)).status,
      403,
    );
    assert.equal(
      (
        await fetch(
          `${base}/agent/codex/threads?canvasId=canvas-1&token=${token}`,
          { headers: protectedHeaders },
        )
      ).status,
      403,
    );
    assert.equal(
      (
        await fetch(`${base}/agent/codex/threads?canvasId=canvas-1`, {
          headers: {
            ...protectedHeaders,
            Origin: "http://127.0.0.1:3000.evil.test",
          },
        })
      ).status,
      403,
    );
    assert.equal(
      (
        await fetch(`${base}/agent/codex/threads?canvasId=canvas-1`, {
          headers: protectedHeaders,
        })
      ).status,
      200,
    );

    const preflight = await fetch(`${base}/agent/codex/turns`, {
      method: "OPTIONS",
      headers: {
        Origin: "http://127.0.0.1:3000",
        "Access-Control-Request-Method": "POST",
        "Access-Control-Request-Private-Network": "true",
      },
    });
    assert.equal(preflight.status, 204);
    assert.equal(
      preflight.headers.get("access-control-allow-origin"),
      "http://127.0.0.1:3000",
    );
    assert.equal(
      preflight.headers.get("access-control-allow-private-network"),
      "true",
    );

    const oversized = await fetch(`${base}/agent/codex/turns`, {
      method: "POST",
      headers: { ...protectedHeaders, "content-type": "application/json" },
      body: JSON.stringify({ padding: "x".repeat(2048) }),
    });
    assert.equal(oversized.status, 413);

    const leaked = await fetch(
      `${base}/agent/codex/threads/local-thread-1?canvasId=canvas-1`,
      { headers: protectedHeaders },
    );
    assert.equal(leaked.status, 500);
    assert.equal((await leaked.text()).includes("E:/private"), false);
  } finally {
    await new Promise<void>((resolve, reject) =>
      server.close((error) => (error ? reject(error) : resolve())),
    );
  }
});

test("event stream delivers normalized Codex events and rejects a second client", async () => {
  const app = createLocalAgentHttpApp({
    config,
    threadService: new FakeThreadService(),
    attachments,
    version: "0.1.0-test",
  });
  const server = app.listen(0, "127.0.0.1");
  await new Promise<void>((resolve, reject) => {
    server.once("listening", resolve);
    server.once("error", reject);
  });
  const address = server.address() as AddressInfo;
  const base = `http://127.0.0.1:${address.port}`;
  const abort = new AbortController();
  try {
    const stream = await fetch(`${base}/events`, {
      headers: protectedHeaders,
      signal: abort.signal,
    });
    assert.equal(stream.status, 200);
    assert.equal(
      stream.headers.get("content-type")?.startsWith("text/event-stream"),
      true,
    );
    const reader = stream.body?.getReader();
    assert.ok(reader);
    const connected = new TextDecoder().decode((await reader.read()).value);
    assert.match(connected, /"kind":"connected"/);

    const duplicate = await fetch(`${base}/events`, {
      headers: protectedHeaders,
    });
    assert.equal(duplicate.status, 409);

    const started = await fetch(`${base}/agent/codex/turns`, {
      method: "POST",
      headers: { ...protectedHeaders, "content-type": "application/json" },
      body: JSON.stringify({
        requestId: "request-1",
        canvasId: "canvas-1",
        message: "读取画布",
        attachments: [],
      }),
    });
    assert.equal(started.status, 202);
    assert.deepEqual(await started.json(), {
      threadId: "local-thread-1",
      turnId: "turn-1",
    });
    let streamed = "";
    while (!streamed.includes("turn_completed")) {
      const chunk = await reader.read();
      assert.equal(chunk.done, false);
      streamed += new TextDecoder().decode(chunk.value);
    }
    assert.match(streamed, /"kind":"turn_started"/);
    assert.match(streamed, /"kind":"turn_completed"/);
  } finally {
    abort.abort();
    await new Promise<void>((resolve, reject) =>
      server.close((error) => (error ? reject(error) : resolve())),
    );
  }
});

test("SSE backpressure waits for drain without disconnecting the active client", async () => {
  const hub = new LocalAgentEventHub();
  let ended = false;
  let disconnected = false;
  let writes = 0;
  let drainStarted = false;
  let releaseDrain: (() => void) | undefined;
  const connection = {
    write: () => {
      writes += 1;
      return writes === 1;
    },
    waitForDrain: () => {
      drainStarted = true;
      return new Promise<void>((resolve) => {
        releaseDrain = resolve;
      });
    },
    end: () => {
      ended = true;
    },
    onClose: () => {},
  };
  hub.connect(connection, () => {
    disconnected = true;
  });

  const emission = Promise.resolve().then(() =>
    hub.emit({ kind: "turn_started" }),
  );
  void emission.catch(() => undefined);
  await new Promise<void>((resolve) => setImmediate(resolve));

  assert.equal(drainStarted, true);
  assert.ok(releaseDrain);
  releaseDrain();
  await emission;

  assert.equal(ended, false);
  assert.equal(disconnected, false);
  assert.equal(hub.connected, true);
});

test("MCP request crosses the SSE bridge and resolves exactly from the browser result", async () => {
  const app = createLocalAgentHttpApp({
    config,
    threadService: new FakeThreadService(),
    attachments,
    version: "0.1.0-test",
  });
  const server = app.listen(0, "127.0.0.1");
  await new Promise<void>((resolve, reject) => {
    server.once("listening", resolve);
    server.once("error", reject);
  });
  const address = server.address() as AddressInfo;
  const base = `http://127.0.0.1:${address.port}`;
  const abort = new AbortController();
  try {
    const stream = await fetch(`${base}/events`, {
      headers: protectedHeaders,
      signal: abort.signal,
    });
    const reader = stream.body?.getReader();
    assert.ok(reader);
    await reader.read();

    const internal = fetch(`${base}/internal/mcp/tools`, {
      method: "POST",
      headers: {
        "content-type": "application/json",
        "x-hmaigc-agent-token": token,
      },
      body: JSON.stringify({
        requestId: "tool-request-1",
        threadId: "local-thread-1",
        turnId: "turn-1",
        toolName: "canvas.read",
        arguments: { canvasId: "canvas-1" },
        expectedDelivery: {
          kind: "answer",
          completionCriteria: [{ fact: "final_message" }],
        },
      }),
    });
    const toolEvent = new TextDecoder().decode((await reader.read()).value);
    assert.match(toolEvent, /"kind":"tool_call"/);
    assert.match(toolEvent, /"requestId":"tool-request-1"/);

    const earlyInternalResponse = await Promise.race([
      internal,
      new Promise<null>((resolve) => setTimeout(() => resolve(null), 1_000)),
    ]);
    assert.ok(
      earlyInternalResponse,
      "MCP 代理应立即收到响应头，避免长审批触发 fetch 响应头超时",
    );
    assert.equal(earlyInternalResponse.status, 200);

    const result = await fetch(`${base}/tools/tool-request-1/results`, {
      method: "POST",
      headers: { ...protectedHeaders, "content-type": "application/json" },
      body: JSON.stringify({
        requestId: "tool-request-1",
        threadId: "local-thread-1",
        turnId: "turn-1",
        toolName: "canvas.read",
        succeeded: true,
        output: { revision: 9 },
      }),
    });
    assert.equal(result.status, 204);
    assert.equal(
      ((await earlyInternalResponse.json()) as { output: { revision: number } })
        .output.revision,
      9,
    );

    const invalidBody = await fetch(`${base}/internal/mcp/tools`, {
      method: "POST",
      headers: {
        "content-type": "application/json",
        "x-hmaigc-agent-token": token,
      },
      body: JSON.stringify({ unknown: true }),
    });
    assert.equal(invalidBody.status, 400);
  } finally {
    abort.abort();
    await new Promise<void>((resolve, reject) =>
      server.close((error) => (error ? reject(error) : resolve())),
    );
  }
});
