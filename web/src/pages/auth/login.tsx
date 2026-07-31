import { type FormEvent, useEffect, useState } from "react";
import { App, Button, Divider, Input } from "antd";
import { LockKeyhole, UserRound } from "lucide-react";
import { Link, useNavigate, useSearchParams } from "react-router";

import { applyUserSession } from "@/lib/user-session";
import { getAuthSession, getAuthSettings, linuxDOLoginURL, login } from "@/services/api/auth";

import { AuthField, AuthLegalCopy, AuthNotice, LinuxDOIcon } from "./auth-components";

export default function LoginPage() {
    const navigate = useNavigate();
    const [params] = useSearchParams();
    const { message } = App.useApp();
    const [username, setUsername] = useState("");
    const [password, setPassword] = useState("");
    const [submitting, setSubmitting] = useState(false);
    const [linuxdoEnabled, setLinuxdoEnabled] = useState(false);
    const [settingsError, setSettingsError] = useState("");
    const next = safeNext(params.get("next"));

    useEffect(() => {
        let cancelled = false;
        void getAuthSettings()
            .then((settings) => {
                if (cancelled) return;
                setLinuxdoEnabled(settings.linuxdoEnabled);
                setSettingsError("");
            })
            .catch((error: unknown) => {
                if (cancelled) return;
                setSettingsError(error instanceof Error ? error.message : "登录配置加载失败");
            });
        const oauthError = params.get("oauth_error");
        if (oauthError) message.error(oauthError);
        return () => {
            cancelled = true;
        };
    }, [message, params]);

    const submit = async (event: FormEvent<HTMLFormElement>) => {
        event.preventDefault();
        setSubmitting(true);
        try {
            await login({ username, password });
            await applyUserSession(await getAuthSession());
            message.success("登录成功");
            navigate(next, { replace: true });
        } catch (error) {
            message.error(error instanceof Error ? error.message : "登录失败");
        } finally {
            setSubmitting(false);
        }
    };

    return (
        <form onSubmit={submit} className="auth-form">
            {settingsError ? <AuthNotice tone="error">{settingsError}</AuthNotice> : null}
            <div className="auth-fields">
                <AuthField label="用户名或邮箱">
                    <Input className="auth-input" prefix={<UserRound className="auth-input-icon" />} value={username} onChange={(event) => setUsername(event.target.value)} placeholder="用户名或邮箱" autoComplete="username" required />
                </AuthField>
                <AuthField label="密码">
                    <Input.Password className="auth-input" prefix={<LockKeyhole className="auth-input-icon" />} value={password} onChange={(event) => setPassword(event.target.value)} placeholder="请输入密码" autoComplete="current-password" required />
                </AuthField>
            </div>

            <Button className="auth-primary-button" type="primary" htmlType="submit" block loading={submitting}>
                登录
            </Button>

            <p className="auth-switch-copy">
                <span className="auth-switch-muted">还没有账号？</span>
                <Link className="auth-switch-link" to={{ pathname: "/register", search: params.toString() ? `?${params.toString()}` : "" }}>
                    立即注册
                </Link>
            </p>

            {linuxdoEnabled ? (
                <div className="auth-oauth-section">
                    <Divider plain className="auth-divider">
                        或
                    </Divider>
                    <Button className="auth-oauth-button" block icon={<LinuxDOIcon />} href={linuxDOLoginURL(next)}>
                        使用 Linux.do 登录
                    </Button>
                </div>
            ) : null}

            <AuthLegalCopy action="登录" />
        </form>
    );
}

function safeNext(value: string | null) {
    if (!value || !value.startsWith("/") || value.startsWith("//")) return "/projects";
    return value;
}
