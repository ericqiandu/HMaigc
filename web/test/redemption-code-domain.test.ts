import { describe, expect, test } from "bun:test";

import { redeemBatchDisableDescription, redeemBatchRequest, redeemCodeDisableDescription } from "../src/pages/admin/components/redemption-code-domain";

describe("redemption code operations", () => {
    test("normalizes a batch creation request", () => {
        expect(redeemBatchRequest({ amount: 1.25, count: 20, note: "  夏季活动  ", expiresAt: "2026-08-31T12:00" })).toEqual({
            amountMicrocredits: 1_250_000,
            count: 20,
            note: "夏季活动",
            expiresAt: new Date("2026-08-31T12:00").toISOString(),
        });
    });

    test("does not submit empty optional values", () => {
        expect(redeemBatchRequest({ amount: 0.000001, count: 1, note: "   " })).toEqual({
            amountMicrocredits: 1,
            count: 1,
            note: undefined,
            expiresAt: undefined,
        });
    });

    test("describes the exact disable impact", () => {
        expect(redeemBatchDisableDescription({
            id: "batch-1",
            amountMicrocredits: 10_000_000,
            count: 12,
            createdBy: "admin-1",
            createdAt: "2026-08-02T00:00:00.000Z",
            availableCount: 7,
            redeemedCount: 3,
            disabledCount: 1,
            expiredCount: 1,
        })).toContain("当前 7 个可用兑换码");
        expect(redeemCodeDisableDescription({ codeSuffix: "9K2P" })).toContain("····9K2P");
    });
});
