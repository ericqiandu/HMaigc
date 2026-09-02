import assert from "node:assert/strict";
import { describe, it } from "node:test";

import { LoopbackCanvasToolSession } from "../src/loopback-tool-session.js";

describe("LoopbackCanvasToolSession", () => {
  it("forwards the complete delivery contract to the protected local tool endpoint", async () => {
    let received: unknown;
    const session = new LoopbackCanvasToolSession({
      url: "http://127.0.0.1:17371",
      token: "a".repeat(64),
      fetch: async (_input, init) => {
        received = JSON.parse(String(init?.body)) as unknown;
        return Response.json({
          requestId: "request-1",
          threadId: "thread-1",
          turnId: "turn-1",
          toolName: "canvas.read",
          succeeded: true,
          output: { revision: 7 },
        });
      },
    });
    const expectedDelivery = {
      kind: "canvas_change" as const,
      targetCanvasId: "canvas-1",
      completionCriteria: [{ fact: "canvas_revision" as const }],
    };

    await session.requestTool({
      requestId: "request-1",
      threadId: "thread-1",
      turnId: "turn-1",
      toolName: "canvas.read",
      arguments: {
        canvasId: "canvas-1",
        selectedNodeIds: [],
        includeViewport: true,
      },
      expectedDelivery,
    });

    assert.deepEqual(received, {
      requestId: "request-1",
      threadId: "thread-1",
      turnId: "turn-1",
      toolName: "canvas.read",
      arguments: {
        canvasId: "canvas-1",
        selectedNodeIds: [],
        includeViewport: true,
      },
      expectedDelivery,
    });
  });
});
