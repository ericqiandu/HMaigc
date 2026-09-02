import type { Express, NextFunction, Request, Response } from "express";
import express from "express";
import { z, ZodError } from "zod";

import type { IngestedAttachments } from "./attachment-manager.js";
import { CanvasSession } from "./canvas-session.js";
import {
  assertInternalRequestSecurity,
  assertProtectedRequestSecurity,
  LOCAL_AGENT_PROTOCOL_VERSION,
  type LocalAgentConfig,
} from "./config.js";
import {
  parseCanvasToolRequestInput,
  parseStartCodexTurnInput,
  parseToolResultInput,
  type LocalAgentAttachment,
} from "./contracts.js";
import type {
  CodexThreadTurnInput,
  CodexTurnExecution,
  LocalCodexThreadRecord,
  ResumeCodexThreadTurnInput,
} from "./codex-thread.js";
import { LocalAgentEventHub } from "./event-hub.js";

export const LOCAL_AGENT_BODY_LIMIT_BYTES = 30 * 1024 * 1024;
const REQUEST_LEDGER_COMPLETED_LIMIT = 4_096;
const MCP_TOOL_RESPONSE_HEARTBEAT_MS = 15_000;

export type LocalAgentThreadServicePort = Readonly<{
  startTurn: (input: CodexThreadTurnInput) => Promise<CodexTurnExecution>;
  resumeTurn: (
    input: ResumeCodexThreadTurnInput,
  ) => Promise<CodexTurnExecution>;
  listThreads: (
    canvasId: string,
    options?: Readonly<{ includeArchived?: boolean }>,
  ) => Promise<LocalCodexThreadRecord[]>;
  readThread: (
    canvasId: string,
    threadId: string,
  ) => Promise<LocalCodexThreadRecord>;
  archiveThread: (canvasId: string, threadId: string) => Promise<void>;
  cancelTurn: (turnId: string) => boolean;
}>;

export type AttachmentIngestionPort = Readonly<{
  ingest: (attachments: LocalAgentAttachment[]) => Promise<IngestedAttachments>;
  cleanup: (directory: string) => Promise<void>;
}>;

export type LocalAgentHttpAppOptions = Readonly<{
  config: LocalAgentConfig;
  threadService: LocalAgentThreadServicePort;
  attachments: AttachmentIngestionPort;
  version: string;
  bodyLimitBytes?: number;
  eventHub?: LocalAgentEventHub;
  log?: (event: Readonly<Record<string, string | number>>) => void;
}>;

type StartResponse = Readonly<{ threadId: string; turnId: string }>;

type RequestLedgerEntry = {
  fingerprint: string;
  response: Promise<StartResponse>;
  settled: boolean;
};

const canvasQuerySchema = z
  .object({
    canvasId: z.string().min(1).max(120),
    includeArchived: z.enum(["true", "false"]).optional(),
  })
  .strict();
const canvasBodySchema = z
  .object({ canvasId: z.string().min(1).max(120) })
  .strict();

export function createLocalAgentHttpApp(
  options: LocalAgentHttpAppOptions,
): Express {
  const app = express();
  const eventHub = options.eventHub ?? new LocalAgentEventHub();
  const log =
    options.log ??
    ((event) => process.stderr.write(`${JSON.stringify(event)}\n`));
  const bodyLimitBytes = options.bodyLimitBytes ?? LOCAL_AGENT_BODY_LIMIT_BYTES;
  if (
    !Number.isSafeInteger(bodyLimitBytes) ||
    bodyLimitBytes < 1 ||
    bodyLimitBytes > LOCAL_AGENT_BODY_LIMIT_BYTES
  ) {
    throw new Error("本机 Agent HTTP body 上限无效");
  }
  const activeTurnIds = new Set<string>();
  const requestLedger = new Map<string, RequestLedgerEntry>();
  let canvasSession: CanvasSession | undefined;

  app.disable("x-powered-by");
  app.use(
    express.json({
      limit: bodyLimitBytes,
      strict: true,
      type: "application/json",
    }),
  );

  app.get("/health", (request, response) => {
    const origin = request.header("origin");
    if (origin) {
      if (
        origin === "null" ||
        !options.config.allowedOrigins.includes(origin)
      ) {
        response.status(403).json({
          error: {
            code: "origin_forbidden",
            message: "请求 Origin 未获授权",
          },
        });
        return;
      }
      setCorsHeaders(response, origin);
    }
    response.status(200).json({
      version: options.version,
      protocolVersion: LOCAL_AGENT_PROTOCOL_VERSION,
      ready: true,
    });
  });

  app.options("/{*path}", (request, response) => {
    const origin = request.header("origin");
    if (
      !origin ||
      origin === "null" ||
      !options.config.allowedOrigins.includes(origin)
    ) {
      response.status(403).json({
        error: { code: "origin_forbidden", message: "请求 Origin 未获授权" },
      });
      return;
    }
    setCorsHeaders(response, origin);
    if (request.header("access-control-request-private-network") === "true") {
      response.setHeader("Access-Control-Allow-Private-Network", "true");
    }
    response.sendStatus(204);
  });

  app.post("/internal/mcp/tools", (request, response) => {
    try {
      const queryToken = readQueryToken(request);
      assertInternalRequestSecurity(options.config, {
        tokenHeader: request.header("x-hmaigc-agent-token"),
        ...(queryToken === undefined ? {} : { queryToken }),
      });
    } catch {
      response.status(403).json({
        error: { code: "internal_auth_failed", message: "内部请求未获授权" },
      });
      return;
    }
    try {
      if (!canvasSession)
        throw new HttpError(
          503,
          "event_stream_unavailable",
          "浏览器事件连接不可用",
        );
      const toolRequest = parseCanvasToolRequestInput(request.body);
      const abort = new AbortController();
      response.once("close", () => {
        if (!response.writableEnded) abort.abort();
      });
      const pendingResult = canvasSession.requestTool({
        ...toolRequest,
        signal: abort.signal,
      });
      response.status(200);
      response.setHeader("Content-Type", "application/json; charset=utf-8");
      response.flushHeaders();
      response.write("\n");
      const heartbeat = setInterval(() => {
        response.write("\n");
      }, MCP_TOOL_RESPONSE_HEARTBEAT_MS);
      heartbeat.unref();
      void pendingResult.then(
        (result) => completeMcpToolResponse(response, heartbeat, result),
        (cause: unknown) =>
          completeMcpToolResponse(response, heartbeat, {
            requestId: toolRequest.requestId,
            threadId: toolRequest.threadId,
            turnId: toolRequest.turnId,
            toolName: toolRequest.toolName,
            succeeded: false,
            output: {},
            errorCode: "local_agent_tool_session_failed",
            errorMessage:
              cause instanceof Error
                ? cause.message
                : "CanvasSession 工具请求失败",
          }),
      );
    } catch (cause) {
      const status =
        cause instanceof HttpError
          ? cause.status
          : cause instanceof ZodError
            ? 400
            : 500;
      const code =
        cause instanceof HttpError
          ? cause.code
          : cause instanceof ZodError
            ? "invalid_request"
            : "internal_error";
      const message =
        cause instanceof Error ? cause.message : "本机 Agent 内部请求失败";
      response.status(status).json({ error: { code, message } });
    }
  });

  app.use((request, response, next) => {
    try {
      const queryToken = readQueryToken(request);
      assertProtectedRequestSecurity(options.config, {
        origin: request.header("origin"),
        tokenHeader: request.header("x-hmaigc-agent-token"),
        ...(queryToken === undefined ? {} : { queryToken }),
      });
      setCorsHeaders(response, requireHeader(request, "origin"));
      next();
    } catch {
      response.status(403).json({
        error: {
          code: "request_forbidden",
          message: "本机 Agent 请求未获授权",
        },
      });
    }
  });

  app.get("/events", (request, response, next) => {
    if (eventHub.connected) {
      response.status(409).json({
        error: { code: "event_stream_conflict", message: "已有活动事件连接" },
      });
      return;
    }
    response.status(200);
    response.setHeader("Content-Type", "text/event-stream; charset=utf-8");
    response.setHeader("Cache-Control", "no-cache, no-transform");
    response.setHeader("Connection", "keep-alive");
    response.flushHeaders();
    const nextSession = new CanvasSession({
      emit: (event) => eventHub.emit(event),
    });
    canvasSession = nextSession;
    const heartbeat = setInterval(() => {
      if (!response.write(": heartbeat\n\n")) response.end();
    }, 15_000);
    heartbeat.unref();
    try {
      eventHub.connect(
        {
          write: (chunk) => response.write(chunk),
          waitForDrain: () => waitForResponseDrain(response),
          end: () => response.end(),
          onClose: (listener) => request.once("close", listener),
        },
        () => {
          clearInterval(heartbeat);
          nextSession.disconnect("浏览器事件连接已断开");
          if (canvasSession === nextSession) canvasSession = undefined;
          for (const turnId of activeTurnIds)
            options.threadService.cancelTurn(turnId);
        },
      );
    } catch (cause) {
      clearInterval(heartbeat);
      canvasSession = undefined;
      next(cause);
    }
  });

  app.get("/agent/codex/workspaces/:canvasId", (request, response) => {
    const configured = Object.hasOwn(
      options.config.canvases,
      request.params.canvasId ?? "",
    );
    if (!configured) {
      response.status(404).json({
        error: {
          code: "workspace_not_configured",
          message: "当前画布未配置本机工作区",
        },
      });
      return;
    }
    response
      .status(200)
      .json({ canvasId: request.params.canvasId, configured: true });
  });

  app.get(
    "/agent/codex/threads",
    asyncRoute(async (request, response) => {
      const query = canvasQuerySchema.parse(request.query);
      const records = await options.threadService.listThreads(query.canvasId, {
        includeArchived: query.includeArchived === "true",
      });
      response.status(200).json({ threads: records.map(toPublicThreadRecord) });
    }),
  );

  app.post(
    "/agent/codex/threads",
    asyncRoute(async (request, response) => {
      const input = parseStartCodexTurnInput(request.body);
      if (input.threadId || input.ephemeral)
        throw new HttpError(
          400,
          "unexpected_thread_identity",
          "创建线程时不能提供 threadId 或 ephemeral",
        );
      const result = await startIdempotent(
        input.requestId,
        input,
        requestLedger,
        () => startExecution(input),
      );
      response.status(202).json(result);
    }),
  );

  app.get(
    "/agent/codex/threads/:threadId",
    asyncRoute(async (request, response) => {
      const query = canvasQuerySchema.parse(request.query);
      const record = await options.threadService.readThread(
        query.canvasId,
        requireParam(request, "threadId"),
      );
      response.status(200).json(toPublicThreadRecord(record));
    }),
  );

  app.post(
    "/agent/codex/threads/:threadId/resume",
    asyncRoute(async (request, response) => {
      const parsed = parseStartCodexTurnInput(request.body);
      const threadId = requireParam(request, "threadId");
      if (parsed.threadId && parsed.threadId !== threadId)
        throw new HttpError(
          409,
          "thread_identity_conflict",
          "threadId 与路由不一致",
        );
      const input = { ...parsed, threadId };
      const result = await startIdempotent(
        input.requestId,
        input,
        requestLedger,
        () => startExecution(input),
      );
      response.status(202).json(result);
    }),
  );

  app.post(
    "/agent/codex/threads/:threadId/archive",
    asyncRoute(async (request, response) => {
      const body = canvasBodySchema.parse(request.body);
      await options.threadService.archiveThread(
        body.canvasId,
        requireParam(request, "threadId"),
      );
      response.sendStatus(204);
    }),
  );

  app.post(
    "/agent/codex/turns",
    asyncRoute(async (request, response) => {
      const input = parseStartCodexTurnInput(request.body);
      if (input.threadId)
        throw new HttpError(
          400,
          "unexpected_thread_id",
          "续接线程必须使用专用 resume 路由",
        );
      const result = await startIdempotent(
        input.requestId,
        input,
        requestLedger,
        () => startExecution(input),
      );
      response.status(202).json(result);
    }),
  );

  app.post("/agent/codex/turns/:turnId/cancel", (request, response) => {
    const cancelled = options.threadService.cancelTurn(
      requireParam(request, "turnId"),
    );
    response.status(cancelled ? 202 : 404).json(
      cancelled
        ? { cancelled: true }
        : {
            error: {
              code: "turn_not_active",
              message: "当前 turn 不在执行中",
            },
          },
    );
  });

  app.post("/tools/:requestId/results", (request, response, next) => {
    try {
      if (!canvasSession)
        throw new HttpError(
          409,
          "event_stream_unavailable",
          "浏览器事件连接不可用",
        );
      const result = parseToolResultInput(request.body);
      if (result.requestId !== requireParam(request, "requestId")) {
        throw new HttpError(
          409,
          "request_identity_conflict",
          "工具结果 requestId 与路由不一致",
        );
      }
      canvasSession.resolveToolResult(result);
      response.sendStatus(204);
    } catch (cause) {
      next(cause);
    }
  });

  app.use((_request, response) => {
    response
      .status(404)
      .json({ error: { code: "not_found", message: "本机 Agent 路由不存在" } });
  });

  app.use(
    (
      cause: unknown,
      _request: Request,
      response: Response,
      _next: NextFunction,
    ) => {
      if (isPayloadTooLarge(cause)) {
        response.status(413).json({
          error: { code: "body_too_large", message: "请求体超过 30MB 上限" },
        });
        return;
      }
      if (cause instanceof ZodError || cause instanceof SyntaxError) {
        response.status(400).json({
          error: { code: "invalid_request", message: "请求格式无效" },
        });
        return;
      }
      if (cause instanceof HttpError) {
        response
          .status(cause.status)
          .json({ error: { code: cause.code, message: cause.publicMessage } });
        return;
      }
      response.status(500).json({
        error: {
          code: "internal_error",
          message: "本机 Agent 执行失败，请查看本机结构化日志",
        },
      });
    },
  );

  return app;

  async function startExecution(
    input: ReturnType<typeof parseStartCodexTurnInput>,
  ): Promise<StartResponse> {
    if (!eventHub.connected)
      throw new HttpError(409, "event_stream_required", "请先建立事件连接");
    const ingested = await options.attachments.ingest(input.attachments);
    let execution: CodexTurnExecution;
    try {
      execution = input.threadId
        ? await options.threadService.resumeTurn({
            canvasId: input.canvasId,
            threadId: input.threadId,
            message: input.message,
            attachments: ingested.attachments,
            ...(input.ephemeral ? { ephemeral: true as const } : {}),
          })
        : await options.threadService.startTurn({
            canvasId: input.canvasId,
            message: input.message,
            attachments: ingested.attachments,
          });
    } catch (cause) {
      await options.attachments.cleanup(ingested.directory);
      throw cause;
    }
    activeTurnIds.add(execution.turnId);
    void consumeExecution(execution, ingested.directory);
    return { threadId: execution.threadId, turnId: execution.turnId };
  }

  async function consumeExecution(
    execution: CodexTurnExecution,
    attachmentDirectory: string,
  ): Promise<void> {
    try {
      for await (const event of execution.events) await eventHub.emit(event);
    } catch (cause) {
      log({
        level: "error",
        event: "codex_event_stream_failed",
        error: safeErrorName(cause),
      });
      options.threadService.cancelTurn(execution.turnId);
    } finally {
      activeTurnIds.delete(execution.turnId);
      try {
        await options.attachments.cleanup(attachmentDirectory);
      } catch (cause) {
        log({
          level: "error",
          event: "attachment_cleanup_failed",
          error: safeErrorName(cause),
        });
      }
    }
  }
}

function waitForResponseDrain(response: Response): Promise<void> {
  if (response.writableEnded || response.destroyed)
    return Promise.reject(new Error("本机 Agent 浏览器事件连接已断开"));
  return new Promise<void>((resolve, reject) => {
    const cleanup = () => {
      response.off("drain", onDrain);
      response.off("close", onClose);
      response.off("error", onError);
    };
    const onDrain = () => {
      cleanup();
      resolve();
    };
    const onClose = () => {
      cleanup();
      reject(new Error("本机 Agent 浏览器事件连接已断开"));
    };
    const onError = (cause: Error) => {
      cleanup();
      reject(cause);
    };
    response.once("drain", onDrain);
    response.once("close", onClose);
    response.once("error", onError);
  });
}

function completeMcpToolResponse(
  response: Response,
  heartbeat: ReturnType<typeof setInterval>,
  result: unknown,
): void {
  clearInterval(heartbeat);
  if (response.writableEnded || response.destroyed) return;
  response.end(JSON.stringify(result));
}

function setCorsHeaders(response: Response, origin: string): void {
  response.setHeader("Access-Control-Allow-Origin", origin);
  response.setHeader("Access-Control-Allow-Methods", "GET,POST,OPTIONS");
  response.setHeader(
    "Access-Control-Allow-Headers",
    "Content-Type,X-HMaigc-Agent-Token",
  );
  response.setHeader("Access-Control-Max-Age", "600");
  response.appendHeader("Vary", "Origin");
}

function toPublicThreadRecord(
  record: LocalCodexThreadRecord,
): Omit<LocalCodexThreadRecord, "workspaceRoot" | "sdkThreadId"> {
  const {
    workspaceRoot: _workspaceRoot,
    sdkThreadId: _sdkThreadId,
    ...publicRecord
  } = record;
  return publicRecord;
}

function asyncRoute(
  handler: (request: Request, response: Response) => Promise<void>,
): (request: Request, response: Response, next: NextFunction) => void {
  return (request, response, next) => {
    void handler(request, response).catch(next);
  };
}

function requireHeader(request: Request, name: string): string {
  const value = request.header(name);
  if (!value) throw new Error(`缺少 ${name} header`);
  return value;
}

function requireParam(request: Request, name: string): string {
  const value = request.params[name];
  if (typeof value !== "string" || !value)
    throw new HttpError(400, "missing_path_parameter", "路由参数缺失");
  return value;
}

function readQueryToken(request: Request): string | undefined {
  const value = request.query.token;
  if (Array.isArray(value)) return value.map(String).join(",");
  return typeof value === "string" ? value : undefined;
}

async function startIdempotent(
  requestId: string,
  input: unknown,
  ledger: Map<string, RequestLedgerEntry>,
  start: () => Promise<StartResponse>,
): Promise<StartResponse> {
  const fingerprint = JSON.stringify(input);
  const existing = ledger.get(requestId);
  if (existing) {
    if (existing.fingerprint !== fingerprint)
      throw new HttpError(
        409,
        "request_replay_conflict",
        "requestId 已用于不同请求",
      );
    return existing.response;
  }
  const response = start();
  const entry: RequestLedgerEntry = { fingerprint, response, settled: false };
  ledger.set(requestId, entry);
  void response.then(
    () => {
      entry.settled = true;
      pruneSettledRequestLedger(ledger);
    },
    () => {
      entry.settled = true;
      pruneSettledRequestLedger(ledger);
    },
  );
  pruneSettledRequestLedger(ledger);
  return response;
}

function pruneSettledRequestLedger(
  ledger: Map<string, RequestLedgerEntry>,
): void {
  let settledCount = 0;
  for (const entry of ledger.values()) {
    if (entry.settled) settledCount += 1;
  }
  let toDelete = settledCount - REQUEST_LEDGER_COMPLETED_LIMIT;
  if (toDelete <= 0) return;
  for (const [requestId, entry] of ledger) {
    if (!entry.settled) continue;
    ledger.delete(requestId);
    toDelete -= 1;
    if (toDelete === 0) return;
  }
}

class HttpError extends Error {
  constructor(
    readonly status: number,
    readonly code: string,
    readonly publicMessage: string,
  ) {
    super(publicMessage);
  }
}

function isPayloadTooLarge(cause: unknown): boolean {
  return (
    cause instanceof Error &&
    "type" in cause &&
    cause.type === "entity.too.large"
  );
}

function safeErrorName(cause: unknown): string {
  return cause instanceof Error ? cause.name : "UnknownError";
}
