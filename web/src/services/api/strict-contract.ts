export function object(value: unknown, label: string): Record<string, unknown> {
    if (!value || typeof value !== "object" || Array.isArray(value)) throw new Error(`${label} 必须是对象`);
    return value as Record<string, unknown>;
}

export function exactObject(value: unknown, label: string, allowedKeys: readonly string[]): Record<string, unknown> {
    const source = object(value, label);
    const allowed = new Set(allowedKeys);
    const unknownKey = Object.keys(source).find((key) => !allowed.has(key));
    if (unknownKey) throw new Error(`${label} 包含未知字段: ${unknownKey}`);
    return source;
}

export function array(value: unknown, label: string): unknown[] {
    if (!Array.isArray(value)) throw new Error(`${label} 必须是数组`);
    return value;
}

export function text(value: unknown, label: string, allowEmpty = false): string {
    if (typeof value !== "string" || (!allowEmpty && !value.trim())) throw new Error(`${label} 必须是字符串`);
    return value;
}

export function boundedText(value: unknown, label: string, maxLength: number, allowEmpty = false): string {
    const result = text(value, label, allowEmpty);
    if (Array.from(result).length > maxLength) throw new Error(`${label} 长度不能超过 ${maxLength}`);
    return result;
}

export function integer(value: unknown, label: string, allowZero = false): number {
    if (typeof value !== "number" || !Number.isSafeInteger(value) || value < (allowZero ? 0 : 1)) {
        throw new Error(`${label} 必须是${allowZero ? "非负" : "正"}整数`);
    }
    return value;
}

export function flag(value: unknown, label: string): boolean {
    if (typeof value !== "boolean") throw new Error(`${label} 必须是布尔值`);
    return value;
}
