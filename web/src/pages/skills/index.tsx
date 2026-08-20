import { useEffect, useMemo, useState, type ReactNode } from "react";
import { App, Button, Collapse, Drawer, Input, Skeleton } from "antd";
import { Check, Heart, Search, ShieldCheck, Sparkles, Zap } from "lucide-react";

import { PaginationBar, WorkspacePage } from "@/components/layout/workspace-page";
import { WorkspaceErrorState, WorkspaceState } from "@/components/layout/workspace-state";
import { renderSkillPrompt } from "@/lib/canvas/canvas-skill-mentions";
import { activateSkill, deactivateSkill, favoriteSkill, getSkill, listActivatedSkills, listFavoriteSkills, listSkillsCatalog, skillImageUrl, unfavoriteSkill, type PlatformSkill } from "@/services/api/skills";

import { SkillCoverFallback, SkillMarketCard, SkillMarketSkeleton } from "./skill-market-card";
import "./skills-workspace.css";

type SkillTab = "all" | "activated" | "favorites";

const PAGE_SIZE = 20;
export default function SkillsPage() {
    const { message } = App.useApp();
    const [tab, setTab] = useState<SkillTab>("all");
    const [category, setCategory] = useState("all");
    const [categories, setCategories] = useState<string[]>([]);
    const [search, setSearch] = useState("");
    const [debouncedSearch, setDebouncedSearch] = useState("");
    const [page, setPage] = useState(1);
    const [pageSize, setPageSize] = useState(PAGE_SIZE);
    const [skills, setSkills] = useState<PlatformSkill[]>([]);
    const [total, setTotal] = useState(0);
    const [loading, setLoading] = useState(false);
    const [loadError, setLoadError] = useState<string | null>(null);
    const [detailLoading, setDetailLoading] = useState(false);
    const [activeSkill, setActiveSkill] = useState<PlatformSkill | null>(null);
    const [mutatingDir, setMutatingDir] = useState<string | null>(null);
    const [reloadKey, setReloadKey] = useState(0);
    const isPagedTab = tab === "all";

    useEffect(() => {
        const timer = window.setTimeout(() => setDebouncedSearch(search.trim()), 260);
        return () => window.clearTimeout(timer);
    }, [search]);

    useEffect(() => {
        let cancelled = false;
        setLoading(true);
        setLoadError(null);
        const request =
            tab === "activated"
                ? listActivatedSkills().then(({ skills }) => ({ skills, total: skills.length, page: 1, page_size: skills.length || pageSize, categories: [] as string[] }))
                : tab === "favorites"
                  ? listFavoriteSkills().then(({ skills }) => ({ skills, total: skills.length, page: 1, page_size: skills.length || pageSize, categories: [] as string[] }))
                  : listSkillsCatalog({
                        page,
                        page_size: pageSize,
                        search: debouncedSearch,
                        categories: category === "all" ? undefined : [category],
                    });

        request
            .then((result) => {
                if (cancelled) return;
                setSkills(result.skills);
                setTotal(result.total);
                if (result.categories.length) setCategories((current) => Array.from(new Set([...current, ...result.categories])).sort((a, b) => a.localeCompare(b, "zh-CN")));
            })
            .catch((error) => {
                if (cancelled) return;
                setSkills([]);
                setTotal(0);
                setLoadError(error instanceof Error ? error.message : "技能加载失败");
            })
            .finally(() => {
                if (!cancelled) setLoading(false);
            });
        return () => {
            cancelled = true;
        };
    }, [category, debouncedSearch, page, pageSize, reloadKey, tab]);

    const visibleSkills = useMemo(() => {
        if (isPagedTab || !debouncedSearch) return skills;
        const query = debouncedSearch.toLowerCase();
        return skills.filter((skill) => `${skill.name} ${skill.description} ${skillUploaderLabel(skill.uploader_name)}`.toLowerCase().includes(query));
    }, [debouncedSearch, isPagedTab, skills]);
    const displayedSkills = isPagedTab ? visibleSkills : visibleSkills.slice((page - 1) * pageSize, page * pageSize);
    const displayedTotal = isPagedTab ? total : visibleSkills.length;

    const openSkill = async (skill: PlatformSkill) => {
        setActiveSkill(skill);
        setDetailLoading(true);
        try {
            const result = await getSkill(skill.dir);
            setActiveSkill(result.skill);
            patchSkill(result.skill);
        } catch (error) {
            message.error(error instanceof Error ? error.message : "技能详情加载失败");
        } finally {
            setDetailLoading(false);
        }
    };

    const patchSkill = (next: PlatformSkill) => {
        setSkills((items) =>
            items.flatMap((item) => {
                if (item.dir !== next.dir) return [item];
                const merged = mergeSkill(item, next);
                if (tab === "activated" && !merged.activated) return [];
                if (tab === "favorites" && !merged.liked) return [];
                return [merged];
            }),
        );
        setActiveSkill((current) => (current?.dir === next.dir ? mergeSkill(current, next) : current));
    };

    const toggleActivation = async (skill: PlatformSkill) => {
        setMutatingDir(skill.dir);
        try {
            const result = skill.activated ? await deactivateSkill(skill.dir) : await activateSkill(skill.dir);
            patchSkill(result.skill);
            message.success(result.skill.activated ? "已激活" : "已取消激活");
        } catch (error) {
            message.error(error instanceof Error ? error.message : "状态更新失败");
        } finally {
            setMutatingDir(null);
        }
    };

    const toggleFavorite = async (skill: PlatformSkill) => {
        setMutatingDir(skill.dir);
        try {
            const result = skill.liked ? await unfavoriteSkill(skill.dir) : await favoriteSkill(skill.dir);
            patchSkill(result.skill);
            message.success(result.skill.liked ? "已收藏" : "已取消收藏");
        } catch (error) {
            message.error(error instanceof Error ? error.message : "收藏更新失败");
        } finally {
            setMutatingDir(null);
        }
    };

    return (
        <>
            <WorkspacePage layout="collection" className="skills-workspace-page">
                <header className="skills-market-header">
                    <nav className="skills-market-tabs" aria-label="技能范围">
                        <button
                            type="button"
                            className={`skills-market-tab ${tab !== "activated" ? "skills-market-tab--active" : ""}`}
                            aria-current={tab !== "activated" ? "page" : undefined}
                            onClick={() => {
                                setTab("all");
                                setCategory("all");
                                setPage(1);
                            }}
                        >
                            技能广场
                        </button>
                        <button
                            type="button"
                            className={`skills-market-tab ${tab === "activated" ? "skills-market-tab--active" : ""}`}
                            aria-current={tab === "activated" ? "page" : undefined}
                            onClick={() => {
                                setTab("activated");
                                setCategory("all");
                                setPage(1);
                            }}
                        >
                            我的技能
                        </button>
                    </nav>
                </header>

                <div className="skills-market-toolbar">
                    <div className="skills-market-filters" aria-label="技能分类">
                        {tab !== "activated" ? (
                            <>
                                <button
                                    type="button"
                                    className={`skills-market-filter ${tab === "all" && category === "all" ? "skills-market-filter--active" : ""}`}
                                    onClick={() => {
                                        setTab("all");
                                        setCategory("all");
                                        setPage(1);
                                    }}
                                >
                                    全部
                                </button>
                                <button
                                    type="button"
                                    className={`skills-market-filter ${tab === "favorites" ? "skills-market-filter--active" : ""}`}
                                    onClick={() => {
                                        setTab("favorites");
                                        setCategory("all");
                                        setPage(1);
                                    }}
                                >
                                    我的收藏
                                </button>
                                {categories.map((value) => (
                                    <button
                                        key={value}
                                        type="button"
                                        className={`skills-market-filter ${tab === "all" && category === value ? "skills-market-filter--active" : ""}`}
                                        onClick={() => {
                                            setTab("all");
                                            setCategory(value);
                                            setPage(1);
                                        }}
                                    >
                                        {value}
                                    </button>
                                ))}
                            </>
                        ) : (
                            <span className="skills-market-activated-label">已激活并可由 Agent 调用的技能</span>
                        )}
                    </div>
                    <Input
                        allowClear
                        className="skills-market-search"
                        prefix={<Search className="skills-market-search-icon" />}
                        value={search}
                        placeholder="搜索技能"
                        aria-label="搜索技能或作者"
                        onChange={(event) => {
                            setPage(1);
                            setSearch(event.target.value);
                        }}
                    />
                </div>

                {loadError ? (
                    <div className="skills-market-error">
                        <WorkspaceErrorState description={loadError} onRetry={() => setReloadKey((value) => value + 1)} />
                    </div>
                ) : loading ? (
                    <SkillMarketSkeleton />
                ) : displayedSkills.length ? (
                    <div className="skills-market-grid">
                        {displayedSkills.map((skill) => (
                            <SkillMarketCard key={skill.dir} skill={skill} loading={mutatingDir === skill.dir} onOpen={() => openSkill(skill)} onActivate={() => toggleActivation(skill)} onFavorite={() => toggleFavorite(skill)} />
                        ))}
                    </div>
                ) : (
                    <div className="skills-market-empty">
                        <WorkspaceState icon="skills" title="暂无匹配技能" description="换一个关键词、分类或技能范围继续查找。" />
                    </div>
                )}

                <PaginationBar
                    current={page}
                    pageSize={pageSize}
                    total={displayedTotal}
                    pageSizeOptions={[20, 40, 60]}
                    onChange={(nextPage, nextPageSize) => {
                        setPage(nextPageSize !== pageSize ? 1 : nextPage);
                        setPageSize(nextPageSize);
                    }}
                />
            </WorkspacePage>

            <SkillDetailModal skill={activeSkill} loading={detailLoading} mutating={Boolean(activeSkill && mutatingDir === activeSkill.dir)} onClose={() => setActiveSkill(null)} onActivate={toggleActivation} onFavorite={toggleFavorite} />
        </>
    );
}

function SkillDetailModal({
    skill,
    loading,
    mutating,
    onClose,
    onActivate,
    onFavorite,
}: {
    skill: PlatformSkill | null;
    loading: boolean;
    mutating: boolean;
    onClose: () => void;
    onActivate: (skill: PlatformSkill) => void;
    onFavorite: (skill: PlatformSkill) => void;
}) {
    const injectedPrompt = skill ? renderSkillPrompt(skill) : "";

    return (
        <Drawer open={Boolean(skill)} size="large" onClose={onClose} destroyOnHidden title={skill?.name || "技能详情"}>
            {skill ? (
                <div className="space-y-4">
                    <div className="overflow-hidden rounded-md bg-stone-100 dark:bg-stone-900">
                        {skill.cover_url ? <img src={skillImageUrl(skill.cover_url)} alt="" className="aspect-[16/8] w-full object-cover" /> : <SkillCoverFallback skill={skill} />}
                    </div>
                    <div className="flex items-center gap-2">
                        <Button className="flex-1" loading={mutating} type={skill.activated ? "default" : "primary"} icon={skill.activated ? <Check className="size-4" /> : <Zap className="size-4" />} onClick={() => onActivate(skill)}>
                            {skill.activated ? "已激活" : "激活"}
                        </Button>
                        <Button loading={mutating} icon={<Heart className={`size-4 ${skill.liked ? "fill-current text-rose-500" : ""}`} />} onClick={() => onFavorite(skill)}>
                            收藏
                        </Button>
                    </div>
                    {loading ? (
                        <Skeleton active paragraph={{ rows: 14 }} />
                    ) : (
                        <div className="space-y-5">
                            <DetailPanel icon={<ShieldCheck className="size-4 text-stone-500" />} title="简介">
                                <p className="text-sm leading-6 text-stone-600 dark:text-stone-300">{skill.description || "暂无简介"}</p>
                            </DetailPanel>
                            <DetailPanel icon={<Sparkles className="size-4 text-stone-500" />} title="能力说明">
                                <pre className="thin-scrollbar max-h-80 overflow-auto whitespace-pre-wrap rounded-md bg-stone-50 p-3 text-sm leading-6 text-stone-700 dark:bg-stone-900 dark:text-stone-300">
                                    {skill.detail_text || skill.description || "暂无详情"}
                                </pre>
                            </DetailPanel>
                            <DetailPanel icon={<Zap className="size-4 text-stone-500" />} title="画布引用内容">
                                <pre className="thin-scrollbar max-h-96 overflow-auto whitespace-pre-wrap rounded-md bg-stone-50 p-3 text-sm leading-6 text-stone-700 dark:bg-stone-900 dark:text-stone-300">{injectedPrompt}</pre>
                            </DetailPanel>
                            <Collapse
                                ghost
                                size="small"
                                items={[
                                    {
                                        key: "technical",
                                        label: "技术信息",
                                        children: (
                                            <div className="space-y-0 text-sm">
                                                <DetailRow label="目录" value={skill.dir} />
                                                <DetailRow label="图标标识" value={skill.icon || "-"} />
                                                <DetailRow label="版本" value={`V${skill.version || "-"}`} />
                                                <DetailRow label="校验值" value={skill.checksum} />
                                                <DetailRow label="来源" value={skill.source_kind === "original" ? "平台原创" : "授权改编"} />
                                                <DetailRow label="授权" value={skill.source_license || "-"} />
                                                <DetailRow label="发布状态" value={skill.status === "published" ? "已发布" : skill.status} />
                                                <DetailRow label="发布时间" value={formatDate(skill.published_at)} />
                                            </div>
                                        ),
                                    },
                                ]}
                            />
                        </div>
                    )}
                </div>
            ) : null}
        </Drawer>
    );
}

function DetailPanel({ icon, title, children }: { icon: ReactNode; title: string; children: ReactNode }) {
    return (
        <section>
            <div className="mb-2 flex items-center gap-2 text-sm font-semibold text-stone-900 dark:text-stone-100">
                {icon}
                {title}
            </div>
            {children}
        </section>
    );
}

function skillUploaderLabel(uploaderName: string | undefined) {
    const normalizedName = uploaderName?.trim();
    return normalizedName || "HMaigc";
}

function DetailRow({ label, value }: { label: string; value: string }) {
    return (
        <div className="grid grid-cols-[104px_minmax(0,1fr)] gap-2 border-b border-stone-200 py-1.5 last:border-b-0 dark:border-stone-800">
            <span className="text-xs text-stone-500">{label}</span>
            <span className="truncate text-xs font-medium text-stone-800 dark:text-stone-200" title={value}>
                {value}
            </span>
        </div>
    );
}

function mergeSkill(current: PlatformSkill, next: PlatformSkill) {
    return { ...current, ...next };
}

function formatDate(value?: string) {
    if (!value) return "-";
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) return value;
    return date.toLocaleDateString("zh-CN");
}
