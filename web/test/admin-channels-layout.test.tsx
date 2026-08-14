import "./setup-happy-dom";

import { afterEach, beforeEach, describe, expect, mock, test } from "bun:test";
import { App, ConfigProvider } from "antd";
import { act, createElement } from "react";
import { createRoot, type Root } from "react-dom/client";
import { createMemoryRouter, RouterProvider } from "react-router";

import type { AdminListParams } from "../src/services/api/auth";
import type { ModelChannel } from "../src/stores/use-config-store";

const channelFixture: ModelChannel = {
    id: "channel-kuaizi",
    name: "筷子科技",
    baseUrl: "https://aiopenapi.kuaizi.cn",
    apiKey: "",
    apiFormat: "openai",
    interfaceType: "ai-open-platform-video-volcengine",
    models: ["seedance-2.5"],
    scope: "system",
    enabled: true,
    hasApiKey: true,
    concurrencyLimit: 4,
    modelCosts: [],
    voices: [],
};

type ChannelListResult = { channels: ModelChannel[]; total: number; page: number; limit: number };
let channelRequest: (params?: AdminListParams) => Promise<ChannelListResult> = async () => ({ channels: [channelFixture], total: 1, page: 1, limit: 20 });

const authModule = await import("../src/services/api/auth");
mock.module("@/services/api/auth", () => ({
    ...authModule,
    getAdminReferences: async () => ({ users: [], channels: [] }),
    listAdminChannels: (params?: AdminListParams) => channelRequest(params),
}));

const [{ default: ChannelsPage }, { AdminProvider }] = await Promise.all([import("../src/pages/admin/channels/channels-page"), import("../src/pages/admin/admin-context")]);

let root: Root | null = null;

beforeEach(() => {
    channelRequest = async () => ({ channels: [channelFixture], total: 1, page: 1, limit: 20 });
});

afterEach(async () => {
    if (root) await act(async () => root?.unmount());
    root = null;
    document.body.replaceChildren();
});

describe("admin channels layout", () => {
    test("orders the real channel summary, filters and directory in one data layout", async () => {
        await renderChannelsPage();

        const dataLayout = document.querySelector(".admin-data-layout");
        expect(Array.from(dataLayout?.children ?? [], (node) => node.className)).toEqual(["admin-metric-band", "admin-filter-section", "admin-content-section"]);
        expect(dataLayout?.querySelector(".admin-metric-band-list")?.textContent).toContain("渠道目录1 个渠道");
        expect(dataLayout?.querySelector(".admin-content-section-title")?.textContent).toBe("渠道目录");
        expect(dataLayout?.querySelector(".admin-data-section-actions")?.textContent).toContain("共 1 个渠道");
        expect(dataLayout?.querySelector(".admin-channel-table")?.textContent).toContain("筷子科技");
    });

    test("keeps a failed first load explicit and retryable without fabricated channel metrics", async () => {
        channelRequest = async () => {
            throw new Error("渠道服务暂时不可用");
        };

        await renderChannelsPage();

        const error = document.querySelector(".admin-content-error");
        expect(error?.textContent).toContain("系统渠道读取失败");
        expect(error?.textContent).toContain("渠道服务暂时不可用");
        expect(error?.querySelector(".admin-content-error-retry")).not.toBeNull();
        expect(document.querySelector(".admin-metric-band")).toBeNull();
        expect(document.querySelector(".admin-channel-table")).toBeNull();
    });

    test("exposes one owned table surface and one filter region for responsive styling", async () => {
        await renderChannelsPage();

        const content = document.querySelector(".admin-channel-content");
        const tableSurface = document.querySelector(".admin-content-section-body > .admin-channel-table-surface");
        const filter = document.querySelector('[role="region"][aria-label="渠道筛选条件"]');

        expect(content).not.toBeNull();
        expect(tableSurface).not.toBeNull();
        expect(tableSurface?.querySelector(".admin-channel-table")).not.toBeNull();
        expect(filter?.querySelector(".admin-channel-list-toolbar")).not.toBeNull();
        expect(filter?.querySelectorAll(".app-list-search, .ant-select").length).toBe(3);
    });
});

async function renderChannelsPage() {
    const host = document.createElement("div");
    host.className = "admin-workspace";
    document.body.append(host);
    root = createRoot(host);
    const router = createMemoryRouter([{ path: "/", element: createElement(ChannelsPage) }], { initialEntries: ["/"] });

    await act(async () => {
        root?.render(createElement(ConfigProvider, null, createElement(App, null, createElement(AdminProvider, null, createElement(RouterProvider, { router })))));
    });
    await act(async () => {
        await Promise.resolve();
        await new Promise((resolve) => setTimeout(resolve, 0));
    });
}
