import { useEffect, useState } from "react";
import { Descriptions, Drawer, Tag } from "antd";

import { AdminContentError, AdminContentSkeleton, AdminTableEmpty } from "@/pages/admin/components/admin-ui";
import { getAdminApiLog, type ApiCallLog } from "@/services/api/auth";

export function ApiLogDetailDrawer({ logId, onClose }: { logId: string | null; onClose: () => void }) {
    const [log, setLog] = useState<ApiCallLog | null>(null);
    const [loading, setLoading] = useState(false);
    const [loadError, setLoadError] = useState<string | null>(null);
    const [retryToken, setRetryToken] = useState(0);
    useEffect(() => {
        if (!logId) return;
        let active = true;
        setLoading(true);
        setLog(null);
        setLoadError(null);
        void getAdminApiLog(logId)
            .then((result) => active && setLog(result.log))
            .catch((error) => {
                if (!active) return;
                const reason = error instanceof Error ? error.message : "读取请求详情失败";
                setLoadError(reason);
            })
            .finally(() => active && setLoading(false));
        return () => {
            active = false;
        };
    }, [logId, retryToken]);
    const items = log
        ? [
              ["时间", new Date(log.createdAt).toLocaleString("zh-CN", { hour12: false })],
              ["状态", <Tag color={log.status === "succeeded" ? "success" : "error"}>{log.status === "succeeded" ? "成功" : "失败"}</Tag>],
              ["用户 ID", log.userId],
              ["任务 ID", log.taskId || "--"],
              ["渠道", log.channelName || log.channelId || "--"],
              ["模型", log.model || "--"],
              ["请求阶段", log.requestKind || "--"],
              ["供应商任务 ID", log.providerRequestId || "--"],
              ["方法与路径", `${log.method} ${log.path}`],
              ["HTTP 状态", String(log.statusCode || "--")],
              ["耗时", `${log.durationMs} ms`],
              ["渠道并发上限", log.concurrencyLimit ? String(log.concurrencyLimit) : "--"],
              ["Token", log.usageAvailable ? `${log.inputTokens} 输入 / ${log.outputTokens} 输出 / ${log.cachedTokens} 缓存` : "未返回"],
              ["错误码", log.errorCode || "--"],
              ["错误详情", log.error || "--"],
              ["上游地址", log.upstreamUrl || "--"],
          ].map(([label, children], index) => ({ key: String(index), label, children }))
        : [];
    return (
        <Drawer className="admin-object-drawer admin-api-log-detail-drawer" title="请求详情" open={Boolean(logId)} onClose={onClose} size="min(760px, 100vw)" destroyOnHidden>
            {loading ? <AdminContentSkeleton compact rows={10} label="正在加载请求详情" /> : loadError ? <AdminContentError title="请求详情加载失败" description={loadError} onRetry={() => setRetryToken((value) => value + 1)} /> : log ? <Descriptions bordered size="small" column={1} items={items} /> : <AdminTableEmpty compact title="没有请求详情" description="请求记录可能已过期，或当前账号无权查看。" />}
        </Drawer>
    );
}
