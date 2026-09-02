import { randomUUID } from "node:crypto";
import { lstat, realpath } from "node:fs/promises";
import { isAbsolute, relative, resolve } from "node:path";

import {
  Codex,
  type CodexOptions,
  type Input,
  type ModelReasoningEffort,
  type ThreadEvent,
  type ThreadOptions,
  type TurnOptions,
} from "@openai/codex-sdk";

import { CANVAS_MCP_TOOL_NAMES } from "./contracts.js";
import {
  LOCAL_AGENT_FINAL_DECISION_JSON_SCHEMA,
  parseFinalCodexDecision,
  type FinalCodexDecision,
} from "./contracts.js";
import {
  type CodexThreadStore,
  type LocalCodexThreadRecord,
  type LocalCodexTurnRecord,
  type NormalizedCodexEvent,
} from "./codex-thread-store.js";

export {
  FileCodexThreadStore,
  InMemoryCodexThreadStore,
  type CodexThreadStore,
  type LocalCodexThreadRecord,
  type LocalCodexTurnRecord,
  type NormalizedCodexEvent,
} from "./codex-thread-store.js";

export type CodexThreadPort = Readonly<{
  id: string | null;
  runStreamed: (
    input: Input,
    options?: TurnOptions,
  ) => Promise<{ events: AsyncGenerator<ThreadEvent> }>;
}>;

export type CodexClientPort = Readonly<{
  startThread: (options: ThreadOptions) => CodexThreadPort;
  resumeThread: (id: string, options: ThreadOptions) => CodexThreadPort;
}>;

export type ResolvedCodexAttachment = Readonly<{
  kind: "image" | "file";
  name: string;
  mimeType: string;
  localPath: string;
}>;

export type CodexThreadTurnInput = Readonly<{
  canvasId: string;
  message: string;
  attachments: ResolvedCodexAttachment[];
}>;

export type ResumeCodexThreadTurnInput = CodexThreadTurnInput &
  Readonly<{
    threadId: string;
    ephemeral?: true;
  }>;

export type CodexTurnExecution = Readonly<{
  threadId: string;
  turnId: string;
  events: AsyncGenerator<NormalizedCodexEvent>;
}>;

export type CanvasMcpProcessConfig = Readonly<{
  command: string;
  args: string[];
  url: string;
  token: string;
}>;

export type CodexThreadServiceOptions = Readonly<{
  model: string;
  modelReasoningEffort?: ModelReasoningEffort;
  attachmentRoot: string;
  resolveWorkspace: (canvasId: string) => string | undefined;
  createCodexClient?: (options: CodexOptions) => CodexClientPort;
  store: CodexThreadStore;
  mcp: CanvasMcpProcessConfig;
  environment?: NodeJS.ProcessEnv;
  now?: () => Date;
  nextId?: () => string;
}>;

const CODEX_ENVIRONMENT_ALLOWLIST = new Set([
  "APPDATA",
  "CODEX_API_KEY",
  "CODEX_HOME",
  "COMSPEC",
  "HOME",
  "HOMEDRIVE",
  "HOMEPATH",
  "HTTP_PROXY",
  "HTTPS_PROXY",
  "LOCALAPPDATA",
  "NO_PROXY",
  "OPENAI_API_KEY",
  "OPENAI_BASE_URL",
  "PATH",
  "PATHEXT",
  "SHELL",
  "SSL_CERT_DIR",
  "SSL_CERT_FILE",
  "SystemRoot",
  "TEMP",
  "TMP",
  "TMPDIR",
  "USER",
  "USERPROFILE",
  "XDG_CACHE_HOME",
  "XDG_CONFIG_HOME",
  "http_proxy",
  "https_proxy",
  "no_proxy",
]);

export function sanitizeCodexEnvironment(
  source: NodeJS.ProcessEnv,
): Record<string, string> {
  const result: Record<string, string> = {};
  for (const [key, value] of Object.entries(source)) {
    if (value !== undefined && CODEX_ENVIRONMENT_ALLOWLIST.has(key))
      result[key] = value;
  }
  return result;
}

export function buildCanvasAgentInstructions(canvasId: string): string {
  if (!canvasId.trim()) throw new Error("canvasId 不能为空");
  return [
    "你是 HMaigc 的本机 Canvas Agent。",
    `当前真实画布标识为 ${canvasId}。只依据用户输入、工具返回和当前工作区中的真实事实行动。`,
    `涉及画布、资产、媒体或技能的读取与操作，只能使用这些 MCP 工具：${CANVAS_MCP_TOOL_NAMES.join("、")}。`,
    "每次工具提案都必须声明与本轮目标一致的 expectedDelivery；最终结构化回答必须保持同一交付契约。",
    "调用 MCP 工具后必须等待该工具返回成功或失败结果，才能输出最终结构化回答；等待网站审批或工具执行期间禁止提前结束 turn。",
    "不得绕过工具直接写入 HMaigc，不得把提案、占位状态或未确认结果描述为已经完成。",
    "根据用户当前意图自主决定是否调用工具；事实不足时显式说明缺失事实。",
  ].join("\n");
}

export class CodexThreadService {
  readonly #model: string;
  readonly #modelReasoningEffort: ModelReasoningEffort | undefined;
  readonly #attachmentRoot: string;
  readonly #resolveWorkspace: (canvasId: string) => string | undefined;
  readonly #createCodexClient: (options: CodexOptions) => CodexClientPort;
  readonly #store: CodexThreadStore;
  readonly #mcp: CanvasMcpProcessConfig;
  readonly #environment: Record<string, string>;
  readonly #now: () => Date;
  readonly #nextId: () => string;
  readonly #activeTurns = new Map<
    string,
    Readonly<{ threadId: string; controller: AbortController }>
  >();

  constructor(options: CodexThreadServiceOptions) {
    this.#model = options.model.trim();
    if (!this.#model)
      throw new Error("Codex 模型配置缺失，无法保证本机线程模型继承");
    if (!isAbsolute(options.attachmentRoot))
      throw new Error("本机附件根目录必须是绝对路径");
    if (
      !options.mcp.command.trim() ||
      options.mcp.args.length === 0 ||
      !options.mcp.url.trim() ||
      !options.mcp.token
    ) {
      throw new Error("Canvas MCP 子进程配置不完整");
    }
    this.#modelReasoningEffort = options.modelReasoningEffort;
    this.#attachmentRoot = resolve(options.attachmentRoot);
    this.#resolveWorkspace = options.resolveWorkspace;
    this.#createCodexClient =
      options.createCodexClient ?? ((codexOptions) => new Codex(codexOptions));
    this.#store = options.store;
    this.#mcp = options.mcp;
    this.#environment = sanitizeCodexEnvironment(
      options.environment ?? process.env,
    );
    this.#now = options.now ?? (() => new Date());
    this.#nextId = options.nextId ?? randomUUID;
  }

  async startTurn(input: CodexThreadTurnInput): Promise<CodexTurnExecution> {
    const message = normalizeMessage(input.message);
    const workspaceRoot = await this.#resolveAndValidateWorkspace(
      input.canvasId,
    );
    const attachments = await this.#validateAttachments(input.attachments);
    const threadId = this.#nextId();
    const now = this.#now().toISOString();
    const record: LocalCodexThreadRecord = {
      threadId,
      canvasId: input.canvasId,
      workspaceRoot,
      model: this.#model,
      createdAt: now,
      updatedAt: now,
      turns: [],
    };
    await this.#store.create(record);
    return this.#beginTurn(record, message, attachments, false);
  }

  async resumeTurn(
    input: ResumeCodexThreadTurnInput,
  ): Promise<CodexTurnExecution> {
    const message = normalizeMessage(input.message);
    const record = await this.#requireOwnedThread(
      input.canvasId,
      input.threadId,
    );
    if (record.archivedAt) throw new Error("本机 Codex 线程已经归档");
    if (!record.sdkThreadId)
      throw new Error("本机 Codex 线程缺少可续接的 SDK threadId");
    const currentWorkspace = await this.#resolveAndValidateWorkspace(
      input.canvasId,
    );
    if (currentWorkspace !== record.workspaceRoot)
      throw new Error("画布工作区归属已经变化，拒绝续接线程");
    const attachments = await this.#validateAttachments(input.attachments);
    return this.#beginTurn(
      record,
      message,
      attachments,
      true,
      input.ephemeral === true,
    );
  }

  recoverInterruptedTurns(): Promise<number> {
    return this.#store.recoverRunningTurns(this.#now().toISOString());
  }

  async listThreads(
    canvasId: string,
    options: Readonly<{ includeArchived?: boolean }> = {},
  ): Promise<LocalCodexThreadRecord[]> {
    await this.#resolveAndValidateWorkspace(canvasId);
    const records = await this.#store.list(canvasId);
    return options.includeArchived
      ? records
      : records.filter((record) => !record.archivedAt);
  }

  async readThread(
    canvasId: string,
    threadId: string,
  ): Promise<LocalCodexThreadRecord> {
    return this.#requireOwnedThread(canvasId, threadId);
  }

  async archiveThread(canvasId: string, threadId: string): Promise<void> {
    const record = await this.#requireOwnedThread(canvasId, threadId);
    if (
      [...this.#activeTurns.values()].some(
        (active) => active.threadId === threadId,
      )
    ) {
      throw new Error("线程仍有活动 turn，不能归档");
    }
    if (record.archivedAt) return;
    const archivedAt = this.#now().toISOString();
    await this.#store.save({ ...record, archivedAt, updatedAt: archivedAt });
  }

  cancelTurn(turnId: string): boolean {
    const active = this.#activeTurns.get(turnId);
    if (!active) return false;
    active.controller.abort();
    return true;
  }

  async #beginTurn(
    record: LocalCodexThreadRecord,
    message: string,
    attachments: ResolvedCodexAttachment[],
    resumeExisting: boolean,
    ephemeral = false,
  ): Promise<CodexTurnExecution> {
    const normalizedMessage = normalizeMessage(message);
    if (
      record.turns.some((turn) => turn.status === "running") ||
      [...this.#activeTurns.values()].some(
        (active) => active.threadId === record.threadId,
      )
    ) {
      throw new Error("本机 Codex 线程已有活动 turn");
    }

    const turnId = this.#nextId();
    const controller = new AbortController();
    const createdAt = this.#now().toISOString();
    const turnRecord: LocalCodexTurnRecord = {
      turnId,
      status: "running",
      message: normalizedMessage,
      attachments: attachments.map(({ kind, name, mimeType }) => ({
        kind,
        name,
        mimeType,
      })),
      events: [],
      createdAt,
    };
    if (!ephemeral) {
      const runningRecord = {
        ...record,
        updatedAt: createdAt,
        turns: [...record.turns, turnRecord],
      };
      await this.#store.save(runningRecord);
    }
    this.#activeTurns.set(turnId, { threadId: record.threadId, controller });

    const client = this.#createCodexClient(
      this.#buildCodexOptions(record.canvasId, record.threadId, turnId),
    );
    const threadOptions = this.#buildThreadOptions(record.workspaceRoot);
    const thread = resumeExisting
      ? client.resumeThread(
          requireString(record.sdkThreadId, "SDK threadId 缺失"),
          threadOptions,
        )
      : client.startThread(threadOptions);
    let streamed: { events: AsyncGenerator<ThreadEvent> };
    try {
      streamed = await thread.runStreamed(
        this.#buildCodexInput(normalizedMessage, attachments),
        {
          signal: controller.signal,
          outputSchema: LOCAL_AGENT_FINAL_DECISION_JSON_SCHEMA,
        },
      );
    } catch (cause) {
      this.#activeTurns.delete(turnId);
      if (!ephemeral)
        await this.#finishTurn(
          record.threadId,
          turnId,
          "failed",
          [],
          errorMessage(cause),
        );
      throw new Error(`Codex turn 启动失败: ${errorMessage(cause)}`, { cause });
    }

    return {
      threadId: record.threadId,
      turnId,
      events: this.#consumeEvents(
        record.threadId,
        turnId,
        streamed.events,
        controller,
        ephemeral,
      ),
    };
  }

  async *#consumeEvents(
    threadId: string,
    turnId: string,
    events: AsyncGenerator<ThreadEvent>,
    controller: AbortController,
    ephemeral: boolean,
  ): AsyncGenerator<NormalizedCodexEvent> {
    const normalizedEvents: NormalizedCodexEvent[] = [];
    let pendingFinalDecision: FinalCodexDecision | undefined;
    const activeMcpToolCallIds = new Set<string>();
    let terminal = false;
    let finalized = false;
    try {
      for await (const event of events) {
        if (
          (event.type === "item.started" ||
            event.type === "item.updated" ||
            event.type === "item.completed") &&
          event.item.type === "mcp_tool_call"
        ) {
          if (event.item.status === "in_progress") {
            activeMcpToolCallIds.add(event.item.id);
            pendingFinalDecision = undefined;
          } else {
            activeMcpToolCallIds.delete(event.item.id);
          }
        }
        if (
          event.type === "item.completed" &&
          event.item.type === "agent_message"
        ) {
          if (activeMcpToolCallIds.size === 0)
            pendingFinalDecision = parseFinalCodexDecision(event.item.text);
          const itemCompleted: NormalizedCodexEvent = {
            kind: "item_completed",
            threadId,
            turnId,
            event,
          };
          normalizedEvents.push(itemCompleted);
          yield itemCompleted;
          continue;
        }
        if (event.type === "turn.completed") {
          if (activeMcpToolCallIds.size > 0) {
            throw new Error(
              `Codex turn 完成时仍有 ${activeMcpToolCallIds.size} 个 MCP 工具仍在执行`,
            );
          }
          if (!pendingFinalDecision)
            throw new Error("Codex turn 完成但缺少结构化最终交付决策");
          const finalDecision: NormalizedCodexEvent = {
            kind: "final_decision",
            threadId,
            turnId,
            ...pendingFinalDecision,
          };
          normalizedEvents.push(finalDecision);
          yield finalDecision;
          const completed: NormalizedCodexEvent = {
            kind: "turn_completed",
            threadId,
            turnId,
            event,
          };
          normalizedEvents.push(completed);
          terminal = true;
          await this.#finalizeConsumedTurn(
            threadId,
            turnId,
            normalizedEvents,
            ephemeral,
          );
          finalized = true;
          yield completed;
          break;
        }
        const normalized = await this.#normalizeEvent(threadId, turnId, event);
        normalizedEvents.push(normalized);
        terminal = normalized.kind === "turn_failed";
        yield normalized;
        if (terminal) break;
      }
      if (!terminal) {
        const message = "Codex 事件流在终态事件前结束";
        const failed: NormalizedCodexEvent = {
          kind: "turn_failed",
          threadId,
          turnId,
          message,
        };
        normalizedEvents.push(failed);
        yield failed;
      }
    } catch (cause) {
      if (controller.signal.aborted) {
        const cancelled: NormalizedCodexEvent = {
          kind: "turn_cancelled",
          threadId,
          turnId,
        };
        normalizedEvents.push(cancelled);
        yield cancelled;
      } else {
        const message = errorMessage(cause);
        const failed: NormalizedCodexEvent = {
          kind: "turn_failed",
          threadId,
          turnId,
          message,
        };
        normalizedEvents.push(failed);
        yield failed;
      }
    } finally {
      if (!finalized)
        await this.#finalizeConsumedTurn(
          threadId,
          turnId,
          normalizedEvents,
          ephemeral,
        );
    }
  }

  async #finalizeConsumedTurn(
    threadId: string,
    turnId: string,
    normalizedEvents: NormalizedCodexEvent[],
    ephemeral: boolean,
  ): Promise<void> {
    this.#activeTurns.delete(turnId);
    if (ephemeral) return;
    const last = normalizedEvents.at(-1);
    const status =
      last?.kind === "turn_completed"
        ? "completed"
        : last?.kind === "turn_cancelled"
          ? "cancelled"
          : "failed";
    const failure = last?.kind === "turn_failed" ? last.message : undefined;
    await this.#finishTurn(threadId, turnId, status, normalizedEvents, failure);
  }

  async #normalizeEvent(
    threadId: string,
    turnId: string,
    event: ThreadEvent,
  ): Promise<NormalizedCodexEvent> {
    switch (event.type) {
      case "thread.started": {
        const record = await this.#requireThread(threadId);
        if (record.sdkThreadId && record.sdkThreadId !== event.thread_id) {
          throw new Error("Codex SDK threadId 与本机目录记录不一致");
        }
        if (!record.sdkThreadId) {
          await this.#store.save({
            ...record,
            sdkThreadId: event.thread_id,
            updatedAt: this.#now().toISOString(),
          });
        }
        return {
          kind: "thread_started",
          threadId,
          sdkThreadId: event.thread_id,
        };
      }
      case "turn.started":
        return { kind: "turn_started", threadId, turnId };
      case "item.started":
        return { kind: "item_started", threadId, turnId, event };
      case "item.updated":
        return { kind: "item_updated", threadId, turnId, event };
      case "item.completed":
        return { kind: "item_completed", threadId, turnId, event };
      case "turn.completed":
        return { kind: "turn_completed", threadId, turnId, event };
      case "turn.failed":
        return {
          kind: "turn_failed",
          threadId,
          turnId,
          message: event.error.message,
          event,
        };
      case "error":
        return {
          kind: "turn_failed",
          threadId,
          turnId,
          message: event.message,
          event,
        };
    }
  }

  #buildCodexOptions(
    canvasId: string,
    threadId: string,
    turnId: string,
  ): CodexOptions {
    return {
      env: {
        ...this.#environment,
        HMAIGC_CANVAS_AGENT_TOKEN: this.#mcp.token,
      },
      config: {
        developer_instructions: buildCanvasAgentInstructions(canvasId),
        mcp_servers: {
          hmaigc_canvas: {
            command: this.#mcp.command,
            args: [
              ...this.#mcp.args,
              "--thread-id",
              threadId,
              "--turn-id",
              turnId,
            ],
            env: {
              HMAIGC_CANVAS_AGENT_URL: this.#mcp.url,
              HMAIGC_CODEX_MODEL: this.#model,
            },
            env_vars: ["HMAIGC_CANVAS_AGENT_TOKEN"],
            enabled_tools: [...CANVAS_MCP_TOOL_NAMES],
            default_tools_approval_mode: "approve",
          },
        },
      },
    };
  }

  #buildThreadOptions(workspaceRoot: string): ThreadOptions {
    const result: ThreadOptions = {
      model: this.#model,
      threadSource: "hmaigc-canvas-agent",
      workingDirectory: workspaceRoot,
      skipGitRepoCheck: true,
      sandboxMode: "read-only",
      networkAccessEnabled: false,
      webSearchMode: "disabled",
      approvalPolicy: "never",
    };
    if (this.#modelReasoningEffort !== undefined)
      result.modelReasoningEffort = this.#modelReasoningEffort;
    return result;
  }

  #buildCodexInput(
    message: string,
    attachments: ResolvedCodexAttachment[],
  ): Input {
    const fileAttachments = attachments.filter(
      (attachment) => attachment.kind === "file",
    );
    const text =
      fileAttachments.length === 0
        ? message
        : `${message}\n\n本轮本机附件：\n${fileAttachments.map((attachment) => `- ${attachment.name}: ${attachment.localPath}`).join("\n")}`;
    const images = attachments
      .filter((attachment) => attachment.kind === "image")
      .map((attachment) => ({
        type: "local_image" as const,
        path: attachment.localPath,
      }));
    return images.length === 0 ? text : [{ type: "text", text }, ...images];
  }

  async #resolveAndValidateWorkspace(canvasId: string): Promise<string> {
    const normalizedCanvasId = canvasId.trim();
    if (!normalizedCanvasId) throw new Error("canvasId 不能为空");
    const configured = this.#resolveWorkspace(normalizedCanvasId);
    if (!configured || !isAbsolute(configured))
      throw new Error("当前画布未配置本机工作区");
    const metadata = await lstat(configured);
    if (!metadata.isDirectory() || metadata.isSymbolicLink())
      throw new Error("画布工作区必须是普通目录");
    return realpath(configured);
  }

  async #validateAttachments(
    attachments: ResolvedCodexAttachment[],
  ): Promise<ResolvedCodexAttachment[]> {
    if (attachments.length > 16) throw new Error("附件数量不能超过 16");
    const result: ResolvedCodexAttachment[] = [];
    for (const attachment of attachments) {
      if (
        !attachment.name.trim() ||
        !attachment.mimeType.trim() ||
        !isAbsolute(attachment.localPath)
      ) {
        throw new Error("Codex 附件事实不完整");
      }
      const normalizedPath = resolve(attachment.localPath);
      if (!isPathWithin(this.#attachmentRoot, normalizedPath))
        throw new Error("附件路径不属于受管目录");
      const metadata = await lstat(normalizedPath);
      if (!metadata.isFile() || metadata.isSymbolicLink())
        throw new Error("Codex 附件必须是普通文件");
      const resolvedPath = await realpath(normalizedPath);
      if (!isPathWithin(this.#attachmentRoot, resolvedPath))
        throw new Error("附件真实路径不属于受管目录");
      result.push({ ...attachment, localPath: resolvedPath });
    }
    return result;
  }

  async #requireOwnedThread(
    canvasId: string,
    threadId: string,
  ): Promise<LocalCodexThreadRecord> {
    const record = await this.#requireThread(threadId);
    if (record.canvasId !== canvasId)
      throw new Error("本机 Codex 线程不属于当前画布");
    return record;
  }

  async #requireThread(threadId: string): Promise<LocalCodexThreadRecord> {
    const record = await this.#store.get(threadId);
    if (!record) throw new Error("本机 Codex 线程不存在");
    return record;
  }

  async #finishTurn(
    threadId: string,
    turnId: string,
    status: "completed" | "failed" | "cancelled",
    events: NormalizedCodexEvent[],
    failure?: string,
  ): Promise<void> {
    const record = await this.#requireThread(threadId);
    const completedAt = this.#now().toISOString();
    const turns = record.turns.map((turn): LocalCodexTurnRecord => {
      if (turn.turnId !== turnId) return turn;
      const base = { ...turn, status, events, completedAt };
      return failure === undefined ? base : { ...base, errorMessage: failure };
    });
    await this.#store.save({ ...record, turns, updatedAt: completedAt });
  }
}

function isPathWithin(root: string, target: string): boolean {
  const child = relative(root, target);
  return (
    child !== "" &&
    child !== ".." &&
    !child.startsWith(`..${process.platform === "win32" ? "\\" : "/"}`) &&
    !isAbsolute(child)
  );
}

function requireString(value: string | undefined, message: string): string {
  if (!value) throw new Error(message);
  return value;
}

function errorMessage(cause: unknown): string {
  return cause instanceof Error ? cause.message : "Codex 运行时返回未知错误";
}

function normalizeMessage(value: string): string {
  const normalized = value.trim();
  if (!normalized) throw new Error("Codex turn 消息不能为空");
  return normalized;
}
