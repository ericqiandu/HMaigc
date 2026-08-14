import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import postcss from "postcss";

const adminWorkspaceStyles = readFileSync(new URL("../src/pages/admin/admin-workspace.css", import.meta.url), "utf8");
const adminArtStyles = readFileSync(new URL("../src/pages/admin/admin-art-layout.css", import.meta.url), "utf8");

type Rgb = readonly [number, number, number];

function customProperty(source: string, selector: string, property: string): string {
    let value = "";
    postcss.parse(source).walkRules((rule) => {
        if (!rule.selectors.includes(selector)) return;
        rule.walkDecls(property, (declaration) => {
            value = declaration.value;
        });
    });
    if (!value) throw new Error(`missing ${property} on ${selector}`);
    return value;
}

function parseHex(value: string): Rgb {
    const match = /^#([\da-f]{2})([\da-f]{2})([\da-f]{2})$/i.exec(value);
    if (!match) throw new Error(`expected six-digit hex color, received ${value}`);
    return [Number.parseInt(match[1], 16), Number.parseInt(match[2], 16), Number.parseInt(match[3], 16)];
}

function resolveColor(value: string, background: Rgb): Rgb {
    if (value.startsWith("#")) return parseHex(value);

    const match = /^rgb\(\s*(\d+)\s+(\d+)\s+(\d+)\s*\/\s*(\d+)%\s*\)$/i.exec(value);
    if (!match) throw new Error(`unsupported CSS color, received ${value}`);
    const alpha = Number.parseInt(match[4], 10) / 100;
    return [Math.round(Number.parseInt(match[1], 10) * alpha + background[0] * (1 - alpha)), Math.round(Number.parseInt(match[2], 10) * alpha + background[1] * (1 - alpha)), Math.round(Number.parseInt(match[3], 10) * alpha + background[2] * (1 - alpha))];
}

function luminance([red, green, blue]: Rgb): number {
    const channels = [red, green, blue].map((channel) => {
        const component = channel / 255;
        return component <= 0.04045 ? component / 12.92 : ((component + 0.055) / 1.055) ** 2.4;
    });
    return 0.2126 * channels[0] + 0.7152 * channels[1] + 0.0722 * channels[2];
}

function contrastRatio(foreground: Rgb, background: Rgb): number {
    const foregroundLuminance = luminance(foreground);
    const backgroundLuminance = luminance(background);
    return (Math.max(foregroundLuminance, backgroundLuminance) + 0.05) / (Math.min(foregroundLuminance, backgroundLuminance) + 0.05);
}

describe("admin light theme contrast", () => {
    test("keeps tertiary text readable on both page and content surfaces", () => {
        const lightWorkspace = '.admin-theme-root[data-admin-theme="light"] .admin-workspace';
        const page = parseHex(customProperty(adminWorkspaceStyles, lightWorkspace, "--workspace-ui-page"));
        const surface = parseHex(customProperty(adminWorkspaceStyles, lightWorkspace, "--workspace-ui-surface"));
        const tertiaryValue = customProperty(adminWorkspaceStyles, lightWorkspace, "--workspace-ui-text-tertiary");

        expect(contrastRatio(resolveColor(tertiaryValue, page), page)).toBeGreaterThanOrEqual(4.5);
        expect(contrastRatio(resolveColor(tertiaryValue, surface), surface)).toBeGreaterThanOrEqual(4.5);
    });

    test("keeps the direct mobile navigation color override readable in light mode", () => {
        const page = parseHex(customProperty(adminArtStyles, ".admin-mobile-navigation-drawer", "--workspace-ui-page"));
        const surface: Rgb = [255, 255, 255];
        const tertiaryValue = customProperty(adminArtStyles, ".admin-mobile-navigation-drawer", "--workspace-ui-text-tertiary");

        expect(contrastRatio(resolveColor(tertiaryValue, page), page)).toBeGreaterThanOrEqual(4.5);
        expect(contrastRatio(resolveColor(tertiaryValue, surface), surface)).toBeGreaterThanOrEqual(4.5);
    });
});
