export type LocalAgentStreamEvent = Readonly<{ kind: string }>;

export type SseConnection = Readonly<{
  write: (chunk: string) => boolean;
  waitForDrain: () => Promise<void>;
  end: () => void;
  onClose: (listener: () => void) => void;
}>;

export class LocalAgentEventHub {
  #connection: SseConnection | undefined;
  #disconnectListener: (() => void) | undefined;

  get connected(): boolean {
    return this.#connection !== undefined;
  }

  connect(connection: SseConnection, onDisconnect: () => void): () => void {
    if (this.#connection) throw new Error("本机 Agent 已有活动事件连接");
    this.#connection = connection;
    this.#disconnectListener = onDisconnect;
    let closed = false;
    const close = () => {
      if (closed) return;
      closed = true;
      if (this.#connection === connection) {
        this.#connection = undefined;
        const listener = this.#disconnectListener;
        this.#disconnectListener = undefined;
        listener?.();
      }
    };
    connection.onClose(close);
    if (!connection.write(formatSse({ kind: "connected" }))) {
      connection.end();
      close();
    }
    return close;
  }

  async emit(event: LocalAgentStreamEvent): Promise<void> {
    const connection = this.#connection;
    if (!connection) throw new Error("本机 Agent 浏览器事件连接不可用");
    if (!connection.write(formatSse(event))) {
      await connection.waitForDrain();
      if (this.#connection !== connection)
        throw new Error("本机 Agent 浏览器事件连接已断开");
    }
  }

  disconnect(): void {
    const connection = this.#connection;
    if (!connection) return;
    this.#connection = undefined;
    const listener = this.#disconnectListener;
    this.#disconnectListener = undefined;
    connection.end();
    listener?.();
  }
}

function formatSse(event: LocalAgentStreamEvent): string {
  return `data: ${JSON.stringify(event)}\n\n`;
}
