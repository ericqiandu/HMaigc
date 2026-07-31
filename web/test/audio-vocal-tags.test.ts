import { describe, expect, test } from "bun:test";

import { miniMaxVocalTags } from "../src/lib/audio-generation";

describe("MiniMax vocal tags", () => {
    test("keeps the synchronous speech API tags and official Chinese labels aligned", () => {
        expect(miniMaxVocalTags).toHaveLength(19);
        expect(miniMaxVocalTags).toContainEqual({ value: "(clear-throat)", label: "清嗓子" });
        expect(miniMaxVocalTags).toContainEqual({ value: "(breath)", label: "正常换气" });
        expect(miniMaxVocalTags).toContainEqual({ value: "(sniffs)", label: "吸鼻子" });
        expect(miniMaxVocalTags).toContainEqual({ value: "(snorts)", label: "喷鼻息" });
        expect(miniMaxVocalTags).toContainEqual({ value: "(hissing)", label: "嘶嘶声" });
    });
});
