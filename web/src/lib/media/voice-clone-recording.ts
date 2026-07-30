import { fetchFile } from "@ffmpeg/util";

import { runFFmpegTask } from "@/lib/media/ffmpeg-runtime";

const preferredRecordingMimeTypes = ["audio/webm;codecs=opus", "audio/webm", "audio/mp4"] as const;

export function resolveRecordingMimeType() {
    if (typeof MediaRecorder === "undefined") return "";
    return preferredRecordingMimeTypes.find((mimeType) => MediaRecorder.isTypeSupported(mimeType)) || "";
}

export async function convertVoiceCloneRecording(blob: Blob, onPreparing?: () => void): Promise<File> {
    if (blob.size === 0) throw new Error("录音文件为空，请重新录制");
    if (blob.type.includes("mp4")) {
        return new File([blob], "voice-clone-input.m4a", { type: "audio/mp4", lastModified: Date.now() });
    }
    if (blob.type.includes("wav")) {
        return new File([blob], "voice-clone-input.wav", { type: "audio/wav", lastModified: Date.now() });
    }
    if (!blob.type.includes("webm")) {
        throw new Error(`浏览器生成了不支持的录音格式：${blob.type || "未知格式"}`);
    }
    return runFFmpegTask(async (ffmpeg) => {
        const taskID = crypto.randomUUID();
        const inputName = `voice-clone-${taskID}.webm`;
        const outputName = `voice-clone-${taskID}.wav`;
        try {
            await ffmpeg.writeFile(inputName, await fetchFile(blob));
            const exitCode = await ffmpeg.exec(["-i", inputName, "-vn", "-ac", "1", "-ar", "44100", outputName]);
            if (exitCode !== 0) throw new Error("录音转码失败，请重新录制");
            const output = await ffmpeg.readFile(outputName);
            return new File([output as BlobPart], "voice-clone-input.wav", { type: "audio/wav", lastModified: Date.now() });
        } finally {
            await Promise.all([inputName, outputName].map((name) => ffmpeg.deleteFile(name).catch(() => undefined)));
        }
    }, onPreparing);
}
