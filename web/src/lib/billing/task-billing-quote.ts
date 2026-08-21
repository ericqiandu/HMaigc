import type { CreateTaskInput, TaskBillingQuote, TaskBillingQuoteRequest } from "@/services/api/task-center";

type QuoteConfigSource = Record<string, unknown>;

type BuildTaskBillingQuoteRequestInput = {
    projectId: string;
    mode: "image" | "video";
    operation: string;
    batchCount: number;
    usage: {
        referenceImageCount: number;
        referenceVideoCount: number;
    };
    config: QuoteConfigSource;
};

export type TaskBillingQuoteLoader = (request: TaskBillingQuoteRequest, signal?: AbortSignal) => Promise<TaskBillingQuote>;
export type ConfirmedTaskBillingQuote = Pick<TaskBillingQuote, "priceVersion" | "quoteFingerprint">;

export function taskBillingQuoteMatches(left: ConfirmedTaskBillingQuote | undefined, right: ConfirmedTaskBillingQuote): boolean {
    return Boolean(left && left.priceVersion === right.priceVersion && left.quoteFingerprint === right.quoteFingerprint);
}

export class TaskPriceChangedError extends Error {
    readonly currentQuote: TaskBillingQuote;

    constructor(currentQuote: TaskBillingQuote) {
        super("预计积分已变化，请确认新报价后重试");
        this.name = "TaskPriceChangedError";
        this.currentQuote = currentQuote;
    }
}

export function buildTaskBillingQuoteRequest({ projectId, mode, operation, batchCount, usage, config }: BuildTaskBillingQuoteRequestInput): TaskBillingQuoteRequest {
    const fps = finiteNumber(config.videoSuperResolutionFps);
    return {
        projectId: requiredString(projectId, "画布"),
        type: mode === "image" ? "canvas_image" : "canvas_video",
        operation,
        batchCount,
        input: {
            mode,
            referenceImageCount: nonNegativeInteger(usage.referenceImageCount, "参考图片数量"),
            referenceVideoCount: nonNegativeInteger(usage.referenceVideoCount, "参考视频数量"),
            config: {
                channelId: requiredString(config.channelId, "渠道"),
                model: requiredString(config.model, "模型"),
                size: optionalString(config.size),
                quality: optionalString(config.quality),
                videoSeconds: optionalString(config.videoSeconds),
                vquality: optionalString(config.vquality),
                videoSuperResolutionEnabled: config.videoSuperResolutionEnabled === true || config.videoSuperResolutionEnabled === "true",
                videoSuperResolutionResolution: optionalString(config.videoSuperResolutionResolution),
                videoSuperResolutionVersion: optionalString(config.videoSuperResolutionVersion),
                videoSuperResolutionFps: fps > 0 ? fps : 0,
            },
        },
    };
}

export async function prepareGenerationTaskSubmission(input: CreateTaskInput, expectedQuote: ConfirmedTaskBillingQuote | undefined, loadQuote: TaskBillingQuoteLoader, signal?: AbortSignal): Promise<CreateTaskInput> {
    const quoteRequest = quoteRequestFromTaskInput(input);
    if (!quoteRequest) return input;

    if (expectedQuote) {
        return { ...input, quotePriceVersion: expectedQuote.priceVersion, quoteFingerprint: expectedQuote.quoteFingerprint };
    }

    const currentQuote = await loadQuote(quoteRequest, signal);
    return { ...input, quotePriceVersion: currentQuote.priceVersion, quoteFingerprint: currentQuote.quoteFingerprint };
}

export function taskPriceChangedQuoteFromEnvelope(value: unknown): TaskBillingQuote | null {
    if (!isRecord(value) || !isRecord(value.data) || value.data.errorCode !== "PRICE_CHANGED" || !isRecord(value.data.currentQuote)) return null;
    const quote = value.data.currentQuote;
    if (
        typeof quote.amountMicrocredits !== "number" ||
        typeof quote.perTaskAmountMicrocredits !== "number" ||
        typeof quote.taskCount !== "number" ||
        typeof quote.priceVersion !== "number" ||
        (quote.billingMode !== "fixed_request" && quote.billingMode !== "per_second") ||
        typeof quote.pricingResolution !== "string" ||
        typeof quote.pricingInputVariant !== "string" ||
        typeof quote.quantity !== "number" ||
        typeof quote.enhancementAmountMicrocredits !== "number" ||
        typeof quote.quoteFingerprint !== "string"
    ) {
        return null;
    }
    const usageAdjustment = parseUsageAdjustment(quote.usageAdjustment);
    if (quote.usageAdjustment !== undefined && usageAdjustment === null) return null;
    return {
        amountMicrocredits: quote.amountMicrocredits,
        perTaskAmountMicrocredits: quote.perTaskAmountMicrocredits,
        taskCount: quote.taskCount,
        priceVersion: quote.priceVersion,
        billingMode: quote.billingMode,
        pricingResolution: quote.pricingResolution,
        pricingInputVariant: quote.pricingInputVariant,
        quantity: quote.quantity,
        enhancementAmountMicrocredits: quote.enhancementAmountMicrocredits,
        ...(usageAdjustment ? { usageAdjustment } : {}),
        quoteFingerprint: quote.quoteFingerprint,
    };
}

function quoteRequestFromTaskInput(input: CreateTaskInput): TaskBillingQuoteRequest | null {
    const mode = input.input?.mode;
    if (mode !== "image" && mode !== "video") return null;
    const config = input.input?.config;
    if (!isRecord(config)) throw new Error("生成任务缺少可报价的模型配置");
    const referenceImages = input.input?.referenceImages;
    const referenceVideos = input.input?.referenceVideos;
    return buildTaskBillingQuoteRequest({
        projectId: requiredString(input.projectId, "画布"),
        mode,
        operation: input.operation?.trim() || mode,
        batchCount: 1,
        usage: {
            referenceImageCount: Array.isArray(referenceImages) ? referenceImages.length : 0,
            referenceVideoCount: Array.isArray(referenceVideos) ? referenceVideos.length : 0,
        },
        config,
    });
}

function parseUsageAdjustment(value: unknown): TaskBillingQuote["usageAdjustment"] | null {
    if (value === undefined) return undefined;
    if (!isRecord(value) || value.metric !== "input_image") return null;
    const numericKeys = ["actualQuantity", "includedQuantity", "billableQuantity", "unitPriceMicrocredits", "perTaskAmountMicrocredits", "amountMicrocredits"] as const;
    if (numericKeys.some((key) => typeof value[key] !== "number" || !Number.isSafeInteger(value[key]) || value[key] < 0)) return null;
    return {
        metric: "input_image",
        actualQuantity: value.actualQuantity as number,
        includedQuantity: value.includedQuantity as number,
        billableQuantity: value.billableQuantity as number,
        unitPriceMicrocredits: value.unitPriceMicrocredits as number,
        perTaskAmountMicrocredits: value.perTaskAmountMicrocredits as number,
        amountMicrocredits: value.amountMicrocredits as number,
    };
}

function isRecord(value: unknown): value is Record<string, unknown> {
    return typeof value === "object" && value !== null && !Array.isArray(value);
}

function requiredString(value: unknown, label: string): string {
    const text = optionalString(value).trim();
    if (!text) throw new Error(`生成任务缺少${label}配置`);
    return text;
}

function optionalString(value: unknown): string {
    return typeof value === "string" ? value : typeof value === "number" ? String(value) : "";
}

function finiteNumber(value: unknown): number {
    const parsed = typeof value === "number" ? value : typeof value === "string" && value.trim() ? Number(value) : 0;
    return Number.isFinite(parsed) ? parsed : 0;
}

function nonNegativeInteger(value: unknown, label: string): number {
    const parsed = typeof value === "number" ? value : Number.NaN;
    if (!Number.isSafeInteger(parsed) || parsed < 0) throw new Error(`${label}无效`);
    return parsed;
}
