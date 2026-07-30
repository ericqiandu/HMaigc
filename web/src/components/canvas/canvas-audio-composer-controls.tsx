import { ArrowUp, SlidersHorizontal, Square } from "lucide-react";
import { Button } from "antd";

import { ModelPicker } from "@/components/model-picker";
import { CreditSymbol } from "@/constant/credits";
import { handleMissingSystemModel } from "@/lib/settings-navigation";
import type { AiConfig } from "@/stores/use-config-store";
import type { CanvasNodeMetadata } from "@/types/canvas";
import { CanvasAudioSettingsPopover, type CanvasAudioSettingKey } from "./canvas-audio-settings-popover";
import { CanvasAudioVoicePicker } from "./canvas-audio-voice-picker";
import "./canvas-audio-model-picker.css";

type CanvasAudioComposerControlsProps = {
    config: AiConfig;
    credits: number | null;
    promptLength: number;
    isRunning: boolean;
    submitDisabled: boolean;
    onConfigChange: (patch: Partial<CanvasNodeMetadata>) => void;
    onSubmit: () => void;
    onStop: () => void;
};

export function CanvasAudioComposerControls({ config, credits, promptLength, isRunning, submitDisabled, onConfigChange, onSubmit, onStop }: CanvasAudioComposerControlsProps) {
    return (
        <div className="canvas-audio-composer-controls">
            <div className="canvas-audio-composer-primary-controls">
                <ModelPicker
                    className="canvas-image-model-picker canvas-audio-model-picker"
                    fullWidth
                    config={config}
                    value={config.model}
                    onChange={(model) => onConfigChange({ model })}
                    capability="audio"
                    onMissingConfig={handleMissingSystemModel}
                    showSelectedPrice={false}
                    presentation="canvasAudio"
                />
                <span className="canvas-image-toolbar-divider canvas-audio-control-divider" aria-hidden="true" />
                <CanvasAudioVoicePicker config={config} value={config.audioVoice} onChange={(audioVoice) => onConfigChange({ audioVoice })} />
            </div>
            <div className="canvas-audio-composer-secondary-controls">
                <CanvasAudioSettingsPopover config={config} placement="topRight" iconOnly buttonClassName="canvas-audio-settings-trigger" onConfigChange={(key, value) => onConfigChange(audioConfigPatch(key, value))} />
                <span className="canvas-audio-character-count" aria-label={`已输入 ${promptLength} 个字符`}>
                    {promptLength.toLocaleString()}/50,000
                </span>
                {credits !== null ? (
                    <span className="canvas-audio-generation-cost" title="本次生成消耗">
                        <CreditSymbol />
                        {credits.toLocaleString()}
                    </span>
                ) : null}
                <Button type="text" className="canvas-audio-submit-button" danger={isRunning} disabled={submitDisabled} onClick={isRunning ? onStop : onSubmit} aria-label={isRunning ? "停止生成音频" : "生成音频"}>
                    {isRunning ? <Square className="canvas-audio-submit-icon size-3 fill-current" /> : <ArrowUp className="canvas-audio-submit-icon size-4" />}
                </Button>
            </div>
        </div>
    );
}

function audioConfigPatch(key: CanvasAudioSettingKey, value: string): Partial<CanvasNodeMetadata> {
    return { [key]: value };
}
