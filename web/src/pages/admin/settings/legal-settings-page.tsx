import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Alert, App, Button, Skeleton, Tabs, Tag } from "antd";
import { ExternalLink, FileCheck2, RefreshCw, Save, ShieldCheck } from "lucide-react";
import { useEffect, useState } from "react";
import { Link } from "react-router";

import { adminSiteSettingsQueryKey, getAdminSiteSettings, publicSiteSettingsQueryKey, updateAdminLegalSettings, type SiteSettings } from "@/services/api/site-settings";
import { AdminPageFrame } from "../components/admin-shell";
import { AdminSettingsActionBar, SettingsSectionCard } from "../components/admin-ui";
import { LegalRichTextEditor } from "../components/legal-rich-text-editor";

type LegalDraft = {
    userAgreement: string;
    privacyPolicy: string;
};

const emptyDraft: LegalDraft = { userAgreement: "", privacyPolicy: "" };

export default function LegalSettingsPage() {
    const { message } = App.useApp();
    const queryClient = useQueryClient();
    const [draft, setDraft] = useState<LegalDraft>(emptyDraft);
    const [characterCounts, setCharacterCounts] = useState({ userAgreement: 0, privacyPolicy: 0 });
    const [dirty, setDirty] = useState(false);
    const settingQuery = useQuery({ queryKey: adminSiteSettingsQueryKey, queryFn: getAdminSiteSettings });

    useEffect(() => {
        if (!settingQuery.data || dirty) return;
        setDraft({ userAgreement: settingQuery.data.userAgreement, privacyPolicy: settingQuery.data.privacyPolicy });
    }, [dirty, settingQuery.data]);

    const saveMutation = useMutation({
        mutationFn: updateAdminLegalSettings,
        onSuccess: (setting) => {
            synchronizeSetting(queryClient, setting);
            setDraft({ userAgreement: setting.userAgreement, privacyPolicy: setting.privacyPolicy });
            setDirty(false);
            message.success("法律内容已保存并同步到公开页面");
        },
        onError: (error) => message.error(error instanceof Error ? error.message : "保存法律内容失败"),
    });

    const updateDraft = (field: keyof LegalDraft) => (html: string, characterCount: number) => {
        setCharacterCounts((current) => ({ ...current, [field]: characterCount }));
        if (draft[field] === html) return;
        setDraft((current) => ({ ...current, [field]: html }));
        setDirty(true);
    };

    const initializeCharacterCount = (field: keyof LegalDraft) => (characterCount: number) => {
        setCharacterCounts((current) => ({ ...current, [field]: characterCount }));
    };

    const configuredCount = Number(characterCounts.userAgreement > 0) + Number(characterCounts.privacyPolicy > 0);
    const setting = settingQuery.data;

    return (
        <AdminPageFrame title="法律与协议" description="独立维护用户协议、隐私政策及其公开展示内容">
            <div className="legal-settings-page mx-auto max-w-6xl space-y-5">
                {settingQuery.error ? (
                    <Alert
                        className="legal-settings-load-error"
                        type="error"
                        showIcon
                        title="法律内容加载失败"
                        description={settingQuery.error instanceof Error ? settingQuery.error.message : "请稍后重试"}
                        action={<Button className="legal-settings-retry-button" icon={<RefreshCw className="legal-settings-retry-icon size-4" />} onClick={() => void settingQuery.refetch()}>重试</Button>}
                    />
                ) : null}

                {settingQuery.isLoading ? (
                    <Skeleton className="legal-settings-skeleton" active paragraph={{ rows: 16 }} />
                ) : (
                    <>
                        <SettingsSectionCard
                            className="legal-settings-editor-card"
                            icon={<ShieldCheck className="legal-settings-card-icon size-4" />}
                            title="公开法律文档"
                            description="编辑内容会显示在登录、注册及站点底部链接对应的公开页面。"
                            status={<Tag className="legal-settings-status" color={configuredCount === 2 ? "success" : "warning"}>{configuredCount === 2 ? "内容完整" : `${configuredCount}/2 已配置`}</Tag>}
                        >
                            <Tabs
                                className="legal-settings-tabs"
                                defaultActiveKey="userAgreement"
                                items={[
                                    {
                                        key: "userAgreement",
                                        label: "用户协议",
                                        forceRender: true,
                                        children: (
                                            <LegalDocumentPane
                                                title="用户协议"
                                                description="建议覆盖账号使用、用户内容权利、付费服务、违约处理、知识产权和争议解决。"
                                                previewPath="/legal/user-agreement"
                                            >
                                                <LegalRichTextEditor value={draft.userAgreement} placeholder="从用户协议第一条开始编辑……" disabled={saveMutation.isPending} onReady={initializeCharacterCount("userAgreement")} onChange={updateDraft("userAgreement")} />
                                            </LegalDocumentPane>
                                        ),
                                    },
                                    {
                                        key: "privacyPolicy",
                                        label: "隐私政策",
                                        forceRender: true,
                                        children: (
                                            <LegalDocumentPane
                                                title="隐私政策"
                                                description="建议说明信息收集范围、处理目的、保存期限、第三方共享规则与用户权利。"
                                                previewPath="/legal/privacy-policy"
                                            >
                                                <LegalRichTextEditor value={draft.privacyPolicy} placeholder="从隐私政策第一条开始编辑……" disabled={saveMutation.isPending} onReady={initializeCharacterCount("privacyPolicy")} onChange={updateDraft("privacyPolicy")} />
                                            </LegalDocumentPane>
                                        ),
                                    },
                                ]}
                            />
                        </SettingsSectionCard>

                        <div className="legal-settings-guidance grid gap-px overflow-hidden sm:grid-cols-3">
                            <LegalGuidance icon={<FileCheck2 className="legal-guidance-icon size-4" />} title="独立保存" description="法律内容更新不会覆盖品牌、备案或 Logo 配置。" />
                            <LegalGuidance icon={<ShieldCheck className="legal-guidance-icon size-4" />} title="安全展示" description="公开页面按受控富文本节点渲染，不执行任意脚本。" />
                            <LegalGuidance icon={<ExternalLink className="legal-guidance-icon size-4" />} title="实时预览" description="保存后可从文档右上角打开公开页面核对。" />
                        </div>

                        <AdminSettingsActionBar
                            meta={setting?.updatedAt ? `上次更新：${new Date(setting.updatedAt).toLocaleString("zh-CN", { hour12: false })}` : "尚未保存法律内容"}
                            status={dirty ? "有未保存的法律内容" : "公开内容已同步"}
                        >
                            <Button className="legal-settings-save-button" type="primary" icon={<Save className="legal-settings-save-icon size-4" />} loading={saveMutation.isPending} disabled={!dirty} onClick={() => saveMutation.mutate(draft)}>保存并发布</Button>
                        </AdminSettingsActionBar>
                    </>
                )}
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

function LegalGuidance({ icon, title, description }: { icon: React.ReactNode; title: string; description: string }) {
    return (
        <div className="legal-guidance-item">
            <span className="legal-guidance-symbol">{icon}</span>
            <div className="legal-guidance-copy">
                <h3 className="legal-guidance-title">{title}</h3>
                <p className="legal-guidance-description">{description}</p>
            </div>
        </div>
    );
}

function synchronizeSetting(queryClient: ReturnType<typeof useQueryClient>, setting: SiteSettings) {
    queryClient.setQueryData(adminSiteSettingsQueryKey, setting);
    queryClient.setQueryData(publicSiteSettingsQueryKey, setting);
}
