import { expect, test } from "bun:test";

import { buildCanvasReferenceManifest, buildNodeMentionReferences } from "../src/lib/canvas/canvas-resource-references";
import { CanvasNodeType, type CanvasConnection, type CanvasNodeData } from "../src/types/canvas";

test("resource-backed upload uses the authoritative resource endpoint for its dialog cover", () => {
    const uploadedImage = {
        id: "uploaded-image",
        type: CanvasNodeType.Image,
        title: "自定义上传图",
        position: { x: 0, y: 0 },
        width: 320,
        height: 180,
        metadata: {
            content: "https://expired-oss.example.com/upload.png?signature=expired",
            storageKey: "resource:upload-resource",
        },
    } satisfies CanvasNodeData;
    const videoNode = {
        id: "video-node",
        type: CanvasNodeType.Video,
        title: "视频",
        position: { x: 400, y: 0 },
        width: 320,
        height: 180,
    } satisfies CanvasNodeData;
    const connection = {
        id: "image-to-video",
        fromNodeId: uploadedImage.id,
        toNodeId: videoNode.id,
    } satisfies CanvasConnection;

    const [reference] = buildNodeMentionReferences(videoNode, [uploadedImage, videoNode], [connection]);

    expect(reference.previewUrl).toBe("/api/resources/upload-resource/file?direct=1");
});

test("provider ordinals follow edge order while stable asset keys survive reordering", () => {
    const firstImage = imageNode("first-image", "resource:first-resource");
    const secondImage = imageNode("second-image", "resource:second-resource");
    const videoNode = targetVideoNode();
    const firstEdge = connection("first-edge", firstImage.id, videoNode.id);
    const secondEdge = connection("second-edge", secondImage.id, videoNode.id);

    const firstOrder = buildCanvasReferenceManifest(videoNode.id, [firstImage, secondImage, videoNode], [firstEdge, secondEdge]);
    const secondOrder = buildCanvasReferenceManifest(videoNode.id, [firstImage, secondImage, videoNode], [secondEdge, firstEdge]);

    expect(firstOrder.entries.map((entry) => [entry.assetKey, entry.ordinal])).toEqual([
        [firstImage.id, 1],
        [secondImage.id, 2],
    ]);
    expect(secondOrder.entries.map((entry) => [entry.assetKey, entry.ordinal])).toEqual([
        [secondImage.id, 1],
        [firstImage.id, 2],
    ]);
    expect(new Set(secondOrder.entries.map((entry) => entry.assetKey))).toEqual(new Set([firstImage.id, secondImage.id]));
});

test("removed edges disappear from both the derived shelf and provider manifest", () => {
    const firstImage = imageNode("first-image", "resource:first-resource");
    const secondImage = imageNode("second-image", "resource:second-resource");
    const videoNode = targetVideoNode();
    const remainingEdge = connection("second-edge", secondImage.id, videoNode.id);

    const references = buildNodeMentionReferences(videoNode, [firstImage, secondImage, videoNode], [remainingEdge]);
    const manifest = buildCanvasReferenceManifest(videoNode.id, [firstImage, secondImage, videoNode], [remainingEdge]);

    expect(references.map((reference) => reference.nodeId)).toEqual([secondImage.id]);
    expect(manifest.entries.map((entry) => entry.assetKey)).toEqual([secondImage.id]);
});

test("manifest exposes structural rejections and never executes media without a durable resource URL", () => {
    const missingResource = imageNode("missing-resource", undefined);
    const unsupportedFrame = {
        id: "frame-node",
        type: CanvasNodeType.Frame,
        title: "Frame",
        position: { x: 0, y: 0 },
        width: 320,
        height: 180,
    } satisfies CanvasNodeData;
    const videoNode = targetVideoNode();
    const missingEdge = connection("missing-edge", missingResource.id, videoNode.id);
    const unsupportedEdge = connection("unsupported-edge", unsupportedFrame.id, videoNode.id);

    const manifest = buildCanvasReferenceManifest(videoNode.id, [missingResource, unsupportedFrame, videoNode], [missingEdge, unsupportedEdge]);

    expect(manifest.entries).toEqual([]);
    expect(manifest.rejections.map((rejection) => rejection.code)).toEqual(["missing_resource_url", "unsupported_source_type"]);
    expect(manifest.rejections.every((rejection) => rejection.message.length > 0)).toBeTrue();
});

test("character references enter the manifest only through durable Resource metadata", () => {
    const character = {
        id: "character-xiaoming",
        type: CanvasNodeType.Text,
        title: "小明",
        position: { x: 0, y: 0 },
        width: 320,
        height: 260,
        metadata: {
            workflowKind: "character",
            characterAssetId: "character-asset-xiaoming",
            storageKey: "resource:character-resource-xiaoming",
        },
    } satisfies CanvasNodeData;
    const videoNode = targetVideoNode();

    const manifest = buildCanvasReferenceManifest(videoNode.id, [character, videoNode], [connection("character-edge", character.id, videoNode.id)]);

    expect(manifest.rejections).toEqual([]);
    expect(manifest.entries).toEqual([
        expect.objectContaining({
            assetKey: character.id,
            mediaType: "image",
            semanticRole: "character",
            resourceId: "character-resource-xiaoming",
        }),
    ]);
});

function imageNode(id: string, storageKey: string | undefined) {
    return {
        id,
        type: CanvasNodeType.Image,
        title: id,
        position: { x: 0, y: 0 },
        width: 320,
        height: 180,
        metadata: { content: `https://example.com/${id}.png`, storageKey },
    } satisfies CanvasNodeData;
}

function targetVideoNode() {
    return {
        id: "video-node",
        type: CanvasNodeType.Video,
        title: "视频",
        position: { x: 400, y: 0 },
        width: 320,
        height: 180,
    } satisfies CanvasNodeData;
}

function connection(id: string, fromNodeId: string, toNodeId: string) {
    return { id, fromNodeId, toNodeId } satisfies CanvasConnection;
}
