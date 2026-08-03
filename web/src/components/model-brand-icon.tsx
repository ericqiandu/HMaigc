import { Cpu } from "lucide-react";

import { staticAssetURL } from "@/lib/static-assets";
import { modelBrandDefinition, type ModelBrandKey } from "@/lib/model-brands";
import { cn } from "@/lib/utils";

type ModelBrandIconProps = {
    brandKey: ModelBrandKey;
    className?: string;
};

export function ModelBrandIcon({ brandKey, className }: ModelBrandIconProps) {
    const brand = modelBrandDefinition(brandKey);
    if (brand.asset) {
        return <img className={cn("model-brand-icon block shrink-0 object-contain brightness-0 dark:invert", className)} src={staticAssetURL(brand.asset)} alt="" aria-hidden="true" />;
    }
    if (brandKey === "generic") {
        return <Cpu className={cn("model-brand-icon shrink-0 text-black dark:text-white", className)} aria-hidden="true" />;
    }
    return (
        <span className={cn("model-brand-icon inline-flex shrink-0 items-center justify-center font-semibold leading-none text-black dark:text-white", className)} aria-hidden="true">
            {brand.mark}
        </span>
    );
}
