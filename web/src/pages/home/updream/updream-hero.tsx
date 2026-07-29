import { ArrowUp, ShieldCheck, Zap } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { useNavigate } from "react-router";
import { App } from "antd";

import { createAgentCanvasProjectWithRemoteSync } from "@/services/user-data-sync";
import type { CanvasAgentExecutionMode } from "@/types/canvas";

const PLACEHOLDERS = [
    '试试说"在画布上为我创建…"，生成不阻塞，随时开启下一轮对话',
    "描述你想创作的内容，AI 帮你生成分镜",
    "进入项目后，按 @ 可引用资产库素材",
] as const;

export function UpdreamHero() {
    const { message } = App.useApp();
    const navigate = useNavigate();
    const [value, setValue] = useState("");
    const [executionMode, setExecutionMode] = useState<CanvasAgentExecutionMode>("guided");
    const [submitting, setSubmitting] = useState(false);
    const [placeholderIndex, setPlaceholderIndex] = useState(0);
    const [placeholderVisible, setPlaceholderVisible] = useState(true);
    const transitionTimeoutRef = useRef<number | null>(null);

    useEffect(() => {
        const timer = window.setInterval(() => {
            setPlaceholderVisible(false);
            transitionTimeoutRef.current = window.setTimeout(() => {
                setPlaceholderIndex((index) => (index + 1) % PLACEHOLDERS.length);
                setPlaceholderVisible(true);
            }, 300);
        }, 3600);

        return () => {
            window.clearInterval(timer);
            if (transitionTimeoutRef.current !== null) window.clearTimeout(transitionTimeoutRef.current);
        };
    }, []);

    const startCreating = async () => {
        const prompt = value.trim();
        if (!prompt || submitting) return;
        setSubmitting(true);
        try {
            const { id, syncError } = await createAgentCanvasProjectWithRemoteSync({ prompt, mode: executionMode });
            if (syncError) {
                const reason = syncError instanceof Error ? syncError.message : "未知错误";
                message.warning(`项目已在本地创建，云端同步暂未完成：${reason}`);
            }
            navigate(`/canvas/${id}`);
        } catch (error) {
            message.error(error instanceof Error ? error.message : "项目创建失败");
            setSubmitting(false);
        }
    };

    return (
        <section className="updream-hero flex flex-col items-center px-4 pt-[150px] sm:pt-[205px]">
            <h1 className="updream-hero-title bg-gradient-to-r from-[#b588f5] via-[#9aa8f7] to-[#6fd5f0] bg-clip-text text-center text-[32px] font-medium leading-tight text-transparent sm:text-[36px]">
                让算力更有想象力
            </h1>
            <p className="updream-hero-description mt-4 max-w-[620px] text-center text-[14px] leading-6 sm:text-[15px]">
                从一个想法开始，让 AI 和你一起完成剧本、分镜与影像创作
            </p>

            <div className="updream-composer mt-9 w-full max-w-[700px] rounded-[22px] border border-white/10 bg-[#16171b] p-4 shadow-[0_8px_40px_rgba(0,0,0,0.45)]">
                <div className="updream-composer-input-wrap relative">
                    <textarea
                        value={value}
                        onChange={(event) => setValue(event.target.value)}
                        onKeyDown={(event) => {
                            if (event.key === "Enter" && !event.shiftKey) {
                                event.preventDefault();
                                void startCreating();
                            }
                        }}
                        disabled={submitting}
                        rows={3}
                        className="updream-composer-input w-full resize-none bg-transparent text-[14px] leading-6 text-white/90 outline-none placeholder:text-transparent"
                        aria-label="描述你想创作的内容"
                    />
                    {value === "" ? (
                        <span
                            className={`updream-composer-placeholder pointer-events-none absolute left-0 top-0 text-[14px] leading-6 text-white/35 transition-opacity duration-300 ${
                                placeholderVisible ? "opacity-100" : "opacity-0"
                            }`}
                        >
                            {PLACEHOLDERS[placeholderIndex]}
                        </span>
                    ) : null}
                </div>
                <div className="updream-composer-actions mt-3 flex items-center justify-between">
                    <div className="updream-composer-modes flex items-center gap-1 rounded-lg bg-white/[0.04] p-1" role="group" aria-label="Agent 执行模式">
                        <button
                            type="button"
                            className={`updream-composer-mode flex h-8 items-center gap-1.5 rounded-md px-2.5 text-xs transition-colors ${executionMode === "guided" ? "bg-white/12 text-white" : "text-white/45 hover:text-white/75"}`}
                            onClick={() => setExecutionMode("guided")}
                            aria-pressed={executionMode === "guided"}
                            title="Agent 先完成方案推演，写入画布和触发生成前由你确认"
                        >
                            <ShieldCheck className="updream-composer-mode-icon size-3.5" />
                            执行前确认
                        </button>
                        <button
                            type="button"
                            className={`updream-composer-mode flex h-8 items-center gap-1.5 rounded-md px-2.5 text-xs transition-colors ${executionMode === "automatic" ? "bg-white/12 text-white" : "text-white/45 hover:text-white/75"}`}
                            onClick={() => setExecutionMode("automatic")}
                            aria-pressed={executionMode === "automatic"}
                            title="Agent 完成推演后直接写入画布并执行生成"
                        >
                            <Zap className="updream-composer-mode-icon size-3.5" />
                            自动执行
                        </button>
                    </div>
                    <button
                        type="button"
                        onClick={() => void startCreating()}
                        className={`updream-composer-send flex h-9 w-9 items-center justify-center rounded-full transition-colors ${
                            value.trim() && !submitting ? "bg-white text-black hover:bg-white/85" : "bg-white/15 text-white/40"
                        }`}
                        aria-label={submitting ? "正在创建项目" : "开始创作"}
                        disabled={!value.trim() || submitting}
                    >
                        <ArrowUp className={`updream-composer-send-icon size-[17px] ${submitting ? "animate-pulse" : ""}`} />
                    </button>
                </div>
            </div>
        </section>
    );
}
