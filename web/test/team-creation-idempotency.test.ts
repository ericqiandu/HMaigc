import { describe, expect, test } from "bun:test";

import { teamCreationRequest } from "../src/services/api/teams";

describe("团队创建请求幂等契约", () => {
    test("把命令协调器提供的键发送为标准 Idempotency-Key 头", () => {
        expect(teamCreationRequest("映像组", "team-create-1")).toEqual({
            data: { name: "映像组" },
            headers: { "Idempotency-Key": "team-create-1" },
            method: "post",
            url: "/teams",
        });
    });
});
