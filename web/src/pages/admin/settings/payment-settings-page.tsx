import { Alert, App, Button, Form, Input, Skeleton, Space, Switch, Tag } from "antd";
import { BadgeDollarSign, CreditCard, KeyRound, RefreshCw, ShieldCheck } from "lucide-react";
import { useCallback, useEffect, useState, type ReactNode } from "react";

import {
    getAdminPaymentSetting,
    updateAdminPaymentSetting,
    type AdminPaymentChannelSetting,
    type AdminPaymentSetting,
    type PaymentChannelSettingInput,
    type UpdatePaymentSettingInput,
} from "@/services/api/payment";
import { AdminPageFrame } from "../components/admin-shell";
import { AdminSettingsActionBar, configuredSecretText, SettingsSectionCard } from "../components/admin-ui";

type PaymentChannelFormValues = Partial<PaymentChannelSettingInput>;
type PaymentFormValues = {
    checkoutBaseUrl?: string;
    wechat?: PaymentChannelFormValues;
    alipay?: PaymentChannelFormValues;
};

type ChannelField = keyof PaymentChannelFormValues;
type ChannelName = "wechat" | "alipay";

export default function PaymentSettingsPage() {
    const { message } = App.useApp();
    const [form] = Form.useForm<PaymentFormValues>();
    const [setting, setSetting] = useState<AdminPaymentSetting | null>(null);
    const [loading, setLoading] = useState(true);
    const [saving, setSaving] = useState(false);
    const [loadError, setLoadError] = useState("");

    const loadSetting = useCallback(async () => {
        setLoading(true);
        setLoadError("");
        try {
            const value = await getAdminPaymentSetting();
            setSetting(value);
            form.setFieldsValue(toFormValues(value));
        } catch (error) {
            const reason = error instanceof Error ? error.message : "读取支付配置失败";
            setLoadError(reason);
            message.error(reason);
        } finally {
            setLoading(false);
        }
    }, [form, message]);

    useEffect(() => {
        void loadSetting();
    }, [loadSetting]);

    const save = async () => {
        const values = await form.validateFields();
        setSaving(true);
        try {
            const result = await updateAdminPaymentSetting(toRequest(values));
            setSetting(result);
            form.setFieldsValue(toFormValues(result));
            message.success("支付配置已保存");
        } catch (error) {
            message.error(error instanceof Error ? error.message : "保存支付配置失败");
        } finally {
            setSaving(false);
        }
    };

    return (
        <AdminPageFrame title="支付配置" description="统一收银台与微信、支付宝商户参数">
            <div className="payment-settings-page mx-auto max-w-5xl space-y-5">
                <Alert
                    className="payment-settings-notice"
                    type="info"
                    showIcon
                    message="当前阶段仅开放商户参数配置"
                    description="配置保存后不会自动启用真实扣款。支付下单、异步回调验签和退款通道接通前，系统会显式返回“支付服务未接通”。"
                />

                {loadError ? (
                    <Alert
                        className="payment-settings-load-error"
                        type="error"
                        showIcon
                        message="支付配置加载失败"
                        description={loadError}
                        action={
                            <Button className="payment-settings-retry-button" icon={<RefreshCw className="size-4" />} onClick={() => void loadSetting()}>
                                重试
                            </Button>
                        }
                    />
                ) : null}

                {loading ? (
                    <Skeleton className="payment-settings-skeleton" active paragraph={{ rows: 10 }} />
                ) : (
                    <Form className="payment-settings-form" form={form} layout="vertical" requiredMark={false}>
                        <SettingsSectionCard
                            icon={<CreditCard className="payment-settings-checkout-icon size-4" />}
                            title="统一收银台"
                            description="用户扫码或跳转支付时使用的公开站点地址。"
                            status={<Tag className="payment-settings-checkout-status" color={setting?.checkoutBaseUrl ? "success" : "default"}>{setting?.checkoutBaseUrl ? "已配置" : "未配置"}</Tag>}
                        >
                            <div className="payment-settings-checkout-fields px-6 pt-6">
                                <Form.Item
                                    className="payment-settings-checkout-field"
                                    name="checkoutBaseUrl"
                                    label="收银台基础地址"
                                    rules={[{ type: "url", warningOnly: true, message: "请输入完整的 HTTP(S) 地址" }]}
                                >
                                    <Input className="payment-settings-checkout-input" autoComplete="off" placeholder="例如：https://pay.example.com" />
                                </Form.Item>
                            </div>
                        </SettingsSectionCard>

                        <PaymentChannelCard
                            channel="wechat"
                            title="微信支付"
                            description="配置微信支付商户号、API v3 密钥和平台证书。"
                            icon={<BadgeDollarSign className="payment-settings-wechat-icon size-4" />}
                            setting={setting?.wechat}
                        />

                        <PaymentChannelCard
                            channel="alipay"
                            title="支付宝"
                            description="配置支付宝应用、商户私钥和平台公钥。"
                            icon={<BadgeDollarSign className="payment-settings-alipay-icon size-4" />}
                            setting={setting?.alipay}
                        />

                        <AdminSettingsActionBar meta={setting?.updatedAt ? `上次更新：${formatTime(setting.updatedAt)}` : "尚未保存支付配置"} status="商户参数仅对管理员可见">
                            <Button className="payment-settings-save-button" type="primary" loading={saving} onClick={() => void save()}>
                                保存支付配置
                            </Button>
                        </AdminSettingsActionBar>
                    </Form>
                )}

                <div className="payment-settings-security-notes grid gap-3 text-xs text-foreground/55 sm:grid-cols-3">
                    <Notice icon={<KeyRound className="payment-settings-key-icon size-3.5" />} text="密钥留空时保留原配置" />
                    <Notice icon={<ShieldCheck className="payment-settings-shield-icon size-3.5" />} text="敏感密钥不会在后台回显" />
                    <Notice icon={<CreditCard className="payment-settings-card-icon size-3.5" />} text="参数完整不代表支付通道已接通" />
                </div>
            </div>
        </AdminPageFrame>
    );
}

function PaymentChannelCard({
    channel,
    title,
    description,
    icon,
    setting,
}: {
    channel: ChannelName;
    title: string;
    description: string;
    icon: ReactNode;
    setting?: AdminPaymentChannelSetting;
}) {
    return (
        <SettingsSectionCard
            icon={icon}
            title={title}
            description={description}
            status={
                <Space className={`payment-settings-${channel}-status`} size={6}>
                    <Tag className={`payment-settings-${channel}-enabled-tag`} color={setting?.enabled ? "success" : "default"}>{setting?.enabled ? "已启用" : "未启用"}</Tag>
                    <Tag className={`payment-settings-${channel}-ready-tag`} color={setting?.ready ? "blue" : "warning"}>{setting?.ready ? "参数完整" : "待补充参数"}</Tag>
                </Space>
            }
        >
            <div className={`payment-settings-${channel}-fields grid grid-cols-1 gap-x-6 px-6 pt-6 md:grid-cols-2`}>
                <Form.Item className={`payment-settings-${channel}-enabled-field`} name={[channel, "enabled"]} label="启用渠道" valuePropName="checked">
                    <Switch className={`payment-settings-${channel}-enabled-switch`} />
                </Form.Item>
                <ChannelInput channel={channel} field="appId" label="应用 ID（App ID）" placeholder="支付平台分配的应用 ID" />
                <ChannelInput channel={channel} field="merchantId" label="商户号" placeholder="支付平台分配的商户号" />
                <ChannelInput channel={channel} field="merchantSerialNo" label="商户证书序列号" placeholder="微信支付常用；支付宝可留空" />
                <ChannelInput channel={channel} field="notifyUrl" label="异步通知地址" placeholder="https://api.example.com/payment/notify" url />
                <ChannelInput channel={channel} field="gatewayUrl" label="支付网关地址" placeholder="支付平台网关地址" url />
                <SecretInput channel={channel} field="merchantPrivateKey" label="商户私钥" configured={setting?.hasMerchantPrivateKey === true} />
                <SecretInput channel={channel} field="platformPublicKey" label="平台公钥" configured={setting?.hasPlatformPublicKey === true} />
                <SecretInput channel={channel} field="apiV3Key" label="API v3 密钥" configured={setting?.hasApiV3Key === true} />
            </div>
        </SettingsSectionCard>
    );
}

function ChannelInput({ channel, field, label, placeholder, url = false }: { channel: ChannelName; field: ChannelField; label: string; placeholder: string; url?: boolean }) {
    return (
        <Form.Item
            className={`payment-settings-${channel}-${field}-field`}
            name={[channel, field]}
            label={label}
            rules={url ? [{ type: "url", warningOnly: true, message: "请输入完整的 HTTP(S) 地址" }] : undefined}
        >
            <Input className={`payment-settings-${channel}-${field}-input`} autoComplete="off" placeholder={placeholder} />
        </Form.Item>
    );
}

function SecretInput({ channel, field, label, configured }: { channel: ChannelName; field: ChannelField; label: string; configured: boolean }) {
    return (
        <Form.Item className={`payment-settings-${channel}-${field}-field`} name={[channel, field]} label={configured ? `${label}（${configuredSecretText}）` : label}>
            <Input.Password
                className={`payment-settings-${channel}-${field}-input`}
                autoComplete="new-password"
                placeholder={configured ? "留空保留原密钥" : `请输入${label}`}
            />
        </Form.Item>
    );
}

function toFormValues(setting: AdminPaymentSetting): PaymentFormValues {
    return {
        checkoutBaseUrl: setting.checkoutBaseUrl,
        wechat: channelFormValues(setting.wechat),
        alipay: channelFormValues(setting.alipay),
    };
}

function channelFormValues(setting: AdminPaymentChannelSetting): PaymentChannelFormValues {
    return {
        enabled: setting.enabled,
        appId: setting.appId,
        merchantId: setting.merchantId,
        merchantSerialNo: setting.merchantSerialNo,
        merchantPrivateKey: "",
        platformPublicKey: "",
        apiV3Key: "",
        notifyUrl: setting.notifyUrl,
        gatewayUrl: setting.gatewayUrl,
    };
}

function toRequest(values: PaymentFormValues): UpdatePaymentSettingInput {
    return {
        checkoutBaseUrl: clean(values.checkoutBaseUrl),
        wechat: channelRequest(values.wechat),
        alipay: channelRequest(values.alipay),
    };
}

function channelRequest(values?: PaymentChannelFormValues): PaymentChannelSettingInput {
    return {
        enabled: values?.enabled === true,
        appId: clean(values?.appId),
        merchantId: clean(values?.merchantId),
        merchantSerialNo: clean(values?.merchantSerialNo),
        merchantPrivateKey: clean(values?.merchantPrivateKey),
        platformPublicKey: clean(values?.platformPublicKey),
        apiV3Key: clean(values?.apiV3Key),
        notifyUrl: clean(values?.notifyUrl),
        gatewayUrl: clean(values?.gatewayUrl),
    };
}

function clean(value?: string) {
    return value?.trim() || "";
}

function formatTime(value: string) {
    return new Date(value).toLocaleString("zh-CN", { hour12: false });
}

function Notice({ icon, text }: { icon: ReactNode; text: string }) {
    return (
        <div className="payment-settings-security-note flex items-center gap-2">
            <span className="payment-settings-security-note-icon text-foreground/40">{icon}</span>
            <span className="payment-settings-security-note-text">{text}</span>
        </div>
    );
}
