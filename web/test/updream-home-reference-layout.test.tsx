import "./setup-happy-dom";

import { afterEach, describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { act, createElement, useState } from "react";
import { createRoot, type Root } from "react-dom/client";
import { MemoryRouter } from "react-router";

import { UpdreamHeroSkillShortcuts } from "../src/pages/home/updream/updream-hero";
import { UpdreamVideoBackground } from "../src/pages/home/updream/updream-video-background";
import type { PlatformSkill } from "@/services/api/skills";
import type { CanvasAgentSkillSelection } from "@/types/canvas";

let root: Root | null = null;
let idleCallback: IdleRequestCallback | null = null;

const originalMatchMedia = window.matchMedia;
const originalRequestIdleCallback = window.requestIdleCallback;
const originalCancelIdleCallback = window.cancelIdleCallback;

afterEach(async () => {
    if (root) await act(async () => root?.unmount());
    root = null;
    idleCallback = null;
    window.matchMedia = originalMatchMedia;
    window.requestIdleCallback = originalRequestIdleCallback;
    window.cancelIdleCallback = originalCancelIdleCallback;
    document.body.replaceChildren();
});

function configureDeferredMedia(reducedMotion = false) {
    window.matchMedia = () =>
        ({
            matches: reducedMotion,
            media: "(prefers-reduced-motion: reduce)",
            onchange: null,
            addListener: () => undefined,
            removeListener: () => undefined,
            addEventListener: () => undefined,
            removeEventListener: () => undefined,
            dispatchEvent: () => true,
        }) satisfies MediaQueryList;
    window.requestIdleCallback = (callback) => {
        idleCallback = callback;
        return 1;
    };
    window.cancelIdleCallback = () => undefined;
}

async function releaseDeferredMedia() {
    await act(async () => window.dispatchEvent(new Event("load")));
    const callback = idleCallback;
    if (!callback) throw new Error("背景媒体未在 window.load 后安排空闲加载");
    await act(async () => callback({ didTimeout: false, timeRemaining: () => 50 }));
}

function skill(index: number): PlatformSkill {
    return {
        dir: `director-${index}`,
        name: `导演技能 ${index}`,
        description: `技能说明 ${index}`,
        icon: "",
        cover_url: "",
        detail_text: "",
        categories: ["导演"],
        version: 1,
        checksum: `checksum-${index}`,
        status: "published",
        source_kind: "original",
        source_license: "proprietary",
        published_at: "2026-08-22T00:00:00Z",
        uploader_name: "HMaigc",
        liked: false,
        activated: false,
    };
}

describe("Updream homepage reference layout behavior", () => {
    test("deferred homepage sections reserve their final vertical space before observation", () => {
        const stylesheet = readFileSync(resolve(import.meta.dir, "../src/pages/home/updream/updream-home.css"), "utf8");
        expect(stylesheet).toMatch(/\.updream-home-deferred--projects\s*\{[^}]*min-height:\s*268px/s);
        expect(stylesheet).toMatch(/\.updream-home-deferred--skills\s*\{[^}]*min-height:\s*420px/s);
    });

    test("skill shortcuts select a live catalog skill into the current Agent draft", async () => {
        const host = document.createElement("div");
        document.body.append(host);
        root = createRoot(host);

        function Harness() {
            const [selectedSkills, setSelectedSkills] = useState<CanvasAgentSkillSelection[]>([]);
            return createElement(UpdreamHeroSkillShortcuts, {
                skills: [1, 2, 3, 4, 5].map(skill),
                selectedSkills,
                onChange: setSelectedSkills,
            });
        }

        await act(async () => {
            root?.render(createElement(MemoryRouter, null, createElement(Harness)));
        });

        const shortcuts = [...document.querySelectorAll<HTMLButtonElement>(".updream-hero-skill-shortcut")];
        expect(shortcuts).toHaveLength(4);
        expect(shortcuts.map((shortcut) => shortcut.textContent?.trim())).toEqual(["导演技能 1", "导演技能 2", "导演技能 3", "导演技能 4"]);
        expect(shortcuts[0]?.getAttribute("aria-pressed")).toBe("false");

        await act(async () => shortcuts[0]?.click());
        expect(shortcuts[0]?.getAttribute("aria-pressed")).toBe("true");

        await act(async () => shortcuts[0]?.click());
        expect(shortcuts[0]?.getAttribute("aria-pressed")).toBe("false");
    });

    test("skill shortcut loading keeps the reference layout slot stable", async () => {
        const host = document.createElement("div");
        document.body.append(host);
        root = createRoot(host);

        await act(async () => {
            root?.render(createElement(MemoryRouter, null, createElement(UpdreamHeroSkillShortcuts, { skills: [], selectedSkills: [], onChange: () => undefined })));
        });

        expect(document.querySelector(".updream-hero-skill-shortcuts--empty")).not.toBeNull();
        expect(document.querySelectorAll(".updream-hero-skill-shortcut")).toHaveLength(0);
    });

    test("homepage background defers its autoplay-safe source until load and idle", async () => {
        configureDeferredMedia();
        const host = document.createElement("div");
        document.body.append(host);
        root = createRoot(host);

        await act(async () => {
            root?.render(createElement(UpdreamVideoBackground));
        });

        const video = document.querySelector<HTMLVideoElement>(".updream-video-background-media");
        expect(video).not.toBeNull();
        expect(video?.autoplay).toBe(true);
        expect(video?.muted).toBe(true);
        expect(video?.loop).toBe(true);
        expect(video?.hasAttribute("playsinline")).toBe(true);
        expect(video?.preload).toBe("none");
        expect(video?.getAttribute("poster")).toEndWith("/videos/hero-poster.svg");
        expect(document.querySelector(".updream-video-background-source")).toBeNull();

        await releaseDeferredMedia();

        const source = document.querySelector<HTMLSourceElement>(".updream-video-background-source");
        expect(document.querySelector<HTMLVideoElement>(".updream-video-background-media")?.preload).toBe("metadata");
        expect(source?.getAttribute("src")).toEndWith("/videos/hero.mp4");
        expect(document.querySelector(".updream-video-background-scrim")).not.toBeNull();
        expect(document.querySelector(".updream-video-background-pattern")).not.toBeNull();
        expect(document.querySelector(".updream-video-background-glow")).not.toBeNull();
    });

    test("homepage background exposes a real loading error instead of silently hiding it", async () => {
        configureDeferredMedia();
        const host = document.createElement("div");
        document.body.append(host);
        root = createRoot(host);

        await act(async () => {
            root?.render(createElement(UpdreamVideoBackground));
        });
        await releaseDeferredMedia();

        const video = document.querySelector<HTMLVideoElement>(".updream-video-background-media");
        await act(async () => video?.dispatchEvent(new Event("error")));

        expect(document.querySelector("[role='alert']")?.textContent).toContain("首页背景视频加载失败");
    });

    test("homepage background keeps the MP4 unloaded for reduced-motion users", async () => {
        configureDeferredMedia(true);
        const host = document.createElement("div");
        document.body.append(host);
        root = createRoot(host);

        await act(async () => {
            root?.render(createElement(UpdreamVideoBackground));
            window.dispatchEvent(new Event("load"));
        });

        expect(idleCallback).toBeNull();
        expect(document.querySelector(".updream-video-background-source")).toBeNull();
        expect(document.querySelector<HTMLVideoElement>(".updream-video-background-media")?.preload).toBe("none");
    });
});
