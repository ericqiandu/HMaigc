import { Alert, Modal, Skeleton, Tag } from "antd";
import { motion, useReducedMotion } from "motion/react";
import { ScrollText } from "lucide-react";
import { useEffect, useState, type CSSProperties } from "react";
import ReactMarkdown from "react-markdown";

import { aceternityMotion } from "@/lib/aceternity-motion";
import { getAdminReleaseNotes } from "@/services/api/release-notes";

export const APP_VERSION = __APP_VERSION__;

type AppChangelogButtonProps = {
    className?: string;
    style?: CSSProperties;
    showVersion?: boolean;
    showLabel?: boolean;
    labelClassName?: string;
    versionClassName?: string;
};

export function AppChangelogButton({ className, style, showVersion = false, showLabel = false, labelClassName, versionClassName }: AppChangelogButtonProps) {
    const [open, setOpen] = useState(false);

    return (
        <>
            <button type="button" className={className} style={style} onClick={() => setOpen(true)} aria-label="查看更新日志" title="更新日志">
                <ScrollText className="size-4 shrink-0" />
                {showLabel ? <span className={`whitespace-nowrap ${labelClassName || ""}`}>更新日志</span> : null}
                {showVersion ? <span className={versionClassName}>v{APP_VERSION.replace(/^v/, "")}</span> : null}
            </button>
            <AppChangelogModal open={open} onClose={() => setOpen(false)} />
        </>
    );
}

function AppChangelogModal({ open, onClose }: { open: boolean; onClose: () => void }) {
    const reducedMotion = useReducedMotion();
    const [changelog, setChangelog] = useState("");
    const [version, setVersion] = useState(APP_VERSION);
    const [loading, setLoading] = useState(false);
    const [error, setError] = useState("");

    useEffect(() => {
        if (!open) return;
        const controller = new AbortController();
        setLoading(true);
        setError("");
        void getAdminReleaseNotes(controller.signal)
            .then((result) => {
                if (controller.signal.aborted) return;
                setChangelog(result.changelog);
                setVersion(result.version);
            })
            .catch((requestError: unknown) => {
                if (controller.signal.aborted) return;
                setError(requestError instanceof Error ? requestError.message : "读取更新日志失败");
                setChangelog("");
            })
            .finally(() => {
                if (!controller.signal.aborted) setLoading(false);
            });
        return () => controller.abort();
    }, [open]);

    return (
        <Modal
            rootClassName="app-spatial-modal"
            title={
                <div className="app-changelog-title flex min-w-0 items-center gap-3 pr-8">
                    <span className="app-changelog-title-icon grid size-9 shrink-0 place-items-center rounded-full border border-border bg-muted/45">
                        <ScrollText className="app-changelog-title-icon-svg size-4" />
                    </span>
                    <div className="app-changelog-title-copy min-w-0">
                        <div className="app-changelog-title-row flex items-center gap-2 text-base font-semibold">
                            更新日志
                            <Tag className="app-changelog-version" variant="filled">
                                v{version.replace(/^v/, "")}
                            </Tag>
                        </div>
                        <div className="app-changelog-subtitle mt-0.5 text-xs font-normal text-foreground/45">仅管理员可查看的产品发布记录</div>
                    </div>
                </div>
            }
            open={open}
            width={760}
            footer={null}
            centered
            onCancel={onClose}
            styles={{ body: { maxHeight: "min(72vh, 760px)", overflowY: "auto", overscrollBehavior: "contain" } }}
            modalRender={(node) => (
                <motion.div
                    className="app-changelog-motion"
                    initial={reducedMotion ? false : { opacity: 0, y: 14, scale: 0.975 }}
                    animate={{ opacity: 1, y: 0, scale: 1 }}
                    transition={{ duration: aceternityMotion.duration.panel, ease: aceternityMotion.easing.enter }}
                >
                    {node}
                </motion.div>
            )}
        >
            <div className="app-changelog-content thin-scrollbar pr-2 text-sm leading-6 text-foreground/75">
                {loading ? <Skeleton className="app-changelog-loading" active paragraph={{ rows: 8 }} /> : null}
                {!loading && error ? <Alert className="app-changelog-error" type="error" showIcon message="更新日志读取失败" description={error} /> : null}
                {!loading && !error ? (
                    <ReactMarkdown
                        components={{
                            h1: ({ children }) => <h2 className="app-changelog-heading-primary mb-4 text-xl font-semibold text-foreground">{children}</h2>,
                            h2: ({ children }) => <h3 className="app-changelog-heading-secondary mb-2 mt-6 border-b border-border pb-2 text-base font-semibold text-foreground first:mt-0">{children}</h3>,
                            ul: ({ children }) => <ul className="app-changelog-list space-y-2 pl-5">{children}</ul>,
                            li: ({ children }) => <li className="app-changelog-list-item list-disc pl-1 marker:text-foreground/35">{children}</li>,
                            p: ({ children }) => <p className="app-changelog-paragraph my-2">{children}</p>,
                            code: ({ children }) => <code className="app-changelog-code rounded bg-muted px-1 py-0.5 text-xs text-foreground">{children}</code>,
                        }}
                    >
                        {changelog}
                    </ReactMarkdown>
                ) : null}
            </div>
        </Modal>
    );
}
