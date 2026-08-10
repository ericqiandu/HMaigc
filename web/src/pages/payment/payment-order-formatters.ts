const moneyFormatters = new Map<string, Intl.NumberFormat>();
const creditFormatter = new Intl.NumberFormat("zh-CN", { maximumFractionDigits: 2, minimumFractionDigits: 0 });

export function formatPaymentOrderMoney(cents: number, currency: string): string {
    const normalizedCurrency = currency.trim().toUpperCase();
    if (!normalizedCurrency) throw new Error("收银台币种不能为空");
    let formatter = moneyFormatters.get(normalizedCurrency);
    if (!formatter) {
        formatter = new Intl.NumberFormat("zh-CN", {
            currency: normalizedCurrency,
            currencyDisplay: "narrowSymbol",
            maximumFractionDigits: 2,
            minimumFractionDigits: 0,
            style: "currency",
        });
        moneyFormatters.set(normalizedCurrency, formatter);
    }
    return formatter.format(cents / 100);
}

export function formatPaymentOrderCredits(microcredits: number): string {
    return creditFormatter.format(microcredits / 1_000_000);
}
