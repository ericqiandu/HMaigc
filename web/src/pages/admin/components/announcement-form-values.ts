import type { AnnouncementLevel } from "@/services/api/announcements";

export type AnnouncementFormValues = {
    title: string;
    content: string;
    level: AnnouncementLevel;
};

export const emptyAnnouncementForm: AnnouncementFormValues = {
    title: "",
    content: "",
    level: "info",
};

export const normalizeAnnouncementForm = (values: AnnouncementFormValues): AnnouncementFormValues => ({
    title: values.title.trim(),
    content: values.content.trim(),
    level: values.level,
});

export const announcementFormIsEmpty = (values: Partial<AnnouncementFormValues>): boolean =>
    (values.title?.trim() || "") === "" &&
    (values.content?.trim() || "") === "" &&
    (values.level === undefined || values.level === emptyAnnouncementForm.level);
