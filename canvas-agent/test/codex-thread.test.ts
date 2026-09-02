import assert from "node:assert/strict";
import { mkdtemp, mkdir, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";

import type {
  CodexOptions,
  Input,
  ThreadEvent,
  ThreadOptions,
  TurnOptions,
} from "@openai/codex-sdk";

import {
  CodexThreadService,
  FileCodexThreadStore,
  InMemoryCodexThreadStore,
  buildCanvasAgentInstructions,
  sanitizeCodexEnvironment,
  type CodexClientPort,
  type CodexThreadStore,
  type CodexThreadPort,
  type LocalCodexThreadRecord,
} from "../src/codex-thread.js";

const completedUsage = {
  input_tokens: 10,
  cached_input_tokens: 2,
  cache_write_input_tokens: 0,
  output_tokens: 4,
  reasoning_output_tokens: 1,
};

const finalDecisionText = (message: string): string =>
  JSON.stringify({
    message,
    expectedDelivery: {
      kind: "answer",
      requiredArtifacts: null,
      targetCanvasId: null,
      completionCriteria: [{ fact: "final_message", artifact: null }],
    },
  });

class FakeCodexThread implements CodexThreadPort {
  id: string | null = null;
  readonly inputs: Input[] = [];
  readonly turnOptions: TurnOptions[] = [];
  events: ThreadEvent[] = [];
  waitForAbort = false;

  async runStreamed(
    input: Input,
    options?: TurnOptions,
  ): Promise<{ events: AsyncGenerator<ThreadEvent> }> {
    this.inputs.push(input);
    this.turnOptions.push(options ?? {});
    const events = this.events;
    const thread = this;
    const waitForAbort = this.waitForAbort;
    return {
      events: (async function* stream(): AsyncGenerator<ThreadEvent> {
        if (waitForAbort) {
          await new Promise<void>((resolve) => {
            if (options?.signal?.aborted) return resolve();
            options?.signal?.addEventListener("abort", () => resolve(), {
              once: true,
            });
          });
          throw new Error("aborted by test");
        }
        for (const event of events) {
          if (event.type === "thread.started") thread.id = event.thread_id;
          yield event;
        }
      })(),
    };
  }
}

class FakeCodexClient implements CodexClientPort {
  readonly started: ThreadOptions[] = [];
  readonly resumed: Array<{ id: string; options: ThreadOptions }> = [];
  readonly threads: FakeCodexThread[] = [];
  nextEvents: ThreadEvent[] = [];
  nextWaitForAbort = false;

  startThread(options: ThreadOptions): CodexThreadPort {
    this.started.push(options);
    const thread = new FakeCodexThread();
    thread.events = this.nextEvents;
    thread.waitForAbort = this.nextWaitForAbort;
    this.threads.push(thread);
    return thread;
  }

  resumeThread(id: string, options: ThreadOptions): CodexThreadPort {
    this.resumed.push({ id, options });
    const thread = new FakeCodexThread();
    thread.id = id;
    thread.events = this.nextEvents;
    thread.waitForAbort = this.nextWaitForAbort;
    this.threads.push(thread);
    return thread;
  }
}

class CountingCodexThreadStore implements CodexThreadStore {
  readonly delegate = new InMemoryCodexThreadStore();
  saveCalls = 0;

  create(record: LocalCodexThreadRecord): Promise<void> {
    return this.delegate.create(record);
  }

  get(threadId: string): Promise<LocalCodexThreadRecord | undefined> {
    return this.delegate.get(threadId);
  }

  list(canvasId: string): Promise<LocalCodexThreadRecord[]> {
    return this.delegate.list(canvasId);
  }

  save(record: LocalCodexThreadRecord): Promise<void> {
    this.saveCalls += 1;
    return this.delegate.save(record);
  }

  recoverRunningTurns(completedAt: string): Promise<number> {
    return this.delegate.recoverRunningTurns(completedAt);
  }
}

async function createFixture(): Promise<{
  root: string;
  workspaceRoot: string;
  attachmentRoot: string;
}> {
  const root = await mkdtemp(join(tmpdir(), "hmaigc-codex-thread-"));
  const workspaceRoot = join(root, "workspace");
  const attachmentRoot = join(root, "attachments");
  await mkdir(workspaceRoot);
  await mkdir(attachmentRoot);
  return { root, workspaceRoot, attachmentRoot };
}

test("starts a model-pinned Codex thread, normalizes events, and records local history", async () => {
  const fixture = await createFixture();
  const client = new FakeCodexClient();
  const codexOptions: CodexOptions[] = [];
  const service = new CodexThreadService({
    model: "gpt-5.6-sol",
    modelReasoningEffort: "high",
    attachmentRoot: fixture.attachmentRoot,
    resolveWorkspace: (canvasId) =>
      canvasId === "canvas-1" ? fixture.workspaceRoot : undefined,
    createCodexClient: (options) => {
      codexOptions.push(options);
      return client;
    },
    store: new InMemoryCodexThreadStore(),
    mcp: {
      command: process.execPath,
      args: [join(fixture.root, "index.js"), "mcp"],
      url: "http://127.0.0.1:17371",
      token: "local-token",
    },
    environment: {
      PATH: process.env.PATH,
      HOME: fixture.root,
      HMAIGC_COOKIE: "must-not-leak",
      OSS_ACCESS_KEY_SECRET: "must-not-leak",
      AUTHORIZATION: "must-not-leak",
    },
  });

  client.nextEvents = [
    { type: "thread.started", thread_id: "codex-thread-1" },
    { type: "turn.started" },
    {
      type: "item.completed",
      item: {
        id: "message-1",
        type: "agent_message",
        text: finalDecisionText("已读取"),
      },
    },
    { type: "turn.completed", usage: completedUsage },
  ];
  const execution = await service.startTurn({
    canvasId: "canvas-1",
    message: "读取当前画布并说明下一步",
    attachments: [],
  });
  const streamed = [];
  for await (const event of execution.events) streamed.push(event);

  assert.equal(streamed[0]?.kind, "thread_started");
  assert.equal(
    streamed.some((event) => event.kind === "final_decision"),
    true,
  );
  assert.equal(streamed.at(-1)?.kind, "turn_completed");
  assert.equal(
    client.threads[0]?.turnOptions[0]?.outputSchema instanceof Object,
    true,
  );
  assert.equal(client.started[0]?.model, "gpt-5.6-sol");
  assert.equal(client.started[0]?.modelReasoningEffort, "high");
  assert.equal(client.started[0]?.workingDirectory, fixture.workspaceRoot);
  assert.equal(client.started[0]?.approvalPolicy, "never");
  assert.equal(
    codexOptions[0]?.config?.developer_instructions,
    buildCanvasAgentInstructions("canvas-1"),
  );
  assert.equal(codexOptions[0]?.config?.mcp_servers instanceof Object, true);
  assert.deepEqual(codexOptions[0]?.config?.mcp_servers, {
    hmaigc_canvas: {
      command: process.execPath,
      args: [
        join(fixture.root, "index.js"),
        "mcp",
        "--thread-id",
        execution.threadId,
        "--turn-id",
        execution.turnId,
      ],
      env: {
        HMAIGC_CANVAS_AGENT_URL: "http://127.0.0.1:17371",
        HMAIGC_CODEX_MODEL: "gpt-5.6-sol",
      },
      env_vars: ["HMAIGC_CANVAS_AGENT_TOKEN"],
      enabled_tools: [
        "canvas_get_state",
        "canvas_apply_ops",
        "assets_read",
        "assets_publish",
        "media_generate",
        "skills_load",
      ],
      default_tools_approval_mode: "approve",
    },
  });
  assert.equal(codexOptions[0]?.env?.HMAIGC_CANVAS_AGENT_TOKEN, "local-token");
  assert.equal(codexOptions[0]?.env?.HMAIGC_COOKIE, undefined);
  assert.equal(codexOptions[0]?.env?.OSS_ACCESS_KEY_SECRET, undefined);
  assert.equal(codexOptions[0]?.env?.AUTHORIZATION, undefined);

  const threads = await service.listThreads("canvas-1");
  assert.equal(threads.length, 1);
  assert.equal(threads[0]?.sdkThreadId, "codex-thread-1");
  assert.equal(threads[0]?.turns[0]?.status, "completed");
  assert.equal(threads[0]?.turns[0]?.events.length, 5);
  assert.equal(
    await service
      .readThread("canvas-1", execution.threadId)
      .then((record) => record.model),
    "gpt-5.6-sol",
  );
});

test("persists a streamed turn without rewriting the complete thread document for every event", async () => {
  const fixture = await createFixture();
  const client = new FakeCodexClient();
  const store = new CountingCodexThreadStore();
  const service = new CodexThreadService({
    model: "gpt-5.6-sol",
    attachmentRoot: fixture.attachmentRoot,
    resolveWorkspace: () => fixture.workspaceRoot,
    createCodexClient: () => client,
    store,
    mcp: {
      command: process.execPath,
      args: ["index.js", "mcp"],
      url: "http://127.0.0.1:17371",
      token: "local-token",
    },
    environment: { PATH: process.env.PATH },
  });

  client.nextEvents = [
    { type: "thread.started", thread_id: "codex-thread-checkpointed" },
    { type: "turn.started" },
    {
      type: "item.started",
      item: { id: "message-1", type: "agent_message", text: "" },
    },
    {
      type: "item.updated",
      item: { id: "message-1", type: "agent_message", text: "partial" },
    },
    {
      type: "item.updated",
      item: { id: "message-1", type: "agent_message", text: "complete" },
    },
    {
      type: "item.completed",
      item: {
        id: "message-1",
        type: "agent_message",
        text: finalDecisionText("done"),
      },
    },
    { type: "turn.completed", usage: completedUsage },
  ];
  const execution = await service.startTurn({
    canvasId: "canvas-1",
    message: "执行",
    attachments: [],
  });
  for await (const _event of execution.events) {
    /* consume */
  }

  assert.equal(store.saveCalls, 3);
  assert.equal(
    (await service.readThread("canvas-1", execution.threadId)).turns[0]?.events
      .length,
    8,
  );
});

test("publishes only the last agent message as the final decision when the turn completes", async () => {
  const fixture = await createFixture();
  const client = new FakeCodexClient();
  const service = new CodexThreadService({
    model: "gpt-5.6-sol",
    attachmentRoot: fixture.attachmentRoot,
    resolveWorkspace: () => fixture.workspaceRoot,
    createCodexClient: () => client,
    store: new InMemoryCodexThreadStore(),
    mcp: {
      command: process.execPath,
      args: ["index.js", "mcp"],
      url: "http://127.0.0.1:17371",
      token: "local-token",
    },
    environment: { PATH: process.env.PATH },
  });

  client.nextEvents = [
    { type: "thread.started", thread_id: "codex-thread-messages" },
    { type: "turn.started" },
    {
      type: "item.completed",
      item: {
        id: "message-1",
        type: "agent_message",
        text: finalDecisionText("先读取画布"),
      },
    },
    {
      type: "item.completed",
      item: {
        id: "message-2",
        type: "agent_message",
        text: finalDecisionText("画布读取完成"),
      },
    },
    { type: "turn.completed", usage: completedUsage },
  ];
  const execution = await service.startTurn({
    canvasId: "canvas-1",
    message: "读取",
    attachments: [],
  });
  const streamed = [];
  for await (const event of execution.events) streamed.push(event);

  const finalDecisions = streamed.filter(
    (event) => event.kind === "final_decision",
  );
  assert.equal(finalDecisions.length, 1);
  assert.equal(
    finalDecisions[0]?.kind === "final_decision"
      ? finalDecisions[0].message
      : "",
    "画布读取完成",
  );
  assert.equal(streamed.at(-1)?.kind, "turn_completed");
});

test("rejects a Codex terminal answer while an MCP tool call is still in progress", async () => {
  const fixture = await createFixture();
  const client = new FakeCodexClient();
  const service = new CodexThreadService({
    model: "gpt-5.6-sol",
    attachmentRoot: fixture.attachmentRoot,
    resolveWorkspace: () => fixture.workspaceRoot,
    createCodexClient: () => client,
    store: new InMemoryCodexThreadStore(),
    mcp: {
      command: process.execPath,
      args: ["index.js", "mcp"],
      url: "http://127.0.0.1:17371",
      token: "local-token",
    },
    environment: { PATH: process.env.PATH },
  });

  client.nextEvents = [
    { type: "thread.started", thread_id: "codex-thread-pending-tool" },
    { type: "turn.started" },
    {
      type: "item.started",
      item: {
        id: "tool-1",
        type: "mcp_tool_call",
        server: "hmaigc_canvas",
        tool: "media_generate",
        arguments: {},
        status: "in_progress",
      },
    },
    {
      type: "item.completed",
      item: {
        id: "message-1",
        type: "agent_message",
        text: finalDecisionText("报价已提交"),
      },
    },
    { type: "turn.completed", usage: completedUsage },
  ];
  const execution = await service.startTurn({
    canvasId: "canvas-1",
    message: "生成图片",
    attachments: [],
  });
  const streamed = [];
  for await (const event of execution.events) streamed.push(event);

  assert.equal(
    streamed.some((event) => event.kind === "final_decision"),
    false,
  );
  assert.equal(streamed.at(-1)?.kind, "turn_failed");
  assert.match(
    streamed.at(-1)?.kind === "turn_failed" ? streamed.at(-1).message : "",
    /MCP 工具仍在执行/,
  );
  const turn = (await service.readThread("canvas-1", execution.threadId))
    .turns[0];
  assert.equal(turn?.status, "failed");
});

test("stops after the first terminal Codex event and records one authoritative failure", async () => {
  const fixture = await createFixture();
  const client = new FakeCodexClient();
  const service = new CodexThreadService({
    model: "gpt-5.6-sol",
    attachmentRoot: fixture.attachmentRoot,
    resolveWorkspace: () => fixture.workspaceRoot,
    createCodexClient: () => client,
    store: new InMemoryCodexThreadStore(),
    mcp: {
      command: process.execPath,
      args: ["index.js", "mcp"],
      url: "http://127.0.0.1:17371",
      token: "local-token",
    },
    environment: { PATH: process.env.PATH },
  });

  client.nextEvents = [
    { type: "thread.started", thread_id: "codex-thread-terminal" },
    { type: "turn.started" },
    { type: "error", message: "schema rejected" },
    { type: "turn.failed", error: { message: "duplicate terminal failure" } },
  ];
  const execution = await service.startTurn({
    canvasId: "canvas-1",
    message: "执行",
    attachments: [],
  });
  const streamed = [];
  for await (const event of execution.events) streamed.push(event);

  assert.deepEqual(
    streamed.map((event) => event.kind),
    ["thread_started", "turn_started", "turn_failed"],
  );
  const turn = (await service.readThread("canvas-1", execution.threadId))
    .turns[0];
  assert.equal(turn?.status, "failed");
  assert.equal(turn?.errorMessage, "schema rejected");
  assert.equal(
    turn?.events.filter((event) => event.kind === "turn_failed").length,
    1,
  );
});

test("resumes only the owning canvas and archive blocks further turns", async () => {
  const fixture = await createFixture();
  const client = new FakeCodexClient();
  const service = new CodexThreadService({
    model: "gpt-5.6-sol",
    attachmentRoot: fixture.attachmentRoot,
    resolveWorkspace: (canvasId) =>
      canvasId === "canvas-1" ? fixture.workspaceRoot : undefined,
    createCodexClient: () => client,
    store: new InMemoryCodexThreadStore(),
    mcp: {
      command: process.execPath,
      args: ["index.js", "mcp"],
      url: "http://127.0.0.1:17371",
      token: "local-token",
    },
    environment: { PATH: process.env.PATH },
  });

  client.nextEvents = [
    { type: "thread.started", thread_id: "codex-thread-owned" },
    { type: "turn.completed", usage: completedUsage },
  ];
  const started = await service.startTurn({
    canvasId: "canvas-1",
    message: "第一轮",
    attachments: [],
  });
  for await (const _event of started.events) {
    /* consume */
  }

  await assert.rejects(
    service.resumeTurn({
      canvasId: "canvas-other",
      threadId: started.threadId,
      message: "越权",
      attachments: [],
    }),
    /不属于当前画布/,
  );

  client.nextEvents = [{ type: "turn.completed", usage: completedUsage }];
  const resumed = await service.resumeTurn({
    canvasId: "canvas-1",
    threadId: started.threadId,
    message: "继续",
    attachments: [],
  });
  for await (const _event of resumed.events) {
    /* consume */
  }
  assert.equal(client.resumed[0]?.id, "codex-thread-owned");

  await service.archiveThread("canvas-1", started.threadId);
  await assert.rejects(
    service.resumeTurn({
      canvasId: "canvas-1",
      threadId: started.threadId,
      message: "归档后续接",
      attachments: [],
    }),
    /已经归档/,
  );
  assert.equal((await service.listThreads("canvas-1")).length, 0);
  assert.equal(
    (await service.listThreads("canvas-1", { includeArchived: true })).length,
    1,
  );
});

test("keeps same-thread delivery repair turns out of persisted user history", async () => {
  const fixture = await createFixture();
  const client = new FakeCodexClient();
  const service = new CodexThreadService({
    model: "gpt-5.6-sol",
    attachmentRoot: fixture.attachmentRoot,
    resolveWorkspace: () => fixture.workspaceRoot,
    createCodexClient: () => client,
    store: new InMemoryCodexThreadStore(),
    mcp: {
      command: process.execPath,
      args: ["index.js", "mcp"],
      url: "http://127.0.0.1:17371",
      token: "local-token",
    },
    environment: { PATH: process.env.PATH },
  });

  client.nextEvents = [
    { type: "thread.started", thread_id: "codex-thread-repair" },
    {
      type: "item.completed",
      item: {
        id: "message-1",
        type: "agent_message",
        text: finalDecisionText("第一轮完成"),
      },
    },
    { type: "turn.completed", usage: completedUsage },
  ];
  const started = await service.startTurn({
    canvasId: "canvas-1",
    message: "生成视频",
    attachments: [],
  });
  for await (const _event of started.events) {
    /* consume */
  }

  client.nextEvents = [
    {
      type: "item.completed",
      item: {
        id: "message-2",
        type: "agent_message",
        text: finalDecisionText("纠偏完成"),
      },
    },
    { type: "turn.completed", usage: completedUsage },
  ];
  const repair = await service.resumeTurn({
    canvasId: "canvas-1",
    threadId: started.threadId,
    message: "内部交付校验要求继续修复",
    attachments: [],
    ephemeral: true,
  });
  for await (const _event of repair.events) {
    /* consume */
  }

  const stored = await service.readThread("canvas-1", started.threadId);
  assert.equal(stored.turns.length, 1);
  assert.equal(stored.turns[0]?.message, "生成视频");
  assert.equal(client.resumed[0]?.id, "codex-thread-repair");
});

test("releases a completed turn before publishing its terminal event", async () => {
  const fixture = await createFixture();
  const client = new FakeCodexClient();
  const service = new CodexThreadService({
    model: "gpt-5.6-sol",
    attachmentRoot: fixture.attachmentRoot,
    resolveWorkspace: () => fixture.workspaceRoot,
    createCodexClient: () => client,
    store: new InMemoryCodexThreadStore(),
    mcp: {
      command: process.execPath,
      args: ["index.js", "mcp"],
      url: "http://127.0.0.1:17371",
      token: "local-token",
    },
    environment: { PATH: process.env.PATH },
  });

  client.nextEvents = [
    { type: "thread.started", thread_id: "codex-thread-terminal-release" },
    {
      type: "item.completed",
      item: {
        id: "message-1",
        type: "agent_message",
        text: finalDecisionText("第一轮完成"),
      },
    },
    { type: "turn.completed", usage: completedUsage },
  ];
  const started = await service.startTurn({
    canvasId: "canvas-1",
    message: "第一轮",
    attachments: [],
  });
  const iterator = started.events[Symbol.asyncIterator]();
  assert.equal((await iterator.next()).value?.kind, "thread_started");
  assert.equal((await iterator.next()).value?.kind, "item_completed");
  assert.equal((await iterator.next()).value?.kind, "final_decision");
  assert.equal((await iterator.next()).value?.kind, "turn_completed");

  client.nextEvents = [
    {
      type: "item.completed",
      item: {
        id: "message-2",
        type: "agent_message",
        text: finalDecisionText("第二轮完成"),
      },
    },
    { type: "turn.completed", usage: completedUsage },
  ];
  const resumed = await service.resumeTurn({
    canvasId: "canvas-1",
    threadId: started.threadId,
    message: "第二轮",
    attachments: [],
  });
  for await (const _event of resumed.events) {
    /* consume */
  }
  await iterator.return?.();
});

test("maps bounded local attachments and cancels the active turn", async () => {
  const fixture = await createFixture();
  const imagePath = join(fixture.attachmentRoot, "frame.png");
  const filePath = join(fixture.attachmentRoot, "script.txt");
  await writeFile(imagePath, "image");
  await writeFile(filePath, "script");
  const client = new FakeCodexClient();
  const service = new CodexThreadService({
    model: "gpt-5.6-sol",
    attachmentRoot: fixture.attachmentRoot,
    resolveWorkspace: () => fixture.workspaceRoot,
    createCodexClient: () => client,
    store: new InMemoryCodexThreadStore(),
    mcp: {
      command: process.execPath,
      args: ["index.js", "mcp"],
      url: "http://127.0.0.1:17371",
      token: "local-token",
    },
    environment: { PATH: process.env.PATH },
  });

  client.nextWaitForAbort = true;
  const execution = await service.startTurn({
    canvasId: "canvas-1",
    message: "分析附件",
    attachments: [
      {
        kind: "image",
        name: "frame.png",
        mimeType: "image/png",
        localPath: imagePath,
      },
      {
        kind: "file",
        name: "script.txt",
        mimeType: "text/plain",
        localPath: filePath,
      },
    ],
  });
  const fakeThread = client.threads[0];
  assert.ok(fakeThread);
  assert.deepEqual(fakeThread.inputs[0], [
    {
      type: "text",
      text: `分析附件\n\n本轮本机附件：\n- script.txt: ${filePath}`,
    },
    { type: "local_image", path: imagePath },
  ]);
  await assert.rejects(
    service.archiveThread("canvas-1", execution.threadId),
    /活动 turn/,
  );
  assert.equal(service.cancelTurn(execution.turnId), true);
  const events = [];
  for await (const event of execution.events) events.push(event);
  assert.equal(events.at(-1)?.kind, "turn_cancelled");

  await assert.rejects(
    service.startTurn({
      canvasId: "canvas-1",
      message: "非法附件",
      attachments: [
        {
          kind: "file",
          name: "outside.txt",
          mimeType: "text/plain",
          localPath: join(fixture.root, "outside.txt"),
        },
      ],
    }),
    /附件路径不属于受管目录/,
  );
});

test("fails explicitly without a model and sanitizes the Codex environment", async () => {
  const fixture = await createFixture();
  assert.throws(
    () =>
      new CodexThreadService({
        model: "",
        attachmentRoot: fixture.attachmentRoot,
        resolveWorkspace: () => fixture.workspaceRoot,
        createCodexClient: () => new FakeCodexClient(),
        store: new InMemoryCodexThreadStore(),
        mcp: {
          command: process.execPath,
          args: ["index.js", "mcp"],
          url: "http://127.0.0.1:17371",
          token: "local-token",
        },
        environment: {},
      }),
    /Codex 模型配置缺失/,
  );

  const sanitized = sanitizeCodexEnvironment({
    PATH: "bin",
    CODEX_HOME: join(fixture.root, ".codex"),
    OPENAI_API_KEY: "codex-auth",
    HMAIGC_COOKIE: "cookie",
    HMAIGC_CHANNEL_KEY: "channel",
    OSS_ACCESS_KEY_SECRET: "oss",
    AUTHORIZATION: "bearer",
  });
  assert.deepEqual(sanitized, {
    PATH: "bin",
    CODEX_HOME: join(fixture.root, ".codex"),
    OPENAI_API_KEY: "codex-auth",
  });
});

test("persists thread history for process restart", async () => {
  const fixture = await createFixture();
  const catalogPath = join(fixture.root, "state", "threads.json");
  const firstStore = new FileCodexThreadStore(catalogPath);
  await firstStore.create({
    threadId: "local-thread-1",
    sdkThreadId: "sdk-thread-1",
    canvasId: "canvas-1",
    workspaceRoot: fixture.workspaceRoot,
    model: "gpt-5.6-sol",
    createdAt: "2026-09-01T00:00:00.000Z",
    updatedAt: "2026-09-01T00:00:00.000Z",
    turns: [
      {
        turnId: "turn-1",
        status: "completed",
        message: "读取画布",
        attachments: [],
        events: [
          {
            kind: "turn_completed",
            threadId: "local-thread-1",
            turnId: "turn-1",
            event: { type: "turn.completed", usage: completedUsage },
          },
        ],
        createdAt: "2026-09-01T00:00:00.000Z",
        completedAt: "2026-09-01T00:00:01.000Z",
      },
    ],
  });

  const reloaded = await new FileCodexThreadStore(catalogPath).get(
    "local-thread-1",
  );
  assert.equal(reloaded?.sdkThreadId, "sdk-thread-1");
  assert.equal(reloaded?.turns[0]?.events[0]?.kind, "turn_completed");
});

test("marks turns left running by a previous process as explicitly interrupted", async () => {
  const fixture = await createFixture();
  const store = new InMemoryCodexThreadStore();
  await store.create({
    threadId: "local-thread-interrupted",
    sdkThreadId: "sdk-thread-interrupted",
    canvasId: "canvas-1",
    workspaceRoot: fixture.workspaceRoot,
    model: "gpt-5.6-sol",
    createdAt: "2026-09-01T00:00:00.000Z",
    updatedAt: "2026-09-01T00:00:00.000Z",
    turns: [
      {
        turnId: "turn-interrupted",
        status: "running",
        message: "未完成",
        attachments: [],
        events: [],
        createdAt: "2026-09-01T00:00:00.000Z",
      },
    ],
  });
  const service = new CodexThreadService({
    model: "gpt-5.6-sol",
    attachmentRoot: fixture.attachmentRoot,
    resolveWorkspace: () => fixture.workspaceRoot,
    createCodexClient: () => new FakeCodexClient(),
    store,
    mcp: {
      command: process.execPath,
      args: ["index.js", "mcp"],
      url: "http://127.0.0.1:17371",
      token: "local-token",
    },
    environment: { PATH: process.env.PATH },
    now: () => new Date("2026-09-01T00:10:00.000Z"),
  });

  assert.equal(await service.recoverInterruptedTurns(), 1);
  const recovered = await store.get("local-thread-interrupted");
  assert.equal(recovered?.turns[0]?.status, "failed");
  assert.equal(
    recovered?.turns[0]?.errorMessage,
    "本机 Agent 进程在 turn 完成前中断",
  );
});
