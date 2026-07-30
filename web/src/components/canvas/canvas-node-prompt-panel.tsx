import { useCallback, useEffect, useMemo, useState, type ReactNode } from "react";
import { ArrowUp, AtSign, Boxes, Check, FileText, ImageIcon, ImagePlus, Maximize2, Music2, Pencil, Square, UserRound, Video } from "lucide-react";
import { Button, Modal, Popover, Tooltip } from "antd";

import { ModelPicker } from "@/components/model-picker";
import { configuredModelMatchesCapability, defaultConfig, modelOptionName, resolveModelChannel, useEffectiveConfig, type AiConfig } from "@/stores/use-config-store";
import { CreditSymbol, requestCreditCost } from "@/constant/credits";
import { canvasThemes } from "@/lib/canvas-theme";
import { miniMaxVocalTags } from "@/lib/audio-generation";
import { normalizeVideoDuration, normalizeVideoResolution } from "@/lib/video-generation-options";
import { handleMissingSystemModel } from "@/lib/settings-navigation";
import { useThemeStore } from "@/stores/use-theme-store";
import { CanvasImageSettingsPopover } from "./canvas-image-settings-popover";
import { CanvasAudioComposerControls } from "./canvas-audio-composer-controls";
import { CanvasResourceMentionTextarea } from "./canvas-resource-mention-textarea";
import { CanvasVideoSettingsPopover } from "./canvas-video-settings-popover";
import { CanvasVideoGenerationModePicker } from "./canvas-video-generation-mode-picker";
import { CanvasVideoSuperResolutionPopover } from "./canvas-video-super-resolution-popover";
import { CanvasVideoPromptTools } from "./canvas-video-prompt-tools";
import { CanvasPresetPicker, type CanvasPromptPreset } from "./canvas-preset-picker";
import { CanvasNodeType, type CanvasGenerationMode, type CanvasNodeData, type CanvasNodeMetadata, type CanvasWorkspaceMode } from "@/types/canvas";
import { canvasResourceMentionToken, type CanvasResourceReference } from "@/lib/canvas/canvas-resource-references";
import "./canvas-audio-composer.css";
import "./canvas-video-composer.css";

export type CanvasNodeGenerationMode = CanvasGenerationMode;

type CanvasNodePromptPanelProps = {
    node: CanvasNodeData;
    isRunning: boolean;
    onPromptChange: (nodeId: string, prompt: string) => void;
    onConfigChange: (nodeId: string, patch: Partial<CanvasNodeMetadata>) => void;
    onGenerate: (nodeId: string, mode: CanvasNodeGenerationMode, prompt: string) => void;
    onStop: (nodeId: string) => void;
    mentionReferences?: CanvasResourceReference[];
    availableReferences: CanvasResourceReference[];
    onReferenceConnect: (sourceNodeId: string, targetNodeId: string) => boolean;
    onImageSettingsOpenChange?: (open: boolean) => void;
    workspaceMode?: CanvasWorkspaceMode;
};

type CanvasTheme = (typeof canvasThemes)[keyof typeof canvasThemes];

export function CanvasNodePromptPanel({
    node,
    isRunning,
    onPromptChange,
    onConfigChange,
    onGenerate,
    onStop,
    mentionReferences = [],
    availableReferences,
    onReferenceConnect,
    onImageSettingsOpenChange,
    workspaceMode = "professional",
}: CanvasNodePromptPanelProps) {
    const globalConfig = useEffectiveConfig();
    const themeName = useThemeStore((state) => state.theme);
    const theme = canvasThemes[themeName];
    const simpleMode = workspaceMode === "simple";
    const mode = defaultMode(node.type);
    const isImageMode = mode === "image";
    const isVideoMode = mode === "video";
    const isAudioMode = mode === "audio";
    const config = buildNodeConfig(globalConfig, node, mode);
    const hasTextContent = node.type === CanvasNodeType.Text && Boolean(node.metadata?.content?.trim());
    const savedPrompt = node.metadata?.composerContent ?? node.metadata?.prompt ?? "";
    const [prompt, setPrompt] = useState(savedPrompt);
    const [presetOpen, setPresetOpen] = useState(false);
    const [bottomPresetOpen, setBottomPresetOpen] = useState(false);
    const [expandedPresetOpen, setExpandedPresetOpen] = useState(false);
    const [expandedPromptOpen, setExpandedPromptOpen] = useState(false);
    const [promptContentHeight, setPromptContentHeight] = useState(0);
    const generationCount = Math.max(1, Math.min(15, Math.floor(Math.abs(Number(config.count)) || 1)));
    const priceChannel = resolveModelChannel(config, config.model);
    const credits = requestCreditCost({
        channelMode: priceChannel.scope === "system" ? "remote" : "local",
        modelCosts: priceChannel.modelCosts,
        model: modelOptionName(config.model),
        count: mode === "image" || mode === "video" ? generationCount : 1,
        seconds: mode === "video" ? config.videoSeconds : 1,
        quality: config.quality,
        resolution: mode === "video" ? config.vquality : config.size,
        videoSuperResolutionEnabled: config.videoSuperResolutionEnabled === "true",
        videoSuperResolutionResolution: config.videoSuperResolutionResolution,
    });
    const activeReferenceCount = mentionReferences.filter((item) => item.active && item.kind !== "skill").length;
    const activeVideoImageNodeIds = useMemo(() => new Set(mentionReferences.filter((item) => item.active && item.kind === "image").map((item) => item.nodeId)), [mentionReferences]);
    const availableVideoImageReferences = useMemo(
        () => availableReferences.filter((item) => item.kind === "image" && item.nodeId !== node.id && Boolean(item.previewUrl)).map((item) => ({ ...item, active: activeVideoImageNodeIds.has(item.nodeId) })),
        [activeVideoImageNodeIds, availableReferences, node.id],
    );
    const videoFrameOptions = availableVideoImageReferences.map((item) => ({
        nodeId: item.nodeId,
        label: item.label,
        title: item.title,
        previewUrl: item.previewUrl,
    }));
    const activeVideoFrameNodeIds = useMemo(
        () => new Set([node.metadata?.videoStartFrameNodeId, node.metadata?.videoEndFrameNodeId].filter((value): value is string => Boolean(value))),
        [node.metadata?.videoEndFrameNodeId, node.metadata?.videoStartFrameNodeId],
    );
    const nonFrameMentionReferences = useMemo(() => mentionReferences.filter((item) => !activeVideoFrameNodeIds.has(item.nodeId)), [activeVideoFrameNodeIds, mentionReferences]);
    const composerSurface = theme.spatial.dropzone;
    const activeNonFrameReferenceCount = nonFrameMentionReferences.filter((item) => item.active && item.kind !== "skill").length;
    const videoFrameShelfVisible = isVideoMode && activeVideoFrameNodeIds.size > 0;
    const referenceShelfRows = isVideoMode ? Number(videoFrameShelfVisible) + Number(activeNonFrameReferenceCount > 0) : Number(activeReferenceCount > 0);
    const referenceShelfHeight = referenceShelfRows * 42;
    const composerMinHeight = activeReferenceCount ? (isImageMode ? 116 : isAudioMode ? 122 : 82) : isImageMode ? 76 : isAudioMode ? 92 : 58;
    const composerHeight = Math.min(isImageMode || isAudioMode ? 180 : 144, Math.max(composerMinHeight, Math.ceil(promptContentHeight + referenceShelfHeight)));
    const isSubmitDisabled = !isRunning && !prompt.trim();
    const canExpandPrompt = mode === "image" || mode === "video" || mode === "audio";
    const updatePromptContentHeight = useCallback((height: number) => {
        setPromptContentHeight((current) => (Math.abs(current - height) < 1 ? current : height));
    }, []);

    useEffect(() => {
        setPrompt(node.metadata?.composerContent ?? node.metadata?.prompt ?? "");
    }, [node.id, node.metadata?.composerContent, node.metadata?.prompt]);

    useEffect(() => setPromptContentHeight(0), [node.id]);

    useEffect(() => {
        setExpandedPromptOpen(false);
        setExpandedPresetOpen(false);
    }, [node.id]);

    const skillReferences = useMemo(() => mentionReferences.filter((item) => item.kind === "skill"), [mentionReferences]);

    const updatePrompt = (value: string) => {
        setPrompt(value);
        onPromptChange(node.id, value);
        if (/(^|\s)\/[\p{L}\p{N}_-]*$/u.test(value)) {
            if (expandedPromptOpen) setExpandedPresetOpen(true);
            else setPresetOpen(true);
        }
    };

    const applyPreset = (preset: CanvasPromptPreset) => {
        const withoutSlash = prompt.replace(/(^|\s)\/[\p{L}\p{N}_-]*$/u, "$1").trimEnd();
        updatePrompt(withoutSlash ? `${withoutSlash}\n${preset.prompt}` : preset.prompt);
    };

    const insertPromptReference = (reference: CanvasResourceReference) => {
        const insertText = `${canvasResourceMentionToken(reference)} `;
        const pendingMentionMatch = /@[^\s@，。！？、,.!?;:]*\s*$/.exec(prompt);
        if (pendingMentionMatch) {
            const prefix = prompt.slice(0, pendingMentionMatch.index).replace(/\s*$/, "");
            updatePrompt(prefix ? `${prefix} ${insertText}` : insertText);
            return;
        }
        const basePrompt = prompt.replace(/\s*$/, "");
        updatePrompt(basePrompt ? `${basePrompt} ${insertText}` : insertText);
    };

    const appendAudioText = (fragment: string) => updatePrompt(prompt ? `${prompt}${fragment}` : fragment.trimStart());

    const updateVideoFrameMetadata = useCallback(
        (patch: Partial<CanvasNodeMetadata>) => {
            const frameNodeIds = [patch.videoStartFrameNodeId, patch.videoEndFrameNodeId].filter((value): value is string => Boolean(value));
            const connectionsReady = frameNodeIds.every((frameNodeId) => activeVideoImageNodeIds.has(frameNodeId) || onReferenceConnect(frameNodeId, node.id));
            if (!connectionsReady) return;
            onConfigChange(node.id, patch);
        },
        [activeVideoImageNodeIds, node.id, onConfigChange, onReferenceConnect],
    );

    const submit = () => {
        const text = prompt.trim();
        if (!text || isRunning) return false;
        onGenerate(node.id, mode, text);
        return true;
    };

    const submitExpandedPrompt = () => {
        if (submit()) {
            setExpandedPresetOpen(false);
            setExpandedPromptOpen(false);
        }
    };

    const renderComposerHeader = (expanded: boolean) => (
        <div className={`canvas-node-composer-header flex min-w-0 items-center gap-1 px-0.5 ${isImageMode ? "canvas-node-composer-header--image" : isVideoMode ? "canvas-node-composer-header--video" : ""}`}>
            {isImageMode ? (
                <>
                    <ReferenceInsertPicker label="+参考" references={mentionReferences} theme={theme} onInsert={insertPromptReference} />
                    <ReferenceInsertPicker label="标记" references={mentionReferences} theme={theme} onInsert={insertPromptReference} icon={<AtSign className="canvas-reference-picker-icon size-3" />} />
                </>
            ) : mode === "video" ? (
                <>
                    <ReferenceConnectPicker label="+参考" references={availableVideoImageReferences} theme={theme} targetNodeId={node.id} onConnect={onReferenceConnect} />
                    <ReferenceInsertPicker label="标记" references={mentionReferences} theme={theme} onInsert={insertPromptReference} icon={<AtSign className="canvas-reference-picker-icon size-3" />} />
                </>
            ) : isAudioMode ? (
                <CanvasAudioTextTools model={modelOptionName(config.model)} theme={theme} onInsert={appendAudioText} />
            ) : (
                <div className="canvas-node-composer-mode inline-flex h-6 min-w-0 shrink-0 items-center gap-1 rounded-md border-0 px-1.5 text-[10px] font-medium" style={{ background: theme.toolbar.itemHover, color: theme.node.muted }}>
                    <span className="canvas-node-composer-mode-icon grid size-3.5 shrink-0 place-items-center">
                        <GenerationModeIcon mode={mode} />
                    </span>
                    <span className="canvas-node-composer-mode-label truncate">{modeDisplayName(mode)}创作</span>
                </div>
            )}
            {!simpleMode ? (
                <CanvasPresetPicker
                    mode={mode}
                    skillReferences={skillReferences}
                    open={expanded ? expandedPresetOpen : presetOpen}
                    onOpenChange={expanded ? setExpandedPresetOpen : setPresetOpen}
                    onSelect={applyPreset}
                    label={mode === "video" ? "特效" : isImageMode ? "风格" : "预设"}
                    dense
                />
            ) : null}
            <div className="canvas-node-composer-header-actions ml-auto flex shrink-0 items-center justify-end gap-1">
                {!isImageMode && activeReferenceCount ? <ComposerPill theme={theme} icon={<Boxes className="size-2.5" />} label={`已连接 ${activeReferenceCount} 个`} active /> : null}
                {!expanded && canExpandPrompt ? (
                    <Tooltip title="放大编辑">
                        <button
                            type="button"
                            className={`grid size-6 shrink-0 place-items-center rounded-md transition hover:brightness-110 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-1 ${isAudioMode ? "canvas-audio-expand-button" : ""}`}
                            style={{ background: theme.toolbar.itemHover, color: theme.node.muted, outlineColor: theme.accent.primary }}
                            onClick={() => setExpandedPromptOpen(true)}
                            aria-label="放大编辑提示词"
                        >
                            <Maximize2 className="size-3" />
                        </button>
                    </Tooltip>
                ) : null}
            </div>
        </div>
    );

    const renderComposerControls = (expanded: boolean) =>
        isAudioMode ? (
            <CanvasAudioComposerControls
                config={config}
                credits={credits}
                promptLength={prompt.length}
                isRunning={isRunning}
                submitDisabled={isSubmitDisabled}
                onConfigChange={(patch) => onConfigChange(node.id, patch)}
                onSubmit={() => {
                    if (expanded) submitExpandedPrompt();
                    else submit();
                }}
                onStop={() => onStop(node.id)}
            />
        ) : simpleMode ? (
            <div className="flex min-w-0 items-center justify-between gap-2 px-0.5">
                <span className="min-w-0 truncate px-2 text-[10px]" style={{ color: theme.node.muted }}>
                    {activeReferenceCount ? `已连接 ${activeReferenceCount} 个素材` : "将使用默认模型与参数"}
                </span>
                <Button
                    type="text"
                    className="!inline-flex !h-8 shrink-0 !items-center !gap-1 !rounded-md !px-2.5 !text-[10px] !font-medium"
                    danger={isRunning}
                    disabled={isSubmitDisabled}
                    style={{ background: isSubmitDisabled ? theme.toolbar.itemHover : isRunning ? theme.accent.danger : theme.node.activeStroke, color: isSubmitDisabled ? theme.node.faint : isRunning ? "#ffffff" : theme.canvas.background }}
                    onClick={() => (isRunning ? onStop(node.id) : expanded ? submitExpandedPrompt() : submit())}
                    aria-label={isRunning ? "停止生成" : "生成"}
                >
                    {isRunning ? <Square className="size-2.5 fill-current" /> : <ArrowUp className="size-3" />}
                    {isRunning ? "停止" : "生成"}
                </Button>
            </div>
        ) : (
            <div className={`flex min-w-0 items-center justify-between gap-0.5 px-0.5 ${isImageMode ? "canvas-node-prompt-controls-row--image" : isVideoMode ? "canvas-node-prompt-controls-row--video" : ""}`}>
                <div className={`${expanded ? "max-w-[320px]" : mode === "image" ? "w-[114px] max-w-[114px] flex-none" : mode === "video" ? "w-[114px] max-w-[114px] flex-none" : "max-w-[174px]"} min-w-[88px] flex-1`}>
                    <ModelPicker
                        className={`!h-8 !w-full !min-w-0 !text-[10px] !font-normal [&_img]:!size-3 [&_.lucide]:!size-3 ${isImageMode || isVideoMode ? "canvas-image-model-picker" : ""}`}
                        fullWidth
                        config={config}
                        value={config.model}
                        onChange={(model) => onConfigChange(node.id, { model })}
                        capability={mode}
                        onMissingConfig={handleMissingSystemModel}
                        showSelectedPrice={false}
                        presentation={isImageMode || isVideoMode ? "canvasImage" : "default"}
                    />
                </div>
                {isImageMode || isVideoMode ? <span className={isVideoMode ? "canvas-video-toolbar-divider" : "canvas-image-toolbar-divider"} aria-hidden="true" /> : null}
                <div className={`ml-auto flex min-w-0 shrink-0 items-center gap-0.5 ${isImageMode ? "canvas-image-toolbar-actions flex-1" : ""}`}>
                    {mode === "image" ? (
                        <CanvasImageSettingsPopover
                            config={config}
                            placement={expanded ? "topRight" : "topLeft"}
                            buttonClassName="canvas-image-settings-trigger--composer !h-7 !w-[190px] !justify-start !rounded-md !border-0 !bg-transparent !px-1.5 !text-[11px] !font-semibold !shadow-none [&>span]:min-w-0 [&_.lucide]:!size-3.5"
                            onConfigChange={(key, value) => onConfigChange(node.id, key === "count" ? { count: Number(value) || 1 } : { [key]: value })}
                            onMissingConfig={handleMissingSystemModel}
                            onOpenChange={expanded ? undefined : onImageSettingsOpenChange}
                        />
                    ) : mode === "video" ? (
                        <>
                            <CanvasVideoGenerationModePicker metadata={node.metadata} frameOptions={videoFrameOptions} onMetadataChange={updateVideoFrameMetadata} />
                            <span className="canvas-video-toolbar-divider" aria-hidden="true" />
                            <CanvasVideoSettingsPopover
                                config={config}
                                buttonClassName="canvas-video-settings-trigger--composer !h-8 !w-[185px] !justify-start !rounded-lg !border-0 !bg-transparent !px-2 !text-[12px] !font-medium !shadow-none [&>span]:min-w-0 [&_.lucide]:!size-3.5"
                                onConfigChange={(key, value) => onConfigChange(node.id, videoConfigPatch(key, value))}
                            />
                            <CanvasVideoSuperResolutionPopover config={config} onConfigChange={(key, value) => onConfigChange(node.id, videoConfigPatch(key, value))} />
                            <span className="canvas-video-toolbar-divider" aria-hidden="true" />
                        </>
                    ) : null}
                    {isImageMode ? <span className="canvas-image-toolbar-divider" aria-hidden="true" /> : null}
                    {isImageMode ? (
                        <span className="canvas-image-bottom-preset inline-flex">
                            <CanvasPresetPicker mode={mode} skillReferences={skillReferences} open={bottomPresetOpen} onOpenChange={setBottomPresetOpen} onSelect={applyPreset} compact />
                        </span>
                    ) : null}
                    <span className={isImageMode ? "canvas-image-generation-cost ml-auto inline-flex" : isVideoMode ? "canvas-video-generation-cost ml-auto inline-flex" : "inline-flex"}>
                        <GenerationCostBadge credits={credits} theme={theme} />
                    </span>
                    <Button
                        type="text"
                        className={`canvas-node-submit-button !inline-flex !h-8 !w-8 shrink-0 !items-center !justify-center !border-0 !p-0 !shadow-none ${isImageMode ? "!rounded-full" : "!rounded-md"}`}
                        danger={isRunning}
                        disabled={isSubmitDisabled}
                        style={{ background: isSubmitDisabled ? theme.toolbar.itemHover : isRunning ? theme.accent.danger : theme.accent.primary, borderColor: "transparent", color: isSubmitDisabled ? theme.node.faint : "#ffffff" }}
                        onClick={() => (isRunning ? onStop(node.id) : expanded ? submitExpandedPrompt() : submit())}
                        aria-label={isRunning ? "停止生成" : "生成"}
                    >
                        {isRunning ? <Square className="size-2.5 fill-current" /> : <ArrowUp className="size-3" />}
                    </Button>
                </div>
            </div>
        );

    return (
        <div
            className={`canvas-node-prompt-panel aceternity-floating-panel overflow-hidden backdrop-blur-2xl ${isImageMode ? "canvas-node-prompt-panel--image rounded-2xl p-2" : isVideoMode ? "canvas-node-prompt-panel--video rounded-2xl p-2" : isAudioMode ? "canvas-node-prompt-panel--audio p-2.5" : "rounded-lg p-1.5"}`}
            style={{ background: theme.spatial.elevated, color: theme.node.text, boxShadow: `0 20px 64px ${theme.spatial.shadow}, inset 0 1px 0 rgba(255,255,255,.07)` }}
            onMouseDown={(event) => event.stopPropagation()}
            onPointerDown={(event) => event.stopPropagation()}
            onWheel={(event) => event.stopPropagation()}
        >
            {renderComposerHeader(false)}

            <div
                className={`canvas-node-prompt-editor relative flex flex-col overflow-hidden transition-[height,outline-color] duration-150 motion-reduce:transition-none ${isImageMode ? "canvas-node-prompt-editor--image mt-1 max-h-[180px]" : isVideoMode ? "canvas-node-prompt-editor--video mt-1 max-h-[180px]" : isAudioMode ? "canvas-node-prompt-editor--audio max-h-[180px]" : "mt-1.5 max-h-36 rounded-lg focus-within:outline focus-within:outline-1"}`}
                style={{ height: composerHeight, background: isImageMode || isVideoMode || isAudioMode ? "transparent" : composerSurface, outlineColor: theme.accent.primary }}
            >
                {isVideoMode && !simpleMode ? <CanvasVideoPromptTools metadata={node.metadata} frameOptions={videoFrameOptions} onMetadataChange={updateVideoFrameMetadata} /> : null}
                <ConnectedReferenceShelf references={isVideoMode ? nonFrameMentionReferences : mentionReferences} theme={theme} onInsert={insertPromptReference} />
                <CanvasResourceMentionTextarea
                    value={prompt}
                    references={mentionReferences}
                    onChange={updatePrompt}
                    containerClassName="min-h-0 flex-1"
                    className={`canvas-node-prompt-textarea thin-scrollbar h-full w-full resize-none overflow-y-auto border-none bg-transparent text-[13px] leading-5 outline-none placeholder:text-current placeholder:opacity-35 ${isImageMode ? "canvas-node-prompt-textarea--image px-0.5 py-1.5" : isVideoMode ? "canvas-node-prompt-textarea--video px-0.5 py-1.5" : isAudioMode ? "canvas-node-prompt-textarea--audio" : "px-2.5 py-2"}`}
                    style={{ color: theme.node.text }}
                    placeholder={promptPlaceholder(mode, hasTextContent)}
                    onContentSizeChange={updatePromptContentHeight}
                />
            </div>

            <div className={`canvas-node-prompt-controls ${isImageMode ? "canvas-node-prompt-controls--image mt-1" : isAudioMode ? "canvas-node-prompt-controls--audio" : "mt-1.5"}`}>{renderComposerControls(false)}</div>

            <Modal
                className="canvas-prompt-editor-modal"
                open={expandedPromptOpen}
                title={null}
                footer={null}
                centered
                width={760}
                destroyOnHidden
                onCancel={() => {
                    setExpandedPresetOpen(false);
                    setExpandedPromptOpen(false);
                }}
                styles={{ container: { display: "flex", height: "min(440px, calc(100vh - 40px))", flexDirection: "column", borderRadius: 12, padding: 0, overflow: "hidden" }, body: { minHeight: 0, flex: 1, padding: 0 } }}
            >
                <div className="flex h-full min-h-0 flex-col gap-2.5 p-3" style={{ color: theme.node.text }}>
                    <div className="shrink-0 pr-8">{renderComposerHeader(true)}</div>
                    <div className="flex min-h-[240px] flex-1 flex-col overflow-hidden rounded-lg border focus-within:outline focus-within:outline-1" style={{ borderColor: theme.toolbar.border, outlineColor: theme.accent.primary }}>
                        {isVideoMode && !simpleMode ? <CanvasVideoPromptTools metadata={node.metadata} frameOptions={videoFrameOptions} onMetadataChange={updateVideoFrameMetadata} /> : null}
                        <ConnectedReferenceShelf references={isVideoMode ? nonFrameMentionReferences : mentionReferences} theme={theme} onInsert={insertPromptReference} />
                        <CanvasResourceMentionTextarea
                            value={prompt}
                            references={mentionReferences}
                            onChange={updatePrompt}
                            containerClassName="min-h-0 flex-1"
                            className="thin-scrollbar h-full w-full resize-none overflow-y-auto border-none bg-transparent px-3 py-2.5 text-[15px] leading-6 outline-none placeholder:text-current placeholder:opacity-35"
                            style={{ color: theme.node.text }}
                            placeholder={promptPlaceholder(mode, hasTextContent)}
                            aria-label={`${modeDisplayName(mode)}提示词`}
                        />
                    </div>
                    <div className="shrink-0">{renderComposerControls(true)}</div>
                </div>
            </Modal>
        </div>
    );
}

function ComposerPill({ theme, icon, label, active = false }: { theme: CanvasTheme; icon: ReactNode; label: string; active?: boolean }) {
    return (
        <span className="inline-flex h-6 shrink-0 items-center gap-1 rounded-md px-1.5 text-[9px] font-medium" style={{ background: active ? theme.accent.primarySoft : theme.toolbar.itemHover, color: active ? theme.accent.primary : theme.node.muted }}>
            {icon}
            {label}
        </span>
    );
}

function CanvasAudioTextTools({ model, theme, onInsert }: { model: string; theme: CanvasTheme; onInsert: (fragment: string) => void }) {
    const [pauseOpen, setPauseOpen] = useState(false);
    const [vocalOpen, setVocalOpen] = useState(false);
    const supportsVocalTags = model.startsWith("speech-2.8-");
    const pauseOptions = [
        { value: "<#0.3#>", label: "短停顿", description: "0.3 秒" },
        { value: "<#0.5#>", label: "自然停顿", description: "0.5 秒" },
        { value: "<#1#>", label: "长停顿", description: "1 秒" },
        { value: "<#2#>", label: "段落停顿", description: "2 秒" },
    ];
    const menuStyle = { background: theme.toolbar.panel, borderColor: theme.toolbar.border, color: theme.node.text };
    return (
        <div className="canvas-audio-text-tools flex min-w-0 items-center gap-1.5" role="group" aria-label="音频文本工具">
            <Popover
                open={pauseOpen}
                onOpenChange={setPauseOpen}
                trigger="click"
                placement="topLeft"
                content={
                    <div className="canvas-audio-text-menu grid w-56 grid-cols-2 gap-1 p-1" style={menuStyle}>
                        {pauseOptions.map((option) => (
                            <button
                                key={option.value}
                                type="button"
                                className="canvas-audio-text-menu-option flex min-w-0 flex-col gap-0.5 rounded-md border-0 px-2 py-1.5 text-left"
                                style={{ background: theme.toolbar.itemHover, color: theme.node.text }}
                                onClick={() => {
                                    onInsert(option.value);
                                    setPauseOpen(false);
                                }}
                            >
                                <span className="canvas-audio-text-menu-label text-[11px] font-medium">{option.label}</span>
                                <span className="canvas-audio-text-menu-description text-[9px]" style={{ color: theme.node.muted }}>
                                    {option.description}
                                </span>
                            </button>
                        ))}
                    </div>
                }
                styles={{ content: { padding: 0, background: theme.toolbar.panel, border: `1px solid ${theme.toolbar.border}` } }}
            >
                <button type="button" className="canvas-audio-text-tool" aria-label="插入停顿">
                    <span className="canvas-audio-text-tool-symbol" aria-hidden="true">
                        &lt;#&gt;
                    </span>
                    <span className="canvas-audio-text-tool-label">停顿</span>
                </button>
            </Popover>
            <Popover
                open={vocalOpen}
                onOpenChange={supportsVocalTags ? setVocalOpen : undefined}
                trigger="click"
                placement="topLeft"
                content={
                    <div className="canvas-audio-vocal-menu thin-scrollbar grid max-h-64 w-72 grid-cols-3 gap-1 overflow-y-auto p-1" style={menuStyle}>
                        {miniMaxVocalTags.map((option) => (
                            <button
                                key={option.value}
                                type="button"
                                className="canvas-audio-vocal-option rounded-md border-0 px-2 py-1.5 text-left text-[10px]"
                                style={{ background: theme.toolbar.itemHover, color: theme.node.text }}
                                title={option.value}
                                onClick={() => {
                                    onInsert(option.value);
                                    setVocalOpen(false);
                                }}
                            >
                                {option.label}
                            </button>
                        ))}
                    </div>
                }
                styles={{ content: { padding: 0, background: theme.toolbar.panel, border: `1px solid ${theme.toolbar.border}` } }}
            >
                <Tooltip title={supportsVocalTags ? "插入 MiniMax 语气词" : "当前模型不支持语气词标记"}>
                    <button type="button" className="canvas-audio-text-tool" disabled={!supportsVocalTags} aria-label="插入语气词">
                        <span className="canvas-audio-text-tool-symbol" aria-hidden="true">
                            ()
                        </span>
                        <span className="canvas-audio-text-tool-label">语气词</span>
                    </button>
                </Tooltip>
            </Popover>
        </div>
    );
}

function ReferenceInsertPicker({ label, references, theme, onInsert, icon }: { label: string; references: CanvasResourceReference[]; theme: CanvasTheme; onInsert: (reference: CanvasResourceReference) => void; icon?: ReactNode }) {
    const [open, setOpen] = useState(false);
    const activeReferences = references.filter((item) => item.active && item.kind !== "skill");
    const content = activeReferences.length ? (
        <div className="canvas-reference-picker-menu thin-scrollbar flex max-h-64 w-64 flex-col gap-1 overflow-y-auto">
            {activeReferences.map((reference) => (
                <button
                    key={reference.id}
                    type="button"
                    className="canvas-reference-picker-option flex min-w-0 items-center gap-2 px-2 py-1.5 text-left transition hover:brightness-110"
                    style={{ background: theme.toolbar.itemHover, color: theme.node.text }}
                    onClick={() => {
                        onInsert(reference);
                        setOpen(false);
                    }}
                >
                    <span className="canvas-reference-picker-thumbnail size-8 shrink-0 overflow-hidden">
                        <ReferenceThumbnail reference={reference} />
                    </span>
                    <span className="canvas-reference-picker-copy min-w-0 flex-1">
                        <span className="canvas-reference-picker-label block truncate text-[11px] font-medium">@{reference.label}</span>
                        <span className="canvas-reference-picker-title block truncate text-[9px]" style={{ color: theme.node.muted }}>
                            {reference.title}
                        </span>
                    </span>
                </button>
            ))}
        </div>
    ) : (
        <div className="canvas-reference-picker-empty w-52 px-2 py-1 text-[10px]" style={{ color: theme.node.muted }}>
            请先连接图片或素材节点
        </div>
    );

    return (
        <Popover open={open} onOpenChange={setOpen} trigger="click" placement="topLeft" content={content} styles={{ content: { padding: 6, background: theme.toolbar.panel, border: `1px solid ${theme.toolbar.border}` } }}>
            <button
                type="button"
                className="canvas-reference-picker-trigger inline-flex h-6 shrink-0 items-center justify-center gap-1 rounded-md border-0 px-1.5 text-[10px] font-medium transition hover:brightness-110 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-1"
                style={{ background: theme.toolbar.itemHover, color: theme.node.muted, outlineColor: theme.accent.primary }}
                aria-label={`打开${label}选择`}
            >
                {icon}
                <span className="canvas-reference-picker-trigger-label">{label}</span>
            </button>
        </Popover>
    );
}

function ReferenceConnectPicker({
    label,
    references,
    theme,
    targetNodeId,
    onConnect,
}: {
    label: string;
    references: CanvasResourceReference[];
    theme: CanvasTheme;
    targetNodeId: string;
    onConnect: (sourceNodeId: string, targetNodeId: string) => boolean;
}) {
    const [open, setOpen] = useState(false);
    const content = references.length ? (
        <div className="canvas-reference-connect-menu thin-scrollbar flex max-h-64 w-64 flex-col gap-1 overflow-y-auto">
            {references.map((reference) => (
                <button
                    key={reference.id}
                    type="button"
                    className="canvas-reference-connect-option flex min-w-0 items-center gap-2 px-2 py-1.5 text-left transition hover:brightness-110"
                    style={{
                        background: reference.active ? theme.accent.primarySoft : theme.toolbar.itemHover,
                        color: reference.active ? theme.accent.primary : theme.node.text,
                    }}
                    onClick={() => {
                        if (reference.active || onConnect(reference.nodeId, targetNodeId)) {
                            setOpen(false);
                        }
                    }}
                >
                    <span className="canvas-reference-connect-thumbnail size-8 shrink-0 overflow-hidden">
                        <ReferenceThumbnail reference={reference} />
                    </span>
                    <span className="canvas-reference-connect-copy min-w-0 flex-1">
                        <span className="canvas-reference-connect-label block truncate text-[11px] font-medium">@{reference.label}</span>
                        <span className="canvas-reference-connect-title block truncate text-[9px]" style={{ color: theme.node.muted }}>
                            {reference.title}
                        </span>
                    </span>
                    {reference.active ? (
                        <span className="canvas-reference-connect-status inline-flex shrink-0 items-center gap-1 text-[9px]">
                            <Check className="canvas-reference-connect-check size-3" />
                            已连接
                        </span>
                    ) : null}
                </button>
            ))}
        </div>
    ) : (
        <div className="canvas-reference-connect-empty w-52 px-2 py-1 text-[10px]" style={{ color: theme.node.muted }}>
            画布中暂无已生成图片
        </div>
    );

    return (
        <Popover open={open} onOpenChange={setOpen} trigger="click" placement="topLeft" content={content} styles={{ content: { padding: 6, background: theme.toolbar.panel, border: `1px solid ${theme.toolbar.border}` } }}>
            <button
                type="button"
                className="canvas-reference-connect-trigger inline-flex h-6 shrink-0 items-center justify-center gap-1 rounded-md border-0 px-1.5 text-[10px] font-medium transition hover:brightness-110 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-1"
                style={{ background: theme.toolbar.itemHover, color: theme.node.muted, outlineColor: theme.accent.primary }}
                aria-label={`打开${label}选择`}
            >
                <span className="canvas-reference-connect-trigger-label">{label}</span>
            </button>
        </Popover>
    );
}

function GenerationModeIcon({ mode }: { mode: CanvasNodeGenerationMode }) {
    if (mode === "image") return <ImagePlus className="size-3" />;
    if (mode === "video") return <Video className="size-3" />;
    if (mode === "audio") return <Music2 className="size-3" />;
    return <FileText className="size-3" />;
}

function modeDisplayName(mode: CanvasNodeGenerationMode) {
    if (mode === "image") return "图片";
    if (mode === "video") return "视频";
    if (mode === "audio") return "音频";
    return "文本";
}

function ConnectedReferenceShelf({ references, theme, onInsert }: { references: CanvasResourceReference[]; theme: CanvasTheme; onInsert: (reference: CanvasResourceReference) => void }) {
    const activeReferences = references.filter((item) => item.active && item.kind !== "skill");
    if (!activeReferences.length) return null;

    return (
        <div className="thin-scrollbar flex h-[42px] shrink-0 min-w-0 items-center gap-1.5 overflow-x-auto px-2.5 pt-1.5" role="group" aria-label="已连接素材">
            {activeReferences.map((reference, index) => (
                <button
                    key={reference.id}
                    type="button"
                    className="group relative size-[34px] shrink-0 overflow-hidden rounded-md text-left transition hover:-translate-y-0.5 hover:brightness-110 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-1 motion-reduce:hover:translate-y-0"
                    style={{ background: theme.toolbar.itemHover, color: theme.node.text, outlineColor: theme.accent.primary }}
                    title={`插入 @${reference.label}`}
                    aria-label={`插入 @${reference.label}`}
                    onClick={() => onInsert(reference)}
                >
                    <span className="block size-full overflow-hidden rounded-md">
                        <ReferenceThumbnail reference={reference} />
                    </span>
                    <span className="absolute left-0.5 top-0.5 grid size-3.5 place-items-center rounded-full bg-black/65 text-[8px] font-semibold text-white backdrop-blur-sm">{index + 1}</span>
                    <span className="absolute bottom-0.5 right-0.5 grid size-3.5 place-items-center rounded-full bg-black/65 text-white backdrop-blur-sm">
                        <AtSign className="size-2" />
                    </span>
                </button>
            ))}
        </div>
    );
}

function ReferenceThumbnail({ reference }: { reference: CanvasResourceReference }) {
    if (reference.kind === "image" && reference.previewUrl) return <img src={reference.previewUrl} alt="" className="size-full object-cover" />;
    if (reference.kind === "video" && reference.previewUrl) return <video src={reference.previewUrl} className="size-full bg-black object-cover" muted preload="metadata" />;
    if (reference.kind === "character" && reference.previewUrl) return <img src={reference.previewUrl} alt="" className="size-full bg-black/5 object-contain" />;

    const Icon = reference.sourceType === CanvasNodeType.Drawing ? Pencil : reference.kind === "character" ? UserRound : reference.kind === "audio" ? Music2 : reference.kind === "video" ? Video : reference.kind === "image" ? ImageIcon : FileText;
    return (
        <span className="grid size-full place-items-center bg-black/10 text-current dark:bg-white/10">
            <Icon className="size-3.5 opacity-75" />
        </span>
    );
}

function GenerationCostBadge({ credits, theme }: { credits: number | null; theme: CanvasTheme }) {
    if (credits === null) return null;
    return (
        <span className="inline-flex h-6 shrink-0 items-center gap-0.5 px-1 text-[9px] font-medium tabular-nums" style={{ color: theme.node.muted }} title="本次生成消耗">
            <CreditSymbol />
            {credits.toLocaleString()}
        </span>
    );
}

function defaultMode(type: CanvasNodeData["type"]): CanvasNodeGenerationMode {
    return type === CanvasNodeType.Text || type === CanvasNodeType.Skill ? "text" : type === CanvasNodeType.Video ? "video" : type === CanvasNodeType.Audio ? "audio" : "image";
}

function buildNodeConfig(globalConfig: AiConfig, node: CanvasNodeData, mode: CanvasNodeGenerationMode): AiConfig {
    const defaultModel = mode === "image" ? globalConfig.imageModel : mode === "video" ? globalConfig.videoModel : mode === "audio" ? globalConfig.audioModel : globalConfig.textModel;
    const fallbackModel = mode === "image" ? defaultConfig.imageModel : mode === "video" ? defaultConfig.videoModel : mode === "audio" ? defaultConfig.audioModel : defaultConfig.textModel;
    const storedModel = node.metadata?.model;
    const model = storedModel && configuredModelMatchesCapability(globalConfig, storedModel, mode) ? storedModel : defaultModel && configuredModelMatchesCapability(globalConfig, defaultModel, mode) ? defaultModel : fallbackModel;
    return {
        ...globalConfig,
        model,
        quality: node.metadata?.quality || globalConfig.quality || defaultConfig.quality,
        size: node.metadata?.size || globalConfig.size || defaultConfig.size,
        transparentBackground: (node.metadata?.transparentBackground || globalConfig.transparentBackground) === "true" ? "true" : "false",
        videoSeconds: normalizeVideoDuration(node.metadata?.seconds || globalConfig.videoSeconds || defaultConfig.videoSeconds),
        vquality: normalizeVideoResolution(node.metadata?.vquality || globalConfig.vquality || defaultConfig.vquality),
        videoGenerateAudio: node.metadata?.generateAudio || globalConfig.videoGenerateAudio || defaultConfig.videoGenerateAudio,
        videoWatermark: node.metadata?.watermark || globalConfig.videoWatermark || defaultConfig.videoWatermark,
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
}

function promptPlaceholder(mode: CanvasNodeGenerationMode, hasTextContent: boolean) {
    if (mode === "video") return "描述要生成的视频内容";
    if (mode === "audio") return "输入要合成的文本";
    if (mode === "image") return "可直接文字生图，或上传图片输入文字指令对图片进行编辑，如：将背景改为雪夜";
    return hasTextContent ? "请输入你想要将本段文本修改成什么" : "请输入你想要生成的文本内容";
}

function videoConfigPatch(key: keyof AiConfig, value: string) {
    if (key === "count") return { count: Math.max(1, Math.floor(Math.abs(Number(value)) || 1)) };
    if (key === "videoSeconds") return { seconds: value };
    if (key === "videoGenerateAudio") return { generateAudio: value };
    if (key === "videoWatermark") return { watermark: value };
    if (key === "videoSuperResolutionEnabled") return { superResolutionEnabled: value };
    if (key === "videoSuperResolutionResolution") return { superResolutionResolution: value };
    if (key === "videoSuperResolutionScene") return { superResolutionScene: value };
    if (key === "videoSuperResolutionVersion") return { superResolutionVersion: value };
    if (key === "videoSuperResolutionFps") return { superResolutionFps: value };
    return { [key]: value };
}
