import { type FormEvent, useEffect, useState } from "react";
import { App, Button, Divider } from "antd";
import { useNavigate, useSearchParams } from "react-router";

import { applyUserSession } from "@/lib/user-session";
import { getAuthSession, getAuthSettings, linuxDOLoginURL, register, sendRegistrationEmailCode } from "@/services/api/auth";

import { AuthLegalConsent, AuthNotice, LinuxDOIcon } from "./auth-components";
import { validateAuthLegalConsent } from "./auth-legal-consent-policy";
import { RegisterFields } from "./register-fields";

type AuthSettings = Awaited<ReturnType<typeof getAuthSettings>>;

export default function RegisterPage() {
    const navigate = useNavigate();
    const [params] = useSearchParams();
    const { message } = App.useApp();
    const [settings, setSettings] = useState<AuthSettings | null>(null);
    const [settingsError, setSettingsError] = useState("");
    const [username, setUsername] = useState("");
    const [email, setEmail] = useState("");
    const [emailCode, setEmailCode] = useState("");
    const [password, setPassword] = useState("");
    const [legalConsentAccepted, setLegalConsentAccepted] = useState(false);
    const [submitting, setSubmitting] = useState(false);
    const [sendingCode, setSendingCode] = useState(false);
    const [countdown, setCountdown] = useState(0);
    const next = safeNext(params.get("next"));
    const inviteCode = normalizeInviteCode(params.get("invite"));

    useEffect(() => {
        let cancelled = false;
        void getAuthSettings()
            .then((value) => {
                if (cancelled) return;
                setSettings(value);
                setSettingsError("");
            })
            .catch((error: unknown) => {
                if (cancelled) return;
                setSettingsError(error instanceof Error ? error.message : "注册配置加载失败");
            });
        return () => {
            cancelled = true;
        };
    }, []);

    useEffect(() => {
        if (countdown <= 0) return;
        const timer = window.setInterval(() => setCountdown((value) => Math.max(0, value - 1)), 1000);
        return () => window.clearInterval(timer);
    }, [countdown]);

    const sendCode = async () => {
        if (!email.trim()) {
            message.warning("请先输入邮箱");
            return;
        }
        setSendingCode(true);
        try {
            await sendRegistrationEmailCode(email.trim());
            setCountdown(60);
            message.success("验证码已发送，请检查邮箱");
        } catch (error) {
            message.error(error instanceof Error ? error.message : "发送验证码失败");
        } finally {
            setSendingCode(false);
        }
    };

    const submit = async (event: FormEvent<HTMLFormElement>) => {
        event.preventDefault();
        const consentValidation = validateAuthLegalConsent(legalConsentAccepted);
        if (!consentValidation.ok) {
            message.warning(consentValidation.message);
            return;
        }
        setSubmitting(true);
        try {
            await register({ username, email, emailCode, password, inviteCode });
            await applyUserSession(await getAuthSession());
            message.success(settings?.firstUser ? "管理员账号已创建" : "注册成功");
            navigate(next, { replace: true });
        } catch (error) {
            message.error(error instanceof Error ? error.message : "注册失败");
        } finally {
            setSubmitting(false);
        }
    };

    const registrationClosed = settings?.registrationEnabled === false;
    const mailUnavailable = Boolean(settings && !registrationClosed && !settings.firstUser && settings.emailCodeRequired && !settings.emailEnabled);
    const legalDocumentsUnavailable = Boolean(settings && !settings.firstUser && !settings.legalDocumentsConfigured);
    const disabled = settings === null || registrationClosed || mailUnavailable || legalDocumentsUnavailable || Boolean(settingsError);
    const requireCode = Boolean(settings && !settings.firstUser && settings.emailCodeRequired);

    return (
        <form onSubmit={submit} className="auth-form auth-register-form">
            <div className="auth-notices">
                {settingsError ? <AuthNotice tone="error">{settingsError}</AuthNotice> : null}
                {settings?.firstUser ? <AuthNotice tone="info">首个账号将自动成为管理员，无需邮箱验证码。</AuthNotice> : null}
                {registrationClosed ? <AuthNotice tone="warning">当前已关闭普通注册，请联系管理员创建账号。</AuthNotice> : null}
                {legalDocumentsUnavailable ? <AuthNotice tone="warning">管理员尚未发布用户协议与隐私政策，暂不能开放注册。</AuthNotice> : null}
                {mailUnavailable ? <AuthNotice tone="warning">注册邮件尚未配置，邮箱注册暂不可用。</AuthNotice> : null}
                {inviteCode ? <AuthNotice tone="info">已绑定邀请码 {inviteCode}，注册成功后不可更换。</AuthNotice> : null}
            </div>

            <RegisterFields
                username={username}
                email={email}
                emailCode={emailCode}
                password={password}
                requireEmail={!settings?.firstUser}
                requireCode={requireCode}
                disabled={disabled}
                sendingCode={sendingCode}
                countdown={countdown}
                onUsernameChange={setUsername}
                onEmailChange={setEmail}
                onEmailCodeChange={setEmailCode}
                onPasswordChange={setPassword}
                onSendCode={() => void sendCode()}
            />

            <AuthLegalConsent checked={legalConsentAccepted} onChange={setLegalConsentAccepted} />

            <Button className="auth-primary-button" type="primary" htmlType="submit" block loading={submitting} disabled={disabled || !legalConsentAccepted}>
                创建账号
            </Button>

            {settings?.linuxdoEnabled ? (
                <div className="auth-oauth-section">
                    <Divider plain className="auth-divider">
                        或
                    </Divider>
                    <Button className="auth-oauth-button" block icon={<LinuxDOIcon />} href={legalConsentAccepted ? linuxDOLoginURL(next, inviteCode) : undefined} disabled={!legalConsentAccepted}>
                        使用 Linux.do 注册或登录
                    </Button>
                </div>
            ) : null}
        </form>
    );
}

function safeNext(value: string | null) {
    if (!value || !value.startsWith("/") || value.startsWith("//")) return "/projects";
    return value;
}

function normalizeInviteCode(value: string | null) {
    return (value || "").trim().toUpperCase().slice(0, 16);
}
