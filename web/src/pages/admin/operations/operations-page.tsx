import { Alert, App, Button, Input, Modal, Table, Tag } from "antd";
import { ArchiveRestore, CircleAlert, CloudDownload, DatabaseBackup, Globe2, RefreshCw, RotateCcw, ServerCog, ShieldCheck } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import { AdminPageFrame } from "@/pages/admin/components/admin-shell";
import { AdminContentSection, AdminDataLayout } from "@/pages/admin/components/admin-data-layout";
import { AdminContentError, AdminContentSkeleton, AdminTableEmpty } from "@/pages/admin/components/admin-ui";
import { OperationActivePanel } from "@/pages/admin/operations/operation-active-panel";
import { operationControlIdempotencyKey } from "@/pages/admin/operations/operations-control";
import {
    actionLabels,
    backupColumns,
    createOperationColumns,
    formatLogTime,
    formatTime,
    OperationActionButton,
    OperationStatusTag,
    OverviewMetric,
    presentPublicVerification,
    releaseCheckDetail,
    shortCommit,
} from "@/pages/admin/operations/operations-presenters";
import {
    cancelOperation,
    getOperation,
    getOperationsOverview,
    listOperationBackups,
    listOperationLogs,
    listOperations,
    recoverOperation,
    startOperation,
    type OperationsAction,
    type OperationsBackup,
    type OperationsLog,
    type OperationsOverview,
    type OperationsRecord,
} from "@/services/api/operations";

type PendingAction = {
    action: OperationsAction | "cancel" | "recover";
    idempotencyKey: string;
    operationId?: string;
    title: string;
    description: string;
    targetVersion?: string;
    expectedConfirmation: string;
    impacts: string[];
    danger?: boolean;
};

export default function OperationsPage() {
    const { message } = App.useApp();
    const [overview, setOverview] = useState<OperationsOverview | null>(null);
    const [operations, setOperations] = useState<OperationsRecord[]>([]);
    const [backups, setBackups] = useState<OperationsBackup[]>([]);
    const [selectedOperationId, setSelectedOperationId] = useState("");
    const [selectedOperation, setSelectedOperation] = useState<OperationsRecord | null>(null);
    const [logs, setLogs] = useState<OperationsLog[]>([]);
    const [loading, setLoading] = useState(true);
    const [refreshing, setRefreshing] = useState(false);
    const [dashboardError, setDashboardError] = useState("");
    const [logError, setLogError] = useState("");
    const [pendingAction, setPendingAction] = useState<PendingAction | null>(null);
    const [confirmation, setConfirmation] = useState("");
    const [submitting, setSubmitting] = useState(false);
    const logCursorRef = useRef(0);
    const logLoadingRef = useRef(false);
    const logRequestVersionRef = useRef(0);
    const selectedOperationIdRef = useRef("");
    const logViewportRef = useRef<HTMLDivElement | null>(null);

    const loadDashboard = useCallback(async (silent = false) => {
        if (!silent) setRefreshing(true);
        try {
            const [nextOverview, operationPage, nextBackups] = await Promise.all([getOperationsOverview(), listOperations(50), listOperationBackups(50)]);
            setOverview(nextOverview);
            setOperations(operationPage.items);
            setBackups(nextBackups);
            setDashboardError("");
            setSelectedOperationId((current) => current || nextOverview.activeOperation?.id || nextOverview.latestOperation?.id || operationPage.items[0]?.id || "");
        } catch (error) {
            setDashboardError(error instanceof Error ? error.message : "读取运维状态失败");
        } finally {
            setLoading(false);
            setRefreshing(false);
        }
    }, []);

    const loadSelectedOperation = useCallback(async (operationId: string) => {
        if (!operationId) {
            setSelectedOperation(null);
            setLogs([]);
            setLogError("");
            logCursorRef.current = 0;
            return;
        }
        if (logLoadingRef.current) return;
        logLoadingRef.current = true;
        const requestVersion = logRequestVersionRef.current;
        const cursor = logCursorRef.current;
        try {
            const [operation, logPage] = await Promise.all([getOperation(operationId), listOperationLogs(operationId, cursor, 500)]);
            if (selectedOperationIdRef.current !== operationId || logRequestVersionRef.current !== requestVersion) return;
            setSelectedOperation(operation);
            setLogError("");
            if (logPage.items.length > 0) {
                setLogs((current) => {
                    const knownSequences = new Set(current.map((entry) => entry.sequence));
                    return [...current, ...logPage.items.filter((entry) => !knownSequences.has(entry.sequence))];
                });
                logCursorRef.current = logPage.nextCursor;
            }
        } catch (error) {
            if (selectedOperationIdRef.current === operationId && logRequestVersionRef.current === requestVersion && error instanceof Error) {
                setLogError(error.message);
            }
        } finally {
            if (logRequestVersionRef.current === requestVersion) logLoadingRef.current = false;
        }
    }, []);

    useEffect(() => {
        void loadDashboard();
    }, [loadDashboard]);

    useEffect(() => {
        selectedOperationIdRef.current = selectedOperationId;
        logRequestVersionRef.current += 1;
        logCursorRef.current = 0;
        logLoadingRef.current = false;
        setLogs([]);
        setLogError("");
        void loadSelectedOperation(selectedOperationId);
    }, [loadSelectedOperation, selectedOperationId]);

    const hasActiveOperation = Boolean(overview?.activeOperation);

    useEffect(() => {
        if (!hasActiveOperation && selectedOperation?.status !== "queued" && selectedOperation?.status !== "running") return;
        const timer = window.setInterval(() => {
            void loadDashboard(true);
            if (selectedOperationId) void loadSelectedOperation(selectedOperationId);
        }, 1500);
        return () => window.clearInterval(timer);
    }, [hasActiveOperation, loadDashboard, loadSelectedOperation, selectedOperation?.status, selectedOperationId]);

    useEffect(() => {
        const viewport = logViewportRef.current;
        if (viewport) viewport.scrollTop = viewport.scrollHeight;
    }, [logs]);

    const openAction = (action: OperationsAction) => {
        if (!overview) return;
        const current = overview.release.currentVersion || "";
        const latest = overview.release.latestVersion || "";
        if (action === "upgrade") {
            const targetVersion = latest;
            if (!targetVersion) {
                message.error("尚未获取到可升级版本，请先修复版本检查配置");
                return;
            }
            setPendingAction({
                action,
                idempotencyKey: crypto.randomUUID(),
                title: `升级到 ${targetVersion}`,
                description: `控制器将先核对当前版本 ${current || "未知"}，拉取不可变镜像、停止业务写入、创建一致性备份，再启动并验活新版本。失败时自动恢复原版本。`,
                targetVersion,
                expectedConfirmation: `UPGRADE ${targetVersion}`,
                impacts: ["暂停业务写入并创建一致性备份", `切换运行版本至 ${targetVersion}`, "新版本验活失败时自动恢复原版本"],
            });
        } else if (action === "rollback") {
            setPendingAction({
                action,
                idempotencyKey: crypto.randomUUID(),
                title: `回滚到 ${overview.previousVersion || "上一版本"}`,
                description: "控制器会先为当前版本创建安全备份，再恢复升级前的数据库、资源卷和镜像。该操作会短暂停止业务服务。",
                expectedConfirmation: "ROLLBACK",
                impacts: ["暂停业务服务并备份当前状态", `恢复至 ${overview.previousVersion || "上一版本"}`, "数据库与资源卷恢复到对应恢复点"],
                danger: true,
            });
        } else if (action === "backup") {
            setPendingAction({
                action,
                idempotencyKey: crypto.randomUUID(),
                title: "创建一致性备份",
                description: "控制器会短暂停止 Web 与业务后端写入，校验 PostgreSQL 与资源卷备份后恢复当前版本。",
                expectedConfirmation: "BACKUP",
                impacts: ["短暂停止 Web 与后端写入", "备份 PostgreSQL 与资源卷", "完成完整性校验后恢复当前版本"],
            });
        } else {
            setPendingAction({
                action,
                idempotencyKey: crypto.randomUUID(),
                title: "执行生产环境校验",
                description: "校验生产配置与 Docker 基础依赖，并检查每个生产 Origin 的画布入口及当前版本全部 CDN 清单资源；不会修改业务数据或回滚业务版本。",
                expectedConfirmation: "VERIFY",
                impacts: ["仅读取当前运行环境", "检查生产画布入口与 CDN 发布清单", "失败只记录环境故障，不修改或回滚业务版本"],
            });
        }
        setConfirmation("");
    };

    const openControlAction = async (action: "cancel" | "recover", operation: OperationsRecord) => {
        const stopping = action === "cancel";
        setPendingAction({
            action,
            idempotencyKey: operationControlIdempotencyKey(action, operation.id),
            operationId: operation.id,
            title: stopping ? "安全停止运维任务" : "恢复运维任务",
            description: stopping ? "控制器会持久化停止命令；Runner 只会在安全点停止，并如实保留当时的服务状态与检查点。" : "控制器会先证明旧 Runner 已停止，再基于持久检查点启动新 generation；无法确定安全动作时会继续显式失败。",
            expectedConfirmation: `${stopping ? "STOP" : "RECOVER"} ${operation.id}`,
            impacts: stopping
                ? ["不会强杀正在写入的阶段", "到达安全点后停止后续输出", "保留日志、检查点和已产生的备份事实"]
                : ["确认旧 Runner 不再拥有生产写入权", `执行恢复动作：${operation.recoveryAction || "由持久事实决定"}`, "恢复结果与服务状态会继续写入审计记录"],
            danger: stopping,
        });
        setConfirmation("");
    };

    const submitAction = async () => {
        if (!pendingAction || confirmation !== pendingAction.expectedConfirmation) return;
        setSubmitting(true);
        try {
            const operation =
                pendingAction.action === "cancel"
                    ? await cancelOperation(pendingAction.operationId || "", { confirmation, idempotencyKey: pendingAction.idempotencyKey })
                    : pendingAction.action === "recover"
                      ? await recoverOperation(pendingAction.operationId || "", { confirmation, idempotencyKey: pendingAction.idempotencyKey })
                      : await startOperation({
                            action: pendingAction.action,
                            targetVersion: pendingAction.targetVersion,
                            confirmation,
                            idempotencyKey: pendingAction.idempotencyKey,
                        });
            setPendingAction(null);
            setConfirmation("");
            setSelectedOperationId(operation.id);
            message.success(pendingAction.action === "cancel" ? "控制器已持久化安全停止请求" : pendingAction.action === "recover" ? "恢复 Runner 已进入独立控制器队列" : "运维任务已进入独立控制器队列");
            await loadDashboard(true);
        } catch (error) {
            message.error(error instanceof Error ? error.message : "创建运维任务失败");
        } finally {
            setSubmitting(false);
        }
    };

    const operationColumns = useMemo(() => createOperationColumns(setSelectedOperationId), []);
    const activeOperation = overview?.activeOperation;
    const hasDashboardData = overview !== null;
    const publicVerification = overview ? presentPublicVerification(overview.publicVerification) : null;

    return (
        <AdminPageFrame
            title="运维升级中心"
            description="由独立部署控制器执行版本检查、备份、升级与回滚"
            actions={
                <Button className="operations-refresh-button" icon={<RefreshCw className="operations-refresh-icon size-4" />} loading={refreshing} onClick={() => void loadDashboard()}>
                    刷新状态
                </Button>
            }
        >
            <AdminDataLayout>
                {dashboardError ? (
                    <AdminContentError
                        title={hasDashboardData ? "运维状态刷新失败" : "独立运维控制器不可用"}
                        description={hasDashboardData ? `${dashboardError}。当前继续显示上一次成功读取的控制器状态。` : dashboardError}
                        onRetry={() => void loadDashboard()}
                    />
                ) : null}

                {loading && !hasDashboardData ? (
                    <AdminContentSkeleton rows={12} label="正在读取运维控制器状态" />
                ) : !hasDashboardData ? null : (
                    <>
                        <AdminContentSection className="operations-overview-section" title="运行状态概览" description="集中查看控制器、版本、备份与回滚恢复点。">
                            <div className="operations-overview-grid grid grid-cols-1 gap-px md:grid-cols-2 xl:grid-cols-5" aria-label="运维状态概览">
                                <OverviewMetric
                                    icon={<ServerCog className="operations-metric-icon-svg size-4" />}
                                    label="控制器"
                                    value={overview?.controller.status === "ok" ? "在线" : loading ? "连接中" : "离线"}
                                    detail={overview ? `${overview.controller.version} · ${shortCommit(overview.controller.commit)}` : "等待状态"}
                                    tone={overview?.controller.status === "ok" ? "success" : "neutral"}
                                />
                                <OverviewMetric
                                    icon={<CloudDownload className="operations-metric-icon-svg size-4" />}
                                    label="当前 / 最新"
                                    value={`${overview?.release.currentVersion || "--"} / ${overview?.release.latestVersion || "--"}`}
                                    detail={releaseCheckDetail(overview)}
                                    tone={overview?.release.updateAvailable ? "warning" : overview?.release.status === "ok" ? "success" : "neutral"}
                                />
                                <OverviewMetric
                                    icon={<DatabaseBackup className="operations-metric-icon-svg size-4" />}
                                    label="最近备份"
                                    value={overview?.latestBackup?.version || "--"}
                                    detail={overview?.latestBackup ? formatTime(overview.latestBackup.createdAt) : "暂无有效恢复点"}
                                    tone={overview?.latestBackup?.checksumStatus === "verified" ? "success" : "neutral"}
                                />
                                <OverviewMetric
                                    icon={<ArchiveRestore className="operations-metric-icon-svg size-4" />}
                                    label="回滚状态"
                                    value={overview?.rollbackReady ? "可回滚" : "不可回滚"}
                                    detail={overview ? `${overview.rollbackStatus}${overview.rollbackReady ? ` · 目标 ${overview.previousVersion}` : ""}` : "等待状态"}
                                    tone={overview?.rollbackReady ? "success" : "neutral"}
                                />
                                <OverviewMetric
                                    icon={<Globe2 className="operations-metric-icon-svg size-4" />}
                                    label="公网校验"
                                    value={publicVerification?.label || "--"}
                                    detail={publicVerification?.detail || "等待状态"}
                                    tone={publicVerification?.tone || "neutral"}
                                />
                            </div>
                        </AdminContentSection>

                        <AdminContentSection
                            className="operations-actions-card"
                            title="受控操作"
                            description="业务后端仅校验管理员并签发请求；Docker、备份和服务重启只由独立控制器执行。"
                            actions={
                                hasActiveOperation ? (
                                    <Tag className="operations-active-tag" color="processing" variant="filled">
                                        任务执行中
                                    </Tag>
                                ) : (
                                    <Tag className="operations-idle-tag" variant="filled">
                                        无活动任务
                                    </Tag>
                                )
                            }
                        >
                            {activeOperation ? (
                                <OperationActivePanel
                                    operation={activeOperation}
                                    submitting={submitting}
                                    onCancel={(operation) => openControlAction("cancel", operation)}
                                    onRecover={(operation) => openControlAction("recover", operation)}
                                    onViewLogs={(operation) => setSelectedOperationId(operation.id)}
                                />
                            ) : null}
                            <div className="operations-actions-grid grid grid-cols-1 gap-px bg-border/60 sm:grid-cols-2 xl:grid-cols-4">
                                <OperationActionButton
                                    icon={<CloudDownload className="operations-action-icon-svg size-4" />}
                                    title="升级"
                                    description={overview?.release.latestVersion ? `升级到 ${overview.release.latestVersion}` : "等待版本检查"}
                                    disabled={hasActiveOperation || !overview?.release.updateAvailable}
                                    disabledReason={hasActiveOperation ? "已有运维任务正在执行" : overview?.release.updateAvailable ? undefined : "当前没有可升级版本"}
                                    onClick={() => openAction("upgrade")}
                                />
                                <OperationActionButton
                                    icon={<RotateCcw className="operations-action-icon-svg size-4" />}
                                    title="回滚"
                                    description={overview?.previousVersion ? `恢复 ${overview.previousVersion}` : "没有可用恢复点"}
                                    disabled={hasActiveOperation || !overview?.rollbackReady}
                                    disabledReason={hasActiveOperation ? "已有运维任务正在执行" : "当前没有通过校验的回滚恢复点"}
                                    onClick={() => openAction("rollback")}
                                />
                                <OperationActionButton
                                    icon={<DatabaseBackup className="operations-action-icon-svg size-4" />}
                                    title="立即备份"
                                    description="PostgreSQL 与资源卷"
                                    disabled={hasActiveOperation || !overview?.release.currentVersion}
                                    disabledReason={hasActiveOperation ? "已有运维任务正在执行" : "尚未识别当前运行版本"}
                                    onClick={() => openAction("backup")}
                                />
                                <OperationActionButton
                                    icon={<ShieldCheck className="operations-action-icon-svg size-4" />}
                                    title="环境校验"
                                    description="生产入口与 CDN 清单"
                                    disabled={hasActiveOperation || !overview?.release.currentVersion}
                                    disabledReason={hasActiveOperation ? "已有运维任务正在执行" : "尚未识别当前运行版本"}
                                    onClick={() => openAction("verify")}
                                />
                            </div>
                        </AdminContentSection>

                        <AdminContentSection
                            className="operations-log-card"
                            title="实时执行日志"
                            description={selectedOperation ? `${actionLabels[selectedOperation.action]} · ${selectedOperation.phase}` : "选择一条审计记录查看控制器日志"}
                            actions={selectedOperation ? <OperationStatusTag status={selectedOperation.status} /> : null}
                        >
                            <div ref={logViewportRef} className="operations-log-viewport thin-scrollbar" role="log" aria-live="polite">
                                {logs.length > 0 ? (
                                    logs.map((entry) => (
                                        <div key={entry.sequence} className={`operations-log-line is-${entry.stream}`}>
                                            <time className="operations-log-time" dateTime={entry.createdAt}>
                                                {formatLogTime(entry.createdAt)}
                                            </time>
                                            <span className="operations-log-stream">{entry.stream}</span>
                                            <span className="operations-log-message">{entry.message}</span>
                                        </div>
                                    ))
                                ) : (
                                    <div className="operations-log-empty">{selectedOperation ? "等待控制器日志..." : "暂无选中的运维任务"}</div>
                                )}
                            </div>
                            {logError ? <AdminContentError title="控制器日志读取失败" description={logs.length ? `${logError}。当前继续显示已成功读取的日志。` : logError} onRetry={() => void loadSelectedOperation(selectedOperationId)} /> : null}
                            {selectedOperation?.error ? <Alert className="operations-operation-error" type="error" showIcon message="任务失败" description={selectedOperation.error} /> : null}
                        </AdminContentSection>

                        <AdminContentSection
                            className="operations-audit-card"
                            title="操作审计"
                            description="控制器独立数据库中的不可编辑执行记录，业务数据库回滚不会清除这些证据。"
                            actions={<span className="operations-section-count">共 {operations.length} 条记录</span>}
                        >
                            <Table<OperationsRecord>
                                className="operations-audit-table"
                                rowKey="id"
                                columns={operationColumns}
                                dataSource={operations}
                                loading={loading}
                                pagination={false}
                                size="middle"
                                scroll={{ x: "max-content" }}
                                rowClassName={(record) => (record.id === selectedOperationId ? "operations-audit-row is-selected" : "operations-audit-row")}
                                onRow={(record) => ({ onClick: () => setSelectedOperationId(record.id) })}
                                locale={{ emptyText: <AdminTableEmpty compact title="暂无运维操作" description="升级、回滚、备份和环境校验记录会显示在这里。" /> }}
                            />
                        </AdminContentSection>

                        <AdminContentSection
                            className="operations-backups-card"
                            title="备份状态"
                            description="恢复点按清单与 SHA-256 校验；校验失败的备份不会作为可用恢复依据。"
                            actions={<span className="operations-section-count">共 {backups.length} 个恢复点</span>}
                        >
                            <Table<OperationsBackup>
                                className="operations-backups-table"
                                rowKey="path"
                                columns={backupColumns}
                                dataSource={backups}
                                loading={loading}
                                pagination={false}
                                size="middle"
                                scroll={{ x: "max-content" }}
                                locale={{ emptyText: <AdminTableEmpty compact title="暂无备份" description="执行升级或立即备份后，恢复点会显示在这里。" /> }}
                            />
                        </AdminContentSection>
                    </>
                )}
            </AdminDataLayout>

            <Modal
                className="admin-operation-modal operations-confirm-modal workspace-ui-scope"
                title={pendingAction?.title}
                open={Boolean(pendingAction)}
                okText="提交到控制器"
                cancelText="取消"
                okButtonProps={{ danger: pendingAction?.danger, disabled: confirmation !== pendingAction?.expectedConfirmation }}
                confirmLoading={submitting}
                onOk={() => void submitAction()}
                onCancel={() => {
                    if (submitting) return;
                    setPendingAction(null);
                    setConfirmation("");
                }}
                destroyOnHidden
            >
                <div className="operations-confirm-content space-y-4">
                    <Alert className="operations-confirm-warning" type={pendingAction?.danger ? "warning" : "info"} showIcon message={pendingAction?.description} />
                    <section className="operations-confirm-impact" aria-labelledby="operations-confirm-impact-title">
                        <div className="operations-confirm-impact-heading">
                            <CircleAlert className="operations-confirm-impact-icon size-4" />
                            <strong className="operations-confirm-impact-title" id="operations-confirm-impact-title">
                                本次操作影响
                            </strong>
                        </div>
                        <ul className="operations-confirm-impact-list">
                            {pendingAction?.impacts.map((impact) => (
                                <li className="operations-confirm-impact-item" key={impact}>
                                    {impact}
                                </li>
                            ))}
                        </ul>
                    </section>
                    <div className="operations-confirm-field">
                        <label className="operations-confirm-label mb-1.5 block text-xs font-medium" htmlFor="operations-confirmation">
                            输入确认短语
                        </label>
                        <Input
                            className="operations-confirm-input"
                            id="operations-confirmation"
                            autoComplete="off"
                            placeholder={pendingAction?.expectedConfirmation}
                            value={confirmation}
                            aria-describedby="operations-confirmation-hint"
                            onChange={(event) => setConfirmation(event.target.value)}
                        />
                        <p className="operations-confirm-hint mt-1.5 text-xs text-foreground/45" id="operations-confirmation-hint">
                            必须完整输入：<code className="operations-confirm-code">{pendingAction?.expectedConfirmation}</code>
                        </p>
                    </div>
                </div>
            </Modal>
        </AdminPageFrame>
    );
}
