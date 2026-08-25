import { Alert, Button } from "antd";
import { Activity, CircleStop, RotateCcw, TriangleAlert } from "lucide-react";

import { actionLabels, formatTime } from "@/pages/admin/operations/operations-presenters";
import type { OperationsRecord, OperationsRecoveryAction, OperationsServiceState } from "@/services/api/operations";

export type OperationActivePanelProps = {
    operation: OperationsRecord;
    submitting: boolean;
    onCancel: (operation: OperationsRecord) => Promise<void>;
    onRecover: (operation: OperationsRecord) => Promise<void>;
    onViewLogs: (operation: OperationsRecord) => void;
};

const serviceStateLabels: Record<OperationsServiceState, string> = {
    current_online: "当前版本在线",
    maintenance: "维护中",
    target_online: "目标版本在线",
    current_restored: "当前版本已恢复",
    unknown: "状态未知",
};

function isExecutableRecoveryAction(action: OperationsRecoveryAction | undefined): boolean {
    return action === "restore_current" || action === "restore_backup" || action === "commit_target" || action === "continue_controller_handoff";
}

export function OperationActivePanel({ operation, submitting, onCancel, onRecover, onViewLogs }: OperationActivePanelProps) {
    const cancellable = operation.status === "queued" || operation.status === "running";
    const cancelling = operation.status === "cancelling";
    const recoveryRequired = operation.status === "recovery_required";
    const recoveryExecutable = recoveryRequired && isExecutableRecoveryAction(operation.recoveryAction);

    return (
        <section className={`operations-active-panel is-${operation.status}`} role="status" aria-live="polite" aria-labelledby="operations-active-panel-title">
            <div className="operations-active-panel-heading">
                <span className="operations-active-panel-icon" aria-hidden="true">
                    {recoveryRequired ? <TriangleAlert className="operations-active-panel-icon-svg size-4" /> : <Activity className="operations-active-panel-icon-svg size-4" />}
                </span>
                <span className="operations-active-panel-copy">
                    <strong className="operations-active-panel-title" id="operations-active-panel-title">
                        {recoveryRequired ? (recoveryExecutable ? "任务等待安全恢复" : "任务需要人工核查") : cancelling ? "任务正在安全停止" : "当前任务正在执行"}
                    </strong>
                    <span className="operations-active-panel-description">
                        {actionLabels[operation.action]} · {operation.phase || operation.stage} · {operation.actorDisplayName}
                    </span>
                </span>
                <div className="operations-active-panel-actions">
                    <Button className="operations-active-panel-log-button" type="text" onClick={() => onViewLogs(operation)}>
                        查看实时日志
                    </Button>
                    {cancellable ? (
                        <Button className="operations-active-panel-stop-button" danger icon={<CircleStop className="operations-active-panel-button-icon size-4" />} loading={submitting} onClick={() => void onCancel(operation)}>
                            停止任务
                        </Button>
                    ) : null}
                    {recoveryExecutable ? (
                        <Button className="operations-active-panel-recover-button" type="primary" icon={<RotateCcw className="operations-active-panel-button-icon size-4" />} loading={submitting} onClick={() => void onRecover(operation)}>
                            恢复任务
                        </Button>
                    ) : null}
                </div>
            </div>

            <dl className="operations-active-panel-facts">
                <div className="operations-active-panel-fact">
                    <dt className="operations-active-panel-fact-label">执行阶段</dt>
                    <dd className="operations-active-panel-fact-value">{operation.phase || operation.stage}</dd>
                </div>
                <div className="operations-active-panel-fact">
                    <dt className="operations-active-panel-fact-label">服务状态</dt>
                    <dd className="operations-active-panel-fact-value">{serviceStateLabels[operation.serviceState]}</dd>
                </div>
                <div className="operations-active-panel-fact">
                    <dt className="operations-active-panel-fact-label">最近心跳</dt>
                    <dd className="operations-active-panel-fact-value">{operation.heartbeatAt ? formatTime(operation.heartbeatAt) : "尚无 Runner 心跳"}</dd>
                </div>
                <div className="operations-active-panel-fact">
                    <dt className="operations-active-panel-fact-label">检查点</dt>
                    <dd className="operations-active-panel-fact-value">#{operation.checkpointSequence ?? 0}</dd>
                </div>
            </dl>

            {cancelling ? <p className="operations-active-panel-safe-stop">已收到停止请求，正在到达安全点</p> : null}
            {operation.status === "recovering" ? <p className="operations-active-panel-safe-stop">已验证旧 Runner 停止，正在按持久检查点恢复</p> : null}
            {recoveryRequired ? (
                <Alert
                    className="operations-active-panel-recovery-alert"
                    type="warning"
                    showIcon
                    title={recoveryExecutable ? "控制器已持久化确定的恢复动作" : "控制器无法仅凭现有事实安全继续"}
                    description={
                        operation.error ||
                        (recoveryExecutable ? "输入精确确认短语后，系统会启动更高 generation 的唯一恢复 Runner。" : "请先核对执行日志、服务状态、恢复点和 Runner 所有权；当前不能自动执行恢复。")
                    }
                />
            ) : null}
            {operation.warnings?.map((warning) => (
                <Alert className="operations-active-panel-warning" key={`${warning.code}:${warning.message}`} type="warning" showIcon title={warning.message} />
            ))}
        </section>
    );
}
