import { describe, expect, test } from "bun:test";

type RetryDecision = (input: { attempt: number; completedCases: number; failedResponses: string[]; requestFailures: string[] }) => boolean;

describe("membership checkout Chromium startup retry", () => {
    test("retries exactly once only when every initial asset request failed because Chromium changed networks", async () => {
        const retryModule = await import("../scripts/membership-checkout-browser-network-retry.mjs");
        expect(typeof retryModule.shouldRetryInitialNetworkChange).toBe("function");
        const shouldRetry = retryModule.shouldRetryInitialNetworkChange as RetryDecision;
        const networkChanged = {
            attempt: 0,
            completedCases: 0,
            failedResponses: [],
            requestFailures: ["net::ERR_NETWORK_CHANGED http://127.0.0.1:32776/assets/app.js", "net::ERR_NETWORK_CHANGED http://127.0.0.1:32776/assets/chunk.js"],
        };

        expect(shouldRetry(networkChanged)).toBe(true);
        expect(shouldRetry({ ...networkChanged, attempt: 1 })).toBe(false);
        expect(shouldRetry({ ...networkChanged, completedCases: 1 })).toBe(false);
        expect(shouldRetry({ ...networkChanged, failedResponses: ["500 http://127.0.0.1:32776/assets/app.js"] })).toBe(false);
        expect(shouldRetry({ ...networkChanged, requestFailures: [...networkChanged.requestFailures, "net::ERR_CONNECTION_REFUSED http://127.0.0.1:32776/assets/fail.js"] })).toBe(false);
        expect(shouldRetry({ ...networkChanged, requestFailures: [] })).toBe(false);
    });
});
