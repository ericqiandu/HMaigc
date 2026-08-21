import { lazy, Suspense, useRef, useState, type UIEventHandler } from "react";

import { WorkspaceFloatingNavigation } from "@/components/layout/workspace-floating-navigation";
import { DeferredSection } from "@/components/ui/deferred-section";
import { UpdreamAnnouncementBar } from "@/pages/home/updream/updream-announcement-bar";
import { UpdreamFooter } from "@/pages/home/updream/updream-footer";
import { UpdreamHeader } from "@/pages/home/updream/updream-header";
import { UpdreamHero } from "@/pages/home/updream/updream-hero";
import { UpdreamVideoBackground } from "@/pages/home/updream/updream-video-background";

import "@/pages/home/updream/updream-home.css";

const UpdreamRecentProjects = lazy(() => import("@/pages/home/updream/updream-recent-projects").then((module) => ({ default: module.UpdreamRecentProjects })));
const UpdreamSkillsSection = lazy(() => import("@/pages/home/updream/updream-skills-section").then((module) => ({ default: module.UpdreamSkillsSection })));

export function UpdreamHomePage() {
    const [isHeaderElevated, setIsHeaderElevated] = useState(false);
    const isHeaderElevatedRef = useRef(false);

    const handlePageScroll: UIEventHandler<HTMLDivElement> = (event) => {
        const nextIsElevated = event.currentTarget.scrollTop > 8;

        if (nextIsElevated === isHeaderElevatedRef.current) return;

        isHeaderElevatedRef.current = nextIsElevated;
        setIsHeaderElevated(nextIsElevated);
    };

    return (
        <div className="updream-home-page h-full min-h-0 overflow-y-auto font-sans antialiased" onScroll={handlePageScroll}>
            <UpdreamVideoBackground />
            <div className="updream-home-content">
                <WorkspaceFloatingNavigation />
                <div className={`updream-sticky-stack${isHeaderElevated ? " updream-sticky-stack--elevated" : ""}`}>
                    <UpdreamAnnouncementBar />
                    <UpdreamHeader />
                </div>
                <main className="updream-home-main">
                    <UpdreamHero />
                    <DeferredSection className="updream-home-deferred updream-home-deferred--projects min-h-[360px]">
                        <Suspense fallback={<div className="updream-home-deferred-placeholder min-h-[360px]" aria-hidden="true" />}>
                            <UpdreamRecentProjects />
                        </Suspense>
                    </DeferredSection>
                    <DeferredSection className="updream-home-deferred updream-home-deferred--skills min-h-[420px]">
                        <Suspense fallback={<div className="updream-home-deferred-placeholder min-h-[420px]" aria-hidden="true" />}>
                            <UpdreamSkillsSection />
                        </Suspense>
                    </DeferredSection>
                </main>
                <UpdreamFooter />
            </div>
        </div>
    );
}
