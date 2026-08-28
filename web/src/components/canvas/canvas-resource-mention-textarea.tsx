import { forwardRef, useCallback, useImperativeHandle, useLayoutEffect, useMemo, useRef, useState } from "react";
import type { CSSProperties, ClipboardEvent, KeyboardEvent, MouseEvent, PointerEvent, Ref, TextareaHTMLAttributes } from "react";
import { createPortal } from "react-dom";
import { FileText, Image as ImageIcon, Music2, Sparkles, UserRound, Video } from "lucide-react";

import { canvasThemes } from "@/lib/canvas-theme";
import { useThemeStore } from "@/stores/use-theme-store";
import { canvasResourceMentionQueryAt, canvasResourceMentionToken, insertCanvasResourceMention, type CanvasResourceReference } from "@/lib/canvas/canvas-resource-references";
import { parseAudioPauseToken, replaceTextRange, type TextRange } from "@/lib/audio-pause";
import { CanvasNodeType } from "@/types/canvas";

type MentionState = {
    start: number;
    query: string;
};

type EditableTextPart =
    | {
          type: "text";
          text: string;
      }
    | {
          type: "mention";
          token: string;
          reference: CanvasResourceReference;
      }
    | {
          type: "audioPause";
          token: string;
      };

type Props = Omit<TextareaHTMLAttributes<HTMLTextAreaElement>, "onChange" | "value"> & {
    value: string;
    references: CanvasResourceReference[];
    onChange: (value: string) => void;
    onSubmit?: () => void;
    containerClassName?: string;
    highlightLabels?: boolean;
    highlightAudioPauseTokens?: boolean;
    onContentSizeChange?: (height: number) => void;
    editorHandleRef?: Ref<CanvasResourceMentionTextareaHandle>;
};

export type CanvasResourceMentionTextareaHandle = {
    insertReference: (reference: CanvasResourceReference) => TextRange;
    replaceSelection: (text: string) => TextRange;
    replaceRange: (range: TextRange, text: string) => TextRange;
};

export const CanvasResourceMentionTextarea = forwardRef<HTMLTextAreaElement, Props>(function CanvasResourceMentionTextarea(
    { value, references, onChange, onSubmit, onKeyDown, className, containerClassName, style, highlightLabels = true, highlightAudioPauseTokens = false, onContentSizeChange, editorHandleRef, ...props },
    forwardedRef,
) {
    const theme = canvasThemes[useThemeStore((state) => state.theme)];
    const containerRef = useRef<HTMLDivElement | null>(null);
    const textareaRef = useRef<HTMLTextAreaElement | null>(null);
    const editorRef = useRef<HTMLDivElement | null>(null);
    const composingRef = useRef(false);
    const pendingSelectionRef = useRef<number | null>(null);
    const lastSelectionRef = useRef<TextRange | null>(null);
    const lastRenderedValueRef = useRef("");
    const [mention, setMention] = useState<MentionState | null>(null);
    const [activeIndex, setActiveIndex] = useState(0);
    const candidates = useMemo(() => {
        if (!mention) return [];
        const query = mention.query.trim().toLowerCase();
        const activeItems = references.filter((item) => item.active);
        if (!query) return activeItems;
        return activeItems.filter((item) => `${item.label} ${item.title} ${item.kind} ${item.text || ""}`.toLowerCase().includes(query));
    }, [mention, references]);
    const activeReferences = useMemo(() => (highlightLabels ? references.filter((item) => item.active) : []), [highlightLabels, references]);
    const useRichEditor = Boolean(activeReferences.length || highlightAudioPauseTokens);
    const reportContentSize = useCallback((element: HTMLElement | null) => {
        if (!element || !onContentSizeChange) return;
        const previousHeight = element.style.height;
        element.style.height = "0px";
        const height = element.scrollHeight;
        element.style.height = previousHeight;
        onContentSizeChange(height);
    }, [onContentSizeChange]);

    useLayoutEffect(() => {
        if (!useRichEditor) return;
        const editor = editorRef.current;
        if (!editor || composingRef.current) return;
        const isFocused = document.activeElement === editor;
        const currentValue = serializeEditableValue(editor);
        if (currentValue === value && lastRenderedValueRef.current === value) {
            pendingSelectionRef.current = null;
            return;
        }
        const selection = pendingSelectionRef.current ?? (isFocused ? getEditableSelection(editor)?.start ?? null : null);
        renderEditableContent(editor, value, activeReferences, highlightAudioPauseTokens);
        lastRenderedValueRef.current = value;
        if (isFocused && selection !== null) setEditableSelection(editor, selection);
        pendingSelectionRef.current = null;
        reportContentSize(editor);
    }, [activeReferences, highlightAudioPauseTokens, reportContentSize, useRichEditor, value]);

    useLayoutEffect(() => {
        const element = useRichEditor ? editorRef.current : textareaRef.current;
        const container = containerRef.current;
        if (!element || !container || !onContentSizeChange) return;
        reportContentSize(element);
        const observer = new ResizeObserver(() => reportContentSize(element));
        observer.observe(container);
        return () => observer.disconnect();
    }, [onContentSizeChange, reportContentSize, useRichEditor]);

    const focusEditor = (selectionStart?: number) => {
        requestAnimationFrame(() => {
            if (useRichEditor) {
                const editor = editorRef.current;
                if (!editor) return;
                editor.focus();
                if (typeof selectionStart === "number") setEditableSelection(editor, selectionStart);
                return;
            }
            textareaRef.current?.focus();
            if (typeof selectionStart === "number") textareaRef.current?.setSelectionRange(selectionStart, selectionStart);
        });
    };

    const updateValue = (next: string, selectionStart?: number) => {
        if (typeof selectionStart === "number") {
            pendingSelectionRef.current = selectionStart;
            lastSelectionRef.current = { start: selectionStart, end: selectionStart };
        }
        onChange(next);
        if (typeof selectionStart === "number") focusEditor(selectionStart);
    };

    const rememberSelection = (selection: TextRange | null, currentValue = value) => {
        if (selection) lastSelectionRef.current = normalizedSelection(currentValue, selection);
    };

    const currentValueAndSelection = () => {
        if (useRichEditor) {
            const currentValue = editorRef.current ? serializeEditableValue(editorRef.current) : value;
            const liveSelection = getEditableSelection(editorRef.current);
            rememberSelection(liveSelection, currentValue);
            return {
                value: currentValue,
                selection: liveSelection ?? normalizedSelection(currentValue, lastSelectionRef.current ?? { start: currentValue.length, end: currentValue.length }),
            };
        }
        const textarea = textareaRef.current;
        const currentValue = textarea?.value ?? value;
        const liveSelection = textarea ? { start: textarea.selectionStart, end: textarea.selectionEnd } : null;
        rememberSelection(liveSelection, currentValue);
        return {
            value: currentValue,
            selection: liveSelection ?? normalizedSelection(currentValue, lastSelectionRef.current ?? { start: currentValue.length, end: currentValue.length }),
        };
    };

    const replaceRange = (range: TextRange, text: string) => {
        const result = replaceTextRange(currentValueAndSelection().value, range, text);
        updateValue(result.value, result.range.end);
        return result.range;
    };

    useImperativeHandle(
        editorHandleRef,
        () => ({
            insertReference: (reference) => {
                const current = currentValueAndSelection();
                const result = insertCanvasResourceMention(current.value, current.selection, reference);
                updateValue(result.value, result.range.end);
                return result.range;
            },
            replaceSelection: (text) => {
                const current = currentValueAndSelection();
                return replaceRange(current.selection, text);
            },
            replaceRange,
        }),
    );

    const closeMention = () => {
        setMention(null);
        setActiveIndex(0);
    };

    const syncMention = (nextValue: string, cursor: number) => {
        const query = canvasResourceMentionQueryAt(nextValue, cursor);
        if (!query || !references.some((item) => item.active)) {
            closeMention();
            return;
        }
        const nextMention = query;
        const isSameMention = mention?.start === nextMention.start && mention.query === nextMention.query;
        if (!isSameMention) {
            setMention(nextMention);
            setActiveIndex(0);
        }
    };

    const insertReference = (reference: CanvasResourceReference) => {
        if (!mention) return;
        const current = currentValueAndSelection();
        const result = insertCanvasResourceMention(current.value, { start: mention.start, end: current.selection.end }, reference);
        closeMention();
        updateValue(result.value, result.range.end);
    };

    const replaceEditableSelection = (insertText: string) => {
        const current = currentValueAndSelection();
        const next = `${current.value.slice(0, current.selection.start)}${insertText}${current.value.slice(current.selection.end)}`;
        const cursor = current.selection.start + insertText.length;
        updateValue(next, cursor);
        syncMention(next, cursor);
    };

    const syncEditableValue = () => {
        if (composingRef.current) return;
        const editor = editorRef.current;
        if (!editor) return;
        const next = serializeEditableValue(editor);
        const selection = getEditableSelection(editor);
        rememberSelection(selection, next);
        const cursor = selection?.start ?? next.length;
        pendingSelectionRef.current = cursor;
        lastRenderedValueRef.current = next;
        onChange(next);
        syncMention(next, cursor);
        reportContentSize(editor);
    };

    const syncEditableMentionFromSelection = () => {
        const editor = editorRef.current;
        if (!editor) return;
        const currentValue = serializeEditableValue(editor);
        const selection = getEditableSelection(editor);
        rememberSelection(selection, currentValue);
        const cursor = selection?.start;
        if (typeof cursor === "number") syncMention(currentValue, cursor);
    };

    const rememberTextareaSelection = (textarea: HTMLTextAreaElement) => {
        const selection = { start: textarea.selectionStart, end: textarea.selectionEnd };
        rememberSelection(selection, textarea.value);
        syncMention(textarea.value, selection.end);
    };

    const mergedStyle = {
        ...(style || {}),
        caretColor: style?.color || theme.node.text,
    } as CSSProperties;
    const menuAnchor = useRichEditor ? editorRef.current : textareaRef.current;
    const menu = mention && candidates.length && menuAnchor ? <MentionMenu anchor={menuAnchor} references={candidates} activeIndex={Math.min(activeIndex, candidates.length - 1)} theme={theme} onSelect={insertReference} /> : null;

    if (useRichEditor) {
        return (
            <div ref={containerRef} data-canvas-no-zoom className={`relative w-full min-h-0 overflow-hidden ${containerClassName || "h-full"}`}>
                {!value && props.placeholder ? (
                    <div aria-hidden className={`${className || ""} pointer-events-none absolute inset-0 z-0`} style={{ ...style, color: style?.color || theme.node.text, opacity: 0.4 }}>
                        {props.placeholder}
                    </div>
                ) : null}
                <div
                    ref={editorRef}
                    role="textbox"
                    aria-multiline="true"
                    aria-label={props["aria-label"]}
                    aria-disabled={props.disabled}
                    contentEditable={!props.disabled && !props.readOnly}
                    suppressContentEditableWarning
                    spellCheck={props.spellCheck}
                    tabIndex={props.tabIndex}
                    className={`${className || ""} relative z-10 cursor-text select-text whitespace-pre-wrap break-words`}
                    style={{ ...mergedStyle, color: style?.color || theme.node.text }}
                    onInput={syncEditableValue}
                    onCompositionStart={(event) => {
                        composingRef.current = true;
                        props.onCompositionStart?.(event as unknown as React.CompositionEvent<HTMLTextAreaElement>);
                    }}
                    onCompositionEnd={(event) => {
                        composingRef.current = false;
                        syncEditableValue();
                        props.onCompositionEnd?.(event as unknown as React.CompositionEvent<HTMLTextAreaElement>);
                    }}
                    onPaste={(event: ClipboardEvent<HTMLDivElement>) => {
                        event.preventDefault();
                        replaceEditableSelection(event.clipboardData.getData("text/plain"));
                    }}
                    onKeyDown={(event: KeyboardEvent<HTMLDivElement>) => {
                        if (mention && candidates.length) {
                            if (event.key === "ArrowDown") {
                                event.preventDefault();
                                setActiveIndex((index) => (index + 1) % candidates.length);
                                return;
                            }
                            if (event.key === "ArrowUp") {
                                event.preventDefault();
                                setActiveIndex((index) => (index - 1 + candidates.length) % candidates.length);
                                return;
                            }
                            if (event.key === "Enter" || event.key === "Tab") {
                                event.preventDefault();
                                insertReference(candidates[Math.min(activeIndex, candidates.length - 1)]);
                                return;
                            }
                            if (event.key === "Escape") {
                                event.preventDefault();
                                closeMention();
                                return;
                            }
                        }
                        if (event.key === "Enter") {
                            event.preventDefault();
                            if (onSubmit && !event.ctrlKey && !event.metaKey && !event.shiftKey) {
                                onSubmit();
                                return;
                            }
                            replaceEditableSelection("\n");
                            return;
                        }
                        onKeyDown?.(event as unknown as React.KeyboardEvent<HTMLTextAreaElement>);
                    }}
                    onKeyUp={(event) => {
                        syncEditableMentionFromSelection();
                        props.onKeyUp?.(event as unknown as React.KeyboardEvent<HTMLTextAreaElement>);
                    }}
                    onMouseDown={(event) => props.onMouseDown?.(event as unknown as React.MouseEvent<HTMLTextAreaElement>)}
                    onPointerDown={(event) => props.onPointerDown?.(event as unknown as React.PointerEvent<HTMLTextAreaElement>)}
                    onPointerUp={(event) => {
                        syncEditableMentionFromSelection();
                        props.onPointerUp?.(event as unknown as React.PointerEvent<HTMLTextAreaElement>);
                    }}
                    onSelect={(event) => {
                        syncEditableMentionFromSelection();
                        props.onSelect?.(event as unknown as React.SyntheticEvent<HTMLTextAreaElement>);
                    }}
                    onWheel={(event) => {
                        event.stopPropagation();
                        props.onWheel?.(event as unknown as React.WheelEvent<HTMLTextAreaElement>);
                    }}
                    onScroll={(event) => props.onScroll?.(event as unknown as React.UIEvent<HTMLTextAreaElement>)}
                    onFocus={(event) => {
                        syncEditableMentionFromSelection();
                        props.onFocus?.(event as unknown as React.FocusEvent<HTMLTextAreaElement>);
                    }}
                    onBlur={(event) => {
                        syncEditableMentionFromSelection();
                        window.setTimeout(closeMention, 120);
                        props.onBlur?.(event as unknown as React.FocusEvent<HTMLTextAreaElement>);
                    }}
                >
                </div>
                {menu}
            </div>
        );
    }

    return (
        <div ref={containerRef} data-canvas-no-zoom className={`relative w-full min-h-0 overflow-hidden ${containerClassName || "h-full"}`}>
            <textarea
                {...props}
                ref={(node) => {
                    textareaRef.current = node;
                    if (typeof forwardedRef === "function") forwardedRef(node);
                    else if (forwardedRef) forwardedRef.current = node;
                }}
                value={value}
                className={`${className || ""} relative z-10`}
                style={mergedStyle}
                onChange={(event) => {
                    const next = event.target.value;
                    rememberSelection({ start: event.target.selectionStart, end: event.target.selectionEnd }, next);
                    onChange(next);
                    syncMention(next, event.target.selectionStart);
                    reportContentSize(event.currentTarget);
                }}
                onKeyDown={(event) => {
                    if (mention && candidates.length) {
                        if (event.key === "ArrowDown") {
                            event.preventDefault();
                            setActiveIndex((index) => (index + 1) % candidates.length);
                            return;
                        }
                        if (event.key === "ArrowUp") {
                            event.preventDefault();
                            setActiveIndex((index) => (index - 1 + candidates.length) % candidates.length);
                            return;
                        }
                        if (event.key === "Enter") {
                            event.preventDefault();
                            insertReference(candidates[Math.min(activeIndex, candidates.length - 1)]);
                            return;
                        }
                        if (event.key === "Escape") {
                            event.preventDefault();
                            closeMention();
                            return;
                        }
                    }
                    if (event.key === "Enter" && onSubmit && !event.ctrlKey && !event.metaKey && !event.shiftKey) {
                        event.preventDefault();
                        onSubmit();
                        return;
                    }
                    onKeyDown?.(event);
                }}
                onKeyUp={(event) => {
                    rememberTextareaSelection(event.currentTarget);
                    props.onKeyUp?.(event);
                }}
                onPointerUp={(event) => {
                    rememberTextareaSelection(event.currentTarget);
                    props.onPointerUp?.(event);
                }}
                onSelect={(event) => {
                    rememberTextareaSelection(event.currentTarget);
                    props.onSelect?.(event);
                }}
                onFocus={(event) => {
                    rememberTextareaSelection(event.currentTarget);
                    props.onFocus?.(event);
                }}
                onWheel={(event) => {
                    event.stopPropagation();
                    const textarea = event.currentTarget;
                    const deltaY = event.deltaMode === 1 ? event.deltaY * 16 : event.deltaMode === 2 ? event.deltaY * textarea.clientHeight : event.deltaY;
                    if (deltaY) {
                        const previousTop = textarea.scrollTop;
                        textarea.scrollTop += deltaY;
                        if (textarea.scrollTop !== previousTop) event.preventDefault();
                    }
                    props.onWheel?.(event);
                }}
                onBlur={(event) => {
                    rememberTextareaSelection(event.currentTarget);
                    window.setTimeout(closeMention, 120);
                    props.onBlur?.(event);
                }}
            />
            {menu}
        </div>
    );
});

function normalizedSelection(value: string, selection: TextRange): TextRange {
    const start = Math.max(0, Math.min(value.length, selection.start));
    const end = Math.max(start, Math.min(value.length, selection.end));
    return { start, end };
}

function createInlineMentionChip(reference: CanvasResourceReference, token: string) {
    const chip = document.createElement("span");
    chip.contentEditable = "false";
    chip.dataset.mentionToken = token;
    chip.dataset.inlineToken = token;
    chip.className = "mx-[0.06em] inline-flex h-[1.55em] translate-y-[0.18em] select-none items-center gap-[0.18em] rounded-[0.38em] bg-black/[0.06] px-[0.22em] text-[0.92em] font-medium leading-none text-current align-baseline dark:bg-white/[0.1]";

    const at = document.createElement("span");
    at.className = "shrink-0 opacity-90";
    at.textContent = "@";
    chip.appendChild(at);

    chip.appendChild(createInlinePreview(reference));

    const label = document.createElement("span");
    label.className = "shrink-0";
    label.textContent = reference.label;
    chip.appendChild(label);

    return chip;
}

function createInlineAudioPauseChip(token: string) {
    const chip = document.createElement("span");
    chip.contentEditable = "false";
    chip.dataset.audioPauseToken = token;
    chip.dataset.inlineToken = token;
    chip.className = "canvas-audio-pause-token";
    chip.textContent = token;
    return chip;
}

function createInlinePreview(reference: CanvasResourceReference) {
    if ((reference.kind === "image" || reference.kind === "video" || reference.kind === "character") && reference.previewUrl) {
        const media = document.createElement(reference.kind === "video" ? "video" : "img");
        media.className = `size-[1.18em] shrink-0 rounded-[0.24em] ${reference.kind === "video" ? "bg-black object-cover" : reference.kind === "character" ? "bg-black/5 object-contain" : "object-cover"}`;
        media.setAttribute("src", reference.previewUrl);
        media.setAttribute("alt", "");
        if (media instanceof HTMLVideoElement) {
            media.muted = true;
            media.preload = "metadata";
        }
        return media;
    }
    const fallback = document.createElement("span");
    fallback.className = "grid size-[1.18em] shrink-0 place-items-center rounded-[0.24em] bg-current/10";
    fallback.textContent = reference.kind === "audio" ? "♪" : reference.kind === "video" ? "▶" : reference.kind === "image" ? "□" : reference.kind === "skill" ? "✦" : "";
    return fallback;
}

function MentionMenu({ anchor, references, activeIndex, theme, onSelect }: { anchor: HTMLElement; references: CanvasResourceReference[]; activeIndex: number; theme: (typeof canvasThemes)[keyof typeof canvasThemes]; onSelect: (reference: CanvasResourceReference) => void }) {
    const selectedRef = useRef(false);
    const rect = anchor.getBoundingClientRect();
    const boundary = anchor.closest(".ant-modal-content")?.getBoundingClientRect() || { left: 8, top: 8, right: window.innerWidth - 8, bottom: window.innerHeight - 8 };
    const menuWidth = 256;
    const maxMenuHeight = 224;
    const gap = 6;
    const left = clamp(rect.left, boundary.left + 8, boundary.right - menuWidth - 8);
    const showAbove = rect.bottom + gap + maxMenuHeight > boundary.bottom && rect.top - gap - maxMenuHeight >= boundary.top;
    const top = clamp(showAbove ? rect.top - gap - maxMenuHeight : rect.bottom + gap, boundary.top + 8, boundary.bottom - maxMenuHeight - 8);

    const stopCanvasInteraction = (event: PointerEvent | MouseEvent) => {
        event.stopPropagation();
    };
    const selectReference = (reference: CanvasResourceReference) => {
        if (selectedRef.current) return;
        selectedRef.current = true;
        onSelect(reference);
    };

    return createPortal(
        <div
            data-canvas-resource-mention-menu="true"
            className="fixed z-[1300] max-h-56 w-64 overflow-y-auto rounded-xl border p-1 shadow-2xl backdrop-blur-md"
            style={{ left, top, background: theme.toolbar.panel, borderColor: theme.toolbar.border, color: theme.node.text }}
            onPointerDown={stopCanvasInteraction}
            onMouseDown={stopCanvasInteraction}
            onClick={(event) => event.stopPropagation()}
        >
            {references.map((reference, index) => (
                <button
                    key={reference.id}
                    type="button"
                    className="flex w-full min-w-0 items-center gap-2 rounded-lg px-2 py-1.5 text-left text-xs transition"
                    style={{ background: index === activeIndex ? theme.toolbar.activeBg : "transparent", color: index === activeIndex ? theme.toolbar.activeText : theme.node.text }}
                    onPointerDown={(event) => {
                        event.preventDefault();
                        event.stopPropagation();
                        selectReference(reference);
                    }}
                    onClick={(event) => {
                        event.preventDefault();
                        event.stopPropagation();
                        selectReference(reference);
                    }}
                >
                    <ReferencePreview reference={reference} />
                    <span className="min-w-0 flex-1">
                        <span className="block font-medium">{reference.label}</span>
                        {reference.kind !== "skill" ? <span className="block truncate opacity-65">{reference.text || reference.title}</span> : null}
                    </span>
                </button>
            ))}
        </div>,
        document.body,
    );
}

function ReferencePreview({ reference }: { reference: CanvasResourceReference }) {
    if (reference.kind === "image" && reference.previewUrl) return <img src={reference.previewUrl} alt="" className="size-9 rounded-md object-cover" />;
    if (reference.kind === "video" && reference.previewUrl) return <video src={reference.previewUrl} className="size-9 rounded-md bg-black object-cover" muted preload="metadata" />;
    if (reference.kind === "character" && reference.previewUrl) return <img src={reference.previewUrl} alt="" className="size-9 rounded-md bg-black/5 object-contain" />;
    if (reference.kind === "skill") {
        return (
            <span className="grid size-9 shrink-0 place-items-center rounded-md bg-cyan-500/12 text-cyan-600 dark:text-cyan-200">
                <Sparkles className="size-4" />
            </span>
        );
    }
    const Icon = reference.kind === "character" ? UserRound : reference.kind === "audio" ? Music2 : reference.kind === "video" ? Video : reference.kind === "image" ? ImageIcon : FileText;
    return (
        <span className="grid size-9 shrink-0 place-items-center rounded-md bg-black/10">
            <Icon className="size-4" />
        </span>
    );
}

function splitEditableText(value: string, references: CanvasResourceReference[], highlightAudioPauseTokens: boolean) {
    if ((!references.length && !highlightAudioPauseTokens) || !value) return value ? [{ type: "text", text: value } as EditableTextPart] : [];
    const referenceByToken = new Map<string, { reference: CanvasResourceReference; serializedToken: string }>();
    references.forEach((reference) => {
        const serializedToken = canvasResourceMentionToken(reference);
        referenceByToken.set(serializedToken, { reference, serializedToken });
        referenceByToken.set(`@${reference.label}`, { reference, serializedToken });
    });
    const tokens = [...referenceByToken.keys()].sort((a, b) => b.length - a.length);
    const parts: EditableTextPart[] = [];
    let index = 0;
    while (index < value.length) {
        const token = tokens.find((item) => value.startsWith(item, index) && hasMentionBoundary(value, index + item.length));
        if (token) {
            const matched = referenceByToken.get(token);
            if (!matched) throw new Error(`未找到画布引用标记：${token}`);
            parts.push({ type: "mention", token: matched.serializedToken, reference: matched.reference });
            index += token.length;
            continue;
        }
        const audioPauseToken = highlightAudioPauseTokens ? audioPauseTokenAt(value, index) : null;
        if (audioPauseToken) {
            parts.push({ type: "audioPause", token: audioPauseToken });
            index += audioPauseToken.length;
            continue;
        }
        const nextMentionIndex = findNextMentionIndex(value, tokens, index + 1);
        const nextAudioPauseIndex = highlightAudioPauseTokens ? findNextAudioPauseIndex(value, index + 1) : -1;
        const nextTokenIndexes = [nextMentionIndex, nextAudioPauseIndex].filter((candidate) => candidate >= 0);
        const end = nextTokenIndexes.length ? Math.min(...nextTokenIndexes) : value.length;
        if (end <= index) {
            parts.push({ type: "text", text: value[index] });
            index += 1;
        } else {
            parts.push({ type: "text", text: value.slice(index, end) });
            index = end;
        }
    }
    return parts;
}

function renderEditableContent(editor: HTMLElement, value: string, references: CanvasResourceReference[], highlightAudioPauseTokens: boolean) {
    const parts = splitEditableText(value, references, highlightAudioPauseTokens);
    const nodes = parts.map((part) => (part.type === "mention" ? createInlineMentionChip(part.reference, part.token) : part.type === "audioPause" ? createInlineAudioPauseChip(part.token) : document.createTextNode(part.text)));
    editor.replaceChildren(...nodes);
}

function findNextMentionIndex(value: string, tokens: string[], fromIndex: number) {
    let next = -1;
    tokens.forEach((token) => {
        const index = value.indexOf(token, fromIndex);
        if (index >= 0 && hasMentionBoundary(value, index + token.length) && (next < 0 || index < next)) next = index;
    });
    return next;
}

function audioPauseTokenAt(value: string, index: number) {
    if (!value.startsWith("<#", index)) return null;
    const tokenEnd = value.indexOf("#>", index + 2);
    if (tokenEnd < 0) return null;
    const token = value.slice(index, tokenEnd + 2);
    return parseAudioPauseToken(token) === null ? null : token;
}

function findNextAudioPauseIndex(value: string, fromIndex: number) {
    let candidate = value.indexOf("<#", fromIndex);
    while (candidate >= 0) {
        if (audioPauseTokenAt(value, candidate)) return candidate;
        candidate = value.indexOf("<#", candidate + 2);
    }
    return -1;
}

function hasMentionBoundary(value: string, index: number) {
    const char = value[index];
    return !char || /\s|[,.!?;:，。！？；：、)\]}】）]/.test(char);
}

function serializeEditableValue(root: HTMLElement) {
    return serializeNodeList(root.childNodes).replace(/\u00a0/g, " ");
}

function serializeNodeList(nodes: NodeListOf<ChildNode> | ChildNode[]) {
    let text = "";
    nodes.forEach((node) => {
        text += serializeNode(node);
    });
    return text;
}

function serializeNode(node: ChildNode): string {
    if (node.nodeType === Node.TEXT_NODE) return node.textContent || "";
    if (!(node instanceof HTMLElement)) return "";
    const token = node.dataset.inlineToken;
    if (token) return token;
    if (node.tagName === "BR") return "\n";
    return serializeNodeList(node.childNodes);
}

function getEditableSelection(root: HTMLElement | null): TextRange | null {
    if (!root) return null;
    const selection = window.getSelection();
    if (!selection || !selection.rangeCount) return null;
    const range = selection.getRangeAt(0);
    if (!root.contains(range.startContainer) || !root.contains(range.endContainer)) return null;
    const start = offsetForPoint(root, range.startContainer, range.startOffset);
    const end = offsetForPoint(root, range.endContainer, range.endOffset);
    return start <= end ? { start, end } : { start: end, end: start };
}

function offsetForPoint(root: Node, target: Node, targetOffset: number): number {
    if (root === target) {
        if (root.nodeType === Node.TEXT_NODE) return targetOffset;
        return Array.from(root.childNodes)
            .slice(0, targetOffset)
            .reduce((offset, node) => offset + plainTextLength(node), 0);
    }
    let offset = 0;
    for (const child of Array.from(root.childNodes)) {
        if (child === target || child.contains(target)) return offset + offsetForPoint(child, target, targetOffset);
        offset += plainTextLength(child);
    }
    return offset;
}

function setEditableSelection(root: HTMLElement, offset: number) {
    const range = document.createRange();
    const point = pointForOffset(root, Math.max(0, offset));
    range.setStart(point.node, point.offset);
    range.collapse(true);
    const selection = window.getSelection();
    selection?.removeAllRanges();
    selection?.addRange(range);
}

function pointForOffset(root: Node, offset: number): { node: Node; offset: number } {
    if (root.nodeType === Node.TEXT_NODE) return { node: root, offset: Math.min(offset, root.textContent?.length || 0) };
    let remaining = offset;
    const children = Array.from(root.childNodes);
    for (let index = 0; index < children.length; index += 1) {
        const child = children[index];
        const length = plainTextLength(child);
        if (remaining > length) {
            remaining -= length;
            continue;
        }
        if (isInlineTokenElement(child)) return { node: root, offset: remaining <= length / 2 ? index : index + 1 };
        return pointForOffset(child, remaining);
    }
    return { node: root, offset: children.length };
}

function plainTextLength(node: Node): number {
    if (node.nodeType === Node.TEXT_NODE) return node.textContent?.length || 0;
    if (node instanceof HTMLElement) {
        const token = node.dataset.inlineToken;
        if (token) return token.length;
        if (node.tagName === "BR") return 1;
    }
    return Array.from(node.childNodes).reduce((total, child) => total + plainTextLength(child), 0);
}

function isInlineTokenElement(node: Node): node is HTMLElement {
    return node instanceof HTMLElement && Boolean(node.dataset.inlineToken);
}

function clamp(value: number, min: number, max: number) {
    if (max < min) return min;
    return Math.min(Math.max(value, min), max);
}
