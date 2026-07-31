export type TextRange = {
    start: number;
    end: number;
};

export type AudioPauseInputResult =
    | {
          ok: true;
          seconds: number;
          token: string;
      }
    | {
          ok: false;
          message: string;
      };

export const audioPauseMinimumSeconds = 0.01;
export const audioPauseMaximumSeconds = 99.99;
export const defaultAudioPauseToken = "<#0.25#>";

const audioPauseTokenPattern = /^<#(\d+(?:\.\d{1,2})?)#>$/;
const audioPauseInputPattern = /^\d+(?:\.\d{1,2})?$/;

export function parseAudioPauseToken(token: string) {
    const match = audioPauseTokenPattern.exec(token);
    if (!match) return null;
    const seconds = Number(match[1]);
    return Number.isFinite(seconds) && seconds >= audioPauseMinimumSeconds && seconds <= audioPauseMaximumSeconds ? seconds : null;
}

export function normalizeAudioPauseInput(input: string): AudioPauseInputResult {
    const normalized = input.trim();
    if (!audioPauseInputPattern.test(normalized)) {
        return { ok: false, message: "请输入最多两位小数的秒数" };
    }
    const seconds = Number(normalized);
    if (!Number.isFinite(seconds) || seconds < audioPauseMinimumSeconds || seconds > audioPauseMaximumSeconds) {
        return { ok: false, message: "停顿时长需为 0.01–99.99 秒" };
    }
    const serializedSeconds = seconds.toFixed(2).replace(/\.?0+$/, "");
    return {
        ok: true,
        seconds,
        token: `<#${serializedSeconds}#>`,
    };
}

export function replaceTextRange(value: string, range: TextRange, replacement: string) {
    const start = Math.max(0, Math.min(value.length, range.start));
    const end = Math.max(start, Math.min(value.length, range.end));
    return {
        value: `${value.slice(0, start)}${replacement}${value.slice(end)}`,
        range: {
            start,
            end: start + replacement.length,
        },
    };
}
