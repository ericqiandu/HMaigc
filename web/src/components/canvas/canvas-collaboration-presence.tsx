import { Avatar, Badge, Tooltip } from "antd";
import { Eye, UsersRound, Wifi, WifiOff } from "lucide-react";

import { canvasThemes } from "@/lib/canvas-theme";
import type { CanvasCollaborationConnectionStatus } from "@/pages/canvas/use-canvas-collaboration";
import type { CanvasAccess, CanvasPresence } from "@/services/api/canvas-collaboration";
import { useThemeStore } from "@/stores/use-theme-store";
import type { ViewportTransform } from "@/types/canvas";

type PresenceButtonProps = {
    status: CanvasCollaborationConnectionStatus;
    access: CanvasAccess | null;
    presence: CanvasPresence[];
    onClick: () => void;
};

export function CanvasCollaborationPresenceButton({
    status,
    access,
    presence,
    onClick,
}: PresenceButtonProps) {
    const theme = canvasThemes[useThemeStore((state) => state.theme)];
    if (status === "personal") {
        return (
            <Tooltip title="启用团队多人协作">
                <button
                    type="button"
                    className="canvas-collaboration-entry grid size-10 place-items-center rounded-xl transition hover:bg-black/5 dark:hover:bg-white/10"
                    style={{ color: theme.node.text }}
                    onClick={onClick}
                    aria-label="团队协作"
                >
                    <UsersRound className="size-4" />
                </button>
            </Tooltip>
        );
    }

    const online = status === "online" || status === "readonly";
    const statusText = collaborationStatusLabel(status, access);
    return (
        <Tooltip title={`${statusText}${presence.length ? ` · ${presence.length} 人在线` : ""}`}>
            <button
                type="button"
                className="canvas-collaboration-presence-button flex h-10 items-center gap-2 rounded-xl px-2 transition hover:bg-black/5 dark:hover:bg-white/10"
                style={{ color: theme.node.text }}
                onClick={onClick}
                aria-label="查看团队协作状态"
            >
                <Badge className="canvas-collaboration-status-badge" status={online ? "processing" : status === "error" ? "error" : "default"}>
                    {access?.canEdit ? <Wifi className="size-4" /> : access ? <Eye className="size-4" /> : <WifiOff className="size-4" />}
                </Badge>
                <Avatar.Group className="canvas-collaboration-avatar-group" max={{ count: 3 }}>
                    {presence.slice(0, 4).map((item) => (
                        <Avatar
                            key={item.connectionId}
                            className="canvas-collaboration-avatar !size-6 !text-[10px]"
                            src={item.avatarUrl}
                            style={{ background: collaboratorColor(item.userId) }}
                        >
                            {presenceInitial(item.displayName)}
                        </Avatar>
                    ))}
                </Avatar.Group>
                {!presence.length ? <span className="canvas-collaboration-status-label hidden text-xs xl:inline">{statusText}</span> : null}
            </button>
        </Tooltip>
    );
}

export function CanvasRemotePresenceLayer({
    presence,
    viewport,
}: {
    presence: CanvasPresence[];
    viewport: ViewportTransform;
}) {
    return (
        <div className="canvas-remote-presence-layer pointer-events-none absolute inset-0 z-[48] overflow-hidden" aria-hidden>
            {presence.flatMap((item) => {
                if (!item.cursor) return [];
                const left = viewport.x + item.cursor.x * viewport.k;
                const top = viewport.y + item.cursor.y * viewport.k;
                const color = collaboratorColor(item.userId);
                return (
                    <div
                        key={item.connectionId}
                        className="canvas-remote-cursor absolute transition-transform duration-75 ease-linear"
                        style={{ transform: `translate3d(${left}px, ${top}px, 0)` }}
                    >
                        <svg className="canvas-remote-cursor-icon block h-5 w-5 drop-shadow" viewBox="0 0 24 24" fill="none" aria-hidden>
                            <path d="M4.4 2.8 19 13.1l-7.05 1.15L8.4 20.4 4.4 2.8Z" fill={color} stroke="white" strokeWidth="1.5" strokeLinejoin="round" />
                        </svg>
                        <span
                            className="canvas-remote-cursor-label ml-3 -mt-0.5 inline-flex max-w-32 truncate rounded-md px-2 py-1 text-[10px] font-medium text-white shadow-sm"
                            style={{ background: color }}
                        >
                            {item.displayName || "团队成员"}
                        </span>
                    </div>
                );
            })}
        </div>
    );
}

function collaborationStatusLabel(status: CanvasCollaborationConnectionStatus, access: CanvasAccess | null) {
    if (status === "connecting") return "正在连接";
    if (status === "reconnecting") return "正在重连";
    if (status === "error") return "协作异常";
    if (status === "readonly" || (access && !access.canEdit)) return "仅查看";
    return "实时协作";
}

function presenceInitial(displayName: string) {
    const trimmed = displayName.trim();
    return trimmed ? Array.from(trimmed)[0] : "协";
}

function collaboratorColor(userId: string) {
    let hash = 0;
    for (const character of userId) hash = (hash * 31 + character.charCodeAt(0)) >>> 0;
    const lightness = 42 + (hash % 18);
    return `hsl(217 78% ${lightness}%)`;
}
