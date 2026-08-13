import "./setup-happy-dom";

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { App } from "antd";
import { afterEach, beforeAll, expect, test } from "bun:test";
import { act, createElement } from "react";
import type { Root } from "react-dom/client";

import type { WatermarkPolicyApi, WatermarkPolicyPublication } from "../src/services/api/watermark-policy";

let Editor: typeof import("../src/pages/admin/settings/ai-watermark-policy-editor").AIWatermarkPolicyEditor;
let createRoot: (container: Element | DocumentFragment) => Root;
let root: Root | null = null;

beforeAll(async () => {
    ({ createRoot } = await import("react-dom/client"));
    ({ AIWatermarkPolicyEditor: Editor } = await import("../src/pages/admin/settings/ai-watermark-policy-editor"));
});

afterEach(async () => {
    if (root) await act(async () => root?.unmount());
    root = null;
    document.body.replaceChildren();
});

test("相同正文和外链重复发布必须二次确认", async () => {
    const current: WatermarkPolicyPublication = {
        id: "publication-1",
        version: 1,
        managementRuleRichText: "<p>管理规则</p>",
        watermarkPolicyUrl: "https://example.com/policy",
        contentHash: "hash-1",
        publishedBy: "admin",
        publishedAt: "2026-08-13T00:00:00Z",
    };
    const publications: Array<{ managementRuleRichText: string; watermarkPolicyUrl: string }> = [];
    const api: WatermarkPolicyApi = {
        getPreference: async () => {
            throw new Error("not used");
        },
        updatePreference: async () => {
            throw new Error("not used");
        },
        getAdminPolicy: async () => current,
        publishPolicy: async (input) => {
            publications.push(input);
            return { ...current, id: "publication-2", version: 2 };
        },
    };
    await mount(api);
    await act(async () => button("发布新版本").click());
    expect(document.body.textContent).toContain("继续发布会要求所有已开启账号重新确认");
    await act(async () => button("确认发布新版本").click());
    expect(publications).toEqual([{ managementRuleRichText: "<p>管理规则</p>", watermarkPolicyUrl: "https://example.com/policy" }]);
});

async function mount(api: WatermarkPolicyApi) {
    const host = document.createElement("div");
    document.body.append(host);
    root = createRoot(host);
    const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
    const editor = createElement(Editor, { api });
    const tree = createElement(QueryClientProvider, { client }, createElement(App, null, editor));
    await act(async () => root?.render(tree));
    await settle();
}

function button(label: string) {
    const match = [...document.querySelectorAll("button")].find((item) => item.textContent?.replace(/\s+/g, "").includes(label.replace(/\s+/g, "")));
    if (!(match instanceof HTMLButtonElement)) throw new Error(`未找到按钮：${label}`);
    return match;
}

async function settle() {
    await act(async () => {
        await Promise.resolve();
        await new Promise((resolve) => setTimeout(resolve, 0));
    });
}
