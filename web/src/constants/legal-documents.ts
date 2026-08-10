export const legalDocumentRoutes = {
    userAgreement: "/legal/user-agreement",
    privacyPolicy: "/legal/privacy-policy",
    membershipAgreement: "/legal/membership-agreement",
} as const;

export type LegalDocumentKind = keyof typeof legalDocumentRoutes;

export type LegalDocumentDefinition = {
    kind: LegalDocumentKind;
    route: (typeof legalDocumentRoutes)[LegalDocumentKind];
    title: string;
    tabLabel: string;
    publicDescription: string;
    editorDescription: string;
    editorPlaceholder: string;
    emptyMessage: string;
};

export const legalDocumentDefinitions: readonly LegalDocumentDefinition[] = [
    {
        kind: "userAgreement",
        route: legalDocumentRoutes.userAgreement,
        title: "用户协议",
        tabLabel: "用户协议",
        publicDescription: "使用本平台服务前，请仔细阅读并理解本协议。",
        editorDescription: "建议覆盖账号使用、用户内容权利、付费服务、违约处理、知识产权和争议解决。",
        editorPlaceholder: "从用户协议第一条开始编辑……",
        emptyMessage: "用户协议尚未配置，请联系平台管理员。",
    },
    {
        kind: "privacyPolicy",
        route: legalDocumentRoutes.privacyPolicy,
        title: "隐私政策",
        tabLabel: "隐私政策",
        publicDescription: "了解平台如何收集、使用、保存和保护你的信息。",
        editorDescription: "建议说明信息收集范围、处理目的、保存期限、第三方共享规则与用户权利。",
        editorPlaceholder: "从隐私政策第一条开始编辑……",
        emptyMessage: "隐私政策尚未配置，请联系平台管理员。",
    },
    {
        kind: "membershipAgreement",
        route: legalDocumentRoutes.membershipAgreement,
        title: "HMaigc会员服务协议",
        tabLabel: "会员服务协议",
        publicDescription: "了解会员购买、服务期限、积分权益和双方权利义务。",
        editorDescription: "说明会员购买、服务期限、积分权益、退款边界、违约处理与争议解决；不得写入系统尚未支持的自动续费承诺。",
        editorPlaceholder: "从会员服务协议第一条开始编辑……",
        emptyMessage: "会员服务协议尚未发布",
    },
] as const;

export function legalDocumentDefinition(kind: LegalDocumentKind): LegalDocumentDefinition {
    const definition = legalDocumentDefinitions.find((candidate) => candidate.kind === kind);
    if (!definition) throw new Error(`未知法律文档类型：${kind}`);
    return definition;
}
