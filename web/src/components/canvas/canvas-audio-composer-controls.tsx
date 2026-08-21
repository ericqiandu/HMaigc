import { SlidersHorizontal } from "lucide-react";

import { ModelPicker } from "@/components/model-picker";
import { CreditSymbol } from "@/constant/credits";
import { handleMissingSystemModel } from "@/lib/settings-navigation";
import type { AiConfig } from "@/stores/use-config-store";
import type { CanvasNodeMetadata } from "@/types/canvas";
import { CanvasAudioSettingsPopover, type CanvasAudioSettingKey } from "./canvas-audio-settings-popover";
import { CanvasAudioVoicePicker } from "./canvas-audio-voice-picker";
import { CanvasSubmitButton } from "./canvas-submit-button";
import "./canvas-audio-model-picker.css";
import "./canvas-media-composer.css";

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
        <div className="canvas-audio-composer-controls canvas-media-controls-row">
            <div className="canvas-audio-composer-primary-controls">
                <ModelPicker
                    className="canvas-image-model-picker canvas-audio-model-picker canvas-media-model-picker canvas-media-model-picker-slot"
                    fullWidth
                    config={config}
                    value={config.model}
                    onChange={(model) => onConfigChange({ model })}
                    capability="audio"
                    onMissingConfig={handleMissingSystemModel}
                    showSelectedEstimate={false}
                    presentation="canvasAudio"
                />
                <span className="canvas-image-toolbar-divider canvas-audio-control-divider" aria-hidden="true" />
                <CanvasAudioVoicePicker className="canvas-media-control" config={config} value={config.audioVoice} onChange={(audioVoice) => onConfigChange({ audioVoice })} />
            </div>
            <div className="canvas-audio-composer-secondary-controls">
                <CanvasAudioSettingsPopover config={config} placement="topRight" iconOnly buttonClassName="canvas-audio-settings-trigger canvas-media-control" onConfigChange={(key, value) => onConfigChange(audioConfigPatch(key, value))} />
                <span className="canvas-audio-character-count canvas-media-meta" aria-label={`已输入 ${promptLength} 个字符`}>
                    {promptLength.toLocaleString()}/50,000
                </span>
                {credits !== null ? (
                    <span className="canvas-audio-generation-cost canvas-media-meta" title="本次生成消耗">
                        <CreditSymbol />
                        {credits.toLocaleString()}
                    </span>
                ) : null}
                <CanvasSubmitButton state={isRunning ? "stop" : "ready"} disabled={submitDisabled} onClick={isRunning ? onStop : onSubmit} ariaLabel={isRunning ? "停止生成音频" : "生成音频"} />
            </div>
        </div>
    );
}

function audioConfigPatch(key: CanvasAudioSettingKey, value: string): Partial<CanvasNodeMetadata> {
    return { [key]: value };
}
