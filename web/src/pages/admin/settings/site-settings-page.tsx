import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Alert, App, Button, Form, Input, Skeleton, Tag } from "antd";
import { FileCheck2, FileText, Image as ImageIcon, RefreshCw, Save, ShieldCheck, Trash2, Upload } from "lucide-react";
import { useEffect, useRef, type ChangeEvent } from "react";

import { staticAssetURL } from "@/lib/static-assets";
import { adminSiteSettingsQueryKey, getAdminSiteSettings, publicSiteSettingsQueryKey, removeAdminSiteLogo, updateAdminSiteSettings, uploadAdminSiteLogo, type SiteSettings, type UpdateSiteSettingsInput } from "@/services/api/site-settings";
import { AdminPageFrame } from "../components/admin-shell";
import { SettingsSectionCard } from "../components/admin-ui";

const { TextArea } = Input;

export default function SiteSettingsPage() {
    const { message, modal } = App.useApp();
    const [form] = Form.useForm<UpdateSiteSettingsInput>();
    const queryClient = useQueryClient();
    const logoInputRef = useRef<HTMLInputElement>(null);
    const settingQuery = useQuery({
        queryKey: adminSiteSettingsQueryKey,
        queryFn: getAdminSiteSettings,
    });

    useEffect(() => {
        if (settingQuery.data) {
            form.setFieldsValue(toFormValues(settingQuery.data));
        }
    }, [form, settingQuery.data]);

    const synchronizeSetting = (setting: SiteSettings) => {
        queryClient.setQueryData(adminSiteSettingsQueryKey, setting);
        queryClient.setQueryData(publicSiteSettingsQueryKey, setting);
    };

    const saveMutation = useMutation({
        mutationFn: updateAdminSiteSettings,
        onSuccess: (setting) => {
            synchronizeSetting(setting);
            form.setFieldsValue(toFormValues(setting));
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

    const save = async () => {
        const values = await form.validateFields();
        saveMutation.mutate(values);
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
            title: "恢复内置 Logo？",
            content: "当前上传的站点 Logo 将被移除，前台会恢复使用内置 Logo。",
            okText: "确认恢复",
            cancelText: "取消",
            onOk: async () => {
                await removeLogoMutation.mutateAsync();
            },
        });
    };

    const setting = settingQuery.data;
    const legalConfigured = Boolean(setting?.userAgreement.trim() && setting?.privacyPolicy.trim());

    return (
        <AdminPageFrame title="站点设置" description="统一管理站点品牌、底部版权、网站备案与公开法律内容">
            <div className="site-settings-page mx-auto max-w-5xl space-y-5">
                {settingQuery.error ? (
                    <Alert
                        className="site-settings-load-error"
                        type="error"
                        showIcon
                        title="站点设置加载失败"
                        description={settingQuery.error instanceof Error ? settingQuery.error.message : "请稍后重试"}
                        action={
                            <Button className="site-settings-retry-button" icon={<RefreshCw className="site-settings-retry-icon size-4" />} onClick={() => void settingQuery.refetch()}>
                                重试
                            </Button>
                        }
                    />
                ) : null}

                {settingQuery.isLoading ? (
                    <Skeleton className="site-settings-skeleton" active paragraph={{ rows: 12 }} />
                ) : (
                    <Form className="site-settings-form space-y-5" form={form} layout="vertical" requiredMark={false}>
                        <SettingsSectionCard
                            icon={<ImageIcon className="site-settings-brand-icon size-4" />}
                            title="品牌信息"
                            description="站点名称会同步到浏览器标题与主要品牌入口，Logo 会用于首页、登录页和后台。"
                            status={
                                <Tag className="site-settings-brand-status" color={setting?.logoUrl ? "success" : "default"}>
                                    {setting?.logoUrl ? "自定义 Logo" : "内置 Logo"}
                                </Tag>
                            }
                        >
                            <div className="site-settings-brand-fields grid gap-6 px-6 py-6 md:grid-cols-[minmax(0,1fr)_280px]">
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
                                                <Button className="site-settings-logo-upload" icon={<Upload className="site-settings-logo-upload-icon size-4" />} loading={logoMutation.isPending} onClick={() => logoInputRef.current?.click()}>
                                                    {setting?.logoUrl ? "替换" : "上传"}
                                                </Button>
                                                {setting?.logoUrl ? (
                                                    <Button className="site-settings-logo-remove" type="text" danger icon={<Trash2 className="site-settings-logo-remove-icon size-4" />} loading={removeLogoMutation.isPending} onClick={confirmRemoveLogo}>
                                                        恢复内置
                                                    </Button>
                                                ) : null}
                                            </div>
                                            <span className="site-settings-logo-help text-[11px] leading-4 text-foreground/45">PNG、JPG 或 WebP，最大 2MB</span>
                                        </div>
                                    </div>
                                </div>
                            </div>
                        </SettingsSectionCard>

                        <SettingsSectionCard icon={<FileText className="site-settings-footer-icon size-4" />} title="底部版权" description="显示在首页底部，支持公司名称、年份和版权声明。">
                            <div className="site-settings-footer-fields px-6 py-6">
                                <Form.Item className="site-settings-copyright-field mb-0" name="footerCopyright" label="版权文案" rules={[{ max: 200, message: "版权文案不能超过 200 个字符" }]}>
                                    <Input className="site-settings-copyright-input" maxLength={200} showCount placeholder="例如：© 2026 HMaigc. 保留所有权利。" />
                                </Form.Item>
                            </div>
                        </SettingsSectionCard>

                        <SettingsSectionCard icon={<FileCheck2 className="site-settings-registration-icon size-4" />} title="网站备案" description="配置首页底部对外展示的 ICP 与公安备案信息，备案链接仅允许 HTTP 或 HTTPS。">
                            <div className="site-settings-registration-fields grid gap-x-6 gap-y-4 px-6 py-6 lg:grid-cols-2">
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
                            </div>
                        </SettingsSectionCard>

                        <SettingsSectionCard
                            icon={<ShieldCheck className="site-settings-legal-icon size-4" />}
                            title="法律内容"
                            description="公开页面以纯文本安全展示，保存后首页底部链接立即可访问。"
                            status={
                                <Tag className="site-settings-legal-status" color={legalConfigured ? "success" : "warning"}>
                                    {legalConfigured ? "内容完整" : "待完善"}
                                </Tag>
                            }
                        >
                            <div className="site-settings-legal-fields grid gap-6 px-6 py-6 lg:grid-cols-2">
                                <Form.Item
                                    className="site-settings-agreement-field mb-0"
                                    name="userAgreement"
                                    label="用户协议"
                                    extra="建议包含账号使用、内容权利、付费服务、违约处理和争议解决。"
                                    rules={[{ max: 50_000, message: "用户协议不能超过 50000 个字符" }]}
                                >
                                    <TextArea className="site-settings-agreement-input" rows={16} showCount maxLength={50_000} placeholder="请输入用户协议正文" />
                                </Form.Item>
                                <Form.Item
                                    className="site-settings-privacy-field mb-0"
                                    name="privacyPolicy"
                                    label="隐私政策"
                                    extra="建议说明收集范围、处理目的、保存期限、共享规则与用户权利。"
                                    rules={[{ max: 50_000, message: "隐私政策不能超过 50000 个字符" }]}
                                >
                                    <TextArea className="site-settings-privacy-input" rows={16} showCount maxLength={50_000} placeholder="请输入隐私政策正文" />
                                </Form.Item>
                            </div>
                        </SettingsSectionCard>

                        <div className="site-settings-actions flex flex-wrap items-center justify-between gap-4">
                            <span className="site-settings-updated-at text-xs text-foreground/45">{setting?.updatedAt ? `上次更新：${new Date(setting.updatedAt).toLocaleString("zh-CN", { hour12: false })}` : "当前使用系统默认站点配置"}</span>
                            <Button className="site-settings-save-button" type="primary" icon={<Save className="site-settings-save-icon size-4" />} loading={saveMutation.isPending} onClick={() => void save()}>
                                保存并生效
                            </Button>
                        </div>
                    </Form>
                )}
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
        userAgreement: setting.userAgreement,
        privacyPolicy: setting.privacyPolicy,
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
