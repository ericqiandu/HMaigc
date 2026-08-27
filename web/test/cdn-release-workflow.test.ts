import { describe, expect, test } from "bun:test";

type WorkflowJob = {
    needs?: string | string[];
    steps?: Array<Record<string, unknown>>;
};

type Workflow = {
    jobs?: Record<string, WorkflowJob>;
};

function parseWorkflow(source: string): Workflow {
    const parsed: unknown = Bun.YAML.parse(source);
    if (!parsed || typeof parsed !== "object") throw new Error("发布工作流不是 YAML 对象");
    return parsed as Workflow;
}

function jobNeeds(job: WorkflowJob | undefined): string[] {
    if (!job?.needs) return [];
    return Array.isArray(job.needs) ? job.needs : [job.needs];
}

describe("versioned CDN web release workflow", () => {
    test("publishes and verifies the single web dist before container images", async () => {
        const source = await Bun.file(new URL("../../.github/workflows/publish-images.yml", import.meta.url)).text();
        const workflow = parseWorkflow(source);
        const jobs = workflow.jobs ?? {};

        expect(jobs["build-web-release"]).toBeUndefined();
        expect(jobs["publish-static-assets"]).toBeDefined();
        expect(jobNeeds(jobs.publish)).toContain("publish-static-assets");

        const staticJob = JSON.stringify(jobs["publish-static-assets"]);
        expect(staticJob).toContain("VITE_STATIC_ASSET_BASE_URL");
        expect(staticJob).toContain("HMAIGC_STATIC_ASSET_BASE_URL");
        expect(staticJob).toContain("HMAIGC_STATIC_OSS_BUCKET");
        expect(staticJob).toContain("HMAIGC_STATIC_OSS_ENDPOINT");
        expect(staticJob).toContain("HMAIGC_STATIC_OSS_ACCESS_KEY_ID");
        expect(staticJob).toContain("HMAIGC_STATIC_OSS_ACCESS_KEY_SECRET");
        expect(staticJob).toContain("go run ./cmd/staticpublisher");
        expect(staticJob).toContain("verify-static-release-assets.sh");
        expect(staticJob).toContain("actions/upload-artifact@v4");

        const publishJob = JSON.stringify(jobs.publish);
        expect(publishJob).toContain("actions/download-artifact@v4");
        expect(publishJob).toContain("Dockerfile.web-release");
    });

    test("keeps the SPA entry revalidated while retaining local immutable assets for the exact image artifact", async () => {
        const nginx = await Bun.file(new URL("../../nginx.conf", import.meta.url)).text();

        expect(nginx).toContain('~^/assets/ "public, max-age=31536000, immutable";');
        expect(nginx).toContain('/index.html "no-cache";');
        expect(nginx).toContain("add_header Cache-Control $canvas_static_cache_control always;");
    });
});
