import { ConfigProvider, Tabs } from "antd";
import { ArrowLeft, Moon, Sun } from "lucide-react";
import { Link, Outlet, useLocation, useNavigate } from "react-router";

import { siteLogoURL, useSiteSettings } from "@/components/site/site-settings-provider";
import { AnimatedThemeToggler } from "@/components/ui/animated-theme-toggler";
import { getAntThemeConfig } from "@/lib/app-theme";
import { staticAssetURL } from "@/lib/static-assets";
import { useThemeStore } from "@/stores/use-theme-store";

import "./auth-form.css";
import "./auth-responsive.css";

const AUTH_VIDEO_URL = staticAssetURL("/videos/hero.mp4");
const AUTH_TABS = [
    { key: "login", label: "登录" },
    { key: "register", label: "注册" },
];

const authCopy = {
    login: {
        title: "欢迎登录",
        description: "继续你的创作之旅",
    },
    register: {
        title: "创建账号",
        description: "从这里开始你的创作之旅",
    },
} as const;

export function AuthScene() {
    const location = useLocation();
    const navigate = useNavigate();
    const activeTab = location.pathname === "/register" ? "register" : "login";
    const copy = authCopy[activeTab];
    const { settings } = useSiteSettings();
    const theme = useThemeStore((state) => state.theme);
    const setTheme = useThemeStore((state) => state.setTheme);
    const dark = theme === "dark";

    return (
        <main className={`auth-scene auth-scene-${theme}`}>
            <section className="auth-media-pane" aria-label={`${settings.siteName} 品牌影片`}>
                <video className="auth-media-video" src={AUTH_VIDEO_URL} autoPlay muted loop playsInline preload="metadata" />
                <div className="auth-media-fade" aria-hidden="true" />
            </section>

            <section className="auth-pane" aria-label={`${copy.title}区域`}>
                <nav className="auth-page-actions" aria-label="页面操作">
                    <Link to="/" className="auth-home-link" aria-label="返回首页">
                        <ArrowLeft className="auth-action-icon" aria-hidden="true" />
                        <span className="auth-home-label">返回首页</span>
                    </Link>
                    <AnimatedThemeToggler
                        className="auth-theme-toggle"
                        theme={theme}
                        onThemeChange={setTheme}
                        aria-label={dark ? "切换到浅色主题" : "切换到深色主题"}
                        title={dark ? "切换到浅色主题" : "切换到深色主题"}
                    >
                        {dark ? <Sun className="auth-action-icon" aria-hidden="true" /> : <Moon className="auth-action-icon" aria-hidden="true" />}
                    </AnimatedThemeToggler>
                </nav>

                <div className="auth-content">
                    <Link to="/" className="auth-brand" aria-label={`${settings.siteName} 首页`} data-logo-source={settings.logoUrl ? "custom" : "default"}>
                        <img className="auth-site-logo" src={siteLogoURL(settings)} alt="" />
                        <span className="auth-site-name">{settings.siteName}</span>
                    </Link>

                    <header className="auth-heading">
                        <h1 className="auth-title">{copy.title}</h1>
                        <p className="auth-description">{copy.description}</p>
                    </header>

                    <ConfigProvider theme={getAntThemeConfig(dark)} button={{ autoInsertSpace: false }}>
                        <div className="auth-route-tabs">
                            <Tabs
                                className="auth-card-tabs"
                                activeKey={activeTab}
                                items={AUTH_TABS}
                                onChange={(key) => navigate({ pathname: key === "register" ? "/register" : "/login", search: location.search })}
                            />
                        </div>
                        <div className="auth-form-surface" key={location.pathname}>
                            <Outlet />
                        </div>
                    </ConfigProvider>
                </div>
            </section>
        </main>
    );
}
