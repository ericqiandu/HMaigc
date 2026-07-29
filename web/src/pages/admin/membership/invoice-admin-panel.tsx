import { App, Button, Form, Input, Modal, Select, Table, Tag } from "antd";
import { useCallback, useEffect, useState } from "react";

import { TableSurface } from "@/components/layout/workspace-page";
import { listAdminInvoiceRequests, resolveAdminInvoiceRequest, type InvoiceRequest, type InvoiceRequestStatus } from "@/services/api/membership";

type ResolveForm = {
    invoiceNumber: string;
    invoiceUrl: string;
    note: string;
};

const statusLabels: Record<InvoiceRequestStatus, string> = {
    pending: "待处理",
    issued: "已开具",
    rejected: "已驳回",
};

export function InvoiceAdminPanel() {
    const { message } = App.useApp();
    const [items, setItems] = useState<InvoiceRequest[]>([]);
    const [loading, setLoading] = useState(false);
    const [status, setStatus] = useState<InvoiceRequestStatus | undefined>();
    const [resolving, setResolving] = useState<{ request: InvoiceRequest; status: "issued" | "rejected" } | null>(null);
    const [form] = Form.useForm<ResolveForm>();

    const load = useCallback(async () => {
        setLoading(true);
        try {
            const response = await listAdminInvoiceRequests(status);
            setItems(response.items);
        } catch (error) {
            message.error(error instanceof Error ? error.message : "开票申请加载失败");
        } finally {
            setLoading(false);
        }
    }, [message, status]);

    useEffect(() => {
        void load();
    }, [load]);

    const openResolve = (request: InvoiceRequest, nextStatus: "issued" | "rejected") => {
        setResolving({ request, status: nextStatus });
        form.resetFields();
    };

    const submit = async () => {
        if (!resolving) return;
        const values = await form.validateFields();
        try {
            await resolveAdminInvoiceRequest(resolving.request.id, {
                status: resolving.status,
                invoiceNumber: resolving.status === "issued" ? values.invoiceNumber.trim() : undefined,
                invoiceUrl: resolving.status === "issued" ? values.invoiceUrl.trim() : undefined,
                note: values.note.trim(),
            });
            message.success(resolving.status === "issued" ? "开票申请已标记为已开具" : "开票申请已驳回");
            setResolving(null);
            await load();
        } catch (error) {
            message.error(error instanceof Error ? error.message : "开票申请处理失败");
        }
    };

    return (
        <section className="admin-invoice-panel">
            <div className="admin-invoice-toolbar mb-3 flex items-center justify-between gap-3">
                <div className="admin-invoice-toolbar-copy">
                    <h3 className="admin-invoice-toolbar-title text-sm font-semibold">电子发票处理</h3>
                    <p className="admin-invoice-toolbar-description mt-1 text-xs text-foreground/45">这里只记录真实开票状态；发票文件必须由税务系统或已配置服务商生成后回填。</p>
                </div>
                <Select
                    className="admin-invoice-status-filter w-32"
                    allowClear
                    placeholder="全部状态"
                    value={status}
                    options={Object.entries(statusLabels).map(([value, label]) => ({ value, label }))}
                    onChange={(value: InvoiceRequestStatus | undefined) => setStatus(value)}
                />
            </div>
            <TableSurface className="admin-invoice-table-surface mt-0">
                <Table
                    className="admin-invoice-table"
                    rowKey="id"
                    loading={loading}
                    dataSource={items}
                    size="small"
                    pagination={{ pageSize: 30, hideOnSinglePage: true }}
                    columns={[
                        { title: "订单", dataIndex: "membershipOrderId", ellipsis: true },
                        { title: "抬头", dataIndex: "title", ellipsis: true },
                        { title: "税号", render: (_, row) => row.taxNumber || "—", ellipsis: true },
                        { title: "金额", render: (_, row) => `¥${(row.amountCents / 100).toLocaleString("zh-CN")}` },
                        { title: "接收邮箱", dataIndex: "email", ellipsis: true },
                        {
                            title: "状态",
                            render: (_, row) => (
                                <Tag className="admin-invoice-status-tag" color={row.status === "issued" ? "green" : row.status === "rejected" ? "red" : "gold"}>
                                    {statusLabels[row.status]}
                                </Tag>
                            ),
                        },
                        { title: "申请时间", render: (_, row) => new Date(row.createdAt).toLocaleString("zh-CN") },
                        {
                            title: "发票",
                            render: (_, row) =>
                                row.invoiceUrl ? (
                                    <a className="admin-invoice-file-link text-xs text-[var(--workspace-accent)]" href={row.invoiceUrl} target="_blank" rel="noreferrer">
                                        查看发票
                                    </a>
                                ) : (
                                    "—"
                                ),
                        },
                        {
                            title: "操作",
                            render: (_, row) =>
                                row.status === "pending" ? (
                                    <div className="admin-invoice-actions flex gap-1">
                                        <Button className="admin-invoice-issue-button" type="primary" size="small" onClick={() => openResolve(row, "issued")}>
                                            登记开票
                                        </Button>
                                        <Button className="admin-invoice-reject-button" type="text" danger size="small" onClick={() => openResolve(row, "rejected")}>
                                            驳回
                                        </Button>
                                    </div>
                                ) : (
                                    <span className="admin-invoice-resolved-note text-xs text-foreground/42">{row.resolutionNote || "已处理"}</span>
                                ),
                        },
                    ]}
                />
            </TableSurface>

            <Modal
                className="admin-invoice-resolve-modal"
                title={resolving?.status === "issued" ? "登记电子发票" : "驳回开票申请"}
                open={Boolean(resolving)}
                okText={resolving?.status === "issued" ? "确认已开具" : "确认驳回"}
                okButtonProps={{ danger: resolving?.status === "rejected" }}
                onOk={() => void submit()}
                onCancel={() => setResolving(null)}
                destroyOnHidden
            >
                <Form className="admin-invoice-resolve-form" form={form} layout="vertical">
                    {resolving?.status === "issued" ? (
                        <>
                            <Form.Item className="admin-invoice-number-field" name="invoiceNumber" label="发票号码" rules={[{ required: true, message: "请输入真实发票号码" }]}>
                                <Input className="admin-invoice-number-input" maxLength={120} />
                            </Form.Item>
                            <Form.Item
                                className="admin-invoice-url-field"
                                name="invoiceUrl"
                                label="发票文件 HTTPS 地址"
                                rules={[
                                    { required: true, message: "请输入发票文件地址" },
                                    { type: "url", message: "请输入有效 URL" },
                                    { pattern: /^https:\/\//i, message: "必须使用 HTTPS 地址" },
                                ]}
                            >
                                <Input className="admin-invoice-url-input" placeholder="https://..." />
                            </Form.Item>
                        </>
                    ) : null}
                    <Form.Item className="admin-invoice-note-field" name="note" label="处理备注" rules={[{ required: true, message: "请输入可审计的处理备注" }]}>
                        <Input.TextArea className="admin-invoice-note-input" rows={3} maxLength={500} showCount />
                    </Form.Item>
                </Form>
            </Modal>
        </section>
    );
}
