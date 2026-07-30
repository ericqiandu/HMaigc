import { useQuery } from "@tanstack/react-query";

import { resolveMembershipAction, type MembershipAction } from "@/lib/membership-action";
import { getMyMembership } from "@/services/api/membership";

export const membershipQueryKey = (userId: string) => ["membership", userId] as const;

export function useMembershipAction(userId?: string): MembershipAction {
    const membershipQuery = useQuery({
        queryKey: membershipQueryKey(userId || ""),
        queryFn: getMyMembership,
        enabled: Boolean(userId),
        refetchOnMount: "always",
        refetchOnWindowFocus: "always",
    });

    if (!userId || membershipQuery.isPending) {
        return resolveMembershipAction({ status: "loading" });
    }
    if (membershipQuery.isError) {
        return resolveMembershipAction({
            status: "error",
            message: membershipQuery.error instanceof Error ? membershipQuery.error.message : "会员状态读取失败",
        });
    }
    return resolveMembershipAction({
        status: "ready",
        isActiveMember: membershipQuery.data.entitlement.isActiveMember,
    });
}
