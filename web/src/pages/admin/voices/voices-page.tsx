import { App, Button, Drawer, Form, Input, Select, Space, Switch, Table, Tag } from "antd";
import type { ColumnsType } from "antd/es/table";
import { AudioLines, CloudDownload, Dna, Pencil, Plus, Trash2 } from "lucide-react";
import { useEffect, useMemo, useState } from "react";

import { ListToolbar, TableSurface } from "@/components/layout/workspace-page";
import { refreshSystemChannels } from "@/lib/user-session";
import { listAdminChannels } from "@/services/api/auth";
import {
    cloneAdminChannelVoice,
    createAdminChannelVoice,
    deleteAdminChannelVoice,
    listAdminChannelVoices,
    syncAdminChannelVoices,
    updateAdminChannelVoice,
    type ChannelVoiceInput,
} from "@/services/api/voices";
import type { ChannelVoice, ModelChannel } from "@/stores/use-config-store";
import { AdminPageFrame } from "../components/admin-shell";
import { AdminRowActions, AdminTableEmpty, AdminTableSkeleton } from "../components/admin-ui";

type VoiceFormValues = ChannelVoiceInput & { consentConfirmed: boolean };
type EditorMode = "manual" | "clone";

const voiceKindLabels: Record<ChannelVoice["kind"], string> = {
    system: "系统音色",
    voice_cloning: "克隆音色",
    voice_generation: "生成音色",
};

export default function VoicesPage() {
    const { message, modal } = App.useApp();
    const [channels, setChannels] = useState<ModelChannel[]>([]);
    const [channelId, setChannelId] = useState("");
    const [voices, setVoices] = useState<ChannelVoice[]>([]);
    const [loading, setLoading] = useState(true);
    const [saving, setSaving] = useState(false);
    const [editorMode, setEditorMode] = useState<EditorMode>("manual");
    const [editing, setEditing] = useState<ChannelVoice | null>(null);
    const [drawerOpen, setDrawerOpen] = useState(false);
    const [cloneFile, setCloneFile] = useState<File | null>(null);
    const [cloneIdempotencyKey, setCloneIdempotencyKey] = useState("");
    const [form] = Form.useForm<VoiceFormValues>();
    const selectedChannel = channels.find((channel) => channel.id === channelId);
    const modelOptions = useMemo(
        () => (selectedChannel?.models || []).map((model) => ({ label: model, value: model })),
        [selectedChannel?.models],
    );

    const loadChannels = async () => {
        const result = await listAdminChannels({ interfaceType: "minimax-speech", page: 1, limit: 100 });
        setChannels(result.channels);
        setChannelId((current) => current || result.channels[0]?.id || "");
    };

    const loadVoices = async (targetChannelId: string) => {
        if (!targetChannelId) {
            setVoices([]);
            setLoading(false);
            return;
        }
        setLoading(true);
        try {
            const result = await listAdminChannelVoices(targetChannelId);
            setVoices(result.voices);
        } catch (error) {
            message.error(error instanceof Error ? error.message : "读取音色目录失败");
        } finally {
            setLoading(false);
        }
    };

    useEffect(() => {
        void loadChannels().catch((error: unknown) => {
            setLoading(false);
            message.error(error instanceof Error ? error.message : "读取 MiniMax Speech 渠道失败");
        });
    }, []);

    useEffect(() => {
        void loadVoices(channelId);
    }, [channelId]);

    const openEditor = (mode: EditorMode, voice?: ChannelVoice) => {
        setEditorMode(mode);
        setEditing(voice || null);
        setCloneFile(null);
        setCloneIdempotencyKey(mode === "clone" ? crypto.randomUUID() : "");
        form.setFieldsValue({
            voiceKey: voice?.voiceKey || "",
            displayName: voice?.displayName || "",
            description: voice?.description || "",
            language: voice?.language || "zh-CN",
            kind: voice?.kind || (mode === "clone" ? "voice_cloning" : "system"),
            accessPolicy: voice?.accessPolicy || "authenticated",
            compatibleModels: voice?.compatibleModels || [],
            enabled: voice?.enabled !== false,
            consentConfirmed: false,
        });
        setDrawerOpen(true);
    };

    const publishCatalog = async () => {
        await refreshSystemChannels();
    };

    const save = async () => {
        if (!channelId) {
            message.error("请先创建 MiniMax Speech 渠道");
            return;
        }
        const values = await form.validateFields();
        setSaving(true);
        try {
            if (editorMode === "clone") {
                if (!cloneFile) throw new Error("请选择 10 秒至 5 分钟、20MB 以内的 MP3、M4A 或 WAV 文件");
                if (!cloneIdempotencyKey) throw new Error("克隆请求缺少幂等键，请关闭后重新打开克隆窗口");
                await validateCloneAudioFile(cloneFile);
                await cloneAdminChannelVoice(channelId, {
                    ...values,
                    kind: "voice_cloning",
                    file: cloneFile,
                    idempotencyKey: cloneIdempotencyKey,
                });
                message.success("克隆音色已创建；首次正式合成后会在供应商音色列表中激活");
            } else if (editing) {
                await updateAdminChannelVoice(channelId, editing.id, values);
                message.success("音色配置已更新");
            } else {
                await createAdminChannelVoice(channelId, values);
                message.success("音色已加入目录");
            }
            setDrawerOpen(false);
            setCloneIdempotencyKey("");
            await Promise.all([loadVoices(channelId), publishCatalog()]);
        } catch (error) {
            message.error(error instanceof Error ? error.message : "保存音色失败");
        } finally {
            setSaving(false);
        }
    };

    const sync = async () => {
        if (!channelId) return;
        setLoading(true);
        try {
            const result = await syncAdminChannelVoices(channelId);
            setVoices(result.voices);
            await publishCatalog();
            message.success(`已同步 ${result.voices.length} 个音色`);
        } catch (error) {
            message.error(error instanceof Error ? error.message : "同步音色失败");
        } finally {
            setLoading(false);
        }
    };

    const remove = (voice: ChannelVoice) => {
        modal.confirm({
            title: `删除音色“${voice.displayName}”？`,
            content: voice.kind === "system" ? "系统音色只会从本地目录移除。" : "该操作会先删除 MiniMax 上游音色，成功后再删除本地目录，无法撤销。",
            okText: "确认删除",
            okButtonProps: { danger: true },
            cancelText: "取消",
            onOk: async () => {
                await deleteAdminChannelVoice(channelId, voice.id);
                await Promise.all([loadVoices(channelId), publishCatalog()]);
                message.success("音色已删除");
            },
        });
    };

    const columns: ColumnsType<ChannelVoice> = [
        {
            title: "音色",
            dataIndex: "displayName",
            render: (_, voice) => (
                <div className="admin-voice-name-cell">
                    <AudioLines className="admin-voice-name-icon size-4" />
                    <div className="admin-voice-name-copy">
                        <strong className="admin-voice-display-name">{voice.displayName}</strong>
                        <span className="admin-voice-key">{voice.voiceKey}</span>
                    </div>
                </div>
            ),
        },
        { title: "类型", dataIndex: "kind", width: 120, render: (kind: ChannelVoice["kind"]) => <Tag className="admin-voice-kind-tag">{voiceKindLabels[kind]}</Tag> },
        { title: "语言", dataIndex: "language", width: 110, render: (value: string) => value || "未指定" },
        { title: "权限", dataIndex: "accessPolicy", width: 110, render: (value: ChannelVoice["accessPolicy"]) => <Tag className="admin-voice-access-tag" color={value === "member" ? "gold" : "default"}>{value === "member" ? "会员专属" : "登录用户"}</Tag> },
        {
            title: "供应商状态",
            dataIndex: "providerStatus",
            width: 180,
            render: (value: ChannelVoice["providerStatus"], voice) => (
                <div className="admin-voice-status-cell flex min-w-0 flex-col items-start gap-1">
                    <Tag className="admin-voice-status-tag" color={value === "active" ? "green" : value === "pending_activation" ? "blue" : value === "failed" || value === "uncertain" || value === "missing" ? "red" : "default"}>{value}</Tag>
                    {voice.lastError ? <span className="admin-voice-status-error block max-w-[160px] truncate text-[11px] text-red-500/80" title={voice.lastError}>{voice.lastError}</span> : null}
                </div>
            ),
        },
        {
            title: "操作",
            key: "actions",
            width: 116,
            render: (_, voice) => (
                <AdminRowActions
                    actions={[
                        { key: "edit", label: "编辑", icon: <Pencil className="admin-voice-action-icon size-3.5" />, onClick: () => openEditor("manual", voice) },
                        { key: "delete", label: "删除", icon: <Trash2 className="admin-voice-action-icon size-3.5" />, danger: true, onClick: () => remove(voice) },
                    ]}
                />
            ),
        },
    ];

    return (
        <AdminPageFrame
            title="音色管理"
            description="统一维护 MiniMax 系统音色、克隆音色、模型兼容范围和会员访问权限；用户端仅使用这里发布的音色。"
            actions={(
                <Space className="admin-voice-page-actions">
                    <Button className="admin-voice-sync-button" icon={<CloudDownload className="admin-voice-button-icon size-4" />} disabled={!channelId} loading={loading} onClick={() => void sync()}>同步供应商</Button>
                    <Button className="admin-voice-clone-button" icon={<Dna className="admin-voice-button-icon size-4" />} disabled={!channelId} onClick={() => openEditor("clone")}>克隆音色</Button>
                    <Button className="admin-voice-create-button" type="primary" icon={<Plus className="admin-voice-button-icon size-4" />} disabled={!channelId} onClick={() => openEditor("manual")}>新增目录音色</Button>
                </Space>
            )}
        >
            <div className="admin-voice-toolbar">
                <ListToolbar>
                    <Select
                        className="admin-voice-channel-select min-w-72"
                        aria-label="选择 MiniMax Speech 渠道"
                        placeholder="请先创建 MiniMax Speech 渠道"
                        value={channelId || undefined}
                        options={channels.map((channel) => ({ label: channel.name, value: channel.id }))}
                        onChange={setChannelId}
                    />
                </ListToolbar>
            </div>
            <div className="admin-voice-table-surface">
                <TableSurface>
                <Table<ChannelVoice>
                    className="admin-voice-table"
                    rowKey="id"
                    columns={columns}
                    dataSource={voices}
                    loading={loading ? { indicator: <AdminTableSkeleton /> } : false}
                    locale={{ emptyText: <AdminTableEmpty title={channels.length ? "尚未发布音色" : "尚未创建 MiniMax Speech 渠道"} description={channels.length ? "可同步供应商音色或新增目录音色。" : "请先在 AI 模型配置中创建渠道并添加音频模型与积分价格。"} /> }}
                    pagination={false}
                />
                </TableSurface>
            </div>
            <Drawer
                className="admin-voice-editor"
                title={editorMode === "clone" ? "克隆 MiniMax 音色" : editing ? "编辑音色" : "新增目录音色"}
                open={drawerOpen}
                width={560}
                destroyOnHidden
                onClose={() => setDrawerOpen(false)}
                extra={<Button className="admin-voice-save-button" type="primary" loading={saving} onClick={() => void save()}>保存并发布</Button>}
            >
                <Form<VoiceFormValues> className="admin-voice-form" form={form} layout="vertical" requiredMark={false}>
                    <Form.Item className="admin-voice-form-item" name="voiceKey" label="音色标识" rules={[{ required: true, message: "请输入音色标识" }]}>
                        <Input className="admin-voice-input" disabled={Boolean(editing)} placeholder="例如 HMVoice001" />
                    </Form.Item>
                    <Form.Item className="admin-voice-form-item" name="displayName" label="展示名称" rules={[{ required: true, message: "请输入展示名称" }]}>
                        <Input className="admin-voice-input" placeholder="用户端看到的音色名称" />
                    </Form.Item>
                    <Form.Item className="admin-voice-form-item" name="description" label="音色说明">
                        <Input.TextArea className="admin-voice-textarea" rows={3} maxLength={500} showCount placeholder="音色特点、适用角色与情绪说明" />
                    </Form.Item>
                    <Form.Item className="admin-voice-form-item" name="language" label="语言">
                        <Input className="admin-voice-input" placeholder="例如 zh-CN" />
                    </Form.Item>
                    {editorMode === "manual" ? (
                        <Form.Item className="admin-voice-form-item" name="kind" label="音色类型" rules={[{ required: true }]}>
                            <Select className="admin-voice-select" options={Object.entries(voiceKindLabels).map(([value, label]) => ({ value, label }))} />
                        </Form.Item>
                    ) : null}
                    <Form.Item className="admin-voice-form-item" name="accessPolicy" label="访问权限" rules={[{ required: true }]}>
                        <Select className="admin-voice-select" options={[{ label: "登录用户", value: "authenticated" }, { label: "会员专属", value: "member" }]} />
                    </Form.Item>
                    <Form.Item className="admin-voice-form-item" name="compatibleModels" label="兼容模型" extra="不选择表示兼容该渠道全部音频模型。">
                        <Select className="admin-voice-model-select" mode="multiple" options={modelOptions} placeholder="全部模型" />
                    </Form.Item>
                    {editorMode === "clone" ? (
                        <div className="admin-voice-clone-fields">
                            <label className="admin-voice-file-label" htmlFor="admin-voice-clone-file">声音样本</label>
                            <input
                                id="admin-voice-clone-file"
                                className="admin-voice-file-input"
                                type="file"
                                accept=".mp3,.m4a,.wav,audio/mpeg,audio/mp4,audio/wav"
                                onChange={(event) => setCloneFile(event.target.files?.[0] || null)}
                            />
                            <p className="admin-voice-file-help">MiniMax 要求 10 秒至 5 分钟、20MB 以内；源文件只上传供应商，本系统仅留文件名、大小和 SHA-256 审计摘要。</p>
                            <Form.Item className="admin-voice-consent-item" name="consentConfirmed" valuePropName="checked" rules={[{ validator: (_, checked: boolean) => checked ? Promise.resolve() : Promise.reject(new Error("必须确认声音授权")) }]}>
                                <Switch className="admin-voice-consent-switch" checkedChildren="已授权" unCheckedChildren="确认授权" />
                            </Form.Item>
                        </div>
                    ) : (
                        <Form.Item className="admin-voice-form-item" name="enabled" label="发布状态" valuePropName="checked">
                            <Switch className="admin-voice-enabled-switch" checkedChildren="已发布" unCheckedChildren="已停用" />
                        </Form.Item>
                    )}
                </Form>
            </Drawer>
        </AdminPageFrame>
    );
}

async function validateCloneAudioFile(file: File) {
    if (file.size <= 0) throw new Error("声音样本不能为空");
    if (file.size > 20 * 1024 * 1024) throw new Error("声音样本不能超过 20MB");
    const extension = file.name.split(".").pop()?.toLocaleLowerCase();
    if (extension !== "mp3" && extension !== "m4a" && extension !== "wav") {
        throw new Error("声音样本仅支持 MP3、M4A 或 WAV");
    }
    const objectURL = URL.createObjectURL(file);
    try {
        const duration = await new Promise<number>((resolve, reject) => {
            const audio = new Audio();
            const timeout = window.setTimeout(() => reject(new Error("读取声音样本时长超时")), 10_000);
            audio.preload = "metadata";
            audio.onloadedmetadata = () => {
                window.clearTimeout(timeout);
                resolve(audio.duration);
            };
            audio.onerror = () => {
                window.clearTimeout(timeout);
                reject(new Error("无法读取声音样本，请确认文件未损坏且编码受支持"));
            };
            audio.src = objectURL;
        });
        if (!Number.isFinite(duration) || duration < 10 || duration > 300) {
            throw new Error("声音样本时长必须在 10 秒至 5 分钟之间");
        }
    } finally {
        URL.revokeObjectURL(objectURL);
    }
}
