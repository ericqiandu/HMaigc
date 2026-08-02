import { useCallback, useEffect, useState, type ReactNode } from "react";
import { App, Button, Form, Input, Select, Switch, type FormInstance } from "antd";
import { ChevronDown, KeyRound, LockKeyhole, ShieldCheck, UserPlus } from "lucide-react";

import {
    getAdminLinuxDOSetting,
    getAdminRegistrationSetting,
    updateAdminLinuxDOSetting,
    updateAdminRegistrationSetting,
    type LinuxDOSetting,
    type RegistrationSetting,
} from "@/services/api/wallet";
import {
    buildLinuxDOSettingRequest,
    linuxDOSettingToFormValues,
    linuxDOSettingValuesEqual,
    type LinuxDOFormValues,
} from "./access-setting-request";
import { AdminContentError, AdminContentSkeleton, configuredSecretText, SettingsSectionCard } from "./admin-ui";

const httpUrlRule = {
    validator: (_: unknown, value?: string) => {
        const candidate = value?.trim();
        if (!candidate) return Promise.resolve();
        try {
            const parsed = new URL(candidate);
            return parsed.protocol === "http:" || parsed.protocol === "https:"
                ? Promise.resolve()
                : Promise.reject(new Error("请输入以 http:// 或 https:// 开头的有效地址"));
        } catch {
            return Promise.reject(new Error("请输入完整有效的 HTTP(S) 地址"));
        }
    },
};

const requiredWhenEnabled = (form: FormInstance<LinuxDOFormValues>, label: string) => ({
    validator: (_: unknown, value?: string | string[]) => {
        if (!form.getFieldValue("enabled")) return Promise.resolve();
        const present = Array.isArray(value) ? value.some((item) => item.trim()) : Boolean(value?.trim());
        return present ? Promise.resolve() : Promise.reject(new Error(`启用 Linux.do 登录前请填写${label}`));
    },
});

export default function AccessSettingsPanel() {
    const { message } = App.useApp();
    const [form] = Form.useForm<LinuxDOFormValues>();
    const [linuxdo, setLinuxdo] = useState<LinuxDOSetting | null>(null);
    const [registration, setRegistration] = useState<RegistrationSetting | null>(null);
    const [loadingLinuxDO, setLoadingLinuxDO] = useState(true);
    const [loadingRegistration, setLoadingRegistration] = useState(true);
    const [linuxDOError, setLinuxDOError] = useState<string | null>(null);
    const [registrationError, setRegistrationError] = useState<string | null>(null);
    const [savingLinuxDO, setSavingLinuxDO] = useState(false);
    const [savingRegistration, setSavingRegistration] = useState(false);
    const [linuxDODirty, setLinuxDODirty] = useState(false);

    const loadLinuxDO = useCallback(async () => {
        setLoadingLinuxDO(true);
        setLinuxDOError(null);
        try {
            const data = await getAdminLinuxDOSetting();
            setLinuxdo(data.setting);
            form.setFieldsValue(linuxDOSettingToFormValues(data.setting));
            setLinuxDODirty(false);
        } catch (error) {
            setLinuxdo(null);
            setLinuxDOError(error instanceof Error ? error.message : "读取 Linux.do 登录配置失败");
        } finally {
            setLoadingLinuxDO(false);
        }
    }, [form]);

    const loadRegistration = useCallback(async () => {
        setLoadingRegistration(true);
        setRegistrationError(null);
        try {
            const data = await getAdminRegistrationSetting();
            setRegistration(data.setting);
        } catch (error) {
            setRegistration(null);
            setRegistrationError(error instanceof Error ? error.message : "读取用户注册策略失败");
        } finally {
            setLoadingRegistration(false);
        }
    }, []);

    useEffect(() => {
        void loadLinuxDO();
        void loadRegistration();
    }, [loadLinuxDO, loadRegistration]);

    const syncLinuxDODirty = (_: Partial<LinuxDOFormValues>, values: LinuxDOFormValues) => {
        setLinuxDODirty(linuxdo ? !linuxDOSettingValuesEqual(values, linuxdo) : false);
    };

    const toggleRegistration = async (enabled: boolean) => {
        if (!registration || savingRegistration) return;
        setSavingRegistration(true);
        try {
            const data = await updateAdminRegistrationSetting(enabled);
            setRegistration(data.setting);
            message.success(enabled ? "用户注册已开启" : "用户注册已关闭");
        } catch (error) {
            message.error(error instanceof Error ? error.message : "更新注册设置失败");
        } finally {
            setSavingRegistration(false);
        }
    };

    const saveLinuxDO = async () => {
        if (!linuxdo || !linuxDODirty || savingLinuxDO) return;
        const values = await form.validateFields();
        const request = buildLinuxDOSettingRequest(values);
        if (request.enabled && !request.clientSecret && !linuxdo.hasClientSecret) {
            form.setFields([{ name: "clientSecret", errors: ["启用 Linux.do 登录前请填写 Client Secret"] }]);
            return;
        }
        setSavingLinuxDO(true);
        try {
            const data = await updateAdminLinuxDOSetting(request);
            setLinuxdo(data.setting);
            form.setFieldsValue(linuxDOSettingToFormValues(data.setting));
            setLinuxDODirty(false);
            message.success("Linux.do 登录配置已保存");
        } catch (error) {
            message.error(error instanceof Error ? error.message : "保存 Linux.do 配置失败");
        } finally {
            setSavingLinuxDO(false);
        }
    };

    return (
        <div className="access-settings-page admin-settings-page space-y-5">
            <SettingsSectionCard
                className="access-registration-card"
                icon={<UserPlus className="size-4" />}
                title="用户注册"
                description="控制新用户能否创建账号，不影响已有账号登录。"
                status={registration ? { label: registration.enabled ? "已开放" : "已关闭", color: registration.enabled ? "success" : "default" } : undefined}
            >
                {loadingRegistration ? <AdminContentSkeleton compact rows={2} label="正在读取用户注册策略" /> : null}
                {!loadingRegistration && registrationError ? (
                    <AdminContentError title="用户注册策略读取失败" description={registrationError} onRetry={() => void loadRegistration()} />
                ) : null}
                {!loadingRegistration && registration ? (
                    <div className="admin-settings-switch-row access-registration-control flex min-h-20 items-center justify-between gap-5 px-5 py-4">
                        <div className="access-registration-copy min-w-0">
                            <h3 className="access-registration-title">开放新用户注册</h3>
                            <p className="access-registration-description">关闭后，本地注册和未绑定账号的 Linux.do 首次登录都会被拒绝。</p>
                        </div>
                        <Switch checked={registration.enabled} loading={savingRegistration} disabled={savingRegistration} onChange={(checked) => void toggleRegistration(checked)} aria-label="开放新用户注册" />
                    </div>
                ) : null}
            </SettingsSectionCard>

            <SettingsSectionCard
                className="access-linuxdo-card"
                icon={<KeyRound className="size-4" />}
                title="Linux.do 单点登录"
                description="连接 Linux.do OAuth，让用户使用社区账号登录。"
                status={linuxdo ? { label: linuxdo.enabled ? "运行中" : "未启用", color: linuxdo.enabled ? "success" : "default" } : undefined}
                footer={linuxdo ? <><span className="access-settings-sync-state">{linuxDODirty ? "有未保存的登录配置变更" : "配置已与服务器同步；密钥不会回显明文。"}</span><Button type="primary" loading={savingLinuxDO} disabled={!linuxDODirty || savingLinuxDO} onClick={() => void saveLinuxDO()}>保存登录配置</Button></> : undefined}
            >
                {loadingLinuxDO ? <AdminContentSkeleton rows={8} label="正在读取 Linux.do 登录配置" /> : null}
                {!loadingLinuxDO && linuxDOError ? (
                    <AdminContentError title="Linux.do 配置读取失败" description={linuxDOError} onRetry={() => void loadLinuxDO()} />
                ) : null}
                {!loadingLinuxDO && linuxdo ? (
                    <Form className="admin-content-form access-settings-form" form={form} layout="vertical" requiredMark={false} disabled={savingLinuxDO} onValuesChange={syncLinuxDODirty}>
                        <div className="access-settings-sections">
                            <section className="admin-form-section access-settings-grid" aria-labelledby="access-credentials-title">
                                <FormSectionTitle id="access-credentials-title" icon={<ShieldCheck className="size-4" />} title="登录状态与应用凭据" />
                                <Form.Item name="enabled" label="启用 Linux.do 登录" valuePropName="checked" extra="启用后，登录与注册页面会显示 Linux.do 入口。">
                                    <Switch />
                                </Form.Item>
                                <Form.Item name="clientAuthMethod" label="Token 请求鉴权方式" rules={[{ required: true, message: "请选择鉴权方式" }]} extra="Linux.do 应用未特别要求时使用 Client Secret Post。">
                                    <Select options={[{ label: "Client Secret Post（推荐）", value: "client_secret_post" }, { label: "Client Secret Basic", value: "client_secret_basic" }]} />
                                </Form.Item>
                                <Form.Item name="clientId" label="Client ID" dependencies={["enabled"]} rules={[requiredWhenEnabled(form, " Client ID")]}>
                                    <Input autoComplete="off" placeholder="Linux.do OAuth 应用的 Client ID" />
                                </Form.Item>
                                <Form.Item name="clientSecret" label={linuxdo.hasClientSecret ? `Client Secret（${configuredSecretText}）` : "Client Secret"}>
                                    <Input.Password autoComplete="new-password" placeholder={linuxdo.hasClientSecret ? "留空保留原密钥" : "Linux.do OAuth 应用的 Client Secret"} />
                                </Form.Item>
                            </section>

                            <section className="admin-form-section access-settings-grid" aria-labelledby="access-oauth-title">
                                <FormSectionTitle id="access-oauth-title" icon={<LockKeyhole className="size-4" />} title="OAuth 地址" />
                                <Form.Item name="authorizationUrl" label="授权地址" dependencies={["enabled"]} rules={[requiredWhenEnabled(form, "授权地址"), httpUrlRule]}>
                                    <Input inputMode="url" placeholder="https://connect.linux.do/oauth2/authorize" />
                                </Form.Item>
                                <Form.Item name="tokenUrl" label="Token 地址" dependencies={["enabled"]} rules={[requiredWhenEnabled(form, " Token 地址"), httpUrlRule]}>
                                    <Input inputMode="url" placeholder="https://connect.linux.do/oauth2/token" />
                                </Form.Item>
                                <Form.Item name="userInfoUrl" label="用户资料地址" dependencies={["enabled"]} rules={[requiredWhenEnabled(form, "用户资料地址"), httpUrlRule]}>
                                    <Input inputMode="url" placeholder="https://connect.linux.do/api/user" />
                                </Form.Item>
                                <Form.Item name="redirectUrl" label="本站回调地址" dependencies={["enabled"]} rules={[requiredWhenEnabled(form, "本站回调地址"), httpUrlRule]} extra="必须与 Linux.do OAuth 应用登记的回调地址完全一致；推荐使用 /oauth/linuxdo/callback。">
                                    <Input inputMode="url" placeholder="https://你的域名/oauth/linuxdo/callback" />
                                </Form.Item>
                                <Form.Item name="scopes" label="授权范围（Scopes）" dependencies={["enabled"]} rules={[requiredWhenEnabled(form, "授权范围")]} className="access-settings-wide-field" extra="通常使用 openid、profile、email；按 Linux.do 应用实际授权范围填写。">
                                    <Select mode="tags" tokenSeparators={[",", " "]} placeholder="输入后按回车添加" />
                                </Form.Item>
                            </section>

                            <details className="admin-form-disclosure access-settings-mapping group">
                                <summary className="admin-form-disclosure-summary flex cursor-pointer list-none items-center justify-between gap-4 transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring">
                                    <div className="admin-form-disclosure-copy">
                                        <div className="admin-form-disclosure-title">高级：Linux.do 返回字段对应关系</div>
                                        <p className="admin-form-disclosure-description">配置用户资料响应与本站账号字段的对应关系，支持 data.user.id 这类嵌套路径。</p>
                                    </div>
                                    <ChevronDown className="admin-form-disclosure-icon size-4 shrink-0 transition-transform group-open:rotate-180" />
                                </summary>
                                <div className="admin-form-disclosure-content access-settings-grid">
                                    <Form.Item name="subjectField" label="唯一用户 ID 字段" dependencies={["enabled"]} rules={[requiredWhenEnabled(form, "唯一用户 ID 字段")]} extra="账号绑定的唯一依据，必须长期稳定。Linux.do 常见值为 id。"><Input placeholder="id" /></Form.Item>
                                    <Form.Item name="usernameField" label="用户名字段" dependencies={["enabled"]} rules={[requiredWhenEnabled(form, "用户名字段")]} extra="用于生成本站用户名。Linux.do 常见值为 username。"><Input placeholder="username" /></Form.Item>
                                    <Form.Item name="displayNameField" label="显示名称字段" extra="显示在用户菜单中的名称，常见值为 name。"><Input placeholder="name" /></Form.Item>
                                    <Form.Item name="emailField" label="邮箱字段" extra="没有或无效时允许留空，常见值为 email。"><Input placeholder="email" /></Form.Item>
                                    <Form.Item name="avatarField" label="头像地址字段" extra="用户头像 URL，常见值为 avatar_url。"><Input placeholder="avatar_url" /></Form.Item>
                                </div>
                            </details>
                        </div>
                    </Form>
                ) : null}
            </SettingsSectionCard>
        </div>
    );
}

function FormSectionTitle({ id, icon, title }: { id: string; icon: ReactNode; title: string }) {
    return <h3 id={id} className="admin-form-section-title access-settings-section-title">{icon}{title}</h3>;
}
