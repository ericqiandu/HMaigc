import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { App, Button, Tabs, Tag } from "antd";
import { ExternalLink, Save, ShieldCheck } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { Link, useBlocker } from "react-router";

import { legalDocumentDefinitions, type LegalDocumentKind } from "@/constants/legal-documents";
import { adminSiteSettingsQueryKey, getAdminSiteSettings, publicSiteSettingsQueryKey, updateAdminLegalSettings, type SiteSettings } from "@/services/api/site-settings";
import { AdminPageFrame } from "../components/admin-shell";
import { AdminContentError, AdminContentSkeleton, SettingsSectionCard } from "../components/admin-ui";
import { LegalRichTextEditor } from "../components/legal-rich-text-editor";
import { AIWatermarkPolicyEditor } from "./ai-watermark-policy-editor";
import { emptyLegalDraft, legalDraftsEqual, normalizeLegalDraft, type LegalDraft } from "./legal-draft";

export default function LegalSettingsPage() {
    const { message, modal } = App.useApp();
    const queryClient = useQueryClient();
    const [draft, setDraft] = useState<LegalDraft>(emptyLegalDraft);
    const [baseline, setBaseline] = useState<LegalDraft>(emptyLegalDraft);
    const [characterCounts, setCharacterCounts] = useState<Record<LegalDocumentKind, number>>({ userAgreement: 0, privacyPolicy: 0, membershipAgreement: 0 });
    const settingQuery = useQuery({ queryKey: adminSiteSettingsQueryKey, queryFn: getAdminSiteSettings });
    const dirty = useMemo(() => !legalDraftsEqual(draft, baseline), [baseline, draft]);

    useEffect(() => {
        if (!settingQuery.data || dirty) return;
        const synchronized = legalDraftFromSetting(settingQuery.data);
        setDraft(synchronized);
        setBaseline(synchronized);
    }, [dirty, settingQuery.data]);

    const saveMutation = useMutation({
        mutationFn: updateAdminLegalSettings,
        onSuccess: (setting) => {
            synchronizeSetting(queryClient, setting);
            const synchronized = legalDraftFromSetting(setting);
            setDraft(synchronized);
            setBaseline(synchronized);
            message.success("法律内容已保存并同步到公开页面");
        },
        onError: (error) => message.error(error instanceof Error ? error.message : "保存法律内容失败"),
    });
    const blocker = useBlocker(dirty && !saveMutation.isPending);

    const updateDraft = (field: keyof LegalDraft) => (html: string, characterCount: number) => {
        setCharacterCounts((current) => ({ ...current, [field]: characterCount }));
        if (draft[field] === html) return;
        setDraft((current) => ({ ...current, [field]: html }));
    };

    const initializeCharacterCount = (field: keyof LegalDraft) => (characterCount: number) => {
        setCharacterCounts((current) => ({ ...current, [field]: characterCount }));
    };

    const configuredCount = legalDocumentDefinitions.reduce((count, document) => count + Number(characterCounts[document.kind] > 0), 0);
    const setting = settingQuery.data;

    useEffect(() => {
        const beforeUnload = (event: BeforeUnloadEvent) => {
            if (!dirty || saveMutation.isPending) return;
            event.preventDefault();
        };
        window.addEventListener("beforeunload", beforeUnload);
        return () => window.removeEventListener("beforeunload", beforeUnload);
    }, [dirty, saveMutation.isPending]);

    useEffect(() => {
        if (blocker.state !== "blocked") return;
        const instance = modal.confirm({
            title: "离开并放弃未保存的法律内容？",
            content: "当前用户协议、隐私政策或会员服务协议尚未保存，离开后本次修改将丢失。",
            okText: "放弃修改并离开",
            cancelText: "继续编辑",
            okButtonProps: { danger: true },
            onOk: () => blocker.proceed(),
            onCancel: () => blocker.reset(),
        });
        return () => instance.destroy();
    }, [blocker, modal]);

    return (
        <AdminPageFrame title="法律与协议" description="独立维护用户协议、隐私政策、会员服务协议及其公开展示内容">
            <div className="legal-settings-page admin-settings-page space-y-5">
                <AIWatermarkPolicyEditor />
                {settingQuery.isLoading ? (
                    <AdminContentSkeleton rows={16} label="正在加载法律内容" />
                ) : settingQuery.error && !setting ? (
                    <AdminContentError title="法律内容加载失败" description={settingQuery.error instanceof Error ? settingQuery.error.message : "请稍后重试"} onRetry={() => void settingQuery.refetch()} />
                ) : setting ? (
                    <>
                        {settingQuery.error ? (
                            <AdminContentError title="法律内容刷新失败" description={`${settingQuery.error instanceof Error ? settingQuery.error.message : "请稍后重试"}；当前继续显示上次成功读取的内容。`} onRetry={() => void settingQuery.refetch()} />
                        ) : null}
                        <SettingsSectionCard
                            className="legal-settings-editor-card"
                            icon={<ShieldCheck className="legal-settings-card-icon size-4" />}
                            title="公开法律文档"
                            description="三份文档分别发布到公开页面；会员服务协议未发布时不阻止订单创建或付款。"
                            status={
                                <div className="legal-settings-header-actions">
                                    <Tag className="legal-settings-status" color={configuredCount === legalDocumentDefinitions.length ? "success" : "warning"}>
                                        {configuredCount === legalDocumentDefinitions.length ? "内容完整" : `${configuredCount}/${legalDocumentDefinitions.length} 已配置`}
                                    </Tag>
                                    <Button
                                        className="legal-settings-save-button"
                                        type="primary"
                                        icon={<Save className="legal-settings-save-icon size-4" />}
                                        loading={saveMutation.isPending}
                                        disabled={!dirty}
                                        onClick={() => saveMutation.mutate(normalizeLegalDraft(draft))}
                                    >
                                        保存并发布
                                    </Button>
                                </div>
                            }
                        >
                            <Tabs
                                className="legal-settings-tabs"
                                defaultActiveKey="userAgreement"
                                items={legalDocumentDefinitions.map((document) => ({
                                    key: document.kind,
                                    label: document.tabLabel,
                                    forceRender: true,
                                    children: (
                                        <LegalDocumentPane title={document.title} description={document.editorDescription} previewPath={document.route}>
                                            <LegalRichTextEditor
                                                value={draft[document.kind]}
                                                placeholder={document.editorPlaceholder}
                                                disabled={saveMutation.isPending}
                                                onReady={initializeCharacterCount(document.kind)}
                                                onChange={updateDraft(document.kind)}
                                            />
                                        </LegalDocumentPane>
                                    ),
                                }))}
                            />
                            <div className="legal-settings-sync-status" role="status">
                                <ShieldCheck className="legal-settings-sync-icon size-3.5" />
                                <span className="legal-settings-sync-label">{dirty ? "有未保存的法律内容" : "公开内容已同步"}</span>
                                <span className="legal-settings-sync-meta">{setting.updatedAt ? `上次更新：${new Date(setting.updatedAt).toLocaleString("zh-CN", { hour12: false })}` : "尚未保存法律内容"}</span>
                            </div>
                        </SettingsSectionCard>
                    </>
                ) : null}
            </div>
        </AdminPageFrame>
    );
}

function LegalDocumentPane({ title, description, previewPath, children }: { title: string; description: string; previewPath: string; children: React.ReactNode }) {
    return (
        <div className="legal-document-pane">
            <div className="legal-document-pane-heading flex flex-wrap items-start justify-between gap-4">
                <div className="legal-document-pane-copy min-w-0">
                    <h3 className="legal-document-pane-title">{title}</h3>
                    <p className="legal-document-pane-description">{description}</p>
                </div>
                <Link className="legal-document-preview-link" to={previewPath} target="_blank" rel="noreferrer">
                    <ExternalLink className="legal-document-preview-icon size-3.5" />
                    预览公开页
                </Link>
            </div>
            <div className="legal-document-pane-editor">{children}</div>
        </div>
    );
}

function synchronizeSetting(queryClient: ReturnType<typeof useQueryClient>, setting: SiteSettings) {
    queryClient.setQueryData(adminSiteSettingsQueryKey, setting);
    queryClient.setQueryData(publicSiteSettingsQueryKey, setting);
}

function legalDraftFromSetting(setting: SiteSettings): LegalDraft {
    return normalizeLegalDraft({
        userAgreement: setting.userAgreement,
        privacyPolicy: setting.privacyPolicy,
        membershipAgreement: setting.membershipAgreement,
    });
}
