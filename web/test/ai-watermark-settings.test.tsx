import "./setup-happy-dom";

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { App } from "antd";
import { afterEach, beforeAll, expect, test } from "bun:test";
import { act, createElement } from "react";
import type { Root } from "react-dom/client";

import { WatermarkPolicyConflictError, type WatermarkPolicyApi, type WatermarkPreferenceView } from "../src/services/api/watermark-policy";

const policy = {
    id: "publication-1",
    version: 1,
    managementRuleRichText: "<p>开启后，平台生成内容不再添加 AI 水印。</p>",
    watermarkPolicyUrl: "https://example.com/watermark-policy",
    contentHash: "hash-1",
    publishedBy: "admin",
    publishedAt: "2026-08-13T00:00:00Z",
};

let Modal: typeof import("../src/components/account/ai-watermark-settings-modal").AIWatermarkSettingsModal;
let createRoot: (container: Element | DocumentFragment) => Root;
let root: Root | null = null;

beforeAll(async () => {
    ({ createRoot } = await import("react-dom/client"));
    ({ AIWatermarkSettingsModal: Modal } = await import("../src/components/account/ai-watermark-settings-modal"));
});

afterEach(async () => {
    if (root) await act(async () => root?.unmount());
    root = null;
    document.body.replaceChildren();
});

test("账号级弹窗阅读当前规范并保存当前发布版本", async () => {
    const puts: Array<{ removeWatermark: boolean; publicationId: string }> = [];
    const view: WatermarkPreferenceView = { removeWatermark: false, status: "disabled", canEnable: true, acceptedAt: null, currentPolicy: policy };
    const api: WatermarkPolicyApi = {
        getPreference: async () => view,
        updatePreference: async (input) => {
            puts.push(input);
            return { ...view, removeWatermark: true, status: "active", acceptedAt: "2026-08-13T01:00:00Z" };
        },
        getAdminPolicy: async () => policy,
        publishPolicy: async () => policy,
    };
    await mount(api);
    const dialog = document.querySelector('[role="dialog"]');
    expect(dialog?.textContent).toContain("AI 生成内容水印管理规则");
    expect(dialog?.getAttribute("aria-labelledby")).not.toBeNull();
    const link = document.querySelector('a[href="https://example.com/watermark-policy"]');
    expect(link?.getAttribute("rel")).toContain("noopener");
    const toggle = document.querySelector('[role="switch"]');
    if (!(toggle instanceof HTMLElement)) throw new Error("未找到去 AI 水印开关");
    await act(async () => toggle.click());
    await act(async () => button("保存设置").click());
    expect(puts).toEqual([{ removeWatermark: true, publicationId: "publication-1" }]);
});

test("保存冲突后采用新规范但保留用户开启草稿", async () => {
    const nextPolicy = { ...policy, id: "publication-2", version: 2, contentHash: "hash-2" };
    let reads = 0;
    const api: WatermarkPolicyApi = {
        getPreference: async () => {
            reads += 1;
            return { removeWatermark: false, status: reads === 1 ? "disabled" : "policy_updated", canEnable: true, acceptedAt: null, currentPolicy: reads === 1 ? policy : nextPolicy };
        },
        updatePreference: async () => {
            throw new WatermarkPolicyConflictError("水印规范已更新，请重新阅读后确认");
        },
        getAdminPolicy: async () => policy,
        publishPolicy: async () => policy,
    };
    await mount(api);
    const toggle = document.querySelector('[role="switch"]');
    if (!(toggle instanceof HTMLElement)) throw new Error("未找到去 AI 水印开关");
    await act(async () => toggle.click());
    await act(async () => button("保存设置").click());
    await settle();
    expect(document.body.textContent).toContain("水印规范已更新，请重新阅读并确认");
    expect(toggle.getAttribute("aria-checked")).toBe("true");
    expect(document.querySelector('a[href="https://example.com/watermark-policy"]')).not.toBeNull();
});

async function mount(api: WatermarkPolicyApi) {
    const host = document.createElement("div");
    document.body.append(host);
    root = createRoot(host);
    const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
    const modal = createElement(Modal, { open: true, onClose: () => undefined, api });
    const tree = createElement(QueryClientProvider, { client }, createElement(App, null, modal));
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
