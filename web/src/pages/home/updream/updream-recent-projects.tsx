import { useQuery } from "@tanstack/react-query";
import { ArrowRight, Clapperboard, Plus } from "lucide-react";
import { Link } from "react-router";

import { projectSummaryStage } from "@/lib/project-workbench";
import { projectsQueryOptions } from "@/queries/projects-query";
import type { ProjectSummary } from "@/services/api/projects";
import { useUserStore } from "@/stores/use-user-store";

import "@/pages/home/updream/updream-recent-projects.css";

const RECENT_PROJECT_LIMIT = 4;

export function UpdreamRecentProjects() {
    const hydrated = useUserStore((state) => state.hydrated);
    const user = useUserStore((state) => state.user);
    const projectsQuery = useQuery({
        ...projectsQueryOptions,
        enabled: hydrated && Boolean(user),
    });

    if (!hydrated || !user) return null;

    const recentProjects = [...(projectsQuery.data?.projects ?? [])].sort((left, right) => right.project.updatedAt.localeCompare(left.project.updatedAt)).slice(0, RECENT_PROJECT_LIMIT);

    return (
        <section className="updream-recent-projects">
            <div className="updream-recent-projects-inner">
                <div className="updream-recent-projects-heading">
                    <h2 className="updream-recent-projects-title">最近项目</h2>
                    <Link className="updream-recent-projects-all" to="/projects">
                        查看全部
                        <ArrowRight className="updream-recent-projects-all-icon" />
                    </Link>
                </div>

                {projectsQuery.isError ? (
                    <div className="updream-recent-projects-error">
                        <span className="updream-recent-projects-error-message">{projectsQuery.error instanceof Error ? projectsQuery.error.message : "最近项目加载失败"}</span>
                        <button type="button" className="updream-recent-projects-retry" onClick={() => void projectsQuery.refetch()}>
                            重新加载
                        </button>
                    </div>
                ) : (
                    <div className="updream-recent-projects-grid">
                        <Link className="updream-recent-project-create" to="/projects?create=1">
                            <span className="updream-recent-project-create-surface">
                                <Plus className="updream-recent-project-create-symbol" />
                                <span className="updream-recent-project-create-label">新建项目</span>
                            </span>
                        </Link>

                        {projectsQuery.isLoading
                            ? Array.from({ length: 2 }, (_, index) => <div key={index} className="updream-recent-project-skeleton" aria-label="正在加载最近项目" />)
                            : recentProjects.map((summary) => <RecentProjectCard key={summary.project.id} summary={summary} />)}
                    </div>
                )}
            </div>
        </section>
    );
}

function RecentProjectCard({ summary }: { summary: ProjectSummary }) {
    const stage = projectSummaryStage(summary);

    return (
        <Link className="updream-recent-project-card" to={`/projects/${summary.project.id}/overview`}>
            <span className="updream-recent-project-preview">
                <Clapperboard className="updream-recent-project-preview-icon" />
            </span>
            <span className="updream-recent-project-meta">
                <strong className="updream-recent-project-name">{summary.project.name}</strong>
                <span className="updream-recent-project-detail">
                    <span className="updream-recent-project-stage truncate">{stage.label}</span>
                    <time className="updream-recent-project-time shrink-0" dateTime={summary.project.updatedAt}>
                        {formatRelativeProjectTime(summary.project.updatedAt)}
                    </time>
                </span>
            </span>
        </Link>
    );
}

function formatRelativeProjectTime(value: string) {
    const elapsedMilliseconds = Date.now() - new Date(value).getTime();
    if (!Number.isFinite(elapsedMilliseconds) || elapsedMilliseconds < 0) return "刚刚";
    const elapsedMinutes = Math.floor(elapsedMilliseconds / 60_000);
    if (elapsedMinutes < 1) return "刚刚";
    if (elapsedMinutes < 60) return `${elapsedMinutes} 分钟前`;
    const elapsedHours = Math.floor(elapsedMinutes / 60);
    if (elapsedHours < 24) return `${elapsedHours} 小时前`;
    const elapsedDays = Math.floor(elapsedHours / 24);
    if (elapsedDays < 30) return `${elapsedDays} 天前`;
    return new Intl.DateTimeFormat("zh-CN", { month: "2-digit", day: "2-digit" }).format(new Date(value));
}
