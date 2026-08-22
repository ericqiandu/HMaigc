import { lazy, Suspense, useRef, useState, type UIEventHandler } from "react";
import { useQuery } from "@tanstack/react-query";

import { WorkspaceFloatingNavigation } from "@/components/layout/workspace-floating-navigation";
import { DeferredSection } from "@/components/ui/deferred-section";
import { UpdreamAnnouncementBar } from "@/pages/home/updream/updream-announcement-bar";
import { UpdreamFooter } from "@/pages/home/updream/updream-footer";
import { UpdreamHeader } from "@/pages/home/updream/updream-header";
import { UpdreamHero } from "@/pages/home/updream/updream-hero";
import { UpdreamVideoBackground } from "@/pages/home/updream/updream-video-background";
import { listSkillsCatalog } from "@/services/api/skills";

import "@/pages/home/updream/updream-home.css";

const UpdreamRecentProjects = lazy(() => import("@/pages/home/updream/updream-recent-projects").then((module) => ({ default: module.UpdreamRecentProjects })));
const UpdreamSkillsSection = lazy(() => import("@/pages/home/updream/updream-skills-section").then((module) => ({ default: module.UpdreamSkillsSection })));

export function UpdreamHomePage() {
    const [isHeaderElevated, setIsHeaderElevated] = useState(false);
    const isHeaderElevatedRef = useRef(false);
    const skillsQuery = useQuery({
        queryKey: ["skills", "homepage", 6],
        queryFn: () => listSkillsCatalog({ page: 1, page_size: 6 }),
        staleTime: 60_000,
    });
    const skills = skillsQuery.data?.skills ?? [];

    const handlePageScroll: UIEventHandler<HTMLDivElement> = (event) => {
        const nextIsElevated = event.currentTarget.scrollTop > 8;

        if (nextIsElevated === isHeaderElevatedRef.current) return;

        isHeaderElevatedRef.current = nextIsElevated;
        setIsHeaderElevated(nextIsElevated);
    };

    return (
        <div className="updream-home-page" onScroll={handlePageScroll}>
            <UpdreamVideoBackground />
            <div className="updream-home-content">
                <WorkspaceFloatingNavigation />
                <div className={`updream-sticky-stack${isHeaderElevated ? " updream-sticky-stack--elevated" : ""}`}>
                    <UpdreamAnnouncementBar />
                    <UpdreamHeader />
                </div>
                <main className="updream-home-main">
                    <UpdreamHero skills={skills} />
                    <DeferredSection className="updream-home-deferred updream-home-deferred--projects">
                        <Suspense fallback={<div className="updream-home-deferred-placeholder updream-home-deferred-placeholder--projects" aria-hidden="true" />}>
                            <UpdreamRecentProjects />
                        </Suspense>
                    </DeferredSection>
                    <DeferredSection className="updream-home-deferred updream-home-deferred--skills">
                        <Suspense fallback={<div className="updream-home-deferred-placeholder updream-home-deferred-placeholder--skills" aria-hidden="true" />}>
                            <UpdreamSkillsSection
                                skills={skills}
                                loading={skillsQuery.isLoading}
                                error={skillsQuery.isError ? (skillsQuery.error instanceof Error ? skillsQuery.error.message : "技能目录加载失败") : ""}
                                onRetry={() => void skillsQuery.refetch()}
                            />
                        </Suspense>
                    </DeferredSection>
                </main>
                <UpdreamFooter />
            </div>
        </div>
    );
}
