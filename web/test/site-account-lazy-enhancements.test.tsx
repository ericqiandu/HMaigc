import "./setup-happy-dom";

import { afterEach, describe, expect, mock, test } from "bun:test";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { App } from "antd";
import { act, createElement } from "react";
import { createRoot, type Root } from "react-dom/client";
import { MemoryRouter } from "react-router";

import { useUserStore } from "../src/stores/use-user-store";

mock.module("@/components/account/ai-watermark-settings-modal", () => ({
    AIWatermarkSettingsModal: () => createElement("div", { className: "watermark-modal-test-marker" }, "watermark"),
}));
mock.module("@/components/account/referral-reward-center", () => ({
    openReferralCenter: () => undefined,
    ReferralRewardCenter: () => null,
}));
mock.module("@/components/auth/use-confirm-logout", () => ({ useConfirmLogout: () => () => undefined }));
mock.module("@/components/layout/system-announcement-center", () => ({ SystemAnnouncementCenter: () => null }));
mock.module("@/hooks/use-membership-action", () => ({ useMembershipAction: () => ({ label: "升级会员", title: "升级会员" }) }));
mock.module("@/hooks/use-wallet-balance", () => ({ useWalletBalance: () => ({ availableMicrocredits: 0 }) }));

let root: Root | null = null;
let queryClient: QueryClient | null = null;

afterEach(async () => {
    if (root) await act(async () => root?.unmount());
    root = null;
    queryClient?.clear();
    queryClient = null;
    document.body.replaceChildren();
    useUserStore.setState({ user: null, hydrated: false });
});

describe("site account optional enhancements", () => {
    test("does not mount the watermark dialog before the user requests it", async () => {
        useUserStore.setState({
            hydrated: true,
            user: { id: "user-1", publicId: 1024, username: "admin", displayName: "Admin", role: "admin", status: "active" },
        });
        const { SiteAccountActions } = await import("../src/components/layout/site-account-actions");
        queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
        const host = document.createElement("div");
        document.body.append(host);
        root = createRoot(host);

        await act(async () => {
            root?.render(createElement(QueryClientProvider, { client: queryClient }, createElement(App, null, createElement(MemoryRouter, null, createElement(SiteAccountActions)))));
        });

        expect(host.querySelector(".watermark-modal-test-marker") === null).toBe(true);
    });
});
