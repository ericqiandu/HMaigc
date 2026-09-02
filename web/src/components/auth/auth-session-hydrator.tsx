import type { ReactNode } from "react";
import { useEffect } from "react";

import { applyUserSession } from "@/lib/user-session";
import { getAuthSession, type AuthSessionPayload } from "@/services/api/auth";

export function AuthSessionHydrator({ children }: { children: ReactNode }) {
    useEffect(() => {
        let cancelled = false;
        void (async () => {
            let payload: AuthSessionPayload;
            try {
                payload = await getAuthSession();
            } catch (error: unknown) {
                console.error("登录会话读取失败", { error });
                payload = { user: null, systemChannels: [] };
            }
            if (cancelled) return;
            try {
                await applyUserSession(payload);
            } catch (error: unknown) {
                console.error("用户工作区会话恢复失败", { userId: payload.user?.id ?? null, error });
            }
        })();
        return () => {
            cancelled = true;
        };
    }, []);

    return children;
}
