import { App, Button, Input, Select, Table, Tag, Tooltip } from "antd";
import type { ColumnsType } from "antd/es/table";
import { CircleStop, RefreshCw, Search } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useSearchParams } from "react-router";

import { ListToolbar, PaginationBar, TableSurface } from "@/components/layout/workspace-page";
import { useDebouncedValue } from "@/hooks/use-debounced-value";
import { AdminContentSection, AdminDataLayout } from "@/pages/admin/components/admin-data-layout";
import { AdminPageFrame } from "@/pages/admin/components/admin-shell";
import { AdminContentError, AdminTableEmpty, AdminTableSkeleton } from "@/pages/admin/components/admin-ui";
import { AdminAgentRunApiError, getAdminAgentRun, getAdminAgentRuns, interruptAdminAgentRun, type AdminAgentRun, type AdminAgentRunActivity, type AdminAgentRunStatus } from "@/services/api/admin-agent-runs";

import { AgentRunInterruptModal } from "./agent-run-interrupt-modal";
import { formatAgentRunInactiveDuration, formatAgentRunTimestamp, getAgentRunActivityLabel, getAgentRunStatusLabel } from "./agent-run-presenters";
import { applyAgentRunConflict, failAgentRunPageLoad, startAgentRunPageLoad, succeedAgentRunPageLoad, type AgentRunInterruptDraft, type AgentRunPageState } from "./agent-run-page-state";

const initialPageState: AgentRunPageState = { data: null, loading: true, refreshing: false, error: "" };

export default function AgentRunsPage() {
    const { message } = App.useApp();
    const [searchParams, setSearchParams] = useSearchParams();
    const user = searchParams.get("user") ?? "";
    const scope = searchParams.get("scope") ?? "";
    const status = normalizeStatus(searchParams.get("status"));
    const activity = normalizeActivity(searchParams.get("activity"));
    const page = positiveInteger(searchParams.get("page"), 1);
    const pageSize = normalizePageSize(searchParams.get("pageSize"));
    const debouncedUser = useDebouncedValue(user);
    const debouncedScope = useDebouncedValue(scope);
    const [pageState, setPageState] = useState(initialPageState);
    const [interruptDraft, setInterruptDraft] = useState<AgentRunInterruptDraft | null>(null);
    const [openingRunId, setOpeningRunId] = useState("");
    const requestSequence = useRef(0);
    const hasFilters = Boolean(user || scope || status || activity);

    const updateUrl = useCallback(
        (patch: Record<string, string | number>, replace = false) => {
            const next = new URLSearchParams(searchParams);
            for (const [key, value] of Object.entries(patch)) {
                const isDefault = value === "" || (key === "page" && value === 1) || (key === "pageSize" && value === 20);
                if (isDefault) next.delete(key);
                else next.set(key, String(value));
            }
            setSearchParams(next, { replace });
        },
        [searchParams, setSearchParams],
    );

    const reload = useCallback(async () => {
        const sequence = ++requestSequence.current;
        setPageState(startAgentRunPageLoad);
        try {
            const result = await getAdminAgentRuns({
                user: debouncedUser || undefined,
                scope: debouncedScope || undefined,
                status: status || undefined,
                activity: activity || undefined,
                page,
                pageSize,
            });
            if (sequence !== requestSequence.current) return;
            setPageState((current) => succeedAgentRunPageLoad(current, result));
            if (result.total > 0 && result.items.length === 0 && page > 1) updateUrl({ page: 1 }, true);
        } catch (error) {
            if (sequence !== requestSequence.current) return;
            setPageState((current) => failAgentRunPageLoad(current, error instanceof Error ? error.message : "读取 Agent 运行失败"));
        }
    }, [activity, debouncedScope, debouncedUser, page, pageSize, status, updateUrl]);

    useEffect(() => {
        void reload();
    }, [reload]);

    const openInterrupt = useCallback(
        async (record: AdminAgentRun) => {
            setOpeningRunId(record.runId);
            try {
                const run = await getAdminAgentRun(record.runId);
                setInterruptDraft({ run, reason: "", confirmation: "", submitting: false, error: "" });
            } catch (error) {
                message.error(error instanceof Error ? error.message : "读取 Agent 运行详情失败");
            } finally {
                setOpeningRunId("");
            }
        },
        [message],
    );

    const submitInterrupt = useCallback(async () => {
        if (!interruptDraft || interruptDraft.submitting) return;
        const submittedDraft = { ...interruptDraft, submitting: true, error: "" };
        setInterruptDraft(submittedDraft);
        try {
            const result = await interruptAdminAgentRun(submittedDraft.run.runId, {
                expectedStateVersion: submittedDraft.run.stateVersion,
                reason: submittedDraft.reason,
                confirmation: submittedDraft.confirmation,
            });
            setInterruptDraft(null);
            message.success(result.reconciliationPending ? "Agent 已终止，供应商任务取消与账务核对处理中" : "Agent 运行已终止");
            await reload();
        } catch (error) {
            if (error instanceof AdminAgentRunApiError && error.status === 409 && error.latestRun) {
                setInterruptDraft(applyAgentRunConflict(submittedDraft, error.latestRun, error.message));
                return;
            }
            setInterruptDraft({ ...submittedDraft, submitting: false, error: error instanceof Error ? error.message : "终止 Agent 运行失败" });
        }
    }, [interruptDraft, message, reload]);

    const columns = useMemo<ColumnsType<AdminAgentRun>>(
        () => [
            {
                title: "用户 / 范围",
                width: 220,
                render: (_, record) => (
                    <div className="agent-runs-scope-cell">
                        <strong className="agent-runs-user-name">{record.actorDisplayName}</strong>
                        <span className="agent-runs-scope-value">{record.domainProjectId || record.canvasId || "未提供范围"}</span>
                    </div>
                ),
            },
            {
                title: "运行状态",
                width: 160,
                render: (_, record) => (
                    <div className="agent-runs-status-cell">
                        <Tag className="agent-runs-status-tag" color={runStatusColor(record.status)} variant="filled">
                            {getAgentRunStatusLabel(record.status)}
                        </Tag>
                        <span className="agent-runs-step-value">
                            第 {record.stepNumber} / {record.maxSteps} 步
                        </span>
                    </div>
                ),
            },
            {
                title: "活动事实",
                width: 170,
                render: (_, record) => (
                    <div className="agent-runs-activity-cell">
                        <span className={`agent-runs-activity-value is-${record.activityClassification}`}>{getAgentRunActivityLabel(record.activityClassification)}</span>
                        <span className="agent-runs-inactive-value">{formatAgentRunInactiveDuration(record.inactiveSeconds)}</span>
                    </div>
                ),
            },
            {
                title: "任务 / 账务",
                width: 190,
                render: (_, record) => (
                    <div className="agent-runs-fact-cell">
                        <span className="agent-runs-task-value">
                            模型 {record.linkedModelTaskStatus} · 识图 {record.linkedVisionTaskStatus} · 媒体 {record.linkedMediaTaskStatus}
                        </span>
                        <span className="agent-runs-billing-value">
                            账务 {record.billingState} · 请求 {record.providerRequestState}
                        </span>
                    </div>
                ),
            },
            {
                title: "最近更新",
                dataIndex: "updatedAt",
                width: 170,
                render: (value: string) => <span className="agent-runs-updated-at">{formatAgentRunTimestamp(value)}</span>,
            },
            {
                title: "操作",
                width: 72,
                fixed: "right",
                align: "right",
                render: (_, record) => {
                    const disabled = record.controlDisposition === "blocked_by_unresolved_billing" || record.controlDisposition === "already_terminal";
                    const button = (
                        <Button
                            className="agent-runs-interrupt-button"
                            type="text"
                            danger
                            icon={<CircleStop className="agent-runs-interrupt-icon size-4" />}
                            loading={openingRunId === record.runId}
                            disabled={disabled}
                            aria-label={`终止 ${record.actorDisplayName} 的 Agent 运行`}
                            onClick={() => void openInterrupt(record)}
                        />
                    );
                    return (
                        <Tooltip rootClassName="agent-runs-interrupt-tooltip" title={disabled ? "当前运行存在账务阻断或已经结束" : "查看影响并终止"}>
                            <span className="agent-runs-interrupt-trigger">{button}</span>
                        </Tooltip>
                    );
                },
            },
        ],
        [openInterrupt, openingRunId],
    );

    return (
        <AdminPageFrame
            title="Agent 任务"
            description="跨用户查看真实 Agent 运行事实，并审计化终止卡住的运行"
            actions={
                <Button className="agent-runs-refresh-button" icon={<RefreshCw className="agent-runs-refresh-icon size-4" />} loading={pageState.refreshing} onClick={() => void reload()}>
                    刷新
                </Button>
            }
        >
            <AdminDataLayout>
                <AdminContentSection
                    className="agent-runs-content-section"
                    title="运行目录"
                    description="列表仅包含未结束运行；活动分类、任务、账务和供应商状态均来自服务端事实。"
                    actions={<span className="agent-runs-result-count">共 {pageState.data?.total ?? 0} 条</span>}
                >
                    <ListToolbar className="agent-runs-toolbar" active={hasFilters} onReset={() => updateUrl({ user: "", scope: "", status: "", activity: "", page: 1 })}>
                        <Input
                            className="agent-runs-user-filter"
                            allowClear
                            prefix={<Search className="agent-runs-filter-icon size-4" />}
                            value={user}
                            placeholder="用户 ID、邮箱或名称"
                            onChange={(event) => updateUrl({ user: event.target.value, page: 1 }, true)}
                        />
                        <Input className="agent-runs-scope-filter" allowClear value={scope} placeholder="运行、项目或画布 ID" onChange={(event) => updateUrl({ scope: event.target.value, page: 1 }, true)} />
                        <Select
                            className="agent-runs-status-filter"
                            value={status || undefined}
                            placeholder="全部运行状态"
                            allowClear
                            onChange={(value: AdminAgentRunStatus | undefined) => updateUrl({ status: value ?? "", page: 1 })}
                            options={nonTerminalStatusOptions}
                        />
                        <Select
                            className="agent-runs-activity-filter"
                            value={activity || undefined}
                            placeholder="全部活动分类"
                            allowClear
                            onChange={(value: AdminAgentRunActivity | undefined) => updateUrl({ activity: value ?? "", page: 1 })}
                            options={activityOptions}
                        />
                    </ListToolbar>
                    <TableSurface className="agent-runs-table-surface">
                        {pageState.loading && pageState.data === null ? (
                            <AdminTableSkeleton rows={8} columns={6} />
                        ) : pageState.error && pageState.data === null ? (
                            <div className="agent-runs-first-load-error">
                                <AdminContentError title="Agent 运行目录读取失败" description={pageState.error} onRetry={() => void reload()} />
                            </div>
                        ) : (
                            <div className="agent-runs-table-region">
                                {pageState.error ? (
                                    <div className="agent-runs-refresh-error">
                                        <AdminContentError title="Agent 运行目录刷新失败" description={`${pageState.error}。当前继续显示上一次成功读取的事实。`} onRetry={() => void reload()} />
                                    </div>
                                ) : null}
                                <Table<AdminAgentRun>
                                    className="app-data-table agent-runs-table"
                                    size="middle"
                                    rowKey="runId"
                                    columns={columns}
                                    dataSource={pageState.data?.items ?? []}
                                    pagination={false}
                                    scroll={{ x: "max-content" }}
                                    locale={{
                                        emptyText: (
                                            <AdminTableEmpty
                                                filtered={hasFilters}
                                                title={hasFilters ? "没有符合筛选条件的运行" : "暂无未结束的 Agent 运行"}
                                                description={hasFilters ? "调整用户、范围或状态筛选后再试。" : "新的 Agent 运行开始后会显示在这里。"}
                                            />
                                        ),
                                    }}
                                />
                                <PaginationBar current={page} pageSize={pageSize} total={pageState.data?.total ?? 0} onChange={(nextPage, nextSize) => updateUrl({ page: nextSize !== pageSize ? 1 : nextPage, pageSize: nextSize })} />
                            </div>
                        )}
                    </TableSurface>
                </AdminContentSection>
            </AdminDataLayout>
            <AgentRunInterruptModal
                draft={interruptDraft}
                onChange={setInterruptDraft}
                onCancel={() => {
                    if (!interruptDraft?.submitting) setInterruptDraft(null);
                }}
                onSubmit={() => void submitInterrupt()}
            />
        </AdminPageFrame>
    );
}

const nonTerminalStatusOptions: Array<{ label: string; value: AdminAgentRunStatus }> = [
    { label: "排队中", value: "queued" },
    { label: "运行中", value: "running" },
    { label: "等待用户回答", value: "waiting_input" },
    { label: "等待用户批准", value: "waiting_approval" },
    { label: "等待工具", value: "waiting_tool" },
];

const activityOptions: Array<{ label: string; value: AdminAgentRunActivity }> = [
    { label: "活跃", value: "active" },
    { label: "等待用户", value: "awaiting_user" },
    { label: "可能卡住", value: "possibly_stalled" },
];

function positiveInteger(value: string | null, fallback: number) {
    const parsed = Number(value);
    return Number.isInteger(parsed) && parsed > 0 ? parsed : fallback;
}

function normalizePageSize(value: string | null): 20 | 50 | 100 {
    const parsed = positiveInteger(value, 20);
    return parsed === 50 || parsed === 100 ? parsed : 20;
}

function normalizeStatus(value: string | null): AdminAgentRunStatus | "" {
    return nonTerminalStatusOptions.some((option) => option.value === value) ? (value as AdminAgentRunStatus) : "";
}

function normalizeActivity(value: string | null): AdminAgentRunActivity | "" {
    return activityOptions.some((option) => option.value === value) ? (value as AdminAgentRunActivity) : "";
}

function runStatusColor(status: AdminAgentRunStatus) {
    if (status === "waiting_input" || status === "waiting_approval") return "warning";
    if (status === "failed" || status === "cancelled") return "error";
    if (status === "succeeded") return "success";
    return "processing";
}
