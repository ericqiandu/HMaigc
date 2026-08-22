import { describe, expect, test } from "bun:test";

const pageSource = await Bun.file(new URL("../src/pages/home/updream/updream-home-page.tsx", import.meta.url)).text();
const headerSource = await Bun.file(new URL("../src/pages/home/updream/updream-header.tsx", import.meta.url)).text();
const projectsSource = await Bun.file(new URL("../src/pages/home/updream/updream-recent-projects.tsx", import.meta.url)).text();
const skillsSource = await Bun.file(new URL("../src/pages/home/updream/updream-skills-section.tsx", import.meta.url)).text();
const styles = await Bun.file(new URL("../src/pages/home/updream/updream-home.css", import.meta.url)).text();

describe("reference-led homepage layout", () => {
    test("keeps the real product owners and complete navigation", () => {
        expect(pageSource).toContain("<WorkspaceFloatingNavigation />");
        expect(pageSource).toContain("<UpdreamHero />");
        expect(headerSource).toContain("<SiteBrandLink />");
        expect(headerSource).toContain("<SiteAccountActions />");
    });

    test("matches the reference Hero geometry", () => {
        expect(styles).toMatch(/\.updream-hero\s*\{[^}]*min-height:\s*max\(632px, 100svh\)[^}]*padding-top:\s*315px/s);
        expect(styles).toContain("margin-top: 49.6px");
        expect(styles).toContain("font-size: 32px");
        expect(styles).toContain("line-height: 38.4px");
    });

    test("keeps dynamic project and skill sources", () => {
        expect(projectsSource).toContain("queryFn: listProjects");
        expect(skillsSource).toContain("listSkillsCatalog({ page: 1, page_size: 6 })");
    });

    test("matches the reference desktop content rails and project tiles", () => {
        expect(styles).toMatch(/@media \(min-width: 1025px\)[\s\S]*?max-width:\s*1448px !important[\s\S]*?padding-right:\s*104px !important[\s\S]*?padding-left:\s*104px !important/s);
        expect(projectsSource).toContain("grid-cols-2 gap-4 sm:grid-cols-3 lg:grid-cols-5");
        expect(projectsSource).toContain("group h-56 rounded-[16px]");
        expect(projectsSource).toContain("hover:scale-[1.02]");
    });

    test("matches the reference three-column gradient skill cards", () => {
        expect(skillsSource).toContain("CARD_GRADIENTS");
        expect(skillsSource).toContain("style={{ background: CARD_GRADIENTS[index % CARD_GRADIENTS.length] }}");
        expect(skillsSource).toContain("HOME_SKILL_COVERS");
        expect(skillsSource).toContain("gap-4 md:grid-cols-2 xl:grid-cols-3");
        expect(skillsSource).toContain("rounded-[16px]");
        expect(skillsSource).toContain("h-[171px] w-32");
        expect(skillsSource).toContain("h-11 w-11");
        expect(skillsSource).toContain('<Zap className="updream-skill-mark-icon size-5" />');
        expect(skillsSource).toContain("text-base font-semibold leading-6");
        expect(skillsSource).toContain("官方精选技能");
        expect(styles).toMatch(/@media \(min-width: 1025px\)[\s\S]*?\.updream-skill-card\s*\{[^}]*height:\s*180px !important/s);
    });
});
