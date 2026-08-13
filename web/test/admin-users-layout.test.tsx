import "./setup-happy-dom";

import { afterEach, beforeEach, describe, expect, test } from "bun:test";
import { App, ConfigProvider } from "antd";
import { act, createElement } from "react";
import { createRoot, type Root } from "react-dom/client";
import { MemoryRouter } from "react-router";

import UsersPanel, { type UsersPanelApi } from "../src/pages/admin/users/users-panel";
import type { AdminUser, LocalUser } from "../src/services/api/auth";
import { useUserStore } from "../src/stores/use-user-store";

const actor: LocalUser = {
    id: "admin-1",
    username: "admin",
    displayName: "管理员",
    role: "admin",
    status: "active",
    createdAt: "2026-08-01T00:00:00Z",
    updatedAt: "2026-08-01T00:00:00Z",
};

const member: AdminUser = {
    id: "user-1",
    username: "member",
    email: "member@example.com",
    displayName: "测试用户",
    role: "user",
    status: "active",
    createdAt: "2026-08-02T00:00:00Z",
    updatedAt: "2026-08-02T00:00:00Z",
    availableMicrocredits: 1_000_000,
    reservedMicrocredits: 0,
};

let root: Root | null = null;
let listUsers: UsersPanelApi["listUsers"];

const api: UsersPanelApi = {
    listUsers: (params) => listUsers(params),
    bulkDisableUsers: async () => ({ users: [], disabledCount: 0 }),
    disableUser: async () => ({ ok: true }),
    updateUser: async (_id, input) => ({ user: { ...member, ...input } }),
};

beforeEach(() => {
    useUserStore.setState({ user: actor, hydrated: true });
    listUsers = async () => ({ users: [member], total: 21, page: 1, limit: 20 });
});

afterEach(async () => {
    if (root) await act(async () => root?.unmount());
    root = null;
    document.body.replaceChildren();
    window.localStorage.clear();
});

describe("admin users layout", () => {
    test("orders filters before one titled user-list content section", async () => {
        await renderUsers();

        const dataLayout = document.querySelector(".admin-data-layout");
        expect(Array.from(dataLayout?.children ?? [], (node) => node.className)).toEqual(["admin-filter-section", "admin-content-section"]);
        expect(dataLayout?.querySelector('[role="region"][aria-label="用户筛选"]')).not.toBeNull();
        expect(dataLayout?.querySelector(".admin-content-section-title")?.textContent).toBe("用户列表");
        expect(dataLayout?.querySelector(".admin-data-section-actions")?.textContent).toContain("共 21 位用户");
        expect(dataLayout?.querySelector(".admin-data-section-actions")?.textContent).toContain("列设置");
        expect(dataLayout?.querySelector(".admin-users-selection-region")).not.toBeNull();
        expect(dataLayout?.querySelector(".app-data-table")?.textContent).toContain("测试用户");
        expect(dataLayout?.querySelector(".app-pagination-bar")).not.toBeNull();
    });

    test("uses title actions and one-column mobile filters without the retired toolbar trailing layout", async () => {
        const workspaceStyles = await Bun.file(new URL("../src/pages/admin/admin-workspace.css", import.meta.url)).text();
        const responsiveStyles = await Bun.file(new URL("../src/pages/admin/admin-responsive.css", import.meta.url)).text();
        const allStyles = `${workspaceStyles}\n${responsiveStyles}`;

        expect(workspaceStyles).toContain(".admin-users-content-actions");
        expect(workspaceStyles).toContain(".admin-content-section-body > .admin-users-table-surface");
        expect(workspaceStyles).toContain("border-radius: 0");
        expect(responsiveStyles).toContain(".admin-users-content-actions");
        expect(responsiveStyles).toContain(".admin-users-list-toolbar .workspace-list-toolbar-fields");
        expect(responsiveStyles).toContain("grid-template-columns: minmax(0, 1fr)");
        expect(allStyles).not.toContain("admin-users-toolbar-trailing");
        expect(allStyles).not.toContain("admin-users-list-toolbar + .admin-users-selection-region");
    });

    test("keeps the first-load failure explicit and retryable", async () => {
        listUsers = async () => {
            throw new Error("用户接口暂时不可用");
        };

        await renderUsers();

        const alert = document.querySelector('.admin-users-load-error [role="alert"]');
        expect(alert?.textContent).toContain("用户列表加载失败");
        expect(alert?.textContent).toContain("用户接口暂时不可用");
        expect(alert?.textContent).toContain("重试");
        expect(document.querySelector(".app-data-table")).toBeNull();
    });

    test("keeps URL filters in the request and explains an empty filtered result", async () => {
        let requestedRole = "";
        listUsers = async (params) => {
            requestedRole = params.role ?? "";
            return { users: [], total: 0, page: 1, limit: 20 };
        };

        await renderUsers("/?role=admin");

        expect(requestedRole).toBe("admin");
        expect(document.querySelector(".admin-table-empty")?.textContent).toContain("没有符合筛选条件的数据");
        expect(document.querySelector(".admin-table-empty")?.textContent).toContain("调整搜索词或筛选条件后再试");
    });
});

async function renderUsers(initialEntry = "/") {
    const host = document.createElement("div");
    document.body.append(host);
    root = createRoot(host);

    await act(async () => {
        root?.render(createElement(ConfigProvider, null, createElement(App, null, createElement(MemoryRouter, { initialEntries: [initialEntry] }, createElement(UsersPanel, { api })))));
    });
    await act(async () => new Promise((resolve) => setTimeout(resolve, 0)));
}
