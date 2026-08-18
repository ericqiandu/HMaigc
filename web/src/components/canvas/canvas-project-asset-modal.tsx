import { useEffect, useMemo, useState } from "react";
import { Button, Modal } from "antd";
import { Check, FileText, Image as ImageIcon, Music2, UserRound, Video } from "lucide-react";

import type { InsertAssetPayload } from "@/components/canvas/asset-picker-modal";
import { WorkspaceState } from "@/components/layout/workspace-state";
import { compileCharacterReferencePrompt } from "@/lib/canvas/canvas-character-reference";
import { resourceFileUrl } from "@/services/api/resources";
import type { ProjectAsset, ProjectDetail } from "@/services/api/projects";
import { listTeamResources, teamResourceFileURL, type TeamResource, type TeamResourceReference } from "@/services/api/teams";
import { useAssetStore, type Asset } from "@/stores/use-asset-store";

const categoryLabels: Record<string, string> = { all: "全部资产", "team-shared": "团队共享", character: "角色", environment: "场景", wardrobe: "服饰", prop: "道具", weapon: "武器", style: "画风", other: "其他" };
export type ProjectPickerItem = { id: string; category: string; character?: ProjectAsset; media?: Asset; teamResource?: TeamResourceReference };

export type TeamResourcePickerApi = {
    listResources: (teamId: string) => Promise<{ resources: TeamResource[] }>;
    fileURL: (teamId: string, resourceId: string) => string;
};

const defaultTeamResourceApi: TeamResourcePickerApi = {
    listResources: (teamId) => listTeamResources(teamId),
    fileURL: teamResourceFileURL,
};

const supportedTeamResourceKinds = new Set(["image", "video", "audio"]);

export async function loadReadyTeamResourceItems(teamId: string, api: TeamResourcePickerApi = defaultTeamResourceApi): Promise<ProjectPickerItem[]> {
    const { resources } = await api.listResources(teamId);
    return resources.flatMap((resource): ProjectPickerItem[] => {
        if (resource.status !== "ready" || !supportedTeamResourceKinds.has(resource.kind)) return [];
        const readyResource = resource as TeamResourceReference["resource"];
        const kindLabel = readyResource.kind === "image" ? "图片" : readyResource.kind === "video" ? "视频" : "音频";
        return [
            {
                id: `team-resource:${readyResource.id}`,
                category: "team-shared",
                teamResource: {
                    resource: readyResource,
                    fileURL: api.fileURL(teamId, readyResource.id),
                    title: `团队${kindLabel} · ${readyResource.id.slice(0, 8)}`,
                },
            },
        ];
    });
}

export function CanvasProjectAssetModal({
    open,
    detail,
    initialCategory = "all",
    onClose,
    onInsert,
    teamResourceApi = defaultTeamResourceApi,
}: {
    open: boolean;
    detail?: Pick<ProjectDetail, "project" | "assets">;
    initialCategory?: string;
    onClose: () => void;
    onInsert: (payloads: InsertAssetPayload[]) => Promise<void> | void;
    teamResourceApi?: TeamResourcePickerApi;
}) {
    const mediaAssets = useAssetStore((state) => state.assets);
    const [category, setCategory] = useState("all");
    const [selectedIds, setSelectedIds] = useState<Set<string>>(new Set());
    const [inserting, setInserting] = useState(false);
    const [teamItems, setTeamItems] = useState<ProjectPickerItem[]>([]);
    const [teamResourceError, setTeamResourceError] = useState("");
    const projectItems = useMemo(() => {
        const mediaById = new Map(mediaAssets.map((asset) => [asset.id, asset]));
        return (detail?.assets || []).flatMap((asset): ProjectPickerItem[] => {
            if (asset.category === "character" && asset.character) return [{ id: asset.id, category: "character", character: asset }];
            const media = mediaById.get(asset.id);
            return media && media.kind !== "model" && media.kind !== "entity" ? [{ id: asset.id, category: asset.category || media.category || "other", media }] : [];
        });
    }, [detail?.assets, mediaAssets]);
    const items = useMemo(() => [...projectItems, ...teamItems], [projectItems, teamItems]);
    const categories = useMemo(() => ["all", ...Array.from(new Set(items.map((item) => item.category)))], [items]);
    const visible = category === "all" ? items : items.filter((item) => item.category === category);

    useEffect(() => {
        setSelectedIds(new Set());
        setInserting(false);
        setCategory(open ? initialCategory : "all");
    }, [initialCategory, open]);
    useEffect(() => {
        const teamId = detail?.project.teamId;
        if (!open || !teamId) {
            setTeamItems([]);
            setTeamResourceError("");
            return;
        }
        let active = true;
        setTeamItems([]);
        setTeamResourceError("");
        void loadReadyTeamResourceItems(teamId, teamResourceApi)
            .then((nextItems) => {
                if (active) setTeamItems(nextItems);
            })
            .catch((error: unknown) => {
                if (active) setTeamResourceError(error instanceof Error ? error.message : "团队共享素材读取失败");
            });
        return () => {
            active = false;
        };
    }, [detail?.project.teamId, open, teamResourceApi]);
    const toggle = (id: string) =>
        setSelectedIds((current) => {
            const next = new Set(current);
            if (next.has(id)) next.delete(id);
            else next.add(id);
            return next;
        });
    const insert = async () => {
        const payloads = items.filter((item) => selectedIds.has(item.id)).map(toInsertPayload);
        if (!payloads.length) return;
        setInserting(true);
        try {
            await onInsert(payloads);
            onClose();
        } finally {
            setInserting(false);
        }
    };

    return (
        <Modal
            rootClassName="canvas-overlay-modal canvas-overlay-modal--project-assets"
            open={open}
            title={null}
            footer={null}
            destroyOnHidden
            onCancel={onClose}
            width="min(920px, calc(100vw - 24px))"
            styles={{ container: { padding: 0, overflow: "hidden" }, body: { padding: 0 } }}
        >
            <div className="flex h-[min(620px,calc(100vh-80px))] min-h-[440px] flex-col overflow-hidden">
                <header className="flex h-12 shrink-0 items-center justify-between border-b border-border py-0 pl-4 pr-12">
                    <h2 className="text-sm font-semibold">引用项目角色与资产</h2>
                    <span className="text-[11px] text-foreground/42">已选 {selectedIds.size} 项</span>
                </header>
                <div className="grid min-h-0 flex-1 grid-rows-[auto_minmax(0,1fr)] sm:grid-cols-[150px_minmax(0,1fr)] sm:grid-rows-1">
                    <nav className="thin-scrollbar flex min-w-0 gap-1 overflow-x-auto border-b border-border p-2 sm:block sm:overflow-y-auto sm:border-b-0 sm:border-r" aria-label="项目资产分类">
                        {categories.map((item) => (
                            <button
                                key={item}
                                type="button"
                                onClick={() => setCategory(item)}
                                className={`flex h-11 min-w-[104px] shrink-0 items-center justify-between rounded-md px-2 text-xs sm:w-full sm:min-w-0 ${category === item ? "bg-foreground/[.08] font-medium" : "text-foreground/55 hover:bg-foreground/[.04]"}`}
                            >
                                <span>{categoryLabels[item] || "其他"}</span>
                                <span className="min-w-5 rounded bg-foreground/[.05] px-1 text-center text-[10px] tabular-nums">{item === "all" ? items.length : items.filter((asset) => asset.category === item).length}</span>
                            </button>
                        ))}
                    </nav>
                    <div className="thin-scrollbar min-h-0 overflow-y-auto p-3">
                        {teamResourceError ? (
                            <div role="alert" className="mb-3 bg-red-500/10 px-3 py-2 text-xs text-red-600 dark:text-red-300">
                                团队共享素材读取失败：{teamResourceError}
                            </div>
                        ) : null}
                        {visible.length ? (
                            <div className="grid grid-cols-2 gap-2 sm:grid-cols-3 lg:grid-cols-4">
                                {visible.map((item) => (
                                    <ProjectAssetCard key={item.id} item={item} selected={selectedIds.has(item.id)} onToggle={() => toggle(item.id)} />
                                ))}
                            </div>
                        ) : (
                            <WorkspaceState icon="assets" compact className="h-full" title="此分类没有可引用资产" description="先在项目角色与资产中完成角色确认或素材关联。" />
                        )}
                    </div>
                </div>
                <footer className="flex h-12 shrink-0 items-center justify-between border-t border-border px-3">
                    <span className="text-[10px] text-foreground/42">角色引用会在生成时解析当前角色版本</span>
                    <div className="flex gap-2">
                        <Button size="small" onClick={onClose}>
                            取消
                        </Button>
                        <Button size="small" type="primary" disabled={!selectedIds.size} loading={inserting} onClick={() => void insert()}>
                            引入 {selectedIds.size || ""} 项
                        </Button>
                    </div>
                </footer>
            </div>
        </Modal>
    );
}

function ProjectAssetCard({ item, selected, onToggle }: { item: ProjectPickerItem; selected: boolean; onToggle: () => void }) {
    const character = item.character;
    const media = item.media;
    const teamResource = item.teamResource;
    const coverRepresentation =
        character?.character?.representations.find((representation) => representation.role === "turnaround_sheet") ||
        character?.character?.representations.find((representation) => representation.role === "primary") ||
        character?.character?.representations.find((representation) => representation.role === "front");
    const mediaKind = teamResource?.resource.kind || media?.kind;
    const cover = coverRepresentation ? resourceFileUrl(coverRepresentation.resourceId) : teamResource?.resource.kind === "image" ? teamResource.fileURL : media?.coverUrl || (media?.kind === "image" ? media.data.dataUrl : "");
    const Icon = character ? UserRound : mediaKind === "video" ? Video : mediaKind === "audio" ? Music2 : mediaKind === "text" ? FileText : ImageIcon;
    const label = character ? "角色卡" : mediaKind === "video" ? "视频" : mediaKind === "audio" ? "音频" : mediaKind === "text" ? "文本" : "图片";
    const title = character?.title || teamResource?.title || media?.title || "未命名资产";
    const videoURL = teamResource?.resource.kind === "video" ? teamResource.fileURL : media?.kind === "video" ? media.data.url : "";
    return (
        <button
            type="button"
            onClick={onToggle}
            className={`relative min-w-0 overflow-hidden rounded-md border text-left transition-colors ${selected ? "border-[var(--workspace-accent)] bg-[var(--workspace-accent-soft)]" : "border-border/80 hover:border-foreground/30"}`}
        >
            <div className="relative aspect-[4/3] overflow-hidden bg-foreground/[.04]">
                {cover ? (
                    <img src={cover} alt={title} loading="lazy" decoding="async" className={`h-full w-full ${character ? "object-contain p-1" : "object-cover"}`} />
                ) : videoURL ? (
                    <video src={videoURL} muted preload="metadata" className="h-full w-full bg-black object-cover" />
                ) : (
                    <div className="grid h-full place-items-center text-foreground/25">
                        <Icon className="size-7" />
                    </div>
                )}
                <span
                    className={`absolute right-1.5 top-1.5 grid size-5 place-items-center rounded border ${selected ? "border-[var(--workspace-accent)] bg-[var(--workspace-accent)] text-white" : "border-white/60 bg-black/25 text-transparent backdrop-blur"}`}
                >
                    <Check className="size-3" />
                </span>
                <span className="absolute bottom-1.5 left-1.5 rounded bg-black/55 px-1.5 py-0.5 text-[9px] text-white">{label}</span>
            </div>
            <div className="px-2 py-1.5">
                <div className="truncate text-[11px] font-medium">{title}</div>
                {character ? (
                    <div className="mt-0.5 truncate text-[9px] text-foreground/42">
                        {character.character?.visualStatus === "ready" ? "形象就绪" : "形象待完善"} · {character.character?.voiceStatus === "ready" ? "声音已绑定" : "声音未绑定"}
                    </div>
                ) : teamResource ? (
                    <div className="mt-0.5 truncate text-[9px] text-foreground/42">团队共享素材</div>
                ) : null}
            </div>
        </button>
    );
}

function toInsertPayload(item: ProjectPickerItem): InsertAssetPayload {
    if (item.teamResource) return teamResourceToInsertPayload(item);
    if (item.character?.character) {
        return projectCharacterToInsertPayload(item.character);
    }
    const asset = item.media;
    if (!asset) throw new Error("项目资产不可用");
    if (asset.kind === "text") return { kind: "text", content: asset.data.content, title: asset.title, assetId: asset.id };
    if (asset.kind === "video")
        return {
            kind: "video",
            url: asset.data.url,
            storageKey: asset.data.storageKey,
            title: asset.title,
            width: asset.data.width,
            height: asset.data.height,
            durationMs: asset.data.durationMs,
            bytes: asset.data.bytes,
            mimeType: asset.data.mimeType,
            assetId: asset.id,
        };
    if (asset.kind === "audio") return { kind: "audio", url: asset.data.url, storageKey: asset.data.storageKey, title: asset.title, durationMs: asset.data.durationMs, bytes: asset.data.bytes, mimeType: asset.data.mimeType, assetId: asset.id };
    if (asset.kind === "image") return { kind: "image", dataUrl: asset.data.dataUrl, storageKey: asset.data.storageKey, title: asset.title, assetId: asset.id };
    throw new Error("当前项目资产不能直接插入画布");
}

export function teamResourceToInsertPayload(item: ProjectPickerItem): InsertAssetPayload {
    const reference = item.teamResource;
    if (!reference) throw new Error("团队共享素材信息不完整");
    const { resource, fileURL, title } = reference;
    const teamResource = { teamId: resource.teamId, resourceId: resource.id };
    if (resource.kind === "image") return { kind: "image", dataUrl: fileURL, title, width: resource.width, height: resource.height, bytes: resource.size, mimeType: resource.mimeType, teamResource };
    if (resource.kind === "video") return { kind: "video", url: fileURL, title, width: resource.width, height: resource.height, durationMs: resource.durationMs, bytes: resource.size, mimeType: resource.mimeType, teamResource };
    return { kind: "audio", url: fileURL, title, durationMs: resource.durationMs, bytes: resource.size, mimeType: resource.mimeType, teamResource };
}

export function projectCharacterToInsertPayload(asset: ProjectAsset): InsertAssetPayload {
    if (!asset.character) throw new Error("项目角色信息不完整");
    const card = asset.character;
    const definition = card.definition;
    const cover =
        card.representations.find((representation) => representation.role === "turnaround_sheet") ||
        card.representations.find((representation) => representation.role === "primary") ||
        card.representations.find((representation) => representation.role === "front");
    return {
        kind: "character",
        title: asset.title,
        assetId: asset.id,
        versionId: card.versionId,
        prompt: compileCharacterReferencePrompt(asset.title, definition),
        aliases: Array.isArray(definition.aliases) ? definition.aliases.filter((alias): alias is string => typeof alias === "string") : [],
        definition,
        coverUrl: cover ? resourceFileUrl(cover.resourceId) : undefined,
        visualStatus: card.visualStatus,
        voiceStatus: card.voiceStatus,
        voiceName: card.voice?.profile.name,
        voiceProfile: card.voice
            ? {
                  name: card.voice.profile.name,
                  provider: card.voice.profile.provider,
                  language: card.voice.profile.language,
                  timbre: card.voice.profile.timbre,
              }
            : undefined,
        voiceInstructions: card.voice?.instructions,
    };
}
