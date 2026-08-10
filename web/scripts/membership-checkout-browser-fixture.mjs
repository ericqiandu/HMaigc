import http from "node:http";

const supportedMutations = new Set(["", "team-total", "membership-single-provider-double"]);
const mutation = (process.env.HMAIGC_CHECKOUT_GATE_MUTATION ?? "").trim();
if (!supportedMutations.has(mutation)) {
    throw new Error(`未知收银台门禁 mutation: ${mutation}`);
}
if (!/^22\.12\./u.test(process.versions.node)) {
    throw new Error(`收银台 fixture 必须运行在 Node 22.12，当前为 ${process.versions.node}`);
}

const port = Number(process.env.PORT ?? "8080");
if (!Number.isSafeInteger(port) || port < 1 || port > 65535) throw new Error("fixture PORT 无效");

const scenarioNames = ["membership-personal", "membership-team", "provider-failure", "poll-failure", "active-qr", "cancelled", "expired", "personal", "team", "topup", "paid"];
const tokenStates = new Map();
const membershipOrdersByKey = new Map();
const membershipOrdersByID = new Map();

const fixtureUser = {
    id: "gate-user",
    username: "checkout-gate",
    email: "checkout-gate@example.invalid",
    displayName: "付款门禁用户",
    role: "user",
    status: "active",
};

const personalPlan = {
    id: "plan-creator-flagship-year",
    code: "creator-flagship-year",
    name: "豪华版VIP",
    tier: "flagship",
    audience: "personal",
    billingCycle: "year",
    priceCents: 699_900,
    originalPriceCents: 1_499_900,
    currency: "CNY",
    creditsPerPeriod: 32_800_000,
    imageConcurrency: 8,
    videoConcurrency: 4,
    unlimitedTaskQueue: true,
    teamStorageBytes: 0,
    sharedAssetsEnabled: false,
    projectPermissionsEnabled: false,
    invoicingEnabled: true,
    commercialUseEnabled: true,
    topupDiscountBasisPoints: 4_700,
    minSeats: 1,
    maxSeats: 1,
    benefitsJson: "[]",
    benefits: [],
    enabled: true,
    sortOrder: 1,
    createdAt: "2026-08-10T00:00:00.000Z",
    updatedAt: "2026-08-10T00:00:00.000Z",
};

const teamPlan = {
    ...personalPlan,
    id: "plan-team-flagship-year",
    code: "team-flagship-year",
    name: "豪华版VIP",
    audience: "team",
    priceCents: 799_900,
    originalPriceCents: 1_699_900,
    creditsPerPeriod: 32_800_000,
    teamStorageBytes: 1_000_000_000,
    sharedAssetsEnabled: true,
    projectPermissionsEnabled: true,
    minSeats: 2,
    maxSeats: 20,
    sortOrder: 2,
};

const fixtureTeam = {
    id: "gate-team",
    ownerUserId: fixtureUser.id,
    name: "星河工作室",
    status: "active",
    createdAt: "2026-08-10T00:00:00.000Z",
    updatedAt: "2026-08-10T00:00:00.000Z",
};

const storefront = {
    presentation: {
        promotion: { enabled: false, title: "", subtitle: "", subtitleHighlight: "", endsAt: "" },
        copy: {
            creatorTab: "创作会员",
            teamTab: "团队会员",
            yearCycle: "连续包年",
            monthCycle: "连续包月",
            creditStore: "积分超市",
            activityHeading: "限时活动",
            exclusiveHeading: "独家功能",
            generationHeading: "生成能力",
            faqHeading: "常见问题",
        },
        activities: [],
        commonFeatures: [],
        exclusiveFeatures: [],
        planHighlights: [{ tier: "flagship", images: "100 张图片", videos: "20 个视频" }],
        generationColumns: [],
        generationSections: [],
        generationFootnote: "",
        membershipNotes: [],
        faqs: [],
    },
    plans: [personalPlan, teamPlan],
    serverNow: "2026-08-10T08:00:00.000Z",
    updatedAt: "2026-08-10T08:00:00.000Z",
};

function membershipOrder(plan, seats, teamId, id = "gate-membership-order") {
    return {
        id,
        orderNumber: "M202608100001",
        userId: fixtureUser.id,
        teamId: teamId || undefined,
        planId: plan.id,
        seats,
        unitPriceCents: plan.priceCents,
        totalPriceCents: plan.priceCents * seats,
        currency: plan.currency,
        status: "pending",
        planSnapshotJson: "{}",
        paymentProvider: "",
        providerTradeNo: "",
        createdAt: "2026-08-10T08:00:00.000Z",
        updatedAt: "2026-08-10T08:00:00.000Z",
    };
}

const membershipOverview = {
    entitlement: {
        planId: "free",
        planName: "免费版",
        tier: "free",
        audience: "personal",
        isActiveMember: false,
        imageConcurrency: 1,
        videoConcurrency: 1,
        topupDiscountBasisPoints: 10_000,
        unlimitedTaskQueue: false,
        teamStorageBytes: 0,
        sharedAssetsEnabled: false,
        projectPermissionsEnabled: false,
        invoicingEnabled: false,
        commercialUseEnabled: false,
    },
    orders: [],
    teams: [fixtureTeam],
};

const historicalOrder = membershipOrder(personalPlan, 1, "", "gate-history-order");
membershipOverview.orders.push(historicalOrder);
membershipOrdersByID.set(historicalOrder.id, historicalOrder);

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
    membershipAgreement: "",
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
        state = { firstCheckoutAt: Date.now(), activeTransaction: null, transactionCount: 0 };
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
        providers: scenario === "membership-personal" || scenario === "membership-team" ? (mutation === "membership-single-provider-double" && scenario === "membership-personal" ? ["wechat", "alipay"] : ["wechat"]) : ["wechat", "alipay"],
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
        membershipSummary:
            scenario === "membership-personal"
                ? {
                      audience: "personal",
                      code: personalPlan.code,
                      name: personalPlan.name,
                      tier: personalPlan.tier,
                      billingCycle: "year",
                      seats: 1,
                      actualPriceCents: personalPlan.priceCents,
                      originalPriceCents: personalPlan.originalPriceCents,
                      creditsPerPeriod: personalPlan.creditsPerPeriod,
                      totalCreditsPerPeriod: personalPlan.creditsPerPeriod,
                  }
                : scenario === "membership-team"
                  ? {
                        audience: "team",
                        code: teamPlan.code,
                        name: teamPlan.name,
                        tier: teamPlan.tier,
                        billingCycle: "year",
                        seats: 2,
                        actualPriceCents: teamPlan.priceCents * 2,
                        originalPriceCents: teamPlan.originalPriceCents * 2,
                        creditsPerPeriod: teamPlan.creditsPerPeriod,
                        totalCreditsPerPeriod: teamPlan.creditsPerPeriod * 2,
                    }
                  : membershipSummary(scenario === "team" ? "team" : "personal"),
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
        if (request.method === "POST" && url.pathname === "/api/__checkout-fixture/membership-agreement") {
            const input = await readJSON(request);
            if (typeof input?.published !== "boolean") {
                sendJSON(response, 400, { code: 400, data: null, msg: "会员协议 fixture 状态无效" });
                return;
            }
            siteSettings.membershipAgreement = input.published ? "<h2>会员购买规则</h2><p>这是浏览器门禁已发布的 HMaigc 会员服务协议。</p>" : "";
            sendJSON(response, 200, envelope({ published: input.published }));
            return;
        }
        if (request.method === "GET" && url.pathname === "/api/public/site") {
            sendJSON(response, 200, envelope(siteSettings));
            return;
        }
        if (request.method === "GET" && url.pathname === "/api/auth/session") {
            sendJSON(response, 200, envelope({ user: fixtureUser, systemChannels: [], runtimeLimits: { activeTaskLimit: 5, resourceUploadMB: 50, sessionUploadMB: 32 } }));
            return;
        }
        if (request.method === "GET" && url.pathname === "/api/assets") {
            sendJSON(response, 200, envelope({ assets: [] }));
            return;
        }
        if (request.method === "GET" && url.pathname === "/api/canvas-projects") {
            sendJSON(response, 200, envelope({ projects: [] }));
            return;
        }
        if (request.method === "GET" && url.pathname === "/api/membership/storefront") {
            sendJSON(response, 200, envelope(storefront));
            return;
        }
        if (request.method === "GET" && url.pathname === "/api/membership") {
            sendJSON(response, 200, envelope(membershipOverview));
            return;
        }
        if (request.method === "GET" && url.pathname === "/api/membership/invoices") {
            sendJSON(response, 200, envelope({ items: [] }));
            return;
        }
        if (request.method === "POST" && url.pathname === "/api/membership/orders") {
            const key = String(request.headers["idempotency-key"] ?? "").trim();
            if (!key) {
                sendJSON(response, 400, { code: 400, data: null, msg: "缺少 Idempotency-Key" });
                return;
            }
            const input = await readJSON(request);
            const selectedPlan = input?.planId === personalPlan.id ? personalPlan : input?.planId === teamPlan.id ? teamPlan : null;
            const expectedSeats = selectedPlan?.audience === "team" ? 2 : 1;
            const expectedTeamID = selectedPlan?.audience === "team" ? fixtureTeam.id : "";
            if (!selectedPlan || input?.seats !== expectedSeats || input?.teamId !== expectedTeamID) {
                sendJSON(response, 400, { code: 400, data: null, msg: "会员订单参数错误" });
                return;
            }
            const fingerprint = JSON.stringify(input);
            const existing = membershipOrdersByKey.get(key);
            if (existing && existing.fingerprint !== fingerprint) {
                sendJSON(response, 409, { code: 409, data: null, msg: "幂等请求内容冲突" });
                return;
            }
            const order = existing?.order ?? membershipOrder(selectedPlan, expectedSeats, expectedTeamID, `gate-membership-order-${membershipOrdersByKey.size + 1}`);
            membershipOrdersByKey.set(key, { fingerprint, order });
            membershipOrdersByID.set(order.id, order);
            sendJSON(response, 200, envelope(order));
            return;
        }
        const membershipCheckoutMatch = url.pathname.match(/^\/api\/membership\/orders\/([^/]+)\/checkout$/u);
        if (request.method === "POST" && membershipCheckoutMatch) {
            const order = membershipOrdersByID.get(decodeURIComponent(membershipCheckoutMatch[1]));
            if (!order) {
                sendJSON(response, 404, { code: 404, data: null, msg: "会员订单不存在" });
                return;
            }
            const prefix = order.planId === teamPlan.id ? "gate-membership-team" : "gate-membership-personal";
            const token = `${prefix}-${order.id}`;
            sendJSON(response, 200, envelope({ checkoutUrl: `/pay/${token}`, expiresAt: "2026-08-10T08:15:00.000Z" }));
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
            state.transactionCount += 1;
            if (state.transactionCount > 1) {
                sendJSON(response, 409, { code: 409, data: null, msg: "重复创建支付交易" }, true);
                return;
            }
            if (scenario === "membership-personal" || scenario === "membership-team") {
                await new Promise((resolve) => setTimeout(resolve, 600));
            }
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
