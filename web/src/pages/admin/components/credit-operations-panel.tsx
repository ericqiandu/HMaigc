import { useEffect, useRef, useState } from "react";
import { App, Button, Form, Input, InputNumber, Modal, Select, Table, Tag } from "antd";
import type { ColumnsType } from "antd/es/table";
import { CircleAlert, Coins, RefreshCw, Save, Search, Settings2, UserRoundCog } from "lucide-react";

import { ListToolbar, TableSurface } from "@/components/layout/workspace-page";
import { formatCredits } from "@/constant/credits";
import { useDebouncedValue } from "@/hooks/use-debounced-value";
import { AdminContentError, AdminContentSkeleton, AdminRowActions, AdminTableEmpty, AdminTableSkeleton, SettingsSectionCard } from "@/pages/admin/components/admin-ui";
import { listAdminUsers, type AdminReferenceData, type LocalUser } from "@/services/api/auth";
import { adjustAdminUserCredits, getAdminCreditPolicy, listAdminBillingOrders, resolveAdminBillingOrder, updateAdminCreditPolicy, type BillingOrder } from "@/services/api/wallet";

type AdjustmentFormValues = { userId: string; amount: number; note: string };
type ResolutionFormValues = { note: string };
type PolicyFormValues = { signupBonus: number; checkinBonus: number; defaultMultiplier: number; modelMultipliers: string };

const billingStatusLabels: Record<BillingOrder["status"], string> = {
    uncertain: "待核对",
    running: "运行中",
    reserved: "已冻结",
    settled: "已结算",
    refunded: "已退款",
};

export default function CreditOperationsPanel({ users }: { users: AdminReferenceData["users"] }) {
    const { message, modal } = App.useApp();
    const [orders, setOrders] = useState<BillingOrder[]>([]);
    const [loading, setLoading] = useState(true);
    const [ordersError, setOrdersError] = useState("");
    const [adjusting, setAdjusting] = useState(false);
    const [resolving, setResolving] = useState(false);
    const [keyword, setKeyword] = useState("");
    const debouncedKeyword = useDebouncedValue(keyword);
    const [orderStatus, setOrderStatus] = useState<"review" | "all" | BillingOrder["status"]>("review");
    const [page, setPage] = useState(1);
    const [pageSize, setPageSize] = useState(20);
    const [total, setTotal] = useState(0);
    const [adjustmentUsers, setAdjustmentUsers] = useState<Array<AdminReferenceData["users"][number] | LocalUser>>(users);
    const [searchingUsers, setSearchingUsers] = useState(false);
    const [resolvingOrder, setResolvingOrder] = useState<{ order: BillingOrder; action: "settle" | "refund" } | null>(null);
    const [adjustmentForm] = Form.useForm<AdjustmentFormValues>();
    const [resolutionForm] = Form.useForm<ResolutionFormValues>();
    const [policyForm] = Form.useForm<PolicyFormValues>();
    const [savingPolicy, setSavingPolicy] = useState(false);
    const [policyLoading, setPolicyLoading] = useState(true);
    const [policyLoaded, setPolicyLoaded] = useState(false);
    const [policyError, setPolicyError] = useState("");
    const [policyDirty, setPolicyDirty] = useState(false);
    const ordersRequestSequence = useRef(0);

    const reload = async (targetPage = page, targetPageSize = pageSize) => {
        const requestSequence = ++ordersRequestSequence.current;
        setLoading(true);
        setOrdersError("");
        try {
            const result = await listAdminBillingOrders({ keyword: debouncedKeyword || undefined, status: orderStatus, page: targetPage, limit: targetPageSize });
            if (requestSequence !== ordersRequestSequence.current) return;
            setOrders(result.orders);
            setTotal(result.total);
        } catch (error) {
            if (requestSequence !== ordersRequestSequence.current) return;
            setOrdersError(error instanceof Error ? error.message : "读取待核对计费失败");
        } finally {
            if (requestSequence === ordersRequestSequence.current) setLoading(false);
        }
    };

    const loadPolicy = async () => {
        setPolicyLoading(true);
        setPolicyError("");
        try {
            const { policy } = await getAdminCreditPolicy();
            policyForm.setFieldsValue({
                signupBonus: policy.signupBonusMicrocredits / 1_000_000,
                checkinBonus: policy.checkinBonusMicrocredits / 1_000_000,
                defaultMultiplier: policy.defaultMultiplierBasisPoints / 10_000,
                modelMultipliers: Object.entries(policy.modelMultiplierBasisPoints)
                    .map(([model, value]) => `${model}=${value / 10_000}`)
                    .join("\n"),
            });
            setPolicyLoaded(true);
            setPolicyDirty(false);
        } catch (error) {
            setPolicyError(error instanceof Error ? error.message : "读取积分策略失败");
        } finally {
            setPolicyLoading(false);
        }
    };

    useEffect(() => {
        void reload(page, pageSize);
    }, [debouncedKeyword, orderStatus, page, pageSize]);

    useEffect(() => {
        setAdjustmentUsers(users);
    }, [users]);

    useEffect(() => {
        void loadPolicy();
    }, [policyForm]);

    const savePolicy = async () => {
        const values = await policyForm.validateFields();
        const modelMultiplierBasisPoints: Record<string, number> = {};
        for (const line of String(values.modelMultipliers || "")
            .split("\n")
            .map((item) => item.trim())
            .filter(Boolean)) {
            const [model, rawMultiplier, ...rest] = line.split("=");
            const multiplier = Number(rawMultiplier);
            if (!model?.trim() || rest.length || !Number.isFinite(multiplier) || multiplier <= 0) {
                message.error(`模型倍率格式无效：${line}`);
                return;
            }
            modelMultiplierBasisPoints[model.trim()] = Math.round(multiplier * 10_000);
        }
        setSavingPolicy(true);
        try {
            await updateAdminCreditPolicy({
                signupBonusMicrocredits: Math.round(values.signupBonus * 1_000_000),
                checkinBonusMicrocredits: Math.round(values.checkinBonus * 1_000_000),
                defaultMultiplierBasisPoints: Math.round(values.defaultMultiplier * 10_000),
                modelMultiplierBasisPoints,
            });
            setPolicyDirty(false);
            message.success("积分策略已保存");
        } catch (error) {
            message.error(error instanceof Error ? error.message : "保存积分策略失败");
        } finally {
            setSavingPolicy(false);
        }
    };

    const searchUsers = async (value: string) => {
        setSearchingUsers(true);
        try {
            const result = await listAdminUsers({ keyword: value.trim() || undefined, page: 1, limit: 50 });
            setAdjustmentUsers(result.users);
        } catch (error) {
            message.error(error instanceof Error ? error.message : "搜索用户失败");
        } finally {
            setSearchingUsers(false);
        }
    };

    const executeAdjustment = async (values: AdjustmentFormValues) => {
        setAdjusting(true);
        try {
            await adjustAdminUserCredits(values.userId, { amountMicrocredits: Math.round(values.amount * 1_000_000), note: values.note.trim() });
            adjustmentForm.resetFields();
            message.success("用户积分已调整");
        } catch (error) {
            message.error(error instanceof Error ? error.message : "调整积分失败");
            throw error;
        } finally {
            setAdjusting(false);
        }
    };

    const adjust = async () => {
        const values = await adjustmentForm.validateFields();
        const target = adjustmentUsers.find((user) => user.id === values.userId);
        const targetName = target ? `${target.displayName || target.username}（@${target.username}）` : values.userId;
        modal.confirm({
            className: "admin-operation-modal admin-credit-adjustment-confirm workspace-ui-scope",
            title: values.amount >= 0 ? "确认增加用户积分" : "确认扣减用户积分",
            content: (
                <div className="credit-adjustment-confirm-summary">
                    <div className="credit-adjustment-confirm-row"><span className="credit-adjustment-confirm-label">目标用户</span><strong className="credit-adjustment-confirm-value">{targetName}</strong></div>
                    <div className="credit-adjustment-confirm-row"><span className="credit-adjustment-confirm-label">积分变化</span><strong className={`credit-adjustment-confirm-value ${values.amount >= 0 ? "is-positive" : "is-negative"}`}>{values.amount >= 0 ? "+" : ""}{values.amount}</strong></div>
                    <div className="credit-adjustment-confirm-row"><span className="credit-adjustment-confirm-label">调整依据</span><span className="credit-adjustment-confirm-value">{values.note.trim()}</span></div>
                </div>
            ),
            okText: values.amount >= 0 ? "确认增加" : "确认扣减",
            cancelText: "返回检查",
            okButtonProps: { danger: values.amount < 0 },
            onOk: () => executeAdjustment(values),
        });
    };

    const resolveBilling = async () => {
        if (!resolvingOrder) return;
        const values = await resolutionForm.validateFields();
        setResolving(true);
        try {
            await resolveAdminBillingOrder(resolvingOrder.order.id, { action: resolvingOrder.action, note: values.note.trim() });
            setResolvingOrder(null);
            resolutionForm.resetFields();
            await reload(page, pageSize);
            message.success(resolvingOrder.action === "settle" ? "计费订单已结算" : "冻结积分已退款");
        } catch (error) {
            message.error(error instanceof Error ? error.message : "处理计费订单失败");
        } finally {
            setResolving(false);
        }
    };

    const columns: ColumnsType<BillingOrder> = [
        { title: "创建时间", dataIndex: "createdAt", width: 170, render: formatTime },
        { title: "用户", dataIndex: "userId", width: 150, render: (id) => users.find((user) => user.id === id)?.displayName || id },
        {
            title: "模型 / 场景",
            width: 220,
            render: (_, order) => (
                <div className="credit-order-model">
                    <div className="credit-order-model-name">{order.model}</div>
                    <div className="credit-order-model-scene">{order.scene || order.capability}</div>
                </div>
            ),
        },
        { title: "冻结积分", dataIndex: "amountMicrocredits", width: 120, align: "right", render: (value) => <span className="font-medium tabular-nums">{formatCredits(value)}</span> },
        {
            title: "状态",
            dataIndex: "status",
            width: 105,
            render: (value: BillingOrder["status"]) => (
                <Tag className="credit-order-status" variant="filled" color={value === "settled" ? "success" : value === "refunded" ? "default" : "warning"}>
                    {billingStatusLabels[value]}
                </Tag>
            ),
        },
        { title: "上游请求", dataIndex: "providerRequestId", width: 180, ellipsis: true, render: (value) => value || "未获取" },
        { title: "原因", dataIndex: "error", width: 260, ellipsis: true, render: (value) => value || "费用状态不明确" },
        {
            title: "处理",
            width: 142,
            fixed: "right",
            render: (_, order) =>
                order.status === "settled" || order.status === "refunded" ? (
                    <span className="text-xs text-foreground/40">处理完成</span>
                ) : (
                    <AdminRowActions
                        primary={{
                            label: "确认扣费",
                            onClick: () => {
                                setResolvingOrder({ order, action: "settle" });
                                resolutionForm.resetFields();
                            },
                        }}
                        actions={[
                            {
                                key: "refund",
                                label: "退回积分",
                                danger: true,
                                onClick: () => {
                                    setResolvingOrder({ order, action: "refund" });
                                    resolutionForm.resetFields();
                                },
                            },
                        ]}
                    />
                ),
        },
    ];

    return (
        <div className="credit-operations space-y-6">
            <div className="credit-operations-overview grid min-w-0 items-start">
                <SettingsSectionCard className="credit-policy-panel" icon={<Settings2 className="credit-policy-icon size-4" />} title="积分策略" description="注册、签到与模型倍率统一在服务端结算。">
                    {policyLoading && !policyLoaded ? <AdminContentSkeleton rows={5} compact label="正在读取积分策略" /> : null}
                    {policyError && !policyLoaded ? <AdminContentError title="积分策略读取失败" description={policyError} onRetry={() => void loadPolicy()} /> : null}
                    {policyLoaded ? <Form form={policyForm} layout="vertical" requiredMark={false} className="credit-policy-form admin-content-form" onValuesChange={() => setPolicyDirty(true)} onFinish={() => void savePolicy()}>
                        <div className="credit-policy-fields grid gap-5 md:grid-cols-3">
                            <Form.Item
                                name="signupBonus"
                                label="注册默认积分"
                                rules={[
                                    { required: true, message: "请填写注册积分" },
                                    { type: "number", min: 0 },
                                ]}
                            >
                                <InputNumber className="w-full" min={0} precision={6} />
                            </Form.Item>
                            <Form.Item
                                name="checkinBonus"
                                label="每日签到积分"
                                rules={[
                                    { required: true, message: "请填写签到积分" },
                                    { type: "number", min: 0 },
                                ]}
                            >
                                <InputNumber className="w-full" min={0} precision={6} />
                            </Form.Item>
                            <Form.Item
                                name="defaultMultiplier"
                                label="默认模型倍率"
                                rules={[
                                    { required: true, message: "请填写默认倍率" },
                                    { type: "number", min: 0.0001, max: 100 },
                                ]}
                            >
                                <InputNumber className="w-full" min={0.0001} max={100} precision={4} />
                            </Form.Item>
                        </div>
                        <Form.Item name="modelMultipliers" label="模型独立倍率" extra="每行一项，格式为 模型名=倍率。例如 gpt-image-1=1.5">
                            <Input.TextArea rows={4} placeholder={"gpt-image-1=1.5\nseedance-1.0-pro=2"} />
                        </Form.Item>
                        <div className="admin-form-actions credit-policy-actions flex justify-end">
                            <span className={`credit-policy-sync-state ${policyDirty ? "is-dirty" : ""}`} role="status">{policyDirty ? "有未保存修改" : "已与服务端同步"}</span>
                            <Button className="admin-form-submit" type="primary" htmlType="submit" icon={<Save className="size-4" />} loading={savingPolicy} disabled={!policyDirty}>
                                保存积分策略
                            </Button>
                        </div>
                    </Form> : null}
                </SettingsSectionCard>
                <SettingsSectionCard
                    className="credit-adjustment-panel"
                    icon={<UserRoundCog className="credit-adjustment-icon size-4" />}
                    title="人工调整积分"
                    description="所有变更都会写入不可修改的用户积分流水。"
                    footer={
                        <div className="credit-adjustment-notice flex items-start gap-3.5">
                            <CircleAlert className="credit-adjustment-notice-icon mt-0.5 size-4 shrink-0" />
                            <div className="credit-adjustment-notice-copy">
                                <h3 className="credit-adjustment-notice-title">写操作强校验</h3>
                                <p className="credit-adjustment-notice-description">余额不足时不允许负向调整，建议在备注中填写工单号或处理依据。</p>
                            </div>
                        </div>
                    }
                >
                        <Form form={adjustmentForm} layout="vertical" requiredMark={false} className="credit-adjustment-form admin-content-form" onFinish={() => void adjust()}>
                            <Form.Item name="userId" label="目标用户" rules={[{ required: true, message: "请选择用户" }]}>
                                <Select
                                    showSearch
                                    filterOption={false}
                                    loading={searchingUsers}
                                    placeholder="搜索用户名或显示名称"
                                    onSearch={(value) => void searchUsers(value)}
                                    options={adjustmentUsers.map((user) => ({ label: `${user.displayName || user.username} · @${user.username}`, value: user.id }))}
                                />
                            </Form.Item>
                            <div className="credit-adjustment-fields grid gap-5 sm:grid-cols-2 xl:grid-cols-1">
                                <Form.Item name="amount" label="积分变化" extra="正数增加，负数扣减。" rules={[
                                    { required: true, message: "请填写积分变化" },
                                    { validator: (_rule, value?: number) => value === 0 ? Promise.reject(new Error("积分变化不能为 0")) : Promise.resolve() },
                                ]}>
                                    <InputNumber className="w-full" precision={6} prefix={<Coins className="size-3.5 text-foreground/45" />} placeholder="例如 10 或 -2" />
                                </Form.Item>
                                <Form.Item name="note" label="调整原因" rules={[{ required: true, message: "请填写调整原因" }]}>
                                    <Input maxLength={500} placeholder="将显示在审计流水中" />
                                </Form.Item>
                            </div>
                            <div className="admin-form-actions credit-adjustment-actions flex justify-end">
                                <Button className="admin-form-submit" type="primary" htmlType="submit" icon={<Coins className="size-4" />} loading={adjusting}>
                                    确认调整
                                </Button>
                            </div>
                        </Form>
                </SettingsSectionCard>
            </div>

            <section className="credit-orders-section min-w-0 pt-1">
                <div className="credit-orders-heading mb-5 flex flex-wrap items-end justify-between gap-4">
                    <div className="credit-orders-heading-copy">
                        <div className="credit-orders-title-row flex items-center gap-2">
                            <h2 className="credit-orders-title text-base font-semibold">计费订单</h2>
                            <Tag variant="filled" color={orderStatus === "review" && total ? "warning" : "default"}>
                                {total} 条
                            </Tag>
                        </div>
                        <p className="mt-1.5 text-xs leading-5 text-foreground/55">待核对订单可人工结算或退款，已结算与已退款历史保持只读。</p>
                    </div>
                </div>
                <ListToolbar
                    active={Boolean(keyword || orderStatus !== "review")}
                    onReset={() => {
                        setKeyword("");
                        setOrderStatus("review");
                        setPage(1);
                    }}
                    trailing={
                        <div className="credit-orders-toolbar-trailing">
                            <span className="credit-orders-result-count" role="status">共 {total} 条记录</span>
                            <Button className="credit-orders-refresh" icon={<RefreshCw className="size-4" />} loading={loading} onClick={() => void reload()}>刷新</Button>
                        </div>
                    }
                >
                    <Input
                        allowClear
                        className="app-list-search"
                        prefix={<Search className="size-4 text-foreground/40" />}
                        value={keyword}
                        placeholder="搜索用户、模型、场景或请求号"
                        onChange={(event) => {
                            setKeyword(event.target.value);
                            setPage(1);
                        }}
                    />
                    <Select
                        className="w-36"
                        value={orderStatus}
                        onChange={(value) => {
                            setOrderStatus(value);
                            setPage(1);
                        }}
                        options={[
                            { label: "待核对队列", value: "review" },
                            { label: "全部历史", value: "all" },
                            { label: "费用待核对", value: "uncertain" },
                            { label: "运行中", value: "running" },
                            { label: "已冻结", value: "reserved" },
                            { label: "已结算", value: "settled" },
                            { label: "已退款", value: "refunded" },
                        ]}
                    />
                </ListToolbar>
                {ordersError ? <AdminContentError title="计费订单读取失败" description={ordersError} onRetry={() => void reload()} /> : null}
                {!ordersError || orders.length > 0 ? <TableSurface>
                    {loading && orders.length === 0 ? <AdminTableSkeleton rows={7} columns={8} /> : null}
                    {orders.length > 0 || !loading ? <Table
                        className="credit-orders-table app-data-table"
                        rowKey="id"
                        size="middle"
                        loading={loading}
                        columns={columns}
                        dataSource={orders}
                        locale={{ emptyText: <AdminTableEmpty filtered={Boolean(keyword || orderStatus !== "review")} /> }}
                        pagination={{
                            current: page,
                            pageSize,
                            total,
                            showSizeChanger: true,
                            pageSizeOptions: [20, 50, 100],
                            showTotal: (value, range) => `${range[0]}-${range[1]} / 共 ${value} 条`,
                            onChange: (nextPage, nextPageSize) => {
                                setPage(nextPageSize !== pageSize ? 1 : nextPage);
                                setPageSize(nextPageSize);
                            },
                        }}
                        scroll={{ x: 1200 }}
                    /> : null}
                </TableSurface> : null}
            </section>

            <Modal
                className="admin-operation-modal admin-credit-resolution-modal workspace-ui-scope"
                title={resolvingOrder?.action === "settle" ? "确认扣除冻结积分" : "确认退回冻结积分"}
                open={Boolean(resolvingOrder)}
                onCancel={() => { if (!resolving) setResolvingOrder(null); }}
                onOk={() => void resolveBilling()}
                confirmLoading={resolving}
                keyboard={!resolving}
                maskClosable={!resolving}
                cancelButtonProps={{ disabled: resolving }}
                okText={resolvingOrder?.action === "settle" ? "确认扣费" : "确认退款"}
                cancelText="取消"
                okButtonProps={{ danger: resolvingOrder?.action === "refund" }}
            >
                {resolvingOrder ? <div className="credit-resolution-summary">
                    <div className="credit-resolution-summary-item"><span className="credit-resolution-summary-label">模型</span><strong className="credit-resolution-summary-value">{resolvingOrder.order.model}</strong></div>
                    <div className="credit-resolution-summary-item"><span className="credit-resolution-summary-label">冻结积分</span><strong className="credit-resolution-summary-value">{formatCredits(resolvingOrder.order.amountMicrocredits)}</strong></div>
                    <div className="credit-resolution-summary-item"><span className="credit-resolution-summary-label">请求号</span><span className="credit-resolution-summary-value is-mono">{resolvingOrder.order.providerRequestId || "未获取"}</span></div>
                </div> : null}
                <Form form={resolutionForm} layout="vertical" requiredMark={false} className="credit-resolution-form">
                    <Form.Item name="note" label="核对依据" rules={[{ required: true, message: "请填写供应商账单、任务状态或处理依据" }]}>
                        <Input.TextArea rows={4} maxLength={500} placeholder="例如：供应商后台确认该请求未产生费用" />
                    </Form.Item>
                </Form>
            </Modal>
        </div>
    );
}

function formatTime(value?: string) {
    return value ? new Date(value).toLocaleString("zh-CN", { hour12: false }) : "--";
}
