import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Alert, App, Button, Tooltip } from "antd";
import { BookOpenText, Images, LayoutDashboard, LayoutGrid, Plus, Settings2, type LucideIcon } from "lucide-react";
import { Link, Navigate, useNavigate, useParams } from "react-router";

import { createCanvasProjectWithRemoteSync } from "@/services/user-data-sync";
import { getProject } from "@/services/api/projects";
import { PageHeader, WorkspacePage } from "@/components/layout/workspace-page";
import { WorkspaceErrorState, WorkspaceLoadingState } from "@/components/layout/workspace-state";

import ProjectAssetsView from "./detail/assets";
import ProjectCanvasesView from "./detail/canvases";
import ProjectChaptersView from "./detail/chapters";
import ProjectOverviewView from "./detail/overview";
import ProjectSettingsView from "./detail/settings";

type DetailView = "overview" | "chapters" | "canvases" | "assets" | "settings";

const views: Array<{ key: DetailView; label: string; shortLabel: string; icon: LucideIcon }> = [
    { key: "overview", label: "制作概览", shortLabel: "概览", icon: LayoutDashboard },
    { key: "chapters", label: "剧情章节", shortLabel: "章节", icon: BookOpenText },
    { key: "canvases", label: "项目画布", shortLabel: "画布", icon: LayoutGrid },
    { key: "assets", label: "角色与资产", shortLabel: "资产", icon: Images },
    { key: "settings", label: "项目设置", shortLabel: "设置", icon: Settings2 },
];

export default function ProjectDetailPage() {
    const { projectId = "", view, chapterId } = useParams();
    const navigate = useNavigate();
    const queryClient = useQueryClient();
    const { message } = App.useApp();
    const activeView: DetailView = chapterId ? "chapters" : views.some((item) => item.key === view) ? view as DetailView : "overview";
    const detail = useQuery({ queryKey: ["project", projectId], queryFn: () => getProject(projectId), enabled: Boolean(projectId), refetchOnMount: "always" });
    const refreshProject = () => { void queryClient.invalidateQueries({ queryKey: ["project", projectId] }); void queryClient.invalidateQueries({ queryKey: ["projects"] }); };
    const createCanvas = () => {
        if (detail.data?.project.status === "archived") { message.warning("项目已归档，请先在项目设置中恢复"); return; }
        void createCanvasProjectWithRemoteSync(`${detail.data?.project.name || "项目"} · 新画布`, projectId).then(({ id, syncError }) => {
            if (syncError) message.warning(syncError instanceof Error ? `画布已创建，项目关联稍后重试：${syncError.message}` : "画布已创建，项目关联稍后重试");
            else refreshProject();
            navigate(`/canvas/${id}`);
        }).catch((error) => message.error(error instanceof Error ? error.message : "画布创建失败"));
    };

    if (detail.isLoading) return <WorkspacePage><WorkspaceLoadingState label="正在打开项目工作台" detail="读取章节、画布、资产和当前进度" /></WorkspacePage>;
    if (detail.isError || !detail.data) return <WorkspacePage><WorkspaceErrorState title="项目不可用" description="项目不存在、已被删除，或当前账号没有访问权限。" actionLabel="返回项目中心" onRetry={() => navigate("/projects")} /></WorkspacePage>;
    if (!chapterId && (!view || !views.some((item) => item.key === view))) return <Navigate to={`/projects/${projectId}/overview`} replace />;
    const chapterHref = projectChapterHref(detail.data.units, projectId, chapterId);
    return (
        <WorkspacePage className="project-workbench-page !overflow-hidden" fluid>
            <div className="flex h-full min-h-0 flex-col px-4 pt-20 sm:px-6 md:px-[104px] md:pt-[90px]">
                <PageHeader
                    title={detail.data.project.name}
                    backTo="/projects"
                    backLabel="返回项目中心"
                    meta={(
                        <span className="inline-flex shrink-0 items-center gap-1.5 text-[11px] font-medium text-foreground/45">
                            <span className={`size-1.5 rounded-full ${detail.data.project.status === "archived" ? "bg-foreground/30" : "bg-[var(--workspace-accent)]"}`} />
                            {detail.data.project.status === "archived" ? "已归档" : "进行中"}
                        </span>
                    )}
                    actions={(
                        <Tooltip title="在当前项目中新建画布">
                            <Button type="primary" className="!h-9 !px-3" icon={<Plus className="size-4" />} onClick={createCanvas} aria-label="新建项目画布">新建画布</Button>
                        </Tooltip>
                    )}
                />
                <nav className="thin-scrollbar mt-3 flex h-11 w-full shrink-0 items-end gap-1 overflow-x-auto border-b border-border/70" aria-label="项目导航">
                    {views.map((item) => {
                        const Icon = item.icon;
                        const active = item.key === activeView;
                        const href = item.key === "chapters" ? chapterHref : `/projects/${projectId}/${item.key}`;
                        return (
                            <Link
                                key={item.key}
                                to={href}
                                className={`relative flex h-10 shrink-0 items-center gap-2 px-2.5 text-xs transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring sm:px-3 ${active ? "font-semibold text-foreground after:absolute after:inset-x-2.5 after:bottom-0 after:h-0.5 after:bg-[var(--workspace-accent)]" : "text-foreground/48 hover:bg-foreground/[.04] hover:text-foreground"}`}
                                aria-current={active ? "page" : undefined}
                            >
                                <Icon className={`size-3.5 shrink-0 ${active ? "text-[var(--workspace-accent)]" : "text-foreground/38"}`} />
                                <span className="sm:hidden">{item.shortLabel}</span>
                                <span className="hidden sm:inline">{item.label}</span>
                            </Link>
                        );
                    })}
                </nav>
                {detail.data.project.status === "archived" ? <Alert type="warning" showIcon banner message="项目已归档，恢复后才能创建画布和生成任务" className="!mt-3 !rounded-md !border-border/70" /> : null}
                <div className={activeView === "chapters" ? "mt-4 min-h-0 flex-1 overflow-hidden pb-4" : "thin-scrollbar mt-5 min-h-0 flex-1 overflow-y-auto pb-8"}>
                    <div className={activeView === "chapters" ? "h-full w-full" : "w-full"}>
                            {activeView === "overview" ? <ProjectOverviewView detail={detail.data} refreshProject={refreshProject} onCreateCanvas={createCanvas} /> : null}
                            {activeView === "chapters" ? <ProjectChaptersView detail={detail.data} refreshProject={refreshProject} onCreateCanvas={createCanvas} /> : null}
                            {activeView === "canvases" ? <ProjectCanvasesView detail={detail.data} refreshProject={refreshProject} onCreateCanvas={createCanvas} /> : null}
                            {activeView === "assets" ? <ProjectAssetsView detail={detail.data} refreshProject={refreshProject} onCreateCanvas={createCanvas} /> : null}
                            {activeView === "settings" ? <ProjectSettingsView detail={detail.data} refreshProject={refreshProject} onCreateCanvas={createCanvas} /> : null}
                    </div>
                </div>
            </div>
        </WorkspacePage>
    );
}

function projectChapterHref(units: Array<{ id: string; position: number }>, projectId: string, routeChapterId?: string) {
    const rememberedId = sessionStorage.getItem(`project-active-chapter:${projectId}`) || "";
    const targetId = [routeChapterId, rememberedId].find((id) => id && units.some((unit) => unit.id === id)) || units.slice().sort((left, right) => left.position - right.position)[0]?.id;
    return targetId ? `/projects/${projectId}/chapters/${targetId}` : `/projects/${projectId}/chapters`;
}
