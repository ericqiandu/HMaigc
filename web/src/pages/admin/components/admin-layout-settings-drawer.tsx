import { Alert, App, Button, Drawer, Input, Switch } from "antd";
import { RotateCcw } from "lucide-react";
import { useEffect, useState, type CSSProperties, type ReactNode } from "react";

import { DEFAULT_ADMIN_LAYOUT_SETTINGS, isAdminBrandColor, useAdminLayoutSettings, type AdminContentWidth, type AdminMenuTheme } from "../admin-layout-settings";
import "../admin-layout-settings.css";
import { useAdminTheme } from "../admin-theme";

type AdminLayoutSettingsDrawerProps = {
    open: boolean;
    onClose: () => void;
};

const BRAND_PRESETS = [
    { label: "专业蓝", value: "#2979C9" },
    { label: "科技青", value: "#08979C" },
    { label: "稳重紫", value: "#722ED1" },
] as const;

export function AdminLayoutSettingsDrawer({ open, onClose }: AdminLayoutSettingsDrawerProps) {
    const { message } = App.useApp();
    const { getPortalContainer } = useAdminTheme();
    const { persistenceError, resetSettings, settings, updateSettings } = useAdminLayoutSettings();
    const [colorDraft, setColorDraft] = useState(settings.colorPrimary);
    const [colorError, setColorError] = useState<string | null>(null);

    useEffect(() => {
        if (!colorError) setColorDraft(settings.colorPrimary);
    }, [colorError, settings.colorPrimary]);

    const updateColor = (value: string) => {
        setColorDraft(value);
        if (!isAdminBrandColor(value)) {
            setColorError("请输入完整的 #RRGGBB 颜色值");
            return;
        }
        const normalized = value.toUpperCase();
        setColorDraft(normalized);
        setColorError(null);
        updateSettings({ colorPrimary: normalized });
    };

    const resetAllSettings = () => {
        const persisted = resetSettings();
        setColorDraft(DEFAULT_ADMIN_LAYOUT_SETTINGS.colorPrimary);
        setColorError(null);
        void message[persisted ? "success" : "warning"](persisted ? "已恢复并保存后台默认界面设置" : "已恢复当前预览，但无法保存到此浏览器");
    };

    return (
        <Drawer
            rootClassName="admin-layout-settings-drawer"
            className="admin-layout-settings-drawer-panel"
            title={
                <div className="admin-layout-settings-heading">
                    <span className="admin-layout-settings-title">界面设置</span>
                    <span className="admin-layout-settings-subtitle">实时预览，仅影响管理后台</span>
                </div>
            }
            placement="right"
            size="min(420px, calc(100vw - 24px))"
            open={open}
            onClose={onClose}
            getContainer={getPortalContainer}
            destroyOnHidden
            footer={
                <div className="admin-layout-settings-footer">
                    <Button className="admin-layout-reset-button" type="text" icon={<RotateCcw className="admin-layout-reset-icon size-4" />} onClick={resetAllSettings}>
                        恢复默认
                    </Button>
                    <Button className="admin-layout-done-button" type="primary" onClick={onClose}>
                        完成
                    </Button>
                </div>
            }
        >
            <div className="admin-layout-settings-body">
                {persistenceError ? <Alert className="admin-layout-persistence-error" type="error" showIcon title="当前预览已生效，但无法保存到此浏览器" description={persistenceError} /> : null}

                <SettingsSection title="整体主题" description="仅影响管理后台，不改变创作端外观。">
                    <div className="admin-layout-choice-grid admin-layout-choice-grid-two" role="group" aria-label="后台整体主题">
                        <ChoiceButton className="admin-layout-theme-light" active={settings.theme === "light"} label="浅色" description="明亮内容表面" preview="light" onClick={() => updateSettings({ theme: "light" })} />
                        <ChoiceButton className="admin-layout-theme-dark" active={settings.theme === "dark"} label="深色" description="低照度工作环境" preview="dark" onClick={() => updateSettings({ theme: "dark" })} />
                    </div>
                </SettingsSection>

                <SettingsSection title="菜单主题" description="可跟随整体主题，也可单独指定菜单明暗。">
                    <div className="admin-layout-choice-grid admin-layout-choice-grid-three" role="group" aria-label="后台菜单主题">
                        {(["follow", "light", "dark"] as const).map((menuTheme) => (
                            <ChoiceButton key={menuTheme} className={`admin-layout-menu-${menuTheme}`} active={settings.menuTheme === menuTheme} label={menuThemeLabel(menuTheme)} onClick={() => updateSettings({ menuTheme })} />
                        ))}
                    </div>
                </SettingsSection>

                <SettingsSection title="品牌与密度" description="品牌色会同步应用到后台主操作和选中状态。">
                    <div className="admin-layout-color-presets" role="group" aria-label="后台品牌色预设">
                        {BRAND_PRESETS.map((preset) => (
                            <button
                                key={preset.value}
                                type="button"
                                className="admin-layout-color-preset"
                                aria-label={`使用${preset.label}`}
                                aria-pressed={settings.colorPrimary === preset.value}
                                data-active={settings.colorPrimary === preset.value ? "true" : "false"}
                                style={{ "--admin-color-swatch": preset.value } as CSSProperties}
                                onClick={() => updateColor(preset.value)}
                            >
                                <span className="admin-layout-color-swatch" aria-hidden="true" />
                                <span className="admin-layout-color-label">{preset.label}</span>
                            </button>
                        ))}
                    </div>
                    <div className="admin-layout-color-field">
                        <label className="admin-layout-field-label" htmlFor="admin-layout-custom-color">
                            自定义品牌色
                        </label>
                        <Input
                            id="admin-layout-custom-color"
                            className="admin-layout-color-input"
                            aria-label="自定义后台品牌色"
                            aria-invalid={Boolean(colorError)}
                            value={colorDraft}
                            status={colorError ? "error" : undefined}
                            maxLength={7}
                            onChange={(event) => updateColor(event.target.value)}
                        />
                        {colorError ? (
                            <span className="admin-layout-color-error" role="alert">
                                {colorError}
                            </span>
                        ) : null}
                    </div>
                    <div className="admin-layout-field-block">
                        <span className="admin-layout-field-label">侧栏宽度</span>
                        <div className="admin-layout-choice-grid admin-layout-choice-grid-three" role="group" aria-label="后台侧栏宽度">
                            {([208, 236, 272] as const).map((siderWidth) => (
                                <ChoiceButton key={siderWidth} className={`admin-layout-sider-${siderWidth}`} active={settings.siderWidth === siderWidth} label={siderWidthLabel(siderWidth)} onClick={() => updateSettings({ siderWidth })} />
                            ))}
                        </div>
                    </div>
                </SettingsSection>

                <SettingsSection title="页面布局" description="控制业务内容的阅读宽度与顶部栏滚动方式。">
                    <div className="admin-layout-choice-grid admin-layout-choice-grid-two" role="group" aria-label="后台内容宽度">
                        {(["fixed", "fluid"] as const).map((contentWidth) => (
                            <ChoiceButton
                                key={contentWidth}
                                className={`admin-layout-content-${contentWidth}`}
                                active={settings.contentWidth === contentWidth}
                                label={contentWidthLabel(contentWidth)}
                                description={contentWidth === "fixed" ? "聚焦阅读宽度" : "充分利用屏幕"}
                                preview={contentWidth}
                                onClick={() => updateSettings({ contentWidth })}
                            />
                        ))}
                    </div>
                    <div className="admin-layout-switch-row">
                        <div className="admin-layout-switch-copy">
                            <span className="admin-layout-switch-title">固定顶部栏</span>
                            <span className="admin-layout-switch-description">关闭后桌面顶部栏随页面内容滚动。</span>
                        </div>
                        <Switch className="admin-layout-fixed-header" aria-label="固定后台顶部栏" checked={settings.fixedHeader} onChange={(fixedHeader) => updateSettings({ fixedHeader })} />
                    </div>
                </SettingsSection>
            </div>
        </Drawer>
    );
}

function SettingsSection({ children, description, title }: { children: ReactNode; description: string; title: string }) {
    return (
        <section className="admin-layout-settings-section">
            <div className="admin-layout-settings-section-heading">
                <h2 className="admin-layout-settings-section-title">{title}</h2>
                <p className="admin-layout-settings-section-description">{description}</p>
            </div>
            <div className="admin-layout-settings-section-content">{children}</div>
        </section>
    );
}

type ChoicePreview = "dark" | "fixed" | "fluid" | "light";

function ChoiceButton({ active, className, description, label, onClick, preview }: { active: boolean; className: string; description?: string; label: string; onClick: () => void; preview?: ChoicePreview }) {
    return (
        <button type="button" className={`admin-layout-choice ${className}`} data-active={active ? "true" : "false"} aria-pressed={active} onClick={onClick}>
            {preview ? (
                <span className={`admin-layout-choice-preview is-${preview}`} aria-hidden="true">
                    <span className="admin-layout-choice-preview-sider" />
                    <span className="admin-layout-choice-preview-content" />
                </span>
            ) : null}
            <span className="admin-layout-choice-copy">
                <span className="admin-layout-choice-label">{label}</span>
                {description ? <span className="admin-layout-choice-description">{description}</span> : null}
            </span>
        </button>
    );
}

function menuThemeLabel(menuTheme: AdminMenuTheme) {
    const labels: Record<AdminMenuTheme, string> = { follow: "跟随", light: "浅色", dark: "深色" };
    return labels[menuTheme];
}

function contentWidthLabel(contentWidth: AdminContentWidth) {
    const labels: Record<AdminContentWidth, string> = { fixed: "固定", fluid: "流式" };
    return labels[contentWidth];
}

function siderWidthLabel(siderWidth: 208 | 236 | 272) {
    const labels: Record<typeof siderWidth, string> = { 208: "紧凑", 236: "标准", 272: "宽松" };
    return labels[siderWidth];
}
