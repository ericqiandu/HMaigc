import "./setup-happy-dom";

import { afterEach, beforeAll, expect, test } from "bun:test";
import { act, createElement } from "react";
import type { Root } from "react-dom/client";

let CanvasSubmitButton: typeof import("../src/components/canvas/canvas-submit-button").CanvasSubmitButton;
let createRoot: (container: Element | DocumentFragment) => Root;
let root: Root | null = null;

beforeAll(async () => {
    ({ createRoot } = await import("react-dom/client"));
    ({ CanvasSubmitButton } = await import("../src/components/canvas/canvas-submit-button"));
});

afterEach(async () => {
    if (root) await act(async () => root?.unmount());
    root = null;
    document.body.replaceChildren();
});

test("共享提交按钮以同一外壳表达提交、等待和停止状态", async () => {
    await mount([
        { state: "ready", ariaLabel: "发送", disabled: false },
        { state: "loading", ariaLabel: "正在发送", disabled: true },
        { state: "stop", ariaLabel: "停止生成", disabled: false },
    ]);

    const [ready, loading, stop] = buttons();
    expect(ready.classList.contains("canvas-submit-button")).toBe(true);
    expect(ready.getAttribute("aria-label")).toBe("发送");
    expect(ready.disabled).toBe(false);
    expect(ready.querySelector(".lucide-arrow-up")).not.toBeNull();
    expect(loading.getAttribute("aria-label")).toBe("正在发送");
    expect(loading.disabled).toBe(true);
    expect(loading.querySelector(".lucide-loader-circle")).not.toBeNull();
    expect(stop.getAttribute("aria-label")).toBe("停止生成");
    expect(stop.disabled).toBe(false);
    expect(stop.querySelector(".lucide-square")).not.toBeNull();
});

test("共享提交按钮只把启用状态的点击交给调用方", async () => {
    const calls: string[] = [];
    await mount([
        { state: "ready", ariaLabel: "发送", disabled: false, onClick: () => calls.push("ready") },
        { state: "loading", ariaLabel: "正在发送", disabled: true, onClick: () => calls.push("loading") },
    ]);

    const [ready, loading] = buttons();
    await act(async () => ready.click());
    await act(async () => loading.click());
    expect(calls).toEqual(["ready"]);
});

type ButtonFixture = {
    state: "ready" | "loading" | "stop";
    ariaLabel: string;
    disabled: boolean;
    onClick?: () => void;
};

async function mount(fixtures: ButtonFixture[]) {
    const host = document.createElement("div");
    document.body.append(host);
    root = createRoot(host);
    await act(async () => {
        root?.render(
            createElement(
                "div",
                { className: "canvas-submit-button-test-host" },
                ...fixtures.map((fixture) =>
                    createElement(CanvasSubmitButton, {
                        key: fixture.ariaLabel,
                        state: fixture.state,
                        disabled: fixture.disabled,
                        ariaLabel: fixture.ariaLabel,
                        onClick: fixture.onClick || (() => undefined),
                    }),
                ),
            ),
        );
    });
}

function buttons() {
    const values = [...document.querySelectorAll("button")];
    if (!values.length) throw new Error("未渲染共享提交按钮");
    return values;
}
