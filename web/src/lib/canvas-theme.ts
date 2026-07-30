export type CanvasColorTheme = "light" | "dark";
export type CanvasBackgroundMode = "dots" | "lines" | "blank";

export const canvasThemes = {
    light: {
        canvas: {
            background: "#f4f5f7",
            dot: "rgba(31,41,55,.22)",
            line: "rgba(31,41,55,.11)",
            selectionFill: "rgba(79,110,232,.12)",
        },
        node: {
            label: "#374151",
            fill: "#ffffff",
            panel: "#ffffff",
            stroke: "#d3d8e0",
            activeStroke: "#111827",
            placeholder: "#687587",
            text: "#111827",
            muted: "#596273",
            faint: "#8a94a3",
        },
        frame: {
            fill: "rgba(17,24,39,.035)",
            stroke: "rgba(17,24,39,.26)",
            activeFill: "rgba(79,110,232,.07)",
            activeStroke: "#4f6ee8",
            preview: "rgba(255,255,255,.94)",
        },
        toolbar: {
            panel: "rgba(255,255,255,.97)",
            border: "rgba(17,24,39,.14)",
            item: "#374151",
            itemHover: "rgba(17,24,39,.075)",
            activeBg: "rgba(17,24,39,.115)",
            activeText: "#111827",
        },
        spatial: {
            surface: "rgba(255,255,255,.84)",
            elevated: "rgba(255,255,255,.98)",
            dropzone: "rgba(248,250,252,.94)",
            glow: "rgba(79,110,232,.20)",
            glowStrong: "rgba(79,110,232,.52)",
            shadow: "rgba(15,23,42,.14)",
        },
        accent: {
            primary: "#4f6ee8",
            primarySoft: "rgba(79,110,232,.14)",
            danger: "#f87171",
        },
    },
    dark: {
        canvas: {
            background: "#111111",
            dot: "rgba(245,245,245,.16)",
            line: "rgba(245,245,245,.065)",
            selectionFill: "rgba(91,110,225,.16)",
        },
        node: {
            label: "#a3a3a3",
            fill: "#242424",
            panel: "#202020",
            stroke: "rgba(255,255,255,.13)",
            activeStroke: "#f5f5f5",
            placeholder: "#8b8b8b",
            text: "#f5f5f5",
            muted: "#a3a3a3",
            faint: "#7b7b7b",
        },
        frame: {
            fill: "rgba(255,255,255,.025)",
            stroke: "rgba(255,255,255,.18)",
            activeFill: "rgba(91,110,225,.08)",
            activeStroke: "#8290f0",
            preview: "rgba(24,24,24,.86)",
        },
        toolbar: {
            panel: "rgba(36,36,36,.94)",
            border: "rgba(255,255,255,.16)",
            item: "#d4d4d4",
            itemHover: "rgba(255,255,255,.08)",
            activeBg: "rgba(255,255,255,.13)",
            activeText: "#ffffff",
        },
        spatial: {
            surface: "rgba(30,30,32,.72)",
            elevated: "rgba(22,22,24,.94)",
            dropzone: "rgba(10,10,12,.78)",
            glow: "rgba(130,144,240,.2)",
            glowStrong: "rgba(130,144,240,.58)",
            shadow: "rgba(0,0,0,.46)",
        },
        accent: {
            primary: "#8290f0",
            primarySoft: "rgba(91,110,225,.2)",
            danger: "#fb7185",
        },
    },
} as const;

export type CanvasTheme = (typeof canvasThemes)[CanvasColorTheme];
