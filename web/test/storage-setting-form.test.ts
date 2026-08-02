import { describe, expect, test } from "bun:test";

import type { AdminOSSSetting } from "../src/services/api/auth";
import { normalizeStorageFormValues, storageFieldErrors, storageFormValues, storageValuesEqual } from "../src/pages/admin/settings/storage-setting-form";

const setting: AdminOSSSetting = {
    enabled: true,
    provider: "aliyun",
    region: "oss-cn-hongkong",
    endpoint: "https://oss-cn-hongkong.aliyuncs.com",
    bucket: "hmaigc-prod-assets",
    accessKeyId: "saved-access-id",
    hasAccessKeySecret: true,
    publicBaseUrl: "",
    pathPrefix: "production/assets",
};

describe("storage setting form", () => {
    test("normalizes transport values without changing their meaning", () => {
        expect(
            normalizeStorageFormValues({
                enabled: true,
                provider: "aliyun",
                region: " oss-cn-hongkong ",
                endpoint: " https://oss-cn-hongkong.aliyuncs.com/ ",
                bucket: " hmaigc-prod-assets ",
                accessKeyId: " saved-access-id ",
                accessKeySecret: " ",
                pathPrefix: "/production/assets/",
            }),
        ).toEqual({
            enabled: true,
            provider: "aliyun",
            region: "oss-cn-hongkong",
            endpoint: "https://oss-cn-hongkong.aliyuncs.com",
            bucket: "hmaigc-prod-assets",
            accessKeyId: "saved-access-id",
            accessKeySecret: "",
            pathPrefix: "production/assets",
        });
    });

    test("treats a blank secret as retaining the saved secret", () => {
        expect(storageValuesEqual(storageFormValues(setting), setting)).toBe(true);
        expect(storageValuesEqual({ ...storageFormValues(setting), accessKeySecret: "replacement-secret" }, setting)).toBe(false);
    });

    test("requires active OSS fields and a valid HTTP endpoint", () => {
        expect(
            storageFieldErrors(
                {
                    enabled: true,
                    provider: "aliyun",
                    endpoint: "oss-cn-hongkong.aliyuncs.com",
                    bucket: "",
                    accessKeyId: "",
                    accessKeySecret: "",
                },
                false,
            ),
        ).toEqual({
            endpoint: "Endpoint 必须是有效的 HTTP(S) 地址",
            bucket: "启用 OSS 时必须填写 Bucket",
            accessKeyId: "启用 OSS 时必须填写 AccessKey ID",
            accessKeySecret: "首次启用 OSS 时必须填写 AccessKey Secret",
        });
    });

    test("does not require operational credentials while OSS is disabled", () => {
        expect(storageFieldErrors({ enabled: false, provider: "aliyun" }, false)).toEqual({});
    });
});
