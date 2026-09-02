import { z } from "zod";

export const CANONICAL_TOOL_NAMES = [
  "canvas.read",
  "canvas.apply_ops",
  "assets.read",
  "assets.publish",
  "media.generate",
  "skills.load",
] as const;

export const CANVAS_MCP_TOOL_NAMES = [
  "canvas_get_state",
  "canvas_apply_ops",
  "assets_read",
  "assets_publish",
  "media_generate",
  "skills_load",
] as const;

export type CanonicalToolName = (typeof CANONICAL_TOOL_NAMES)[number];

const boundedIdentity = (label: string) =>
  z.string().min(1, `${label} 不能为空`).max(120, `${label} 过长`);
const recordValue = z.record(z.string(), z.unknown());
const artifactSchema = z.enum([
  "image",
  "video",
  "audio",
  "text",
  "canvas_revision",
]);
const completionCriterionSchema = z.discriminatedUnion("fact", [
  z.object({ fact: z.literal("final_message") }).strict(),
  z.object({ fact: z.literal("canvas_revision") }).strict(),
  z.object({ fact: z.literal("artifact"), artifact: artifactSchema }).strict(),
  z
    .object({ fact: z.literal("artifact_revision"), artifact: artifactSchema })
    .strict(),
  z.object({ fact: z.literal("resource"), artifact: artifactSchema }).strict(),
  z
    .object({
      fact: z.literal("task_backed_resource"),
      artifact: artifactSchema,
    })
    .strict(),
  z
    .object({ fact: z.literal("publication"), artifact: artifactSchema })
    .strict(),
]);

export const expectedDeliverySchema = z
  .object({
    kind: z.enum(["answer", "canvas_change", "generated_asset", "mixed"]),
    requiredArtifacts: z.array(artifactSchema).max(5).optional(),
    targetCanvasId: boundedIdentity("targetCanvasId").optional(),
    completionCriteria: z.array(completionCriterionSchema).min(1).max(20),
  })
  .strict();

const finalCompletionCriterionSchema = z.union([
  z
    .object({
      fact: z.enum(["final_message", "canvas_revision"]),
      artifact: z.null(),
    })
    .strict(),
  z
    .object({
      fact: z.enum([
        "artifact",
        "artifact_revision",
        "resource",
        "task_backed_resource",
        "publication",
      ]),
      artifact: artifactSchema,
    })
    .strict(),
]);

const finalCodexDecisionSchema = z
  .object({
    message: z
      .string()
      .min(1)
      .max(64 * 1024),
    expectedDelivery: z
      .object({
        kind: z.enum(["answer", "canvas_change", "generated_asset", "mixed"]),
        requiredArtifacts: z.array(artifactSchema).max(5).nullable(),
        targetCanvasId: boundedIdentity("targetCanvasId").nullable(),
        completionCriteria: z
          .array(finalCompletionCriterionSchema)
          .min(1)
          .max(20),
      })
      .strict(),
  })
  .strict();

export const LOCAL_AGENT_FINAL_DECISION_JSON_SCHEMA = {
  type: "object",
  additionalProperties: false,
  properties: {
    message: { type: "string", minLength: 1, maxLength: 65_536 },
    expectedDelivery: {
      type: "object",
      additionalProperties: false,
      properties: {
        kind: {
          type: "string",
          enum: ["answer", "canvas_change", "generated_asset", "mixed"],
        },
        requiredArtifacts: {
          type: ["array", "null"],
          maxItems: 5,
          items: {
            type: "string",
            enum: ["image", "video", "audio", "text", "canvas_revision"],
          },
        },
        targetCanvasId: {
          type: ["string", "null"],
          minLength: 1,
          maxLength: 120,
        },
        completionCriteria: {
          type: "array",
          minItems: 1,
          maxItems: 20,
          items: {
            anyOf: [
              {
                type: "object",
                additionalProperties: false,
                properties: {
                  fact: {
                    type: "string",
                    enum: ["final_message", "canvas_revision"],
                  },
                  artifact: { type: "null" },
                },
                required: ["fact", "artifact"],
              },
              {
                type: "object",
                additionalProperties: false,
                properties: {
                  fact: {
                    type: "string",
                    enum: [
                      "artifact",
                      "artifact_revision",
                      "resource",
                      "task_backed_resource",
                      "publication",
                    ],
                  },
                  artifact: {
                    type: "string",
                    enum: [
                      "image",
                      "video",
                      "audio",
                      "text",
                      "canvas_revision",
                    ],
                  },
                },
                required: ["fact", "artifact"],
              },
            ],
          },
        },
      },
      required: [
        "kind",
        "requiredArtifacts",
        "targetCanvasId",
        "completionCriteria",
      ],
    },
  },
  required: ["message", "expectedDelivery"],
} as const;

const attachmentSchema = z
  .object({
    kind: z.enum(["image", "file"]),
    name: z.string().min(1).max(240),
    mimeType: z.string().min(1).max(255),
    url: z.url().max(4096),
  })
  .strict();

const startCodexTurnInputSchema = z
  .object({
    requestId: boundedIdentity("requestId"),
    canvasId: boundedIdentity("canvasId"),
    threadId: boundedIdentity("threadId").optional(),
    message: z
      .string()
      .min(1)
      .max(64 * 1024),
    attachments: z.array(attachmentSchema).max(16, "附件数量不能超过 16"),
    ephemeral: z.literal(true).optional(),
  })
  .strict()
  .superRefine((value, context) => {
    if (value.ephemeral && !value.threadId) {
      context.addIssue({
        code: "custom",
        message: "临时纠偏 turn 必须续接既有 threadId",
      });
    }
  });

const toolResultInputSchema = z
  .object({
    requestId: boundedIdentity("requestId"),
    threadId: boundedIdentity("threadId"),
    turnId: boundedIdentity("turnId"),
    toolName: z.enum(CANONICAL_TOOL_NAMES),
    succeeded: z.boolean(),
    output: recordValue,
    errorCode: z.string().min(1).max(120).optional(),
    errorMessage: z.string().min(1).max(2000).optional(),
  })
  .strict()
  .superRefine((value, context) => {
    if (
      value.succeeded &&
      (value.errorCode !== undefined || value.errorMessage !== undefined)
    ) {
      context.addIssue({
        code: "custom",
        message: "成功工具结果不能携带错误事实",
      });
    }
    if (
      !value.succeeded &&
      (value.errorCode === undefined || value.errorMessage === undefined)
    ) {
      context.addIssue({
        code: "custom",
        message: "失败工具结果必须携带错误事实",
      });
    }
  });

const canvasToolRequestSchema = z
  .object({
    requestId: boundedIdentity("requestId"),
    threadId: boundedIdentity("threadId"),
    turnId: boundedIdentity("turnId"),
    toolName: z.enum(CANONICAL_TOOL_NAMES),
    arguments: recordValue,
    expectedDelivery: expectedDeliverySchema,
  })
  .strict();

export type LocalAgentAttachment = z.infer<typeof attachmentSchema>;
export type StartCodexTurnInput = z.infer<typeof startCodexTurnInputSchema>;
export type ToolResultInput = z.infer<typeof toolResultInputSchema>;
export type CanvasToolRequestInput = z.infer<typeof canvasToolRequestSchema>;
export type LocalAgentExpectedDelivery = z.infer<typeof expectedDeliverySchema>;
export type FinalCodexDecision = Readonly<{
  message: string;
  expectedDelivery: LocalAgentExpectedDelivery;
}>;

export type ToolCallEvent = Readonly<{
  protocolVersion: 1;
  kind: "tool_call";
  requestId: string;
  threadId: string;
  turnId: string;
  toolName: CanonicalToolName;
  arguments: Record<string, unknown>;
  expectedDelivery: LocalAgentExpectedDelivery;
  createdAt: string;
}>;

export function parseStartCodexTurnInput(value: unknown): StartCodexTurnInput {
  return startCodexTurnInputSchema.parse(value);
}

export function parseToolResultInput(value: unknown): ToolResultInput {
  return toolResultInputSchema.parse(value);
}

export function parseCanvasToolRequestInput(
  value: unknown,
): CanvasToolRequestInput {
  return canvasToolRequestSchema.parse(value);
}

export function parseFinalCodexDecision(value: string): FinalCodexDecision {
  let parsed: unknown;
  try {
    parsed = JSON.parse(value);
  } catch (cause) {
    throw new Error("Codex 最终回答不是有效 JSON", { cause });
  }
  const decision = finalCodexDecisionSchema.parse(parsed);
  return {
    message: decision.message,
    expectedDelivery: {
      kind: decision.expectedDelivery.kind,
      completionCriteria: decision.expectedDelivery.completionCriteria.map(
        (criterion) =>
          criterion.artifact === null
            ? { fact: criterion.fact }
            : { fact: criterion.fact, artifact: criterion.artifact },
      ),
      ...(decision.expectedDelivery.requiredArtifacts === null
        ? {}
        : { requiredArtifacts: decision.expectedDelivery.requiredArtifacts }),
      ...(decision.expectedDelivery.targetCanvasId === null
        ? {}
        : { targetCanvasId: decision.expectedDelivery.targetCanvasId }),
    },
  };
}
