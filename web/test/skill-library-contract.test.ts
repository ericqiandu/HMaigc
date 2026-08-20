import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";

const workspaceRoot = resolve(import.meta.dir, "..");

describe("first-party skill library contract", () => {
    test("the skill page never offers a page size above the backend limit", () => {
        const page = readFileSync(resolve(workspaceRoot, "src/pages/skills/index.tsx"), "utf8");

        expect(page).toContain("pageSizeOptions={[20, 40, 60]}");
        expect(page).not.toContain("pageSizeOptions={[20, 40, 80]}");
    });

    test("the web client uses only the first-party catalog endpoints", () => {
        const client = readFileSync(resolve(workspaceRoot, "src/services/api/skills.ts"), "utf8");

        expect(client).toContain("/skills/catalog");
        expect(client).not.toContain("/skills/community");
        expect(client).not.toContain("updream.cn");
    });

    test("skill icon identifiers are rendered as icons instead of user-visible text", () => {
        const page = readFileSync(resolve(workspaceRoot, "src/pages/skills/index.tsx"), "utf8");
        const card = readFileSync(resolve(workspaceRoot, "src/pages/skills/skill-market-card.tsx"), "utf8");

        expect(card).toContain("<SkillIcon icon={skill.icon}");
        expect(`${page}\n${card}`).not.toContain('{skill.icon || "skill"}');
    });
});
