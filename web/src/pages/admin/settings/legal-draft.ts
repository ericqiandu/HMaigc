export type LegalDraft = {
    userAgreement: string;
    privacyPolicy: string;
    membershipAgreement: string;
};

export const emptyLegalDraft: LegalDraft = {
    userAgreement: "",
    privacyPolicy: "",
    membershipAgreement: "",
};

export function normalizeLegalDraft(draft: LegalDraft): LegalDraft {
    return {
        userAgreement: normalizeLegalHTML(draft.userAgreement),
        privacyPolicy: normalizeLegalHTML(draft.privacyPolicy),
        membershipAgreement: normalizeLegalHTML(draft.membershipAgreement),
    };
}

function normalizeLegalHTML(value: string): string {
    const normalized = value.trim();
    return normalized === "<p></p>" ? "" : normalized;
}

export function legalDraftsEqual(left: LegalDraft, right: LegalDraft): boolean {
    const normalizedLeft = normalizeLegalDraft(left);
    const normalizedRight = normalizeLegalDraft(right);
    return normalizedLeft.userAgreement === normalizedRight.userAgreement && normalizedLeft.privacyPolicy === normalizedRight.privacyPolicy && normalizedLeft.membershipAgreement === normalizedRight.membershipAgreement;
}
