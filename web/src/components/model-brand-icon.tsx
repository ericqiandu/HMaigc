import type { CSSProperties } from "react";

import { Cpu } from "lucide-react";

import { staticAssetURL } from "@/lib/static-assets";
import { modelBrandDefinition, type ModelBrandKey } from "@/lib/model-brands";
import { cn } from "@/lib/utils";

type ModelBrandIconProps = {
    brandKey: ModelBrandKey;
    className?: string;
};

type ModelBrandIconStyle = CSSProperties & {
    "--model-brand-icon-source"?: string;
};

export function ModelBrandIcon({ brandKey, className }: ModelBrandIconProps) {
    const brand = modelBrandDefinition(brandKey);
    if (brand.asset) {
        const style: ModelBrandIconStyle = {
            "--model-brand-icon-source": `url("${staticAssetURL(brand.asset)}")`,
        };
        return <span className={cn("model-brand-icon model-brand-icon--asset block shrink-0", className)} style={style} aria-hidden="true" />;
    }
    if (brandKey === "generic") {
        return <Cpu className={cn("model-brand-icon shrink-0 text-current", className)} aria-hidden="true" />;
    }
    return (
        <span className={cn("model-brand-icon model-brand-icon--fallback inline-flex shrink-0 items-center justify-center font-semibold leading-none text-current", className)} aria-hidden="true">
            {brand.mark}
        </span>
    );
}
