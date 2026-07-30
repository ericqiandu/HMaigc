import { Switch } from "antd";
import { Stamp } from "lucide-react";

type WatermarkSettingsProps = {
    enabled: boolean;
    onEnabledChange: (enabled: boolean) => void;
};

export function WatermarkSettings({ enabled, onEnabledChange }: WatermarkSettingsProps) {
    return (
        <section className="settings-watermark-section">
            <div className="settings-watermark-heading mb-5">
                <h2 className="settings-watermark-title text-sm font-semibold">AI 视频水印</h2>
                <p className="settings-watermark-description mt-1 max-w-xl text-xs text-foreground/55">统一控制新生成视频是否携带模型服务商提供的 AI 水印。节点内单独配置时，以节点设置为准。</p>
            </div>
            <div className="settings-watermark-option flex max-w-2xl items-center gap-4 bg-foreground/[.035] px-4 py-3">
                <span className="settings-watermark-option-icon grid size-8 shrink-0 place-items-center text-foreground/55" aria-hidden="true">
                    <Stamp className="settings-watermark-stamp-icon size-4" />
                </span>
                <span className="settings-watermark-option-copy min-w-0 flex-1">
                    <span className="settings-watermark-option-label block text-[13px] font-semibold">生成视频时添加 AI 水印</span>
                    <span className="settings-watermark-option-description mt-0.5 block text-xs text-foreground/48">开启后会将水印参数传递给支持该能力的视频模型。</span>
                </span>
                <Switch className="settings-watermark-switch shrink-0" size="small" checked={enabled} onChange={onEnabledChange} aria-label="生成视频时添加 AI 水印" />
            </div>
        </section>
    );
}
