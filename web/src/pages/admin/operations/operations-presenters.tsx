import { Tag, Tooltip } from "antd";
import type { ColumnsType } from "antd/es/table";
import { CheckCircle2, CircleAlert, LoaderCircle } from "lucide-react";
import type { ReactNode } from "react";

import type {
    OperationsBackup,
    OperationsOverview,
    OperationsRecord,
    OperationsStatus,
} from "@/services/api/operations";

export const actionLabels: Record<OperationsRecord["action"], string> = {
    install: "首次安装",
    upgrade: "版本升级",
    rollback: "版本回滚",
    backup: "创建备份",
    verify: "环境校验",
};

const statusLabels: Record<OperationsStatus, string> = {
    queued: "排队中",
    running: "执行中",
    succeeded: "成功",
    failed: "失败",
};

export function createOperationColumns(onSelect: (id: string) => void): ColumnsType<OperationsRecord> {
    return [
        {
            title: "动作",
            dataIndex: "action",
            width: 110,
            render: (value: OperationsRecord["action"]) => <span className="operations-table-action font-medium">{actionLabels[value]}</span>,
        },
        {
            title: "版本",
            key: "version",
            width: 180,
            render: (_, record) => (
                <span className="operations-table-version text-xs text-foreground/65">
                    {record.currentVersionAtStart || "--"}
                    {record.targetVersion || record.resultVersion ? ` → ${record.targetVersion || record.resultVersion}` : ""}
                </span>
            ),
        },
        {
            title: "状态",
            dataIndex: "status",
            width: 100,
            render: (value: OperationsStatus) => <OperationStatusTag status={value} />,
        },
        {
            title: "执行阶段",
            dataIndex: "phase",
            ellipsis: true,
            render: (value: string, record) => (
                <button className="operations-table-phase text-left text-xs text-foreground/65 hover:text-foreground" type="button" onClick={() => onSelect(record.id)}>
                    {value || "--"}
                </button>
            ),
        },
        {
            title: "管理员",
            dataIndex: "actorDisplayName",
            width: 140,
            ellipsis: true,
        },
        {
            title: "时间",
            dataIndex: "createdAt",
            width: 170,
            render: (value: string) => <span className="operations-table-time text-xs text-foreground/55">{formatTime(value)}</span>,
        },
    ];
}

export const backupColumns: ColumnsType<OperationsBackup> = [
    { title: "恢复点", dataIndex: "name", ellipsis: true },
    { title: "版本", dataIndex: "version", width: 120 },
    {
        title: "完整性",
        dataIndex: "checksumStatus",
        width: 110,
        render: (value: OperationsBackup["checksumStatus"], record) => value === "verified"
            ? <Tag className="operations-backup-status" color="success" variant="filled">校验通过</Tag>
            : <Tooltip title={record.validationError}><Tag className="operations-backup-status" color="error" variant="filled">校验失败</Tag></Tooltip>,
    },
    {
        title: "大小",
        dataIndex: "sizeBytes",
        width: 120,
        render: (value: number) => <span className="operations-backup-size text-xs text-foreground/55">{formatBytes(value)}</span>,
    },
    {
        title: "创建时间",
        dataIndex: "createdAt",
        width: 170,
        render: (value: string) => <span className="operations-backup-time text-xs text-foreground/55">{formatTime(value)}</span>,
    },
];

export function OverviewMetric({ icon, label, value, detail, tone }: { icon: ReactNode; label: string; value: string; detail: string; tone: "success" | "warning" | "neutral" }) {
    return (
        <article className={`operations-metric is-${tone}`}>
            <div className="operations-metric-heading flex items-center gap-2">
                <span className="operations-metric-icon grid size-7 place-items-center">{icon}</span>
                <span className="operations-metric-label">{label}</span>
            </div>
            <strong className="operations-metric-value block">{value}</strong>
            <span className="operations-metric-detail block">{detail}</span>
        </article>
    );
}

export function OperationActionButton({ icon, title, description, disabled, onClick }: { icon: ReactNode; title: string; description: string; disabled: boolean; onClick: () => void }) {
    return (
        <button className="operations-action-button" type="button" disabled={disabled} onClick={onClick}>
            <span className="operations-action-icon grid size-8 place-items-center">{icon}</span>
            <span className="operations-action-copy min-w-0 text-left">
                <span className="operations-action-title block">{title}</span>
                <span className="operations-action-description block truncate">{description}</span>
            </span>
        </button>
    );
}

export function OperationStatusTag({ status }: { status: OperationsStatus }) {
    const icon = status === "running" || status === "queued"
        ? <LoaderCircle className="operations-status-icon size-3 animate-spin motion-reduce:animate-none" />
        : status === "succeeded"
            ? <CheckCircle2 className="operations-status-icon size-3" />
            : <CircleAlert className="operations-status-icon size-3" />;
    const color = status === "succeeded" ? "success" : status === "failed" ? "error" : "processing";
    return <Tag className="operations-status-tag inline-flex items-center gap-1" icon={icon} color={color} variant="filled">{statusLabels[status]}</Tag>;
}

export function releaseCheckDetail(overview: OperationsOverview | null) {
    if (!overview) return "等待版本检查";
    if (overview.release.message) return overview.release.message;
    return overview.release.updateAvailable ? "发现可升级版本" : "已是最新版本";
}

export function shortCommit(value: string) {
    return value && value !== "unknown" ? value.slice(0, 8) : "未知提交";
}

export function formatTime(value?: string) {
    return value ? new Date(value).toLocaleString("zh-CN", { hour12: false }) : "--";
}

export function formatLogTime(value: string) {
    return new Date(value).toLocaleTimeString("zh-CN", { hour12: false });
}

function formatBytes(value: number) {
    if (!Number.isFinite(value) || value <= 0) return "--";
    const units = ["B", "KB", "MB", "GB", "TB"];
    let size = value;
    let unitIndex = 0;
    while (size >= 1024 && unitIndex < units.length - 1) {
        size /= 1024;
        unitIndex += 1;
    }
    return `${size.toFixed(unitIndex === 0 ? 0 : 1)} ${units[unitIndex]}`;
}
