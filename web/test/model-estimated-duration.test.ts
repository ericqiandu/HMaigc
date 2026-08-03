import { describe, expect, test } from "bun:test";

import { formatModelEstimatedDuration } from "../src/lib/model-estimated-duration";

describe("formatModelEstimatedDuration", () => {
    test("按分钟向上取整展示模型预计耗时", () => {
        expect(formatModelEstimatedDuration(120)).toBe("2min");
        expect(formatModelEstimatedDuration(121)).toBe("3min");
    });

    test("未配置或非法耗时不展示", () => {
        expect(formatModelEstimatedDuration(undefined)).toBeNull();
        expect(formatModelEstimatedDuration(0)).toBeNull();
        expect(formatModelEstimatedDuration(Number.NaN)).toBeNull();
    });
});
