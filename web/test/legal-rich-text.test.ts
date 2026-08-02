import { describe, expect, test } from "bun:test";

import { isSafeLegalImageURL, isSafeLegalLink } from "../src/components/legal/legal-rich-text";

describe("legal rich text URL policy", () => {
    test("accepts supported links and rejects executable schemes", () => {
        expect(isSafeLegalLink("https://hmaigc.ai/help")).toBe(true);
        expect(isSafeLegalLink("mailto:support@hmaigc.ai")).toBe(true);
        expect(isSafeLegalLink("javascript:alert(1)")).toBe(false);
    });

    test("only accepts credential-free HTTP image URLs", () => {
        expect(isSafeLegalImageURL("https://assets.hmaigc.ai/legal/example.png")).toBe(true);
        expect(isSafeLegalImageURL("https://user:secret@assets.hmaigc.ai/example.png")).toBe(false);
        expect(isSafeLegalImageURL("data:image/png;base64,AAAA")).toBe(false);
    });
});
