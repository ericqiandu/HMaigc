import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";

const root = resolve(import.meta.dir, "..");

describe("积分商城生产契约", () => {
    test("会员页进入受保护的独立积分商城路由", () => {
        const router = readFileSync(resolve(root, "src/router.tsx"), "utf8");
        const membership = readFileSync(resolve(root, "src/pages/membership/index.tsx"), "utf8");
        expect(router).toContain('{ path: "/credit-store", element: protectedRoute(<CreditStorePage />) }');
        expect(membership).toContain('navigate("/credit-store")');
        expect(membership).not.toContain('onOpenWallet={() => (user ? navigate("/wallet")');
    });

    test("商品来自后端且购买使用幂等订单与真实收银台", () => {
        const page = readFileSync(resolve(root, "src/pages/credit-store/index.tsx"), "utf8");
        const api = readFileSync(resolve(root, "src/services/api/credit-store.ts"), "utf8");
        expect(page).toContain("getCreditStorefront()");
        expect(page).toContain("createCreditTopupOrder(product.id, crypto.randomUUID())");
        expect(page).toContain("createCreditTopupCheckout(order.id)");
        expect(api).toContain('"Idempotency-Key": idempotencyKey');
        expect(api).toContain('api.get("/credit-store")');
    });
});
