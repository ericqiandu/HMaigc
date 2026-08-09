import { describe, expect, test } from "bun:test";

import { publicPlanName } from "@/pages/membership/membership-formatters";

describe("publicPlanName", () => {
    test("renders the backend catalog name without a frontend alias layer", () => {
        expect(publicPlanName({ name: "标准版" })).toBe("标准版");
        expect(publicPlanName({ name: "高级版" })).toBe("高级版");
        expect(publicPlanName({ name: "至尊版" })).toBe("至尊版");
    });
});
