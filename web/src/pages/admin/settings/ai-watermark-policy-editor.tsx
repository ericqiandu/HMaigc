import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { App, Button, Input, Tag } from "antd";
import { ExternalLink, Save, Stamp } from "lucide-react";
import { useEffect, useMemo, useState } from "react";

import { adminWatermarkPolicyQueryKey, watermarkPolicyApi, type PublishWatermarkPolicyInput, type WatermarkPolicyApi } from "@/services/api/watermark-policy";
import { AdminContentError, AdminContentSkeleton, SettingsSectionCard } from "../components/admin-ui";
import { LegalRichTextEditor } from "../components/legal-rich-text-editor";

const emptyDraft: PublishWatermarkPolicyInput = { managementRuleRichText: "", watermarkPolicyUrl: "" };

export function AIWatermarkPolicyEditor({ api = watermarkPolicyApi }: { api?: WatermarkPolicyApi }) {
    const { message, modal } = App.useApp();
    const queryClient = useQueryClient();
    const query = useQuery({ queryKey: adminWatermarkPolicyQueryKey, queryFn: api.getAdminPolicy });
    const [draft, setDraft] = useState(emptyDraft);
    const [baseline, setBaseline] = useState(emptyDraft);
    const [initialized, setInitialized] = useState(false);
    const normalizedDraft = useMemo(() => normalizeDraft(draft), [draft]);
    const dirty = normalizedDraft.managementRuleRichText !== baseline.managementRuleRichText || normalizedDraft.watermarkPolicyUrl !== baseline.watermarkPolicyUrl;

    useEffect(() => {
        if (query.isLoading || (initialized && dirty)) return;
        const next = query.data ? normalizeDraft(query.data) : emptyDraft;
        setDraft(next);
        setBaseline(next);
        setInitialized(true);
    }, [dirty, initialized, query.data, query.isLoading]);

    const mutation = useMutation({
        mutationFn: api.publishPolicy,
        onSuccess: (publication) => {
            queryClient.setQueryData(adminWatermarkPolicyQueryKey, publication);
            const synchronized = normalizeDraft(publication);
            setDraft(synchronized);
            setBaseline(synchronized);
            void message.success(`AI 水印规则第 ${publication.version} 版已发布`);
        },
        onError: (error) => void message.error(error instanceof Error ? error.message : "发布 AI 水印规则失败"),
    });
    const publish = () => mutation.mutate(normalizedDraft);
    const confirmOrPublish = () => {
        if (!isHTTPSURL(normalizedDraft.watermarkPolicyUrl) || !hasVisibleRichText(normalizedDraft.managementRuleRichText)) return;
        if (!dirty && query.data) {
            modal.confirm({ title: "确认发布相同内容的新版本？", content: "继续发布会要求所有已开启账号重新确认。", okText: "确认发布新版本", cancelText: "取消", onOk: publish });
            return;
        }
        publish();
    };

    return (
        <SettingsSectionCard
            className="ai-watermark-policy-editor"
            icon={<Stamp className="ai-watermark-policy-icon size-4" />}
            title="AI 生成内容水印管理规则"
            description="规则正文由平台独立维护；水印规范仅填写公开 HTTPS 外链。每次发布都会生成不可变新版本。"
            status={
                <Tag className="ai-watermark-policy-version" color={query.data ? "success" : "warning"}>
                    {query.data ? `当前版本 v${query.data.version}` : "尚未发布"}
                </Tag>
            }
        >
            {query.isLoading && !query.data ? <AdminContentSkeleton rows={8} label="正在加载 AI 水印规则" /> : null}
            {query.error && !query.data ? <AdminContentError title="AI 水印规则加载失败" description={query.error instanceof Error ? query.error.message : "请稍后重试"} onRetry={() => void query.refetch()} /> : null}
            {!query.isLoading && (!query.error || query.data) ? (
                <div className="ai-watermark-policy-form">
                    {query.error && query.data ? (
                        <AdminContentError title="AI 水印规则刷新失败" description={`${query.error instanceof Error ? query.error.message : "请稍后重试"}；当前显示上次成功读取的版本。`} onRetry={() => void query.refetch()} />
                    ) : null}
                    <div className="ai-watermark-policy-field">
                        <label className="ai-watermark-policy-label" htmlFor="watermark-policy-url">
                            水印规范外链
                        </label>
                        <Input
                            className="ai-watermark-policy-input"
                            id="watermark-policy-url"
                            value={draft.watermarkPolicyUrl}
                            status={draft.watermarkPolicyUrl && !isHTTPSURL(draft.watermarkPolicyUrl) ? "error" : undefined}
                            placeholder="https://example.com/ai-watermark-policy"
                            disabled={mutation.isPending}
                            onChange={(event) => setDraft((current) => ({ ...current, watermarkPolicyUrl: event.target.value }))}
                        />
                        <span className="ai-watermark-policy-help">仅支持无账号信息与片段的 HTTPS 地址。</span>
                    </div>
                    <div className="ai-watermark-policy-field">
                        <label className="ai-watermark-policy-label">管理规则正文</label>
                        <LegalRichTextEditor
                            value={draft.managementRuleRichText}
                            placeholder="编辑 AI 生成内容水印管理规则"
                            disabled={mutation.isPending}
                            onChange={(managementRuleRichText) => setDraft((current) => ({ ...current, managementRuleRichText }))}
                        />
                    </div>
                    <div className="ai-watermark-policy-footer">
                        <div className="ai-watermark-policy-meta" role="status">
                            {dirty ? "有尚未发布的修改" : query.data ? `已发布：${new Date(query.data.publishedAt).toLocaleString("zh-CN", { hour12: false })}` : "请填写规则正文与外链"}
                        </div>
                        <div className="ai-watermark-policy-actions">
                            {query.data ? (
                                <a className="ai-watermark-policy-link" href={query.data.watermarkPolicyUrl} target="_blank" rel="noopener noreferrer">
                                    <ExternalLink className="ai-watermark-policy-link-icon size-3.5" />
                                    预览水印规范
                                </a>
                            ) : null}
                            <Button
                                className="ai-watermark-policy-publish"
                                type="primary"
                                icon={<Save className="ai-watermark-policy-publish-icon size-4" />}
                                loading={mutation.isPending}
                                disabled={!isHTTPSURL(normalizedDraft.watermarkPolicyUrl) || !hasVisibleRichText(normalizedDraft.managementRuleRichText)}
                                onClick={confirmOrPublish}
                            >
                                发布新版本
                            </Button>
                        </div>
                    </div>
                </div>
            ) : null}
        </SettingsSectionCard>
    );
}

function normalizeDraft(input: PublishWatermarkPolicyInput): PublishWatermarkPolicyInput {
    return { managementRuleRichText: input.managementRuleRichText.trim(), watermarkPolicyUrl: input.watermarkPolicyUrl.trim() };
}
function hasVisibleRichText(value: string) {
    return (
        value
            .replace(/<[^>]*>/g, "")
            .replace(/&nbsp;/g, " ")
            .trim().length > 0
    );
}
function isHTTPSURL(value: string) {
    try {
        const parsed = new URL(value.trim());
        return parsed.protocol === "https:" && Boolean(parsed.hostname) && !parsed.username && !parsed.password && !parsed.hash;
    } catch {
        return false;
    }
}
