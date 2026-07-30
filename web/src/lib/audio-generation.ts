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

export type AudioSettingKey = "audioFormat" | "audioSpeed" | "audioVolume" | "audioPitch" | "audioEmotion" | "audioLanguageBoost" | "audioSampleRate" | "audioBitrate" | "audioChannel" | "audioInstructions";

export const miniMaxEmotionOptions = [
    { value: "", label: "自动" },
    { value: "happy", label: "开心" },
    { value: "sad", label: "悲伤" },
    { value: "angry", label: "愤怒" },
    { value: "fearful", label: "害怕" },
    { value: "disgusted", label: "厌恶" },
    { value: "surprised", label: "惊讶" },
    { value: "calm", label: "平静" },
    { value: "fluent", label: "流畅" },
    { value: "whisper", label: "耳语" },
] as const;

export const miniMaxLanguageOptions = [
    { value: "auto", label: "自动识别" },
    { value: "Chinese", label: "中文（普通话）" },
    { value: "Chinese,Yue", label: "中文（粤语）" },
    { value: "English", label: "英语" },
    { value: "Japanese", label: "日语" },
    { value: "Korean", label: "韩语" },
    { value: "Spanish", label: "西班牙语" },
    { value: "French", label: "法语" },
    { value: "Portuguese", label: "葡萄牙语" },
    { value: "German", label: "德语" },
    { value: "Russian", label: "俄语" },
    { value: "Arabic", label: "阿拉伯语" },
    { value: "Italian", label: "意大利语" },
    { value: "Turkish", label: "土耳其语" },
    { value: "Dutch", label: "荷兰语" },
    { value: "Ukrainian", label: "乌克兰语" },
    { value: "Vietnamese", label: "越南语" },
    { value: "Indonesian", label: "印尼语" },
    { value: "Thai", label: "泰语" },
    { value: "Polish", label: "波兰语" },
    { value: "Romanian", label: "罗马尼亚语" },
    { value: "Greek", label: "希腊语" },
    { value: "Czech", label: "捷克语" },
    { value: "Finnish", label: "芬兰语" },
    { value: "Hindi", label: "印地语" },
    { value: "Bulgarian", label: "保加利亚语" },
    { value: "Danish", label: "丹麦语" },
    { value: "Hebrew", label: "希伯来语" },
    { value: "Malay", label: "马来语" },
    { value: "Persian", label: "波斯语" },
    { value: "Slovak", label: "斯洛伐克语" },
    { value: "Swedish", label: "瑞典语" },
    { value: "Croatian", label: "克罗地亚语" },
    { value: "Filipino", label: "菲律宾语" },
    { value: "Hungarian", label: "匈牙利语" },
    { value: "Norwegian", label: "挪威语" },
    { value: "Slovenian", label: "斯洛文尼亚语" },
    { value: "Catalan", label: "加泰罗尼亚语" },
    { value: "Nynorsk", label: "新挪威语" },
    { value: "Tamil", label: "泰米尔语" },
    { value: "Afrikaans", label: "南非语" },
] as const;

export const miniMaxSampleRateOptions = [
    { value: "8000", label: "8 kHz" },
    { value: "16000", label: "16 kHz" },
    { value: "22050", label: "22.05 kHz" },
    { value: "24000", label: "24 kHz" },
    { value: "32000", label: "32 kHz" },
    { value: "44100", label: "44.1 kHz" },
] as const;

export const miniMaxBitrateOptions = [
    { value: "32000", label: "32 kbps" },
    { value: "64000", label: "64 kbps" },
    { value: "128000", label: "128 kbps" },
    { value: "256000", label: "256 kbps" },
] as const;

export const miniMaxChannelOptions = [
    { value: "1", label: "单声道" },
    { value: "2", label: "双声道" },
] as const;

export const miniMaxVocalTags = [
    { value: "(laughs)", label: "笑声" },
    { value: "(chuckle)", label: "轻笑" },
    { value: "(coughs)", label: "咳嗽" },
    { value: "(clear-throat)", label: "清嗓" },
    { value: "(groans)", label: "叹吟" },
    { value: "(breath)", label: "呼吸" },
    { value: "(pant)", label: "喘气" },
    { value: "(inhale)", label: "吸气" },
    { value: "(exhale)", label: "呼气" },
    { value: "(gasps)", label: "倒吸气" },
    { value: "(sniffs)", label: "抽鼻" },
    { value: "(sighs)", label: "叹气" },
    { value: "(snorts)", label: "哼气" },
    { value: "(burps)", label: "打嗝" },
    { value: "(lip-smacking)", label: "咂嘴" },
    { value: "(humming)", label: "哼唱" },
    { value: "(hissing)", label: "嘶声" },
    { value: "(emm)", label: "嗯声" },
    { value: "(sneezes)", label: "喷嚏" },
] as const;

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

export function normalizeMiniMaxVolumeValue(value: string) {
    return normalizeNumberValue(value, 1, 0.1, 10, 2);
}

export function normalizeMiniMaxPitchValue(value: string) {
    return normalizeNumberValue(value, 0, -12, 12, 0);
}

export function miniMaxEmotionOptionsForModel(model: string) {
    return model.startsWith("speech-2.8-") ? miniMaxEmotionOptions.filter((item) => item.value !== "whisper") : [...miniMaxEmotionOptions];
}

export function audioLanguageLabel(value: string) {
    return miniMaxLanguageOptions.find((item) => item.value === value)?.label || value || "未标注";
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

function normalizeNumberValue(value: string, fallback: number, minimum: number, maximum: number, digits: number) {
    const numeric = Number(value);
    if (!Number.isFinite(numeric)) return String(fallback);
    return String(Math.max(minimum, Math.min(maximum, Number(numeric.toFixed(digits)))));
}
