import { useState } from "react";
import { Popover, Tooltip } from "antd";

import { miniMaxVocalTags } from "@/lib/audio-generation";

type CanvasAudioVocalToolProps = {
    model: string;
    onInsert: (fragment: string) => void;
};

export function CanvasAudioVocalTool({ model, onInsert }: CanvasAudioVocalToolProps) {
    const [open, setOpen] = useState(false);
    const supported = model.startsWith("speech-2.8-");

    return (
        <Popover
            open={open}
            onOpenChange={supported ? setOpen : undefined}
            trigger="click"
            placement="topLeft"
            rootClassName="canvas-overlay-popover canvas-audio-text-popover canvas-audio-text-popover--vocal"
            content={
                <section className="canvas-audio-insert-menu canvas-audio-insert-menu--vocal" aria-label="选择语气词">
                    <div className="canvas-audio-vocal-list">
                        {miniMaxVocalTags.map((option) => (
                            <button
                                key={option.value}
                                type="button"
                                className="canvas-audio-vocal-option"
                                onClick={() => {
                                    onInsert(option.value);
                                    setOpen(false);
                                }}
                            >
                                <span className="canvas-audio-vocal-option-label">{option.label}</span>
                            </button>
                        ))}
                    </div>
                </section>
            }
            styles={{ content: { padding: 0 } }}
        >
            <Tooltip title={supported ? "点击插入生动的语气词，让语音更具感染力；仅支持列表内的语气词标签" : "当前模型不支持语气词标签"}>
                <button type="button" className="canvas-audio-text-tool" disabled={!supported} aria-label="插入语气词">
                    <span className="canvas-audio-text-tool-symbol" aria-hidden="true">
                        ()
                    </span>
                    <span className="canvas-audio-text-tool-label">语气词</span>
                </button>
            </Tooltip>
        </Popover>
    );
}
