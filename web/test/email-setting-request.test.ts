import { describe, expect, test } from "bun:test";

import { buildEmailSettingRequest, emailSettingValuesEqual } from "../src/pages/admin/components/email-setting-request";

const validFormValues = {
    enabled: true,
    host: " smtpdm-ap-southeast-1.aliyuncs.com ",
    port: "465",
    username: "mailer@example.com",
    password: "secret",
    encryption: "tls" as const,
    fromEmail: "noreply@example.com",
    fromName: "HMaigc",
};

describe("email setting request", () => {
    test("serializes the SMTP port as an integer instead of an HTML input string", () => {
        const request = buildEmailSettingRequest(validFormValues);

        expect(request.port).toBe(465);
        expect(typeof request.port).toBe("number");
        expect(request.host).toBe("smtpdm-ap-southeast-1.aliyuncs.com");
    });

    test("rejects invalid SMTP ports before sending the request", () => {
        expect(() => buildEmailSettingRequest({ ...validFormValues, port: "465.5" })).toThrow(
            "SMTP 端口必须是 1 到 65535 的整数",
        );
        expect(() => buildEmailSettingRequest({ ...validFormValues, port: "70000" })).toThrow(
            "SMTP 端口必须是 1 到 65535 的整数",
        );
    });

    test("treats whitespace-only edits as the synchronized server value", () => {
        expect(emailSettingValuesEqual(
            { ...validFormValues, host: " ", username: " ", password: " ", fromEmail: " ", fromName: " HMaigc " },
            {
                enabled: true,
                host: "",
                port: 465,
                username: "",
                encryption: "tls",
                fromEmail: "",
                fromName: "HMaigc",
                hasPassword: true,
            },
        )).toBe(true);
    });

    test("detects a new password as an unsaved secret change", () => {
        expect(emailSettingValuesEqual(
            validFormValues,
            {
                enabled: true,
                host: "smtpdm-ap-southeast-1.aliyuncs.com",
                port: 465,
                username: "mailer@example.com",
                encryption: "tls",
                fromEmail: "noreply@example.com",
                fromName: "HMaigc",
                hasPassword: true,
            },
        )).toBe(false);
    });
});
