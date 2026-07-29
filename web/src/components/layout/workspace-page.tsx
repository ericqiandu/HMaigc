import { Button, Pagination } from "antd";
import { ArrowLeft, RotateCcw } from "lucide-react";
import type { ReactNode } from "react";
import { useNavigate } from "react-router";

import { cn } from "@/lib/utils";
import "@/styles/workspace-ui.css";
import "@/styles/workspace-shell.css";

type WorkspacePageLayout = "standard" | "collection" | "data";

export function WorkspacePage({
    children,
    className,
    grid = false,
    fluid = false,
    layout = "standard",
}: {
    children: ReactNode;
    className?: string;
    grid?: boolean;
    fluid?: boolean;
    layout?: WorkspacePageLayout;
}) {
    return (
        <main className={cn("app-user-content workspace-ui-scope thin-scrollbar h-full overflow-y-auto text-foreground", `workspace-page--${layout}`, grid && "app-workspace-grid", className)}>
            <div className={fluid ? "h-full w-full" : "workspace-page-frame w-full px-4 pb-8 pt-20 sm:px-6 md:px-[104px] md:pt-[90px]"}>{children}</div>
        </main>
    );
}

type PageHeaderProps = {
    title: string;
    description?: string;
    meta?: ReactNode;
    actions?: ReactNode;
    backTo?: string;
    onBack?: () => void;
    backLabel?: string;
};

export function PageHeader({ title, description, meta, actions, backTo = "/", onBack, backLabel = "返回首页" }: PageHeaderProps) {
    const navigate = useNavigate();
    const handleBack = () => {
        if (onBack) {
            onBack();
            return;
        }
        navigate(backTo);
    };

    return (
        <header className="app-page-header flex min-h-9 flex-col items-start gap-3 sm:flex-row sm:items-center sm:justify-between">
            <div className="page-header-copy flex min-w-0 items-center gap-2">
                <button
                    type="button"
                    className="page-header-back grid size-9 shrink-0 place-items-center rounded-full text-foreground/60 transition-colors hover:bg-foreground/[.06] hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                    onClick={handleBack}
                    aria-label={backLabel}
                    title={backLabel}
                >
                    <ArrowLeft className="page-header-back-icon size-4" />
                </button>
                <div className="page-header-text min-w-0">
                    <div className="page-header-title-row flex min-w-0 flex-wrap items-baseline gap-2">
                        <h1 className="page-header-title truncate text-sm font-semibold leading-6 tracking-[-0.01em]">{title}</h1>
                        {meta}
                    </div>
                    {description ? <p className="page-header-description hidden">{description}</p> : null}
                </div>
            </div>
            {actions ? <div className="page-header-actions ml-auto flex max-w-full shrink-0 flex-wrap items-center justify-end gap-2">{actions}</div> : null}
        </header>
    );
}

export function ListToolbar({ children, trailing, active, onReset }: { children: ReactNode; trailing?: ReactNode; active?: boolean; onReset?: () => void }) {
    const hasActions = Boolean((active && onReset) || trailing);

    return (
        <div className="workspace-list-toolbar mt-3 flex min-h-10 flex-col gap-2 lg:flex-row lg:items-center lg:justify-between">
            <div className="workspace-list-toolbar-fields flex min-w-0 flex-1 flex-wrap items-center gap-2">{children}</div>
            {hasActions ? (
                <div className="workspace-list-toolbar-actions flex shrink-0 flex-wrap items-center gap-2">
                    {active && onReset ? <Button className="workspace-list-toolbar-reset" type="text" icon={<RotateCcw className="workspace-list-toolbar-reset-icon size-3.5" />} onClick={onReset}>重置</Button> : null}
                    {trailing}
                </div>
            ) : null}
        </div>
    );
}

export function TableSurface({ children, className }: { children: ReactNode; className?: string }) {
    return <div className={cn("app-table-surface mt-4 min-w-0 overflow-hidden rounded-lg bg-foreground/[.025]", className)}>{children}</div>;
}

export function CollectionGrid({ children, className }: { children: ReactNode; className?: string }) {
    return <div className={cn("workspace-collection-grid mt-5 grid grid-cols-1 gap-x-4 gap-y-5 sm:grid-cols-[repeat(auto-fill,minmax(248px,1fr))]", className)}>{children}</div>;
}

export function PaginationBar({ current, pageSize, total, onChange, pageSizeOptions = [20, 50, 100] }: { current: number; pageSize: number; total: number; onChange: (page: number, pageSize: number) => void; pageSizeOptions?: number[] }) {
    if (total <= pageSize && current === 1) return null;
    return (
        <div className="app-pagination-bar sticky bottom-0 z-10 mt-4 flex min-w-0 justify-end bg-background/92 px-2 py-3 backdrop-blur-xl">
            <Pagination
                className="app-pagination-control"
                current={current}
                pageSize={pageSize}
                total={total}
                showSizeChanger
                showLessItems
                responsive
                pageSizeOptions={pageSizeOptions.map(String)}
                showTotal={(value, range) => `${range[0]}-${range[1]} / 共 ${value} 条`}
                onChange={onChange}
            />
        </div>
    );
}
