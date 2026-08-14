import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import postcss from "postcss";

import { createAdminAntTheme } from "../src/pages/admin/admin-theme";

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

function declaration(source: string, selector: string, property: string): { important: boolean; value: string } {
    let result: { important: boolean; value: string } | null = null;
    postcss.parse(source).walkRules((rule) => {
        if (!rule.selectors.includes(selector)) return;
        rule.walkDecls(property, (item) => {
            result = { important: item.important, value: item.value };
        });
    });
    if (!result) throw new Error(`missing ${property} on ${selector}`);
    return result;
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

    test("keeps neutral admin controls visibly separated from light content surfaces", () => {
        const lightWorkspace = '.admin-theme-root[data-admin-theme="light"] .admin-workspace';
        const surface = parseHex(customProperty(adminWorkspaceStyles, lightWorkspace, "--workspace-ui-surface"));
        const controlBorder = parseHex(customProperty(adminWorkspaceStyles, lightWorkspace, "--workspace-ui-control-border"));

        expect(contrastRatio(controlBorder, surface)).toBeGreaterThanOrEqual(1.5);
    });

    test("keeps light theme placeholders and success labels readable", () => {
        const theme = createAdminAntTheme("light", "#2979C9");
        const placeholder = parseHex(String(theme.token?.colorTextPlaceholder));
        const success = parseHex(String(theme.token?.colorSuccess));
        const lightWorkspace = '.admin-theme-root[data-admin-theme="light"] .admin-workspace';
        const successSurface = parseHex(customProperty(adminWorkspaceStyles, lightWorkspace, "--workspace-ui-success-surface"));

        expect(contrastRatio(placeholder, [255, 255, 255])).toBeGreaterThanOrEqual(4.5);
        expect(contrastRatio(success, successSurface)).toBeGreaterThanOrEqual(4.5);
    });

    test("keeps the semantic success surface above Ant Design generated tag styles", () => {
        const successSurface = declaration(adminWorkspaceStyles, ".admin-workspace .ant-tag-success", "background-color");

        expect(successSurface).toEqual({ important: true, value: "var(--workspace-ui-success-surface)" });
    });

    test("keeps the admin control border above legacy transparent toolbar overrides", () => {
        const inputBoundary = declaration(adminWorkspaceStyles, ".admin-workspace :where(.ant-input-affix-wrapper)", "border-color");
        const selectBoundary = declaration(adminWorkspaceStyles, ".admin-workspace :where(.ant-select-single)", "border-color");

        expect(inputBoundary).toEqual({ important: true, value: "var(--workspace-ui-control-border)" });
        expect(selectBoundary).toEqual({ important: true, value: "var(--workspace-ui-control-border)" });
    });

    test("keeps the direct mobile navigation color override readable in light mode", () => {
        const page = parseHex(customProperty(adminArtStyles, ".admin-mobile-navigation-drawer", "--workspace-ui-page"));
        const surface: Rgb = [255, 255, 255];
        const tertiaryValue = customProperty(adminArtStyles, ".admin-mobile-navigation-drawer", "--workspace-ui-text-tertiary");

        expect(contrastRatio(resolveColor(tertiaryValue, page), page)).toBeGreaterThanOrEqual(4.5);
        expect(contrastRatio(resolveColor(tertiaryValue, surface), surface)).toBeGreaterThanOrEqual(4.5);
    });

    test("keeps workspace-scoped admin portal content on the light token set", () => {
        const lightPortalScope = '.admin-theme-root[data-admin-theme="light"] .workspace-ui-scope';

        expect(customProperty(adminWorkspaceStyles, lightPortalScope, "--workspace-ui-text")).toBe("#1f2329");
        expect(customProperty(adminWorkspaceStyles, lightPortalScope, "--workspace-ui-surface")).toBe("#ffffff");
        expect(customProperty(adminWorkspaceStyles, lightPortalScope, "--workspace-ui-text-secondary")).toBe("#646a73");
    });
});
