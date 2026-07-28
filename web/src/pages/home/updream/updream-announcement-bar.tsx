import { X } from "lucide-react";
import { useState } from "react";

export function UpdreamAnnouncementBar() {
    const [visible, setVisible] = useState(true);

    if (!visible) return null;

    return (
        <div className="updream-announcement relative flex min-h-10 items-center justify-center bg-[#d9edfc] px-12 py-2 text-[13px]">
            <div className="updream-announcement-content flex flex-wrap items-center justify-center gap-2.5 sm:gap-3">
                <span className="updream-announcement-badge rounded-full bg-gradient-to-r from-[#f43f8e] to-[#f06292] px-2.5 py-0.5 text-[12px] font-medium text-white">
                    招募中
                </span>
                <span className="updream-announcement-copy text-center text-[#1f2d3d]">
                    招增长伙伴：懂冷启动、内容增长或海外增长，欢迎加入{" "}
                    <span className="updream-announcement-brand font-semibold">HMaigc</span>。
                </span>
                <button
                    type="button"
                    className="updream-announcement-submit rounded-full bg-gradient-to-r from-[#a855f7] to-[#d946ef] px-4 py-1 text-[12px] font-medium text-white transition-opacity hover:opacity-90"
                >
                    投递
                </button>
                <button
                    type="button"
                    className="updream-announcement-share rounded-full border border-[#c4d7e8] bg-white px-4 py-1 text-[12px] font-medium text-[#1f2d3d] transition-colors hover:bg-[#f2f8fd]"
                >
                    分享
                </button>
            </div>
            <button
                type="button"
                onClick={() => setVisible(false)}
                className="updream-announcement-close absolute right-4 text-[#3b4a5c] transition-colors hover:text-black sm:right-5"
                aria-label="关闭招募公告"
            >
                <X className="updream-announcement-close-icon size-4" />
            </button>
        </div>
    );
}
