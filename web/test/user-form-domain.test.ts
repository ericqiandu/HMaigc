import { describe, expect, test } from "bun:test";

import type { AdminUser } from "../src/services/api/auth";
import { describeAccessChanges, normalizeUserFormValues, toUserFormValues, userFormValuesEqual } from "../src/pages/admin/users/user-form-domain";

const user = {
    id: "user-1",
    username: "operator",
    displayName: "  运营人员  ",
    email: " operator@example.com ",
    role: "user",
    status: "active",
} as AdminUser;

describe("user form domain", () => {
    test("normalizes editable values before dirty comparison and submission", () => {
        const initial = toUserFormValues(user);
        const normalized = normalizeUserFormValues({ displayName: "运营人员 ", email: "operator@example.com" }, initial);
        expect(userFormValuesEqual(initial, normalized)).toBe(true);
    });

    test("describes only access facts that actually changed", () => {
        expect(describeAccessChanges(user, { displayName: "运营人员", email: "operator@example.com", role: "admin", status: "active" }))
            .toEqual(["角色：普通用户 → 管理员"]);
    });
});
