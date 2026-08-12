import { describe, expect, test } from "bun:test";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";

import { createProviderAccountsApi, parseAdminProviderAccount, type AdminProviderAccount, type ProviderAccountTransport } from "../src/services/api/provider-accounts";
import { credentialSecretRequest, formatKuaiziBalance, providerFamilyViews } from "../src/pages/admin/providers/kuaizi-provider-domain";

(globalThis as typeof globalThis & { __APP_VERSION__: string }).__APP_VERSION__ = "test";
const { KuaiziProviderPageView, ProviderCredentialEditor } = await import("../src/pages/admin/providers/kuaizi-provider-page");

const seedanceModel = {
    modelKey: "doubao-seedance-2-5-260628",
    displayName: "Seedance 2.5",
    upstreamMode: "seedance-2.5",
    capability: "video",
    resolutions: ["720p", "1080p"],
    ratios: ["16:9", "9:16"],
    durationMin: 4,
    durationMax: 15,
    supportsSmartDuration: true,
    supportsGeneratedAudio: true,
    supportsWatermark: true,
    supportsAudioOnly: false,
    requiresAdaptiveFrames: false,
    maxImages: 9,
    maxVideos: 3,
    maxAudios: 3,
    maxVideoDurationSeconds: 15,
    maxAudioDurationSeconds: 15,
    tools: ["web_search"],
    published: false,
    channelModelId: "",
    enabled: false,
    priceConfigured: false,
};

const accountFixture = {
    providerKind: "kuaizi",
    name: "筷子科技",
    enabled: true,
    endpoint: { baseUrl: "https://aiopenapi.kuaizi.cn", version: 3, active: true },
    endpointCandidate: { baseUrl: "https://candidate.kuaizi.example", version: 4, active: false },
    credential: {
        active: {
            hasKey: true,
            keyFingerprint: "sha256:seedance-fingerprint",
            version: 8,
            healthStatus: "healthy",
            walletBalanceSubunits: "900719925474099312345",
            verifiedAt: "2026-08-11T08:00:00Z",
        },
        candidate: {
            hasKey: true,
            keyFingerprint: "sha256:candidate-fingerprint",
            version: 9,
            healthStatus: "invalid",
            walletBalanceSubunits: "",
            verifiedAt: "2026-08-11T08:01:00Z",
        },
    },
    adapters: [{ providerKind: "kuaizi", family: "seedance", models: [seedanceModel] }],
};

function parsedFixture(): AdminProviderAccount {
    return parseAdminProviderAccount(accountFixture);
}

describe("kuaizi provider API and domain", () => {
    test("parses explicit credential lifecycle roles and rejects the retired flattened shape", () => {
        const version = {
            hasKey: true,
            keyFingerprint: "sha256:lifecycle-fixture",
            version: 3,
            healthStatus: "invalid",
            walletBalanceSubunits: "100",
        };
        const explicit = parseAdminProviderAccount({
            ...accountFixture,
            credential: { active: null, candidate: version },
        });

        expect(explicit.credential?.active).toBeNull();
        expect(explicit.credential?.candidate?.healthStatus).toBe("invalid");
        expect(() =>
            parseAdminProviderAccount({
                ...accountFixture,
                credential: {
                    hasKey: true,
                    keyFingerprint: "sha256:retired-flat-shape",
                    version: 1,
                    healthStatus: "healthy",
                    walletBalanceSubunits: "100",
                },
            }),
        ).toThrow("必须显式提供 active 与 candidate 生命周期角色");
    });

    test("formats balance subunits with exact string division by one hundred", () => {
        const account = parsedFixture();

        expect(account.credential?.active?.walletBalanceSubunits).toBe("900719925474099312345");
        expect(["", "0", "1", "99", "100", "101", account.credential?.active?.walletBalanceSubunits ?? ""].map(formatKuaiziBalance)).toEqual([
            "尚未验证",
            "0.00 筷子点数",
            "0.01 筷子点数",
            "0.99 筷子点数",
            "1.00 筷子点数",
            "1.01 筷子点数",
            "9,007,199,254,740,993,123.45 筷子点数",
        ]);
    });

    test("rejects non-canonical balance subunits at the DTO boundary", () => {
        for (const invalid of ["-1", "01", "1.2", "1e2", " 1", "1 "]) {
            const malformed = structuredClone(accountFixture);
            malformed.credential!.active!.walletBalanceSubunits = invalid;

            expect(() => parseAdminProviderAccount(malformed)).toThrow("walletBalanceSubunits 必须是空字符串或规范化非负十进制整数");
        }
    });

    test("treats a blank key as no credential write", async () => {
        const requests: Parameters<ProviderAccountTransport>[0][] = [];
        const transport: ProviderAccountTransport = async (request) => {
            requests.push(request);
            return accountFixture;
        };
        const api = createProviderAccountsApi(transport);

        expect(credentialSecretRequest("   ")).toBeNull();
        expect(await api.saveCredential("   ")).toBeNull();
        expect(requests).toEqual([]);
    });

    test("writes a replacement key only to the account credential endpoint", async () => {
        const requests: Parameters<ProviderAccountTransport>[0][] = [];
        const transport: ProviderAccountTransport = async (request) => {
            requests.push(request);
            return accountFixture;
        };
        const api = createProviderAccountsApi(transport);

        await api.saveCredential("  replacement-key  ");

        expect(requests).toEqual([
            {
                method: "PUT",
                path: "/admin/providers/kuaizi/credential",
                data: { key: "replacement-key" },
            },
        ]);
    });

    test("uses the Task 2 endpoint and verification routes without parallel APIs", async () => {
        const requests: Parameters<ProviderAccountTransport>[0][] = [];
        const transport: ProviderAccountTransport = async (request) => {
            requests.push(request);
            return accountFixture;
        };
        const api = createProviderAccountsApi(transport);

        await api.get();
        await api.saveEndpoint(" https://aiopenapi.kuaizi.cn ");
        await api.verifyCredential();
        await api.publishModels("seedance/video");

        expect(requests).toEqual([
            { method: "GET", path: "/admin/providers/kuaizi" },
            { method: "PUT", path: "/admin/providers/kuaizi", data: { baseUrl: "https://aiopenapi.kuaizi.cn" } },
            { method: "POST", path: "/admin/providers/kuaizi/credential/verify" },
            { method: "POST", path: "/admin/providers/kuaizi/models/seedance%2Fvideo/publish" },
        ]);
    });

    test("rejects an unknown backend health status instead of mapping it", () => {
        const malformed = structuredClone(accountFixture);
        malformed.credential!.active!.healthStatus = "future_status";

        expect(() => parseAdminProviderAccount(malformed)).toThrow("不受支持的 healthStatus: future_status");
    });
});

describe("kuaizi provider settings components", () => {
    test("renders first failed candidates and equally unhealthy active versions by explicit role", () => {
        for (const healthStatus of ["invalid", "blocked", "unavailable"] as const) {
            const version = {
                hasKey: true,
                keyFingerprint: `sha256:${healthStatus}`,
                version: 3,
                healthStatus,
                walletBalanceSubunits: "100",
            };
            const candidateAccount = parseAdminProviderAccount({
                ...accountFixture,
                credential: { active: null, candidate: version },
            });
            expect(candidateAccount.credential?.active).toBeNull();
            const candidateMarkup = renderToStaticMarkup(
                createElement(KuaiziProviderPageView, {
                    account: candidateAccount,
                    endpointDraft: "https://aiopenapi.kuaizi.cn",
                    endpointDirty: false,
                    endpointSyncPending: false,
                    loading: false,
                    operation: null,
                    loadError: null,
                    operationErrors: {},
                    onEndpointChange: () => undefined,
                    onSaveEndpoint: () => undefined,
                    onOpenCredential: () => undefined,
                    onVerifyCredential: () => undefined,
                    onPublishModels: () => undefined,
                    onRetry: () => undefined,
                }),
            );
            expect(candidateMarkup).toContain("尚未激活凭据");
            expect(candidateMarkup).toContain("候选版本 3");
            expect(candidateMarkup).toContain("验证成功前不会成为活动凭据");
            expect(candidateMarkup).not.toContain("凭据版本");
            expect(candidateMarkup).not.toContain("当前活动版本未变");

            const activeAccount = parseAdminProviderAccount({
                ...accountFixture,
                credential: { active: version, candidate: null },
            });
            const activeMarkup = renderToStaticMarkup(
                createElement(KuaiziProviderPageView, {
                    account: activeAccount,
                    endpointDraft: "https://aiopenapi.kuaizi.cn",
                    endpointDirty: false,
                    endpointSyncPending: false,
                    loading: false,
                    operation: null,
                    loadError: null,
                    operationErrors: {},
                    onEndpointChange: () => undefined,
                    onSaveEndpoint: () => undefined,
                    onOpenCredential: () => undefined,
                    onVerifyCredential: () => undefined,
                    onPublishModels: () => undefined,
                    onRetry: () => undefined,
                }),
            );
            expect(activeMarkup).toContain("凭据版本");
            expect(activeMarkup).not.toContain("候选版本");
            expect(activeMarkup).not.toContain("尚未激活凭据");
        }
    });

    test("renders every backend adapter without a hardcoded family list", () => {
        const account = parsedFixture();
        account.adapters.push({
            providerKind: "kuaizi",
            family: "motionverse",
            models: [{ ...seedanceModel, modelKey: "motionverse-alpha", displayName: "MotionVerse Alpha", upstreamMode: "motionverse-alpha" }],
        });

        const markup = renderToStaticMarkup(
            createElement(KuaiziProviderPageView, {
                account,
                endpointDraft: account.endpoint?.baseUrl ?? "",
                endpointDirty: false,
                endpointSyncPending: false,
                loading: false,
                operation: null,
                loadError: null,
                operationErrors: {},
                onEndpointChange: () => undefined,
                onSaveEndpoint: () => undefined,
                onOpenCredential: () => undefined,
                onVerifyCredential: () => undefined,
                onPublishModels: () => undefined,
                onRetry: () => undefined,
            }),
        );

        expect(markup).toContain("Seedance 2.5");
        expect(markup).toContain("MotionVerse Alpha");
        expect(markup).toContain("motionverse");
    });

    test("keeps the key write-only and locks input and closing during verification", () => {
        const markup = renderToStaticMarkup(
            createElement(ProviderCredentialEditor, {
                adapter: parsedFixture().adapters[0]!,
                credential: parsedFixture().credential,
                open: true,
                verifying: true,
                error: new Error("筷子凭据验证失败（code=invalid_key, trace_id=trace-401）"),
                onCancel: () => undefined,
                onSubmit: async () => undefined,
            }),
        );

        expect(markup).toContain('type="password"');
        expect(markup).toContain("disabled");
        expect(markup).toContain("候选版本 9");
        expect(markup).toContain("sha256:candidate-fingerprint");
        expect(markup).toContain("code=invalid_key");
        expect(markup).toContain("trace_id=trace-401");
        expect(markup).not.toContain("replacement-key");
        expect(markup).not.toContain("enc:provider:v1:");
    });

    test("keeps the active health separate from a failed candidate", () => {
        const markup = renderToStaticMarkup(
            createElement(KuaiziProviderPageView, {
                account: parsedFixture(),
                endpointDraft: "https://aiopenapi.kuaizi.cn",
                endpointDirty: false,
                endpointSyncPending: false,
                loading: false,
                operation: null,
                loadError: null,
                operationErrors: {},
                onEndpointChange: () => undefined,
                onSaveEndpoint: () => undefined,
                onOpenCredential: () => undefined,
                onVerifyCredential: () => undefined,
                onPublishModels: () => undefined,
                onRetry: () => undefined,
            }),
        );

        const mainStatusIndex = markup.indexOf("验证健康");
        const candidateIndex = markup.indexOf("候选版本 9");
        const candidateStatusIndex = markup.indexOf("密钥无效");
        expect(mainStatusIndex).toBeGreaterThan(-1);
        expect(candidateIndex).toBeGreaterThan(mainStatusIndex);
        expect(candidateStatusIndex).toBeGreaterThan(candidateIndex);
        expect(markup).toContain("当前活动版本未变");
    });
});
