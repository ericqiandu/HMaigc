import { Alert, App, Button, Form, Input, Modal, Popconfirm, Select, Table, Tag } from "antd";
import type { ColumnsType } from "antd/es/table";
import { BellRing, CircleAlert, Info, Plus, Search, ShieldAlert, Wrench } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { useBlocker } from "react-router";

import { ListToolbar, TableSurface } from "@/components/layout/workspace-page";
import { useDebouncedValue } from "@/hooks/use-debounced-value";
import { closeAdminAnnouncement, createAdminAnnouncement, listAdminAnnouncements, type AnnouncementLevel, type AnnouncementStatus, type SystemAnnouncement } from "@/services/api/announcements";
import { AdminContentError, AdminTableEmpty, AdminTableSkeleton } from "./admin-ui";
import { announcementFormIsEmpty, emptyAnnouncementForm, normalizeAnnouncementForm, type AnnouncementFormValues } from "./announcement-form-values";

const levelOptions: Array<{ value: AnnouncementLevel; label: string }> = [
    { value: "info", label: "平台通知" },
    { value: "success", label: "状态恢复" },
    { value: "warning", label: "服务提醒" },
    { value: "critical", label: "重要通知" },
];

const levelMeta: Record<AnnouncementLevel, { label: string; color: string; icon: typeof Info }> = {
    info: { label: "平台通知", color: "blue", icon: Info },
    success: { label: "状态恢复", color: "green", icon: Wrench },
    warning: { label: "服务提醒", color: "orange", icon: CircleAlert },
    critical: { label: "重要通知", color: "red", icon: ShieldAlert },
};

export default function AdminAnnouncementsPanel() {
    const { message, modal } = App.useApp();
    const [form] = Form.useForm<AnnouncementFormValues>();
    const [announcements, setAnnouncements] = useState<SystemAnnouncement[]>([]);
    const [keyword, setKeyword] = useState("");
    const debouncedKeyword = useDebouncedValue(keyword);
    const [status, setStatus] = useState<"all" | AnnouncementStatus>("all");
    const [page, setPage] = useState(1);
    const [pageSize, setPageSize] = useState(20);
    const [total, setTotal] = useState(0);
    const [loading, setLoading] = useState(false);
    const [hasSuccessfulLoad, setHasSuccessfulLoad] = useState(false);
    const [loadError, setLoadError] = useState<string | null>(null);
    const [modalOpen, setModalOpen] = useState(false);
    const [publishing, setPublishing] = useState(false);
    const [formDirty, setFormDirty] = useState(false);
    const [closingId, setClosingId] = useState("");
    const blocker = useBlocker(modalOpen && formDirty && !publishing);

    const reload = useCallback(async () => {
        setLoading(true);
        setLoadError(null);
        try {
            const data = await listAdminAnnouncements({ keyword: debouncedKeyword || undefined, status: status === "all" ? undefined : status, page, limit: pageSize });
            setAnnouncements(data.announcements);
            setTotal(data.total);
            setHasSuccessfulLoad(true);
        } catch (error) {
            setLoadError(error instanceof Error ? error.message : "读取公告列表失败");
        } finally {
            setLoading(false);
        }
    }, [debouncedKeyword, page, pageSize, status]);

    useEffect(() => {
        void reload();
    }, [reload]);

    useEffect(() => {
        const beforeUnload = (event: BeforeUnloadEvent) => {
            if (!modalOpen || !formDirty || publishing) return;
            event.preventDefault();
        };
        window.addEventListener("beforeunload", beforeUnload);
        return () => window.removeEventListener("beforeunload", beforeUnload);
    }, [formDirty, modalOpen, publishing]);

    useEffect(() => {
        if (blocker.state !== "blocked") return;
        modal.confirm({
            className: "admin-operation-modal workspace-ui-scope",
            title: "离开并放弃公告草稿？",
            content: "当前标题或正文尚未发布。离开此页面后，未发布内容将丢失。",
            okText: "放弃并离开",
            cancelText: "继续编辑",
            okButtonProps: { danger: true },
            onOk: () => {
                setModalOpen(false);
                setFormDirty(false);
                form.resetFields();
                blocker.proceed();
            },
            onCancel: () => blocker.reset(),
        });
    }, [blocker, form, modal]);

    const openPublishModal = () => {
        form.setFieldsValue(emptyAnnouncementForm);
        setFormDirty(false);
        setModalOpen(true);
    };

    const requestCloseModal = () => {
        if (publishing) return;
        if (!formDirty) {
            setModalOpen(false);
            return;
        }
        modal.confirm({
            className: "admin-operation-modal workspace-ui-scope",
            title: "放弃未发布的公告？",
            content: "当前标题或正文尚未发布，关闭后输入内容将丢失。",
            okText: "放弃草稿",
            cancelText: "继续编辑",
            okButtonProps: { danger: true },
            onOk: () => {
                setModalOpen(false);
                setFormDirty(false);
                form.resetFields();
            },
        });
    };

    const publish = async () => {
        let values: AnnouncementFormValues;
        try {
            values = normalizeAnnouncementForm(await form.validateFields());
        } catch {
            message.error("请先修正公告内容中的字段错误");
            return;
        }
        setPublishing(true);
        try {
            await createAdminAnnouncement(values);
            setModalOpen(false);
            setFormDirty(false);
            form.resetFields();
            setPage(1);
            if (page === 1) await reload();
            message.success("公告已发布");
        } catch (error) {
            message.error(error instanceof Error ? error.message : "发布公告失败");
        } finally {
            setPublishing(false);
        }
    };

    const closeAnnouncement = async (announcement: SystemAnnouncement) => {
        setClosingId(announcement.id);
        try {
            await closeAdminAnnouncement(announcement.id);
            await reload();
            message.success("公告已关闭");
        } catch (error) {
            message.error(error instanceof Error ? error.message : "关闭公告失败");
        } finally {
            setClosingId("");
        }
    };

    const columns: ColumnsType<SystemAnnouncement> = [
        {
            title: "公告内容",
            dataIndex: "title",
            minWidth: 300,
            render: (_, announcement) => (
                <div className="admin-announcement-cell min-w-0 py-0.5">
                    <div className="admin-announcement-cell-title truncate text-sm font-medium text-foreground" title={announcement.title}>
                        {announcement.title}
                    </div>
                    <div className="admin-announcement-cell-content mt-1 line-clamp-2 whitespace-pre-wrap text-xs leading-5 text-foreground/50">{announcement.content}</div>
                </div>
            ),
        },
        {
            title: "级别",
            dataIndex: "level",
            width: 120,
            render: (level: AnnouncementLevel) => {
                const meta = levelMeta[level] || levelMeta.info;
                const Icon = meta.icon;
                return (
                    <Tag className="admin-announcement-level" color={meta.color} icon={<Icon className="admin-announcement-level-icon size-3" />}>
                        {meta.label}
                    </Tag>
                );
            },
        },
        {
            title: "状态",
            dataIndex: "status",
            width: 100,
            render: (value: AnnouncementStatus) =>
                value === "active" ? (
                    <Tag className="admin-announcement-status" color="green">
                        发布中
                    </Tag>
                ) : (
                    <Tag className="admin-announcement-status">已关闭</Tag>
                ),
        },
        {
            title: "发布时间",
            dataIndex: "publishedAt",
            width: 150,
            render: formatDateTime,
        },
        {
            title: "关闭时间",
            dataIndex: "closedAt",
            width: 150,
            render: (value?: string) => (value ? formatDateTime(value) : "--"),
        },
        {
            title: "操作",
            key: "actions",
            fixed: "right",
            width: 100,
            render: (_, announcement) =>
                announcement.status === "active" ? (
                    <Popconfirm
                        rootClassName="admin-operation-popconfirm workspace-ui-scope"
                        title={`关闭“${announcement.title}”？`}
                        description="关闭后将立即从用户公告中心移除，历史记录仍会保留。"
                        okText="关闭公告"
                        cancelText="取消"
                        onConfirm={() => void closeAnnouncement(announcement)}
                    >
                        <Button className="admin-announcement-close" type="text" danger size="small" disabled={Boolean(closingId) && closingId !== announcement.id} loading={closingId === announcement.id}>
                            关闭
                        </Button>
                    </Popconfirm>
                ) : (
                    <span className="admin-announcement-ended text-xs text-foreground/35">已结束</span>
                ),
        },
    ];

    return (
        <div className="admin-announcements-layout">
            <div className="admin-announcements-heading mb-5 flex flex-wrap items-center justify-between gap-4">
                <div className="admin-announcements-heading-copy flex min-w-0 items-center gap-4">
                    <span className="admin-announcements-icon grid size-9 shrink-0 place-items-center">
                        <BellRing className="admin-announcements-icon-symbol size-4" />
                    </span>
                    <div className="admin-announcements-summary min-w-0">
                        <div className="admin-announcements-count text-sm font-medium text-foreground" role="status" aria-live="polite">{hasSuccessfulLoad ? `${total} 条公告记录` : "公告记录尚未读取"}</div>
                        <div className="admin-announcements-description mt-1 text-xs leading-5 text-foreground/50">关闭公告会立即从用户公告中心移除</div>
                    </div>
                </div>
                <Button className="admin-announcements-publish" type="primary" icon={<Plus className="admin-announcements-publish-icon size-4" />} onClick={openPublishModal}>
                    发布公告
                </Button>
            </div>

            <ListToolbar
                active={Boolean(keyword || status !== "all")}
                onReset={() => {
                    setKeyword("");
                    setStatus("all");
                    setPage(1);
                }}
            >
                <Input
                    allowClear
                    className="admin-announcements-search app-list-search"
                    prefix={<Search className="admin-announcements-search-icon size-4 text-foreground/40" />}
                    value={keyword}
                    placeholder="搜索公告标题或正文"
                    onChange={(event) => {
                        setKeyword(event.target.value);
                        setPage(1);
                    }}
                />
                <Select
                    className="admin-announcements-status-filter w-32"
                    aria-label="筛选系统公告状态"
                    value={status}
                    onChange={(value) => {
                        setStatus(value);
                        setPage(1);
                    }}
                    options={[
                        { label: "全部状态", value: "all" },
                        { label: "发布中", value: "active" },
                        { label: "已关闭", value: "closed" },
                    ]}
                />
            </ListToolbar>
            {loadError && hasSuccessfulLoad ? <AdminContentError title="公告列表刷新失败" description={`${loadError}。当前仍显示上次成功读取的结果。`} onRetry={() => void reload()} /> : null}
            <TableSurface>
                {!hasSuccessfulLoad && loading ? <AdminTableSkeleton rows={8} columns={6} /> : null}
                {!hasSuccessfulLoad && !loading && loadError ? <AdminContentError title="公告列表读取失败" description={loadError} onRetry={() => void reload()} /> : null}
                {hasSuccessfulLoad ? (
                    <Table
                        className="app-data-table"
                        size="middle"
                        rowKey="id"
                        loading={loading}
                        columns={columns}
                        dataSource={announcements}
                        locale={{
                            emptyText: (
                                <AdminTableEmpty
                                    filtered={Boolean(keyword || status !== "all")}
                                    title={keyword || status !== "all" ? "没有符合条件的公告" : "暂无公告"}
                                    description={keyword || status !== "all" ? "调整关键词或状态筛选后再试。" : "发布后，公告会出现在这里并同步到用户公告中心。"}
                                />
                            ),
                        }}
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
                        scroll={{ x: 890 }}
                    />
                ) : null}
            </TableSurface>

            <Modal
                className="admin-operation-modal admin-announcement-modal workspace-ui-scope"
                title="发布系统公告"
                open={modalOpen}
                width={680}
                centered
                okText="立即发布"
                cancelText="取消"
                confirmLoading={publishing}
                okButtonProps={{ disabled: !formDirty }}
                maskClosable={!publishing}
                keyboard={!publishing}
                closable={!publishing}
                onOk={() => void publish()}
                onCancel={requestCloseModal}
                destroyOnHidden
            >
                <Form form={form} layout="vertical" className="admin-announcement-form" requiredMark={false} disabled={publishing} onValuesChange={(_, values) => setFormDirty(!announcementFormIsEmpty(values))}>
                    <Alert className="admin-announcement-impact" type="info" showIcon message="发布后立即对全部用户生效" description="公告会进入用户公告中心；如需撤回，请在列表中执行关闭操作。" />
                    <Form.Item
                        className="admin-announcement-title-field"
                        name="title"
                        label="公告标题"
                        rules={[
                            { required: true, whitespace: true, message: "请填写公告标题" },
                            { max: 120, message: "标题不能超过 120 个字符" },
                        ]}
                    >
                        <Input className="admin-announcement-title-input" maxLength={120} showCount placeholder="例如：视频模型已恢复正常使用" />
                    </Form.Item>
                    <Form.Item className="admin-announcement-level-field" name="level" label="公告级别" rules={[{ required: true, message: "请选择公告级别" }]}>
                        <Select className="admin-announcement-level-select" options={levelOptions} />
                    </Form.Item>
                    <Form.Item
                        className="admin-announcement-content-field"
                        name="content"
                        label="公告正文"
                        rules={[
                            { required: true, whitespace: true, message: "请填写公告正文" },
                            { max: 4000, message: "正文不能超过 4000 个字符" },
                        ]}
                    >
                        <Input.TextArea className="admin-announcement-content-input" maxLength={4000} showCount autoSize={{ minRows: 6, maxRows: 12 }} placeholder="填写服务状态、影响范围和用户需要采取的操作" />
                    </Form.Item>
                </Form>
            </Modal>
        </div>
    );
}

function formatDateTime(value: string) {
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) return "--";
    return new Intl.DateTimeFormat("zh-CN", { year: "numeric", month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit", hour12: false }).format(date).replaceAll("/", "-");
}
