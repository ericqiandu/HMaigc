import assert from "node:assert/strict";
import { mkdtemp, readFile, rm, stat } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { test } from "node:test";

import {
  assertProtectedRequestSecurity,
  generateLocalAgentToken,
  parseLocalAgentConfig,
  writeLocalAgentConfig,
} from "../src/config";

const token = "ab".repeat(32);
const validConfig = {
  url: "http://127.0.0.1:17371",
  token,
  allowedOrigins: ["http://127.0.0.1:3000", "https://hm.kunagent.com"],
  allowedAttachmentOrigins: ["https://static.hm.kunagent.com"],
  codex: { model: "gpt-5.6-sol", modelReasoningEffort: "high" },
  canvases: {
    "canvas-1": { workspaceRoot: "E:/workspaces/canvas-1" },
  },
} as const;

test("本机 Agent 配置只允许 loopback、高熵 token 和精确 Origin", () => {
  assert.deepEqual(parseLocalAgentConfig(validConfig), validConfig);
  assert.throws(
    () =>
      parseLocalAgentConfig({ ...validConfig, url: "http://0.0.0.0:17371" }),
    /127\.0\.0\.1/,
  );
  assert.throws(
    () => parseLocalAgentConfig({ ...validConfig, token: "short-token" }),
    /256/,
  );
  assert.throws(
    () => parseLocalAgentConfig({ ...validConfig, token: "A".repeat(45) }),
    /编码/,
  );
  assert.throws(
    () => parseLocalAgentConfig({ ...validConfig, allowedOrigins: ["*"] }),
    /Origin/,
  );
  assert.throws(
    () => parseLocalAgentConfig({ ...validConfig, allowedOrigins: ["null"] }),
    /Origin/,
  );
  assert.throws(
    () =>
      parseLocalAgentConfig({
        ...validConfig,
        allowedAttachmentOrigins: ["http://127.0.0.1:9000"],
      }),
    /Origin|附件/,
  );
  assert.throws(
    () => parseLocalAgentConfig({ ...validConfig, codex: { model: "" } }),
    /model/i,
  );
  assert.equal(
    Buffer.from(generateLocalAgentToken(), "base64url").byteLength,
    32,
  );
});

test("受保护请求必须同时匹配 header token 和精确 Origin", () => {
  const config = parseLocalAgentConfig(validConfig);
  assert.doesNotThrow(() =>
    assertProtectedRequestSecurity(config, {
      origin: "http://127.0.0.1:3000",
      tokenHeader: token,
    }),
  );
  assert.throws(
    () =>
      assertProtectedRequestSecurity(config, {
        origin: undefined,
        tokenHeader: token,
      }),
    /Origin/,
  );
  assert.throws(
    () =>
      assertProtectedRequestSecurity(config, {
        origin: "null",
        tokenHeader: token,
      }),
    /Origin/,
  );
  assert.throws(
    () =>
      assertProtectedRequestSecurity(config, {
        origin: "http://127.0.0.1:3000.evil.test",
        tokenHeader: token,
      }),
    /Origin/,
  );
  assert.throws(
    () =>
      assertProtectedRequestSecurity(config, {
        origin: "http://127.0.0.1:3000",
        tokenHeader: "wrong",
      }),
    /token/,
  );
  assert.throws(
    () =>
      assertProtectedRequestSecurity(config, {
        origin: "http://127.0.0.1:3000",
        tokenHeader: token,
        queryToken: token,
      }),
    /query/,
  );
});

test("配置文件以 0600 写入且内容可严格重读", async () => {
  const directory = await mkdtemp(join(tmpdir(), "hmaigc-canvas-agent-"));
  const path = join(directory, "config.json");
  try {
    await writeLocalAgentConfig(path, parseLocalAgentConfig(validConfig));
    assert.deepEqual(
      parseLocalAgentConfig(JSON.parse(await readFile(path, "utf8"))),
      validConfig,
    );
    if (process.platform !== "win32") {
      assert.equal((await stat(path)).mode & 0o777, 0o600);
    }
  } finally {
    await rm(directory, { recursive: true, force: true });
  }
});
