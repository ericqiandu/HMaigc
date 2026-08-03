import { describe, expect, test } from "bun:test";

import { runVideoOutputBatch } from "../src/lib/canvas/video-output-batch";

describe("视频多任务批次", () => {
    test("同时启动全部输出任务而不是串行等待", async () => {
        const started: string[] = [];
        let release = () => undefined;
        const gate = new Promise<void>((resolve) => {
            release = resolve;
        });

        const pending = runVideoOutputBatch(["a", "b", "c", "d"], async (id) => {
            started.push(id);
            await gate;
            return id;
        });

        await Promise.resolve();
        expect(started).toEqual(["a", "b", "c", "d"]);
        release();
        expect(await pending).toEqual({ succeeded: ["a", "b", "c", "d"], failed: [] });
    });

    test("部分失败时保留成功项并明确返回失败项", async () => {
        const result = await runVideoOutputBatch(["a", "b", "c"], async (id) => {
            if (id === "b") throw new Error("upstream failed");
            return id;
        });

        expect(result.succeeded).toEqual(["a", "c"]);
        expect(result.failed.map((item) => ({ id: item.id, message: item.error.message }))).toEqual([{ id: "b", message: "upstream failed" }]);
    });
});
