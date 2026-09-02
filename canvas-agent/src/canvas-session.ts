import { createHash } from "node:crypto";

import { LOCAL_AGENT_PROTOCOL_VERSION } from "./config.js";
import {
  parseToolResultInput,
  type CanonicalToolName,
  type LocalAgentExpectedDelivery,
  type ToolCallEvent,
  type ToolResultInput,
} from "./contracts.js";

export type CanvasToolRequest = Readonly<{
  requestId: string;
  threadId: string;
  turnId: string;
  toolName: CanonicalToolName;
  arguments: Record<string, unknown>;
  expectedDelivery: LocalAgentExpectedDelivery;
  signal?: AbortSignal;
}>;

export type CanvasSessionOptions = Readonly<{
  emit: (event: ToolCallEvent) => void | Promise<void>;
  timeoutMs?: number;
  now?: () => Date;
}>;

export type CanvasToolSession = Readonly<{
  requestTool: (request: CanvasToolRequest) => Promise<ToolResultInput>;
}>;

type PendingToolRequest = {
  request: CanvasToolRequest;
  resolve: (result: ToolResultInput) => void;
  reject: (error: Error) => void;
  timeout: ReturnType<typeof setTimeout>;
  abortHandler?: () => void;
};

const MAX_COMPLETED_TOOL_REQUESTS = 4_096;
const WEBSITE_APPROVAL_PROPOSAL_TTL_MS = 15 * 60_000;
export const APPROVAL_RESULT_GRACE_MS = 60_000;
export const DEFAULT_CANVAS_TOOL_RESULT_TIMEOUT_MS =
  WEBSITE_APPROVAL_PROPOSAL_TTL_MS + APPROVAL_RESULT_GRACE_MS;

export class CanvasSession {
  readonly #emit: (event: ToolCallEvent) => void | Promise<void>;
  readonly #timeoutMs: number;
  readonly #now: () => Date;
  readonly #pending = new Map<string, PendingToolRequest>();
  readonly #completed = new Map<string, string | null>();
  #disconnectReason: string | undefined;

  constructor(options: CanvasSessionOptions) {
    this.#emit = options.emit;
    this.#timeoutMs =
      options.timeoutMs ?? DEFAULT_CANVAS_TOOL_RESULT_TIMEOUT_MS;
    this.#now = options.now ?? (() => new Date());
    if (
      !Number.isSafeInteger(this.#timeoutMs) ||
      this.#timeoutMs < 1 ||
      this.#timeoutMs > DEFAULT_CANVAS_TOOL_RESULT_TIMEOUT_MS
    ) {
      throw new Error("CanvasSession 超时时间无效");
    }
  }

  requestTool(request: CanvasToolRequest): Promise<ToolResultInput> {
    if (this.#disconnectReason) {
      throw new Error(`CanvasSession 已断开: ${this.#disconnectReason}`);
    }
    if (
      !request.requestId ||
      !request.threadId ||
      !request.turnId ||
      this.#pending.has(request.requestId) ||
      this.#completed.has(request.requestId)
    ) {
      throw new Error("CanvasSession 工具请求身份无效或重复");
    }
    if (request.signal?.aborted) {
      throw new Error("CanvasSession 工具请求已经取消");
    }

    return new Promise<ToolResultInput>((resolve, reject) => {
      const timeout = setTimeout(() => {
        this.#completeWithError(
          request.requestId,
          new Error(`CanvasSession 工具请求超时: ${request.requestId}`),
        );
      }, this.#timeoutMs);
      const pending: PendingToolRequest = { request, resolve, reject, timeout };
      if (request.signal) {
        pending.abortHandler = () => {
          this.#completeWithError(
            request.requestId,
            new Error(`CanvasSession 工具请求已取消: ${request.requestId}`),
          );
        };
        request.signal.addEventListener("abort", pending.abortHandler, {
          once: true,
        });
      }
      this.#pending.set(request.requestId, pending);
      try {
        const emission = this.#emit({
          protocolVersion: LOCAL_AGENT_PROTOCOL_VERSION,
          kind: "tool_call",
          requestId: request.requestId,
          threadId: request.threadId,
          turnId: request.turnId,
          toolName: request.toolName,
          arguments: request.arguments,
          expectedDelivery: request.expectedDelivery,
          createdAt: this.#now().toISOString(),
        });
        void Promise.resolve(emission).catch((cause: unknown) => {
          this.#completeWithError(
            request.requestId,
            cause instanceof Error
              ? cause
              : new Error("CanvasSession 事件发送失败"),
          );
        });
      } catch (cause) {
        this.#completeWithError(
          request.requestId,
          cause instanceof Error
            ? cause
            : new Error("CanvasSession 事件发送失败"),
        );
      }
    });
  }

  resolveToolResult(value: unknown): void {
    const result = parseToolResultInput(value);
    const fingerprint = resultFingerprint(result);
    const completedFingerprint = this.#completed.get(result.requestId);
    if (completedFingerprint !== undefined) {
      if (completedFingerprint === fingerprint) return;
      throw new Error(`CanvasSession 工具结果重放冲突: ${result.requestId}`);
    }
    const pending = this.#pending.get(result.requestId);
    if (!pending) {
      throw new Error(`CanvasSession 工具请求不存在: ${result.requestId}`);
    }
    if (pending.request.threadId !== result.threadId)
      throw new Error("工具结果 threadId 与请求不一致");
    if (pending.request.turnId !== result.turnId)
      throw new Error("工具结果 turnId 与请求不一致");
    if (pending.request.toolName !== result.toolName)
      throw new Error("工具结果 toolName 与请求不一致");

    this.#finishPending(pending, fingerprint);
    pending.resolve(result);
  }

  disconnect(reason: string): void {
    const normalized = reason.trim();
    if (!normalized) throw new Error("CanvasSession 断开原因不能为空");
    if (this.#disconnectReason) return;
    this.#disconnectReason = normalized;
    for (const requestId of [...this.#pending.keys()]) {
      this.#completeWithError(
        requestId,
        new Error(`CanvasSession 已断开: ${normalized}`),
      );
    }
  }

  #completeWithError(requestId: string, error: Error): void {
    const pending = this.#pending.get(requestId);
    if (!pending) return;
    this.#finishPending(pending, null);
    pending.reject(error);
  }

  #finishPending(
    pending: PendingToolRequest,
    resultFingerprintValue: string | null,
  ): void {
    clearTimeout(pending.timeout);
    if (pending.abortHandler && pending.request.signal) {
      pending.request.signal.removeEventListener("abort", pending.abortHandler);
    }
    this.#pending.delete(pending.request.requestId);
    this.#completed.set(pending.request.requestId, resultFingerprintValue);
    while (this.#completed.size > MAX_COMPLETED_TOOL_REQUESTS) {
      const oldestRequestId = this.#completed.keys().next().value;
      if (typeof oldestRequestId !== "string") break;
      this.#completed.delete(oldestRequestId);
    }
  }
}

function resultFingerprint(result: ToolResultInput): string {
  return createHash("sha256")
    .update(JSON.stringify(result))
    .digest("base64url");
}
