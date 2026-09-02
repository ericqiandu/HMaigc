import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { randomBytes } from "node:crypto";
import { createReadStream, statSync } from "node:fs";
import { access, mkdtemp, rm, stat } from "node:fs/promises";
import http from "node:http";
import net from "node:net";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";

import puppeteer from "puppeteer-core";

const webRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const repositoryRoot = path.resolve(webRoot, "..");
const backendRoot = path.join(repositoryRoot, "backend");
const distRoot = path.join(webRoot, "dist");
const localToken = randomBytes(48).toString("hex");
const wrongToken = randomBytes(48).toString("hex");
const username = `agent_gate_${randomBytes(5).toString("hex")}`;
const password = `AgentGate!${randomBytes(9).toString("hex")}`;
const canvasId = `canvas-agent-gate-${randomBytes(8).toString("hex")}`;
const nodeId = `agent-gate-node-${randomBytes(8).toString("hex")}`;
const expectedDelivery = {
    kind: "canvas_change",
    requiredArtifacts: [],
    targetCanvasId: canvasId,
    completionCriteria: [{ fact: "canvas_revision" }],
};
const activeAgentPromptSelector = '.canvas-agent-workspace-slot:not([hidden]) [aria-label="输入 Agent 指令"]';
const activeAgentSendSelector = '.canvas-agent-workspace-slot:not([hidden]) [aria-label="发送"]';

const mimeTypes = new Map([
    [".css", "text/css; charset=utf-8"],
    [".html", "text/html; charset=utf-8"],
    [".ico", "image/x-icon"],
    [".js", "text/javascript; charset=utf-8"],
    [".json", "application/json; charset=utf-8"],
    [".png", "image/png"],
    [".svg", "image/svg+xml"],
    [".webp", "image/webp"],
    [".woff2", "font/woff2"],
]);

function run(command, args, options = {}) {
    return new Promise((resolve, reject) => {
        const child = spawn(command, args, {
            cwd: options.cwd ?? repositoryRoot,
            env: options.env ?? process.env,
            windowsHide: true,
        });
        const stdout = [];
        const stderr = [];
        child.stdout.on("data", (chunk) => stdout.push(chunk));
        child.stderr.on("data", (chunk) => stderr.push(chunk));
        const timeout = setTimeout(() => child.kill("SIGKILL"), options.timeoutMs ?? 300_000);
        child.once("error", (error) => {
            clearTimeout(timeout);
            reject(error);
        });
        child.once("close", (code, signal) => {
            clearTimeout(timeout);
            const result = {
                code: code ?? -1,
                signal,
                stdout: Buffer.concat(stdout).toString("utf8"),
                stderr: Buffer.concat(stderr).toString("utf8"),
            };
            if (result.code === 0) resolve(result);
            else reject(new Error(`${command} ${args.join(" ")} 失败（exit ${result.code}${signal ? `, signal ${signal}` : ""}）\n${result.stdout}${result.stderr}`));
        });
    });
}

async function freePort() {
    const server = http.createServer();
    await new Promise((resolve, reject) => {
        server.once("error", reject);
        server.listen(0, "127.0.0.1", resolve);
    });
    const address = server.address();
    assert.ok(address && typeof address === "object", "无法分配随机端口");
    const port = address.port;
    await closeServer(server);
    return port;
}

function findChromium() {
    const candidates = [
        process.env.HMAIGC_CHROMIUM_EXECUTABLE,
        "C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe",
        "C:\\Program Files (x86)\\Google\\Chrome\\Application\\chrome.exe",
        "C:\\Program Files\\Microsoft\\Edge\\Application\\msedge.exe",
        "C:\\Program Files (x86)\\Microsoft\\Edge\\Application\\msedge.exe",
    ].filter(Boolean);
    return candidates.find((candidate) => {
        try {
            return statSync(candidate).isFile();
        } catch {
            return false;
        }
    });
}

function startBackend(executable, dataRoot, port, webOrigin) {
    const child = spawn(executable, [], {
        cwd: backendRoot,
        env: {
            ...process.env,
            CANVAS_BACKEND_ADDR: `127.0.0.1:${port}`,
            CANVAS_BACKEND_DATA_DIR: dataRoot,
            CANVAS_CORS_ORIGINS: webOrigin,
            CANVAS_ENVIRONMENT: "development",
            CANVAS_REGISTRATION_ENABLED: "true",
        },
        windowsHide: true,
    });
    const logs = [];
    child.stdout.on("data", (chunk) => logs.push(chunk.toString("utf8")));
    child.stderr.on("data", (chunk) => logs.push(chunk.toString("utf8")));
    return { child, logs };
}

async function waitForHealth(url, backend) {
    const deadline = Date.now() + 45_000;
    let lastError;
    while (Date.now() < deadline) {
        if (backend.child.exitCode !== null) throw new Error(`隔离后端提前退出\n${backend.logs.join("")}`);
        try {
            const response = await fetch(url);
            if (response.ok) return;
            lastError = new Error(`HTTP ${response.status}`);
        } catch (error) {
            lastError = error;
        }
        await delay(150);
    }
    throw new Error(`隔离后端未就绪：${lastError instanceof Error ? lastError.message : "unknown"}\n${backend.logs.join("")}`);
}

function createWebServer(backendPort) {
    const server = http.createServer(async (request, response) => {
        try {
            if (request.url?.startsWith("/api/")) {
                proxyHttp(request, response, backendPort);
                return;
            }
            const requestUrl = new URL(request.url ?? "/", "http://127.0.0.1");
            const decodedPath = decodeURIComponent(requestUrl.pathname);
            const relativePath = decodedPath === "/" ? "index.html" : decodedPath.replace(/^\/+/, "");
            const candidate = path.resolve(distRoot, relativePath);
            let filePath = candidate.startsWith(`${path.resolve(distRoot)}${path.sep}`) || candidate === path.resolve(distRoot) ? candidate : path.join(distRoot, "index.html");
            try {
                if (!(await stat(filePath)).isFile()) filePath = path.join(distRoot, "index.html");
            } catch {
                filePath = path.join(distRoot, "index.html");
            }
            response.statusCode = 200;
            response.setHeader("Content-Type", mimeTypes.get(path.extname(filePath).toLowerCase()) ?? "application/octet-stream");
            response.setHeader("Cache-Control", filePath.endsWith("index.html") ? "no-store" : "public, max-age=31536000, immutable");
            createReadStream(filePath).pipe(response);
        } catch (error) {
            response.statusCode = 500;
            response.end(error instanceof Error ? error.message : "static server failed");
        }
    });
    server.on("upgrade", (request, socket, head) => proxyUpgrade(request, socket, head, backendPort));
    return server;
}

function proxyHttp(request, response, backendPort) {
    const upstream = http.request(
        {
            hostname: "127.0.0.1",
            port: backendPort,
            path: request.url,
            method: request.method,
            headers: { ...request.headers, host: `127.0.0.1:${backendPort}` },
        },
        (upstreamResponse) => {
            response.writeHead(upstreamResponse.statusCode ?? 502, upstreamResponse.headers);
            upstreamResponse.pipe(response);
        },
    );
    upstream.on("error", (error) => {
        if (!response.headersSent) response.writeHead(502, { "Content-Type": "application/json" });
        response.end(JSON.stringify({ code: 502, data: null, msg: error.message }));
    });
    request.pipe(upstream);
}

function proxyUpgrade(request, browserSocket, head, backendPort) {
    const upstream = net.createConnection({ host: "127.0.0.1", port: backendPort }, () => {
        const lines = [`${request.method ?? "GET"} ${request.url ?? "/"} HTTP/${request.httpVersion}`];
        for (const [name, value] of Object.entries(request.headers)) {
            if (value !== undefined) lines.push(`${name}: ${Array.isArray(value) ? value.join(", ") : value}`);
        }
        upstream.write(`${lines.join("\r\n")}\r\n\r\n`);
        if (head.length) upstream.write(head);
        browserSocket.pipe(upstream).pipe(browserSocket);
    });
    upstream.on("error", () => browserSocket.destroy());
    browserSocket.on("error", () => upstream.destroy());
}

function createLocalAgentStub(origin) {
    const streams = new Set();
    const requestFacts = [];
    const toolResults = [];
    let historyReads = 0;
    let activeThreadId = "local-history-thread";
    let activeTurnId = "local-history-turn";
    let applyAttempt = 0;
    let baseRevision = 0;
    const now = () => new Date().toISOString();
    const historyThread = () => ({
        threadId: "local-history-thread",
        canvasId,
        model: "gpt-5-codex",
        createdAt: now(),
        updatedAt: now(),
        turns: [
            {
                turnId: "local-history-turn",
                status: "completed",
                message: "历史验收对话",
                attachments: [],
                events: [],
                createdAt: now(),
                completedAt: now(),
            },
        ],
    });
    const activeThread = () => ({
        threadId: activeThreadId,
        canvasId,
        model: "gpt-5-codex",
        createdAt: now(),
        updatedAt: now(),
        turns: [
            {
                turnId: activeTurnId,
                status: applyAttempt >= 2 ? "completed" : "running",
                message: "请读取画布并新增一个验收文本节点",
                attachments: [],
                events: [],
                createdAt: now(),
                ...(applyAttempt >= 2 ? { completedAt: now() } : {}),
            },
        ],
    });
    const send = (event) => {
        const frame = `data: ${JSON.stringify(event)}\n\n`;
        for (const response of streams) response.write(frame);
    };
    const toolCall = (requestId, toolName, argumentsValue) => ({
        protocolVersion: 1,
        kind: "tool_call",
        requestId,
        threadId: activeThreadId,
        turnId: activeTurnId,
        toolName,
        arguments: argumentsValue,
        expectedDelivery,
        createdAt: now(),
    });
    const emitApply = () => {
        applyAttempt += 1;
        send(
            toolCall(`apply-request-${applyAttempt}`, "canvas.apply_ops", {
                canvasId,
                baseRevision,
                clientMutationId: `agent-gate-mutation-${applyAttempt}`,
                operations: [
                    {
                        operationId: `agent-gate-operation-${applyAttempt}`,
                        type: "add_node",
                        node: {
                            id: nodeId,
                            type: "text",
                            title: "浏览器门禁节点",
                            position: { x: 120, y: 120 },
                            width: 240,
                            height: 120,
                            metadata: { content: "由本机 Codex 双模式浏览器门禁写入" },
                        },
                    },
                ],
            }),
        );
    };

    const server = http.createServer(async (request, response) => {
        setLocalCors(response, origin);
        if (request.method === "OPTIONS") {
            response.writeHead(204);
            response.end();
            return;
        }
        const requestUrl = new URL(request.url ?? "/", "http://127.0.0.1");
        const bodyText = await readBody(request);
        requestFacts.push({ method: request.method ?? "GET", path: requestUrl.pathname, body: bodyText });
        if (requestUrl.pathname === "/health" && request.method === "GET") {
            json(response, 200, { version: "browser-gate", protocolVersion: 1, ready: true });
            return;
        }
        if (request.headers["x-hmaigc-agent-token"] !== localToken) {
            json(response, 401, { error: { code: "local_agent_unauthorized", message: "本机 Agent 令牌无效" } });
            return;
        }
        if (requestUrl.pathname === "/events" && request.method === "GET") {
            response.writeHead(200, {
                "Cache-Control": "no-cache",
                Connection: "keep-alive",
                "Content-Type": "text/event-stream; charset=utf-8",
            });
            streams.add(response);
            response.write(`data: ${JSON.stringify({ kind: "connected" })}\n\n`);
            request.once("close", () => streams.delete(response));
            return;
        }
        if (requestUrl.pathname === "/agent/codex/threads" && request.method === "GET") {
            historyReads += 1;
            json(response, 200, { threads: [historyThread()] });
            return;
        }
        if (requestUrl.pathname === "/agent/codex/threads/local-history-thread" && request.method === "GET") {
            historyReads += 1;
            json(response, 200, historyThread());
            return;
        }
        if (requestUrl.pathname === "/agent/codex/turns" && request.method === "POST") {
            const input = JSON.parse(bodyText);
            activeThreadId = typeof input.threadId === "string" && input.threadId ? input.threadId : `local-thread-${randomBytes(4).toString("hex")}`;
            activeTurnId = `local-turn-${randomBytes(4).toString("hex")}`;
            json(response, 200, { threadId: activeThreadId, turnId: activeTurnId });
            setTimeout(() => {
                send({ kind: "thread_started", threadId: activeThreadId, sdkThreadId: "sdk-browser-gate" });
                send({ kind: "turn_started", threadId: activeThreadId, turnId: activeTurnId });
                send(toolCall("read-request-1", "canvas.read", { canvasId, selectedNodeIds: [], includeViewport: true }));
            }, 30);
            return;
        }
        if (requestUrl.pathname.startsWith("/tools/") && requestUrl.pathname.endsWith("/results") && request.method === "POST") {
            const result = JSON.parse(bodyText);
            toolResults.push(result);
            json(response, 204, undefined);
            if (result.requestId === "read-request-1" && result.succeeded === true) {
                assert.ok(Number.isSafeInteger(result.output?.revision), "canvas.read 未返回权威 revision");
                baseRevision = result.output.revision;
                setTimeout(emitApply, 30);
            } else if (result.requestId === "apply-request-1" && result.succeeded === false) {
                setTimeout(emitApply, 30);
            } else if (result.requestId === "apply-request-2" && result.succeeded === true) {
                setTimeout(() => {
                    send({ kind: "final_decision", threadId: activeThreadId, turnId: activeTurnId, message: "画布节点已写入并由后端确认。", expectedDelivery });
                    send({ kind: "turn_completed", threadId: activeThreadId, turnId: activeTurnId, event: {} });
                }, 30);
            }
            return;
        }
        if (requestUrl.pathname === `/agent/codex/turns/${encodeURIComponent(activeTurnId)}/cancel` && request.method === "POST") {
            json(response, 204, undefined);
            return;
        }
        if (requestUrl.pathname === `/agent/codex/threads/${encodeURIComponent(activeThreadId)}` && request.method === "GET") {
            json(response, 200, activeThread());
            return;
        }
        json(response, 404, { error: { code: "local_agent_not_found", message: "浏览器门禁路由不存在" } });
    });
    return {
        server,
        requestFacts,
        toolResults,
        historyReads: () => historyReads,
        closeStreams: () => {
            for (const response of streams) response.destroy();
            streams.clear();
        },
    };
}

function setLocalCors(response, origin) {
    response.setHeader("Access-Control-Allow-Origin", origin);
    response.setHeader("Access-Control-Allow-Headers", "Content-Type, X-HMaigc-Agent-Token");
    response.setHeader("Access-Control-Allow-Methods", "GET, POST, OPTIONS");
    response.setHeader("Vary", "Origin");
}

function json(response, status, body) {
    response.statusCode = status;
    if (body === undefined) {
        response.end();
        return;
    }
    response.setHeader("Content-Type", "application/json; charset=utf-8");
    response.end(JSON.stringify(body));
}

async function readBody(request) {
    const chunks = [];
    for await (const chunk of request) chunks.push(chunk);
    return Buffer.concat(chunks).toString("utf8");
}

async function listen(server, port) {
    await new Promise((resolve, reject) => {
        server.once("error", reject);
        server.listen(port, "127.0.0.1", resolve);
    });
}

async function closeServer(server) {
    if (!server.listening) return;
    await new Promise((resolve) => server.close(() => resolve()));
}

async function stopChild(child) {
    if (!child || child.exitCode !== null) return;
    child.kill("SIGTERM");
    const exited = await Promise.race([new Promise((resolve) => child.once("close", () => resolve(true))), delay(5_000).then(() => false)]);
    if (exited) return;
    if (process.platform === "win32" && child.pid) {
        await run("taskkill", ["/PID", String(child.pid), "/T", "/F"], { timeoutMs: 15_000 }).catch(() => undefined);
    } else {
        child.kill("SIGKILL");
    }
}

function delay(milliseconds) {
    return new Promise((resolve) => setTimeout(resolve, milliseconds));
}

async function api(page, pathValue, init = {}) {
    return page.evaluate(
        async (request) => {
            const response = await fetch(request.path, {
                method: request.method ?? "GET",
                credentials: "include",
                headers: request.body === undefined ? undefined : { "Content-Type": "application/json" },
                body: request.body === undefined ? undefined : JSON.stringify(request.body),
            });
            const payload = await response.json();
            return { status: response.status, payload };
        },
        { path: pathValue, ...init },
    );
}

async function waitForText(page, textValue, timeout = 15_000) {
    try {
        await page.waitForFunction((expected) => document.body.innerText.includes(expected), { timeout }, textValue);
    } catch (cause) {
        const snapshot = await page.evaluate(() => document.body.innerText.slice(-2_000));
        throw new Error(`等待页面文本“${textValue}”超时；当前页面末尾：\n${snapshot}`, { cause });
    }
}

async function clickText(page, selector, textValue) {
    const clicked = await page.evaluate(
        (candidateSelector, expected) => {
            const normalizedExpected = expected.replace(/\s+/gu, "");
            const element = [...document.querySelectorAll(candidateSelector)].find((candidate) => candidate.textContent?.replace(/\s+/gu, "") === normalizedExpected);
            if (!(element instanceof HTMLElement)) return false;
            element.click();
            return true;
        },
        selector,
        textValue,
    );
    assert.ok(clicked, `找不到可点击文本“${textValue}”`);
}

async function fillInputs(page, selector, values) {
    const count = await page.evaluate(
        (candidateSelector, nextValues) => {
            const inputs = [...document.querySelectorAll(candidateSelector)].filter((candidate) => candidate instanceof HTMLInputElement);
            const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")?.set;
            if (!setter) throw new Error("浏览器没有暴露 input value setter");
            inputs.forEach((input, index) => {
                setter.call(input, nextValues[index] ?? "");
                input.dispatchEvent(new Event("input", { bubbles: true }));
            });
            return inputs.length;
        },
        selector,
        values,
    );
    assert.equal(count, values.length, `输入字段数量错误：期望 ${values.length}，实际 ${count}`);
}

async function clickEnabled(page, selector) {
    const clicked = await page.evaluate((candidateSelector) => {
        const element = document.querySelector(candidateSelector);
        if (!(element instanceof HTMLButtonElement) || element.disabled) return false;
        element.click();
        return true;
    }, selector);
    assert.ok(clicked, `按钮不可用或不存在：${selector}`);
}

async function waitForApproval(page, diagnostics) {
    try {
        await page.waitForSelector('[aria-label="Agent 执行审批"]', { timeout: 30_000 });
    } catch (cause) {
        const snapshot = await page.evaluate(() => document.body.innerText.slice(-3_000));
        throw new Error(
            [
                "等待 Agent 执行审批超时",
                `页面：${snapshot}`,
                `本机请求：${JSON.stringify(diagnostics.requestFacts)}`,
                `工具结果：${JSON.stringify(diagnostics.toolResults)}`,
                `浏览器控制台：${diagnostics.consoleEntries.slice(-20).join("\n")}`,
                `后端日志：${diagnostics.backendLogs.slice(-20).join("")}`,
            ].join("\n"),
            { cause },
        );
    }
}

async function readRemoteCanvas(page) {
    const response = await api(page, `/api/canvas-projects/${encodeURIComponent(canvasId)}`);
    assert.equal(response.status, 200, `读取隔离画布失败：${JSON.stringify(response.payload)}`);
    assert.equal(response.payload.code, 0, `读取隔离画布返回业务错误：${JSON.stringify(response.payload)}`);
    return response.payload.data.project;
}

async function main() {
    await access(path.join(distRoot, "index.html"));
    const chromium = findChromium();
    assert.ok(chromium, "未找到可用于 Agent 双模式门禁的 Chrome/Edge");
    const tempRoot = await mkdtemp(path.join(os.tmpdir(), "hmaigc-agent-dual-mode-gate-"));
    const backendExecutable = path.join(tempRoot, process.platform === "win32" ? "agent-browser-backend.exe" : "agent-browser-backend");
    const dataRoot = path.join(tempRoot, "backend-data");
    const backendPort = await freePort();
    const webPort = await freePort();
    const localPort = await freePort();
    const webOrigin = `http://127.0.0.1:${webPort}`;
    const localOrigin = `http://127.0.0.1:${localPort}`;
    const webServer = createWebServer(backendPort);
    const localStub = createLocalAgentStub(webOrigin);
    let backend;
    let browser;
    let page;
    let primaryError;
    const cleanupErrors = [];
    try {
        process.stdout.write("[1/8] 构建隔离后端并启动真实 Web/API 环境\n");
        await run("go", ["build", "-o", backendExecutable, "./cmd/server"], { cwd: backendRoot, timeoutMs: 300_000 });
        await listen(webServer, webPort);
        await listen(localStub.server, localPort);
        backend = startBackend(backendExecutable, dataRoot, backendPort, webOrigin);
        await waitForHealth(`http://127.0.0.1:${backendPort}/api/health`, backend);

        browser = await puppeteer.launch({ executablePath: chromium, headless: true, args: ["--disable-dev-shm-usage", "--no-sandbox"] });
        page = await browser.newPage();
        await page.setViewport({ width: 1440, height: 900, deviceScaleFactor: 1 });
        const pageErrors = [];
        const consoleEntries = [];
        const browserRequests = [];
        page.on("pageerror", (error) => pageErrors.push(error.message));
        page.on("console", (entry) => consoleEntries.push(`${entry.type()}: ${entry.text()}`));
        page.on("request", (request) => browserRequests.push({ url: request.url(), body: request.postData() ?? "" }));

        process.stdout.write("[2/8] 创建隔离账号、业务项目和绑定画布，并验证真实登录\n");
        await page.goto(`${webOrigin}/login`, { waitUntil: "domcontentloaded", timeout: 30_000 });
        const registration = await api(page, "/api/auth/register", {
            method: "POST",
            body: { username, email: "", emailCode: "", displayName: "Agent 门禁", password, inviteCode: "" },
        });
        assert.equal(registration.status, 200, `隔离账号注册失败：${JSON.stringify(registration.payload)}`);
        const domainProjectResponse = await api(page, "/api/projects", {
            method: "POST",
            body: { name: "Agent 双模式浏览器门禁", type: "short-drama", aspectRatio: "16:9", sourceType: "blank" },
        });
        assert.equal(domainProjectResponse.status, 200, `业务项目创建失败：${JSON.stringify(domainProjectResponse.payload)}`);
        const domainProjectId = domainProjectResponse.payload.data.project.id;
        const createdAt = new Date().toISOString();
        const initialProject = {
            id: canvasId,
            projectId: domainProjectId,
            title: "Agent 双模式浏览器门禁",
            createdAt,
            updatedAt: createdAt,
            nodes: [],
            connections: [],
            chatSessions: [],
            activeChatId: null,
            backgroundMode: "lines",
            showImageInfo: false,
            viewport: { x: 0, y: 0, k: 1 },
            directorScenes: [],
        };
        const canvasResponse = await api(page, `/api/canvas-projects/${encodeURIComponent(canvasId)}`, { method: "PUT", body: { project: initialProject } });
        assert.equal(canvasResponse.status, 200, `绑定画布创建失败：${JSON.stringify(canvasResponse.payload)}`);
        const logout = await api(page, "/api/auth/logout", { method: "POST", body: {} });
        assert.equal(logout.status, 200, "隔离账号退出失败");
        await page.goto(`${webOrigin}/login?next=${encodeURIComponent(`/canvas/${canvasId}`)}`, { waitUntil: "domcontentloaded", timeout: 30_000 });
        await page.waitForSelector('input[placeholder="用户名或邮箱"]', { timeout: 15_000 });
        await page.type('input[placeholder="用户名或邮箱"]', username);
        await page.type('input[placeholder="请输入密码"]', password);
        await page.click(".auth-legal-checkbox");
        await page.click(".auth-primary-button");
        await page.waitForFunction((id) => location.pathname === `/canvas/${id}`, { timeout: 30_000 }, canvasId);
        await page.waitForSelector('[aria-label="Agent"]', { timeout: 30_000 });

        process.stdout.write("[3/8] 验证默认网站 Agent 与网站草稿隔离保存\n");
        await page.click('[aria-label="Agent"]');
        await page.waitForSelector('[aria-label="使用网站 Agent"][aria-pressed="true"]', { timeout: 15_000 });
        const managedDraft = "网站 Agent 草稿必须在双模式切换后保留";
        await page.waitForSelector(activeAgentPromptSelector, { timeout: 15_000 });
        await page.type(activeAgentPromptSelector, managedDraft);

        process.stdout.write("[4/8] 验证错误令牌显式失败、正确令牌连接与本机历史\n");
        await page.click('[aria-label="使用本机 Codex"]');
        await page.waitForSelector(".canvas-agent-local-connect-input", { timeout: 15_000 });
        await fillInputs(page, ".canvas-agent-local-connect-field input", [localOrigin, wrongToken]);
        await page.waitForFunction(
            () => {
                const button = document.querySelector(".canvas-agent-local-connect-submit");
                return button instanceof HTMLButtonElement && !button.disabled;
            },
            { timeout: 15_000 },
        );
        await page.click(".canvas-agent-local-connect-submit");
        await waitForText(page, "本机 Agent 令牌无效");
        await fillInputs(page, ".canvas-agent-local-connect-field input", [localOrigin, localToken]);
        await page.waitForFunction(
            () => {
                const button = document.querySelector(".canvas-agent-local-connect-submit");
                return button instanceof HTMLButtonElement && !button.disabled;
            },
            { timeout: 15_000 },
        );
        await page.click(".canvas-agent-local-connect-submit");
        await waitForText(page, "本机 Codex 已连接", 20_000);
        await page.click('[aria-label="本机历史"]');
        await waitForText(page, "历史验收对话");
        assert.ok(localStub.historyReads() >= 1, "本机历史没有通过本机 Agent 服务读取");
        await page.click('[aria-label="本机历史"]');

        process.stdout.write("[5/8] 验证 canvas.read 与首次写入拒绝不改变 revision\n");
        const before = await readRemoteCanvas(page);
        const baselineRevision = before.revision;
        assert.equal(before.nodes.length, 0, "隔离画布初始节点不为空");
        await page.type(activeAgentPromptSelector, "请读取画布并新增一个验收文本节点");
        await clickEnabled(page, activeAgentSendSelector);
        await waitForApproval(page, {
            requestFacts: localStub.requestFacts,
            toolResults: localStub.toolResults,
            consoleEntries,
            backendLogs: backend.logs,
        });
        await clickText(page, "button", "拒绝执行");
        await page.waitForFunction(() => !document.querySelector('[aria-label="Agent 执行审批"]'), { timeout: 20_000 });
        const afterReject = await readRemoteCanvas(page);
        assert.equal(afterReject.revision, baselineRevision, "拒绝写入后画布 revision 发生变化");
        assert.equal(afterReject.nodes.length, 0, "拒绝写入后仍新增了节点");

        process.stdout.write("[6/8] 验证第二次写入批准、精确 revision 增长与刷新持久化\n");
        await waitForApproval(page, {
            requestFacts: localStub.requestFacts,
            toolResults: localStub.toolResults,
            consoleEntries,
            backendLogs: backend.logs,
        });
        await clickText(page, "button", "批准执行");
        await waitForText(page, "画布节点已写入并由后端确认。", 30_000);
        const afterApprove = await readRemoteCanvas(page);
        assert.equal(afterApprove.revision, baselineRevision + 1, "批准写入后画布 revision 没有精确增加 1");
        assert.ok(
            afterApprove.nodes.some((node) => node.id === nodeId),
            "批准写入后权威画布缺少目标节点",
        );
        await page.click('[aria-label="使用网站 Agent"]');
        await page.waitForSelector('[aria-label="使用网站 Agent"][aria-pressed="true"]', { timeout: 15_000 });
        const switchedDraft = await page.$eval(activeAgentPromptSelector, (element) => element.value);
        assert.equal(switchedDraft, managedDraft, "网站 Agent 草稿在双模式切换后丢失");
        await page.click('[aria-label="使用本机 Codex"]');
        await page.waitForSelector('[aria-label="使用本机 Codex"][aria-pressed="true"]', { timeout: 15_000 });
        await page.reload({ waitUntil: "domcontentloaded", timeout: 30_000 });
        await page.waitForSelector('[aria-label="Agent"]', { timeout: 30_000 });
        const afterReload = await readRemoteCanvas(page);
        assert.equal(afterReload.revision, baselineRevision + 1, "刷新后画布 revision 不一致");
        assert.ok(
            afterReload.nodes.some((node) => node.id === nodeId),
            "刷新后目标节点没有持久化",
        );

        process.stdout.write("[7/8] 验证本机断流不自动切换、网站草稿保留与令牌不泄漏\n");
        await page.click('[aria-label="Agent"]');
        await page.waitForSelector('[aria-label="使用网站 Agent"][aria-pressed="true"]', { timeout: 15_000 });
        await page.click('[aria-label="使用本机 Codex"]');
        await page.waitForSelector(".canvas-agent-local-connect-field input", { timeout: 15_000 });
        const restoredConnection = await page.$$eval(".canvas-agent-local-connect-field input", (inputs) => inputs.map((input) => (input instanceof HTMLInputElement ? input.value : "")));
        assert.deepEqual(restoredConnection, [localOrigin, localToken], "刷新后本机连接配置没有从专用 session 恢复");
        await clickEnabled(page, ".canvas-agent-local-connect-submit");
        await waitForText(page, "本机 Codex 已连接", 20_000);
        localStub.closeStreams();
        await waitForText(page, "本机 Agent 事件流已断开", 15_000);
        assert.ok(await page.$('[aria-label="使用本机 Codex"][aria-pressed="true"]'), "本机断流后被静默切回网站 Agent");
        await page.click('[aria-label="使用网站 Agent"]');
        await page.waitForSelector('[aria-label="使用网站 Agent"][aria-pressed="true"]', { timeout: 15_000 });

        const storageFacts = await page.evaluate(() => ({
            dom: document.documentElement.innerHTML,
            local: Object.fromEntries(Object.keys(localStorage).map((key) => [key, localStorage.getItem(key)])),
            session: Object.fromEntries(Object.keys(sessionStorage).map((key) => [key, sessionStorage.getItem(key)])),
        }));
        assert.ok(storageFacts.session["hmaigc-local-agent-connection:v1"]?.includes(localToken), "本机令牌没有存入专用 sessionStorage 键");
        for (const [key, value] of Object.entries(storageFacts.session)) {
            if (key === "hmaigc-local-agent-connection:v1") continue;
            assert.ok(!String(value).includes(localToken), `本机令牌泄漏到其他 sessionStorage 键：${key}`);
        }
        const leakSurfaces = [storageFacts.dom, JSON.stringify(storageFacts.local), JSON.stringify(browserRequests), JSON.stringify(localStub.requestFacts), consoleEntries.join("\n"), backend.logs.join("")];
        leakSurfaces.forEach((surface, index) => assert.ok(!surface.includes(localToken), `本机令牌泄漏到检查面 ${index + 1}`));
        assert.deepEqual(pageErrors, [], `浏览器页面出现脚本异常：${pageErrors.join(" | ")}`);
        assert.ok(
            localStub.toolResults.some((result) => result.requestId === "read-request-1" && result.succeeded === true),
            "canvas.read 权威结果未回传本机 Agent",
        );
        assert.ok(
            localStub.toolResults.some((result) => result.requestId === "apply-request-1" && result.succeeded === false),
            "拒绝事实未回传本机 Agent",
        );
        assert.ok(
            localStub.toolResults.some((result) => result.requestId === "apply-request-2" && result.succeeded === true),
            "批准写入结果未回传本机 Agent",
        );

        process.stdout.write("[8/8] Agent 双模式真实浏览器门禁通过\n");
    } catch (error) {
        primaryError = error;
    } finally {
        if (page) await page.close().catch((error) => cleanupErrors.push(error));
        if (browser) await browser.close().catch((error) => cleanupErrors.push(error));
        localStub.closeStreams();
        await closeServer(localStub.server).catch((error) => cleanupErrors.push(error));
        await closeServer(webServer).catch((error) => cleanupErrors.push(error));
        if (backend) await stopChild(backend.child).catch((error) => cleanupErrors.push(error));
        const expectedPrefix = path.resolve(path.join(os.tmpdir(), "hmaigc-agent-dual-mode-gate-"));
        if (!path.resolve(tempRoot).startsWith(expectedPrefix)) cleanupErrors.push(new Error(`拒绝清理非门禁临时目录：${tempRoot}`));
        else await rm(tempRoot, { recursive: true, force: true }).catch((error) => cleanupErrors.push(error));
    }
    if (primaryError && cleanupErrors.length) throw new AggregateError([primaryError, ...cleanupErrors], "Agent 双模式门禁与资源清理同时失败");
    if (primaryError) throw primaryError;
    if (cleanupErrors.length) throw new AggregateError(cleanupErrors, "Agent 双模式门禁资源清理失败");
}

await main();
