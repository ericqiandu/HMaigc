import type { ConfirmedTaskBillingQuote, TaskBillingQuoteLoader } from "./task-billing-quote";
import type { TaskBillingQuoteRequest } from "@/services/api/task-center";

export type GenerationBatchQuoteTarget = {
    targetId: string;
    request: TaskBillingQuoteRequest;
};

export type GenerationBatchQuoteConfirmation = ConfirmedTaskBillingQuote & {
    targetId: string;
};

export type GenerationBatchQuote = {
    amountMicrocredits: number;
    confirmations: GenerationBatchQuoteConfirmation[];
};

const taskQuoteBatchLimit = 15;

export function currentGenerationTargets<T extends { id: string }>(
    requested: readonly T[],
    current: readonly T[],
): T[] {
    const currentById = new Map(current.map((target) => [target.id, target]));
    return requested.map((target) => {
        const currentTarget = currentById.get(target.id);
        if (!currentTarget) throw new Error(`生成目标 ${target.id} 已不存在`);
        return currentTarget;
    });
}

export async function quoteGenerationBatch(targets: GenerationBatchQuoteTarget[], loadQuote: TaskBillingQuoteLoader): Promise<GenerationBatchQuote> {
    if (!targets.length) throw new Error("没有可报价的生成任务");
    const groups = groupQuoteTargets(targets);
    const quotedGroups = await Promise.all(groups.map(async (group) => {
        const quote = await loadQuote({ ...group.request, batchCount: group.targetIds.length });
        return { targetIds: group.targetIds, quote };
    }));
    const confirmationByTargetId = new Map(quotedGroups.flatMap(({ targetIds, quote }) => targetIds.map((targetId) => [targetId, {
        targetId,
        priceVersion: quote.priceVersion,
        quoteFingerprint: quote.quoteFingerprint,
    }] as const)));
    return {
        amountMicrocredits: quotedGroups.reduce((sum, group) => sum + group.quote.amountMicrocredits, 0),
        confirmations: targets.map(({ targetId }) => {
            const confirmation = confirmationByTargetId.get(targetId);
            if (!confirmation) throw new Error(`生成任务 ${targetId} 缺少报价确认`);
            return confirmation;
        }),
    };
}

function groupQuoteTargets(targets: GenerationBatchQuoteTarget[]) {
    const groups = new Map<string, { request: TaskBillingQuoteRequest; targetIds: string[] }>();
    targets.forEach(({ targetId, request }) => {
        const normalized = { ...request, batchCount: 1 };
        const key = JSON.stringify(normalized);
        const group = groups.get(key);
        if (group) group.targetIds.push(targetId);
        else groups.set(key, { request: normalized, targetIds: [targetId] });
    });
    return [...groups.values()].flatMap((group) => {
        const chunks: Array<{ request: TaskBillingQuoteRequest; targetIds: string[] }> = [];
        for (let offset = 0; offset < group.targetIds.length; offset += taskQuoteBatchLimit) {
            chunks.push({
                request: group.request,
                targetIds: group.targetIds.slice(offset, offset + taskQuoteBatchLimit),
            });
        }
        return chunks;
    });
}
