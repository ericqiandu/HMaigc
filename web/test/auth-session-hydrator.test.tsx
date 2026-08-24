import "./setup-happy-dom";

import { afterEach, describe, expect, mock, test } from "bun:test";
import { act, createElement } from "react";
import { createRoot, type Root } from "react-dom/client";

import type { AuthSessionPayload } from "../src/services/api/auth";

let root: Root | null = null;
let requestSession: () => Promise<AuthSessionPayload>;
const appliedSessions: AuthSessionPayload[] = [];

mock.module("@/services/api/auth", () => ({
    getAuthSession: () => requestSession(),
}));

mock.module("@/lib/user-session", () => ({
    applyUserSession: async (payload: AuthSessionPayload) => {
        appliedSessions.push(payload);
    },
}));

mock.module("@/components/ui/aceternity/full-screen-loader", () => ({
    FullScreenLoader: () => createElement("div", { className: "session-loading-state" }, "恢复登录态"),
}));

afterEach(async () => {
    if (root) await act(async () => root?.unmount());
    root = null;
    appliedSessions.length = 0;
    document.body.replaceChildren();
});

describe("AuthSessionHydrator public startup", () => {
    test("renders public content while the authoritative session request is pending", async () => {
        let resolveSession: ((payload: AuthSessionPayload) => void) | undefined;
        requestSession = () =>
            new Promise<AuthSessionPayload>((resolve) => {
                resolveSession = resolve;
            });
        const { AuthSessionHydrator } = await import("../src/components/auth/auth-session-hydrator");
        const host = document.createElement("div");
        document.body.append(host);
        root = createRoot(host);

        await act(async () => {
            root?.render(createElement(AuthSessionHydrator, null, createElement("main", { className: "public-startup-content" }, "公开首页")));
        });

        expect(host.querySelector(".public-startup-content")?.textContent).toBe("公开首页");
        expect(appliedSessions).toHaveLength(0);

        await act(async () => {
            resolveSession?.({ user: null, systemChannels: [] });
            await Promise.resolve();
        });
        expect(appliedSessions).toEqual([{ user: null, systemChannels: [] }]);
    });

    test("records an anonymous hydrated fact when the session request fails", async () => {
        requestSession = () => Promise.reject(new Error("session unavailable"));
        const { AuthSessionHydrator } = await import("../src/components/auth/auth-session-hydrator");
        const host = document.createElement("div");
        document.body.append(host);
        root = createRoot(host);

        await act(async () => {
            root?.render(createElement(AuthSessionHydrator, null, createElement("main", { className: "public-startup-content" }, "登录页")));
            await Promise.resolve();
            await Promise.resolve();
        });

        expect(host.querySelector(".public-startup-content")?.textContent).toBe("登录页");
        expect(appliedSessions).toEqual([{ user: null, systemChannels: [] }]);
    });
});
