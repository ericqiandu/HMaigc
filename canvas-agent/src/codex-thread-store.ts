import { randomUUID } from "node:crypto";
import {
  chmod,
  lstat,
  mkdir,
  readFile,
  rename,
  writeFile,
} from "node:fs/promises";
import { dirname } from "node:path";

import { z } from "zod";

import type { ThreadEvent } from "@openai/codex-sdk";
import type { LocalAgentExpectedDelivery } from "./contracts.js";

export type NormalizedCodexEvent = Readonly<
  | { kind: "thread_started"; threadId: string; sdkThreadId: string }
  | { kind: "turn_started"; threadId: string; turnId: string }
  | {
      kind: "item_started" | "item_updated" | "item_completed";
      threadId: string;
      turnId: string;
      event: ThreadEvent;
    }
  | {
      kind: "final_decision";
      threadId: string;
      turnId: string;
      message: string;
      expectedDelivery: LocalAgentExpectedDelivery;
    }
  | {
      kind: "turn_completed";
      threadId: string;
      turnId: string;
      event: ThreadEvent;
    }
  | {
      kind: "turn_failed";
      threadId: string;
      turnId: string;
      message: string;
      event?: ThreadEvent;
    }
  | { kind: "turn_cancelled"; threadId: string; turnId: string }
>;

export type LocalCodexTurnRecord = Readonly<{
  turnId: string;
  status: "running" | "completed" | "failed" | "cancelled";
  message: string;
  attachments: ReadonlyArray<
    Readonly<{
      kind: "image" | "file";
      name: string;
      mimeType: string;
    }>
  >;
  events: NormalizedCodexEvent[];
  createdAt: string;
  completedAt?: string;
  errorMessage?: string;
}>;

export type LocalCodexThreadRecord = Readonly<{
  threadId: string;
  sdkThreadId?: string;
  canvasId: string;
  workspaceRoot: string;
  model: string;
  createdAt: string;
  updatedAt: string;
  archivedAt?: string;
  turns: LocalCodexTurnRecord[];
}>;

export type CodexThreadStore = Readonly<{
  create: (record: LocalCodexThreadRecord) => Promise<void>;
  get: (threadId: string) => Promise<LocalCodexThreadRecord | undefined>;
  list: (canvasId: string) => Promise<LocalCodexThreadRecord[]>;
  save: (record: LocalCodexThreadRecord) => Promise<void>;
  recoverRunningTurns: (completedAt: string) => Promise<number>;
}>;

const identitySchema = z.string().min(1).max(240);
const timestampSchema = z.iso.datetime({ offset: true });
const normalizedEventSchema = z.custom<NormalizedCodexEvent>(
  isNormalizedCodexEvent,
  "Codex 事件记录无效",
);
const attachmentSchema = z
  .object({
    kind: z.enum(["image", "file"]),
    name: z.string().min(1).max(240),
    mimeType: z.string().min(1).max(255),
  })
  .strict();
const turnSchema = z
  .object({
    turnId: identitySchema,
    status: z.enum(["running", "completed", "failed", "cancelled"]),
    message: z
      .string()
      .min(1)
      .max(64 * 1024),
    attachments: z.array(attachmentSchema).max(16),
    events: z.array(normalizedEventSchema).max(100_000),
    createdAt: timestampSchema,
    completedAt: timestampSchema.optional(),
    errorMessage: z
      .string()
      .min(1)
      .max(8 * 1024)
      .optional(),
  })
  .strict();
const threadSchema = z
  .object({
    threadId: identitySchema,
    sdkThreadId: identitySchema.optional(),
    canvasId: identitySchema,
    workspaceRoot: z.string().min(1).max(4096),
    model: z.string().min(1).max(240),
    createdAt: timestampSchema,
    updatedAt: timestampSchema,
    archivedAt: timestampSchema.optional(),
    turns: z.array(turnSchema).max(10_000),
  })
  .strict();
const storeDocumentSchema = z
  .object({
    version: z.literal(1),
    threads: z.array(threadSchema).max(100_000),
  })
  .strict();

export class InMemoryCodexThreadStore implements CodexThreadStore {
  readonly #records = new Map<string, LocalCodexThreadRecord>();

  async create(record: LocalCodexThreadRecord): Promise<void> {
    const validated = parseThreadRecord(record);
    if (this.#records.has(validated.threadId))
      throw new Error("本机 Codex 线程已经存在");
    this.#records.set(validated.threadId, structuredClone(validated));
  }

  async get(threadId: string): Promise<LocalCodexThreadRecord | undefined> {
    const record = this.#records.get(threadId);
    return record ? structuredClone(record) : undefined;
  }

  async list(canvasId: string): Promise<LocalCodexThreadRecord[]> {
    return sortAndClone(
      [...this.#records.values()].filter(
        (record) => record.canvasId === canvasId,
      ),
    );
  }

  async save(record: LocalCodexThreadRecord): Promise<void> {
    const validated = parseThreadRecord(record);
    if (!this.#records.has(validated.threadId))
      throw new Error("本机 Codex 线程不存在");
    this.#records.set(validated.threadId, structuredClone(validated));
  }

  async recoverRunningTurns(completedAt: string): Promise<number> {
    let recovered = 0;
    for (const [threadId, record] of this.#records) {
      const result = recoverRecord(record, completedAt);
      recovered += result.recovered;
      this.#records.set(threadId, result.record);
    }
    return recovered;
  }
}

export class FileCodexThreadStore implements CodexThreadStore {
  readonly #path: string;
  #pending: Promise<void> = Promise.resolve();

  constructor(path: string) {
    if (!path.trim()) throw new Error("本机 Codex 线程目录路径不能为空");
    this.#path = path;
  }

  create(record: LocalCodexThreadRecord): Promise<void> {
    return this.#serialize(async () => {
      const records = await this.#load();
      const validated = parseThreadRecord(record);
      if (records.some((current) => current.threadId === validated.threadId))
        throw new Error("本机 Codex 线程已经存在");
      await this.#write([...records, validated]);
    });
  }

  get(threadId: string): Promise<LocalCodexThreadRecord | undefined> {
    return this.#serialize(async () => {
      const record = (await this.#load()).find(
        (current) => current.threadId === threadId,
      );
      return record ? structuredClone(record) : undefined;
    });
  }

  list(canvasId: string): Promise<LocalCodexThreadRecord[]> {
    return this.#serialize(async () =>
      sortAndClone(
        (await this.#load()).filter((record) => record.canvasId === canvasId),
      ),
    );
  }

  save(record: LocalCodexThreadRecord): Promise<void> {
    return this.#serialize(async () => {
      const records = await this.#load();
      const validated = parseThreadRecord(record);
      const index = records.findIndex(
        (current) => current.threadId === validated.threadId,
      );
      if (index < 0) throw new Error("本机 Codex 线程不存在");
      records[index] = validated;
      await this.#write(records);
    });
  }

  recoverRunningTurns(completedAt: string): Promise<number> {
    return this.#serialize(async () => {
      const records = await this.#load();
      let recovered = 0;
      const updated = records.map((record) => {
        const result = recoverRecord(record, completedAt);
        recovered += result.recovered;
        return result.record;
      });
      if (recovered > 0) await this.#write(updated);
      return recovered;
    });
  }

  #serialize<Result>(operation: () => Promise<Result>): Promise<Result> {
    const result = this.#pending.then(operation, operation);
    this.#pending = result.then(
      () => undefined,
      () => undefined,
    );
    return result;
  }

  async #load(): Promise<LocalCodexThreadRecord[]> {
    let metadata;
    try {
      metadata = await lstat(this.#path);
    } catch (cause) {
      if (isMissingFileError(cause)) return [];
      throw cause;
    }
    if (!metadata.isFile() || metadata.isSymbolicLink())
      throw new Error("本机 Codex 线程目录必须是普通文件");
    if (process.platform !== "win32" && (metadata.mode & 0o777) !== 0o600) {
      throw new Error("本机 Codex 线程目录权限必须是 0600");
    }
    const document = storeDocumentSchema.parse(
      JSON.parse(await readFile(this.#path, "utf8")),
    );
    const identities = new Set<string>();
    for (const record of document.threads) {
      if (identities.has(record.threadId))
        throw new Error("本机 Codex 线程目录包含重复 threadId");
      identities.add(record.threadId);
    }
    return document.threads.map(parseThreadRecord);
  }

  async #write(records: LocalCodexThreadRecord[]): Promise<void> {
    await mkdir(dirname(this.#path), { recursive: true, mode: 0o700 });
    const temporaryPath = `${this.#path}.tmp-${process.pid}-${randomUUID()}`;
    const document = storeDocumentSchema.parse({
      version: 1,
      threads: records,
    });
    await writeFile(temporaryPath, `${JSON.stringify(document, null, 2)}\n`, {
      encoding: "utf8",
      mode: 0o600,
    });
    if (process.platform !== "win32") await chmod(temporaryPath, 0o600);
    await rename(temporaryPath, this.#path);
    if (process.platform !== "win32") await chmod(this.#path, 0o600);
  }
}

function parseThreadRecord(value: unknown): LocalCodexThreadRecord {
  const parsed = threadSchema.parse(value);
  const turns: LocalCodexTurnRecord[] = parsed.turns.map((turn) => ({
    turnId: turn.turnId,
    status: turn.status,
    message: turn.message,
    attachments: turn.attachments,
    events: turn.events,
    createdAt: turn.createdAt,
    ...(turn.completedAt === undefined
      ? {}
      : { completedAt: turn.completedAt }),
    ...(turn.errorMessage === undefined
      ? {}
      : { errorMessage: turn.errorMessage }),
  }));
  return {
    threadId: parsed.threadId,
    canvasId: parsed.canvasId,
    workspaceRoot: parsed.workspaceRoot,
    model: parsed.model,
    createdAt: parsed.createdAt,
    updatedAt: parsed.updatedAt,
    turns,
    ...(parsed.sdkThreadId === undefined
      ? {}
      : { sdkThreadId: parsed.sdkThreadId }),
    ...(parsed.archivedAt === undefined
      ? {}
      : { archivedAt: parsed.archivedAt }),
  };
}

function sortAndClone(
  records: LocalCodexThreadRecord[],
): LocalCodexThreadRecord[] {
  return records
    .sort((left, right) => right.updatedAt.localeCompare(left.updatedAt))
    .map((record) => structuredClone(record));
}

function recoverRecord(
  record: LocalCodexThreadRecord,
  completedAt: string,
): Readonly<{
  record: LocalCodexThreadRecord;
  recovered: number;
}> {
  let recovered = 0;
  const turns = record.turns.map((turn): LocalCodexTurnRecord => {
    if (turn.status !== "running") return turn;
    recovered += 1;
    return {
      ...turn,
      status: "failed",
      completedAt,
      errorMessage: "本机 Agent 进程在 turn 完成前中断",
    };
  });
  return recovered === 0
    ? { record, recovered }
    : { record: { ...record, turns, updatedAt: completedAt }, recovered };
}

function isNormalizedCodexEvent(value: unknown): value is NormalizedCodexEvent {
  if (
    !isRecord(value) ||
    typeof value.kind !== "string" ||
    typeof value.threadId !== "string"
  )
    return false;
  switch (value.kind) {
    case "thread_started":
      return typeof value.sdkThreadId === "string";
    case "turn_started":
    case "turn_cancelled":
      return typeof value.turnId === "string";
    case "item_started":
    case "item_updated":
    case "item_completed":
    case "turn_completed":
      return (
        typeof value.turnId === "string" &&
        isRecord(value.event) &&
        typeof value.event.type === "string"
      );
    case "final_decision":
      return (
        typeof value.turnId === "string" &&
        typeof value.message === "string" &&
        isExpectedDelivery(value.expectedDelivery)
      );
    case "turn_failed":
      return (
        typeof value.turnId === "string" &&
        typeof value.message === "string" &&
        (value.event === undefined ||
          (isRecord(value.event) && typeof value.event.type === "string"))
      );
    default:
      return false;
  }
}

function isExpectedDelivery(value: unknown): boolean {
  if (
    !isRecord(value) ||
    typeof value.kind !== "string" ||
    !Array.isArray(value.completionCriteria) ||
    value.completionCriteria.length < 1
  )
    return false;
  return value.completionCriteria.every(
    (criterion) => isRecord(criterion) && typeof criterion.fact === "string",
  );
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function isMissingFileError(cause: unknown): cause is NodeJS.ErrnoException {
  return cause instanceof Error && "code" in cause && cause.code === "ENOENT";
}
