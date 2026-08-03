export function formatModelEstimatedDuration(seconds: number | undefined): string | null {
    if (!Number.isFinite(seconds) || Number(seconds) <= 0) return null;
    const minutes = Math.ceil(Number(seconds) / 60);
    return `${minutes}min`;
}
