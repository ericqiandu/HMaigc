import "./setup-happy-dom";

import { afterEach, expect, test } from "bun:test";

import { personalWorkspaceScope, projectBelongsToWorkspace, readWorkspaceScope, writeWorkspaceScope, type WorkspaceScope } from "../src/lib/workspace-scope";
import { useUserStore } from "../src/stores/use-user-store";

afterEach(() => {
    useUserStore.getState().clearSession();
    window.localStorage.clear();
});

test("工作区选择按用户隔离并默认回到个人空间", () => {
    const teamScope: WorkspaceScope = { kind: "team", teamId: "team-a" };

    writeWorkspaceScope("user-a", teamScope);

    expect(readWorkspaceScope("user-a")).toEqual(teamScope);
    expect(readWorkspaceScope("user-b")).toEqual(personalWorkspaceScope);
});

test("项目只属于当前明确选择的个人或团队工作区", () => {
    const personalProject = { userId: "user-a", teamId: "" };
    const teamProject = { userId: "another-team-member", teamId: "team-a" };

    expect(projectBelongsToWorkspace(personalProject, "user-a", personalWorkspaceScope)).toBeTrue();
    expect(projectBelongsToWorkspace(teamProject, "user-a", personalWorkspaceScope)).toBeFalse();
    expect(projectBelongsToWorkspace(teamProject, "user-a", { kind: "team", teamId: "team-a" })).toBeTrue();
    expect(projectBelongsToWorkspace(teamProject, "user-a", { kind: "team", teamId: "team-b" })).toBeFalse();
});

test("登录用户切换工作区后会恢复同一用户最后一次选择", () => {
    const user = { id: "user-a", publicId: 10001, username: "owner", displayName: "Owner", role: "user" as const, status: "active" as const };
    const teamScope: WorkspaceScope = { kind: "team", teamId: "team-a" };

    useUserStore.getState().setUser(user);
    useUserStore.getState().selectWorkspaceScope(teamScope);
    useUserStore.getState().clearSession();
    useUserStore.getState().setUser(user);

    expect(useUserStore.getState().workspaceScope).toEqual(teamScope);
});

test("浏览器拒绝访问本地存储时工作区会话仍可初始化和切换", () => {
    const originalStorage = window.localStorage;
    const originalWarn = console.warn;
    const unavailableStorage: Storage = {
        get length() {
            throw new DOMException("storage blocked", "SecurityError");
        },
        clear() {
            throw new DOMException("storage blocked", "SecurityError");
        },
        getItem() {
            throw new DOMException("storage blocked", "SecurityError");
        },
        key() {
            throw new DOMException("storage blocked", "SecurityError");
        },
        removeItem() {
            throw new DOMException("storage blocked", "SecurityError");
        },
        setItem() {
            throw new DOMException("storage blocked", "SecurityError");
        },
    };
    Object.defineProperty(window, "localStorage", { configurable: true, value: unavailableStorage });
    console.warn = () => undefined;
    try {
        expect(readWorkspaceScope("user-a")).toEqual(personalWorkspaceScope);
        expect(() => writeWorkspaceScope("user-a", { kind: "team", teamId: "team-a" })).not.toThrow();
    } finally {
        Object.defineProperty(window, "localStorage", { configurable: true, value: originalStorage });
        console.warn = originalWarn;
    }
});
