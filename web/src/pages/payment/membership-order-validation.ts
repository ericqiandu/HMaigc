export type MembershipSeatBounds = {
    maxSeats: number;
    minSeats: number;
};

type MembershipOrderValidationFacts = {
    audience: "personal" | "team";
    billingCycle: "month" | "year";
    code: string;
    creditsPerPeriod: number;
    currency: string;
    name: string;
    orderNumber: string;
    originalPriceCents: number;
    originalUnitPriceCents?: number;
    seats: number;
    tier: string;
    totalCredits: number;
    totalPriceCents: number;
    unitPriceCents?: number;
};

export type MembershipOrderValidationInput = MembershipOrderValidationFacts &
    (
        | {
              orderId: string;
              seatBounds: MembershipSeatBounds;
              source: "frozen-order";
          }
        | {
              source: "checkout";
          }
    );

export type ValidatedMembershipOrderFacts = MembershipOrderValidationInput & {
    originalUnitPriceCents: number;
    unitPriceCents: number;
};

function requireNonBlank(value: string, field: string): void {
    if (value.trim().length === 0) throw new Error(`${field}为空`);
}

function requireSafeNonNegativeInteger(value: number, field: string): void {
    if (!Number.isSafeInteger(value) || value < 0) throw new Error(`${field}必须是安全的非负整数`);
}

function requireSafePositiveInteger(value: number, field: string): void {
    if (!Number.isSafeInteger(value) || value < 1) throw new Error(`${field}必须是安全的正整数`);
}

function checkedProduct(left: number, right: number, field: string): number {
    const result = left * right;
    if (!Number.isSafeInteger(result)) throw new Error(`${field}超出安全整数范围`);
    return result;
}

function exactUnitValue(total: number, seats: number, field: string): number {
    if (total % seats !== 0) throw new Error(field);
    return total / seats;
}

function validateAudienceAndSeats(input: MembershipOrderValidationInput): void {
    requireSafePositiveInteger(input.seats, "会员席位数");
    if (input.audience === "personal") {
        if (input.seats !== 1) throw new Error("个人会员席位必须为 1");
        if (input.source === "frozen-order" && (input.seatBounds.minSeats !== 1 || input.seatBounds.maxSeats !== 1)) throw new Error("个人会员冻结席位范围无效");
        return;
    }
    if (input.audience !== "team") throw new Error("会员类型无效");
    if (input.seats < 2) throw new Error("团队会员席位必须至少为 2");
    if (input.source === "checkout") return;
    const { maxSeats, minSeats } = input.seatBounds;
    if (!Number.isSafeInteger(minSeats) || !Number.isSafeInteger(maxSeats) || minSeats < 2 || maxSeats < minSeats) throw new Error("团队会员冻结席位范围无效");
    if (input.seats < minSeats || input.seats > maxSeats) throw new Error("团队会员席位超出冻结范围");
}

export function validateMembershipOrderFacts(input: MembershipOrderValidationInput): ValidatedMembershipOrderFacts {
    if (input.source === "frozen-order" && input.orderId.trim().length === 0) throw new Error("订单 ID 为空");
    requireNonBlank(input.orderNumber, "会员订单号");
    requireNonBlank(input.code, "会员套餐编码");
    requireNonBlank(input.name, "会员套餐名称");
    requireNonBlank(input.tier, "会员套餐层级");
    requireNonBlank(input.currency, "会员订单币种");
    if (input.billingCycle !== "month" && input.billingCycle !== "year") throw new Error("会员计费周期无效");
    validateAudienceAndSeats(input);

    requireSafePositiveInteger(input.totalPriceCents, "会员实付总价");
    const audienceLabel = input.audience === "team" ? "团队" : "个人";
    const unitPriceCents = input.unitPriceCents ?? exactUnitValue(input.totalPriceCents, input.seats, `${audienceLabel}实付金额无法还原为单席冻结金额`);
    requireSafePositiveInteger(unitPriceCents, "会员实付单价");
    if (checkedProduct(unitPriceCents, input.seats, "会员实付金额") !== input.totalPriceCents) throw new Error("会员实付总价与单席金额不一致");

    requireSafeNonNegativeInteger(input.originalPriceCents, "会员原价合计");
    const originalUnitPriceCents = input.originalUnitPriceCents ?? exactUnitValue(input.originalPriceCents, input.seats, `${audienceLabel}原价无法还原为单席冻结金额`);
    requireSafeNonNegativeInteger(originalUnitPriceCents, "会员单席原价");
    if (checkedProduct(originalUnitPriceCents, input.seats, "会员原价合计") !== input.originalPriceCents) throw new Error("会员原价合计与单席原价不一致");
    if (originalUnitPriceCents < unitPriceCents || input.originalPriceCents < input.totalPriceCents) throw new Error("会员原价不得低于实付金额");

    requireSafeNonNegativeInteger(input.creditsPerPeriod, "会员单席积分");
    requireSafeNonNegativeInteger(input.totalCredits, "会员积分合计");
    if (checkedProduct(input.creditsPerPeriod, input.seats, "会员积分合计") !== input.totalCredits) throw new Error(`${audienceLabel}积分合计与冻结单席积分不一致`);

    return { ...input, originalUnitPriceCents, unitPriceCents };
}
