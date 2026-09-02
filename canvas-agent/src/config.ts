import { timingSafeEqual, randomBytes } from "node:crypto";
import { chmod, lstat, mkdir, readFile, writeFile } from "node:fs/promises";
import { homedir } from "node:os";
import { isIP } from "node:net";
import { dirname, isAbsolute, join } from "node:path";

import { z } from "zod";

export const LOCAL_AGENT_PORT = 17371;
export const LOCAL_AGENT_PROTOCOL_VERSION = 1;
export const LOCAL_AGENT_CONFIG_DIRECTORY = join(
  homedir(),
  ".hmaigc",
  "canvas-agent",
);
export const LOCAL_AGENT_CONFIG_PATH = join(
  LOCAL_AGENT_CONFIG_DIRECTORY,
  "config.json",
);

export type LocalCanvasWorkspace = Readonly<{
  workspaceRoot: string;
}>;

export type LocalAgentConfig = Readonly<{
  url: `http://127.0.0.1:${number}`;
  token: string;
  allowedOrigins: string[];
  allowedAttachmentOrigins: string[];
  codex: Readonly<{
    model: string;
    modelReasoningEffort?:
      | "minimal"
      | "low"
      | "medium"
      | "high"
      | "xhigh"
      | "max"
      | "ultra"
      | "persistent";
  }>;
  canvases: Record<string, LocalCanvasWorkspace>;
}>;

export type ProtectedRequestFacts = Readonly<{
  origin: string | undefined;
  tokenHeader: string | undefined;
  queryToken?: string;
}>;

const workspaceSchema = z
  .object({
    workspaceRoot: z
      .string()
      .min(1)
      .max(4096)
      .refine((value) => isAbsolute(value), "workspaceRoot 必须是绝对路径"),
  })
  .strict();

const configSchema = z
  .object({
    url: z.string().min(1).max(128),
    token: z.string().min(1).max(512),
    allowedOrigins: z.array(z.string().min(1).max(512)).min(1).max(16),
    allowedAttachmentOrigins: z
      .array(z.string().min(1).max(512))
      .min(1)
      .max(32),
    codex: z
      .object({
        model: z.string().trim().min(1).max(240),
        modelReasoningEffort: z
          .enum([
            "minimal",
            "low",
            "medium",
            "high",
            "xhigh",
            "max",
            "ultra",
            "persistent",
          ])
          .optional(),
      })
      .strict(),
    canvases: z.record(z.string().min(1).max(120), workspaceSchema),
  })
  .strict();

export function generateLocalAgentToken(): string {
  return randomBytes(32).toString("base64url");
}

export function parseLocalAgentConfig(value: unknown): LocalAgentConfig {
  const parsed = configSchema.parse(value);
  const url = parseLoopbackURL(parsed.url);
  assertTokenHasAtLeast256Bits(parsed.token);
  const allowedOrigins = parsed.allowedOrigins.map(parseAllowedOrigin);
  const allowedAttachmentOrigins = parsed.allowedAttachmentOrigins.map(
    parsePublicAttachmentOrigin,
  );
  if (new Set(allowedOrigins).size !== allowedOrigins.length) {
    throw new Error("Origin allowlist 不能包含重复项");
  }
  if (
    new Set(allowedAttachmentOrigins).size !== allowedAttachmentOrigins.length
  ) {
    throw new Error("附件 Origin allowlist 不能包含重复项");
  }
  return {
    url,
    token: parsed.token,
    allowedOrigins,
    allowedAttachmentOrigins,
    codex: {
      model: parsed.codex.model,
      ...(parsed.codex.modelReasoningEffort === undefined
        ? {}
        : { modelReasoningEffort: parsed.codex.modelReasoningEffort }),
    },
    canvases: parsed.canvases,
  };
}

export async function writeLocalAgentConfig(
  path: string,
  config: LocalAgentConfig,
): Promise<void> {
  const validated = parseLocalAgentConfig(config);
  await mkdir(dirname(path), { recursive: true, mode: 0o700 });
  await writeFile(path, `${JSON.stringify(validated, null, 2)}\n`, {
    encoding: "utf8",
    mode: 0o600,
  });
  if (process.platform !== "win32") {
    await chmod(path, 0o600);
  }
}

export async function loadLocalAgentConfig(
  path = LOCAL_AGENT_CONFIG_PATH,
): Promise<LocalAgentConfig> {
  const metadata = await lstat(path);
  if (!metadata.isFile() || metadata.isSymbolicLink()) {
    throw new Error("本机 Agent 配置必须是普通文件");
  }
  if (process.platform !== "win32" && (metadata.mode & 0o777) !== 0o600) {
    throw new Error("本机 Agent 配置权限必须是 0600");
  }
  return parseLocalAgentConfig(JSON.parse(await readFile(path, "utf8")));
}

export function assertProtectedRequestSecurity(
  config: LocalAgentConfig,
  facts: ProtectedRequestFacts,
): void {
  if (facts.queryToken !== undefined) {
    throw new Error("Agent token 禁止通过 query string 传递");
  }
  if (
    !facts.origin ||
    facts.origin === "null" ||
    !config.allowedOrigins.includes(facts.origin)
  ) {
    throw new Error("请求 Origin 未获授权");
  }
  if (
    !facts.tokenHeader ||
    !constantTimeTextEqual(facts.tokenHeader, config.token)
  ) {
    throw new Error("Agent token 无效");
  }
}

export function assertInternalRequestSecurity(
  config: LocalAgentConfig,
  facts: Readonly<{
    tokenHeader: string | undefined;
    queryToken?: string;
  }>,
): void {
  if (facts.queryToken !== undefined)
    throw new Error("Agent token 禁止通过 query string 传递");
  if (
    !facts.tokenHeader ||
    !constantTimeTextEqual(facts.tokenHeader, config.token)
  )
    throw new Error("Agent token 无效");
}

function parseLoopbackURL(value: string): `http://127.0.0.1:${number}` {
  let url: URL;
  try {
    url = new URL(value);
  } catch {
    throw new Error("本机 Agent URL 无效");
  }
  if (
    url.protocol !== "http:" ||
    url.hostname !== "127.0.0.1" ||
    !url.port ||
    url.username ||
    url.password ||
    url.pathname !== "/" ||
    url.search ||
    url.hash
  ) {
    throw new Error("本机 Agent 只能绑定 http://127.0.0.1:<port>");
  }
  const port = Number(url.port);
  if (!Number.isSafeInteger(port) || port < 1 || port > 65535) {
    throw new Error("本机 Agent 端口无效");
  }
  return `http://127.0.0.1:${port}`;
}

function parseAllowedOrigin(value: string): string {
  if (value === "null" || value === "*") {
    throw new Error("Origin allowlist 不允许 null 或通配符");
  }
  let url: URL;
  try {
    url = new URL(value);
  } catch {
    throw new Error("Origin allowlist 包含无效地址");
  }
  if (
    (url.protocol !== "http:" && url.protocol !== "https:") ||
    url.username ||
    url.password ||
    url.pathname !== "/" ||
    url.search ||
    url.hash ||
    value !== url.origin
  ) {
    throw new Error("Origin allowlist 必须使用精确 Origin");
  }
  return url.origin;
}

function parsePublicAttachmentOrigin(value: string): string {
  const origin = parseAllowedOrigin(value);
  const hostname = new URL(origin).hostname
    .replace(/^\[|\]$/g, "")
    .toLowerCase();
  if (hostname === "localhost" || isPrivateAddress(hostname)) {
    throw new Error("附件 Origin 必须是公网地址");
  }
  return origin;
}

function isPrivateAddress(hostname: string): boolean {
  const family = isIP(hostname);
  if (family === 4) {
    const octets = hostname.split(".").map(Number);
    const first = octets[0] ?? -1;
    const second = octets[1] ?? -1;
    return (
      first === 0 ||
      first === 10 ||
      first === 127 ||
      (first === 169 && second === 254) ||
      (first === 172 && second >= 16 && second <= 31) ||
      (first === 192 && second === 168)
    );
  }
  if (family === 6) {
    return (
      hostname === "::" ||
      hostname === "::1" ||
      hostname.startsWith("fc") ||
      hostname.startsWith("fd") ||
      hostname.startsWith("fe8") ||
      hostname.startsWith("fe9") ||
      hostname.startsWith("fea") ||
      hostname.startsWith("feb")
    );
  }
  return false;
}

function assertTokenHasAtLeast256Bits(token: string): void {
  let decoded: Buffer;
  if (/^[0-9a-fA-F]+$/.test(token) && token.length % 2 === 0) {
    decoded = Buffer.from(token, "hex");
  } else if (/^[A-Za-z0-9_-]+$/.test(token)) {
    if (token.length % 4 === 1) throw new Error("Agent token 编码无效");
    decoded = Buffer.from(token, "base64url");
  } else {
    throw new Error("Agent token 编码无效");
  }
  if (decoded.byteLength < 32) {
    throw new Error("Agent token 至少需要 256 bit 熵");
  }
}

function constantTimeTextEqual(left: string, right: string): boolean {
  const leftBytes = Buffer.from(left);
  const rightBytes = Buffer.from(right);
  if (leftBytes.byteLength !== rightBytes.byteLength) return false;
  return timingSafeEqual(leftBytes, rightBytes);
}
