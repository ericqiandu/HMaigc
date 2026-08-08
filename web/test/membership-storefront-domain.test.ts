import { describe, expect, it } from "bun:test";

import { countdownParts } from "../src/pages/membership/membership-storefront-domain";

describe("membership storefront countdown", () => {
    it("uses the backend clock and advances from the client receipt baseline", () => {
        const serverNow = "2026-08-09T00:00:00Z";
        const endsAt = "2026-08-10T02:03:04Z";
        const startedAt = 100_000;

        expect(countdownParts(endsAt, serverNow, startedAt, startedAt).map((part) => part.value)).toEqual(["01", "02", "03", "04"]);
        expect(countdownParts(endsAt, serverNow, startedAt, startedAt + 5_000).map((part) => part.value)).toEqual(["01", "02", "02", "59"]);
    });

    it("stops at zero after the configured campaign deadline", () => {
        const parts = countdownParts("2026-08-09T00:00:01Z", "2026-08-09T00:00:00Z", 10_000, 12_000);
        expect(parts.map((part) => part.value)).toEqual(["00", "00", "00", "00"]);
    });
});
