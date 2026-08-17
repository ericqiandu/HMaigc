import { ChevronRight } from "lucide-react";
import type { CSSProperties, KeyboardEvent, ReactNode } from "react";

import { canvasThemes } from "@/lib/canvas-theme";
import { cn } from "@/lib/utils";
import { useThemeStore } from "@/stores/use-theme-store";

export type CanvasCreateCommand = {
    id: string;
    label: string;
    icon: ReactNode;
    badge?: string;
    onClick: () => void;
};

export type CanvasCommandItemProps = {
    icon?: ReactNode;
    label: string;
    detail?: string;
    shortcut?: string;
    badge?: string;
    chevron?: boolean;
    active?: boolean;
    disabled?: boolean;
    danger?: boolean;
    onSelect?: () => void;
};

export function CanvasCommandList({ ariaLabel, children, className, onEscape }: { ariaLabel: string; children: ReactNode; className?: string; onEscape?: () => void }) {
    const handleKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
        if (event.key === "Escape") {
            event.preventDefault();
            onEscape?.();
            return;
        }
        if (!(["ArrowDown", "ArrowUp", "Home", "End"] as string[]).includes(event.key)) return;

        const items = [...event.currentTarget.querySelectorAll<HTMLButtonElement>('[data-canvas-command-item="true"]:not(:disabled)')];
        if (!items.length) return;
        event.preventDefault();
        const currentIndex = items.findIndex((item) => item === document.activeElement);
        if (event.key === "Home") {
            items[0]?.focus();
            return;
        }
        if (event.key === "End") {
            items.at(-1)?.focus();
            return;
        }
        const offset = event.key === "ArrowDown" ? 1 : -1;
        const nextIndex = currentIndex < 0 ? (offset > 0 ? 0 : items.length - 1) : (currentIndex + offset + items.length) % items.length;
        items[nextIndex]?.focus();
    };

    return (
        <div className={cn("canvas-command-list", className)} role="menu" aria-label={ariaLabel} onKeyDown={handleKeyDown}>
            {children}
        </div>
    );
}

export function CanvasCommandItem({ icon, label, detail, shortcut, badge, chevron = false, active = false, disabled = false, danger = false, onSelect }: CanvasCommandItemProps) {
    const theme = canvasThemes[useThemeStore((state) => state.theme)];
    const color = danger ? theme.accent.danger : theme.node.text;
    return (
        <button
            type="button"
            role="menuitem"
            aria-label={label}
            aria-current={active ? "true" : undefined}
            data-canvas-command-item="true"
            className={cn("canvas-command-item group flex w-full items-center text-left outline-none transition-colors focus-visible:ring-2", danger && "canvas-command-item--danger")}
            style={{ color, background: active ? theme.toolbar.activeBg : undefined, "--tw-ring-color": theme.node.muted } as CSSProperties}
            disabled={disabled}
            onMouseDown={(event) => event.stopPropagation()}
            onClick={onSelect}
        >
            {icon ? <span className="canvas-command-item-icon grid shrink-0 place-items-center">{icon}</span> : null}
            <span className="canvas-command-item-copy min-w-0 flex-1">
                <span className="canvas-command-item-label block truncate">{label}</span>
                {detail ? <span className="canvas-command-item-detail block truncate">{detail}</span> : null}
            </span>
            {badge ? <span className="canvas-command-badge shrink-0">{badge}</span> : null}
            {shortcut ? <span className="canvas-command-shortcut shrink-0">{shortcut}</span> : null}
            {chevron ? <ChevronRight className="canvas-command-chevron shrink-0" aria-hidden="true" /> : null}
        </button>
    );
}

export function CanvasCommandSectionLabel({ children }: { children: ReactNode }) {
    return <div className="canvas-command-section-label">{children}</div>;
}

export function CanvasCommandDivider() {
    return <div className="canvas-command-divider" role="separator" />;
}

export function CanvasCreateCommandSections({ nodeCommands, resourceCommands }: { nodeCommands: CanvasCreateCommand[]; resourceCommands: CanvasCreateCommand[] }) {
    return (
        <div className="canvas-create-command-sections min-w-0 w-full max-w-full overflow-hidden">
            <CanvasCommandList ariaLabel="添加节点与资源">
                <CanvasCommandSectionLabel>添加节点</CanvasCommandSectionLabel>
                {nodeCommands.map((command) => (
                    <CanvasCommandItem key={command.id} icon={command.icon} label={command.label} badge={command.badge} onSelect={command.onClick} />
                ))}
                <CanvasCommandSectionLabel>添加资源</CanvasCommandSectionLabel>
                {resourceCommands.map((command) => (
                    <CanvasCommandItem key={command.id} icon={command.icon} label={command.label} badge={command.badge} onSelect={command.onClick} />
                ))}
            </CanvasCommandList>
        </div>
    );
}
