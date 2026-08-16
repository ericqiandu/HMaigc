import { describe, expect, test } from "bun:test";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";

import { CreditSymbol, formatCredits } from "../src/constant/credits";
import { GenerationCreditQuoteBadge } from "../src/components/canvas/generation-credit-quote-badge";

describe("credits presentation", () => {
    test("formats backend microcredits as user-facing credits", () => {
        expect(formatCredits(1_000_000)).toBe("1");
        expect(formatCredits(1_510_000)).toBe("1.51");
    });

    test("uses the product lightning symbol", () => {
        const markup = renderToStaticMarkup(createElement(CreditSymbol, { "aria-label": "预计积分" }));

        expect(markup).toContain("lucide-zap");
        expect(markup).not.toContain("lucide-coins");
    });

    test("keeps the lightning symbol visible for loading and failed quotes", () => {
        const loading = renderToStaticMarkup(createElement(GenerationCreditQuoteBadge, { state: { status: "loading", quote: null, error: null } }));
        const failed = renderToStaticMarkup(createElement(GenerationCreditQuoteBadge, { state: { status: "error", quote: null, error: "报价服务不可用" } }));

        expect(loading).toContain("lucide-zap");
        expect(loading).toContain("计算中");
        expect(failed).toContain("lucide-zap");
        expect(failed).toContain("报价失败");
        expect(failed).toContain("报价服务不可用");
    });
});
