import { useState } from "react";

import { staticAssetURL } from "@/lib/static-assets";

const heroVideoURL = staticAssetURL("/videos/hero.mp4");
const heroPosterURL = staticAssetURL("/videos/hero-poster.svg");

export function UpdreamVideoBackground() {
    const [loadFailed, setLoadFailed] = useState(false);

    return (
        <div className="updream-video-background" aria-hidden={!loadFailed}>
            <video className="updream-video-background-media" autoPlay loop muted playsInline preload="metadata" poster={heroPosterURL} onCanPlay={() => setLoadFailed(false)} onError={() => setLoadFailed(true)}>
                <source className="updream-video-background-source" src={heroVideoURL} type="video/mp4" />
            </video>
            <div className="updream-video-background-scrim" />
            <div className="updream-video-background-pattern" />
            <div className="updream-video-background-glow" />
            {loadFailed ? (
                <p className="updream-video-background-error" role="alert">
                    首页背景视频加载失败。
                </p>
            ) : null}
        </div>
    );
}
