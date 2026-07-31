import { useState } from "react";
import { Popover, Tooltip } from "antd";

import { miniMaxVocalTags } from "@/lib/audio-generation";
import type { CanvasTheme } from "@/lib/canvas-theme";

type CanvasAudioTextToolsProps = {
    model: string;
    theme: CanvasTheme;
    onInsert: (fragment: string) => void;
};

const pauseOptions = [
    { value: "<#0.3#>", label: "短停顿", duration: "0.3s" },
    { value: "<#0.5#>", label: "自然停顿", duration: "0.5s" },
    { value: "<#1#>", label: "长停顿", duration: "1.0s" },
    { value: "<#2#>", label: "段落停顿", duration: "2.0s" },
] as const;

export function CanvasAudioTextTools({ model, theme, onInsert }: CanvasAudioTextToolsProps) {
    const [pauseOpen, setPauseOpen] = useState(false);
    const [vocalOpen, setVocalOpen] = useState(false);
    const supportsVocalTags = model.startsWith("speech-2.8-");
    const popoverStyle = { background: theme.toolbar.panel, border: `1px solid ${theme.toolbar.border}` };

    return (
        <div className="canvas-audio-text-tools" role="group" aria-label="音频文本工具">
            <Popover
                open={pauseOpen}
                onOpenChange={setPauseOpen}
                trigger="click"
                placement="bottomLeft"
                overlayClassName="canvas-audio-text-popover canvas-audio-text-popover--pause"
                content={
                    <section className="canvas-audio-insert-menu canvas-audio-insert-menu--pause" aria-label="选择停顿时长">
                        <div className="canvas-audio-insert-menu-list">
                            {pauseOptions.map((option, index) => (
                                <button
                                    key={option.value}
                                    type="button"
                                    className="canvas-audio-insert-option"
                                    aria-label={`插入${option.label}，${option.duration}`}
                                    autoFocus={index === 0}
                                    onClick={() => {
                                        onInsert(option.value);
                                        setPauseOpen(false);
                                    }}
                                >
                                    <span className="canvas-audio-insert-option-duration">{option.duration}</span>
                                </button>
                            ))}
                        </div>
                    </section>
                }
                styles={{ content: { padding: 0, ...popoverStyle } }}
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
