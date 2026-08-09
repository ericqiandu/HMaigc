import http from "node:http";

const supportedMutations = new Set(["", "team-total"]);
const mutation = (process.env.HMAIGC_CHECKOUT_GATE_MUTATION ?? "").trim();
if (!supportedMutations.has(mutation)) {
    throw new Error(`未知收银台门禁 mutation: ${mutation}`);
}
if (!/^22\.12\./u.test(process.versions.node)) {
    throw new Error(`收银台 fixture 必须运行在 Node 22.12，当前为 ${process.versions.node}`);
}

const port = Number(process.env.PORT ?? "8080");
if (!Number.isSafeInteger(port) || port < 1 || port > 65535) throw new Error("fixture PORT 无效");

const scenarioNames = ["provider-failure", "poll-failure", "active-qr", "cancelled", "expired", "personal", "team", "topup", "paid"];
const tokenStates = new Map();

const siteSettings = {
    siteName: "HMaigc",
    logoUrl: "",
    footerCopyright: "",
    icpRegistrationNumber: "",
    icpRegistrationUrl: "",
    publicSecurityRegistrationNumber: "",
    publicSecurityRegistrationUrl: "",
    userAgreement: "",
    privacyPolicy: "",
    homeBannerEnabled: false,
    homeBannerLabel: "",
    homeBannerText: "",
    homeBannerPrimaryActionLabel: "",
    homeBannerPrimaryActionUrl: "",
    homeBannerSecondaryActionLabel: "",
    homeBannerSecondaryActionUrl: "",
    homeBannerFrequency: "always",
    marketingPopupEnabled: false,
    marketingPopupImageUrl: "",
    marketingPopupTitle: "",
    marketingPopupDescription: "",
    marketingPopupActionLabel: "",
    marketingPopupActionUrl: "",
    marketingPopupFrequency: "once",
    updatedBy: "",
    createdAt: "",
    updatedAt: "",
};

function envelope(data, msg = "ok") {
    return { code: 0, data, msg };
}

function checkoutHeaders(statusCode) {
    return {
        "Cache-Control": "public, max-age=86400",
        "Content-Type": "application/json; charset=utf-8",
        Pragma: "cache",
        "Referrer-Policy": "unsafe-url",
        "X-Fixture-Status": String(statusCode),
    };
}

function sendJSON(response, statusCode, body, sensitive = false) {
    response.writeHead(statusCode, sensitive ? checkoutHeaders(statusCode) : { "Content-Type": "application/json; charset=utf-8" });
    response.end(JSON.stringify(body));
}

function scenarioForToken(token) {
    return scenarioNames.find((name) => token.startsWith(`gate-${name}-`)) ?? null;
}

function stateForToken(token) {
    let state = tokenStates.get(token);
    if (!state) {
        state = { firstCheckoutAt: Date.now(), activeTransaction: null };
        tokenStates.set(token, state);
    }
    return state;
}

function membershipSummary(audience) {
    if (audience === "team") {
        return {
            audience: "team",
            code: "team-flagship",
            name: "旗舰团队会员",
            tier: "flagship",
            billingCycle: "year",
            seats: 3,
            actualPriceCents: 2_399_700,
            originalPriceCents: 2_999_700,
            creditsPerPeriod: 32_800_000,
            totalCreditsPerPeriod: mutation === "team-total" ? 98_300_000 : 98_400_000,
        };
    }
    return {
        audience: "personal",
        code: "creator-flagship",
        name: "旗舰创作会员",
        tier: "flagship",
        billingCycle: "month",
        seats: 1,
        actualPriceCents: 129_900,
        originalPriceCents: 139_900,
        creditsPerPeriod: 32_800_000,
        totalCreditsPerPeriod: 32_800_000,
    };
}

function activeTransaction(provider, now) {
    return {
        provider,
        status: "pending",
        codeUrl: `https://qr.invalid/${provider}/checkout-gate-code`,
        expiresAt: new Date(now + 10 * 60_000).toISOString(),
    };
}

function checkoutFor(scenario, token) {
    const now = Date.now();
    const state = stateForToken(token);
    const orderStatus = scenario === "paid" ? "paid" : scenario === "cancelled" ? "cancelled" : "pending";
    const checkoutStatus = scenario === "paid" ? "consumed" : scenario === "expired" ? "expired" : "active";
    const expiresAt = new Date(now + (scenario === "expired" ? -60_000 : 15 * 60_000)).toISOString();
    const base = {
        orderNumber: `GATE-${scenario.toUpperCase()}`,
        orderStatus,
        checkoutStatus,
        currency: "CNY",
        serverNow: new Date(now).toISOString(),
        expiresAt,
        providers: ["wechat", "alipay"],
    };

    if ((scenario === "active-qr" || scenario === "poll-failure") && !state.activeTransaction) {
        state.activeTransaction = activeTransaction("wechat", now);
    }
    if (state.activeTransaction) base.activeTransaction = state.activeTransaction;

    if (scenario === "topup") {
        return {
            ...base,
            orderType: "credit_topup",
            creditTopupSummary: {
                actualPriceCents: 19_900,
                totalMicrocredits: 500_000_000,
            },
        };
    }
    return {
        ...base,
        orderType: "membership",
        membershipSummary: membershipSummary(scenario === "team" ? "team" : "personal"),
    };
}

async function readJSON(request) {
    const chunks = [];
    let size = 0;
    for await (const chunk of request) {
        size += chunk.length;
        if (size > 8_192) throw new Error("fixture 请求体过大");
        chunks.push(chunk);
    }
    return JSON.parse(Buffer.concat(chunks).toString("utf8"));
}

const server = http.createServer(async (request, response) => {
    try {
        const url = new URL(request.url ?? "/", "http://backend");
        if (request.method === "GET" && url.pathname === "/api/public/site") {
            sendJSON(response, 200, envelope(siteSettings));
            return;
        }
        if (request.method === "GET" && url.pathname === "/api/auth/session") {
            sendJSON(response, 200, envelope({ user: null }));
            return;
        }

        const transactionMatch = url.pathname.match(/^\/api\/payments\/checkout\/([^/]+)\/transactions$/u);
        if (request.method === "POST" && transactionMatch) {
            const token = decodeURIComponent(transactionMatch[1]);
            const scenario = scenarioForToken(token);
            if (!scenario) {
                sendJSON(response, 404, { code: 404, data: null, msg: "收银台不存在" }, true);
                return;
            }
            const input = await readJSON(request);
            if (input?.provider !== "wechat" && input?.provider !== "alipay") {
                sendJSON(response, 400, { code: 400, data: null, msg: "支付渠道无效" }, true);
                return;
            }
            if (scenario === "provider-failure") {
                sendJSON(response, 503, { code: 503, data: null, msg: "支付渠道暂不可用，请稍后重试" }, true);
                return;
            }
            const state = stateForToken(token);
            state.activeTransaction ??= activeTransaction(input.provider, Date.now());
            sendJSON(response, 200, envelope(state.activeTransaction), true);
            return;
        }

        const checkoutMatch = url.pathname.match(/^\/api\/payments\/checkout\/([^/]+)$/u);
        if (request.method === "GET" && checkoutMatch) {
            const token = decodeURIComponent(checkoutMatch[1]);
            const scenario = scenarioForToken(token);
            if (!scenario) {
                sendJSON(response, 404, { code: 404, data: null, msg: "收银台不存在" }, true);
                return;
            }
            const state = stateForToken(token);
            if (scenario === "poll-failure" && Date.now() - state.firstCheckoutAt >= 2_200) {
                sendJSON(response, 503, { code: 503, data: null, msg: "订单状态刷新失败，请重试" }, true);
                return;
            }
            sendJSON(response, 200, envelope(checkoutFor(scenario, token)), true);
            return;
        }

        sendJSON(response, 404, { code: 404, data: null, msg: "fixture 路由不存在" });
    } catch (error) {
        sendJSON(response, 500, { code: 500, data: null, msg: error instanceof Error ? error.message : "fixture 失败" }, true);
    }
});

server.listen(port, "0.0.0.0");

function close() {
    server.close((error) => {
        if (error) process.exitCode = 1;
    });
}

process.on("SIGINT", close);
process.on("SIGTERM", close);
