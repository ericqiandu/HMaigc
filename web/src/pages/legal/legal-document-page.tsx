import { Alert, Button, Empty, Skeleton } from "antd";
import { ArrowLeft, ShieldCheck } from "lucide-react";
import { Link } from "react-router";

import { siteLogoURL, useSiteSettings } from "@/components/site/site-settings-provider";

type LegalDocumentKind = "userAgreement" | "privacyPolicy";

const documentMeta: Record<LegalDocumentKind, { title: string; description: string }> = {
    userAgreement: {
        title: "用户协议",
        description: "使用本平台服务前，请仔细阅读并理解本协议。",
    },
    privacyPolicy: {
        title: "隐私政策",
        description: "了解平台如何收集、使用、保存和保护你的信息。",
    },
};

export function LegalDocumentPage({ document }: { document: LegalDocumentKind }) {
    const { settings, loading, error, refresh } = useSiteSettings();
    const meta = documentMeta[document];
    const content = settings[document].trim();

    return (
        <main className="legal-document-page h-full overflow-y-auto bg-background text-foreground">
            <header className="legal-document-header sticky top-0 z-10 border-b border-border/70 bg-background/90 backdrop-blur-xl">
                <div className="legal-document-header-inner mx-auto flex h-16 w-full max-w-4xl items-center justify-between px-5 sm:px-8">
                    <Link className="legal-document-brand flex min-w-0 items-center gap-2.5" to="/">
                        <img className="legal-document-logo size-7 object-contain" src={siteLogoURL(settings)} alt="" />
                        <span className="legal-document-site-name truncate text-sm font-semibold">{settings.siteName}</span>
                    </Link>
                    <Link className="legal-document-back inline-flex h-9 items-center gap-2 rounded-md px-3 text-xs text-foreground/60 transition-colors hover:bg-foreground/[.05] hover:text-foreground" to="/">
                        <ArrowLeft className="legal-document-back-icon size-3.5" />
                        返回首页
                    </Link>
                </div>
            </header>
            <section className="legal-document-content mx-auto w-full max-w-4xl px-5 pb-20 pt-12 sm:px-8 sm:pt-16">
                <div className="legal-document-heading max-w-2xl">
                    <span className="legal-document-icon grid size-10 place-items-center rounded-lg bg-muted/45 text-foreground/70">
                        <ShieldCheck className="legal-document-icon-symbol size-5" />
                    </span>
                    <h1 className="legal-document-title mt-5 text-3xl font-semibold tracking-[-0.03em]">{meta.title}</h1>
                    <p className="legal-document-description mt-3 text-sm leading-6 text-foreground/50">{meta.description}</p>
                </div>

                {error ? (
                    <Alert
                        className="legal-document-error mt-8"
                        type="error"
                        showIcon
                        title="法律内容加载失败"
                        description={error.message}
                        action={
                            <Button className="legal-document-retry" onClick={() => void refresh()}>
                                重试
                            </Button>
                        }
                    />
                ) : null}

                {loading ? (
                    <Skeleton className="legal-document-skeleton mt-10" active paragraph={{ rows: 14 }} />
                ) : content ? (
                    <article className="legal-document-body mt-10 whitespace-pre-wrap text-[15px] leading-8 text-foreground/78">{content}</article>
                ) : (
                    <div className="legal-document-empty mt-10 bg-muted/20 py-16">
                        <Empty className="legal-document-empty-state" image={Empty.PRESENTED_IMAGE_SIMPLE} description={`${meta.title}尚未配置，请联系平台管理员。`} />
                    </div>
                )}
            </section>
        </main>
    );
}
