import { Alert, App, Button, Form, Input, Modal, Progress, Select, Space, Switch, Tag } from "antd";
import { ArchiveRestore, Cloud, DatabaseBackup, Info, KeyRound, RefreshCw, ShieldCheck } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { useBlocker } from "react-router";

import { getAdminOSSSetting, updateAdminOSSSetting, type AdminOSSSetting } from "@/services/api/auth";
import { getStorageMigrationOverview, retryStorageMigration, startStorageMigration, type StorageMigrationOverview, type StorageMigrationStatus } from "@/services/api/storage-migrations";
import { useAdminContext } from "../admin-context";
import { AdminPageFrame } from "../components/admin-shell";
import { AdminContentError, AdminContentSkeleton, AdminSettingsActionBar, configuredSecretText, SettingsSectionCard } from "../components/admin-ui";
import { normalizeStorageFormValues, storageFieldErrors, storageFormValues, storageValuesEqual, type OSSFormValues } from "./storage-setting-form";

export default function StorageSettingsPage() {
    const { message, modal } = App.useApp();
    const { references } = useAdminContext();
    const [setting, setSetting] = useState<AdminOSSSetting | null>(null);
    const [loading, setLoading] = useState(true);
    const [settingError, setSettingError] = useState<string | null>(null);
    const [saving, setSaving] = useState(false);
    const [dirty, setDirty] = useState(false);
    const [migration, setMigration] = useState<StorageMigrationOverview | null>(null);
    const [migrationLoading, setMigrationLoading] = useState(false);
    const [migrationError, setMigrationError] = useState<string | null>(null);
    const [migrationOpen, setMigrationOpen] = useState(false);
    const [migrationConfirmation, setMigrationConfirmation] = useState("");
    const [retryOpen, setRetryOpen] = useState(false);
    const [retryConfirmation, setRetryConfirmation] = useState("");
    const [migrationSubmitting, setMigrationSubmitting] = useState(false);
    const [form] = Form.useForm<OSSFormValues>();
    const dirtyRef = useRef(false);
    const settingRef = useRef<AdminOSSSetting | null>(null);
    const userNameById = useMemo(() => new Map(references.users.map((user) => [user.id, user.displayName || user.username])), [references.users]);

    const loadSetting = useCallback(async () => {
        setLoading(true);
        setSettingError(null);
        try {
            const result = await getAdminOSSSetting();
            if (dirtyRef.current && settingRef.current) {
                message.info("已保留当前未保存的 OSS 配置，未使用刷新结果覆盖输入");
                return;
            }
            setSetting(result.setting);
            settingRef.current = result.setting;
            form.setFieldsValue(storageFormValues(result.setting));
            setDirty(false);
        } catch (error) {
            setSettingError(error instanceof Error ? error.message : "读取 OSS 配置失败");
        } finally {
            setLoading(false);
        }
    }, [form, message]);

    const refreshMigration = useCallback(
        async (showLoading = true) => {
            if (showLoading) setMigrationLoading(true);
            setMigrationError(null);
            try {
                setMigration(await getStorageMigrationOverview());
            } catch (error) {
                setMigrationError(error instanceof Error ? error.message : "读取迁移状态失败");
            } finally {
                if (showLoading) setMigrationLoading(false);
            }
        },
        [],
    );

    useEffect(() => {
        void loadSetting();
        void refreshMigration();
    }, [loadSetting, refreshMigration]);

    useEffect(() => {
        dirtyRef.current = dirty;
    }, [dirty]);

    const blocker = useBlocker(dirty && !saving);

    useEffect(() => {
        const beforeUnload = (event: BeforeUnloadEvent) => {
            if (!dirty || saving) return;
            event.preventDefault();
        };
        window.addEventListener("beforeunload", beforeUnload);
        return () => window.removeEventListener("beforeunload", beforeUnload);
    }, [dirty, saving]);

    useEffect(() => {
        if (blocker.state !== "blocked") return;
        modal.confirm({
            title: "离开并放弃未保存的 OSS 配置？",
            content: "Endpoint、Bucket、路径前缀或密钥输入尚未保存，离开后这些修改将丢失。",
            okText: "放弃修改并离开",
            cancelText: "继续编辑",
            okButtonProps: { danger: true },
            onOk: () => blocker.proceed(),
            onCancel: () => blocker.reset(),
        });
    }, [blocker, modal]);

    useEffect(() => {
        if (!migration?.active) return;
        const timer = window.setInterval(() => {
            void refreshMigration(false);
        }, 2_000);
        return () => window.clearInterval(timer);
    }, [migration?.active?.id, refreshMigration]);

    const save = async () => {
        const values = await form.validateFields();
        const normalized = normalizeStorageFormValues(values);
        setSaving(true);
        try {
            const result = await updateAdminOSSSetting({
                enabled: normalized.enabled === true,
                provider: normalized.provider,
                region: normalized.region,
                endpoint: normalized.endpoint,
                bucket: normalized.bucket,
                accessKeyId: normalized.accessKeyId,
                accessKeySecret: normalized.accessKeySecret,
                publicBaseUrl: "",
                pathPrefix: normalized.pathPrefix,
            });
            setSetting(result.setting);
            settingRef.current = result.setting;
            form.setFieldsValue(storageFormValues(result.setting));
            setDirty(false);
            message.success("OSS 配置已保存");
        } catch (error) {
            message.error(error instanceof Error ? error.message : "保存 OSS 配置失败");
        } finally {
            setSaving(false);
        }
    };

    const startMigration = async () => {
        setMigrationSubmitting(true);
        try {
            await startStorageMigration(migrationConfirmation);
            setMigrationOpen(false);
            setMigrationConfirmation("");
            message.success("历史资源迁移任务已创建");
            await refreshMigration(false);
        } catch (error) {
            message.error(error instanceof Error ? error.message : "创建迁移任务失败");
        } finally {
            setMigrationSubmitting(false);
        }
    };

    const retryMigration = async () => {
        const job = migration?.latest;
        if (!job) return;
        setMigrationSubmitting(true);
        try {
            await retryStorageMigration(job.id, retryConfirmation);
            setRetryOpen(false);
            setRetryConfirmation("");
            message.success("失败资源已重新进入迁移队列");
            await refreshMigration(false);
        } catch (error) {
            message.error(error instanceof Error ? error.message : "重试迁移失败");
        } finally {
            setMigrationSubmitting(false);
        }
    };

    return (
        <AdminPageFrame title="存储服务" description="OSS 与资源存储">
            <div className="admin-storage-layout admin-settings-page space-y-6">
                <div className="admin-storage-guidance bg-muted/25 px-5 py-4 text-foreground/75" role="note">
                    <div className="admin-storage-guidance-content flex items-start gap-4">
                        <span className="admin-storage-guidance-icon mt-0.5 grid size-5 shrink-0 place-items-center" aria-hidden="true">
                            <Info className="size-4" />
                        </span>
                        <div className="admin-storage-guidance-copy">
                            <div className="admin-storage-guidance-title text-sm font-semibold text-foreground">资源存储规则</div>
                            <p className="admin-storage-guidance-description mt-1.5 text-xs leading-6 text-foreground/55">启用后，新上传和生成的媒体由后端写入 OSS；未启用时写入后端数据卷。资源统一通过登录鉴权接口读取，不直接暴露 OSS 对象地址。</p>
                        </div>
                    </div>
                </div>
                {loading && !setting ? <AdminContentSkeleton rows={8} label="正在读取 OSS 配置" /> : null}
                {settingError ? <AdminContentError title={setting ? "OSS 配置刷新失败" : "OSS 配置读取失败"} description={setting ? `${settingError}。当前保留上次成功读取的配置。` : settingError} onRetry={() => void loadSetting()} /> : null}
                {!loading && setting ? <SettingsSectionCard
                    icon={<Cloud className="size-4" />}
                    title="平台 OSS"
                    description="配置平台媒体资源的默认存储位置。"
                    status={
                        <Space size={6}>
                            <Tag variant="filled" color={setting?.enabled ? "success" : "default"}>
                                {setting?.enabled ? "已启用" : "未启用"}
                            </Tag>
                            <Tag variant="filled" color={setting?.hasAccessKeySecret ? "blue" : "warning"}>
                                {setting?.hasAccessKeySecret ? configuredSecretText : "未保存密钥"}
                            </Tag>
                        </Space>
                    }
                >
                    <Form
                        className="admin-content-form storage-settings-form"
                        form={form}
                        layout="vertical"
                        requiredMark={false}
                        disabled={loading || saving}
                        onValuesChange={(_, values: OSSFormValues) => setDirty(!storageValuesEqual(values, setting))}
                    >
                        <div className="storage-settings-groups">
                            <section className="storage-settings-group" aria-labelledby="storage-connection-heading">
                                <div className="storage-settings-group-heading">
                                    <h3 id="storage-connection-heading">存储目标</h3>
                                    <p>决定新资源的写入位置与对象路径，不影响已经保存的历史资源。</p>
                                </div>
                                <div className="storage-settings-fields grid grid-cols-1 gap-x-6 md:grid-cols-2">
                                    <Form.Item name="enabled" label="启用 OSS" valuePropName="checked">
                                        <Switch />
                                    </Form.Item>
                                    <Form.Item name="provider" label="存储渠道" rules={[{ required: true, message: "请选择存储渠道" }]}>
                                        <Select options={[{ label: "阿里云 OSS", value: "aliyun" }]} />
                                    </Form.Item>
                                    <Form.Item name="region" label="Region">
                                        <Input autoComplete="off" placeholder="例如：oss-cn-hangzhou" />
                                    </Form.Item>
                                    <Form.Item
                                        name="endpoint"
                                        label="Endpoint"
                                        dependencies={["enabled"]}
                                        rules={[{ validator: async (_, value: string | undefined) => validateStorageField(form.getFieldsValue(true), setting.hasAccessKeySecret, "endpoint", value) }]}
                                        extra="填写 Bucket 所在地域的公网 Endpoint，保存时会自动移除末尾斜杠。"
                                    >
                                        <Input autoComplete="off" placeholder="https://oss-cn-hangzhou.aliyuncs.com" />
                                    </Form.Item>
                                    <Form.Item name="bucket" label="Bucket" dependencies={["enabled"]} rules={[{ validator: async () => validateStorageField(form.getFieldsValue(true), setting.hasAccessKeySecret, "bucket") }]}>
                                        <Input autoComplete="off" placeholder="例如：my-canvas-assets" />
                                    </Form.Item>
                                    <Form.Item name="pathPrefix" label="路径前缀" extra="可选。用于隔离环境或业务目录，例如 production/assets。">
                                        <Input autoComplete="off" placeholder="例如：uploads/infinite-canvas" />
                                    </Form.Item>
                                </div>
                            </section>
                            <section className="storage-settings-group" aria-labelledby="storage-credential-heading">
                                <div className="storage-settings-group-heading">
                                    <h3 id="storage-credential-heading">访问凭据</h3>
                                    <p>建议使用只允许访问目标 Bucket 的 RAM 子账号，并按最小权限配置。</p>
                                </div>
                                <div className="storage-settings-fields grid grid-cols-1 gap-x-6 md:grid-cols-2">
                                    <Form.Item name="accessKeyId" label="AccessKey ID" dependencies={["enabled"]} rules={[{ validator: async () => validateStorageField(form.getFieldsValue(true), setting.hasAccessKeySecret, "accessKeyId") }]}>
                                        <Input autoComplete="off" placeholder="阿里云 AccessKey ID" />
                                    </Form.Item>
                                    <Form.Item
                                        name="accessKeySecret"
                                        label={setting.hasAccessKeySecret ? `AccessKey Secret（${configuredSecretText}）` : "AccessKey Secret"}
                                        dependencies={["enabled"]}
                                        rules={[{ validator: async () => validateStorageField(form.getFieldsValue(true), setting.hasAccessKeySecret, "accessKeySecret") }]}
                                        extra={setting.hasAccessKeySecret ? "密钥不会回显；保持为空会继续使用当前已保存密钥。" : "密钥仅提交到后端加密保存，不会在页面中回显。"}
                                    >
                                        <Input.Password autoComplete="new-password" placeholder={setting.hasAccessKeySecret ? "留空保留原密钥" : "阿里云 AccessKey Secret"} />
                                    </Form.Item>
                                </div>
                            </section>
                        </div>
                    </Form>
                    <AdminSettingsActionBar
                        status={dirty ? "有未保存的 OSS 配置变更" : "OSS 配置已同步"}
                        meta={setting?.updatedAt ? `上次更新：${formatTime(setting.updatedAt)}${setting.updatedBy ? ` · ${userNameById.get(setting.updatedBy) || setting.updatedBy}` : ""}` : "尚未保存 OSS 配置"}
                    >
                        <Button type="primary" loading={saving} disabled={!dirty} onClick={() => void save()}>
                            保存 OSS 配置
                        </Button>
                    </AdminSettingsActionBar>
                </SettingsSectionCard> : null}
                <SettingsSectionCard
                    icon={<DatabaseBackup className="size-4" />}
                    title="历史资源迁移"
                    description="将后端数据卷中的历史媒体复制到平台 OSS；校验成功后更新资源记录，原文件继续保留。"
                    status={
                        migrationError && !migration ? (
                            <Tag color="error" variant="filled">读取失败</Tag>
                        ) : migration?.active ? (
                            <Tag color="processing" variant="filled">
                                迁移进行中
                            </Tag>
                        ) : migration?.latest ? (
                            <MigrationStatusTag status={migration.latest.status} />
                        ) : (
                            <Tag variant="filled">尚未执行</Tag>
                        )
                    }
                    footer={
                        <>
                            <div className="text-xs text-foreground/45">迁移失败会保留原资源地址和本地文件，不会伪造成功状态。</div>
                            <div className="storage-migration-actions flex flex-wrap items-center justify-end gap-2">
                                <Button icon={<RefreshCw className="size-3.5" />} loading={migrationLoading} onClick={() => void refreshMigration()}>
                                    刷新
                                </Button>
                                {migration?.latest && migration.latest.totalItems > 0 && (migration.latest.status === "failed" || migration.latest.status === "partial_failed") ? (
                                    <Button icon={<ArchiveRestore className="size-3.5" />} loading={migrationSubmitting} onClick={() => setRetryOpen(true)}>
                                        重试失败项
                                    </Button>
                                ) : null}
                                <Button type="primary" disabled={Boolean(migrationError) || !setting?.enabled || Boolean(migration?.active) || !migration?.eligible.items} onClick={() => setMigrationOpen(true)}>
                                    开始迁移
                                </Button>
                            </div>
                        </>
                    }
                >
                    <div className="storage-migration-content space-y-5 px-6 py-5">
                        {migrationLoading && !migration ? <AdminContentSkeleton compact rows={4} label="正在读取历史资源迁移状态" /> : null}
                        {migrationError ? <AdminContentError title={migration ? "迁移状态刷新失败" : "迁移状态读取失败"} description={migration ? `${migrationError}。当前保留上次成功读取的迁移状态。` : migrationError} onRetry={() => void refreshMigration()} /> : null}
                        {migration?.active ? (
                            <div className="storage-migration-active-summary" role="status" aria-live="polite">
                                <div className="storage-migration-active-title">活动任务正在执行</div>
                                <div className="storage-migration-active-meta">
                                    <span>任务 {migration.active.id}</span>
                                    <span>发起人 {userNameById.get(migration.active.requestedBy) || migration.active.requestedBy}</span>
                                    <span>开始于 {formatTime(migration.active.startedAt || migration.active.createdAt)}</span>
                                </div>
                            </div>
                        ) : null}
                        {migration ? (
                            <div className="storage-migration-metrics grid gap-px overflow-hidden bg-border/60 sm:grid-cols-3">
                                <MigrationMetric label="待迁移资源" value={`${migration.eligible.items} 个`} />
                                <MigrationMetric label="待迁移容量" value={formatBytes(migration.eligible.bytes)} />
                                <MigrationMetric label="当前目标" value={setting?.enabled ? `${setting.bucket}/${setting.pathPrefix || ""}`.replace(/\/$/, "") : "OSS 未启用"} />
                            </div>
                        ) : null}
                        {migration?.latest ? (
                            <div className="storage-migration-progress space-y-2">
                                <div className="storage-migration-progress-heading flex items-center justify-between gap-3 text-xs">
                                    <span className="font-medium">最近任务 · {migration.latest.id}</span>
                                    <span className="text-foreground/50">
                                        {migration.latest.committedItems}/{migration.latest.totalItems} · 失败 {migration.latest.failedItems}
                                    </span>
                                </div>
                                <Progress
                                    percent={migrationPercent(migration.latest.committedItems, migration.latest.failedItems, migration.latest.totalItems)}
                                    status={migration.latest.status === "failed" || migration.latest.status === "partial_failed" ? "exception" : migration.latest.status === "succeeded" ? "success" : "active"}
                                    showInfo={false}
                                />
                                {migration.latest.error ? <Alert type="error" showIcon message={migration.latest.error} /> : null}
                            </div>
                        ) : null}
                        {migration?.items.some((item) => item.status === "failed") ? (
                            <div className="storage-migration-failures space-y-2">
                                <div className="text-xs font-medium">失败资源</div>
                                {migration.items
                                    .filter((item) => item.status === "failed")
                                    .slice(0, 5)
                                    .map((item) => (
                                        <div key={item.id} className="storage-migration-failure grid gap-1 bg-destructive/[.045] px-3 py-2 text-xs sm:grid-cols-[minmax(0,1fr)_auto]">
                                            <span className="truncate" title={item.sourceObjectKey}>
                                                {item.sourceObjectKey}
                                            </span>
                                            <span className="text-destructive">{item.error || "未知错误"}</span>
                                        </div>
                                    ))}
                            </div>
                        ) : null}
                    </div>
                </SettingsSectionCard>
                <div className="admin-storage-notices grid gap-1 text-xs text-foreground/55 sm:grid-cols-3">
                    <Notice icon={<Cloud className="size-3.5" />} text="新资源优先上传 OSS" />
                    <Notice icon={<ShieldCheck className="size-3.5" />} text="AccessKey Secret 不回显" />
                    <Notice icon={<KeyRound className="size-3.5" />} text="存储异常时明确返回错误" />
                </div>
            </div>
            <Modal
                className="admin-operation-modal admin-storage-migration-modal workspace-ui-scope"
                title="迁移历史本地资源"
                open={migrationOpen}
                okText="创建迁移任务"
                cancelText="取消"
                confirmLoading={migrationSubmitting}
                closable={!migrationSubmitting}
                maskClosable={!migrationSubmitting}
                keyboard={!migrationSubmitting}
                okButtonProps={{ disabled: migrationConfirmation !== "MIGRATE LOCAL TO OSS" }}
                onOk={() => void startMigration()}
                onCancel={() => {
                    if (migrationSubmitting) return;
                    setMigrationOpen(false);
                    setMigrationConfirmation("");
                }}
            >
                <div className="storage-migration-confirm space-y-4">
                    <Alert
                        type="info"
                        showIcon
                        message={`将复制 ${migration?.eligible.items || 0} 个资源（${formatBytes(migration?.eligible.bytes || 0)}）到 ${setting?.bucket || "OSS"}`}
                        description="任务逐文件校验大小和 ETag 后才更新数据库；不会删除数据卷中的原始文件。"
                    />
                    <div className="storage-migration-confirm-field">
                        <label className="mb-1.5 block text-xs font-medium" htmlFor="storage-migration-confirmation">
                            输入确认短语
                        </label>
                        <Input id="storage-migration-confirmation" value={migrationConfirmation} placeholder="MIGRATE LOCAL TO OSS" onChange={(event) => setMigrationConfirmation(event.target.value)} />
                    </div>
                </div>
            </Modal>
            <Modal
                className="admin-operation-modal admin-storage-retry-modal workspace-ui-scope"
                title="重试迁移失败资源"
                open={retryOpen}
                okText="确认重试"
                cancelText="取消"
                confirmLoading={migrationSubmitting}
                closable={!migrationSubmitting}
                maskClosable={!migrationSubmitting}
                keyboard={!migrationSubmitting}
                okButtonProps={{ disabled: !migration?.latest || retryConfirmation !== `RETRY ${migration.latest.id}` }}
                onOk={() => void retryMigration()}
                onCancel={() => {
                    if (migrationSubmitting) return;
                    setRetryOpen(false);
                    setRetryConfirmation("");
                }}
            >
                <div className="storage-migration-confirm space-y-4">
                    <Alert
                        type="warning"
                        showIcon
                        message={`将重新提交 ${migration?.latest?.failedItems || 0} 个失败资源`}
                        description="只重试最近任务中的失败项；已迁移成功的资源不会重复写入。"
                    />
                    <div className="storage-migration-confirm-field">
                        <label className="mb-1.5 block text-xs font-medium" htmlFor="storage-migration-retry-confirmation">
                            输入确认短语 {migration?.latest ? `RETRY ${migration.latest.id}` : ""}
                        </label>
                        <Input
                            id="storage-migration-retry-confirmation"
                            value={retryConfirmation}
                            placeholder={migration?.latest ? `RETRY ${migration.latest.id}` : ""}
                            onChange={(event) => setRetryConfirmation(event.target.value)}
                        />
                    </div>
                </div>
            </Modal>
        </AdminPageFrame>
    );
}

function validateStorageField(values: OSSFormValues, hasSavedSecret: boolean, field: keyof OSSFormValues, value?: string): Promise<void> {
    const errors = storageFieldErrors({ ...values, [field]: value ?? values[field] }, hasSavedSecret);
    return errors[field] ? Promise.reject(new Error(errors[field])) : Promise.resolve();
}

function formatTime(value?: string) {
    return value ? new Date(value).toLocaleString("zh-CN", { hour12: false }) : "--";
}

function Notice({ icon, text }: { icon: ReactNode; text: string }) {
    return (
        <div className="admin-storage-notice flex items-center gap-2 bg-foreground/[.025] px-3 py-2.5">
            <span className="admin-storage-notice-icon text-foreground/40">{icon}</span>
            <span className="admin-storage-notice-text">{text}</span>
        </div>
    );
}

function MigrationMetric({ label, value }: { label: string; value: string }) {
    return (
        <div className="storage-migration-metric min-w-0 bg-background px-4 py-3">
            <div className="storage-migration-metric-label text-[11px] text-foreground/45">{label}</div>
            <div className="storage-migration-metric-value mt-1 truncate text-sm font-semibold" title={value}>
                {value}
            </div>
        </div>
    );
}

function migrationPercent(committed: number, failed: number, total: number) {
    return total > 0 ? Math.min(100, Math.round(((committed + failed) / total) * 100)) : 0;
}

function formatBytes(value: number) {
    if (value < 1024) return `${value} B`;
    const units = ["KB", "MB", "GB", "TB"];
    let size = value / 1024;
    let index = 0;
    while (size >= 1024 && index < units.length - 1) {
        size /= 1024;
        index++;
    }
    return `${size.toFixed(size >= 100 ? 0 : size >= 10 ? 1 : 2)} ${units[index]}`;
}

function MigrationStatusTag({ status }: { status: StorageMigrationStatus }) {
    const labels: Record<StorageMigrationStatus, string> = {
        preparing: "准备中",
        queued: "等待执行",
        running: "迁移中",
        succeeded: "迁移完成",
        partial_failed: "部分失败",
        failed: "迁移失败",
    };
    const color = status === "succeeded" ? "success" : status === "failed" || status === "partial_failed" ? "error" : "processing";
    return (
        <Tag className="storage-migration-status-tag" color={color} variant="filled">
            {labels[status]}
        </Tag>
    );
}
