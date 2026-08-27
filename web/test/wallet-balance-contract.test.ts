import { expect, test } from "bun:test";

const walletApi = Bun.file(new URL("../src/services/api/wallet.ts", import.meta.url));
const walletBalanceHook = Bun.file(new URL("../src/hooks/use-wallet-balance.ts", import.meta.url));

test("top-bar balance uses the dedicated lightweight balance endpoint", async () => {
    const [apiSource, hookSource] = await Promise.all([walletApi.text(), walletBalanceHook.text()]);

    expect(apiSource).toContain('api.get("/wallet/balance")');
    expect(hookSource).toContain('import { getWalletBalance } from "@/services/api/wallet"');
    expect(hookSource).toContain("const balance = await getWalletBalance()");
    expect(hookSource).not.toContain("getWallet(1, 1)");
});
