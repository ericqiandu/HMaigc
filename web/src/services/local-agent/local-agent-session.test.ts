import assert from "node:assert/strict";
import { describe, it } from "node:test";

import { createLocalAgentSessionStore } from "./local-agent-session";

class FakeStorage implements Storage {
    readonly values = new Map<string, string>();
    get length() {
        return this.values.size;
    }
    clear() {
        this.values.clear();
    }
    getItem(key: string) {
        return this.values.get(key) ?? null;
    }
    key(index: number) {
        return [...this.values.keys()][index] ?? null;
    }
    removeItem(key: string) {
        this.values.delete(key);
    }
    setItem(key: string, value: string) {
        this.values.set(key, value);
    }
}

describe("local Agent session store", () => {
    it("accepts the 43-character base64url token produced from 32 random bytes", () => {
        const sessionStorage = new FakeStorage();
        const store = createLocalAgentSessionStore(sessionStorage);
        const generatedToken = "A".repeat(43);

        store.save({ baseUrl: "http://127.0.0.1:17371", token: generatedToken });

        assert.deepEqual(store.load(), { baseUrl: "http://127.0.0.1:17371", token: generatedToken });
    });

    it("persists endpoint and token only in the injected session storage", () => {
        const sessionStorage = new FakeStorage();
        const store = createLocalAgentSessionStore(sessionStorage);
        store.save({ baseUrl: "http://127.0.0.1:17371", token: "ab".repeat(32) });
        assert.deepEqual(store.load(), { baseUrl: "http://127.0.0.1:17371", token: "ab".repeat(32) });
        assert.equal(sessionStorage.length, 1);
        store.clear();
        assert.equal(store.load(), null);
    });

    it("rejects non-loopback endpoints and weak tokens", () => {
        const store = createLocalAgentSessionStore(new FakeStorage());
        assert.throws(() => store.save({ baseUrl: "https://example.com", token: "ab".repeat(32) }), /loopback/);
        assert.throws(() => store.save({ baseUrl: "http://127.0.0.1:17371", token: "short" }), /令牌/);
    });
});
