import assert from "node:assert/strict";

const REQUIRED_CSP_DIRECTIVES = new Map([
    ["default-src", ["'self'"]],
    ["base-uri", ["'self'"]],
    ["connect-src", ["'self'", "https:", "wss:", "blob:", "data:"]],
    ["form-action", ["'self'"]],
    ["frame-ancestors", ["'self'"]],
    ["object-src", ["'none'"]],
]);

const staticAssetBaseURL = (process.env.HMAIGC_STATIC_ASSET_BASE_URL ?? "").trim() || "https://static.hm.kunagent.com/hmaigc/web";
const staticAssetOrigin = new URL(staticAssetBaseURL).origin;

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
    assert.ok(scripts.includes(staticAssetOrigin), `${label}: script-src 必须允许实际发布静态资源 Origin`);
    assert.ok(scripts.includes("'wasm-unsafe-eval'"), `${label}: script-src 必须显式允许 WebAssembly 编译`);
    assert.ok(
        scripts.some((source) => source.startsWith("'sha256-")),
        `${label}: script-src 缺少当前主题初始化脚本 hash`,
    );
    assert.ok(!scripts.includes("'unsafe-inline'"), `${label}: script-src 禁止 unsafe-inline`);
    assert.ok(!scripts.includes("'unsafe-eval'"), `${label}: script-src 禁止 broad unsafe-eval`);
    assert.ok(!scripts.includes("*"), `${label}: script-src 禁止通配源`);
    assert.ok(!scripts.includes("https:"), `${label}: script-src 禁止任意 HTTPS 脚本`);

    for (const directive of ["font-src", "style-src", "worker-src"]) {
        const sources = directives.get(directive) ?? [];
        assert.ok(sources.includes(staticAssetOrigin), `${label}: ${directive} 必须允许实际发布静态资源 Origin`);
    }
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
            isTeam: shell.classList.contains("is-team"),
            shellClientWidth: shell.clientWidth,
            shellScrollWidth: shell.scrollWidth,
            shellOverflowX: shellStyle.overflowX,
            shellRadius: Number.parseFloat(shellStyle.borderTopRightRadius),
            shellRect: { left: shellRect.left, right: shellRect.right, width: shellRect.width },
            orderRect: { bottom: orderRect.bottom, left: orderRect.left, right: orderRect.right, top: orderRect.top, width: orderRect.width },
            paymentRect: { bottom: paymentRect.bottom, left: paymentRect.left, right: paymentRect.right, top: paymentRect.top, width: paymentRect.width },
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
    assert.equal(snapshot.shellOverflowX, "visible", `${viewport.name}: 外壳不得依靠裁切掩盖布局错误`);
    assert.ok(snapshot.shellRadius > 0, `${viewport.name}: 唯一外壳必须保留圆角`);
    assert.ok(snapshot.shellRect.left >= 0 && snapshot.shellRect.right <= viewport.width + 0.5, `${viewport.name}: shell 超出视口`);
    assert.deepEqual(snapshot.overflowing, [], `${viewport.name}: 子节点超出视口边界`);

    const expectedShellWidth = Math.min(snapshot.isTeam ? 880 : 766, viewport.width - (viewport.width <= 767 ? 32 : 48));
    assert.ok(Math.abs(snapshot.shellRect.width - expectedShellWidth) <= 1, `${viewport.name}: 收银台宽度应为 ${expectedShellWidth}px，实际 ${snapshot.shellRect.width}px`);

    if (viewport.width > 767) {
        assert.ok(snapshot.paymentRect.left >= snapshot.orderRect.right - 1, `${viewport.name}: 桌面/平板必须保持左右双栏`);
        assert.ok(Math.abs(snapshot.paymentRect.top - snapshot.orderRect.top) <= 1, `${viewport.name}: 双栏顶部必须对齐`);
        assert.equal(snapshot.paymentRadii.topRight, 0, `${viewport.name}: 右侧表面不得在外壳内重复圆角`);
        assert.equal(snapshot.paymentRadii.bottomRight, 0, `${viewport.name}: 右侧表面不得在外壳内重复圆角`);
        const expectedOrderWidth = snapshot.isTeam ? (viewport.width >= 928 ? 560 : snapshot.shellRect.width - 320) : snapshot.shellRect.width * (425 / 766);
        const expectedPaymentWidth = snapshot.isTeam ? 320 : snapshot.shellRect.width * (341 / 766);
        assert.ok(Math.abs(snapshot.orderRect.width - expectedOrderWidth) <= 2, `${viewport.name}: 左侧订单区比例不正确`);
        assert.ok(Math.abs(snapshot.paymentRect.width - expectedPaymentWidth) <= 2, `${viewport.name}: 右侧支付区比例不正确`);
        if (viewport.width >= (snapshot.isTeam ? 928 : 814)) {
            assert.ok(Math.abs(snapshot.orderRect.width - (snapshot.isTeam ? 560 : 425)) <= 1, `${viewport.name}: 桌面左侧订单区宽度不正确`);
            assert.ok(Math.abs(snapshot.paymentRect.width - (snapshot.isTeam ? 320 : 341)) <= 1, `${viewport.name}: 桌面右侧支付区宽度不正确`);
        }
    } else {
        assert.ok(snapshot.paymentRect.top >= snapshot.orderRect.bottom - 1, `${viewport.name}: 手机必须改为上下单列`);
        assert.ok(Math.abs(snapshot.paymentRect.left - snapshot.orderRect.left) <= 1, `${viewport.name}: 手机单列左右边界必须对齐`);
        assert.equal(snapshot.paymentRadii.bottomLeft, 0, `${viewport.name}: 底部表面不得在外壳内重复圆角`);
        assert.equal(snapshot.paymentRadii.bottomRight, 0, `${viewport.name}: 底部表面不得在外壳内重复圆角`);
    }
}

export async function assertMembershipDialogLayout(page, viewport, label) {
    const snapshot = await page.evaluate(() => {
        const shell = document.querySelector(".membership-payment-dialog .payment-checkout-shell.is-dialog");
        const dialog = shell?.closest(".membership-payment-dialog");
        const content = shell?.closest(".ant-modal-container, .ant-modal-content") ?? dialog;
        const order = document.querySelector(".membership-payment-dialog .payment-checkout-order-surface");
        const payment = document.querySelector(".membership-payment-dialog .payment-checkout-payment-surface");
        const close = document.querySelector(".membership-payment-dialog .ant-modal-close");
        const qr = document.querySelector(".membership-payment-dialog .payment-checkout-qr-image");
        const qrSurface = document.querySelector(".membership-payment-dialog .payment-checkout-qr-code");
        if (![dialog, content, shell, order, payment, close, qr, qrSurface].every((node) => node instanceof HTMLElement || node instanceof SVGElement)) {
            throw new Error(`会员付款弹窗布局节点不完整: ${[dialog, content, shell, order, payment, close, qr, qrSurface].map(Boolean).join(",")}`);
        }
        const dialogRect = dialog.getBoundingClientRect();
        const shellRect = shell.getBoundingClientRect();
        const orderRect = order.getBoundingClientRect();
        const paymentRect = payment.getBoundingClientRect();
        const closeRect = close.getBoundingClientRect();
        const qrRect = qr.getBoundingClientRect();
        const qrSurfaceRect = qrSurface.getBoundingClientRect();
        const isTeam = shell.classList.contains("is-team");
        const teamPlanOptions = Array.from(shell.querySelectorAll(".membership-team-plan-option"));
        const selectedTeamPlan = shell.querySelector(".membership-team-plan-option.is-selected");
        const teamSeatStepper = shell.querySelector(".membership-team-seat-stepper");
        const contentStyle = getComputedStyle(content);
        const shellStyle = getComputedStyle(shell);
        const orderStyle = getComputedStyle(order);
        const paymentStyle = getComputedStyle(payment);
        const overflowing = Array.from(dialog.querySelectorAll("*"))
            .filter((node) => node instanceof HTMLElement || node instanceof SVGElement)
            .map((node) => {
                const rect = node.getBoundingClientRect();
                return { className: node.getAttribute("class") ?? node.tagName, height: rect.height, left: rect.left, right: rect.right, width: rect.width };
            })
            .filter((item) => item.width > 0 && item.height > 0)
            .filter((item) => item.left < dialogRect.left - 0.5 || item.right > dialogRect.right + 0.5);
        const verticalCandidates = [content, dialog.closest(".ant-modal-wrap"), document.scrollingElement].filter((node, index, values) => node instanceof HTMLElement && values.indexOf(node) === index);
        let verticalContainer = content;
        for (const candidate of verticalCandidates) {
            const candidateScrollTop = candidate.scrollTop;
            candidate.scrollTop = candidate.scrollHeight;
            const canReachBottom = candidate.scrollTop > candidateScrollTop;
            candidate.scrollTop = candidateScrollTop;
            if (canReachBottom) {
                verticalContainer = candidate;
                break;
            }
        }
        const verticalStyle = getComputedStyle(verticalContainer);
        const scrollTopBefore = verticalContainer.scrollTop;
        const scrollable = verticalContainer.scrollHeight > verticalContainer.clientHeight + 1;
        verticalContainer.scrollTop = verticalContainer.scrollHeight;
        const scrollTopAfter = verticalContainer.scrollTop;
        verticalContainer.scrollTop = scrollTopBefore;
        return {
            close: { height: closeRect.height, width: closeRect.width },
            contentBorderWidth: Number.parseFloat(contentStyle.borderTopWidth),
            contentRadius: Number.parseFloat(contentStyle.borderTopLeftRadius),
            contentShadow: contentStyle.boxShadow,
            contentOverflowY: verticalStyle.overflowY,
            contentScroll: { clientHeight: verticalContainer.clientHeight, scrollHeight: verticalContainer.scrollHeight, scrollTopAfter, scrollTopBefore, scrollable },
            dialog: { left: dialogRect.left, right: dialogRect.right, width: dialogRect.width },
            order: { left: orderRect.left, right: orderRect.right, top: orderRect.top, width: orderRect.width },
            orderRadius: Number.parseFloat(orderStyle.borderTopLeftRadius),
            overflowing,
            payment: { left: paymentRect.left, right: paymentRect.right, top: paymentRect.top, width: paymentRect.width },
            paymentRadius: Number.parseFloat(paymentStyle.borderTopRightRadius),
            qr: { height: qrRect.height, width: qrRect.width },
            qrSurface: { height: qrSurfaceRect.height, width: qrSurfaceRect.width },
            isTeam,
            selectedTeamPlan: Boolean(selectedTeamPlan),
            teamPlanOptionCount: teamPlanOptions.length,
            teamPlanOptionHeights: teamPlanOptions.map((node) => node.getBoundingClientRect().height),
            teamSeatStepper: teamSeatStepper instanceof HTMLElement ? { height: teamSeatStepper.getBoundingClientRect().height, width: teamSeatStepper.getBoundingClientRect().width } : null,
            shell: { left: shellRect.left, right: shellRect.right, width: shellRect.width },
            shellBorderWidth: Number.parseFloat(shellStyle.borderTopWidth),
            shellOverflowX: shellStyle.overflowX,
            shellRadius: Number.parseFloat(shellStyle.borderTopLeftRadius),
            shellShadow: shellStyle.boxShadow,
        };
    });

    const expectedWidth = Math.min(snapshot.isTeam ? 880 : 766, viewport.width - (viewport.width <= 767 ? 32 : 48));
    assert.ok(Math.abs(snapshot.dialog.width - expectedWidth) <= 1, `${label}: 弹窗宽度未对齐参考层级，期望 ${expectedWidth}，实际 ${snapshot.dialog.width}`);
    assert.ok(snapshot.dialog.left >= 0 && snapshot.dialog.right <= viewport.width + 0.5, `${label}: 弹窗超出视口`);
    assert.equal(snapshot.contentBorderWidth, 0, `${label}: Ant 容器不得占用 766px 收银台内容宽度`);
    assert.equal(snapshot.contentRadius, 0, `${label}: Ant 容器不得创建第二层圆角`);
    assert.equal(snapshot.contentShadow, "none", `${label}: Ant 容器不得创建第二层阴影`);
    assert.equal(snapshot.shellBorderWidth, 0, `${label}: 内层收银台不得重复边框`);
    assert.ok(snapshot.shellRadius >= 12, `${label}: 唯一收银台外壳圆角不足`);
    assert.ok(snapshot.shellShadow.includes("inset"), `${label}: 唯一收银台外壳缺少不占宽度的内描边`);
    assert.notEqual(snapshot.shellOverflowX, "hidden", `${label}: 禁止隐藏横向溢出掩盖布局错误`);
    if (viewport.width > 767) assert.equal(snapshot.orderRadius, 0, `${label}: 桌面订单表面不得重复圆角`);
    assert.equal(snapshot.paymentRadius, 0, `${label}: 二维码表面不得重复圆角`);
    assert.ok(Math.abs(snapshot.qr.width - 112) <= 1 && Math.abs(snapshot.qr.height - 112) <= 1, `${label}: 二维码 SVG 必须保持 112×112，实际 ${snapshot.qr.width}×${snapshot.qr.height}`);
    assert.ok(Math.abs(snapshot.qrSurface.width - 128) <= 1 && Math.abs(snapshot.qrSurface.height - 128) <= 1, `${label}: 二维码白底整体必须保持 128×128，实际 ${snapshot.qrSurface.width}×${snapshot.qrSurface.height}`);
    assert.deepEqual(snapshot.overflowing, [], `${label}: 弹窗子节点存在横向溢出`);

    if (snapshot.isTeam) {
        assert.ok(snapshot.teamPlanOptionCount >= 2, `${label}: 团队付款弹窗必须展示同档位真实套餐选项`);
        assert.equal(snapshot.selectedTeamPlan, true, `${label}: 团队付款弹窗缺少唯一选中套餐`);
        if (viewport.width >= 928) {
            assert.ok(
                snapshot.teamPlanOptionHeights.every((height) => Math.abs(height - 92) <= 1),
                `${label}: 桌面团队套餐卡高度必须为 92px，实际 ${snapshot.teamPlanOptionHeights.join(",")}`,
            );
        } else {
            assert.ok(
                snapshot.teamPlanOptionHeights.every((height) => height >= 92),
                `${label}: 响应式团队套餐卡不得低于 92px，实际 ${snapshot.teamPlanOptionHeights.join(",")}`,
            );
            assert.ok(Math.max(...snapshot.teamPlanOptionHeights) - Math.min(...snapshot.teamPlanOptionHeights) <= 1, `${label}: 响应式团队套餐卡高度必须对齐`);
        }
        assert.ok(snapshot.teamSeatStepper, `${label}: 团队付款弹窗缺少席位步进器`);
    }

    if (viewport.width > 767) {
        const expectedOrderWidth = snapshot.isTeam ? (viewport.width >= 928 ? 560 : snapshot.dialog.width - 320) : snapshot.dialog.width * (425 / 766);
        const expectedPaymentWidth = snapshot.isTeam ? 320 : snapshot.dialog.width * (341 / 766);
        assert.ok(Math.abs(snapshot.order.width - expectedOrderWidth) <= 2, `${label}: 左侧订单区比例不正确，期望 ${expectedOrderWidth}，实际 ${snapshot.order.width}，弹窗 ${snapshot.dialog.width}`);
        assert.ok(Math.abs(snapshot.payment.width - expectedPaymentWidth) <= 2, `${label}: 右侧二维码区比例不正确，期望 ${expectedPaymentWidth}，实际 ${snapshot.payment.width}，弹窗 ${snapshot.dialog.width}`);
        if (viewport.width >= (snapshot.isTeam ? 928 : 814)) {
            assert.ok(Math.abs(snapshot.order.width - (snapshot.isTeam ? 560 : 425)) <= 1, `${label}: 桌面左侧订单区宽度不正确`);
            assert.ok(Math.abs(snapshot.payment.width - (snapshot.isTeam ? 320 : 341)) <= 1, `${label}: 桌面右侧二维码区宽度不正确`);
        }
        assert.ok(snapshot.payment.left >= snapshot.order.right - 1, `${label}: 双栏表面发生重叠`);
        assert.ok(Math.abs(snapshot.payment.top - snapshot.order.top) <= 1, `${label}: 双栏顶部未对齐`);
    } else {
        assert.ok(snapshot.payment.top >= snapshot.order.top, `${label}: 手机付款区顺序错误`);
        assert.ok(Math.abs(snapshot.payment.width - snapshot.order.width) <= 1, `${label}: 手机单列宽度未对齐`);
        assert.ok(snapshot.close.width >= 44 && snapshot.close.height >= 44, `${label}: 手机关闭热区不足 44×44`);
        assert.ok(!["hidden", "clip"].includes(snapshot.contentOverflowY), `${label}: 手机纵向溢出不得被隐藏或裁剪`);
        if (snapshot.contentScroll.scrollable) {
            assert.ok(snapshot.contentScroll.scrollTopAfter > snapshot.contentScroll.scrollTopBefore, `${label}: 手机纵向内容无法实际滚动`);
            assert.ok(snapshot.contentScroll.scrollTopAfter >= snapshot.contentScroll.scrollHeight - snapshot.contentScroll.clientHeight - 1, `${label}: 手机无法滚动至底部内容`);
        }
    }
}

export async function assertMembershipSetupDialogLayout(page, viewport, label) {
    const snapshot = await page.evaluate(() => {
        const shell = document.querySelector(".membership-payment-dialog .payment-checkout-shell.is-dialog.membership-payment-setup");
        const dialog = shell?.closest(".membership-payment-dialog");
        const content = shell?.closest(".ant-modal-container, .ant-modal-content") ?? dialog;
        const order = shell?.querySelector(":scope > .payment-checkout-order-surface");
        const payment = shell?.querySelector(":scope > .payment-checkout-payment-surface");
        const facts = order?.querySelector(":scope > .membership-order-facts");
        if (![dialog, content, shell, order, payment, facts].every((node) => node instanceof HTMLElement)) {
            throw new Error("会员付款创建态缺少共享收银台结构");
        }
        const dialogRect = dialog.getBoundingClientRect();
        const shellRect = shell.getBoundingClientRect();
        const orderRect = order.getBoundingClientRect();
        const paymentRect = payment.getBoundingClientRect();
        const isTeam = shell.classList.contains("is-team");
        const owner = (node) => node.className;
        const descendants = Array.from(dialog.querySelectorAll("*")).map((node) => {
            const rect = node.getBoundingClientRect();
            return { className: node.getAttribute("class") ?? node.tagName, height: rect.height, left: rect.left, right: rect.right, width: rect.width };
        });
        const verticalCandidates = [content, dialog.closest(".ant-modal-wrap"), document.scrollingElement].filter((node, index, values) => node instanceof HTMLElement && values.indexOf(node) === index);
        let verticalContainer = content;
        for (const candidate of verticalCandidates) {
            const candidateScrollTop = candidate.scrollTop;
            candidate.scrollTop = candidate.scrollHeight;
            const canReachBottom = candidate.scrollTop > candidateScrollTop;
            candidate.scrollTop = candidateScrollTop;
            if (canReachBottom) {
                verticalContainer = candidate;
                break;
            }
        }
        const verticalStyle = getComputedStyle(verticalContainer);
        const scrollTopBefore = verticalContainer.scrollTop;
        const scrollable = verticalContainer.scrollHeight > verticalContainer.clientHeight + 1;
        verticalContainer.scrollTop = verticalContainer.scrollHeight;
        const scrollTopAfter = verticalContainer.scrollTop;
        verticalContainer.scrollTop = scrollTopBefore;
        return {
            contentOverflowY: verticalStyle.overflowY,
            contentScroll: { clientHeight: verticalContainer.clientHeight, scrollHeight: verticalContainer.scrollHeight, scrollTopAfter, scrollTopBefore, scrollable },
            descendants,
            dialog: { left: dialogRect.left, right: dialogRect.right, width: dialogRect.width },
            order: { left: orderRect.left, right: orderRect.right, top: orderRect.top, width: orderRect.width },
            owners: {
                facts: owner(facts),
                order: owner(order),
                payment: owner(payment),
                shell: owner(shell),
            },
            payment: { left: paymentRect.left, right: paymentRect.right, top: paymentRect.top, width: paymentRect.width },
            isTeam,
            shell: { left: shellRect.left, right: shellRect.right, width: shellRect.width },
        };
    });

    const expectedWidth = Math.min(snapshot.isTeam ? 880 : 766, viewport.width - (viewport.width <= 767 ? 32 : 48));
    assert.ok(Math.abs(snapshot.dialog.width - expectedWidth) <= 1, `${label}: 弹窗宽度未对齐参考收银台，期望 ${expectedWidth}，实际 ${snapshot.dialog.width}`);
    assert.ok(Math.abs(snapshot.shell.width - expectedWidth) <= 1, `${label}: 创建态 shell 宽度不正确，期望 ${expectedWidth}，实际 ${snapshot.shell.width}`);
    assert.deepEqual(
        snapshot.owners,
        {
            facts: `membership-order-facts membership-checkout-summary ${snapshot.isTeam ? "is-team" : "is-personal"}`,
            order: "payment-checkout-order-surface",
            payment: "payment-checkout-payment-surface membership-payment-setup-action",
            shell: `payment-checkout-shell is-dialog membership-payment-setup ${snapshot.isTeam ? "is-team" : "is-personal"}`,
        },
        `${label}: 创建态未使用唯一的左侧事实结构`,
    );
    assert.deepEqual(
        snapshot.descendants.filter((item) => item.width > 0 && item.height > 0 && (item.left < snapshot.dialog.left - 0.5 || item.right > snapshot.dialog.right + 0.5)),
        [],
        `${label}: 创建态子节点出现横向溢出`,
    );

    if (viewport.width > 767) {
        const expectedOrderWidth = snapshot.isTeam ? (viewport.width >= 928 ? 560 : snapshot.dialog.width - 320) : snapshot.dialog.width * (425 / 766);
        const expectedPaymentWidth = snapshot.isTeam ? 320 : snapshot.dialog.width * (341 / 766);
        assert.ok(Math.abs(snapshot.order.width - expectedOrderWidth) <= 2, `${label}: 左侧订单区比例不正确`);
        assert.ok(Math.abs(snapshot.payment.width - expectedPaymentWidth) <= 2, `${label}: 右侧付款区比例不正确`);
        if (viewport.width >= (snapshot.isTeam ? 928 : 814)) {
            assert.ok(Math.abs(snapshot.order.width - (snapshot.isTeam ? 560 : 425)) <= 1, `${label}: 桌面左侧订单区宽度不正确`);
            assert.ok(Math.abs(snapshot.payment.width - (snapshot.isTeam ? 320 : 341)) <= 1, `${label}: 桌面右侧付款区宽度不正确`);
        }
        assert.ok(snapshot.payment.left >= snapshot.order.right - 1, `${label}: 双栏表面发生重叠`);
        assert.ok(Math.abs(snapshot.payment.top - snapshot.order.top) <= 1, `${label}: 双栏顶部未对齐`);
    } else {
        assert.ok(snapshot.payment.top >= snapshot.order.top, `${label}: 移动端付款区顺序错误`);
        assert.ok(Math.abs(snapshot.payment.left - snapshot.order.left) <= 1, `${label}: 移动端单列左右边界不一致`);
        assert.ok(Math.abs(snapshot.payment.width - snapshot.order.width) <= 1, `${label}: 移动端单列宽度不一致`);
        assert.ok(!["hidden", "clip"].includes(snapshot.contentOverflowY), `${label}: 创建态手机纵向溢出不得被隐藏或裁剪`);
        if (snapshot.contentScroll.scrollable) {
            assert.ok(snapshot.contentScroll.scrollTopAfter > snapshot.contentScroll.scrollTopBefore, `${label}: 创建态手机纵向内容无法实际滚动`);
            assert.ok(snapshot.contentScroll.scrollTopAfter >= snapshot.contentScroll.scrollHeight - snapshot.contentScroll.clientHeight - 1, `${label}: 创建态手机无法滚动至底部内容`);
        }
    }

    return snapshot.owners;
}

export async function waitForStableMembershipDialog(page, expectedWidth, label) {
    await page.evaluate(
        ({ expectedWidth: targetWidth, label: dialogLabel }) =>
            new Promise((resolve, reject) => {
                const timeoutMs = 5_000;
                const startedAt = performance.now();
                let previousWidth = Number.NaN;
                let stableFrames = 0;

                const inspect = () => {
                    const shell = document.querySelector(".membership-payment-dialog .payment-checkout-shell.is-dialog");
                    const dialog = shell?.closest(".membership-payment-dialog");
                    if (dialog instanceof HTMLElement) {
                        const width = dialog.getBoundingClientRect().width;
                        const animationRunning = dialog.getAnimations().some((animation) => animation.playState === "pending" || animation.playState === "running");
                        const targetReached = Math.abs(width - targetWidth) <= 1;
                        const geometryStable = Number.isFinite(previousWidth) && Math.abs(width - previousWidth) <= 0.1;
                        stableFrames = targetReached && geometryStable && !animationRunning ? stableFrames + 1 : 0;
                        previousWidth = width;
                        if (stableFrames >= 3) {
                            resolve();
                            return;
                        }
                    }
                    if (performance.now() - startedAt >= timeoutMs) {
                        reject(new Error(`${dialogLabel}: 弹窗动画结束后仍未达到稳定宽度 ${targetWidth}`));
                        return;
                    }
                    requestAnimationFrame(inspect);
                };

                requestAnimationFrame(inspect);
            }),
        { expectedWidth, label },
    );
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
    const buttonSelector = [".payment-checkout-back:not(:disabled)", ".payment-checkout-action:not(:disabled)", ".payment-checkout-inline-action:not(:disabled)", ".payment-checkout-agreement-link"].join(", ");
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
