import { App, Button, Input, Select, Table, Tag } from "antd";
import type { ColumnsType } from "antd/es/table";
import { CircleCheck, CircleX, Eye, FileClock, RefreshCw, Search } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useSearchParams } from "react-router";

import { ListToolbar, PaginationBar, TableSurface } from "@/components/layout/workspace-page";
import { useDebouncedValue } from "@/hooks/use-debounced-value";
import { exportAdminApiLogs, listAdminApiLogs, type ApiCallLog } from "@/services/api/auth";
import { useAdminContext } from "../admin-context";
import { ApiLogDetailDrawer } from "../components/api-log-detail-drawer";
import { AdminPageFrame } from "../components/admin-shell";
import { AdminBatchBar, AdminContentError, AdminExportButton, AdminTableEmpty, AdminTableSkeleton } from "../components/admin-ui";

export default function LogsPage() {
    const { message } = App.useApp();
    const { references } = useAdminContext();
    const [searchParams, setSearchParams] = useSearchParams();
    const keyword = searchParams.get("filter") || "";
    const status = normalizeStatus(searchParams.get("status"));
    const page = positiveInt(searchParams.get("page"), 1);
    const pageSize = normalizePageSize(searchParams.get("pageSize"));
    const debouncedKeyword = useDebouncedValue(keyword);
    const [logs, setLogs] = useState<ApiCallLog[]>([]);
    const [total, setTotal] = useState(0);
    const [loading, setLoading] = useState(true);
    const [loadError, setLoadError] = useState("");
    const [selectedIds, setSelectedIds] = useState<string[]>([]);
    const [detailLogId, setDetailLogId] = useState<string | null>(null);
    const requestSequence = useRef(0);
    const hasFilters = Boolean(keyword || status !== "all");
    const userNameById = useMemo(() => new Map(references.users.map((user) => [user.id, user.displayName || user.username])), [references.users]);
    const currentPageSummary = useMemo(() => {
        const succeeded = logs.filter((log) => log.status === "succeeded").length;
        return { succeeded, failed: logs.length - succeeded };
    }, [logs]);

    const updateUrl = (patch: Record<string, string | number>, replace = false) => {
        const next = new URLSearchParams(searchParams);
        Object.entries(patch).forEach(([key, value]) => {
            const isDefault = (key === "filter" && value === "") || (key === "status" && value === "all") || (key === "page" && value === 1) || (key === "pageSize" && value === 20);
            if (isDefault) next.delete(key);
            else next.set(key, String(value));
        });
        setSearchParams(next, { replace });
    };

    const reload = useCallback(async () => {
        const sequence = ++requestSequence.current;
        setLoading(true);
        setLoadError("");
        try {
            const result = await listAdminApiLogs({ keyword: debouncedKeyword || undefined, status: status === "all" ? undefined : status, page, limit: pageSize });
            if (sequence !== requestSequence.current) return;
            setLogs(result.logs);
            setTotal(result.total);
            setSelectedIds([]);
            if (result.total > 0 && result.logs.length === 0 && page > 1) updateUrl({ page: 1 }, true);
        } catch (error) {
            if (sequence !== requestSequence.current) return;
            const reason = error instanceof Error ? error.message : "读取请求日志失败";
            setLoadError(reason);
            message.error(reason);
        } finally {
            if (sequence === requestSequence.current) setLoading(false);
        }
    }, [debouncedKeyword, message, page, pageSize, status]);

    useEffect(() => {
        void reload();
    }, [reload]);

    const columns: ColumnsType<ApiCallLog> = [
        { title: "时间", dataIndex: "createdAt", width: 170, render: (value) => <span className="admin-log-time">{formatTime(value)}</span> },
        { title: "用户", dataIndex: "userId", width: 160, render: (id) => <span className="admin-log-user">{userNameById.get(id) || id}</span> },
        { title: "渠道", dataIndex: "channelName", width: 170, render: (name, log) => <span className="admin-log-channel">{name || log.channelId || <span className="admin-log-muted-value">未记录</span>}</span> },
        { title: "模型", dataIndex: "model", width: 180, render: (model) => <span className="admin-log-model">{model || <span className="admin-log-muted-value">未识别</span>}</span> },
        {
            title: "能力 / 阶段",
            width: 125,
            render: (_, log) => (
                <span className="admin-log-stage">
                    {capabilityText(log.capability)} / {requestKindText(log.requestKind)}
                </span>
            ),
        },
        {
            title: "状态",
            dataIndex: "status",
            width: 110,
            render: (value, log) => (
                <Tag className="admin-log-status-tag" variant="filled" color={value === "succeeded" ? "success" : "error"}>
                    {value === "succeeded" ? "成功" : `失败 ${log.statusCode || ""}`}
                </Tag>
            ),
        },
        { title: "错误码", dataIndex: "errorCode", width: 160, ellipsis: true, render: (value) => <span className="admin-log-error-code">{value || "--"}</span> },
        { title: "耗时", dataIndex: "durationMs", width: 100, align: "right", render: (value) => <span className="admin-log-numeric-value">{value}ms</span> },
        { title: "Token", width: 145, align: "right", render: (_, log) => <span className="admin-log-numeric-value">{log.usageAvailable ? `${log.inputTokens} / ${log.outputTokens}` : "--"}</span> },
        { title: "费用", width: 140, align: "right", render: (_, log) => <span className="admin-log-numeric-value">{log.costAvailable ? `${log.currency || "USD"} ${(log.estimatedCostMicros / 1_000_000).toFixed(6)}` : "--"}</span> },
        {
            title: "操作",
            width: 90,
            fixed: "right",
            render: (_, log) => (
                <Button className="admin-log-detail-button" size="small" icon={<Eye className="admin-log-detail-icon size-3.5" />} onClick={() => setDetailLogId(log.id)}>
                    详情
                </Button>
            ),
        },
    ];

    return (
        <AdminPageFrame
            title="请求日志"
            description="追踪上游调用、处理阶段、耗时、Token、费用与失败原因"
            actions={
                <div className="admin-page-action-group">
                    <Button className="admin-log-refresh-button" icon={<RefreshCw className="admin-log-refresh-icon size-4" />} loading={loading} onClick={() => void reload()}>
                        刷新
                    </Button>
                    <AdminExportButton
                        className="admin-log-export-button"
                        exportFile={() => exportAdminApiLogs({ keyword: debouncedKeyword || undefined, status: status === "all" ? undefined : status })}
                        fileName={() => `请求日志-${new Date().toISOString().slice(0, 10)}.csv`}
                        label="导出当前筛选"
                        successMessage="已按当前筛选导出请求日志"
                        errorMessage="导出请求日志失败"
                    />
                </div>
            }
        >
            <section className="admin-log-summary" aria-label="请求日志摘要">
                <div className="admin-log-summary-item">
                    <FileClock className="admin-log-summary-icon size-4" />
                    <span className="admin-log-summary-label">筛选结果</span>
                    <strong className="admin-log-summary-value">{total}</strong>
                </div>
                <div className="admin-log-summary-item is-success">
                    <CircleCheck className="admin-log-summary-icon size-4" />
                    <span className="admin-log-summary-label">当前页成功</span>
                    <strong className="admin-log-summary-value">{currentPageSummary.succeeded}</strong>
                </div>
                <div className="admin-log-summary-item is-failed">
                    <CircleX className="admin-log-summary-icon size-4" />
                    <span className="admin-log-summary-label">当前页失败</span>
                    <strong className="admin-log-summary-value">{currentPageSummary.failed}</strong>
                </div>
            </section>
            <ListToolbar
                className="admin-log-toolbar"
                active={hasFilters}
                onReset={() => updateUrl({ filter: "", status: "all", page: 1 })}
                trailing={
                    <span className="admin-log-result-context">
                        第 {page} 页 · 当前显示 {logs.length} 条
                    </span>
                }
            >
                <Input
                    allowClear
                    className="app-list-search admin-log-search"
                    prefix={<Search className="admin-log-search-icon size-4 text-foreground/40" />}
                    value={keyword}
                    placeholder="搜索用户、渠道、模型、路径或请求号"
                    onChange={(event) => updateUrl({ filter: event.target.value, page: 1 }, true)}
                />
                <Select
                    className="admin-log-status-filter"
                    value={status}
                    onChange={(value) => updateUrl({ status: value, page: 1 })}
                    options={[
                        { label: "全部结果", value: "all" },
                        { label: "成功", value: "succeeded" },
                        { label: "失败", value: "failed" },
                    ]}
                />
            </ListToolbar>
            <div className="admin-log-selection-region">
                <AdminBatchBar count={selectedIds.length} onClear={() => setSelectedIds([])}>
                    <AdminExportButton
                        className="admin-log-selected-export-button"
                        type="primary"
                        size="small"
                        exportFile={() => exportAdminApiLogs({ ids: selectedIds })}
                        fileName={() => `请求明细-已选${selectedIds.length}条.csv`}
                        label="导出已选"
                        successMessage={`已导出选中的 ${selectedIds.length} 条请求明细`}
                        errorMessage="导出请求明细失败"
                    />
                </AdminBatchBar>
            </div>
            <TableSurface className="admin-log-table-surface">
                {loading && logs.length === 0 ? (
                    <AdminTableSkeleton rows={8} columns={11} />
                ) : loadError && logs.length === 0 ? (
                    <div className="admin-log-load-error">
                        <AdminContentError title="请求日志读取失败" description={loadError} onRetry={() => void reload()} />
                    </div>
                ) : (
                    <>
                        {loadError ? (
                            <div className="admin-log-refresh-error">
                                <AdminContentError title="请求日志刷新失败" description={`${loadError}。当前继续显示上一次成功读取的数据。`} onRetry={() => void reload()} />
                            </div>
                        ) : null}
                        <Table
                            className="app-data-table admin-log-table"
                            size="middle"
                            rowKey="id"
                            loading={loading}
                            rowSelection={{ selectedRowKeys: selectedIds, preserveSelectedRowKeys: false, onChange: (keys) => setSelectedIds(keys.map(String)) }}
                            columns={columns}
                            dataSource={logs}
                            locale={{
                                emptyText: (
                                    <AdminTableEmpty filtered={hasFilters} title={hasFilters ? "没有符合筛选条件的请求" : "暂无请求日志"} description={hasFilters ? "调整搜索词或状态筛选后再试。" : "模型调用发生后，请求阶段、耗时和费用会显示在这里。"} />
                                ),
                            }}
                            pagination={false}
                            scroll={{ x: "max-content" }}
                        />
                        <PaginationBar current={page} pageSize={pageSize} total={total} onChange={(nextPage, nextSize) => updateUrl({ page: nextSize !== pageSize ? 1 : nextPage, pageSize: nextSize })} />
                    </>
                )}
            </TableSurface>
            <ApiLogDetailDrawer logId={detailLogId} onClose={() => setDetailLogId(null)} />
        </AdminPageFrame>
    );
}

function positiveInt(value: string | null, fallback: number) {
    const parsed = Number(value);
    return Number.isInteger(parsed) && parsed > 0 ? parsed : fallback;
}
function normalizePageSize(value: string | null) {
    const parsed = positiveInt(value, 20);
    return [20, 50, 100].includes(parsed) ? parsed : 20;
}
function normalizeStatus(value: string | null): "all" | "succeeded" | "failed" {
    return value === "succeeded" || value === "failed" ? value : "all";
}
function formatTime(value?: string) {
    return value ? new Date(value).toLocaleString("zh-CN", { hour12: false }) : "--";
}
function capabilityText(value: string) {
    return ({ text: "文本", image: "图片", video: "视频", audio: "音频" } as Record<string, string>)[value] || "未知";
}
function requestKindText(value: string) {
    return ({ create: "创建", poll: "轮询", download: "下载", repair: "修复" } as Record<string, string>)[value] || "请求";
}
