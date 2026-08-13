import { forwardRef, useCallback, useEffect, useImperativeHandle, useRef, useState, type TextareaHTMLAttributes } from "react";

import type { CanvasResourceReference } from "@/lib/canvas/canvas-resource-references";
import { CanvasResourceMentionTextarea } from "./canvas-resource-mention-textarea";

const DRAFT_IDLE_COMMIT_MS = 600;

export type CanvasTextDraftEditorHandle = {
    element: HTMLTextAreaElement | null;
    commit: () => void;
};

type CanvasTextDraftEditorProps = Omit<TextareaHTMLAttributes<HTMLTextAreaElement>, "value" | "onChange" | "onSubmit"> & {
    nodeId: string;
    value: string;
    references: CanvasResourceReference[];
    highlightLabels?: boolean;
    onCommit: (nodeId: string, value: string) => void;
    onStopEditing: () => void;
};

export const CanvasTextDraftEditor = forwardRef<CanvasTextDraftEditorHandle, CanvasTextDraftEditorProps>(function CanvasTextDraftEditor({ nodeId, value, references, highlightLabels, onCommit, onStopEditing, onKeyDown, ...textareaProps }, forwardedRef) {
    const [draft, setDraft] = useState(value);
    const draftRef = useRef(value);
    const committedRef = useRef(value);
    const textareaRef = useRef<HTMLTextAreaElement | null>(null);
    const idleTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

    const clearIdleCommit = useCallback(() => {
        if (!idleTimerRef.current) return;
        clearTimeout(idleTimerRef.current);
        idleTimerRef.current = null;
    }, []);

    const commit = useCallback(() => {
        clearIdleCommit();
        const next = draftRef.current;
        if (next === committedRef.current) return;
        committedRef.current = next;
        onCommit(nodeId, next);
    }, [clearIdleCommit, nodeId, onCommit]);

    useImperativeHandle(
        forwardedRef,
        () => ({
            get element() {
                return textareaRef.current;
            },
            commit,
        }),
        [commit],
    );

    useEffect(() => clearIdleCommit, [clearIdleCommit]);

    return (
        <CanvasResourceMentionTextarea
            {...textareaProps}
            ref={textareaRef}
            value={draft}
            references={references}
            highlightLabels={highlightLabels}
            onChange={(next) => {
                draftRef.current = next;
                setDraft(next);
                clearIdleCommit();
                idleTimerRef.current = setTimeout(commit, DRAFT_IDLE_COMMIT_MS);
            }}
            onBlur={() => {
                commit();
                onStopEditing();
            }}
            onKeyDown={(event) => {
                onKeyDown?.(event);
                if (event.defaultPrevented || event.key !== "Escape") return;
                commit();
                onStopEditing();
            }}
        />
    );
});
