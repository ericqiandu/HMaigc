type MembershipActionInput = { status: "loading" } | { status: "error"; message: string } | { status: "ready"; isActiveMember: boolean };

export type MembershipAction = {
    label: string;
    title: string;
};

export function resolveMembershipAction(input: MembershipActionInput): MembershipAction {
    if (input.status === "loading") {
        return { label: "会员权益", title: "正在读取会员状态" };
    }
    if (input.status === "error") {
        return { label: "会员状态异常", title: input.message };
    }
    return input.isActiveMember ? { label: "会员中心", title: "进入会员中心" } : { label: "升级会员", title: "升级会员" };
}
