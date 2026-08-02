import { AnimatePresence, motion, useReducedMotion } from "motion/react";
import { useEffect, useRef, useState, type ReactNode } from "react";
import { Link } from "react-router";
import { Bot, Check, ChevronDown, Coins, FolderKanban, Gauge, Home, LayoutGrid, LoaderCircle, Pencil, Plus, Redo2, Search, Settings2, Share2, Sparkles, Trash2, Undo2, Upload } from "lucide-react";
import { Button, Dropdown } from "antd";

import { CanvasAgentTooltip } from "@/components/canvas/canvas-agent-tooltip";
import { CanvasShortcutsModal } from "@/components/canvas/canvas-shortcuts-modal";
import { siteLogoURL, useSiteSettings } from "@/components/site/site-settings-provider";
import { useWalletBalance } from "@/hooks/use-wallet-balance";
import { aceternityMotion } from "@/lib/aceternity-motion";
import type { CanvasContextSummary } from "@/lib/canvas/canvas-context-summary";
import { canvasThemes } from "@/lib/canvas-theme";
import { useThemeStore } from "@/stores/use-theme-store";
import { useUserStore } from "@/stores/use-user-store";
import type { CanvasMediaPerformanceMode, CanvasWorkspaceMode } from "@/types/canvas";

type CanvasTopBarProps = {
    canEdit?: boolean;
    canManage?: boolean;
    title: string;
    workspaceMode: CanvasWorkspaceMode;
    onWorkspaceModeChange: (mode: CanvasWorkspaceMode) => void;
    titleDraft: string;
    isTitleEditing: boolean;
    onTitleDraftChange: (value: string) => void;
    onStartTitleEditing: () => void;
    onFinishTitleEditing: () => void;
    onCancelTitleEditing: () => void;
    canUndo: boolean;
    canRedo: boolean;
    onCreateProject: () => void;
    onDeleteProject: () => void;
    onImportImage: () => void;
    onUndo: () => void;
    onRedo: () => void;
    onShare: () => void;
    agentOpen: boolean;
    onToggleAgent: () => void;
    shortcutRequestNonce: number;
    mediaPerformanceMode: CanvasMediaPerformanceMode;
    onMediaPerformanceModeChange: (mode: CanvasMediaPerformanceMode) => void;
    onOpenSearch: () => void;
    projectContext?: CanvasContextSummary & { projectId: string; projectName: string };
    collaborationControl?: ReactNode;
};

export function CanvasTopBar({
    canEdit = true,
    canManage = true,
    title,
    workspaceMode,
    onWorkspaceModeChange,
    titleDraft,
    isTitleEditing,
    onTitleDraftChange,
    onStartTitleEditing,
    onFinishTitleEditing,
    onCancelTitleEditing,
    canUndo,
    canRedo,
    onCreateProject,
    onDeleteProject,
    onImportImage,
    onUndo,
    onRedo,
    onShare,
    agentOpen,
    onToggleAgent,
    shortcutRequestNonce,
    mediaPerformanceMode,
    onMediaPerformanceModeChange,
    onOpenSearch,
    projectContext,
    collaborationControl,
}: CanvasTopBarProps) {
    const theme = canvasThemes[useThemeStore((state) => state.theme)];
    const { settings: siteSettings } = useSiteSettings();
    const user = useUserStore((state) => state.user);
    const { availableMicrocredits, refreshing } = useWalletBalance(user?.id);
    const titleRef = useRef<HTMLDivElement>(null);
    const [shortcutsOpen, setShortcutsOpen] = useState(false);
    const mediaPerformanceLabel = mediaPerformanceMode === "quality" ? "画质优先" : mediaPerformanceMode === "performance" ? "性能优先" : "自动性能";

    useEffect(() => {
        if (shortcutRequestNonce > 0) setShortcutsOpen(true);
    }, [shortcutRequestNonce]);

    useEffect(() => {
        if (!isTitleEditing) return;
        const close = (event: PointerEvent) => {
            if (!titleRef.current?.contains(event.target as Node)) onFinishTitleEditing();
        };
        document.addEventListener("pointerdown", close, true);
        return () => document.removeEventListener("pointerdown", close, true);
    }, [isTitleEditing, onFinishTitleEditing]);

    return (
        <>
            <div className="canvas-top-bar pointer-events-none absolute left-0 right-0 top-0 z-50 flex h-16 items-center justify-between gap-2 px-2 sm:px-4">
                <div className="canvas-top-bar-identity pointer-events-auto flex min-w-0 items-center gap-1 sm:gap-3">
                    <Dropdown
                        trigger={["click"]}
                        overlayClassName="canvas-overlay-dropdown canvas-overlay-dropdown--navigation"
                        menu={{
                            items: [
                                { key: "home", icon: <Home className="size-4" />, label: <Link to="/">主页</Link> },
                                { key: "projects", icon: <LayoutGrid className="size-4" />, label: <Link to="/canvas">画布</Link> },
                                { type: "divider" },
                                { key: "new", icon: <Plus className="size-4" />, label: "新建画布", onClick: onCreateProject },
                                { key: "delete", disabled: !canManage, danger: true, icon: <Trash2 className="size-4" />, label: "删除当前画布", onClick: onDeleteProject },
                                { type: "divider" },
                                { key: "import", disabled: !canEdit, icon: <Upload className="size-4" />, label: "导入素材", onClick: onImportImage },
                                { key: "search", icon: <Search className="size-4" />, label: <MenuLabel text="搜索节点" shortcut="⌘ K" />, onClick: onOpenSearch },
                                {
                                    key: "performance",
                                    icon: <Gauge className="size-4" />,
                                    label: `媒体性能 · ${mediaPerformanceLabel}`,
                                    children: [
                                        { key: "performance-auto", label: "自动性能", onClick: () => onMediaPerformanceModeChange("auto") },
                                        { key: "performance-quality", label: "画质优先", onClick: () => onMediaPerformanceModeChange("quality") },
                                        { key: "performance-fast", label: "性能优先", onClick: () => onMediaPerformanceModeChange("performance") },
                                    ],
                                },
                                { type: "divider" },
                                { key: "undo", disabled: !canEdit || !canUndo, icon: <Undo2 className="size-4" />, label: <MenuLabel text="撤销" shortcut="⌘ Z" />, onClick: onUndo },
                                { key: "redo", disabled: !canEdit || !canRedo, icon: <Redo2 className="size-4" />, label: <MenuLabel text="重做" shortcut="⌘ ⇧ Z / ⌘ Y" />, onClick: onRedo },
                            ],
                        }}
                    >
                        <button type="button" className="canvas-top-bar-menu flex size-9 items-center justify-center gap-0.5 rounded-lg transition hover:bg-black/5 dark:hover:bg-white/10" style={{ color: theme.node.text }} aria-label="打开画布菜单">
                            <img className="canvas-top-bar-brand-image size-5 object-contain" src={siteLogoURL(siteSettings)} alt="" />
                            <ChevronDown className="canvas-top-bar-brand-chevron size-2.5 opacity-55" />
                        </button>
                    </Dropdown>

                    {projectContext ? (
                        <>
                            <Link className="canvas-top-bar-project max-w-[76px] truncate text-[13px] font-medium leading-5" to={`/projects/${projectContext.projectId}/overview`} title={`返回项目：${projectContext.projectName}`}>
                                {projectContext.projectName}
                            </Link>
                            <span className="canvas-top-bar-identity-divider h-5 w-px shrink-0" aria-hidden />
                        </>
                    ) : null}

                    <div ref={titleRef} className="canvas-top-bar-title flex min-w-0 max-w-[112px] flex-col items-start sm:max-w-none">
                        {isTitleEditing ? (
                            <input
                                autoFocus
                                size={canvasTitleInputSize(titleDraft)}
                                value={titleDraft}
                                onChange={(event) => onTitleDraftChange(event.target.value)}
                                onBlur={onFinishTitleEditing}
                                onKeyDown={(event) => {
                                    if (event.key === "Enter") onFinishTitleEditing();
                                    if (event.key === "Escape") onCancelTitleEditing();
                                }}
                                className="h-8 w-auto min-w-12 max-w-[112px] appearance-none border-0 bg-transparent p-0 text-left text-sm font-semibold tracking-normal outline-none ring-0 focus:outline-none focus:ring-0 focus-visible:outline-none focus-visible:ring-0 sm:max-w-[min(280px,42vw)] sm:text-base"
                                style={{ color: theme.node.text, caretColor: theme.accent.primary, border: 0, boxShadow: "none", outline: "none" }}
                                aria-label="画布名称"
                            />
                        ) : (
                            <div className="flex min-w-0 items-center gap-0.5">
                                <button
                                    type="button"
                                    className="canvas-top-bar-title-button max-w-[96px] truncate text-left text-[13px] font-medium leading-5 tracking-normal transition-opacity hover:opacity-75 disabled:cursor-default disabled:hover:opacity-100 sm:max-w-[132px]"
                                    onClick={onStartTitleEditing}
                                    title={canEdit ? "点击修改画布名称" : "当前画布仅可查看"}
                                    disabled={!canEdit}
                                >
                                    {title}
                                </button>
                                {canEdit ? (
                                    <CanvasAgentTooltip title="重命名画布">
                                        <button
                                            type="button"
                                            className="hidden size-7 shrink-0 place-items-center rounded-md opacity-60 transition hover:bg-black/5 hover:opacity-100 focus-visible:outline-none focus-visible:ring-2 dark:hover:bg-white/10 sm:grid"
                                            style={{ color: theme.node.text }}
                                            onClick={onStartTitleEditing}
                                            aria-label="重命名画布"
                                        >
                                            <Pencil className="size-3.5" />
                                        </button>
                                    </CanvasAgentTooltip>
                                ) : null}
                            </div>
                        )}
                        {projectContext && !isTitleEditing ? (
                            <div className="canvas-top-bar-context mt-0.5 hidden max-w-[360px] items-center gap-1.5 text-[10px] sm:flex" style={{ color: theme.node.muted }}>
                                <Link to={`/projects/${projectContext.projectId}/overview`} className="inline-flex min-w-0 items-center gap-1 hover:underline" title={`返回项目：${projectContext.projectName}`}>
                                    <FolderKanban className="size-3 shrink-0" />
                                    <span className="max-w-[120px] truncate">{projectContext.projectName}</span>
                                </Link>
                                <span aria-hidden>·</span>
                                <button type="button" className="min-w-0 truncate hover:underline" onClick={onOpenSearch} title="搜索并定位章节或镜头">
                                    {projectContext.chapterLabel || `${projectContext.nodeCount} 个节点`}
                                    {projectContext.shotLabel ? ` · ${projectContext.shotLabel}` : ""}
                                    {projectContext.selectedCount ? ` · 已选 ${projectContext.selectedCount}` : ""}
                                </button>
                            </div>
                        ) : null}
                    </div>
                </div>

                <CanvasWorkspaceModeSwitch mode={workspaceMode} onChange={onWorkspaceModeChange} />

                <div className="canvas-top-bar-actions pointer-events-auto flex items-center gap-1.5">
                    {collaborationControl}
                    <CanvasAgentTooltip title={canManage ? "分享画布" : "只有画布管理者可以公开分享"}>
                        <Button
                            disabled={!canManage}
                            type="text"
                            className="canvas-top-bar-action !p-0"
                            style={{ color: theme.node.text }}
                            icon={<Share2 className="size-4" />}
                            onClick={onShare}
                            aria-label="分享画布"
                        />
                    </CanvasAgentTooltip>
                    {user ? (
                        <CanvasAgentTooltip title={`查看积分明细：${availableMicrocredits === null ? "--" : (availableMicrocredits / 1_000_000).toLocaleString("zh-CN", { maximumFractionDigits: 3 })}`}>
                            <Link
                                to="/wallet"
                                className="canvas-top-bar-balance items-center justify-center gap-1.5 text-xs font-medium tabular-nums transition hover:bg-black/5 dark:hover:bg-white/10"
                                style={{ color: theme.node.text }}
                            >
                                {refreshing && availableMicrocredits === null ? <LoaderCircle className="size-3.5 animate-spin opacity-60" /> : <Coins className="size-3.5" />}
                                <span>{availableMicrocredits === null ? "--" : formatCanvasBalance(availableMicrocredits / 1_000_000)}</span>
                            </Link>
                        </CanvasAgentTooltip>
                    ) : null}
                    <CanvasAgentTooltip title={agentOpen ? "关闭 Agent" : "打开 Agent"}>
                        <Button
                            disabled={!canEdit}
                            type="text"
                            className="canvas-top-bar-agent !px-3 !font-medium"
                            style={{ color: theme.node.text }}
                            data-active={agentOpen}
                            icon={<Bot className="size-4" />}
                            onClick={onToggleAgent}
                            aria-label="Agent"
                        >
                            <span className="hidden sm:inline">Agent</span>
                        </Button>
                    </CanvasAgentTooltip>
                </div>
            </div>
            <CanvasShortcutsModal open={shortcutsOpen} onClose={() => setShortcutsOpen(false)} />
        </>
    );
}

function formatCanvasBalance(value: number) {
    return new Intl.NumberFormat("zh-CN", {
        notation: "compact",
        maximumFractionDigits: 1,
    }).format(value);
}

function CanvasWorkspaceModeSwitch({ mode, onChange }: { mode: CanvasWorkspaceMode; onChange: (mode: CanvasWorkspaceMode) => void }) {
    const theme = canvasThemes[useThemeStore((state) => state.theme)];
    const reducedMotion = useReducedMotion();
    const simple = mode === "simple";
    const [open, setOpen] = useState(false);
    const rootRef = useRef<HTMLDivElement>(null);

    useEffect(() => {
        if (!open) return;
        const closeOnOutsidePress = (event: PointerEvent) => {
            if (event.target instanceof Node && !rootRef.current?.contains(event.target)) setOpen(false);
        };
        const closeOnEscape = (event: KeyboardEvent) => {
            if (event.key === "Escape") setOpen(false);
        };
        document.addEventListener("pointerdown", closeOnOutsidePress);
        document.addEventListener("keydown", closeOnEscape);
        return () => {
            document.removeEventListener("pointerdown", closeOnOutsidePress);
            document.removeEventListener("keydown", closeOnEscape);
        };
    }, [open]);

    const selectMode = (nextMode: CanvasWorkspaceMode) => {
        if (nextMode !== mode) onChange(nextMode);
        setOpen(false);
    };

    return (
        <div ref={rootRef} className="canvas-workspace-mode-switch aceternity-mode-switch pointer-events-auto relative z-30">
            <motion.button
                type="button"
                whileHover={reducedMotion ? undefined : { y: -1, scale: 1.015 }}
                whileTap={reducedMotion ? undefined : { scale: 0.97 }}
                transition={aceternityMotion.spring.dock}
                className="canvas-workspace-mode-trigger flex h-8 min-w-[112px] items-center gap-1.5 rounded-full border px-2 text-left outline-none backdrop-blur-2xl focus-visible:ring-2"
                style={{ background: theme.spatial.elevated, borderColor: open ? theme.spatial.glowStrong : theme.toolbar.border, color: theme.node.text, boxShadow: `0 16px 44px ${theme.spatial.shadow}` }}
                aria-haspopup="listbox"
                aria-expanded={open}
                aria-label={`当前为${simple ? "简洁" : "专业"}模式，点击切换`}
                onClick={() => setOpen((value) => !value)}
            >
                <span className="canvas-workspace-mode-icon grid size-6 shrink-0 place-items-center rounded-full border" style={{ background: theme.spatial.surface, borderColor: theme.toolbar.border, color: theme.accent.primary }}>
                    {simple ? <Sparkles className="size-3" /> : <Settings2 className="size-3" />}
                </span>
                <span className="min-w-0 flex-1">
                    <span className="canvas-workspace-mode-eyebrow block text-[8px] leading-none" style={{ color: theme.node.muted }}>
                        工作空间
                    </span>
                    <span className="canvas-workspace-mode-label mt-0.5 block text-[10px] font-semibold leading-none">{simple ? "简洁模式" : "专业模式"}</span>
                </span>
                <motion.span
                    animate={{ rotate: open ? 180 : 0 }}
                    transition={reducedMotion ? { duration: 0 } : aceternityMotion.spring.dock}
                    className="canvas-workspace-mode-chevron grid size-5 place-items-center rounded-full"
                    style={{ background: theme.toolbar.itemHover }}
                >
                    <ChevronDown className="size-2.5" />
                </motion.span>
            </motion.button>

            <div className="absolute left-1/2 top-[38px] w-[236px] -translate-x-1/2">
                <AnimatePresence>
                    {open ? (
                        <motion.div
                            role="listbox"
                            aria-label="选择画布工作模式"
                            initial={reducedMotion ? { opacity: 0 } : { opacity: 0, y: -8, scale: 0.92 }}
                            animate={{ opacity: 1, y: 0, scale: 1 }}
                            exit={reducedMotion ? { opacity: 0 } : { opacity: 0, y: -6, scale: 0.95 }}
                            transition={aceternityMotion.spring.panel}
                            className="canvas-overlay-panel canvas-workspace-mode-panel aceternity-floating-panel w-full overflow-hidden border p-1.5 backdrop-blur-2xl"
                            style={{ background: theme.spatial.elevated, borderColor: theme.toolbar.border, color: theme.node.text, boxShadow: `0 28px 80px ${theme.spatial.shadow}` }}
                        >
                            <div className="absolute inset-x-10 top-0 h-px" style={{ background: `linear-gradient(90deg, transparent, ${theme.spatial.glowStrong}, transparent)` }} />
                            <ModeOption active={simple} motionEnabled={!reducedMotion} icon={<Sparkles className="size-4" />} title="简洁模式" description="保留核心创作路径，降低参数密度" theme={theme} onClick={() => selectMode("simple")} />
                            <ModeOption active={!simple} motionEnabled={!reducedMotion} icon={<Settings2 className="size-4" />} title="专业模式" description="显示完整节点、导演台与生成控制" theme={theme} onClick={() => selectMode("professional")} />
                        </motion.div>
                    ) : null}
                </AnimatePresence>
            </div>
        </div>
    );
}

type CanvasTheme = (typeof canvasThemes)[keyof typeof canvasThemes];

function ModeOption({ active, motionEnabled, icon, title, description, theme, onClick }: { active: boolean; motionEnabled: boolean; icon: ReactNode; title: string; description: string; theme: CanvasTheme; onClick: () => void }) {
    return (
        <motion.button
            type="button"
            role="option"
            aria-selected={active}
            whileHover={motionEnabled ? { x: 3 } : undefined}
            whileTap={motionEnabled ? { scale: 0.98 } : undefined}
            transition={aceternityMotion.spring.dock}
            className="canvas-overlay-option group flex min-h-11 w-full items-center gap-2 px-2 py-1.5 text-left outline-none focus-visible:ring-2"
            data-active={active}
            style={{ background: active ? theme.accent.primarySoft : "transparent", borderColor: active ? theme.spatial.glowStrong : "transparent", color: theme.node.text }}
            onClick={onClick}
        >
            <span className="canvas-overlay-option-icon grid size-8 shrink-0 place-items-center [&_svg]:size-3.5" style={{ background: theme.spatial.surface, color: active ? theme.accent.primary : theme.node.muted }}>
                {icon}
            </span>
            <span className="min-w-0 flex-1">
                <span className="canvas-overlay-option-title">{title}</span>
                <span className="canvas-overlay-option-description" style={{ color: theme.node.muted }}>
                    {description}
                </span>
            </span>
            <span
                className="grid size-5 shrink-0 place-items-center rounded-full border transition-opacity"
                style={{ background: active ? theme.accent.primary : theme.spatial.surface, borderColor: active ? theme.accent.primary : theme.toolbar.border, color: active ? "white" : theme.node.muted, opacity: active ? 1 : 0.28 }}
            >
                <Check className="size-3" />
            </span>
        </motion.button>
    );
}

function MenuLabel({ text, shortcut }: { text: string; shortcut: string }) {
    return (
        <span className="flex min-w-36 items-center justify-between gap-8">
            <span>{text}</span>
            <span className="text-xs opacity-45">{shortcut}</span>
        </span>
    );
}

function canvasTitleInputSize(value: string) {
    const visualLength = Array.from(value || "画布名称").reduce((length, character) => length + (character.codePointAt(0)! > 0xff ? 2 : 1), 0);
    return Math.min(30, Math.max(5, visualLength));
}
