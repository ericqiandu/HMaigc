import assert from "node:assert/strict";
import { test } from "node:test";

import {
  APPROVAL_RESULT_GRACE_MS,
  CanvasSession,
  DEFAULT_CANVAS_TOOL_RESULT_TIMEOUT_MS,
} from "../src/canvas-session";

const request = {
  requestId: "tool-request-1",
  threadId: "thread-1",
  turnId: "turn-1",
  toolName: "canvas.read",
  arguments: {
    canvasId: "canvas-1",
    selectedNodeIds: [],
    includeViewport: true,
  },
  expectedDelivery: {
    kind: "answer",
    completionCriteria: [{ fact: "final_message" }],
  },
} as const;

const result = {
  requestId: "tool-request-1",
  threadId: "thread-1",
  turnId: "turn-1",
  toolName: "canvas.read",
  succeeded: true,
  output: { revision: 7 },
} as const;

test("CanvasSession 对相同结果重放保持幂等并拒绝冲突结果", async () => {
  const events: unknown[] = [];
  const session = new CanvasSession({
    emit: (event) => events.push(event),
    timeoutMs: 1_000,
  });
  const pending = session.requestTool(request);
  assert.equal(events.length, 1);
  assert.deepEqual(await session.resolveToolResult(result), undefined);
  assert.deepEqual(await pending, result);
  assert.doesNotThrow(() => session.resolveToolResult(result));
  assert.throws(
    () => session.resolveToolResult({ ...result, output: { revision: 8 } }),
    /冲突/,
  );

  const secondPending = session.requestTool({
    ...request,
    requestId: "tool-request-2",
  });
  assert.throws(
    () =>
      session.resolveToolResult({
        ...result,
        requestId: "tool-request-2",
        turnId: "other-turn",
      }),
    /turnId/,
  );
  session.resolveToolResult({ ...result, requestId: "tool-request-2" });
  await secondPending;
});

test("CanvasSession 在浏览器断开或等待超时时显式失败", async () => {
  const disconnected = new CanvasSession({
    emit: () => undefined,
    timeoutMs: 1_000,
  });
  const disconnectPending = disconnected.requestTool(request);
  disconnected.disconnect("browser_disconnected");
  await assert.rejects(disconnectPending, /browser_disconnected/);

  const timedOut = new CanvasSession({ emit: () => undefined, timeoutMs: 10 });
  await assert.rejects(timedOut.requestTool(request), /超时/);
});

test("CanvasSession 默认等待覆盖网站审批有效期并保留结果交付余量", () => {
  const websiteApprovalLifetimeMs = 15 * 60_000;
  assert.equal(
    DEFAULT_CANVAS_TOOL_RESULT_TIMEOUT_MS,
    websiteApprovalLifetimeMs + APPROVAL_RESULT_GRACE_MS,
  );
  assert.ok(APPROVAL_RESULT_GRACE_MS >= 60_000);
  assert.doesNotThrow(() => new CanvasSession({ emit: () => undefined }));
  assert.doesNotThrow(
    () =>
      new CanvasSession({
        emit: () => undefined,
        timeoutMs: DEFAULT_CANVAS_TOOL_RESULT_TIMEOUT_MS,
      }),
  );
});
