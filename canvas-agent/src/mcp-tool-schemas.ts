import { z } from "zod";

import {
  expectedDeliverySchema,
  type LocalAgentExpectedDelivery,
} from "./contracts.js";

const identifierSchema = z.string().min(1).max(120);
const displayNameSchema = z.string().min(1).max(240);
const resourceIdsSchema = z.array(identifierSchema).max(100);
const jsonObjectSchema = z.record(z.string(), z.unknown());
const canvasNodeTypeSchema = z.enum([
  "image",
  "text",
  "script",
  "skill",
  "config",
  "video",
  "audio",
  "frame",
]);
const coordinateSchema = z.number().finite().min(-1_000_000).max(1_000_000);
const dimensionSchema = z.number().finite().min(1).max(20_000);
const mediaPromptSchema = z
  .string()
  .min(1)
  .max(64 * 1024);
const mediaParameterValueSchema = z.string().min(1).max(240);
const mcpArtifactSchema = z.enum([
  "image",
  "video",
  "audio",
  "text",
  "canvas_revision",
]);
const mcpCompletionCriterionSchema = z.union([
  z
    .object({
      fact: z.enum(["final_message", "canvas_revision", "project_revision"]),
      artifact: z.null().optional(),
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
      artifact: mcpArtifactSchema,
    })
    .strict(),
]);
const mcpExpectedDeliverySchema = z
  .object({
    kind: z.enum([
      "answer",
      "canvas_change",
      "generated_asset",
      "project_change",
      "mixed",
    ]),
    requiredArtifacts: z.array(mcpArtifactSchema).max(5).nullable().optional(),
    targetCanvasId: identifierSchema.nullable().optional(),
    targetProjectId: identifierSchema.nullable().optional(),
    completionCriteria: z.array(mcpCompletionCriterionSchema).min(1).max(20),
  })
  .strict();
const mediaRequestFields = {
  modelRecordId: identifierSchema,
  modelKey: identifierSchema,
  sourceResourceIds: resourceIdsSchema,
  targetCanvasNodeId: identifierSchema,
  clientRequestId: identifierSchema,
};

const imageGenerationParametersSchema = z
  .object({
    prompt: mediaPromptSchema,
    aspectRatio: mediaParameterValueSchema,
    resolution: mediaParameterValueSchema,
    quality: mediaParameterValueSchema.optional(),
    count: z.number().int().min(1).max(16),
    transparentBackground: z.boolean().optional(),
  })
  .strict();

const videoGenerationParametersSchema = z
  .object({
    prompt: mediaPromptSchema,
    aspectRatio: mediaParameterValueSchema,
    resolution: mediaParameterValueSchema,
    durationSeconds: z.number().int().min(1).max(3_600),
    generateAudio: z.boolean(),
  })
  .strict();

const audioGenerationParametersSchema = z
  .object({
    prompt: mediaPromptSchema,
    voice: mediaParameterValueSchema,
    format: mediaParameterValueSchema.optional(),
    speed: mediaParameterValueSchema.optional(),
    volume: mediaParameterValueSchema.optional(),
    pitch: mediaParameterValueSchema.optional(),
    emotion: mediaParameterValueSchema.optional(),
    languageBoost: mediaParameterValueSchema.optional(),
    sampleRate: mediaParameterValueSchema.optional(),
    bitrate: mediaParameterValueSchema.optional(),
    channel: mediaParameterValueSchema.optional(),
    instructions: mediaPromptSchema.optional(),
  })
  .strict();

const canvasNodeSchema = z
  .object({
    id: identifierSchema,
    type: canvasNodeTypeSchema,
    title: displayNameSchema,
    position: z.object({ x: coordinateSchema, y: coordinateSchema }).strict(),
    width: dimensionSchema,
    height: dimensionSchema,
    metadata: jsonObjectSchema.optional(),
  })
  .strict();

const canvasConnectionSchema = z
  .object({
    id: identifierSchema,
    fromNodeId: identifierSchema,
    toNodeId: identifierSchema,
    fromHandleId: identifierSchema.optional(),
    toHandleId: identifierSchema.optional(),
  })
  .strict();

const canvasOperationSchema = z.discriminatedUnion("type", [
  z
    .object({
      operationId: identifierSchema,
      type: z.literal("add_node"),
      node: canvasNodeSchema,
    })
    .strict(),
  z
    .object({
      operationId: identifierSchema,
      type: z.literal("update_node"),
      nodeId: identifierSchema,
      patch: jsonObjectSchema,
    })
    .strict(),
  z
    .object({
      operationId: identifierSchema,
      type: z.literal("delete_node"),
      nodeId: identifierSchema,
    })
    .strict(),
  z
    .object({
      operationId: identifierSchema,
      type: z.literal("connect_nodes"),
      connection: canvasConnectionSchema,
    })
    .strict(),
  z
    .object({
      operationId: identifierSchema,
      type: z.literal("delete_connections"),
      connectionIds: z.array(identifierSchema).min(1).max(100),
    })
    .strict(),
  z
    .object({
      operationId: identifierSchema,
      type: z.literal("set_viewport"),
      viewport: z
        .object({
          x: coordinateSchema,
          y: coordinateSchema,
          zoom: z.number().finite().min(0.01).max(16),
        })
        .strict(),
    })
    .strict(),
  z
    .object({
      operationId: identifierSchema,
      type: z.literal("select_nodes"),
      nodeIds: z.array(identifierSchema).max(100),
    })
    .strict(),
]);

const projectCharacterCandidateSchema = z
  .object({
    name: displayNameSchema,
    aliases: z.array(displayNameSchema).max(20),
    role: z.string().min(1).max(2_048),
    appearance: z.string().min(1).max(2_048),
    clothing: z.string().min(1).max(2_048),
    physique: z.string().min(1).max(2_048),
    personality: z.string().min(1).max(2_048),
    props: z.string().max(2_048),
    consistencyPrompt: z.string().min(1).max(2_048),
    multiViewPrompt: z.string().min(1).max(2_048),
    voiceLanguage: z.string().min(1).max(2_048),
    voiceAge: z.string().min(1).max(2_048),
    voiceTimbre: z.string().min(1).max(2_048),
  })
  .strict();

const projectOperationSchema = z.discriminatedUnion("type", [
  z
    .object({
      operationId: identifierSchema,
      type: z.literal("create_character_candidates"),
      unitId: identifierSchema,
      candidates: z.array(projectCharacterCandidateSchema).min(1).max(100),
    })
    .strict(),
  z
    .object({
      operationId: identifierSchema,
      type: z.literal("replace_unit_shots"),
      unitId: identifierSchema,
      shots: z
        .array(
          z
            .object({
              title: displayNameSchema,
              description: z.string().min(1).max(8 * 1_024),
              durationMs: z.number().int().min(1_000).max(30_000),
            })
            .strict(),
        )
        .min(1)
        .max(200),
    })
    .strict(),
]);

const wrapToolArguments = <Schema extends z.ZodType>(argumentsSchema: Schema) =>
  z
    .object({
      arguments: argumentsSchema,
      expectedDelivery: mcpExpectedDeliverySchema,
    })
    .strict();

export function normalizeMcpExpectedDelivery(
  value: z.infer<typeof mcpExpectedDeliverySchema>,
): LocalAgentExpectedDelivery {
  return expectedDeliverySchema.parse({
    kind: value.kind,
    completionCriteria: value.completionCriteria.map((criterion) =>
      criterion.artifact === null || criterion.artifact === undefined
        ? { fact: criterion.fact }
        : { fact: criterion.fact, artifact: criterion.artifact },
    ),
    ...(value.requiredArtifacts === null ||
    value.requiredArtifacts === undefined
      ? {}
      : { requiredArtifacts: value.requiredArtifacts }),
    ...(value.targetCanvasId === null || value.targetCanvasId === undefined
      ? {}
      : { targetCanvasId: value.targetCanvasId }),
    ...(value.targetProjectId === null || value.targetProjectId === undefined
      ? {}
      : { targetProjectId: value.targetProjectId }),
  });
}

export const canvasToolEnvelopeSchema = wrapToolArguments(jsonObjectSchema);

export const canvasReadToolInputSchema = wrapToolArguments(
  z
    .object({
      canvasId: identifierSchema,
      selectedNodeIds: z.array(identifierSchema).max(100),
      includeViewport: z.literal(true),
    })
    .strict(),
);

export const canvasApplyOpsToolInputSchema = wrapToolArguments(
  z
    .object({
      canvasId: identifierSchema,
      baseRevision: z.number().int().nonnegative(),
      clientMutationId: identifierSchema,
      operations: z.array(canvasOperationSchema).min(1).max(100),
    })
    .strict(),
);

export const assetsReadToolInputSchema = wrapToolArguments(
  z
    .object({
      domainProjectId: identifierSchema,
      resourceIds: resourceIdsSchema,
      limit: z.number().int().min(1).max(100),
    })
    .strict(),
);

export const assetsPublishToolInputSchema = wrapToolArguments(
  z
    .object({
      resourceId: identifierSchema,
      domainProjectId: identifierSchema,
      displayName: displayNameSchema,
      clientMutationId: identifierSchema,
    })
    .strict(),
);

export const projectReadToolInputSchema = wrapToolArguments(
  z
    .object({
      domainProjectId: identifierSchema,
      canvasId: identifierSchema,
    })
    .strict(),
);

export const projectApplyOpsToolInputSchema = wrapToolArguments(
  z
    .object({
      domainProjectId: identifierSchema,
      baseRevision: z.number().int().nonnegative(),
      clientMutationId: identifierSchema,
      operations: z.array(projectOperationSchema).min(1).max(100),
    })
    .strict(),
);

export const mediaGenerateToolInputSchema = wrapToolArguments(
  z.discriminatedUnion("mediaKind", [
    z
      .object({
        ...mediaRequestFields,
        mediaKind: z.literal("image"),
        parameters: imageGenerationParametersSchema,
      })
      .strict(),
    z
      .object({
        ...mediaRequestFields,
        mediaKind: z.literal("video"),
        parameters: videoGenerationParametersSchema,
      })
      .strict(),
    z
      .object({
        ...mediaRequestFields,
        mediaKind: z.literal("audio"),
        parameters: audioGenerationParametersSchema,
      })
      .strict(),
  ]),
);

export const skillsLoadToolInputSchema = wrapToolArguments(
  z
    .object({
      skillDir: displayNameSchema,
      version: z.number().int().min(1),
      checksum: z.string().regex(/^[0-9a-f]{64}$/),
    })
    .strict(),
);
