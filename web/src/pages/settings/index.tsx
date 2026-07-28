import { Form, Input, InputNumber, Select } from "antd";
import { Cloud, SlidersHorizontal } from "lucide-react";
import { useEffect, useState, type ReactNode } from "react";
import { useNavigate, useSearchParams } from "react-router";

import { UserOSSSettingsForm } from "@/components/layout/user-oss-settings-form";
import { PageHeader } from "@/components/layout/workspace-page";
import { audioFormatOptions, audioVoiceOptions, normalizeAudioSpeedValue } from "@/lib/audio-generation";
import { defaultConfig, useConfigStore } from "@/stores/use-config-store";

type SettingsSection = "preferences" | "storage";

const settingsSections: Array<{ key: SettingsSection; label: string; description: string; icon: ReactNode }> = [
    { key: "preferences", label: "生成偏好", description: "画布与音频默认值", icon: <SlidersHorizontal className="size-4" /> },
    { key: "storage", label: "我的 OSS", description: "管理个人媒体存储", icon: <Cloud className="size-4" /> },
];

function isSettingsSection(value: string | null): value is SettingsSection {
    return settingsSections.some((section) => section.key === value);
}

export default function SettingsPage() {
    const navigate = useNavigate();
    const [searchParams, setSearchParams] = useSearchParams();
    const requestedSection = searchParams.get("section");
    const [activeSection, setActiveSection] = useState<SettingsSection>(isSettingsSection(requestedSection) ? requestedSection : "preferences");
    const config = useConfigStore((state) => state.config);
    const updateConfig = useConfigStore((state) => state.updateConfig);
    const shouldReturnToCreation = searchParams.get("continue") === "1";

    useEffect(() => {
        if (isSettingsSection(requestedSection)) {
            setActiveSection(requestedSection);
            return;
        }
        const next = new URLSearchParams(searchParams);
        next.set("section", "preferences");
        setSearchParams(next, { replace: true });
    }, [requestedSection, searchParams, setSearchParams]);

    const selectSection = (section: SettingsSection) => {
        setActiveSection(section);
        const next = new URLSearchParams(searchParams);
        next.set("section", section);
        setSearchParams(next, { replace: true });
    };

    return (
        <main className="settings-workspace-page flex h-full min-h-0 flex-col bg-background px-4 pb-8 pt-20 text-foreground sm:px-6 md:px-[104px] md:pt-[90px]">
            <PageHeader
                title="个人设置"
                description="管理生成偏好与个人媒体存储"
                onBack={shouldReturnToCreation ? () => navigate(-1) : undefined}
                backLabel={shouldReturnToCreation ? "返回创作页面" : "返回首页"}
            />
            <div className="settings-page-body mt-4 flex min-h-0 flex-1 flex-col gap-4 md:flex-row">
                <aside className="settings-section-nav w-full shrink-0 bg-muted/[.18] p-2 md:w-[208px] md:rounded-lg" aria-label="个人设置分类">
                    <nav className="thin-scrollbar flex gap-1 overflow-x-auto md:block md:space-y-1">
                        {settingsSections.map((item) => {
                            const selected = item.key === activeSection;
                            return (
                                <button
                                    key={item.key}
                                    type="button"
                                    className={`settings-section-button flex min-w-[176px] items-center gap-3 rounded-md px-3 py-2 text-left transition-colors md:w-full ${selected ? "bg-foreground text-background" : "text-foreground/62 hover:bg-foreground/[.05] hover:text-foreground"}`}
                                    aria-current={selected ? "page" : undefined}
                                    onClick={() => selectSection(item.key)}
                                >
                                    <span className="settings-section-icon shrink-0">{item.icon}</span>
                                    <span className="settings-section-copy min-w-0">
                                        <span className="settings-section-label block truncate text-[13px] font-medium">{item.label}</span>
                                        <span className={`settings-section-description mt-0.5 block truncate text-[10px] ${selected ? "text-background/60" : "text-foreground/42"}`}>{item.description}</span>
                                    </span>
                                </button>
                            );
                        })}
                    </nav>
                </aside>

                <section className="settings-content-panel min-h-0 min-w-0 flex-1">
                    <div className="settings-content-scroll thin-scrollbar h-full overflow-y-auto">
                        {activeSection === "preferences" ? (
                            <PreferencesSettings config={config} updateConfig={updateConfig} />
                        ) : (
                            <div className="settings-storage-pane">
                                <UserOSSSettingsForm />
                            </div>
                        )}
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
                <div className="settings-audio-grid grid gap-4 md:grid-cols-3">
                    <Form.Item label="默认声音" className="settings-audio-field mb-0">
                        <Select value={config.audioVoice} options={audioVoiceOptions} onChange={(value) => updateConfig("audioVoice", value)} />
                    </Form.Item>
                    <Form.Item label="文件格式" className="settings-audio-field mb-0">
                        <Select value={config.audioFormat} options={audioFormatOptions} onChange={(value) => updateConfig("audioFormat", value)} />
                    </Form.Item>
                    <Form.Item label="语速" className="settings-audio-field mb-0">
                        <InputNumber
                            min={0.25}
                            max={4}
                            step={0.05}
                            precision={2}
                            className="settings-audio-speed-input w-full"
                            value={Number(config.audioSpeed)}
                            onChange={(value) => updateConfig("audioSpeed", normalizeAudioSpeedValue(String(value ?? defaultConfig.audioSpeed)))}
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
