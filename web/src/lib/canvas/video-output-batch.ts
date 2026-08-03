export type VideoOutputBatchFailure<Id extends string> = Readonly<{
    id: Id;
    error: Error;
}>;

export type VideoOutputBatchResult<Id extends string> = Readonly<{
    succeeded: Id[];
    failed: VideoOutputBatchFailure<Id>[];
}>;

export async function runVideoOutputBatch<Id extends string, Value>(ids: readonly Id[], execute: (id: Id) => Promise<Value>): Promise<VideoOutputBatchResult<Id>> {
    const settled = await Promise.allSettled(ids.map(async (id) => ({ id, value: await execute(id) })));
    const succeeded: Id[] = [];
    const failed: VideoOutputBatchFailure<Id>[] = [];

    settled.forEach((item, index) => {
        const id = ids[index];
        if (item.status === "fulfilled") {
            succeeded.push(item.value.id);
            return;
        }
        failed.push({ id, error: item.reason instanceof Error ? item.reason : new Error(String(item.reason)) });
    });

    return { succeeded, failed };
}
