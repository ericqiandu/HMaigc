import "../../../test/setup-happy-dom";

import assert from "node:assert/strict";
import { afterEach, before, test } from "node:test";
import { act, createElement } from "react";
import type { Root } from "react-dom/client";

import { CanvasAgentHostSwitch } from "./canvas-agent-host-switch";

let createRoot: (container: Element | DocumentFragment) => Root;
let root: Root | null = null;

before(async () => {
    ({ createRoot } = await import("react-dom/client"));
});

afterEach(async () => {
    if (root) await act(async () => root?.unmount());
    root = null;
    document.body.replaceChildren();
});

test("默认展示网站模式且仅在用户点击后切换本机模式", async () => {
    const changes: string[] = [];
    const host = document.createElement("div");
    document.body.append(host);
    root = createRoot(host);
    await act(async () => root?.render(createElement(CanvasAgentHostSwitch, { value: "managed", onChange: (value) => changes.push(value) })));

    const managed = host.querySelector<HTMLButtonElement>('[aria-label="使用网站 Agent"]');
    const local = host.querySelector<HTMLButtonElement>('[aria-label="使用本机 Codex"]');
    assert.equal(managed?.getAttribute("aria-pressed"), "true");
    assert.equal(local?.getAttribute("aria-pressed"), "false");
    assert.deepEqual(changes, []);

    await act(async () => local?.click());
    assert.deepEqual(changes, ["local_codex"]);
});
