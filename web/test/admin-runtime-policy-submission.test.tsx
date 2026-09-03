import "./setup-happy-dom";

import { afterEach, describe, expect, mock, test } from "bun:test";
import { App, ConfigProvider } from "antd";
import { act, createElement } from "react";
import { createRoot, type Root } from "react-dom/client";
import { createMemoryRouter, RouterProvider } from "react-router";

import type { RuntimePolicySetting } from "../src/services/api/auth";

const runtimePolicySetting: RuntimePolicySetting = {
    resource: {
        resourceUploadMB: 50,
        sessionUploadMB: 32,
        generatedFileMB: 64,
        dailyUploadMB: 200,
        storedFileGB: 2,
        structuredDataMB: 256,
        taskDataGB: 1,
        assetCount: 2_000,
        canvasCount: 100,
        sessionCount: 100,
        taskCount: 1_000,
        apiCallLogCount: 2_000,
    },
    task: {
        workerConcurrency: 4,
        channelConcurrency: 8,
        activeTaskLimit: 6,
        imageTimeoutMinutes: 10,
        textTimeoutMinutes: 5,
        audioTimeoutMinutes: 10,
        videoTimeoutMinutes: 30,
        storyboardTimeoutMinutes: 20,
        defaultTimeoutMinutes: 15,
    },
    request: {
        taskCreatePerMinute: 20,
        sessionCreatePerMinute: 10,
        resourceUploadPerMinute: 30,
        resourceImportPerMinute: 30,
        sessionFilePerMinute: 20,
        assetWritePerMinute: 60,
        canvasWritePerMinute: 120,
        registerPerHour: 10,
        emailCodePerHour: 10,
        loginIPPerTenMinutes: 20,
        loginAccountPerTenMinutes: 10,
        systemRelayPerMinute: 60,
        customRelayPerMinute: 45,
        customRelayConcurrency: 7,
        customRelayRequestMB: 48,
        customRelayResponseMB: 96,
        customRelayTimeoutMinutes: 25,
        systemRelayRequestMB: 32,
        systemRelayResponseMB: 64,
        channelCircuitFailureCount: 5,
        channelCircuitOpenSeconds: 60,
    },
    configured: true,
    updatedBy: "admin-user",
    updatedAt: "2026-09-03T08:00:00Z",
};

let submittedPolicy: Pick<RuntimePolicySetting, "resource" | "task" | "request"> | null = null;

const authModule = await import("../src/services/api/auth");
mock.module("@/services/api/auth", () => ({
    ...authModule,
    getAdminReferences: async () => ({ users: [{ id: "admin-user", username: "admin", displayName: "管理员" }], channels: [] }),
    getAdminRuntimePolicySetting: async () => ({ setting: runtimePolicySetting }),
    getAdminSelfUseRuntimePolicy: async () => ({ setting: runtimePolicySetting }),
    resetAdminRuntimePolicySetting: async () => ({ setting: runtimePolicySetting }),
    updateAdminRuntimePolicySetting: async (input: Pick<RuntimePolicySetting, "resource" | "task" | "request">) => {
        submittedPolicy = input;
        return { setting: { ...runtimePolicySetting, ...input } };
    },
}));

const [{ default: RuntimePolicySettingsPage }, { AdminProvider }] = await Promise.all([import("../src/pages/admin/settings/runtime-policy-settings-page"), import("../src/pages/admin/admin-context")]);

let root: Root | null = null;

afterEach(async () => {
    if (root) await act(async () => root?.unmount());
    root = null;
    submittedPolicy = null;
    document.body.replaceChildren();
});

describe("admin runtime policy submission", () => {
    test("保存可见策略时携带服务端要求的全部自定义渠道字段", async () => {
        await renderRuntimePolicyPage();

        for (const label of ["自定义渠道中转", "自定义渠道并发", "自定义渠道请求体", "自定义渠道响应体", "自定义渠道超时"]) {
            expect(document.querySelector(`input[aria-label="${label}"]`)).not.toBeNull();
        }

        const field = requiredElement<HTMLInputElement>('input[aria-label="普通资源单文件"]');
        await setInputValue(field, "51");
        await clickButton("保存配置");

        expect(submittedPolicy?.request).toEqual(runtimePolicySetting.request);
    });
});

async function renderRuntimePolicyPage() {
    const host = document.createElement("div");
    host.className = "admin-workspace";
    document.body.append(host);
    root = createRoot(host);
    const router = createMemoryRouter([{ path: "/", element: createElement(RuntimePolicySettingsPage) }], { initialEntries: ["/"] });
    await act(async () => {
        root?.render(createElement(ConfigProvider, null, createElement(App, null, createElement(AdminProvider, null, createElement(RouterProvider, { router })))));
    });
    await flushUpdates();
}

async function setInputValue(input: HTMLInputElement, value: string) {
    await act(async () => {
        const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")?.set;
        setter?.call(input, value);
        input.dispatchEvent(new Event("input", { bubbles: true }));
        input.dispatchEvent(new Event("change", { bubbles: true }));
    });
    await flushUpdates();
}

async function clickButton(label: string) {
    const button = Array.from(document.querySelectorAll<HTMLButtonElement>("button")).find((candidate) => candidate.textContent?.replaceAll(" ", "").includes(label));
    expect(button).not.toBeUndefined();
    await act(async () => button?.click());
    await flushUpdates();
}

function requiredElement<T extends Element>(selector: string): T {
    const element = document.querySelector<T>(selector);
    if (!element) throw new Error(`找不到元素：${selector}`);
    return element;
}

async function flushUpdates() {
    await act(async () => {
        await Promise.resolve();
        await new Promise((resolve) => setTimeout(resolve, 0));
    });
}
