import { nanoid } from "nanoid";

import { NODE_DEFAULT_SIZE } from "@/constant/canvas";
import { audioMetadata, storeBackendGeneratedVideo, videoMetadata } from "@/lib/canvas/canvas-generation-task-sync";
import { fitNodeSize, nodeSizeFromRatio } from "@/lib/canvas/canvas-node-size";
import { nextCanvasVersionLabel } from "@/lib/canvas/canvas-layout";
import { buildAudioGenerationMetadata, buildVideoGenerationMetadata, generationReferenceUrls, runBackendCanvasGenerationTask } from "@/lib/canvas/canvas-project-generation";
import { selectVideoGenerationContext } from "@/lib/canvas/canvas-video-generation-mode";
import { runVideoOutputBatch } from "@/lib/canvas/video-output-batch";
import { generationFailureMetadata } from "@/lib/generation-error";
import { storeGeneratedAudio } from "@/services/api/audio";
import { CanvasNodeType, type CanvasNodeData } from "@/types/canvas";

import type { CanvasGenerationExecution } from "./canvas-generation-executor-types";

const VIDEO_NODE_MAX_WIDTH = 420;
const VIDEO_NODE_MAX_HEIGHT = 420;
const NODE_STATUS_LOADING = "loading" as const;
const NODE_STATUS_SUCCESS = "success" as const;

export async function executeVideoGeneration({
    nodeId,
    sourceNode,
    effectivePrompt,
    generationConfig,
    generationContext,
    controller,
    projectId,
    setNodes,
    setConnections,
    startGenerationRequest,
    finishGenerationRequest,
    bindGenerationTask,
    showError,
    registerPendingNodeIds,
}: CanvasGenerationExecution) {
    const spec = nodeSizeFromRatio(generationConfig.size, NODE_DEFAULT_SIZE[CanvasNodeType.Video].width, NODE_DEFAULT_SIZE[CanvasNodeType.Video].height) || NODE_DEFAULT_SIZE[CanvasNodeType.Video];
    const isEmptyVideoNode = sourceNode?.type === CanvasNodeType.Video && !sourceNode.metadata?.content;
    const isExistingVideoNode = sourceNode?.type === CanvasNodeType.Video && Boolean(sourceNode.metadata?.content);
    const count = Number(generationConfig.count || "1");
    if (count !== 1 && count !== 2 && count !== 4) throw new Error(`不支持的视频生成数量：${generationConfig.count}`);
    const videoId = isEmptyVideoNode ? nodeId : nanoid();
    const childIds = count > 1 ? Array.from({ length: count - 1 }, () => nanoid()) : [];
    const targetIds = [videoId, ...childIds];
    const parent = sourceNode?.position || { x: 0, y: 0 };
    const selectedGenerationContext = selectVideoGenerationContext(sourceNode?.metadata, generationContext);
    const videoGenerationMetadata = buildVideoGenerationMetadata(sourceNode, selectedGenerationContext);
    const videoNode: CanvasNodeData = {
        id: videoId,
        type: CanvasNodeType.Video,
        title: effectivePrompt.slice(0, 32) || "Generated Video",
        position: isEmptyVideoNode ? sourceNode.position : { x: parent.x + (sourceNode?.width || spec.width) + 96, y: parent.y },
        width: isEmptyVideoNode ? sourceNode.width : spec.width,
        height: isEmptyVideoNode ? sourceNode.height : spec.height,
        metadata: {
            ...(isEmptyVideoNode ? sourceNode.metadata || {} : {}),
            prompt: effectivePrompt,
            status: NODE_STATUS_LOADING,
            isBatchRoot: count > 1,
            batchChildIds: count > 1 ? childIds : undefined,
            batchExpanded: count > 1 ? true : undefined,
            errorDetails: undefined,
            generationErrorCode: undefined,
            failedPromptFingerprint: undefined,
            model: generationConfig.model,
            size: generationConfig.size,
            seconds: generationConfig.videoSeconds,
            vquality: generationConfig.vquality,
            generateAudio: generationConfig.videoGenerateAudio,
            watermark: generationConfig.videoWatermark,
            superResolutionEnabled: generationConfig.videoSuperResolutionEnabled,
            superResolutionResolution: generationConfig.videoSuperResolutionResolution,
            superResolutionScene: generationConfig.videoSuperResolutionScene,
            superResolutionVersion: generationConfig.videoSuperResolutionVersion,
            superResolutionFps: generationConfig.videoSuperResolutionFps,
            references: generationReferenceUrls(selectedGenerationContext),
            ...videoGenerationMetadata,
        },
    };
    const childNodes: CanvasNodeData[] = childIds.map((id, index) => ({
        id,
        type: CanvasNodeType.Video,
        title: effectivePrompt.slice(0, 32) || "Generated Video",
        position: {
            x: videoNode.position.x + videoNode.width + 120 + (index % 2) * (spec.width + 36),
            y: videoNode.position.y + Math.floor(index / 2) * (spec.height + 36),
        },
        width: spec.width,
        height: spec.height,
        metadata: {
            prompt: effectivePrompt,
            status: NODE_STATUS_LOADING,
            batchRootId: videoId,
            model: generationConfig.model,
            size: generationConfig.size,
            seconds: generationConfig.videoSeconds,
            vquality: generationConfig.vquality,
            generateAudio: generationConfig.videoGenerateAudio,
            watermark: generationConfig.videoWatermark,
            superResolutionEnabled: generationConfig.videoSuperResolutionEnabled,
            superResolutionResolution: generationConfig.videoSuperResolutionResolution,
            superResolutionScene: generationConfig.videoSuperResolutionScene,
            superResolutionVersion: generationConfig.videoSuperResolutionVersion,
            superResolutionFps: generationConfig.videoSuperResolutionFps,
            references: generationReferenceUrls(selectedGenerationContext),
            ...videoGenerationMetadata,
        },
    }));
    registerPendingNodeIds([videoId, ...childIds]);
    setNodes((current) => {
        if (isEmptyVideoNode) return [...current.map((node) => (node.id === nodeId ? { ...node, ...videoNode } : node)), ...childNodes];
        if (!isExistingVideoNode || !sourceNode) return [...current.map((node) => (node.id === nodeId ? { ...node, metadata: { ...node.metadata, status: NODE_STATUS_SUCCESS } } : node)), videoNode, ...childNodes];
        const rootId = sourceNode.metadata?.versionOfNodeId || sourceNode.id;
        const nextLabel = nextCanvasVersionLabel(rootId, current);
        return [
            ...current.map((node) => {
                if ((node.metadata?.versionOfNodeId || node.id) !== rootId) return node;
                return { ...node, metadata: { ...node.metadata, versionOfNodeId: rootId, versionLabel: node.metadata?.versionLabel || "A", versionPrimary: false, status: node.id === nodeId ? NODE_STATUS_SUCCESS : node.metadata?.status } };
            }),
            { ...videoNode, metadata: { ...videoNode.metadata, versionOfNodeId: rootId, versionLabel: nextLabel, versionPrimary: true } },
            ...childNodes,
        ];
    });
    setConnections((current) => [
        ...current,
        ...(!isEmptyVideoNode ? [{ id: nanoid(), fromNodeId: nodeId, toNodeId: videoId }] : []),
        ...childIds.map((childId) => ({ id: nanoid(), fromNodeId: videoId, toNodeId: childId })),
    ]);

    targetIds.forEach((targetId) => startGenerationRequest(targetId, nodeId, nodeId, controller));
    const batch = await runVideoOutputBatch(targetIds, async (targetId) => {
            try {
                const result = await runBackendCanvasGenerationTask({ projectId, nodeId: targetId, mode: "video", prompt: effectivePrompt, config: { ...generationConfig, count: "1" }, referenceImages: selectedGenerationContext.referenceImages, referenceVideos: selectedGenerationContext.referenceVideos, referenceAudios: selectedGenerationContext.referenceAudios, signal: controller.signal, metadata: { sourceNodeId: nodeId, batchRootId: count > 1 ? videoId : undefined, resolvedCharacterVersions: selectedGenerationContext.resolvedCharacterVersions, resolvedCharacterVoices: selectedGenerationContext.resolvedCharacterVoices, ...videoGenerationMetadata }, onTaskCreated: (task) => bindGenerationTask(targetId, task) });
                if (!result.video?.dataUrl) throw new Error("后端任务没有返回视频");
                const video = await storeBackendGeneratedVideo(result.video);
                const videoSize = fitNodeSize(video.width || spec.width, video.height || spec.height, VIDEO_NODE_MAX_WIDTH, VIDEO_NODE_MAX_HEIGHT);
                setNodes((current) => {
                    return current.map((node) => {
                        if (node.id !== targetId) return node;
                        const center = { x: node.position.x + node.width / 2, y: node.position.y + node.height / 2 };
                        const geometry = node.metadata?.locked ? {} : { width: videoSize.width, height: videoSize.height, position: { x: center.x - videoSize.width / 2, y: center.y - videoSize.height / 2 } };
                        const generatedMetadata = { ...videoMetadata(video), prompt: effectivePrompt, model: generationConfig.model, size: generationConfig.size, seconds: generationConfig.videoSeconds, vquality: generationConfig.vquality, generateAudio: generationConfig.videoGenerateAudio, watermark: generationConfig.videoWatermark, superResolutionEnabled: generationConfig.videoSuperResolutionEnabled, superResolutionResolution: generationConfig.videoSuperResolutionResolution, superResolutionScene: generationConfig.videoSuperResolutionScene, superResolutionVersion: generationConfig.videoSuperResolutionVersion, superResolutionFps: generationConfig.videoSuperResolutionFps, references: generationReferenceUrls(selectedGenerationContext), ...videoGenerationMetadata };
                        return { ...node, ...geometry, metadata: { ...node.metadata, ...generatedMetadata } };
                    });
                });
                return targetId;
            } catch (error) {
                if (!controller.signal.aborted) {
                    const failure = generationFailureMetadata(error, effectivePrompt);
                    setNodes((current) => current.map((node) => (node.id === targetId ? { ...node, metadata: { ...node.metadata, status: "error", ...failure } } : node)));
                }
                throw error;
            } finally {
                finishGenerationRequest(targetId, controller);
            }
        });
    if (controller.signal.aborted) return;
    if (batch.failed.length) showError(batch.succeeded.length ? "部分视频生成失败" : "全部视频生成失败");
}

export async function executeAudioGeneration({
    nodeId,
    sourceNode,
    effectivePrompt,
    generationConfig,
    generationContext,
    controller,
    projectId,
    setNodes,
    setConnections,
    startGenerationRequest,
    finishGenerationRequest,
    bindGenerationTask,
    registerPendingNodeIds,
}: CanvasGenerationExecution) {
    const spec = NODE_DEFAULT_SIZE[CanvasNodeType.Audio];
    const isEmptyAudioNode = sourceNode?.type === CanvasNodeType.Audio && !sourceNode.metadata?.content;
    const audioId = isEmptyAudioNode ? nodeId : nanoid();
    const parent = sourceNode?.position || { x: 0, y: 0 };
    const audioNode: CanvasNodeData = {
        id: audioId,
        type: CanvasNodeType.Audio,
        title: effectivePrompt.slice(0, 32) || "Generated Audio",
        position: isEmptyAudioNode ? sourceNode.position : { x: parent.x + (sourceNode?.width || spec.width) + 96, y: parent.y + ((sourceNode?.height || spec.height) - spec.height) / 2 },
        width: isEmptyAudioNode ? sourceNode.width : spec.width,
        height: isEmptyAudioNode ? sourceNode.height : spec.height,
        metadata: { prompt: effectivePrompt, status: NODE_STATUS_LOADING, ...buildAudioGenerationMetadata(generationConfig) },
    };
    registerPendingNodeIds([audioId]);
    setNodes((current) => (isEmptyAudioNode ? current.map((node) => (node.id === nodeId ? { ...node, ...audioNode } : node)) : [...current.map((node) => (node.id === nodeId ? { ...node, metadata: { ...node.metadata, status: NODE_STATUS_SUCCESS } } : node)), audioNode]));
    if (!isEmptyAudioNode) setConnections((current) => [...current, { id: nanoid(), fromNodeId: nodeId, toNodeId: audioId }]);

    startGenerationRequest(audioId, nodeId, nodeId, controller);
    try {
        const result = await runBackendCanvasGenerationTask({ projectId, nodeId: audioId, mode: "audio", prompt: effectivePrompt, config: generationConfig, signal: controller.signal, metadata: { sourceNodeId: nodeId, resolvedCharacterVersions: generationContext.resolvedCharacterVersions, resolvedCharacterVoiceKey: generationContext.resolvedCharacterVoices[0]?.voiceKey }, onTaskCreated: (task) => bindGenerationTask(audioId, task) });
        if (!result.audio?.dataUrl) throw new Error("后端任务没有返回音频");
        const audio = await storeGeneratedAudio(await (await fetch(result.audio.dataUrl)).blob(), generationConfig.audioFormat);
        setNodes((current) => current.map((node) => (node.id === audioId ? { ...node, metadata: { ...node.metadata, ...audioMetadata(audio), prompt: effectivePrompt, ...buildAudioGenerationMetadata(generationConfig) } } : node)));
    } finally {
        finishGenerationRequest(audioId, controller);
    }
}
