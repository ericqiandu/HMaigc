import { useCallback, useEffect, useRef, useState } from "react";
import { App, Button, Form, Input, InputNumber, Modal, Popconfirm, Select, Table, Tag } from "antd";
import type { ColumnsType } from "antd/es/table";
import { Ban, Copy, Eye, RefreshCw, Search, TicketCheck } from "lucide-react";

import { ListToolbar, TableSurface } from "@/components/layout/workspace-page";
import { formatCredits } from "@/constant/credits";
import { useDebouncedValue } from "@/hooks/use-debounced-value";
import { createAdminRedeemBatch, disableAdminRedeemBatch, disableAdminRedeemCode, listAdminRedeemBatchCodes, listAdminRedeemBatches, type AdminRedeemCode, type RedeemBatch } from "@/services/api/wallet";
import { AdminContentSection, AdminDataLayout } from "./admin-data-layout";
import { formatCompactNumberInput } from "./admin-form-system";
import { AdminContentError, AdminExportButton, AdminTableEmpty, AdminTableSkeleton } from "./admin-ui";
import { redeemBatchDisableDescription, redeemBatchRequest, redeemCodeDisableDescription, type RedeemFormValues } from "./redemption-code-domain";

export default function RedemptionCodesPanel() {
    const { message } = App.useApp();
    const [batches, setBatches] = useState<RedeemBatch[]>([]);
    const [generatedCodes, setGeneratedCodes] = useState<string[]>([]);
    const [selectedBatch, setSelectedBatch] = useState<RedeemBatch | null>(null);
    const [loading, setLoading] = useState(true);
    const [listError, setListError] = useState("");
    const [creating, setCreating] = useState(false);
    const [disablingBatchId, setDisablingBatchId] = useState("");
    const [keyword, setKeyword] = useState("");
    const debouncedKeyword = useDebouncedValue(keyword);
    const [validity, setValidity] = useState<"all" | "active" | "expired">("all");
    const [page, setPage] = useState(1);
    const [pageSize, setPageSize] = useState(20);
    const [total, setTotal] = useState(0);
    const [form] = Form.useForm<RedeemFormValues>();
    const listRequestId = useRef(0);

    const reload = async (targetPage = page, targetPageSize = pageSize) => {
        const requestId = ++listRequestId.current;
        setLoading(true);
        try {
            const result = await listAdminRedeemBatches({ keyword: debouncedKeyword || undefined, validity: validity === "all" ? undefined : validity, page: targetPage, limit: targetPageSize });
            if (requestId !== listRequestId.current) return;
            setBatches(result.batches);
            setTotal(result.total);
            setListError("");
        } catch (error) {
            if (requestId !== listRequestId.current) return;
            setListError(error instanceof Error ? error.message : "读取兑换码批次失败");
        } finally {
            if (requestId === listRequestId.current) setLoading(false);
        }
    };

    useEffect(() => {
        form.setFieldsValue({ amount: 10, count: 10 });
    }, [form]);

    useEffect(() => {
        void reload(page, pageSize);
    }, [debouncedKeyword, validity, page, pageSize]);

    const createBatch = async () => {
        let values: RedeemFormValues;
        try {
            values = await form.validateFields();
        } catch {
            return;
        }
        setCreating(true);
        try {
            const result = await createAdminRedeemBatch(redeemBatchRequest(values));
            setGeneratedCodes(result.codes);
            setPage(1);
            form.resetFields(["note", "expiresAt"]);
            await reload(1, pageSize);
            message.success(`已生成 ${result.codes.length} 个兑换码`);
        } catch (error) {
            const detail = error instanceof Error ? error.message : "生成兑换码失败";
            message.error(detail.toLowerCase().includes("timeout") ? "生成超过 30 秒，后台可能仍已完成；请刷新批次列表确认，兑换码可从批次明细重新查看。" : detail);
        } finally {
            setCreating(false);
        }
    };

    const columns: ColumnsType<RedeemBatch> = [
        { title: "创建时间", dataIndex: "createdAt", width: 150, render: formatTime },
        { title: "单码积分", dataIndex: "amountMicrocredits", width: 110, align: "right", render: (value) => <span className="font-medium tabular-nums">{formatCredits(value)}</span> },
        { title: "数量", dataIndex: "count", width: 80, align: "right", render: (value) => <span className="tabular-nums">{value}</span> },
        {
            title: "核销状态",
            width: 150,
            render: (_, batch) => (
                <div className="flex items-center gap-2">
                    <span className="font-medium tabular-nums">
                        {batch.redeemedCount ?? 0}/{batch.count}
                    </span>
                    <span className="text-xs text-foreground/45">已核销</span>
                    {(batch.expiredCount ?? 0) > 0 ? (
                        <Tag variant="filled" color="default">
                            {batch.expiredCount} 已过期
                        </Tag>
                    ) : null}
                </div>
            ),
        },
        { title: "有效期", dataIndex: "expiresAt", width: 150, render: (value) => (value ? formatTime(value) : <Tag variant="filled">永久有效</Tag>) },
        { title: "批次备注", dataIndex: "note", width: 140, ellipsis: true, render: (value) => value || <span className="text-foreground/35">未填写</span> },
        {
            title: "操作",
            width: 180,
            fixed: "right",
            render: (_, batch) => (
                <div className="admin-redemption-row-actions">
                    <Button size="small" icon={<Eye className="size-3.5" />} onClick={() => setSelectedBatch(batch)}>
                        查看明细
                    </Button>
                    <Popconfirm
                        rootClassName="admin-operation-popconfirm workspace-ui-scope"
                        title="禁用该批次的可用兑换码？"
                        description={redeemBatchDisableDescription(batch)}
                        okText="禁用"
                        cancelText="取消"
                        okButtonProps={{ danger: true }}
                        onConfirm={async () => {
                            setDisablingBatchId(batch.id);
                            try {
                                const result = await disableAdminRedeemBatch(batch.id);
                                message.success(`已禁用 ${result.disabledCount} 个兑换码`);
                                await reload();
                            } catch (error) {
                                message.error(error instanceof Error ? error.message : "禁用批次失败");
                            } finally {
                                setDisablingBatchId("");
                            }
                        }}
                    >
                        <Button size="small" danger icon={<Ban className="size-3.5" />} loading={disablingBatchId === batch.id} disabled={(batch.availableCount ?? 0) <= 0 || Boolean(disablingBatchId)}>
                            禁用
                        </Button>
                    </Popconfirm>
                </div>
            ),
        },
    ];

    return (
        <AdminDataLayout>
            <AdminContentSection className="admin-redemption-generator" title="生成兑换码批次" description="兑换码为 32 位随机字符串，生成后加密保存，可在批次明细中再次查看。">
                <Form form={form} layout="vertical" requiredMark={false} disabled={creating} className="admin-redemption-form grid md:grid-cols-12">
                    <Form.Item
                        name="amount"
                        label="每个兑换码的积分"
                        rules={[
                            { required: true, message: "请填写积分面额" },
                            { type: "number", min: 0.000001, message: "积分面额必须大于 0" },
                        ]}
                        className="admin-redemption-amount-field md:col-span-4"
                    >
                        <InputNumber style={{ width: "100%" }} min={0.000001} precision={6} step={0.000001} formatter={formatCompactNumberInput} />
                    </Form.Item>
                    <Form.Item
                        name="count"
                        label="生成数量"
                        rules={[
                            { required: true, message: "请填写生成数量" },
                            { type: "number", min: 1, max: 5000, message: "单批生成数量必须为 1–5,000" },
                        ]}
                        className="admin-redemption-count-field md:col-span-4"
                    >
                        <InputNumber style={{ width: "100%" }} min={1} max={5000} precision={0} />
                    </Form.Item>
                    <Form.Item name="expiresAt" label="过期时间" className="admin-redemption-expiry-field md:col-span-4">
                        <Input type="datetime-local" />
                    </Form.Item>
                    <Form.Item name="note" label="批次备注" className="admin-redemption-note-field md:col-span-12">
                        <Input maxLength={500} placeholder="例如：7 月活动赠送" />
                    </Form.Item>
                    <div className="admin-redemption-actions flex items-center justify-between gap-5 md:col-span-12">
                        <span className="admin-redemption-actions-note">单批最多生成 5,000 个。生成成功后会立即显示结果，请及时下载留存。</span>
                        <Button type="primary" loading={creating} icon={<TicketCheck className="size-4" />} onClick={() => void createBatch()}>
                            生成兑换码
                        </Button>
                    </div>
                </Form>
            </AdminContentSection>

            <AdminContentSection
                className="admin-redemption-records"
                title="批次记录"
                description="查看每个兑换码的当前状态、核销用户、时间和来源 IP。"
                actions={
                    <div className="admin-redemption-records-meta">
                        <span className="admin-redemption-records-count">共 {total} 个批次</span>
                        <Button className="admin-redemption-refresh" icon={<RefreshCw className="size-4" />} loading={loading} onClick={() => void reload()}>
                            刷新
                        </Button>
                    </div>
                }
            >
                <ListToolbar
                    active={Boolean(keyword || validity !== "all")}
                    onReset={() => {
                        setKeyword("");
                        setValidity("all");
                        setPage(1);
                    }}
                >
                    <Input
                        allowClear
                        className="app-list-search admin-redemption-search"
                        prefix={<Search className="size-4 text-foreground/40" />}
                        value={keyword}
                        placeholder="搜索批次备注、积分或数量"
                        onChange={(event) => {
                            setKeyword(event.target.value);
                            setPage(1);
                        }}
                    />
                    <Select
                        className="admin-redemption-validity-filter"
                        aria-label="筛选兑换码批次有效期"
                        value={validity}
                        onChange={(value) => {
                            setValidity(value);
                            setPage(1);
                        }}
                        options={[
                            { label: "全部有效期", value: "all" },
                            { label: "有效", value: "active" },
                            { label: "已过期", value: "expired" },
                        ]}
                    />
                </ListToolbar>
                {listError ? <AdminContentError title={batches.length ? "兑换码批次刷新失败" : "兑换码批次读取失败"} description={batches.length ? `${listError}；当前继续展示上次成功读取的批次。` : listError} onRetry={() => void reload()} /> : null}
                {!listError || batches.length ? (
                    <TableSurface>
                        {loading && !batches.length ? <AdminTableSkeleton rows={8} columns={7} /> : null}
                        {!loading && !listError && !batches.length ? (
                            <AdminTableEmpty
                                filtered={Boolean(keyword || validity !== "all")}
                                title={keyword || validity !== "all" ? undefined : "暂无兑换码批次"}
                                description={keyword || validity !== "all" ? undefined : "创建首个兑换码批次后，批次状态与核销记录会显示在这里。"}
                            />
                        ) : null}
                        {batches.length ? (
                            <Table
                                className="app-data-table"
                                rowKey="id"
                                size="middle"
                                loading={loading}
                                columns={columns}
                                dataSource={batches}
                                pagination={{
                                    current: page,
                                    pageSize,
                                    total,
                                    showSizeChanger: true,
                                    pageSizeOptions: [20, 50, 100],
                                    showTotal: (value, range) => `${range[0]}-${range[1]} / 共 ${value} 个批次`,
                                    onChange: (nextPage, nextPageSize) => {
                                        setPage(nextPageSize !== pageSize ? 1 : nextPage);
                                        setPageSize(nextPageSize);
                                    },
                                }}
                                scroll={{ x: 960 }}
                            />
                        ) : null}
                    </TableSurface>
                ) : null}
            </AdminContentSection>

            <GeneratedCodesModal codes={generatedCodes} onClose={() => setGeneratedCodes([])} />
            <RedeemBatchCodesModal key={selectedBatch?.id || "closed"} batch={selectedBatch} onClose={() => setSelectedBatch(null)} />
        </AdminDataLayout>
    );
}

function GeneratedCodesModal({ codes, onClose }: { codes: string[]; onClose: () => void }) {
    const { message } = App.useApp();
    const content = codes.join("\n");
    const copy = async () => {
        try {
            await navigator.clipboard.writeText(content);
            message.success("兑换码已复制");
        } catch (error) {
            message.error(error instanceof Error ? `复制失败：${error.message}` : "复制失败，请下载 TXT 文件");
        }
    };
    return (
        <Modal
            className="admin-operation-modal admin-generated-codes-modal workspace-ui-scope"
            title={`已生成 ${codes.length} 个兑换码`}
            open={codes.length > 0}
            onCancel={onClose}
            footer={
                <div className="admin-generated-codes-actions">
                    <Button icon={<Copy className="size-4" />} onClick={() => void copy()}>
                        复制全部
                    </Button>
                    <AdminExportButton type="primary" exportFile={() => new Blob([content + "\n"], { type: "text/plain;charset=utf-8" })} fileName={() => `兑换码-${new Date().toISOString().slice(0, 10)}.txt`} label="下载 TXT" />
                </div>
            }
            width={680}
        >
            <div className="admin-generated-codes-notice">兑换码已加密保存，可在批次明细中再次查看；仍建议立即下载一份用于发放。</div>
            <Input.TextArea value={content} readOnly autoSize={{ minRows: 10, maxRows: 18 }} className="admin-generated-codes-content font-mono text-xs" />
        </Modal>
    );
}

function RedeemBatchCodesModal({ batch, onClose }: { batch: RedeemBatch | null; onClose: () => void }) {
    const { message } = App.useApp();
    const [batchSummary, setBatchSummary] = useState<RedeemBatch | null>(batch);
    const [codes, setCodes] = useState<AdminRedeemCode[]>([]);
    const [loading, setLoading] = useState(false);
    const [loadError, setLoadError] = useState("");
    const [disablingCodeId, setDisablingCodeId] = useState("");
    const [plaintextAvailable, setPlaintextAvailable] = useState(true);
    const [status, setStatus] = useState("all");
    const [page, setPage] = useState(1);
    const [pageSize, setPageSize] = useState(50);
    const [total, setTotal] = useState(0);
    const detailRequestId = useRef(0);

    const loadCodes = useCallback(async () => {
        if (!batch) return;
        const requestId = ++detailRequestId.current;
        setLoading(true);
        try {
            const result = await listAdminRedeemBatchCodes(batch.id, { status: status === "all" ? undefined : status, page, limit: pageSize });
            if (requestId !== detailRequestId.current) return;
            setCodes(result.codes);
            setTotal(result.total);
            setPlaintextAvailable(result.plaintextAvailable);
            setBatchSummary(result.batch);
            setLoadError("");
        } catch (error) {
            if (requestId !== detailRequestId.current) return;
            setLoadError(error instanceof Error ? error.message : "读取兑换码明细失败");
        } finally {
            if (requestId === detailRequestId.current) setLoading(false);
        }
    }, [batch, page, pageSize, status]);

    useEffect(() => {
        void loadCodes();
    }, [loadCodes]);

    const copyCode = async (code?: string) => {
        if (!code) return;
        try {
            await navigator.clipboard.writeText(code);
            message.success("兑换码已复制");
        } catch (error) {
            message.error(error instanceof Error ? `复制失败：${error.message}` : "复制兑换码失败");
        }
    };
    const copyPage = async () => {
        const content = codes
            .map((item) => item.code)
            .filter(Boolean)
            .join("\n");
        if (!content) return;
        try {
            await navigator.clipboard.writeText(content);
            message.success("本页兑换码已复制");
        } catch (error) {
            message.error(error instanceof Error ? `复制失败：${error.message}` : "复制本页兑换码失败");
        }
    };
    const disableCode = async (item: AdminRedeemCode) => {
        if (!batch) return;
        setDisablingCodeId(item.id);
        try {
            await disableAdminRedeemCode(batch.id, item.id);
            setCodes((current) => current.map((code) => (code.id === item.id ? { ...code, status: "disabled" } : code)));
            setBatchSummary((current) => (current ? { ...current, availableCount: Math.max(0, current.availableCount - 1), disabledCount: current.disabledCount + 1 } : current));
            message.success("兑换码已禁用");
        } catch (error) {
            message.error(error instanceof Error ? error.message : "禁用兑换码失败");
        } finally {
            setDisablingCodeId("");
        }
    };
    const columns: ColumnsType<AdminRedeemCode> = [
        {
            title: "兑换码",
            width: 330,
            render: (_, item) => (
                <div className="flex items-center gap-2">
                    <code className="min-w-0 flex-1 truncate text-xs">{item.code || `明文不可恢复 ····${item.codeSuffix}`}</code>
                    <Button type="text" size="small" aria-label="复制兑换码" icon={<Copy className="size-3.5" />} disabled={!item.code} onClick={() => void copyCode(item.code)} />
                </div>
            ),
        },
        { title: "状态", dataIndex: "status", width: 110, render: renderCodeStatus },
        {
            title: "核销用户",
            width: 190,
            render: (_, item) =>
                item.redeemedBy ? (
                    <div>
                        <div className="text-sm">{item.redeemedDisplayName || item.redeemedUsername || item.redeemedBy}</div>
                        <div className="truncate text-xs text-foreground/40">{item.redeemedUsername ? `@${item.redeemedUsername}` : item.redeemedBy}</div>
                    </div>
                ) : (
                    <span className="text-foreground/35">--</span>
                ),
        },
        { title: "核销时间", dataIndex: "redeemedAt", width: 180, render: formatTime },
        { title: "核销 IP", dataIndex: "redeemedIp", width: 150, render: (value) => value || <span className="text-foreground/35">--</span> },
        {
            title: "操作",
            width: 90,
            fixed: "right",
            render: (_, item) =>
                item.status === "unused" ? (
                    <Popconfirm
                        rootClassName="admin-operation-popconfirm workspace-ui-scope"
                        title="禁用这个兑换码？"
                        description={redeemCodeDisableDescription(item)}
                        okText="禁用"
                        cancelText="取消"
                        okButtonProps={{ danger: true }}
                        onConfirm={() => void disableCode(item)}
                    >
                        <Button type="text" size="small" danger loading={disablingCodeId === item.id} disabled={Boolean(disablingCodeId)} icon={<Ban className="size-3.5" />} aria-label={`禁用尾号 ${item.codeSuffix} 的兑换码`} />
                    </Popconfirm>
                ) : (
                    <span className="text-xs text-foreground/35">--</span>
                ),
        },
    ];

    return (
        <Modal
            className="admin-operation-modal admin-redeem-codes-modal workspace-ui-scope"
            title={batchSummary ? `兑换码明细 · ${batchSummary.note || formatTime(batchSummary.createdAt)}` : "兑换码明细"}
            open={Boolean(batch)}
            onCancel={onClose}
            maskClosable={!disablingCodeId}
            keyboard={!disablingCodeId}
            closable={!disablingCodeId}
            footer={
                <div className="admin-redeem-codes-actions">
                    <Button icon={<Copy className="size-4" />} disabled={!codes.some((item) => item.code)} onClick={() => void copyPage()}>
                        复制本页
                    </Button>
                    <Button type="primary" disabled={Boolean(disablingCodeId)} onClick={onClose}>
                        关闭
                    </Button>
                </div>
            }
            width={1080}
        >
            {!plaintextAvailable ? <div className="admin-redeem-codes-notice">该批次创建于加密回看功能上线前，系统当时只保存了哈希，无法恢复完整明文；核销状态和审计信息仍可查看。</div> : null}
            <div className="admin-redeem-codes-toolbar flex flex-wrap items-center justify-between gap-3">
                <div className="admin-redeem-codes-summary flex flex-wrap gap-2">
                    <Tag variant="filled" color="green">
                        可用 {batchSummary?.availableCount ?? 0}
                    </Tag>
                    <Tag variant="filled" color="blue">
                        已核销 {batchSummary?.redeemedCount ?? 0}
                    </Tag>
                    <Tag variant="filled">已过期 {batchSummary?.expiredCount ?? 0}</Tag>
                    <Tag variant="filled">已禁用 {batchSummary?.disabledCount ?? 0}</Tag>
                </div>
                <Select
                    className="w-32"
                    value={status}
                    onChange={(value) => {
                        setStatus(value);
                        setPage(1);
                    }}
                    options={[
                        { label: "全部状态", value: "all" },
                        { label: "可用", value: "available" },
                        { label: "已核销", value: "redeemed" },
                        { label: "已过期", value: "expired" },
                        { label: "已禁用", value: "disabled" },
                    ]}
                />
            </div>
            {loadError ? <AdminContentError title={codes.length ? "兑换码明细刷新失败" : "兑换码明细读取失败"} description={codes.length ? `${loadError}；当前继续展示上次成功读取的明细。` : loadError} onRetry={() => void loadCodes()} /> : null}
            {loading && !codes.length ? <AdminTableSkeleton rows={7} columns={6} /> : null}
            {!loading && !loadError && !codes.length ? <AdminTableEmpty compact filtered={status !== "all"} title={status === "all" ? "该批次暂无兑换码" : undefined} /> : null}
            {codes.length ? (
                <Table
                    className="app-data-table"
                    rowKey="id"
                    size="small"
                    loading={loading}
                    columns={columns}
                    dataSource={codes}
                    pagination={{
                        current: page,
                        pageSize,
                        total,
                        showSizeChanger: true,
                        pageSizeOptions: [20, 50, 100],
                        showTotal: (value) => `共 ${value} 个兑换码`,
                        onChange: (nextPage, nextSize) => {
                            setPage(nextSize !== pageSize ? 1 : nextPage);
                            setPageSize(nextSize);
                        },
                    }}
                    scroll={{ x: 960, y: 460 }}
                />
            ) : null}
        </Modal>
    );
}

function renderCodeStatus(status: AdminRedeemCode["status"]) {
    const config = {
        unused: { label: "可用", color: "green" },
        redeemed: { label: "已核销", color: "blue" },
        disabled: { label: "已禁用", color: "default" },
        expired: { label: "已过期", color: "default" },
    }[status];
    return (
        <Tag variant="filled" color={config.color}>
            {config.label}
        </Tag>
    );
}

function formatTime(value?: string) {
    return value ? new Date(value).toLocaleString("zh-CN", { hour12: false }) : "--";
}
