import "./setup-happy-dom";

import { afterEach, describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";

const skillCardStyles = readFileSync(new URL("../src/pages/home/updream/updream-skills-section.css", import.meta.url), "utf8");
const homeStyles = readFileSync(new URL("../src/pages/home/updream/updream-home.css", import.meta.url), "utf8");
const designTokenStyles = readFileSync(new URL("../src/styles/design-tokens.css", import.meta.url), "utf8");

afterEach(() => {
    document.head.replaceChildren();
    document.body.replaceChildren();
});

describe("homepage skill card visual contract", () => {
    test.each([
        { tone: "0", background: "linear-gradient(128deg, #ca7ac5 0%, #b56bc0 100%)" },
        { tone: "1", background: "linear-gradient(128deg, #6aabea 0%, #3b8bdc 100%)" },
        { tone: "2", background: "linear-gradient(128deg, #edb07d 0%, #f28a3c 100%)" },
        { tone: "3", background: "linear-gradient(128deg, #8ba1f2 0%, #627ee8 100%)" },
        { tone: "4", background: "linear-gradient(128deg, #e4aec2 0%, #dc7894 100%)" },
        { tone: "5", background: "linear-gradient(128deg, #9ed1e4 0%, #69bde5 100%)" },
    ])("renders tone $tone with the approved brighter palette", ({ tone, background }) => {
        const card = mountSkillCard(tone);

        expect(getComputedStyle(card).backgroundImage).toBe(background);
    });

    test("keeps only a subtle readability veil over the brighter palette", () => {
        const card = mountSkillCard("0");

        expect(getComputedStyle(card).getPropertyValue("--skill-card-overlay-alpha")).toBe("6%");
    });

    test("consumes the shared design token instead of owning a private palette", () => {
        const card = mountSkillCard("0");
        const override = document.createElement("style");
        override.textContent = ":root { --home-skill-tone-0: linear-gradient(128deg, #010203 0%, #040506 100%); }";
        document.head.append(override);

        expect(getComputedStyle(card).backgroundImage).toBe("linear-gradient(128deg, #010203 0%, #040506 100%)");
    });

    test("keeps the selected skill shortcut on shared semantic tokens", () => {
        expect(homeStyles).toContain("var(--home-skill-shortcut-selected-border)");
        expect(homeStyles).toContain("var(--home-skill-shortcut-selected-background)");
        expect(homeStyles).toContain("var(--home-skill-shortcut-selected-text)");
    });
});

function mountSkillCard(tone: string): HTMLElement {
    const style = document.createElement("style");
    style.textContent = `${designTokenStyles}\n${skillCardStyles}`;
    document.head.append(style);

    const card = document.createElement("article");
    card.className = "updream-skill-card";
    card.dataset.tone = tone;
    document.body.append(card);
    return card;
}
