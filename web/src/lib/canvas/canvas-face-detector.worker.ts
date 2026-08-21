/// <reference lib="webworker" />

import { FaceDetector } from "@mediapipe/tasks-vision";

import { staticAssetURL } from "@/lib/static-assets";
import type { CanvasFaceBox } from "./canvas-emotion";

type DetectFaceRequest = {
    id: number;
    image: ImageBitmap;
};

type DetectFaceResponse = {
    id: number;
    faces?: CanvasFaceBox[];
    imageWidth?: number;
    imageHeight?: number;
    error?: string;
};

let detectorPromise: Promise<FaceDetector> | null = null;

const workerGlobal = self as typeof self & {
    importScripts: (...urls: string[]) => void;
    import?: (url: string) => Promise<unknown>;
};

// MediaPipe 会在模块 Worker 中从 importScripts 失败分支转向 self.import。
// loader 是同源 public 资源，直接导入可遵守 script-src 'self'，也避免 blob: 模块被 CSP 拒绝。
workerGlobal.importScripts = () => { throw new TypeError("module worker uses dynamic import"); };
workerGlobal.import = (url: string) => import(/* @vite-ignore */ url);

function getDetector() {
    if (!detectorPromise) {
        detectorPromise = FaceDetector.createFromOptions(
            {
                wasmLoaderPath: staticAssetURL("/mediapipe/wasm/vision_wasm_module_internal.js"),
                wasmBinaryPath: staticAssetURL("/mediapipe/wasm/vision_wasm_module_internal.wasm"),
            },
            {
                baseOptions: { modelAssetPath: staticAssetURL("/runtime-assets/canvas-models/blaze-face-full-range-sparse.tflite") },
                runningMode: "IMAGE",
                minDetectionConfidence: 0.25,
                minSuppressionThreshold: 0.3,
            },
        );
    }
    return detectorPromise;
}

self.onmessage = async (event: MessageEvent<DetectFaceRequest>) => {
    const { id, image } = event.data;
    const response: DetectFaceResponse = { id, imageWidth: image.width, imageHeight: image.height };
    try {
        const detector = await getDetector();
        response.faces = detector.detect(image).detections.flatMap((detection, index) => {
            const box = detection.boundingBox;
            if (!box) return [];
            return [{
                id: `face-${id}-${index}`,
                x: box.originX,
                y: box.originY,
                width: box.width,
                height: box.height,
                confidence: detection.categories[0]?.score,
                source: "detected" as const,
            }];
        });
    } catch (error) {
        response.error = error instanceof Error ? error.message : "人脸识别失败";
    } finally {
        image.close();
    }
    self.postMessage(response);
};

export {};
