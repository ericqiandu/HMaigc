import type { ReactNode } from "react";
import { useEffect } from "react";

import { applyUserSession } from "@/lib/user-session";
import { getAuthSession } from "@/services/api/auth";

export function AuthSessionHydrator({ children }: { children: ReactNode }) {
    useEffect(() => {
        let cancelled = false;
        getAuthSession()
            .then(async (payload) => {
                if (!cancelled) await applyUserSession(payload);
            })
            .catch(async () => {
                if (!cancelled) await applyUserSession({ user: null, systemChannels: [] });
            });
        return () => {
            cancelled = true;
        };
    }, []);

    return children;
}
