import { ArrowLeft, Check } from "lucide-react";
import { Link, Outlet, useLocation } from "react-router";

import { siteLogoURL, useSiteSettings } from "@/components/site/site-settings-provider";
import { staticAssetURL } from "@/lib/static-assets";

import "./auth.css";
import "./auth-form.css";
import "./auth-responsive.css";

const AUTH_VIDEO_URL = staticAssetURL("/videos/hero.mp4");

const authCopy = {
    login: {
        title: "欢迎回来",
        description: "登录后继续你的创作。",
    },
    register: {
        title: "创建你的账号",
        description: "从一个想法开始，建立完整的创作工作流。",
    },
} as const;

const productHighlights = ["从故事梗概到分镜画布，集中完成创作", "统一管理图片、视频、音频与角色素材", "通过可视化节点连接完整的生成流程"] as const;

export function AuthScene() {
    const location = useLocation();
    const activePage = location.pathname === "/register" ? "register" : "login";
    const copy = authCopy[activePage];
    const { settings } = useSiteSettings();

    return (
        <main className="auth-page">
            <section className="auth-showcase" aria-label={`${settings.siteName} 产品介绍`}>
                <video className="auth-showcase-video" src={AUTH_VIDEO_URL} autoPlay muted loop playsInline preload="metadata" />
                <div className="auth-showcase-shade" aria-hidden />
                <Link to="/" className="auth-showcase-brand" aria-label={`返回 ${settings.siteName} 首页`}>
                    <img className="auth-showcase-logo" src={siteLogoURL(settings)} alt="" />
                    <span className="auth-showcase-name">{settings.siteName}</span>
                </Link>
                <div className="auth-showcase-content">
                    <p className="auth-showcase-eyebrow">AI CREATIVE WORKSPACE</p>
                    <h2 className="auth-showcase-title">让灵感成为作品</h2>
                    <ul className="auth-showcase-list">
                        {productHighlights.map((highlight) => (
                            <li className="auth-showcase-list-item" key={highlight}>
                                <span className="auth-showcase-check" aria-hidden>
                                    <Check className="auth-showcase-check-icon" />
                                </span>
                                <span className="auth-showcase-list-text">{highlight}</span>
                            </li>
                        ))}
                    </ul>
                </div>
            </section>

            <section className="auth-panel" aria-label={copy.title}>
                <Link to="/" className="auth-back-link">
                    <ArrowLeft className="auth-back-icon" />
                    <span className="auth-back-label">返回首页</span>
                </Link>

                <div className="auth-panel-scroll">
                    <div className="auth-panel-content">
                        <header className="auth-heading">
                            <img className="auth-heading-logo" src={siteLogoURL(settings)} alt="" />
                            <p className="auth-heading-brand">{settings.siteName}</p>
                            <h1 className="auth-heading-title">{copy.title}</h1>
                            <p className="auth-heading-description">{copy.description}</p>
                        </header>
                        <div className="auth-route-content">
                            <Outlet />
                        </div>
                    </div>
                </div>
            </section>
        </main>
    );
}
