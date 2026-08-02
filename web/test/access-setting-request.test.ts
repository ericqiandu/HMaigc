import { describe, expect, test } from "bun:test";

import {
    buildLinuxDOSettingRequest,
    linuxDOSettingToFormValues,
    linuxDOSettingValuesEqual,
} from "../src/pages/admin/components/access-setting-request";
import type { LinuxDOSetting } from "../src/services/api/wallet";

const setting: LinuxDOSetting = {
    enabled: true,
    clientId: "client-id",
    hasClientSecret: true,
    authorizationUrl: "https://connect.linux.do/oauth2/authorize",
    tokenUrl: "https://connect.linux.do/oauth2/token",
    userInfoUrl: "https://connect.linux.do/api/user",
    redirectUrl: "https://hmaigc.ai/oauth/linuxdo/callback",
    scopes: ["openid", "profile", "email"],
    clientAuthMethod: "client_secret_post",
    subjectField: "id",
    usernameField: "username",
    displayNameField: "name",
    emailField: "email",
    avatarField: "avatar_url",
};

describe("Linux.do setting request", () => {
    test("maps a saved secret to an intentionally blank form value", () => {
        const values = linuxDOSettingToFormValues(setting);
        expect(values.clientSecret).toBe("");
        expect(linuxDOSettingValuesEqual(values, setting)).toBe(true);
    });

    test("normalizes whitespace and scope order before submission", () => {
        const request = buildLinuxDOSettingRequest({
            ...linuxDOSettingToFormValues(setting),
            clientId: " client-id ",
            scopes: [" profile ", "openid", "email", "openid"],
        });
        expect(request.clientId).toBe("client-id");
        expect(request.scopes).toEqual(["email", "openid", "profile"]);
        expect(linuxDOSettingValuesEqual(request, setting)).toBe(true);
    });

    test("detects a changed secret and changed callback as unsaved", () => {
        const values = linuxDOSettingToFormValues(setting);
        expect(linuxDOSettingValuesEqual({ ...values, clientSecret: "new-secret" }, setting)).toBe(false);
        expect(linuxDOSettingValuesEqual({ ...values, redirectUrl: "https://example.com/callback" }, setting)).toBe(false);
    });
});
