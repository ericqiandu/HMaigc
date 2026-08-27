import { dirname, resolve } from "node:path";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

const webDir = dirname(fileURLToPath(import.meta.url));
const appVersion = readFileSync(resolve(webDir, "../VERSION"), "utf8").trim();

export default defineConfig({
    base: "/",
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
        rolldownOptions: {
            output: {
                codeSplitting: {
                    groups: [
                        {
                            name: "icons",
                            test: /node_modules[\\/]lucide-react[\\/]/,
                            priority: 30,
                            includeDependenciesRecursively: false,
                        },
                        {
                            name: "react-core",
                            test: /node_modules[\\/](?:react|react-dom|react-router|scheduler)[\\/]/,
                            priority: 20,
                            includeDependenciesRecursively: false,
                        },
                    ],
                },
            },
        },
    },
});
