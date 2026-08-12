import { Alert, Button, Input, Modal, Tag } from "antd";
import { KeyRound, ServerCog, ShieldCheck } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";

import { providerAccountsApi, type AdminProviderAccount, type AdminProviderCredential, type AdminProviderCredentialVersion, type ProviderAccountsApi, type ProviderAdapterDescriptor, type ProviderHealthStatus } from "@/services/api/provider-accounts";
import { AdminPageFrame } from "../components/admin-shell";
import { AdminContentError, AdminContentSkeleton, SettingsSectionCard, configuredSecretText } from "../components/admin-ui";
import { endpointDraftChanged, formatKuaiziBalance, providerFamilyViews } from "./kuaizi-provider-domain";
import { createKuaiziAwaitingSync, isKuaiziAwaitingSync, kuaiziAwaitingSyncError, type KuaiziMutationScope, type KuaiziProviderOperation } from "./kuaizi-provider-mutation";
import "./kuaizi-provider.css";

type KuaiziProviderPageViewProps = {
    account: AdminProviderAccount;
    endpointDraft: string;
    endpointDirty: boolean;
    endpointSyncPending: boolean;
    loading: boolean;
    operation: KuaiziProviderOperation;
    loadError: Error | null;
    operationErrors: Record<string, Error>;
    onEndpointChange: (value: string) => void;
    onSaveEndpoint: () => void;
    onOpenCredential: () => void;
    onVerifyCredential: () => void;
    onPublishModels: (family: string) => void;
    onRetry: () => void;
};

const healthPresentation: Record<ProviderHealthStatus, { label: string; color?: string }> = {
    unverified: { label: "未验证" },
    healthy: { label: "验证健康", color: "success" },
    insufficient_balance: { label: "余额不足", color: "warning" },
    invalid: { label: "密钥无效", color: "error" },
    blocked: { label: "IP 被拒绝", color: "error" },
    unavailable: { label: "暂时不可用", color: "warning" },
    rejected: { label: "上游拒绝", color: "error" },
    unknown: { label: "验证异常", color: "error" },
};

function errorValue(value: unknown): Error {
    return value instanceof Error ? value : new Error("筷子科技配置请求失败");
}

function endpointBaseline(account: AdminProviderAccount): string {
    return account.endpointCandidate?.baseUrl ?? account.endpoint?.baseUrl ?? "";
}

function healthTag(status: ProviderHealthStatus) {
    const presentation = healthPresentation[status];
    return (
        <Tag className="kuaizi-provider-health-tag" color={presentation.color} variant="filled">
            {presentation.label}
        </Tag>
    );
}

function CredentialFacts({ active }: { active: AdminProviderCredentialVersion | null | undefined }) {
    if (!active?.hasKey) {
        return <p className="kuaizi-provider-empty-fact">尚未激活凭据。</p>;
    }
    return (
        <dl className="kuaizi-provider-facts">
            <div className="kuaizi-provider-fact">
                <dt className="kuaizi-provider-fact-label">凭据版本</dt>
                <dd className="kuaizi-provider-fact-value">{active.version}</dd>
            </div>
            <div className="kuaizi-provider-fact">
                <dt className="kuaizi-provider-fact-label">密钥指纹</dt>
                <dd className="kuaizi-provider-fact-value is-mono">{active.keyFingerprint}</dd>
            </div>
            <div className="kuaizi-provider-fact">
                <dt className="kuaizi-provider-fact-label">账户余额</dt>
                <dd className="kuaizi-provider-fact-value">{formatKuaiziBalance(active.walletBalanceSubunits)}</dd>
            </div>
            <div className="kuaizi-provider-fact">
                <dt className="kuaizi-provider-fact-label">验证时间</dt>
                <dd className="kuaizi-provider-fact-value">{active.verifiedAt ? new Date(active.verifiedAt).toLocaleString("zh-CN") : "尚未验证"}</dd>
            </div>
        </dl>
    );
}

function CandidateFacts({ candidate, hasActive }: { candidate: AdminProviderCredentialVersion; hasActive: boolean }) {
    return (
        <div className="kuaizi-provider-candidate" role="status">
            <div className="kuaizi-provider-candidate-heading">
                <span className="kuaizi-provider-candidate-title">候选版本 {candidate.version}</span>
                {healthTag(candidate.healthStatus)}
            </div>
            <span className="kuaizi-provider-candidate-fingerprint">{candidate.keyFingerprint}</span>
            <span className="kuaizi-provider-candidate-note">{hasActive ? "候选凭据保留到验证成功；失败不会覆盖当前活动版本，当前活动版本未变。" : "候选凭据尚未激活；验证成功前不会成为活动凭据。"}</span>
        </div>
    );
}

function ProviderModelFamilyCard({ adapter, credentialHealthy, busy, locked, error, onPublish }: { adapter: ProviderAdapterDescriptor; credentialHealthy: boolean; busy: boolean; locked: boolean; error?: Error; onPublish: () => void }) {
    const publishedCount = adapter.models.filter((item) => item.published).length;
    return (
        <SettingsSectionCard
            className="kuaizi-provider-family-card"
            icon={<ServerCog className="size-5" aria-hidden="true" />}
            title={adapter.family}
            description={adapter.models.map((model) => model.displayName).join("、") || "后端尚未登记模型"}
            status={
                <Tag className="kuaizi-provider-health-tag" variant="filled">
                    已登记
                </Tag>
            }
            footer={
                <div className="kuaizi-provider-family-actions">
                    <Button className="kuaizi-provider-secondary-action" loading={busy} disabled={locked || !credentialHealthy} onClick={onPublish}>
                        {publishedCount === adapter.models.length ? "同步模型" : "发布模型"}
                    </Button>
                </div>
            }
        >
            <p className="kuaizi-provider-publication-state">
                已发布 {publishedCount}/{adapter.models.length}；未定价模型不会向用户开放。
            </p>
            {error ? <Alert className="kuaizi-provider-operation-error" type="error" showIcon title="最近一次操作失败" description={error.message} /> : null}
        </SettingsSectionCard>
    );
}

function AccountCredentialCard({ account, busy, locked, error, onOpen, onVerify }: { account: AdminProviderAccount; busy: boolean; locked: boolean; error?: Error; onOpen: () => void; onVerify: () => void }) {
    const active = account.credential?.active;
    const candidate = account.credential?.candidate;
    return (
        <SettingsSectionCard
            className="kuaizi-provider-account-credential-card"
            icon={<KeyRound className="size-5" aria-hidden="true" />}
            title="账号统一凭据"
            description="所有筷子科技图片、视频与 Agent 模型共同使用这一枚 write-only Key。"
            status={
                active ? (
                    healthTag(active.healthStatus)
                ) : (
                    <Tag className="kuaizi-provider-health-tag" variant="filled">
                        未配置
                    </Tag>
                )
            }
            footer={
                <div className="kuaizi-provider-family-actions">
                    <Button className="kuaizi-provider-secondary-action" disabled={locked} onClick={onOpen}>
                        {active?.hasKey || candidate?.hasKey ? "更新密钥" : "配置密钥"}
                    </Button>
                    <Button className="kuaizi-provider-primary-action" type="primary" loading={busy} disabled={locked || (!active?.hasKey && !candidate?.hasKey)} onClick={onVerify}>
                        验证凭据
                    </Button>
                </div>
            }
        >
            <CredentialFacts active={active} />
            {candidate ? <CandidateFacts candidate={candidate} hasActive={Boolean(active)} /> : null}
            {error ? <Alert className="kuaizi-provider-operation-error" type="error" showIcon title="最近一次操作失败" description={error.message} /> : null}
        </SettingsSectionCard>
    );
}

export function KuaiziProviderPageView({
    account,
    endpointDraft,
    endpointDirty,
    endpointSyncPending,
    loading,
    operation,
    loadError,
    operationErrors,
    onEndpointChange,
    onSaveEndpoint,
    onOpenCredential,
    onVerifyCredential,
    onPublishModels,
    onRetry,
}: KuaiziProviderPageViewProps) {
    const families = providerFamilyViews(account);
    const endpointBusy = operation === "endpoint";
    const endpointStatus = account.endpointCandidate ? { label: "有待验证更新" } : account.endpoint?.active ? { label: "已启用", color: "success" } : account.endpoint ? { label: "待验证" } : { label: "未配置" };
    return (
        <AdminPageFrame title="筷子科技" description="统一维护一套服务地址与账号 Key；各模型系列只负责能力与发布。">
            <div className="kuaizi-provider-page-content">
                {loadError ? <AdminContentError title="筷子科技配置刷新失败" description={loadError.message} onRetry={onRetry} /> : null}
                <SettingsSectionCard
                    className="kuaizi-provider-endpoint-card"
                    icon={<ServerCog className="size-5" aria-hidden="true" />}
                    title="公共服务配置"
                    description="Base URL 由所有后端登记的筷子科技模型系列共享。"
                    status={endpointStatus}
                    footer={
                        <div className="kuaizi-provider-endpoint-footer">
                            <span className="kuaizi-provider-sync-state">{endpointSyncPending ? "写入结果待同步" : endpointDirty ? "有未保存变更" : "已同步"}</span>
                            <Button className="kuaizi-provider-save-endpoint" type="primary" loading={endpointBusy} disabled={Boolean(operation) || endpointSyncPending || !endpointDirty || loading} onClick={onSaveEndpoint}>
                                保存服务地址
                            </Button>
                        </div>
                    }
                >
                    <label className="kuaizi-provider-field-label" htmlFor="kuaizi-provider-base-url">
                        Base URL
                    </label>
                    <Input
                        id="kuaizi-provider-base-url"
                        className="kuaizi-provider-base-url"
                        value={endpointDraft}
                        disabled={Boolean(operation)}
                        placeholder="https://…"
                        autoComplete="url"
                        onChange={(event) => onEndpointChange(event.currentTarget.value)}
                    />
                    {operationErrors.endpoint ? <Alert className="kuaizi-provider-operation-error" type="error" showIcon title="服务地址保存失败" description={operationErrors.endpoint.message} /> : null}
                    {account.endpointCandidate ? (
                        <p className="kuaizi-provider-endpoint-candidate">
                            候选地址 v{account.endpointCandidate.version}：{account.endpointCandidate.baseUrl}
                        </p>
                    ) : null}
                </SettingsSectionCard>

                <AccountCredentialCard account={account} busy={operation === "credential"} locked={Boolean(operation)} error={operationErrors.credential} onOpen={onOpenCredential} onVerify={onVerifyCredential} />

                <section className="kuaizi-provider-families" aria-labelledby="kuaizi-provider-families-title">
                    <div className="kuaizi-provider-section-heading">
                        <div className="kuaizi-provider-section-copy">
                            <h2 id="kuaizi-provider-families-title" className="kuaizi-provider-section-title">
                                模型系列
                            </h2>
                            <p className="kuaizi-provider-section-description">系列目录完全来自后端 adapter registry，不在前端维护模型家族枚举。</p>
                        </div>
                        <span className="kuaizi-provider-family-count">{families.length} 个系列</span>
                    </div>
                    <div className="kuaizi-provider-family-list">
                        {families.map(({ adapter }) => (
                            <ProviderModelFamilyCard
                                key={`${adapter.providerKind}:${adapter.family}`}
                                adapter={adapter}
                                credentialHealthy={account.credential?.active?.healthStatus === "healthy"}
                                busy={operation === `models:${adapter.family}`}
                                locked={Boolean(operation)}
                                error={operationErrors[adapter.family]}
                                onPublish={() => onPublishModels(adapter.family)}
                            />
                        ))}
                    </div>
                </section>
            </div>
        </AdminPageFrame>
    );
}

export function ProviderCredentialEditor({
    credential,
    open,
    verifying,
    error,
    onCancel,
    onSubmit,
}: {
    credential: AdminProviderCredential | null;
    open: boolean;
    verifying: boolean;
    error?: Error;
    onCancel: () => void;
    onSubmit: (key: string) => Promise<void>;
}) {
    const inputRef = useRef<HTMLInputElement>(null);
    const [hasSecretDraft, setHasSecretDraft] = useState(false);
    const canVerifyExisting = Boolean(credential?.active?.hasKey || credential?.candidate?.hasKey);
    if (!open) return null;

    const submit = async () => {
        const key = inputRef.current?.value ?? "";
        if (inputRef.current) inputRef.current.value = "";
        setHasSecretDraft(false);
        await onSubmit(key);
    };

    return (
        <div className="kuaizi-provider-editor" aria-busy={verifying || undefined}>
            <div className="kuaizi-provider-editor-heading">
                <ShieldCheck className="kuaizi-provider-editor-icon size-5" aria-hidden="true" />
                <div className="kuaizi-provider-editor-copy">
                    <h3 className="kuaizi-provider-editor-title">筷子科技账号凭据</h3>
                    <p className="kuaizi-provider-editor-description">{configuredSecretText}。浏览器不会从服务端读取或回显任何已保存密钥。</p>
                </div>
            </div>
            {credential?.candidate ? <CandidateFacts candidate={credential.candidate} hasActive={Boolean(credential.active)} /> : null}
            <label className="kuaizi-provider-field-label" htmlFor="kuaizi-provider-secret">
                新 Key
            </label>
            <input
                ref={inputRef}
                id="kuaizi-provider-secret"
                className="kuaizi-provider-secret-input"
                type="password"
                autoComplete="new-password"
                disabled={verifying}
                placeholder={canVerifyExisting ? "留空则继续验证现有凭据" : "输入新 Key"}
                onChange={(event) => setHasSecretDraft(Boolean(event.currentTarget.value.trim()))}
            />
            {error ? <Alert className="kuaizi-provider-operation-error" type="error" showIcon title="验证失败" description={error.message} /> : null}
            <div className="kuaizi-provider-editor-actions">
                <Button className="kuaizi-provider-editor-cancel" disabled={verifying} onClick={onCancel}>
                    取消
                </Button>
                <Button className="kuaizi-provider-editor-submit" type="primary" loading={verifying} disabled={verifying || (!hasSecretDraft && !canVerifyExisting)} onClick={() => void submit()}>
                    保存并验证
                </Button>
            </div>
        </div>
    );
}

export default function KuaiziProviderPage({ api = providerAccountsApi }: { api?: ProviderAccountsApi } = {}) {
    const [account, setAccount] = useState<AdminProviderAccount | null>(null);
    const [endpointDraft, setEndpointDraft] = useState("");
    const [loading, setLoading] = useState(true);
    const [loadError, setLoadError] = useState<Error | null>(null);
    const [operation, setOperation] = useState<KuaiziProviderOperation>(null);
    const [operationErrors, setOperationErrors] = useState<Record<string, Error>>({});
    const [credentialEditorOpen, setCredentialEditorOpen] = useState(false);
    const operationOwnerRef = useRef<KuaiziProviderOperation>(null);
    const endpointSyncPending = isKuaiziAwaitingSync(operation) && operation.scope === "endpoint";

    const claimOperation = (owner: KuaiziMutationScope): boolean => {
        if (operationOwnerRef.current) return false;
        operationOwnerRef.current = owner;
        setOperation(owner);
        return true;
    };

    const releaseOperation = (owner: KuaiziMutationScope) => {
        if (operationOwnerRef.current !== owner) return;
        operationOwnerRef.current = null;
        setOperation(null);
    };

    const awaitFactSync = (scope: KuaiziMutationScope, mutationError: Error, syncError: Error) => {
        const awaiting = createKuaiziAwaitingSync(scope, mutationError, syncError);
        operationOwnerRef.current = awaiting;
        setOperation(awaiting);
        setLoadError(kuaiziAwaitingSyncError(awaiting));
    };

    const load = useCallback(async () => {
        setLoading(true);
        try {
            const next = await api.get();
            setAccount(next);
            setEndpointDraft(endpointBaseline(next));
            if (isKuaiziAwaitingSync(operationOwnerRef.current)) {
                operationOwnerRef.current = null;
                setOperation(null);
            }
            setLoadError(null);
        } catch (error) {
            const syncError = errorValue(error);
            const owner = operationOwnerRef.current;
            if (isKuaiziAwaitingSync(owner)) {
                const awaiting = { ...owner, syncError };
                operationOwnerRef.current = awaiting;
                setOperation(awaiting);
                setLoadError(kuaiziAwaitingSyncError(awaiting));
            } else {
                setLoadError(syncError);
            }
        } finally {
            setLoading(false);
        }
    }, [api]);

    useEffect(() => {
        void load();
    }, [load]);

    const endpointDirty = account ? endpointDraftChanged(account, endpointDraft) : false;
    const saveEndpoint = async () => {
        if (!account || !endpointDirty || operationOwnerRef.current) return;
        let parsed: URL;
        try {
            parsed = new URL(endpointDraft.trim());
        } catch {
            setOperationErrors((current) => ({ ...current, endpoint: new Error("Base URL 必须是有效的 HTTP(S) 地址") }));
            return;
        }
        if (parsed.protocol !== "http:" && parsed.protocol !== "https:") {
            setOperationErrors((current) => ({ ...current, endpoint: new Error("Base URL 必须使用 HTTP(S) 协议") }));
            return;
        }
        const owner = "endpoint" as const;
        if (!claimOperation(owner)) return;
        try {
            const next = await api.saveEndpoint(endpointDraft);
            setAccount(next);
            setEndpointDraft(endpointBaseline(next));
            setOperationErrors((current) => {
                const nextErrors = { ...current };
                delete nextErrors.endpoint;
                return nextErrors;
            });
        } catch (error) {
            const mutationError = errorValue(error);
            setOperationErrors((current) => ({ ...current, endpoint: mutationError }));
            try {
                const next = await api.get();
                setAccount(next);
                setEndpointDraft(endpointBaseline(next));
                setLoadError(null);
            } catch (syncError) {
                awaitFactSync(owner, mutationError, errorValue(syncError));
            }
        } finally {
            releaseOperation(owner);
        }
    };

    const verifyCredential = async (key = "") => {
        if (!account || operationOwnerRef.current) return;
        const owner = "credential" as const;
        if (!claimOperation(owner)) return;
        setOperationErrors((current) => {
            const next = { ...current };
            delete next.credential;
            return next;
        });
        try {
            const candidateAccount = await api.saveCredential(key);
            if (candidateAccount) setAccount(candidateAccount);
            const next = await api.verifyCredential();
            setAccount(next);
            setLoadError(null);
            setCredentialEditorOpen(false);
        } catch (error) {
            const mutationError = errorValue(error);
            setOperationErrors((current) => ({ ...current, credential: mutationError }));
            try {
                const next = await api.get();
                setAccount(next);
                setEndpointDraft(endpointBaseline(next));
                setLoadError(null);
            } catch (syncError) {
                awaitFactSync(owner, mutationError, errorValue(syncError));
            }
        } finally {
            releaseOperation(owner);
        }
    };

    const publishModels = async (family: string) => {
        if (!account || operationOwnerRef.current) return;
        const owner = `models:${family}` as const;
        if (!claimOperation(owner)) return;
        try {
            const next = await api.publishModels(family);
            setAccount(next);
            setLoadError(null);
        } catch (error) {
            const mutationError = errorValue(error);
            setOperationErrors((current) => ({ ...current, [family]: mutationError }));
            try {
                const next = await api.get();
                setAccount(next);
                setEndpointDraft(endpointBaseline(next));
                setLoadError(null);
            } catch (syncError) {
                awaitFactSync(owner, mutationError, errorValue(syncError));
            }
        } finally {
            releaseOperation(owner);
        }
    };

    if (loading && !account) {
        return (
            <AdminPageFrame title="筷子科技" description="正在读取供应商配置。">
                <AdminContentSkeleton rows={8} label="正在加载筷子科技配置" />
            </AdminPageFrame>
        );
    }

    if (!account) {
        return (
            <AdminPageFrame title="筷子科技" description="统一维护服务地址与模型系列凭据。">
                <AdminContentError title="筷子科技配置加载失败" description={loadError?.message ?? "服务端未返回供应商配置"} onRetry={() => void load()} />
            </AdminPageFrame>
        );
    }

    return (
        <>
            <KuaiziProviderPageView
                account={account}
                endpointDraft={endpointDraft}
                endpointDirty={endpointDirty}
                endpointSyncPending={endpointSyncPending}
                loading={loading}
                operation={operation}
                loadError={loadError}
                operationErrors={operationErrors}
                onEndpointChange={(value) => {
                    if (!operationOwnerRef.current) setEndpointDraft(value);
                }}
                onSaveEndpoint={() => void saveEndpoint()}
                onOpenCredential={() => {
                    if (!operationOwnerRef.current) setCredentialEditorOpen(true);
                }}
                onVerifyCredential={() => void verifyCredential()}
                onPublishModels={(family) => void publishModels(family)}
                onRetry={() => void load()}
            />
            <Modal
                rootClassName="admin-operation-modal workspace-ui-scope kuaizi-provider-modal"
                title="配置账号凭据"
                open={credentialEditorOpen}
                footer={null}
                closable={!operation}
                mask={{ closable: !operation }}
                keyboard={!operation}
                onCancel={() => {
                    if (!operation) setCredentialEditorOpen(false);
                }}
                destroyOnHidden
            >
                {credentialEditorOpen ? <ProviderCredentialEditor credential={account.credential} open verifying={Boolean(operation)} error={operationErrors.credential} onCancel={() => setCredentialEditorOpen(false)} onSubmit={verifyCredential} /> : null}
            </Modal>
        </>
    );
}
