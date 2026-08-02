import { useCallback, useEffect, useState } from "react";
import { App, Button, Form, Input, InputNumber, Select, Space, Switch, Tag } from "antd";
import { KeyRound, MailCheck, Send, Server, ShieldCheck } from "lucide-react";
import { useBlocker } from "react-router";

import { getAdminEmailSetting, updateAdminEmailSetting, type EmailSetting, type EmailSettingUpdateRequest } from "@/services/api/wallet";
import { AdminContentError, AdminContentSkeleton, AdminSettingsSection, configuredSecretText, SettingsSectionCard } from "./admin-ui";
import { buildEmailSettingRequest, emailSettingValuesEqual, emailSettingToFormValues, type EmailSettingFormValues } from "./email-setting-request";

export default function EmailSettingsPanel() {
    const { message, modal } = App.useApp();
    const [setting, setSetting] = useState<EmailSetting | null>(null);
    const [loading, setLoading] = useState(true);
    const [saving, setSaving] = useState(false);
    const [loadError, setLoadError] = useState("");
    const [dirty, setDirty] = useState(false);
    const [form] = Form.useForm<EmailSettingFormValues>();

    const loadSetting = useCallback(async () => {
        setLoading(true);
        setLoadError("");
        try {
            const { setting: value } = await getAdminEmailSetting();
            setSetting(value);
            form.setFieldsValue(emailSettingToFormValues(value));
            setDirty(false);
        } catch (error) {
            const reason = error instanceof Error ? error.message : "读取邮件配置失败";
            setSetting(null);
            setLoadError(reason);
        } finally {
            setLoading(false);
        }
    }, [form]);

    useEffect(() => {
        void loadSetting();
    }, [loadSetting]);

    const blocker = useBlocker(dirty && !saving);

    useEffect(() => {
        const beforeUnload = (event: BeforeUnloadEvent) => {
            if (!dirty || saving) return;
            event.preventDefault();
        };
        window.addEventListener("beforeunload", beforeUnload);
        return () => window.removeEventListener("beforeunload", beforeUnload);
    }, [dirty, saving]);

    useEffect(() => {
        if (blocker.state !== "blocked") return;
        modal.confirm({
            title: "离开并放弃未保存的邮件配置？",
            content: "SMTP 连接、发件人身份或密钥输入尚未保存，离开后这些修改将丢失。",
            okText: "放弃修改并离开",
            cancelText: "继续编辑",
            okButtonProps: { danger: true },
            onOk: () => blocker.proceed(),
            onCancel: () => blocker.reset(),
        });
    }, [blocker, modal]);

    const save = async () => {
        let values: EmailSettingFormValues;
        try {
            values = await form.validateFields();
        } catch {
            message.error("请先修正邮件配置中的字段错误");
            return;
        }
        let request: EmailSettingUpdateRequest;
        try {
            request = buildEmailSettingRequest({ ...values, password: values.password?.trim() || "" });
        } catch (error) {
            message.error(error instanceof Error ? error.message : "邮件配置格式不正确");
            return;
        }
        setSaving(true);
        try {
            const result = await updateAdminEmailSetting(request);
            setSetting(result.setting);
            form.setFieldsValue(emailSettingToFormValues(result.setting));
            setDirty(false);
            message.success("注册邮件配置已保存");
        } catch (error) {
            message.error(error instanceof Error ? error.message : "保存邮件配置失败");
        } finally {
            setSaving(false);
        }
    };

    if (loading) {
        return <AdminContentSkeleton rows={8} label="正在加载邮件服务配置" />;
    }

    if (loadError || !setting) {
        return <AdminContentError title="邮件服务配置加载失败" description={loadError || "服务端未返回邮件配置"} onRetry={() => void loadSetting()} />;
    }

    return (
        <div className="email-settings-page admin-settings-page">
            <SettingsSectionCard
                className="email-settings-card"
                icon={<MailCheck className="email-settings-card-icon size-4" />}
                title="注册验证邮件"
                description="通过 SMTP 向普通用户发送注册验证码。"
                status={
                    <Space className="email-settings-status" size={6}>
                        <Tag variant="filled" color={setting.enabled ? "success" : "default"}>
                            {setting.enabled ? "已启用" : "未启用"}
                        </Tag>
                        {setting.hasPassword ? (
                            <Tag variant="filled" color="blue">
                                {configuredSecretText}
                            </Tag>
                        ) : null}
                    </Space>
                }
                footer={
                    <>
                        <span className="email-settings-sync-status text-xs text-foreground/45">{dirty ? "有未保存的邮件配置变更" : formatEmailSettingMeta(setting)}</span>
                        <Button className="email-settings-save-button" type="primary" loading={saving} disabled={!dirty} onClick={() => void save()}>
                            保存邮件配置
                        </Button>
                    </>
                }
            >
                <Form
                    className="admin-content-form email-settings-form"
                    form={form}
                    layout="vertical"
                    requiredMark={false}
                    disabled={saving}
                    onValuesChange={(_, values: EmailSettingFormValues) => {
                        try {
                            setDirty(!emailSettingValuesEqual(values, setting));
                        } catch {
                            setDirty(true);
                        }
                    }}
                >
                    <div className="email-settings-sections">
                        <AdminSettingsSection id="email-delivery-title" icon={<Server className="email-settings-section-icon size-4" />} title="SMTP 连接" description="配置邮件服务器的连接方式与登录凭据。">
                            <Form.Item className="email-settings-enabled-field" name="enabled" label="启用注册验证邮件" valuePropName="checked" extra="公开注册开启后，普通邮箱注册必须完成验证码校验。">
                                <Switch className="email-settings-enabled-switch" />
                            </Form.Item>
                            <Form.Item className="email-settings-encryption-field" name="encryption" label="连接加密" rules={[{ required: true, message: "请选择连接加密方式" }]}>
                                <Select
                                    className="email-settings-encryption-select"
                                    options={[
                                        { label: "STARTTLS（推荐，通常 587）", value: "starttls" },
                                        { label: "TLS（通常 465）", value: "tls" },
                                        { label: "无加密", value: "none" },
                                    ]}
                                />
                            </Form.Item>
                            <Form.Item
                                className="email-settings-host-field"
                                name="host"
                                label="SMTP 主机"
                                dependencies={["enabled"]}
                                rules={[
                                    ({ getFieldValue }) => ({
                                        validator: (_, value: string | undefined) => (getFieldValue("enabled") !== true || value?.trim() ? Promise.resolve() : Promise.reject(new Error("启用邮件服务前请填写 SMTP 主机"))),
                                    }),
                                ]}
                            >
                                <Input className="email-settings-host-input" autoComplete="off" placeholder="smtp.example.com" />
                            </Form.Item>
                            <Form.Item className="email-settings-port-field" name="port" label="SMTP 端口" rules={[{ required: true, message: "请输入 SMTP 端口" }]}>
                                <InputNumber<number> className="email-settings-port-input w-full" min={1} max={65535} precision={0} placeholder="587" />
                            </Form.Item>
                            <Form.Item className="email-settings-username-field" name="username" label="SMTP 用户名">
                                <Input className="email-settings-username-input" autoComplete="off" placeholder="通常为完整邮箱地址" />
                            </Form.Item>
                            <Form.Item
                                className="email-settings-password-field"
                                name="password"
                                label={setting.hasPassword ? `SMTP 密码（${configuredSecretText}）` : "SMTP 密码"}
                                dependencies={["enabled", "username"]}
                                rules={[
                                    ({ getFieldValue }) => ({
                                        validator: (_, value: string | undefined) =>
                                            getFieldValue("enabled") !== true || !String(getFieldValue("username") || "").trim() || setting.hasPassword || value?.trim() ? Promise.resolve() : Promise.reject(new Error("启用 SMTP 登录前请填写密码或授权码")),
                                    }),
                                ]}
                            >
                                <Input.Password className="email-settings-password-input" autoComplete="new-password" placeholder={setting.hasPassword ? "留空保留原密码" : "SMTP 密码或授权码"} />
                            </Form.Item>
                        </AdminSettingsSection>
                        <AdminSettingsSection id="email-sender-title" icon={<Send className="email-settings-section-icon size-4" />} title="发件人身份" description="设置收件人看到的发件邮箱与品牌名称。">
                            <Form.Item
                                className="email-settings-from-email-field"
                                name="fromEmail"
                                label="发件邮箱"
                                dependencies={["enabled"]}
                                rules={[
                                    ({ getFieldValue }) => ({
                                        validator: (_, value: string | undefined) => (getFieldValue("enabled") !== true || value?.trim() ? Promise.resolve() : Promise.reject(new Error("启用邮件服务前请填写发件邮箱"))),
                                    }),
                                    { type: "email", message: "请输入有效的发件邮箱" },
                                ]}
                            >
                                <Input className="email-settings-from-email-input" autoComplete="email" placeholder="noreply@example.com" />
                            </Form.Item>
                            <Form.Item className="email-settings-from-name-field" name="fromName" label="发件人名称">
                                <Input className="email-settings-from-name-input" placeholder="HMaigc" />
                            </Form.Item>
                            <div className="email-settings-notes">
                                <div className="email-settings-note">
                                    <KeyRound className="email-settings-note-icon size-3.5" aria-hidden="true" />
                                    <span className="email-settings-note-text">已保存密码留空时保持不变</span>
                                </div>
                                <div className="email-settings-note">
                                    <ShieldCheck className="email-settings-note-icon size-3.5" aria-hidden="true" />
                                    <span className="email-settings-note-text">敏感密码由服务端加密存储且不会回显</span>
                                </div>
                            </div>
                        </AdminSettingsSection>
                    </div>
                </Form>
            </SettingsSectionCard>
        </div>
    );
}

function formatEmailSettingMeta(setting: EmailSetting): string {
    if (!setting.updatedAt) return "邮件配置已同步 · 尚无更新时间";
    const updatedAt = new Date(setting.updatedAt);
    if (Number.isNaN(updatedAt.getTime()) || updatedAt.getUTCFullYear() <= 1) {
        return "邮件配置已同步 · 尚无更新时间";
    }
    return `邮件配置已同步 · 上次更新：${updatedAt.toLocaleString("zh-CN", { hour12: false })}`;
}
