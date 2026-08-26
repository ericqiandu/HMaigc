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

function jobNeeds(job: WorkflowJob | undefined) {
    if (!job?.needs) return [];
    return Array.isArray(job.needs) ? job.needs : [job.needs];
}

describe("same-origin web release workflow", () => {
    test("builds one root-relative dist artifact and packages it without the retired OSS/CDN publish path", async () => {
        const source = await Bun.file(new URL("../../.github/workflows/publish-images.yml", import.meta.url)).text();
        const workflow = parseWorkflow(source);
        const jobs = workflow.jobs ?? {};

        expect(jobs["publish-static-assets"]).toBeUndefined();
        expect(jobs["build-web-release"]).toBeDefined();
        expect(jobNeeds(jobs.publish)).toContain("build-web-release");

        const buildJob = JSON.stringify(jobs["build-web-release"]);
        expect(buildJob).toContain("bun run build");
        expect(buildJob).toContain("actions/upload-artifact@v4");
        expect(buildJob).not.toContain("VITE_STATIC_ASSET_BASE_URL");

        const releaseContract = JSON.stringify(workflow);
        expect(releaseContract).not.toContain("HMAIGC_STATIC_OSS_");
        expect(releaseContract).not.toContain("HMAIGC_STATIC_ASSET_BASE_URL");
        expect(releaseContract).not.toContain("cmd/staticpublisher");
        expect(releaseContract).not.toContain("verify-static-release-assets.sh");
    });

    test("serves same-origin program assets with immutable caching and revalidates the SPA entry", async () => {
        const nginx = await Bun.file(new URL("../../nginx.conf", import.meta.url)).text();

        expect(nginx).toContain('~^/assets/ "public, max-age=31536000, immutable";');
        expect(nginx).toContain('/index.html "no-cache";');
        expect(nginx).toContain("add_header Cache-Control $canvas_static_cache_control always;");
    });
});
