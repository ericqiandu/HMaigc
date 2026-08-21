import "./setup-happy-dom";

import { App } from "antd";
import { afterEach, beforeAll, beforeEach, expect, test } from "bun:test";
import { act, createElement, type ComponentType } from "react";
import type { Root } from "react-dom/client";
import { MemoryRouter } from "react-router";

import { createCanvasProjectDeletionService } from "../src/services/canvas-project-deletion";
import { useCanvasStore, type CanvasProject } from "../src/stores/canvas/use-canvas-store";
import { useCanvasUiStore } from "../src/stores/canvas/use-canvas-ui-store";

type DeleteFailure = { id: string; reason: string };
type DeleteResult = { deletedIds: string[]; failures: DeleteFailure[] };
type DeleteAction = (ids: string[]) => Promise<DeleteResult>;

let DeleteDialog: ComponentType<{ deleteAction?: DeleteAction }>;
let ProjectCard: ComponentType<{ project: CanvasProject }>;
let createRoot: (container: Element | DocumentFragment) => Root;
let root: Root | null = null;

beforeAll(async () => {
    ({ createRoot } = await import("react-dom/client"));
    ({ CanvasDeleteProjectsDialog: DeleteDialog } = await import("../src/components/canvas/canvas-delete-projects-dialog"));
    ({ CanvasProjectCard: ProjectCard } = await import("../src/components/canvas/canvas-project-card"));
});

afterEach(async () => {
    if (root) await act(async () => root?.unmount());
    root = null;
    document.body.replaceChildren();
});

beforeEach(async () => {
    if (root) await act(async () => root?.unmount());
    root = null;
    document.body.replaceChildren();
    useCanvasStore.setState({ hydrated: true, projects: [], pendingDeletionIds: [] });
    useCanvasUiStore.setState({ deleteProjectIds: [], selectedProjectIds: [] });
});

test("云端删除成功前保留本地项目，成功后再一次性删除", async () => {
    let finishRemoteDelete: (() => void) | null = null;
    const localDeletes: string[][] = [];
    const remove = createCanvasProjectDeletionService({
        resolveTarget: (id) => ({ id, requiresRemoteDelete: true, canManage: true }),
        hasRemoteSession: () => true,
        isRemoteDeleteStaged: () => false,
        stageRemoteDelete: async () => undefined,
        cancelRemoteDelete: async () => undefined,
        waitForRemoteWrites: async () => undefined,
        deleteRemote: () =>
            new Promise<void>((resolve) => {
                finishRemoteDelete = resolve;
            }),
        deleteLocal: (ids) => localDeletes.push(ids),
    });

    const pending = remove(["canvas-1"]);
    await Promise.resolve();
    await Promise.resolve();
    expect(localDeletes).toEqual([]);
    expect(finishRemoteDelete).not.toBeNull();
    finishRemoteDelete?.();

    expect(await pending).toEqual({ deletedIds: ["canvas-1"], failures: [] });
    expect(localDeletes).toEqual([["canvas-1"]]);
});

test("批量删除只移除云端已成功的项目并保留失败事实", async () => {
    const localDeletes: string[][] = [];
    const remove = createCanvasProjectDeletionService({
        resolveTarget: (id) => ({ id, requiresRemoteDelete: true, canManage: true }),
        hasRemoteSession: () => true,
        isRemoteDeleteStaged: () => false,
        stageRemoteDelete: async () => undefined,
        cancelRemoteDelete: async () => undefined,
        waitForRemoteWrites: async () => undefined,
        deleteRemote: async (id) => {
            if (id === "canvas-2") throw new Error("云端数据库暂时不可用");
        },
        deleteLocal: (ids) => localDeletes.push(ids),
    });

    expect(await remove(["canvas-1", "canvas-2"])).toEqual({
        deletedIds: ["canvas-1"],
        failures: [{ id: "canvas-2", reason: "云端数据库暂时不可用" }],
    });
    expect(localDeletes).toEqual([["canvas-1"]]);
});

test("云端已不存在的陈旧项目按幂等删除收敛本地残留", async () => {
    const localDeletes: string[][] = [];
    const alreadyDeleted = Object.assign(new Error("画布不存在"), { status: 404 });
    const remove = createCanvasProjectDeletionService({
        resolveTarget: (id) => ({ id, requiresRemoteDelete: true, canManage: true }),
        hasRemoteSession: () => true,
        isRemoteDeleteStaged: () => false,
        stageRemoteDelete: async () => undefined,
        cancelRemoteDelete: async () => undefined,
        waitForRemoteWrites: async () => undefined,
        deleteRemote: async () => {
            throw alreadyDeleted;
        },
        deleteLocal: (ids) => localDeletes.push(ids),
    });

    expect(await remove(["canvas-stale"])).toEqual({ deletedIds: ["canvas-stale"], failures: [] });
    expect(localDeletes).toEqual([["canvas-stale"]]);
});

test("无管理权限的云端画布不会调用删除接口或清除本地状态", async () => {
    const remoteDeletes: string[] = [];
    const localDeletes: string[][] = [];
    const remove = createCanvasProjectDeletionService({
        resolveTarget: (id) => ({ id, requiresRemoteDelete: true, canManage: false }),
        hasRemoteSession: () => true,
        isRemoteDeleteStaged: () => false,
        stageRemoteDelete: async () => undefined,
        cancelRemoteDelete: async () => undefined,
        waitForRemoteWrites: async () => undefined,
        deleteRemote: async (id) => {
            remoteDeletes.push(id);
        },
        deleteLocal: (ids) => localDeletes.push(ids),
    });

    expect(await remove(["canvas-1"])).toEqual({
        deletedIds: [],
        failures: [{ id: "canvas-1", reason: "当前用户不能删除该画布" }],
    });
    expect(remoteDeletes).toEqual([]);
    expect(localDeletes).toEqual([]);
});

test("云端会话不可用时保留远端画布，避免只删本地后再次同步回来", async () => {
    const localDeletes: string[][] = [];
    const remove = createCanvasProjectDeletionService({
        resolveTarget: (id) => ({ id, requiresRemoteDelete: true, canManage: true }),
        hasRemoteSession: () => false,
        isRemoteDeleteStaged: () => false,
        stageRemoteDelete: async () => undefined,
        cancelRemoteDelete: async () => undefined,
        waitForRemoteWrites: async () => undefined,
        deleteRemote: async () => {
            throw new Error("不应调用云端删除");
        },
        deleteLocal: (ids) => localDeletes.push(ids),
    });

    expect(await remove(["canvas-remote"])).toEqual({
        deletedIds: [],
        failures: [{ id: "canvas-remote", reason: "尚未建立云端同步会话" }],
    });
    expect(localDeletes).toEqual([]);
});

test("未同步到云端的本地画布可在离线状态直接删除", async () => {
    const localDeletes: string[][] = [];
    const remove = createCanvasProjectDeletionService({
        resolveTarget: (id) => ({ id, requiresRemoteDelete: false, canManage: true }),
        hasRemoteSession: () => false,
        isRemoteDeleteStaged: () => false,
        stageRemoteDelete: async () => undefined,
        cancelRemoteDelete: async () => undefined,
        waitForRemoteWrites: async () => undefined,
        deleteRemote: async () => {
            throw new Error("不应调用云端删除");
        },
        deleteLocal: (ids) => localDeletes.push(ids),
    });

    expect(await remove(["canvas-local"])).toEqual({ deletedIds: ["canvas-local"], failures: [] });
    expect(localDeletes).toEqual([["canvas-local"]]);
});

test("云端成功后本地持久化中断，下一次执行通过删除事实收敛", async () => {
    const projects = new Set(["canvas-crash"]);
    const remoteProjects = new Set(["canvas-crash"]);
    const staged = new Set<string>();
    const events: string[] = [];
    let interruptLocalPersistence = true;
    const remove = createCanvasProjectDeletionService({
        resolveTarget: (id) => (projects.has(id) || staged.has(id) ? { id, requiresRemoteDelete: true, canManage: true } : null),
        hasRemoteSession: () => true,
        isRemoteDeleteStaged: (id) => staged.has(id),
        stageRemoteDelete: async (ids) => {
            events.push(`stage:${ids.join(",")}`);
            ids.forEach((id) => staged.add(id));
        },
        cancelRemoteDelete: async (ids) => ids.forEach((id) => staged.delete(id)),
        waitForRemoteWrites: async () => undefined,
        deleteRemote: async (id) => {
            events.push(`remote:${id}`);
            if (!remoteProjects.delete(id)) throw Object.assign(new Error("画布不存在"), { status: 404 });
        },
        deleteLocal: async (ids) => {
            events.push(`local:${ids.join(",")}`);
            if (interruptLocalPersistence) {
                interruptLocalPersistence = false;
                throw new Error("模拟本地持久化中断");
            }
            ids.forEach((id) => {
                projects.delete(id);
                staged.delete(id);
            });
        },
    });

    expect(await remove(["canvas-crash"])).toEqual({
        deletedIds: [],
        failures: [{ id: "canvas-crash", reason: "模拟本地持久化中断" }],
    });
    expect([...projects]).toEqual(["canvas-crash"]);
    expect([...staged]).toEqual(["canvas-crash"]);
    expect([...remoteProjects]).toEqual([]);

    expect(await remove(["canvas-crash"])).toEqual({ deletedIds: ["canvas-crash"], failures: [] });
    expect(events).toEqual(["stage:canvas-crash", "remote:canvas-crash", "local:canvas-crash", "remote:canvas-crash", "local:canvas-crash"]);
    expect([...projects]).toEqual([]);
    expect([...staged]).toEqual([]);
});

test("远端删除必须等待已经开始的云端写入，避免迟到 upsert 复活项目", async () => {
    let releaseRemoteWrite: (() => void) | null = null;
    let remoteDeleteStarted = false;
    const remove = createCanvasProjectDeletionService({
        resolveTarget: (id) => ({ id, requiresRemoteDelete: true, canManage: true }),
        hasRemoteSession: () => true,
        isRemoteDeleteStaged: () => false,
        stageRemoteDelete: async () => undefined,
        cancelRemoteDelete: async () => undefined,
        waitForRemoteWrites: () => new Promise<void>((resolve) => {
            releaseRemoteWrite = resolve;
        }),
        deleteRemote: async () => {
            remoteDeleteStarted = true;
        },
        deleteLocal: async () => undefined,
    });

    const pending = remove(["canvas-sync-race"]);
    await Promise.resolve();
    await Promise.resolve();
    expect(releaseRemoteWrite).not.toBeNull();
    expect(remoteDeleteStarted).toBe(false);

    releaseRemoteWrite?.();
    expect(await pending).toEqual({ deletedIds: ["canvas-sync-race"], failures: [] });
    expect(remoteDeleteStarted).toBe(true);
});

test("删除确认框必须委托统一删除动作，失败时保持项目和弹窗", async () => {
    const id = useCanvasStore.getState().createProject("云端画布");
    useCanvasUiStore.setState({ deleteProjectIds: [id], selectedProjectIds: [id] });
    const calls: string[][] = [];
    const deleteAction: DeleteAction = async (ids) => {
        calls.push(ids);
        return { deletedIds: [], failures: [{ id, reason: "云端删除失败" }] };
    };

    await mountDeleteDialog(deleteAction);
    await act(async () => deleteButton().click());
    await settle();

    expect(calls).toEqual([[id]]);
    expect(useCanvasStore.getState().projects.map((project) => project.id)).toEqual([id]);
    expect(useCanvasUiStore.getState().deleteProjectIds).toEqual([id]);
});

test("无管理权限的团队画布不展示删除入口", async () => {
    const id = useCanvasStore.getState().createProject("只读团队画布");
    useCanvasStore.getState().updateProject(id, { teamId: "team-1", canManage: false });
    const project = useCanvasStore.getState().projects.find((item) => item.id === id);
    if (!project) throw new Error("测试画布创建失败");

    const host = document.createElement("div");
    document.body.append(host);
    root = createRoot(host);
    await act(async () => root?.render(createElement(MemoryRouter, null, createElement(ProjectCard, { project }))));
    await settle();
    const operations = document.querySelector('button[aria-label="只读团队画布 画布操作"]');
    if (!(operations instanceof HTMLButtonElement)) throw new Error("未找到画布操作按钮");
    await act(async () => operations.click());
    await settle();

    expect([...document.querySelectorAll('[role="menuitem"]')].some((item) => item.textContent?.includes("删除"))).toBeFalse();
});

async function mountDeleteDialog(deleteAction: DeleteAction) {
    const host = document.createElement("div");
    document.body.append(host);
    root = createRoot(host);
    await act(async () => root?.render(createElement(App, null, createElement(DeleteDialog, { deleteAction }))));
    await settle();
}

function deleteButton() {
    const match = [...document.querySelectorAll("button")].find((item) => item.textContent?.replace(/\s+/g, "") === "删除");
    if (!(match instanceof HTMLButtonElement)) throw new Error("未找到删除确认按钮");
    return match;
}

async function settle() {
    await act(async () => {
        await Promise.resolve();
        await new Promise((resolve) => setTimeout(resolve, 0));
    });
}
