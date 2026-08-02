import { describe, expect, test } from "bun:test";

import { emptyLegalDraft, legalDraftsEqual, normalizeLegalDraft } from "../src/pages/admin/settings/legal-draft";

describe("legal draft", () => {
    test("treats synchronized legal content as unchanged", () => {
        expect(legalDraftsEqual(emptyLegalDraft, { ...emptyLegalDraft })).toBe(true);
        expect(legalDraftsEqual(emptyLegalDraft, { userAgreement: "<p></p>", privacyPolicy: "<p></p>" })).toBe(true);
    });

    test("ignores surrounding transport whitespace", () => {
        expect(legalDraftsEqual(
            { userAgreement: "<p>用户协议</p>", privacyPolicy: "<p>隐私政策</p>" },
            { userAgreement: "  <p>用户协议</p>\n", privacyPolicy: "\n<p>隐私政策</p>  " },
        )).toBe(true);
    });

    test("detects a real document change and normalizes the publish payload", () => {
        const draft = { userAgreement: " <p>新协议</p> ", privacyPolicy: "<p>隐私政策</p>" };
        expect(legalDraftsEqual(draft, { userAgreement: "<p>旧协议</p>", privacyPolicy: "<p>隐私政策</p>" })).toBe(false);
        expect(normalizeLegalDraft(draft)).toEqual({ userAgreement: "<p>新协议</p>", privacyPolicy: "<p>隐私政策</p>" });
    });
});
