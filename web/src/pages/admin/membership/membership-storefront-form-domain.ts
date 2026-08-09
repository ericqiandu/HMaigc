import type { MembershipStorefrontSetting } from "@/services/api/membership";

export type StorefrontGenerationSectionForm = Omit<MembershipStorefrontSetting["generationSections"][number], "rows"> & {
    rows: Array<Omit<MembershipStorefrontSetting["generationSections"][number]["rows"][number], "values"> & { values: string }>;
};

export function generationSectionsToForm(sections: MembershipStorefrontSetting["generationSections"]): StorefrontGenerationSectionForm[] {
    return sections.map((section) => ({
        ...section,
        rows: section.rows.map((row) => ({ ...row, values: row.values.join("\n") })),
    }));
}

export function generationSectionsFromForm(sections: StorefrontGenerationSectionForm[]): MembershipStorefrontSetting["generationSections"] {
    return sections.map((section) => ({
        ...section,
        rows: section.rows.map((row) => ({
            ...row,
            values: row.values
                .split(/\r?\n/)
                .map((value) => value.trim())
                .filter(Boolean),
        })),
    }));
}
