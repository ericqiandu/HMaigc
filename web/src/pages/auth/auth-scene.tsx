import { ConfigProvider, Tabs } from "antd";
import { ArrowLeft, Play } from "lucide-react";
import { motion, useReducedMotion } from "motion/react";
import { Link, Outlet, useLocation, useNavigate } from "react-router";

import { siteLogoURL, useSiteSettings } from "@/components/site/site-settings-provider";
import { CometCard } from "@/components/ui/aceternity/comet-card";
import { SpotlightSurface } from "@/components/ui/aceternity/spotlight-surface";
import { aceternityMotion } from "@/lib/aceternity-motion";
import { getAntThemeConfig } from "@/lib/app-theme";
import { staticAssetURL } from "@/lib/static-assets";

import "./auth-form.css";
import "./auth-responsive.css";

const AUTH_VIDEO_URL = staticAssetURL("/videos/hero.mp4");
const AUTH_TABS = [
    { key: "login", label: "登录" },
    { key: "register", label: "注册" },
];

const authCopy = {
    login: {
        eyebrow: "WELCOME BACK",
        title: "进入创作现场",
        description: "继续编辑你的画布、素材与生成任务。",
    },
    register: {
        eyebrow: "CREATE ACCOUNT",
        title: "建立你的创作空间",
        description: "一个账号管理画布、素材、技能和模型偏好。",
    },
} as const;

export function AuthScene() {
    const location = useLocation();
    const navigate = useNavigate();
    const reducedMotion = useReducedMotion();
    const activeTab = location.pathname === "/register" ? "register" : "login";
    const copy = authCopy[activeTab];
    const { settings } = useSiteSettings();

    return (
        <main className="h-dvh min-h-0 overflow-y-auto bg-[#08090c] text-white lg:overflow-hidden">
            <div className="grid min-h-full lg:h-full lg:grid-cols-[minmax(0,1.32fr)_minmax(520px,1fr)]">
                <section className="relative min-h-[250px] overflow-hidden sm:min-h-[320px] lg:min-h-0" aria-label={`${settings.siteName} 品牌影片`}>
                    <video className="absolute inset-0 size-full object-cover" src={AUTH_VIDEO_URL} autoPlay muted loop playsInline preload="metadata" />
                    <div className="absolute inset-0 bg-[linear-gradient(180deg,rgba(4,5,8,.58),transparent_42%,rgba(4,5,8,.74))]" aria-hidden />
                    <div className="absolute inset-y-0 right-0 hidden w-[clamp(120px,14vw,240px)] bg-[linear-gradient(90deg,transparent_0%,rgba(11,12,16,.68)_58%,#0b0c10_100%)] lg:block" aria-hidden />
                    <div className="absolute inset-x-0 top-0 flex items-center justify-between gap-4 p-5 sm:p-7 lg:p-9">
                        <Link to="/" className="inline-flex items-center gap-2.5 text-sm font-semibold text-white drop-shadow-sm transition-opacity hover:opacity-80">
                            <img className="auth-site-logo size-7 object-contain" src={siteLogoURL(settings)} alt="" />
                            <span className="auth-site-name">{settings.siteName}</span>
                        </Link>
                        <span className="inline-flex items-center gap-2 rounded-full border border-white/16 bg-black/20 px-3 py-1.5 text-[11px] text-white/76 backdrop-blur-xl">
                            <Play className="size-3 fill-current" />
                            创作正在发生
                        </span>
                    </div>
                    <motion.div
                        initial={reducedMotion ? false : { opacity: 0, y: 18 }}
                        animate={{ opacity: 1, y: 0 }}
                        transition={{ duration: aceternityMotion.duration.panel, ease: aceternityMotion.easing.enter }}
                        className="absolute inset-x-0 bottom-0 max-w-2xl p-5 sm:p-7 lg:p-10"
                    >
                        <p className="text-xs font-semibold tracking-[0.18em] text-white/58">YINGCE STUDIO</p>
                        <h1 className="mt-3 max-w-xl text-3xl font-semibold leading-tight sm:text-4xl lg:text-5xl">让一个故事，从文字走向银幕。</h1>
                    </motion.div>
                </section>

                <section className="relative flex min-h-[620px] items-start justify-center overflow-y-auto bg-[#0b0c10] px-4 pb-8 pt-20 sm:px-8 lg:min-h-0 lg:px-10 lg:pb-10 lg:pt-20">
                    <Link
                        to="/"
                        className="absolute right-5 top-5 z-20 inline-flex h-9 items-center gap-2 rounded-full border border-white/10 bg-white/[0.04] px-4 text-xs text-white/58 backdrop-blur-xl transition hover:border-white/20 hover:bg-white/[0.08] hover:text-white lg:right-8 lg:top-8"
                    >
                        <ArrowLeft className="size-3.5" />
                        返回首页
                    </Link>

                    <motion.div
                        initial={reducedMotion ? false : { opacity: 0, y: 14 }}
                        animate={{ opacity: 1, y: 0 }}
                        layout={!reducedMotion}
                        transition={{ duration: aceternityMotion.duration.panel, ease: aceternityMotion.easing.enter }}
                        className="my-auto w-full max-w-[460px]"
                    >
                        <ConfigProvider theme={getAntThemeConfig(true)} button={{ autoInsertSpace: false }}>
                            <SpotlightSurface spotlightColor="rgba(96,165,250,.14)" spotlightRadius={300} className="flex h-auto rounded-[26px]">
                                <CometCard
                                    rotateDepth={1.8}
                                    translateDepth={1.5}
                                    glare={!reducedMotion}
                                    containerClassName="h-auto"
                                    className="auth-card-dark h-auto overflow-hidden rounded-[26px] border border-white/12 bg-[#111216]/88 shadow-[0_40px_120px_rgba(0,0,0,.48)] backdrop-blur-2xl"
                                >
                                    <section className="flex min-h-[620px] flex-col sm:min-h-[640px]" aria-label={copy.title}>
                                        <header className="px-6 pb-5 pt-6 sm:px-8 sm:pt-7">
                                            <p className="text-xs font-semibold tracking-[0.18em] text-blue-300/80">{copy.eyebrow}</p>
                                            <h2 className="mt-2 text-3xl font-semibold">{copy.title}</h2>
                                            <p className="mt-2 text-sm leading-6 text-white/45">{copy.description}</p>
                                        </header>
                                        <div className="border-b border-white/[0.08] px-6 sm:px-8">
                                            <Tabs className="auth-card-tabs" activeKey={activeTab} items={AUTH_TABS} onChange={(key) => navigate({ pathname: key === "register" ? "/register" : "/login", search: location.search })} />
                                        </div>
                                        <div className="flex-1 px-6 py-6 sm:px-8 sm:py-7" key={location.pathname}>
                                            <Outlet />
                                        </div>
                                    </section>
                                </CometCard>
                            </SpotlightSurface>
                        </ConfigProvider>
                    </motion.div>
                </section>
            </div>
        </main>
    );
}
