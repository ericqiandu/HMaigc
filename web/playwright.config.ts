import { defineConfig, devices } from "@playwright/test";

export default defineConfig({
    testDir: "./e2e",
    testMatch: "*.e2e.ts",
    fullyParallel: false,
    retries: 0,
    use: {
        baseURL: "http://127.0.0.1:4178",
        viewport: { width: 1440, height: 900 },
    },
    webServer: {
        command: "bunx vite --host 127.0.0.1 --port 4178 --strictPort",
        port: 4178,
        reuseExistingServer: false,
    },
    projects: [
        { name: "chromium", use: { ...devices["Desktop Chrome"] } },
        { name: "firefox", use: { ...devices["Desktop Firefox"] } },
        { name: "webkit", use: { ...devices["Desktop Safari"] } },
    ],
});
