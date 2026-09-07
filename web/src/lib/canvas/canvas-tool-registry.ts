import { CONTENT_MODERATION_ERROR_CODE, isContentModerationError } from "@/lib/generation-error";
import { supportsVideoAssetUpload } from "@/lib/canvas/canvas-video-composition";
import { CanvasNodeType, type CanvasNodeData, type CanvasWorkspaceMode } from "@/types/canvas";

export type CanvasToolGroup = "primary" | "generate" | "edit" | "asset" | "node";

export type CanvasImageToolId =
    | "copyPrompt"
    | "reversePrompt"
    | "replace"
    | "resize"
    | "annotation"
    | "maskEdit"
    | "emotion"
    | "crop"
    | "split"
    | "upscale"
    | "superResolve"
    | "angle"
    | "view";

export type CanvasToolId =
    | "info"
    | "delete"
    | "retry"
    | "extractLastFrame"
    | "saveAsset"
    | "download"
    | "edit"
    | "editText"
    | "generateImage"
    | "config"
    | "decreaseFont"
    | "increaseFont"
    | "uploadImage"
    | "uploadVideo"
    | "uploadAudio"
    | CanvasImageToolId;

export interface CanvasToolDescriptor {
    id: CanvasToolId;
    group: CanvasToolGroup;
    order: number;
    visible: boolean;
    disabled: boolean;
    disabledReason?: string;
}

export interface CanvasToolContext {
    node: CanvasNodeData;
    workspaceMode: CanvasWorkspaceMode;
    extractingVideoFrame: boolean;
}

const imageToolIds: readonly CanvasImageToolId[] = [
    "copyPrompt",
    "reversePrompt",
    "replace",
    "resize",
    "annotation",
    "maskEdit",
    "emotion",
    "crop",
    "split",
    "upscale",
    "superResolve",
    "angle",
    "view",
];

function descriptor(id: CanvasToolId, group: CanvasToolGroup, order: number, visible: boolean): CanvasToolDescriptor {
    return { id, group, order, visible, disabled: false };
}

export function resolveCanvasTools({ node, workspaceMode, extractingVideoFrame }: CanvasToolContext): CanvasToolDescriptor[] {
    const isImage = node.type === CanvasNodeType.Image;
    const isVideo = node.type === CanvasNodeType.Video;
    const isAudio = node.type === CanvasNodeType.Audio;
    const isText = node.type === CanvasNodeType.Text;
    const isCharacterReference = isText && node.metadata?.workflowKind === "character" && Boolean(node.metadata.characterAssetId);
    const isEditableText = isText && !isCharacterReference;
    const isConfig = node.type === CanvasNodeType.Config;
    const hasImage = isImage && Boolean(node.metadata?.content);
    const hasVideo = isVideo && Boolean(node.metadata?.content);
    const hasAudio = isAudio && Boolean(node.metadata?.content);
    const simpleMode = workspaceMode === "simple";
    const requiresPromptChange = node.metadata?.generationErrorCode === CONTENT_MODERATION_ERROR_CODE || isContentModerationError(node.metadata?.errorDetails);
    const canRetry = node.metadata?.status === "error" && !requiresPromptChange;
    const tools: CanvasToolDescriptor[] = [
        descriptor("info", "node", 0, true),
        descriptor("delete", "node", 10, true),
        descriptor("retry", "generate", 20, canRetry),
        descriptor("extractLastFrame", "edit", 30, hasVideo && !simpleMode),
        descriptor("saveAsset", "asset", 40, hasImage || hasVideo || isEditableText),
        descriptor("download", "asset", 50, hasImage || hasVideo || hasAudio),
        descriptor("edit", "edit", 60, isEditableText || isImage || isVideo),
        descriptor("editText", "edit", 70, isEditableText),
        descriptor("generateImage", "generate", 80, isEditableText),
        descriptor("config", "generate", 90, isConfig && !simpleMode),
        descriptor("decreaseFont", "edit", 100, isEditableText && !simpleMode),
        descriptor("increaseFont", "edit", 110, isEditableText && !simpleMode),
        descriptor("uploadImage", "asset", 120, isImage && !hasImage),
        descriptor("uploadVideo", "asset", 130, supportsVideoAssetUpload(node)),
        descriptor("uploadAudio", "asset", 140, isAudio),
        ...imageToolIds.map((id, index) => descriptor(id, "edit", 150 + index * 10, hasImage && !simpleMode)),
    ];

    if (extractingVideoFrame) {
        const extractTool = tools.find((tool) => tool.id === "extractLastFrame");
        if (extractTool) return tools.map((tool) => tool === extractTool ? { ...tool, disabled: true, disabledReason: "正在截取尾帧" } : tool);
    }
    return tools;
}
