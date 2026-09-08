const VIDEO_FRAME_TIMEOUT_MS = 20_000;
const LAST_FRAME_EPSILON_SECONDS = 0.001;

export async function captureVideoLastFrame(source: Blob | string) {
    return captureVideoSourceFrame(source, { kind: "last" });
}

export async function captureVideoFrameAtTime(source: Blob | string, seconds: number) {
    if (!Number.isFinite(seconds) || seconds < 0) throw new Error("当前视频时间无效");
    return captureVideoSourceFrame(source, { kind: "time", seconds });
}

type VideoFrameTarget =
    | { kind: "last" }
    | { kind: "time"; seconds: number };

async function captureVideoSourceFrame(source: Blob | string, target: VideoFrameTarget) {
    const blob = await readVideoBlob(source);
    const objectUrl = URL.createObjectURL(blob);
    const video = document.createElement("video");
    video.muted = true;
    video.playsInline = true;
    video.preload = "auto";
    const isCurrentFrame = target.kind === "time";

    try {
        const loaded = waitForVideoEvent(video, "loadeddata", isCurrentFrame ? "当前视频帧读取失败" : "视频读取超时或编码不受浏览器支持");
        video.src = objectUrl;
        video.load();
        await loaded;

        if (!Number.isFinite(video.duration) || video.duration <= 0) throw new Error(isCurrentFrame ? "当前视频时长无效" : "无法确定视频时长");
        const targetTime = target.kind === "last" ? Math.max(0, video.duration - LAST_FRAME_EPSILON_SECONDS) : Math.min(target.seconds, video.duration);
        if (targetTime > 0) {
            const seeked = waitForVideoEvent(video, "seeked", isCurrentFrame ? "无法定位到当前视频帧" : "无法定位到视频最后一帧");
            video.currentTime = targetTime;
            await seeked;
        }

        return captureVideoElementFrame(
            video,
            isCurrentFrame ? "当前视频帧尚未就绪" : "无法读取视频画面尺寸",
            isCurrentFrame ? "当前视频帧编码失败" : "尾帧图片编码失败",
        );
    } finally {
        video.pause();
        video.removeAttribute("src");
        video.load();
        URL.revokeObjectURL(objectUrl);
    }
}

async function readVideoBlob(source: Blob | string) {
    if (source instanceof Blob) return source;
    try {
        const response = await fetch(source);
        if (!response.ok) throw new Error(String(response.status));
        return await response.blob();
    } catch {
        throw new Error("无法读取视频文件，请重新上传视频后再截取尾帧");
    }
}

function waitForVideoEvent(video: HTMLVideoElement, eventName: "loadeddata" | "seeked", errorMessage: string) {
    return new Promise<void>((resolve, reject) => {
        let timer = 0;
        const cleanup = () => {
            window.clearTimeout(timer);
            video.removeEventListener(eventName, onSuccess);
            video.removeEventListener("error", onError);
        };
        const onSuccess = () => {
            cleanup();
            resolve();
        };
        const onError = () => {
            cleanup();
            reject(new Error(errorMessage));
        };
        video.addEventListener(eventName, onSuccess, { once: true });
        video.addEventListener("error", onError, { once: true });
        timer = window.setTimeout(onError, VIDEO_FRAME_TIMEOUT_MS);
    });
}

function canvasToPngBlob(canvas: HTMLCanvasElement, encodingErrorMessage: string) {
    return new Promise<Blob>((resolve, reject) => canvas.toBlob((blob) => (blob ? resolve(blob) : reject(new Error(encodingErrorMessage))), "image/png"));
}

function captureVideoElementFrame(video: HTMLVideoElement, missingFrameMessage: string, encodingErrorMessage: string) {
    if (!video.videoWidth || !video.videoHeight) throw new Error(missingFrameMessage);
    const canvas = document.createElement("canvas");
    canvas.width = video.videoWidth;
    canvas.height = video.videoHeight;
    const context = canvas.getContext("2d");
    if (!context) throw new Error("浏览器无法创建图片画布");
    context.drawImage(video, 0, 0, canvas.width, canvas.height);
    return canvasToPngBlob(canvas, encodingErrorMessage);
}
