import type {
    AdminAgentRun,
    AdminAgentRunActivity,
    AdminAgentRunStatus,
} from "@/services/api/admin-agent-runs";

const runStatusLabels: Record<AdminAgentRunStatus, string> = {
    queued: "排队中",
    running: "运行中",
    waiting_input: "等待用户回答",
    waiting_approval: "等待用户批准",
    waiting_tool: "等待工具",
    succeeded: "已完成",
    failed: "失败",
    cancelled: "已终止",
};

const activityLabels: Record<AdminAgentRunActivity, string> = {
    active: "活跃",
    awaiting_user: "等待用户",
    possibly_stalled: "可能卡住",
};

const taskStatusLabels: Record<string, string> = {
    none: "无关联任务",
    queued: "排队中",
    running: "运行中",
    succeeded: "已完成",
    failed: "失败",
    cancelled: "已取消",
};

const billingStatusLabels: Record<string, string> = {
    none: "无关联账务",
    reserved: "已预留",
    running: "计费中",
    settled: "已结算",
    refunded: "已退回",
    uncertain: "待核对",
};

const providerRequestLabels: Record<string, string> = {
    none: "无关联请求",
    not_submitted: "尚未提交",
    submitted: "已提交",
};

export type AgentRunControlPresentation = {
    title: string;
    description: string;
    canInterrupt: boolean;
    danger: boolean;
};

export type AgentRunFact = {
    label: string;
    value: string;
};

export function getAgentRunStatusLabel(status: AdminAgentRunStatus) {
    return runStatusLabels[status];
}

export function getAgentRunActivityLabel(activity: AdminAgentRunActivity) {
    return activityLabels[activity];
}

export function formatAgentRunInactiveDuration(seconds: number) {
    const safeSeconds = Number.isFinite(seconds) && seconds > 0 ? Math.floor(seconds) : 0;
    if (safeSeconds < 60) return `${safeSeconds} 秒未更新`;
    const minutes = Math.floor(safeSeconds / 60);
    const remainingSeconds = safeSeconds % 60;
    if (minutes < 60) return remainingSeconds ? `${minutes} 分 ${remainingSeconds} 秒未更新` : `${minutes} 分未更新`;
    const hours = Math.floor(minutes / 60);
    const remainingMinutes = minutes % 60;
    return remainingMinutes ? `${hours} 小时 ${remainingMinutes} 分未更新` : `${hours} 小时未更新`;
}

export function formatAgentRunTimestamp(value: string) {
    if (!value) return "未提供";
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) return "时间格式无效";
    const parts = new Intl.DateTimeFormat("zh-CN", {
        timeZone: "Asia/Shanghai",
        year: "numeric",
        month: "2-digit",
        day: "2-digit",
        hour: "2-digit",
        minute: "2-digit",
        second: "2-digit",
        hour12: false,
    }).formatToParts(date);
    const valueByType = Object.fromEntries(parts.map((part) => [part.type, part.value]));
    return `${valueByType.year}-${valueByType.month}-${valueByType.day} ${valueByType.hour}:${valueByType.minute}:${valueByType.second}`;
}

export function describeAgentRunControl(run: AdminAgentRun): AgentRunControlPresentation {
    switch (run.controlDisposition) {
        case "interruptible_now":
            return {
                title: "终止 Agent 运行",
                description: "Agent 将停止继续输出和调用工具；尚未提交给供应商的关联任务会取消并退回预留积分。",
                canInterrupt: true,
                danger: true,
            };
        case "cancel_request_required":
            if (run.providerRequestState !== "submitted") {
                return {
                    title: "终止 Agent 运行并取消关联任务",
                    description: "关联任务尚未提交给供应商。终止后会取消任务、停止 Agent 继续推进，并退回尚未消费的预留积分。",
                    canInterrupt: true,
                    danger: true,
                };
            }
            return {
                title: "终止运行并请求取消供应商任务",
                description: "供应商请求已经提交。终止后会停止 Agent 继续推进，同时请求取消关联任务；已提交的供应商任务可能继续执行，账务将进入核对。",
                canInterrupt: true,
                danger: true,
            };
        case "blocked_by_unresolved_billing":
            return {
                title: "暂不可终止",
                description: "存在无法与活动任务安全对应的未决账务，请先完成账务核对。",
                canInterrupt: false,
                danger: false,
            };
        case "already_terminal":
            return {
                title: "运行已结束",
                description: "该运行已经处于终态，无需再次终止。",
                canInterrupt: false,
                danger: false,
            };
    }
}

export function describeAgentRunFacts(run: AdminAgentRun): AgentRunFact[] {
    return [
        { label: "文本模型任务", value: taskStatusLabels[run.linkedModelTaskStatus] ?? `未知状态：${run.linkedModelTaskStatus}` },
        { label: "媒体任务", value: taskStatusLabels[run.linkedMediaTaskStatus] ?? `未知状态：${run.linkedMediaTaskStatus}` },
        { label: "账务", value: billingStatusLabels[run.billingState] ?? `未知状态：${run.billingState}` },
        { label: "供应商请求", value: providerRequestLabels[run.providerRequestState] ?? `未知状态：${run.providerRequestState}` },
        { label: "等待工具", value: run.pendingToolName || "无" },
    ];
}
