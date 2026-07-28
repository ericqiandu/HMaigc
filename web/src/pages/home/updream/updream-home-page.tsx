import { WorkspaceFloatingNavigation } from "@/components/layout/workspace-floating-navigation";
import { UpdreamAnnouncementBar } from "@/pages/home/updream/updream-announcement-bar";
import { UpdreamFooter } from "@/pages/home/updream/updream-footer";
import { UpdreamHeader } from "@/pages/home/updream/updream-header";
import { UpdreamHero } from "@/pages/home/updream/updream-hero";
import { UpdreamRecentProjects } from "@/pages/home/updream/updream-recent-projects";
import { UpdreamSkillsSection } from "@/pages/home/updream/updream-skills-section";
import { UpdreamVideoBackground } from "@/pages/home/updream/updream-video-background";

import "@/pages/home/updream/updream-home.css";

export function UpdreamHomePage() {
    return (
        <div className="updream-home-page h-full min-h-0 overflow-y-auto font-sans antialiased">
            <UpdreamVideoBackground />
            <div className="updream-home-content">
                <WorkspaceFloatingNavigation />
                <UpdreamAnnouncementBar />
                <UpdreamHeader />
                <main className="updream-home-main">
                    <UpdreamHero />
                    <UpdreamRecentProjects />
                    <UpdreamSkillsSection />
                </main>
                <UpdreamFooter />
            </div>
        </div>
    );
}
