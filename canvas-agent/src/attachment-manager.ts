import { randomUUID } from "node:crypto";
import { mkdir, mkdtemp, rm, writeFile } from "node:fs/promises";
import { extname, join } from "node:path";
import { isAbsolute, resolve } from "node:path";

import type { LocalAgentAttachment } from "./contracts.js";
import type { ResolvedCodexAttachment } from "./codex-thread.js";

export const MAX_ATTACHMENT_BYTES = 25 * 1024 * 1024;
export const MAX_TURN_ATTACHMENT_BYTES = 30 * 1024 * 1024;

export type AttachmentManagerOptions = Readonly<{
  root: string;
  allowedOrigins: string[];
  fetch?: typeof fetch;
  requestTimeoutMs?: number;
}>;

export type IngestedAttachments = Readonly<{
  directory: string;
  attachments: ResolvedCodexAttachment[];
}>;

export class AttachmentManager {
  readonly #root: string;
  readonly #allowedOrigins: Set<string>;
  readonly #fetch: typeof fetch;
  readonly #requestTimeoutMs: number;

  constructor(options: AttachmentManagerOptions) {
    if (!options.root.trim() || !isAbsolute(options.root))
      throw new Error("附件暂存根目录必须是绝对路径");
    if (options.allowedOrigins.length === 0)
      throw new Error("附件 Origin allowlist 不能为空");
    this.#root = resolve(options.root);
    this.#allowedOrigins = new Set(options.allowedOrigins);
    this.#fetch = options.fetch ?? fetch;
    this.#requestTimeoutMs = options.requestTimeoutMs ?? 30_000;
    if (
      !Number.isSafeInteger(this.#requestTimeoutMs) ||
      this.#requestTimeoutMs < 1 ||
      this.#requestTimeoutMs > 120_000
    ) {
      throw new Error("附件下载超时时间无效");
    }
  }

  async ingest(
    attachments: LocalAgentAttachment[],
  ): Promise<IngestedAttachments> {
    if (attachments.length > 16) throw new Error("附件数量不能超过 16");
    await mkdir(this.#root, { recursive: true, mode: 0o700 });
    const directory = await mkdtemp(join(this.#root, "turn-"));
    let totalBytes = 0;
    const resolved: ResolvedCodexAttachment[] = [];
    try {
      for (const attachment of attachments) {
        const content = await this.#download(attachment);
        totalBytes += content.byteLength;
        if (totalBytes > MAX_TURN_ATTACHMENT_BYTES)
          throw new Error("本轮附件总大小超过 30MB");
        const suffix = safeExtension(attachment.name);
        const localPath = join(directory, `${randomUUID()}${suffix}`);
        await writeFile(localPath, content, { mode: 0o600 });
        resolved.push({
          kind: attachment.kind,
          name: attachment.name,
          mimeType: attachment.mimeType,
          localPath,
        });
      }
      return { directory, attachments: resolved };
    } catch (cause) {
      await this.cleanup(directory);
      throw cause;
    }
  }

  async cleanup(directory: string): Promise<void> {
    if (
      !directory.startsWith(
        `${this.#root}${process.platform === "win32" ? "\\" : "/"}`,
      )
    ) {
      throw new Error("拒绝清理非受管附件目录");
    }
    await rm(directory, { recursive: true, force: true });
  }

  async #download(attachment: LocalAgentAttachment): Promise<Uint8Array> {
    let current = parseAllowedURL(attachment.url, this.#allowedOrigins);
    for (let redirectCount = 0; redirectCount <= 3; redirectCount += 1) {
      const response = await this.#fetch(current, {
        method: "GET",
        redirect: "manual",
        credentials: "omit",
        referrerPolicy: "no-referrer",
        signal: AbortSignal.timeout(this.#requestTimeoutMs),
        headers: { Accept: attachment.mimeType },
      });
      if (isRedirect(response.status)) {
        const location = response.headers.get("location");
        if (!location) throw new Error("附件下载重定向缺少 Location");
        current = parseAllowedURL(
          new URL(location, current).toString(),
          this.#allowedOrigins,
        );
        continue;
      }
      if (!response.ok)
        throw new Error(`附件下载失败: HTTP ${response.status}`);
      const contentType = response.headers
        .get("content-type")
        ?.split(";", 1)[0]
        ?.trim()
        .toLowerCase();
      if (!contentType || contentType !== attachment.mimeType.toLowerCase()) {
        throw new Error("附件响应类型与声明不一致");
      }
      const declaredSize = Number(response.headers.get("content-length"));
      if (Number.isFinite(declaredSize) && declaredSize > MAX_ATTACHMENT_BYTES)
        throw new Error("单个附件超过 25MB");
      if (!response.body) throw new Error("附件响应缺少内容");
      const reader = response.body.getReader();
      const chunks: Uint8Array[] = [];
      let bytes = 0;
      while (true) {
        const result = await reader.read();
        if (result.done) break;
        bytes += result.value.byteLength;
        if (bytes > MAX_ATTACHMENT_BYTES) {
          await reader.cancel("attachment too large");
          throw new Error("单个附件超过 25MB");
        }
        chunks.push(result.value);
      }
      const content = new Uint8Array(bytes);
      let offset = 0;
      for (const chunk of chunks) {
        content.set(chunk, offset);
        offset += chunk.byteLength;
      }
      return content;
    }
    throw new Error("附件下载重定向次数超过限制");
  }
}

function parseAllowedURL(value: string, allowedOrigins: Set<string>): URL {
  const url = new URL(value);
  if (
    url.protocol !== "https:" ||
    url.username ||
    url.password ||
    !allowedOrigins.has(url.origin)
  ) {
    throw new Error("附件 URL Origin 未获授权");
  }
  return url;
}

function safeExtension(name: string): string {
  const suffix = extname(name).toLowerCase();
  return /^\.[a-z0-9]{1,10}$/.test(suffix) ? suffix : "";
}

function isRedirect(status: number): boolean {
  return (
    status === 301 ||
    status === 302 ||
    status === 303 ||
    status === 307 ||
    status === 308
  );
}
