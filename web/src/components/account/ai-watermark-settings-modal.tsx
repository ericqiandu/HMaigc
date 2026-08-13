import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Alert, App, Button, Modal, Skeleton, Switch } from "antd";
import { ExternalLink, Stamp } from "lucide-react";
import { useEffect, useState } from "react";

import { LegalRichTextViewer } from "@/components/legal/legal-rich-text-viewer";
import { WatermarkPolicyConflictError, watermarkPolicyApi, watermarkPreferenceQueryKey, type WatermarkPolicyApi } from "@/services/api/watermark-policy";
import "./ai-watermark-settings-modal.css";

export function AIWatermarkSettingsModal({ open, onClose, api = watermarkPolicyApi }: { open: boolean; onClose: () => void; api?: WatermarkPolicyApi }) {
    const { message } = App.useApp();
    const queryClient = useQueryClient();
    const query = useQuery({ queryKey: watermarkPreferenceQueryKey, queryFn: api.getPreference, enabled: open });
    const [removeWatermark, setRemoveWatermark] = useState(false);
    const [saveError, setSaveError] = useState("");
    const [draftDirty, setDraftDirty] = useState(false);
    useEffect(() => {
        if (query.data && !draftDirty) setRemoveWatermark(query.data.removeWatermark);
    }, [draftDirty, query.data]);
    const closeAndDiscardDraft = () => {
        if (mutation.isPending) return;
        setRemoveWatermark(query.data?.removeWatermark ?? false);
        setDraftDirty(false);
        setSaveError("");
        onClose();
    };
    const mutation = useMutation({
        mutationFn: () => api.updatePreference({ removeWatermark, publicationId: removeWatermark ? query.data?.currentPolicy?.id || "" : "" }),
        onSuccess: (view) => {
            queryClient.setQueryData(watermarkPreferenceQueryKey, view);
            setSaveError("");
            setDraftDirty(false);
            void message.success("AI 水印设置已同步");
            onClose();
        },
        onError: async (error) => {
            setSaveError(error instanceof WatermarkPolicyConflictError ? "水印规范已更新，请重新阅读并确认" : error instanceof Error ? error.message : "保存设置失败");
            if (error instanceof WatermarkPolicyConflictError) await query.refetch();
        },
    });
    const view = query.data;
    const switchDisabled = mutation.isPending || !view?.canEnable || query.isLoading || Boolean(query.error && !view);
    const unchanged = Boolean(view && removeWatermark === view.removeWatermark);
    return (
        <Modal
            className="ai-watermark-settings-modal"
            rootClassName="workspace-ui-scope"
            title="AI 生成内容水印管理规则"
            open={open}
            width={740}
            centered
            keyboard
            destroyOnHidden
            mask={{ closable: !mutation.isPending }}
            closable={{ disabled: mutation.isPending }}
            onCancel={closeAndDiscardDraft}
            footer={null}
        >
            <div className="ai-watermark-settings-content">
                {query.isLoading && !view ? <Skeleton active paragraph={{ rows: 8 }} aria-label="正在读取 AI 水印规则" /> : null}
                {query.error && !view ? <Alert type="error" showIcon title="水印设置加载失败" description={query.error instanceof Error ? query.error.message : "请稍后重试"} action={<Button onClick={() => void query.refetch()}>重试</Button>} /> : null}
                {view ? (
                    <>
                        <p className="ai-watermark-settings-intro">使用去 AI 水印前，请阅读并同意当前管理规则；账号设置会同步到所有支持水印控制的模型。</p>
                        {view.currentPolicy ? (
                            <div className="ai-watermark-settings-rule">
                                <LegalRichTextViewer content={view.currentPolicy.managementRuleRichText} />
                            </div>
                        ) : (
                            <Alert type="warning" showIcon title="当前尚未发布可用的水印规范" />
                        )}
                        {view.currentPolicy ? (
                            <a className="ai-watermark-settings-link" href={view.currentPolicy.watermarkPolicyUrl} target="_blank" rel="noopener noreferrer">
                                <ExternalLink className="size-3.5" aria-hidden />
                                水印规范
                            </a>
                        ) : null}
                        {view.status === "policy_updated" ? <Alert type="warning" showIcon title="水印规范已更新，请重新阅读并确认" /> : null}
                        {saveError ? <Alert type="error" showIcon title={saveError} /> : null}
                        <section className="ai-watermark-settings-control" aria-labelledby="ai-watermark-toggle-title">
                            <Stamp className="ai-watermark-settings-control-icon size-5" aria-hidden />
                            <div className="ai-watermark-settings-control-copy">
                                <h3 id="ai-watermark-toggle-title">去 AI 水印</h3>
                                <p>开启后，支持该能力的模型将按账号偏好生成无水印内容；其他模型仍由服务商决定。</p>
                            </div>
                            <Switch
                                checked={removeWatermark}
                                disabled={switchDisabled}
                                onChange={(checked) => {
                                    setRemoveWatermark(checked);
                                    setDraftDirty(true);
                                }}
                                aria-label="去 AI 水印"
                            />
                        </section>
                        <div className="ai-watermark-settings-actions">
                            <Button onClick={closeAndDiscardDraft} disabled={mutation.isPending}>
                                取消
                            </Button>
                            <Button type="primary" loading={mutation.isPending} disabled={unchanged || switchDisabled} onClick={() => mutation.mutate()}>
                                保存设置
                            </Button>
                        </div>
                    </>
                ) : null}
            </div>
        </Modal>
    );
}
