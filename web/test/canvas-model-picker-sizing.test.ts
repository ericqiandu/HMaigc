import { expect, test } from "bun:test";
import postcss from "postcss";

const styles = await Bun.file(new URL("../src/components/canvas/canvas-media-composer.css", import.meta.url)).text();
const root = postcss.parse(styles);

function declarationSetsFor(selector: string) {
    const declarationSets: Array<{ atRule: boolean; declarations: Map<string, string> }> = [];
    root.walkRules(selector, (rule) => {
        const declarations = new Map<string, string>();
        rule.walkDecls((declaration) => declarations.set(declaration.prop, declaration.value));
        declarationSets.push({ atRule: rule.parent?.type === "atrule", declarations });
    });
    return declarationSets;
}

test("canvas media model picker sizes to its model label instead of reserving a fixed 200px slot", () => {
    const slots = declarationSetsFor(".canvas-media-model-picker-slot");
    const baseSlot = slots.find((slot) => !slot.atRule)?.declarations;
    const responsiveSlot = slots.find((slot) => slot.atRule)?.declarations;

    expect(baseSlot?.get("width")).toBe("fit-content");
    expect(baseSlot?.get("min-width")).toBe("0");
    expect(baseSlot?.get("max-width")).toBe("180px");
    expect(baseSlot?.get("flex")).toBe("0 1 auto");
    expect(responsiveSlot?.get("width")).toBeUndefined();
    expect(responsiveSlot?.get("min-width")).toBeUndefined();
    expect(responsiveSlot?.get("flex-basis")).toBeUndefined();
});
