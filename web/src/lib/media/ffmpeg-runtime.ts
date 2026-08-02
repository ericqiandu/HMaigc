import { FFmpeg } from "@ffmpeg/ffmpeg";
import ffmpegCoreURL from "@ffmpeg/core?url";
import ffmpegWasmURL from "@ffmpeg/core/wasm?url";

let ffmpegPromise: Promise<FFmpeg> | null = null;
let ffmpegQueue: Promise<void> = Promise.resolve();

async function loadFFmpeg(onLoadStart?: () => void) {
    if (!ffmpegPromise) {
        ffmpegPromise = (async () => {
            onLoadStart?.();
            const ffmpeg = new FFmpeg();
            await ffmpeg.load({
                coreURL: ffmpegCoreURL,
                wasmURL: ffmpegWasmURL,
            });
            return ffmpeg;
        })();
    }
    try {
        return await ffmpegPromise;
    } catch (error) {
        ffmpegPromise = null;
        throw error;
    }
}

// FFmpeg WASM 的文件系统和执行器是共享实例；任务必须串行，避免录音转码与视频合并互相覆盖文件。
export function runFFmpegTask<T>(task: (ffmpeg: FFmpeg) => Promise<T>, onLoadStart?: () => void): Promise<T> {
    const result = ffmpegQueue.then(
        async () => task(await loadFFmpeg(onLoadStart)),
        async () => task(await loadFFmpeg(onLoadStart)),
    );
    ffmpegQueue = result.then(
        () => undefined,
        () => undefined,
    );
    return result;
}
