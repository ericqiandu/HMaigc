import { useEffect, useId, useMemo, useState } from "react";
import { Coins, Cpu } from "lucide-react";
import { Select } from "antd";

import { cn } from "@/lib/utils";
import { catalogModelsByCapability, isModelAccessible, modelDisplayName, modelOptionName, resolveModelChannel, type AiConfig, type ModelCapability } from "@/stores/use-config-store";

type ModelPickerProps = {
    config: AiConfig;
    value?: string;
    onChange: (model: string) => void;
    capability?: ModelCapability;
    className?: string;
    fullWidth?: boolean;
    placeholder?: string;
    onMissingConfig?: () => void;
    showSelectedPrice?: boolean;
    presentation?: "default" | "canvasImage" | "canvasAudio";
};

export function ModelPicker({ config, value, onChange, capability, className, fullWidth = false, placeholder = "选择模型", onMissingConfig, showSelectedPrice = true, presentation = "default" }: ModelPickerProps) {
    const pickerId = useId();
    const [open, setOpen] = useState(false);
    const options = useMemo(
        () => Array.from(new Set([...(config.channelMode === "local" && !capability ? [value] : []), ...catalogModelsByCapability(config, capability)].filter((model): model is string => Boolean(model)))),
        [capability, config, value],
    );
    const optionGroups = useMemo(() => {
        const channelGroups = config.channels
            .map((channel) => ({
                key: channel.id,
                label: channel.name || "未命名渠道",
                scope: "系统渠道",
                models: options.filter((model) => resolveModelChannel(config, model).id === channel.id),
            }))
            .filter((group) => group.models.length);
        const groupedModels = new Set(channelGroups.flatMap((group) => group.models));
        const ungroupedModels = options.filter((model) => !groupedModels.has(model));
        return ungroupedModels.length ? [...channelGroups, { key: "ungrouped", label: "其他模型", scope: "未指定渠道", models: ungroupedModels }] : channelGroups;
    }, [config, options]);
    const current = value || "";
    const currentPrice = modelMenuPrice(config, current);
    const canvasMediaPresentation = presentation === "canvasImage" || presentation === "canvasAudio";
    const selectOptions = useMemo(
        () => canvasMediaPresentation
            ? options.map((model) => ({ value: model, label: modelDisplayName(config, model), disabled: !isModelAccessible(config, model) }))
            : optionGroups.map((group) => ({
                label: (
                    <span className="canvas-model-picker-group flex min-w-0 items-center gap-1.5">
                        <span className="canvas-model-picker-group-name truncate">{group.label}</span>
                        <span className="canvas-model-picker-group-scope shrink-0 text-[10px] font-normal text-foreground/38">{group.scope}</span>
                    </span>
                ),
                options: group.models.map((model) => ({ value: model, label: modelDisplayName(config, model), disabled: !isModelAccessible(config, model) })),
            })),
        [canvasMediaPresentation, config, optionGroups, options],
    );

    useEffect(() => {
        const closeOtherPicker = (event: Event) => {
            if ((event as CustomEvent<string>).detail !== pickerId) setOpen(false);
        };
        window.addEventListener("model-picker-open", closeOtherPicker);
        return () => window.removeEventListener("model-picker-open", closeOtherPicker);
    }, [pickerId]);

    return (
        <div className={cn("model-picker-root", fullWidth ? "w-full min-w-0" : "w-fit max-w-full", className)} onMouseDown={(event) => event.stopPropagation()} onPointerDown={(event) => event.stopPropagation()}>
            <Select<string>
                size="small"
                open={open}
                value={current || undefined}
                placeholder={
                    canvasMediaPresentation ? (
                        <span className="canvas-model-picker-placeholder flex min-w-0 items-center gap-1.5">
                            <ModelIcon config={config} model="" />
                            <span className="canvas-model-picker-placeholder-text truncate">{placeholder}</span>
                        </span>
                    ) : placeholder
                }
                options={selectOptions}
                showSearch
                filterOption={(input, option) =>
                    String(option?.label || "")
                        .toLocaleLowerCase()
                        .includes(input.toLocaleLowerCase())
                }
                notFoundContent={<span className="canvas-model-picker-empty block px-2 py-3 text-center text-xs text-foreground/48">{emptyModelLabel(config, capability)}</span>}
                popupMatchSelectWidth={presentation === "canvasImage" ? 370 : presentation === "canvasAudio" ? 360 : capability === "image" || capability === "video" ? 320 : 280}
                placement={canvasMediaPresentation ? "topLeft" : "bottomLeft"}
                className={cn("canvas-composer-model-picker", fullWidth ? "w-full" : "min-w-36 max-w-full")}
                classNames={{
                    popup: {
                        root: cn("canvas-model-picker-popup", presentation === "canvasImage" && "canvas-image-model-picker-popup", presentation === "canvasAudio" && "canvas-audio-model-picker-popup"),
                    },
                }}
                onOpenChange={(nextOpen) => {
                    if (nextOpen && !options.length && config.channelMode === "local") onMissingConfig?.();
                    if (nextOpen) window.dispatchEvent(new CustomEvent("model-picker-open", { detail: pickerId }));
                    setOpen(nextOpen);
                }}
                onChange={(nextModel) => {
                    if (isModelAccessible(config, nextModel)) onChange(nextModel);
                }}
                optionRender={(option) => <ModelLabel config={config} model={String(option.value)} presentation={presentation} selected={String(option.value) === current} />}
                labelRender={() => (
                    <span className="canvas-model-picker-label flex min-w-0 items-center gap-1.5 text-[11px]">
                        <ModelIcon config={config} model={current} />
                        <span className="canvas-model-picker-label-copy flex min-w-0 flex-1 items-center gap-1">
                            <span className="canvas-model-picker-label-text min-w-0 truncate">{current ? modelDisplayName(config, current) : placeholder}</span>
                            {isMemberModel(config, current) ? <MemberDiamond /> : null}
                        </span>
                        {showSelectedPrice ? <ModelPrice price={currentPrice} compact /> : null}
                    </span>
                )}
                aria-label={placeholder}
                title={current ? modelDisplayName(config, current) : placeholder}
            />
        </div>
    );
}

function emptyModelLabel(config: AiConfig, capability?: ModelCapability) {
    const label = capability === "image" ? "生图" : capability === "video" ? "视频" : capability === "text" ? "文本" : capability === "audio" ? "音频" : "";
    if (capability && config.models.length) return `当前渠道没有匹配的${label}模型`;
    return config.models.length ? `暂无匹配的${label}模型` : "系统暂无可用模型，请联系管理员配置";
}

function ModelLabel({ config, model, presentation, selected }: { config: AiConfig; model: string; presentation: ModelPickerProps["presentation"]; selected: boolean }) {
    const [hovered, setHovered] = useState(false);
    const canvasImage = presentation === "canvasImage";
    const canvasAudio = presentation === "canvasAudio";
    const canvasMedia = canvasImage || canvasAudio;
    const presentationConfig = modelCatalogEntry(config, model);
    const marketingCopy = canvasMedia ? presentationConfig?.marketingCopy?.trim() : "";
    const modelMeta = marketingCopy || (canvasAudio ? "未配置模型说明" : "");
    const showMarketingCopy = Boolean(modelMeta && (canvasAudio || selected || hovered));
    return (
        <span
            className={cn("canvas-model-picker-option flex min-w-0 items-center", canvasMedia ? "gap-2.5" : "gap-1.5 py-0", selected && "canvas-model-picker-option--selected")}
            onMouseEnter={() => setHovered(true)}
            onMouseLeave={() => setHovered(false)}
        >
            <span className={cn("canvas-model-picker-option-icon grid shrink-0 place-items-center bg-foreground/[.07]", canvasMedia ? "size-9 rounded-lg" : "size-6 rounded-md")}>
                <ModelIcon config={config} model={model} />
            </span>
            <span className="canvas-model-picker-option-body flex min-w-0 flex-1 flex-col justify-center">
                <span className={cn("canvas-model-picker-option-title flex min-w-0 items-center gap-1 font-semibold", canvasMedia ? "text-[14px] leading-5" : "text-[11px] leading-none")}>
                    <span className="canvas-model-picker-option-title-text truncate">{modelDisplayName(config, model)}</span>
                    {isMemberModel(config, model) ? <MemberDiamond /> : null}
                    {presentationConfig?.promotionBadge ? (
                        <span className="canvas-model-picker-promotion-badge inline-flex h-[18px] max-w-20 shrink-0 items-center rounded-full bg-[#ffbf3f] px-1.5 text-[9px] font-semibold leading-none text-[#493000]">
                            <span className="canvas-model-picker-promotion-badge-text truncate">{presentationConfig.promotionBadge}</span>
                        </span>
                    ) : null}
                </span>
                {showMarketingCopy ? (
                    <span className="canvas-model-picker-option-meta mt-0.5 block w-full truncate text-[11px] font-normal leading-4">
                        {modelMeta}
                    </span>
                ) : null}
            </span>
            <ModelPrice price={modelMenuPrice(config, model)} presentation={presentation} />
        </span>
    );
}

function modelMenuPrice(config: AiConfig, model: string): { value: number; unit: "次" | "秒" } | null | undefined {
    if (!model) return undefined;
    const channel = resolveModelChannel(config, model);
    const cost = channel.modelCosts?.find((item) => item.model === modelOptionName(model));
    if (!cost) return channel.scope === "system" ? null : undefined;
    return { value: cost.unitPriceMicrocredits / 1_000_000, unit: cost.billingMode === "per_second" ? "秒" : "次" };
}

function ModelPrice({ price, compact = false, presentation = "default" }: { price: { value: number; unit: "次" | "秒" } | null | undefined; compact?: boolean; presentation?: ModelPickerProps["presentation"] }) {
    if (price === undefined) return null;
    const canvasMedia = presentation === "canvasImage" || presentation === "canvasAudio";
    if (price === null) return compact ? null : <span className={cn("canvas-model-picker-price shrink-0 text-[10px] text-foreground/40", canvasMedia && "rounded-full bg-foreground/[.06] px-2 py-1")}>未配置</span>;
    return (
        <span
            className={cn(
                "canvas-model-picker-price inline-flex shrink-0 items-center gap-0.5 text-[10px] font-medium tabular-nums text-foreground/55",
                canvasMedia ? "rounded-full bg-foreground/[.06] px-2 py-1" : "rounded border border-foreground/10 bg-foreground/[.045] px-1.5 py-0.5",
            )}
            title={`每${price.unit}消耗 ${price.value.toLocaleString("zh-CN", { maximumFractionDigits: 6 })} 积分`}
        >
            {canvasMedia ? null : <Coins className="size-3" />}
            {price.value.toLocaleString("zh-CN", { maximumFractionDigits: compact ? 3 : 6 })}/{price.unit}
        </span>
    );
}

export function ModelIcon({ model, config }: { model: string; config?: AiConfig }) {
    const configuredIcon = config ? modelCatalogEntry(config, model)?.iconUrl?.trim() : "";
    if (configuredIcon) {
        return <img src={configuredIcon} alt="" className="canvas-model-picker-icon canvas-model-picker-icon--configured size-3.5 shrink-0 object-contain" />;
    }
    const icon = resolveModelIcon(modelOptionName(model));
    return icon ? <img src={icon} alt="" className="canvas-model-picker-icon size-3.5 shrink-0 dark:invert" /> : <Cpu className="canvas-model-picker-icon size-3.5 shrink-0 opacity-70" />;
}

function modelCatalogEntry(config: AiConfig, model: string) {
    const channel = resolveModelChannel(config, model);
    return channel.modelCosts?.find((item) => item.model === modelOptionName(model));
}

function isMemberModel(config: AiConfig, model: string) {
    return modelCatalogEntry(config, model)?.accessPolicy === "member";
}

function MemberDiamond() {
    return (
        <span className="canvas-model-picker-member-diamond inline-flex size-4 shrink-0 items-center justify-center" role="img" aria-label="会员专属模型" title="会员专属模型">
            <img className="canvas-model-picker-member-diamond-image size-3.5 object-contain" src="/icons/member-diamond.svg" alt="" aria-hidden="true" />
        </span>
    );
}

function resolveModelIcon(model: string) {
    const name = model.toLowerCase();
    if (name.includes("claude") || name.includes("anthropic")) return "/icons/claude.svg";
    if (name.includes("gemini") || name.includes("google")) return "/icons/gemini.svg";
    if (name.includes("gpt") || name.includes("openai")) return "/icons/openai.svg";
    if (name.includes("grok") || name.includes("grok")) return "/icons/grok.svg";
    if (name.includes("deepseek") || name.includes("deepseek")) return "/icons/deepseek.svg";
    if (name.includes("glm") || name.includes("glm")) return "/icons/glm.svg";
    return "";
}
