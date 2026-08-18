import { dirname, resolve } from "node:path";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

const webDir = dirname(fileURLToPath(import.meta.url));
const appVersion = readFileSync(resolve(webDir, "../VERSION"), "utf8").trim();
const staticAssetBaseURL = (process.env.VITE_STATIC_ASSET_BASE_URL || "").trim();

if (staticAssetBaseURL && !staticAssetBaseURL.startsWith("https://")) {
    throw new Error("VITE_STATIC_ASSET_BASE_URL 必须使用 HTTPS");
}

export default defineConfig({
    base: staticAssetBaseURL ? `${staticAssetBaseURL.replace(/\/+$/, "")}/` : "/",
    plugins: [react()],
    define: {
        __APP_VERSION__: JSON.stringify(appVersion),
    },
    server: {
        proxy: {
            "/api": {
                target: "http://127.0.0.1:8080",
                changeOrigin: true,
                ws: true,
            },
            "/oauth/linuxdo/callback": {
                target: "http://127.0.0.1:8080",
                changeOrigin: true,
            },
        },
    },
    resolve: {
        alias: {
            "@": resolve(webDir, "src"),
        },
    },
    build: {
        manifest: true,
        chunkSizeWarningLimit: 900,
    },
});
