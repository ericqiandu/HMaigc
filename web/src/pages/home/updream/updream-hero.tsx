import { useEffect, useRef, useState } from "react";
import { useNavigate } from "react-router";
import { App } from "antd";
import { nanoid } from "nanoid";

import { createAgentCanvasProjectWithRemoteSync } from "@/services/user-data-sync";
import { useThemeStore } from "@/stores/use-theme-store";
import { canvasThemes } from "@/lib/canvas-theme";
import { uploadImage } from "@/services/image-storage";
import { resourceIdFromStorageKey } from "@/services/api/resources";
import { AgentChatComposer } from "@/components/canvas/canvas-agent-chat-ui";
import { CanvasAgentComposerControls } from "@/components/canvas/canvas-agent-composer-controls";
import { CanvasAgentSelectionSummary } from "@/components/canvas/canvas-agent-selection-summary";
import { createEmptyCanvasAgentDraft, removeLastCanvasAgentDraftSelection, type CanvasAgentDraft } from "@/lib/canvas/canvas-agent-draft";
import { useEffectiveConfig } from "@/stores/use-config-store";

const MAX_REFERENCE_IMAGES = 4;

const PLACEHOLDERS = ['试试说"在画布上为我创建…"，生成不阻塞，随时开启下一轮对话', "描述你想创作的内容，AI 帮你生成分镜", "进入项目后，按 @ 可引用资产库素材"] as const;

export function UpdreamHero() {
    const { message } = App.useApp();
    const navigate = useNavigate();
    const theme = canvasThemes[useThemeStore((state) => state.theme)];
    const effectiveConfig = useEffectiveConfig();
    const [draft, setDraft] = useState<CanvasAgentDraft>(createEmptyCanvasAgentDraft);
    const [submitting, setSubmitting] = useState(false);
    const [placeholderIndex, setPlaceholderIndex] = useState(0);
    const [placeholderVisible, setPlaceholderVisible] = useState(true);
    const transitionTimeoutRef = useRef<number | null>(null);
    const attachmentsRef = useRef(draft.attachments);
    const attachmentFilesRef = useRef(new Map<string, File>());

    useEffect(() => {
        const timer = window.setInterval(() => {
            setPlaceholderVisible(false);
            transitionTimeoutRef.current = window.setTimeout(() => {
                setPlaceholderIndex((index) => (index + 1) % PLACEHOLDERS.length);
                setPlaceholderVisible(true);
            }, 300);
        }, 3600);

        return () => {
            window.clearInterval(timer);
            if (transitionTimeoutRef.current !== null) window.clearTimeout(transitionTimeoutRef.current);
        };
    }, []);

    useEffect(() => {
        attachmentsRef.current = draft.attachments;
    }, [draft.attachments]);

    useEffect(
        () => () => {
            attachmentsRef.current.forEach((attachment) => URL.revokeObjectURL(attachment.url));
        },
        [],
    );

    const addReferenceImages = (files: FileList | File[] | null) => {
        const images = Array.from(files || []).filter((file) => file.type.startsWith("image/"));
        if (!images.length) return;
        const remaining = MAX_REFERENCE_IMAGES - draft.attachments.length;
        if (remaining <= 0) {
            message.warning(`最多添加 ${MAX_REFERENCE_IMAGES} 张参考图片`);
            return;
        }
        const accepted = images.slice(0, remaining);
        if (accepted.length < images.length) message.warning(`最多添加 ${MAX_REFERENCE_IMAGES} 张参考图片`);
        const attachments = accepted.map((file) => {
            const id = nanoid();
            attachmentFilesRef.current.set(id, file);
            return { id, name: file.name, url: URL.createObjectURL(file) };
        });
        setDraft((current) => ({ ...current, attachments: [...current.attachments, ...attachments] }));
    };

    const removeReferenceImage = (id: string) => {
        setDraft((current) => {
            const removed = current.attachments.find((attachment) => attachment.id === id);
            if (removed) URL.revokeObjectURL(removed.url);
            attachmentFilesRef.current.delete(id);
            return { ...current, attachments: current.attachments.filter((attachment) => attachment.id !== id) };
        });
    };

    const startCreating = async () => {
        const prompt = draft.prompt.trim();
        if (!prompt || submitting) return;
        setSubmitting(true);
        try {
            const referenceImages = await Promise.all(
                draft.attachments.map(async (attachment) => {
                    const file = attachmentFilesRef.current.get(attachment.id);
                    if (!file) throw new Error(`参考图片“${attachment.name}”的本地文件已失效，请重新添加`);
                    return { ...(await uploadImage(file)), name: attachment.name };
                }),
            );
            const persistedAttachments = referenceImages.map((image, index) => {
                const resourceId = resourceIdFromStorageKey(image.storageKey);
                if (!resourceId) throw new Error(`参考图片“${image.name}”尚未保存到账号资源，请检查存储配置后重试`);
                const source = draft.attachments[index];
                return { id: source.id, name: image.name, url: image.url, resourceId };
            });
            const { id, syncError } = await createAgentCanvasProjectWithRemoteSync({
                draft: { ...draft, prompt, attachments: persistedAttachments },
                referenceImages,
            });
            if (syncError) {
                const reason = syncError instanceof Error ? syncError.message : "未知错误";
                message.warning(`项目已在本地创建，云端同步暂未完成：${reason}`);
            }
            navigate(`/canvas/${id}`);
        } catch (error) {
            message.error(error instanceof Error ? error.message : "项目创建失败");
            setSubmitting(false);
        }
    };

    return (
        <section className="updream-hero flex flex-col items-center px-4">
            <h1 className="updream-hero-title bg-clip-text text-center text-transparent">灵感从这里开始！</h1>

            <div className="updream-home-agent-composer w-full max-w-[700px]">
                <AgentChatComposer
                    prompt={draft.prompt}
                    attachments={draft.attachments}
                    disabled={submitting}
                    sending={submitting}
                    submitReady={Boolean(draft.prompt.trim())}
                    placeholder={placeholderVisible ? PLACEHOLDERS[placeholderIndex] : ""}
                    theme={theme}
                    onPromptChange={(prompt) => setDraft((current) => ({ ...current, prompt }))}
                    onSubmit={() => void startCreating()}
                    onAddFiles={addReferenceImages}
                    onRemoveAttachment={removeReferenceImage}
                    onDeleteBackwardAtStart={() => {
                        const next = removeLastCanvasAgentDraftSelection(draft);
                        if (!next) return false;
                        setDraft(next);
                        return true;
                    }}
                    selectionSummary={
                        <CanvasAgentSelectionSummary
                            config={effectiveConfig}
                            models={draft.generationModels}
                            selectedSkills={draft.skillSelections}
                            disabled={submitting}
                            onModelsChange={(generationModels) => setDraft((current) => ({ ...current, generationModels }))}
                            onSkillsChange={(skillSelections) => setDraft((current) => ({ ...current, skillSelections }))}
                        />
                    }
                    left={
                        <CanvasAgentComposerControls
                            config={effectiveConfig}
                            disabled={submitting}
                            models={draft.generationModels}
                            selectedSkills={draft.skillSelections}
                            executionMode={draft.executionMode}
                            placement="bottom"
                            onModelsChange={(generationModels) => setDraft((current) => ({ ...current, generationModels }))}
                            onSkillsChange={(skillSelections) => setDraft((current) => ({ ...current, skillSelections }))}
                            onExecutionModeChange={(executionMode) => setDraft((current) => ({ ...current, executionMode }))}
                        />
                    }
                />
            </div>
        </section>
    );
}
