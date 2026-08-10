import { Alert, Button, Empty, Skeleton } from "antd";
import { FileText } from "lucide-react";

import { useSiteSettings } from "@/components/site/site-settings-provider";
import { LegalRichTextViewer } from "@/components/legal/legal-rich-text-viewer";
import { legalDocumentDefinition, type LegalDocumentKind } from "@/constants/legal-documents";

export function LegalDocumentPage({ document }: { document: LegalDocumentKind }) {
    const { settings, loading, error, refresh } = useSiteSettings();
    return <LegalDocumentView document={document} content={settings[document]} loading={loading} error={error} onRetry={refresh} />;
}

export function LegalDocumentView({ document, content, loading, error, onRetry }: { document: LegalDocumentKind; content: string; loading: boolean; error: Error | null; onRetry: () => void | Promise<void> }) {
    const definition = legalDocumentDefinition(document);
    const normalizedContent = content.trim();

    return (
        <main className="legal-document-page h-full overflow-y-auto bg-background text-foreground">
            <section className="legal-document-content mx-auto w-full max-w-[920px] px-5 pb-20 pt-10 sm:px-8 sm:pt-14">
                <div className="legal-document-heading border-b border-border/60 pb-6">
                    <div className="legal-document-title-row flex items-center gap-2.5">
                        <FileText className="legal-document-title-icon size-5 text-foreground/55" aria-hidden="true" />
                        <h1 className="legal-document-title text-2xl font-semibold tracking-[-0.02em]">{definition.title}</h1>
                    </div>
                    <p className="legal-document-description mt-2 text-sm leading-6 text-foreground/50">{definition.publicDescription}</p>
                </div>

                {error ? (
                    <Alert
                        className="legal-document-error mt-8"
                        type="error"
                        showIcon
                        title="法律内容加载失败"
                        description={error.message}
                        action={
                            <Button className="legal-document-retry" onClick={() => void onRetry()}>
                                重试
                            </Button>
                        }
                    />
                ) : null}

                {loading ? (
                    <Skeleton className="legal-document-skeleton mt-10" active paragraph={{ rows: 14 }} />
                ) : normalizedContent ? (
                    <article className="legal-document-body mt-8 text-[15px] leading-7 text-foreground/78">
                        <LegalRichTextViewer content={normalizedContent} />
                    </article>
                ) : error ? null : (
                    <div className="legal-document-empty mt-10 bg-muted/20 py-16">
                        <Empty className="legal-document-empty-state" image={Empty.PRESENTED_IMAGE_SIMPLE} description={definition.emptyMessage} />
                    </div>
                )}
            </section>
        </main>
    );
}
