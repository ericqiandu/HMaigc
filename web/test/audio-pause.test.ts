import { describe, expect, test } from "bun:test";

import { normalizeAudioPauseInput, parseAudioPauseToken, replaceTextRange } from "../src/lib/audio-pause";

describe("audio pause protocol", () => {
    test("normalizes a valid custom duration into the MiniMax pause token", () => {
        expect(normalizeAudioPauseInput("0.80")).toEqual({
            ok: true,
            seconds: 0.8,
            token: "<#0.8#>",
        });
        expect(normalizeAudioPauseInput("99.99")).toEqual({
            ok: true,
            seconds: 99.99,
            token: "<#99.99#>",
        });
    });

    test("rejects values outside the provider contract or with excessive precision", () => {
        expect(normalizeAudioPauseInput("0")).toEqual({
            ok: false,
            message: "停顿时长需为 0.01–99.99 秒",
        });
        expect(normalizeAudioPauseInput("100")).toEqual({
            ok: false,
            message: "停顿时长需为 0.01–99.99 秒",
        });
        expect(normalizeAudioPauseInput("0.123")).toEqual({
            ok: false,
            message: "请输入最多两位小数的秒数",
        });
    });

    test("parses only valid structural pause tokens", () => {
        expect(parseAudioPauseToken("<#0.25#>")).toBe(0.25);
        expect(parseAudioPauseToken("<#99.99#>")).toBe(99.99);
        expect(parseAudioPauseToken("<#0#>")).toBeNull();
        expect(parseAudioPauseToken("<#pause#>")).toBeNull();
    });

    test("replaces the active pause token without appending a second token", () => {
        expect(replaceTextRange("开场<#0.25#>继续", { start: 2, end: 10 }, "<#0.5#>")).toEqual({
            value: "开场<#0.5#>继续",
            range: {
                start: 2,
                end: 9,
            },
        });
    });
});
