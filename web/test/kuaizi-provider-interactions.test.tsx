import { afterEach, beforeAll, describe, expect, test } from "bun:test";
import { Window } from "happy-dom";
import { readFileSync } from "node:fs";
import { act, createElement } from "react";
import type { Root } from "react-dom/client";

import { parseAdminProviderAccount, type AdminProviderAccount, type ProviderAccountsApi } from "../src/services/api/provider-accounts";

type Deferred<T> = {
    promise: Promise<T>;
    resolve: (value: T) => void;
    reject: (reason: Error) => void;
};

type InjectedProviderApi = Omit<ProviderAccountsApi, "publishModels"> & { publishModels?: ProviderAccountsApi["publishModels"] };

const model = {
    modelKey: "fixture-video-v1",
    displayName: "Fixture Video V1",
    upstreamMode: "fixture-video-v1",
    capability: "video",
    resolutions: ["720p"],
    ratios: ["16:9"],
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

function account(overrides: Partial<AdminProviderAccount> = {}): AdminProviderAccount {
    return {
        providerKind: "kuaizi",
        name: "筷子科技",
        enabled: true,
        endpoint: { baseUrl: "https://active.example.com", version: 1, active: true },
        credential: {
            active: {
                hasKey: true,
                keyFingerprint: "sha256:active",
                version: 1,
                healthStatus: "healthy",
                walletBalanceSubunits: "100",
                verifiedAt: "2026-08-11T08:00:00Z",
            },
            candidate: null,
        },
        adapters: [{ providerKind: "kuaizi", family: "fixture-family", models: [model] }],
        ...overrides,
    };
}

function deferred<T>(): Deferred<T> {
    let resolvePromise: ((value: T) => void) | undefined;
    let rejectPromise: ((reason: Error) => void) | undefined;
    const promise = new Promise<T>((resolve, reject) => {
        resolvePromise = resolve;
        rejectPromise = reject;
    });
    return {
        promise,
        resolve: (value) => resolvePromise?.(value),
        reject: (reason) => rejectPromise?.(reason),
    };
}

function button(label: string): HTMLButtonElement {
    const normalizedLabel = label.replace(/\s+/g, "");
    const match = [...document.querySelectorAll("button")].find((element) => element.textContent?.replace(/\s+/g, "").includes(normalizedLabel));
    if (!(match instanceof HTMLButtonElement)) throw new Error(`未找到按钮：${label}；当前页面：${document.body.textContent}`);
    return match;
}

function input(selector: string): HTMLInputElement {
    const match = document.querySelector(selector);
    if (!(match instanceof HTMLInputElement)) throw new Error(`未找到输入框：${selector}`);
    return match;
}

async function changeInput(element: HTMLInputElement, value: string) {
    const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")?.set;
    if (!setter) throw new Error("测试 DOM 缺少 input value setter");
    await act(async () => {
        setter.call(element, value);
        element.dispatchEvent(new Event("input", { bubbles: true }));
        element.dispatchEvent(new Event("change", { bubbles: true }));
    });
}

async function settle() {
    await act(async () => {
        await Promise.resolve();
        await new Promise((resolve) => setTimeout(resolve, 0));
    });
}

let KuaiziProviderPage: (props: { api?: InjectedProviderApi }) => ReturnType<typeof createElement>;
let createRoot: (container: Element | DocumentFragment) => Root;
let mountedRoot: Root | null = null;

beforeAll(async () => {
    const browserWindow = new Window({ url: "http://localhost/admin/providers/kuaizi" });
    const globals: Record<string, unknown> = {
        window: browserWindow,
        document: browserWindow.document,
        navigator: browserWindow.navigator,
        localStorage: browserWindow.localStorage,
        Event: browserWindow.Event,
        MouseEvent: browserWindow.MouseEvent,
        KeyboardEvent: browserWindow.KeyboardEvent,
        HTMLElement: browserWindow.HTMLElement,
        HTMLAnchorElement: browserWindow.HTMLAnchorElement,
        HTMLButtonElement: browserWindow.HTMLButtonElement,
        HTMLInputElement: browserWindow.HTMLInputElement,
        Element: browserWindow.Element,
        Node: browserWindow.Node,
        ShadowRoot: browserWindow.ShadowRoot,
        SVGElement: browserWindow.SVGElement,
        Blob: browserWindow.Blob,
        FileReader: browserWindow.FileReader,
        XMLHttpRequest: browserWindow.XMLHttpRequest,
        getComputedStyle: browserWindow.getComputedStyle.bind(browserWindow),
        requestAnimationFrame: browserWindow.requestAnimationFrame.bind(browserWindow),
        cancelAnimationFrame: browserWindow.cancelAnimationFrame.bind(browserWindow),
        ResizeObserver: browserWindow.ResizeObserver,
    };
    for (const [name, value] of Object.entries(globals)) {
        Object.defineProperty(globalThis, name, { configurable: true, writable: true, value });
    }
    (globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean; __APP_VERSION__: string }).IS_REACT_ACT_ENVIRONMENT = true;
    (globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean; __APP_VERSION__: string }).__APP_VERSION__ = "test";
    ({ createRoot } = await import("react-dom/client"));
    ({ default: KuaiziProviderPage } = await import("../src/pages/admin/providers/kuaizi-provider-page"));
});

afterEach(async () => {
    if (mountedRoot) {
        await act(async () => mountedRoot?.unmount());
        mountedRoot = null;
    }
    document.body.replaceChildren();
});

async function mount(api: InjectedProviderApi) {
    const container = document.createElement("div");
    document.body.append(container);
    mountedRoot = createRoot(container);
    const completeApi: ProviderAccountsApi = { ...api, publishModels: api.publishModels ?? (async () => account()) };
    await act(async () => mountedRoot?.render(createElement(KuaiziProviderPage, { api: completeApi })));
    await settle();
}

describe("kuaizi provider page mutation controller", () => {
    test("opens an explicitly role-tagged first failed candidate without presenting it as active", async () => {
        const firstFailedCandidate = parseAdminProviderAccount({
            ...account(),
            credential: {
                active: null,
                candidate: {
                    hasKey: true,
                    keyFingerprint: "sha256:first-failed-candidate",
                    version: 1,
                    healthStatus: "blocked",
                    walletBalanceSubunits: "",
                },
            },
        });
        const api: InjectedProviderApi = {
            get: async () => firstFailedCandidate,
            saveEndpoint: async () => firstFailedCandidate,
            saveCredential: async () => firstFailedCandidate,
            verifyCredential: async () => firstFailedCandidate,
        };
        await mount(api);

        expect(document.body.textContent).toContain("尚未激活凭据");
        expect(document.body.textContent).toContain("候选版本 1");
        expect(document.body.textContent).not.toContain("凭据版本");
        await act(async () => button("更新密钥").click());
        expect(document.querySelector('input[type="password"]')).not.toBeNull();
        expect(document.body.textContent).toContain("sha256:first-failed-candidate");
    });

    test("clears a replacement secret, orders save before verify, locks dismissal, rejects duplicate clicks, and fills the verified result", async () => {
        const save = deferred<AdminProviderAccount | null>();
        const verify = deferred<AdminProviderAccount>();
        const calls: string[] = [];
        const api: InjectedProviderApi = {
            get: async () => account(),
            saveEndpoint: async () => account(),
            saveCredential: (key) => {
                calls.push(`save:${key}`);
                return save.promise;
            },
            verifyCredential: () => {
                calls.push("verify");
                return verify.promise;
            },
        };
        await mount(api);

        await act(async () => button("更新密钥").click());
        const secretInput = input('input[type="password"]');
        await changeInput(secretInput, "sentinel-browser-secret");
        await act(async () => {
            button("保存并验证").click();
            button("保存并验证").click();
        });

        expect(secretInput.value).toBe("");
        expect(document.body.textContent).not.toContain("sentinel-browser-secret");
        expect(calls).toEqual(["save:sentinel-browser-secret"]);
        expect(button("取消").disabled).toBe(true);
        expect(document.querySelector(".ant-modal-close")).toBeNull();
        await act(async () => {
            document.querySelector<HTMLElement>(".ant-modal-mask")?.click();
            window.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape", bubbles: true }));
        });
        expect(document.querySelector('input[type="password"]')).not.toBeNull();

        const candidate = account({
            credential: {
                ...account().credential!,
                candidate: {
                    hasKey: true,
                    keyFingerprint: "sha256:candidate",
                    version: 2,
                    healthStatus: "unverified",
                    walletBalanceSubunits: "",
                },
            },
        });
        await act(async () => save.resolve(candidate));
        await settle();
        expect(calls).toEqual(["save:sentinel-browser-secret", "verify"]);

        const verified = account({
            credential: {
                ...account().credential!,
                active: {
                    ...account().credential!.active!,
                    keyFingerprint: "sha256:verified-v2",
                    version: 2,
                    walletBalanceSubunits: "101",
                },
                candidate: null,
            },
        });
        await act(async () => verify.resolve(verified));
        await settle();
        expect(document.body.textContent).toContain("sha256:verified-v2");
        expect(document.body.textContent).toContain("1.01 筷子点数");
    });

    test("runs first configuration in endpoint, credential, verify order", async () => {
        const calls: string[] = [];
        const initial = account({ endpoint: undefined, credential: null });
        const endpointCandidate = account({ endpoint: { baseUrl: "https://first.example.com", version: 1, active: false }, credential: null });
        const credentialCandidate = account({
            endpoint: endpointCandidate.endpoint,
            credential: {
                active: null,
                candidate: {
                    hasKey: true,
                    keyFingerprint: "sha256:first-candidate",
                    version: 1,
                    healthStatus: "unverified",
                    walletBalanceSubunits: "",
                },
            },
        });
        const verified = account({ endpoint: { baseUrl: "https://first.example.com", version: 1, active: true } });
        const api: InjectedProviderApi = {
            get: async () => initial,
            saveEndpoint: async (baseUrl) => {
                calls.push(`endpoint:${baseUrl}`);
                return endpointCandidate;
            },
            saveCredential: async (key) => {
                calls.push(`credential:${key}`);
                return credentialCandidate;
            },
            verifyCredential: async () => {
                calls.push("verify");
                return verified;
            },
        };
        await mount(api);

        await changeInput(input("#kuaizi-provider-base-url"), "https://first.example.com");
        await act(async () => button("保存服务地址").click());
        await settle();
        await act(async () => button("配置密钥").click());
        await changeInput(input('input[type="password"]'), "sentinel-first-secret");
        await act(async () => button("保存并验证").click());
        await settle();

        expect(calls).toEqual(["endpoint:https://first.example.com", "credential:sentinel-first-secret", "verify"]);
        expect(document.body.textContent).toContain("sha256:active");
    });

    test("keeps a failed credential candidate and the original code and trace after refetch", async () => {
        const calls: string[] = [];
        const refreshed = deferred<AdminProviderAccount>();
        let getCount = 0;
        const candidate = account({
            credential: {
                ...account().credential!,
                candidate: {
                    hasKey: true,
                    keyFingerprint: "sha256:failed-candidate",
                    version: 2,
                    healthStatus: "invalid",
                    walletBalanceSubunits: "",
                },
            },
        });
        const api: InjectedProviderApi = {
            get: () => {
                getCount += 1;
                calls.push("get");
                return getCount === 1 ? Promise.resolve(account()) : refreshed.promise;
            },
            saveEndpoint: async () => account(),
            saveCredential: async () => {
                calls.push("save");
                return candidate;
            },
            verifyCredential: async () => {
                calls.push("verify");
                throw new Error("筷子凭据验证失败（code=invalid_key, trace_id=trace-browser）");
            },
        };
        await mount(api);

        await act(async () => button("更新密钥").click());
        await changeInput(input('input[type="password"]'), "sentinel-failed-secret");
        await act(async () => button("保存并验证").click());
        await settle();
        expect(calls).toEqual(["get", "save", "verify", "get"]);

        await act(async () => refreshed.resolve(candidate));
        await settle();
        expect(document.body.textContent).toContain("sha256:failed-candidate");
        expect(document.body.textContent).toContain("code=invalid_key");
        expect(document.body.textContent).toContain("trace_id=trace-browser");
        expect(document.body.textContent).not.toContain("sentinel-failed-secret");
    });

    test("keeps the global owner when a committed credential candidate response and its convergence GET both fail", async () => {
        const synchronized = deferred<AdminProviderAccount>();
        let getCalls = 0;
        let saveCalls = 0;
        let verifyCalls = 0;
        let endpointCalls = 0;
        const serverCandidate = account({
            credential: {
                ...account().credential!,
                candidate: {
                    hasKey: true,
                    keyFingerprint: "sha256:committed-candidate",
                    version: 2,
                    healthStatus: "unverified",
                    walletBalanceSubunits: "",
                },
            },
        });
        const api: InjectedProviderApi = {
            get: () => {
                getCalls += 1;
                if (getCalls === 1) return Promise.resolve(account());
                if (getCalls === 2) return Promise.reject(new Error("凭据候选事实读取失败"));
                return synchronized.promise;
            },
            saveEndpoint: async () => {
                endpointCalls += 1;
                return account();
            },
            saveCredential: async () => {
                saveCalls += 1;
                throw new Error(saveCalls === 1 ? "筷子候选保存响应丢失（code=candidate_response_lost, trace_id=trace-candidate-commit）" : "不应发出第二次候选保存（code=duplicate_candidate_write, trace_id=trace-duplicate）");
            },
            verifyCredential: async () => {
                verifyCalls += 1;
                return account();
            },
        };
        await mount(api);

        await act(async () => button("更新密钥").click());
        await changeInput(input('input[type="password"]'), "sentinel-committed-candidate-secret");
        await act(async () => button("保存并验证").click());
        await settle();

        expect(document.body.textContent).toContain("code=candidate_response_lost");
        expect(document.body.textContent).toContain("trace_id=trace-candidate-commit");
        expect(document.body.textContent).toContain("写入结果待同步（账号凭据）");
        expect(document.body.textContent).toContain("凭据候选事实读取失败");
        const secretInput = input('input[type="password"]');
        secretInput.disabled = false;
        await changeInput(secretInput, "sentinel-second-candidate-secret");
        const submit = button("保存并验证");
        submit.disabled = false;
        await act(async () => submit.click());
        await settle();
        expect({ saveCalls, verifyCalls, endpointCalls }).toEqual({ saveCalls: 1, verifyCalls: 0, endpointCalls: 0 });
        expect(document.body.textContent).not.toContain("sentinel-committed-candidate-secret");
        expect(document.body.textContent).not.toContain("sentinel-second-candidate-secret");

        await act(async () => button("重试").click());
        await act(async () => synchronized.resolve(serverCandidate));
        await settle();
        expect(document.body.textContent).toContain("sha256:committed-candidate");
        expect(document.body.textContent).toContain("code=candidate_response_lost");
        expect(document.body.textContent).toContain("trace_id=trace-candidate-commit");
        expect(button("取消").disabled).toBe(false);
        expect(button("保存并验证").disabled).toBe(false);
        expect(document.body.textContent).not.toContain("写入结果待同步");
    });

    test("keeps the global owner when an activated credential response and its convergence GET both fail", async () => {
        const synchronized = deferred<AdminProviderAccount>();
        let getCalls = 0;
        let verifyCalls = 0;
        let endpointCalls = 0;
        const serverActive = account({
            credential: {
                ...account().credential!,
                active: {
                    ...account().credential!.active!,
                    keyFingerprint: "sha256:activated-after-response-loss",
                    version: 2,
                    walletBalanceSubunits: "250",
                },
                candidate: null,
            },
        });
        const api: InjectedProviderApi = {
            get: () => {
                getCalls += 1;
                if (getCalls === 1) return Promise.resolve(account());
                if (getCalls === 2) return Promise.reject(new Error("活动凭据事实读取失败"));
                return synchronized.promise;
            },
            saveEndpoint: async () => {
                endpointCalls += 1;
                return account();
            },
            saveCredential: async () => null,
            verifyCredential: async () => {
                verifyCalls += 1;
                throw new Error(verifyCalls === 1 ? "筷子验证响应丢失（code=activation_response_lost, trace_id=trace-active-commit）" : "不应发出第二次验证（code=duplicate_verify, trace_id=trace-duplicate-verify）");
            },
        };
        await mount(api);

        await act(async () => button("验证凭据").click());
        await settle();
        expect(document.body.textContent).toContain("code=activation_response_lost");
        expect(document.body.textContent).toContain("trace_id=trace-active-commit");
        expect(document.body.textContent).toContain("写入结果待同步（账号凭据）");
        expect(document.body.textContent).toContain("活动凭据事实读取失败");

        const baseUrl = input("#kuaizi-provider-base-url");
        baseUrl.disabled = false;
        await changeInput(baseUrl, "https://must-not-write-after-activation.example.com");
        const endpointSubmit = button("保存服务地址");
        endpointSubmit.disabled = false;
        const verifySubmit = button("验证凭据");
        verifySubmit.disabled = false;
        await act(async () => endpointSubmit.click());
        await settle();
        verifySubmit.disabled = false;
        await act(async () => verifySubmit.click());
        await settle();
        expect({ endpointCalls, verifyCalls }).toEqual({ endpointCalls: 0, verifyCalls: 1 });

        await act(async () => button("重试").click());
        await act(async () => synchronized.resolve(serverActive));
        await settle();
        expect(document.body.textContent).toContain("sha256:activated-after-response-loss");
        expect(document.body.textContent).toContain("2.50 筷子点数");
        expect(document.body.textContent).toContain("code=activation_response_lost");
        expect(document.body.textContent).toContain("trace_id=trace-active-commit");
        expect(input("#kuaizi-provider-base-url").disabled).toBe(false);
        expect(button("验证凭据").disabled).toBe(false);
        expect(document.body.textContent).not.toContain("写入结果待同步");
    });

    test("converges a failed endpoint write, and exposes an uncertain write when refetch also fails", async () => {
        const draft = "https://candidate.example.com";
        let getCount = 0;
        let endpointCalls = 0;
        const converged = account({ endpointCandidate: { baseUrl: draft, version: 2, active: false } });
        const api: InjectedProviderApi = {
            get: async () => {
                getCount += 1;
                if (getCount === 1) return account();
                if (getCount === 2) return converged;
                throw new Error("候选事实读取失败");
            },
            saveEndpoint: async () => {
                endpointCalls += 1;
                throw new Error("筷子凭据验证失败（code=timeout, trace_id=trace-endpoint）");
            },
            saveCredential: async () => null,
            verifyCredential: async () => account(),
        };
        await mount(api);

        await changeInput(input("#kuaizi-provider-base-url"), draft);
        await act(async () => button("保存服务地址").click());
        await settle();
        expect(document.body.textContent).toContain("code=timeout");
        expect(document.body.textContent).toContain("候选地址 v2");
        expect(document.body.textContent).toContain("已同步");
        await act(async () => button("保存服务地址").click());
        expect(endpointCalls).toBe(1);

        await changeInput(input("#kuaizi-provider-base-url"), "https://uncertain.example.com");
        await act(async () => button("保存服务地址").click());
        await settle();
        expect(input("#kuaizi-provider-base-url").value).toBe("https://uncertain.example.com");
        expect(document.body.textContent).toContain("code=timeout");
        expect(document.body.textContent).toContain("写入结果待同步");
        expect(document.body.textContent).not.toContain("有未保存变更");
    });

    test("blocks every write while endpoint facts await sync and unlocks only after an explicit GET succeeds", async () => {
        const uncertainDraft = "https://uncertain.example.com";
        const synchronized = deferred<AdminProviderAccount>();
        let getCalls = 0;
        let endpointCalls = 0;
        let credentialSaveCalls = 0;
        let verifyCalls = 0;
        const api: InjectedProviderApi = {
            get: () => {
                getCalls += 1;
                if (getCalls === 1) return Promise.resolve(account());
                if (getCalls === 2) return Promise.reject(new Error("候选事实读取失败"));
                return synchronized.promise;
            },
            saveEndpoint: async () => {
                endpointCalls += 1;
                throw new Error("筷子凭据验证失败（code=timeout, trace_id=trace-awaiting-sync）");
            },
            saveCredential: async () => {
                credentialSaveCalls += 1;
                return null;
            },
            verifyCredential: async () => {
                verifyCalls += 1;
                return account();
            },
        };
        await mount(api);

        await changeInput(input("#kuaizi-provider-base-url"), uncertainDraft);
        await act(async () => button("保存服务地址").click());
        await settle();

        const baseUrl = input("#kuaizi-provider-base-url");
        const updateCredential = button("更新密钥");
        const verifyCredential = button("验证凭据");
        expect(baseUrl.disabled).toBe(true);
        expect(updateCredential.disabled).toBe(true);
        expect(verifyCredential.disabled).toBe(true);

        baseUrl.disabled = false;
        await changeInput(baseUrl, "https://must-not-overwrite.example.com");
        updateCredential.disabled = false;
        verifyCredential.disabled = false;
        await act(async () => {
            updateCredential.click();
            verifyCredential.click();
        });
        baseUrl.disabled = true;
        updateCredential.disabled = true;
        verifyCredential.disabled = true;
        await settle();
        await act(async () => button("重试").click());
        expect(input("#kuaizi-provider-base-url").value).toBe(uncertainDraft);
        expect(document.querySelector('input[type="password"]')).toBeNull();
        expect({ endpointCalls, credentialSaveCalls, verifyCalls }).toEqual({ endpointCalls: 1, credentialSaveCalls: 0, verifyCalls: 0 });
        expect(button("更新密钥").disabled).toBe(true);
        const serverFact = account({ endpointCandidate: { baseUrl: "https://server-fact.example.com", version: 2, active: false } });
        await act(async () => synchronized.resolve(serverFact));
        await settle();

        expect(input("#kuaizi-provider-base-url").value).toBe("https://server-fact.example.com");
        expect(input("#kuaizi-provider-base-url").disabled).toBe(false);
        expect(button("更新密钥").disabled).toBe(false);
        expect(button("验证凭据").disabled).toBe(false);
        expect(document.body.textContent).not.toContain("写入结果待同步");
    });

    test("locks an already-open credential editor and rejects its submit while endpoint facts await sync", async () => {
        let getCalls = 0;
        let credentialSaveCalls = 0;
        let verifyCalls = 0;
        const api: InjectedProviderApi = {
            get: async () => {
                getCalls += 1;
                if (getCalls === 1) return account();
                throw new Error("候选事实读取失败");
            },
            saveEndpoint: async () => {
                throw new Error("筷子凭据验证失败（code=timeout, trace_id=trace-modal-awaiting-sync）");
            },
            saveCredential: async () => {
                credentialSaveCalls += 1;
                return null;
            },
            verifyCredential: async () => {
                verifyCalls += 1;
                return account();
            },
        };
        await mount(api);

        await changeInput(input("#kuaizi-provider-base-url"), "https://uncertain-with-modal.example.com");
        await act(async () => button("更新密钥").click());
        expect(input('input[type="password"]').disabled).toBe(false);
        await act(async () => button("保存服务地址").click());
        await settle();

        const secretInput = input('input[type="password"]');
        expect(secretInput.disabled).toBe(true);
        expect(button("取消").disabled).toBe(true);
        expect(button("保存并验证").disabled).toBe(true);
        expect(document.querySelector(".ant-modal-close")).toBeNull();

        secretInput.disabled = false;
        await changeInput(secretInput, "sentinel-awaiting-sync-secret");
        const submit = button("保存并验证");
        submit.disabled = false;
        await act(async () => submit.click());
        await settle();
        expect({ credentialSaveCalls, verifyCalls }).toEqual({ credentialSaveCalls: 0, verifyCalls: 0 });
        expect(document.body.textContent).not.toContain("sentinel-awaiting-sync-secret");
    });

    test("claims one global mutation synchronously and releases only after its owner finishes", async () => {
        const verification = deferred<AdminProviderAccount>();
        let verifyCalls = 0;
        let endpointCalls = 0;
        const api: InjectedProviderApi = {
            get: async () => account(),
            saveEndpoint: async () => {
                endpointCalls += 1;
                return account();
            },
            saveCredential: async () => null,
            verifyCredential: () => {
                verifyCalls += 1;
                return verification.promise;
            },
        };
        await mount(api);

        await changeInput(input("#kuaizi-provider-base-url"), "https://dirty.example.com");
        await act(async () => {
            button("验证凭据").click();
            button("验证凭据").click();
            button("保存服务地址").click();
        });
        expect(verifyCalls).toBe(1);
        expect(endpointCalls).toBe(0);
        expect(button("保存服务地址").disabled).toBe(true);

        await act(async () => verification.resolve(account()));
        await settle();
        expect(button("保存服务地址").disabled).toBe(false);
    });
});

describe("kuaizi provider responsive form controls", () => {
    test("keeps the Base URL touch target at 44px for tablet and mobile viewports", () => {
        const style = document.createElement("style");
        style.textContent = readFileSync(new URL("../src/pages/admin/admin-responsive.css", import.meta.url), "utf8");
        document.head.append(style);

        for (const viewport of [390, 768, 1024]) {
            (window as unknown as Window).happyDOM.setInnerWidth(viewport);
            const workspace = document.createElement("div");
            workspace.className = "admin-workspace";
            const baseUrlInput = document.createElement("input");
            baseUrlInput.className = "ant-input kuaizi-provider-base-url";
            const multilineInput = document.createElement("textarea");
            multilineInput.className = "ant-input";
            const outsideInput = document.createElement("input");
            outsideInput.className = "ant-input";
            workspace.append(baseUrlInput, multilineInput);
            document.body.append(workspace, outsideInput);

            expect(getComputedStyle(baseUrlInput).minHeight).toBe("44px");
            expect(getComputedStyle(multilineInput).minHeight).not.toBe("44px");
            expect(getComputedStyle(outsideInput).minHeight).not.toBe("44px");
            workspace.remove();
            outsideInput.remove();
        }

        style.remove();
    });
});
