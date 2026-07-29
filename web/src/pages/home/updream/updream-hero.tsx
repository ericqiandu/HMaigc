import { useEffect, useRef, useState } from "react";
import { useNavigate } from "react-router";
import { App } from "antd";
import { nanoid } from "nanoid";

import { createAgentCanvasProjectWithRemoteSync } from "@/services/user-data-sync";
import { useEffectiveConfig } from "@/stores/use-config-store";
import { useThemeStore } from "@/stores/use-theme-store";
import { canvasThemes } from "@/lib/canvas-theme";
import { uploadImage } from "@/services/image-storage";
import type {
    CanvasAgentExecutionMode,
    CanvasAgentGenerationModels,
    CanvasAgentSkillSelection,
} from "@/types/canvas";
import { CanvasAgentComposerControls } from "@/components/canvas/canvas-agent-composer-controls";
import {
    AgentChatComposer,
    type CanvasAgentChatAttachment,
} from "@/components/canvas/canvas-agent-chat-ui";

const MAX_REFERENCE_IMAGES = 4;

type HomeReferenceAttachment = CanvasAgentChatAttachment & {
    file: File;
};

const PLACEHOLDERS = [
    '试试说"在画布上为我创建…"，生成不阻塞，随时开启下一轮对话',
    "描述你想创作的内容，AI 帮你生成分镜",
    "进入项目后，按 @ 可引用资产库素材",
] as const;

export function UpdreamHero() {
    const { message } = App.useApp();
    const navigate = useNavigate();
    const config = useEffectiveConfig();
    const theme = canvasThemes[useThemeStore((state) => state.theme)];
    const [value, setValue] = useState("");
    const [executionMode, setExecutionMode] = useState<CanvasAgentExecutionMode>("guided");
    const [models, setModels] = useState<CanvasAgentGenerationModels>({ image: "", video: "" });
    const [selectedSkills, setSelectedSkills] = useState<CanvasAgentSkillSelection[]>([]);
    const [referenceAttachments, setReferenceAttachments] = useState<HomeReferenceAttachment[]>([]);
    const [submitting, setSubmitting] = useState(false);
    const [placeholderIndex, setPlaceholderIndex] = useState(0);
    const [placeholderVisible, setPlaceholderVisible] = useState(true);
    const transitionTimeoutRef = useRef<number | null>(null);
    const referenceAttachmentsRef = useRef<HomeReferenceAttachment[]>([]);

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
        referenceAttachmentsRef.current = referenceAttachments;
    }, [referenceAttachments]);

    useEffect(() => () => {
        referenceAttachmentsRef.current.forEach((attachment) => URL.revokeObjectURL(attachment.url));
    }, []);

    const addReferenceImages = (files: FileList | File[] | null) => {
        const images = Array.from(files || []).filter((file) => file.type.startsWith("image/"));
        if (!images.length) return;
        const remaining = MAX_REFERENCE_IMAGES - referenceAttachments.length;
        if (remaining <= 0) {
            message.warning(`最多添加 ${MAX_REFERENCE_IMAGES} 张参考图片`);
            return;
        }
        const accepted = images.slice(0, remaining);
        if (accepted.length < images.length) message.warning(`最多添加 ${MAX_REFERENCE_IMAGES} 张参考图片`);
        setReferenceAttachments((current) => [
            ...current,
            ...accepted.map((file) => ({
                id: nanoid(),
                name: file.name,
                url: URL.createObjectURL(file),
                file,
            })),
        ]);
    };

    const removeReferenceImage = (id: string) => {
        setReferenceAttachments((current) => {
            const removed = current.find((attachment) => attachment.id === id);
            if (removed) URL.revokeObjectURL(removed.url);
            return current.filter((attachment) => attachment.id !== id);
        });
    };

    const startCreating = async () => {
        const prompt = value.trim();
        if (!prompt || submitting) return;
        setSubmitting(true);
        try {
            const referenceImages = await Promise.all(
                referenceAttachments.map(async (attachment) => ({
                    ...(await uploadImage(attachment.file)),
                    name: attachment.name,
                })),
            );
            const { id, syncError } = await createAgentCanvasProjectWithRemoteSync({
                prompt,
                mode: executionMode,
                models,
                skills: selectedSkills,
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
            <h1 className="updream-hero-title text-center">
                让算力更有想象力
            </h1>
            <p className="updream-hero-description max-w-[620px] text-center">
                从一个想法开始，让 AI 和你一起完成剧本、分镜与影像创作
            </p>

            <div className="updream-home-agent-composer w-full">
                <AgentChatComposer
                    prompt={value}
                    attachments={referenceAttachments}
                    disabled={submitting}
                    sending={submitting}
                    submitReady={Boolean(value.trim())}
                    placeholder={placeholderVisible ? PLACEHOLDERS[placeholderIndex] : ""}
                    theme={theme}
                    onPromptChange={setValue}
                    onSubmit={() => void startCreating()}
                    onAddFiles={addReferenceImages}
                    onRemoveAttachment={removeReferenceImage}
                    left={
                        <CanvasAgentComposerControls
                            config={config}
                            disabled={submitting}
                            models={models}
                            selectedSkills={selectedSkills}
                            executionMode={executionMode}
                            placement="bottomLeft"
                            onModelsChange={setModels}
                            onSkillsChange={setSelectedSkills}
                            onExecutionModeChange={setExecutionMode}
                        />
                    }
                />
            </div>
        </section>
    );
}
