import { App, Button, Dropdown, Tag } from "antd";
import type { ButtonProps, MenuProps } from "antd";
import { saveAs } from "file-saver";
import { CheckSquare2, Download, MoreHorizontal, SearchX, X } from "lucide-react";
import { useState } from "react";
import type { ReactNode } from "react";

import { cn } from "@/lib/utils";

export const configuredSecretText = "已配置 · 留空不改";

function isStatusConfig(value: ReactNode | { label: string; color?: string }): value is { label: string; color?: string } {
    if (!value || typeof value !== "object") return false;
    return typeof (value as { label?: unknown }).label === "string";
}

export function AdminExportButton({
    exportFile,
    fileName,
    label = "导出",
    successMessage,
    errorMessage = "导出失败",
    size,
    ...buttonProps
}: Omit<ButtonProps, "children" | "icon" | "loading" | "onClick"> & {
    exportFile: () => Blob | Promise<Blob>;
    fileName: string | (() => string);
    label?: string;
    successMessage?: string;
    errorMessage?: string;
}) {
    const { message } = App.useApp();
    const [exporting, setExporting] = useState(false);

    const runExport = async () => {
        setExporting(true);
        try {
            const blob = await exportFile();
            saveAs(blob, typeof fileName === "function" ? fileName() : fileName);
            if (successMessage) message.success(successMessage);
        } catch (error) {
            message.error(error instanceof Error ? error.message : errorMessage);
        } finally {
            setExporting(false);
        }
    };

    return (
        <Button
            {...buttonProps}
            className={cn("admin-export-button", buttonProps.className)}
            size={size}
            icon={<Download className={cn("admin-export-button-icon", size === "small" ? "size-3.5" : "size-4")} />}
            loading={exporting}
            onClick={() => void runExport()}
        >
            {label}
        </Button>
    );
}

export function AdminTableEmpty({ filtered = false, title, description, action }: { filtered?: boolean; title?: string; description?: string; action?: ReactNode }) {
    return (
        <div className="admin-table-empty flex min-h-64 flex-col items-center justify-center px-6 py-12 text-center">
            <span className="admin-table-empty-icon grid size-10 place-items-center">
                <SearchX className="admin-table-empty-icon-symbol size-[18px]" />
            </span>
            <div className="admin-table-empty-title mt-3 text-sm font-medium">{title || (filtered ? "没有符合筛选条件的数据" : "暂无数据")}</div>
            <p className="admin-table-empty-description mt-1 max-w-sm text-xs leading-5">{description || (filtered ? "调整搜索词或筛选条件后再试。" : "数据产生后会显示在这里。")}</p>
            {action ? <div className="admin-table-empty-action mt-4">{action}</div> : null}
        </div>
    );
}

export function AdminTableSkeleton({ rows = 8, columns = 6 }: { rows?: number; columns?: number }) {
    return (
        <div className="admin-table-skeleton animate-pulse motion-reduce:animate-none" aria-label="正在加载表格" role="status">
            <div className="admin-table-skeleton-header grid h-11 items-center gap-4 px-4" style={{ gridTemplateColumns: `repeat(${columns}, minmax(72px, 1fr))` }}>
                {Array.from({ length: columns }).map((_, index) => (
                    <span key={index} className="admin-table-skeleton-heading h-3 w-16 max-w-full" />
                ))}
            </div>
            {Array.from({ length: Math.max(8, rows) }).map((_, rowIndex) => (
                <div key={rowIndex} className="admin-table-skeleton-row grid min-h-14 items-center gap-4 px-4" style={{ gridTemplateColumns: `repeat(${columns}, minmax(72px, 1fr))` }}>
                    {Array.from({ length: columns }).map((_, columnIndex) => (
                        <span key={columnIndex} className={cn("admin-table-skeleton-cell h-3", columnIndex === 0 ? "w-4/5" : columnIndex === columns - 1 ? "w-10" : "w-2/3")} />
                    ))}
                </div>
            ))}
        </div>
    );
}

export function AdminBatchBar({ count, onClear, children }: { count: number; onClear: () => void; children: ReactNode }) {
    if (count <= 0) return null;
    return (
        <div className="admin-batch-bar sticky top-0 z-20 mt-3 flex min-h-11 flex-wrap items-center justify-between gap-3 px-3 py-2 backdrop-blur">
            <div className="admin-batch-summary flex items-center gap-2 text-sm font-medium">
                <CheckSquare2 className="admin-batch-summary-icon size-4" />
                <span className="admin-batch-summary-text">已选择 {count} 项</span>
            </div>
            <div className="admin-batch-actions flex flex-wrap items-center gap-2">
                {children}
                <Button className="admin-batch-clear-button" type="text" size="small" icon={<X className="admin-batch-clear-icon size-3.5" />} onClick={onClear}>
                    取消选择
                </Button>
            </div>
        </div>
    );
}

export type AdminRowAction = {
    key: string;
    label: ReactNode;
    icon?: ReactNode;
    danger?: boolean;
    disabled?: boolean;
    onClick: () => void | Promise<void>;
    confirm?: {
        title: string;
        description: string;
        okText: string;
    };
};

export function AdminRowActions({ primary, actions }: { primary?: { label: ReactNode; icon?: ReactNode; onClick: () => void; disabled?: boolean }; actions: AdminRowAction[] }) {
    const { modal } = App.useApp();
    const items: MenuProps["items"] = actions.map((action) => ({
        key: action.key,
        label: action.label,
        icon: action.icon,
        danger: action.danger,
        disabled: action.disabled,
    }));

    const runAction = (action: AdminRowAction) => {
        if (!action.confirm) {
            void action.onClick();
            return;
        }
        modal.confirm({
            title: action.confirm.title,
            content: action.confirm.description,
            okText: action.confirm.okText,
            cancelText: "取消",
            okButtonProps: { danger: action.danger },
            onOk: action.onClick,
        });
    };

    return (
        <div className="admin-row-actions flex items-center justify-end gap-1">
            {primary ? (
                <Button className="admin-row-primary-action" size="small" icon={primary.icon} disabled={primary.disabled} onClick={primary.onClick}>
                    {primary.label}
                </Button>
            ) : null}
            {actions.length ? (
                <Dropdown
                    trigger={["click"]}
                    menu={{
                        items,
                        onClick: ({ key }) => {
                            const action = actions.find((item) => item.key === key);
                            if (action) runAction(action);
                        },
                    }}
                >
                    <Button className="admin-row-more-action" size="small" type="text" icon={<MoreHorizontal className="size-4" />} aria-label="更多操作" />
                </Dropdown>
            ) : null}
        </div>
    );
}

export function SettingsSectionCard({
    icon,
    title,
    description,
    status,
    children,
    footer,
    className,
}: {
    icon?: ReactNode;
    title: string;
    description: string;
    status?: { label: string; color?: string } | ReactNode;
    children: ReactNode;
    footer?: ReactNode;
    className?: string;
}) {
    return (
        <section className={cn("admin-section-card overflow-hidden", className)}>
            <div className="admin-section-card-header flex flex-wrap items-start justify-between gap-4">
                <div className="admin-section-card-heading flex min-w-0 items-start gap-4">
                    {icon ? <span className="admin-section-card-icon grid size-9 shrink-0 place-items-center">{icon}</span> : null}
                    <div className="admin-section-card-copy min-w-0">
                        <h2 className="admin-section-card-title">{title}</h2>
                        <p className="admin-section-card-description">{description}</p>
                    </div>
                </div>
                {status ? (
                    <div className="admin-section-card-status">
                        {isStatusConfig(status) ? (
                            <Tag className="admin-section-card-status-tag" variant="filled" color={status.color}>
                                {status.label}
                            </Tag>
                        ) : (
                            status
                        )}
                    </div>
                ) : null}
            </div>
            <div className="admin-section-card-content">{children}</div>
            {footer ? <div className="admin-section-card-footer flex flex-wrap items-center justify-between gap-4">{footer}</div> : null}
        </section>
    );
}
