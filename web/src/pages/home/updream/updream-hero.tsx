import { ArrowUp, FileText, Image as ImageIcon, Music, Table } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { useNavigate } from "react-router";

const PLACEHOLDERS = [
    '试试说"在画布上为我创建…"，生成不阻塞，随时开启下一轮对话',
    "描述你想创作的内容，AI 帮你生成分镜",
    "进入项目后，按 @ 可引用资产库素材",
] as const;

const INPUT_TOOLS = [
    { label: "添加图片", Icon: ImageIcon },
    { label: "添加文档", Icon: FileText },
    { label: "添加音频", Icon: Music },
    { label: "添加表格", Icon: Table },
] as const;

export function UpdreamHero() {
    const navigate = useNavigate();
    const [value, setValue] = useState("");
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

    const startCreating = () => {
        if (!value.trim()) return;
        navigate(`/canvas?prompt=${encodeURIComponent(value.trim())}`);
    };

    return (
        <section className="updream-hero flex flex-col items-center px-4 pt-[150px] sm:pt-[205px]">
            <h1 className="updream-hero-title bg-gradient-to-r from-[#b588f5] via-[#9aa8f7] to-[#6fd5f0] bg-clip-text text-center text-[32px] font-medium leading-tight text-transparent sm:text-[36px]">
                今天想拍点什么故事？
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
                                startCreating();
                            }
                        }}
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
                    <div className="updream-composer-tools flex items-center gap-2.5">
                        {INPUT_TOOLS.map(({ label, Icon }) => (
                            <button
                                key={label}
                                type="button"
                                className="updream-composer-tool flex h-8 w-8 items-center justify-center rounded-[8px] border border-white/10 bg-white/[0.04] text-white/50 transition-colors hover:bg-white/10 hover:text-white/80"
                                aria-label={label}
                                title={label}
                            >
                                <Icon className="updream-composer-tool-icon size-[15px]" />
                            </button>
                        ))}
                    </div>
                    <button
                        type="button"
                        onClick={startCreating}
                        className={`updream-composer-send flex h-9 w-9 items-center justify-center rounded-full transition-colors ${
                            value.trim() ? "bg-white text-black hover:bg-white/85" : "bg-white/15 text-white/40"
                        }`}
                        aria-label="开始创作"
                        disabled={!value.trim()}
                    >
                        <ArrowUp className="updream-composer-send-icon size-[17px]" />
                    </button>
                </div>
            </div>
        </section>
    );
}
