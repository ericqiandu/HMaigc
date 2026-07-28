import { useState } from "react";

export function UpdreamVideoBackground() {
    const [loadFailed, setLoadFailed] = useState(false);

    return (
        <div className="updream-video-background" aria-hidden={!loadFailed}>
            <video
                className="updream-video-background-media"
                autoPlay
                loop
                muted
                playsInline
                preload="metadata"
                onCanPlay={() => setLoadFailed(false)}
                onError={() => setLoadFailed(true)}
            >
                <source className="updream-video-background-source" src="/videos/hero.mp4" type="video/mp4" />
            </video>
            <div className="updream-video-background-scrim" />
            <div className="updream-video-background-glow" />
            {loadFailed ? (
                <p className="updream-video-background-error" role="alert">
                    首页背景视频加载失败，请检查 /videos/hero.mp4。
                </p>
            ) : null}
        </div>
    );
}
