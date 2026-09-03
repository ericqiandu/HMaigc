import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import type { CanvasToolSession } from "./canvas-session.js";
import type { CanonicalToolName } from "./contracts.js";
import {
  assetsPublishToolInputSchema,
  assetsReadToolInputSchema,
  canvasApplyOpsToolInputSchema,
  canvasReadToolInputSchema,
  canvasToolEnvelopeSchema,
  mediaGenerateToolInputSchema,
  skillsLoadToolInputSchema,
} from "./mcp-tool-schemas.js";

export const MCP_TOOL_NAME_MAP = {
  canvas_get_state: "canvas.read",
  canvas_apply_ops: "canvas.apply_ops",
  assets_read: "assets.read",
  assets_publish: "assets.publish",
  media_generate: "media.generate",
  skills_load: "skills.load",
} as const satisfies Record<string, CanonicalToolName>;

export type CanvasMcpServerOptions = Readonly<{
  session: CanvasToolSession;
  threadId: string;
  turnId: string;
  nextRequestId: () => string;
}>;

export function createCanvasMcpServer(
  options: CanvasMcpServerOptions,
): McpServer {
  if (!options.threadId || !options.turnId)
    throw new Error("MCP thread/turn 身份不能为空");
  const server = new McpServer({
    name: "hmaigc-canvas-agent",
    version: "0.1.0",
  });
  server.registerTool(
    "canvas_get_state",
    {
      description:
        "读取权威画布 revision、节点、连线、选择、viewport、domainProjectId 与当前可调用模型事实；后续 assets_* 和 media_generate 必须复用返回的权威身份字段。",
      inputSchema: canvasReadToolInputSchema,
    },
    async (input) => executeCanvasTool(options, "canvas.read", input),
  );
  server.registerTool(
    "canvas_apply_ops",
    {
      description:
        "基于刚读取的 baseRevision 原子提交闭合画布操作；每项 operationId 与 clientMutationId 必须唯一且稳定。媒体占位节点的 metadata.prompt 与 metadata.composerContent 必须逐字保存随后 media_generate 的 parameters.prompt；update_node.metadata 是字段级补丁，不得清空生成提示词。",
      inputSchema: canvasApplyOpsToolInputSchema,
    },
    async (input) => executeCanvasTool(options, "canvas.apply_ops", input),
  );
  server.registerTool(
    "assets_read",
    {
      description:
        "读取当前领域项目中的权威资源事实；resourceIds 可为空，limit 必须明确给出。",
      inputSchema: assetsReadToolInputSchema,
    },
    async (input) => executeCanvasTool(options, "assets.read", input),
  );
  server.registerTool(
    "assets_publish",
    {
      description:
        "将已经存在的资源发布到资产库；不会生成媒体，写入前需要用户审批。",
      inputSchema: assetsPublishToolInputSchema,
    },
    async (input) => executeCanvasTool(options, "assets.publish", input),
  );
  server.registerTool(
    "media_generate",
    {
      description:
        "使用 canvas_get_state 返回的可调用模型事实生成图片、视频或音频；parameters 必须符合所选 modelRecordId 的动态参数契约，parameters.prompt 必须已逐字保存到目标节点 metadata.prompt，生成前需要用户审批并可能计费。成功后用 canvas_apply_ops 回填资源并保留提示词。",
      inputSchema: mediaGenerateToolInputSchema,
    },
    async (input) => executeCanvasTool(options, "media.generate", input),
  );
  server.registerTool(
    "skills_load",
    {
      description:
        "按 canvas_get_state 返回的 skillDir、version 与 checksum 精确加载技能；禁止猜测 checksum。",
      inputSchema: skillsLoadToolInputSchema,
    },
    async (input) => executeCanvasTool(options, "skills.load", input),
  );
  return server;
}

async function executeCanvasTool(
  options: CanvasMcpServerOptions,
  canonicalName: CanonicalToolName,
  input: unknown,
) {
  const parsedInput = canvasToolEnvelopeSchema.parse(input);
  const result = await options.session.requestTool({
    requestId: options.nextRequestId(),
    threadId: options.threadId,
    turnId: options.turnId,
    toolName: canonicalName,
    arguments: parsedInput.arguments,
    expectedDelivery: parsedInput.expectedDelivery,
  });
  const content = result.succeeded
    ? JSON.stringify(result.output)
    : JSON.stringify({
        errorCode: result.errorCode,
        errorMessage: result.errorMessage,
      });
  return {
    content: [{ type: "text" as const, text: content }],
    structuredContent: result.output,
    isError: !result.succeeded,
  };
}
