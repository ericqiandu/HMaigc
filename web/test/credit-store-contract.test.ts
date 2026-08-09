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

    test("保留交付稿的三分区导航、惊喜横幅与卡片视觉结构", () => {
        const page = readFileSync(resolve(root, "src/pages/credit-store/index.tsx"), "utf8");
        const styles = readFileSync(resolve(root, "src/pages/credit-store/credit-store.css"), "utf8");
        expect(page).toContain('import bannerImage from "./assets/banner-surprise.jpg"');
        expect(page).toContain('{ key: "surprise", label: "惊喜专区", icon: "🎁" }');
        expect(page).toContain('{ key: "general", label: "通用积分卡", icon: "⚡" }');
        expect(page).toContain('{ key: "model", label: "专属模型卡", icon: "🎲" }');
        expect(page).toContain('className="points-surprise-grid"');
        expect(page).toContain('className="points-general-grid"');
        expect(page).toContain("限时商品以实际上架时间为准");
        expect(page).toContain("通用积分卡暂无上架商品");
        expect(page).not.toContain("00</strong>");
        expect(styles).toContain("background: #070b11");
        expect(styles).toContain("position: sticky");
    });
});
