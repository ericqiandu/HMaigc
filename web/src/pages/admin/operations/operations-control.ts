export type OperationControlAction = "cancel" | "recover";

export function operationControlIdempotencyKey(action: OperationControlAction, operationId: string): string {
    return `control-${action}-${operationId}`;
}
