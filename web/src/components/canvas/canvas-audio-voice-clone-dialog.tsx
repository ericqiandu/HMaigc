import { useEffect, useRef, useState, type CSSProperties } from "react";
import { LoaderCircle, Mic, RefreshCw, Square, X } from "lucide-react";
import { Checkbox, Modal } from "antd";

import { canvasThemes } from "@/lib/canvas-theme";
import { convertVoiceCloneRecording, resolveRecordingMimeType } from "@/lib/media/voice-clone-recording";
import { cloneUserChannelVoice } from "@/services/api/voices";
import type { ChannelVoice } from "@/stores/use-config-store";
import { useThemeStore } from "@/stores/use-theme-store";

const minimumRecordingSeconds = 10;
const maximumRecordingSeconds = 300;
const readingScripts = [
    "欢迎收听今天的节目。我们常说灵感很重要，但真正让灵感变成作品的，往往是清晰的方法和持续的修改。一个想法刚出现时可能很模糊，只要把它写下来，拆成几个步骤，再一点点完善，它就会慢慢变成可以被看见、被听见的内容。",
    "清晨的风从窗边吹进来，街道还没有完全醒来。远处传来车辆经过的声音，桌上的咖啡散发着温暖的香气。我打开新的文档，把昨晚想到的故事认真记录下来，希望今天能让这个故事拥有更清楚的节奏和更真实的情绪。",
    "每一段声音都有自己的温度。说话时不需要刻意夸张，只要保持自然、清晰和稳定，让语速稍微慢一些，在句子之间留出恰当的停顿。这样录制出来的样本，更能准确保留你的音色、语气和表达习惯。",
] as const;

type RecordingState = "idle" | "recording" | "processing" | "ready" | "submitting";

type CanvasAudioVoiceCloneDialogProps = {
    open: boolean;
    channelId: string;
    onCancel: () => void;
    onCreated: (voice: ChannelVoice) => void | Promise<void>;
};

function formatRecordingTime(seconds: number) {
    const minutes = Math.floor(seconds / 60)
        .toString()
        .padStart(2, "0");
    const remainder = (seconds % 60).toString().padStart(2, "0");
    return `${minutes}:${remainder}`;
}

export function CanvasAudioVoiceCloneDialog({ open, channelId, onCancel, onCreated }: CanvasAudioVoiceCloneDialogProps) {
    const themeName = useThemeStore((state) => state.theme);
    const theme = canvasThemes[themeName];
    const [scriptIndex, setScriptIndex] = useState(0);
    const [state, setState] = useState<RecordingState>("idle");
    const [elapsedSeconds, setElapsedSeconds] = useState(0);
    const [recordingFile, setRecordingFile] = useState<File | null>(null);
    const [consentConfirmed, setConsentConfirmed] = useState(false);
    const [error, setError] = useState("");
    const recorderRef = useRef<MediaRecorder | null>(null);
    const streamRef = useRef<MediaStream | null>(null);
    const chunksRef = useRef<Blob[]>([]);
    const startedAtRef = useRef(0);
    const discardOnStopRef = useRef(false);
    const timerRef = useRef<number | null>(null);

    const dialogStyle = {
        "--audio-voice-surface": theme.spatial.elevated,
        "--audio-voice-fill": theme.node.fill,
        "--audio-voice-text": theme.node.text,
        "--audio-voice-muted": theme.node.muted,
        "--audio-voice-stroke": theme.node.stroke,
        "--audio-voice-accent": themeName === "dark" ? "#2997ff" : "#0071e3",
    } as CSSProperties;

    function stopTimer() {
        if (timerRef.current !== null) {
            window.clearInterval(timerRef.current);
            timerRef.current = null;
        }
    }

    function releaseMicrophone() {
        streamRef.current?.getTracks().forEach((track) => track.stop());
        streamRef.current = null;
    }

    function resetRecording() {
        stopTimer();
        releaseMicrophone();
        recorderRef.current = null;
        chunksRef.current = [];
        startedAtRef.current = 0;
        discardOnStopRef.current = false;
        setState("idle");
        setElapsedSeconds(0);
        setRecordingFile(null);
        setConsentConfirmed(false);
        setError("");
    }

    useEffect(() => {
        if (open) return;
        const recorder = recorderRef.current;
        if (recorder?.state === "recording") {
            discardOnStopRef.current = true;
            recorder.stop();
        }
        resetRecording();
    }, [open]);

    useEffect(
        () => () => {
            const recorder = recorderRef.current;
            if (recorder?.state === "recording") {
                discardOnStopRef.current = true;
                recorder.stop();
            }
            stopTimer();
            releaseMicrophone();
        },
        [],
    );

    async function startRecording() {
        setError("");
        setRecordingFile(null);
        if (!navigator.mediaDevices?.getUserMedia) {
            setError("当前浏览器不支持麦克风录音，请使用最新版 Chrome、Edge 或 Safari");
            return;
        }
        const mimeType = resolveRecordingMimeType();
        if (!mimeType) {
            setError("当前浏览器不支持可转换的录音格式");
            return;
        }
        try {
            const stream = await navigator.mediaDevices.getUserMedia({ audio: { echoCancellation: true, noiseSuppression: true } });
            const recorder = new MediaRecorder(stream, { mimeType });
            streamRef.current = stream;
            recorderRef.current = recorder;
            chunksRef.current = [];
            discardOnStopRef.current = false;
            recorder.ondataavailable = (event) => {
                if (event.data.size > 0) chunksRef.current.push(event.data);
            };
            recorder.onerror = () => {
                stopTimer();
                releaseMicrophone();
                recorderRef.current = null;
                setState("idle");
                setError("录音过程中发生错误，请检查麦克风后重试");
            };
            recorder.onstop = () => {
                stopTimer();
                releaseMicrophone();
                recorderRef.current = null;
                if (discardOnStopRef.current) return;
                const durationSeconds = Math.round((Date.now() - startedAtRef.current) / 1000);
                if (durationSeconds < minimumRecordingSeconds) {
                    setState("idle");
                    setRecordingFile(null);
                    setError(`录音至少需要 ${minimumRecordingSeconds} 秒，当前为 ${durationSeconds} 秒`);
                    return;
                }
                const blob = new Blob(chunksRef.current, { type: mimeType });
                void convertVoiceCloneRecording(blob, () => setState("processing"))
                    .then((file) => {
                        setRecordingFile(file);
                        setState("ready");
                    })
                    .catch((conversionError: unknown) => {
                        setRecordingFile(null);
                        setState("idle");
                        setError(conversionError instanceof Error ? conversionError.message : "录音处理失败");
                    });
            };
            recorder.start(1000);
            startedAtRef.current = Date.now();
            setElapsedSeconds(0);
            setState("recording");
            timerRef.current = window.setInterval(() => {
                const seconds = Math.min(maximumRecordingSeconds, Math.floor((Date.now() - startedAtRef.current) / 1000));
                setElapsedSeconds(seconds);
                if (seconds >= maximumRecordingSeconds && recorder.state === "recording") {
                    setState("processing");
                    recorder.stop();
                }
            }, 250);
        } catch (permissionError) {
            setError(permissionError instanceof Error ? `无法使用麦克风：${permissionError.message}` : "无法使用麦克风");
            releaseMicrophone();
        }
    }

    function stopRecording() {
        const recorder = recorderRef.current;
        if (recorder?.state !== "recording") return;
        setState("processing");
        recorder.stop();
    }

    function handleCancel() {
        if (state === "submitting") return;
        const recorder = recorderRef.current;
        if (recorder?.state === "recording") {
            discardOnStopRef.current = true;
            recorder.stop();
        }
        resetRecording();
        onCancel();
    }

    async function createVoice() {
        if (!recordingFile) {
            setError("请先完成至少 10 秒的录音");
            return;
        }
        if (!consentConfirmed) {
            setError("请确认已获得声音本人授权并同意音色克隆规则");
            return;
        }
        setError("");
        setState("submitting");
        try {
            const result = await cloneUserChannelVoice(channelId, {
                file: recordingFile,
                language: "Chinese",
                consentConfirmed: true,
                idempotencyKey: crypto.randomUUID(),
            });
            await onCreated(result.voice);
            resetRecording();
            onCancel();
        } catch (submitError) {
            setState("ready");
            setError(submitError instanceof Error ? submitError.message : "克隆音色失败");
        }
    }

    const recording = state === "recording";
    const busy = state === "processing" || state === "submitting";
    const statusText = recording ? `录音中 ${formatRecordingTime(elapsedSeconds)}` : state === "processing" ? "正在处理录音" : state === "ready" ? `录音已完成 ${formatRecordingTime(elapsedSeconds)}` : "开始录音即表示您已获得声音授权";

    return (
        <Modal
            className="canvas-audio-voice-clone-modal"
            open={open}
            title={null}
            footer={null}
            centered
            width={620}
            destroyOnHidden
            closeIcon={null}
            maskClosable={!busy}
            keyboard={!busy}
            onCancel={handleCancel}
            styles={{
                container: {
                    padding: 0,
                    overflow: "hidden",
                    borderRadius: 12,
                    background: theme.spatial.elevated,
                    color: theme.node.text,
                },
                body: { padding: 0 },
                mask: { backdropFilter: "blur(6px)" },
            }}
        >
            <section className="canvas-audio-voice-clone-dialog" style={dialogStyle} aria-labelledby="canvas-audio-voice-clone-title">
                <header className="canvas-audio-voice-clone-header">
                    <h2 id="canvas-audio-voice-clone-title" className="canvas-audio-voice-clone-title">
                        克隆新音色
                    </h2>
                    <button type="button" className="canvas-audio-voice-dialog-close" onClick={handleCancel} disabled={busy} aria-label="关闭克隆音色">
                        <X className="canvas-audio-voice-dialog-close-icon size-4" />
                    </button>
                </header>
                <div className="canvas-audio-voice-clone-content">
                    <div className="canvas-audio-voice-clone-intro">
                        <p className="canvas-audio-voice-clone-description">朗读一段文字，即可克隆你的专属声音</p>
                        <button type="button" className="canvas-audio-voice-clone-refresh" disabled={recording || busy} onClick={() => setScriptIndex((current) => (current + 1) % readingScripts.length)}>
                            <RefreshCw className="canvas-audio-voice-clone-refresh-icon size-4" />
                            文本刷新
                        </button>
                    </div>
                    <div className="canvas-audio-voice-clone-recorder">
                        <p className="canvas-audio-voice-clone-script">
                            <span className="canvas-audio-voice-clone-script-label">需阅读内容：</span>
                            {readingScripts[scriptIndex]}
                        </p>
                        <button
                            type="button"
                            className={`canvas-audio-voice-clone-record-button ${recording ? "canvas-audio-voice-clone-record-button--recording" : ""}`}
                            disabled={busy}
                            onClick={recording ? stopRecording : () => void startRecording()}
                            aria-label={recording ? "停止录音" : "开始录音"}
                        >
                            {state === "processing" ? (
                                <LoaderCircle className="canvas-audio-voice-clone-record-icon size-7 animate-spin" />
                            ) : recording ? (
                                <Square className="canvas-audio-voice-clone-record-icon size-6 fill-current" />
                            ) : (
                                <Mic className="canvas-audio-voice-clone-record-icon size-8" />
                            )}
                            <span className="canvas-audio-voice-clone-record-status">{statusText}</span>
                        </button>
                    </div>
                    {error ? (
                        <div className="canvas-audio-voice-clone-error" role="alert">
                            {error}
                        </div>
                    ) : null}
                    <Checkbox className="canvas-audio-voice-clone-consent" checked={consentConfirmed} disabled={state === "submitting"} onChange={(event) => setConsentConfirmed(event.target.checked)}>
                        我已阅读并同意《声音克隆功能使用规则》；我确认上传的声音样本具有充分、合法、必要的权利或授权，并同意将其用于本次音色克隆。
                    </Checkbox>
                    <footer className="canvas-audio-voice-clone-footer">
                        <span className="canvas-audio-voice-clone-limit">录音需为 10 秒至 5 分钟，生成后可在“我的音色”中使用</span>
                        <button type="button" className="canvas-audio-voice-clone-submit" disabled={!recordingFile || !consentConfirmed || state !== "ready"} onClick={() => void createVoice()}>
                            {state === "submitting" ? <LoaderCircle className="canvas-audio-voice-clone-submit-icon size-4 animate-spin" /> : null}
                            生成音色
                        </button>
                    </footer>
                </div>
            </section>
        </Modal>
    );
}
