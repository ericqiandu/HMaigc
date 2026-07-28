import { useQuery } from "@tanstack/react-query";
import { ArrowRight, Clapperboard, Plus } from "lucide-react";
import { Link } from "react-router";

import { projectSummaryStage } from "@/lib/project-workbench";
import { listProjects, type ProjectSummary } from "@/services/api/projects";
import { useUserStore } from "@/stores/use-user-store";

const RECENT_PROJECT_LIMIT = 4;

export function UpdreamRecentProjects() {
    const hydrated = useUserStore((state) => state.hydrated);
    const user = useUserStore((state) => state.user);
    const projectsQuery = useQuery({
        queryKey: ["projects"],
        queryFn: listProjects,
        enabled: hydrated && Boolean(user),
    });

    if (!hydrated || !user) return null;

    const recentProjects = [...(projectsQuery.data?.projects ?? [])]
        .sort((left, right) => right.project.updatedAt.localeCompare(left.project.updatedAt))
        .slice(0, RECENT_PROJECT_LIMIT);

    return (
        <section className="updream-recent-projects w-full pb-14">
            <div className="updream-recent-projects-inner mx-auto flex w-full max-w-[1408px] flex-col gap-4 px-4 sm:px-8">
                <div className="updream-recent-projects-heading flex h-7 items-center justify-between">
                    <h2 className="updream-recent-projects-title text-xl font-semibold leading-7 tracking-[-0.02em]">最近项目</h2>
                    <Link className="updream-recent-projects-all inline-flex items-center gap-1 text-[13px] transition-colors" to="/projects">
                        查看全部
                        <ArrowRight className="updream-recent-projects-all-icon size-3.5" />
                    </Link>
                </div>

                {projectsQuery.isError ? (
                    <div className="updream-recent-projects-error flex min-h-24 items-center justify-between gap-4 px-4 text-sm">
                        <span className="updream-recent-projects-error-message">
                            {projectsQuery.error instanceof Error ? projectsQuery.error.message : "最近项目加载失败"}
                        </span>
                        <button
                            type="button"
                            className="updream-recent-projects-retry text-xs font-medium"
                            onClick={() => void projectsQuery.refetch()}
                        >
                            重新加载
                        </button>
                    </div>
                ) : (
                    <div className="updream-recent-projects-grid grid grid-cols-2 gap-4 sm:grid-cols-3 lg:grid-cols-5">
                        <Link
                            className="updream-recent-project-create group h-56 rounded-[16px] p-2.5 transition-transform duration-200 hover:scale-[1.02]"
                            to="/projects?create=1"
                        >
                            <span className="updream-recent-project-create-surface flex h-full w-full flex-col items-center justify-center gap-[11px] rounded-xl">
                                <Plus className="updream-recent-project-create-symbol size-7 transition-transform group-hover:scale-110" />
                                <span className="updream-recent-project-create-label text-base font-normal leading-6">新建项目</span>
                            </span>
                        </Link>

                        {projectsQuery.isLoading
                            ? Array.from({ length: 2 }, (_, index) => (
                                  <div
                                      key={index}
                                      className="updream-recent-project-skeleton h-56 animate-pulse rounded-[16px]"
                                      aria-label="正在加载最近项目"
                                  />
                              ))
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
        <Link
            className="updream-recent-project-card group flex h-56 flex-col rounded-[16px] p-2.5 transition-transform duration-200 hover:scale-[1.02]"
            to={`/projects/${summary.project.id}/overview`}
        >
            <span className="updream-recent-project-preview flex min-h-0 flex-1 items-center justify-center rounded-xl">
                <Clapperboard className="updream-recent-project-preview-icon size-8 transition-transform group-hover:scale-105" />
            </span>
            <span className="updream-recent-project-meta block px-1 pb-1 pt-3">
                <strong className="updream-recent-project-name block truncate text-sm font-semibold leading-5">
                    {summary.project.name}
                </strong>
                <span className="updream-recent-project-detail mt-1 flex items-center justify-between gap-2 text-xs leading-4">
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
