import { useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { App, Button, Form, Input, Modal, Popover, Select } from "antd";
import { LayoutGrid, Plus, Search, SlidersHorizontal } from "lucide-react";
import { useNavigate, useSearchParams } from "react-router";

import { PageHeader, WorkspacePage } from "@/components/layout/workspace-page";
import { WorkspaceErrorState, WorkspaceLoadingState, WorkspaceState } from "@/components/layout/workspace-state";
import { projectBelongsToWorkspace } from "@/lib/workspace-scope";
import { projectsQueryKey, projectsQueryOptions } from "@/queries/projects-query";
import { createProject } from "@/services/api/projects";
import { getTeamWorkspace } from "@/services/api/teams";
import { useUserStore } from "@/stores/use-user-store";

import { ProjectGallery } from "./project-gallery";
import "./projects-workspace.css";

type ProjectForm = {
    name: string;
    aspectRatio: string;
    sourceType: string;
};

type ProjectStatusFilter = "all" | "active" | "archived";
type ProjectSort = "updated" | "progress" | "name";

export default function ProjectsPage() {
    const navigate = useNavigate();
    const queryClient = useQueryClient();
    const { message } = App.useApp();
    const user = useUserStore((state) => state.user);
    const workspaceScope = useUserStore((state) => state.workspaceScope);
    const [searchParams, setSearchParams] = useSearchParams();
    const [keyword, setKeyword] = useState("");
    const [status, setStatus] = useState<ProjectStatusFilter>("all");
    const [sort, setSort] = useState<ProjectSort>("updated");
    const createOpen = searchParams.get("create") === "1";

    const setCreateOpen = (open: boolean) => {
        const next = new URLSearchParams(searchParams);
        if (open) next.set("create", "1");
        else next.delete("create");
        setSearchParams(next, { replace: true });
    };

    const query = useQuery(projectsQueryOptions);
    const teamWorkspaceQuery = useQuery({
        queryKey: ["team-workspace", user?.id],
        queryFn: getTeamWorkspace,
        enabled: Boolean(user) && workspaceScope.kind === "team",
        staleTime: 30_000,
    });
    const activeTeam = workspaceScope.kind === "team" ? teamWorkspaceQuery.data?.teams.find((summary) => summary.team.id === workspaceScope.teamId) : undefined;
    const canCreateProject = workspaceScope.kind === "personal" || activeTeam?.capabilities.canManageProjects === true;

    const mutation = useMutation({
        mutationFn: (input: ProjectForm) =>
            createProject({
                ...input,
                type: "short-drama",
                teamId: workspaceScope.kind === "team" ? workspaceScope.teamId : undefined,
            }),
        onSuccess: ({ project }) => {
            setCreateOpen(false);
            void queryClient.invalidateQueries({ queryKey: projectsQueryKey });
            navigate(`/projects/${project.id}/overview`);
        },
        onError: (error) => {
            message.error(error instanceof Error ? error.message : "项目创建失败");
        },
    });

    const rows = useMemo(() => {
        const normalizedKeyword = keyword.trim().toLowerCase();
        return [...(query.data?.projects || [])]
            .filter(({ project }) => Boolean(user) && projectBelongsToWorkspace(project, user?.id || "", workspaceScope))
            .filter(({ project }) => status === "all" || project.status === status)
            .filter(({ project }) => !normalizedKeyword || `${project.name} ${project.description} ${project.stylePresetId}`.toLowerCase().includes(normalizedKeyword))
            .sort((left, right) => {
                if (sort === "name") {
                    return left.project.name.localeCompare(right.project.name, "zh-CN");
                }
                if (sort === "progress") {
                    const leftProgress = left.unitCount ? left.completedUnitCount / left.unitCount : 0;
                    const rightProgress = right.unitCount ? right.completedUnitCount / right.unitCount : 0;
                    return rightProgress - leftProgress;
                }
                return right.project.updatedAt.localeCompare(left.project.updatedAt);
            });
    }, [keyword, query.data, sort, status, user, workspaceScope]);

    const filtersActive = status !== "all" || sort !== "updated";
    const teamWorkspaceBlocked = workspaceScope.kind === "team" && (teamWorkspaceQuery.isLoading || teamWorkspaceQuery.isError || !activeTeam);
    const showGallery = !query.isLoading && !query.isError && !teamWorkspaceBlocked && (rows.length > 0 || (!keyword && status === "all"));
    const openCreateProject = () => {
        if (canCreateProject) setCreateOpen(true);
    };

    const resetFilters = () => {
        setStatus("all");
        setSort("updated");
    };

    const filterContent = (
        <div className="projects-workspace-filter-content">
            <label className="projects-workspace-filter-field">
                <span className="projects-workspace-filter-label">项目状态</span>
                <Select<ProjectStatusFilter>
                    className="projects-workspace-filter-select"
                    value={status}
                    onChange={setStatus}
                    options={[
                        { label: "全部状态", value: "all" },
                        { label: "进行中", value: "active" },
                        { label: "已归档", value: "archived" },
                    ]}
                />
            </label>
            <label className="projects-workspace-filter-field">
                <span className="projects-workspace-filter-label">排序方式</span>
                <Select<ProjectSort>
                    className="projects-workspace-filter-select"
                    value={sort}
                    onChange={setSort}
                    options={[
                        { label: "最近更新", value: "updated" },
                        { label: "章节进度", value: "progress" },
                        { label: "项目名称", value: "name" },
                    ]}
                />
            </label>
            {filtersActive ? (
                <Button type="text" size="small" className="projects-workspace-filter-reset" onClick={resetFilters}>
                    恢复默认
                </Button>
            ) : null}
        </div>
    );

    return (
        <WorkspacePage className="projects-workspace" layout="collection">
            <PageHeader
                title={workspaceScope.kind === "team" ? activeTeam?.team.name || "团队项目" : "我的项目"}
                description={workspaceScope.kind === "team" ? "浏览团队共享项目，并按团队权限创建制作空间。" : "继续最近的短剧项目，或创建新的制作空间。"}
                actions={
                    <>
                        <Popover content={filterContent} placement="bottomRight" trigger="click">
                            <Button
                                className={`projects-workspace-header-button projects-workspace-filter-button ${filtersActive ? "projects-workspace-filter-button--active" : ""}`}
                                icon={<SlidersHorizontal className="projects-workspace-header-icon size-3.5" />}
                            >
                                筛选
                            </Button>
                        </Popover>
                        <Button className="projects-workspace-header-button projects-page-canvas-button" icon={<LayoutGrid className="projects-workspace-header-icon size-3.5" />} onClick={() => navigate("/canvas")}>
                            画布
                        </Button>
                        <Button className="projects-workspace-header-button projects-workspace-create-button projects-page-create-button" icon={<Plus className="projects-workspace-header-icon size-3.5" />} onClick={openCreateProject} disabled={!canCreateProject} title={!canCreateProject && workspaceScope.kind === "team" ? "当前团队角色没有创建项目权限" : undefined}>
                            新建项目
                        </Button>
                        <Input
                            allowClear
                            className="projects-workspace-search"
                            prefix={<Search className="projects-workspace-search-icon size-4" />}
                            value={keyword}
                            placeholder="搜索"
                            aria-label="搜索项目"
                            onChange={(event) => setKeyword(event.target.value)}
                        />
                    </>
                }
            />

            {query.isError ? (
                <div className="projects-workspace-state">
                    <WorkspaceErrorState description={query.error instanceof Error ? query.error.message : "项目列表加载失败"} onRetry={() => void query.refetch()} />
                </div>
            ) : null}

            {query.isLoading ? (
                <div className="projects-workspace-state">
                    <WorkspaceLoadingState label="正在整理项目" detail="读取章节、画布与素材进度" />
                </div>
            ) : null}

            {workspaceScope.kind === "team" && teamWorkspaceQuery.isError ? (
                <div className="projects-workspace-state">
                    <WorkspaceErrorState description={teamWorkspaceQuery.error instanceof Error ? teamWorkspaceQuery.error.message : "团队工作区加载失败"} onRetry={() => void teamWorkspaceQuery.refetch()} />
                </div>
            ) : null}

            {workspaceScope.kind === "team" && teamWorkspaceQuery.isLoading ? (
                <div className="projects-workspace-state">
                    <WorkspaceLoadingState label="正在读取团队工作区" detail="核对团队权限与项目范围" />
                </div>
            ) : null}

            {workspaceScope.kind === "team" && teamWorkspaceQuery.isSuccess && !activeTeam ? (
                <div className="projects-workspace-state">
                    <WorkspaceErrorState description="当前团队已不可访问，请从右上角账户菜单切换工作区。" onRetry={() => void teamWorkspaceQuery.refetch()} />
                </div>
            ) : null}

            {showGallery ? <ProjectGallery rows={rows} onCreate={canCreateProject ? openCreateProject : undefined} /> : null}

            {!query.isLoading && !query.isError && !rows.length && (Boolean(keyword) || status !== "all") ? (
                <div className="projects-workspace-state">
                    <WorkspaceState
                        icon="projects"
                        title="没有匹配的项目"
                        description="调整搜索词或筛选条件后再试。"
                        action={
                            <Button
                                className="projects-workspace-reset-search"
                                onClick={() => {
                                    setKeyword("");
                                    resetFilters();
                                }}
                            >
                                清除筛选
                            </Button>
                        }
                    />
                </div>
            ) : null}

            <Modal title={workspaceScope.kind === "team" ? "创建团队短剧项目" : "创建短剧项目"} open={createOpen && canCreateProject} footer={null} destroyOnHidden onCancel={() => setCreateOpen(false)} width={500} className="projects-workspace-create-modal" styles={{ body: { paddingTop: 12 } }}>
                <Form<ProjectForm> className="projects-workspace-create-form" layout="vertical" initialValues={{ aspectRatio: "9:16", sourceType: "blank" }} onFinish={(values) => mutation.mutate(values)}>
                    <Form.Item className="projects-workspace-create-form-item" name="name" label="项目名称" rules={[{ required: true, whitespace: true, message: "请输入项目名称" }]}>
                        <Input autoFocus className="projects-workspace-create-name" placeholder="例如：长安夜行" />
                    </Form.Item>
                    <div className="projects-workspace-create-form-grid">
                        <Form.Item className="projects-workspace-create-form-item" name="aspectRatio" label="默认画幅">
                            <Select
                                className="projects-workspace-create-select"
                                options={[
                                    { label: "9:16 竖屏", value: "9:16" },
                                    { label: "16:9 横屏", value: "16:9" },
                                    { label: "1:1 方形", value: "1:1" },
                                ]}
                            />
                        </Form.Item>
                        <Form.Item className="projects-workspace-create-form-item" name="sourceType" label="内容来源">
                            <Select
                                className="projects-workspace-create-select"
                                options={[
                                    { label: "空白开始", value: "blank" },
                                    { label: "导入小说", value: "novel" },
                                    { label: "粘贴文本", value: "text" },
                                ]}
                            />
                        </Form.Item>
                    </div>
                    <p className="projects-workspace-create-help">创建后先进入项目概览。章节、画风和参考素材可以逐步补充。</p>
                    <div className="projects-workspace-create-actions">
                        <Button className="projects-workspace-create-cancel" onClick={() => setCreateOpen(false)}>
                            取消
                        </Button>
                        <Button className="projects-workspace-create-submit" type="primary" htmlType="submit" loading={mutation.isPending}>
                            创建项目
                        </Button>
                    </div>
                </Form>
            </Modal>
        </WorkspacePage>
    );
}
