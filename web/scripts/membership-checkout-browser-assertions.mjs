import assert from "node:assert/strict";

export const EXPECTED_CASE_COUNT = 54;

const REQUIRED_CSP_DIRECTIVES = new Map([
    ["default-src", ["'self'"]],
    ["base-uri", ["'self'"]],
    ["connect-src", ["'self'", "https:", "wss:", "blob:", "data:"]],
    ["form-action", ["'self'"]],
    ["frame-ancestors", ["'self'"]],
    ["object-src", ["'none'"]],
    ["worker-src", ["'self'", "blob:"]],
]);

function parseCSP(value) {
    return new Map(
        value
            .split(";")
            .map((part) => part.trim())
            .filter(Boolean)
            .map((part) => {
                const [name, ...sources] = part.split(/\s+/u);
                return [name, sources];
            }),
    );
}

export function assertCheckoutSecurityHeaders(headers, label) {
    assert.equal(headers["cache-control"], "private, no-store", `${label}: Cache-Control 必须精确为 private, no-store`);
    assert.equal(headers.pragma, "no-cache", `${label}: Pragma 必须精确为 no-cache`);
    assert.equal(headers["referrer-policy"], "no-referrer", `${label}: Referrer-Policy 必须精确为 no-referrer`);

    const value = headers["content-security-policy"];
    assert.ok(value, `${label}: 缺少强制 Content-Security-Policy`);
    const directives = parseCSP(value);
    for (const [name, sources] of REQUIRED_CSP_DIRECTIVES) {
        assert.deepEqual(directives.get(name), sources, `${label}: CSP ${name} 不符合生产契约`);
    }

    const scripts = directives.get("script-src") ?? [];
    assert.ok(scripts.includes("'self'"), `${label}: script-src 必须允许同源构建产物`);
    assert.ok(scripts.includes("https://static.hmaigc.ai"), `${label}: script-src 必须允许正式静态 CDN`);
    assert.ok(scripts.includes("'wasm-unsafe-eval'"), `${label}: script-src 必须显式允许 WebAssembly 编译`);
    assert.ok(
        scripts.some((source) => source.startsWith("'sha256-")),
        `${label}: script-src 缺少当前主题初始化脚本 hash`,
    );
    assert.ok(!scripts.includes("'unsafe-inline'"), `${label}: script-src 禁止 unsafe-inline`);
    assert.ok(!scripts.includes("'unsafe-eval'"), `${label}: script-src 禁止 broad unsafe-eval`);
    assert.ok(!scripts.includes("*"), `${label}: script-src 禁止通配源`);
    assert.ok(!scripts.includes("https:"), `${label}: script-src 禁止任意 HTTPS 脚本`);
}

export async function assertProductionMediaReads(page, label) {
    const results = await page.evaluate(async () => {
        const blobURL = URL.createObjectURL(new Blob(["hmaigc-blob"], { type: "text/plain" }));
        try {
            const [blobResponse, dataResponse] = await Promise.all([fetch(blobURL), fetch("data:text/plain,hmaigc-data")]);
            return {
                blob: blobResponse.ok ? await blobResponse.text() : `HTTP ${blobResponse.status}`,
                data: dataResponse.ok ? await dataResponse.text() : `HTTP ${dataResponse.status}`,
            };
        } finally {
            URL.revokeObjectURL(blobURL);
        }
    });
    assert.deepEqual(results, { blob: "hmaigc-blob", data: "hmaigc-data" }, `${label}: 全站 CSP 阻断画布本地媒体读取`);
}

export async function assertCheckoutLayout(page, viewport) {
    const snapshot = await page.evaluate(() => {
        const shell = document.querySelector(".payment-checkout-shell");
        const order = document.querySelector(".payment-checkout-order-surface");
        const payment = document.querySelector(".payment-checkout-payment-surface");
        if (!(shell instanceof HTMLElement) || !(order instanceof HTMLElement) || !(payment instanceof HTMLElement)) {
            throw new Error("收银台布局节点不完整");
        }
        const shellRect = shell.getBoundingClientRect();
        const orderRect = order.getBoundingClientRect();
        const paymentRect = payment.getBoundingClientRect();
        const shellStyle = getComputedStyle(shell);
        const paymentStyle = getComputedStyle(payment);
        const viewportWidth = document.documentElement.clientWidth;
        const overflowing = Array.from(document.querySelectorAll(".payment-checkout-page *"))
            .filter((node) => node instanceof HTMLElement || node instanceof SVGElement)
            .map((node) => {
                const rect = node.getBoundingClientRect();
                return { className: node.getAttribute("class") ?? node.tagName, left: rect.left, right: rect.right };
            })
            .filter((item) => item.right > viewportWidth + 0.5 || item.left < -0.5);
        return {
            documentClientWidth: document.documentElement.clientWidth,
            documentScrollWidth: document.documentElement.scrollWidth,
            bodyClientWidth: document.body.clientWidth,
            bodyScrollWidth: document.body.scrollWidth,
            shellClientWidth: shell.clientWidth,
            shellScrollWidth: shell.scrollWidth,
            shellOverflowX: shellStyle.overflowX,
            shellRadius: Number.parseFloat(shellStyle.borderTopRightRadius),
            shellRect: { left: shellRect.left, right: shellRect.right },
            orderRect: { bottom: orderRect.bottom, left: orderRect.left, right: orderRect.right, top: orderRect.top },
            paymentRect: { bottom: paymentRect.bottom, left: paymentRect.left, right: paymentRect.right, top: paymentRect.top },
            paymentRadii: {
                bottomLeft: Number.parseFloat(paymentStyle.borderBottomLeftRadius),
                bottomRight: Number.parseFloat(paymentStyle.borderBottomRightRadius),
                topRight: Number.parseFloat(paymentStyle.borderTopRightRadius),
            },
            overflowing,
        };
    });

    assert.equal(snapshot.documentScrollWidth, snapshot.documentClientWidth, `${viewport.name}: document 存在横向溢出`);
    assert.equal(snapshot.bodyScrollWidth, snapshot.bodyClientWidth, `${viewport.name}: body 存在横向溢出`);
    assert.equal(snapshot.shellScrollWidth, snapshot.shellClientWidth, `${viewport.name}: shell 存在被裁切内容`);
    assert.notEqual(snapshot.shellOverflowX, "hidden", `${viewport.name}: 禁止用 overflow-x:hidden 掩盖布局错误`);
    assert.equal(snapshot.shellOverflowX, "clip", `${viewport.name}: 外壳只允许裁切圆角绘制，不创建隐藏滚动容器`);
    assert.ok(snapshot.shellRadius > 0, `${viewport.name}: 唯一外壳必须保留圆角`);
    assert.ok(snapshot.shellRect.left >= 0 && snapshot.shellRect.right <= viewport.width + 0.5, `${viewport.name}: shell 超出视口`);
    assert.deepEqual(snapshot.overflowing, [], `${viewport.name}: 子节点超出视口边界`);

    if (viewport.width > 767) {
        assert.ok(snapshot.paymentRect.left >= snapshot.orderRect.right - 1, `${viewport.name}: 桌面/平板必须保持左右双栏`);
        assert.ok(Math.abs(snapshot.paymentRect.top - snapshot.orderRect.top) <= 1, `${viewport.name}: 双栏顶部必须对齐`);
        assert.equal(snapshot.paymentRadii.topRight, 0, `${viewport.name}: 右侧表面不得在外壳内重复圆角`);
        assert.equal(snapshot.paymentRadii.bottomRight, 0, `${viewport.name}: 右侧表面不得在外壳内重复圆角`);
    } else {
        assert.ok(snapshot.paymentRect.top >= snapshot.orderRect.bottom - 1, `${viewport.name}: 手机必须改为上下单列`);
        assert.ok(Math.abs(snapshot.paymentRect.left - snapshot.orderRect.left) <= 1, `${viewport.name}: 手机单列左右边界必须对齐`);
        assert.equal(snapshot.paymentRadii.bottomLeft, 0, `${viewport.name}: 底部表面不得在外壳内重复圆角`);
        assert.equal(snapshot.paymentRadii.bottomRight, 0, `${viewport.name}: 底部表面不得在外壳内重复圆角`);
    }
}

export async function assertVisibleControlTargets(page, label) {
    const undersized = await page.$$eval(".payment-checkout-back, .payment-checkout-action, .payment-checkout-inline-action, .payment-checkout-provider", (nodes) =>
        nodes
            .filter((node) => {
                const style = getComputedStyle(node);
                const rect = node.getBoundingClientRect();
                return style.display !== "none" && style.visibility !== "hidden" && rect.width > 0 && rect.height > 0;
            })
            .map((node) => {
                const rect = node.getBoundingClientRect();
                return { className: node.className, height: rect.height, width: rect.width };
            })
            .filter((item) => item.height < 44 || item.width < 44),
    );
    assert.deepEqual(undersized, [], `${label}: 可见操作热区必须至少 44×44px`);
}

export async function assertKeyboardFocus(page, label) {
    await page.evaluate(() => {
        if (document.activeElement instanceof HTMLElement) document.activeElement.blur();
    });
    const buttonSelector = [".payment-checkout-back:not(:disabled)", ".payment-checkout-action:not(:disabled)", ".payment-checkout-inline-action:not(:disabled)"].join(", ");
    const radioSelector = ".payment-checkout-provider-input:not(:disabled)";
    const expected = await page.evaluate(
        ({ buttons, radios }) => ({
            buttonCount: document.querySelectorAll(buttons).length,
            radioValues: Array.from(document.querySelectorAll(radios), (node) => (node instanceof HTMLInputElement ? node.value : "")),
        }),
        { buttons: buttonSelector, radios: radioSelector },
    );
    assert.ok(expected.buttonCount + expected.radioValues.length > 0, `${label}: 页面没有可操作的收银台控件`);

    const reachedButtons = new Set();
    const reachedRadios = new Set();
    const focusSnapshot = () =>
        page.evaluate(
            ({ buttons, radios }) => {
                const element = document.activeElement;
                if (!(element instanceof HTMLElement)) return null;
                const buttonIndex = Array.from(document.querySelectorAll(buttons)).indexOf(element);
                const radioValue = element.matches(radios) && element instanceof HTMLInputElement ? element.value : "";
                if (buttonIndex < 0 && !radioValue) return null;
                let target = element;
                let focusRing = { className: element.className, outlineStyle: "none", outlineWidth: 0 };
                while (target) {
                    const style = getComputedStyle(target);
                    const outlineWidth = Number.parseFloat(style.outlineWidth);
                    if (style.outlineStyle !== "none" && outlineWidth > focusRing.outlineWidth) {
                        focusRing = { className: target.className, outlineStyle: style.outlineStyle, outlineWidth };
                    }
                    target = target.parentElement;
                }
                return { ...focusRing, buttonIndex, radioValue };
            },
            { buttons: buttonSelector, radios: radioSelector },
        );
    const assertFocusRing = (focus) => {
        assert.ok(focus, `${label}: Tab 后没有聚焦收银台控件`);
        assert.notEqual(focus.outlineStyle, "none", `${label}: ${focus.className} 键盘焦点不可见`);
        assert.ok(focus.outlineWidth >= 2, `${label}: ${focus.className} 键盘焦点轮廓必须至少 2px`);
    };

    for (let attempt = 0; attempt < expected.buttonCount + expected.radioValues.length + 12; attempt += 1) {
        await page.keyboard.press("Tab");
        const focus = await focusSnapshot();
        if (!focus) continue;
        assertFocusRing(focus);
        if (focus.buttonIndex >= 0) reachedButtons.add(focus.buttonIndex);
        if (focus.radioValue && reachedRadios.size === 0) {
            reachedRadios.add(focus.radioValue);
            for (let radioIndex = 1; radioIndex < expected.radioValues.length; radioIndex += 1) {
                await page.keyboard.press("ArrowRight");
                const radioFocus = await focusSnapshot();
                assertFocusRing(radioFocus);
                assert.ok(radioFocus.radioValue, `${label}: 方向键离开了支付渠道单选组`);
                reachedRadios.add(radioFocus.radioValue);
            }
        }
        if (reachedButtons.size === expected.buttonCount && reachedRadios.size === expected.radioValues.length) break;
    }
    assert.equal(reachedButtons.size, expected.buttonCount, `${label}: 并非所有收银台按钮都能通过 Tab 到达`);
    assert.equal(reachedRadios.size, expected.radioValues.length, `${label}: 并非所有支付渠道都能通过键盘切换`);
}

export async function assertSmallTextContrast(page, label) {
    const failures = await page.evaluate(() => {
        const parseColor = (value) => {
            const match = value.match(/rgba?\(([^)]+)\)/u);
            if (!match) return null;
            const parts = match[1]
                .split(/[\s,/]+/u)
                .filter(Boolean)
                .map(Number);
            if (parts.length < 3 || parts.some((part) => !Number.isFinite(part))) return null;
            return { r: parts[0], g: parts[1], b: parts[2], a: parts.length >= 4 ? parts[3] : 1 };
        };
        const blend = (foreground, background) => ({
            r: foreground.r * foreground.a + background.r * (1 - foreground.a),
            g: foreground.g * foreground.a + background.g * (1 - foreground.a),
            b: foreground.b * foreground.a + background.b * (1 - foreground.a),
            a: 1,
        });
        const channel = (value) => {
            const normalized = value / 255;
            return normalized <= 0.04045 ? normalized / 12.92 : ((normalized + 0.055) / 1.055) ** 2.4;
        };
        const luminance = (color) => 0.2126 * channel(color.r) + 0.7152 * channel(color.g) + 0.0722 * channel(color.b);
        const ratio = (left, right) => {
            const light = Math.max(luminance(left), luminance(right));
            const dark = Math.min(luminance(left), luminance(right));
            return (light + 0.05) / (dark + 0.05);
        };
        const effectiveBackground = (element) => {
            const chain = [];
            let current = element;
            while (current) {
                chain.unshift(current);
                current = current.parentElement;
            }
            let background = { r: 255, g: 255, b: 255, a: 1 };
            for (const item of chain) {
                const color = parseColor(getComputedStyle(item).backgroundColor);
                if (color && color.a > 0) background = blend(color, background);
            }
            return background;
        };
        const candidates = Array.from(document.querySelectorAll('[class^="membership-checkout"], [class^="payment-checkout"]'));
        return candidates.flatMap((element) => {
            if (!(element instanceof HTMLElement)) return [];
            const directText = Array.from(element.childNodes)
                .filter((node) => node.nodeType === Node.TEXT_NODE)
                .map((node) => node.textContent ?? "")
                .join(" ")
                .trim();
            if (!directText) return [];
            const style = getComputedStyle(element);
            const rect = element.getBoundingClientRect();
            const fontSize = Number.parseFloat(style.fontSize);
            if (style.display === "none" || style.visibility === "hidden" || rect.width <= 0 || rect.height <= 0 || fontSize > 18) return [];
            const foreground = parseColor(style.color);
            if (!foreground) return [{ className: element.className, ratio: 0, text: directText }];
            const background = effectiveBackground(element);
            const effectiveForeground = blend(foreground, background);
            const contrast = ratio(effectiveForeground, background);
            return contrast < 4.5 ? [{ className: element.className, ratio: Number(contrast.toFixed(2)), text: directText }] : [];
        });
    });
    assert.deepEqual(failures, [], `${label}: 小字号文字/操作/错误提示对比度必须至少 4.5:1`);
}

export async function accessibilityText(page) {
    const session = await page.createCDPSession();
    try {
        await session.send("Accessibility.enable");
        const tree = await session.send("Accessibility.getFullAXTree");
        return tree.nodes
            .flatMap((node) => [node.name?.value, node.description?.value, node.value?.value])
            .filter((value) => typeof value === "string")
            .join("\n");
    } finally {
        await session.detach();
    }
}

export function assertNoSensitivePresentation({ accessibility, bodyText, consoleText, forbidden, label }) {
    for (const sentinel of forbidden) {
        if (!sentinel) continue;
        assert.ok(!bodyText.includes(sentinel), `${label}: DOM 泄漏敏感/伪造事实 ${sentinel}`);
        assert.ok(!accessibility.includes(sentinel), `${label}: 可访问性树泄漏敏感/伪造事实 ${sentinel}`);
        assert.ok(!consoleText.includes(sentinel), `${label}: 控制台泄漏敏感/伪造事实 ${sentinel}`);
    }
}
