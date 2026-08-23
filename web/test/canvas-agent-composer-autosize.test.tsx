import "./setup-happy-dom";

import { afterEach, describe, expect, test } from "bun:test";
import { act, createElement, useState } from "react";
import { createRoot, type Root } from "react-dom/client";

import { AgentChatComposer } from "../src/components/canvas/canvas-agent-chat-ui";
import { canvasThemes } from "../src/lib/canvas-theme";

let root: Root | null = null;

afterEach(async () => {
    if (root) await act(async () => root?.unmount());
    root = null;
    document.body.replaceChildren();
});

describe("Agent composer auto sizing", () => {
    test("grows the textarea to the measured content height as the prompt expands", async () => {
        const host = document.createElement("div");
        document.body.append(host);
        root = createRoot(host);

        function Harness() {
            const [prompt, setPrompt] = useState("");
            return createElement(AgentChatComposer, {
                prompt,
                placeholder: "描述你的创作需求",
                theme: canvasThemes.light,
                onPromptChange: setPrompt,
                onSubmit: () => undefined,
            });
        }

        await act(async () => root?.render(createElement(Harness)));

        const textarea = document.querySelector<HTMLTextAreaElement>(".canvas-agent-composer-textarea");
        expect(textarea).not.toBeNull();
        Object.defineProperty(textarea, "scrollHeight", { configurable: true, value: 168 });

        await act(async () => {
            if (!textarea) return;
            textarea.value = "第一段创作需求\n第二段人物设定\n第三段场景和镜头要求";
            textarea.dispatchEvent(new Event("input", { bubbles: true }));
        });

        expect(textarea?.style.height).toBe("168px");
    });
});
