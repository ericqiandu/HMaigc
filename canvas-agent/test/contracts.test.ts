import assert from "node:assert/strict";
import { test } from "node:test";

import {
  LOCAL_AGENT_FINAL_DECISION_JSON_SCHEMA,
  parseCanvasToolRequestInput,
  parseFinalCodexDecision,
  parseStartCodexTurnInput,
  parseToolResultInput,
} from "../src/contracts";

const startTurn = {
  requestId: "request-1",
  canvasId: "canvas-1",
  message: "读取当前画布并给出建议",
  attachments: [
    {
      kind: "image",
      name: "参考图.png",
      mimeType: "image/png",
      url: "https://static.example.com/reference.png",
    },
  ],
} as const;

test("Codex turn 请求拒绝未知字段和超量附件", () => {
  assert.deepEqual(parseStartCodexTurnInput(startTurn), startTurn);
  assert.throws(
    () => parseStartCodexTurnInput({ ...startTurn, fallbackModel: "other" }),
    /unrecognized|未知|字段/i,
  );
  assert.throws(
    () =>
      parseStartCodexTurnInput({
        ...startTurn,
        attachments: Array.from({ length: 17 }, (_, index) => ({
          kind: "image",
          name: `reference-${index}.png`,
          mimeType: "image/png",
          url: `https://static.example.com/reference-${index}.png`,
        })),
      }),
    /16|附件/,
  );
  assert.throws(
    () => parseStartCodexTurnInput({ ...startTurn, ephemeral: true }),
    /threadId|临时纠偏/,
  );
  assert.equal(
    parseStartCodexTurnInput({
      ...startTurn,
      threadId: "thread-1",
      ephemeral: true,
    }).ephemeral,
    true,
  );
});

test("工具结果必须携带完整关联身份且拒绝未知字段", () => {
  const result = {
    requestId: "tool-request-1",
    threadId: "thread-1",
    turnId: "turn-1",
    toolName: "canvas.read",
    succeeded: true,
    output: { revision: 4 },
  } as const;
  assert.deepEqual(parseToolResultInput(result), result);
  assert.throws(
    () => parseToolResultInput({ ...result, threadId: "" }),
    /threadId/,
  );
  assert.throws(
    () => parseToolResultInput({ ...result, directCanvasWrite: true }),
    /unrecognized|未知|字段/i,
  );
});

test("工具提案与最终回答必须携带同一类结构化交付契约", () => {
  const expectedDelivery = {
    kind: "canvas_change",
    requiredArtifacts: ["canvas_revision"],
    targetCanvasId: "canvas-1",
    completionCriteria: [{ fact: "canvas_revision" }],
  } as const;
  const toolRequest = {
    requestId: "tool-request-1",
    threadId: "thread-1",
    turnId: "turn-1",
    toolName: "canvas.apply_ops",
    arguments: { canvasId: "canvas-1" },
    expectedDelivery,
  } as const;
  assert.deepEqual(parseCanvasToolRequestInput(toolRequest), toolRequest);
  assert.deepEqual(
    parseFinalCodexDecision(
      JSON.stringify({
        message: "已经完成画布修改。",
        expectedDelivery: {
          kind: expectedDelivery.kind,
          requiredArtifacts: expectedDelivery.requiredArtifacts,
          targetCanvasId: expectedDelivery.targetCanvasId,
          completionCriteria: [{ fact: "canvas_revision", artifact: null }],
        },
      }),
    ),
    {
      message: "已经完成画布修改。",
      expectedDelivery,
    },
  );
  assert.throws(
    () =>
      parseCanvasToolRequestInput({
        ...toolRequest,
        expectedDelivery: undefined,
      }),
    /expectedDelivery|invalid/i,
  );
  assert.throws(
    () => parseFinalCodexDecision(JSON.stringify({ message: "缺少交付契约" })),
    /expectedDelivery|invalid/i,
  );
});

test("Codex structured-output schema requires every declared object property", () => {
  const visit = (value: unknown, path: string): void => {
    if (typeof value !== "object" || value === null || Array.isArray(value))
      return;
    const record = value as Record<string, unknown>;
    const properties = record.properties;
    if (
      typeof properties === "object" &&
      properties !== null &&
      !Array.isArray(properties)
    ) {
      assert.deepEqual(
        new Set(Array.isArray(record.required) ? record.required : []),
        new Set(Object.keys(properties)),
        `${path}.required must include every property`,
      );
      for (const [key, child] of Object.entries(properties))
        visit(child, `${path}.properties.${key}`);
    }
    if (record.items !== undefined) visit(record.items, `${path}.items`);
    if (Array.isArray(record.anyOf)) {
      record.anyOf.forEach((branch, index) =>
        visit(branch, `${path}.anyOf[${index}]`),
      );
    }
  };

  visit(LOCAL_AGENT_FINAL_DECISION_JSON_SCHEMA, "root");
});
