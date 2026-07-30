import { describe, expect, test } from "bun:test";

import { resolveMembershipAction } from "@/lib/membership-action";

describe("resolveMembershipAction", () => {
    test("shows membership center for an active member", () => {
        expect(resolveMembershipAction({ status: "ready", isActiveMember: true })).toEqual({
            label: "会员中心",
            title: "进入会员中心",
        });
    });

    test("shows upgrade membership for an Origin user", () => {
        expect(resolveMembershipAction({ status: "ready", isActiveMember: false })).toEqual({
            label: "升级会员",
            title: "升级会员",
        });
    });

    test("does not mislabel unresolved membership state", () => {
        expect(resolveMembershipAction({ status: "loading" })).toEqual({
            label: "会员权益",
            title: "正在读取会员状态",
        });
        expect(resolveMembershipAction({ status: "error", message: "会员接口异常" })).toEqual({
            label: "会员状态异常",
            title: "会员接口异常",
        });
    });
});
