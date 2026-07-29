import { useMemo, useRef, useState } from "react";
import { useNavigate } from "react-router";
import { useQuery } from "@tanstack/react-query";
import { App, Button, Dropdown, Input, Modal, Popover, Select } from "antd";
import { CheckSquare2, Download, FileUp, MoreHorizontal, Plus, Search, SlidersHorizontal, Trash2 } from "lucide-react";

import { ListToolbar, PageHeader, PaginationBar, WorkspacePage } from "@/components/layout/workspace-page";
import { WorkspaceLoadingState } from "@/components/layout/workspace-state";

import { readZip } from "@/lib/zip";
import { setMediaBlob } from "@/services/file-storage";
import { setImageBlob } from "@/services/image-storage";
import { CanvasProjectCard } from "@/components/canvas/canvas-project-card";
import type { CanvasExportFile } from "@/types/canvas-export";
import { useCanvasStore } from "@/stores/canvas/use-canvas-store";
import { useCanvasUiStore } from "@/stores/canvas/use-canvas-ui-store";
import { exportCanvasProjects } from "@/lib/canvas/canvas-export";
import { saveCanvasDrawing, type CanvasDrawingRenderDraft } from "@/lib/canvas/canvas-drawing-storage";
import { createCanvasProjectWithRemoteSync, saveRemoteUserDataNow } from "@/services/user-data-sync";
import { listProjects } from "@/services/api/projects";

export default function CanvasPage() {
    const { message } = App.useApp();
    const navigate = useNavigate();
    const inputRef = useRef<HTMLInputElement>(null);
    const [keyword, setKeyword] = useState("");
    const [sort, setSort] = useState<"updated" | "name" | "nodes">("updated");
    const [projectFilter, setProjectFilter] = useState("all");
    const [page, setPage] = useState(1);
    const [pageSize, setPageSize] = useState(24);
    const [selectionMode, setSelectionMode] = useState(false);
    const hydrated = useCanvasStore((state) => state.hydrated);
    const projects = useCanvasStore((state) => state.projects);
    const importProject = useCanvasStore((state) => state.importProject);
    const selectedIds = useCanvasUiStore((state) => state.selectedProjectIds);
    const setDeleteIds = useCanvasUiStore((state) => state.setDeleteProjectIds);
    const updateProject = useCanvasStore((state) => state.updateProject);
    const [associationOpen, setAssociationOpen] = useState(false);
    const [associationProjectId, setAssociationProjectId] = useState("");
    const projectQuery = useQuery({ queryKey: ["projects"], queryFn: listProjects });

    const enterProject = (id: string) => {
        navigate(`/canvas/${id}`);
    };
    const createAndEnter = () => {
        void createCanvasProjectWithRemoteSync(`自由画布 ${projects.length + 1}`).then(({ id, syncError }) => {
            if (syncError) message.warning(syncError instanceof Error ? `画布已在本地创建，云端同步失败：${syncError.message}` : "画布已在本地创建，云端同步失败");
            enterProject(id);
        });
    };
    const filteredProjects = useMemo(() => {
        const query = keyword.trim().toLowerCase();
        const scoped = projects.filter((project) => projectFilter === "all" || (projectFilter === "independent" ? !project.projectId : project.projectId === projectFilter));
        const values = query ? scoped.filter((project) => project.title.toLowerCase().includes(query)) : [...scoped];
        values.sort((a, b) => sort === "name" ? a.title.localeCompare(b.title, "zh-CN") : sort === "nodes" ? b.nodes.length - a.nodes.length : new Date(b.updatedAt).getTime() - new Date(a.updatedAt).getTime());
        return values;
    }, [keyword, projectFilter, projects, sort]);
    const projectNames = useMemo(() => new Map((projectQuery.data?.projects || []).map(({ project }) => [project.id, project.name])), [projectQuery.data]);
    const visibleProjects = filteredProjects.slice((page - 1) * pageSize, page * pageSize);
    const selectedProjects = projects.filter((project) => selectedIds.includes(project.id));
    const associateSelected = async (nextProjectId = associationProjectId) => {
        const projectId = nextProjectId || undefined;
        selectedIds.forEach((id) => updateProject(id, { projectId }));
        try {
            await saveRemoteUserDataNow();
            message.success(projectId ? "已加入项目" : "已移出项目，画布仍保留");
            setAssociationOpen(false);
        } catch (error) {
            message.error(error instanceof Error ? `画布关系保存失败：${error.message}` : "画布关系保存失败");
        }
    };
    const importCanvas = async (file?: File) => {
        if (!file) return;
        try {
            const zip = await readZip(file);
            const projectFile = zip.get("projects.json");
            if (!projectFile) throw new Error("missing projects.json");
            const data = JSON.parse(await projectFile.text()) as CanvasExportFile;
            await Promise.all(
                data.projects.flatMap((project) =>
                    project.files.map(async (item) => {
                        const blob = zip.get(item.path);
                        if (!blob) return;
                        const typedBlob = blob.type ? blob : blob.slice(0, blob.size, item.mimeType);
                        await (item.storageKey.startsWith("image:") ? setImageBlob(item.storageKey, typedBlob) : setMediaBlob(item.storageKey, typedBlob));
                    }),
                ),
            );
            await Promise.all(data.projects.map(async (item) => {
                const importedProjectId = importProject(item.project);
                await Promise.all((item.drawingDocuments || []).map((document) => {
                    const previewFile = document.previewPath ? zip.get(document.previewPath) : undefined;
                    const preview = previewFile && !previewFile.type ? previewFile.slice(0, previewFile.size, "image/png") : previewFile;
                    const renderFile = document.generationRender?.path ? zip.get(document.generationRender.path) : undefined;
                    const renderBlob = renderFile && !renderFile.type ? renderFile.slice(0, renderFile.size, document.generationRender?.mimeType || "image/png") : renderFile;
                    const render = renderBlob && document.generationRender
                        ? {
                              blob: renderBlob,
                              pageId: document.generationRender.pageId,
                              width: document.generationRender.width,
                              height: document.generationRender.height,
                              mimeType: document.generationRender.mimeType,
                              background: document.generationRender.background,
                          } satisfies CanvasDrawingRenderDraft
                        : undefined;
                    return saveCanvasDrawing(importedProjectId, document.drawingId, document.snapshot, { ...document, version: 1, revision: Math.max(0, document.revision - 1) }, preview, render);
                }));
            }));
            message.success(`已导入 ${data.projects.length} 个画布`);
        } catch {
            message.error("导入失败，请选择有效的画布压缩包");
        } finally {
            if (inputRef.current) inputRef.current.value = "";
        }
    };

    return (
        <WorkspacePage grid fluid className="canvas-library-page">
            <section className="canvas-library-content min-h-full px-4 pb-8 pt-20 sm:px-6 md:pl-[104px] md:pr-[104px] md:pt-[90px]">
                <PageHeader
                    title="我的画布"
                    meta={<span className="canvas-library-count text-xs tabular-nums text-foreground/38">{hydrated ? filteredProjects.length : "—"}</span>}
                />
                <ListToolbar
                    active={Boolean(keyword || projectFilter !== "all" || sort !== "updated")}
                    onReset={() => { setKeyword(""); setProjectFilter("all"); setSort("updated"); setPage(1); }}
                    trailing={(
                        <>
                            <Button className="canvas-library-selection-button !h-9 !px-3" type={selectionMode ? "primary" : "default"} icon={<CheckSquare2 className="canvas-library-selection-icon size-3.5" />} onClick={() => setSelectionMode((active) => !active)}>多选</Button>
                            <Button className="canvas-library-import-button !h-9 !px-3" disabled={!hydrated} icon={<FileUp className="canvas-library-import-icon size-3.5" />} onClick={() => inputRef.current?.click()}>导入</Button>
                            {projects.length ? (
                                <Dropdown menu={{ items: [{ key: "delete-all", danger: true, icon: <Trash2 className="canvas-library-delete-icon size-3.5" />, label: "删除全部画布", onClick: () => setDeleteIds(projects.map((project) => project.id)) }] }} trigger={["click"]}>
                                    <Button className="canvas-library-more-button !size-9 !p-0" aria-label="更多画布操作" title="更多操作" icon={<MoreHorizontal className="canvas-library-more-icon size-4" />} />
                                </Dropdown>
                            ) : null}
                        </>
                    )}
                >
                    <div className="canvas-library-search min-w-0 w-full sm:w-60">
                        <Input allowClear className="canvas-library-search-input !h-9" prefix={<Search className="canvas-library-search-icon size-3.5 text-foreground/40" />} value={keyword} placeholder="搜索画布" aria-label="搜索画布" onChange={(event) => { setKeyword(event.target.value); setPage(1); }} />
                    </div>
                    <Popover
                        trigger="click"
                        placement="bottomLeft"
                        content={(
                            <div className="canvas-library-filter-panel grid w-56 gap-3">
                                <label className="canvas-library-filter-field grid gap-1.5 text-xs text-foreground/55">
                                    所属项目
                                    <Select aria-label="按所属项目筛选" className="canvas-library-project-filter w-full" value={projectFilter} onChange={(value) => { setProjectFilter(value); setPage(1); }} options={[{ label: "全部项目", value: "all" }, { label: "自由画布", value: "independent" }, ...(projectQuery.data?.projects || []).map(({ project }) => ({ label: project.name, value: project.id }))]} />
                                </label>
                                <label className="canvas-library-filter-field grid gap-1.5 text-xs text-foreground/55">
                                    排序方式
                                    <Select aria-label="画布排序" className="canvas-library-sort-filter w-full" value={sort} onChange={(value) => { setSort(value); setPage(1); }} options={[{ label: "最近更新", value: "updated" }, { label: "名称排序", value: "name" }, { label: "节点数量", value: "nodes" }]} />
                                </label>
                            </div>
                        )}
                    >
                        <Button className="canvas-library-filter-button !size-9 !p-0" aria-label="筛选与排序" title="筛选与排序" icon={<SlidersHorizontal className="canvas-library-filter-icon size-3.5" />} />
                    </Popover>
                </ListToolbar>

                {selectedIds.length ? (
                    <div className="app-canvas-selection-toolbar mt-3 flex min-h-10 flex-wrap items-center gap-2 rounded-lg bg-foreground/[.05] px-3 py-1.5 text-xs">
                        <strong className="canvas-library-selection-count mr-auto font-medium">已选 {selectedIds.length} 个画布</strong>
                        <Button size="small" disabled={!hydrated || projectQuery.isLoading} onClick={() => { setAssociationProjectId(selectedProjects[0]?.projectId || ""); setAssociationOpen(true); }}>加入项目</Button>
                        {selectedProjects.some((project) => project.projectId) ? <Button size="small" disabled={!hydrated} onClick={() => { setAssociationProjectId(""); void associateSelected(""); }}>移出项目</Button> : null}
                        <Button size="small" disabled={!hydrated} icon={<Download className="size-3.5" />} onClick={() => void exportCanvasProjects(selectedProjects, `HMaigc画布-${selectedIds.length}个画布`)}>导出</Button>
                        <Button size="small" danger disabled={!hydrated} onClick={() => setDeleteIds(selectedIds)}>删除</Button>
                    </div>
                ) : null}

                {!hydrated ? (
                    <WorkspaceLoadingState label="正在恢复画布" detail="读取本地缓存与账号同步状态" />
                ) : (
                    <div className="canvas-library-grid mt-5 grid grid-cols-1 gap-x-3 gap-y-5 sm:grid-cols-[repeat(auto-fill,minmax(240px,260px))] sm:justify-start">
                        {!keyword && projectFilter === "all" ? (
                            <article className="canvas-library-new-card group min-w-0">
                                <button type="button" className="canvas-library-new-button flex aspect-[260/207] w-full flex-col items-center justify-center gap-2 rounded-lg bg-foreground/[.035] text-foreground/55 transition hover:bg-foreground/[.07] hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-foreground/30" disabled={!hydrated} onClick={createAndEnter}>
                                    <span className="canvas-library-new-icon grid size-9 place-items-center rounded-full bg-foreground/[.07] transition group-hover:bg-foreground/[.1]">
                                        <Plus className="canvas-library-new-plus size-4" />
                                    </span>
                                    <span className="canvas-library-new-label text-sm font-medium">新建画布</span>
                                </button>
                            </article>
                        ) : null}
                        {visibleProjects.map((project) => (
                            <CanvasProjectCard key={project.id} project={project} projectName={project.projectId ? projectNames.get(project.projectId) || "未同步项目" : undefined} selectionMode={selectionMode} />
                        ))}
                    </div>
                )}

                {hydrated && !visibleProjects.length && (keyword || projectFilter !== "all") ? <p className="canvas-library-empty py-12 text-center text-xs text-foreground/38">没有匹配的画布</p> : null}
                {hydrated && visibleProjects.length ? <p className="canvas-library-end py-8 text-center text-xs text-foreground/32">没有更多画布了</p> : null}
                <PaginationBar current={page} pageSize={pageSize} total={filteredProjects.length} pageSizeOptions={[12, 24, 48]} onChange={(nextPage, nextPageSize) => { setPage(nextPageSize !== pageSize ? 1 : nextPage); setPageSize(nextPageSize); }} />

                <input ref={inputRef} type="file" accept="application/zip,.zip" className="hidden" onChange={(event) => void importCanvas(event.target.files?.[0])} />
                <Modal title="加入项目" open={associationOpen} okText="保存关联" cancelText="取消" okButtonProps={{ disabled: !associationProjectId, loading: projectQuery.isFetching }} onCancel={() => setAssociationOpen(false)} onOk={() => void associateSelected()}>
                    <p className="mb-3 text-sm text-foreground/60">选中的画布会保留原有节点和本地媒体，只增加项目关联。</p>
                    <Select className="w-full" value={associationProjectId || undefined} placeholder="选择项目" options={(projectQuery.data?.projects || []).map((item) => ({ label: item.project.name, value: item.project.id }))} onChange={setAssociationProjectId} />
                </Modal>
            </section>
        </WorkspacePage>
    );
}
