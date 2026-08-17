import { AnimatePresence, useReducedMotion } from "motion/react";
import { useEffect, useState } from "react";
import {
    ArrowLeft,
    Check,
    Clapperboard,
    Clipboard,
    Copy,
    FolderOpen,
    FolderPlus,
    Image as ImageIcon,
    Layers3,
    Link2,
    Maximize2,
    Music2,
    PanelTop,
    Plus,
    Redo2,
    Scissors,
    Settings2,
    Tags,
    Trash2,
    Type,
    Undo2,
    Upload,
    UserRound,
    Video,
} from "lucide-react";

import { aceternityMotion } from "@/lib/aceternity-motion";
import { SpotlightSurface } from "@/components/ui/aceternity/spotlight-surface";
import {
    CanvasCommandDivider,
    CanvasCommandItem,
    CanvasCommandList,
    CanvasCommandSectionLabel,
    CanvasCreateCommandSections,
    type CanvasCreateCommand,
} from "@/components/canvas/canvas-create-command-grid";
import { canvasThemes } from "@/lib/canvas-theme";
import { canvasNodeAssetCategory } from "@/lib/canvas/canvas-node-asset";
import { useThemeStore } from "@/stores/use-theme-store";
import { CanvasNodeType, type CanvasNodeData, type CanvasWorkspaceMode, type ContextMenuState, type Position } from "@/types/canvas";
import "./canvas-context-menu.css";

type CanvasAssetCategory = NonNullable<NonNullable<CanvasNodeData["metadata"]>["assetCategory"]>;

const assetCategoryOptions: Array<{ value: CanvasAssetCategory; label: string }> = [
    { value: "character", label: "角色" },
    { value: "environment", label: "场景" },
    { value: "wardrobe", label: "服饰" },
    { value: "prop", label: "道具" },
    { value: "weapon", label: "武器" },
    { value: "style", label: "画风" },
    { value: "other", label: "其他" },
];

type CanvasNodeContextMenuProps = {
    menu: ContextMenuState;
    node?: CanvasNodeData | null;
    workspaceMode?: CanvasWorkspaceMode;
    isProjectLinked?: boolean;
    canUndo: boolean;
    canRedo: boolean;
    canPaste: boolean;
    onClose: () => void;
    onAddNode: (type: CanvasNodeType) => void;
    onAddVideoComposition: () => void;
    onOpenDirector: (position: Position) => void;
    onUpload: () => void;
    onOpenAssets: () => void;
    onOpenProjectCharacters: () => void;
    onUndo: () => void;
    onRedo: () => void;
    onPaste: () => void;
    onCopyNode: () => void;
    onDuplicate: () => void;
    onDelete: () => void;
    onSaveAsset: () => void;
    onViewMedia: () => void;
    onEditText: () => void;
    onGenerateImage: () => void;
    onCopyContent: () => void;
    onCopyMediaUrl: () => void;
    onSetAssetCategory: (category: CanvasAssetCategory) => void;
    onToggleFrame: () => void;
};

export function CanvasNodeContextMenu({
    menu,
    node,
    workspaceMode = "professional",
    isProjectLinked = false,
    canUndo,
    canRedo,
    canPaste,
    onClose,
    onAddNode,
    onAddVideoComposition,
    onOpenDirector,
    onUpload,
    onOpenAssets,
    onOpenProjectCharacters,
    onUndo,
    onRedo,
    onPaste,
    onCopyNode,
    onDuplicate,
    onDelete,
    onSaveAsset,
    onViewMedia,
    onEditText,
    onGenerateImage,
    onCopyContent,
    onCopyMediaUrl,
    onSetAssetCategory,
    onToggleFrame,
}: CanvasNodeContextMenuProps) {
    const themeMode = useThemeStore((state) => state.theme);
    const theme = canvasThemes[themeMode];
    const reducedMotion = useReducedMotion();
    const [addOpen, setAddOpen] = useState(false);
    const [categoryOpen, setCategoryOpen] = useState(false);

    useEffect(() => {
        const close = (event: PointerEvent) => {
            const target = event.target;
            if (target instanceof Element && target.closest(".ant-popover")) return;
            onClose();
        };
        const closeOnEscape = (event: KeyboardEvent) => {
            if (event.key !== "Escape") return;
            if (categoryOpen) setCategoryOpen(false);
            else onClose();
        };
        window.addEventListener("pointerdown", close);
        window.addEventListener("keydown", closeOnEscape);
        return () => {
            window.removeEventListener("pointerdown", close);
            window.removeEventListener("keydown", closeOnEscape);
        };
    }, [categoryOpen, onClose]);

    useEffect(() => {
        setAddOpen(false);
        setCategoryOpen(false);
    }, [menu.type, menu.x, menu.y]);

    const runAction = (action: () => void) => {
        action();
        onClose();
    };
    const nodeContent = typeof node?.metadata?.content === "string" ? node.metadata.content : "";
    const isImage = node?.type === CanvasNodeType.Image;
    const isText = node?.type === CanvasNodeType.Text;
    const isCharacterReference = Boolean(isText && node?.metadata?.workflowKind === "character" && node.metadata.characterAssetId);
    const isVideo = node?.type === CanvasNodeType.Video;
    const isMedia = isImage || isVideo;
    const isAudio = node?.type === CanvasNodeType.Audio;
    const isFrame = node?.type === CanvasNodeType.Frame;
    const hasNodeContent = isText ? Boolean(nodeContent.trim()) : Boolean(nodeContent);
    const canSaveAsset = Boolean(node && !isCharacterReference && (isText ? hasNodeContent : hasNodeContent && (isImage || isVideo || isAudio)));
    const canOpenPreview = Boolean(isMedia && hasNodeContent);
    const canGenerateFromText = Boolean(isText && !isCharacterReference && hasNodeContent);
    const canCopyMediaUrl = Boolean(isMedia && hasNodeContent);
    const assetCategory = node ? canvasNodeAssetCategory(node) : "other";
    const position = getContextMenuPosition(menu);

    return (
        <>
            {menu.type === "canvas" && addOpen ? null : (
                <SpotlightSurface
                    spotlightColor={theme.toolbar.itemHover}
                    initial={reducedMotion ? { opacity: 0 } : { opacity: 0, scale: 0.97, x: -3, y: -3 }}
                    animate={{ opacity: 1, scale: 1, x: 0, y: 0 }}
                    transition={{ duration: aceternityMotion.duration.instant, ease: aceternityMotion.easing.enter }}
                    className={`canvas-overlay-panel canvas-command-menu canvas-context-menu ${menu.type === "canvas" ? "canvas-context-menu--canvas" : "canvas-context-menu--entity"} aceternity-floating-panel fixed z-[180] flex max-h-[calc(100vh-56px)] origin-top-left flex-col overflow-hidden border backdrop-blur-2xl`}
                    style={{
                        left: position.left,
                        top: position.top,
                        color: theme.node.text,
                    }}
                    onContextMenu={(event) => event.preventDefault()}
                    onPointerDown={(event) => event.stopPropagation()}
                >
                    {menu.type === "canvas" ? null : <div className="canvas-context-menu-highlight absolute inset-x-8 top-0 h-px" style={{ background: `linear-gradient(90deg, transparent, ${theme.toolbar.border}, transparent)` }} />}
                    <CanvasCommandList ariaLabel={menu.type === "canvas" ? "画布命令" : menu.type === "node" ? "节点命令" : "连接命令"} className="canvas-context-menu-scroll min-h-0 overflow-x-hidden overflow-y-auto">
                        {menu.type === "node" && isMedia && categoryOpen ? (
                            <>
                                <MenuHeader title="设置资产分类" description={node?.title || nodeTypeLabel(node)} onBack={() => setCategoryOpen(false)} />
                                <CanvasCommandSectionLabel>项目用途</CanvasCommandSectionLabel>
                                {assetCategoryOptions.map((option) => (
                                    <CanvasCommandItem
                                        key={option.value}
                                        icon={assetCategory === option.value ? <Check /> : <Tags />}
                                        label={option.label}
                                        active={assetCategory === option.value}
                                        onSelect={() => runAction(() => onSetAssetCategory(option.value))}
                                    />
                                ))}
                            </>
                        ) : menu.type === "canvas" ? (
                            <>
                                <CanvasCommandItem label="添加节点" chevron active={addOpen} onSelect={() => setAddOpen((value) => !value)} />
                                <CanvasCommandItem label="上传到这里" onSelect={() => runAction(onUpload)} />
                                {!isProjectLinked ? <CanvasCommandItem label="从素材库插入" onSelect={() => runAction(onOpenAssets)} /> : null}
                                <CanvasCommandDivider />
                                <CanvasCommandItem label="撤销" shortcut="⌘Z" disabled={!canUndo} onSelect={() => runAction(onUndo)} />
                                <CanvasCommandItem label="重做" shortcut="⇧⌘Z" disabled={!canRedo} onSelect={() => runAction(onRedo)} />
                                <CanvasCommandItem label="粘贴" shortcut="⌘V" disabled={!canPaste} onSelect={() => runAction(onPaste)} />
                            </>
                        ) : menu.type === "node" ? (
                            <>
                                {isCharacterReference ? (
                                    <>
                                        <MenuHeader title="角色卡" description={node?.metadata?.characterName || node?.title} />
                                        <CanvasCommandSectionLabel>角色引用</CanvasCommandSectionLabel>
                                        <CanvasCommandItem icon={<UserRound />} label="查看角色详情" onSelect={() => runAction(onEditText)} />
                                        <CanvasCommandDivider />
                                        <CanvasCommandSectionLabel>节点</CanvasCommandSectionLabel>
                                        <CanvasCommandItem icon={<Copy />} label="复制角色引用" shortcut="⌘C" onSelect={() => runAction(onCopyNode)} />
                                        <CanvasCommandItem icon={<Layers3 />} label="创建引用副本" shortcut="⌘D" onSelect={() => runAction(onDuplicate)} />
                                        <CanvasCommandItem icon={<Trash2 />} label="删除节点" danger onSelect={() => runAction(onDelete)} />
                                    </>
                                ) : isMedia ? (
                                    <>
                                        <MenuHeader title={isImage ? "图片" : "视频"} description={node?.title || nodeTypeLabel(node)} />
                                        <CanvasCommandSectionLabel>查看与归档</CanvasCommandSectionLabel>
                                        <CanvasCommandItem icon={<Maximize2 />} label="进入全景预览" disabled={!canOpenPreview} onSelect={() => runAction(onViewMedia)} />
                                        <CanvasCommandItem icon={<Tags />} label="设置资产分类" chevron onSelect={() => setCategoryOpen(true)} />
                                        <CanvasCommandDivider />
                                        <CanvasCommandSectionLabel>节点</CanvasCommandSectionLabel>
                                        <CanvasCommandItem icon={<Copy />} label="复制节点" shortcut="⌘C" onSelect={() => runAction(onCopyNode)} />
                                        <CanvasCommandItem icon={<Link2 />} label={isImage ? "复制图片地址" : "复制视频地址"} disabled={!canCopyMediaUrl} onSelect={() => runAction(onCopyMediaUrl)} />
                                        <CanvasCommandItem icon={<Layers3 />} label="创建参数变体" shortcut="⌘D" onSelect={() => runAction(onDuplicate)} />
                                        <CanvasCommandItem icon={<Trash2 />} label="删除节点" danger onSelect={() => runAction(onDelete)} />
                                    </>
                                ) : (
                                    <>
                                        <MenuHeader title={node?.title || nodeTypeLabel(node)} />
                                        <CanvasCommandSectionLabel>节点操作</CanvasCommandSectionLabel>
                                        {isFrame ? (
                                            <CanvasCommandItem icon={<PanelTop />} label={node?.metadata?.frame?.collapsed ? "展开背板" : "折叠背板"} onSelect={() => runAction(onToggleFrame)} />
                                        ) : (
                                            <CanvasCommandItem icon={<FolderPlus />} label="保存到我的素材" disabled={!canSaveAsset} onSelect={() => runAction(onSaveAsset)} />
                                        )}
                                        {isText ? <CanvasCommandItem icon={<Maximize2 />} label="放大编辑" onSelect={() => runAction(onEditText)} /> : null}
                                        {isText ? <CanvasCommandItem icon={<ImageIcon />} label="用文本生图" disabled={!canGenerateFromText} onSelect={() => runAction(onGenerateImage)} /> : null}
                                        <CanvasCommandDivider />
                                        <CanvasCommandSectionLabel>副本与内容</CanvasCommandSectionLabel>
                                        <CanvasCommandItem icon={<Copy />} label={isFrame ? "复制背板及内容" : "复制节点"} shortcut="⌘C" onSelect={() => runAction(onCopyNode)} />
                                        {isText ? <CanvasCommandItem icon={<Clipboard />} label="复制文本" disabled={!hasNodeContent} onSelect={() => runAction(onCopyContent)} /> : null}
                                        <CanvasCommandItem icon={<Copy />} label={isFrame ? "创建背板副本" : "创建参数变体"} shortcut="⌘D" onSelect={() => runAction(onDuplicate)} />
                                        <CanvasCommandItem icon={<Clipboard />} label="粘贴" shortcut="⌘V" disabled={!canPaste} onSelect={() => runAction(onPaste)} />
                                        <CanvasCommandItem icon={<Trash2 />} label={isFrame ? "删除背板" : "删除节点"} danger onSelect={() => runAction(onDelete)} />
                                    </>
                                )}
                            </>
                        ) : (
                            <>
                                <MenuHeader title="连接" />
                                <CanvasCommandItem icon={<Trash2 className="size-4" />} label="删除连接" danger onSelect={() => runAction(onDelete)} />
                            </>
                        )}
                    </CanvasCommandList>
                </SpotlightSurface>
            )}

            <AnimatePresence>
                {menu.type === "canvas" && addOpen ? (
                    <AddNodeContextMenu
                        parentPosition={position}
                        workspaceMode={workspaceMode}
                        isProjectLinked={isProjectLinked}
                        reducedMotion={Boolean(reducedMotion)}
                        onAddNode={(type) => runAction(() => onAddNode(type))}
                        onAddVideoComposition={() => runAction(onAddVideoComposition)}
                        onOpenDirector={() => runAction(() => onOpenDirector(menu.position))}
                        onUpload={() => runAction(onUpload)}
                        onOpenAssets={() => runAction(onOpenAssets)}
                        onOpenProjectCharacters={() => runAction(onOpenProjectCharacters)}
                    />
                ) : null}
            </AnimatePresence>
        </>
    );
}

function AddNodeContextMenu({
    parentPosition,
    workspaceMode,
    isProjectLinked,
    reducedMotion,
    onAddNode,
    onAddVideoComposition,
    onOpenDirector,
    onUpload,
    onOpenAssets,
    onOpenProjectCharacters,
}: {
    parentPosition: { left: number; top: number };
    workspaceMode: CanvasWorkspaceMode;
    isProjectLinked: boolean;
    reducedMotion: boolean;
    onAddNode: (type: CanvasNodeType) => void;
    onAddVideoComposition: () => void;
    onOpenDirector: () => void;
    onUpload: () => void;
    onOpenAssets: () => void;
    onOpenProjectCharacters: () => void;
}) {
    const theme = canvasThemes[useThemeStore((state) => state.theme)];
    const position = getAddNodeContextMenuPosition(parentPosition);
    const simpleMode = workspaceMode === "simple";
    const nodeCommands: CanvasCreateCommand[] = [
        { id: "text", label: "文本", icon: <Type />, onClick: () => onAddNode(CanvasNodeType.Text) },
        { id: "script", label: "分镜脚本", icon: <Clapperboard />, badge: "核心", onClick: () => onAddNode(CanvasNodeType.Script) },
        ...(!simpleMode ? [{ id: "frame", label: "背板", icon: <PanelTop />, onClick: () => onAddNode(CanvasNodeType.Frame) }] : []),
        { id: "image", label: "图片", icon: <ImageIcon />, onClick: () => onAddNode(CanvasNodeType.Image) },
        { id: "video", label: "视频", icon: <Video />, onClick: () => onAddNode(CanvasNodeType.Video) },
        { id: "video-composition", label: "视频合成", icon: <Scissors />, badge: "Beta", onClick: onAddVideoComposition },
        ...(!simpleMode
            ? [
                  { id: "director", label: "导演台", icon: <Layers3 />, badge: "3D", onClick: onOpenDirector },
                  { id: "audio", label: "音频", icon: <Music2 />, onClick: () => onAddNode(CanvasNodeType.Audio) },
                  { id: "config", label: "生成配置", icon: <Settings2 />, onClick: () => onAddNode(CanvasNodeType.Config) },
              ]
            : []),
    ];
    const resourceCommands: CanvasCreateCommand[] = [
        { id: "upload", label: "上传文件", icon: <Upload />, onClick: onUpload },
        ...(isProjectLinked ? [{ id: "project-character", label: "添加角色卡", icon: <UserRound />, onClick: onOpenProjectCharacters }] : []),
        ...(!isProjectLinked ? [{ id: "assets", label: "素材库", icon: <FolderOpen />, onClick: onOpenAssets }] : []),
    ];

    return (
        <SpotlightSurface
            spotlightColor={theme.toolbar.itemHover}
            initial={reducedMotion ? { opacity: 0 } : { opacity: 0, scale: 0.97 }}
            animate={{ opacity: 1, x: 0, scale: 1 }}
            exit={reducedMotion ? { opacity: 0 } : { opacity: 0, scale: 0.98 }}
            transition={{ duration: aceternityMotion.duration.instant, ease: aceternityMotion.easing.enter }}
            className="canvas-overlay-panel canvas-command-menu canvas-context-submenu canvas-context-submenu--add hide-scrollbar fixed z-[181] max-h-[calc(100vh-56px)] origin-top overflow-x-hidden overflow-y-auto backdrop-blur-2xl"
            style={{
                left: position.left,
                top: position.top,
                color: theme.node.text,
            }}
            onContextMenu={(event) => event.preventDefault()}
            onPointerDown={(event) => event.stopPropagation()}
        >
            <div className="canvas-context-submenu-content">
                <CanvasCreateCommandSections nodeCommands={nodeCommands} resourceCommands={resourceCommands} />
            </div>
        </SpotlightSurface>
    );
}

function MenuHeader({ title, description, onBack }: { title: string; description?: string; onBack?: () => void }) {
    const theme = canvasThemes[useThemeStore((state) => state.theme)];
    return (
        <div className="mb-0.5 flex items-start gap-1 px-1.5 py-1.5">
            {onBack ? (
                <button type="button" onClick={onBack} className="mt-0.5 grid size-6 shrink-0 place-items-center rounded-md outline-none hover:bg-black/5 focus-visible:ring-2 dark:hover:bg-white/8" aria-label="返回媒体操作">
                    <ArrowLeft className="size-3.5" />
                </button>
            ) : null}
            <span className="min-w-0">
                <span className="block truncate text-xs font-semibold">{title}</span>
                {description && description !== title ? (
                    <span className="mt-0.5 block truncate text-[11px] leading-4" style={{ color: theme.node.muted }}>
                        {description}
                    </span>
                ) : null}
            </span>
        </div>
    );
}

function getContextMenuPosition(menu: ContextMenuState) {
    if (typeof window === "undefined") return { left: menu.x, top: menu.y };
    const width = menu.type === "canvas" ? 196 : 224;
    const estimatedHeight = menu.type === "node" ? Math.min(360, window.innerHeight - 72) : menu.type === "canvas" ? 250 : 84;
    return {
        left: clamp(menu.x, 12, Math.max(12, window.innerWidth - width - 12)),
        top: clamp(menu.y, 68, Math.max(68, window.innerHeight - estimatedHeight - 12)),
    };
}

function getAddNodeContextMenuPosition(parentPosition: { left: number; top: number }) {
    if (typeof window === "undefined") return parentPosition;
    const estimatedHeight = 482;
    return {
        left: parentPosition.left,
        top: clamp(parentPosition.top, 68, Math.max(68, window.innerHeight - estimatedHeight - 12)),
    };
}

function clamp(value: number, min: number, max: number) {
    return Math.min(Math.max(value, min), max);
}

function nodeTypeLabel(node?: CanvasNodeData | null) {
    if (!node) return "节点";
    if (node.type === CanvasNodeType.Image) return "图片节点";
    if (node.type === CanvasNodeType.Text) return "文本节点";
    if (node.type === CanvasNodeType.Script) return "分镜脚本节点";
    if (node.type === CanvasNodeType.Skill) return "技能节点";
    if (node.type === CanvasNodeType.Video) return "视频节点";
    if (node.type === CanvasNodeType.Audio) return "音频节点";
    if (node.type === CanvasNodeType.Frame) return "背板";
    return "生成配置节点";
}
