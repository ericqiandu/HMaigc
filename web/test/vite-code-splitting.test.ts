import { describe, expect, test } from "bun:test";

import { criticalUiCodeSplittingGroup } from "../vite.config";

describe("Vite critical UI code splitting", () => {
    test("groups only the eager Ant Design provider roots and their dependency subtree", () => {
        expect(criticalUiCodeSplittingGroup.name).toBe("app-ui-core");
        expect(criticalUiCodeSplittingGroup.test.test("C:/repo/node_modules/antd/es/app/index.js")).toBe(true);
        expect(criticalUiCodeSplittingGroup.test.test("C:/repo/node_modules/antd/es/config-provider/index.js")).toBe(true);
        expect(criticalUiCodeSplittingGroup.test.test("C:/repo/node_modules/antd/es/table/index.js")).toBe(false);
        expect(criticalUiCodeSplittingGroup.test.test("C:/repo/node_modules/@ant-design/icons/es/icons/HomeOutlined.js")).toBe(false);
        expect(criticalUiCodeSplittingGroup.includeDependenciesRecursively).toBe(true);
    });
});
