import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { App, Button, Form, Input, Select, Switch, Tag } from "antd";
import { CheckCircle2, CircleAlert, FileCheck2, FileText, Image as ImageIcon, Megaphone, RectangleHorizontal, Save, Trash2, Upload } from "lucide-react";
import { useEffect, useRef, useState, type ChangeEvent } from "react";

import { staticAssetURL } from "@/lib/static-assets";
import { adminSiteSettingsQueryKey, getAdminSiteSettings, publicSiteSettingsQueryKey, removeAdminMarketingPopupImage, removeAdminSiteLogo, updateAdminSiteSettings, uploadAdminMarketingPopupImage, uploadAdminSiteLogo, type SiteSettings, type UpdateSiteSettingsInput } from "@/services/api/site-settings";
import { AdminPageFrame } from "../components/admin-shell";
import { AdminContentError, AdminContentSkeleton, AdminSettingsSection, AdminSettingsSwitchPanel } from "../components/admin-ui";

export default function SiteSettingsPage() {
    const { message, modal } = App.useApp();
    const [form] = Form.useForm<UpdateSiteSettingsInput>();
    const queryClient = useQueryClient();
    const logoInputRef = useRef<HTMLInputElement>(null);
    const marketingImageInputRef = useRef<HTMLInputElement>(null);
    const synchronizedValuesRef = useRef<UpdateSiteSettingsInput | null>(null);
    const [dirty, setDirty] = useState(false);
    const settingQuery = useQuery({
        queryKey: adminSiteSettingsQueryKey,
        queryFn: getAdminSiteSettings,
    });

    useEffect(() => {
        if (settingQuery.data && !dirty) {
            const values = toFormValues(settingQuery.data);
            form.setFieldsValue(values);
            synchronizedValuesRef.current = values;
            setDirty(false);
        }
    }, [dirty, form, settingQuery.data]);

    const synchronizeSetting = (setting: SiteSettings) => {
        queryClient.setQueryData(adminSiteSettingsQueryKey, setting);
        queryClient.setQueryData(publicSiteSettingsQueryKey, setting);
    };

    const saveMutation = useMutation({
        mutationFn: updateAdminSiteSettings,
        onSuccess: (setting) => {
            synchronizeSetting(setting);
            const values = toFormValues(setting);
            form.setFieldsValue(values);
            synchronizedValuesRef.current = values;
            setDirty(false);
            message.success("站点设置已保存并生效");
        },
        onError: (error) => message.error(error instanceof Error ? error.message : "保存站点设置失败"),
    });

    const logoMutation = useMutation({
        mutationFn: uploadAdminSiteLogo,
        onSuccess: (setting) => {
            synchronizeSetting(setting);
            message.success("站点 Logo 已更新");
        },
        onError: (error) => message.error(error instanceof Error ? error.message : "上传 Logo 失败"),
    });

    const removeLogoMutation = useMutation({
        mutationFn: removeAdminSiteLogo,
        onSuccess: (setting) => {
            synchronizeSetting(setting);
            message.success("已恢复内置 Logo");
        },
        onError: (error) => message.error(error instanceof Error ? error.message : "移除 Logo 失败"),
    });

    const marketingImageMutation = useMutation({
        mutationFn: uploadAdminMarketingPopupImage,
        onSuccess: (setting) => {
            synchronizeSetting(setting);
            message.success("营销弹窗图片已更新");
        },
        onError: (error) => message.error(error instanceof Error ? error.message : "上传营销图片失败"),
    });

    const removeMarketingImageMutation = useMutation({
        mutationFn: removeAdminMarketingPopupImage,
        onSuccess: (setting) => {
            synchronizeSetting(setting);
            message.success("营销弹窗图片已移除");
        },
        onError: (error) => message.error(error instanceof Error ? error.message : "移除营销图片失败"),
    });

    const save = async () => {
        const values = await form.validateFields();
        saveMutation.mutate(values);
    };

    const trackDirtyState = () => {
        const synchronizedValues = synchronizedValuesRef.current;
        if (!synchronizedValues) {
            setDirty(false);
            return;
        }
        setDirty(!siteSettingsInputEqual(synchronizedValues, form.getFieldsValue(true)));
    };

    const selectLogo = (event: ChangeEvent<HTMLInputElement>) => {
        const file = event.target.files?.[0];
        event.target.value = "";
        if (!file) return;
        if (!["image/png", "image/jpeg", "image/webp"].includes(file.type)) {
            message.error("Logo 仅支持 PNG、JPG 或 WebP 格式");
            return;
        }
        if (file.size > 2 * 1024 * 1024) {
            message.error("Logo 文件大小不能超过 2MB");
            return;
        }
        logoMutation.mutate(file);
    };

    const confirmRemoveLogo = () => {
        modal.confirm({
            className: "admin-operation-modal site-settings-remove-logo-modal workspace-ui-scope",
            title: "恢复内置 Logo？",
            content: "当前上传的站点 Logo 将被移除，前台会恢复使用内置 Logo。",
            okText: "确认恢复",
            cancelText: "取消",
            okButtonProps: { danger: true },
            onOk: async () => {
                await removeLogoMutation.mutateAsync();
            },
        });
    };

    const selectMarketingImage = (event: ChangeEvent<HTMLInputElement>) => {
        const file = event.target.files?.[0];
        event.target.value = "";
        if (!file) return;
        if (!["image/png", "image/jpeg", "image/webp"].includes(file.type)) {
            message.error("营销图片仅支持 PNG、JPG 或 WebP 格式");
            return;
        }
        if (file.size > 8 * 1024 * 1024) {
            message.error("营销图片大小不能超过 8MB");
            return;
        }
        marketingImageMutation.mutate(file);
    };

    const confirmRemoveMarketingImage = () => {
        if (form.getFieldValue("marketingPopupEnabled")) {
            message.warning("请先关闭营销弹窗并保存，再移除图片");
            return;
        }
        modal.confirm({
            className: "admin-operation-modal site-settings-remove-marketing-image-modal workspace-ui-scope",
            title: "移除营销弹窗图片？",
            content: "图片会从站点受管素材中删除，后续重新启用前需要再次上传。",
            okText: "确认移除",
            cancelText: "取消",
            okButtonProps: { danger: true },
            onOk: async () => {
                await removeMarketingImageMutation.mutateAsync();
            },
        });
    };

    const setting = settingQuery.data;
    const operationPending = saveMutation.isPending || logoMutation.isPending || removeLogoMutation.isPending || marketingImageMutation.isPending || removeMarketingImageMutation.isPending;
    return (
        <AdminPageFrame
            title="站点与品牌"
            description="管理站点名称、品牌标识、首页运营横幅、底部版权与网站备案"
            actions={
                setting ? (
                    <Button
                        className="site-settings-save-button"
                        type="primary"
                        icon={<Save className="site-settings-save-icon size-4" />}
                        loading={saveMutation.isPending}
                        disabled={!dirty || logoMutation.isPending || removeLogoMutation.isPending}
                        onClick={() => void save()}
                    >
                        保存并生效
                    </Button>
                ) : null
            }
        >
            <div className="site-settings-page space-y-5">
                {settingQuery.isLoading && !setting ? <AdminContentSkeleton rows={12} label="正在加载站点配置" /> : null}
                {settingQuery.error ? (
                    <AdminContentError title={setting ? "站点配置刷新失败" : "站点配置读取失败"} description={settingQuery.error instanceof Error ? settingQuery.error.message : "读取站点配置失败"} onRetry={() => void settingQuery.refetch()} />
                ) : null}
                {setting ? (
                    <Form className="site-settings-form" form={form} layout="vertical" requiredMark={false} disabled={operationPending} onValuesChange={trackDirtyState}>
                        <div className={`site-settings-sync-status${dirty ? " is-dirty" : ""}`} role="status" aria-live="polite">
                            {dirty ? <CircleAlert className="site-settings-sync-icon size-4" aria-hidden="true" /> : <CheckCircle2 className="site-settings-sync-icon size-4" aria-hidden="true" />}
                            <span className="site-settings-sync-label">{dirty ? "有未保存的站点配置变更" : "公开页面配置已同步"}</span>
                            <span className="site-settings-sync-meta">
                                {setting.updatedAt ? `上次更新：${new Date(setting.updatedAt).toLocaleString("zh-CN", { hour12: false })}` : "当前使用系统默认站点配置"}
                            </span>
                        </div>
                        <div id="site-brand" className="site-settings-section-anchor">
                            <AdminSettingsSwitchPanel
                                icon={<ImageIcon className="site-settings-brand-icon size-4" />}
                                title="品牌信息"
                                description="站点名称会同步到浏览器标题与主要品牌入口，Logo 会用于首页、登录页和后台。"
                                status={
                                    <Tag className="site-settings-brand-status" color={setting?.logoUrl ? "success" : "default"}>
                                        {setting?.logoUrl ? "自定义 Logo" : "内置 Logo"}
                                    </Tag>
                                }
                            >
                                <AdminSettingsSection id="site-brand-identity-heading" icon={<ImageIcon className="site-settings-brand-identity-icon size-4" />} title="名称与标识" description="统一公开页面、登录入口和管理后台使用的品牌名称与图形标识。">
                                    <Form.Item
                                        className="site-settings-name-field mb-0"
                                        name="siteName"
                                        label="站点名称"
                                        rules={[
                                            { required: true, whitespace: true, message: "请输入站点名称" },
                                            { max: 40, message: "站点名称不能超过 40 个字符" },
                                        ]}
                                    >
                                        <Input className="site-settings-name-input" maxLength={40} showCount placeholder="例如：HMaigc" />
                                    </Form.Item>
                                    <div className="site-settings-logo-control">
                                        <span className="site-settings-logo-label mb-2 block text-sm text-foreground/85">站点 Logo</span>
                                        <div className="site-settings-logo-row flex items-center gap-3">
                                            <span className="site-settings-logo-preview grid size-16 shrink-0 place-items-center overflow-hidden rounded-lg bg-muted/45">
                                                <img className="site-settings-logo-image max-h-11 max-w-11 object-contain" src={setting?.logoUrl || staticAssetURL("/logo.svg")} alt={`${setting?.siteName || "站点"} Logo`} />
                                            </span>
                                            <div className="site-settings-logo-actions flex min-w-0 flex-col items-start gap-2">
                                                <input ref={logoInputRef} className="site-settings-logo-input !hidden" type="file" accept="image/png,image/jpeg,image/webp" onChange={selectLogo} />
                                                <div className="site-settings-logo-buttons flex flex-wrap gap-2">
                                                    <Button
                                                        className="site-settings-logo-upload"
                                                        icon={<Upload className="site-settings-logo-upload-icon size-4" />}
                                                        loading={logoMutation.isPending}
                                                        disabled={saveMutation.isPending || removeLogoMutation.isPending}
                                                        onClick={() => logoInputRef.current?.click()}
                                                    >
                                                        {setting?.logoUrl ? "替换" : "上传"}
                                                    </Button>
                                                    {setting?.logoUrl ? (
                                                        <Button
                                                            className="site-settings-logo-remove"
                                                            type="text"
                                                            danger
                                                            icon={<Trash2 className="site-settings-logo-remove-icon size-4" />}
                                                            loading={removeLogoMutation.isPending}
                                                            disabled={saveMutation.isPending || logoMutation.isPending}
                                                            onClick={confirmRemoveLogo}
                                                        >
                                                            恢复内置
                                                        </Button>
                                                    ) : null}
                                                </div>
                                                <span className="site-settings-logo-help text-[11px] leading-4 text-foreground/45">PNG、JPG 或 WebP，最大 2MB</span>
                                            </div>
                                        </div>
                                    </div>
                                </AdminSettingsSection>
                            </AdminSettingsSwitchPanel>
                        </div>

                        <div id="site-home-banner" className="site-settings-section-anchor">
                            <AdminSettingsSwitchPanel
                                icon={<Megaphone className="site-settings-banner-icon size-4" />}
                                title="首页顶部横幅"
                                description="配置桌面端首页顶部的运营横幅；手机端固定不展示，避免内容拥挤和截断。"
                                status={
                                    <Form.Item className="site-settings-banner-switch-field mb-0" name="homeBannerEnabled" valuePropName="checked">
                                        <Switch className="site-settings-banner-switch" checkedChildren="展示" unCheckedChildren="隐藏" />
                                    </Form.Item>
                                }
                            >
                                <AdminSettingsSection id="site-banner-content-heading" icon={<Megaphone className="site-settings-banner-content-icon size-4" />} title="横幅内容" description="用简短状态标签和一句完整文案说明当前运营活动。">
                                    <Form.Item className="site-settings-banner-label-field mb-0" name="homeBannerLabel" label="状态标签" rules={[{ max: 20, message: "状态标签不能超过 20 个字符" }]}>
                                        <Input className="site-settings-banner-label-input" maxLength={20} showCount placeholder="例如：招募中" />
                                    </Form.Item>
                                    <Form.Item
                                        className="site-settings-banner-text-field mb-0"
                                        name="homeBannerText"
                                        label="展示文案"
                                        dependencies={["homeBannerEnabled"]}
                                        rules={[{ max: 200, message: "展示文案不能超过 200 个字符" }, requiredWhenEnabled(form)]}
                                    >
                                        <Input.TextArea className="site-settings-banner-text-input" autoSize={{ minRows: 2, maxRows: 4 }} maxLength={200} showCount placeholder="输入桌面端首页顶部展示的运营文案" />
                                    </Form.Item>
                                </AdminSettingsSection>
                                <AdminSettingsSection id="site-banner-actions-heading" icon={<Megaphone className="site-settings-banner-actions-icon size-4" />} title="行动入口" description="按钮名称与链接必须成对填写；未配置的按钮不会在首页展示。">
                                    <Form.Item
                                        className="site-settings-banner-primary-label-field mb-0"
                                        name="homeBannerPrimaryActionLabel"
                                        label="主按钮名称"
                                        dependencies={["homeBannerPrimaryActionUrl"]}
                                        rules={[{ max: 20, message: "主按钮名称不能超过 20 个字符" }, pairedActionFieldRule("主按钮", () => form.getFieldValue("homeBannerPrimaryActionUrl"))]}
                                    >
                                        <Input className="site-settings-banner-primary-label-input" maxLength={20} placeholder="例如：立即投递" />
                                    </Form.Item>
                                    <Form.Item
                                        className="site-settings-banner-primary-url-field mb-0"
                                        name="homeBannerPrimaryActionUrl"
                                        label="主按钮链接"
                                        dependencies={["homeBannerPrimaryActionLabel"]}
                                        rules={[{ max: 500, message: "主按钮链接不能超过 500 个字符" }, pairedActionURLRule("主按钮", () => form.getFieldValue("homeBannerPrimaryActionLabel"))]}
                                    >
                                        <Input className="site-settings-banner-primary-url-input" maxLength={500} placeholder="https://example.com/apply" />
                                    </Form.Item>
                                    <Form.Item
                                        className="site-settings-banner-secondary-label-field mb-0"
                                        name="homeBannerSecondaryActionLabel"
                                        label="次按钮名称"
                                        dependencies={["homeBannerSecondaryActionUrl"]}
                                        rules={[{ max: 20, message: "次按钮名称不能超过 20 个字符" }, pairedActionFieldRule("次按钮", () => form.getFieldValue("homeBannerSecondaryActionUrl"))]}
                                    >
                                        <Input className="site-settings-banner-secondary-label-input" maxLength={20} placeholder="例如：了解详情" />
                                    </Form.Item>
                                    <Form.Item
                                        className="site-settings-banner-secondary-url-field mb-0"
                                        name="homeBannerSecondaryActionUrl"
                                        label="次按钮链接"
                                        dependencies={["homeBannerSecondaryActionLabel"]}
                                        rules={[{ max: 500, message: "次按钮链接不能超过 500 个字符" }, pairedActionURLRule("次按钮", () => form.getFieldValue("homeBannerSecondaryActionLabel"))]}
                                    >
                                        <Input className="site-settings-banner-secondary-url-input" maxLength={500} placeholder="https://example.com/details" />
                                    </Form.Item>
                                </AdminSettingsSection>
                            </AdminSettingsSwitchPanel>
                        </div>

                        <div id="site-marketing-popup" className="site-settings-section-anchor">
                            <AdminSettingsSwitchPanel
                                icon={<RectangleHorizontal className="site-settings-marketing-icon size-4" />}
                                title="登录后营销弹窗"
                                description="面向已登录用户展示重点活动或新品信息；内容变更后会作为新一轮活动重新展示。"
                                status={
                                    <Form.Item className="site-settings-marketing-switch-field mb-0" name="marketingPopupEnabled" valuePropName="checked">
                                        <Switch className="site-settings-marketing-switch" checkedChildren="展示" unCheckedChildren="隐藏" />
                                    </Form.Item>
                                }
                            >
                                <AdminSettingsSection id="site-marketing-visual-heading" icon={<ImageIcon className="site-settings-marketing-visual-icon size-4" />} title="活动视觉" description="推荐使用 16:9 横图，主体与文字保留安全边距；桌面与移动端会按比例裁切。">
                                    <div className="site-settings-marketing-image-control">
                                        <span className="site-settings-marketing-image-label mb-2 block text-sm text-foreground/85">展示图片</span>
                                        {setting.marketingPopupImageUrl ? (
                                            <div className="site-settings-marketing-image-preview overflow-hidden bg-muted/35">
                                                <img className="site-settings-marketing-image block aspect-video w-full object-cover" src={setting.marketingPopupImageUrl} alt="营销弹窗预览" />
                                            </div>
                                        ) : (
                                            <div className="site-settings-marketing-image-empty grid aspect-video place-items-center bg-muted/30 text-sm text-foreground/45">尚未上传营销图片</div>
                                        )}
                                        <input ref={marketingImageInputRef} className="site-settings-marketing-image-input !hidden" type="file" accept="image/png,image/jpeg,image/webp" onChange={selectMarketingImage} />
                                        <div className="site-settings-marketing-image-actions mt-3 flex flex-wrap items-center gap-2">
                                            <Button className="site-settings-marketing-image-upload" icon={<Upload className="site-settings-marketing-image-upload-icon size-4" />} loading={marketingImageMutation.isPending} onClick={() => marketingImageInputRef.current?.click()}>
                                                {setting.marketingPopupImageUrl ? "替换图片" : "上传图片"}
                                            </Button>
                                            {setting.marketingPopupImageUrl ? (
                                                <Button className="site-settings-marketing-image-remove" type="text" danger icon={<Trash2 className="site-settings-marketing-image-remove-icon size-4" />} loading={removeMarketingImageMutation.isPending} onClick={confirmRemoveMarketingImage}>
                                                    移除图片
                                                </Button>
                                            ) : null}
                                            <span className="site-settings-marketing-image-help text-[11px] leading-4 text-foreground/45">PNG、JPG 或 WebP，最大 8MB</span>
                                        </div>
                                    </div>
                                </AdminSettingsSection>
                                <AdminSettingsSection id="site-marketing-content-heading" icon={<Megaphone className="site-settings-marketing-content-icon size-4" />} title="活动内容" description="标题直接说明权益或新品，说明文字补充限制条件，按钮引导用户完成下一步。">
                                    <Form.Item className="site-settings-marketing-title-field mb-0" name="marketingPopupTitle" label="标题" dependencies={["marketingPopupEnabled"]} rules={[{ max: 80, message: "标题不能超过 80 个字符" }, requiredMarketingField(form, "标题")]}>
                                        <Input className="site-settings-marketing-title-input" maxLength={80} showCount placeholder="例如：Seedance 2.5 旗舰模型预售上线" />
                                    </Form.Item>
                                    <Form.Item className="site-settings-marketing-description-field mb-0" name="marketingPopupDescription" label="补充说明" rules={[{ max: 200, message: "补充说明不能超过 200 个字符" }]}>
                                        <Input.TextArea className="site-settings-marketing-description-input" autoSize={{ minRows: 2, maxRows: 4 }} maxLength={200} showCount placeholder="例如：预售加赠最高 60 条免费生成，最长可输出 30 秒视频" />
                                    </Form.Item>
                                    <Form.Item className="site-settings-marketing-action-label-field mb-0" name="marketingPopupActionLabel" label="按钮名称" dependencies={["marketingPopupActionUrl"]} rules={[{ max: 20, message: "按钮名称不能超过 20 个字符" }, pairedActionFieldRule("营销弹窗按钮", () => form.getFieldValue("marketingPopupActionUrl"))]}>
                                        <Input className="site-settings-marketing-action-label-input" maxLength={20} placeholder="例如：立即抢购" />
                                    </Form.Item>
                                    <Form.Item className="site-settings-marketing-action-url-field mb-0" name="marketingPopupActionUrl" label="跳转链接" dependencies={["marketingPopupActionLabel"]} rules={[{ max: 500, message: "跳转链接不能超过 500 个字符" }, pairedActionURLRule("营销弹窗按钮", () => form.getFieldValue("marketingPopupActionLabel"))]}>
                                        <Input className="site-settings-marketing-action-url-input" maxLength={500} placeholder="https://example.com/campaign" />
                                    </Form.Item>
                                    <Form.Item className="site-settings-marketing-frequency-field mb-0" name="marketingPopupFrequency" label="展示频率" rules={[{ required: true, message: "请选择展示频率" }]}>
                                        <Select
                                            className="site-settings-marketing-frequency-select"
                                            options={[
                                                { value: "once", label: "每轮活动仅展示一次" },
                                                { value: "daily", label: "每位用户每天展示一次" },
                                                { value: "session", label: "每次浏览器会话展示一次" },
                                            ]}
                                        />
                                    </Form.Item>
                                </AdminSettingsSection>
                            </AdminSettingsSwitchPanel>
                        </div>

                        <AdminSettingsSwitchPanel icon={<FileText className="site-settings-footer-icon size-4" />} title="页脚与备案" description="统一维护首页底部版权、ICP备案与公安备案展示信息。">
                            <AdminSettingsSection id="site-footer" icon={<FileText className="site-settings-footer-copy-icon size-4" />} title="底部版权" description="显示在首页底部，支持公司名称、年份和版权声明。">
                                <Form.Item className="site-settings-copyright-field mb-0" name="footerCopyright" label="版权文案" rules={[{ max: 200, message: "版权文案不能超过 200 个字符" }]}>
                                    <Input className="site-settings-copyright-input" maxLength={200} showCount placeholder="例如：© 2026 HMaigc. 保留所有权利。" />
                                </Form.Item>
                            </AdminSettingsSection>
                            <AdminSettingsSection id="site-registration" icon={<FileCheck2 className="site-settings-registration-icon size-4" />} title="网站备案" description="配置首页底部对外展示的 ICP 与公安备案信息，备案链接仅允许 HTTP 或 HTTPS。">
                                <Form.Item className="site-settings-icp-number-field mb-0" name="icpRegistrationNumber" label="ICP备案号" rules={[{ max: 100, message: "ICP备案号不能超过 100 个字符" }]}>
                                    <Input className="site-settings-icp-number-input" maxLength={100} showCount placeholder="例如：蜀ICP备2026000000号-1" />
                                </Form.Item>
                                <Form.Item
                                    className="site-settings-icp-url-field mb-0"
                                    name="icpRegistrationUrl"
                                    label="ICP备案链接"
                                    dependencies={["icpRegistrationNumber"]}
                                    rules={[{ max: 500, message: "ICP备案链接不能超过 500 个字符" }, optionalHTTPURLRule("ICP备案链接", () => form.getFieldValue("icpRegistrationNumber"))]}
                                >
                                    <Input className="site-settings-icp-url-input" maxLength={500} placeholder="https://beian.miit.gov.cn/" />
                                </Form.Item>
                                <Form.Item className="site-settings-security-number-field mb-0" name="publicSecurityRegistrationNumber" label="公安备案号" rules={[{ max: 100, message: "公安备案号不能超过 100 个字符" }]}>
                                    <Input className="site-settings-security-number-input" maxLength={100} showCount placeholder="例如：川公网安备51000000000000号" />
                                </Form.Item>
                                <Form.Item
                                    className="site-settings-security-url-field mb-0"
                                    name="publicSecurityRegistrationUrl"
                                    label="公安备案链接"
                                    dependencies={["publicSecurityRegistrationNumber"]}
                                    rules={[{ max: 500, message: "公安备案链接不能超过 500 个字符" }, optionalHTTPURLRule("公安备案链接", () => form.getFieldValue("publicSecurityRegistrationNumber"))]}
                                >
                                    <Input className="site-settings-security-url-input" maxLength={500} placeholder="http://www.beian.gov.cn/portal/registerSystemInfo?recordcode=..." />
                                </Form.Item>
                            </AdminSettingsSection>
                        </AdminSettingsSwitchPanel>

                    </Form>
                ) : null}
            </div>
        </AdminPageFrame>
    );
}

function toFormValues(setting: SiteSettings): UpdateSiteSettingsInput {
    return {
        siteName: setting.siteName,
        footerCopyright: setting.footerCopyright,
        icpRegistrationNumber: setting.icpRegistrationNumber,
        icpRegistrationUrl: setting.icpRegistrationUrl,
        publicSecurityRegistrationNumber: setting.publicSecurityRegistrationNumber,
        publicSecurityRegistrationUrl: setting.publicSecurityRegistrationUrl,
        homeBannerEnabled: setting.homeBannerEnabled,
        homeBannerLabel: setting.homeBannerLabel,
        homeBannerText: setting.homeBannerText,
        homeBannerPrimaryActionLabel: setting.homeBannerPrimaryActionLabel,
        homeBannerPrimaryActionUrl: setting.homeBannerPrimaryActionUrl,
        homeBannerSecondaryActionLabel: setting.homeBannerSecondaryActionLabel,
        homeBannerSecondaryActionUrl: setting.homeBannerSecondaryActionUrl,
        marketingPopupEnabled: setting.marketingPopupEnabled,
        marketingPopupTitle: setting.marketingPopupTitle,
        marketingPopupDescription: setting.marketingPopupDescription,
        marketingPopupActionLabel: setting.marketingPopupActionLabel,
        marketingPopupActionUrl: setting.marketingPopupActionUrl,
        marketingPopupFrequency: setting.marketingPopupFrequency,
    };
}

function siteSettingsInputEqual(left: UpdateSiteSettingsInput, right: UpdateSiteSettingsInput) {
    return JSON.stringify(normalizeSiteSettingsInput(left)) === JSON.stringify(normalizeSiteSettingsInput(right));
}

function normalizeSiteSettingsInput(input: UpdateSiteSettingsInput): UpdateSiteSettingsInput {
    return {
        siteName: input.siteName.trim(),
        footerCopyright: input.footerCopyright?.trim() || "",
        icpRegistrationNumber: input.icpRegistrationNumber?.trim() || "",
        icpRegistrationUrl: input.icpRegistrationUrl?.trim() || "",
        publicSecurityRegistrationNumber: input.publicSecurityRegistrationNumber?.trim() || "",
        publicSecurityRegistrationUrl: input.publicSecurityRegistrationUrl?.trim() || "",
        homeBannerEnabled: Boolean(input.homeBannerEnabled),
        homeBannerLabel: input.homeBannerLabel?.trim() || "",
        homeBannerText: input.homeBannerText?.trim() || "",
        homeBannerPrimaryActionLabel: input.homeBannerPrimaryActionLabel?.trim() || "",
        homeBannerPrimaryActionUrl: input.homeBannerPrimaryActionUrl?.trim() || "",
        homeBannerSecondaryActionLabel: input.homeBannerSecondaryActionLabel?.trim() || "",
        homeBannerSecondaryActionUrl: input.homeBannerSecondaryActionUrl?.trim() || "",
        marketingPopupEnabled: Boolean(input.marketingPopupEnabled),
        marketingPopupTitle: input.marketingPopupTitle?.trim() || "",
        marketingPopupDescription: input.marketingPopupDescription?.trim() || "",
        marketingPopupActionLabel: input.marketingPopupActionLabel?.trim() || "",
        marketingPopupActionUrl: input.marketingPopupActionUrl?.trim() || "",
        marketingPopupFrequency: input.marketingPopupFrequency,
    };
}

function requiredMarketingField(form: ReturnType<typeof Form.useForm<UpdateSiteSettingsInput>>[0], label: string) {
    return {
        async validator(_rule: unknown, value?: string) {
            if (form.getFieldValue("marketingPopupEnabled") && !value?.trim()) {
                throw new Error(`启用营销弹窗时必须填写${label}`);
            }
        },
    };
}

function requiredWhenEnabled(form: ReturnType<typeof Form.useForm<UpdateSiteSettingsInput>>[0]) {
    return {
        async validator(_rule: unknown, value?: string) {
            if (form.getFieldValue("homeBannerEnabled") && !value?.trim()) {
                throw new Error("启用首页横幅时必须填写展示文案");
            }
        },
    };
}

function pairedActionFieldRule(label: string, getURL: () => string | undefined) {
    return {
        async validator(_rule: unknown, value?: string) {
            if (Boolean(value?.trim()) !== Boolean(getURL()?.trim())) {
                throw new Error(`${label}名称与链接必须同时填写`);
            }
        },
    };
}

function pairedActionURLRule(label: string, getLabel: () => string | undefined) {
    return {
        async validator(_rule: unknown, value?: string) {
            const normalized = value?.trim();
            if (Boolean(normalized) !== Boolean(getLabel()?.trim())) {
                throw new Error(`${label}名称与链接必须同时填写`);
            }
            if (!normalized) return;
            try {
                const parsed = new URL(normalized);
                if (!["http:", "https:"].includes(parsed.protocol) || !parsed.hostname || parsed.username || parsed.password) {
                    throw new Error("invalid action URL");
                }
            } catch {
                throw new Error(`${label}链接必须是有效的 HTTP 或 HTTPS 地址`);
            }
        },
    };
}

function optionalHTTPURLRule(label: string, getRegistrationNumber: () => string | undefined) {
    return {
        async validator(_rule: unknown, value?: string) {
            const normalized = value?.trim();
            if (!normalized) return;
            if (!getRegistrationNumber()?.trim()) {
                throw new Error(`填写${label}时必须同时填写对应备案号`);
            }
            try {
                const parsed = new URL(normalized);
                if (!["http:", "https:"].includes(parsed.protocol) || !parsed.hostname || parsed.username || parsed.password) {
                    throw new Error("invalid registration URL");
                }
            } catch {
                throw new Error(`${label}必须是有效的 HTTP 或 HTTPS 地址`);
            }
        },
    };
}
