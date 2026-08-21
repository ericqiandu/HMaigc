import { Alert, App, Button, Empty, Select, Spin, Tooltip } from "antd";
import { Check, FileArchive, FileAudio, FileImage, FileVideo, FolderKanban, Gauge, Library, ReceiptText, ShieldCheck, Upload } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";

import { assignProjectTeam, clearProjectCollaborator, getProjectPermissions, listProjects, type ProjectAccessOverview, type ProjectAccessRole, type ProjectSummary, updateProjectPermission } from "@/services/api/projects";
import { listTeamResources, teamResourceFileURL, type TeamDetail, type TeamResource, uploadTeamResource } from "@/services/api/teams";
import { useUserStore } from "@/stores/use-user-store";

type TeamCommercialPanelProps = {
    detail: TeamDetail;
    onTeamChanged: () => Promise<void>;
};

const projectRoleOptions: Array<{ label: string; value: ProjectAccessRole }> = [
    { label: "仅查看", value: "viewer" },
    { label: "可编辑", value: "editor" },
    { label: "项目管理", value: "manager" },
];

export function TeamCommercialPanel({ detail, onTeamChanged }: TeamCommercialPanelProps) {
    const { message } = App.useApp();
    const currentUserId = useUserStore((store) => store.user?.id || "");
    const fileInput = useRef<HTMLInputElement>(null);
    const commercialRequestGeneration = useRef(0);
    const permissionsRequestGeneration = useRef(0);
    const [resources, setResources] = useState<TeamResource[]>([]);
    const [projects, setProjects] = useState<ProjectSummary[]>([]);
    const [loadedTeamId, setLoadedTeamId] = useState("");
    const [permissions, setPermissions] = useState<ProjectAccessOverview | null>(null);
    const [loading, setLoading] = useState(false);
    const [loadError, setLoadError] = useState("");
    const [busyKey, setBusyKey] = useState("");
    const subscription = detail.summary.subscription;
    const { capabilities } = detail.summary;
    const teamId = detail.summary.team.id;

    const loadCommercialData = useCallback(async () => {
        const requestGeneration = commercialRequestGeneration.current + 1;
        commercialRequestGeneration.current = requestGeneration;
        if (!subscription) {
            setResources([]);
            setProjects([]);
            setLoadedTeamId("");
            setLoadError("");
            setLoading(false);
            return;
        }
        setLoading(true);
        setLoadError("");
        try {
            const [resourceResult, projectResult] = await Promise.all([
                subscription.sharedAssetsEnabled ? listTeamResources(teamId) : Promise.resolve({ resources: [] }),
                subscription.projectPermissionsEnabled ? listProjects() : Promise.resolve({ projects: [] }),
            ]);
            if (commercialRequestGeneration.current !== requestGeneration) return;
            setResources(resourceResult.resources);
            setProjects(projectResult.projects);
            setLoadedTeamId(teamId);
        } catch (error) {
            if (commercialRequestGeneration.current !== requestGeneration) return;
            setLoadError(error instanceof Error ? error.message : "团队商业数据加载失败");
        } finally {
            if (commercialRequestGeneration.current === requestGeneration) setLoading(false);
        }
    }, [subscription, teamId]);

    useEffect(() => {
        permissionsRequestGeneration.current += 1;
        setPermissions(null);
        setLoadedTeamId("");
        void loadCommercialData();
    }, [loadCommercialData]);

    const uploadFile = async (file: File) => {
        setBusyKey("asset-upload");
        try {
            const kind = mediaKind(file.type);
            await uploadTeamResource(teamId, file, kind);
            await loadCommercialData();
            await onTeamChanged();
            message.success("文件已上传到团队共享资产库");
        } catch (error) {
            message.error(error instanceof Error ? error.message : "团队文件上传失败");
        } finally {
            setBusyKey("");
            if (fileInput.current) fileInput.current.value = "";
        }
    };

    const addProject = async (projectId: string) => {
        setBusyKey(`project-assign:${projectId}`);
        try {
            await assignProjectTeam(projectId, teamId);
            await loadCommercialData();
            message.success("项目已纳入团队空间");
        } catch (error) {
            message.error(error instanceof Error ? error.message : "项目归属调整失败");
        } finally {
            setBusyKey("");
        }
    };

    const detachProject = async (projectId: string) => {
        setBusyKey(`project-detach:${projectId}`);
        try {
            await assignProjectTeam(projectId, "");
            if (permissions?.projectId === projectId) setPermissions(null);
            await loadCommercialData();
            message.success("项目已移出团队空间");
        } catch (error) {
            message.error(error instanceof Error ? error.message : "移出团队项目失败");
        } finally {
            setBusyKey("");
        }
    };

    const openPermissions = async (projectId: string) => {
        const requestGeneration = permissionsRequestGeneration.current + 1;
        permissionsRequestGeneration.current = requestGeneration;
        setBusyKey(`permissions:${projectId}`);
        try {
            const result = await getProjectPermissions(projectId);
            if (permissionsRequestGeneration.current === requestGeneration) setPermissions(result);
        } catch (error) {
            if (permissionsRequestGeneration.current === requestGeneration) message.error(error instanceof Error ? error.message : "项目权限加载失败");
        } finally {
            if (permissionsRequestGeneration.current === requestGeneration) setBusyKey("");
        }
    };

    const changePermission = async (userId: string, role: ProjectAccessRole) => {
        if (!permissions) return;
        setBusyKey(`permission:${permissions.projectId}:${userId}`);
        try {
            await updateProjectPermission(permissions.projectId, userId, role);
            setPermissions(await getProjectPermissions(permissions.projectId));
            message.success("项目权限已更新");
        } catch (error) {
            message.error(error instanceof Error ? error.message : "项目权限更新失败");
        } finally {
            setBusyKey("");
        }
    };

    const clearPermission = async (userId: string) => {
        if (!permissions) return;
        setBusyKey(`permission-clear:${permissions.projectId}:${userId}`);
        try {
            await clearProjectCollaborator(permissions.projectId, userId);
            setPermissions(await getProjectPermissions(permissions.projectId));
            message.success("已恢复继承团队角色权限");
        } catch (error) {
            message.error(error instanceof Error ? error.message : "恢复继承权限失败");
        } finally {
            setBusyKey("");
        }
    };

    if (!subscription) return null;

    const teamProjects = projects.filter((item) => item.project.teamId === teamId);
    const assignableProjects = projects.filter((item) => !item.project.teamId && item.project.userId === currentUserId);

    return (
        <section className="team-commercial-panel mt-7">
            <div className="team-commercial-heading">
                <h3 className="team-commercial-title text-sm font-semibold">团队商业能力</h3>
                <p className="team-commercial-description mt-1 text-xs text-foreground/45">权益状态来自当前有效订阅快照，额度与用量来自实时业务数据。</p>
            </div>

            <div className="team-entitlement-groups mt-4 grid gap-5 border-b border-border/55 pb-6 lg:grid-cols-3">
                <EntitlementGroup
                    title="协作效率"
                    items={[
                        ["多人画布协作", true],
                        ["团队共享资产库", subscription.sharedAssetsEnabled],
                        ["团队任务不限排队（执行并发受模型渠道限制）", subscription.unlimitedTaskQueue],
                    ]}
                />
                <EntitlementGroup
                    title="管理"
                    items={[
                        ["团队席位管理", true],
                        ["积分用量管控", true],
                        ["项目权限管理", subscription.projectPermissionsEnabled],
                        ["极速开发票", subscription.invoicingEnabled],
                    ]}
                />
                <EntitlementGroup
                    title="安全与其他"
                    items={[
                        ["团队资产隔离", subscription.sharedAssetsEnabled],
                        [`云端存储空间 ${formatBytes(subscription.teamStorageBytes)}`, subscription.teamStorageBytes > 0],
                        ["商业使用授权", subscription.commercialUseEnabled],
                    ]}
                />
            </div>

            <div className="team-commercial-usage grid gap-4 border-b border-border/55 py-5 sm:grid-cols-3">
                <UsageMetric icon={<Gauge className="team-commercial-usage-icon size-4" />} label="可用团队积分" value={formatCredits(detail.summary.availableMicrocredits)} />
                <UsageMetric icon={<ShieldCheck className="team-commercial-usage-icon size-4" />} label="任务预留积分" value={formatCredits(detail.summary.reservedMicrocredits)} />
                <UsageMetric icon={<Library className="team-commercial-usage-icon size-4" />} label="共享资产存储" value={`${formatBytes(detail.summary.storageUsedBytes)} / ${formatBytes(subscription.teamStorageBytes)}`} />
            </div>

            {loadError ? (
                <Alert
                    className="team-commercial-load-error mt-5"
                    type="error"
                    showIcon
                    message="团队商业数据加载失败"
                    description={loadError}
                    action={
                        <Button className="team-commercial-load-retry" size="small" onClick={() => void loadCommercialData()}>
                            重新加载
                        </Button>
                    }
                />
            ) : null}

            <Spin className="team-commercial-loading" spinning={loading}>
                {loadedTeamId === teamId && subscription.sharedAssetsEnabled ? (
                    <section className="team-assets-section border-b border-border/55 py-6">
                        <div className="team-commercial-section-heading flex items-center justify-between gap-3">
                            <div className="team-commercial-section-copy">
                                <h4 className="team-commercial-section-title text-sm font-medium">团队共享资产库</h4>
                                <p className="team-commercial-section-description mt-1 text-xs text-foreground/42">团队成员均可读取，文件按团队空间隔离存储。</p>
                            </div>
                            <div className="team-assets-upload-action">
                                <input
                                    ref={fileInput}
                                    className="team-assets-file-input hidden"
                                    type="file"
                                    onChange={(event) => {
                                        const file = event.target.files?.[0];
                                        if (file) void uploadFile(file);
                                    }}
                                />
                                {capabilities.canUploadSharedAssets ? (
                                    <Button className="team-assets-upload-button" icon={<Upload className="team-assets-upload-icon size-4" />} loading={busyKey === "asset-upload"} onClick={() => fileInput.current?.click()}>
                                        上传资产
                                    </Button>
                                ) : null}
                            </div>
                        </div>
                        {resources.length ? (
                            <div className="team-assets-list mt-3 grid gap-1 sm:grid-cols-2 xl:grid-cols-3">
                                {resources.map((resource) => (
                                    <a
                                        className="team-asset-row flex min-w-0 items-center gap-2.5 bg-foreground/[.025] px-3 py-2.5 text-foreground transition-colors hover:bg-foreground/[.05]"
                                        href={teamResourceFileURL(teamId, resource.id)}
                                        target="_blank"
                                        rel="noreferrer"
                                        key={resource.id}
                                    >
                                        <span className="team-asset-icon grid size-8 shrink-0 place-items-center bg-foreground/[.05]">{resourceIcon(resource.mimeType)}</span>
                                        <span className="team-asset-copy min-w-0 flex-1">
                                            <span className="team-asset-name block truncate text-xs font-medium">{resource.kind || "团队文件"}</span>
                                            <span className="team-asset-meta mt-0.5 block text-[10px] text-foreground/38">
                                                {formatBytes(resource.size)} · {formatDateTime(resource.createdAt)}
                                            </span>
                                        </span>
                                    </a>
                                ))}
                            </div>
                        ) : (
                            <Empty className="team-assets-empty my-6" image={Empty.PRESENTED_IMAGE_SIMPLE} description="还没有团队共享资产" />
                        )}
                    </section>
                ) : null}

                {loadedTeamId === teamId && subscription.projectPermissionsEnabled ? (
                    <section className="team-projects-section py-6">
                        <div className="team-commercial-section-heading flex flex-wrap items-end justify-between gap-3">
                            <div className="team-commercial-section-copy">
                                <h4 className="team-commercial-section-title text-sm font-medium">团队项目与权限</h4>
                                <p className="team-commercial-section-description mt-1 text-xs text-foreground/42">团队角色提供默认权限，项目级配置可做明确覆盖。</p>
                            </div>
                            {capabilities.canManageProjects && assignableProjects.length ? (
                                <Select
                                    className="team-project-assign-select min-w-48"
                                    placeholder="添加个人项目到团队"
                                    value={undefined}
                                    options={assignableProjects.map((item) => ({ label: item.project.name, value: item.project.id }))}
                                    disabled={busyKey !== ""}
                                    onChange={(projectId: string) => void addProject(projectId)}
                                />
                            ) : null}
                        </div>
                        {teamProjects.length ? (
                            <div className="team-project-list mt-3 divide-y divide-border/45 bg-foreground/[.02]">
                                {teamProjects.map((item) => (
                                    <article className="team-project-row flex min-h-14 items-center gap-3 px-3 py-2.5" key={item.project.id}>
                                        <FolderKanban className="team-project-icon size-4 shrink-0 text-foreground/42" />
                                        <div className="team-project-copy min-w-0 flex-1">
                                            <div className="team-project-name truncate text-xs font-medium">{item.project.name}</div>
                                            <div className="team-project-meta mt-0.5 text-[10px] text-foreground/38">
                                                {item.canvasCount} 个画布 · {item.assetCount} 项资产
                                            </div>
                                        </div>
                                        {capabilities.canManageProjects ? (
                                            <div className="team-project-actions flex items-center gap-1">
                                                <Button className="team-project-permissions-button" type="text" size="small" loading={busyKey === `permissions:${item.project.id}`} onClick={() => void openPermissions(item.project.id)}>
                                                    权限
                                                </Button>
                                                {item.project.userId === currentUserId ? (
                                                    <Button className="team-project-detach-button" type="text" size="small" danger loading={busyKey === `project-detach:${item.project.id}`} onClick={() => void detachProject(item.project.id)}>
                                                        移出
                                                    </Button>
                                                ) : null}
                                            </div>
                                        ) : null}
                                    </article>
                                ))}
                            </div>
                        ) : (
                            <Empty className="team-projects-empty my-6" image={Empty.PRESENTED_IMAGE_SIMPLE} description="还没有团队项目" />
                        )}

                        {permissions ? (
                            <div className="team-project-permission-editor mt-5 bg-foreground/[.025] px-3 py-3">
                                <div className="team-project-permission-heading flex items-center justify-between gap-3">
                                    <span className="team-project-permission-title text-xs font-medium">项目成员权限</span>
                                    <Button className="team-project-permission-close" type="text" size="small" onClick={() => setPermissions(null)}>
                                        收起
                                    </Button>
                                </div>
                                <div className="team-project-permission-list mt-2 divide-y divide-border/45">
                                    {permissions.members.map((member) => (
                                        <div className="team-project-permission-row flex items-center gap-3 py-2" key={member.userId}>
                                            <span className="team-project-permission-name min-w-0 flex-1 truncate text-xs">{member.displayName || member.username}</span>
                                            <Tooltip className="team-project-permission-source-tooltip" title={member.explicit ? "项目级权限覆盖" : "继承团队角色"}>
                                                <span className="team-project-permission-source text-[10px] text-foreground/38">{member.explicit ? "单独配置" : "继承"}</span>
                                            </Tooltip>
                                            <Select
                                                className="team-project-permission-select w-28"
                                                size="small"
                                                value={member.role}
                                                options={projectRoleOptions}
                                                loading={busyKey === `permission:${permissions.projectId}:${member.userId}`}
                                                disabled={busyKey !== ""}
                                                onChange={(role: ProjectAccessRole) => void changePermission(member.userId, role)}
                                            />
                                            {member.explicit ? (
                                                <Button
                                                    className="team-project-permission-clear"
                                                    type="text"
                                                    size="small"
                                                    loading={busyKey === `permission-clear:${permissions.projectId}:${member.userId}`}
                                                    disabled={busyKey !== ""}
                                                    onClick={() => void clearPermission(member.userId)}
                                                >
                                                    恢复继承
                                                </Button>
                                            ) : null}
                                        </div>
                                    ))}
                                </div>
                            </div>
                        ) : null}
                    </section>
                ) : null}
            </Spin>
        </section>
    );
}

function EntitlementGroup({ title, items }: { title: string; items: Array<[string, boolean]> }) {
    return (
        <div className="team-entitlement-group min-w-0">
            <h4 className="team-entitlement-group-title text-xs font-semibold">{title}</h4>
            <div className="team-entitlement-list mt-3 space-y-2.5">
                {items.map(([label, enabled]) => (
                    <div className={`team-entitlement-row flex items-center gap-2 text-xs ${enabled ? "text-foreground/78" : "text-foreground/28"}`} key={label}>
                        <Check className="team-entitlement-check size-3.5 shrink-0" />
                        <span className="team-entitlement-label truncate">{label}</span>
                    </div>
                ))}
            </div>
        </div>
    );
}

function UsageMetric({ icon, label, value }: { icon: React.ReactNode; label: string; value: string }) {
    return (
        <div className="team-commercial-usage-metric min-w-0">
            <div className="team-commercial-usage-label flex items-center gap-1.5 text-[11px] text-foreground/42">
                {icon}
                {label}
            </div>
            <div className="team-commercial-usage-value mt-1.5 truncate text-sm font-semibold tabular-nums">{value}</div>
        </div>
    );
}

function mediaKind(mimeType: string) {
    if (mimeType.startsWith("image/")) return "image";
    if (mimeType.startsWith("video/")) return "video";
    if (mimeType.startsWith("audio/")) return "audio";
    return "file";
}

function resourceIcon(mimeType: string) {
    if (mimeType.startsWith("image/")) return <FileImage className="team-asset-type-icon size-4" />;
    if (mimeType.startsWith("video/")) return <FileVideo className="team-asset-type-icon size-4" />;
    if (mimeType.startsWith("audio/")) return <FileAudio className="team-asset-type-icon size-4" />;
    return <FileArchive className="team-asset-type-icon size-4" />;
}

function formatCredits(value: number) {
    return `${(value / 1_000_000).toLocaleString("zh-CN", { maximumFractionDigits: 2 })} 积分`;
}

function formatBytes(value: number) {
    if (!Number.isFinite(value) || value <= 0) return "0 B";
    const units = ["B", "KB", "MB", "GB", "TB"];
    const unitIndex = Math.min(Math.floor(Math.log(value) / Math.log(1024)), units.length - 1);
    return `${(value / 1024 ** unitIndex).toLocaleString("zh-CN", { maximumFractionDigits: unitIndex >= 3 ? 1 : 0 })} ${units[unitIndex]}`;
}

function formatDateTime(value: string) {
    return new Date(value).toLocaleString("zh-CN", { month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit" });
}
