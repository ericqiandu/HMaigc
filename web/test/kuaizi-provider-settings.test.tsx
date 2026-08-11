import { describe, expect, test } from "bun:test";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";

import { createProviderAccountsApi, parseAdminProviderAccount, type AdminProviderAccount, type ProviderAccountTransport } from "../src/services/api/provider-accounts";
import { credentialSecretRequest, formatKuaiziBalance } from "../src/pages/admin/providers/kuaizi-provider-domain";

(globalThis as typeof globalThis & { __APP_VERSION__: string }).__APP_VERSION__ = "test";
const { KuaiziProviderPageView, ProviderCredentialEditor } = await import("../src/pages/admin/providers/kuaizi-provider-page");

const seedanceModel = {
    modelKey: "kuaizi-seedance-2.5",
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
    maxImages: 9,
    maxVideos: 3,
    maxAudios: 3,
};

const accountFixture = {
    providerKind: "kuaizi",
    name: "筷子科技",
    enabled: true,
    endpoint: { baseUrl: "https://aiopenapi.kuaizi.cn", version: 3, active: true },
    endpointCandidate: { baseUrl: "https://candidate.kuaizi.example", version: 4, active: false },
    credentials: [
        {
            family: "seedance",
            hasKey: true,
            keyFingerprint: "sha256:seedance-fingerprint",
            version: 8,
            healthStatus: "healthy",
            walletBalanceSubunits: "900719925474099312345",
            verifiedAt: "2026-08-11T08:00:00Z",
            candidate: {
                hasKey: true,
                keyFingerprint: "sha256:candidate-fingerprint",
                version: 9,
                healthStatus: "invalid",
                walletBalanceSubunits: "",
                verifiedAt: "2026-08-11T08:01:00Z",
            },
        },
    ],
    adapters: [{ providerKind: "kuaizi", family: "seedance", models: [seedanceModel] }],
};

function parsedFixture(): AdminProviderAccount {
    return parseAdminProviderAccount(accountFixture);
}

describe("kuaizi provider API and domain", () => {
    test("keeps a large decimal balance as a string and labels it as Kuaizi points", () => {
        const account = parsedFixture();

        expect(account.credentials[0]?.walletBalanceSubunits).toBe("900719925474099312345");
        expect(formatKuaiziBalance(account.credentials[0]?.walletBalanceSubunits ?? "")).toBe("900,719,925,474,099,312,345 筷子点数");
    });

    test("treats a blank key as no credential write", async () => {
        const requests: Parameters<ProviderAccountTransport>[0][] = [];
        const transport: ProviderAccountTransport = async (request) => {
            requests.push(request);
            return accountFixture;
        };
        const api = createProviderAccountsApi(transport);

        expect(credentialSecretRequest("   ")).toBeNull();
        expect(await api.saveCredential("seedance", "   ")).toBeNull();
        expect(requests).toEqual([]);
    });

    test("writes a replacement key only to the family credential endpoint", async () => {
        const requests: Parameters<ProviderAccountTransport>[0][] = [];
        const transport: ProviderAccountTransport = async (request) => {
            requests.push(request);
            return accountFixture;
        };
        const api = createProviderAccountsApi(transport);

        await api.saveCredential("seedance/video", "  replacement-key  ");

        expect(requests).toEqual([
            {
                method: "PUT",
                path: "/admin/providers/kuaizi/credentials/seedance%2Fvideo",
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
        await api.verifyCredential("seedance/video");

        expect(requests).toEqual([
            { method: "GET", path: "/admin/providers/kuaizi" },
            { method: "PUT", path: "/admin/providers/kuaizi", data: { baseUrl: "https://aiopenapi.kuaizi.cn" } },
            { method: "POST", path: "/admin/providers/kuaizi/credentials/seedance%2Fvideo/verify" },
        ]);
    });

    test("rejects an unknown backend health status instead of mapping it", () => {
        const malformed = structuredClone(accountFixture);
        malformed.credentials[0]!.healthStatus = "future_status";

        expect(() => parseAdminProviderAccount(malformed)).toThrow("不受支持的 healthStatus: future_status");
    });
});

describe("kuaizi provider settings components", () => {
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
                loading: false,
                operation: null,
                loadError: null,
                operationErrors: {},
                onEndpointChange: () => undefined,
                onSaveEndpoint: () => undefined,
                onOpenCredential: () => undefined,
                onVerifyCredential: () => undefined,
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
                credential: parsedFixture().credentials[0]!,
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
});
