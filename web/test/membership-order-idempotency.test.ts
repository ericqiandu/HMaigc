import { describe, expect, test } from "bun:test";

import { membershipOrderRequest, type MembershipOrder, type MembershipOrderRequestIdentity, type MembershipOrderRequestInput, type Team } from "../src/services/api/membership";

const order: MembershipOrder = {
    id: "order-1",
    orderNumber: "M-1",
    userId: "user-1",
    planId: "plan-1",
    seats: 1,
    unitPriceCents: 100,
    totalPriceCents: 100,
    currency: "CNY",
    status: "pending",
    planSnapshotJson: "{}",
    paymentProvider: "",
    providerTradeNo: "",
    createdAt: "2026-08-09T00:00:00Z",
    updatedAt: "2026-08-09T00:00:00Z",
};

describe("会员订单请求幂等契约", () => {
    test("规范化购买指纹复用同一 key，任一计划、团队或席位变化都会轮换", async () => {
        const { bindMembershipOrderRequestIdentity } = await import("../src/services/api/membership");
        const initial = bindMembershipOrderRequestIdentity({ planId: " plan-a ", teamId: " team-a ", seats: 3 }, null, "key-a");
        expect(initial).toEqual({ fingerprint: '{"planId":"plan-a","teamId":"team-a","seats":3}', key: "key-a" });
        expect(bindMembershipOrderRequestIdentity({ planId: "plan-a", teamId: "team-a", seats: 3 }, initial, "unused-key")).toEqual(initial);

        const changes: MembershipOrderRequestInput[] = [
            { planId: "plan-b", teamId: "team-a", seats: 3 },
            { planId: "plan-a", teamId: "team-b", seats: 3 },
            { planId: "plan-a", teamId: "team-a", seats: 4 },
        ];
        changes.forEach((input, index) => {
            const nextKey = `key-${index + 2}`;
            expect(bindMembershipOrderRequestIdentity(input, initial, nextKey).key).toBe(nextKey);
        });
    });

    test("订单 API 把显式幂等 key 发送为标准 Idempotency-Key 头", () => {
        expect(membershipOrderRequest({ planId: "plan-1", seats: 1 }, "request-key-1")).toEqual({
            data: { planId: "plan-1", seats: 1 },
            headers: { "Idempotency-Key": "request-key-1" },
            method: "post",
            url: "/membership/orders",
        });
    });

    test("新团队 ID 和请求 identity 都在订单请求前持久化，失败重试不会产生第二个指纹", async () => {
        const { submitMembershipOrderRequest } = await import("../src/services/api/membership");
        const events: string[] = [];
        let persistedTeamID: string | undefined;
        let persistedIdentity: MembershipOrderRequestIdentity | null = null;
        const team: Team = { id: "team-created", ownerUserId: "user-1", name: "新团队", status: "active", createdAt: "2026-08-09T00:00:00Z", updatedAt: "2026-08-09T00:00:00Z" };
        const dependencies = {
            createTeam: async () => {
                events.push("team-created");
                return team;
            },
            persistResolvedTeamID: (teamID: string) => {
                events.push(`team-persisted:${teamID}`);
                persistedTeamID = teamID;
            },
            persistIdentity: (identity: MembershipOrderRequestIdentity) => {
                events.push(`identity-persisted:${identity.fingerprint}`);
                persistedIdentity = identity;
            },
            createOrder: async (input: MembershipOrderRequestInput, key: string) => {
                events.push(`order-request:${input.teamId}:${key}`);
                return { ...order, teamId: input.teamId, seats: input.seats ?? 1 };
            },
        };

        await submitMembershipOrderRequest({ planId: "team-plan", seats: 3 }, "新团队", true, null, "request-key", dependencies);
        expect(events).toEqual(["team-created", "team-persisted:team-created", 'identity-persisted:{"planId":"team-plan","teamId":"team-created","seats":3}', "order-request:team-created:request-key"]);
        expect(persistedTeamID).toBe("team-created");

        events.length = 0;
        await submitMembershipOrderRequest({ planId: "team-plan", teamId: persistedTeamID, seats: 3 }, "", true, persistedIdentity, "unused-key", dependencies);
        expect(events).toEqual(['identity-persisted:{"planId":"team-plan","teamId":"team-created","seats":3}', "order-request:team-created:request-key"]);
    });

    test("个人套餐提交不会携带先前团队购买残留的 teamId", async () => {
        const { submitMembershipOrderRequest } = await import("../src/services/api/membership");
        let requestedInput: MembershipOrderRequestInput | null = null;
        await submitMembershipOrderRequest({ planId: "personal-plan", teamId: "stale-team", seats: 1 }, "", false, null, "personal-key", {
            createTeam: async () => {
                throw new Error("personal purchase must not create a team");
            },
            persistResolvedTeamID: () => {
                throw new Error("personal purchase must not persist a team");
            },
            persistIdentity: () => undefined,
            createOrder: async (input) => {
                requestedInput = input;
                return order;
            },
        });
        expect(requestedInput?.teamId).toBe("");
    });
});
