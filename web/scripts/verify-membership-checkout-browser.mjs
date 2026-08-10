import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { randomBytes } from "node:crypto";
import { access, cp, copyFile, mkdtemp, rm } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";

import puppeteer, { PUPPETEER_REVISIONS } from "puppeteer-core";

import {
    EXPECTED_CASE_COUNT,
    accessibilityText,
    assertCheckoutLayout,
    assertMembershipDialogLayout,
    assertMembershipSetupDialogLayout,
    assertCheckoutSecurityHeaders,
    assertKeyboardFocus,
    assertNoSensitivePresentation,
    assertProductionMediaReads,
    assertSmallTextContrast,
    assertVisibleControlTargets,
    waitForStableMembershipDialog,
} from "./membership-checkout-browser-assertions.mjs";

const webRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const repositoryRoot = path.resolve(webRoot, "..");
const fixtureScript = path.join(webRoot, "scripts", "membership-checkout-browser-fixture.mjs");
const distDirectory = path.join(webRoot, "dist");
const chromeExecutable = (process.env.HMAIGC_CHROMIUM_EXECUTABLE ?? "").trim();
const mutation = (process.env.HMAIGC_CHECKOUT_GATE_MUTATION ?? "").trim();
const supportedMutations = new Set(["", "team-total", "membership-single-provider-double"]);

if (!supportedMutations.has(mutation)) throw new Error(`未知收银台门禁 mutation: ${mutation}`);
if (!chromeExecutable) throw new Error("缺少 HMAIGC_CHROMIUM_EXECUTABLE，收银台 Chromium 门禁禁止跳过");

const scenarios = ["personal", "team", "topup", "active-qr", "paid", "expired", "cancelled", "poll-failure", "provider-failure"];
const viewports = [
    { name: "desktop", width: 1440, height: 900 },
    { name: "tablet", width: 768, height: 1024 },
    { name: "mobile", width: 390, height: 844 },
];
const themes = ["light", "dark"];
const membershipDialogStates = ["membership-personal-creating-dialog", "membership-personal-order-failure-dialog", "membership-personal-checkout-failure-dialog", "membership-personal-active-qr-dialog"];

async function setMembershipDialogFixtureState(baseURL, state, label) {
    const response = await fetch(`${baseURL}/api/__checkout-fixture/membership-dialog-state`, {
        body: JSON.stringify({ state }),
        headers: { "Content-Type": "application/json" },
        method: "POST",
    });
    assert.equal(response.status, 200, `${label}: 无法设置会员付款弹窗 fixture 状态`);
}

async function waitForMembershipDialogAnimation() {
    await new Promise((resolve) => setTimeout(resolve, 500));
}

async function readMembershipFacts(page, label) {
    const facts = await page.evaluate(() => {
        const root = document.querySelector(".membership-payment-dialog .membership-order-facts");
        const orderNumber = root?.querySelector(".membership-checkout-order-number");
        const amount = root?.querySelector(".membership-checkout-total-price");
        if (!(root instanceof HTMLElement) || !(orderNumber instanceof HTMLElement) || !(amount instanceof HTMLElement)) {
            throw new Error("会员冻结事实节点不完整");
        }
        const normalize = (value) => value.replace(/\s+/gu, " ").trim();
        return { amount: normalize(amount.innerText), orderNumber: normalize(orderNumber.innerText), text: normalize(root.innerText) };
    });
    assert.equal(facts.orderNumber, "订单 M202608100001", `${label}: 冻结订单号没有显示`);
    assert.equal(facts.amount, "¥1,299", `${label}: 冻结订单金额没有显示`);
    assert.ok(!facts.text.includes("豪华版VIP"), `${label}: mutable current plan 名称泄漏到冻结事实`);
    assert.ok(!facts.text.includes("¥6,999") && !facts.text.includes("¥14,999"), `${label}: mutable current plan 金额泄漏到冻结事实`);
    return facts;
}

async function runMembershipSetupDialogCase(browser, baseURL, theme, viewport, state) {
    const label = `${viewport.name}/${theme}/${state}`;
    await setMembershipDialogFixtureState(baseURL, state, label);
    const page = await browser.newPage();
    try {
        await page.setViewport({ width: viewport.width, height: viewport.height, deviceScaleFactor: 1 });
        await page.evaluateOnNewDocument((selectedTheme) => {
            localStorage.setItem("infinite-canvas:theme_store", JSON.stringify({ state: { theme: selectedTheme }, version: 0 }));
        }, theme);
        const navigation = await page.goto(`${baseURL}/membership`, { waitUntil: "domcontentloaded", timeout: 30_000 });
        assert.ok(navigation, `${label}: 会员页导航没有响应`);
        await page.waitForSelector('[data-plan-code="creator-flagship-year"] .membership-storefront-plan-action', { timeout: 15_000 });
        await page.click('[data-plan-code="creator-flagship-year"] .membership-storefront-plan-action');
        if (state === "membership-personal-order-failure-dialog") {
            await page.waitForFunction(() => document.body.innerText.includes("会员订单服务暂时不可用"), { timeout: 15_000 });
            assert.ok(await page.$(".membership-payment-setup-error .membership-payment-setup-primary"), `${label}: 创建失败缺少重试入口`);
        } else if (state === "membership-personal-checkout-failure-dialog") {
            await page.waitForFunction(() => document.body.innerText.includes("支付收银台暂时不可用"), { timeout: 15_000 });
            assert.ok(await page.$(".membership-payment-setup-error .membership-payment-setup-primary"), `${label}: 收银台恢复失败缺少重试入口`);
        } else if (state === "membership-personal-creating-dialog") {
            await page.waitForSelector(".membership-payment-setup-progress", { timeout: 15_000 });
        } else {
            await page.waitForSelector(".membership-payment-dialog .payment-checkout-qr-code", { timeout: 15_000 });
            await waitForMembershipDialogAnimation();
            const expectedDialogWidth = Math.min(766, viewport.width - (viewport.width <= 767 ? 32 : 48));
            await waitForStableMembershipDialog(page, expectedDialogWidth, label);
            await assertMembershipDialogLayout(page, viewport, label);
            const frozenOwners = await page.evaluate(() => {
                const shell = document.querySelector(".membership-payment-dialog .payment-checkout-shell.is-dialog");
                const order = shell?.querySelector(":scope > .payment-checkout-order-surface");
                const payment = shell?.querySelector(":scope > .payment-checkout-payment-surface");
                const facts = order?.querySelector(":scope > .membership-order-facts");
                if (![shell, order, payment, facts].every((node) => node instanceof HTMLElement)) throw new Error("活跃付款码缺少收银台结构");
                return {
                    facts: facts.className,
                    order: order.className,
                    payment: payment.className,
                    shell: shell.className,
                };
            });
            assert.deepEqual(
                frozenOwners,
                {
                    facts: "membership-order-facts membership-checkout-summary",
                    order: "payment-checkout-order-surface",
                    payment: "payment-checkout-payment-surface",
                    shell: "payment-checkout-shell is-dialog",
                },
                `${label}: 活跃付款码未使用冻结收银台结构`,
            );
            return { facts: await readMembershipFacts(page, label), owners: frozenOwners };
        }
        await waitForMembershipDialogAnimation();
        const expectedDialogWidth = Math.min(766, viewport.width - (viewport.width <= 767 ? 32 : 48));
        await waitForStableMembershipDialog(page, expectedDialogWidth, label);
        const owners = await assertMembershipSetupDialogLayout(page, viewport, label);
        if (state === "membership-personal-checkout-failure-dialog") {
            return { facts: await readMembershipFacts(page, label), owners };
        }
        return { facts: null, owners };
    } finally {
        await page.close();
    }
}

async function runMembershipDialogCase(browser, baseURL, theme, viewport, audience) {
    const label = `${viewport.name}/${theme}/membership-${audience}-dialog`;
    const page = await browser.newPage();
    try {
        await page.setViewport({ width: viewport.width, height: viewport.height, deviceScaleFactor: 1 });
        await page.evaluateOnNewDocument((selectedTheme) => {
            localStorage.setItem("infinite-canvas:theme_store", JSON.stringify({ state: { theme: selectedTheme }, version: 0 }));
        }, theme);
        const navigation = await page.goto(`${baseURL}/membership`, { waitUntil: "domcontentloaded", timeout: 30_000 });
        assert.ok(navigation, `${label}: 会员页导航没有响应`);
        if (audience === "history") {
            await page.waitForSelector(".membership-orders-summary", { timeout: 15_000 });
            await page.click(".membership-orders-summary");
            await page.waitForSelector(".membership-pay-order", { timeout: 15_000, visible: true });
            await page.click(".membership-pay-order");
        } else if (audience === "team") {
            await page.waitForSelector('.membership-storefront-audience-tab[aria-selected="false"]', { timeout: 15_000 });
            await page.click('.membership-storefront-audience-tab[aria-selected="false"]');
        }
        if (audience !== "history") {
            const planCode = audience === "team" ? "team-flagship-year" : "creator-flagship-year";
            await page.waitForSelector(`[data-plan-code="${planCode}"] .membership-storefront-plan-action`, { timeout: 15_000 });
            await page.click(`[data-plan-code="${planCode}"] .membership-storefront-plan-action`);
            if (audience === "team") {
                await page.waitForSelector(".membership-payment-setup-primary", { timeout: 15_000 });
                await page.click(".membership-payment-setup-primary");
            }
        }
        try {
            await page.waitForSelector(".membership-payment-dialog .payment-checkout-shell.is-dialog", { timeout: 15_000 });
        } catch (error) {
            const bodyText = await page.$eval("body", (node) => node.innerText);
            throw new Error(`${label}: 收银台未打开\n${bodyText}`, { cause: error });
        }
        if (audience !== "history") {
            await page.waitForFunction(() => document.querySelector(".membership-payment-dialog .payment-checkout-action")?.textContent?.includes("正在生成"), { timeout: 5_000 });
            await page.waitForFunction(() => document.querySelector(".membership-payment-dialog .ant-modal-close") === null, { timeout: 5_000 });
            assert.equal(await page.$(".membership-payment-dialog .ant-modal-close"), null, `${label}: 创建支付交易期间仍可关闭弹窗`);
            await page.keyboard.press("Escape");
            assert.ok(await page.$(".membership-payment-dialog .payment-checkout-shell.is-dialog"), `${label}: 写入期间 Escape 关闭了弹窗`);
        }
        try {
            await page.waitForSelector(".membership-payment-dialog .payment-checkout-qr-code", { timeout: 15_000 });
        } catch (error) {
            const bodyText = await page.$eval("body", (node) => node.innerText);
            throw new Error(`${label}: 二维码未生成\n${bodyText}`, { cause: error });
        }
        const expectedDialogWidth = Math.min(766, viewport.width - (viewport.width <= 767 ? 32 : 48));
        await waitForStableMembershipDialog(page, expectedDialogWidth, label);
        await page.waitForFunction(
            () => {
                const close = document.querySelector(".membership-payment-dialog .ant-modal-close");
                if (!(close instanceof HTMLElement)) return false;
                const rect = close.getBoundingClientRect();
                return rect.width >= 44 && rect.height >= 44;
            },
            { timeout: 5_000 },
        );
        assert.equal(new URL(page.url()).pathname, "/membership", `${label}: 生成二维码后离开了会员页`);
        assert.equal(await page.$(".membership-order-modal"), null, `${label}: 旧确认购买弹窗仍存在`);
        await assertMembershipDialogLayout(page, viewport, label);
        await assertQR(page, "微信支付", label);
        await assertMembershipAgreementLink(page, label);
    } finally {
        await page.close();
    }
}

async function runMembershipAgreementCase(browser, baseURL, theme, viewport, published) {
    const state = published ? "published" : "unpublished";
    const label = `${viewport.name}/${theme}/membership-agreement-${state}`;
    const control = await fetch(`${baseURL}/api/__checkout-fixture/membership-agreement`, {
        body: JSON.stringify({ published }),
        headers: { "Content-Type": "application/json" },
        method: "POST",
    });
    assert.equal(control.status, 200, `${label}: 无法设置会员协议 fixture 状态`);

    const page = await browser.newPage();
    try {
        await page.setViewport({ width: viewport.width, height: viewport.height, deviceScaleFactor: 1 });
        await page.evaluateOnNewDocument((selectedTheme) => {
            localStorage.setItem("infinite-canvas:theme_store", JSON.stringify({ state: { theme: selectedTheme }, version: 0 }));
        }, theme);
        const token = `gate-active-qr-agreement-${state}-${randomBytes(6).toString("hex")}`;
        const navigation = await page.goto(`${baseURL}/pay/${encodeURIComponent(token)}`, { waitUntil: "domcontentloaded", timeout: 30_000 });
        assert.ok(navigation, `${label}: 付款页导航没有响应`);
        assertCheckoutSecurityHeaders(navigation.headers(), `${label} /pay`);
        await waitForCheckout(page);
        await page.waitForSelector(".payment-checkout-qr-code", { timeout: 15_000 });
        await assertQR(page, "微信支付", label);
        await assertMembershipAgreementLink(page, label);

        await page.goto(`${baseURL}/legal/membership-agreement`, { waitUntil: "domcontentloaded", timeout: 30_000 });
        await page.waitForSelector(".legal-document-page", { timeout: 15_000 });
        await page.waitForFunction((expectsPublished) => (expectsPublished ? Boolean(document.querySelector(".legal-document-body")) : Boolean(document.querySelector(".legal-document-empty"))), { timeout: 15_000 }, published);
        const bodyText = await page.$eval("body", (node) => node.innerText);
        requireText(bodyText, ["HMaigc会员服务协议"], label);
        if (published) {
            requireText(bodyText, ["会员购买规则", "浏览器门禁已发布"], label);
            assert.ok(!bodyText.includes("会员服务协议尚未发布"), `${label}: 已发布协议误显示未发布状态`);
        } else {
            requireText(bodyText, ["会员服务协议尚未发布"], label);
            assert.ok(!bodyText.includes("法律内容加载失败"), `${label}: 未发布协议被误报为加载失败`);
        }
    } finally {
        await page.close();
    }
}

function command(commandName, args, { allowFailure = false, timeoutMs = 300_000 } = {}) {
    return new Promise((resolve, reject) => {
        const child = spawn(commandName, args, { cwd: repositoryRoot, env: process.env, windowsHide: true });
        const stdout = [];
        const stderr = [];
        child.stdout.on("data", (chunk) => stdout.push(chunk));
        child.stderr.on("data", (chunk) => stderr.push(chunk));
        const timer = setTimeout(() => child.kill("SIGKILL"), timeoutMs);
        child.on("error", (error) => {
            clearTimeout(timer);
            reject(error);
        });
        child.on("close", (code, signal) => {
            clearTimeout(timer);
            const result = { code: code ?? -1, signal, stderr: Buffer.concat(stderr).toString("utf8"), stdout: Buffer.concat(stdout).toString("utf8") };
            if (result.code === 0 || allowFailure) {
                resolve(result);
                return;
            }
            reject(new Error(`${commandName} ${args.join(" ")} 失败（exit ${result.code}${signal ? `, signal ${signal}` : ""}）\n${result.stdout}${result.stderr}`));
        });
    });
}

function requireText(bodyText, expected, label) {
    for (const value of expected) assert.ok(bodyText.includes(value), `${label}: 缺少真实展示事实“${value}”`);
}

async function waitForCheckout(page) {
    await page.waitForSelector(".payment-checkout-shell", { timeout: 15_000 });
    await page.waitForFunction(() => Boolean(document.querySelector("#payment-checkout-title") || document.querySelector(".payment-checkout-initial-error")), { timeout: 15_000 });
}

async function assertQR(page, providerLabel, label) {
    const qr = await page.$eval(".payment-checkout-qr-code", (node) => {
        const svg = node.querySelector("svg");
        if (!(svg instanceof SVGSVGElement)) throw new Error("二维码 SVG 不存在");
        const viewBox = svg.viewBox.baseVal;
        const darkGraphics = Array.from(svg.querySelectorAll("path, rect"))
            .filter((item) => {
                const fill = getComputedStyle(item).fill;
                return fill !== "rgb(255, 255, 255)" && fill !== "none";
            })
            .map((item) => item.getBBox())
            .filter((box) => box.width > 0 && box.height > 0);
        if (darkGraphics.length === 0) throw new Error("二维码没有可扫码图形");
        const left = Math.min(...darkGraphics.map((box) => box.x));
        const top = Math.min(...darkGraphics.map((box) => box.y));
        const right = Math.max(...darkGraphics.map((box) => box.x + box.width));
        const bottom = Math.max(...darkGraphics.map((box) => box.y + box.height));
        const rect = svg.getBoundingClientRect();
        const surfaceRect = node.getBoundingClientRect();
        const fills = [...new Set(Array.from(svg.querySelectorAll("path, rect"), (item) => getComputedStyle(item).fill))];
        return {
            ariaLabel: node.getAttribute("aria-label") ?? "",
            role: node.getAttribute("role"),
            width: rect.width,
            height: rect.height,
            surfaceWidth: surfaceRect.width,
            surfaceHeight: surfaceRect.height,
            surfaceBackground: getComputedStyle(node).backgroundColor,
            fills,
            quietZone: Math.min(left - viewBox.x, top - viewBox.y, viewBox.x + viewBox.width - right, viewBox.y + viewBox.height - bottom),
        };
    });
    assert.equal(qr.role, "img", `${label}: 二维码必须暴露 img 语义`);
    assert.equal(qr.ariaLabel, `${providerLabel}付款二维码`, `${label}: 二维码可访问名称错误`);
    assert.ok(Math.abs(qr.width - 112) <= 1 && Math.abs(qr.height - 112) <= 1, `${label}: 二维码 SVG 必须为 112×112，实际 ${qr.width}×${qr.height}`);
    assert.ok(Math.abs(qr.surfaceWidth - 128) <= 1 && Math.abs(qr.surfaceHeight - 128) <= 1, `${label}: 二维码白底整体必须为 128×128，实际 ${qr.surfaceWidth}×${qr.surfaceHeight}`);
    assert.equal(qr.surfaceBackground, "rgb(255, 255, 255)", `${label}: 二维码静区背景必须保持白色`);
    assert.ok(qr.fills.includes("rgb(0, 0, 0)"), `${label}: 二维码前景必须保持黑色`);
    assert.ok(qr.fills.includes("rgb(255, 255, 255)"), `${label}: 二维码背景必须保持白色`);
    assert.ok(qr.quietZone >= 4, `${label}: 二维码静区不足 4 个 SVG 单位`);
}

async function assertMembershipAgreementLink(page, label) {
    await page.evaluate(() => {
        if (document.activeElement instanceof HTMLElement) document.activeElement.blur();
        document.body.tabIndex = -1;
        document.body.focus();
        document.body.removeAttribute("tabindex");
    });
    let agreementFocused = false;
    for (let step = 0; step < 20; step += 1) {
        await page.keyboard.press("Tab");
        agreementFocused = await page.evaluate(() => document.activeElement?.classList.contains("payment-checkout-agreement-link") ?? false);
        if (agreementFocused) break;
    }
    assert.equal(agreementFocused, true, `${label}: 键盘无法到达会员协议入口`);
    const agreement = await page.$eval(".payment-checkout-agreement-link", (node) => {
        if (!(node instanceof HTMLAnchorElement)) throw new Error("会员协议入口不是链接");
        const style = getComputedStyle(node);
        return {
            href: new URL(node.href).pathname,
            outlineStyle: style.outlineStyle,
            outlineWidth: Number.parseFloat(style.outlineWidth),
            rel: node.rel,
            target: node.target,
            text: node.textContent?.trim() ?? "",
        };
    });
    assert.deepEqual(
        agreement,
        {
            href: "/legal/membership-agreement",
            outlineStyle: "solid",
            outlineWidth: 2,
            rel: "noopener noreferrer",
            target: "_blank",
            text: "《HMaigc会员服务协议》",
        },
        `${label}: 会员协议入口契约错误`,
    );
}

async function exerciseScenario(page, scenario, label) {
    let bodyText = await page.$eval("body", (node) => node.innerText);
    requireText(bodyText, ["安全收银台"], label);

    if (scenario === "personal" || scenario === "active-qr" || scenario === "poll-failure" || scenario === "paid" || scenario === "expired" || scenario === "cancelled" || scenario === "provider-failure") {
        requireText(bodyText, ["旗舰创作会员", "按月购买", "32.8 积分/月", "¥1,399", "−¥100", "¥1,299", "到期不自动续费"], label);
    }
    if (scenario === "team") {
        requireText(bodyText, ["开通团队会员", "旗舰团队会员", "按年购买", "3 席位", "¥7,999/年", "32.8 积分/年/席位", "98.4 积分/年", "¥29,997", "−¥6,000", "¥23,997", "到期不自动续费"], label);
    }
    if (scenario === "topup") {
        requireText(bodyText, ["积分充值", "500 积分", "¥199"], label);
        assert.ok(!bodyText.includes("到期不自动续费"), `${label}: 积分充值不得伪造会员续费说明`);
    }

    if (scenario === "personal") {
        await page.click('.payment-checkout-provider-input[value="alipay"]');
        await page.click(".payment-checkout-action");
        await page.waitForSelector(".payment-checkout-qr-code", { timeout: 10_000 });
        await assertQR(page, "支付宝", label);
        await assertMembershipAgreementLink(page, label);
        assert.equal(await page.$(".payment-checkout-provider-fieldset"), null, `${label}: 生成付款码后仍重复显示渠道选择`);
    } else if (scenario === "active-qr" || scenario === "poll-failure") {
        await assertQR(page, "微信支付", label);
        await assertMembershipAgreementLink(page, label);
        assert.equal(await page.$(".payment-checkout-provider-fieldset"), null, `${label}: 恢复付款码后仍重复显示渠道选择`);
        if (scenario === "poll-failure") {
            await page.waitForFunction(() => Array.from(document.querySelectorAll('[role="alert"]')).some((node) => node.textContent?.includes("订单状态刷新失败，请重试")), { timeout: 8_000 });
            await assertQR(page, "微信支付", label);
            bodyText = await page.$eval("body", (node) => node.innerText);
            requireText(bodyText, ["订单状态刷新失败，请重试", "旗舰创作会员", "¥1,299"], label);
        }
    } else if (scenario === "team") {
        await page.click('.payment-checkout-provider-input[value="wechat"]');
        await page.click(".payment-checkout-action");
        await page.waitForSelector(".payment-checkout-qr-code", { timeout: 10_000 });
        await assertQR(page, "微信支付", label);
        await assertMembershipAgreementLink(page, label);
        assert.equal(await page.$(".payment-checkout-provider-fieldset"), null, `${label}: 团队付款码生成后仍重复显示渠道选择`);
    } else if (scenario === "topup") {
        await page.click('.payment-checkout-provider-input[value="alipay"]');
        await page.click(".payment-checkout-action");
        await page.waitForSelector(".payment-checkout-qr-code", { timeout: 10_000 });
        await assertQR(page, "支付宝", label);
        assert.equal(await page.$(".payment-checkout-agreement"), null, `${label}: 积分充值错误显示会员协议`);
    } else if (scenario === "provider-failure") {
        await page.click('.payment-checkout-provider-input[value="alipay"]');
        await page.click(".payment-checkout-action");
        await page.waitForFunction(() => Array.from(document.querySelectorAll('[role="alert"]')).some((node) => node.textContent?.includes("支付渠道暂不可用，请稍后重试")), { timeout: 10_000 });
        assert.equal(await page.$eval('.payment-checkout-provider-input[value="alipay"]', (node) => node.checked), true, `${label}: 渠道失败后选择未保留`);
        assert.equal(await page.$eval(".payment-checkout-provider-fieldset", (node) => node.disabled), false, `${label}: 渠道失败后错误锁定支付方式`);
    } else if (scenario === "paid") {
        requireText(bodyText, ["支付成功", "会员权益已激活", "查看到账结果"], label);
        assert.equal(await page.$(".payment-checkout-provider-fieldset"), null, `${label}: 已支付状态仍显示付款操作`);
    } else if (scenario === "expired") {
        requireText(bodyText, ["收银台已过期", "返回订单入口"], label);
        assert.equal(await page.$(".payment-checkout-provider-fieldset"), null, `${label}: 已过期状态仍显示付款操作`);
    } else if (scenario === "cancelled") {
        requireText(bodyText, ["订单已关闭", "返回订单入口"], label);
        assert.equal(await page.$(".payment-checkout-provider-fieldset"), null, `${label}: 已取消状态仍显示付款操作`);
    }
}

function assertExpectedHTTPFailures(failures, scenario, label) {
    if (scenario === "provider-failure") {
        assert.equal(failures.length, 1, `${label}: 渠道失败场景必须只有一次预期 503`);
        assert.equal(failures[0].status, 503, `${label}: 渠道失败状态码错误`);
        assert.equal(failures[0].method, "POST", `${label}: 渠道失败必须来自交易 POST`);
        assert.ok(failures[0].pathname.endsWith("/transactions"), `${label}: 渠道失败路径错误`);
        return;
    }
    if (scenario === "poll-failure") {
        assert.ok(failures.length >= 1, `${label}: 轮询失败场景没有观察到预期 503`);
        assert.ok(
            failures.every((failure) => failure.status === 503 && failure.method === "GET" && !failure.pathname.endsWith("/transactions")),
            `${label}: 轮询失败包含非预期 HTTP 错误`,
        );
        return;
    }
    assert.deepEqual(failures, [], `${label}: 出现非预期 HTTP 错误`);
}

async function runCase(browser, baseURL, testCase, tokens) {
    const { index, scenario, theme, viewport } = testCase;
    const label = `${viewport.name}/${theme}/${scenario}`;
    const token = `gate-${scenario}-${index}-${randomBytes(6).toString("hex")}`;
    tokens.push(token);
    const page = await browser.newPage();
    const consoleEntries = [];
    const pageErrors = [];
    const requestFailures = [];
    const httpFailures = [];
    const checkoutResponses = [];

    try {
        await page.setViewport({ width: viewport.width, height: viewport.height, deviceScaleFactor: 1 });
        await page.evaluateOnNewDocument((selectedTheme) => {
            localStorage.setItem("infinite-canvas:theme_store", JSON.stringify({ state: { theme: selectedTheme }, version: 0 }));
            window.__hmaigcCheckoutCSPViolations = [];
            document.addEventListener("securitypolicyviolation", (event) => {
                window.__hmaigcCheckoutCSPViolations.push({ blockedURI: event.blockedURI, directive: event.effectiveDirective });
            });
        }, theme);
        page.on("console", (message) => consoleEntries.push(`${message.type()}: ${message.text()}`));
        page.on("pageerror", (error) => pageErrors.push(error.message));
        page.on("requestfailed", (request) => requestFailures.push(`${request.method()} ${new URL(request.url()).pathname}: ${request.failure()?.errorText ?? "unknown"}`));
        page.on("response", (response) => {
            const url = new URL(response.url());
            if (url.pathname.includes("/api/payments/checkout/")) {
                checkoutResponses.push({ headers: response.headers(), method: response.request().method(), pathname: url.pathname, status: response.status() });
            }
            if (response.status() >= 400) httpFailures.push({ method: response.request().method(), pathname: url.pathname, status: response.status() });
        });

        const navigation = await page.goto(`${baseURL}/pay/${encodeURIComponent(token)}`, { waitUntil: "domcontentloaded", timeout: 30_000 });
        assert.ok(navigation, `${label}: 页面导航没有响应`);
        assert.equal(navigation.status(), 200, `${label}: 生产 Nginx 页面状态错误`);
        assertCheckoutSecurityHeaders(navigation.headers(), `${label} /pay`);
        await waitForCheckout(page);
        await assertProductionMediaReads(page, label);
        await exerciseScenario(page, scenario, label);

        assert.ok(
            checkoutResponses.some((response) => response.method === "GET" && response.status === 200),
            `${label}: 未观察到成功的结算 GET`,
        );
        for (const response of checkoutResponses) assertCheckoutSecurityHeaders(response.headers, `${label} ${response.method} ${response.status}`);
        assertExpectedHTTPFailures(httpFailures, scenario, label);

        const rootTheme = await page.evaluate(() => (document.documentElement.classList.contains("dark") ? "dark" : "light"));
        assert.equal(rootTheme, theme, `${label}: 实际主题与矩阵不一致`);
        await assertCheckoutLayout(page, viewport);
        await assertVisibleControlTargets(page, label);
        await assertKeyboardFocus(page, label);
        await assertSmallTextContrast(page, label);

        const cspViolations = await page.evaluate(() => window.__hmaigcCheckoutCSPViolations ?? []);
        assert.deepEqual(cspViolations, [], `${label}: 发生 CSP violation`);
        assert.deepEqual(pageErrors, [], `${label}: 页面脚本异常`);
        assert.deepEqual(requestFailures, [], `${label}: 浏览器请求失败`);
        const unexpectedConsole = consoleEntries.filter((entry) => entry.startsWith("error:") || entry.startsWith("warning:"));
        const expectedResourceError = "error: Failed to load resource: the server responded with a status of 503 (Service Unavailable)";
        if (scenario === "poll-failure" || scenario === "provider-failure") {
            assert.equal(unexpectedConsole.length, httpFailures.length, `${label}: 预期 HTTP 失败与浏览器控制台错误数量不一致`);
            assert.ok(
                unexpectedConsole.every((entry) => entry === expectedResourceError),
                `${label}: 控制台包含预期 503 之外的错误/警告`,
            );
        } else {
            assert.deepEqual(unexpectedConsole, [], `${label}: 控制台出现非预期错误/警告`);
        }

        const bodyText = await page.$eval("body", (node) => node.innerText);
        const accessibility = await accessibilityText(page);
        assertNoSensitivePresentation({
            accessibility,
            bodyText,
            consoleText: consoleEntries.join("\n"),
            forbidden: [token, "checkout-gate-code", "https://qr.invalid", "LibLib", "libtv", "连续包月", "次月续费", "随时取消", "teamName", "teamId", "userId", "planSnapshotJson", "codeUrl", "INTERNAL_SENTINEL"],
            label,
        });
    } finally {
        await page.close();
    }
}

async function waitForServer(baseURL) {
    let lastError = null;
    for (let attempt = 0; attempt < 60; attempt += 1) {
        try {
            const response = await fetch(`${baseURL}/api/public/site`, { redirect: "manual" });
            if (response.ok) return;
            lastError = new Error(`readiness HTTP ${response.status}`);
        } catch (error) {
            lastError = error;
        }
        await new Promise((resolve) => setTimeout(resolve, 250));
    }
    throw new Error(`生产 Nginx/fixture 未就绪: ${lastError instanceof Error ? lastError.message : "unknown"}`);
}

async function prepareContext(tempDirectory) {
    const releaseDirectory = path.join(tempDirectory, "release-web-dist");
    await cp(distDirectory, releaseDirectory, { recursive: true, errorOnExist: true, force: false });
    await copyFile(path.join(repositoryRoot, "Dockerfile.web-release"), path.join(tempDirectory, "Dockerfile.web-release"));
    await copyFile(path.join(repositoryRoot, "nginx.conf"), path.join(tempDirectory, "nginx.conf"));
}

async function cleanupResource(commandArgs, cleanupErrors) {
    try {
        const result = await command("docker", commandArgs, { allowFailure: true, timeoutMs: 60_000 });
        if (result.code !== 0 && !/(?:No such (?:container|image|network)|network .* not found)/iu.test(`${result.stdout}${result.stderr}`)) {
            cleanupErrors.push(new Error(`docker ${commandArgs.join(" ")} 清理失败\n${result.stdout}${result.stderr}`));
        }
    } catch (error) {
        cleanupErrors.push(error);
    }
}

async function main() {
    await access(chromeExecutable);
    await access(path.join(distDirectory, "index.html"));
    await access(fixtureScript);
    await command("docker", ["info", "--format", "{{.ServerVersion}}"], { timeoutMs: 30_000 });

    const caseMatrix = viewports.flatMap((viewport) => themes.flatMap((theme) => scenarios.map((scenario, matrixIndex) => ({ index: `${viewport.name}-${theme}-${matrixIndex}`, scenario, theme, viewport }))));
    assert.equal(caseMatrix.length + viewports.length * themes.length * 9, EXPECTED_CASE_COUNT, "收银台浏览器矩阵必须精确执行 108 个案例");

    const identifier = randomBytes(6).toString("hex");
    const resourcePrefix = `hmaigc-checkout-gate-${identifier}`;
    const fixtureContainer = `${resourcePrefix}-fixture`;
    const webContainer = `${resourcePrefix}-web`;
    const network = `${resourcePrefix}-network`;
    const image = `${resourcePrefix}:test`;
    const tempDirectory = await mkdtemp(path.join(os.tmpdir(), "hmaigc-checkout-gate-"));
    const tokens = [];
    const cleanupErrors = [];
    let browser = null;
    let primaryError = null;
    let webLogs = "";

    try {
        await prepareContext(tempDirectory);
        await command("docker", ["build", "--file", path.join(tempDirectory, "Dockerfile.web-release"), "--tag", image, tempDirectory], { timeoutMs: 600_000 });
        await command("docker", ["network", "create", network]);
        await command("docker", [
            "run",
            "--detach",
            "--rm",
            "--name",
            fixtureContainer,
            "--network",
            network,
            "--network-alias",
            "backend",
            "--env",
            `HMAIGC_CHECKOUT_GATE_MUTATION=${mutation}`,
            "--mount",
            `type=bind,source=${fixtureScript},target=/app/fixture.mjs,readonly`,
            "node:22.12.0-alpine",
            "node",
            "/app/fixture.mjs",
        ]);
        await command("docker", ["run", "--detach", "--rm", "--name", webContainer, "--network", network, "--publish", "127.0.0.1::3000", image]);
        const portResult = await command("docker", ["port", webContainer, "3000/tcp"]);
        const address = portResult.stdout.trim().split(/\r?\n/u)[0];
        const portMatch = address.match(/:(\d+)$/u);
        if (!portMatch) throw new Error(`无法解析生产 Nginx 随机端口: ${address}`);
        const baseURL = `http://127.0.0.1:${portMatch[1]}`;
        await waitForServer(baseURL);

        browser = await puppeteer.launch({
            executablePath: chromeExecutable,
            headless: true,
            args: ["--disable-dev-shm-usage", "--no-sandbox"],
        });
        const expectedChromeMajor = Number(PUPPETEER_REVISIONS.chrome.split(".")[0]);
        const version = await browser.version();
        const versionMatch = version.match(/\/(\d+)\./u);
        const actualChromeMajor = versionMatch ? Number(versionMatch[1]) : Number.NaN;
        assert.equal(actualChromeMajor, expectedChromeMajor, `Chromium major ${actualChromeMajor} 与 puppeteer-core 要求 ${expectedChromeMajor} 不一致`);

        let completed = 0;
        for (const viewport of viewports) {
            for (const theme of themes) {
                let expectedLeftOwners = null;
                let checkoutFailureFacts = null;
                for (const state of membershipDialogStates) {
                    const result = await runMembershipSetupDialogCase(browser, baseURL, theme, viewport, state);
                    const leftOwners = { facts: result.owners.facts, order: result.owners.order };
                    if (expectedLeftOwners === null) {
                        expectedLeftOwners = leftOwners;
                    } else {
                        assert.deepEqual(leftOwners, expectedLeftOwners, `${viewport.name}/${theme}/${state}: 左侧结构 owner 与其他付款态不一致`);
                    }
                    if (state === "membership-personal-checkout-failure-dialog") checkoutFailureFacts = result.facts;
                    if (state === "membership-personal-active-qr-dialog") {
                        assert.ok(checkoutFailureFacts, `${viewport.name}/${theme}/${state}: 缺少收银台失败的冻结事实`);
                        assert.deepEqual(result.facts, checkoutFailureFacts, `${viewport.name}/${theme}/${state}: 活跃付款码与收银台失败的冻结事实不一致`);
                    }
                    completed += 1;
                    process.stdout.write(`PASS ${completed}/${EXPECTED_CASE_COUNT} ${viewport.name}/${theme}/${state}\n`);
                }
                for (const audience of ["personal", "team", "history"]) {
                    await runMembershipDialogCase(browser, baseURL, theme, viewport, audience);
                    completed += 1;
                    process.stdout.write(`PASS ${completed}/${EXPECTED_CASE_COUNT} ${viewport.name}/${theme}/membership-${audience}-dialog\n`);
                }
                for (const published of [true, false]) {
                    await runMembershipAgreementCase(browser, baseURL, theme, viewport, published);
                    completed += 1;
                    process.stdout.write(`PASS ${completed}/${EXPECTED_CASE_COUNT} ${viewport.name}/${theme}/membership-agreement-${published ? "published" : "unpublished"}\n`);
                }
            }
        }
        for (const testCase of caseMatrix) {
            await runCase(browser, baseURL, testCase, tokens);
            completed += 1;
            process.stdout.write(`PASS ${completed}/${EXPECTED_CASE_COUNT} ${testCase.viewport.name}/${testCase.theme}/${testCase.scenario}\n`);
        }
        assert.equal(completed, EXPECTED_CASE_COUNT, "收银台浏览器矩阵存在跳过案例");

        const logsResult = await command("docker", ["logs", webContainer], { allowFailure: true, timeoutMs: 30_000 });
        if (logsResult.code !== 0) throw new Error(`无法读取生产 Nginx 日志\n${logsResult.stdout}${logsResult.stderr}`);
        webLogs = `${logsResult.stdout}${logsResult.stderr}`;
        for (const sentinel of [...tokens, "checkout-gate-code", "https://qr.invalid", "INTERNAL_SENTINEL"]) {
            assert.ok(!webLogs.includes(sentinel), `生产 Nginx 日志泄漏支付能力事实: ${sentinel}`);
        }
    } catch (error) {
        primaryError = error;
        const logsResult = await command("docker", ["logs", webContainer], { allowFailure: true, timeoutMs: 30_000 }).catch(() => null);
        if (logsResult) webLogs = `${logsResult.stdout}${logsResult.stderr}`;
    } finally {
        if (browser) {
            try {
                await browser.close();
            } catch (error) {
                cleanupErrors.push(error);
            }
        }
        await cleanupResource(["rm", "--force", webContainer], cleanupErrors);
        await cleanupResource(["rm", "--force", fixtureContainer], cleanupErrors);
        await cleanupResource(["network", "rm", network], cleanupErrors);
        await cleanupResource(["image", "rm", "--force", image], cleanupErrors);
        const expectedTempPrefix = path.join(os.tmpdir(), "hmaigc-checkout-gate-");
        if (!path.resolve(tempDirectory).startsWith(path.resolve(expectedTempPrefix))) {
            cleanupErrors.push(new Error(`拒绝清理非门禁临时目录: ${tempDirectory}`));
        } else {
            try {
                await rm(tempDirectory, { recursive: true, force: false });
            } catch (error) {
                cleanupErrors.push(error);
            }
        }
    }

    if (primaryError && cleanupErrors.length > 0) throw new AggregateError([primaryError, ...cleanupErrors], "收银台浏览器门禁和资源清理同时失败");
    if (primaryError) throw primaryError;
    if (cleanupErrors.length > 0) throw new AggregateError(cleanupErrors, "收银台浏览器门禁资源清理失败");
    process.stdout.write(`membership checkout production Chromium gate passed (${EXPECTED_CASE_COUNT}/${EXPECTED_CASE_COUNT})\n`);
}

await main();
