import assert from "node:assert/strict";
import { test } from "node:test";

import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { InMemoryTransport } from "@modelcontextprotocol/sdk/inMemory.js";

import { CanvasSession } from "../src/canvas-session";
import { MCP_TOOL_NAME_MAP, createCanvasMcpServer } from "../src/mcp-server";
import {
  canvasReadToolInputSchema,
  mediaGenerateToolInputSchema,
} from "../src/mcp-tool-schemas";

test("canvas.read MCP contract rejects a viewport scope the backend cannot authorize", () => {
  const input = {
    arguments: {
      canvasId: "canvas-1",
      selectedNodeIds: [],
      includeViewport: false,
    },
    expectedDelivery: {
      kind: "answer",
      completionCriteria: [{ fact: "final_message" }],
    },
  };

  assert.equal(canvasReadToolInputSchema.safeParse(input).success, false);
});

test("media.generate MCP contract rejects guessed image parameter keys", () => {
  const input = {
    arguments: {
      mediaKind: "image",
      modelRecordId: "model-record-1",
      modelKey: "gpt-image-2",
      parameters: {
        prompt: "雨夜老巷中的红灯笼照亮白猫",
        aspectRatio: "16:9",
        resolution: "1K",
        outputCount: 1,
      },
      sourceResourceIds: [],
      targetCanvasNodeId: "image-node-1",
      clientRequestId: "image-request-1",
    },
    expectedDelivery: {
      kind: "answer",
      completionCriteria: [{ fact: "final_message" }],
    },
  };

  assert.equal(mediaGenerateToolInputSchema.safeParse(input).success, false);
});

test("MCP 只暴露六种纯映射能力并通过 CanvasSession 等待权威结果", async () => {
  let emittedResolve: ((value: void) => void) | undefined;
  const emitted = new Promise<void>((resolve) => {
    emittedResolve = resolve;
  });
  const events: Array<{
    requestId: string;
    threadId: string;
    turnId: string;
    toolName: string;
    protocolVersion: number;
    kind: string;
    expectedDelivery: { kind: string };
  }> = [];
  const session = new CanvasSession({
    emit: (event) => {
      events.push(event);
      emittedResolve?.();
    },
    timeoutMs: 1_000,
  });
  let requestSequence = 0;
  const server = createCanvasMcpServer({
    session,
    threadId: "thread-1",
    turnId: "turn-1",
    nextRequestId: () => `tool-request-${++requestSequence}`,
  });
  const client = new Client({ name: "canvas-agent-test", version: "1.0.0" });
  const [clientTransport, serverTransport] =
    InMemoryTransport.createLinkedPair();
  await server.connect(serverTransport);
  await client.connect(clientTransport);
  try {
    const tools = await client.listTools();
    assert.deepEqual(
      tools.tools.map((tool) => tool.name).sort(),
      Object.keys(MCP_TOOL_NAME_MAP).sort(),
    );
    const mediaGenerate = tools.tools.find(
      (tool) => tool.name === "media_generate",
    );
    const canvasApplyOps = tools.tools.find(
      (tool) => tool.name === "canvas_apply_ops",
    );
    const skillsLoad = tools.tools.find((tool) => tool.name === "skills_load");
    assert.match(JSON.stringify(mediaGenerate?.inputSchema), /"mediaKind"/);
    assert.match(JSON.stringify(mediaGenerate?.inputSchema), /"modelRecordId"/);
    assert.match(JSON.stringify(canvasApplyOps?.inputSchema), /"operations"/);
    assert.match(JSON.stringify(canvasApplyOps?.inputSchema), /"add_node"/);
    assert.match(canvasApplyOps?.description ?? "", /metadata\.prompt/);
    assert.match(mediaGenerate?.description ?? "", /parameters\.prompt/);
    assert.match(JSON.stringify(skillsLoad?.inputSchema), /"checksum"/);

    const call = client.callTool({
      name: "canvas_get_state",
      arguments: {
        arguments: {
          canvasId: "canvas-1",
          selectedNodeIds: [],
          includeViewport: true,
        },
        expectedDelivery: {
          kind: "answer",
          completionCriteria: [{ fact: "final_message" }],
        },
      },
    });
    await emitted;
    assert.equal(events.length, 1);
    assert.equal(events[0]?.requestId, "tool-request-1");
    assert.equal(events[0]?.threadId, "thread-1");
    assert.equal(events[0]?.turnId, "turn-1");
    assert.equal(events[0]?.toolName, "canvas.read");
    assert.equal(events[0]?.protocolVersion, 1);
    assert.equal(events[0]?.kind, "tool_call");
    assert.equal(events[0]?.expectedDelivery.kind, "answer");
    session.resolveToolResult({
      requestId: "tool-request-1",
      threadId: "thread-1",
      turnId: "turn-1",
      toolName: "canvas.read",
      succeeded: true,
      output: { revision: 9 },
    });
    const result = await call;
    assert.equal(result.isError, false);
    assert.deepEqual(result.structuredContent, { revision: 9 });
  } finally {
    await client.close();
    await server.close();
  }
});
