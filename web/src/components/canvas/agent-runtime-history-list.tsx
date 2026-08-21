import { Button } from "antd";

import type { AgentThreadHistoryItem } from "@/services/api/agent-runtime";
import { agentRuntimeStatusLabel } from "./use-agent-runtime";

type AgentRuntimeHistoryListProps = {
    items: AgentThreadHistoryItem[];
    selectedThreadId: string;
    loading: boolean;
    error: string;
    onSelect: (item: AgentThreadHistoryItem) => void;
    onRetry: () => void;
};

const activityFormatter = new Intl.DateTimeFormat("zh-CN", {
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
});

export function AgentRuntimeHistoryList({ items, selectedThreadId, loading, error, onSelect, onRetry }: AgentRuntimeHistoryListProps) {
    return (
        <div className="canvas-agent-runtime-history-list">
            <div className="canvas-agent-runtime-history-heading">
                <strong className="canvas-agent-runtime-history-title">历史对话</strong>
                <span className="canvas-agent-runtime-history-count">最近 {items.length} 条</span>
            </div>
            {error ? (
                <div className="canvas-agent-runtime-history-error" role="alert">
                    <span className="canvas-agent-runtime-history-error-copy">{error}</span>
                    <Button className="canvas-agent-runtime-history-retry" size="small" onClick={onRetry}>
                        重试历史
                    </Button>
                </div>
            ) : null}
            {loading ? <span className="canvas-agent-runtime-history-state">正在读取历史事实</span> : null}
            {!loading && !error && items.length === 0 ? <span className="canvas-agent-runtime-history-state">当前画布还没有历史对话</span> : null}
            <div className="canvas-agent-runtime-history-items">
                {items.map((item) => {
                    const title = item.latestRun?.state.userMessage ?? "尚未开始";
                    return (
                        <button key={item.thread.id} className="canvas-agent-runtime-history-item" type="button" aria-current={selectedThreadId === item.thread.id ? "true" : undefined} onClick={() => onSelect(item)}>
                            <span className="canvas-agent-runtime-history-item-main">
                                <span className="canvas-agent-runtime-history-item-title">{title}</span>
                                <span className="canvas-agent-runtime-history-item-status">{item.latestRun ? agentRuntimeStatusLabel(item.latestRun.state.status) : "尚未运行"}</span>
                            </span>
                            <time className="canvas-agent-runtime-history-item-time" dateTime={item.activityAt}>
                                {activityFormatter.format(new Date(item.activityAt))}
                            </time>
                        </button>
                    );
                })}
            </div>
        </div>
    );
}
