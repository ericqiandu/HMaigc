export const audioVoiceOptions = [
    { value: "alloy", label: "Alloy" },
    { value: "ash", label: "Ash" },
    { value: "ballad", label: "Ballad" },
    { value: "coral", label: "Coral" },
    { value: "echo", label: "Echo" },
    { value: "fable", label: "Fable" },
    { value: "nova", label: "Nova" },
    { value: "onyx", label: "Onyx" },
    { value: "sage", label: "Sage" },
    { value: "shimmer", label: "Shimmer" },
    { value: "verse", label: "Verse" },
    { value: "marin", label: "Marin" },
    { value: "cedar", label: "Cedar" },
];

export const audioFormatOptions = [
    { value: "mp3", label: "MP3" },
    { value: "wav", label: "WAV" },
    { value: "opus", label: "Opus" },
    { value: "aac", label: "AAC" },
    { value: "flac", label: "FLAC" },
    { value: "pcm", label: "PCM" },
];

const miniMaxSpeechInterface = "minimax-speech";

export function normalizeAudioVoiceValue(value: string) {
    return typeof value === "string" ? value.trim() : "";
}

export function normalizeAudioFormatValue(value: string) {
    return audioFormatOptions.some((item) => item.value === value) ? value : "mp3";
}

export function audioFormatOptionsForInterface(interfaceType?: string) {
    if (interfaceType !== miniMaxSpeechInterface) return audioFormatOptions;
    return audioFormatOptions.filter((item) => item.value === "mp3" || item.value === "wav" || item.value === "flac");
}

export function audioSpeedRangeForInterface(interfaceType?: string) {
    return interfaceType === miniMaxSpeechInterface ? { min: 0.5, max: 2 } : { min: 0.25, max: 4 };
}

export function normalizeAudioSpeedValue(value: string, interfaceType?: string) {
    const speed = Number(value);
    if (!Number.isFinite(speed)) return "1";
    const range = audioSpeedRangeForInterface(interfaceType);
    return String(Math.max(range.min, Math.min(range.max, Number(speed.toFixed(2)))));
}

export function audioVoiceLabel(value: string) {
    const voice = normalizeAudioVoiceValue(value);
    return audioVoiceOptions.find((item) => item.value === voice)?.label || voice || "选择音色";
}

export function audioFormatLabel(value: string) {
    const format = normalizeAudioFormatValue(value);
    return audioFormatOptions.find((item) => item.value === format)?.label || format;
}

export function audioSpeedLabel(value: string) {
    return `${normalizeAudioSpeedValue(value)}x`;
}

export function audioMimeType(format: string) {
    if (format === "wav") return "audio/wav";
    if (format === "opus") return "audio/opus";
    if (format === "aac") return "audio/aac";
    if (format === "flac") return "audio/flac";
    if (format === "pcm") return "audio/pcm";
    return "audio/mpeg";
}
