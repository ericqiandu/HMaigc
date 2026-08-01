import { describe, expect, test } from "bun:test";

import { validateAuthLegalConsent } from "../src/pages/auth/auth-legal-consent-policy";

describe("authentication legal consent policy", () => {
    test("blocks authentication until the user explicitly accepts both legal documents", () => {
        expect(validateAuthLegalConsent(false)).toEqual({
            ok: false,
            message: "请先阅读并同意《用户协议》和《隐私政策》",
        });
    });

    test("allows authentication after explicit consent", () => {
        expect(validateAuthLegalConsent(true)).toEqual({ ok: true });
    });
});
