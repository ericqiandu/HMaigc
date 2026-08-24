import { lazy, Suspense } from "react";

import { useSiteSettings } from "@/components/site/site-settings-provider";
import type { PlatformSkill } from "@/services/api/skills";

const UpdreamHeroComposer = lazy(() =>
    import("@/pages/home/updream/updream-hero-composer").then((module) => ({ default: module.UpdreamHeroComposer })),
);

export function UpdreamHero({ skills = [] }: { skills?: PlatformSkill[] }) {
    const { settings } = useSiteSettings();

    return (
        <section className="updream-hero">
            <h1 className="updream-hero-title">{settings.homeHeroSlogan}</h1>
            <Suspense fallback={<div className="updream-home-agent-composer updream-home-agent-composer-loading" aria-hidden="true" />}>
                <UpdreamHeroComposer skills={skills} />
            </Suspense>
        </section>
    );
}
