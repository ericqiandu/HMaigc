import type { CanvasToolRequest, CanvasToolSession } from "./canvas-session.js";
import { parseToolResultInput, type ToolResultInput } from "./contracts.js";

export type LoopbackCanvasToolSessionOptions = Readonly<{
  url: string;
  token: string;
  fetch?: typeof fetch;
}>;

export class LoopbackCanvasToolSession implements CanvasToolSession {
  readonly #url: string;
  readonly #token: string;
  readonly #fetch: typeof fetch;

  constructor(options: LoopbackCanvasToolSessionOptions) {
    const url = new URL(options.url);
    if (
      url.protocol !== "http:" ||
      url.hostname !== "127.0.0.1" ||
      !url.port ||
      url.pathname !== "/" ||
      url.search ||
      url.hash
    ) {
      throw new Error("MCP 工具代理只能连接 loopback Canvas Agent");
    }
    if (!options.token) throw new Error("MCP 工具代理 token 缺失");
    this.#url = url.origin;
    this.#token = options.token;
    this.#fetch = options.fetch ?? fetch;
  }

  async requestTool(request: CanvasToolRequest): Promise<ToolResultInput> {
    const requestInit: RequestInit = {
      method: "POST",
      headers: {
        "content-type": "application/json",
        "x-hmaigc-agent-token": this.#token,
      },
      body: JSON.stringify({
        requestId: request.requestId,
        threadId: request.threadId,
        turnId: request.turnId,
        toolName: request.toolName,
        arguments: request.arguments,
        expectedDelivery: request.expectedDelivery,
      }),
      ...(request.signal === undefined ? {} : { signal: request.signal }),
    };
    const response = await this.#fetch(
      `${this.#url}/internal/mcp/tools`,
      requestInit,
    );
    const body: unknown = await response.json().catch(() => undefined);
    if (!response.ok)
      throw new Error(`Canvas MCP 工具代理失败: HTTP ${response.status}`);
    return parseToolResultInput(body);
  }
}
