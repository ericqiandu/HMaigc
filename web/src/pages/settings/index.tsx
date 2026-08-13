import { Form, Input, InputNumber, Select } from "antd";
import { useNavigate, useSearchParams } from "react-router";

import { PageHeader } from "@/components/layout/workspace-page";
import { audioFormatOptionsForInterface, audioSpeedRangeForInterface, normalizeAudioSpeedValue } from "@/lib/audio-generation";
import { defaultConfig, resolveModelChannel, useConfigStore } from "@/stores/use-config-store";

export default function SettingsPage() {
    const navigate = useNavigate();
    const [searchParams] = useSearchParams();
    const config = useConfigStore((state) => state.config);
    const updateConfig = useConfigStore((state) => state.updateConfig);
    const shouldReturnToCreation = searchParams.get("continue") === "1";

    return (
        <main className="settings-workspace-page flex h-full min-h-0 flex-col bg-background px-4 pb-8 pt-20 text-foreground sm:px-6 md:px-[104px] md:pt-[90px]">
            <PageHeader title="个人设置" description="管理生成任务的个人默认值" onBack={shouldReturnToCreation ? () => navigate(-1) : undefined} backLabel={shouldReturnToCreation ? "返回创作页面" : "返回首页"} />
            <div className="settings-page-body mt-4 min-h-0 flex-1">
                <section className="settings-content-panel mx-auto h-full min-h-0 w-full max-w-5xl">
                    <div className="settings-content-scroll thin-scrollbar h-full overflow-y-auto">
                        <PreferencesSettings config={config} updateConfig={updateConfig} />
                    </div>
                </section>
            </div>
        </main>
    );
}

type PreferencesSettingsProps = {
    config: ReturnType<typeof useConfigStore.getState>["config"];
    updateConfig: ReturnType<typeof useConfigStore.getState>["updateConfig"];
};

function PreferencesSettings({ config, updateConfig }: PreferencesSettingsProps) {
    const audioChannel = resolveModelChannel(config, config.audioModel);
    const audioFormatOptions = audioFormatOptionsForInterface(audioChannel.interfaceType);
    const audioSpeedRange = audioSpeedRangeForInterface(audioChannel.interfaceType);

    return (
        <Form className="settings-preferences-form" layout="vertical" requiredMark={false}>
            <section className="settings-preference-section border-b border-border pb-6">
                <div className="settings-preference-heading mb-4">
                    <h2 className="settings-preference-title text-sm font-semibold">画布生成</h2>
                    <p className="settings-preference-description mt-1 text-xs text-foreground/55">设置新建任务的初始值，节点内仍可单独调整。</p>
                </div>
                <Form.Item label="默认生图张数" className="settings-image-count-field mb-0 max-w-xs">
                    <InputNumber
                        min={1}
                        max={15}
                        precision={0}
                        className="settings-image-count-input w-full"
                        value={Number(config.canvasImageCount)}
                        onChange={(value) => updateConfig("canvasImageCount", normalizeImageCount(String(value ?? defaultConfig.canvasImageCount)))}
                    />
                </Form.Item>
            </section>

            <section className="settings-preference-section border-b border-border py-6">
                <div className="settings-preference-heading mb-4">
                    <h2 className="settings-preference-title text-sm font-semibold">音频默认值</h2>
                    <p className="settings-preference-description mt-1 text-xs text-foreground/55">用于新建音频节点和未单独设置参数的任务。</p>
                </div>
                <div className="settings-audio-grid grid gap-4 md:grid-cols-2">
                    <Form.Item label="文件格式" className="settings-audio-field mb-0">
                        <Select value={config.audioFormat} options={audioFormatOptions} onChange={(value) => updateConfig("audioFormat", value)} />
                    </Form.Item>
                    <Form.Item label="语速" className="settings-audio-field mb-0">
                        <InputNumber
                            min={audioSpeedRange.min}
                            max={audioSpeedRange.max}
                            step={0.05}
                            precision={2}
                            className="settings-audio-speed-input w-full"
                            value={Number(config.audioSpeed)}
                            onChange={(value) => updateConfig("audioSpeed", normalizeAudioSpeedValue(String(value ?? defaultConfig.audioSpeed), audioChannel.interfaceType))}
                        />
                    </Form.Item>
                </div>
            </section>

            <section className="settings-preference-section pt-6">
                <div className="settings-preference-heading mb-4">
                    <h2 className="settings-preference-title text-sm font-semibold">默认指令</h2>
                    <p className="settings-preference-description mt-1 text-xs text-foreground/55">在未单独填写时附加到对应生成请求。</p>
                </div>
                <div className="settings-instruction-grid grid gap-5 lg:grid-cols-2">
                    <Form.Item label="音频指令" className="settings-instruction-field mb-0">
                        <Input.TextArea rows={5} value={config.audioInstructions} placeholder="例如：自然、温暖、适合旁白。" onChange={(event) => updateConfig("audioInstructions", event.target.value)} />
                    </Form.Item>
                    <Form.Item label="系统提示词" className="settings-instruction-field mb-0">
                        <Input.TextArea rows={5} value={config.systemPrompt} placeholder="例如：你是一位擅长电影感写实摄影的视觉导演。" onChange={(event) => updateConfig("systemPrompt", event.target.value)} />
                    </Form.Item>
                </div>
            </section>
        </Form>
    );
}

function normalizeImageCount(value: string) {
    return String(Math.max(1, Math.min(15, Math.floor(Math.abs(Number(value)) || Number(defaultConfig.canvasImageCount)))));
}
