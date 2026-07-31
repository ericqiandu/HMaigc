import { useCallback, useEffect, useMemo, useState } from "react";
import { App, Button, DatePicker, Select, Table, Tabs, Tag, Tooltip } from "antd";
import type { ColumnsType } from "antd/es/table";
import dayjs, { type Dayjs } from "dayjs";
import { RefreshCw } from "lucide-react";
import { Area, CartesianGrid, ComposedChart, Legend, Line, ResponsiveContainer, Tooltip as ChartTooltip, XAxis, YAxis } from "recharts";
import { useSearchParams } from "react-router";

import { ListToolbar, TableSurface } from "@/components/layout/workspace-page";
import { formatCredits } from "@/constant/credits";
import { exportAdminAnalytics, getAdminAnalytics, listAdminUsers, type AdminReferenceData, type AdminAnalytics, type AnalyticsFilters } from "@/services/api/auth";
import { AdminExportButton } from "./admin-ui";

type Props = {
    users: AdminReferenceData["users"];
    channels: AdminReferenceData["channels"];
};

const capabilityOptions = [
    { label: "文本", value: "text" },
    { label: "图片", value: "image" },
    { label: "视频", value: "video" },
    { label: "音频", value: "audio" },
];

export default function AnalyticsPanel({ users, channels }: Props) {
    const { message } = App.useApp();
    const [searchParams, setSearchParams] = useSearchParams();
    const [range, setRange] = useState<[Dayjs, Dayjs]>(() => [filterDate(searchParams.get("from"), dayjs().subtract(29, "day")), filterDate(searchParams.get("to"), dayjs())]);
    const [userId, setUserId] = useState(searchParams.get("userId") || undefined);
    const [model, setModel] = useState(searchParams.get("model") || undefined);
    const [channelId, setChannelId] = useState(searchParams.get("channelId") || undefined);
    const [capability, setCapability] = useState(searchParams.get("capability") || undefined);
    const [data, setData] = useState<AdminAnalytics | null>(null);
    const [loading, setLoading] = useState(false);
    const [userOptions, setUserOptions] = useState(users);
    const [searchingUsers, setSearchingUsers] = useState(false);

    const filters = useMemo<AnalyticsFilters>(
        () => ({
            from: range[0].format("YYYY-MM-DD"),
            to: range[1].format("YYYY-MM-DD"),
            userId,
            model,
            channelId,
            capability,
        }),
        [capability, channelId, model, range, userId],
    );

    const reload = useCallback(async () => {
        setLoading(true);
        try {
            setData(await getAdminAnalytics(filters));
        } catch (error) {
            message.error(error instanceof Error ? error.message : "读取统计数据失败");
        } finally {
            setLoading(false);
        }
    }, [filters, message]);

    useEffect(() => {
        const next = new URLSearchParams(searchParams);
        for (const [key, value] of Object.entries(filters)) {
            if (value) next.set(key, value);
            else next.delete(key);
        }
        setSearchParams(next, { replace: true });
        void reload();
    }, [filters]);

    useEffect(() => {
        setUserOptions(users);
    }, [users]);

    const searchUsers = async (keyword: string) => {
        setSearchingUsers(true);
        try {
            const result = await listAdminUsers({ keyword: keyword.trim() || undefined, page: 1, limit: 50 });
            setUserOptions(result.users);
        } catch (error) {
            message.error(error instanceof Error ? error.message : "搜索用户失败");
        } finally {
            setSearchingUsers(false);
        }
    };

    const modelOptions = useMemo(() => {
        const names = new Set<string>();
        channels.forEach((channel) => channel.models?.forEach((name) => names.add(name)));
        data?.models.forEach((item) => item.model !== "未识别" && names.add(item.model));
        return [...names].sort().map((name) => ({ label: name, value: name }));
    }, [channels, data?.models]);

    const modelColumns: ColumnsType<AdminAnalytics["models"][number]> = [
        {
            title: "模型",
            dataIndex: "model",
            fixed: "left",
            width: 210,
            render: (value, row) => (
                <div>
                    <div className="font-medium">{value}</div>
                    <div className="mt-1">
                        <Tag variant="filled">{capabilityLabel(row.capability)}</Tag>
                    </div>
                </div>
            ),
        },
        { title: "任务 / 请求", width: 120, render: (_, row) => `${row.tasks} / ${row.requests}` },
        { title: "用户", dataIndex: "uniqueUsers", width: 80 },
        { title: "任务成功率", dataIndex: "taskSuccessRate", width: 110, render: percent },
        { title: "请求成功率", dataIndex: "requestSuccessRate", width: 110, render: percent },
        { title: "P50 / P95", width: 145, render: (_, row) => `${formatDuration(row.p50DurationMs)} / ${formatDuration(row.p95DurationMs)}` },
        { title: "Token（入 / 出 / 缓存）", width: 190, render: (_, row) => (row.usageAvailable ? `${formatNumber(row.inputTokens)} / ${formatNumber(row.outputTokens)} / ${formatNumber(row.cachedTokens)}` : "--") },
        { title: "媒体 / 视频秒", width: 125, render: (_, row) => `${row.mediaCount} / ${row.videoSeconds}` },
        { title: "估算费用", width: 120, render: (_, row) => formatCost(row.estimatedCostMicros, row.currency, row.costAvailable) },
    ];

    const userColumns: ColumnsType<AdminAnalytics["users"][number]> = [
        {
            title: "用户",
            dataIndex: "name",
            width: 180,
            render: (name, row) => (
                <div>
                    <div className="font-medium">{name}</div>
                    <div className="text-xs text-foreground/45">{row.userId}</div>
                </div>
            ),
        },
        { title: "活跃天数", dataIndex: "activeDays", width: 95 },
        { title: "任务", dataIndex: "tasks", width: 80 },
        { title: "Agent 消息", dataIndex: "agentMessages", width: 105 },
        { title: "画布活跃天数", dataIndex: "canvasDays", width: 120 },
        { title: "素材 / 资源", width: 110, render: (_, row) => `${row.assets} / ${row.resources}` },
        { title: "常用模型", dataIndex: "commonModel", ellipsis: true, render: (value) => value || "--" },
    ];

    const failureColumns: ColumnsType<AdminAnalytics["failures"][number]> = [
        { title: "错误类型", dataIndex: "type", width: 120, render: (value) => <Tag color={value === "超时" ? "orange" : "red"}>{value}</Tag> },
        { title: "模型", dataIndex: "model", width: 220 },
        { title: "次数", dataIndex: "count", width: 90 },
        { title: "最近错误", dataIndex: "lastError", ellipsis: true, render: (value) => <Tooltip title={value}>{value || "--"}</Tooltip> },
        { title: "最近发生", dataIndex: "lastSeenAt", width: 170, render: (value) => dayjs(value).format("YYYY-MM-DD HH:mm") },
    ];

    return (
        <div className="admin-analytics-layout">
            <div className="admin-analytics-toolbar">
                <ListToolbar
                    trailing={
                        <>
                            <Button className="admin-analytics-refresh-button" icon={<RefreshCw className="admin-analytics-refresh-icon size-4" />} loading={loading} onClick={() => void reload()}>
                                刷新
                            </Button>
                            <AdminExportButton exportFile={() => exportAdminAnalytics(filters)} fileName={() => `usage-${filters.from}-${filters.to}.csv`} label="导出 CSV" />
                        </>
                    }
                >
                    <label className="admin-analytics-filter admin-analytics-date-filter">
                        <span className="admin-analytics-filter-label">时间范围</span>
                        <DatePicker.RangePicker className="admin-analytics-range-picker" allowClear={false} value={range} onChange={(value) => value?.[0] && value?.[1] && setRange([value[0], value[1]])} />
                    </label>
                    <FilterSelect
                        label="用户"
                        value={userId}
                        onChange={setUserId}
                        options={userOptions.map((user) => ({ label: user.displayName || user.username, value: user.id }))}
                        filterOption={false}
                        loading={searchingUsers}
                        onSearch={(value) => void searchUsers(value)}
                    />
                    <FilterSelect label="模型" value={model} onChange={setModel} options={modelOptions} wide />
                    <FilterSelect label="渠道" value={channelId} onChange={setChannelId} options={channels.map((channel) => ({ label: channel.name, value: channel.id }))} />
                    <FilterSelect label="能力" value={capability} onChange={setCapability} options={capabilityOptions} />
                </ListToolbar>
            </div>

            <section className="admin-analytics-metric-section" aria-labelledby="admin-analytics-efficiency-title">
                <div className="admin-analytics-section-heading">
                    <h2 id="admin-analytics-efficiency-title" className="admin-analytics-section-title">
                        运行效率
                    </h2>
                    <p className="admin-analytics-section-description">关注用户活跃、任务规模、请求稳定性与实时队列。</p>
                </div>
                <div className="admin-analytics-metrics">
                    <Metric label="活跃用户" value={data?.kpi.activeUsers ?? "--"} detail={data ? `DAU ${data.kpi.dau} · WAU ${data.kpi.wau} · MAU ${data.kpi.mau}` : undefined} />
                    <Metric label="生成任务" value={data?.kpi.generationTasks ?? "--"} detail={data ? `上游请求 ${data.kpi.upstreamRequests}` : undefined} />
                    <Metric label="请求成功率" value={data ? percent(data.kpi.successRate) : "--"} />
                    <Metric label="P95 耗时" value={data ? formatDuration(data.kpi.p95DurationMs) : "--"} />
                    <Metric label="当前队列" value={data?.kpi.currentQueuedTasks ?? "--"} detail="排队 + 运行中" />
                </div>
            </section>

            <section className="admin-analytics-metric-section" aria-labelledby="admin-analytics-business-title">
                <div className="admin-analytics-section-heading">
                    <h2 id="admin-analytics-business-title" className="admin-analytics-section-title">
                        商业指标
                    </h2>
                    <p className="admin-analytics-section-description">成本以供应商货币记录，积分营收按已结算计费订单统计。</p>
                </div>
                <div className="admin-analytics-metrics">
                    <Metric label="上游估算成本" value={data ? formatCost(data.kpi.estimatedCostMicros, data.kpi.currency, data.kpi.costAvailable) : "--"} detail="供应商货币成本，不与积分混算" />
                    <Metric label="已结算积分营收" value={data ? formatCredits(data.kpi.settledRevenueMicrocredits) : "--"} detail={data ? `${data.kpi.settledBillingOrders} 笔已结算订单` : undefined} />
                    <Metric label="基础积分成本" value={data ? formatCredits(data.kpi.settledBaseCostMicrocredits) : "--"} detail="按订单计费快照统计" />
                    <Metric label="积分毛利" value={data ? formatCredits(data.kpi.grossProfitMicrocredits) : "--"} detail="积分营收 − 基础积分成本" />
                    <Metric
                        label="冻结积分"
                        value={data ? formatCredits(data.kpi.pendingAmountMicrocredits + data.kpi.reviewAmountMicrocredits) : "--"}
                        detail={data ? `处理中 ${data.kpi.pendingBillingOrders} 笔 · 待复核 ${formatCredits(data.kpi.reviewAmountMicrocredits)} / ${data.kpi.reviewBillingOrders} 笔` : undefined}
                    />
                </div>
            </section>

            <section className="admin-analytics-trend">
                <div className="admin-analytics-trend-heading">
                    <h2 className="admin-analytics-trend-title">使用趋势</h2>
                    <p className="admin-analytics-trend-description">生成任务与真实上游请求分开统计，成功率按上游请求计算。</p>
                </div>
                <div className="admin-analytics-chart">
                    <ResponsiveContainer width="100%" height="100%">
                        <ComposedChart data={data?.trend || []} margin={{ top: 8, right: 12, bottom: 0, left: -16 }}>
                            <CartesianGrid stroke="currentColor" className="text-foreground/10" vertical={false} />
                            <XAxis dataKey="day" tickFormatter={(value) => value.slice(5)} tick={{ fontSize: 11 }} />
                            <YAxis yAxisId="count" allowDecimals={false} tick={{ fontSize: 11 }} />
                            <YAxis yAxisId="rate" orientation="right" domain={[0, 100]} tickFormatter={(value) => `${value}%`} tick={{ fontSize: 11 }} />
                            <ChartTooltip labelFormatter={(value) => `日期 ${value}`} />
                            <Legend wrapperStyle={{ fontSize: 12 }} />
                            <Area yAxisId="count" type="monotone" dataKey="tasks" name="生成任务" stroke="#2563eb" fill="#2563eb" fillOpacity={0.1} />
                            <Area yAxisId="count" type="monotone" dataKey="requests" name="上游请求" stroke="#0f766e" fill="#0f766e" fillOpacity={0.08} />
                            <Line yAxisId="rate" type="monotone" dataKey="requestSuccessRate" name="成功率" stroke="#d97706" dot={false} strokeWidth={2} />
                        </ComposedChart>
                    </ResponsiveContainer>
                </div>
            </section>

            <Tabs
                className="admin-analytics-tabs"
                items={[
                    {
                        key: "models",
                        label: "模型分析",
                        children: (
                            <TableSurface className="admin-analytics-table-surface mt-0">
                                <Table
                                    className="admin-analytics-table"
                                    rowKey={(row) => `${row.model}:${row.capability}`}
                                    size="small"
                                    loading={loading}
                                    columns={modelColumns}
                                    dataSource={data?.models || []}
                                    pagination={{ pageSize: 10 }}
                                    scroll={{ x: 1250 }}
                                />
                            </TableSurface>
                        ),
                    },
                    {
                        key: "users",
                        label: "用户活动",
                        children: (
                            <TableSurface className="admin-analytics-table-surface mt-0">
                                <Table className="admin-analytics-table" rowKey="userId" size="small" loading={loading} columns={userColumns} dataSource={data?.users || []} pagination={{ pageSize: 10 }} scroll={{ x: 900 }} />
                            </TableSurface>
                        ),
                    },
                    {
                        key: "failures",
                        label: `异常定位${data?.failures.length ? ` (${data.failures.reduce((sum, item) => sum + item.count, 0)})` : ""}`,
                        children: (
                            <TableSurface className="admin-analytics-table-surface mt-0">
                                <Table
                                    className="admin-analytics-table"
                                    rowKey={(row) => `${row.type}:${row.model}`}
                                    size="small"
                                    loading={loading}
                                    columns={failureColumns}
                                    dataSource={data?.failures || []}
                                    pagination={{ pageSize: 10 }}
                                    scroll={{ x: 900 }}
                                />
                            </TableSurface>
                        ),
                    },
                ]}
            />
        </div>
    );
}

function FilterSelect({
    label,
    value,
    onChange,
    options,
    wide = false,
    filterOption = true,
    loading,
    onSearch,
}: {
    label: string;
    value?: string;
    onChange: (value?: string) => void;
    options: Array<{ label: string; value: string }>;
    wide?: boolean;
    filterOption?: boolean;
    loading?: boolean;
    onSearch?: (value: string) => void;
}) {
    return (
        <label className={wide ? "admin-analytics-filter is-wide" : "admin-analytics-filter"}>
            <span className="admin-analytics-filter-label">{label}</span>
            <Select
                className="admin-analytics-filter-control"
                aria-label={label}
                allowClear
                showSearch
                optionFilterProp="label"
                filterOption={filterOption}
                loading={loading}
                placeholder="全部"
                value={value}
                onChange={onChange}
                onSearch={onSearch}
                options={options}
            />
        </label>
    );
}

function Metric({ label, value, detail }: { label: string; value: string | number; detail?: string }) {
    return (
        <article className="admin-analytics-metric">
            <div className="admin-analytics-metric-label">{label}</div>
            <div className="admin-analytics-metric-value">{value}</div>
            {detail ? <div className="admin-analytics-metric-detail">{detail}</div> : null}
        </article>
    );
}

function capabilityLabel(value: string) {
    return capabilityOptions.find((item) => item.value === value)?.label || "未分类";
}

function percent(value: number) {
    return `${Number(value || 0).toFixed(1)}%`;
}

function formatDuration(value: number) {
    if (!value) return "--";
    return value >= 1000 ? `${(value / 1000).toFixed(1)}s` : `${value}ms`;
}

function formatNumber(value: number) {
    return new Intl.NumberFormat("zh-CN", { notation: value >= 100000 ? "compact" : "standard", maximumFractionDigits: 1 }).format(value);
}

function formatCost(micros: number, currency?: string, available?: boolean) {
    return available ? formatMoney(fromMicros(micros), currency || "USD") : "--";
}

function formatMoney(value: number, currency = "USD") {
    if (currency === "MIXED") return `${value.toFixed(6)}（混合币种）`;
    try {
        return new Intl.NumberFormat("zh-CN", { style: "currency", currency, minimumFractionDigits: 2, maximumFractionDigits: 6 }).format(value);
    } catch {
        return `${currency} ${value.toFixed(6)}`;
    }
}

function fromMicros(value: number) {
    return value / 1_000_000;
}

function filterDate(value: string | null, fallback: Dayjs) {
    if (!value) return fallback;
    const parsed = dayjs(value);
    return parsed.isValid() ? parsed : fallback;
}
