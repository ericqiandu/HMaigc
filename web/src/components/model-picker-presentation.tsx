import { ModelBrandIcon } from "@/components/model-brand-icon";
import { staticAssetURL } from "@/lib/static-assets";
import { modelOptionName, resolveModelChannel, type AiConfig } from "@/stores/use-config-store";

export function ModelIcon({ model, config }: { model: string; config?: AiConfig }) {
    const brandKey = config ? (modelCatalogEntry(config, model)?.brandKey ?? "generic") : "generic";
    return <ModelBrandIcon brandKey={brandKey} className="canvas-model-picker-icon size-3.5 opacity-90" />;
}

export function MemberDiamond() {
    return (
        <span className="canvas-model-picker-member-diamond inline-flex size-4 shrink-0 items-center justify-center" role="img" aria-label="会员专属模型" title="会员专属模型">
            <img className="canvas-model-picker-member-diamond-image size-3.5 object-contain" src={staticAssetURL("/icons/member-diamond.svg")} alt="" aria-hidden="true" />
        </span>
    );
}

export function modelCatalogEntry(config: AiConfig, model: string) {
    const channel = resolveModelChannel(config, model);
    return channel.modelCosts?.find((item) => item.model === modelOptionName(model));
}

export function isMemberModel(config: AiConfig, model: string) {
    return modelCatalogEntry(config, model)?.accessPolicy === "member";
}
