import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";

const logoutHookSource = readFileSync(new URL("../src/components/auth/use-confirm-logout.ts", import.meta.url), "utf8");
const logoutStyles = readFileSync(new URL("../src/components/auth/logout-confirm.css", import.meta.url), "utf8");
const logoutEntrypoints = [readFileSync(new URL("../src/components/layout/workspace-sidebar-footer.tsx", import.meta.url), "utf8"), readFileSync(new URL("../src/pages/home/updream/updream-account-actions.tsx", import.meta.url), "utf8")];

describe("account logout confirmation", () => {
    test("uses one shared confirmation contract for every logout entrypoint", () => {
        for (const source of logoutEntrypoints) {
            expect(source).toContain('import { useConfirmLogout } from "@/components/auth/use-confirm-logout";');
            expect(source).toContain("confirmLogout();");
            expect(source).not.toContain('from "@/services/api/auth"');
            expect(source).not.toContain("await logout()");
        }
    });

    test("only executes logout from the explicit confirm action", () => {
        expect(logoutHookSource).toContain("modal.confirm({");
        expect(logoutHookSource).toContain('title: "您确定要退出登录吗？"');
        expect(logoutHookSource).toContain('okText: "确认退出"');
        expect(logoutHookSource).toContain('cancelText: "取消"');
        expect(logoutHookSource).toContain('autoFocusButton: "cancel"');
        expect(logoutHookSource.match(/await logout\(\)/g)).toHaveLength(1);
        expect(logoutHookSource.indexOf("await logout()")).toBeGreaterThan(logoutHookSource.indexOf("onOk: async"));
    });

    test("uses semantic theme tokens and preserves mobile touch targets", () => {
        expect(logoutStyles).toContain("background: var(--bg-surface)");
        expect(logoutStyles).toContain("color: var(--text-secondary)");
        expect(logoutStyles).toContain("background: var(--brand-primary)");
        expect(logoutStyles).toContain("@media (max-width: 639px)");
        expect(logoutStyles).toContain("min-height: 44px");
        expect(logoutStyles).not.toMatch(/#[0-9a-f]{3,8}\b/i);
    });
});
