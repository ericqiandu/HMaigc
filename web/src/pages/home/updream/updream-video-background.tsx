import { useState } from "react";

import { useDeferredMedia } from "@/hooks/use-deferred-media";
import { staticAssetURL } from "@/lib/static-assets";

const heroVideoURL = staticAssetURL("/videos/hero.mp4");
const heroPosterURL = staticAssetURL("/videos/hero-poster.svg");

export function UpdreamVideoBackground() {
    const [loadFailed, setLoadFailed] = useState(false);
    const mediaEnabled = useDeferredMedia();

    return (
        <div className="updream-video-background" aria-hidden={!loadFailed}>
            <video
                key={mediaEnabled ? "active" : "poster"}
                className="updream-video-background-media"
                autoPlay
                loop
                muted
                playsInline
                preload={mediaEnabled ? "metadata" : "none"}
                poster={heroPosterURL}
                onCanPlay={() => setLoadFailed(false)}
                onError={() => setLoadFailed(true)}
            >
                {mediaEnabled ? <source className="updream-video-background-source" src={heroVideoURL} type="video/mp4" /> : null}
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
