import { useMemo, type CSSProperties } from "react";
import { Image as ImageIcon, MessageSquare, Music2, Settings2, Video } from "lucide-react";
import { Segmented, Select } from "antd";

import { ModelPicker } from "@/components/model-picker";
import { configuredModelMatchesCapability, defaultConfig, useEffectiveConfig, type AiConfig } from "@/stores/use-config-store";
import { useCanvasTaskBillingQuote } from "@/hooks/use-canvas-task-billing-quote";
import type { TaskBillingQuote } from "@/services/api/task-center";
import { canvasThemes } from "@/lib/canvas-theme";
import { hasPublishedVideoModel, normalizeVideoConfigForModel, videoModelMetadataPatch } from "@/lib/video-model-capabilities";
import { handleMissingSystemModel } from "@/lib/settings-navigation";
import { useThemeStore } from "@/stores/use-theme-store";
import { CanvasImageSettingsPopover } from "./canvas-image-settings-popover";
import { findImageModelCapabilities, imageModelMetadataPatch, normalizeImageConfigForModel } from "@/lib/image-model-capabilities";
import { CanvasAudioSettingsPopover, type CanvasAudioSettingKey } from "./canvas-audio-settings-popover";
import { CanvasAudioVoicePicker } from "./canvas-audio-voice-picker";
import { CanvasVideoSettingsPopover } from "./canvas-video-settings-popover";
import type { CanvasGenerationMode, CanvasNodeData, CanvasNodeMetadata, CanvasVideoEditOperation, CanvasWorkspaceMode } from "@/types/canvas";
import { resolveVideoGenerationMode } from "@/lib/canvas/canvas-video-generation-mode";
import { GenerationCreditQuoteBadge } from "./generation-credit-quote-badge";
import { CanvasSubmitButton } from "./canvas-submit-button";

type CanvasConfigNodePanelProps = {
    projectId: string;
    node: CanvasNodeData;
    isRunning: boolean;
    inputSummary: { textCount: number; imageCount: number; videoCount: number; audioCount: number };
    onConfigChange: (nodeId: string, patch: Partial<CanvasNodeMetadata>) => void;
    onGenerate: (nodeId: string, expectedQuote?: TaskBillingQuote) => void;
    onStop: (nodeId: string) => void;
    onComposerToggle: () => void;
    workspaceMode?: CanvasWorkspaceMode;
};

const videoOperationOptions: Array<{ label: string; value: CanvasVideoEditOperation }> = [
    { label: "文生视频", value: "text_to_video" },
    { label: "图生视频", value: "image_to_video" },
    { label: "视频续写", value: "extend" },
    { label: "局部修改", value: "inpaint" },
    { label: "元素替换", value: "replace_element" },
    { label: "运镜调整", value: "camera_motion" },
    { label: "风格迁移", value: "style_transfer" },
    { label: "音频生视频", value: "audio_to_video" },
    { label: "版本对比", value: "compare_versions" },
];

export function CanvasConfigNodePanel({ projectId, node, isRunning, inputSummary, onConfigChange, onGenerate, onStop, onComposerToggle, workspaceMode = "professional" }: CanvasConfigNodePanelProps) {
    const globalConfig = useEffectiveConfig();
    const theme = canvasThemes[useThemeStore((state) => state.theme)];
    const mode = node.metadata?.generationMode || "image";
    const simpleMode = workspaceMode === "simple";
    const config = buildNodeConfig(globalConfig, node, mode);
    const videoModelPublished = mode === "video" && hasPublishedVideoModel(config);
    const effectiveVideoConfig = videoModelPublished ? normalizeVideoConfigForModel(config, resolveVideoGenerationMode(node.metadata)) : null;
    const operationOptions = node.metadata?.videoEditOperation === "concat" ? [...videoOperationOptions, { label: "合并成片", value: "concat" as const }] : videoOperationOptions;
    const quoteConfig = useMemo(() => (mode === "image" && findImageModelCapabilities(config) ? normalizeImageConfigForModel(config) : effectiveVideoConfig || config), [config, effectiveVideoConfig, mode]);
    const count = Math.max(1, Math.min(15, Math.floor(Math.abs(Number(quoteConfig.count)) || 1)));
    const quoteReferenceVideoCount = mode === "video" && resolveVideoGenerationMode(node.metadata) === "omni_reference" ? inputSummary.videoCount : 0;
    const quoteState = useCanvasTaskBillingQuote(projectId, quoteConfig, mode, mode === "video" ? node.metadata?.videoEditOperation || defaultVideoOperation(inputSummary) : mode, count, {
        referenceImageCount: inputSummary.imageCount,
        referenceVideoCount: quoteReferenceVideoCount,
    });
    const chipStyle = { background: theme.node.fill, borderColor: theme.node.stroke, color: theme.node.text };
    const hasAnyInput = Boolean(inputSummary.textCount || inputSummary.imageCount || inputSummary.videoCount || inputSummary.audioCount);
    const hasComposerContent = Boolean((node.metadata?.composerContent ?? node.metadata?.prompt ?? "").trim());
    const canGenerate = hasComposerContent || (mode === "audio" ? inputSummary.textCount > 0 : hasAnyInput);

    return (
        <div className="flex h-full w-full cursor-move flex-col px-3 pb-3 pt-7 text-sm" style={{ color: theme.node.text }} onWheel={(event) => event.stopPropagation()}>
            <div className="mb-2 flex items-center justify-between gap-3">
                <div className="canvas-node-title shrink-0">{simpleMode ? "快速生成" : "生成配置"}</div>
                {simpleMode ? (
                    <span className="rounded-md px-2 py-1 text-[10px]" style={{ background: theme.node.fill, color: theme.node.muted }}>
                        自动配置
                    </span>
                ) : (
                    <div className="cursor-default" onMouseDown={(event) => event.stopPropagation()}>
                        <Segmented
                            size="small"
                            className="canvas-config-mode !rounded-md !p-0.5"
                            value={mode}
                            onChange={(value) => onConfigChange(node.id, { generationMode: value as CanvasGenerationMode })}
                            options={[
                                {
                                    value: "image",
                                    label: (
                                        <span className="inline-flex items-center gap-1">
                                            <ImageIcon className="size-3.5" />
                                            生图
                                        </span>
                                    ),
                                },
                                {
                                    value: "text",
                                    label: (
                                        <span className="inline-flex items-center gap-1">
                                            <MessageSquare className="size-3.5" />
                                            文本
                                        </span>
                                    ),
                                },
                                {
                                    value: "video",
                                    label: (
                                        <span className="inline-flex items-center gap-1">
                                            <Video className="size-3.5" />
                                            视频
                                        </span>
                                    ),
                                },
                                {
                                    value: "audio",
                                    label: (
                                        <span className="inline-flex items-center gap-1">
                                            <Music2 className="size-3.5" />
                                            音频
                                        </span>
                                    ),
                                },
                            ]}
                        />
                    </div>
                )}
            </div>

            <div className="mb-2 flex flex-wrap gap-1.5">
                <InputChip label="提示词" value={`${inputSummary.textCount} 个`} style={chipStyle} />
                <InputChip label="参考图" value={`${inputSummary.imageCount} 张`} style={chipStyle} />
                <InputChip label="参考视频" value={`${inputSummary.videoCount} 个`} style={chipStyle} />
                <InputChip label="参考音频" value={`${inputSummary.audioCount} 个`} style={chipStyle} />
                <button type="button" className="inline-flex h-7 cursor-pointer items-center gap-1 rounded-md border px-2 text-[11px]" style={chipStyle} onMouseDown={(event) => event.stopPropagation()} onClick={onComposerToggle}>
                    {simpleMode ? <MessageSquare className="size-3.5" /> : <Settings2 className="size-3.5" />}
                    {simpleMode ? "编辑生成内容" : "组装提示词"}
                </button>
            </div>

            {mode === "video" && !simpleMode ? (
                <div className="mb-2 cursor-default" data-canvas-no-zoom onMouseDown={(event) => event.stopPropagation()} onPointerDown={(event) => event.stopPropagation()}>
                    <Select
                        size="small"
                        className="canvas-compact-control canvas-control-select !h-9 !w-full"
                        value={node.metadata?.videoEditOperation || defaultVideoOperation(inputSummary)}
                        options={operationOptions}
                        placement="bottomLeft"
                        popupMatchSelectWidth={false}
                        styles={{ popup: { root: { minWidth: 180, maxWidth: 260 } } }}
                        popupRender={(menu) => (
                            <div data-canvas-no-zoom onMouseDown={(event) => event.stopPropagation()} onPointerDown={(event) => event.stopPropagation()}>
                                {menu}
                            </div>
                        )}
                        onChange={(value) => onConfigChange(node.id, { videoEditOperation: value })}
                    />
                </div>
            ) : null}

            {simpleMode ? (
                <div className="mb-2 rounded-lg px-2 py-2 text-[11px]" style={{ background: theme.node.fill, color: theme.node.muted }}>
                    将使用当前默认模型与生成参数
                </div>
            ) : (
                <div
                    className={`mb-2 grid min-w-0 cursor-default items-center gap-2 ${mode === "audio" ? "grid-cols-[minmax(0,1fr)_132px_40px]" : mode === "image" || mode === "video" ? "grid-cols-[minmax(0,1fr)_148px]" : "grid-cols-1"}`}
                    onMouseDown={(event) => event.stopPropagation()}
                >
                    <ModelPicker
                        className="canvas-compact-control h-10"
                        config={config}
                        value={config.model}
                        onChange={(model) => onConfigChange(node.id, mode === "video" ? videoModelMetadataPatch(config, model, resolveVideoGenerationMode(node.metadata)) : mode === "image" ? imageModelMetadataPatch(config, model) : { model })}
                        capability={mode}
                        onMissingConfig={handleMissingSystemModel}
                        presentation={mode === "image" || mode === "video" || mode === "audio" ? "canvasMedia" : "default"}
                    />
                    {mode === "video" && videoModelPublished ? (
                        <CanvasVideoSettingsPopover
                            config={config}
                            generationMode={resolveVideoGenerationMode(node.metadata)}
                            placement="topRight"
                            buttonClassName="canvas-compact-control !h-10 !w-full !justify-start !rounded-lg !px-2"
                            onConfigChange={(key, value) => onConfigChange(node.id, videoConfigPatch(key, value))}
                        />
                    ) : mode === "image" ? (
                        <CanvasImageSettingsPopover
                            config={config}
                            placement="topRight"
                            autoAdjustOverflow={false}
                            buttonClassName="canvas-compact-control !h-10 !w-full !justify-start !rounded-lg !px-2"
                            onConfigChange={(key, value) => onConfigChange(node.id, key === "count" ? { count: Number(value) || 1 } : { [key]: value })}
                        />
                    ) : mode === "audio" ? (
                        <>
                            <CanvasAudioVoicePicker config={config} value={config.audioVoice} className="canvas-compact-control h-10 w-full rounded-lg px-2" onChange={(audioVoice) => onConfigChange(node.id, { audioVoice })} />
                            <CanvasAudioSettingsPopover
                                config={config}
                                placement="topRight"
                                iconOnly
                                buttonClassName="canvas-compact-control !h-10 !w-10 !justify-center !rounded-lg !px-0"
                                onConfigChange={(key, value) => onConfigChange(node.id, audioConfigPatch(key, value))}
                            />
                        </>
                    ) : null}
                </div>
            )}

            <div className="canvas-config-submit-row mt-auto flex items-center justify-end gap-1.5" onMouseDown={(event) => event.stopPropagation()}>
                {isRunning ? null : <GenerationCreditQuoteBadge state={quoteState} />}
                <CanvasSubmitButton
                    state={isRunning ? "stop" : "ready"}
                    disabled={!isRunning && (!canGenerate || ((mode === "image" || mode === "video") && quoteState.status !== "ready"))}
                    ariaLabel={isRunning ? "停止生成" : "生成"}
                    onClick={() => (isRunning ? onStop(node.id) : onGenerate(node.id, quoteState.status === "ready" ? quoteState.quote : undefined))}
                />
            </div>
        </div>
    );
}

function defaultVideoOperation(inputSummary: CanvasConfigNodePanelProps["inputSummary"]): CanvasVideoEditOperation {
    if (inputSummary.audioCount > 0 && inputSummary.imageCount === 0 && inputSummary.videoCount === 0) return "audio_to_video";
    if (inputSummary.videoCount > 0) return "extend";
    if (inputSummary.imageCount > 0) return "image_to_video";
    return "image_to_video";
}

function InputChip({ label, value, style }: { label: string; value: string; style: CSSProperties }) {
    return (
        <div className="inline-flex h-7 items-center gap-1 rounded-md border px-2 text-[11px]" style={style}>
            <span>{label}</span>
            <span className="font-medium">{value}</span>
        </div>
    );
}

function buildNodeConfig(globalConfig: AiConfig, node: CanvasNodeData, mode: CanvasGenerationMode): AiConfig {
    const defaultModel = mode === "image" ? globalConfig.imageModel : mode === "video" ? globalConfig.videoModel : mode === "audio" ? globalConfig.audioModel : globalConfig.textModel;
    const fallbackModel = mode === "image" ? defaultConfig.imageModel : mode === "video" ? defaultConfig.videoModel : mode === "audio" ? defaultConfig.audioModel : defaultConfig.textModel;
    const storedModel = node.metadata?.model;
    const model = storedModel && configuredModelMatchesCapability(globalConfig, storedModel, mode) ? storedModel : defaultModel && configuredModelMatchesCapability(globalConfig, defaultModel, mode) ? defaultModel : fallbackModel;
    const config: AiConfig = {
        ...globalConfig,
        model,
        quality: node.metadata?.quality || globalConfig.quality || defaultConfig.quality,
        size: node.metadata?.size || globalConfig.size || defaultConfig.size,
        transparentBackground: (node.metadata?.transparentBackground || globalConfig.transparentBackground) === "true" ? "true" : "false",
        videoSeconds: node.metadata?.seconds || globalConfig.videoSeconds || defaultConfig.videoSeconds,
        vquality: node.metadata?.vquality || globalConfig.vquality || defaultConfig.vquality,
        videoGenerateAudio: node.metadata?.generateAudio || globalConfig.videoGenerateAudio || defaultConfig.videoGenerateAudio,
        videoSuperResolutionEnabled: node.metadata?.superResolutionEnabled || globalConfig.videoSuperResolutionEnabled || defaultConfig.videoSuperResolutionEnabled,
        videoSuperResolutionResolution: node.metadata?.superResolutionResolution || globalConfig.videoSuperResolutionResolution || defaultConfig.videoSuperResolutionResolution,
        videoSuperResolutionScene: node.metadata?.superResolutionScene || globalConfig.videoSuperResolutionScene || defaultConfig.videoSuperResolutionScene,
        videoSuperResolutionVersion: node.metadata?.superResolutionVersion || globalConfig.videoSuperResolutionVersion || defaultConfig.videoSuperResolutionVersion,
        videoSuperResolutionFps: node.metadata?.superResolutionFps || globalConfig.videoSuperResolutionFps || defaultConfig.videoSuperResolutionFps,
        audioVoice: node.metadata?.audioVoice || globalConfig.audioVoice || defaultConfig.audioVoice,
        audioFormat: node.metadata?.audioFormat || globalConfig.audioFormat || defaultConfig.audioFormat,
        audioSpeed: node.metadata?.audioSpeed || globalConfig.audioSpeed || defaultConfig.audioSpeed,
        audioVolume: node.metadata?.audioVolume || globalConfig.audioVolume || defaultConfig.audioVolume,
        audioPitch: node.metadata?.audioPitch || globalConfig.audioPitch || defaultConfig.audioPitch,
        audioEmotion: node.metadata?.audioEmotion || globalConfig.audioEmotion || defaultConfig.audioEmotion,
        audioLanguageBoost: node.metadata?.audioLanguageBoost || globalConfig.audioLanguageBoost || defaultConfig.audioLanguageBoost,
        audioSampleRate: node.metadata?.audioSampleRate || globalConfig.audioSampleRate || defaultConfig.audioSampleRate,
        audioBitrate: node.metadata?.audioBitrate || globalConfig.audioBitrate || defaultConfig.audioBitrate,
        audioChannel: node.metadata?.audioChannel || globalConfig.audioChannel || defaultConfig.audioChannel,
        audioInstructions: node.metadata?.audioInstructions || globalConfig.audioInstructions || defaultConfig.audioInstructions,
        count: String(node.metadata?.count || (mode === "image" ? globalConfig.canvasImageCount || globalConfig.count : globalConfig.count) || defaultConfig.count),
    };
    return mode === "video" && hasPublishedVideoModel(config) ? normalizeVideoConfigForModel(config, resolveVideoGenerationMode(node.metadata)) : config;
}

function videoConfigPatch(key: keyof AiConfig, value: string) {
    if (key === "videoSeconds") return { seconds: value };
    if (key === "videoGenerateAudio") return { generateAudio: value };
    if (key === "videoSuperResolutionEnabled") return { superResolutionEnabled: value };
    if (key === "videoSuperResolutionResolution") return { superResolutionResolution: value };
    if (key === "videoSuperResolutionScene") return { superResolutionScene: value };
    if (key === "videoSuperResolutionVersion") return { superResolutionVersion: value };
    if (key === "videoSuperResolutionFps") return { superResolutionFps: value };
    return { [key]: value };
}

function audioConfigPatch(key: CanvasAudioSettingKey, value: string) {
    return { [key]: value };
}
