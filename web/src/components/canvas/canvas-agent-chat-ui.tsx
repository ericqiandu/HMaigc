import { useLayoutEffect, useRef, type ReactNode } from "react";
import { Button } from "antd";
import { Plus, X } from "lucide-react";

import { canvasThemes } from "@/lib/canvas-theme";
import { CanvasAgentTooltip } from "./canvas-agent-tooltip";
import { CanvasSubmitButton } from "./canvas-submit-button";
import "./canvas-agent-panel.css";

export type CanvasAgentChatAttachment = { id: string; name: string; url: string };

function resizeAgentComposerTextarea(element: HTMLTextAreaElement | null) {
    if (!element) return;
    element.style.height = "auto";
    element.style.height = `${element.scrollHeight}px`;
}

export function AgentChatComposer({
    prompt,
    attachments = [],
    disabled,
    sending,
    running,
    placeholder,
    theme,
    onPromptChange,
    onSubmit,
    onStop,
    onAddFiles,
    onRemoveAttachment,
    onDeleteBackwardAtStart,
    selectionSummary,
    left,
    submitReady,
}: {
    prompt: string;
    attachments?: CanvasAgentChatAttachment[];
    disabled?: boolean;
    sending?: boolean;
    running?: boolean;
    placeholder: string;
    theme: (typeof canvasThemes)[keyof typeof canvasThemes];
    onPromptChange: (value: string) => void;
    onSubmit: () => void;
    onStop?: () => void;
    onAddFiles?: (files: FileList | File[] | null) => void | Promise<void>;
    onRemoveAttachment?: (id: string) => void;
    onDeleteBackwardAtStart?: () => boolean;
    selectionSummary?: ReactNode;
    left?: ReactNode;
    submitReady?: boolean;
}) {
    const fileInputRef = useRef<HTMLInputElement>(null);
    const textareaRef = useRef<HTMLTextAreaElement>(null);
    const canSubmit = !disabled && !sending && (submitReady ?? Boolean(prompt.trim() || attachments.length));
    const canStop = Boolean(running && !sending && onStop);

    useLayoutEffect(() => {
        resizeAgentComposerTextarea(textareaRef.current);
    }, [prompt]);

    return (
        <div className="canvas-agent-composer-wrap" onWheelCapture={(event) => event.stopPropagation()}>
            <div className="canvas-agent-composer border" style={{ background: theme.node.fill, borderColor: theme.node.stroke }}>
                {attachments.length ? (
                    <div className="canvas-agent-composer-attachments thin-scrollbar mb-2 flex gap-2 overflow-x-auto pb-1">
                        {attachments.map((item) => (
                            <div key={item.id} className="canvas-agent-composer-attachment group relative size-14 shrink-0 overflow-hidden rounded-md" title={item.name}>
                                <img src={item.url} alt={item.name} className="canvas-agent-composer-attachment-image size-full object-cover" />
                                {onRemoveAttachment ? (
                                    <button
                                        type="button"
                                        className="canvas-agent-composer-attachment-remove absolute right-1 top-1 grid size-5 place-items-center rounded-md opacity-0 transition group-hover:opacity-100"
                                        style={{ background: theme.toolbar.panel, color: theme.node.text }}
                                        onClick={() => onRemoveAttachment(item.id)}
                                        aria-label="移除图片"
                                    >
                                        <X className="canvas-agent-composer-attachment-remove-icon size-3" />
                                    </button>
                                ) : null}
                            </div>
                        ))}
                    </div>
                ) : null}
                <div className="canvas-agent-composer-input-flow">
                    {selectionSummary}
                    <textarea
                        ref={textareaRef}
                        value={prompt}
                        aria-label="输入 Agent 指令"
                        disabled={disabled || sending || running}
                        onInput={(event) => {
                            resizeAgentComposerTextarea(event.currentTarget);
                            onPromptChange(event.currentTarget.value);
                        }}
                        onPaste={(event) => {
                            if (!onAddFiles) return;
                            const images = Array.from(event.clipboardData.files).filter((file) => file.type.startsWith("image/"));
                            if (!images.length) return;
                            event.preventDefault();
                            void onAddFiles(images);
                        }}
                        onKeyDown={(event) => {
                            if (event.key === "Backspace" && !prompt.length && onDeleteBackwardAtStart?.()) {
                                event.preventDefault();
                                return;
                            }
                            if (event.key !== "Enter" || event.shiftKey || event.ctrlKey || event.metaKey) return;
                            event.preventDefault();
                            void onSubmit();
                        }}
                        className="canvas-agent-composer-textarea thin-scrollbar resize-none border-0 bg-transparent outline-none placeholder:opacity-45"
                        style={{ color: theme.node.text }}
                        placeholder={placeholder}
                    />
                </div>
                <div className="canvas-agent-composer-footer">
                    <div className="canvas-agent-composer-left flex min-w-0 items-center gap-1">
                        {onAddFiles ? (
                            <>
                                <input
                                    ref={fileInputRef}
                                    className="canvas-agent-composer-file-input"
                                    hidden
                                    type="file"
                                    accept="image/*"
                                    multiple
                                    onChange={(event) => {
                                        void onAddFiles(event.target.files);
                                        event.target.value = "";
                                    }}
                                />
                                <CanvasAgentTooltip title="添加图片">
                                    <Button
                                        type="text"
                                        className="canvas-agent-composer-tool"
                                        disabled={disabled || sending || running}
                                        style={{ color: theme.node.muted }}
                                        icon={<Plus className="canvas-agent-composer-glyph" strokeWidth={1.8} />}
                                        onClick={() => fileInputRef.current?.click()}
                                        aria-label="添加图片"
                                    />
                                </CanvasAgentTooltip>
                            </>
                        ) : null}
                        {left}
                    </div>
                    <CanvasSubmitButton
                        state={running ? (sending ? "loading" : "stop") : sending ? "loading" : "ready"}
                        disabled={running ? !canStop : !canSubmit}
                        onClick={() => void (running ? onStop?.() : onSubmit())}
                        ariaLabel={running ? (sending ? "正在停止 Agent" : "停止 Agent") : sending ? "正在发送" : "发送"}
                    />
                </div>
            </div>
        </div>
    );
}
