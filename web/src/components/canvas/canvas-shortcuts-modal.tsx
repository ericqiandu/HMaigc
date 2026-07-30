import { useEffect, useRef } from "react";
import { Hand, Mouse, SlidersHorizontal } from "lucide-react";
import { Modal } from "antd";

import "./canvas-shortcuts-modal.css";

type CanvasShortcutsModalProps = {
    open: boolean;
    onClose: () => void;
};

type GestureIconName = "drag" | "mouse" | "slider";

type ShortcutToken = { kind: "key"; label: string } | { kind: "gesture"; icon: GestureIconName; label: string };

type ShortcutItem = {
    label: string;
    tokens: readonly ShortcutToken[];
};

type ShortcutSection = {
    title: string;
    items: readonly ShortcutItem[];
};

const key = (label: string): ShortcutToken => ({ kind: "key", label });
const gesture = (icon: GestureIconName, label: string): ShortcutToken => ({ kind: "gesture", icon, label });

const shortcutSections: readonly ShortcutSection[] = [
    {
        title: "创作",
        items: [
            { label: "全选节点", tokens: [key("Ctrl/⌘"), key("A")] },
            { label: "复制节点", tokens: [key("Ctrl/⌘"), key("C")] },
            { label: "粘贴节点", tokens: [key("Ctrl/⌘"), key("V")] },
            { label: "保存画布", tokens: [key("Ctrl/⌘"), key("S")] },
            { label: "搜索节点", tokens: [key("Ctrl/⌘"), key("K")] },
            { label: "框选节点", tokens: [key("Shift/Ctrl/⌘"), gesture("drag", "拖动")] },
            { label: "追加选择", tokens: [key("Shift/Ctrl/⌘"), gesture("mouse", "点击")] },
            { label: "移除选择", tokens: [key("Alt"), gesture("drag", "点击/框选")] },
        ],
    },
    {
        title: "缩放",
        items: [
            { label: "原始比例", tokens: [key("Ctrl/⌘"), key("1")] },
            { label: "适应全部", tokens: [key("Ctrl/⌘"), key("2")] },
            { label: "适应选择", tokens: [key("Ctrl/⌘"), key("3")] },
            { label: "缩放画布", tokens: [gesture("mouse", "滚轮")] },
            { label: "精确缩放", tokens: [gesture("slider", "缩放滑杆")] },
        ],
    },
    {
        title: "移动画布",
        items: [
            { label: "空白区域", tokens: [gesture("drag", "左键拖动")] },
            { label: "键盘", tokens: [key("Space"), gesture("drag", "左键拖动")] },
            { label: "鼠标", tokens: [gesture("mouse", "中键拖动")] },
            { label: "触控板", tokens: [gesture("drag", "双指移动")] },
        ],
    },
    {
        title: "其他",
        items: [
            { label: "撤销", tokens: [key("Ctrl/⌘"), key("Z")] },
            { label: "重做", tokens: [key("Ctrl/⌘"), key("⇧ Z")] },
            { label: "再次重做", tokens: [key("Ctrl/⌘"), key("Y")] },
            { label: "删除", tokens: [key("Delete / Backspace")] },
            { label: "取消选择", tokens: [key("Esc")] },
            { label: "快捷键", tokens: [key("?")] },
        ],
    },
];

export function CanvasShortcutsModal({ open, onClose }: CanvasShortcutsModalProps) {
    const contentRef = useRef<HTMLDivElement>(null);

    useEffect(() => {
        if (!open) return;
        const frame = window.requestAnimationFrame(() => contentRef.current?.focus({ preventScroll: true }));
        return () => window.cancelAnimationFrame(frame);
    }, [open]);

    return (
        <Modal
            className="canvas-shortcuts-modal-shell"
            rootClassName="canvas-overlay-modal canvas-shortcuts-modal-root"
            title={null}
            open={open}
            onCancel={onClose}
            footer={null}
            centered
            width={1240}
            destroyOnHidden
            aria-labelledby="canvas-shortcuts-heading"
        >
            <div ref={contentRef} className="canvas-shortcuts-modal-content" tabIndex={-1}>
                <h2 id="canvas-shortcuts-heading" className="canvas-shortcuts-modal-heading">
                    画布快捷键
                </h2>
                <div className="canvas-shortcuts-grid">
                    {shortcutSections.map((section) => (
                        <section key={section.title} className="canvas-shortcuts-section">
                            <h3 className="canvas-shortcuts-section-title">{section.title}</h3>
                            <div className="canvas-shortcuts-section-list">
                                {section.items.map((item) => (
                                    <ShortcutRow key={item.label} item={item} />
                                ))}
                            </div>
                        </section>
                    ))}
                </div>
            </div>
        </Modal>
    );
}

function ShortcutRow({ item }: { item: ShortcutItem }) {
    return (
        <div className="canvas-shortcuts-row">
            <span className="canvas-shortcuts-row-label">{item.label}</span>
            <span className="canvas-shortcuts-row-command">
                {item.tokens.map((token, index) => (
                    <span key={`${token.kind}-${token.label}-${index}`} className="canvas-shortcuts-token-group">
                        {index > 0 ? <span className="canvas-shortcuts-token-plus">+</span> : null}
                        {token.kind === "key" ? (
                            <kbd className="canvas-shortcuts-key">{token.label}</kbd>
                        ) : (
                            <span className="canvas-shortcuts-gesture" aria-label={token.label} title={token.label}>
                                <GestureIcon name={token.icon} />
                                <span className="canvas-shortcuts-gesture-label">{token.label}</span>
                            </span>
                        )}
                    </span>
                ))}
            </span>
        </div>
    );
}

function GestureIcon({ name }: { name: GestureIconName }) {
    if (name === "mouse") return <Mouse className="canvas-shortcuts-gesture-icon" aria-hidden="true" />;
    if (name === "slider") return <SlidersHorizontal className="canvas-shortcuts-gesture-icon" aria-hidden="true" />;
    return <Hand className="canvas-shortcuts-gesture-icon" aria-hidden="true" />;
}
