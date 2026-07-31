import { useState, type CSSProperties, type KeyboardEvent } from "react";
import { Popover, Tooltip } from "antd";

import { miniMaxVocalTags } from "@/lib/audio-generation";
import { defaultAudioPauseToken, normalizeAudioPauseInput, type TextRange } from "@/lib/audio-pause";
import type { CanvasTheme } from "@/lib/canvas-theme";

type CanvasAudioTextToolsProps = {
    model: string;
    theme: CanvasTheme;
    onInsert: (fragment: string) => void;
    onInsertPause: (fragment: string) => TextRange;
    onReplacePause: (range: TextRange, fragment: string) => TextRange;
};

const pauseOptions = [
    { value: defaultAudioPauseToken, duration: "0.25s" },
    { value: "<#0.5#>", duration: "0.5s" },
    { value: "<#1#>", duration: "1.0s" },
    { value: "<#1.5#>", duration: "1.5s" },
] as const;

type AudioPauseMenuStyle = CSSProperties & {
    "--canvas-audio-popover-accent": string;
};

export function CanvasAudioTextTools({ model, theme, onInsert, onInsertPause, onReplacePause }: CanvasAudioTextToolsProps) {
    const [pauseOpen, setPauseOpen] = useState(false);
    const [vocalOpen, setVocalOpen] = useState(false);
    const [activePauseRange, setActivePauseRange] = useState<TextRange | null>(null);
    const [activePauseValue, setActivePauseValue] = useState(defaultAudioPauseToken);
    const [customPauseMode, setCustomPauseMode] = useState(false);
    const [customPauseValue, setCustomPauseValue] = useState("");
    const [customPauseError, setCustomPauseError] = useState("");
    const supportsVocalTags = model.startsWith("speech-2.8-");
    const popoverStyle: CSSProperties = {
        background: theme.toolbar.panel,
        border: `1px solid ${theme.toolbar.border}`,
    };
    const pauseMenuStyle: AudioPauseMenuStyle = {
        "--canvas-audio-popover-accent": theme.accent.primary,
    };

    const handlePauseOpenChange = (open: boolean) => {
        if (open) {
            const range = onInsertPause(defaultAudioPauseToken);
            setActivePauseRange(range);
            setActivePauseValue(defaultAudioPauseToken);
            setCustomPauseMode(false);
            setCustomPauseValue("");
            setCustomPauseError("");
        }
        setPauseOpen(open);
    };

    const replaceActivePause = (token: string) => {
        if (!activePauseRange) throw new Error("停顿标记上下文缺失，无法更新时间");
        const nextRange = onReplacePause(activePauseRange, token);
        setActivePauseRange(nextRange);
        setActivePauseValue(token);
        return nextRange;
    };

    const selectPauseOption = (token: string) => {
        replaceActivePause(token);
        setPauseOpen(false);
    };

    const submitCustomPause = () => {
        const result = normalizeAudioPauseInput(customPauseValue);
        if (!result.ok) {
            setCustomPauseError(result.message);
            return;
        }
        replaceActivePause(result.token);
        setPauseOpen(false);
    };

    const handleCustomPauseKeyDown = (event: KeyboardEvent<HTMLInputElement>) => {
        if (event.key === "Enter") {
            event.preventDefault();
            submitCustomPause();
            return;
        }
        if (event.key === "Escape") {
            event.preventDefault();
            setCustomPauseMode(false);
            setCustomPauseError("");
        }
    };

    return (
        <div className="canvas-audio-text-tools" role="group" aria-label="音频文本工具">
            <Popover
                open={pauseOpen}
                onOpenChange={handlePauseOpenChange}
                trigger="click"
                placement="bottomLeft"
                overlayClassName="canvas-audio-text-popover canvas-audio-text-popover--pause"
                content={
                    <section className="canvas-audio-insert-menu canvas-audio-insert-menu--pause" style={pauseMenuStyle} aria-label="选择停顿时长">
                        <div className="canvas-audio-insert-menu-list">
                            {pauseOptions.map((option) => (
                                <button
                                    key={option.value}
                                    type="button"
                                    className={`canvas-audio-insert-option ${activePauseValue === option.value ? "canvas-audio-insert-option--active" : ""}`}
                                    aria-label={`停顿 ${option.duration}`}
                                    aria-pressed={activePauseValue === option.value}
                                    onClick={() => selectPauseOption(option.value)}
                                >
                                    <span className="canvas-audio-insert-option-duration">{option.duration}</span>
                                </button>
                            ))}
                            {customPauseMode ? (
                                <div className={`canvas-audio-custom-pause ${customPauseError ? "canvas-audio-custom-pause--invalid" : ""}`}>
                                    <input
                                        type="text"
                                        inputMode="decimal"
                                        className="canvas-audio-custom-pause-input"
                                        value={customPauseValue}
                                        placeholder="秒数"
                                        aria-label="自定义停顿秒数"
                                        aria-invalid={Boolean(customPauseError)}
                                        aria-describedby={customPauseError ? "canvas-audio-custom-pause-error" : undefined}
                                        autoFocus
                                        onChange={(event) => {
                                            setCustomPauseValue(event.target.value);
                                            setCustomPauseError("");
                                        }}
                                        onKeyDown={handleCustomPauseKeyDown}
                                    />
                                    {customPauseError ? (
                                        <span id="canvas-audio-custom-pause-error" className="canvas-audio-custom-pause-error" role="alert">
                                            {customPauseError}
                                        </span>
                                    ) : null}
                                </div>
                            ) : (
                                <button
                                    type="button"
                                    className="canvas-audio-insert-option"
                                    onClick={() => {
                                        setCustomPauseMode(true);
                                        setCustomPauseValue("");
                                        setCustomPauseError("");
                                    }}
                                >
                                    <span className="canvas-audio-insert-option-duration">自定义</span>
                                </button>
                            )}
                        </div>
                    </section>
                }
                styles={{ content: { padding: 0 } }}
            >
                <Tooltip title="在文中插入停顿，精准掌控音频节奏，支持选择预设时长或直接输入秒数">
                    <button type="button" className="canvas-audio-text-tool" aria-label="插入停顿">
                        <span className="canvas-audio-text-tool-symbol" aria-hidden="true">
                            &lt;#&gt;
                        </span>
                        <span className="canvas-audio-text-tool-label">停顿</span>
                    </button>
                </Tooltip>
            </Popover>

            <Popover
                open={vocalOpen}
                onOpenChange={supportsVocalTags ? setVocalOpen : undefined}
                trigger="click"
                placement="topLeft"
                overlayClassName="canvas-audio-text-popover"
                content={
                    <section className="canvas-audio-insert-menu canvas-audio-insert-menu--vocal" aria-label="选择语气词">
                        <header className="canvas-audio-insert-menu-header">
                            <span className="canvas-audio-insert-menu-title">语气词</span>
                            <span className="canvas-audio-insert-menu-hint">MiniMax Speech 2.8</span>
                        </header>
                        <div className="canvas-audio-vocal-list thin-scrollbar">
                            {miniMaxVocalTags.map((option) => (
                                <button
                                    key={option.value}
                                    type="button"
                                    className="canvas-audio-vocal-option"
                                    title={option.value}
                                    onClick={() => {
                                        onInsert(option.value);
                                        setVocalOpen(false);
                                    }}
                                >
                                    <span className="canvas-audio-vocal-option-label">{option.label}</span>
                                    <span className="canvas-audio-vocal-option-code">{option.value}</span>
                                </button>
                            ))}
                        </div>
                    </section>
                }
                styles={{ content: { padding: 0, ...popoverStyle } }}
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
