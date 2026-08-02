import { fetchFile } from "@ffmpeg/util";
import { runFFmpegTask } from "@/lib/media/ffmpeg-runtime";
import { getMediaBlob } from "@/services/file-storage";

export type MergeVideoInput = { id: string; url?: string; storageKey?: string; trimStartMs?: number; trimEndMs?: number };
export type MergeVideoProgress = { phase: "loading" | "reading" | "encoding"; progress: number };

export async function mergeVideos(inputs: MergeVideoInput[], onProgress?: (progress: MergeVideoProgress) => void) {
    if (inputs.length < 1) throw new Error("至少需要 1 个视频片段才能渲染");
    return runFFmpegTask(
        async (ffmpeg) => {
            const taskID = crypto.randomUUID();
            const files: string[] = [];
            const concatName = `concat-${taskID}.txt`;
            const outputName = `merged-${taskID}.mp4`;
            try {
                for (let index = 0; index < inputs.length; index += 1) {
                    const input = inputs[index];
                    const storedBlob = input.storageKey ? await getMediaBlob(input.storageKey) : null;
                    const remoteBlob =
                        !storedBlob && input.url
                            ? await fetch(input.url).then((response) => {
                                  if (!response.ok) throw new Error(`视频资源请求失败（${response.status}）`);
                                  return response.blob();
                              })
                            : null;
                    const blob = storedBlob || remoteBlob;
                    if (!blob) throw new Error(`无法读取第 ${index + 1} 个视频`);
                    const sourceName = `source-${taskID}-${index}.mp4`;
                    await ffmpeg.writeFile(sourceName, await fetchFile(blob));
                    files.push(sourceName);
                    const hasTrim = (input.trimStartMs || 0) > 0 || input.trimEndMs !== undefined;
                    if (hasTrim) {
                        const clipName = `clip-${taskID}-${index}.mp4`;
                        const args = ["-ss", String(Math.max(0, input.trimStartMs || 0) / 1000), "-i", sourceName];
                        if (input.trimEndMs !== undefined) {
                            const durationMs = input.trimEndMs - Math.max(0, input.trimStartMs || 0);
                            if (durationMs <= 0) throw new Error(`第 ${index + 1} 个片段的出点必须晚于入点`);
                            args.push("-t", String(durationMs / 1000));
                        }
                        args.push("-c:v", "libx264", "-c:a", "aac", "-movflags", "+faststart", clipName);
                        const trimExitCode = await ffmpeg.exec(args);
                        if (trimExitCode !== 0) throw new Error(`第 ${index + 1} 个片段裁剪失败`);
                        files.push(clipName);
                    }
                    onProgress?.({ phase: "reading", progress: Math.round(((index + 1) / inputs.length) * 45) });
                }
                const concatFiles = inputs.map((input, index) => ((input.trimStartMs || 0) > 0 || input.trimEndMs !== undefined) ? `clip-${taskID}-${index}.mp4` : `source-${taskID}-${index}.mp4`);
                const concatList = concatFiles.map((file) => `file '${file}'`).join("\n");
                await ffmpeg.writeFile(concatName, concatList);
                onProgress?.({ phase: "encoding", progress: 55 });
                const exitCode = await ffmpeg.exec(["-f", "concat", "-safe", "0", "-i", concatName, "-c:v", "libx264", "-c:a", "aac", "-movflags", "+faststart", outputName]);
                if (exitCode !== 0) throw new Error("视频编码失败，请确认视频编码格式兼容");
                const output = await ffmpeg.readFile(outputName);
                onProgress?.({ phase: "encoding", progress: 100 });
                return new Blob([output as BlobPart], { type: "video/mp4" });
            } finally {
                await Promise.all([...files, concatName, outputName].map((file) => ffmpeg.deleteFile(file).catch(() => undefined)));
            }
        },
        () => onProgress?.({ phase: "loading", progress: 0 }),
    );
}
