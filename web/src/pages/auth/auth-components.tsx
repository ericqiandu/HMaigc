import { CircleAlert, Info, TriangleAlert, type LucideIcon } from "lucide-react";
import { type ReactNode } from "react";
import { Link } from "react-router";

export function LinuxDOIcon() {
    return (
        <span className="auth-linuxdo-icon" aria-hidden>
            <span className="auth-linuxdo-icon-band auth-linuxdo-icon-band-dark" />
            <span className="auth-linuxdo-icon-band auth-linuxdo-icon-band-light" />
            <span className="auth-linuxdo-icon-band auth-linuxdo-icon-band-gold" />
        </span>
    );
}

export function AuthField({ label, children }: { label: string; children: ReactNode }) {
    return (
        <label className="auth-field">
            <span className="auth-field-label">{label}</span>
            <div className="auth-field-control">{children}</div>
        </label>
    );
}

export function AuthLegalCopy({ action }: { action: "登录" | "注册" }) {
    return (
        <p className="auth-legal-copy">
            {action}即表示你已阅读并同意
            <Link className="auth-legal-link" to="/legal/user-agreement">
                《用户协议》
            </Link>
            和
            <Link className="auth-legal-link" to="/legal/privacy-policy">
                《隐私政策》
            </Link>
        </p>
    );
}

const noticeIcons: Record<"error" | "info" | "warning", LucideIcon> = {
    error: CircleAlert,
    info: Info,
    warning: TriangleAlert,
};

export function AuthNotice({ tone, children }: { tone: "error" | "info" | "warning"; children: ReactNode }) {
    const Icon = noticeIcons[tone];
    return (
        <div className={`auth-notice auth-notice-${tone}`} role={tone === "error" ? "alert" : "status"}>
            <Icon className="auth-notice-icon" />
            <span className="auth-notice-text">{children}</span>
        </div>
    );
}
