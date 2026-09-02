import { randomUUID } from "node:crypto";
import { fileURLToPath } from "node:url";
import { join } from "node:path";

import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";

import { AttachmentManager } from "./attachment-manager.js";
import {
  LOCAL_AGENT_CONFIG_DIRECTORY,
  loadLocalAgentConfig,
} from "./config.js";
import { CodexThreadService, FileCodexThreadStore } from "./codex-thread.js";
import { createLocalAgentHttpApp } from "./http-server.js";
import { LoopbackCanvasToolSession } from "./loopback-tool-session.js";
import { createCanvasMcpServer } from "./mcp-server.js";

const LOCAL_AGENT_VERSION = "0.1.0";

await main(process.argv.slice(2)).catch((cause: unknown) => {
  process.stderr.write(
    `${JSON.stringify({
      level: "error",
      event: "canvas_agent_failed",
      message:
        cause instanceof Error ? cause.message : "本机 Agent 返回未知错误",
    })}\n`,
  );
  process.exitCode = 1;
});

async function main(argumentsValue: string[]): Promise<void> {
  const command = argumentsValue[0];
  if (command === "serve") {
    await serve();
    return;
  }
  if (command === "mcp") {
    await serveMcp(argumentsValue.slice(1));
    return;
  }
  throw new Error(
    "用法: canvas-agent serve | canvas-agent mcp --thread-id <id> --turn-id <id>",
  );
}

async function serve(): Promise<void> {
  const config = await loadLocalAgentConfig();
  const attachmentRoot = join(LOCAL_AGENT_CONFIG_DIRECTORY, "attachments");
  const threadService = new CodexThreadService({
    model: config.codex.model,
    ...(config.codex.modelReasoningEffort === undefined
      ? {}
      : { modelReasoningEffort: config.codex.modelReasoningEffort }),
    attachmentRoot,
    resolveWorkspace: (canvasId) => config.canvases[canvasId]?.workspaceRoot,
    store: new FileCodexThreadStore(
      join(LOCAL_AGENT_CONFIG_DIRECTORY, "threads.json"),
    ),
    mcp: {
      command: process.execPath,
      args: [fileURLToPath(import.meta.url), "mcp"],
      url: config.url,
      token: config.token,
    },
  });
  const recoveredTurns = await threadService.recoverInterruptedTurns();
  if (recoveredTurns > 0) {
    process.stderr.write(
      `${JSON.stringify({
        level: "warn",
        event: "canvas_agent_interrupted_turns_recovered",
        count: recoveredTurns,
      })}\n`,
    );
  }
  const app = createLocalAgentHttpApp({
    config,
    threadService,
    attachments: new AttachmentManager({
      root: attachmentRoot,
      allowedOrigins: config.allowedAttachmentOrigins,
    }),
    version: LOCAL_AGENT_VERSION,
  });
  const port = Number(new URL(config.url).port);
  const server = app.listen(port, "127.0.0.1", () => {
    process.stdout.write(
      `${JSON.stringify({
        level: "info",
        event: "canvas_agent_listening",
        url: config.url,
        version: LOCAL_AGENT_VERSION,
      })}\n`,
    );
  });
  await new Promise<void>((resolve, reject) => {
    server.once("error", reject);
    const close = () =>
      server.close((error) => (error ? reject(error) : resolve()));
    process.once("SIGINT", close);
    process.once("SIGTERM", close);
  });
}

async function serveMcp(argumentsValue: string[]): Promise<void> {
  const threadId = readRequiredFlag(argumentsValue, "--thread-id");
  const turnId = readRequiredFlag(argumentsValue, "--turn-id");
  const url = requireEnvironment("HMAIGC_CANVAS_AGENT_URL");
  const token = requireEnvironment("HMAIGC_CANVAS_AGENT_TOKEN");
  requireEnvironment("HMAIGC_CODEX_MODEL");
  const session = new LoopbackCanvasToolSession({ url, token });
  const server = createCanvasMcpServer({
    session,
    threadId,
    turnId,
    nextRequestId: randomUUID,
  });
  await server.connect(new StdioServerTransport());
}

function readRequiredFlag(argumentsValue: string[], flag: string): string {
  const index = argumentsValue.indexOf(flag);
  const value = index < 0 ? undefined : argumentsValue[index + 1];
  if (!value || value.startsWith("--")) throw new Error(`缺少 ${flag}`);
  return value;
}

function requireEnvironment(name: string): string {
  const value = process.env[name]?.trim();
  if (!value) throw new Error(`MCP 子进程缺少 ${name}`);
  return value;
}
