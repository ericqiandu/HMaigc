import { BookOpenText, FolderKanban, Images, LayoutGrid, Plus } from "lucide-react";
import type { ReactElement } from "react";
import { Link } from "react-router";

import { projectSummaryCompletion, projectSummaryStage } from "@/lib/project-workbench";
import type { ProjectSummary } from "@/services/api/projects";

export function ProjectGallery({ rows, onCreate }: { rows: ProjectSummary[]; onCreate: () => void }) {
    return (
        <section className="projects-gallery" aria-label="项目列表">
            <button type="button" className="projects-gallery-create-card" onClick={onCreate}>
                <span className="projects-gallery-create-content">
                    <Plus className="projects-gallery-create-icon" aria-hidden="true" />
                    <span className="projects-gallery-create-label">新建项目</span>
                </span>
            </button>
            {rows.map((row) => (
                <ProjectCard key={row.project.id} row={row} />
            ))}
        </section>
    );
}

function ProjectCard({ row }: { row: ProjectSummary }) {
    const completion = projectSummaryCompletion(row);
    const stage = projectSummaryStage(row);
    const project = row.project;

    return (
        <article className="projects-gallery-card">
            <Link to={`/projects/${project.id}/overview`} className="projects-gallery-card-link" aria-label={`打开项目：${project.name}`}>
                <span className="projects-gallery-card-preview">
                    <span className="projects-gallery-card-cover-empty" aria-hidden="true">
                        <FolderKanban className="projects-gallery-card-cover-icon" />
                    </span>
                    <span className="projects-gallery-card-preview-meta">
                        <span className="projects-gallery-card-stage">{stage.label}</span>
                        <span className="projects-gallery-card-progress">{completion}%</span>
                    </span>
                    <span className="projects-gallery-card-progress-track" aria-hidden="true">
                        <span className="projects-gallery-card-progress-value" style={{ width: `${completion}%` }} />
                    </span>
                </span>
                <span className="projects-gallery-card-body">
                    <span className="projects-gallery-card-heading">
                        <strong className="projects-gallery-card-title">{project.name}</strong>
                        {project.status === "archived" ? <span className="projects-gallery-card-status">已归档</span> : null}
                    </span>
                    <span className="projects-gallery-card-footer">
                        <span className="projects-gallery-card-counts">
                            <ProjectCount icon={<BookOpenText className="projects-gallery-card-count-glyph" />} label="章节" value={row.unitCount} />
                            <ProjectCount icon={<LayoutGrid className="projects-gallery-card-count-glyph" />} label="画布" value={row.canvasCount} />
                            <ProjectCount icon={<Images className="projects-gallery-card-count-glyph" />} label="素材" value={row.assetCount} />
                        </span>
                        <span className="projects-gallery-card-updated">{formatProjectUpdatedAt(project.updatedAt)}</span>
                    </span>
                </span>
            </Link>
        </article>
    );
}

function ProjectCount({ icon, label, value }: { icon: ReactElement; label: string; value: number }) {
    return (
        <span className="projects-gallery-card-count" title={`${value} ${label}`}>
            <span className="projects-gallery-card-count-icon" aria-hidden="true">
                {icon}
            </span>
            <span className="projects-gallery-card-count-value">{value}</span>
            <span className="sr-only">{label}</span>
        </span>
    );
}

function formatProjectUpdatedAt(value: string) {
    const timestamp = new Date(value).getTime();
    if (!Number.isFinite(timestamp)) return "时间格式异常";

    const elapsedMinutes = Math.max(0, Math.floor((Date.now() - timestamp) / 60_000));
    if (elapsedMinutes < 1) return "刚刚编辑";
    if (elapsedMinutes < 60) return `编辑于 ${elapsedMinutes} 分钟前`;

    const elapsedHours = Math.floor(elapsedMinutes / 60);
    if (elapsedHours < 24) return `编辑于 ${elapsedHours} 小时前`;

    const elapsedDays = Math.floor(elapsedHours / 24);
    if (elapsedDays < 30) return `编辑于 ${elapsedDays} 天前`;

    return `编辑于 ${new Intl.DateTimeFormat("zh-CN", {
        year: "numeric",
        month: "2-digit",
        day: "2-digit",
    }).format(timestamp)}`;
}
