import { describe, expect, test } from "bun:test";

import {
    announcementFormIsEmpty,
    emptyAnnouncementForm,
    normalizeAnnouncementForm,
} from "../src/pages/admin/components/announcement-form-values";

describe("announcement form values", () => {
    test("treats the initial form and whitespace-only content as empty", () => {
        expect(announcementFormIsEmpty(emptyAnnouncementForm)).toBe(true);
        expect(announcementFormIsEmpty({ title: "  ", content: "\n", level: "info" })).toBe(true);
    });

    test("detects content or a changed level as an unsaved draft", () => {
        expect(announcementFormIsEmpty({ ...emptyAnnouncementForm, title: "服务通知" })).toBe(false);
        expect(announcementFormIsEmpty({ ...emptyAnnouncementForm, level: "critical" })).toBe(false);
    });

    test("trims the publish payload without changing its level", () => {
        expect(normalizeAnnouncementForm({ title: " 标题 ", content: " 正文\n", level: "warning" })).toEqual({
            title: "标题",
            content: "正文",
            level: "warning",
        });
    });
});
